//go:build !headless

// view_agent.go — the Agent view (areas 7 + 12 partial): goals sidebar,
// transcript + live panel, activity ring, net-search composer and the
// task panel for the selected goal, plus the goal decision surface
// (Approve/Reject through the core's GoalDecision — the approvals seam).
package goguiui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// agentViewState is the Agent view's state.
type agentViewState struct {
	st *shellState

	mu        sync.Mutex
	goals     []Goal
	selected  int // index into goals (-1 none)
	messages  []Message
	draft     string
	netSearch bool
	err       error
	seeded    bool
	busyUI    bool
	taskText  string
}

func agentState(s *shellState) *agentViewState {
	return s.view("agent", func() any {
		return &agentViewState{st: s, selected: -1, taskText: "Select a goal to see its observed plan."}
	}).(*agentViewState)
}

func buildAgentView(s *shellState) gui.View {
	v := agentState(s)
	v.ensureSeeded()

	return gui.Column(gui.ContainerCfg{
		ID:      absID("view-agent"),
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingPx(6),
		Content: []gui.View{
			PageHeader("Agent", "Goals, tasks and net-search runs"),
			v.errView(),
			gui.Row(gui.ContainerCfg{
				Sizing:  gui.FillFill,
				Spacing: gui.SpacingPx(6),
				Content: []gui.View{v.goalsView(), v.centerView()},
			}),
		},
	})
}

func (v *agentViewState) ensureSeeded() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.seeded {
		return
	}
	v.seeded = true
	v.busyUI = v.st.ap.rt.Busy()
	goals, err := v.st.ap.Client().Goals(v.st.ap.Ctx())
	if err == nil {
		v.goals = goals
	} else {
		v.err = err
	}
	if s, ok := v.st.ap.rt.ActiveSession(); ok {
		v.messages = s.Messages
	}
}

func (v *agentViewState) errView() gui.View {
	v.mu.Lock()
	err := v.err
	v.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("agent", err, nil)
}

// goalsView is the goals sidebar: the observed goal list (core truth),
// a new-goal entry and the decision actions for the selected goal.
func (v *agentViewState) goalsView() gui.View {
	v.mu.Lock()
	goals := append([]Goal{}, v.goals...)
	selected := v.selected
	v.mu.Unlock()

	items := make([]gui.ListBoxOption, 0, len(goals))
	for i, g := range goals {
		items = append(items, gui.ListBoxOption{
			ID:    g.ID,
			Name:  fmt.Sprintf("[%s] %s", goalStatusLabel(g.Status), truncate(g.Goal, 48)),
			Value: fmt.Sprintf("%d", i),
		})
	}
	sel := []string{}
	if selected >= 0 && selected < len(goals) {
		sel = []string{goals[selected].ID}
	}

	decision := []gui.View{}
	if selected >= 0 && selected < len(goals) {
		g := goals[selected]
		if g.ApprovalID != "" || g.Status == "awaiting_approval" || g.Status == "needs_approval" {
			decision = append(decision,
				gui.Row(gui.ContainerCfg{
					Sizing: gui.FillFit,
					Content: []gui.View{
						gui.Button(gui.ButtonCfg{
							ID:      absID("agent-approve"),
							Label:   "Approve",
							Variant: gui.ButtonPrimary,
							OnClick: func(gui.EventCtx) { v.decide(g.ID, "approve", g.ApprovalID) },
						}),
						gui.Button(gui.ButtonCfg{
							ID:      absID("agent-reject"),
							Label:   "Reject",
							Variant: gui.ButtonDanger,
							OnClick: func(gui.EventCtx) { v.decide(g.ID, "reject", g.ApprovalID) },
						}),
					},
				}),
			)
		}
	}

	return gui.Column(gui.ContainerCfg{
		ID:      absID("agent-goals"),
		Sizing:  gui.FixedFit,
		Width:   250,
		Spacing: gui.SpacingPx(4),
		Content: append([]gui.View{
			gui.Text(gui.TextCfg{Text: "Goals", TextStyle: gui.CurrentTheme().TextStyleBody}),
			gui.Input(gui.InputCfg{
				ID:          absID("agent-new-goal"),
				Placeholder: "New goal…",
				OnEnter:     func(gui.EventCtx) { v.createGoal() },
				OnTextChanged: func(text string, _ gui.EventCtx) {
					v.mu.Lock()
					v.draft = text
					v.mu.Unlock()
				},
			}),
			gui.Button(gui.ButtonCfg{ID: absID("agent-add-goal"), Label: "Add", OnClick: func(gui.EventCtx) { v.createGoal() }}),
			gui.ListBox(gui.ListBoxCfg{
				ID:          absID("agent-goal-list"),
				Data:        items,
				SelectedIDs: sel,
				Height:      220,
				OnSelect: func(sel []string, _ gui.EventCtx) {
					if len(sel) == 0 {
						return
					}
					v.selectGoal(sel[0])
				},
			}),
		}, decision...),
	})
}

