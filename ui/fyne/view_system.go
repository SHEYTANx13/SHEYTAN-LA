//go:build !headless

// view_system.go — the System Centre: read-only evidence from existing API
// facts (hardware, engine, governor, maintenance). Distinguishes detected
// vs available vs selected vs executed vs verified; never upgrades a
// detection into a capability claim. Refresh-all is the one mutator.
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

type systemView struct {
	ap       *App
	hardware *widget.Label
	engine   *widget.Label
	governor *widget.Label
	maint    *widget.Label
	refreshB *widget.Button
	errLabel *widget.Label
}

func buildSystemView(ap *App) fyne.CanvasObject {
	v := &systemView{ap: ap}
	v.hardware = wrappedLabel("Loading hardware facts…")
	v.engine = wrappedLabel("Loading engine state…")
	v.governor = wrappedLabel("Loading policy envelope…")
	v.maint = wrappedLabel("Loading maintenance status…")
	v.refreshB = widget.NewButtonWithIcon("Refresh all", theme.ViewRefreshIcon(), func() { v.refreshAll() })
	v.errLabel = widget.NewLabel("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Wrapping = fyne.TextWrapWord
	v.errLabel.Hide()

	root := container.NewBorder(
		PageHeader("System", "Hardware, engine and runtime evidence", v.refreshB),
		v.errLabel, nil, nil,
		container.NewScroll(container.NewVBox(
			widget.NewCard("Hardware", "Detected by the core's probe — not inferred", v.hardware),
			widget.NewCard("Engine", "Selected, executed and verified states only", v.engine),
			widget.NewCard("Governor", "Policy envelope (read-only)", v.governor),
			widget.NewCard("Maintenance", "Explicit phases; never a generic spinner", v.maint),
		)),
	)
	v.refreshAll()
	return root
}

func (v *systemView) refreshAll() {
	c := v.ap.Client()
	ctx := v.ap.Ctx()
	go func() {
		// Hardware.
		if raw, err := c.SysInfo(ctx); err == nil {
			var si struct {
				OS       string          `json:"os"`
				Arch     string          `json:"arch"`
				Hostname string          `json:"hostname"`
				CPU      string          `json:"cpu"`
				RAM      json.RawMessage `json:"ram"`
				GPUs     json.RawMessage `json:"gpus"`
				NPU      json.RawMessage `json:"npu"`
				Disk     json.RawMessage `json:"disk"`
			}
			if json.Unmarshal(raw, &si) == nil {
				var b strings.Builder
				b.WriteString(infoLine("OS", si.OS) + "\n")
				b.WriteString(infoLine("Architecture", si.Arch) + "\n")
				b.WriteString(infoLine("Host", si.Hostname) + "\n")
				b.WriteString(infoLine("CPU", si.CPU) + "\n")
				for _, kv := range []struct {
					k string
					v json.RawMessage
				}{
					{"RAM", si.RAM}, {"GPUs", si.GPUs}, {"NPU", si.NPU}, {"Disk", si.Disk},
				} {
					if len(kv.v) > 0 && string(kv.v) != "null" {
						b.WriteString(kv.k + ": " + jsonPretty(kv.v, 900) + "\n")
					}
				}
				b.WriteString("\n(Only detected hardware is listed. Detection is NOT a performance claim.)")
				uiDo(func() { v.hardware.SetText(b.String()) })
			}
		} else {
			uiDo(func() { v.showErr("hardware", err) })
		}

		// Engine.
		if e, err := c.Engine(ctx); err == nil {
			{
				var b strings.Builder
				b.WriteString(infoLine("State", e.State) + "\n")
				b.WriteString(infoLine("Backend", e.Backend) + "\n")
				b.WriteString(infoLine("Model", e.Model) + "\n")
				if e.Verified {
					b.WriteString(infoLine("Verified model", e.VerifiedModel) + "\n")
				} else {
					b.WriteString("Verified: no — the engine has not proven this model yet\n")
				}
				if e.SelectionRequired {
					b.WriteString("\nModel selection required — open Models and run the real selection path.\n")
				}
				if e.Detail != "" {
					b.WriteString(infoLine("Detail", e.Detail) + "\n")
				}
				uiDo(func() { v.engine.SetText(b.String()) })
			}
		} else {
			uiDo(func() { v.showErr("engine", err) })
		}

		// Governor + maintenance as bounded evidence cards.
		if raw, err := c.Governor(ctx); err == nil {
			uiDo(func() { v.governor.SetText(jsonPretty(raw, 3000)) })
		}
		if raw, err := c.Maintenance(ctx); err == nil {
			var m struct {
				Phase  string `json:"phase"`
				Reason string `json:"reason"`
			}
			phase := "unknown"
			if json.Unmarshal(raw, &m) == nil && m.Phase != "" {
				phase = m.Phase
				if m.Reason != "" {
					phase += " (" + m.Reason + ")"
				}
			}
			txt := fmt.Sprintf("Maintenance phase: %s", phase)
			uiDo(func() { v.maint.SetText(txt) })
		}
	}()
}

func (v *systemView) showErr(what string, err error) {
	v.errLabel.SetText(what + " refresh failed: " + err.Error())
	v.errLabel.Show()
}
