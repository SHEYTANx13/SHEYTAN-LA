//go:build !headless

// view_chat.go — the Chat view: session sidebar, transcript, live stream
// bubble and composer. Contracts carried from the retired surfaces:
//
//   - zero-session send: the composer is ALWAYS enabled; Send with no
//     session creates one in the current mode and completes the send.
//   - live events fold cumulatively (REPLACE, never append).
//   - settlement reloads the authoritative transcript; the live bubble
//     never promotes itself as the final message.
//   - stop keeps partial output; terminal outcomes are honest.
//   - chat and agent are separate conversation spaces.
package fyneui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// chatView is the Chat surface state.
type chatView struct {
	ap          *App
	sessionList *widget.List
	transcript  *widget.List
	live        *widget.Label
	liveBox     *fyne.Container
	input       *widget.Entry
	sendBtn     *widget.Button
	stopBtn     *widget.Button
	regenBtn    *widget.Button
	thinking    *widget.Select
	netSearch   *widget.Check
	sessions    []Session
	messages    []Message
	errLabel    *widget.Label
	wsBtn       *widget.Button
	newBtn      *widget.Button
	delBtn      *widget.Button
	renBtn      *widget.Button
}

// buildChatView constructs the Chat view bound to mode "chat".
func buildChatView(ap *App) fyne.CanvasObject {
	v := &chatView{ap: ap}

	v.sessionList = widget.NewList(
		func() int { return len(v.sessions) },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			l := o.(*widget.Label)
			s := v.sessions[i]
			title := s.Title
			if title == "" {
				title = "Session " + shortID(s.ID)
			}
			if c := s.MsgCount; c != nil {
				title += fmt.Sprintf("  (%d)", *c)
			}
			l.SetText(title)
		},
	)
	v.sessionList.OnSelected = func(i widget.ListItemID) {
		if i < 0 || i >= len(v.sessions) {
			return
		}
		v.openSession(v.sessions[i].ID)
	}

	v.transcript = widget.NewList(
		func() int { return len(v.messages) },
		func() fyne.CanvasObject {
			role := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			body := widget.NewLabel("")
			body.Wrapping = fyne.TextWrapWord
			return container.NewVBox(role, body)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			role := box.Objects[0].(*widget.Label)
			body := box.Objects[1].(*widget.Label)
			m := v.messages[i]
			role.SetText(roleLabel(m.Role))
			body.SetText(displayContent(m))
		},
	)

	v.live = widget.NewLabel("")
	v.live.Wrapping = fyne.TextWrapWord
	v.live.TextStyle = fyne.TextStyle{Italic: true}
	v.live.Hide()
	stopInline := widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), v.stop)
	stopInline.Importance = widget.DangerImportance
	v.liveBox = container.NewVBox(
		widget.NewLabelWithStyle("Live", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(v.live, stopInline),
	)
	v.liveBox.Hide()

	v.input = widget.NewMultiLineEntry()
	v.input.SetPlaceHolder("Message SHEYTAN… (Enter to send, Shift+Enter for newline)")
	v.input.OnSubmitted = func(string) { v.send() }

	v.thinking = widget.NewSelect(ThinkingLevels, nil)
	v.thinking.PlaceHolder = "Thinking: mid"
	v.thinking.SetSelected("mid")
	v.netSearch = widget.NewCheck("Net search", nil)

	v.sendBtn = widget.NewButtonWithIcon("Send", theme.MediaPlayIcon(), v.send)
	v.sendBtn.Importance = widget.HighImportance
	v.stopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), v.stop)
	v.stopBtn.Importance = widget.DangerImportance
	v.regenBtn = widget.NewButtonWithIcon("Regenerate", theme.MediaReplayIcon(), v.regenerate)
	// Web Workspace (on-demand): opens the browser-hosted rich surface for
	// the settled rich blocks. The native transcript stays the source of
	// truth; a browser/server failure only disables this extra surface.
	v.wsBtn = widget.NewButtonWithIcon("Workspace", theme.ComputerIcon(), v.openWorkspace)

	v.newBtn = widget.NewButtonWithIcon("New", theme.ContentAddIcon(), func() {
		go func() {
			if _, err := ap.rt.CreateSession(ap.Ctx(), ""); err != nil {
				v.showErr("create session", err)
				return
			}
			uiDo(func() { v.refreshSessions(ap.rt.Sessions()); v.sessionList.UnselectAll() })
		}()
	})
	v.delBtn = widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() { v.confirmDelete() })
	v.renBtn = widget.NewButtonWithIcon("Rename", theme.DocumentCreateIcon(), func() { v.rename() })

	v.errLabel = widget.NewLabel("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Wrapping = fyne.TextWrapWord
	v.errLabel.Hide()

	// Composer: gated ONLY while a run is live (zero-session send is the
	// contract — an empty session list must not wedge the composer).
	composer := container.NewVBox(
		container.NewHBox(v.thinking, v.netSearch, layoutSpacer(), v.wsBtn, v.sendBtn, v.stopBtn, v.regenBtn),
		v.input,
	)
	controls := container.NewHBox(v.newBtn, v.renBtn, v.delBtn)

	sessionsScroll := container.NewScroll(v.sessionList)
	sessionsScroll.SetMinSize(fyne.NewSize(220, 100))
	sessionsPane := container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Sessions", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), controls),
		nil, nil, nil,
		sessionsScroll,
	)

	liveColumn := container.NewBorder(nil, composer, nil, nil, container.NewScroll(container.NewVBox(v.transcript, v.liveBox)))

	split := container.NewHSplit(sessionsPane, liveColumn)
	split.Offset = 0.28
	root := container.NewBorder(
		PageHeader("Chat", "Conversations in the chat space"),
		v.errLabel,
		nil, nil,
		split,
	)

	// Runtime callbacks — dispatched by the shell after the app starts.
	ap.shell.register(
		func() { v.onStream() },
		func() { v.onSettled() },
		func() { v.refreshSessions(ap.rt.Sessions()) },
	)

	// Presentation seed only — widget Refresh is deferred until the
	// event loop runs (the bootstrap onSessions callback refreshes).
	v.sessions = ap.rt.Sessions()
	v.attachIfActive()
	return root
}

