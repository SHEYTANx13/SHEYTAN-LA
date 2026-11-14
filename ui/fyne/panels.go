//go:build !headless

// panels.go — drawer-style surfaces: History (search + cross-mode open),
// Models (REAL selection path, import, fit evidence), My Tools (custom
// tool store) and Logs (bounded redacted ring + filter). Every control
// posts a real core operation.
package fyneui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// --- History ---

type historyPanel struct {
	ap      *App
	q       *widget.Entry
	mode    *widget.Select
	hits    *widget.List
	results []Session
	err     *widget.Label
}

func buildHistoryPanel(ap *App) fyne.CanvasObject {
	p := &historyPanel{ap: ap}
	p.q = widget.NewEntry()
	p.q.SetPlaceHolder("Search conversation history…")
	p.q.OnSubmitted = func(string) { p.search() }
	p.mode = widget.NewSelect([]string{"both", "chat", "agent"}, nil)
	p.mode.PlaceHolder = "both"
	p.err = widget.NewLabel("")
	p.err.Importance = widget.DangerImportance
	p.err.Hide()

	p.hits = widget.NewList(
		func() int { return len(p.results) },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			h := p.results[i]
			mode := NormalizeMode(h.Mode)
			o.(*widget.Label).SetText(fmt.Sprintf("[%s] %s", mode, historyTitle(h)))
		},
	)
	p.hits.OnSelected = func(i widget.ListItemID) {
		if i < 0 || i >= len(p.results) {
			return
		}
		id := p.results[i].ID
		go func() {
			if _, err := ap.rt.SelectSession(ap.Ctx(), id); err != nil {
				uiDo(func() { p.showErr("open hit", err) })
				return
			}
			uiDo(func() { p.err.Hide() })
		}()
	}

	root := container.NewBorder(
		container.NewVBox(
			PageHeader("History", "Cross-session search (bounded, server-side)"),
			container.NewHBox(p.q, p.mode),
		),
		p.err, nil, nil, p.hits,
	)
	return root
}

func historyTitle(s Session) string {
	if s.Title != "" {
		return s.Title
	}
	return "Session " + shortID(s.ID)
}

func (p *historyPanel) search() {
	q := strings.TrimSpace(p.q.Text)
	mode := p.mode.Selected
	if mode == "both" {
		mode = ""
	}
	ap := p.ap
	go func() {
		got, err := ap.Client().SearchHistory(ap.Ctx(), q, mode, 20)
		if err != nil {
			uiDo(func() { p.showErr("history search", err) })
			return
		}
		uiDo(func() {
			p.results = got
			p.hits.Refresh()
			p.err.Hide()
		})
	}()
}

func (p *historyPanel) showErr(what string, err error) {
	p.err.SetText(what + " failed: " + err.Error())
	p.err.Show()
}

// --- Models ---

// selectionPollInterval is the bounded cadence of the live phase poll
// that runs while a POST selection is in flight (v1.11.1). Tests tighten
// it for determinism.
var selectionPollInterval = 500 * time.Millisecond

// modelsPanel — the Models utility surface. Browsing is NON-DESTRUCTIVE
// (v1.11.1 operator UX fix): highlighting a row only renders its facts;
// the engine transaction starts exclusively from the explicit "Use this
// model" action — single-flight, generation-guarded, with the backend's
// LIVE selection phase rendered while it runs. The Go core stays the one
// selection authority: the panel only calls the real selection API.
type modelsPanel struct {
	ap         *App
	list       *widget.List
	models     ModelsPayload
	sel        *widget.Label
	engineCard *widget.Label
	importIn   *widget.Entry
	importB    *widget.Button
	openFldr   *widget.Button
	task       *widget.Select
	err        *widget.Label

	// v1.11.1 non-destructive selection flow.
	row    int            // highlighted row (-1 = none)
	useB   *widget.Button // the ONE explicit selection action
	detail *widget.Label  // highlighted-row fact summary

	// v1.11.1 engine-updates surface (operator request): a visible Check
	// now action against the official llama.cpp release source, the
	// truthful result line, and a USER-CONTROLLED install through the
	// core's engine-update API. All network work stays off the UI thread;
	// the bounded install poll runs only while a transaction is in flight.
	updCheckB    *widget.Button
	updInstallB  *widget.Button
	updCancelB   *widget.Button
	updLabel     *widget.Label
	updHint      *widget.Label
	openFldrHint *widget.Label
	updMu        sync.Mutex
	updBusy      bool   // single-flight: one check/install interaction at a time
	updPolling   bool   // the bounded install poll is running
	updTag       string // the resolved update target tag

	// v1.11.1 Model Studio (run #41415240891 §5.2/§7): per-AI-System
	// model assignment + runtime profile, edited through the EXISTING
	// systems store PUT (the Go core validates and persists; the run
	// freezes the binding). Inherit is the explicit first option — it is
	// DISTINCT from a missing/unreadable model. One Apply action;
	// single-flight; all network work off the UI thread.
	studioSys     *widget.Select
	studioModel   *widget.Select
	studioProfile *widget.Select
	studioApply   *widget.Button
	studioLabel   *widget.Label
	systems       []AISystem // fetched mirror (full editable surface)
	studioSel     int        // index into systems (-1 = none)
	studioBusy    bool       // single-flight Apply

	current SelectionState // last authoritative selection (GET state)

	// Single-flight + generation guards. inFlight admits exactly one
	// engine transaction; opGen makes a late poll/response from an OLD
	// operation unable to overwrite newer state; settledGen marks the
	// operation whose terminal render already landed (a trailing poll
	// tick can never repaint an intermediate phase over it).
	mu         sync.Mutex
	inFlight   bool
	opGen      uint64
	settledGen uint64
	pollCancel context.CancelFunc
}

