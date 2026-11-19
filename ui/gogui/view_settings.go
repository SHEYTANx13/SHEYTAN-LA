//go:build !headless

// view_settings.go — the Settings view (area 10): the 8-section IA driven
// by the typed schema (settings_schema.go), one control per
// FIELD_PLACEMENT row, patches through the core's authoritative
// deep-merge path and a re-read after every change so the UI can never
// drift from persisted state. Includes the enabled-tools matrix
// (the approvals surface, area 12) and the read-only diagnostics card.
package goguiui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

type settingsViewState struct {
	st *shellState

	mu          sync.Mutex
	section     string
	config      *Config
	tools       []Tool
	toolsLoaded bool
	toolChecks  map[string]bool
	diagText    string
	texts       map[string]string
	err         error
	status      string
	loaded      bool
}

func settingsState(s *shellState) *settingsViewState {
	return s.view("settings", func() any {
		return &settingsViewState{st: s, section: SettingsSections[0].ID, toolChecks: map[string]bool{}}
	}).(*settingsViewState)
}

func buildSettingsView(s *shellState) gui.View {
	v := settingsState(s)
	v.load()

	tabs := make([]gui.TabItemCfg, 0, len(SettingsSections))
	for _, sec := range SettingsSections {
		tabs = append(tabs, gui.NewTabItem(sec.ID, sec.Label, []gui.View{v.buildSection(sec.ID)}))
	}
	active := v.section

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-settings"),
		Sizing: gui.FillFill,
		Content: []gui.View{
			PageHeader("Settings", "Every field patches the core schema and re-reads the saved state"),
			v.statusView(),
			v.errView(),
			gui.TabControl(gui.TabControlCfg{
				ID:       absID("settings-tabs"),
				Items:    tabs,
				Selected: active,
				OnSelect: func(id string, _ gui.EventCtx) {
					uiDo(func() {
						v.mu.Lock()
						v.section = id
						v.mu.Unlock()
					})
				},
			}),
		},
	})
}

func (v *settingsViewState) statusView() gui.View {
	v.mu.Lock()
	status := v.status
	v.mu.Unlock()
	if status == "" {
		return nil
	}
	return gui.Text(gui.TextCfg{ID: absID("settings-status"), Text: status, TextStyle: gui.CurrentTheme().TextStyleCaption})
}

func (v *settingsViewState) errView() gui.View {
	v.mu.Lock()
	err := v.err
	v.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("settings", err, nil)
}

// buildSection renders one section's fields from the typed registry.
func (v *settingsViewState) buildSection(id string) gui.View {
	rows := make([]gui.View, 0, 8)
	for _, f := range FieldsForSection(id) {
		rows = append(rows, v.controlFor(f))
	}
	if id == "agent" {
		rows = append(rows, v.buildToolsMatrix())
	}
	if id == "diagnostics" {
		rows = append(rows, v.buildDiagnostics())
	}
	return gui.Column(gui.ContainerCfg{
		ID:         absID("settings-section", id),
		Sizing:     gui.FillFill,
		Scrollable: true,
		Spacing:    gui.SpacingPx(6),
		Padding:    gui.PadAll(6),
		Content:    rows,
	})
}

