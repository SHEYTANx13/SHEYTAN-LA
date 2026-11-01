//go:build !headless

// composer.go — Phase 1 conversation-surface composer contracts
// (docs/ui-contract.md §2, contracts #1–#3 and #5):
//
//   - the keyboard pin: Enter = send, Shift+Enter = newline. The fyne
//     v2.8.2 multiline default is the INVERSE mapping (typedKeyReturn
//     submits when Shift is held) — verified against the module source —
//     so the mapping is owned here, on the real widget type, and pinned
//     by tests that drive the actual key path (no assumed OnSubmitted).
//   - the single primary action slot: Send while idle, Stop while a run
//     is live — one slot, never two competing primary buttons. Stop
//     always routes through the existing cancellation path.
//   - safe focus return to the composer after sending (never stealing
//     focus from a dialog or another deliberate focus target).
//   - the capability-priming empty state: sample prompts fill the
//     composer DRAFT and never auto-send; zero-session Send is intact.
package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// composerSendDecision is the PURE keyboard mapping (unit-tested): a
// Return-family key on the composer sends, unless Shift is held — then it
// inserts a newline. Every other key delegates to the entry.
type composerSendDecision int

const (
	composerKeyDefault composerSendDecision = iota
	composerKeySend
	composerKeyNewline
)

func composerSendDecisionFor(key fyne.KeyEvent, shiftDown bool) composerSendDecision {
	switch key.Name {
	case fyne.KeyEnter, fyne.KeyReturn:
		if shiftDown {
			return composerKeyNewline
		}
		return composerKeySend
	}
	return composerKeyDefault
}

// composerEntry is the Chat/Agent input: a multiline entry whose Enter
// behavior follows the product contract. The entry's own OnSubmitted is
// left NIL on purpose — the wrapper intercepts plain Enter for send and
// delegates everything else (Shift+Enter reaches the entry's typedKeyReturn,
// which inserts the newline because OnSubmitted is nil).
type composerEntry struct {
	widget.Entry
	onSend    func()
	shiftDown bool
}

func newComposerEntry(placeHolder string, onSend func()) *composerEntry {
	c := &composerEntry{onSend: onSend}
	c.MultiLine = true
	c.Wrapping = fyne.TextWrapWord
	c.SetPlaceHolder(placeHolder)
	c.ExtendBaseWidget(c)
	return c
}

// KeyDown tracks the Shift modifier exactly like the base entry does for
// selection, plus our own mapping flag.
func (c *composerEntry) KeyDown(key *fyne.KeyEvent) {
	if key.Name == desktop.KeyShiftLeft || key.Name == desktop.KeyShiftRight {
		c.shiftDown = true
	}
	c.Entry.KeyDown(key)
}

// KeyUp releases the Shift tracking.
func (c *composerEntry) KeyUp(key *fyne.KeyEvent) {
	if key.Name == desktop.KeyShiftLeft || key.Name == desktop.KeyShiftRight {
		c.shiftDown = false
	}
	c.Entry.KeyUp(key)
}

// TypedKey intercepts ONLY the send decision; every other key — including
// Shift+Enter — reaches the base entry unchanged.
func (c *composerEntry) TypedKey(key *fyne.KeyEvent) {
	switch composerSendDecisionFor(*key, c.shiftDown) {
	case composerKeySend:
		if c.onSend != nil {
			c.onSend()
		}
		return
	case composerKeyNewline:
		// The entry inserts the newline itself (OnSubmitted is nil).
		c.Entry.TypedKey(key)
		return
	}
	c.Entry.TypedKey(key)
}

// primaryActionSlot is the ONE primary composer action: Send while idle,
// Stop while a run is live — the same visual slot (docs/ui-contract.md §2
// #3). Stop is a real button wired to the existing cancellation path.
type primaryActionSlot struct {
	stack *fyne.Container
	send  *widget.Button
	stop  *widget.Button
}

