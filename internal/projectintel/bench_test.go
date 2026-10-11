package projectintel

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkCardSteady measures the per-turn project card render.
// Baseline v1.2.3 re-reads + re-parses the per-project JSON file on every call.
func BenchmarkCardSteady(b *testing.B) {
	root := b.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "tools"), 0o755); err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"main.go", "go.mod", "internal/tools/tools.go", "README.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package tools\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	st := NewStore(b.TempDir())
	if _, err := st.Observe(root); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if card := st.Card(root); card == "" {
			b.Fatal("expected non-empty card")
		}
	}
}
