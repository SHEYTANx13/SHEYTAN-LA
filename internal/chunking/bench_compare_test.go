// bench_compare_test.go — controlled same-binary comparison of the legacy
// buildChunks algorithm vs the new ChunkText on identical inputs.
package chunking

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

type benchChunk struct {
	ID      string
	Index   int
	Hash    string
	Offset  int
	Bytes   int
	Tokens  int
	Preview string
}

func legacyBuildChunks(attID, text string, chunkSize, maxChunks int) []benchChunk {
	if chunkSize < 256 {
		chunkSize = 256
	}
	if maxChunks < 1 {
		maxChunks = 1
	}
	if text == "" {
		return nil
	}
	parts := SplitParagraphs(text, chunkSize)
	if len(parts) > maxChunks {
		parts = parts[:maxChunks]
	}
	out := make([]benchChunk, 0, len(parts))
	offset := 0
	for i, p := range parts {
		idx := strings.Index(text[offset:], p)
		start := offset
		if idx >= 0 {
			start = offset + idx
		}
		sum := sha256.Sum256([]byte(p))
		hash := hex.EncodeToString(sum[:])
		out = append(out, benchChunk{
			ID:      fmt.Sprintf("%s:%d:%s", attID, i, hash[:8]),
			Index:   i,
			Hash:    hash,
			Offset:  start,
			Bytes:   len(p),
			Tokens:  EstimateTokens(p),
			Preview: legacyClip(strings.TrimSpace(p), 120),
		})
		offset = start + len(p)
	}
	return out
}

func legacyClip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func benchCorpus() string {
	var tb strings.Builder
	for i := 0; i < 90_000; i++ {
		fmt.Fprintf(&tb, "func handler%d(w http.ResponseWriter, r *http.Request) {\n\tnext := step(%d)\n\n\tif next > 0 {\n\t\treturn\n\t}\n}\n\n", i%7, i)
	}
	return tb.String()
}

func BenchmarkCompareLegacy(b *testing.B) {
	text := benchCorpus()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = legacyBuildChunks("att", text, 4096, 512)
	}
}

func BenchmarkCompareNew(b *testing.B) {
	text := benchCorpus()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ChunkText("aabbccddeeff", text, ChunkerConfig{MaxBytes: 4096, MaxChunks: 512, WithPreview: true})
	}
}

func BenchmarkCompareNewNoPreview(b *testing.B) {
	text := benchCorpus()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ChunkText("aabbccddeeff", text, ChunkerConfig{MaxBytes: 4096, MaxChunks: 512, WithPreview: false})
	}
}