func buildModelsPanel(ap *App) fyne.CanvasObject {
	obj, _ := buildModelsPanelFor(ap)
	return obj
}

func buildModelsPanelFor(ap *App) (fyne.CanvasObject, *modelsPanel) {
	p := &modelsPanel{ap: ap, row: -1}
	p.list = widget.NewList(
		func() int { return len(p.models.Local) },
		func() fyne.CanvasObject {
			name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			detail := widget.NewLabel("")
			return container.NewVBox(name, detail)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) { p.renderModelRow(i, o) },
	)
	// v1.11.1: a row selection only HIGHLIGHTS — it never starts an
	// engine transaction. The explicit action below does.
	p.list.OnSelected = func(i widget.ListItemID) { p.highlightRow(i) }

	p.sel = wrappedLabel("")
	p.detail = wrappedLabel("")
	p.detail.Hide() // no row highlighted yet
	p.useB = widget.NewButton("Use this model", func() { p.useModel() })
	p.useB.Disable()
	// Engine & backend status row (Phase 1 scope: narrow, truthful
	// llama.cpp/Vulkan lifecycle presentation fed ONLY by core-owned
	// facts — the Go core stays the single state authority).
	p.engineCard = wrappedLabel("Loading engine state…")
	p.engineCard.TextStyle = fyne.TextStyle{Monospace: true}
	p.importIn = widget.NewEntry()
	p.importIn.SetPlaceHolder("Absolute path to a .gguf model on disk")
	p.importB = widget.NewButton("Import", func() { p.importModel() })
	// v1.11.1 button-fit policy: short, clear action labels on
	// constrained controls; the explanatory detail lives in the adjacent
	// wrapped caption (meaning kept, labels never clipped). "Open models
	// folder" (18 chars) clipped at compact widths.
	p.openFldr = widget.NewButton("Open folder", func() {
		go func() {
			if err := ap.Client().OpenModelsFolder(ap.Ctx()); err != nil {
				uiDo(func() { p.showErr("open models folder", err) })
			}
		}()
	})
	p.task = widget.NewSelect([]string{"chat", "coding", "research", "vision", "agent", "low-power", "maximum"}, nil)
	p.task.PlaceHolder = "task fit (optional)"
	p.err = widget.NewLabel("")
	p.err.Importance = widget.DangerImportance
	p.err.Hide()

	// Engine updates (v1.11.1): Check now against the OFFICIAL
	// ggml-org/llama.cpp releases; the install is USER-CONTROLLED and
	// driven only through the core's engine-update API (the same
	// transactional authority as the scheduled updater — never a second
	// updater in the UI).
	// Button-fit policy (v1.11.1): the three actions carry short labels
	// ("Check now", "Install update", "Cancel") STACKED vertically —
	// Fyne 2.8.2's VBox grants every child its full MinSize, so no label
	// can ever be compressed or clipped at any matrix width — and the
	// restart consequence lives in the adjacent caption, exactly where an
	// operator looks before pressing Install.
	p.updLabel = wrappedLabel("Engine updates: not checked yet — run Check now.")
	p.updLabel.TextStyle = fyne.TextStyle{Monospace: true}
	p.updHint = wrappedLabel("Install restarts the local engine after the package verifies. Check is read-only; Cancel stops a running download.")
	p.updHint.TextStyle = fyne.TextStyle{Italic: true}
	p.updCheckB = widget.NewButton("Check now", func() { p.checkEngineUpdate() })
	p.updCheckB.Importance = widget.LowImportance
	p.updInstallB = widget.NewButton("Install update", func() { p.installEngineUpdate() })
	p.updInstallB.Importance = widget.HighImportance
	p.updInstallB.Disable() // armed only by an update-available check
	p.updCancelB = widget.NewButton("Cancel", func() {
		go func() {
			if err := ap.Client().CancelEngineDownload(ap.Ctx()); err != nil {
				uiDo(func() { p.showErr("cancel engine update download", err) })
			}
		}()
	})
	p.updCancelB.Hide() // visible only while an install download runs

	// Model Studio (v1.11.1): the per-system binding card. The
	// selects SHOW facts; the Apply button is the one mutator; the
	// label renders what the STORE holds, never what the form holds.
	p.studioSys = widget.NewSelect(nil, func(string) { p.studioSystemChanged() })
	p.studioSys.PlaceHolder = "choose an AI System…"
	p.studioModel = widget.NewSelect(nil, nil)
	p.studioModel.PlaceHolder = "model assignment…"
	p.studioProfile = widget.NewSelect(nil, nil)
	p.studioProfile.PlaceHolder = "runtime profile…"
	p.studioApply = widget.NewButton("Apply binding", func() { p.applyStudio() })
	p.studioApply.Disable()
	p.studioLabel = wrappedLabel("Inherit is the default state: a system without its own model serves the globally selected model.")
	p.studioLabel.TextStyle = fyne.TextStyle{Monospace: true}

	root := container.NewBorder(
		container.NewVBox(
			PageHeader("Models", "Discovery and the REAL selection state machine"),
			widget.NewCard("Engine & backend", "Requested ≠ available ≠ selected ≠ executed ≠ verified — strongest PROVEN state only", p.engineCard),
			widget.NewCard("Engine updates", "Official ggml-org/llama.cpp releases · check is read-only · install is user-controlled", container.NewVBox(
				container.NewHBox(p.updCheckB, p.updInstallB, p.updCancelB),
				p.updLabel,
			)),
			widget.NewCard("Model Studio", "Per-AI-System model binding · one Apply action · the core validates and freezes it per run", container.NewVBox(
				p.studioSys,
				p.studioModel,
				p.studioProfile,
				p.studioLabel,
				p.studioApply,
			)),
			container.NewHBox(p.importIn, p.importB, p.task),
			container.NewBorder(nil, nil, p.useB, nil, p.detail),
			p.sel,
		),
		p.err, nil, nil, p.list,
	)
	p.refresh()
	return root, p
}

