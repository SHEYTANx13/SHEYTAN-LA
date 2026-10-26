//go:build !headless

// panels.go — drawer-style surfaces: History (search + cross-mode open),
// Models (REAL selection path, import, fit evidence), My Tools (custom
// tool store) and Logs (bounded redacted ring + filter). Every control
// posts a real core operation.
package fyneui

import (
	"fmt"
	"strings"

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

type modelsPanel struct {
	ap       *App
	list     *widget.List
	models   ModelsPayload
	sel      *widget.Label
	importIn *widget.Entry
	importB  *widget.Button
	openFldr *widget.Button
	task     *widget.Select
	err      *widget.Label
}

func buildModelsPanel(ap *App) fyne.CanvasObject {
	p := &modelsPanel{ap: ap}
	p.list = widget.NewList(
		func() int { return len(p.models.Local) },
		func() fyne.CanvasObject {
			name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			detail := widget.NewLabel("")
			return container.NewVBox(name, detail)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			m := p.models.Local[i]
			name := m.Name
			if name == "" {
				name = m.ID
			}
			active := m.ID == ap.rt.currentModel()
			if m.Serving {
				name += "  ● ACTIVE"
			}
			box.Objects[0].(*widget.Label).SetText(name)
			box.Objects[1].(*widget.Label).SetText(modelDetail(m))
			_ = active
		},
	)
	p.list.OnSelected = func(i widget.ListItemID) { p.selectModel(i) }

	p.sel = wrappedLabel("")
	p.importIn = widget.NewEntry()
	p.importIn.SetPlaceHolder("Absolute path to a .gguf model on disk")
	p.importB = widget.NewButton("Import", func() { p.importModel() })
	p.openFldr = widget.NewButton("Open models folder", func() {
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

	root := container.NewBorder(
		container.NewVBox(
			PageHeader("Models", "Discovery and the REAL selection state machine"),
			container.NewHBox(p.importIn, p.importB, p.task),
			p.sel,
		),
		p.err, nil, nil, p.list,
	)
	p.refresh()
	return root
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
			p.list.Refresh()
			if selErr == nil {
				p.sel.SetText(selectionText(sel))
			}
			p.err.Hide()
		})
	}()
}

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

// selectModel runs the REAL selection path (POST /api/models/select) —
// never patchConfig({model}).
func (p *modelsPanel) selectModel(i widget.ListItemID) {
	if i < 0 || i >= len(p.models.Local) {
		return
	}
	m := p.models.Local[i]
	id := m.ID
	if id == "" {
		id = m.Name
	}
	if id == "" {
		return
	}
	ap := p.ap
	task := p.task.Selected
	p.sel.SetText("Selecting " + id + "… (analyzing → configuring → loading)")
	go func() {
		sel, err := ap.Client().SelectModel(ap.Ctx(), id, task)
		if err != nil {
			uiDo(func() {
				p.showErr("model selection", err)
				p.sel.SetText("Selection failed — see the reason above. The previous model stays active.")
			})
			return
		}
		uiDo(func() {
			p.sel.SetText(selectionText(sel))
			p.err.Hide()
		})
		p.refresh()
	}()
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

type logsPanel struct {
	ap       *App
	view     *widget.Label
	filt     *widget.Entry
	refreshB *widget.Button
	entries  []LogEntry
	err      *widget.Label
}

func buildLogsPanel(ap *App) fyne.CanvasObject {
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
	p.err.Hide()

	root := container.NewBorder(
		container.NewVBox(PageHeader("Logs", "Redacted ring buffer from the core", p.refreshB), p.filt),
		p.err, nil, nil, container.NewScroll(p.view),
	)
	p.load()
	return root
}

func (p *logsPanel) load() {
	go func() {
		entries, err := p.ap.Client().Logs(p.ap.Ctx(), 200)
		if err != nil {
			uiDo(func() { p.err.SetText("logs: " + err.Error()); p.err.Show() })
			return
		}
		uiDo(func() { p.entries = entries; p.render(); p.err.Hide() })
	}()
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
		b.WriteString("(no entries match)")
	}
	p.view.SetText(b.String())
}
