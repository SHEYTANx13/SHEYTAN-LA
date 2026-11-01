//go:build !headless

// artifact_card_test.go — UI/UX Phase 2 (Artifact / Workspace Integration)
// contracts over the Fyne test harness:
//
//   - the native artifact card renders the LATEST SETTLED rich block only —
//     the provisional live bubble can never promote itself onto it;
//   - no actionable Workspace entry exists for empty content (no blank
//     surface);
//   - Open resolves the REGISTERED block at open time and degrades visibly
//     and safely on a stale (evicted) reference;
//   - the Workspace status is truthful across open/close;
//   - Chat and Agent each promote their own settled assistant responses
//     (separate conversation spaces, recorded in the block source).
package fyneui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/presentation"
)

// newCardTestApp builds the test app AND the shell, then navigates to the
// chat and agent views (the shell builds views lazily) so ap.chat/ap.agent
// are the shell-owned view instances the production settle path drives.
func newCardTestApp(t *testing.T) (*App, *artifactCard) {
	t.Helper()
	ap, _ := newTestAppForShell(t)
	ap.shell = newShell(ap)
	_ = ap.shell.root() // assembles the body the navigation swaps into
	ap.shell.show("chat")
	ap.shell.show("agent")
	if ap.chat == nil || ap.agent == nil {
		t.Fatal("shell must own the chat and agent view instances after navigation")
	}
	return ap, ap.chat.artCard
}

func TestArtifactCardHiddenUntilSettled(t *testing.T) {
	ap, card := newCardTestApp(t)
	_ = ap
	if card.hasBlock() || card.card.Visible() {
		t.Fatal("fresh app must not show an artifact card (no fabricated affordance)")
	}
	if ap.chat.wsBtn.Disabled() != true {
		t.Fatal("the Workspace entry must not be actionable with zero settled blocks (no blank surface)")
	}
	if ap.agent.wsBtn.Disabled() != true {
		t.Fatal("agent Workspace entry must not be actionable with zero settled blocks")
	}
}

func TestArtifactCardShowsSettledBlockWithSafeExcerpt(t *testing.T) {
	ap, card := newCardTestApp(t)
	ap.chat.promoteSettledAssistant(Session{
		ID: "sess-card",
		Messages: []Message{
			{Role: "user", Content: "question"},
			{Role: "assistant", Content: "# Heading\n\nA rich answer with **markdown**\nand a second line."},
		},
	})
	if !card.hasBlock() || !card.card.Visible() {
		t.Fatal("settled assistant response must promote the artifact card")
	}
	m := *card.meta
	if m.Kind != presentation.KindMarkdown {
		t.Fatalf("meta = %+v, want markdown kind", m)
	}
	if b, ok := ap.ws.WorkspaceBlock(m.ID); !ok || b.Source != "ui/chat" {
		t.Fatalf("stored block source = %q (found=%v), want ui/chat", b.Source, ok)
	}
	if got := card.kindLab.Text; !strings.HasPrefix(got, "markdown · ") {
		t.Fatalf("kind line = %q, want the kind · title form", got)
	}
	got := card.excerpt.Text
	if strings.ContainsAny(got, "\n\r") {
		t.Fatalf("excerpt must be a single collapsed line, got %q", got)
	}
	if !strings.Contains(got, "A rich answer") {
		t.Fatalf("excerpt lost the content head: %q", got)
	}
	if ap.chat.wsBtn.Disabled() {
		t.Fatal("Workspace entry must become actionable once a settled artifact exists")
	}
}

func TestChatSettlementPromotesSettledAssistantOnly(t *testing.T) {
	ap, card := newCardTestApp(t)
	// The live-bubble path must never touch the card: stream events alone
	// leave the affordance hidden.
	ap.chat.onStream()
	if card.hasBlock() || card.card.Visible() {
		t.Fatal("onStream must not promote the provisional live bubble onto the artifact card")
	}
	// Non-assistant or empty content registers nothing.
	ap.chat.promoteSettledAssistant(Session{ID: "s", Messages: []Message{{Role: "user", Content: "not an assistant reply"}}})
	if card.hasBlock() {
		t.Fatal("a user message must not promote the artifact card")
	}
	ap.chat.promoteSettledAssistant(Session{ID: "s", Messages: []Message{{Role: "assistant", Content: "   "}}})
	if card.hasBlock() {
		t.Fatal("empty settled content must not promote the artifact card")
	}
	if n := len(ap.ws.Blocks()); n != 0 {
		t.Fatalf("no fabricated blocks allowed, store has %d", n)
	}
	// One settled assistant response promotes exactly one block.
	ap.chat.promoteSettledAssistant(Session{ID: "s", Messages: []Message{{Role: "assistant", Content: "settled answer"}}})
	if len(ap.ws.Blocks()) != 1 {
		t.Fatalf("blocks = %d, want 1", len(ap.ws.Blocks()))
	}
}

func TestAgentSettlementPromotesItsOwnSettledAssistant(t *testing.T) {
	ap, _ := newCardTestApp(t)
	ap.agent.promoteSettledAssistant(Session{ID: "goal-sess", Messages: []Message{{Role: "assistant", Content: "agent plan output"}}})
	if len(ap.ws.Blocks()) != 1 {
		t.Fatalf("agent blocks = %d, want 1", len(ap.ws.Blocks()))
	}
	m, ok := ap.ws.Latest()
	if !ok || m.Title != "Agent response" {
		t.Fatalf("latest meta = %+v, want the agent-sourced response", m)
	}
	if b, found := ap.ws.WorkspaceBlock(m.ID); !found || b.Source != "ui/agent" {
		t.Fatalf("stored block source = %q (found=%v), want ui/agent", b.Source, found)
	}
	if ap.agent.artCard.meta == nil || ap.agent.artCard.meta.ID != m.ID {
		t.Fatal("agent card must reference the agent-settled block")
	}
}

