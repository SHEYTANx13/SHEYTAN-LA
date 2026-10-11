package chunking

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkFormatFileAttachment1MB measures the legacy attached-file compose
// path with a 1 MB text file. Baseline v1.2.3 reads the whole file AND makes a
// full string copy before windowing, on every turn composition.
func BenchmarkFormatFileAttachment1MB(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "sample.log")
	var sb strings.Builder
	line := "2026-09-16T00:00:00Z INFO request handled in 12ms status=200 path=/api/v1/items\n"
	for sb.Len() < 1<<20 {
		sb.WriteString(line)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(sb.Len()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := FormatFileAttachment(path, DefaultAttachmentBudgetBytes)
		if out == "" {
			b.Fatal("empty output")
		}
	}
}
