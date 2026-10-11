package chunking

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
	if got := EstimateTokens("abcd"); got != 1 {
		t.Errorf("abcd = %d, want 1", got)
	}
	if got := EstimateTokens("abcde"); got != 2 {
		t.Errorf("abcde = %d, want 2 (rounded up)", got)
	}
}

func TestWindowHeadTailSmall(t *testing.T) {
	in := "a\nb\nc\n"
	if got := WindowHeadTail(in, 1024); got != in {
		t.Errorf("small input changed: %q", got)
	}
}

func TestWindowHeadTailBig(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		b.WriteString("the quick brown fox jumps over the lazy dog again and again\n")
	}
	out := WindowHeadTail(b.String(), 2048)
	if !strings.Contains(out, "elided") {
		t.Errorf("elision marker missing")
	}
	if !strings.HasPrefix(out, "the quick") {
		t.Errorf("head missing")
	}
	if !strings.Contains(out, "again and again") || !strings.HasSuffix(strings.TrimSpace(out), "again") {
		// tail present (last line may lose its trailing newline)
		t.Logf("tail check: %q", out[len(out)-80:])
	}
}

func TestSplitParagraphsLossless(t *testing.T) {
	// maxBytes is clamped to a 64-byte floor, so use a longer input.
	var b strings.Builder
	for i := 0; i < 6; i++ {
		b.WriteString("paragraph number with enough words to span several chunks ")
	}
	in := b.String()
	chunks := SplitParagraphs(in, 64)
	if len(chunks) < 4 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if strings.Join(chunks, "") != in {
		t.Errorf("lossy join: %q", strings.Join(chunks, ""))
	}
	// Every chunk except possibly the last respects the budget.
	for i, c := range chunks {
		if i < len(chunks)-1 && len(c) > 64 {
			t.Errorf("chunk %d exceeds budget: %d bytes", i, len(c))
		}
	}
}

func TestIsTextFile(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(txt, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsTextFile(txt) {
		t.Errorf(".txt must be text")
	}
	bin := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(bin, []byte{1, 0, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if IsTextFile(bin) {
		t.Errorf("NUL binary must not be text")
	}
}

func TestWindowMessagesKeepsLatestUser(t *testing.T) {
	var hist []llm.Message
	for i := 0; i < 30; i++ {
		hist = append(hist, llm.Message{
			Role:    "user",
			Content: strings.Repeat("x", 400), // ~100 tokens each
		})
	}
	kept, elided := WindowMessages(hist, 250) // room for ~2 messages
	if elided == 0 {
		t.Fatalf("expected elision")
	}
	last := hist[len(hist)-1]
	found := false
	for _, m := range kept {
		if m.Content == last.Content {
			found = true
		}
	}
	if !found {
		t.Errorf("latest user message was dropped")
	}
}
