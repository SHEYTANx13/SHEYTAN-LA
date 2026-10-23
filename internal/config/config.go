// Package config holds runtime configuration for SHEYTAN-Local-Agent.
//
// The configuration surface covers the autonomous Coding Lab (isolated
// workspaces, controlled execution, verification) and the web research
// engine, alongside the core runtime settings.
package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	AppName    = "SHEYTAN-Local-Agent"
	AppVersion = "1.10.15"
)

// The product identity is version-only: AppName + AppVersion (synchronized
// from package.json by scripts/release-version.mjs). There is deliberately
// no codename constant — every release surface derives from these two
// values and package.json is the single source of truth.

// v1.2.0 product identity: the short product name (SHEYTAN-LA) used on
// window titles, taskbars, notifications, installers and update artifacts.
// The full name stays the canonical config identity; the short name is the
// product face.
const (
	AppShortName   = "SHEYTAN-LA"
	AppDescription = "SHEYTAN Local Agent"
	AppUserModelID = "Parsaetak.SHEYTAN-LA"
	AppPublisher   = "Parsaetak"
	ExecutableName = "SHEYTAN-LA.exe"
)

// legacyDefaultLLMBaseURL is the legacy hardcoded local endpoint. Configs
// still carrying this exact value are migrated to "follow the managed
// engine port" (see EffectiveBaseURL) so a moved llama.cpp server never
// breaks an old installation.
const legacyDefaultLLMBaseURL = "http://127.0.0.1:8080/v1"

// Provider kinds.
const (
	ProviderLocal  = "local"
	ProviderRemote = "remote"
)

// Engine backend kinds.
const (
	// BackendLlama is the managed llama.cpp engine backend — the
	// complete, shipping inference engine and the default.
	BackendLlama = "llama"
	// BackendNative is the SHEYTAN Native Engine backend — Phase 1
	// implements the architecture, supervised lifecycle, health and
	// metrics; generation still falls back to llama.cpp.
	BackendNative = "native"
)

// Performance-mode constants (v1.5.0). The mode decides who owns the
// runtime profile: AUTO derives it from measured evidence (the ONE
// recommendation engine) on every model change; MANUAL preserves the
// user's explicit values verbatim.
const (
	// ModeAuto lets SHEYTAN calculate and apply the runtime profile
	// automatically (the default).
	ModeAuto = "auto"
	// ModeManual preserves explicit user values; automatic profile
	// application is suppressed.
	ModeManual = "manual"
)

// EffectivePerformanceMode returns the normalized performance mode.
// Anything that is not exactly MANUAL resolves to AUTO — the default
// posture — so a corrupt or hand-edited value can never silently
// disable automatic configuration.
func (c *Config) EffectivePerformanceMode() string {
	if c == nil || c.PerformanceMode != ModeManual {
		return ModeAuto
	}
	return ModeManual
}

// LLMOptions mirrors the available local/remote sampling and runtime knobs.
type LLMOptions struct {
	Temperature      float64 `json:"temperature" yaml:"temperature"`
	TopP             float64 `json:"topP" yaml:"top_p"`
	TopK             int     `json:"topK" yaml:"top_k"`
	MinP             float64 `json:"minP" yaml:"min_p"`
	MaxTokens        int     `json:"maxTokens" yaml:"max_tokens"`
	Stop             string  `json:"stop" yaml:"stop"`
	Seed             int     `json:"seed" yaml:"seed"`
	RepeatPenalty    float64 `json:"repeatPenalty" yaml:"repeat_penalty"`
	RepeatLastN      int     `json:"repeatLastN" yaml:"repeat_last_n"`
	PresencePenalty  float64 `json:"presencePenalty" yaml:"presence_penalty"`
	FrequencyPenalty float64 `json:"frequencyPenalty" yaml:"frequency_penalty"`
	Mirostat         int     `json:"mirostat" yaml:"mirostat"`
	MirostatTau      float64 `json:"mirostatTau" yaml:"mirostat_tau"`
	MirostatEta      float64 `json:"mirostatEta" yaml:"mirostat_eta"`
	NumCtx           int     `json:"numCtx" yaml:"num_ctx"`
	NumBatch         int     `json:"numBatch" yaml:"num_batch"`
	NumGPU           int     `json:"numGpu" yaml:"num_gpu"`
	NumThread        int     `json:"numThread" yaml:"num_thread"`
	Stream           bool    `json:"stream" yaml:"stream"`
	Preset           string  `json:"preset" yaml:"preset"`
}

