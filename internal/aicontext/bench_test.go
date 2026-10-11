package aicontext

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// BenchmarkSystemMessageSteady measures the per-turn system message build.
// Baseline v1.2.3 re-reads the AI-CONTEXT.md file from disk on every turn.
func BenchmarkSystemMessageSteady(b *testing.B) {
	dir := b.TempDir()
	cfg := &config.Config{DataDir: dir}
	if _, err := EnsureFile(dir); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := SystemMessageWithTools(cfg, []string{"files", "shell", "git"})
		if strings.TrimSpace(msg) == "" {
			b.Fatal("empty system message")
		}
	}
}