// engineUpdatePollInterval is the bounded cadence of the install-progress
// poll (tests tighten it for determinism).
var engineUpdatePollInterval = 500 * time.Millisecond

// engineUpdateTerminal reports check statuses that end the install poll.
func engineUpdateTerminal(status string) bool {
	return status != "installing"
}

// renderEngineUpdate renders the truthful check/install state. The
// install action is armed ONLY by an update-available resolution; the
// cancel affordance appears only while an install download runs.
func (p *modelsPanel) renderEngineUpdate(st *EngineUpdateCheck, err error) {
	if err != nil {
		p.updLabel.SetText("Engine updates: check failed — " + err.Error())
		p.updInstallB.Disable()
		p.updCancelB.Hide()
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
	p.updLabel.SetText(b.String())

	if st.Status == "update-available" {
		p.updTag = st.LatestTag
		p.updInstallB.Enable()
	} else {
		p.updInstallB.Disable()
	}
	if st.Status == "installing" && st.Progress != nil {
		p.updCancelB.Show()
	} else {
		p.updCancelB.Hide()
	}
}

// checkEngineUpdate runs the READ-ONLY official-release check off the UI
// thread and renders its truthful result.
func (p *modelsPanel) checkEngineUpdate() {
	p.updMu.Lock()
	if p.updBusy {
		p.updMu.Unlock()
		return
	}
	p.updBusy = true
	p.updMu.Unlock()

	ap := p.ap
	// The action runs on the UI event loop (button OnTapped): the interim
	// label write is a direct widget mutation; every write from the
	// goroutine below goes through uiDo.
	p.updLabel.SetText("Engine updates: checking the official release source…")
	go func() {
		defer func() {
			p.updMu.Lock()
			p.updBusy = false
			p.updMu.Unlock()
		}()
		st, err := ap.Client().EngineUpdateCheckNow(ap.Ctx())
		uiDo(func() {
			p.renderEngineUpdate(st, err)
			if err == nil {
				p.err.Hide()
			}
		})
	}()
}

// installEngineUpdate starts the USER-TRIGGERED engine update through the
// core API, then polls the bounded check surface while the transaction
// runs — real phases only, no fabricated progress, no UI-thread network.
func (p *modelsPanel) installEngineUpdate() {
	p.updMu.Lock()
	if p.updBusy || p.updPolling {
		p.updMu.Unlock()
		return
	}
	tag := p.updTag
	p.updBusy = true
	p.updMu.Unlock()

	ap := p.ap
	// UI-thread path: direct widget mutations; the goroutine below owns
	// every uiDo write.
	p.updInstallB.Disable()
	p.updLabel.SetText("Engine updates: starting the engine update transaction…")

	go func() {
		st, err := ap.Client().EngineUpdateInstall(ap.Ctx(), tag)
		if err != nil {
			uiDo(func() {
				p.showErr("engine update", err)
				p.renderEngineUpdate(nil, err)
			})
			p.updMu.Lock()
			p.updBusy = false
			p.updMu.Unlock()
			return
		}
		p.updMu.Lock()
		p.updBusy = false
		p.updPolling = true
		p.updMu.Unlock()
		uiDo(func() { p.renderEngineUpdate(st, nil) })
		p.pollEngineUpdate()
	}()
}

// pollEngineUpdate renders the live install state at a bounded cadence
// while the core transaction runs; stops at every terminal status, on
// app close, or on a poll error (a Check now re-arms the surface).
func (p *modelsPanel) pollEngineUpdate() {
	ticker := time.NewTicker(engineUpdatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.ap.Ctx().Done():
			return
		case <-ticker.C:
		}
		p.updMu.Lock()
		polling := p.updPolling
		p.updMu.Unlock()
		if !polling {
			return
		}
		st, err := p.ap.Client().EngineUpdateCheckNow(p.ap.Ctx())
		if err != nil {
			return // poll transport error: stop polling; a Check now re-arms
		}
		terminal := engineUpdateTerminal(st.Status)
		uiDo(func() { p.renderEngineUpdate(st, nil) })
		if terminal {
			p.updMu.Lock()
			p.updPolling = false
			p.updMu.Unlock()
			p.refresh()
			return
		}
	}
}

