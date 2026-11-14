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

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/presentation"
)

// chatView is the Chat surface state.
type chatView struct {
	ap          *App
	sessionList *widget.List
	transcript  *widget.List
	live        *widget.Label
	liveBox     *fyne.Container
	emptyBox    *fyne.Container
	input       *composerEntry
	slot        *primaryActionSlot
	regenBtn    *widget.Button
	thinking    *widget.Select
	netSearch   *widget.Check
	sessions    []Session
	messages    []Message
	errLabel    *widget.Label
	wsBtn       *widget.Button
	artCard     *artifactCard
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
			l.TextStyle = fyne.TextStyle{}
			l.Importance = widget.MediumImportance
			active := v.sessions[i].ID == v.ap.rt.ActiveID()
			l.SetText(sessionRowText(v.sessions[i], active))
			if active {
				l.TextStyle = fyne.TextStyle{Bold: true}
				l.Importance = widget.HighImportance
			}
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
			// Conversation hierarchy (Phase 1): the role line is secondary
			// (muted via LowImportance → the theme's Muted role); the
			// content carries the visual priority.
			role := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			role.Importance = widget.LowImportance
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
	// The live surface is PROVISIONAL (settlement rule): it renders the
	// stream accumulator only. Its Stop lives in the composer's single
	// primary slot — no second competing Stop button here.
	v.liveBox = container.NewVBox(
		liveCaption(),
		v.live,
	)
	v.liveBox.Hide()

	v.input = newComposerEntry("Message SHEYTAN… (Enter to send, Shift+Enter for newline)", v.send)

	// Capability-priming empty state (mode-aware). Sample prompts fill
	// the draft only; zero-session Send stays the composer's contract.
	empty := emptyStateSurface("chat", v.input, ap.Window())
	v.emptyBox = container.NewVBox(empty)
	v.emptyBox.Hide()

	v.thinking = widget.NewSelect(ThinkingLevels, nil)
	v.thinking.PlaceHolder = "Thinking: mid"
	v.thinking.SetSelected("mid")
	v.netSearch = widget.NewCheck("Net search", nil)

	v.slot = newPrimaryActionSlot(v.send, v.stop)
	v.regenBtn = widget.NewButtonWithIcon("Regenerate", theme.MediaReplayIcon(), v.regenerate)
	// Web Workspace (on-demand): opens the browser-hosted rich surface for
	// the settled rich blocks. The native transcript stays the source of
	// truth; a browser/server failure only disables this extra surface.
	// Phase 2: the entry is actionable ONLY once a settled artifact exists
	// (an empty Workspace surface is never a click target); the contextual
	// settled-artifact card below carries the prominent affordance.
	v.wsBtn = widget.NewButtonWithIcon("Workspace", theme.ComputerIcon(), v.openWorkspace)
	syncWorkspaceAffordance(v.wsBtn, ap)
	// Phase 2 native artifact card: starts hidden (no settled artifact yet);
	// re-seeded from the hub so a rebuilt view keeps a truthful reference.
	v.artCard = newArtifactCard(ap)
	if m, ok := ap.ws.Latest(); ok {
		if b, found := ap.ws.WorkspaceBlock(m.ID); found {
			v.artCard.setBlock(m, string(b.Payload))
		}
	}

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
	// contract — an empty session list must not wedge the composer). The
	// primary action is ONE slot: Send while idle, Stop while busy.
	//
	// v1.11.1 button-fit repair: the former single HBox (options +
	// spacer + two actions + the primary slot) overflowed the content
	// width at phone sizes — the trailing buttons rendered outside the
	// visible area, unreachable and clipped. The composer now stacks by
	// role: generation options, artifact actions, the ONE primary action
	// (full width), then the input — every control keeps its full
	// MinSize at every matrix width.
	composer := container.NewVBox(
		container.NewHBox(v.thinking, v.netSearch),
		container.NewHBox(v.wsBtn, v.regenBtn),
		v.slot.canvas(),
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

	liveColumn := container.NewBorder(nil, composer, nil, nil, container.NewScroll(container.NewVBox(v.emptyBox, v.transcript, v.liveBox, v.artCard.canvas())))

	// v1.11.1 compact repair: the adaptive dual pane stacks the sessions
	// list above the conversation below the compact breakpoint (the
	// documented one-column contract) and keeps the 0.28 side-by-side
	// split above it.
	split := newAdaptiveDualPane(0.28, sessionsPane, liveColumn)
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
	// Phase 2 test/wiring seam: the shell-owned instance of this view.
	ap.chat = v

	// Presentation seed only — widget Refresh is deferred until the
	// event loop runs (the bootstrap onSessions callback refreshes).
	v.sessions = ap.rt.Sessions()
	v.attachIfActive()
	return root
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
			v.updateEmptyState()
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
	v.slot.setBusy(true)
	// Focus return (docs/ui-contract.md §2 #2): after initiating the send
	// the composer regains focus unless another deliberate target (a
	// dialog, an approval prompt) holds it.
	focusComposerIfSafe(ap.Window(), v.input)
	go func() {
		if _, err := ap.rt.Send(ap.Ctx(), text, net, think); err != nil {
			uiDo(func() {
				v.showErr("send", err)
				v.slot.setBusy(false)
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
	v.slot.setBusy(true)
	go func() {
		if _, err := ap.rt.Regenerate(ap.Ctx()); err != nil {
			uiDo(func() {
				v.showErr("regenerate", err)
				v.slot.setBusy(false)
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
	v.slot.setBusy(busy)
	v.updateEmptyState()
}

// onSettled swaps the live bubble out and the persisted transcript in.
func (v *chatView) onSettled() {
	acc := v.ap.rt.Stream()
	if id := v.ap.rt.ActiveID(); id != "" {
		if s, ok := v.ap.rt.ActiveSession(); ok {
			v.messages = s.Messages
			v.transcript.Refresh()
			v.promoteSettledAssistant(s)
		}
	}
	v.liveBox.Hide()
	v.live.SetText("")
	v.slot.setBusy(false)
	v.errLabel.Hide()
	_ = acc
	v.updateButtons()
}

// promoteSettledAssistant is the Phase 2 settle-path promotion: the settled
// assistant response is issued as a workspace block (markdown kind) and a
// SUCCESSFUL registration is promoted onto the native artifact card — the
// contextual affordance refers to the settled block, never to the
// provisional live bubble. Registration is additive: it can only fail by
// refusing invalid content, and the native rendering above is untouched
// either way. Empty or non-assistant content registers nothing.
func (v *chatView) promoteSettledAssistant(s Session) {
	if len(s.Messages) == 0 {
		return
	}
	last := s.Messages[len(s.Messages)-1]
	if last.Role != "assistant" || strings.TrimSpace(last.Content) == "" {
		return
	}
	if b, err := v.ap.Workspace().RegisterChatResponse(s.ID, last.Content); err == nil && b.ID != "" {
		meta := presentation.MetaOf(b)
		uiDo(func() {
			v.artCard.setBlock(meta, last.Content)
			syncWorkspaceAffordance(v.wsBtn, v.ap)
		})
	}
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
					v.updateEmptyState()
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
	v.slot.setBusy(busy)
	v.updateEmptyState()
}

// updateEmptyState shows the capability-priming surface ONLY while the
// conversation is genuinely empty and no run is live — never over live
// content, never wedging the composer (zero-session Send stays usable).
func (v *chatView) updateEmptyState() {
	if len(v.messages) == 0 && !v.ap.rt.Busy() {
		v.emptyBox.Show()
		return
	}
	v.emptyBox.Hide()
}

// sessionRowText renders one sidebar row: active marker, title, and the
// structured message count the core already provides (never UI-parsed).
func sessionRowText(s Session, active bool) string {
	title := s.Title
	if title == "" {
		title = "Session " + shortID(s.ID)
	}
	if active {
		title = "● " + title
	}
	if c := s.MsgCount; c != nil {
		title += fmt.Sprintf("  ·  %d", *c)
	}
	return title
}

// ensure version import used (honest status line carries the core version).
var _ = config.AppVersion
var _ = time.Now

// liveCaption is the shared "Live" heading, WRAPPED so it reflows at
// compact widths instead of escaping the content column (v1.11.1 fit
// repair — the former rigid 331-unit label clipped on phones).
func liveCaption() fyne.CanvasObject {
	l := widget.NewLabelWithStyle("Live — provisional, settled below on completion", fyne.TextAlignLeading, fyne.TextStyle{Bold: true, Italic: true})
	l.Wrapping = fyne.TextWrapWord
	return l
}