// controlFor builds the type-correct control for one field. Type-correct
// controls are a shipped contract: wrong types produced documented 400s.
func (v *settingsViewState) controlFor(f SettingsField) gui.View {
	rows := []gui.View{
		gui.Text(gui.TextCfg{Text: f.Label, TextStyle: gui.CurrentTheme().TextStyleBody}),
		subtitleText(f.Key),
	}
	switch f.Kind {
	case FieldBool:
		on := v.boolValue(f)
		rows = append(rows, gui.Switch(gui.SwitchCfg{
			ID:       absID("settings-field", f.Key),
			Label:    "on",
			Selected: on,
			OnClick: func(gui.EventCtx) {
				uiDo(func() { v.patch(f, !on) })
			},
		}))
	case FieldInt:
		rows = append(rows, gui.Input(gui.InputCfg{
			ID: absID("settings-field", f.Key),
			Text: func() string {
				if n, ok := v.intValue(f); ok {
					return fmt.Sprintf("%d", n)
				}
				return ""
			}(),
			Placeholder: "integer",
			OnEnter:     func(gui.EventCtx) { v.patchInt(f, v.fieldText(f)) },
		}))
	case FieldFloat:
		rows = append(rows, gui.Input(gui.InputCfg{
			ID: absID("settings-field", f.Key),
			Text: func() string {
				if x, ok := v.floatValue(f); ok {
					return strconv.FormatFloat(x, 'g', -1, 64)
				}
				return ""
			}(),
			Placeholder: "number",
			OnEnter:     func(gui.EventCtx) { v.patchFloat(f, v.fieldText(f)) },
		}))
	case FieldEnum:
		rows = append(rows, gui.Select(gui.SelectCfg{
			ID: absID("settings-field", f.Key), Items: f.Enum,
			Placeholder: f.Label,
			Selected: func() []string {
				if s := v.stringValue(f); s != "" {
					return []string{s}
				}
				return nil
			}(),
			MaxWidth: 260,
			OnSelect: func(vals []string, _ gui.EventCtx) {
				if len(vals) > 0 {
					uiDo(func() { v.patch(f, vals[0]) })
				}
			},
		}))
	case FieldString:
		rows = append(rows, gui.Input(gui.InputCfg{
			ID:          absID("settings-field", f.Key),
			Text:        v.stringValue(f),
			Placeholder: "text",
			OnEnter:     func(gui.EventCtx) { v.patch(f, v.fieldText(f)) },
		}))
	case FieldTools:
		// rendered as the matrix below
	}
	return card("", "", rows...)
}

// fieldText reads the current text of a text-ish control from its state
// mirror (the control regenerates from state every frame, so the state is
// the single source of the committed value).
func (v *settingsViewState) fieldText(f SettingsField) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.texts == nil {
		return ""
	}
	return v.texts[f.Key]
}

// rememberText mirrors committed field text so patch* reads the typed
// value (the control regenerates from state every frame).
func (v *settingsViewState) rememberText(f SettingsField, text string) {
	v.mu.Lock()
	if v.texts == nil {
		v.texts = map[string]string{}
	}
	v.texts[f.Key] = text
	v.mu.Unlock()
}

func (v *settingsViewState) currentLLM() LLMCfg {
	if v.config != nil {
		return v.config.LLM
	}
	return LLMCfg{}
}

func (v *settingsViewState) boolValue(f SettingsField) bool {
	c := v.config
	if c == nil {
		return false
	}
	switch f.Key {
	case "llamaAutoStart":
		return c.LlamaAutoStart
	case "smoothStream":
		return c.SmoothStream
	case "preferFastest":
		return c.PreferFastest
	case "showPerfHud":
		return c.ShowPerfHUD
	case "studioEnabled":
		return c.StudioEnabled
	case "visionEnabled":
		return c.VisionEnabled
	case "continuumEnabled":
		return c.ContinuumEnabled
	case "parallelTools":
		return c.ParallelTools
	case "thinkingMode":
		return c.ThinkingMode
	case "recallEnabled":
		return c.RecallEnabled
	case "labEnabled":
		return c.LabEnabled
	case "labKeepWorkspaces":
		return c.LabKeepWs
	case "labAllowNetwork":
		return c.LabAllowNetwork
	case "sandboxEnabled":
		return c.SandboxEnabled
	case "researchEnabled":
		return c.ResearchEnabled
	case "researchGitHub":
		return c.ResearchGitHub
	case "researchReddit":
		return c.ResearchReddit
	case "researchWeb":
		return c.ResearchWeb
	case "flashAttention":
		return c.FlashAttention
	case "gpuAutoOffload":
		return c.GPUAutoOffload
	case "mlock":
		return c.Mlock
	case "browserHeadless":
		return c.BrowserHeadless
	}
	return false
}

