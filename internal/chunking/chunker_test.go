// chunker_test.go — Phase 3 chunk-engine tests: determinism, boundary
// correctness, UTF-8 safety, metadata integrity, overlap semantics, and
// equivalence of SplitParagraphs with the pre-Phase-3 reference algorithm.
package chunking

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// legacySplitParagraphs is the pre-Phase-3 algorithm, kept here as the
// behavioral reference: with ASCII-only input the refactored
// SplitParagraphs must produce byte-identical chunks.
func legacySplitParagraphs(text string, maxBytes int) []string {
	if maxBytes < 64 {
		maxBytes = 64
	}
	if text == "" {
		return nil
	}
	var out []string
	rest := text
	for len(rest) > maxBytes {
		cut := boundary(rest, maxBytes)
		if cut <= 0 {
			cut = maxBytes
		}
		out = append(out, rest[:cut])
		rest = rest[cut:]
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

func TestSplitParagraphsMatchesLegacyForASCII(t *testing.T) {
	inputs := []string{
		strings.Repeat("paragraph line\n\nnext block with data\n", 300),
		strings.Repeat("no blank lines here, just lines\n", 500),
		strings.Repeat("x", 10_000), // no newlines at all → hard splits
		"tiny",
		strings.Repeat("word ", 2000) + "\n\n" + strings.Repeat("tail ", 800),
	}

	for i, in := range inputs {
		got := SplitParagraphs(in, 4096)
		want := legacySplitParagraphs(in, 4096)

		if len(got) != len(want) {
			t.Fatalf("case %d: chunk count %d != legacy %d", i, len(got), len(want))
		}

		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("case %d chunk %d differs:\n got %q\nwant %q", i, j, got[j][:30], want[j][:30])
			}
		}
	}
}

func TestSplitParagraphsLosslessReconstruction(t *testing.T) {
	inputs := []string{
		strings.Repeat("paragraph line\n\nnext block with data\n", 300),
		strings.Repeat("你好世界，这是一段中文文本。\n\n第二段。\n", 400),
		strings.Repeat("émoji 🎉 and more unicode ©®\n", 600),
		strings.Repeat("x", 9999),
	}

	for i, in := range inputs {
		chunks := SplitParagraphs(in, 1024)
		var b strings.Builder
		for _, c := range chunks {
			b.WriteString(c)
		}
		if b.String() != in {
			t.Fatalf("case %d: reconstruction is not lossless", i)
		}
	}
}

func TestChunkTextDeterministic(t *testing.T) {
	text := strings.Repeat("alpha bravo charlie delta\n\necho foxtrot\n", 400)

	a := ChunkText("", text, ChunkerConfig{MaxBytes: 2048, WithPreview: true})
	b := ChunkText("", text, ChunkerConfig{MaxBytes: 2048, WithPreview: true})

	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("chunk count mismatch: %d vs %d", len(a), len(b))
	}

	for i := range a {
		if a[i].ID != b[i].ID || a[i].Hash != b[i].Hash || a[i].Offset != b[i].Offset {
			t.Fatalf("chunk %d not deterministic: %+v vs %+v", i, a[i], b[i])
		}
	}

	// Same source hash must yield the same IDs as computing it inline.
	c := ChunkText(a[0].SourceHash, text, ChunkerConfig{MaxBytes: 2048, WithPreview: true})
	if c[0].ID != a[0].ID {
		t.Fatalf("explicit source hash changed the ID: %s vs %s", c[0].ID, a[0].ID)
	}

	// Different parameters must change IDs.
	d := ChunkText(a[0].SourceHash, text, ChunkerConfig{MaxBytes: 4096, WithPreview: true})
	if d[0].ID == a[0].ID {
		t.Fatalf("different config produced identical IDs")
	}
}

func TestChunkTextMetadataIntegrity(t *testing.T) {
	text := strings.Repeat("line one of the paragraph\n\nline two follows\n", 300)

	chunks := ChunkText("", text, ChunkerConfig{MaxBytes: 1024, WithPreview: true})
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}

	srcHash := chunks[0].SourceHash

	for i, c := range chunks {
		if c.Index != i {
			t.Fatalf("chunk %d has Index %d", i, c.Index)
		}
		if c.Total != len(chunks) {
			t.Fatalf("chunk %d Total %d != %d", i, c.Total, len(chunks))
		}
		if c.Bytes <= 0 || c.Offset < 0 {
			t.Fatalf("chunk %d bad range: offset=%d bytes=%d", i, c.Offset, c.Bytes)
		}
		if c.Offset+int64(c.Bytes) > int64(len(text)) {
			t.Fatalf("chunk %d range overruns source", i)
		}

		// Byte range must reproduce the chunk text exactly.
		slice := text[c.Offset : c.Offset+int64(c.Bytes)]
		sum := sha256.Sum256([]byte(slice))
		if hex.EncodeToString(sum[:]) != c.Hash {
			t.Fatalf("chunk %d hash does not match its byte range", i)
		}

		if c.SourceHash != srcHash {
			t.Fatalf("chunk %d source hash mismatch", i)
		}
		if c.Version != ProcessingVersion {
			t.Fatalf("chunk %d processing version %d != %d", i, c.Version, ProcessingVersion)
		}
		if c.Tokens <= 0 {
			t.Fatalf("chunk %d has no token estimate", i)
		}
		if c.Preview == "" {
			t.Fatalf("chunk %d missing preview", i)
		}
		if c.ID == "" {
			t.Fatalf("chunk %d missing ID", i)
		}

		// Contiguity (no overlap configured): each chunk starts where the
		// previous ended.
		if i > 0 {
			prev := chunks[i-1]
			if c.Offset != prev.Offset+int64(prev.Bytes) {
				t.Fatalf("chunk %d not contiguous with %d", i, i-1)
			}
		}
	}
}

