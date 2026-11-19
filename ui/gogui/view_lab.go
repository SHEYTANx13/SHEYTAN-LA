//go:build !headless

// view_lab.go + view_automation.go in one file (mirroring the Fyne
// layout): Coding Lab (area 7, structured action console) and Automation
// & Tasks (area 8, task list + run controls + artifacts). Every action
// posts the real backend operation; verification lifecycles stay
// server-side.
package goguiui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// --- Coding Lab (area 7: goals sidebar lives in Agent; the Lab surface
// carries the verified-workspace console) ---

type labViewState struct {
	st *shellState

	mu       sync.Mutex
	tasks    string
	actionIn string
	err      error
	loaded   bool
}

func labState(s *shellState) *labViewState {
	return s.view("lab", func() any { return &labViewState{st: s} }).(*labViewState)
}

func buildLabView(s *shellState) gui.View {
	v := labState(s)
	v.ensureLoaded()

	presets := make([]gui.View, 0, 4)
	for _, p := range []struct{ label, action string }{
		{"Inspect source", "inspect"},
		{"Run command", "command"},
		{"Verify", "verify"},
		{"Finish", "finish"},
	} {
		pp := p
		presets = append(presets, gui.Button(gui.ButtonCfg{
			ID:      absID("lab-preset", pp.action),
			Label:   pp.label,
			OnClick: func(gui.EventCtx) { v.preset(pp.action) },
		}))
	}

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-lab"),
		Sizing: gui.FillFill,
		Content: []gui.View{
			PageHeader("Coding Lab", "Verified tool-execution workspaces"),
			v.errView(),
			gui.Column(gui.ContainerCfg{
				ID:         absID("lab-scroll"),
				Sizing:     gui.FillFill,
				Scrollable: true,
				Spacing:    gui.SpacingPx(6),
				Content: []gui.View{
					v.tasksCard(),
					card("Action console", "Server-validated actions only",
						gui.Row(gui.ContainerCfg{Content: presets}),
						gui.Input(gui.InputCfg{
							ID:          absID("lab-action"),
							Text:        v.action(),
							Placeholder: `{"action": "start", …} — forwarded verbatim to POST /api/lab`,
							OnEnter:     func(gui.EventCtx) { v.run() },
							OnTextChanged: func(text string, _ gui.EventCtx) {
								v.mu.Lock()
								v.actionIn = text
								v.mu.Unlock()
							},
						}),
						gui.Button(gui.ButtonCfg{ID: absID("lab-run"), Label: "Run request", Variant: gui.ButtonPrimary, OnClick: func(gui.EventCtx) { v.run() }}),
						wrappedText("The Lab lifecycle gates completion: a finish without verification is refused by the core. The UI never marks unverified work complete."),
					),
				},
			}),
		},
	})
}

func (v *labViewState) ensureLoaded() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()
	v.refresh()
}

func (v *labViewState) action() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.actionIn
}

func (v *labViewState) tasksCard() gui.View {
	v.mu.Lock()
	tasks := v.tasks
	err := v.err
	v.mu.Unlock()
	if err != nil {
		return ErrorSurface("lab", err, func() { v.refresh() })
	}
	if tasks == "" {
		return card("Tasks", "", LoadingSurface("lab tasks"))
	}
	return card("Tasks", "", codeText(tasks))
}

func (v *labViewState) errView() gui.View {
	v.mu.Lock()
	err := v.err
	v.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("lab", err, nil)
}

func (v *labViewState) preset(action string) {
	payload := map[string]any{"action": action}
	b, _ := json.MarshalIndent(payload, "", "  ")
	uiDo(func() {
		v.mu.Lock()
		v.actionIn = string(b)
		v.mu.Unlock()
	})
}

func (v *labViewState) refresh() {
	go func() {
		raw, err := v.st.ap.Client().Lab(v.st.ap.Ctx())
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
			v.tasks = jsonPretty(raw, 6000)
			v.err = nil
			v.mu.Unlock()
		})
	}()
}

func (v *labViewState) run() {
	var payload map[string]any
	if err := json.Unmarshal([]byte(v.action()), &payload); err != nil {
		uiDo(func() {
			v.mu.Lock()
			v.err = fmt.Errorf("action JSON: %w", err)
			v.mu.Unlock()
		})
		return
	}
	go func() {
		raw, err := v.st.ap.Client().LabRun(v.st.ap.Ctx(), payload)
		if err != nil {
			uiDo(func() {
				v.mu.Lock()
				v.err = fmt.Errorf("lab action: %w", err)
				v.mu.Unlock()
			})
			return
		}
		uiDo(func() {
			v.mu.Lock()
			v.tasks = jsonPretty(raw, 6000)
			v.err = nil
			v.mu.Unlock()
		})
		v.refresh()
	}()
}