// renderModelRow renders one model row into a template cell — the exact
// code path the widget list's UpdateItem drives (extracted so tests drive
// the identical renderer). Truthful, SEPARATE markers (v1.11.1): SERVING
// is the engine-loaded fact; SELECTED is the authoritative selection
// state — one never implies the other.
func (p *modelsPanel) renderModelRow(i widget.ListItemID, o fyne.CanvasObject) {
	box := o.(*fyne.Container)
	m := p.models.Local[i]
	name := m.Name
	if name == "" {
		name = m.ID
	}
	switch {
	case m.Serving:
		name += "  ● ACTIVE"
	case p.isCurrentSelection(m):
		name += "  ○ SELECTED"
	}
	box.Objects[0].(*widget.Label).SetText(name)
	box.Objects[1].(*widget.Label).SetText(modelDetail(m))
}

// modelSummary renders the highlighted row's already-available facts —
// name, architecture, quantization, file size, vision capability,
// context/metadata — with NO analysis on any thread (the GGUF facts were
// gathered by the core's discovery pass).
func modelSummary(m ModelInfo) string {
	parts := []string{}
	if m.ParameterInfo != "" {
		parts = append(parts, m.ParameterInfo)
	}
	if d := modelDetail(m); d != "" {
		parts = append(parts, d)
	}
	if m.ContextLength > 0 {
		s := fmt.Sprintf("context limit %d", m.ContextLength)
		if m.RecContext > 0 {
			s += fmt.Sprintf(" (recommended %d)", m.RecContext)
		}
		parts = append(parts, s)
	}
	if m.Path != "" {
		parts = append(parts, m.Path)
	}
	return strings.Join(parts, " · ")
}

func modelDetail(m ModelInfo) string {
	parts := []string{}
	if m.Architecture != "" {
		parts = append(parts, m.Architecture)
	}
	if m.Quantization != "" {
		parts = append(parts, m.Quantization)
	}
	if m.SizeBytes > 0 {
		parts = append(parts, humanBytes(m.SizeBytes))
	}
	if m.VisionState != "" && m.VisionState != "none" {
		parts = append(parts, "vision: "+m.VisionState)
	}
	return strings.Join(parts, " · ")
}

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

// highlightRow is the NON-DESTRUCTIVE browse step (v1.11.1): selecting a
// list row renders its facts and arms the explicit selection action — it
// never starts an engine transaction.
func (p *modelsPanel) highlightRow(i widget.ListItemID) {
	if i < 0 || i >= len(p.models.Local) {
		return
	}
	p.row = int(i)
	m := p.models.Local[i]
	p.detail.SetText(modelSummary(m))
	p.detail.Show()
	p.syncUseButton()
}

// isCurrentSelection reports whether m is the AUTHORITATIVE current
// selection (the GET selection state's model — the core's config
// selection), independent of serving.
func (p *modelsPanel) isCurrentSelection(m ModelInfo) bool {
	cur := p.current.Model
	if cur == "" {
		return false
	}
	return m.ID == cur || m.Name == cur
}

// syncUseButton enables the explicit selection action exactly when a row
// is highlighted, no selection is in flight, and the highlighted model is
// not already the SERVING selection (the core short-circuits that case —
// the button simply avoids a pointless round-trip; a selected-but-not-
// serving model stays actionable so the engine can be started).
func (p *modelsPanel) syncUseButton() {
	if p.row < 0 || p.row >= len(p.models.Local) {
		p.useB.Disable()
		return
	}
	p.mu.Lock()
	busy := p.inFlight
	p.mu.Unlock()
	if busy {
		p.useB.Disable()
		return
	}
	m := p.models.Local[p.row]
	if m.Serving && p.isCurrentSelection(m) {
		p.useB.Disable()
		return
	}
	p.useB.Enable()
}