func (v *settingsViewState) intValue(f SettingsField) (int, bool) {
	c := v.config
	if c == nil {
		return 0, false
	}
	if f.LLM {
		switch f.Key {
		case "numCtx":
			return c.LLM.NumCtx, true
		case "topK":
			return c.LLM.TopK, true
		case "maxTokens":
			return c.LLM.MaxTokens, true
		case "seed":
			return c.LLM.Seed, true
		}
		return 0, false
	}
	switch f.Key {
	case "targetFps":
		return c.TargetFPS, true
	case "historyWindowPct":
		return c.HistoryWindowPct, true
	case "maxIterations":
		return c.MaxIterations, true
	case "recallTopK":
		return c.RecallTopK, true
	case "llamaPort":
		return c.LlamaPort, true
	case "sandboxCPU":
		return c.SandboxCPU, true
	case "cacheReuse":
		return c.CacheReuse, true
	case "ubatchSize":
		return c.UBatchSize, true
	case "threadsBatch":
		return c.ThreadsBatch, true
	case "maxWorkspaceMb":
		return c.MaxWorkspaceMB, true
	case "maxSessionsKept":
		return c.MaxSessionsKept, true
	case "maxLogMb":
		return c.MaxLogMB, true
	case "labCommandTimeoutSec":
		return c.LabCmdTimeoutSec, true
	case "labMaxIterations":
		return c.LabMaxIter, true
	case "runTimeoutMinutes":
		return c.RunTimeoutMin, true
	case "attachmentsBudgetKb":
		return c.AttachBudgetKB, true
	case "multiAgentDepth":
		return c.MultiAgentDepth, true
	case "researchMaxResults":
		return c.ResearchMaxRes, true
	case "researchTimeoutSec":
		return c.ResearchTimeoutS, true
	case "researchCacheTtlMin":
		return c.ResearchCacheMin, true
	case "browserSlowMoMs":
		return c.BrowserSlowMoMs, true
	}
	return 0, false
}

func (v *settingsViewState) floatValue(f SettingsField) (float64, bool) {
	c := v.config
	if c == nil {
		return 0, false
	}
	switch f.Key {
	case "temperature":
		return c.LLM.Temperature, true
	case "topP":
		return c.LLM.TopP, true
	case "minP":
		return c.LLM.MinP, true
	case "repeatPenalty":
		return c.LLM.RepeatPenalty, true
	}
	return 0, false
}

func (v *settingsViewState) stringValue(f SettingsField) string {
	c := v.config
	if c == nil {
		return ""
	}
	switch f.Key {
	case "provider":
		return c.Provider
	case "model":
		return c.Model
	case "remoteBaseUrl":
		return c.RemoteBaseURL
	case "remoteModel":
		return c.RemoteModel
	case "performanceMode":
		return c.PerformanceMode
	case "updateSchedule":
		return c.UpdateSchedule
	case "llamaBinPath":
		return c.LlamaBinPath
	case "llamaHost":
		return c.LlamaHost
	case "llamaExtraArgs":
		return c.LlamaExtraArgs
	case "llmBaseUrl":
		return c.LLMBaseURL
	case "modelsDir":
		return c.ModelsDir
	case "visionMmproj":
		return c.VisionMmproj
	case "visionMmprojOffload":
		return c.VisionOffload
	case "kvCacheQuant":
		return c.KVCacheQuant
	case "draftModel":
		return c.DraftModel
	case "sandboxMemory":
		return c.SandboxMemory
	case "labWorkspaceRoot":
		return c.LabWorkspaceRoot
	case "researchBackend":
		return c.ResearchBackend
	case "researchSearxngUrl":
		return c.ResearchSearxURL
	case "researchUserAgent":
		return c.ResearchUserAgent_
	case "browserExecutablePath":
		return c.BrowserExecPath
	}
	return ""
}

// patch applies one typed patch through the core and re-reads.
func (v *settingsViewState) patch(f SettingsField, val any) {
	ap := v.st.ap
	go func() {
		if _, err := ap.Client().PatchConfig(ap.Ctx(), BuildPatch(f, val)); err != nil {
			uiDo(func() { v.showErr("save "+f.Key, err) })
			return
		}
		if cfg, err := ap.Client().Config(ap.Ctx()); err == nil {
			uiDo(func() {
				v.mu.Lock()
				v.config = &cfg
				v.status = "Saved " + f.Label
				v.err = nil
				v.mu.Unlock()
			})
		}
	}()
}

func (v *settingsViewState) patchInt(f SettingsField, s string) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		v.showErr(f.Key, fmt.Errorf("expected an integer"))
		return
	}
	v.patch(f, n)
}

func (v *settingsViewState) patchFloat(f SettingsField, s string) {
	x, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		v.showErr(f.Key, fmt.Errorf("expected a number"))
		return
	}
	v.patch(f, x)
}

func (v *settingsViewState) showErr(what string, err error) {
	uiDo(func() {
		v.mu.Lock()
		v.err = fmt.Errorf("%s: %w", what, err)
		v.mu.Unlock()
	})
}

