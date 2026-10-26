//go:build !headless

// Settings schema — the ONE declarative registry for the Settings surface,
// ported verbatim from the retired src/settings-sections.ts FIELD_PLACEMENT
// (v1.3.0 IA: user-outcome sections; expert fields quarantined in
// Advanced; automated fields never rendered as controls). The core config
// schema (internal/config) remains the only authority for values, defaults
// and validation; this registry decides WHERE a field is edited and its
// wire type.
package fyneui

// SettingsSection mirrors the 8-section IA.
type SettingsSection struct {
	ID    string
	Label string
	Hint  string
}

// SettingsSections in display order.
var SettingsSections = []SettingsSection{
	{"general", "General", "Startup, workspace, interaction"},
	{"models", "Models", "Local models, provider, vision"},
	{"performance", "Performance", "Quiet, Balanced, Maximum"},
	{"agent", "Agent & Tools", "Tools, permissions, sandbox"},
	{"network", "Network", "Research, remote provider"},
	{"updates", "Updates", "Check, policy, version"},
	{"diagnostics", "Diagnostics", "Health, hardware, logs"},
	{"advanced", "Advanced", "Expert controls"},
}

// FieldKind is the wire type of a settings field.
type FieldKind int

const (
	FieldBool FieldKind = iota
	FieldInt
	FieldFloat
	FieldString
	FieldEnum
	FieldTools
)

// SettingsField describes one editable control.
type SettingsField struct {
	Key     string // config JSON path ("temperature" lives under llm)
	Label   string
	Section string
	Kind    FieldKind
	Enum    []string // FieldEnum choices
	LLM     bool     // nested under the llm object (deep-merged)
	Hint    string
}