func (p *modelsPanel) refresh() {
	go func() {
		got, err := p.ap.Client().Models(p.ap.Ctx())
		if err != nil {
			uiDo(func() { p.showErr("models", err) })
			return
		}
		sel, selErr := p.ap.Client().Selection(p.ap.Ctx())
		uiDo(func() {
			p.models = got
			if selErr == nil {
				// The authoritative selection feeds both the
				// status line and the truthful SELECTED marker.
				p.current = sel
				p.sel.SetText(selectionText(sel))
			}
			p.list.Refresh()
			p.syncUseButton()
			p.err.Hide()
			p.refreshStudioOptions()
		})
	}()
	p.refreshEngineCard()
	p.refreshStudio()
}

// refreshEngineCard renders the truthful engine/backend lifecycle from
// three core surfaces (/api/engine, /api/maintenance, /api/perf
// .accelerator). It only renders what the core authoritatively provides —
// no derivation, no evidence upgrade, unavailable stays unavailable.
func (p *modelsPanel) refreshEngineCard() {
	go func() {
		c := p.ap.Client()
		ctx := p.ap.Ctx()
		var b strings.Builder

		if e, err := c.Engine(ctx); err == nil {
			state, backend := e.State, e.Backend
			if state == "" {
				state = "unknown"
			}
			if backend == "" {
				backend = "unknown"
			}
			b.WriteString("Engine: " + state + " · backend: " + backend)
			if e.Diagnostics != nil && e.Diagnostics.BinaryTag != "" {
				b.WriteString(" · engine: " + e.Diagnostics.BinaryTag)
			}
			b.WriteString("\n")
			model := e.Model
			if model == "" {
				model = "—"
			}
			verdict := "not verified yet"
			if e.Verified {
				verdict = "verified"
			}
			b.WriteString("Model: " + model + " (" + verdict + ")\n")
			if e.SelectionRequired {
				b.WriteString("Model selection required — pick a model above; the engine never loads one on its own.\n")
			}
		}

		if raw, err := c.Maintenance(ctx); err == nil {
			var m struct {
				Phase     string `json:"phase"`
				Reason    string `json:"reason"`
				TargetTag string `json:"targetTag"`
			}
			if json.Unmarshal(raw, &m) == nil && m.Phase != "" {
				txt := "Engine update: " + m.Phase
				if m.TargetTag != "" {
					txt += " → " + m.TargetTag
				}
				if m.Reason != "" {
					txt += " (" + m.Reason + ")"
				}
				b.WriteString(txt + "\n")
			}
		}

		if a, err := c.Accelerator(ctx); err == nil {
			device := a.Device
			if device == "" {
				device = "unknown"
			}
			b.WriteString(fmt.Sprintf("Accelerator: requested %s → selected %s · device: %s", a.Requested, a.Selected, device))
			switch {
			case a.GPULayers < 0:
				b.WriteString(" · layers: all")
			case a.GPULayers > 0:
				b.WriteString(fmt.Sprintf(" · layers: %d", a.GPULayers))
			}
			b.WriteString("\n")
			ev := "not verified"
			if a.ExecutionVerified {
				ev = "execution verified"
			} else if a.Verification != "" {
				ev += " — " + a.Verification
			}
			b.WriteString("Evidence: " + ev)
			if a.ReasonCode != "" {
				b.WriteString(" (" + a.ReasonCode + ")")
			}
			b.WriteString("\n")
			if len(a.Available) > 0 {
				b.WriteString("Available backends: " + strings.Join(a.Available, ", ") + "\n")
			}
		}

		card := b.String()
		if strings.TrimSpace(card) == "" {
			card = "Engine state unavailable — the core has not reported it yet."
		}
		uiDo(func() { p.engineCard.SetText(card) })
	}()
}

// selectionText renders the authoritative selection state.
func selectionText(s SelectionState) string {
	txt := "Selection phase: " + s.Phase
	if s.Model != "" {
		txt += " — " + s.Model
	}
	if s.Error != "" {
		txt += " — error: " + s.Error
	}
	return txt
}

// terminalSelectionPhase reports backend phases that end a selection
// flow (the poll stops there — the POST response is the terminal truth).
func terminalSelectionPhase(phase string) bool {
	switch phase {
	case "ready", "failed", "idle":
		return true
	}
	return false
}

// staleOp reports whether gen no longer names the live operation.
func (p *modelsPanel) staleOp(gen uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return gen != p.opGen
}

// settledAtOrAfter reports whether gen's terminal render already landed
// (a trailing poll tick can never repaint an intermediate phase over it).
func (p *modelsPanel) settledAtOrAfter(gen uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.settledGen >= gen
}

