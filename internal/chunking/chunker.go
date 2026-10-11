// chunker.go — the derived-chunk engine (v1.1.5 Phase 3).
//
// ChunkText is the single chunk derivation used by every pipeline stage
// that needs CHUNKS with full provenance (attachments retrieval today,
// any future structural indexing tomorrow). It improves on the raw
// SplitParagraphs primitive with:
//
//   - a single interval pass (no rescanning, no repeated copying — chunk
//     strings share the source backing array);
//   - exact byte offsets recorded during the pass (the old attachment
//     chunker re-scanned the text with strings.Index per chunk);
//   - configurable overlap (rune-aligned, line-preferred) without
//     unbounded memory;
//   - UTF-8-correct hard splits (a hard cut backs up to a rune boundary —
//     the old primitive could slice a multi-byte rune in half);
//   - deterministic chunk IDs derived from source content hash +
//     processing parameters + chunk content hash;
//   - full chunk metadata: source identity, byte range, estimated
//     tokens, sequence index, total chunks, processing version.
//
// Backward compatibility: with Overlap=0 the cut decisions are exactly
// SplitParagraphs' (blank line, then newline, then hard split), so every
// existing consumer sees the same chunk boundaries it always did.
package chunking

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ProcessingVersion is the processing version stamped into every derived
// chunk and baked into cache keys. Bump it whenever the derivation logic
// changes shape so stale derivatives can never be served.
const ProcessingVersion = 2

// previewRunes is the default preview length (runes) for chunks.
const previewRunes = 120

// Chunk is one derived chunk with full provenance. It is a pure value:
// it references the SOURCE via offsets/hashes, never by retaining it.
type Chunk struct {
	// ID is deterministic from source content hash + processing
	// parameters + chunk content hash: the same source processed with the
	// same configuration always yields the same IDs.
	ID string `json:"id"`

	// Index / Total locate the chunk in the produced sequence.
	Index int `json:"index"`
	Total int `json:"total"`

	// Offset and Bytes delimit the chunk's byte range in the source text
	// (post-normalization). Re-reading that range reproduces the chunk
	// byte-for-byte; concatenating all ranges reproduces the source.
	Offset int64 `json:"offset"`
	Bytes  int   `json:"bytes"`

	// Tokens is the shared estimated token count for the chunk text.
	Tokens int `json:"tokens"`

	// Hash is the hex SHA-256 of the chunk text (content identity of the
	// chunk itself). SourceHash is the hex SHA-256 of the whole source.
	Hash       string `json:"hash"`
	SourceHash string `json:"sourceHash,omitempty"`

	// Preview is a short prefix of the trimmed chunk text (cheap
	// first-pass retrieval signal — full text is never retained here).
	Preview string `json:"preview,omitempty"`

	// Version is the ProcessingVersion that produced this chunk.
	Version int `json:"version"`
}

// ChunkerConfig controls ChunkText. Zero fields fall back to the same
// defaults the raw primitive used (MaxBytes 64 floor; no overlap; no cap).
type ChunkerConfig struct {
	// MaxBytes is the maximum chunk size in bytes (floor 64).
	MaxBytes int

	// Overlap is the number of bytes re-included from the tail of the
	// previous chunk (floor-aligned to a rune start, preferring a line
	// boundary). 0 = no overlap (the historical behavior).
	Overlap int

	// MaxChunks caps the produced chunk count (0 = unlimited). When the
	// cap applies the earliest chunks win.
	MaxChunks int

	// WithPreview produces the trimmed preview (default: on).
	WithPreview bool
}

// fingerprint returns a stable short string describing the processing
// parameters — part of every chunk ID so IDs change when parameters do.
func (c ChunkerConfig) fingerprint() string {
	overlap := 0
	if c.Overlap > 0 {
		overlap = c.Overlap
	}
	maxChunks := 0
	if c.MaxChunks > 0 {
		maxChunks = c.MaxChunks
	}
	return fmt.Sprintf("v%d.%d.%d.%d", ProcessingVersion, c.MaxBytes, overlap, maxChunks)
}

// normalizeChunkerConfig applies floors/defaults.
func normalizeChunkerConfig(cfg ChunkerConfig) ChunkerConfig {
	if cfg.MaxBytes < 64 {
		cfg.MaxBytes = 64
	}
	if cfg.Overlap < 0 {
		cfg.Overlap = 0
	}
	if cfg.Overlap >= cfg.MaxBytes {
		cfg.Overlap = cfg.MaxBytes / 4
	}
	if cfg.MaxChunks < 0 {
		cfg.MaxChunks = 0
	}
	return cfg
}