// Config is the top-level runtime configuration.
type Config struct {
	// Portable application data.
	DataDir     string `json:"dataDir" yaml:"dataDir"`
	ModelsDir   string `json:"modelsDir" yaml:"modelsDir"`
	SessionsDir string `json:"sessionsDir" yaml:"sessionsDir"`

	// PathNotes (v1.3.0) carries the normalization/migration reports
	// produced by Load — every rejected literal "%TOKEN%" path, every
	// fallback to the canonical root. It is transient (never
	// serialized, never persisted): the command layer drains it into
	// the log catcher once logging is live, because Load can run
	// before the log manager boots.
	PathNotes []string `json:"-" yaml:"-"`

	// SamplingNotes (v1.6.1) carries the sampling-value repairs Load
	// performed — every parser-invalid repeatPenalty/temperature/…
	// reset back to its documented default (the P0 defect: a zero
	// repeat penalty reached the engine and fed the compatibility
	// ladder). Transient like PathNotes: drained into the log catcher
	// by the command layer, never serialized.
	SamplingNotes []string `json:"-" yaml:"-"`

	// HTTP/API server.
	Host string `json:"host" yaml:"host"`
	Port int    `json:"port" yaml:"port"`

	// Local LLM endpoint.
	LLMBaseURL string `json:"llmBaseUrl" yaml:"llmBaseUrl"`
	Model      string `json:"model" yaml:"model"`

	// Remote OpenAI-compatible provider.
	Provider      string `json:"provider" yaml:"provider"`
	RemoteBaseURL string `json:"remoteBaseUrl" yaml:"remoteBaseUrl"`
	RemoteAPIKey  string `json:"remoteApiKey" yaml:"remoteApiKey"`
	RemoteModel   string `json:"remoteModel" yaml:"remoteModel"`

	// llama.cpp subprocess.
	LlamaBinPath   string `json:"llamaBinPath" yaml:"llamaBinPath"`
	LlamaHost      string `json:"llamaHost" yaml:"llamaHost"`
	LlamaPort      int    `json:"llamaPort" yaml:"llamaPort"`
	LlamaAutoStart bool   `json:"llamaAutoStart" yaml:"llamaAutoStart"`
	LlamaExtraArgs string `json:"llamaExtraArgs" yaml:"llamaExtraArgs"`

	// Engine backend selection (v1.1.5 Phase 1).
	//
	//   "llama" — managed llama.cpp subprocess (default; complete
	//             inference engine)
	//   "native" — SHEYTAN Native Engine path: the supervised
	//             shtn-engine-host subprocess is started with real
	//             lifecycle/health/metrics. Since Phase 5 the native
	//             backend performs REAL generation for validated
	//             llama-architecture models (plain-text requests);
	//             anything it cannot serve still falls back to the
	//             llama backend.
	//
	// The default preserves v1.1.4 behavior exactly: no native
	// subprocess is spawned unless the user explicitly opts in.
	EngineBackend string `json:"engineBackend" yaml:"engineBackend"`

	// NativeEnginePath optionally overrides where the shtn-engine-host
	// binary is looked up. Empty = {DataDir}/bin/shtn-engine-host(.exe).
	// Phase 1 does not download or build the native engine; the binary
	// must be provided (e.g. built from native/engine with CMake).
	NativeEnginePath string `json:"nativeEnginePath" yaml:"nativeEnginePath"`

	// Agent loop.
	MaxIterations int  `json:"maxIterations" yaml:"maxIterations"`
	ParallelTools bool `json:"parallelTools" yaml:"parallelTools"`

	// RunTimeoutMinutes bounds one agent turn end-to-end (v1.1.4). Zero
	// disables the budget; the default of 60 prevents a pathological
	// turn (maxIterations x retrying LLM calls) from running for hours.
	RunTimeoutMinutes int `json:"runTimeoutMinutes" yaml:"runTimeoutMinutes"`

	// Sandbox governor controls (v1.1.4: now actually wired - previously
	// stored but ignored by the runtime). Memory accepts "512m"/"512"/
	// "1g" strings; CPU is a percent reserved for spawned code.
	SandboxEnabled bool   `json:"sandboxEnabled" yaml:"sandboxEnabled"`
	SandboxMemory  string `json:"sandboxMemory" yaml:"sandboxMemory"`
	SandboxCPU     int    `json:"sandboxCPU" yaml:"sandboxCPU"`

	// Sampling.
	LLM LLMOptions `json:"llm" yaml:"llm"`

	// Browser automation.
	BrowserExecutablePath string `json:"browserExecutablePath" yaml:"browserExecutablePath"`
	BrowserHeadless       bool   `json:"browserHeadless" yaml:"browserHeadless"`
	BrowserSlowMo         int    `json:"browserSlowMoMs" yaml:"browserSlowMoMs"`

	// Experience and maintenance.
	UpdateSchedule  string `json:"updateSchedule" yaml:"updateSchedule"`
	LastUpdateCheck string `json:"lastUpdateCheck" yaml:"lastUpdateCheck"`

	// Thinking, tools, attachments, history, recall.
	ThinkingMode        bool     `json:"thinkingMode" yaml:"thinkingMode"`
	EnabledTools        []string `json:"enabledTools" yaml:"enabledTools"`
	AttachmentsBudgetKB int      `json:"attachmentsBudgetKb" yaml:"attachmentsBudgetKb"`
	HistoryWindowPct    int      `json:"historyWindowPct" yaml:"historyWindowPct"`
	RecallEnabled       bool     `json:"recallEnabled" yaml:"recallEnabled"`
	RecallTopK          int      `json:"recallTopK" yaml:"recallTopK"`

	// Local engine tuning.
	GPUAutoOffload bool `json:"gpuAutoOffload" yaml:"gpuAutoOffload"`
	// GPUAutoOffloadUserSet (v1.8.4) records that the gpuAutoOffload
	// posture was set by an EXPLICIT user action (the settings toggle
	// or a direct config patch), not derived by the recommendation /
	// posture pipeline. Only a user-set posture is treated as an
	// explicit OFF that AUTO must respect; a derived CPU posture
	// (the pre-1.8.4 recommendation wrote gpuAutoOffload=false while
	// no Vulkan engine backend was installed — "until a Vulkan engine
	// build is provisioned") is exactly the stale state that
	// permanently blocked the AUTO Vulkan candidate, and is repaired
	// once at load (see repairDerivedGPUPosture). Absent (false) in
	// pre-1.8.4 configs: the migration cannot misfire onto a config
	// that never carried a derived posture, and an explicit user OFF
	// survives forever after the first v1.8.4+ toggle save.
	GPUAutoOffloadUserSet bool `json:"gpuAutoOffloadUserSet,omitempty" yaml:"gpuAutoOffloadUserSet,omitempty"`
	EngineCompat          int  `json:"engineCompat" yaml:"engineCompat"`
	// v1.1.7: WHY the engine last settled above compatibility level 0.
	// Recorded when a boot succeeds at level > 0, cleared when it
	// succeeds at level 0, and surfaced through /api/perf so the UI can
	// explain the downgrade instead of hiding it.
	EngineCompatReason string `json:"engineCompatReason,omitempty" yaml:"engineCompatReason,omitempty"`
	// v1.1.7: RFC3339 time the compatibility reason was recorded. The
	// retry-up gate compares it against the capability profile's
	// VerifiedAt so a repaired profile earns exactly one bounded
	// full-speed retry instead of a rediscovery attempt every boot.
	EngineCompatAt string `json:"engineCompatAt,omitempty" yaml:"engineCompatAt,omitempty"`
	FlashAttention bool   `json:"flashAttention" yaml:"flashAttention"`
	CacheReuse     int    `json:"cacheReuse" yaml:"cacheReuse"`
	UBatchSize     int    `json:"ubatchSize" yaml:"ubatchSize"`
	ThreadsBatch   int    `json:"threadsBatch" yaml:"threadsBatch"`
	KVCacheQuant   string `json:"kvCacheQuant" yaml:"kvCacheQuant"`
	Mlock          bool   `json:"mlock" yaml:"mlock"`
	DraftModel     string `json:"draftModel" yaml:"draftModel"`
	ShowPerfHUD    bool   `json:"showPerfHud" yaml:"showPerfHud"`

	// Vision.
	VisionEnabled bool   `json:"visionEnabled" yaml:"visionEnabled"`
	VisionMMProj  string `json:"visionMmproj" yaml:"visionMmproj"`

	// VisionMMProjOffload (v1.2.0) is the projector GPU-offload posture:
	// "auto" (engine default), "on", or "off" (appends
	// --no-mmproj-offload, verified by the capability adapter).
	VisionMMProjOffload string `json:"visionMmprojOffload" yaml:"visionMmprojOffload"`

	// RuntimeProfile (v1.2.0) names the active task-aware runtime
	// profile (chat | coding | research | vision | agent | low-power |
	// maximum). It records what the user selected so the UI can show
	// WHY the recommended values were applied; it never changes engine
	// behaviour on its own — the applied values live in the LLM/GPU
	// fields above.
	RuntimeProfile string `json:"runtimeProfile" yaml:"runtimeProfile"`

	// PerformanceMode (v1.5.0) is the automatic-performance contract:
	//
	//   "auto"   — SHEYTAN calculates and applies the runtime profile
	//              from measured hardware + engine capability + the
	//              selected model (internal/recommendation — the ONE
	//              tuning engine), recalculated on every model change,
	//              and may run a bounded real calibration whose verified
	//              winner is retained.
	//   "manual" — explicit user values are preserved verbatim; model
	//              changes never silently overwrite manual settings.
	//
	// The default is "auto"; invalid values normalize to "auto" on load.
	PerformanceMode string `json:"performanceMode" yaml:"performanceMode"`

	// PreviousModel (v1.5.0) records the last-but-one selected model —
	// the Model Selector's "previous selection" marker. It is updated
	// by the model-selection flow when the user switches models and is
	// purely informational (never resolved or loaded).
	PreviousModel string `json:"previousModel,omitempty" yaml:"previousModel,omitempty"`

	// Accelerator (v1.2.6) is the REQUESTED accelerator profile:
	// "auto" (default, evidence-based) | "gpu" | "npu" | "cpu".
	// The RESOLVED backend/device (with its measured reason) is served
	// by /api/perf — the request alone never claims anything.
	Accelerator string `json:"accelerator" yaml:"accelerator"`

	// WorkspaceRoot (v1.2.4) is the user's CURRENT PROJECT FOLDER — the
	// root the agent's file/shell/git tools and the project-intelligence
	// card operate against. Empty means the legacy scratch workspace
	// (DataDir/workspace); EffectiveWorkspaceRoot resolves it. Switching
	// is explicit (UI action or config edit), persisted, and surfaced in
	// RecentWorkspaces.
	WorkspaceRoot string `json:"workspaceRoot" yaml:"workspaceRoot"`

	// RecentWorkspaces (v1.2.4) remembers the last few workspace roots
	// for one-click switching. Oldest entries are evicted past
	// maxRecentWorkspaces.
	RecentWorkspaces []string `json:"recentWorkspaces" yaml:"recentWorkspaces"`

	// Resource retention.
	MaxWorkspaceMB  int `json:"maxWorkspaceMb" yaml:"maxWorkspaceMb"`
	MaxSessionsKept int `json:"maxSessionsKept" yaml:"maxSessionsKept"`
	MaxLogMB        int `json:"maxLogMb" yaml:"maxLogMb"`

	// Multi-agent pipeline.
	MultiAgentDepth int `json:"multiAgentDepth" yaml:"multiAgentDepth"`

	// Continuum context.
	ContinuumEnabled         bool `json:"continuumEnabled" yaml:"continuumEnabled"`
	ContinuumThresholdPct    int  `json:"continuumThresholdPct" yaml:"continuumThresholdPct"`
	ContinuumCarryMessages   int  `json:"continuumCarryMessages" yaml:"continuumCarryMessages"`
	ContinuumFrameworkTokens int  `json:"continuumFrameworkTokens" yaml:"continuumFrameworkTokens"`

	// Streaming/UI frame pacing.
	SmoothStream bool `json:"smoothStream" yaml:"smoothStream"`
	TargetFPS    int  `json:"targetFps" yaml:"targetFps"`

	// ---------------------------------------------------------------------
	// Autonomous Coding Lab.
	// ---------------------------------------------------------------------

	// LabEnabled enables the autonomous coding-laboratory subsystem.
	LabEnabled bool `json:"labEnabled" yaml:"labEnabled"`

	// LabWorkspaceRoot is the root directory under which disposable coding
	// workspaces are created.
	LabWorkspaceRoot string `json:"labWorkspaceRoot" yaml:"labWorkspaceRoot"`

	// LabCommandTimeoutSec limits one laboratory process execution.
	LabCommandTimeoutSec int `json:"labCommandTimeoutSec" yaml:"labCommandTimeoutSec"`

	// LabMaxIterations controls autonomous repair/verification iterations.
	// If <= 0, MaxIterations is used.
	LabMaxIterations int `json:"labMaxIterations" yaml:"labMaxIterations"`

	// LabKeepWorkspaces preserves completed workspaces for inspection.
	LabKeepWorkspaces bool `json:"labKeepWorkspaces" yaml:"labKeepWorkspaces"`

	// LabAllowNetwork controls whether laboratory commands may eventually use
	// network access. The current runner may impose stricter policy.
	LabAllowNetwork bool `json:"labAllowNetwork" yaml:"labAllowNetwork"`

	// ---------------------------------------------------------------------
	// Web Research Engine.
	// ---------------------------------------------------------------------

	// ResearchEnabled enables external research tools.
	ResearchEnabled bool `json:"researchEnabled" yaml:"researchEnabled"`

	// ResearchBackend selects the search implementation.
	// Supported design targets include "auto", "searxng", "duckduckgo".
	ResearchBackend string `json:"researchBackend" yaml:"researchBackend"`

	// ResearchSearXNGURL is the optional local/self-hosted SearXNG endpoint.
	ResearchSearXNGURL string `json:"researchSearxngUrl" yaml:"researchSearxngUrl"`

	// ResearchMaxResults limits results returned to the agent per search.
	ResearchMaxResults int `json:"researchMaxResults" yaml:"researchMaxResults"`

	// ResearchTimeoutSec bounds one research HTTP operation.
	ResearchTimeoutSec int `json:"researchTimeoutSec" yaml:"researchTimeoutSec"`

	// ResearchCacheTTLMin controls cached research lifetime.
	ResearchCacheTTLMin int `json:"researchCacheTtlMin" yaml:"researchCacheTtlMin"`

	// ResearchGitHub enables GitHub-specific research tools.
	ResearchGitHub bool `json:"researchGitHub" yaml:"researchGitHub"`

	// ResearchReddit enables Reddit-specific research tools.
	ResearchReddit bool `json:"researchReddit" yaml:"researchReddit"`

	// ResearchWeb enables general web research/fetching.
	ResearchWeb bool `json:"researchWeb" yaml:"researchWeb"`

	// ResearchUserAgent identifies bounded research requests. When empty or
	// unset it resolves to the version-derived application identity
	// (EffectiveResearchUserAgent) — providers never hardcode their own.
	ResearchUserAgent string `json:"researchUserAgent" yaml:"researchUserAgent"`
}

