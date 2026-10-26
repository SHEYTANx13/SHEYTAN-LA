//go:build !headless

// view_lab.go + view_automation.go live here: Coding Lab (snapshots +
// structured action console) and Automation & Tasks (task list, run
// controls, artifacts). Every action posts the real backend operation;
// verification lifecycles stay server-side.
package fyneui

import (
	"encoding/json"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// --- Coding Lab ---

type labView struct {
	ap       *App
	tasks    *widget.Label
	actionIn *widget.Entry
	runBtn   *widget.Button
	presets  []*widget.Button
	errLabel *widget.Label
}

func buildLabView(ap *App) fyne.CanvasObject {
	v := &labView{ap: ap}
	v.tasks = wrappedLabel("Loading lab tasks…")
	v.actionIn = widget.NewMultiLineEntry()
	v.actionIn.SetPlaceHolder(`{"action": "start", …} — forwarded verbatim to POST /api/lab`)
	v.actionIn.TextStyle = fyne.TextStyle{Monospace: true}
	v.runBtn = widget.NewButton("Run request", func() { v.run() })
	v.runBtn.Importance = widget.HighImportance

	presetBar := container.NewHBox()
	for _, p := range []struct{ label, action string }{
		{"Inspect source", "inspect"},
		{"Run command", "command"},
		{"Verify", "verify"},
		{"Finish", "finish"},
	} {
		pp := p
		b := widget.NewButton(pp.label, func() { v.preset(pp.action) })
		presetBar.Add(b)
	}

	v.errLabel = widget.NewLabel("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Wrapping = fyne.TextWrapWord
	v.errLabel.Hide()

	note := wrappedLabel("The Lab lifecycle gates completion: a finish without verification is refused by the core. The UI never marks unverified work complete.")
	root := container.NewBorder(
		PageHeader("Coding Lab", "Verified tool-execution workspaces"),
		v.errLabel, nil, nil,
		container.NewScroll(container.NewVBox(
			widget.NewCard("Tasks", "", v.tasks),
			widget.NewCard("Action console", "Server-validated actions only", container.NewVBox(presetBar, v.actionIn, v.runBtn, note)),
		)),
	)
	v.refresh()
	return root
}

func (v *labView) preset(action string) {
	payload := map[string]any{"action": action}
	b, _ := json.MarshalIndent(payload, "", "  ")
	v.actionIn.SetText(string(b))
}

func (v *labView) refresh() {
	go func() {
		raw, err := v.ap.Client().Lab(v.ap.Ctx())
		if err != nil {
			uiDo(func() { v.showErr("lab", err) })
			return
		}
		uiDo(func() { v.tasks.SetText(jsonPretty(raw, 6000)); v.errLabel.Hide() })
	}()
}

func (v *labView) run() {
	var payload map[string]any
	if err := json.Unmarshal([]byte(v.actionIn.Text), &payload); err != nil {
		v.showErr("action JSON", err)
		return
	}
	ap := v.ap
	go func() {
		raw, err := ap.Client().LabRun(ap.Ctx(), payload)
		if err != nil {
			uiDo(func() { v.showErr("lab action", err) })
			return
		}
		uiDo(func() {
			v.tasks.SetText(jsonPretty(raw, 6000))
			v.errLabel.Hide()
		})
		v.refresh()
	}()
}

func (v *labView) showErr(what string, err error) {
	v.errLabel.SetText(what + " failed: " + err.Error())
	v.errLabel.Show()
}

// --- Automation & Tasks ---

type automationView struct {
	ap       *App
	tasks    *widget.Label
	arts     *widget.Label
	refreshB *widget.Button
	errLabel *widget.Label
}

func buildAutomationView(ap *App) fyne.CanvasObject {
	v := &automationView{ap: ap}
	v.tasks = wrappedLabel("Loading tasks…")
	v.arts = wrappedLabel("Loading artifacts…")
	v.refreshB = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() { v.refresh() })
	v.errLabel = widget.NewLabel("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Wrapping = fyne.TextWrapWord
	v.errLabel.Hide()

	actions := container.NewHBox()
	for _, a := range []struct{ label, action string }{
		{"Run", "run"}, {"Pause", "pause"}, {"Resume", "resume"}, {"Cancel", "cancel"},
	} {
		aa := a
		b := widget.NewButton(aa.label, func() { v.act(aa.action) })
		actions.Add(b)
	}

	root := container.NewBorder(
		PageHeader("Automation", "Scheduled tasks and artifacts", v.refreshB),
		v.errLabel, nil, nil,
		container.NewScroll(container.NewVBox(
			widget.NewCard("Task controls", "Applies the action typed below to the task id typed below (server-validated)", container.NewVBox(actions)),
			widget.NewCard("Tasks", "", v.tasks),
			widget.NewCard("Artifacts", "", v.arts),
		)),
	)
	v.refresh()
	return root
}

func (v *automationView) refresh() {
	go func() {
		tasks, arts, err := v.ap.Client().Automation(v.ap.Ctx())
		if err != nil {
			uiDo(func() { v.showErr("automation", err) })
			return
		}
		uiDo(func() {
			v.tasks.SetText(jsonPretty(tasks, 6000))
			v.arts.SetText(jsonPretty(arts, 4000))
			v.errLabel.Hide()
		})
	}()
}

func (v *automationView) act(action string) {
	// Bounded, explicit task targeting: the task list carries the ids and
	// this dialog posts the REAL backend operation.
	entry := widget.NewEntry()
	entry.SetPlaceHolder("task id")
	d := newTaskActionDialog(v, action, entry)
	d.Show()
}

// newTaskActionDialog builds the modal form for one task action.
func newTaskActionDialog(v *automationView, action string, entry *widget.Entry) dialog.Dialog {
	f := dialog.NewForm("Automation: "+action, "Apply", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Task id", entry)},
		func(ok bool) {
			id := strings.TrimSpace(entry.Text)
			if !ok || id == "" {
				return
			}
			go func() {
				if err := v.ap.Client().AutomationTaskAction(v.ap.Ctx(), id, action); err != nil {
					uiDo(func() { v.showErr("task "+action, err) })
					return
				}
				v.refresh()
			}()
		}, v.ap.Window())
	f.Resize(fyne.NewSize(380, 170))
	return f
}

func (v *automationView) showErr(what string, err error) {
	v.errLabel.SetText(fmt.Sprintf("%s failed: %v", what, err))
	v.errLabel.Show()
}
