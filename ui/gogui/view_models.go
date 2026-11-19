//go:build !headless

// view_models.go — the Models panel (area 2): the REAL selection path,
// import, fit evidence, engine update surface and the Model Studio
// binding card. Ported from ui/fyne/panels.go buildModelsPanel with the
// same contracts:
//
//   - a row selection only HIGHLIGHTS — the explicit "Use this model"
//     action is the ONE selection action;
//   - the selection flow is non-destructive and single-flight;
//   - engine updates: check is read-only, install is user-controlled,
//     driven only through the core's engine-update API;
//   - Model Studio: per-AI-System binding through the EXISTING systems
//     store PUT; Inherit is the explicit first option; one Apply action.
package goguiui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-gui-org/go-gui/gui"
)

// modelsPanelState is the Models panel's state.
type modelsPanelState struct {
	st *shellState

	mu        sync.Mutex
	models    ModelsPayload
	current   SelectionState
	highlight int // highlighted row (-1 = none)
	task      string
	importIn  string
	err       error
	loaded    bool
	busy      bool

	// engine updates
	updLine   string
	updTag    string
	updArmed  bool // update-available resolved → install armed
	updCancel bool // an install download runs → cancel visible
	updBusy   bool // single-flight check/install

	// model studio
	systems       []AISystem
	studioSel     int // index into systems (-1 = none)
	studioModel   string
	studioProfile string
	studioLine    string
	studioBusy    bool
}

func modelsPanelStateFor(s *shellState) *modelsPanelState {
	return s.view("models", func() any {
		return &modelsPanelState{st: s, highlight: -1, studioSel: -1,
			studioLine: "Inherit is the default state: a system without its own model serves the globally selected model."}
	}).(*modelsPanelState)
}

// buildModelsPanel is the "models" utility surface (rail Utilities +
// dock panel).
func buildModelsPanel(s *shellState) gui.View {
	p := modelsPanelStateFor(s)
	p.ensureLoaded()

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-models"),
		Sizing: gui.FillFill,
		Content: []gui.View{
			PageHeader("Models", "Discovery and the REAL selection state machine"),
			p.errView(),
			gui.Column(gui.ContainerCfg{
				ID:         absID("models-scroll"),
				Sizing:     gui.FillFill,
				Scrollable: true,
				Spacing:    gui.SpacingPx(6),
				Content: []gui.View{
					p.engineCard(),
					p.engineUpdatesCard(),
					p.studioCard(),
					p.importCard(),
					p.selectionCard(),
					p.listCard(),
				},
			}),
		},
	})
}

func (p *modelsPanelState) ensureLoaded() {
	p.mu.Lock()
	if p.loaded {
		p.mu.Unlock()
		return
	}
	p.loaded = true
	p.mu.Unlock()
	p.refresh()
	p.refreshStudioSystems()
}

func (p *modelsPanelState) errView() gui.View {
	p.mu.Lock()
	err := p.err
	p.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("models", err, nil)
}

func (p *modelsPanelState) refresh() {
	ap := p.st.ap
	go func() {
		models, err := ap.Client().Models(ap.Ctx())
		if err != nil {
			uiDo(func() {
				p.mu.Lock()
				p.err = fmt.Errorf("models: %w", err)
				p.mu.Unlock()
			})
			return
		}
		sel, _ := ap.Client().Selection(ap.Ctx())
		uiDo(func() {
			p.mu.Lock()
			p.models = models
			p.current = sel
			p.err = nil
			p.mu.Unlock()
		})
	}()
}

func (p *modelsPanelState) refreshStudioSystems() {
	ap := p.st.ap
	go func() {
		systems, _, err := ap.Client().Systems(ap.Ctx())
		uiDo(func() {
			p.mu.Lock()
			if err != nil {
				p.studioLine = "AI Systems store unavailable: " + err.Error()
			} else {
				p.systems = systems
			}
			p.mu.Unlock()
		})
	}()
}

