// settings-sections.ts — v1.3.0 the Settings information architecture.
//
// ONE declarative registry, consumed by SettingsPanel (rendering) and by
// the regression tests (structure assertions). The v1.2.9 surface
// exposed ten tabs of implementation-level controls (engine host/port,
// projector paths, batch/thread knobs) to ordinary users; v1.3.0
// reorganizes everything around USER OUTCOMES:
//
//   General        startup behavior, default workspace, interaction prefs
//   Models         current model, available models, vision capability
//   Performance    Quiet / Balanced / Maximum + measured facts
//   Agent & Tools  tool access, permissions, thinking, sandbox posture
//   Network        research + remote provider configuration
//   Updates        check now, automatic update policy, version
//   Diagnostics    read-only runtime health, hardware, logs
//   Advanced       the ONLY home for genuinely expert controls
//
// FIELD_PLACEMENT records where every config field lives — fields NOT
// listed in a user-facing section are either automated (AUTOMATED_FIELDS)
// or live in Advanced. The tests pin theMM projector and other
// implementation fields OUT of the normal sections.

export type SettingsSectionId =
  | "general"
  | "models"
  | "performance"
  | "agent"
  | "network"
  | "updates"
  | "diagnostics"
  | "advanced";

export interface SettingsSectionMeta {
  id: SettingsSectionId;
  label: string;
  hint: string;
}

export const SETTINGS_SECTIONS: SettingsSectionMeta[] = [
  { id: "general", label: "General", hint: "Startup, workspace, interaction" },
  { id: "models", label: "Models", hint: "Local models, provider, vision" },
  { id: "performance", label: "Performance", hint: "Quiet, Balanced, Maximum" },
  { id: "agent", label: "Agent & Tools", hint: "Tools, permissions, sandbox" },
  { id: "network", label: "Network", hint: "Research, remote provider" },
  { id: "updates", label: "Updates", hint: "Check, policy, version" },
  { id: "diagnostics", label: "Diagnostics", hint: "Health, hardware, logs" },
  { id: "advanced", label: "Advanced", hint: "Expert controls" },
];

// FIELD_PLACEMENT: config fields exposed as EDITABLE controls per
// section. Fields absent from this map have no editable control in the
// normal UI (they are automated, read-only, or backend-only).
export const FIELD_PLACEMENT: Record<string, SettingsSectionId> = {
  // General
  llamaAutoStart: "general",
  smoothStream: "general",
  showPerfHud: "general",
  targetFps: "general",

  // Models
  provider: "models",
  model: "models",
  remoteBaseUrl: "models",
  remoteModel: "models",
  visionEnabled: "models",

  // Performance
  numCtx: "performance",
  historyWindowPct: "performance",
  continuumEnabled: "performance",
  // v1.5.0: the AUTO/MANUAL runtime concept owns the performance tab's
  // semantics (the PerformanceModeCard renders it).
  performanceMode: "performance",

  // Agent & Tools
  maxIterations: "agent",
  parallelTools: "agent",
  thinkingMode: "agent",
  recallEnabled: "agent",
  recallTopK: "agent",
  enabledTools: "agent",
  labEnabled: "agent",
  labKeepWorkspaces: "agent",
  labAllowNetwork: "agent",
  sandboxEnabled: "agent",

  // Network
  researchEnabled: "network",
  researchBackend: "network",
  researchSearxngUrl: "network",
  researchMaxResults: "network",
  researchGitHub: "network",
  researchReddit: "network",
  researchWeb: "network",

  // Updates
  updateSchedule: "updates",

  // Advanced — the only expert surface
  llamaBinPath: "advanced",
  llamaHost: "advanced",
  llamaPort: "advanced",
  llamaExtraArgs: "advanced",
  llmBaseUrl: "advanced",
  modelsDir: "advanced",
  visionMmproj: "advanced",
  visionMmprojOffload: "advanced",
  flashAttention: "advanced",
  gpuAutoOffload: "advanced",
  mlock: "advanced",
  cacheReuse: "advanced",
  ubatchSize: "advanced",
  threadsBatch: "advanced",
  kvCacheQuant: "advanced",
  draftModel: "advanced",
  sandboxMemory: "advanced",
  sandboxCPU: "advanced",
  maxWorkspaceMb: "advanced",
  maxSessionsKept: "advanced",
  maxLogMb: "advanced",
  labWorkspaceRoot: "advanced",
  labCommandTimeoutSec: "advanced",
  labMaxIterations: "advanced",
  runTimeoutMinutes: "advanced",
  attachmentsBudgetKb: "advanced",
  multiAgentDepth: "advanced",
  researchTimeoutSec: "advanced",
  researchCacheTtlMin: "advanced",
  researchUserAgent: "advanced",
  browserExecutablePath: "advanced",
  browserSlowMoMs: "advanced",
  browserHeadless: "advanced",
  browserSlowMoMsAlias: "advanced",
  temperature: "advanced",
  topP: "advanced",
  topK: "advanced",
  minP: "advanced",
  maxTokens: "advanced",
  repeatPenalty: "advanced",
  seed: "advanced",
};

// AUTOMATED_FIELDS: implementation-level knobs the runtime now manages
// automatically (measured / recommended / detected). They may still be
// reported as read-only evidence, and remain persisted for backward
// compatibility, but they are not ordinary user controls.
export const AUTOMATED_FIELDS: string[] = [
  "engineCompat",
  "engineCompatReason",
  "engineCompatAt",
  "visionMmprojDetection",
  "engineBackend",
  "nativeEnginePath",
];

// REMOVED_FROM_NORMAL_UI: fields that were ordinary user-facing controls
// in v1.2.9 and are deliberately NOT editable outside Advanced (or at
// all) in v1.3.0.
export const REMOVED_FROM_NORMAL_UI: string[] = [
  "visionMmproj",
  "llamaBinPath",
  "llamaHost",
  "llamaPort",
  "llamaExtraArgs",
  "kvCacheQuant",
  "threadsBatch",
  "cacheReuse",
  "ubatchSize",
  "mlock",
  "draftModel",
  "sandboxMemory",
  "sandboxCPU",
  "maxWorkspaceMb",
  "maxSessionsKept",
  "maxLogMb",
];

// isNormalSection reports whether a section is user-facing (everything
// except Advanced).
export function isNormalSection(id: SettingsSectionId): boolean {
  return id !== "advanced";
}