// AppRoot resolves the portable application root.
func AppRoot() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		return filepath.Dir(exe)
	}

	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// Default returns conservative research/lab-aware defaults.
func Default() *Config {
	// v1.3.0: the canonical data root comes from the single authoritative
	// resolver (internal/config/paths.go). A SHEYTAN_DATA_DIR override is
	// expanded ONCE here; an override that still contains a literal
	// environment token (the v1.2.9 "%LOCALAPPDATA%" defect) is REJECTED
	// in favor of the portable application root — the raw token can never
	// reach runtime state or the filesystem.
	dataDir, fallback, reason := ResolveRoot()

	_ = fallback // callers that need the reason use ResolveRoot directly.
	_ = reason

	return &Config{
		DataDir:     dataDir,
		ModelsDir:   filepath.Join(dataDir, "models"),
		SessionsDir: filepath.Join(dataDir, "sessions"),

		Host:       "127.0.0.1",
		Port:       8765,
		LLMBaseURL: "", // empty = follow the managed engine endpoint
		Model:      "",

		LlamaHost:      "127.0.0.1",
		LlamaPort:      8080,
		LlamaAutoStart: true,

		// v1.1.5 Phase 1: llama.cpp stays the default engine; the
		// native engine path is an explicit opt-in.
		EngineBackend:    BackendLlama,
		NativeEnginePath: "",

		MaxIterations: 25,
		ParallelTools: true,

		RunTimeoutMinutes: 60,

		// v1.1.4: sandbox on by default (it was silently ALWAYS
		// active while the config claimed "disabled" - the field
		// was never read). Resource governance for model-spawned
		// code should fail closed.
		SandboxEnabled: true,
		SandboxMemory:  "512m",
		SandboxCPU:     25,

		LLM: LLMOptions{
			Temperature: 0.7,
			TopP:        0.95,
			TopK:        40,
			MaxTokens:   1024,
			// v1.1.3: raised from 8192 — measured against the real
			// llama.cpp engine, the AI-context briefing + full tool
			// schemas cost ~9.8k tokens, so 8k left NO room for the
			// conversation and the engine rejected the first request.
			NumCtx:        16384,
			NumBatch:      512,
			NumGPU:        0,
			NumThread:     runtime.NumCPU(),
			Stream:        true,
			Preset:        "balanced",
			RepeatPenalty: 1.1,
		},

		Provider:        ProviderLocal,
		UpdateSchedule:  "daily",
		BrowserHeadless: true,
		BrowserSlowMo:   0,

		ThinkingMode:        false,
		AttachmentsBudgetKB: 256,
		HistoryWindowPct:    60,
		RecallEnabled:       true,
		RecallTopK:          4,

		GPUAutoOffload: true,

		EngineCompat:   0,
		FlashAttention: true,
		CacheReuse:     32,
		UBatchSize:     512,
		ThreadsBatch:   0,
		KVCacheQuant:   "",
		Mlock:          false,
		DraftModel:     "",
		ShowPerfHUD:    true,

		VisionEnabled: true,
		VisionMMProj:  "",

		// v1.5.0: automatic performance is the default posture —
		// MANUAL is an explicit user opt-out that preserves
		// hand-set values verbatim.
		PerformanceMode: ModeAuto,

		MaxWorkspaceMB:  512,
		MaxSessionsKept: 100,
		MaxLogMB:        50,

		MultiAgentDepth: 3,

		ContinuumEnabled:         true,
		ContinuumThresholdPct:    75,
		ContinuumCarryMessages:   4,
		ContinuumFrameworkTokens: 700,

		SmoothStream: true,
		TargetFPS:    120,

		// Coding Lab defaults.
		LabEnabled:           true,
		LabWorkspaceRoot:     filepath.Join(dataDir, "lab", "workspaces"),
		LabCommandTimeoutSec: 300,
		LabMaxIterations:     25,
		LabKeepWorkspaces:    false,
		LabAllowNetwork:      false,

		// Research defaults.
		ResearchEnabled:     true,
		ResearchBackend:     "auto",
		ResearchMaxResults:  8,
		ResearchTimeoutSec:  20,
		ResearchCacheTTLMin: 60,
		ResearchGitHub:      true,
		ResearchReddit:      true,
		ResearchWeb:         true,
		ResearchUserAgent:   AppName + "/" + AppVersion,
	}
}

