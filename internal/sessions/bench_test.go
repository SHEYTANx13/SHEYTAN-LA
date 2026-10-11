package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// BenchmarkAppendMessageGrowing measures the hot per-message write path on a
// session that already carries 400 prior messages (~ realistic long session).
// Baseline v1.2.3 re-reads + re-marshals + rewrites the whole session file on
// every append.
func BenchmarkAppendMessageGrowing(b *testing.B) {
	dir := b.TempDir()
	st := New(dir)
	s := st.Create()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Seed 400 messages, then persist so the file exists on disk.
	for i := 0; i < 400; i++ {
		s.Messages = append(s.Messages, llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("seed message %d with some realistic payload text to give the session file weight and measure the append cost honestly", i),
			At:      base.Add(time.Duration(i*2) * time.Minute),
		})
		s.Messages = append(s.Messages, llm.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("assistant reply %d — a moderately long answer with enough bulk to approach real transcript sizes in aggregate.", i),
			At:      base.Add(time.Duration(i*2+1) * time.Minute),
		})
	}
	if err := st.Save(s); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, s.ID+".json")
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("seeded session file: %d bytes, %d messages", info.Size(), len(s.Messages))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.AppendMessage(s.ID, llm.Message{
			Role:    "user",
			Content: "bench append message",
			At:      time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetLoaded measures session reads (per UI load / per turn).
func BenchmarkGetLoaded(b *testing.B) {
	dir := b.TempDir()
	st := New(dir)
	s := st.Create()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 400; i++ {
		s.Messages = append(s.Messages, llm.Message{Role: "user", Content: fmt.Sprintf("payload %d — realistic transcript line for read benchmarking.", i), At: base.Add(time.Duration(i) * time.Minute)})
	}
	if err := st.Save(s); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.Get(s.ID); err != nil {
			b.Fatal(err)
		}
	}
}
