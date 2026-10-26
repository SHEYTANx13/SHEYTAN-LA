//go:build !headless

// view_agent.go — the Agent view: goals sidebar, transcript + live panel,
// maintenance banner, engine download progress, activity ring, net-search
// armed composer and the task panel for the selected goal.
package fyneui

import (
	"encoding/json"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type agentView struct {
	ap         *App
	goalList   *widget.List
	goals      []Goal
	transcript *widget.List
	messages   []Message
	live       *widget.Label
	activity   *widget.Label
	taskPanel  *widget.Label
	maint      *widget.Label
	download   *widget.Label
	cancelDl   *widget.Button
	input      *widget.Entry
	sendBtn    *widget.Button
	stopBtn    *widget.Button
	netSearch  *widget.Check
	errLabel   *widget.Label
	newGoal    *widget.Entry
	addGoal    *widget.Button
}

func buildAgentView(ap *App) fyne.CanvasObject {
	v := &agentView{ap: ap}

	v.goalList = widget.NewList(
		func() int { return len(v.goals) },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			g := v.goals[i]
			o.(*widget.Label).SetText(fmt.Sprintf("[%s] %s", goalStatusLabel(g.Status), truncate(g.Goal, 60)))
		},
	)
	v.goalList.OnSelected = func(i widget.ListItemID) { v.renderTask(i) }

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
			m := v.messages[i]
			box.Objects[0].(*widget.Label).SetText(roleLabel(m.Role))
			box.Objects[1].(*widget.Label).SetText(displayContent(m))
		},
	)

	v.live = widget.NewLabel("")
	v.live.Wrapping = fyne.TextWrapWord
	v.live.TextStyle = fyne.TextStyle{Italic: true}
	liveStop := widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), v.stop)
	liveStop.Importance = widget.DangerImportance
	liveBox := container.NewVBox(widget.NewLabel("Live"), container.NewHBox(v.live, liveStop))

	v.activity = widget.NewLabel("")
	v.activity.Wrapping = fyne.TextWrapWord
	v.activity.TextStyle = fyne.TextStyle{Monospace: true}
	activityBox := container.NewVBox(widget.NewLabelWithStyle("Activity (observed evidence)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), v.activity)

	v.taskPanel = widget.NewLabel("Select a goal to see its observed plan.")
	v.taskPanel.Wrapping = fyne.TextWrapWord

	v.maint = widget.NewLabel("")
	v.maint.Wrapping = fyne.TextWrapWord
	v.maint.Hide()
	v.download = widget.NewLabel("")
	v.download.Wrapping = fyne.TextWrapWord
	v.download.Hide()
	v.cancelDl = widget.NewButton("Stop download", func() {
		go func() {
			if err := ap.Client().CancelEngineDownload(ap.Ctx()); err != nil {
				uiDo(func() { v.showErr("cancel download", err) })
			}
		}()
	})
	v.cancelDl.Hide()

	v.input = widget.NewMultiLineEntry()
	v.input.SetPlaceHolder("Describe the agent task…")
	v.input.OnSubmitted = func(string) { v.send() }
	v.sendBtn = widget.NewButtonWithIcon("Send", theme.MediaPlayIcon(), v.send)
	v.sendBtn.Importance = widget.HighImportance
	v.stopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), v.stop)
	v.stopBtn.Importance = widget.DangerImportance
	v.netSearch = widget.NewCheck("Net search", nil)

	v.newGoal = widget.NewEntry()
	v.newGoal.SetPlaceHolder("New goal…")
	v.addGoal = widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), func() { v.createGoal() })

	v.errLabel = widget.NewLabel("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Wrapping = fyne.TextWrapWord
	v.errLabel.Hide()

	composer := container.NewVBox(
		container.NewHBox(v.netSearch, layoutSpacer(), v.sendBtn, v.stopBtn),
		v.input,
	)

	goalsPane := container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Goals", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), v.newGoal, v.addGoal),
		nil, nil, nil, container.NewScroll(v.goalList),
	)

	center := container.NewAppTabs(
		container.NewTabItem("Transcript", container.NewScroll(container.NewVBox(v.transcript, liveBox))),
		container.NewTabItem("Activity", container.NewScroll(activityBox)),
		container.NewTabItem("Task", container.NewScroll(v.taskPanel)),
	)

	mid := container.NewBorder(nil, composer, nil, nil, center)
	split := container.NewHSplit(goalsPane, mid)
	split.Offset = 0.26

	root := container.NewBorder(
		container.NewVBox(PageHeader("Agent", "Goals, tasks and net-search runs"), v.maint, v.download),
		v.errLabel,
		nil, nil, split,
	)

	ap.shell.register(
		func() { v.onStream() },
		func() { v.onSettled() },
		nil,
	)
	v.refreshGoals()
	v.refreshBackend()
	v.attachIfActive()
	return root
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