// chunkIntervals is the single-pass split: it returns the byte intervals
// [lo,hi) of every chunk (before any MaxChunks cap). For Overlap=0 the
// intervals are contiguous and their concatenation reproduces the source
// exactly; with overlap the previous chunk's tail is re-included.
//
// Cut decisions for Overlap=0 are byte-identical to the historical
// SplitParagraphs: blank-line boundary first, then newline, then hard
// split — with one improvement: a hard split backs up to a rune boundary
// so no chunk ever ends mid-rune.
//
// Termination: every emitted interval ends strictly after the previous
// one begins, and the next start never moves backwards past the previous
// start (overlap clamps to prevStart+1 when the previous chunk is shorter
// than the requested overlap), so the loop always makes progress.
func chunkIntervals(text string, cfg ChunkerConfig) [][2]int {
	var out [][2]int
	start := 0
	n := len(text)

	for n-start > cfg.MaxBytes {
		cut := boundary(text[start:], cfg.MaxBytes)
		end := start + cfg.MaxBytes // hard-split default

		if cut > 0 {
			end = start + cut
		} else {
			// Hard split: back up to a UTF-8 rune boundary so the chunk
			// ends cleanly (only reachable when no newline exists within
			// the window — a pure-byte split once sliced runes).
			for end > start && !utf8.RuneStart(text[end]) {
				end--
			}
		}

		if end <= start {
			end = start + cfg.MaxBytes
		}

		out = append(out, [2]int{start, end})

		prevStart := start
		start = end

		if cfg.Overlap > 0 && start < n {
			ov := start - cfg.Overlap
			if ov < 0 {
				ov = 0
			}
			for ov < start && !utf8.RuneStart(text[ov]) {
				ov++
			}
			if ov <= prevStart {
				// Previous chunk shorter than the requested overlap: never
				// move backwards — that would re-emit the same interval.
				ov = prevStart + 1
				for ov < start && !utf8.RuneStart(text[ov]) {
					ov++
				}
			}
			if ov >= start {
				ov = start // no room left to re-include
			}
			start = ov
		}
	}

	if start < n {
		out = append(out, [2]int{start, n})
	}

	return out
}

// ChunkText derives the full chunk set for text. sourceHash is the
// content identity of the source (hex SHA-256 of the normalized bytes);
// callers that already hashed their content pass it in to avoid hashing
// the same bytes twice — when empty it is computed here.
func ChunkText(sourceHash, text string, cfg ChunkerConfig) []Chunk {
	cfg = normalizeChunkerConfig(cfg)
	if text == "" {
		return nil
	}

	if sourceHash == "" {
		sum := sha256.Sum256([]byte(text))
		sourceHash = hex.EncodeToString(sum[:])
	}

	intervals := chunkIntervals(text, cfg)
	if cfg.MaxChunks > 0 && len(intervals) > cfg.MaxChunks {
		intervals = intervals[:cfg.MaxChunks]
	}

	total := len(intervals)
	cfgID := cfg.fingerprint()
	out := make([]Chunk, 0, total)

	for i, iv := range intervals {
		part := text[iv[0]:iv[1]] // shares backing — no copy
		sum := sha256.Sum256([]byte(part))
		hash := hex.EncodeToString(sum[:])

		c := Chunk{
			ID:         fmt.Sprintf("%s-%s:%d/%d:%s", sourceHash[:12], cfgID, i, total, hash[:8]),
			Index:      i,
			Total:      total,
			Offset:     int64(iv[0]),
			Bytes:      iv[1] - iv[0],
			Tokens:     EstimateTokens(part),
			Hash:       hash,
			SourceHash: sourceHash,
			Version:    ProcessingVersion,
		}
		if cfg.WithPreview {
			c.Preview = clipRunesLocal(strings.TrimSpace(part), previewRunes)
		}
		out = append(out, c)
	}

	return out
}

// clipRunes truncates to at most n runes (preview helper; local to avoid a
// dependency cycle with the attachments package which has its own copy).
//
// v1.1.5 Phase 3: byte-scanning instead of a []rune conversion — clipping
// a 4 KiB chunk used to allocate a full rune slice per chunk just to keep
// 120 runes; the decode loop touches only the first ~n runes and allocates
// nothing but the result.
func clipRunesLocal(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	i := 0
	for k := 0; k < n && i < len(s); k++ {
		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
	}

	return s[:i] + "…"
}
