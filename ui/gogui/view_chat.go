//go:build !headless

// view_chat.go — the Chat/Home view (area 1): session sidebar, transcript,
// live stream bubble and composer. The contracts carried from the retired
// surfaces (and pinned by the Fyne tests, re-pinned here):
//
//   - zero-session send: the composer is ALWAYS enabled; Send with no
//     session creates one in the current mode and completes the send.
//   - live events fold cumulatively (REPLACE, never append).
//   - settlement reloads the authoritative transcript; the live bubble
//     never promotes itself as the final message.
//   - stop keeps partial output; terminal outcomes are honest.
//
// go-gui mapping: the view is a pure function of chatViewState; the
// composer mutates state, runtime workers fold the stream into the
// runtime accumulator, and the frame renders whatever is current.
package goguiui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// chatViewState is the Chat view's state (one instance per shell).
type chatViewState struct {
	st *shellState

	mu        sync.Mutex
	sessions  []Session
	messages  []Message
	draft     string
	thinking  string
	netSearch bool
	err       error
	seeded    bool
	busyUI    bool // mirrors rt.Busy() for the primary slot rendering
}

func chatState(s *shellState) *chatViewState {
	return s.view("chat", func() any {
		return &chatViewState{st: s, thinking: "mid"}
	}).(*chatViewState)
}

// buildChatView constructs the Chat view bound to mode "chat".
func buildChatView(s *shellState) gui.View {
	v := chatState(s)
	v.ensureSeeded()

	return gui.Column(gui.ContainerCfg{
		ID:      absID("view-chat"),
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingPx(6),
		Content: []gui.View{
			PageHeader("Chat", "Conversations in the chat space"),
			v.errView(),
			v.bodyView(),
		},
	})
}

// ensureSeeded copies the runtime's session list once (presentation seed;
// the bootstrap onSessions callback refreshes it afterwards).
func (v *chatViewState) ensureSeeded() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.seeded {
		return
	}
	v.seeded = true
	v.sessions = v.st.ap.rt.Sessions()
	v.busyUI = v.st.ap.rt.Busy()
}

// refreshSessions applies a new session list (runtime callback).
func (v *chatViewState) refreshSessions(list []Session) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.sessions = list
	v.busyUI = v.st.ap.rt.Busy()
}

// bodyView is the responsive dual pane: sessions sidebar + conversation.
func (v *chatViewState) bodyView() gui.View {
	return gui.Row(gui.ContainerCfg{
		ID:      absID("chat-body"),
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingPx(6),
		Content: []gui.View{
			v.sessionsView(),
			gui.Column(gui.ContainerCfg{
				Sizing:  gui.FillFill,
				Spacing: gui.SpacingPx(6),
				Content: []gui.View{v.transcriptView(), v.composerView()},
			}),
		},
	})
}

// sessionsView is the left sidebar: the sessions list + controls.
func (v *chatViewState) sessionsView() gui.View {
	v.mu.Lock()
	sessions := append([]Session{}, v.sessions...)
	activeID := v.st.ap.rt.ActiveID()
	v.mu.Unlock()

	items := make([]gui.ListBoxOption, 0, len(sessions))
	selected := []string{}
	for _, s := range sessions {
		items = append(items, gui.ListBoxOption{ID: s.ID, Name: sessionRowText(s, s.ID == activeID), Value: s.ID})
		if s.ID == activeID {
			selected = append(selected, s.ID)
		}
	}

	return gui.Column(gui.ContainerCfg{
		ID:      absID("chat-sessions"),
		Sizing:  gui.FixedFit,
		Width:   230,
		Spacing: gui.SpacingPx(4),
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: "Sessions", TextStyle: gui.CurrentTheme().TextStyleBody}),
			gui.Row(gui.ContainerCfg{
				Sizing: gui.FitFit,
				Content: []gui.View{
					gui.Button(gui.ButtonCfg{ID: absID("chat-new"), Label: "New", OnClick: func(gui.EventCtx) { v.newSession() }}),
					gui.Button(gui.ButtonCfg{ID: absID("chat-del"), Label: "Delete", OnClick: func(gui.EventCtx) { v.deleteActive() }}),
				},
			}),
			gui.ListBox(gui.ListBoxCfg{
				ID:          absID("chat-session-list"),
				Data:        items,
				SelectedIDs: selected,
				Height:      320,
				OnSelect: func(sel []string, _ gui.EventCtx) {
					if len(sel) == 0 {
						return
					}
					v.openSession(sel[0])
				},
			}),
		},
	})
}