// Load reads the portable JSON configuration.
func Load(path string) (*Config, error) {
	cfg := Default()

	// fileExisted tracks whether the config FILE is the value source
	// (v1.6.2: repaired sampling values are persisted back only when
	// the file — not the environment — carried the corruption).
	fileExisted := false

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			MigrateLegacy(cfg.DataDir)

			if migrated, rerr := os.ReadFile(path); rerr == nil {
				data = migrated
			} else {
				applyEnv(cfg)
				// v1.3.0: the fresh-install path normalizes too —
				// an environment override must never reach
				// runtime state unexpanded.
				cfg.PathNotes = normalizeRuntimePaths(cfg)
				// v1.6.1: environment overrides can carry
				// parser-invalid sampling values too.
				cfg.SamplingNotes = NormalizeSamplingOptions(&cfg.LLM)
				return cfg, nil
			}
		} else {
			return nil, err
		}
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// The file was the value source (fresh files that never existed
	// return earlier — environment-only configurations never persist).
	fileExisted = true

	// v1.1.5: normalize the engine-backend opt-in so hand-edited configs
	// ("Native", " NATIVE ") mean what they say; EffectiveEngineBackend
	// still only accepts the exact canonical values.
	cfg.EngineBackend = strings.ToLower(strings.TrimSpace(cfg.EngineBackend))

	// v1.5.0: normalize the performance mode the same way — an absent,
	// blank or misspelled value means AUTO (the documented default);
	// MANUAL must be spelled exactly to take effect.
	cfg.PerformanceMode = strings.ToLower(strings.TrimSpace(cfg.PerformanceMode))
	if cfg.PerformanceMode != ModeManual {
		cfg.PerformanceMode = ModeAuto
	}

	if cfg.DataDir == "" {
		cfg.DataDir = Default().DataDir
	}
	if cfg.ModelsDir == "" {
		cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	}
	if cfg.SessionsDir == "" {
		cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	}
	if cfg.LabWorkspaceRoot == "" {
		cfg.LabWorkspaceRoot = filepath.Join(cfg.DataDir, "lab", "workspaces")
	}
	if cfg.WorkspaceRoot != "" {
		if abs, err := filepath.Abs(cfg.WorkspaceRoot); err == nil {
			cfg.WorkspaceRoot = filepath.Clean(abs)
		}
	}
	if cfg.ResearchUserAgent == "" {
		cfg.ResearchUserAgent = AppName + "/" + AppVersion
	}

	applyEnv(cfg)

	// v1.3.0: the LAST step of Load is the authoritative path
	// normalization (internal/config/paths.go). Every path field that
	// arrives from disk or environment carrying a literal environment
	// reference ("%LOCALAPPDATA%\SHEYTAN-LA") is expanded once — or
	// rejected and re-derived from the canonical root when the token
	// cannot resolve. No subsystem below this point can ever observe a
	// raw "%...%" path. The reports are surfaced to the caller via
	// PathNotes so they are logged once logging is live.
	cfg.PathNotes = normalizeRuntimePaths(cfg)

	// v1.6.1: model-file fields (model / draftModel / visionMmproj)
	// that still point inside a RETIRED runtime root (the migrated
	// AppData root, the legacy home directory) are re-anchored onto
	// the canonical root — external model paths are never touched.
	if repaired := RepairRetiredModelPaths(cfg); len(repaired) > 0 {
		cfg.PathNotes = append(cfg.PathNotes, repaired...)
	}

	// v1.6.1 (P0): the authoritative sampling-value gate at LOAD time.
	// A parser-invalid value (the in-the-wild repeatPenalty=0, a
	// negative temperature, a non-finite topP) is safely repaired to
	// its documented default and REPORTED — a legacy broken config.json
	// still boots, and the violation is visible instead of silently
	// feeding the engine's compatibility ladder. The engine boot gate
	// (llm.startLocked) enforces the same contract as the last line of
	// defense; the API patch layer rejects bad input outright.
	cfg.SamplingNotes = NormalizeSamplingOptions(&cfg.LLM)

	// v1.6.2 audit closure: persist the REPAIRED values back into the
	// config file atomically, so the same corruption does not require
	// the identical repair on every launch. ONLY the fields that were
	// parser-invalid IN THE FILE are rewritten (see
	// persistRepairedSamplingFields) — environment-introduced values
	// are never baked into the file, and read-only/external configs
	// (missing file, unwritable file) degrade to the historical
	// repair-in-memory behavior with an honest note.
	if len(cfg.SamplingNotes) > 0 && fileExisted {
		if perr := persistRepairedSamplingFields(path, &cfg.LLM); perr != nil {
			cfg.SamplingNotes = append(cfg.SamplingNotes, fmt.Sprintf(
				"could not persist the repaired values (%v) — they apply to this session only",
				perr,
			))
		} else {
			cfg.SamplingNotes = append(cfg.SamplingNotes,
				"repaired values persisted to the config file (no repeated repair on next launch)")
		}
	}

	// v1.8.4 (P0-B): the derived-GPU-posture repair. A pre-1.8.4
	// config whose CPU posture was DERIVED by the recommendation
	// pipeline (gpuAutoOffload=false + numGpu<=0, written "until a
	// Vulkan engine build is provisioned") must not silently keep the
	// requested AUTO profile CPU-only forever — the exact stale state
	// that blocked the AUTO Vulkan candidate (gpu_autoprobe's
	// eligibility gate treats the posture as "offload disabled"). The
	// launch-time evidence gate (autoGPUOffload) keeps the engine
	// honestly on CPU while no usable device exists, so restoring the
	// AUTO default never launches an unproven GPU path. An explicit
	// user OFF (GPUAutoOffloadUserSet) is NEVER touched, and an
	// explicit manual layer count (numGpu>0) is NEVER touched.
	if notes := repairDerivedGPUPosture(cfg); len(notes) > 0 {
		cfg.PathNotes = append(cfg.PathNotes, notes...)

		if fileExisted {
			if perr := persistRepairedGPUPosture(path, cfg); perr != nil {
				cfg.PathNotes = append(cfg.PathNotes, fmt.Sprintf(
					"could not persist the GPU posture repair (%v) — it applies to this session only",
					perr,
				))
			} else {
				cfg.PathNotes = append(cfg.PathNotes,
					"GPU posture repair persisted to the config file (no repeated repair on next launch)")
			}
		}
	}

	return cfg, nil
}