// engineCard is the engine & backend status (core-owned facts only).
func (p *modelsPanelState) engineCard() gui.View {
	e, err := p.st.ap.Client().Engine(p.st.ap.Ctx())
	if err != nil {
		return card("Engine & backend", "Requested ≠ available ≠ selected ≠ executed ≠ verified — strongest PROVEN state only", wrappedText("Engine state unavailable: "+err.Error()))
	}
	var b strings.Builder
	b.WriteString(infoLine("State", e.State) + "\n")
	b.WriteString(infoLine("Backend", e.Backend) + "\n")
	b.WriteString(infoLine("Model", e.Model) + "\n")
	if e.Verified {
		b.WriteString(infoLine("Verified model", e.VerifiedModel) + "\n")
	}
	return card("Engine & backend", "Requested ≠ available ≠ selected ≠ executed ≠ verified — strongest PROVEN state only", codeText(b.String()))
}

// engineUpdatesCard is the check/install/cancel surface.
func (p *modelsPanelState) engineUpdatesCard() gui.View {
	p.mu.Lock()
	line := p.updLine
	armed := p.updArmed
	cancelVisible := p.updCancel
	busy := p.updBusy
	p.mu.Unlock()
	if line == "" {
		line = "Engine updates: not checked yet — run Check now."
	}
	actions := []gui.View{
		gui.Button(gui.ButtonCfg{
			ID: absID("models-upd-check"), Label: "Check now",
			Disabled: busy,
			OnClick:  func(gui.EventCtx) { p.checkEngineUpdate() },
		}),
		gui.Button(gui.ButtonCfg{
			ID: absID("models-upd-install"), Label: "Install update", Variant: gui.ButtonPrimary,
			Disabled: !armed || busy,
			OnClick:  func(gui.EventCtx) { p.installEngineUpdate() },
		}),
	}
	if cancelVisible {
		actions = append(actions, gui.Button(gui.ButtonCfg{
			ID: absID("models-upd-cancel"), Label: "Cancel",
			OnClick: func(gui.EventCtx) {
				go func() {
					if err := p.st.ap.Client().CancelEngineDownload(p.st.ap.Ctx()); err != nil {
						p.fail("cancel engine update download", err)
					}
				}()
			},
		}))
	}
	actions = append(actions, codeText(line))
	return card("Engine updates", "Official ggml-org/llama.cpp releases · check is read-only · install is user-controlled. Install restarts the local engine after the package verifies; Cancel stops a running download.", actions...)
}

// studioCard is the Model Studio binding card (area 2 requirement).
func (p *modelsPanelState) studioCard() gui.View {
	p.mu.Lock()
	systems := append([]AISystem{}, p.systems...)
	sel := p.studioSel
	model := p.studioModel
	profile := p.studioProfile
	line := p.studioLine
	busy := p.studioBusy
	p.mu.Unlock()

	sysNames := make([]string, 0, len(systems))
	for _, s := range systems {
		sysNames = append(sysNames, s.Name)
	}
	selected := []string{}
	if sel >= 0 && sel < len(systems) {
		selected = []string{systems[sel].Name}
	}

	return card("Model Studio", "Per-AI-System model binding · one Apply action · the core validates and freezes it per run",
		gui.Select(gui.SelectCfg{
			ID: absID("models-studio-sys"), Items: sysNames, Placeholder: "choose an AI System…",
			Selected: selected,
			MaxWidth: 300,
			OnSelect: func(vals []string, _ gui.EventCtx) {
				if len(vals) == 0 {
					return
				}
				p.mu.Lock()
				for i, s := range p.systems {
					if s.Name == vals[0] {
						p.studioSel = i
						break
					}
				}
				p.mu.Unlock()
			},
		}),
		gui.Select(gui.SelectCfg{
			ID: absID("models-studio-model"), Items: []string{"(inherit)", "(globally selected)"}, Placeholder: "model assignment…",
			Selected: func() []string {
				if model == "" {
					return nil
				}
				return []string{model}
			}(),
			MaxWidth: 300,
			OnSelect: func(vals []string, _ gui.EventCtx) {
				if len(vals) > 0 {
					p.mu.Lock()
					p.studioModel = vals[0]
					p.mu.Unlock()
				}
			},
		}),
		gui.Select(gui.SelectCfg{
			ID: absID("models-studio-profile"), Items: []string{"(inherit)", "auto", "manual"}, Placeholder: "runtime profile…",
			Selected: func() []string {
				if profile == "" {
					return nil
				}
				return []string{profile}
			}(),
			MaxWidth: 300,
			OnSelect: func(vals []string, _ gui.EventCtx) {
				if len(vals) > 0 {
					p.mu.Lock()
					p.studioProfile = vals[0]
					p.mu.Unlock()
				}
			},
		}),
		codeText(line),
		gui.Button(gui.ButtonCfg{
			ID: absID("models-studio-apply"), Label: "Apply binding", Variant: gui.ButtonPrimary,
			Disabled: busy || sel < 0,
			OnClick:  func(gui.EventCtx) { p.applyStudio() },
		}),
	)
}