// transcriptView renders the persisted transcript (if any) + the live
// bubble (PROVISIONAL — settlement rule) + the honest empty state.
func (v *chatViewState) transcriptView() gui.View {
	v.mu.Lock()
	messages := append([]Message{}, v.messages...)
	v.mu.Unlock()

	acc := v.st.ap.rt.Stream()
	busy := v.st.ap.rt.Busy()

	var rows []gui.View
	if len(messages) == 0 && !busy && acc.Response == "" {
		rows = append(rows, EmptySurface("◇", "No conversation yet",
			"Send a message to start. SHEYTAN runs the local core; this view mirrors only what the core publishes."))
	}
	for _, m := range messages {
		rows = append(rows, v.messageBubble(m))
	}
	if busy {
		// The live surface is PROVISIONAL: settlement replaces it with the
		// persisted transcript (onSettled reloads the session).
		live := acc.Response
		if live == "" {
			live = acc.Reasoning
		}
		if live == "" {
			live = acc.Status
		}
		if live == "" {
			live = "waiting for the core…"
		}
		rows = append(rows, card("Live — provisional, settled below on completion", "", wrappedText(live)))
	}
	return gui.Column(gui.ContainerCfg{
		ID:         absID("chat-transcript"),
		Sizing:     gui.FillFill,
		Scrollable: true,
		Spacing:    gui.SpacingPx(6),
		Padding:    gui.PadAll(4),
		Content:    rows,
	})
}

// messageBubble renders one persisted message (role line + wrapped body).
func (v *chatViewState) messageBubble(m Message) gui.View {
	role := roleLabel(m.Role)
	body := displayContent(m)
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingPx(2),
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: role, TextStyle: gui.CurrentTheme().TextStyleCaption}),
			wrappedText(body),
			gui.Separator(gui.SeparatorCfg{}),
		},
	})
}

// composerView is the entry + primary action + generation options.
// ZERO-SESSION contract: the composer is ALWAYS enabled (Send with no
// session creates one in the current mode and completes the send).
func (v *chatViewState) composerView() gui.View {
	v.mu.Lock()
	draft := v.draft
	busy := v.busyUI
	thinking := v.thinking
	net := v.netSearch
	v.mu.Unlock()

	primary := gui.Button(gui.ButtonCfg{
		ID:      absID("chat-send"),
		Label:   "Send",
		Width:   120,
		Variant: gui.ButtonPrimary,
		OnClick: func(gui.EventCtx) { v.send() },
	})
	if busy {
		// ONE primary slot: Stop while a run is live (the Fyne
		// primaryActionSlot contract).
		primary = gui.Button(gui.ButtonCfg{
			ID:      absID("chat-stop"),
			Label:   "Stop",
			Width:   120,
			Variant: gui.ButtonDanger,
			OnClick: func(gui.EventCtx) { v.stop() },
		})
	}

	return gui.Column(gui.ContainerCfg{
		ID:      absID("chat-composer"),
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingPx(6),
		Content: []gui.View{
			gui.Row(gui.ContainerCfg{
				Sizing: gui.FitFit,
				Content: []gui.View{
					gui.Select(gui.SelectCfg{
						ID:          absID("chat-thinking"),
						Items:       ThinkingLevels,
						Placeholder: "Thinking: " + orDash(thinking),
						Selected:    []string{thinking},
						MaxWidth:    160,
						OnSelect: func(vals []string, _ gui.EventCtx) {
							if len(vals) > 0 {
								v.mu.Lock()
								v.thinking = vals[0]
								v.mu.Unlock()
							}
						},
					}),
					gui.Switch(gui.SwitchCfg{
						ID:       absID("chat-netsearch"),
						Label:    "Net search",
						Selected: net,
						OnClick: func(_ gui.EventCtx) {
							v.mu.Lock()
							v.netSearch = !v.netSearch
							v.mu.Unlock()
						},
					}),
				},
			}),
			gui.Row(gui.ContainerCfg{
				Sizing: gui.FillFit,
				Content: []gui.View{
					gui.Input(gui.InputCfg{
						ID:          absID("chat-composer-entry"),
						Text:        draft,
						Placeholder: "Message SHEYTAN… (Enter to send)",
						// Width-fill: the entry absorbs the remaining row
						// width so the composer NEVER overflows a narrow
						// dock pane (the measured placeholder width would).
						Sizing: gui.FillFit,
						OnTextChanged: func(text string, _ gui.EventCtx) {
							v.mu.Lock()
							v.draft = text
							v.mu.Unlock()
						},
						OnEnter: func(_ gui.EventCtx) { v.send() },
					}),
					primary,
				},
			}),
		},
	})
}