func TestArtifactCardOpenResolvesRegisteredBlock(t *testing.T) {
	ap, card := newCardTestApp(t)
	ap.chat.promoteSettledAssistant(Session{ID: "sess-open", Messages: []Message{{Role: "assistant", Content: "## rich"}}})
	raw, err := card.openTarget()
	if err != nil {
		t.Fatalf("openTarget: %v", err)
	}
	defer ap.stopWorkspace()
	if !strings.Contains(raw, "#t=") {
		t.Fatal("launch URL must carry the fragment token")
	}
	if !strings.HasPrefix(raw, "http://127.0.0.1:") {
		t.Fatalf("launch URL %q is not loopback", raw)
	}
	if !ap.ws.Running() {
		t.Fatal("hub must report the server running after a successful resolve")
	}
}

func TestArtifactCardStaleReferenceDegradesVisibly(t *testing.T) {
	ap, card := newCardTestApp(t)
	ap.chat.promoteSettledAssistant(Session{ID: "sess-stale", Messages: []Message{{Role: "assistant", Content: "will be evicted"}}})
	stale := *card.meta
	// Simulate bounded-store eviction: a card reference whose block is no
	// longer resolvable (white-box: point the card at an unknown ID).
	ap.chat.artCard.meta = &presentation.Meta{ID: "chat-evicted", Kind: presentation.KindMarkdown, Title: "Chat response"}
	_, err := ap.chat.artCard.openTarget()
	if !errors.Is(err, errArtifactEvicted) {
		t.Fatalf("stale open = %v, want errArtifactEvicted", err)
	}
	ap.chat.artCard.open() // must not panic, must degrade visibly
	if !strings.Contains(ap.chat.artCard.status.Text, "Evicted") {
		t.Fatalf("status = %q, want the visible eviction degradation", ap.chat.artCard.status.Text)
	}
	if !ap.chat.artCard.openBtn.Disabled() {
		t.Fatal("open action must disable itself on a stale reference")
	}
	if !ap.ws.Running() {
		_ = stale // reference retained for readability
	}
	// The truthful hub status stays authoritative for the REAL reference.
	_ = card
}

func TestWorkspaceStatusTruthfulAcrossOpenClose(t *testing.T) {
	ap, card := newCardTestApp(t)
	ap.chat.promoteSettledAssistant(Session{ID: "sess-status", Messages: []Message{{Role: "assistant", Content: "status content"}}})
	card.syncStatus()
	if ap.ws.Running() {
		t.Fatal("hub must report closed before any open")
	}
	if strings.Contains(card.status.Text, "Workspace open") {
		t.Fatalf("status must not claim open while the server is down: %q", card.status.Text)
	}
	if _, err := ap.ws.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	card.syncStatus()
	if !ap.ws.Running() || !strings.Contains(card.status.Text, "Workspace open") {
		t.Fatalf("status must be truthful while open: running=%v text=%q", ap.ws.Running(), card.status.Text)
	}
	ap.stopWorkspace()
	card.syncStatus()
	if ap.ws.Running() || strings.Contains(card.status.Text, "Workspace open") {
		t.Fatal("status must return to closed after the listener is released")
	}
}

func TestWorkspaceEntryRecoversAcrossUpsertAndRebuild(t *testing.T) {
	ap, _ := newCardTestApp(t)
	content := "upsert content"
	ap.chat.promoteSettledAssistant(Session{ID: "s", Messages: []Message{{Role: "assistant", Content: content}}})
	// Content-addressed upsert: the same settled content refreshes the SAME
	// block — the card must keep exactly one truthful reference.
	ap.chat.promoteSettledAssistant(Session{ID: "s", Messages: []Message{{Role: "assistant", Content: content}}})
	if n := len(ap.ws.Blocks()); n != 1 {
		t.Fatalf("blocks = %d, want 1 (one ID — one artifact)", n)
	}
	// A rebuilt view re-seeds its card from the hub projection (reopened
	// state is correct without fabricating blocks).
	ap.chat = nil
	buildChatView(ap)
	if ap.chat == nil || ap.chat.artCard == nil || !ap.chat.artCard.hasBlock() {
		t.Fatal("rebuilt chat view must re-seed its artifact card from the hub")
	}
	if ap.chat.wsBtn.Disabled() {
		t.Fatal("rebuilt view's Workspace entry must be actionable with an existing artifact")
	}
}

// TestShellShowLazyBuildDoesNotDeadlock is the Phase 2 navigation
// regression: the FIRST navigation to a not-yet-built view builds it — and
// the build calls ap.shell.register(), which takes the shell mutex. With
// the shell non-nil (the production shape), building under the mutex
// self-deadlocked the UI thread on first navigation. The build now runs
// outside the critical section; this test drives the production shape
// (non-nil shell, lazy build through show) and must return.
func TestShellShowLazyBuildDoesNotDeadlock(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	ap.shell = newShell(ap)
	_ = ap.shell.root()
	done := make(chan struct{})
	go func() {
		uiDoLocked(func() { ap.shell.show("chat") })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("show() deadlocked building a lazy view with a non-nil shell")
	}
	if ap.chat == nil {
		t.Fatal("lazy build must install the shell-owned view instance")
	}
	// Second navigation reuses the built instance (no rebuild, no deadlock).
	ap.shell.show("agent")
	ap.shell.show("chat")
}
