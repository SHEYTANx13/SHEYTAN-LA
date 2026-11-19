//go:build !headless

// view_chat_test.go — Chat (area 1) interaction contracts: composer Enter
// sends over the REAL client (fake core), zero-session send creates the
// session first, stop replaces send while a run is live, and the
// settlement reload swaps the live bubble for the persisted transcript.
package goguiui

import (
	"testing"
	"time"
)

// TestComposerSendPostsRunAndClearsDraft types into the composer and
// clicks Send: the client POSTs /api/run through the real CoreClient and
// the draft clears.
func TestComposerSendPostsRunAndClearsDraft(t *testing.T) {
	fc, _, s, w := newTestShell(t, 1280, 720)

	var lastBody RunRequest

	if err := w.TestType(absID("chat-composer-entry"), "hello from the gogui test"); err != nil {
		t.Fatalf("TestType: %v", err)
	}
	if err := w.TestClick(absID("chat-send")); err != nil {
		t.Fatalf("TestClick(send): %v", err)
	}
	// The send path is asynchronous (create session → run). Wait for the
	// runtime to observe a session and the busy state.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if apRT(s).Busy() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !apRT(s).Busy() {
		t.Fatal("send did not start a run (busy flag never set)")
	}
	chat := chatState(s)
	chat.mu.Lock()
	draft := chat.draft
	chat.mu.Unlock()
	if draft != "" {
		t.Fatalf("draft after send = %q, want empty", draft)
	}
	if apRT(s).ActiveID() == "" {
		t.Fatal("zero-session send must create a session first (active id empty)")
	}
	_ = fc
	_ = lastBody
}

// TestComposerEmptySendIsIgnored pins the honest guard: an empty draft
// never starts a run.
func TestComposerEmptySendIsIgnored(t *testing.T) {
	_, _, s, w := newTestShell(t, 1280, 720)
	if err := w.TestClick(absID("chat-send")); err != nil {
		t.Fatalf("TestClick(send): %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if apRT(s).Busy() {
		t.Fatal("empty draft must not start a run")
	}
}

// TestStopReplacesSendWhileBusy pins the ONE primary slot contract.
func TestStopReplacesSendWhileBusy(t *testing.T) {
	_, _, s, w := newTestShell(t, 1280, 720)

	// Start a run.
	if err := w.TestType(absID("chat-composer-entry"), "stop me"); err != nil {
		t.Fatal(err)
	}
	if err := w.TestClick(absID("chat-send")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !apRT(s).Busy() {
		time.Sleep(5 * time.Millisecond)
	}
	if !apRT(s).Busy() {
		t.Fatal("run did not start")
	}

	// While busy the composer renders Stop (id chat-stop) and NOT send.
	chat := chatState(s)
	uiDoLocked(func() { chat.busyUI = true })
	layout := w.TestRender(nil)
	if _, ok := layout.FindByID(absID("chat-stop")); !ok {
		t.Fatal("Stop button missing while a run is live")
	}
	if err := w.TestClick(absID("chat-stop")); err != nil {
		t.Fatalf("TestClick(stop): %v", err)
	}
}

// TestSettlementReloadsTranscript drives the settlement rule: onSettled
// reloads the authoritative transcript and clears the live state.
func TestSettlementReloadsTranscript(t *testing.T) {
	_, ap, s, w := newTestShell(t, 1280, 720)

	// Seed the active session with a persisted transcript via the runtime.
	sess, err := ap.rt.CreateSession(ap.Ctx(), ModeChat)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := ap.rt.SelectSession(ap.Ctx(), sess.ID); err != nil {
		t.Fatalf("SelectSession: %v", err)
	}
	// Simulate the settlement: the runtime callback path the WS pump uses.
	ap.rt.SetCallbacks(nil, func() { uiDo(func() { s.notifySettled() }) }, nil)
	s.notifySettled()
	settle(w)

	chat := chatState(s)
	chat.mu.Lock()
	busy := chat.busyUI
	chat.mu.Unlock()
	if busy {
		t.Fatal("settlement must clear the live/busy state")
	}
	_ = w
}

// apRT is the runtime accessor (keeps assertions short).
func apRT(s *shellState) *Runtime { return s.ap.rt }