// centerView is the transcript + task panel + composer column.
func (v *agentViewState) centerView() gui.View {
	v.mu.Lock()
	messages := append([]Message{}, v.messages...)
	taskText := v.taskText
	busy := v.busyUI
	net := v.netSearch
	v.mu.Unlock()

	var rows []gui.View
	if len(messages) == 0 && !busy {
		rows = append(rows, EmptySurface("◇", "No agent run yet", "Describe a task for the agent; goals and plans render from core evidence only."))
	}
	for _, m := range messages {
		rows = append(rows, v.bubble(m))
	}
	acc := v.st.ap.rt.Stream()
	if busy {
		live := acc.Response
		if live == "" {
			live = acc.Status
		}
		if live == "" {
			live = "waiting for the core…"
		}
		rows = append(rows, card("Live — provisional", "", wrappedText(live)))
	}

	primary := gui.Button(gui.ButtonCfg{
		ID: absID("agent-send"), Label: "Send", Variant: gui.ButtonPrimary,
		OnClick: func(gui.EventCtx) { v.send() },
	})
	if busy {
		primary = gui.Button(gui.ButtonCfg{
			ID: absID("agent-stop"), Label: "Stop", Variant: gui.ButtonDanger,
			OnClick: func(gui.EventCtx) {
				go func() {
					if err := v.st.ap.rt.Stop(v.st.ap.Ctx()); err != nil {
						v.fail("stop", err)
					}
				}()
			},
		})
	}

	return gui.Column(gui.ContainerCfg{
		ID:      absID("agent-center"),
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingPx(6),
		Content: []gui.View{
			gui.Row(gui.ContainerCfg{
				Sizing:  gui.FillFill,
				Spacing: gui.SpacingPx(6),
				Content: []gui.View{
					gui.Column(gui.ContainerCfg{
						ID:         absID("agent-transcript"),
						Sizing:     gui.FillFill,
						Scrollable: true,
						Content:    rows,
					}),
					card("Task", "Observed plan of the selected goal", wrappedText(taskText)),
				},
			}),
			gui.Row(gui.ContainerCfg{
				Sizing: gui.FillFit,
				Content: []gui.View{
					gui.Switch(gui.SwitchCfg{
						ID:       absID("agent-netsearch"),
						Label:    "Net search",
						Selected: net,
						OnClick: func(gui.EventCtx) {
							v.mu.Lock()
							v.netSearch = !v.netSearch
							v.mu.Unlock()
						},
					}),
					gui.Input(gui.InputCfg{
						ID:          absID("agent-composer-entry"),
						Placeholder: "Describe the agent task… (Enter to send)",
						OnEnter:     func(gui.EventCtx) { v.send() },
						OnTextChanged: func(text string, _ gui.EventCtx) {
							// NOTE: the goal entry shares draft state; the agent
							// composer keeps its own field.
							v.mu.Lock()
							v.draft = text
							v.mu.Unlock()
						},
					}),
					primary,
				},
			}),
		},
	})
}

func (v *agentViewState) bubble(m Message) gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingPx(2),
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: roleLabel(m.Role), TextStyle: gui.CurrentTheme().TextStyleCaption}),
			wrappedText(displayContent(m)),
			gui.Separator(gui.SeparatorCfg{}),
		},
	})
}

// --- actions ---

