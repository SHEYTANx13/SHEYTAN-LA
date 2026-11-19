//go:build !headless

// view_approvals.go — the Approvals/Permissions view (area 12): pending
// goal decisions (Approve/Reject through the core's GoalDecision) and the
// tool access matrix summary. The Fyne client carries GoalDecision in the
// Agent surface; this view exposes the SAME core operation as its own
// registry destination so approvals are reachable without hunting.
package goguiui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// approvalsViewState is the Approvals surface's state.
type approvalsViewState struct {
	st *shellState

	mu      sync.Mutex
	goals   []Goal
	tools   []Tool
	enabled map[string]bool
	err     error
	loaded  bool
}

func approvalsState(s *shellState) *approvalsViewState {
	return s.view("approvals", func() any {
		return &approvalsViewState{st: s, enabled: map[string]bool{}}
	}).(*approvalsViewState)
}

// buildApprovalsView is NOT in the primary registry (the IA keeps 9
// primary destinations); it rides the approvals dock panel and the Agent
// decision surface. It is a real, tested surface — just not a rail entry.
func buildApprovalsView(s *shellState) gui.View {
	v := approvalsState(s)
	v.ensureLoaded()

	v.mu.Lock()
	goals := append([]Goal{}, v.goals...)
	tools := append([]Tool{}, v.tools...)
	enabled := map[string]bool{}
	for k, on := range v.enabled {
		enabled[k] = on
	}
	err := v.err
	v.mu.Unlock()

	rows := []gui.View{PageHeader("Approvals", "Goal decisions and tool access — the core enforces every grant")}
	if err != nil {
		rows = append(rows, ErrorSurface("approvals", err, nil))
	}

	// Pending decisions.
	pending := 0
	rows = append(rows, gui.Text(gui.TextCfg{Text: "Pending decisions", TextStyle: gui.CurrentTheme().TextStyleBody}))
	for _, g := range goals {
		if g.Status != "awaiting_approval" && g.Status != "needs_approval" && g.ApprovalID == "" {
			continue
		}
		pending++
		rows = append(rows, card(truncate(g.Goal, 60), "status: "+goalStatusLabel(g.Status),
			gui.Row(gui.ContainerCfg{
				Content: []gui.View{
					gui.Button(gui.ButtonCfg{
						ID:      absID("approvals-approve", g.ID),
						Label:   "Approve",
						Variant: gui.ButtonPrimary,
						OnClick: func(gui.EventCtx) { v.decide(g.ID, "approve", g.ApprovalID) },
					}),
					gui.Button(gui.ButtonCfg{
						ID:      absID("approvals-reject", g.ID),
						Label:   "Reject",
						Variant: gui.ButtonDanger,
						OnClick: func(gui.EventCtx) { v.decide(g.ID, "reject", g.ApprovalID) },
					}),
				},
			}),
		))
	}
	if pending == 0 {
		rows = append(rows, EmptySurface("◇", "No pending decisions", "Goals awaiting permission appear here with Approve/Reject."))
	}

	// Tool access matrix (the same ONE-patch contract).
	rows = append(rows, gui.Text(gui.TextCfg{Text: "Tool access", TextStyle: gui.CurrentTheme().TextStyleBody}))
	for _, t := range tools {
		tt := t
		on := enabled[tt.Name]
		rows = append(rows, gui.Row(gui.ContainerCfg{
			Content: []gui.View{
				gui.Switch(gui.SwitchCfg{
					ID: absID("approvals-tool", tt.Name), Label: tt.Name, Selected: on,
					OnClick: func(gui.EventCtx) { v.toggleTool(tt.Name, !on) },
				}),
				wrappedText(tt.Description),
			},
		}))
	}

	return gui.Column(gui.ContainerCfg{
		ID:         absID("view-approvals"),
		Sizing:     gui.FillFill,
		Scrollable: true,
		Spacing:    gui.SpacingPx(6),
		Content:    rows,
	})
}

func (v *approvalsViewState) ensureLoaded() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()

	ap := v.st.ap
	go func() {
		goals, gerr := ap.Client().Goals(ap.Ctx())
		tools, terr := ap.Client().Tools(ap.Ctx())
		cfg, cerr := ap.Client().Config(ap.Ctx())
		uiDo(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if gerr != nil {
				v.err = gerr
				return
			}
			v.goals = goals
			if terr == nil {
				v.tools = tools
			}
			if cerr == nil {
				for _, t := range cfg.EnabledTools {
					v.enabled[t] = true
				}
			}
		})
	}()
}

func (v *approvalsViewState) decide(goalID, decision, approvalID string) {
	ap := v.st.ap
	go func() {
		if err := ap.Client().GoalDecision(ap.Ctx(), goalID, decision, approvalID); err != nil {
			uiDo(func() {
				v.mu.Lock()
				v.err = fmt.Errorf("goal %s: %w", decision, err)
				v.mu.Unlock()
			})
			return
		}
		v.ensureLoaded()
		v.reload()
	}()
}

func (v *approvalsViewState) reload() {
	go func() {
		goals, err := v.st.ap.Client().Goals(v.st.ap.Ctx())
		uiDo(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if err == nil {
				v.goals = goals
				v.err = nil
			}
		})
	}()
}

func (v *approvalsViewState) toggleTool(name string, on bool) {
	uiDo(func() {
		v.mu.Lock()
		set := map[string]bool{}
		for k, enabled := range v.enabled {
			set[k] = enabled
		}
		set[name] = on
		names := make([]string, 0, len(set))
		for k, enabled := range set {
			if enabled {
				names = append(names, k)
			}
		}
		sortStrings(names)
		v.enabled[name] = on
		v.mu.Unlock()
		go func() {
			cfg, err := v.st.ap.Client().PatchConfig(v.st.ap.Ctx(), EnabledToolsPatch(names))
			if err != nil {
				uiDo(func() {
					v.mu.Lock()
					v.err = err
					v.mu.Unlock()
				})
				return
			}
			uiDo(func() {
				v.mu.Lock()
				for _, t := range cfg.EnabledTools {
					v.enabled[t] = true
				}
				v.mu.Unlock()
			})
		}()
	})
}

var _ = strings.TrimSpace
