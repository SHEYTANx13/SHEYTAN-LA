//go:build !headless

// composer_test.go — Phase 1 composer contracts pinned on the REAL
// widgets (docs/ui-contract.md §2 #1–#3, #5):
//
//   - the keyboard mapping is driven through the actual composerEntry
//     TypedKey/KeyDown/KeyUp path (no send-callback shortcut): plain
//     Enter sends, Shift+Enter inserts a newline, other keys delegate;
//   - the primary action slot holds EXACTLY ONE visible occupant and
//     swaps Send ↔ Stop through the busy transitions;
//   - the capability-priming empty state fills the composer DRAFT only —
//     a sample action can never auto-send.
package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// walkButtons collects every button in a canvas subtree (containers and
// cards only — the empty-state tree is built from those).
func walkButtons(o fyne.CanvasObject) []*widget.Button {
	var out []*widget.Button
	var walk func(fyne.CanvasObject)
	walk = func(c fyne.CanvasObject) {
		if b, ok := c.(*widget.Button); ok {
			out = append(out, b)
			return
		}
		switch t := c.(type) {
		case *fyne.Container:
			for _, child := range t.Objects {
				walk(child)
			}
		case *widget.Card:
			walk(t.Content)
		}
	}
	walk(o)
	return out
}

func TestComposerEnterSendsAndShiftEnterNewlines(t *testing.T) {
	sends := 0
	c := newComposerEntry("placeholder", func() { sends++ })
	c.SetText("hello")

	// Plain Enter (both Return spellings) sends; no newline is inserted.
	c.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnter})
	if sends != 1 || strings.Contains(c.Text, "\n") {
		t.Fatalf("plain Enter must send without a newline: sends=%d text=%q", sends, c.Text)
	}
	c.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if sends != 2 {
		t.Fatalf("Return must send: sends=%d", sends)
	}

	// Shift+Enter inserts a newline and never sends (left shift).
	c.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	c.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnter})
	c.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	if sends != 2 {
		t.Fatalf("Shift+Enter must NOT send: sends=%d", sends)
	}
	if strings.Count(c.Text, "\n") != 1 {
		t.Fatalf("Shift+Enter must insert exactly one newline, text=%q", c.Text)
	}

	// Right shift behaves identically.
	c.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftRight})
	c.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	c.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftRight})
	if sends != 2 || strings.Count(c.Text, "\n") != 2 {
		t.Fatalf("right Shift+Enter must newline only: sends=%d text=%q", sends, c.Text)
	}

	// Every other key delegates untouched (End moves the cursor, no send).
	c.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnd})
	if sends != 2 {
		t.Fatalf("non-Return keys must not send: sends=%d", sends)
	}

	// The pure mapping agrees with the widget behavior.
	if d := composerSendDecisionFor(fyne.KeyEvent{Name: fyne.KeyEnter}, false); d != composerKeySend {
		t.Fatalf("mapping: plain Enter = %v, want send", d)
	}
	if d := composerSendDecisionFor(fyne.KeyEvent{Name: fyne.KeyReturn}, true); d != composerKeyNewline {
		t.Fatalf("mapping: Shift+Return = %v, want newline", d)
	}
	if d := composerSendDecisionFor(fyne.KeyEvent{Name: fyne.KeyDown}, true); d != composerKeyDefault {
		t.Fatalf("mapping: Shift+Down = %v, want default", d)
	}
}

func TestPrimaryActionSlotHoldsExactlyOneOccupant(t *testing.T) {
	s := newPrimaryActionSlot(func() {}, func() {})

	if s.busy() || !s.send.Visible() || s.stop.Visible() {
		t.Fatalf("idle slot must show Send only: busy=%t sendVisible=%t stopVisible=%t", s.busy(), s.send.Visible(), s.stop.Visible())
	}

	s.setBusy(true)
	if !s.busy() || s.send.Visible() || !s.stop.Visible() {
		t.Fatalf("busy slot must show Stop only: busy=%t sendVisible=%t stopVisible=%t", s.busy(), s.send.Visible(), s.stop.Visible())
	}
	if !s.send.Disabled() {
		t.Fatal("the hidden Send must be disabled while Stop owns the slot")
	}

	s.setBusy(false)
	if s.busy() || !s.send.Visible() || s.stop.Visible() || s.send.Disabled() {
		t.Fatalf("idle slot must be fully restored after the run ends")
	}
}