func newPrimaryActionSlot(onSend, onStop func()) *primaryActionSlot {
	s := &primaryActionSlot{
		send: widget.NewButtonWithIcon("Send", theme.MediaPlayIcon(), onSend),
		stop: widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), onStop),
	}
	s.send.Importance = widget.HighImportance
	s.stop.Importance = widget.DangerImportance
	s.stack = container.NewStack(s.send, s.stop)
	s.setBusy(false)
	return s
}

// canvas returns the slot object for layout composition.
func (s *primaryActionSlot) canvas() fyne.CanvasObject { return s.stack }

// setBusy swaps the slot's occupant. Exactly one action is ever visible;
// the hidden one is also disabled so it can never be triggered
// out-of-band while not the occupant.
func (s *primaryActionSlot) setBusy(busy bool) {
	if busy {
		s.send.Disable()
		s.send.Hide()
		s.stop.Enable()
		s.stop.Show()
		return
	}
	s.stop.Disable()
	s.stop.Hide()
	s.send.Enable()
	s.send.Show()
}

// busy reports which action currently owns the slot.
func (s *primaryActionSlot) busy() bool { return s.stop.Visible() }

// focusComposerIfSafe returns focus to the composer after a send. It is
// deliberately conservative: a dialog, an approval prompt or any other
// deliberate focus target keeps the focus — the composer is focused only
// when nothing else holds it.
func focusComposerIfSafe(w fyne.Window, input fyne.Focusable) {
	if w == nil || w.Canvas() == nil {
		return
	}
	if cur := w.Canvas().Focused(); cur != nil && cur != input {
		return // another deliberate target owns the focus — never steal it
	}
	w.Canvas().Focus(input)
}

// samplePrompt is one capability-priming suggestion for the empty state.
type samplePrompt struct {
	label string
	text  string
}

// chatSamplePrompts / agentSamplePrompts — mode-appropriate samples. They
// populate the composer DRAFT (never auto-send, never bypass the composer
// — docs/ui-contract.md §2 #5 and the zero-session Send contract).
var chatSamplePrompts = []samplePrompt{
	{label: "Draft a message", text: "Help me draft a short, friendly message to reschedule a meeting."},
	{label: "Explain a concept", text: "Explain how attention works in Transformer models, with a small worked example."},
	{label: "Plan work", text: "Help me break a large refactoring task into safe, ordered steps."},
}

var agentSamplePrompts = []samplePrompt{
	{label: "Inspect a repo", text: "Inspect the repository I have open and summarize its structure and main entry points."},
	{label: "Diagnose a failure", text: "Diagnose the failing test I describe below and propose the minimal fix:\n"},
	{label: "Research a topic", text: "Research the topic I paste below and report findings with sources:\n"},
}

// emptyStateSurface builds the capability-priming empty state for the
// current mode ("chat" or "agent"). win is the product window used for
// the safe focus return.
func emptyStateSurface(mode string, input *composerEntry, win fyne.Window) fyne.CanvasObject {
	prompts, title, detail := chatSamplePrompts, "Start a conversation",
		"Send your first message — a session is created automatically."
	if mode == "agent" {
		prompts, title, detail = agentSamplePrompts, "Start an agent task",
			"Describe a goal — the agent plans, acts and reports observed evidence."
	}

	box := container.NewVBox()
	head := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	sub := wrappedLabel(detail)
	sub.Importance = widget.LowImportance
	box.Add(container.NewPadded(head))
	box.Add(container.NewPadded(sub))
	row := container.NewHBox()
	for _, sp := range prompts {
		sample := sp
		row.Add(widget.NewButton(sample.label, func() {
			// DRAFT ONLY: fill the composer, focus it, never send.
			input.SetText(sample.text)
			focusComposerIfSafe(win, input)
		}))
	}
	box.Add(container.NewPadded(row))
	hint := wrappedLabel("Samples fill the draft — nothing is sent until you press Send.")
	hint.Importance = widget.LowImportance
	box.Add(container.NewPadded(hint))
	return box
}