// SettingsFields is FIELD_PLACEMENT + type info. Order within a section is
// display order.
var SettingsFields = []SettingsField{
	// General
	{Key: "llamaAutoStart", Label: "Start model engine with the app", Section: "general", Kind: FieldBool},
	{Key: "smoothStream", Label: "Smooth streaming animation", Section: "general", Kind: FieldBool},
	{Key: "showPerfHud", Label: "Show performance overlay", Section: "general", Kind: FieldBool},
	{Key: "targetFps", Label: "Target frame rate", Section: "general", Kind: FieldInt},

	// Models
	{Key: "provider", Label: "Provider", Section: "models", Kind: FieldEnum, Enum: []string{"local", "remote"}},
	{Key: "model", Label: "Model (selection belongs in the Models view)", Section: "models", Kind: FieldString},
	{Key: "remoteBaseUrl", Label: "Remote base URL", Section: "models", Kind: FieldString},
	{Key: "remoteModel", Label: "Remote model", Section: "models", Kind: FieldString},
	{Key: "visionEnabled", Label: "Vision (multimodal input)", Section: "models", Kind: FieldBool},

	// Performance
	{Key: "numCtx", Label: "Context window (tokens)", Section: "performance", Kind: FieldInt, LLM: true},
	{Key: "historyWindowPct", Label: "History window (%)", Section: "performance", Kind: FieldInt},
	{Key: "continuumEnabled", Label: "Continuum context carry", Section: "performance", Kind: FieldBool},
	{Key: "performanceMode", Label: "Runtime posture", Section: "performance", Kind: FieldEnum, Enum: []string{"auto", "manual"}},

	// Agent & Tools
	{Key: "maxIterations", Label: "Max agent iterations", Section: "agent", Kind: FieldInt},
	{Key: "parallelTools", Label: "Run tools in parallel", Section: "agent", Kind: FieldBool},
	{Key: "thinkingMode", Label: "Thinking output", Section: "agent", Kind: FieldBool},
	{Key: "recallEnabled", Label: "Memory recall", Section: "agent", Kind: FieldBool},
	{Key: "recallTopK", Label: "Recall top-K", Section: "agent", Kind: FieldInt},
	{Key: "enabledTools", Label: "Enabled tools", Section: "agent", Kind: FieldTools},
	{Key: "labEnabled", Label: "Coding Lab", Section: "agent", Kind: FieldBool},
	{Key: "labKeepWorkspaces", Label: "Keep Lab workspaces", Section: "agent", Kind: FieldBool},
	{Key: "labAllowNetwork", Label: "Allow Lab network access", Section: "agent", Kind: FieldBool},
	{Key: "sandboxEnabled", Label: "Sandbox tool execution", Section: "agent", Kind: FieldBool},

	// Network
	{Key: "researchEnabled", Label: "Research / net search", Section: "network", Kind: FieldBool},
	{Key: "researchBackend", Label: "Research backend", Section: "network", Kind: FieldString},
	{Key: "researchSearxngUrl", Label: "SearXNG URL", Section: "network", Kind: FieldString},
	{Key: "researchMaxResults", Label: "Max results", Section: "network", Kind: FieldInt},
	{Key: "researchGitHub", Label: "GitHub search", Section: "network", Kind: FieldBool},
	{Key: "researchReddit", Label: "Reddit search", Section: "network", Kind: FieldBool},
	{Key: "researchWeb", Label: "Web search", Section: "network", Kind: FieldBool},

	// Updates
	{Key: "updateSchedule", Label: "Automatic update policy", Section: "updates", Kind: FieldEnum,
		Enum: []string{"off", "daily", "weekly"}},

	// Advanced — the ONLY expert surface (ported placement)
	{Key: "llamaBinPath", Label: "llama.cpp binary path", Section: "advanced", Kind: FieldString},
	{Key: "llamaHost", Label: "llama host", Section: "advanced", Kind: FieldString},
	{Key: "llamaPort", Label: "llama port", Section: "advanced", Kind: FieldInt},
	{Key: "llamaExtraArgs", Label: "llama extra args", Section: "advanced", Kind: FieldString},
	{Key: "llmBaseUrl", Label: "LLM base URL", Section: "advanced", Kind: FieldString},
	{Key: "modelsDir", Label: "Models directory", Section: "advanced", Kind: FieldString},
	{Key: "visionMmproj", Label: "Vision projector path", Section: "advanced", Kind: FieldString},
	{Key: "visionMmprojOffload", Label: "Projector offload", Section: "advanced", Kind: FieldEnum, Enum: []string{"auto", "on", "off"}},
	{Key: "flashAttention", Label: "Flash attention", Section: "advanced", Kind: FieldBool},
	{Key: "gpuAutoOffload", Label: "GPU auto offload", Section: "advanced", Kind: FieldBool},
	{Key: "mlock", Label: "Lock model in RAM", Section: "advanced", Kind: FieldBool},
	{Key: "cacheReuse", Label: "Cache reuse", Section: "advanced", Kind: FieldInt},
	{Key: "ubatchSize", Label: "Micro-batch size", Section: "advanced", Kind: FieldInt},
	{Key: "threadsBatch", Label: "Batch threads", Section: "advanced", Kind: FieldInt},
	{Key: "kvCacheQuant", Label: "KV cache quantization", Section: "advanced", Kind: FieldString},
	{Key: "draftModel", Label: "Draft model", Section: "advanced", Kind: FieldString},
	{Key: "sandboxMemory", Label: "Sandbox memory limit", Section: "advanced", Kind: FieldString},
	{Key: "sandboxCPU", Label: "Sandbox CPU cores", Section: "advanced", Kind: FieldInt},
	{Key: "maxWorkspaceMb", Label: "Workspace budget (MB)", Section: "advanced", Kind: FieldInt},
	{Key: "maxSessionsKept", Label: "Sessions kept", Section: "advanced", Kind: FieldInt},
	{Key: "maxLogMb", Label: "Log budget (MB)", Section: "advanced", Kind: FieldInt},
	{Key: "labWorkspaceRoot", Label: "Lab workspace root", Section: "advanced", Kind: FieldString},
	{Key: "labCommandTimeoutSec", Label: "Lab command timeout (s)", Section: "advanced", Kind: FieldInt},
	{Key: "labMaxIterations", Label: "Lab max iterations", Section: "advanced", Kind: FieldInt},
	{Key: "runTimeoutMinutes", Label: "Run timeout (minutes)", Section: "advanced", Kind: FieldInt},
	{Key: "attachmentsBudgetKb", Label: "Attachments budget (KB)", Section: "advanced", Kind: FieldInt},
	{Key: "multiAgentDepth", Label: "Multi-agent depth", Section: "advanced", Kind: FieldInt},
	{Key: "researchTimeoutSec", Label: "Research timeout (s)", Section: "advanced", Kind: FieldInt},
	{Key: "researchCacheTtlMin", Label: "Research cache TTL (min)", Section: "advanced", Kind: FieldInt},
	{Key: "researchUserAgent", Label: "Research user agent", Section: "advanced", Kind: FieldString},
	{Key: "browserExecutablePath", Label: "Browser executable", Section: "advanced", Kind: FieldString},
	{Key: "browserSlowMoMs", Label: "Browser slow-motion (ms)", Section: "advanced", Kind: FieldInt},
	{Key: "browserHeadless", Label: "Headless browser", Section: "advanced", Kind: FieldBool},
	{Key: "temperature", Label: "Temperature", Section: "advanced", Kind: FieldFloat, LLM: true},
	{Key: "topP", Label: "Top-P", Section: "advanced", Kind: FieldFloat, LLM: true},
	{Key: "topK", Label: "Top-K", Section: "advanced", Kind: FieldInt, LLM: true},
	{Key: "minP", Label: "Min-P", Section: "advanced", Kind: FieldFloat, LLM: true},
	{Key: "maxTokens", Label: "Max tokens", Section: "advanced", Kind: FieldInt, LLM: true},
	{Key: "repeatPenalty", Label: "Repeat penalty", Section: "advanced", Kind: FieldFloat, LLM: true},
	{Key: "seed", Label: "Seed", Section: "advanced", Kind: FieldInt, LLM: true},
}

// AutomatedFields are implementation knobs the runtime manages; they are
// reported as read-only evidence and never rendered as controls.
var AutomatedFields = []string{
	"engineCompat", "engineCompatReason", "engineCompatAt",
	"visionMmprojDetection", "engineBackend", "nativeEnginePath",
}

// FieldsForSection returns the fields of one section in display order.
func FieldsForSection(id string) []SettingsField {
	out := make([]SettingsField, 0, 8)
	for _, f := range SettingsFields {
		if f.Section == id {
			out = append(out, f)
		}
	}
	return out
}

// BuildPatch produces the wire patch for one field: nested llm.* fields go
// under the deep-merged "llm" object; everything else is top-level. This
// is the exact typed-merge contract the core enforces (flat llm keys are
// silently dropped by the typed merge).
func BuildPatch(f SettingsField, value any) map[string]any {
	if !f.LLM {
		return map[string]any{f.Key: value}
	}
	return map[string]any{"llm": map[string]any{f.Key: value}}
}

// EnabledToolsPatch builds the one-patch rewrite of the enabledTools CSV.
func EnabledToolsPatch(enabled []string) map[string]any {
	return map[string]any{"enabledTools": enabled}
}