// --- Automation & Tasks (area 8: the activity stream is the runtime WS
// pump; this view shows the scheduled tasks + artifacts + run controls) ---

type automationViewState struct {
	st *shellState

	mu        sync.Mutex
	tasks     string
	arts      string
	taskIDVal string
	err       error
	loaded    bool
	activity  []string
}

func automationState(s *shellState) *automationViewState {
	return s.view("automation", func() any { return &automationViewState{st: s} }).(*automationViewState)
}

func buildAutomationView(s *shellState) gui.View {
	v := automationState(s)
	v.ensureLoaded()

	refresh := gui.Button(gui.ButtonCfg{
		ID:      absID("automation-refresh"),
		Label:   "Refresh",
		OnClick: func(gui.EventCtx) { v.refresh() },
	})

	actions := make([]gui.View, 0, 4)
	for _, a := range []struct{ label, action string }{
		{"Run", "run"}, {"Pause", "pause"}, {"Resume", "resume"}, {"Cancel", "cancel"},
	} {
		aa := a
		actions = append(actions, gui.Button(gui.ButtonCfg{
			ID:      absID("automation-act", aa.action),
			Label:   aa.label,
			OnClick: func(gui.EventCtx) { v.act(aa.action) },
		}))
	}

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-automation"),
		Sizing: gui.FillFill,
		Content: []gui.View{
			PageHeader("Automation", "Scheduled tasks and artifacts", refresh),
			v.errView(),
			gui.Column(gui.ContainerCfg{
				ID:         absID("automation-scroll"),
				Sizing:     gui.FillFill,
				Scrollable: true,
				Spacing:    gui.SpacingPx(6),
				Content: []gui.View{
					card("Task controls", "Applies the action to the task id (server-validated)",
						gui.Input(gui.InputCfg{
							ID:          absID("automation-task-id"),
							Text:        v.taskID(),
							Placeholder: "task id",
							OnEnter:     func(gui.EventCtx) { v.act("run") },
							OnTextChanged: func(text string, _ gui.EventCtx) {
								v.mu.Lock()
								v.taskIDVal = text
								v.mu.Unlock()
							},
						}),
						gui.Row(gui.ContainerCfg{Content: actions}),
					),
					v.tasksCard(),
					v.artsCard(),
					v.activityCard(),
				},
			}),
		},
	})
}

func (v *automationViewState) ensureLoaded() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()
	v.refresh()
}

func (v *automationViewState) taskID() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.taskIDVal
}

func (v *automationViewState) tasksCard() gui.View {
	v.mu.Lock()
	tasks := v.tasks
	v.mu.Unlock()
	if tasks == "" {
		return card("Tasks", "", LoadingSurface("tasks"))
	}
	return card("Tasks", "", codeText(tasks))
}

func (v *automationViewState) artsCard() gui.View {
	v.mu.Lock()
	arts := v.arts
	v.mu.Unlock()
	if arts == "" {
		return card("Artifacts", "", LoadingSurface("artifacts"))
	}
	return card("Artifacts", "", codeText(arts))
}

// activityCard renders the live WS activity ring (the same bounded
// captions the runtime accumulator folds — Global Queue/Activity).
func (v *automationViewState) activityCard() gui.View {
	acc := v.st.ap.rt.Stream()
	rows := []gui.View{}
	if len(acc.Activities) == 0 {
		rows = append(rows, gui.Text(gui.TextCfg{Text: "No live activity — the stream attaches when a run is live.", TextStyle: gui.CurrentTheme().TextStyleCaption}))
	} else {
		for i := len(acc.Activities) - 1; i >= 0 && len(rows) < 10; i-- {
			rows = append(rows, gui.Text(gui.TextCfg{Text: "· " + acc.Activities[i], TextStyle: gui.CurrentTheme().TextStyleCaption, Mode: gui.TextModeWrap}))
		}
	}
	return card("Activity stream", "Live events from /ws/activity", rows...)
}

func (v *automationViewState) errView() gui.View {
	v.mu.Lock()
	err := v.err
	v.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("automation", err, nil)
}

func (v *automationViewState) refresh() {
	go func() {
		tasks, arts, err := v.st.ap.Client().Automation(v.st.ap.Ctx())
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
			v.tasks = jsonPretty(tasks, 6000)
			v.arts = jsonPretty(arts, 4000)
			v.err = nil
			v.mu.Unlock()
		})
	}()
}

func (v *automationViewState) act(action string) {
	id := strings.TrimSpace(v.taskID())
	if id == "" {
		return
	}
	go func() {
		if err := v.st.ap.Client().AutomationTaskAction(v.st.ap.Ctx(), id, action); err != nil {
			uiDo(func() {
				v.mu.Lock()
				v.err = fmt.Errorf("task %s: %w", action, err)
				v.mu.Unlock()
			})
			return
		}
		v.refresh()
	}()
}