func (v *agentView) refreshGoals() {
	go func() {
		got, err := v.ap.Client().Goals(v.ap.Ctx())
		if err != nil {
			uiDo(func() { v.showErr("goals", err) })
			return
		}
		uiDo(func() {
			v.goals = got
			v.goalList.Refresh()
			v.errLabel.Hide()
		})
	}()
}

func (v *agentView) renderTask(i widget.ListItemID) {
	if i < 0 || i >= len(v.goals) {
		return
	}
	g := v.goals[i]
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
	v.taskPanel.SetText(b.String())
}

func (v *agentView) refreshBackend() {
	go func() {
		c := v.ap.Client()
		ctx := v.ap.Ctx()
		if raw, err := c.Maintenance(ctx); err == nil {
			var m struct {
				Phase  string `json:"phase"`
				Detail string `json:"detail"`
				Reason string `json:"reason"`
			}
			if json.Unmarshal(raw, &m) == nil && m.Phase != "" && m.Phase != "CHECKING" {
				text := "Maintenance: " + m.Phase
				if m.Detail != "" {
					text += " — " + m.Detail
				}
				if m.Reason != "" {
					text += " (" + m.Reason + ")"
				}
				uiDo(func() { v.maint.SetText(text); v.maint.Show() })
			}
		}
		if e, err := c.Engine(ctx); err == nil {
			if len(e.Download) > 0 && string(e.Download) != "null" {
				var d struct {
					Active     bool   `json:"active"`
					Phase      string `json:"phase"`
					BytesDone  int64  `json:"bytesDone"`
					BytesTotal int64  `json:"bytesTotal"`
				}
				if json.Unmarshal(e.Download, &d) == nil && (d.Active || d.Phase != "") {
					text := "Download: " + d.Phase
					if d.BytesTotal > 0 {
						text += fmt.Sprintf(" (%d / %d bytes)", d.BytesDone, d.BytesTotal)
					} else {
						text += " (size unknown — progress is indeterminate)"
					}
					uiDo(func() { v.download.SetText(text); v.download.Show(); v.cancelDl.Show() })
				}
			}
		}
	}()
}

func (v *agentView) createGoal() {
	text := strings.TrimSpace(v.newGoal.Text)
	if text == "" {
		return
	}
	ap := v.ap
	go func() {
		sessID := ap.rt.ActiveID()
		_, err := ap.Client().CreateGoal(ap.Ctx(), text, sessID, true)
		if err != nil {
			uiDo(func() { v.showErr("create goal", err) })
			return
		}
		uiDo(func() { v.newGoal.SetText(""); v.errLabel.Hide() })
		v.refreshGoals()
	}()
}

func (v *agentView) attachIfActive() {
	if id := v.ap.rt.ActiveID(); id != "" {
		go func() { _ = v.ap.rt.AttachActivity(v.ap.Ctx(), id) }()
	}
}

func (v *agentView) send() {
	text := strings.TrimSpace(v.input.Text)
	if text == "" || v.ap.rt.Busy() {
		return
	}
	ap := v.ap
	net := v.netSearch.Checked
	v.input.SetText("")
	v.sendBtn.Disable()
	go func() {
		if _, err := ap.rt.Send(ap.Ctx(), text, net, "mid"); err != nil {
			uiDo(func() {
				v.showErr("send", err)
				v.sendBtn.Enable()
			})
		}
	}()
}

func (v *agentView) stop() {
	go func() {
		if err := v.ap.rt.Stop(v.ap.Ctx()); err != nil {
			uiDo(func() { v.showErr("stop", err) })
		}
	}()
}

func (v *agentView) onStream() {
	acc := v.ap.rt.Stream()
	if acc.Response != "" {
		v.live.SetText(acc.Response)
	} else if acc.Reasoning != "" {
		v.live.SetText("thinking: " + acc.Reasoning)
	}
	var acts []string
	acts = append(acts, acc.Activities...)
	if len(acts) > 0 {
		v.activity.SetText(strings.Join(acts, "\n"))
	}
	if v.ap.rt.Busy() {
		v.stopBtn.Enable()
		v.sendBtn.Disable()
	} else {
		v.sendBtn.Enable()
		v.stopBtn.Disable()
	}
}

func (v *agentView) onSettled() {
	if s, ok := v.ap.rt.ActiveSession(); ok {
		v.messages = s.Messages
		v.transcript.Refresh()
	}
	v.live.SetText("")
	v.sendBtn.Enable()
	v.stopBtn.Disable()
	v.refreshGoals() // plan steps may have advanced
}

func (v *agentView) showErr(what string, err error) {
	v.errLabel.SetText(what + " failed: " + err.Error())
	v.errLabel.Show()
}
