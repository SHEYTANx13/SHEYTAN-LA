//go:build !headless

// view_settings.go — the Settings view: the 8-section IA driven by the
// typed schema (settings_schema.go), one control per FIELD_PLACEMENT row,
// patches through the core's authoritative deep-merge path and a re-read
// after every change so the UI can never drift from persisted state.
// Includes the enabled-tools matrix from GET /api/tools and the
// read-only diagnostics evidence card.
package fyneui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type settingsView struct {
	ap         *App
	sections   *container.AppTabs
	config     *Config
	tools      []Tool
	toolChecks map[string]*widget.Check
	errLabel   *widget.Label
	status     *widget.Label
}

func buildSettingsView(ap *App) fyne.CanvasObject {
	v := &settingsView{ap: ap, toolChecks: map[string]*widget.Check{}}
	v.errLabel = widget.NewLabel("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Wrapping = fyne.TextWrapWord
	v.errLabel.Hide()
	v.status = wrappedLabel("")

	tabs := container.NewAppTabs()
	for _, sec := range SettingsSections {
		sec := sec
		if sec.ID == "diagnostics" {
			tabs.Append(container.NewTabItem(sec.Label, v.buildDiagnostics()))
			continue
		}
		tabs.Append(container.NewTabItem(sec.Label, v.buildSection(sec.ID)))
	}

	root := container.NewBorder(
		container.NewVBox(PageHeader("Settings", "Every field patches the core schema and re-reads the saved state"), v.status),
		v.errLabel, nil, nil,
		tabs,
	)
	v.load()
	return root
}

func (v *settingsView) buildSection(id string) fyne.CanvasObject {
	rows := container.NewVBox()
	for _, f := range FieldsForSection(id) {
		rows.Add(v.controlFor(f))
	}
	if id == "agent" {
		rows.Add(v.buildToolsMatrix())
	}
	return container.NewScroll(rows)
}

func (v *settingsView) controlFor(f SettingsField) fyne.CanvasObject {
	label := widget.NewLabelWithStyle(f.Label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	hintText := f.Hint
	if hintText == "" {
		hintText = f.Key
	}
	hint := wrappedLabel(hintText)
	row := container.NewVBox(label, hint)

	switch f.Kind {
	case FieldBool:
		c := widget.NewCheck("", func(on bool) { v.patch(f, on) })
		row.Add(c)
		v.bindBool(f, c)
	case FieldInt:
		e := widget.NewEntry()
		e.SetPlaceHolder("integer")
		e.OnSubmitted = func(s string) { v.patchInt(f, s) }
		row.Add(e)
		v.bindInt(f, e)
	case FieldFloat:
		e := widget.NewEntry()
		e.SetPlaceHolder("number")
		e.OnSubmitted = func(s string) { v.patchFloat(f, s) }
		row.Add(e)
		v.bindFloat(f, e)
	case FieldEnum:
		s := widget.NewSelect(f.Enum, func(sel string) { v.patch(f, sel) })
		row.Add(s)
		v.bindEnum(f, s)
	case FieldString:
		e := widget.NewEntry()
		e.OnSubmitted = func(s string) { v.patch(f, s) }
		row.Add(e)
		v.bindString(f, e)
	case FieldTools:
	}
	return widget.NewCard("", "", row)
}

func (v *settingsView) currentLLM() LLMCfg {
	if v.config != nil {
		return v.config.LLM
	}
	return LLMCfg{}
}

func (v *settingsView) bindBool(f SettingsField, c *widget.Check) {
	go func() {
		v.waitConfig()
		uiDo(func() { c.SetChecked(v.boolValue(f)) })
	}()
}

func (v *settingsView) bindInt(f SettingsField, e *widget.Entry) {
	go func() {
		v.waitConfig()
		uiDo(func() {
			if n, ok := v.intValue(f); ok {
				e.SetText(fmt.Sprintf("%d", n))
			}
		})
	}()
}

func (v *settingsView) bindFloat(f SettingsField, e *widget.Entry) {
	go func() {
		v.waitConfig()
		uiDo(func() {
			if f, ok := v.floatValue(f); ok {
				e.SetText(fmt.Sprintf("%g", f))
			}
		})
	}()
}

func (v *settingsView) bindEnum(f SettingsField, s *widget.Select) {
	go func() {
		v.waitConfig()
		uiDo(func() { s.SetSelected(v.stringValue(f)) })
	}()
}

func (v *settingsView) bindString(f SettingsField, e *widget.Entry) {
	go func() {
		v.waitConfig()
		uiDo(func() { e.SetText(v.stringValue(f)) })
	}()
}

func (v *settingsView) waitConfig() {
	for i := 0; i < 100 && v.config == nil; i++ {
		time.Sleep(50 * time.Millisecond)
	}
}

func (v *settingsView) boolValue(f SettingsField) bool {
	c := v.config
	if c == nil {
		return false
	}
	switch f.Key {
	case "llamaAutoStart":
		return c.LlamaAutoStart
	case "smoothStream":
		return c.SmoothStream
	case "showPerfHud":
		return c.ShowPerfHUD
	case "developerDebug":
		return c.DeveloperDebug
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
	case "webWorkspaceEnabled":
		return c.WebWorkspaceEnabled
	case "studioEnabled":
		return c.StudioEnabled
	}
	return false
}

func (v *settingsView) intValue(f SettingsField) (int, bool) {
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

func (v *settingsView) floatValue(f SettingsField) (float64, bool) {
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

func (v *settingsView) stringValue(f SettingsField) string {
	c := v.config
	if c == nil {
		return ""
	}
	if f.LLM {
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

func (v *settingsView) patch(f SettingsField, val any) {
	ap := v.ap
	go func() {
		patch := BuildPatch(f, val)
		if _, err := ap.Client().PatchConfig(ap.Ctx(), patch); err != nil {
			uiDo(func() { v.showErr("save "+f.Key, err) })
			return
		}
		if f.Key == "developerDebug" {
			if on, ok := val.(bool); ok {
				setDeveloperDebugLive(on)
			}
		}
		if cfg, err := ap.Client().Config(ap.Ctx()); err == nil {
			uiDo(func() {
				v.config = &cfg
				v.status.SetText("Saved " + f.Label)
				v.errLabel.Hide()
			})
		}
	}()
}

func (v *settingsView) patchInt(f SettingsField, s string) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		v.showErr(f.Key, fmt.Errorf("expected an integer"))
		return
	}
	v.patch(f, n)
}

func (v *settingsView) patchFloat(f SettingsField, s string) {
	x, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		v.showErr(f.Key, fmt.Errorf("expected a number"))
		return
	}
	v.patch(f, x)
}

func (v *settingsView) buildToolsMatrix() fyne.CanvasObject {
	box := container.NewVBox(widget.NewLabelWithStyle("Enabled tools", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	go func() {
		tools, err := v.ap.Client().Tools(v.ap.Ctx())
		if err != nil {
			uiDo(func() { v.showErr("tool catalog", err) })
			return
		}
		uiDo(func() {
			v.tools = tools
			enabled := map[string]bool{}
			if v.config != nil {
				for _, t := range v.config.EnabledTools {
					enabled[t] = true
				}
			}
			for _, t := range tools {
				tt := t
				c := widget.NewCheck(tt.Name, func(on bool) { v.toolsPatched(tt.Name, on) })
				c.SetChecked(enabled[tt.Name])
				v.toolChecks[tt.Name] = c
				desc := wrappedLabel(tt.Description)
				box.Add(container.NewHBox(c, desc))
			}
			box.Refresh()
		})
	}()
	return widget.NewCard("Tool access", "Server-side permissions still apply to every tool", box)
}

func (v *settingsView) toolsPatched(name string, on bool) {
	set := map[string]bool{}
	if v.config != nil {
		for _, t := range v.config.EnabledTools {
			set[t] = true
		}
	}
	set[name] = on
	names := make([]string, 0, len(set))
	for k := range set {
		if set[k] {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	go func() {
		cfg, err := v.ap.Client().PatchConfig(v.ap.Ctx(), EnabledToolsPatch(names))
		if err != nil {
			uiDo(func() { v.showErr("enabled tools", err) })
			return
		}
		uiDo(func() {
			v.config = &cfg
			v.status.SetText("Saved enabled tools")
		})
	}()
}

func (v *settingsView) buildDiagnostics() fyne.CanvasObject {
	logs := wrappedLabel("Loading logs…")
	go func() {
		entries, err := v.ap.Client().Logs(v.ap.Ctx(), 120)
		if err != nil {
			uiDo(func() { v.showErr("logs", err) })
			return
		}
		var b strings.Builder
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("%s [%s] %s: %s\n", e.TS, strings.ToUpper(e.Level), e.Sub, e.Msg))
		}
		uiDo(func() { logs.SetText(b.String()) })
	}()
	return container.NewScroll(widget.NewCard("Recent log entries", "Redacted by the core", logs))
}

func (v *settingsView) load() {
	ap := v.ap
	go func() {
		cfg, err := ap.Client().Config(ap.Ctx())
		if err != nil {
			uiDo(func() { v.showErr("config", err) })
			return
		}
		uiDo(func() {
			v.config = &cfg
			v.status.SetText(fmt.Sprintf("Loaded configuration (model: %s)", cfg.Model))
			v.errLabel.Hide()
		})
	}()
}

func (v *settingsView) showErr(what string, err error) {
	v.errLabel.SetText(what + ": " + err.Error())
	v.errLabel.Show()
}