// repairDerivedGPUPosture implements the v1.8.4 AUTO-posture migration and
// reports the (possibly empty) repair notes. It fires only when ALL hold:
//
//   - gpuAutoOffload is false AND numGpu <= 0 (the derived CPU posture —
//     with a manual layer count the posture is explicit, not derived);
//   - the posture was never set by an explicit user action
//     (GPUAutoOffloadUserSet is false — the only writers of a false
//     posture without that marker are the pre-1.8.4 recommendation
//     apply and legacy config versions);
//   - the requested accelerator profile is not an explicit CPU request
//     (profile CPU means the user asked for CPU at the policy level —
//     untouched, nothing to repair).
//
// The repair restores gpuAutoOffload=true (the documented AUTO default:
// "the engine uses the GPU when it can and silently stays on CPU when it
// cannot" — the launch decision stays evidence-gated), records WHY in the
// returned note, and marks nothing else. Idempotent: after the repair the
// posture is true, so the gate cannot fire again.
func repairDerivedGPUPosture(cfg *Config) []string {
	if cfg.GPUAutoOffload || cfg.GPUAutoOffloadUserSet || cfg.LLM.NumGPU > 0 {
		return nil
	}

	profile := strings.ToUpper(strings.TrimSpace(cfg.Accelerator))
	if profile == "CPU" {
		return nil
	}

	cfg.GPUAutoOffload = true

	return []string{
		"repaired derived GPU posture: gpuAutoOffload was false with numGpu<=0 and no user-set marker — " +
			"the pre-1.8.4 recommendation pipeline wrote this CPU posture only \"until a Vulkan engine build is provisioned\", " +
			"but it permanently blocked the AUTO Vulkan candidate; restored the AUTO default (launch stays evidence-gated, CPU while no device is proven)",
	}
}

// persistRepairedGPUPosture rewrites ONLY the repaired GPU-posture fields
// (gpuAutoOffload + the gpuAutoOffloadUserSet marker) into the config
// file, preserving every other byte of user configuration. Mirrors
// persistRepairedSamplingFields: read → patch → atomic rename.
func persistRepairedGPUPosture(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}

	offload, merr := json.Marshal(cfg.GPUAutoOffload)
	if merr != nil {
		return merr
	}
	marker, merr := json.Marshal(cfg.GPUAutoOffloadUserSet)
	if merr != nil {
		return merr
	}

	file["gpuAutoOffload"] = offload
	file["gpuAutoOffloadUserSet"] = marker

	patched, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}

	// v1.8.4: unique temp (the fixed config.json.tmp name races every
	// concurrent config writer — see Save).
	tmp, err := writeUniqueTemp(filepath.Dir(path), "config", patched, 0o600)
	if err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

// persistRepairedSamplingFields rewrites ONLY the parser-invalid LLM
// fields inside the config file at path, leaving every other stored
// value byte-identical in meaning (the file is re-marshaled from its own
// JSON tree; only the repaired keys change). The write is atomic
// (temp+rename, the Save discipline).
func persistRepairedSamplingFields(path string, repaired *LLMOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var tree map[string]any
	if err := json.Unmarshal(data, &tree); err != nil {
		return err
	}

	rawLLM, ok := tree["llm"].(map[string]any)
	if !ok {
		// No llm object in the file: the corruption came from the
		// environment, not the file — nothing to repair on disk.
		return nil
	}

	var fileLLM LLMOptions
	if err := json.Unmarshal(data, &struct{ LLM *LLMOptions }{&fileLLM}); err != nil {
		return err
	}

	// Only fields that are invalid IN THE FILE are rewritten.
	for _, p := range ValidateSamplingOptions(fileLLM) {
		switch p.Field {
		case "llm.repeatPenalty":
			rawLLM["repeatPenalty"] = repaired.RepeatPenalty
		case "llm.temperature":
			rawLLM["temperature"] = repaired.Temperature
		case "llm.topP":
			rawLLM["topP"] = repaired.TopP
		case "llm.minP":
			rawLLM["minP"] = repaired.MinP
		case "llm.presencePenalty":
			rawLLM["presencePenalty"] = repaired.PresencePenalty
		case "llm.frequencyPenalty":
			rawLLM["frequencyPenalty"] = repaired.FrequencyPenalty
		case "llm.mirostatTau":
			rawLLM["mirostatTau"] = repaired.MirostatTau
		case "llm.mirostatEta":
			rawLLM["mirostatEta"] = repaired.MirostatEta
		case "llm.topK":
			rawLLM["topK"] = repaired.TopK
		case "llm.repeatLastN":
			rawLLM["repeatLastN"] = repaired.RepeatLastN
		case "llm.numCtx":
			rawLLM["numCtx"] = repaired.NumCtx
		case "llm.numBatch":
			rawLLM["numBatch"] = repaired.NumBatch
		case "llm.maxTokens":
			rawLLM["maxTokens"] = repaired.MaxTokens
		case "llm.mirostat":
			rawLLM["mirostat"] = repaired.Mirostat
		}
	}

	tree["llm"] = rawLLM

	out, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return err
	}

	// v1.8.4: unique temp (the fixed config.json.tmp name races every
	// concurrent config writer — see Save).
	tmp, err := writeUniqueTemp(filepath.Dir(path), "config", out, 0o644)
	if err != nil {
		return err
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	return nil
}

// MigrateLegacy copies legacy ~/.sheytan data into the portable root.
func MigrateLegacy(root string) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}

	legacy := filepath.Join(home, ".sheytan")

	if legacy == root {
		return
	}

	if _, err := os.Stat(filepath.Join(root, "config.json")); err == nil {
		return
	}

	fi, err := os.Stat(legacy)
	if err != nil || !fi.IsDir() {
		return
	}

	files := []string{
		"config.json",
		"memory.jsonl",
		"installed.json",
	}

	dirs := []string{
		"sessions",
		"logs",
		"models",
		"bin",
	}

	for _, file := range files {
		src := filepath.Join(legacy, file)
		if _, err := os.Stat(src); err == nil {
			_ = copyFile(src, filepath.Join(root, file))
		}
	}

	for _, dir := range dirs {
		src := filepath.Join(legacy, dir)
		if fi, err := os.Stat(src); err == nil && fi.IsDir() {
			_ = copyDir(src, filepath.Join(root, dir))
		}
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	return os.WriteFile(dst, data, 0o644)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return copyFile(path, target)
	})
}

