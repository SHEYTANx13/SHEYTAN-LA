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

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/presentation"
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
	input      *composerEntry
	slot       *primaryActionSlot
	netSearch  *widget.Check
	errLabel   *widget.Label
	emptyBox   *fyne.Container
	newGoal    *widget.Entry
	addGoal    *widget.Button
	wsBtn      *widget.Button
	artCard    *artifactCard
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
			// Conversation hierarchy (Phase 1): role line secondary (muted).
			role := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			role.Importance = widget.LowImportance
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
	// The live surface is PROVISIONAL (settlement rule); its Stop lives in
	// the composer's single primary slot.
	liveBox := container.NewVBox(widget.NewLabelWithStyle("Live — provisional, settled below on completion", fyne.TextAlignLeading, fyne.TextStyle{Bold: true, Italic: true}), v.live)
	var emptyBox *fyne.Container

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

	v.input = newComposerEntry("Describe the agent task… (Enter to send, Shift+Enter for newline)", v.send)
	// Capability-priming empty state (agent mode): samples fill the draft
	// only; zero-session Send stays the composer's contract.
	emptyBox = container.NewVBox(emptyStateSurface("agent", v.input, ap.Window()))
	emptyBox.Hide()
	v.emptyBox = emptyBox
	v.slot = newPrimaryActionSlot(v.send, v.stop)
	v.netSearch = widget.NewCheck("Net search", nil)
	// Phase 2: the Agent surface gets the same contextual artifact
	// affordance as Chat — one truthful Workspace entry (actionable only
	// once a settled artifact exists) plus the native settled-artifact
	// card. Chat and Agent stay separate conversation spaces; the block
	// Source field records which surface produced it.
	v.wsBtn = widget.NewButtonWithIcon("Workspace", theme.ComputerIcon(), v.openWorkspace)
	syncWorkspaceAffordance(v.wsBtn, ap)
	v.artCard = newArtifactCard(ap)
	if m, ok := ap.ws.Latest(); ok {
		if b, found := ap.ws.WorkspaceBlock(m.ID); found {
			v.artCard.setBlock(m, string(b.Payload))
		}
	}

	v.newGoal = widget.NewEntry()
	v.newGoal.SetPlaceHolder("New goal…")
	v.addGoal = widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), func() { v.createGoal() })

	v.errLabel = widget.NewLabel("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Wrapping = fyne.TextWrapWord
	v.errLabel.Hide()

	composer := container.NewVBox(
		container.NewHBox(v.netSearch, layoutSpacer(), v.wsBtn, v.slot.canvas()),
		v.input,
	)

	goalsPane := container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Goals", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), v.newGoal, v.addGoal),
		nil, nil, nil, container.NewScroll(v.goalList),
	)

	center := container.NewAppTabs(
		container.NewTabItem("Transcript", container.NewScroll(container.NewVBox(v.emptyBox, v.transcript, liveBox, v.artCard.canvas()))),
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
	// Phase 2 test/wiring seam: the shell-owned instance of this view.
	ap.agent = v
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
	v.slot.setBusy(true)
	// Focus return after send (docs/ui-contract.md §2 #2) — never stealing
	// focus from a dialog or another deliberate target.
	focusComposerIfSafe(ap.Window(), v.input)
	go func() {
		if _, err := ap.rt.Send(ap.Ctx(), text, net, "mid"); err != nil {
			uiDo(func() {
				v.showErr("send", err)
				v.slot.setBusy(false)
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
	v.slot.setBusy(v.ap.rt.Busy())
	v.updateEmptyState()
}

func (v *agentView) onSettled() {
	if s, ok := v.ap.rt.ActiveSession(); ok {
		v.messages = s.Messages
		v.transcript.Refresh()
		v.promoteSettledAssistant(s)
	}
	v.live.SetText("")
	v.slot.setBusy(false)
	v.updateEmptyState()
	v.refreshGoals() // plan steps may have advanced
}

// promoteSettledAssistant is the Phase 2 settle-path promotion for the
// Agent surface: the settled AGENT response is issued as a workspace block
// (additive; refusals leave the native rendering untouched) and a
// successful registration is promoted onto the native artifact card — the
// settled block, never the provisional live bubble.
func (v *agentView) promoteSettledAssistant(s Session) {
	if len(s.Messages) == 0 {
		return
	}
	last := s.Messages[len(s.Messages)-1]
	if last.Role != "assistant" || strings.TrimSpace(last.Content) == "" {
		return
	}
	if b, err := v.ap.Workspace().RegisterAgentResponse(s.ID, last.Content); err == nil && b.ID != "" {
		meta := presentation.MetaOf(b)
		uiDo(func() {
			v.artCard.setBlock(meta, last.Content)
			syncWorkspaceAffordance(v.wsBtn, v.ap)
		})
	}
}

// openWorkspace starts (if needed) the loopback workspace server and hands
// the per-launch URL to the default browser (mirror of the chat action).
func (v *agentView) openWorkspace() {
	if err := v.ap.openWorkspace(); err != nil {
		v.showErr("open workspace", err)
	}
}

// updateEmptyState shows the capability-priming surface only while the
// transcript is empty and no run is live (zero-session Send stays intact).
func (v *agentView) updateEmptyState() {
	if len(v.messages) == 0 && !v.ap.rt.Busy() {
		v.emptyBox.Show()
		return
	}
	v.emptyBox.Hide()
}

func (v *agentView) showErr(what string, err error) {
	v.errLabel.SetText(what + " failed: " + err.Error())
	v.errLabel.Show()
}
