//go:build !headless

// view_panels.go — the utility drawer surfaces: History (search +
// cross-mode open), My Tools (custom tool store + the approvals access
// matrix entry, area 12) and Logs (bounded redacted ring + honest state
// surface). Ported from ui/fyne/panels.go; every control posts a real
// core operation.
package goguiui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// --- History ---

type historyPanelState struct {
	st *shellState

	mu      sync.Mutex
	query   string
	mode    string
	results []Session
	err     error
}

func historyState(s *shellState) *historyPanelState {
	return s.view("history", func() any { return &historyPanelState{st: s, mode: "both"} }).(*historyPanelState)
}

func buildHistoryPanel(s *shellState) gui.View {
	p := historyState(s)

	p.mu.Lock()
	query, mode, results, err := p.query, p.mode, append([]Session{}, p.results...), p.err
	p.mu.Unlock()

	var body gui.View
	switch {
	case err != nil:
		body = ErrorSurface("history search", err, nil)
	case len(results) == 0:
		body = EmptySurface("◇", "No results", "Search finds sessions across both conversation spaces.")
	default:
		items := make([]gui.ListBoxOption, 0, len(results))
		for _, h := range results {
			m := NormalizeMode(h.Mode)
			items = append(items, gui.ListBoxOption{ID: h.ID, Name: fmt.Sprintf("[%s] %s", m, historyTitle(h)), Value: h.ID})
		}
		body = gui.ListBox(gui.ListBoxCfg{
			ID:     absID("history-hits"),
			Data:   items,
			Height: 320,
			OnSelect: func(sel []string, _ gui.EventCtx) {
				if len(sel) == 0 {
					return
				}
				// Cross-mode open: activating a hit opens it in its own space.
				p.st.show("chat")
				chat := chatState(p.st)
				chat.openSession(sel[0])
			},
		})
	}

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-history"),
		Sizing: gui.FillFill,
		Content: append([]gui.View{
			gui.Input(gui.InputCfg{
				ID:          absID("history-query"),
				Text:        query,
				Placeholder: "Search conversation history…",
				OnEnter:     func(gui.EventCtx) { p.search() },
				OnTextChanged: func(text string, _ gui.EventCtx) {
					p.mu.Lock()
					p.query = text
					p.mu.Unlock()
				},
			}),
			gui.Select(gui.SelectCfg{
				ID: absID("history-mode"), Items: []string{"both", "chat", "agent"},
				Placeholder: orDash(mode),
				Selected:    []string{mode},
				MaxWidth:    180,
				OnSelect: func(vals []string, _ gui.EventCtx) {
					if len(vals) == 0 {
						return
					}
					p.mu.Lock()
					p.mode = vals[0]
					p.mu.Unlock()
				},
			}),
			gui.Button(gui.ButtonCfg{ID: absID("history-search"), Label: "Search", OnClick: func(gui.EventCtx) { p.search() }}),
		}, body),
		Scrollable: true,
	})
}

func (p *historyPanelState) search() {
	p.mu.Lock()
	q := strings.TrimSpace(p.query)
	mode := p.mode
	p.mu.Unlock()
	if q == "" {
		return
	}
	go func() {
		got, err := p.st.ap.Client().SearchHistory(p.st.ap.Ctx(), q, func() string {
			if mode == "both" {
				return ""
			}
			return mode
		}(), 50)
		uiDo(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if err != nil {
				p.err = err
				return
			}
			p.results = got
			p.err = nil
		})
	}()
}

func historyTitle(s Session) string {
	if s.Title != "" {
		return s.Title
	}
	return "Session " + shortID(s.ID)
}

// --- My Tools (area 12 partial: the tool access matrix + custom tools) ---

type toolsPanelState struct {
	st *shellState

	mu      sync.Mutex
	tools   []Tool
	custom  []CustomTool
	err     error
	loaded  bool
	toggles map[string]bool
}

func toolsState(s *shellState) *toolsPanelState {
	return s.view("tools", func() any { return &toolsPanelState{st: s, toggles: map[string]bool{}} }).(*toolsPanelState)
}