// Save writes configuration as pretty JSON.
//
// v1.1.4: the write is atomic (temp file + rename). The previous plain
// WriteFile could leave a truncated config.json behind if the process died
// mid-write — every other persistent store in the app already used the
// tmp+rename pattern, config was the odd one out.
//
// v1.8.4: the temp file is UNIQUE PER WRITE (os.CreateTemp). The fixed
// "config.json.tmp" name was a latent lost-update/race: two concurrent
// Saves shared one temp name, and the interleaving
// "A writes tmp → B writes tmp → A renames tmp away → B renames: no such
// file" surfaced as a hard engine-start failure ("persist default engine
// path: rename …: no such file or directory"). Same-directory unique
// temps keep the atomic-rename semantics without the name collision.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := writeUniqueTemp(filepath.Dir(path), "config", data, 0o644)
	if err != nil {
		return err
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	return nil
}

// writeUniqueTemp writes data to a unique temp file in dir and returns
// its path (the caller renames or removes it). The file is closed before
// returning — the subsequent rename needs no open handles.
func writeUniqueTemp(dir, prefix string, data []byte, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(dir, prefix+"-*.tmp")
	if err != nil {
		return "", err
	}

	tmp := f.Name()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", err
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}

	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}

	return tmp, nil
}

// applyEnv overlays supported SHEYTAN_* variables.
func applyEnv(cfg *Config) {
	if v := os.Getenv("SHEYTAN_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("SHEYTAN_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("SHEYTAN_BASE_URL"); v != "" {
		cfg.LLMBaseURL = v
	}
	if v := os.Getenv("SHEYTAN_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("SHEYTAN_DATA_DIR"); v != "" {
		// v1.3.0: expand environment references ONCE and refuse to pass
		// the raw token through (the v1.2.9 defect made DataDir a literal
		// "%LOCALAPPDATA%\SHEYTAN-LA" string, which later joined into
		// <root>\%LOCALAPPDATA%\SHEYTAN-LA\models). Unresolvable tokens
		// are dropped here; normalizeRuntimePaths re-derives everything.
		expanded := ExpandEnvPath(strings.TrimSpace(v))
		if !HasEnvToken(expanded) {
			cfg.DataDir = AbsAgainstAppRoot(expanded)
			cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
			cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
			cfg.LabWorkspaceRoot = filepath.Join(cfg.DataDir, "lab", "workspaces")
		}
	}
	if v := os.Getenv("SHEYTAN_PROVIDER"); v != "" {
		cfg.Provider = strings.ToLower(v)
	}
	if v := os.Getenv("SHEYTAN_ENGINE_BACKEND"); v != "" {
		cfg.EngineBackend = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("SHEYTAN_NATIVE_ENGINE_PATH"); v != "" {
		cfg.NativeEnginePath = v
	}
	if v := os.Getenv("SHEYTAN_REMOTE_BASE_URL"); v != "" {
		cfg.RemoteBaseURL = v
		if cfg.Provider == "" {
			cfg.Provider = ProviderRemote
		}
	}
	if v := os.Getenv("SHEYTAN_REMOTE_API_KEY"); v != "" {
		cfg.RemoteAPIKey = v
	}
	if v := os.Getenv("SHEYTAN_REMOTE_MODEL"); v != "" {
		cfg.RemoteModel = v
	}
	if v := os.Getenv("SHEYTAN_BROWSER_PATH"); v != "" {
		cfg.BrowserExecutablePath = v
	}

	// LLM.
	if v := os.Getenv("SHEYTAN_LLM_PRESET"); v != "" {
		cfg.LLM.Preset = strings.ToLower(v)
	}
	if v := os.Getenv("SHEYTAN_LLM_NUM_CTX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.LLM.NumCtx = n
		}
	}
	if v := os.Getenv("SHEYTAN_LLM_NUM_THREADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.LLM.NumThread = n
		}
	}
	if v := os.Getenv("SHEYTAN_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.LLM.MaxTokens = n
		}
	}
	if v := os.Getenv("SHEYTAN_LLM_TEMPERATURE"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil &&
			!math.IsNaN(n) && !math.IsInf(n, 0) {
			cfg.LLM.Temperature = n
		}
	}

	// Engine tuning.
	if v := os.Getenv("SHEYTAN_FLASH_ATTENTION"); v != "" {
		cfg.FlashAttention = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_KV_CACHE_QUANT"); v != "" {
		cfg.KVCacheQuant = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("SHEYTAN_CACHE_REUSE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.CacheReuse = n
		}
	}

	// Experience.
	if v := os.Getenv("SHEYTAN_THINKING_MODE"); v != "" {
		cfg.ThinkingMode = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_SMOOTH_STREAM"); v != "" {
		cfg.SmoothStream = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_TARGET_FPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.TargetFPS = n
		}
	}

	// Coding Lab.
	if v := os.Getenv("SHEYTAN_LAB_ENABLED"); v != "" {
		cfg.LabEnabled = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_LAB_WORKSPACE_ROOT"); v != "" {
		cfg.LabWorkspaceRoot = v
	}
	if v := os.Getenv("SHEYTAN_LAB_COMMAND_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.LabCommandTimeoutSec = n
		}
	}
	if v := os.Getenv("SHEYTAN_LAB_MAX_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.LabMaxIterations = n
		}
	}
	if v := os.Getenv("SHEYTAN_LAB_KEEP_WORKSPACES"); v != "" {
		cfg.LabKeepWorkspaces = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_LAB_ALLOW_NETWORK"); v != "" {
		cfg.LabAllowNetwork = parseBool(v)
	}

	// Research.
	if v := os.Getenv("SHEYTAN_RESEARCH_ENABLED"); v != "" {
		cfg.ResearchEnabled = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_RESEARCH_BACKEND"); v != "" {
		cfg.ResearchBackend = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("SHEYTAN_SEARXNG_URL"); v != "" {
		cfg.ResearchSearXNGURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("SHEYTAN_RESEARCH_MAX_RESULTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ResearchMaxResults = n
		}
	}
	if v := os.Getenv("SHEYTAN_RESEARCH_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ResearchTimeoutSec = n
		}
	}
	if v := os.Getenv("SHEYTAN_RESEARCH_CACHE_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.ResearchCacheTTLMin = n
		}
	}
	if v := os.Getenv("SHEYTAN_RESEARCH_GITHUB"); v != "" {
		cfg.ResearchGitHub = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_RESEARCH_REDDIT"); v != "" {
		cfg.ResearchReddit = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_RESEARCH_WEB"); v != "" {
		cfg.ResearchWeb = parseBool(v)
	}
	if v := os.Getenv("SHEYTAN_RESEARCH_USER_AGENT"); v != "" {
		cfg.ResearchUserAgent = v
	}
}

func parseBool(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// EnsureDirs creates all required portable directories.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.DataDir,
		c.ModelsDir,
		c.SessionsDir,
		c.LogsDir(),
		c.ScreenshotsDir(),
		c.ChartsDir(),
		c.BrowserProfileDir(),
		c.SandboxDir(),
		c.WorkspaceDir(),
		c.LabWorkspaceRoot,
		filepath.Join(c.DataDir, "lab"),
		filepath.Join(c.DataDir, "research"),
		filepath.Join(c.DataDir, "bin"),
	}

	for _, path := range dirs {
		if path == "" {
			continue
		}

		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", path, err)
		}
	}

	return nil
}