// helpers shared by chat/agent views

func layoutSpacer() fyne.CanvasObject {
	return widget.NewLabel("")
}

func roleLabel(role string) string {
	switch role {
	case "user":
		return "You"
	case "assistant":
		return "SHEYTAN"
	case "system":
		return "System"
	case "tool":
		return "Tool"
	default:
		return strings.ToUpper(role)
	}
}

func displayContent(m Message) string {
	if m.Content != "" {
		return m.Content
	}
	if m.Reasoning != "" {
		return "(thinking) " + m.Reasoning
	}
	return ""
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// attachIfActive subscribes to the activity stream for the active session.
func (v *chatView) attachIfActive() {
	if id := v.ap.rt.ActiveID(); id != "" {
		go func() {
			_ = v.ap.rt.AttachActivity(v.ap.Ctx(), id)
		}()
	}
}

func (v *chatView) refreshSessions(list []Session) {
	v.sessions = list
	v.sessionList.Refresh()
	if id := v.ap.rt.ActiveID(); id != "" {
		for i, s := range list {
			if s.ID == id {
				v.sessionList.Select(i)
				break
			}
		}
	}
	v.updateButtons()
}

func (v *chatView) openSession(id string) {
	ap := v.ap
	go func() {
		s, err := ap.rt.SelectSession(ap.Ctx(), id)
		if err != nil {
			uiDo(func() { v.showErr("open session", err) })
			return
		}
		uiDo(func() {
			v.messages = s.Messages
			v.transcript.Refresh()
			v.liveBox.Hide()
			v.live.SetText("")
		})
		_ = ap.rt.AttachActivity(ap.Ctx(), id)
	}()
}

func (v *chatView) send() {
	text := strings.TrimSpace(v.input.Text)
	if text == "" || v.ap.rt.Busy() {
		return
	}
	ap := v.ap
	think := v.thinking.Selected
	if think == "" {
		think = "mid"
	}
	net := v.netSearch.Checked
	v.input.SetText("")
	v.errLabel.Hide()
	v.sendBtn.Disable()
	go func() {
		if _, err := ap.rt.Send(ap.Ctx(), text, net, think); err != nil {
			uiDo(func() {
				v.showErr("send", err)
				v.sendBtn.Enable()
			})
		}
	}()
}

func (v *chatView) stop() {
	go func() {
		if err := v.ap.rt.Stop(v.ap.Ctx()); err != nil {
			uiDo(func() { v.showErr("stop", err) })
		}
	}()
}

func (v *chatView) regenerate() {
	ap := v.ap
	v.sendBtn.Disable()
	go func() {
		if _, err := ap.rt.Regenerate(ap.Ctx()); err != nil {
			uiDo(func() {
				v.showErr("regenerate", err)
				v.sendBtn.Enable()
			})
		}
	}()
}

// onStream folds the live accumulator into the live bubble. Coalescing is
// inherent: fyne.Do serializes onto the event loop, and label text setting
// is idempotent — no per-token storm, no dropped final event.
func (v *chatView) onStream() {
	acc := v.ap.rt.Stream()
	busy := v.ap.rt.Busy()
	v.liveBox.Show()
	if acc.Response != "" {
		v.live.SetText(acc.Response)
	} else if acc.Reasoning != "" {
		v.live.SetText("thinking: " + acc.Reasoning)
	} else if acc.Status != "" {
		v.live.SetText(acc.Status)
	}
	if busy {
		v.stopBtn.Enable()
		v.sendBtn.Disable()
	} else {
		v.sendBtn.Enable()
		v.stopBtn.Disable()
	}
}

// onSettled swaps the live bubble out and the persisted transcript in.
func (v *chatView) onSettled() {
	acc := v.ap.rt.Stream()
	if id := v.ap.rt.ActiveID(); id != "" {
		if s, ok := v.ap.rt.ActiveSession(); ok {
			v.messages = s.Messages
			v.transcript.Refresh()
			// Issue the settled assistant response as a workspace block
			// (markdown kind). Registration is additive: it can only fail
			// by refusing invalid content, and the native rendering above
			// is untouched either way.
			if len(s.Messages) > 0 {
				last := s.Messages[len(s.Messages)-1]
				if last.Role == "assistant" && strings.TrimSpace(last.Content) != "" {
					_, _ = v.ap.Workspace().RegisterChatResponse(s.ID, last.Content)
				}
			}
		}
	}
	v.liveBox.Hide()
	v.live.SetText("")
	v.sendBtn.Enable()
	v.stopBtn.Disable()
	v.errLabel.Hide()
	_ = acc
	v.updateButtons()
}

func (v *chatView) showErr(what string, err error) {
	v.errLabel.SetText(what + " failed: " + err.Error())
	v.errLabel.Show()
}

// openWorkspace starts (if needed) the loopback workspace server and hands
// the per-launch URL to the default browser. Failure is surfaced in the
// native shell; the native transcript never depends on this succeeding.
func (v *chatView) openWorkspace() {
	if err := v.ap.openWorkspace(); err != nil {
		v.showErr("open workspace", err)
		return
	}
	v.errLabel.Hide()
}

func (v *chatView) confirmDelete() {
	id := v.ap.rt.ActiveID()
	if id == "" {
		return
	}
	dialog.NewConfirm("Delete session",
		"Delete the current session and its transcript? This cannot be undone.",
		func(ok bool) {
			if !ok {
				return
			}
			go func() {
				if err := v.ap.rt.DeleteSession(v.ap.Ctx(), id); err != nil {
					uiDo(func() { v.showErr("delete session", err) })
					return
				}
				uiDo(func() {
					v.refreshSessions(v.ap.rt.Sessions())
					v.messages = nil
					v.transcript.Refresh()
				})
			}()
		}, v.ap.Window()).Show()
}

func (v *chatView) rename() {
	id := v.ap.rt.ActiveID()
	if id == "" {
		return
	}
	entry := widget.NewEntry()
	entry.SetPlaceHolder("New title")
	d := dialog.NewForm("Rename session", "Rename", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Title", entry)},
		func(ok bool) {
			if !ok || strings.TrimSpace(entry.Text) == "" {
				return
			}
			go func() {
				err := v.ap.rt.RenameSession(v.ap.Ctx(), id, strings.TrimSpace(entry.Text))
				if err != nil {
					uiDo(func() { v.showErr("rename session", err) })
				}
			}()
		}, v.ap.Window())
	d.Resize(fyne.NewSize(380, 160))
	d.Show()
}

func (v *chatView) updateButtons() {
	busy := v.ap.rt.Busy()
	if busy {
		v.sendBtn.Disable()
		v.stopBtn.Enable()
	} else {
		v.sendBtn.Enable()
		v.stopBtn.Disable()
	}
}

// ensure version import used (honest status line carries the core version).
var _ = config.AppVersion
var _ = time.Now