func buildToolsPanel(s *shellState) gui.View {
	v := toolsState(s)
	v.ensureLoaded()

	v.mu.Lock()
	tools := append([]Tool{}, v.tools...)
	custom := append([]CustomTool{}, v.custom...)
	err := v.err
	enabled := map[string]bool{}
	for k, on := range v.toggles {
		enabled[k] = on
	}
	v.mu.Unlock()

	rows := []gui.View{}
	if err != nil {
		rows = append(rows, ErrorSurface("tools", err, nil))
	}

	// The BUILT-IN tool access matrix: every catalog tool with its
	// permission toggle (the same ONE-patch rewrite the Settings agent
	// section uses).
	rows = append(rows, gui.Text(gui.TextCfg{Text: "Built-in tools", TextStyle: gui.CurrentTheme().TextStyleBody}))
	for _, t := range tools {
		tt := t
		on := enabled[tt.Name]
		rows = append(rows, gui.Row(gui.ContainerCfg{
			Content: []gui.View{
				gui.Switch(gui.SwitchCfg{
					ID: absID("tools-toggle", tt.Name), Label: tt.Name, Selected: on,
					OnClick: func(gui.EventCtx) { v.toggleTool(tt.Name, !on) },
				}),
				wrappedText(tt.Description),
			},
		}))
	}

	rows = append(rows, gui.Text(gui.TextCfg{Text: "Custom tools", TextStyle: gui.CurrentTheme().TextStyleBody}))
	if len(custom) == 0 {
		rows = append(rows, EmptySurface("◇", "No custom tools", "Custom tools registered in the core appear here with their permission."))
	}
	for _, ct := range custom {
		rows = append(rows, card(ct.Name, "",
			kvRow("Permission", orDash(ct.Permission)),
			kvRow("Enabled", fmt.Sprintf("%v", ct.Enabled)),
			kvRow("Registered", fmt.Sprintf("%v", ct.Registered)),
			wrappedText(ct.Description),
		))
	}

	return gui.Column(gui.ContainerCfg{
		ID:         absID("view-tools"),
		Sizing:     gui.FillFill,
		Content:    append([]gui.View{PageHeader("My Tools", "Tool catalog, permissions and custom tools")}, rows...),
		Scrollable: true,
	})
}

func (v *toolsPanelState) ensureLoaded() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()

	ap := v.st.ap
	go func() {
		tools, terr := ap.Client().Tools(ap.Ctx())
		cfg, cerr := ap.Client().Config(ap.Ctx())
		custom, xerr := ap.Client().CustomTools(ap.Ctx())
		uiDo(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if terr != nil {
				v.err = terr
				return
			}
			v.tools = tools
			if cerr == nil {
				for _, t := range cfg.EnabledTools {
					v.toggles[t] = true
				}
			}
			if xerr == nil {
				v.custom = custom
			}
		})
	}()
}

func (v *toolsPanelState) toggleTool(name string, on bool) {
	uiDo(func() {
		v.mu.Lock()
		set := map[string]bool{}
		for k, enabled := range v.toggles {
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
		v.toggles[name] = on
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
					v.toggles[t] = true
				}
				v.err = nil
				v.mu.Unlock()
			})
		}()
	})
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// --- Logs ---

type logsPanelState struct {
	st *shellState

	mu      sync.Mutex
	entries []LogEntry
	meta    LogsMeta
	filter  string
	err     error
	loaded  bool
}

func logsState(s *shellState) *logsPanelState {
	return s.view("logs", func() any { return &logsPanelState{st: s} }).(*logsPanelState)
}

func buildLogsPanel(s *shellState) gui.View {
	v := logsState(s)
	v.ensureLoaded()

	v.mu.Lock()
	filter := v.filter
	err := v.err
	loaded := v.loaded
	entries := v.entries
	dir := v.meta.Dir
	v.mu.Unlock()

	rows := []gui.View{
		gui.Input(gui.InputCfg{
			ID:          absID("logs-filter"),
			Text:        filter,
			Placeholder: "Filter (substring)",
			OnEnter:     func(gui.EventCtx) { v.refresh() },
			OnTextChanged: func(text string, _ gui.EventCtx) {
				v.mu.Lock()
				v.filter = text
				v.mu.Unlock()
			},
		}),
	}
	if err != nil {
		rows = append(rows, ErrorSurface("logs", err, nil))
	} else if !loaded {
		rows = append(rows, LoadingSurface("logs"))
	} else {
		if dir != "" {
			head := "Log folder: " + dir
			if v.meta.SinkHealth.FallbackReason != "" {
				head += " (fallback active)"
			}
			if !v.meta.SinkHealth.SinksReady {
				head += " (sink problem — events may not persist)"
			}
			rows = append(rows, wrappedText(head))
		}
		f := strings.ToLower(filter)
		shown := 0
		var b strings.Builder
		for _, e := range entries {
			line := fmt.Sprintf("%s [%s] %s: %s", e.TS, strings.ToUpper(e.Level), e.Sub, e.Msg)
			if f != "" && !strings.Contains(strings.ToLower(line), f) {
				continue
			}
			b.WriteString(line + "\n")
			shown++
			if shown >= 200 {
				break
			}
		}
		if shown == 0 {
			rows = append(rows, EmptySurface("◇", "No matching entries", "Adjust the filter or refresh."))
		} else {
			rows = append(rows, codeText(b.String()))
		}
		rows = append(rows, gui.Button(gui.ButtonCfg{ID: absID("logs-refresh"), Label: "Refresh", OnClick: func(gui.EventCtx) { v.refresh() }}))
	}

	return gui.Column(gui.ContainerCfg{
		ID:         absID("view-logs"),
		Sizing:     gui.FillFill,
		Content:    append([]gui.View{PageHeader("Logs", "Bounded redacted ring from the core")}, rows...),
		Scrollable: true,
	})
}

func (v *logsPanelState) ensureLoaded() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()
	v.refresh()
}

func (v *logsPanelState) refresh() {
	go func() {
		entries, meta, err := v.st.ap.Client().Logs(v.st.ap.Ctx(), 200)
		uiDo(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if err != nil {
				v.err = err
				return
			}
			v.entries = entries
			v.meta = meta
			v.err = nil
			v.loaded = true
		})
	}()
}