// LogsDir returns the log directory.
func (c *Config) LogsDir() string {
	return filepath.Join(c.DataDir, "logs")
}

// ScreenshotsDir returns the browser screenshot directory.
func (c *Config) ScreenshotsDir() string {
	return filepath.Join(c.DataDir, "logs", "screenshots")
}

// ChartsDir returns the chart directory.
func (c *Config) ChartsDir() string {
	return filepath.Join(c.DataDir, "charts")
}

// BrowserProfileDir returns the persistent browser profile directory.
func (c *Config) BrowserProfileDir() string {
	return filepath.Join(c.DataDir, "browser-profile")
}

// SandboxDir returns the sandbox runtime directory.
func (c *Config) SandboxDir() string {
	return filepath.Join(c.DataDir, "sandbox")
}

// WorkspaceDir returns the legacy/general scratch workspace.
func (c *Config) WorkspaceDir() string {
	return filepath.Join(c.DataDir, "workspace")
}

// maxRecentWorkspaces bounds the remembered recent-workspace list.
const maxRecentWorkspaces = 5

// EffectiveWorkspaceRoot returns the CURRENT PROJECT FOLDER (v1.2.4): the
// configured WorkspaceRoot when set, otherwise the legacy scratch workspace.
// This is the single resolution point — the tools base dir, the project
// intelligence card and the Workspace surface all read through it so a
// switch is immediately coherent everywhere.
func (c *Config) EffectiveWorkspaceRoot() string {
	if root := strings.TrimSpace(c.WorkspaceRoot); root != "" {
		return root
	}
	return c.WorkspaceDir()
}

// PushRecentWorkspace records root at the front of RecentWorkspaces
// (deduplicated, bounded). Returns the updated slice.
func (c *Config) PushRecentWorkspace(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return c.RecentWorkspaces
	}
	out := make([]string, 0, len(c.RecentWorkspaces)+1)
	out = append(out, root)
	for _, r := range c.RecentWorkspaces {
		if r == root {
			continue
		}
		out = append(out, r)
		if len(out) >= maxRecentWorkspaces {
			break
		}
	}
	c.RecentWorkspaces = out
	return out
}

// LabDir returns the Coding Lab root.
func (c *Config) LabDir() string {
	return filepath.Join(c.DataDir, "lab")
}

// ProviderKind returns local or remote.
func (c *Config) ProviderKind() string {
	if strings.EqualFold(c.Provider, ProviderRemote) {
		return ProviderRemote
	}
	return ProviderLocal
}

// IsRemote reports whether a remote provider is active.
func (c *Config) IsRemote() bool {
	return c.ProviderKind() == ProviderRemote
}

// EffectiveEngineBackend resolves the configured engine backend to a valid
// kind (v1.1.5 Phase 1). The native path requires an EXACT "native"
// opt-in; anything else — empty, differently-cased, malformed — resolves
// to the llama.cpp backend so bad values fail closed to the shipping
// engine. (Env overlay values are lowercased before they get here; a
// hand-edited config.json must still spell the opt-in exactly.)
func (c *Config) EffectiveEngineBackend() string {
	if c.EngineBackend == BackendNative {
		return BackendNative
	}
	return BackendLlama
}

// NativeBackendEnabled reports whether the native engine path is selected.
func (c *Config) NativeBackendEnabled() bool {
	return c.EffectiveEngineBackend() == BackendNative
}

// EffectiveBaseURL resolves the chat-completions endpoint of the active
// provider.
//
// v1.1.3 divergence fix: in local mode the MANAGED ENGINE's endpoint
// (LlamaHost:LlamaPort) is the single source of truth. LLMBaseURL is now an
// optional explicit override (advanced external local servers); a legacy
// stored value equal to the old hardcoded default is treated as unset so it
// follows the engine port instead of silently diverging from it.
func (c *Config) EffectiveBaseURL() string {
	if c.IsRemote() && c.RemoteBaseURL != "" {
		return strings.TrimRight(c.RemoteBaseURL, "/")
	}

	if override := strings.TrimRight(c.LLMBaseURL, "/"); override != "" &&
		override != legacyDefaultLLMBaseURL {
		return override
	}

	return fmt.Sprintf("http://%s:%d/v1", c.LlamaHost, c.LlamaPort)
}

// EffectiveAPIKey returns the active bearer token.
func (c *Config) EffectiveAPIKey() string {
	if c.IsRemote() && c.RemoteAPIKey != "" {
		return c.RemoteAPIKey
	}
	return "no-key"
}

// EffectiveModel returns the active model ID.
func (c *Config) EffectiveModel() string {
	if c.IsRemote() && c.RemoteModel != "" {
		return c.RemoteModel
	}
	return c.Model
}

// DisplayModel returns a human-friendly model label.
func (c *Config) DisplayModel() string {
	model := c.EffectiveModel()
	if model == "" {
		return ""
	}
	return strings.TrimSuffix(model, ".gguf")
}

// DefaultPath returns the canonical portable config path.
func DefaultPath() string {
	return Default().ConfigPath()
}

// ConfigPath returns config.json under DataDir.
func (c *Config) ConfigPath() string {
	return filepath.Join(c.DataDir, "config.json")
}

// StatePath returns the installed-component state file.
func (c *Config) StatePath() string {
	return filepath.Join(c.DataDir, "installed.json")
}

// EffectiveRunTimeout returns the per-turn budget (0 = unbounded).
// Clamped to 1..1440 minutes; values in range pass through.
func (c *Config) EffectiveRunTimeout() time.Duration {
	if c.RunTimeoutMinutes == 0 {
		return 0
	}
	if c.RunTimeoutMinutes < 1 {
		c.RunTimeoutMinutes = 1
	}
	if c.RunTimeoutMinutes > 1440 {
		c.RunTimeoutMinutes = 1440
	}
	return time.Duration(c.RunTimeoutMinutes) * time.Minute
}

// EffectiveSandboxMemoryMB parses SandboxMemory ("512m", "512", "1g", "2G")
// into megabytes, clamped to 64..16384. Invalid input falls back to 512.
func (c *Config) EffectiveSandboxMemoryMB() int {
	s := strings.ToLower(strings.TrimSpace(c.SandboxMemory))
	if s == "" {
		return 512
	}

	mult := 1
	switch {
	case strings.HasSuffix(s, "g"):
		mult = 1024
		s = strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "k"):
		s = strings.TrimSuffix(s, "k")
	}

	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 512
	}

	mb := n * mult
	if mb < 64 {
		return 64
	}
	if mb > 16384 {
		return 16384
	}
	return mb
}

// EffectiveSandboxCPUPercent clamps SandboxCPU to 5..100 (percent reserved
// for sandboxed code). Non-positive values fall back to 25 - the value the
// governor always used before the setting was wired.
func (c *Config) EffectiveSandboxCPUPercent() int {
	if c.SandboxCPU <= 0 {
		return 25
	}
	if c.SandboxCPU < 5 {
		return 5
	}
	if c.SandboxCPU > 100 {
		return 100
	}
	return c.SandboxCPU
}

// ToolEnabled reports whether a tool is enabled.
func (c *Config) ToolEnabled(name string) bool {
	if len(c.EnabledTools) == 0 {
		return true
	}

	for _, item := range c.EnabledTools {
		if strings.EqualFold(item, name) {
			return true
		}
	}

	return false
}