func (p *modelsPanelState) importCard() gui.View {
	p.mu.Lock()
	importIn := p.importIn
	task := p.task
	p.mu.Unlock()
	return card("Import a model", "Paste an absolute path to a .gguf file on disk, optionally pick a task fit, then Import.",
		gui.Input(gui.InputCfg{
			ID: absID("models-import-path"), Text: importIn,
			Placeholder: "Absolute path to a .gguf model on disk",
			OnEnter:     func(gui.EventCtx) { p.importModel() },
			OnTextChanged: func(text string, _ gui.EventCtx) {
				p.mu.Lock()
				p.importIn = text
				p.mu.Unlock()
			},
		}),
		gui.Row(gui.ContainerCfg{
			Content: []gui.View{
				gui.Button(gui.ButtonCfg{ID: absID("models-import"), Label: "Import", Variant: gui.ButtonPrimary, OnClick: func(gui.EventCtx) { p.importModel() }}),
				gui.Button(gui.ButtonCfg{
					ID: absID("models-open-folder"), Label: "Open folder",
					OnClick: func(gui.EventCtx) {
						go func() {
							if err := p.st.ap.Client().OpenModelsFolder(p.st.ap.Ctx()); err != nil {
								p.fail("open models folder", err)
							}
						}()
					},
				}),
			},
		}),
		gui.Select(gui.SelectCfg{
			ID: absID("models-task"), Items: []string{"chat", "coding", "research", "vision", "agent", "low-power", "maximum"},
			Placeholder: "task fit (optional)",
			Selected: func() []string {
				if task == "" {
					return nil
				}
				return []string{task}
			}(),
			MaxWidth: 260,
			OnSelect: func(vals []string, _ gui.EventCtx) {
				if len(vals) > 0 {
					p.mu.Lock()
					p.task = vals[0]
					p.mu.Unlock()
				}
			},
		}),
	)
}

// selectionCard renders the authoritative selection + the ONE selection
// action for the highlighted row.
func (p *modelsPanelState) selectionCard() gui.View {
	p.mu.Lock()
	cur := p.current
	hl := p.highlight
	models := p.models
	p.mu.Unlock()

	rows := []gui.View{}
	if cur.Model != "" {
		rows = append(rows, wrappedText("Selected: "+cur.Model+" · phase "+cur.Phase))
	} else {
		rows = append(rows, wrappedText("No model selected yet."))
	}
	if hl >= 0 && hl < len(models.Local) {
		m := models.Local[hl]
		rows = append(rows, wrappedText(fmt.Sprintf("Highlighted: %s — %s, %d ctx, %.1f GB",
			m.Name, m.Quantization, m.ContextLength, float64(m.SizeBytes)/1e9)))
		rows = append(rows, gui.Button(gui.ButtonCfg{
			ID: absID("models-use"), Label: "Use this model", Variant: gui.ButtonPrimary,
			OnClick: func(gui.EventCtx) { p.useModel() },
		}))
	} else {
		rows = append(rows, wrappedText("Select a row in the model list to arm the selection action."))
	}
	return card("Selection", "Highlight arms the action; the click starts the real selection flow.", rows...)
}