// useModel runs the REAL selection path (POST /api/models/select) — never
// patchConfig({model}). v1.11.1 contract:
//   - single-flight: exactly one engine transaction at a time; a second
//     action while one is in flight is a no-op (the button is disabled,
//     and the guard holds even for direct invocations);
//   - generation token: a late poll or response from an OLD operation can
//     never overwrite newer state;
//   - live phases: while the synchronous POST walks the backend state
//     machine (select → analyzing → configuring → loading → calibrating),
//     the AUTHORITATIVE GET state is polled at a bounded cadence and
//     rendered — the UI thread never performs network work;
//   - every terminal path (success, failure) re-enables the controls.
func (p *modelsPanel) useModel() {
	if p.row < 0 || p.row >= len(p.models.Local) {
		return
	}
	m := p.models.Local[p.row]
	id := m.ID
	if id == "" {
		id = m.Name
	}
	if id == "" {
		return
	}

	p.mu.Lock()
	if p.inFlight {
		p.mu.Unlock()
		return // single-flight: one engine transaction at a time
	}
	p.inFlight = true
	p.opGen++
	gen := p.opGen
	p.mu.Unlock()

	ap := p.ap
	task := p.task.Selected
	ctx := ap.Ctx()
	p.useB.Disable()
	p.sel.SetText("Selecting " + id + "… — live phase: (starting)")

	// Bounded, cancellation-aware phase polling — only while this
	// selection is in progress.
	pollCtx, cancelPoll := context.WithCancel(ctx)
	p.mu.Lock()
	p.pollCancel = cancelPoll
	p.mu.Unlock()
	go p.pollSelectionPhases(gen, id, pollCtx)

	go func() {
		sel, err := ap.Client().SelectModel(ctx, id, task)

		// The POST returned: stop the poll, detach its cancel.
		cancelPoll()
		p.mu.Lock()
		p.pollCancel = nil
		stale := gen != p.opGen
		p.settledGen = gen
		p.mu.Unlock()
		if stale {
			// A newer operation owns the UI state; leave it alone.
			p.mu.Lock()
			p.inFlight = false
			p.mu.Unlock()
			return
		}

		if err != nil {
			uiDo(func() {
				p.showErr("model selection", err)
				p.sel.SetText("Selection failed — see the reason above. The previous model stays active.")
			})
		} else {
			uiDo(func() {
				p.sel.SetText(selectionText(sel))
				p.err.Hide()
				p.current = sel
			})
		}

		p.mu.Lock()
		p.inFlight = false
		p.mu.Unlock()
		uiDo(func() {
			p.list.Refresh()
			p.syncUseButton()
		})
		p.refresh()
	}()
}

// pollSelectionPhases renders the backend's LIVE selection phases while
// the POST runs: bounded cadence, cancellation-aware (app close stops
// it), stops on terminal state, network error, cancellation, or a
// superseded operation. All widget writes go through uiDo with a final
// staleness/settled re-check inside the callback (a queued render can
// never land out of order over a terminal state).
func (p *modelsPanel) pollSelectionPhases(gen uint64, id string, ctx context.Context) {
	ticker := time.NewTicker(selectionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if p.staleOp(gen) {
			return
		}
		sel, err := p.ap.Client().Selection(ctx)
		if err != nil {
			return // network error: stop polling; the POST result still lands
		}
		if p.staleOp(gen) || p.settledAtOrAfter(gen) {
			return
		}
		phase := sel.Phase
		model := sel.Model
		uiDo(func() {
			if p.staleOp(gen) || p.settledAtOrAfter(gen) {
				return
			}
			txt := "Selecting " + id + "… — live phase: " + phase
			if model != "" {
				txt += " (" + model + ")"
			}
			p.sel.SetText(txt)
		})
		if terminalSelectionPhase(phase) {
			return
		}
	}
}

func (p *modelsPanel) importModel() {
	path := strings.TrimSpace(p.importIn.Text)
	if path == "" {
		return
	}
	ap := p.ap
	go func() {
		out, err := ap.Client().ImportModel(ap.Ctx(), path)
		if err != nil {
			uiDo(func() { p.showErr("import model", err) })
			return
		}
		uiDo(func() {
			p.importIn.SetText("")
			p.err.Hide()
			if name, ok := out["name"].(string); ok {
				p.sel.SetText("Imported: " + name)
			}
		})
		p.refresh()
	}()
}

// studioInheritModel / studioInheritProfile are the explicit INHERIT
// options — distinguishable from a missing/unreadable model by
// construction (they are real options, never blank placeholders).
const (
	studioInheritModel   = "(inherit the global default)"
	studioInheritProfile = "(inherit the global profile)"
)

// refreshStudio fetches the AI Systems mirror off the UI thread and
// populates the binding card. One bounded fetch per panel refresh — no
// polling loop, no unbounded growth.
func (p *modelsPanel) refreshStudio() {
	go func() {
		systems, _, err := p.ap.Client().Systems(p.ap.Ctx())
		uiDo(func() {
			if err != nil {
				p.studioLabel.SetText("AI Systems unavailable: " + err.Error())
				return
			}
			p.systems = systems
			p.studioSel = -1

			names := make([]string, 0, len(systems))
			for _, s := range systems {
				name := s.Name
				if name == "" {
					name = s.SystemID
				}
				if s.Active {
					name += "  ● active"
				}
				names = append(names, name)
			}
			p.studioSys.Options = names
			p.studioSys.ClearSelected()
			p.studioModel.Options = nil
			p.studioProfile.Options = nil
			p.studioApply.Disable()
			p.studioSys.Refresh()
		})
	}()
}