func (v *agentViewState) fail(what string, err error) {
	uiDo(func() {
		v.mu.Lock()
		v.err = fmt.Errorf("%s: %w", what, err)
		v.mu.Unlock()
	})
}

func (v *agentViewState) refreshGoals() {
	go func() {
		got, err := v.st.ap.Client().Goals(v.st.ap.Ctx())
		if err != nil {
			v.fail("goals", err)
			return
		}
		uiDo(func() {
			v.mu.Lock()
			v.goals = got
			v.err = nil
			v.mu.Unlock()
		})
	}()
}

func (v *agentViewState) selectGoal(id string) {
	v.mu.Lock()
	idx := -1
	for i, g := range v.goals {
		if g.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		v.mu.Unlock()
		return
	}
	g := v.goals[idx]
	v.selected = idx
	v.mu.Unlock()

	var b strings.Builder
	b.WriteString("Goal: " + g.Goal + "\n")
	b.WriteString("Status: " + goalStatusLabel(g.Status) + "\n\n")
	if len(g.Plan) == 0 {
		b.WriteString("No plan steps observed yet. Progress renders ONLY from backend evidence.")
	} else {
		for _, st := range g.Plan {
			mark := "○"
			if st.Status == "completed" || st.Status == "done" {
				mark = "●"
			}
			b.WriteString(fmt.Sprintf("%s %s — %s\n", mark, st.Title, goalStatusLabel(st.Status)))
		}
	}
	uiDo(func() {
		v.mu.Lock()
		v.taskText = b.String()
		v.mu.Unlock()
	})
}

func (v *agentViewState) createGoal() {
	v.mu.Lock()
	text := strings.TrimSpace(v.draft)
	v.draft = ""
	v.mu.Unlock()
	if text == "" {
		return
	}
	ap := v.st.ap
	go func() {
		if _, err := ap.Client().CreateGoal(ap.Ctx(), text, ap.rt.ActiveID(), true); err != nil {
			v.fail("create goal", err)
			return
		}
		v.refreshGoals()
	}()
}

// decide posts the goal decision (the approvals seam — wire it, area 12).
func (v *agentViewState) decide(goalID, decision, approvalID string) {
	ap := v.st.ap
	go func() {
		if err := ap.Client().GoalDecision(ap.Ctx(), goalID, decision, approvalID); err != nil {
			v.fail("goal "+decision, err)
			return
		}
		v.refreshGoals()
	}()
}

func (v *agentViewState) send() {
	v.mu.Lock()
	text := strings.TrimSpace(v.draft)
	busy := v.busyUI
	net := v.netSearch
	v.draft = ""
	v.err = nil
	v.busyUI = true
	v.mu.Unlock()
	if text == "" || busy {
		v.mu.Lock()
		v.busyUI = busy
		v.mu.Unlock()
		return
	}
	ap := v.st.ap
	go func() {
		if _, err := ap.rt.Send(ap.Ctx(), text, net, "mid"); err != nil {
			uiDo(func() {
				v.mu.Lock()
				v.err = fmt.Errorf("send: %w", err)
				v.busyUI = false
				v.mu.Unlock()
			})
		}
	}()
}

// wireAgentCallbacks installs the runtime → agent dispatch (App boot).
func wireAgentCallbacks(s *shellState) {
	v := agentState(s)
	s.register(
		func() {
			v.mu.Lock()
			v.busyUI = true
			v.mu.Unlock()
		},
		func() {
			if s, ok := v.st.ap.rt.ActiveSession(); ok {
				v.mu.Lock()
				v.messages = s.Messages
				v.busyUI = false
				v.mu.Unlock()
				if len(s.Messages) > 0 {
					last := s.Messages[len(s.Messages)-1]
					if last.Role == "assistant" && strings.TrimSpace(last.Content) != "" {
						_, _ = v.st.ap.Workspace().RegisterAgentResponse(s.ID, last.Content)
					}
				}
			}
		},
		nil,
	)
}

func goalStatusLabel(s string) string {
	if s == "" {
		return "?"
	}
	return strings.ToUpper(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ = json.Marshal // json import guard (maintenance decode lives in view_system)