// buildToolsMatrix renders the enabledTools checkboxes from the catalog
// and rewrites the CSV as ONE patch (the shipped approvals contract,
// area 12's tool access matrix).
func (v *settingsViewState) buildToolsMatrix() gui.View {
	v.loadTools()
	v.mu.Lock()
	tools := append([]Tool{}, v.tools...)
	checks := map[string]bool{}
	for k, on := range v.toolChecks {
		checks[k] = on
	}
	v.mu.Unlock()

	rows := []gui.View{gui.Text(gui.TextCfg{Text: "Enabled tools", TextStyle: gui.CurrentTheme().TextStyleBody})}
	if len(tools) == 0 {
		rows = append(rows, LoadingSurface("tool catalog"))
	}
	for _, t := range tools {
		tt := t
		on := checks[tt.Name]
		rows = append(rows, gui.Row(gui.ContainerCfg{
			Sizing: gui.FillFit,
			Content: []gui.View{
				gui.Switch(gui.SwitchCfg{
					ID:       absID("settings-tool", tt.Name),
					Label:    tt.Name,
					Selected: on,
					OnClick:  func(gui.EventCtx) { v.toolPatched(tt.Name, !on) },
				}),
				wrappedText(tt.Description),
			},
		}))
	}
	return card("Tool access", "Server-side permissions still apply to every tool", rows...)
}

func (v *settingsViewState) loadTools() {
	v.mu.Lock()
	if v.toolsLoaded {
		v.mu.Unlock()
		return
	}
	v.toolsLoaded = true
	v.mu.Unlock()
	go func() {
		tools, err := v.st.ap.Client().Tools(v.st.ap.Ctx())
		uiDo(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if err != nil {
				v.err = fmt.Errorf("tool catalog: %w", err)
				return
			}
			v.tools = tools
			if v.config != nil {
				for _, t := range v.config.EnabledTools {
					v.toolChecks[t] = true
				}
			}
		})
	}()
}

func (v *settingsViewState) toolPatched(name string, on bool) {
	// Recompute the full enabled set from the current config + toggles and
	// rewrite it as one patch.
	uiDo(func() {
		v.mu.Lock()
		set := map[string]bool{}
		if v.config != nil {
			for _, t := range v.config.EnabledTools {
				set[t] = true
			}
		}
		set[name] = on
		names := make([]string, 0, len(set))
		for k, enabled := range set {
			if enabled {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		v.toolChecks[name] = on
		v.mu.Unlock()
		go func() {
			cfg, err := v.st.ap.Client().PatchConfig(v.st.ap.Ctx(), EnabledToolsPatch(names))
			if err != nil {
				v.showErr("enabled tools", err)
				return
			}
			uiDo(func() {
				v.mu.Lock()
				v.config = &cfg
				v.status = "Saved enabled tools"
				v.mu.Unlock()
			})
		}()
	})
}

// buildDiagnostics is the read-only evidence surface (health, logs).
func (v *settingsViewState) buildDiagnostics() gui.View {
	go func() {
		entries, meta, err := v.st.ap.Client().Logs(v.st.ap.Ctx(), 120)
		uiDo(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if err != nil {
				v.err = fmt.Errorf("logs: %w", err)
				return
			}
			var b strings.Builder
			if meta.Dir != "" {
				b.WriteString(fmt.Sprintf("Log folder: %s", meta.Dir))
				if meta.SinkHealth.FallbackReason != "" {
					b.WriteString(" (fallback active)")
				}
				if !meta.SinkHealth.SinksReady {
					b.WriteString(" (sink problem — events may not persist)")
				}
				b.WriteString("\n\n")
			}
			for _, e := range entries {
				b.WriteString(fmt.Sprintf("%s [%s] %s: %s\n", e.TS, strings.ToUpper(e.Level), e.Sub, e.Msg))
			}
			v.diagText = b.String()
		})
	}()
	v.mu.Lock()
	text := v.diagText
	v.mu.Unlock()
	if text == "" {
		return card("Recent log entries", "Redacted by the core", LoadingSurface("logs"))
	}
	return card("Recent log entries", "Redacted by the core", codeText(text))
}

func (v *settingsViewState) load() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()
	ap := v.st.ap
	go func() {
		cfg, err := ap.Client().Config(ap.Ctx())
		uiDo(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if err != nil {
				v.err = fmt.Errorf("config: %w", err)
				return
			}
			v.config = &cfg
			v.status = fmt.Sprintf("Loaded configuration (model: %s)", cfg.Model)
			v.err = nil
		})
	}()
}