// EnabledToolList returns only enabled tools.
func (c *Config) EnabledToolList(all []string) []string {
	if len(c.EnabledTools) == 0 {
		return all
	}

	out := make([]string, 0, len(all))

	for _, name := range all {
		if c.ToolEnabled(name) {
			out = append(out, name)
		}
	}

	return out
}

// AttachmentsBudgetBytes returns the configured attachment byte budget.
func (c *Config) AttachmentsBudgetBytes() int {
	kb := c.AttachmentsBudgetKB
	if kb <= 0 {
		kb = 256
	}
	return kb * 1024
}

// HistoryWindowTokens returns the configured history token budget.
func (c *Config) HistoryWindowTokens() int {
	ctxN := c.LLM.NumCtx
	if ctxN <= 0 {
		ctxN = 8192
	}

	pct := c.HistoryWindowPct
	if pct <= 0 {
		pct = 60
	}
	if pct > 95 {
		pct = 95
	}

	return ctxN * pct / 100
}

// EffectiveRecallTopK returns the bounded recall capsule count.
func (c *Config) EffectiveRecallTopK() int {
	if c.RecallTopK <= 0 {
		return 4
	}
	if c.RecallTopK > 12 {
		return 12
	}
	return c.RecallTopK
}

// EffectiveKVCacheQuant validates the KV cache quantization.
func (c *Config) EffectiveKVCacheQuant() string {
	switch strings.ToLower(strings.TrimSpace(c.KVCacheQuant)) {
	case "q8_0":
		return "q8_0"
	case "q4_0":
		return "q4_0"
	default:
		return ""
	}
}

// EffectiveCacheReuse returns the bounded KV reuse chunk.
func (c *Config) EffectiveCacheReuse() int {
	if c.CacheReuse <= 0 {
		return 0
	}
	if c.CacheReuse > 512 {
		return 512
	}
	return c.CacheReuse
}

// EffectiveUBatchSize returns the bounded physical batch.
func (c *Config) EffectiveUBatchSize() int {
	if c.UBatchSize <= 0 {
		return 512
	}
	if c.UBatchSize > 8192 {
		return 8192
	}
	return c.UBatchSize
}

// EffectiveMultiAgentDepth returns the bounded planning depth.
func (c *Config) EffectiveMultiAgentDepth() int {
	if c.MultiAgentDepth <= 0 {
		return 3
	}
	if c.MultiAgentDepth > 5 {
		return 5
	}
	return c.MultiAgentDepth
}

// EffectiveContinuumThreshold returns the rollover threshold.
func (c *Config) EffectiveContinuumThreshold() int {
	if c.ContinuumThresholdPct <= 0 {
		return 75
	}
	if c.ContinuumThresholdPct < 50 {
		return 50
	}
	if c.ContinuumThresholdPct > 95 {
		return 95
	}
	return c.ContinuumThresholdPct
}

// EffectiveContinuumCarry returns the number of carried messages.
func (c *Config) EffectiveContinuumCarry() int {
	if c.ContinuumCarryMessages < 0 {
		return 4
	}
	if c.ContinuumCarryMessages > 16 {
		return 16
	}
	return c.ContinuumCarryMessages
}

// EffectiveContinuumFrameworkTokens returns the Framework token budget.
func (c *Config) EffectiveContinuumFrameworkTokens() int {
	if c.ContinuumFrameworkTokens <= 0 {
		return 700
	}
	if c.ContinuumFrameworkTokens < 200 {
		return 200
	}
	if c.ContinuumFrameworkTokens > 2000 {
		return 2000
	}
	return c.ContinuumFrameworkTokens
}

// EffectiveMaxWorkspaceMB returns the general workspace quota.
func (c *Config) EffectiveMaxWorkspaceMB() int {
	if c.MaxWorkspaceMB < 0 {
		return 0
	}
	return c.MaxWorkspaceMB
}

// EffectiveMaxSessionsKept returns session retention.
func (c *Config) EffectiveMaxSessionsKept() int {
	if c.MaxSessionsKept <= 0 {
		return 0
	}
	if c.MaxSessionsKept < 10 {
		return 10
	}
	return c.MaxSessionsKept
}

// EffectiveMaxLogMB returns the log quota.
func (c *Config) EffectiveMaxLogMB() int {
	if c.MaxLogMB < 0 {
		return 0
	}
	return c.MaxLogMB
}

// EffectiveTargetFPS returns a bounded UI frame target.
func (c *Config) EffectiveTargetFPS() int {
	fps := c.TargetFPS

	if fps == 0 {
		fps = 120
	}
	if fps < 30 {
		fps = 30
	}
	if fps > 240 {
		fps = 240
	}

	return fps
}

// EffectiveStreamEmitInterval returns the streaming update cadence.
func (c *Config) EffectiveStreamEmitInterval() time.Duration {
	interval := time.Second / time.Duration(c.EffectiveTargetFPS())

	if interval < 8*time.Millisecond {
		interval = 8 * time.Millisecond
	}
	if interval > 80*time.Millisecond {
		interval = 80 * time.Millisecond
	}

	return interval
}

// EffectiveLabCommandTimeout returns the per-command laboratory timeout.
func (c *Config) EffectiveLabCommandTimeout() time.Duration {
	seconds := c.LabCommandTimeoutSec
	if seconds <= 0 {
		seconds = 300
	}
	if seconds > 3600 {
		seconds = 3600
	}
	return time.Duration(seconds) * time.Second
}

// EffectiveLabMaxIterations returns the Coding Lab repair limit.
func (c *Config) EffectiveLabMaxIterations() int {
	if c.LabMaxIterations > 0 {
		if c.LabMaxIterations > 100 {
			return 100
		}
		return c.LabMaxIterations
	}

	if c.MaxIterations > 0 {
		if c.MaxIterations > 100 {
			return 100
		}
		return c.MaxIterations
	}

	return 25
}

// EffectiveResearchMaxResults returns the per-search result limit.
func (c *Config) EffectiveResearchMaxResults() int {
	n := c.ResearchMaxResults
	if n <= 0 {
		n = 8
	}
	if n > 50 {
		n = 50
	}
	return n
}

// EffectiveResearchTimeout returns the research request timeout.
func (c *Config) EffectiveResearchTimeout() time.Duration {
	seconds := c.ResearchTimeoutSec
	if seconds <= 0 {
		seconds = 20
	}
	if seconds > 120 {
		seconds = 120
	}
	return time.Duration(seconds) * time.Second
}

// EffectiveResearchBackend returns a normalized research backend.
func (c *Config) EffectiveResearchBackend() string {
	switch strings.ToLower(strings.TrimSpace(c.ResearchBackend)) {
	case "searxng":
		return "searxng"
	case "duckduckgo":
		return "duckduckgo"
	case "auto":
		return "auto"
	default:
		return "auto"
	}
}

// EffectiveResearchCacheTTL returns the research cache lifetime.
func (c *Config) EffectiveResearchCacheTTL() time.Duration {
	minutes := c.ResearchCacheTTLMin
	if minutes < 0 {
		minutes = 0
	}
	if minutes > 10080 {
		minutes = 10080
	}
	return time.Duration(minutes) * time.Minute
}

// EffectiveResearchUserAgent returns the configured research User-Agent.
func (c *Config) EffectiveResearchUserAgent() string {
	if strings.TrimSpace(c.ResearchUserAgent) == "" {
		return AppName + "/" + AppVersion
	}
	return c.ResearchUserAgent
}