// errView renders the last view-local failure (typed, never silent).
func (v *chatViewState) errView() gui.View {
	v.mu.Lock()
	err := v.err
	v.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("chat", err, nil)
}

// --- actions ---

func (v *chatViewState) fail(what string, err error) {
	uiDo(func() {
		v.mu.Lock()
		v.err = fmt.Errorf("%s: %w", what, err)
		v.mu.Unlock()
	})
}

func (v *chatViewState) newSession() {
	ap := v.st.ap
	go func() {
		if _, err := ap.rt.CreateSession(ap.Ctx(), ""); err != nil {
			v.fail("create session", err)
			return
		}
		uiDo(func() {
			v.refreshSessions(ap.rt.Sessions())
			v.mu.Lock()
			v.messages = nil
			v.mu.Unlock()
		})
	}()
}

func (v *chatViewState) openSession(id string) {
	ap := v.st.ap
	go func() {
		s, err := ap.rt.SelectSession(ap.Ctx(), id)
		if err != nil {
			v.fail("open session", err)
			return
		}
		uiDo(func() {
			v.mu.Lock()
			v.messages = s.Messages
			v.mu.Unlock()
		})
		_ = ap.rt.AttachActivity(ap.Ctx(), id)
	}()
}

func (v *chatViewState) deleteActive() {
	id := v.st.ap.rt.ActiveID()
	if id == "" {
		return
	}
	ap := v.st.ap
	go func() {
		if err := ap.rt.DeleteSession(ap.Ctx(), id); err != nil {
			v.fail("delete session", err)
			return
		}
		uiDo(func() {
			v.refreshSessions(ap.rt.Sessions())
			v.mu.Lock()
			v.messages = nil
			v.mu.Unlock()
		})
	}()
}

func (v *chatViewState) send() {
	v.mu.Lock()
	text := strings.TrimSpace(v.draft)
	busy := v.busyUI
	thinking := v.thinking
	net := v.netSearch
	v.mu.Unlock()
	if text == "" || busy {
		return
	}
	ap := v.st.ap
	v.mu.Lock()
	v.draft = ""
	v.err = nil
	v.busyUI = true
	v.mu.Unlock()
	go func() {
		if _, err := ap.rt.Send(ap.Ctx(), text, net, thinking); err != nil {
			uiDo(func() {
				v.mu.Lock()
				v.err = fmt.Errorf("send: %w", err)
				v.busyUI = false
				v.mu.Unlock()
			})
		}
	}()
}

func (v *chatViewState) stop() {
	ap := v.st.ap
	go func() {
		if err := ap.rt.Stop(ap.Ctx()); err != nil {
			v.fail("stop", err)
		}
	}()
}

// wireChatCallbacks installs the runtime → chat dispatch (App boot).
// The shell's ONE dispatch fans out to the built chat view state.
func wireChatCallbacks(s *shellState) {
	v := chatState(s)
	s.register(
		func() { v.onStream() },
		func() { v.onSettled() },
		func() { v.refreshSessions(s.ap.rt.Sessions()) },
	)
}

func (v *chatViewState) onStream() {
	v.mu.Lock()
	v.busyUI = true
	v.mu.Unlock()
}

// onSettled reloads the authoritative transcript; the live bubble is
// replaced by the persisted server message (settlement rule).
func (v *chatViewState) onSettled() {
	if s, ok := v.st.ap.rt.ActiveSession(); ok {
		v.mu.Lock()
		v.messages = s.Messages
		v.busyUI = false
		v.mu.Unlock()
		// Phase 2 settle-path promotion: the settled assistant response is
		// issued as a workspace block (additive; failures are invisible).
		if len(s.Messages) > 0 {
			last := s.Messages[len(s.Messages)-1]
			if last.Role == "assistant" && strings.TrimSpace(last.Content) != "" {
				_, _ = v.st.ap.Workspace().RegisterChatResponse(s.ID, last.Content)
			}
		}
	}
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