// refreshStudioOptions rebuilds the model option list after the local
// model list changed — the picker shows REAL local model facts (the
// same list the models panel renders).
func (p *modelsPanel) refreshStudioOptions() {
	opts := make([]string, 0, len(p.models.Local)+1)
	opts = append(opts, studioInheritModel)
	for _, m := range p.models.Local {
		if m.Name != "" {
			opts = append(opts, m.Name)
		}
	}
	p.studioModel.Options = opts
	p.studioModel.Refresh()
}

// studioSystemChanged re-renders the current binding of the chosen
// system (choosing a system only SHOWS its facts — the Apply action is
// the one mutator; other systems are never touched).
func (p *modelsPanel) studioSystemChanged() {
	i := p.studioSys.SelectedIndex()
	if i < 0 || i >= len(p.systems) {
		return
	}
	p.studioSel = i
	sys := p.systems[i]

	p.refreshStudioOptions()
	if sys.Model == "" {
		p.studioModel.SetSelected(studioInheritModel)
	} else {
		p.studioModel.SetSelected(sys.Model)
	}

	p.studioProfile.Options = []string{studioInheritProfile,
		"chat", "coding", "research", "vision", "agent", "low-power", "maximum"}
	if sys.RuntimeProfile == "" {
		p.studioProfile.SetSelected(studioInheritProfile)
	} else {
		p.studioProfile.SetSelected(sys.RuntimeProfile)
	}
	p.studioProfile.Refresh()

	p.studioLabel.SetText(studioBindingText(sys))
	p.studioApply.Enable()
}

// studioBindingText renders the fetched binding truthfully — what the
// STORE holds, not what the form holds.
func studioBindingText(sys AISystem) string {
	model := sys.Model
	if model == "" {
		model = "inherit (the globally selected model)"
	}
	profile := sys.RuntimeProfile
	if profile == "" {
		profile = "inherit (the global runtime profile)"
	}
	return "Binding: model=" + model + " · profile=" + profile +
		" · rev " + strconv.Itoa(sys.Revision)
}

// applyStudio persists the binding through the ONE store PUT. The FULL
// fetched record is sent back with only the two binding fields changed —
// never a partial payload that could wipe fields edited elsewhere
// (instructions, approval policy, …). Single-flight: one Apply at a
// time; the button re-arms on every terminal path.
func (p *modelsPanel) applyStudio() {
	p.mu.Lock()
	if p.studioBusy || p.studioSel < 0 || p.studioSel >= len(p.systems) {
		p.mu.Unlock()
		return
	}
	p.studioBusy = true
	sys := p.systems[p.studioSel]
	p.mu.Unlock()

	p.studioApply.Disable()

	model := p.studioModel.Selected
	if model == studioInheritModel || model == "" {
		model = ""
	}
	profile := p.studioProfile.Selected
	if profile == studioInheritProfile || profile == "" {
		profile = ""
	}

	go func() {
		sys.Model = model
		sys.RuntimeProfile = profile

		updated, err := p.ap.Client().UpdateSystem(p.ap.Ctx(), sys)

		p.mu.Lock()
		p.studioBusy = false
		p.mu.Unlock()

		uiDo(func() {
			if err != nil {
				p.showErr("apply model binding", err)
				p.studioApply.Enable()
				return
			}
			if p.studioSel >= 0 && p.studioSel < len(p.systems) {
				p.systems[p.studioSel] = updated
			}
			p.studioLabel.SetText("Applied. " + studioBindingText(updated))
			p.studioApply.Enable()
		})
	}()
}

func (p *modelsPanel) showErr(what string, err error) {
	p.err.SetText(what + " failed: " + err.Error())
	p.err.Show()
}

// --- My Tools ---

type toolsPanel struct {
	ap    *App
	list  *widget.List
	tools []CustomTool
	err   *widget.Label
}

func buildToolsPanel(ap *App) fyne.CanvasObject {
	p := &toolsPanel{ap: ap}
	p.list = widget.NewList(
		func() int { return len(p.tools) },
		func() fyne.CanvasObject {
			name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			detail := widget.NewLabel("")
			return container.NewVBox(name, detail)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			t := p.tools[i]
			state := "unregistered"
			if t.Registered {
				state = "registered"
			}
			box.Objects[0].(*widget.Label).SetText(t.Name + "  (" + state + ")")
			box.Objects[1].(*widget.Label).SetText(t.Description)
		},
	)
	p.err = widget.NewLabel("")
	p.err.Importance = widget.DangerImportance
	p.err.Hide()
	go func() {
		got, err := ap.Client().CustomTools(ap.Ctx())
		if err != nil {
			uiDo(func() { p.err.SetText("custom tools: " + err.Error()); p.err.Show() })
			return
		}
		uiDo(func() { p.tools = got; p.list.Refresh() })
	}()
	return container.NewBorder(
		PageHeader("My Tools", "Custom tool store (read-only; management lives in the core API)"),
		p.err, nil, nil, p.list,
	)
}

// --- Logs ---

