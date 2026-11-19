//go:build !headless

// view_system.go — the System Centre (area 9, Diagnostics): read-only
// evidence from existing API facts (hardware, engine, governor,
// maintenance, accelerator). Distinguishes detected vs available vs
// selected vs executed vs verified; never upgrades a detection into a
// capability claim. Refresh-all is the one mutator.
package goguiui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

type systemViewState struct {
	st *shellState

	mu       sync.Mutex
	hardware string
	engine   string
	governor string
	maint    string
	accel    string
	err      error
	loaded   bool
}

func systemState(s *shellState) *systemViewState {
	return s.view("system", func() any { return &systemViewState{st: s} }).(*systemViewState)
}

func buildSystemView(s *shellState) gui.View {
	v := systemState(s)
	v.ensureLoaded()

	refresh := gui.Button(gui.ButtonCfg{
		ID:      absID("system-refresh"),
		Label:   "Refresh all",
		OnClick: func(gui.EventCtx) { v.refreshAll() },
	})

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-system"),
		Sizing: gui.FillFill,
		Content: []gui.View{
			PageHeader("System", "Hardware, engine and runtime evidence", refresh),
			v.errView(),
			gui.Column(gui.ContainerCfg{
				ID:         absID("system-scroll"),
				Sizing:     gui.FillFill,
				Scrollable: true,
				Spacing:    gui.SpacingPx(6),
				Content: []gui.View{
					v.cardFor("Hardware", "Detected by the core's probe — not inferred", &v.hardware, "hardware"),
					v.cardFor("Engine", "Selected, executed and verified states only", &v.engine, "engine"),
					v.cardFor("Governor", "Policy envelope (read-only)", &v.governor, "governor"),
					v.cardFor("Maintenance", "Explicit phases; never a generic spinner", &v.maint, "maintenance"),
					v.cardFor("Accelerator", "Requested ≠ available ≠ selected ≠ executed ≠ verified", &v.accel, "accelerator"),
				},
			}),
		},
	})
}

func (v *systemViewState) ensureLoaded() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()
	v.refreshAll()
}

// cardFor renders one evidence card from its state field pointer.
func (v *systemViewState) cardFor(title, caption string, field *string, id string) gui.View {
	v.mu.Lock()
	val := *field
	v.mu.Unlock()
	if val == "" {
		return card(title, caption, LoadingSurface(title))
	}
	return card(title, caption, codeText(val))
}

func (v *systemViewState) errView() gui.View {
	v.mu.Lock()
	err := v.err
	v.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("system", err, nil)
}

func (v *systemViewState) refreshAll() {
	c := v.st.ap.Client()
	ctx := v.st.ap.Ctx()
	go func() {
		// Hardware.
		var hw, eng, gov, maint, accel string
		var hwErr, engErr error
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
					x json.RawMessage
				}{
					{"RAM", si.RAM}, {"GPUs", si.GPUs}, {"NPU", si.NPU}, {"Disk", si.Disk},
				} {
					if len(kv.x) > 0 && string(kv.x) != "null" {
						b.WriteString(kv.k + ": " + jsonPretty(kv.x, 900) + "\n")
					}
				}
				b.WriteString("\n(Only detected hardware is listed. Detection is NOT a performance claim.)")
				hw = b.String()
			}
		} else {
			hwErr = err
		}

		// Engine.
		if e, err := c.Engine(ctx); err == nil {
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
			eng = b.String()
		} else {
			engErr = err
		}

		// Governor + maintenance + accelerator as bounded evidence.
		if raw, err := c.Governor(ctx); err == nil {
			gov = jsonPretty(raw, 3000)
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
			maint = fmt.Sprintf("Maintenance phase: %s", phase)
		}
		if a, err := c.Accelerator(ctx); err == nil && a != nil {
			var b strings.Builder
			b.WriteString(infoLine("Requested", a.Requested) + "\n")
			b.WriteString(infoLine("Selected", a.Selected) + "\n")
			b.WriteString(infoLine("Backend", a.Backend) + "\n")
			b.WriteString(infoLine("Device", a.Device) + "\n")
			b.WriteString("Available: " + strings.Join(a.Available, ", ") + "\n")
			if a.Reason != "" {
				b.WriteString(infoLine("Reason", a.Reason) + "\n")
			}
			accel = b.String()
		}

		uiDo(func() {
			v.mu.Lock()
			v.hardware = hw
			v.engine = eng
			v.governor = gov
			v.maint = maint
			v.accel = accel
			v.err = nil
			if hwErr != nil {
				v.err = fmt.Errorf("hardware: %w", hwErr)
			} else if engErr != nil {
				v.err = fmt.Errorf("engine: %w", engErr)
			}
			v.mu.Unlock()
		})
	}()
}