func (p *modelsPanelState) listCard() gui.View {
	p.mu.Lock()
	models := p.models
	hl := p.highlight
	p.mu.Unlock()

	items := make([]gui.ListBoxOption, 0, len(models.Local))
	for i, m := range models.Local {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		if i == hl {
			name = "▸ " + name
		}
		if m.Serving {
			name += "  (serving)"
		}
		items = append(items, gui.ListBoxOption{ID: fmt.Sprintf("%d", i), Name: name, Value: fmt.Sprintf("%d", i)})
	}
	if len(items) == 0 {
		return card("Model list", "", EmptySurface("◇", "No local models", "Import a .gguf file or download one from the Downloads surface."))
	}
	return card("Model list", "",
		gui.ListBox(gui.ListBoxCfg{
			ID:     absID("models-list"),
			Data:   items,
			Height: 260,
			OnSelect: func(sel []string, _ gui.EventCtx) {
				if len(sel) == 0 {
					return
				}
				var idx int
				if _, err := fmt.Sscanf(sel[0], "%d", &idx); err != nil {
					return
				}
				uiDo(func() {
					p.mu.Lock()
					p.highlight = idx
					p.mu.Unlock()
				})
			},
		}),
	)
}

// --- actions ---

func (p *modelsPanelState) fail(what string, err error) {
	uiDo(func() {
		p.mu.Lock()
		p.err = fmt.Errorf("%s: %w", what, err)
		p.mu.Unlock()
	})
}

func (p *modelsPanelState) useModel() {
	p.mu.Lock()
	hl := p.highlight
	models := p.models
	busy := p.busy
	p.mu.Unlock()
	if hl < 0 || hl >= len(models.Local) || busy {
		return
	}
	m := models.Local[hl]
	ap := p.st.ap
	uiDo(func() {
		p.mu.Lock()
		p.busy = true
		p.mu.Unlock()
	})
	go func() {
		defer uiDo(func() {
			p.mu.Lock()
			p.busy = false
			p.mu.Unlock()
		})
		task := p.task
		if _, err := ap.Client().SelectModel(ap.Ctx(), m.ID, task); err != nil {
			p.fail("select model", err)
			return
		}
		p.refresh()
	}()
}

func (p *modelsPanelState) importModel() {
	p.mu.Lock()
	path := strings.TrimSpace(p.importIn)
	p.mu.Unlock()
	if path == "" {
		return
	}
	ap := p.st.ap
	go func() {
		if _, err := ap.Client().ImportModel(ap.Ctx(), path); err != nil {
			p.fail("import model", err)
			return
		}
		p.refresh()
	}()
}

// engineUpdatePollInterval is the bounded cadence of the install-progress
// poll (tests tighten it for determinism).
var engineUpdatePollInterval = pollInterval(500)

// checkEngineUpdate runs the READ-ONLY official-release check.
func (p *modelsPanelState) checkEngineUpdate() {
	p.mu.Lock()
	if p.updBusy {
		p.mu.Unlock()
		return
	}
	p.updBusy = true
	p.mu.Unlock()
	ap := p.st.ap
	go func() {
		defer uiDo(func() {
			p.mu.Lock()
			p.updBusy = false
			p.mu.Unlock()
		})
		st, err := ap.Client().EngineUpdateCheckNow(ap.Ctx())
		p.renderEngineUpdate(st, err)
	}()
}

// installEngineUpdate starts the USER-TRIGGERED engine update, then polls
// the bounded check surface while the transaction runs — real phases only,
// no fabricated progress.
func (p *modelsPanelState) installEngineUpdate() {
	p.mu.Lock()
	if p.updBusy || !p.updArmed {
		p.mu.Unlock()
		return
	}
	tag := p.updTag
	p.updBusy = true
	p.mu.Unlock()
	ap := p.st.ap
	go func() {
		defer uiDo(func() {
			p.mu.Lock()
			p.updBusy = false
			p.mu.Unlock()
		})
		if _, err := ap.Client().EngineUpdateInstall(ap.Ctx(), tag); err != nil {
			p.fail("engine update", err)
			return
		}
		// Bounded poll while the transaction runs (≤ 2 min at the
		// production cadence; tests tighten the interval).
		for i := 0; i < 240; i++ {
			select {
			case <-ap.Ctx().Done():
				return
			case <-engineUpdatePollInterval.c():
			}
			st, err := ap.Client().EngineUpdateCheckNow(ap.Ctx())
			p.renderEngineUpdate(st, err)
			if err != nil || st == nil || st.Status != "installing" {
				return
			}
		}
	}()
}