// logsPanel is the operator log surface. v1.11.1 honest-state contract:
// loading, empty (ONLY after a successful fetch that returned nothing),
// error (a failed request is NEVER rendered as "no entries"), refreshed,
// and the resolved log directory + sink health — an unhealthy or
// fallback sink is shown as such, never hidden behind a healthy-looking
// panel.
type logsPanel struct {
	ap       *App
	view     *widget.Label
	filt     *widget.Entry
	refreshB *widget.Button
	dirInfo  *widget.Label
	err      *widget.Label
	entries  []LogEntry

	// inFlight guards against overlapping requests; reqSeq drops stale
	// responses so a slow earlier fetch can never overwrite a newer one.
	inFlight bool
	reqSeq   int
}

func buildLogsPanel(ap *App) fyne.CanvasObject {
	root, _ := buildLogsPanelFor(ap)
	return root
}

// buildLogsPanelFor is the test seam: it returns the assembled panel
// object AND the panel state (the same single construction path).
func buildLogsPanelFor(ap *App) (fyne.CanvasObject, *logsPanel) {
	p := &logsPanel{ap: ap}
	p.view = widget.NewLabel("")
	p.view.Wrapping = fyne.TextWrapWord
	p.view.TextStyle = fyne.TextStyle{Monospace: true}
	p.filt = widget.NewEntry()
	p.filt.SetPlaceHolder("Filter (substring)…")
	p.filt.OnChanged = func(string) { p.render() }
	p.refreshB = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() { p.load() })
	p.err = widget.NewLabel("")
	p.err.Importance = widget.DangerImportance
	p.err.Wrapping = fyne.TextWrapWord
	p.err.Hide()
	p.dirInfo = widget.NewLabel("")
	p.dirInfo.TextStyle = fyne.TextStyle{Italic: true}
	p.dirInfo.Wrapping = fyne.TextWrapOff

	root := container.NewBorder(
		container.NewVBox(
			PageHeader("Logs", "Redacted ring buffer from the core", p.refreshB),
			p.dirInfo,
			p.filt,
		),
		p.err, nil, nil, container.NewScroll(p.view),
	)
	p.load()
	return root, p
}

// load fetches the core log ring. One request at a time; a response from
// a superseded request is discarded (reqSeq), so manual Refresh clicks
// can never interleave into a stale overwrite.
//
// Threading: load() runs on the UI goroutine (panel construction or the
// Refresh button tap) — the loading-state mutations below are direct,
// consistent with the surrounding panel construction; only the fetch
// callback marshals back through uiDo.
func (p *logsPanel) load() {
	if p.inFlight {
		return
	}
	p.inFlight = true
	seq := p.reqSeq + 1
	p.reqSeq = seq

	p.view.SetText("(loading…)")
	p.err.Hide()

	go func() {
		entries, meta, err := p.ap.Client().Logs(p.ap.Ctx(), 200)
		uiDo(func() {
			p.inFlight = false
			if seq != p.reqSeq {
				return // superseded by a newer request — never a stale overwrite
			}
			if err != nil {
				// A failed request is an ERROR state, never "no entries":
				// the transport being broken must be visible here.
				p.entries = nil
				p.view.SetText("")
				p.dirInfo.SetText("")
				p.err.SetText("logs unavailable — the core connection is broken or the request failed: " + err.Error())
				p.err.Show()
				return
			}
			p.entries = entries
			p.dirInfo.SetText(p.describeDir(meta))
			p.err.Hide()
			p.render()
		})
	}()
}

// describeDir renders the resolved log directory and sink health in an
// operator-friendly single caption. Fallback and failure states are
// named, never hidden.
func (p *logsPanel) describeDir(meta LogsMeta) string {
	dir := meta.Dir
	if dir == "" {
		dir = "(unknown)"
	}
	text := "Log folder: " + dir
	if meta.SinkHealth.FallbackReason != "" {
		text += " — FALLBACK ACTIVE (the configured log folder could not be used)"
	}
	if !meta.SinkHealth.SinksReady {
		if meta.SinkHealth.LastErr != "" {
			text += " — sink problem: " + meta.SinkHealth.LastErr
		} else {
			text += " — sink problem (events may not persist to disk)"
		}
	}
	if meta.SinkHealth.EmergencyLines > 0 {
		text += fmt.Sprintf(" — %d recent event(s) held in memory only", meta.SinkHealth.EmergencyLines)
	}
	return text
}

func (p *logsPanel) render() {
	filt := strings.ToLower(p.filt.Text)
	var b strings.Builder
	for _, e := range p.entries {
		line := fmt.Sprintf("%s [%s] %s: %s", e.TS, strings.ToUpper(e.Level), e.Sub, e.Msg)
		if filt != "" && !strings.Contains(strings.ToLower(line), filt) {
			continue
		}
		b.WriteString(line + "\n")
	}
	if b.Len() == 0 {
		// Only reachable after a SUCCESSFUL fetch: the honest empty state.
		if p.err.Visible() {
			return // an error is showing — never mask it with "no entries"
		}
		b.WriteString("(no entries match)")
	}
	p.view.SetText(b.String())
}
