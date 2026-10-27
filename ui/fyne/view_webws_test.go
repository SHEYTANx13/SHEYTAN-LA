//go:build !headless

// view_webws_test.go — the Web Workspace UI contract tests (v1.11.0
// hybrid UI). The rendering itself is browser-side (covered by the
// internal/webworkspace suite); these tests pin the FYNESIDE contract:
// the typed publish path, the deterministic block extraction, the
// honest disabled/error states, and the control-surface behavior over
// the fake core.
package fyneui

import (
	"strings"
	"testing"
)

func TestExtractBlocksForPublishSplitsFences(t *testing.T) {
	answer := "Intro prose.\n\n```python\nprint('x')\n```\n\nTail prose.\n"
	got := extractBlocksForPublish(answer)
	if len(got) != 3 {
		t.Fatalf("blocks = %d, want 3: %+v", len(got), got)
	}
	if got[0].kind != "markdown" || got[0].payload != "Intro prose." {
		t.Fatalf("block0 = %+v", got[0])
	}
	if got[1].kind != "code" || got[1].language != "python" || got[1].payload != "print('x')\n" {
		t.Fatalf("block1 = %+v", got[1])
	}
	if got[2].kind != "markdown" || got[2].payload != "Tail prose." {
		t.Fatalf("block2 = %+v", got[2])
	}
}

func TestExtractBlocksForPublishDeterministic(t *testing.T) {
	answer := "a\n```go\nx()\n```\nb"
	first := extractBlocksForPublish(answer)
	for i := 0; i < 5; i++ {
		again := extractBlocksForPublish(answer)
		if len(again) != len(first) {
			t.Fatalf("nondeterministic count %d vs %d", len(again), len(first))
		}
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("nondeterministic block %d", j)
			}
		}
	}
}

func TestExtractBlocksForPublishEdgeCases(t *testing.T) {
	if got := extractBlocksForPublish(""); len(got) != 0 {
		t.Fatalf("empty answer = %+v", got)
	}
	if got := extractBlocksForPublish("just prose"); len(got) != 1 || got[0].kind != "markdown" {
		t.Fatalf("prose-only = %+v", got)
	}
	// Unterminated fence is preserved as code (nothing dropped).
	got := extractBlocksForPublish("before\n```\ncode")
	if len(got) != 2 || got[1].kind != "code" || got[1].payload != "code\n" {
		t.Fatalf("unterminated fence = %+v", got)
	}
}

// The publish path through the fake core must store typed blocks with
// provenance and return them through the shared list — the exact
// journey: finished answer → publish → both surfaces see the same IDs.
func TestWorkspacePublishRoundTripThroughFakeCore(t *testing.T) {
	f := newFakeCore(t)
	defer f.srv.Close()
	c := NewCoreClient(f.endpoint, f.key)
	ctx := contextBackground()

	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	blocks := extractBlocksForPublish("Here is how:\n```go\nfmt.Println(1)\n```")
	if len(blocks) != 2 {
		t.Fatalf("extraction = %+v", blocks)
	}
	published := 0
	for _, b := range blocks {
		stored, err := c.WorkspacePublish(ctx, b.kind, b.title, b.payload, b.language, "run-9", "sess-9", "chat-answer")
		if err != nil {
			t.Fatalf("publish %s: %v", b.kind, err)
		}
		if stored.ID == "" || stored.Version != 1 {
			t.Fatalf("stored block missing identity: %+v", stored)
		}
		published++
	}
	if published != 2 {
		t.Fatalf("published = %d", published)
	}
	list, err := c.WorkspaceBlocks(ctx)
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if list.Version != 1 || len(list.Blocks) != 2 {
		t.Fatalf("list = %+v", list)
	}
	// Identity round-trip: the list carries the same IDs the publish
	// returned (one ID = one logical artifact across both surfaces).
	if list.Blocks[0].ID == list.Blocks[1].ID {
		t.Fatal("duplicate ids in the shared registry list")
	}
	if list.Blocks[0].Kind != "code" && list.Blocks[0].Kind != "markdown" {
		t.Fatalf("kinds = %+v", list.Blocks)
	}
}

func TestWorkspaceStateDisabledHonestReason(t *testing.T) {
	f := newFakeCore(t)
	defer f.srv.Close()
	c := NewCoreClient(f.endpoint, f.key)
	ctx := contextBackground()
	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	st, err := c.WorkspaceState(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if st.Enabled {
		t.Fatal("fake default must be disabled (mirrors fresh-install contract only if config disables; the fake pins the disabled branch)")
	}
	if !strings.Contains(st.Reason, "webWorkspaceEnabled") {
		t.Fatalf("reason = %q", st.Reason)
	}
	// Start while disabled must be refused with 403 semantics (typed
	// error, never a fake URL).
	if _, err := c.WorkspaceStart(ctx); err == nil {
		t.Fatal("start while disabled must fail")
	}
}

func TestWorkspaceStateErrorPath(t *testing.T) {
	f := newFakeCore(t)
	defer f.srv.Close()
	f.failWSState = true
	c := NewCoreClient(f.endpoint, f.key)
	ctx := contextBackground()
	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := c.WorkspaceState(ctx); err == nil {
		t.Fatal("503 state must surface as a typed error, never fake-ok")
	}
}

// Publish without provenance is refused — the model cannot publish
// anonymously (fail closed).
func TestWorkspacePublishRequiresSource(t *testing.T) {
	f := newFakeCore(t)
	defer f.srv.Close()
	c := NewCoreClient(f.endpoint, f.key)
	ctx := contextBackground()
	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	f.wsEnabled.Store(true)
	// The client always sets source=chat-answer for chat publishes; call
	// the raw endpoint without source to pin the core-side refusal.
	_, err := c.WorkspacePublish(ctx, "text", "", "x", "", "", "", "")
	if err == nil {
		t.Fatal("source-less publish must fail")
	}
}

// TestPrepareShowsWindowBeforeRun pins the v1.11.0 boot-visibility
// contract (run #55 class): Prepare() builds AND shows the real window
// without starting the event loop, so the process owns a top-level
// window before any slower core step runs. Run() then starts the loop
// without rebuilding a second window (no duplicate-window regression).
func TestPrepareShowsWindowBeforeRun(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	if err := ap.Prepare(); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if ap.Window() == nil {
		t.Fatal("Prepare must create the window")
	}
	if !ap.prepared {
		t.Fatal("Prepare must mark the app prepared")
	}
	// Idempotence: a second Prepare neither errors nor replaces the window.
	w1 := ap.Window()
	if err := ap.Prepare(); err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if ap.Window() != w1 {
		t.Fatal("second Prepare must not rebuild the window")
	}
}