// renderEngineUpdate renders the truthful check/install state (mutates
// state through uiDo — callable from any goroutine).
func (p *modelsPanelState) renderEngineUpdate(st *EngineUpdateCheck, err error) {
	uiDo(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if err != nil {
			p.updLine = "Engine updates: check failed — " + err.Error()
			p.updArmed = false
			p.updCancel = false
			return
		}
		var b strings.Builder
		b.WriteString("Engine: " + st.InstalledTag)
		if st.InstalledVariant != "" {
			b.WriteString(" (" + st.InstalledVariant + ")")
		}
		b.WriteString(" · " + st.Platform + " · source " + st.Source + "\n")
		if st.LatestTag != "" && st.LatestTag != st.InstalledTag {
			b.WriteString("Latest official release: " + st.LatestTag)
			if st.AssetName != "" {
				b.WriteString(" · exact asset " + st.AssetName)
			}
			b.WriteString("\n")
		}
		b.WriteString("Status: " + st.Status)
		if st.Message != "" {
			b.WriteString(" — " + st.Message)
		}
		if st.Progress != nil && st.Progress.Total > 0 {
			b.WriteString(fmt.Sprintf("\nProgress: %s (%s / %s)", st.Progress.Phase,
				humanBytes(st.Progress.Bytes), humanBytes(st.Progress.Total)))
		}
		p.updLine = b.String()
		if st.Status == "update-available" {
			p.updTag = st.LatestTag
			p.updArmed = true
		} else {
			p.updArmed = false
		}
		p.updCancel = st.Status == "installing" && st.Progress != nil
	})
}

func (p *modelsPanelState) applyStudio() {
	p.mu.Lock()
	sel := p.studioSel
	systems := p.systems
	busy := p.studioBusy
	model := p.studioModel
	profile := p.studioProfile
	p.mu.Unlock()
	if sel < 0 || sel >= len(systems) || busy {
		return
	}
	sys := systems[sel]
	if model == "(inherit)" || model == "(globally selected)" {
		sys.Model = ""
	} else if model != "" {
		sys.Model = model
	}
	if profile == "(inherit)" {
		sys.RuntimeProfile = ""
	} else if profile != "" {
		sys.RuntimeProfile = profile
	}
	ap := p.st.ap
	uiDo(func() {
		p.mu.Lock()
		p.studioBusy = true
		p.mu.Unlock()
	})
	go func() {
		defer uiDo(func() {
			p.mu.Lock()
			p.studioBusy = false
			p.mu.Unlock()
		})
		updated, err := ap.Client().UpdateSystem(ap.Ctx(), sys)
		uiDo(func() {
			p.mu.Lock()
			if err != nil {
				p.studioLine = "Apply failed: " + err.Error()
			} else {
				p.studioLine = fmt.Sprintf("Bound: %s → model %q, profile %q (revision %d).",
					updated.Name, orDash(updated.Model), orDash(updated.RuntimeProfile), updated.Revision)
				for i := range p.systems {
					if p.systems[i].SystemID == updated.SystemID {
						p.systems[i] = updated
					}
				}
			}
			p.mu.Unlock()
		})
	}()
}

// pollInterval is a test-seam duration: tests override
// engineUpdatePollInterval for determinism without touching the
// production cadence. c() renders it as a receive-ready channel.
type pollInterval time.Duration

func (d pollInterval) c() <-chan time.Time { return time.After(time.Duration(d)) }

// humanBytes formats a byte count (the same buckets the Fyne panel used).
func humanBytes(n int64) string {
	const kb, mb, gb = 1024.0, 1024.0 * 1024.0, 1024.0 * 1024.0 * 1024.0
	switch {
	case n >= int64(gb):
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	case n >= int64(mb):
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	case n >= int64(kb):
		return fmt.Sprintf("%.1f KB", float64(n)/kb)
	}
	return fmt.Sprintf("%d B", n)
}