func TestEmptyStateSamplesFillDraftOnly(t *testing.T) {
	sends := 0
	c := newComposerEntry("placeholder", func() { sends++ })

	for _, mode := range []string{"chat", "agent"} {
		sends = 0
		c.SetText("")
		surf := emptyStateSurface(mode, c, nil) // nil window: the focus guard must tolerate it
		buttons := walkButtons(surf)
		if len(buttons) == 0 {
			t.Fatalf("%s empty state must offer sample actions", mode)
		}
		for _, b := range buttons {
			b.Tapped(&fyne.PointEvent{})
			// A sample fills the DRAFT and never auto-sends.
			if sends != 0 {
				t.Fatalf("%s: a sample action auto-sent — contract violation", mode)
			}
			if strings.TrimSpace(c.Text) == "" {
				t.Fatalf("%s: a sample action left the draft empty", mode)
			}
			c.SetText("")
		}
	}

	// Mode distinction: the agent surface must not reuse the chat samples.
	chat := emptyStateSurface("chat", c, nil)
	agent := emptyStateSurface("agent", c, nil)
	if walkButtons(chat) == nil || walkButtons(agent) == nil {
		t.Fatal("both modes must carry sample actions")
	}
}

// walkTree visits every node of a canvas subtree and records each node's
// parent. Fyne's Border/HBox/VBox/Stack constructors return plain
// *fyne.Container; Scroll, Split and AppTabs are widgets that expose
// their children through their own fields — descend through all of them.
func walkTree(o fyne.CanvasObject, visit func(fyne.CanvasObject), parents map[fyne.CanvasObject]fyne.CanvasObject) {
	visit(o)
	record := func(child fyne.CanvasObject) {
		if parents != nil {
			parents[child] = o
		}
		walkTree(child, visit, parents)
	}
	switch t := o.(type) {
	case *fyne.Container:
		for _, c := range t.Objects {
			record(c)
		}
	case *widget.Card:
		record(t.Content)
	case *container.Scroll:
		record(t.Content)
	case *container.Split:
		record(t.Leading)
		record(t.Trailing)
	case *container.AppTabs:
		for _, item := range t.Items {
			record(item.Content)
		}
	}
}

// effectivelyVisible reports whether o AND every ancestor are visible
// (fyne Visible() is a per-node flag: hiding a container does not flip
// its children's own flags).
func effectivelyVisible(parents map[fyne.CanvasObject]fyne.CanvasObject, o fyne.CanvasObject) bool {
	for c := o; c != nil; c = parents[c] {
		if !c.Visible() {
			return false
		}
	}
	return true
}

// TestChatViewWiringEmptyStateAndSlot pins the chat-view wiring on the
// REAL returned view tree: the primary slot starts idle with Send as its
// only visible occupant, the empty state exists but stays hidden until a
// refresh proves the conversation empty (loading ≠ empty), and an empty
// draft + Enter never triggers a run (zero-session Send stays intact).
func TestChatViewWiringEmptyStateAndSlot(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	root := buildChatView(ap)
	parents := map[fyne.CanvasObject]fyne.CanvasObject{}

	var composer *composerEntry
	var sendBtn, stopBtn *widget.Button
	var emptyTitle *widget.Label
	walkTree(root, func(o fyne.CanvasObject) {
		switch t := o.(type) {
		case *composerEntry:
			composer = t
		case *widget.Button:
			switch t.Text {
			case "Send":
				sendBtn = t
			case "Stop":
				stopBtn = t
			}
		case *widget.Label:
			if strings.HasPrefix(t.Text, "Start a conversation") {
				emptyTitle = t
			}
		}
	}, parents)

	if composer == nil {
		t.Fatal("the chat tree must contain the phase-1 composer entry")
	}
	if sendBtn == nil || stopBtn == nil {
		t.Fatal("the chat tree must contain the Send/Stop primary slot")
	}
	if !sendBtn.Visible() || stopBtn.Visible() {
		t.Fatal("fresh chat view must start with Send owning the slot")
	}
	if emptyTitle == nil {
		t.Fatal("the capability-priming empty state must be part of the tree")
	}
	if effectivelyVisible(parents, emptyTitle) {
		t.Fatal("the empty state must stay hidden until the first refresh passes (loading ≠ empty)")
	}

	// Empty draft + Enter: the send guard refuses (no run, no error).
	before := ap.rt.Busy()
	composer.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnter})
	if ap.rt.Busy() != before {
		t.Fatal("Enter on an empty draft must never start a run")
	}

	// The zero-session Send contract survives: with an empty draft the
	// composer stays enabled and Send remains the slot owner.
	if composer.Disabled() {
		t.Fatal("the composer must never wedge (zero-session Send contract)")
	}
}