func TestChunkTextUTF8SafeHardSplit(t *testing.T) {
	// Multi-byte runes with NO newlines: the split must be a hard split —
	// every chunk must remain valid UTF-8.
	text := strings.Repeat("你好世界", 3000) // 48k bytes, 4-byte runes

	chunks := ChunkText("", text, ChunkerConfig{MaxBytes: 997}) // odd size → mid-rune cuts

	for i, c := range chunks {
		slice := text[c.Offset : c.Offset+int64(c.Bytes)]
		if !utf8.ValidString(slice) {
			t.Fatalf("chunk %d is not valid UTF-8", i)
		}
	}
}

func TestChunkTextMaxChunksCap(t *testing.T) {
	text := strings.Repeat("paragraph\n\n", 2000)

	chunks := ChunkText("", text, ChunkerConfig{MaxBytes: 512, MaxChunks: 7})

	if len(chunks) != 7 {
		t.Fatalf("expected cap of 7 chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.Total != 7 || c.Index != i {
			t.Fatalf("chunk %d metadata wrong after cap: %+v", i, c)
		}
	}
}

func TestChunkTextOverlapReincludesTail(t *testing.T) {
	// No newlines → hard splits; overlap must re-include the previous
	// chunk's tail and never lose progress.
	text := strings.Repeat("0123456789", 1000) // 10k bytes

	chunks := ChunkText("", text, ChunkerConfig{MaxBytes: 1000, Overlap: 100})

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	for i := 1; i < len(chunks); i++ {
		prev := chunks[i-1]
		cur := chunks[i]

		// Overlap: current chunk must start before the previous one ends.
		if cur.Offset >= prev.Offset+int64(prev.Bytes) {
			t.Fatalf("chunk %d has no overlap with previous", i)
		}

		// But must still make progress (never identical intervals).
		if cur.Offset+int64(cur.Bytes) <= prev.Offset+int64(prev.Bytes) {
			t.Fatalf("chunk %d makes no progress", i)
		}

		// The overlap content must match the previous chunk's tail.
		ov := int(prev.Offset + int64(prev.Bytes) - cur.Offset)
		if ov > 0 {
			tail := text[prev.Offset+int64(prev.Bytes)-int64(ov) : prev.Offset+int64(prev.Bytes)]
			head := text[cur.Offset : cur.Offset+int64(ov)]
			if tail != head {
				t.Fatalf("chunk %d overlap content mismatch", i)
			}
		}
	}
}

func TestChunkTextOverlapTinyChunksTerminates(t *testing.T) {
	// Pathological: blank-line cuts smaller than the overlap — the chunker
	// must still terminate and produce valid chunks.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "s%d\n\n", i%10) // tiny paragraphs
	}
	text := b.String()

	chunks := ChunkText("", text, ChunkerConfig{MaxBytes: 64, Overlap: 63})

	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}

	// Invariant: chunk ends never DECREASE (they may stagnate for at most
	// a bounded number of degenerate overlap steps) and the last chunk
	// reaches the end of the input — the pass always terminates and covers.
	lastEnd := int64(-1)
	for i, c := range chunks {
		if c.Offset+int64(c.Bytes) < lastEnd {
			t.Fatalf("chunk %d end went backwards: %d < %d", i, c.Offset+int64(c.Bytes), lastEnd)
		}
		lastEnd = c.Offset + int64(c.Bytes)
	}
	if lastEnd < int64(len(text))-int64(64) {
		t.Fatalf("chunking did not cover the input: lastEnd=%d len=%d", lastEnd, len(text))
	}
}

func TestChunkTextEmpty(t *testing.T) {
	if got := ChunkText("", "", ChunkerConfig{MaxBytes: 512}); got != nil {
		t.Fatalf("expected nil for empty text, got %d chunks", len(got))
	}
}

// --- benchmarks -------------------------------------------------------------

func benchText() string {
	var b strings.Builder
	for i := 0; i < 90_000; i++ {
		fmt.Fprintf(&b, "func handler%d(w http.ResponseWriter, r *http.Request) {\n\tnext := step(%d)\n\n\tif next > 0 {\n\t\treturn\n\t}\n}\n\n", i%7, i)
	}
	return b.String()
}

func BenchmarkChunkText4KB(b *testing.B) {
	text := benchText()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ChunkText("", text, ChunkerConfig{MaxBytes: 4096, MaxChunks: 512, WithPreview: true})
	}
}

func BenchmarkSplitParagraphs4KB(b *testing.B) {
	text := benchText()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SplitParagraphs(text, 4096)
	}
}
