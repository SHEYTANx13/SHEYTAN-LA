import { API_BASE } from "./config";

export interface AppState {
  appName: string;
  appVersion: string;
  state: string;
}

export interface SysInfoCPU {
  name: string;
  physicalCores: number;
  logicalCores: number;
  frequencyMHz: number;
}

export interface SysInfoRAM {
  totalBytes: number;
  freeBytes: number;
  availableBytes: number;
}

export interface SysInfoDisk {
  totalBytes: number;
  freeBytes: number;
  path: string;
}

export interface SysInfoGPU {
  vendor: string;
  name: string;
  vramBytes: number;
  driverVersion: string;
}

export interface SysInfoRecommended {
  numThread: number;
  numGPU: number;
  numCtx: number;
  numBatch: number;
  maxTokens: number;
  canRunCPU: boolean;
  canRunGPU: boolean;
  warnings: string[];
}

export interface SysInfo {
  os: string;
  arch: string;
  hostname: string;
  cpu: SysInfoCPU;
  ram: SysInfoRAM;
  disk: SysInfoDisk;
  gpus: SysInfoGPU[];
  wsl2: boolean;
  docker: boolean;
  recommended: SysInfoRecommended;
  // v1.3.0: measured OS identity (build-derived display label).
  osBuild?: number;
  osDisplay?: string;
}

export interface Preset {
  id: string;
  name?: string;
  label?: string;
  description?: string;
  temperature?: number;
  top_p?: number;
  top_k?: number;
  max_tokens?: number;
  repeat_penalty?: number;
  mirostat?: number;
  num_ctx?: number;
}

// v1.2.0 — Environment Centre / health / recommendation / update payloads.
// Every field below is backend-measured or backend-derived; the UI renders
// unknown values as "—" instead of inventing them.

export interface HardwareCPU {
  name: string;
  physicalCores?: number;
  logicalCores?: number;
  frequencyMHz?: number;
}

export interface HardwareRAM {
  totalBytes: number;
  freeBytes?: number;
  availableBytes?: number;
}

export interface HardwareStorage {
  totalBytes?: number;
  freeBytes?: number;
  path?: string;
}

export interface HardwareGPU {
  vendor?: string;
  name?: string;
  vramBytes?: number;
  driverVer?: string;
}

export interface HardwareBackend {
  engineBinary?: string;
  vulkan: boolean;
  nativeEnabled: boolean;
  engineTag?: string;
}

// v1.2.6 — the measured neural accelerator identity (Intel AI Boost
// etc.). Presence is HARDWARE identity only — it never implies an inference
// backend can use it; runtime capability is a separate, measured verdict.
export interface HardwareNPU {
  vendor?: string;
  name?: string;
  driverVersion?: string;
  status?: string;
  detectedBy?: string;
}

export interface EnvironmentPayload {
  device: {
    os: string;
    arch: string;
    cpu: HardwareCPU;
    ram: HardwareRAM;
    storage: HardwareStorage;
    gpus: HardwareGPU[];
    npu?: HardwareNPU;
    deepReady?: boolean;
    backend: HardwareBackend;
    identity: {
      product: string;
      shortName: string;
      appUserModelId?: string;
      platform: string;
    };
  };
  runtime: {
    engineState: string;
    enginePhase?: string;
    provider: string;
    backend: string;
    model?: string;
    context?: number;
    verified: boolean;
    vision: {
      state: string;
      reason?: string;
      active: boolean;
      projector?: string;
      bytes?: number;
    };
    engineTag?: string;
  };
  recommendation: {
    task: string;
    applied: boolean;
    context?: number;
    threads?: number;
    gpuLayers?: number;
    summary: string;
    reasons?: string[];
  };
  generatedAt: string;
}

export interface HealthCheck {
  id: string;
  label: string;
  state: "ok" | "warn" | "fail" | "unknown" | "na";
  evidence: string;
}

export interface HealthPayload {
  overall: "ok" | "warn" | "fail";
  checks: HealthCheck[];
  generatedAt: string;
}

// v1.8.0: the Runtime Governor / system-health surface (GET /api/governor).
// Every block is composed server-side from the EXISTING authorities; a
// fact the platform cannot measure arrives as measured:false — the UI
// renders "—", never an invented number.
export interface GovernorResourceState {
  at: string;
  ramTotalBytes?: number;
  ramAvailableBytes?: number;
  availableKnown: boolean;
  procRssBytes?: number;
  engineRssBytes?: number;
  engineRssKnown?: boolean;
  engineRunning?: boolean;
  engineModel?: string;
  activeRuns: number;
  cpuLoadPercent?: number;
  cpuLoadKnown?: boolean;
  cpuRollingAvg?: number;
  level: "ok" | "warning" | "high_pressure" | "critical_pressure";
  sustained: number;
  unknowns?: string[];
}

export interface GovernorEnvelope {
  level: GovernorResourceState["level"];
  sustained: number;
  admitHeavyweight: boolean;
  reduceBackground: boolean;
  reduceContextWork: boolean;
  reduceToolConcurrency: boolean;
  residentMemoryBudgetBytes?: number;
  adjustmentClass: "none" | "live" | "next-run" | "reload";
  reasons?: string[];
}

export interface GovernorPayload {
  appName: string;
  appVersion: string;
  governor: {
    available: boolean;
    reason?: string;
    state?: GovernorResourceState;
    envelope?: GovernorEnvelope;
    selfModel?: Record<string, unknown>;
  };
  hardware: Record<string, unknown>;
  engine: Record<string, unknown>;
  capabilities: Record<string, boolean>;
}

export interface TaskProfileInfo {
  id: string;
  label: string;
  description: string;
}

export interface RuntimeRecommendation {
  task: string;
  context: number;
  threads: number;
  threadsBatch: number;
  gpuLayers: number;
  ubatchSize: number;
  flashAttention: boolean;
  kvCacheQuant: string;
  gpuAutoOffload: boolean;
  mmprojOffload: string;
  projectorDevice: string;
  cacheReuse?: number;
  predicted: {
    totalBytes?: number;
    kvBytes?: number;
    class?: string;
    speed?: string;
    vramBytes?: number;
  };
  reasons: string[];
  notes?: string[];
}

export interface RecommendationPayload {
  task: string;
  current: {
    model?: string;
    context?: number;
    threads?: number;
    ubatchSize?: number;
    gpuLayers?: number;
    gpuAutoOffload: boolean;
    flashAttention: boolean;
    kvCacheQuant?: string;
    mmprojOffload?: string;
    runtimeProfile?: string;
  };
  recommended?: RuntimeRecommendation;
  available: TaskProfileInfo[];
}

export interface AppUpdateStatus {
  state:
    | "up-to-date"
    | "update-available"
    | "check-failed"
    | "downloading"
    | "ready"
    | "failed"
    | "unknown";
  version: string;
  latest?: string;
  channel?: string;
  notes?: string;
  publishedAt?: string;
  stagedPath?: string;
  stagedSHA256?: string;
  checkedAt?: string;
  message?: string;
  // v1.2.3: live staging progress from the Download Manager while the
  // state is "downloading" — phase, bytes, speed, ETA, source, checks.
  download?: DownloadProgress;
}

// v1.2.3 Download Manager progress (mirrors internal/downloader.Progress).
// `bytesTotal`/`etaSeconds` go negative when the server did not declare a
// size; the UI must render indeterminate states honestly.
export interface DownloadProgress {
  phase:
    | "resolving"
    | "connecting"
    | "downloading"
    | "verifying"
    | "installing"
    | "ready";
  sourceUrl?: string;
  sourceLabel?: string;
  sourceTrust?: string;
  attempt?: number;
  bytesDone: number;
  bytesTotal: number;
  bytesPerSec?: number;
  etaSeconds?: number;
  retries?: number;
  resumable?: boolean;
  verified?: boolean;
  paused?: boolean;
  message?: string;
}

export interface Model {
  id: string;
  name: string;
  provider?: string;
  path?: string;
  sizeBytes?: number;
  loaded?: boolean;
  // v1.1.5 Phase 6: true when the ACTIVE backend is currently serving
  // this model (the honest "currently serving" marker).
  serving?: boolean;
  // v1.1.4: GGUF header metadata (populated from the model card parser).
  architecture?: string;
  quantization?: string;
  contextLength?: number;
  parameterInfo?: string;
  // v1.1.6 first-use model card: backend-measured footprint at the
  // model class' recommended context, plus that recommendation itself.
  estimatedMemoryBytes?: number;
  recommendedContext?: number;
  // v1.1.8 model-picker capability facts — measured by the backend's
  // capability resolver and served from its bounded cache. The UI never
  // invents these: a missing field means "not detected", not "maybe".
  multimodal?: boolean;
  nativeBackend?: boolean;
  chatTemplate?: boolean;
  nativeReason?: string;
  estimatedVRAMBytes?: number;
  // v1.2.0 vision readiness state machine + projector evidence. The UI
  // renders exactly what the backend derived from GGUF architecture,
  // projector discovery and (when serving) the verified engine boot —
  // never from filenames alone.
  visionState?: string;
  visionReason?: string;
  mmprojPath?: string;
  mmprojName?: string;
  mmprojSizeBytes?: number;
  mmprojVerified?: boolean;
  // v1.5.0: the last-but-one selection (config.previousModel) — the
  // picker's "previous selection" marker. Informational only.
  previous?: boolean;
}

export interface ModelsResponse {
  local: Model[];
  loaded: Model[];
  llamaRunning: boolean;
  // v1.1.5 Phase 6: honest status — which backend serves generation
  // ("native" | "llama") and which model file it is serving right now.
  backend?: string;
  servingPath?: string;
}

export interface ToolInfo {
  name: string;
  // v1.1.7: concise one-line description for the Options UI.
  description?: string;
  // v1.1.7: the full model-facing operational spec (never rendered in the
  // main Options list — kept for tooltips/debugging surfaces).
  detail?: string;
  enabled?: boolean;
  // v1.6.0 P3 (spec §17): builtin vs custom — the ONE registry carries
  // both with explicit source metadata.
  source?: "builtin" | "custom";
}

// v1.6.0 P3 — custom tool definitions (Settings → Agent & Tools →
// My Tools). The shape mirrors the backend's deterministic API view.
export type CustomToolParam = {
  name: string;
  type: "string" | "number" | "integer" | "boolean" | "array" | "object";
  required: boolean;
  description?: string;
  default?: unknown;
  enum?: string[];
};

export type CustomToolDefinition = {
  id: string;
  name: string;
  shortDescription: string;
  description: string;
  inputParameters: CustomToolParam[];
  executionType: "http" | "command";
  http?: {
    method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
    url: string;
    headers?: Record<string, string>;
    bodyTemplate?: string;
  };
  command?: {
    executable: string;
    args?: string[];
    workingDir?: string;
  };
  permission: "network" | "local" | "";
  timeoutSeconds: number;
  outputLimitBytes: number;
  enabled: boolean;
  registered?: boolean;
  createdAt?: string;
  updatedAt?: string;
};

export type CustomToolTestResult = {
  ok: boolean;
  output?: string;
  error?: string;
  duration?: number;
};

// v1.6.2 — the engine backend-variant provisioning surface
// (GET/POST /api/engine/provision). The POST drives the full updater
// transaction (stop → staged install → startup/health → runtime backend
// verification → commit / rollback) and can therefore take minutes on a
// slow link — callers pass a generous timeout.
export type EngineProvisionState = {
  installedVariant: string;
  supportedVariants: string[];
  platform: string;
};

export type EngineProvisionResult = {
  ok: boolean;
  variant: string;
  installedVariant: string;
  outcome: string;
};

// v1.6.0 P0 — the startup maintenance gate's explicit state
// (GET /api/maintenance). The UI renders the REAL lifecycle phase,
// never a generic "Updating…".
export type MaintenanceStatus = {
  phase:
    | "CHECKING"
    | "MAINTENANCE_REQUIRED"
    | "DOWNLOADING"
    | "VERIFYING"
    | "INSTALLING"
    | "READY_FOR_PREWARM"
    | "DEFERRED"
    | "BLOCKED"
    | "FAILED";
  reason?: string;
  detail?: string;
  startedAt?: string;
  finishedAt?: string;
  prewarmPermitted: boolean;
  updated: boolean;
  targetTag?: string;
};

export interface LLMConfig {
  temperature: number;
  topP: number;
  topK: number;
  minP: number;
  maxTokens: number;
  stop: string;
  seed: number;
  repeatPenalty: number;
  repeatLastN: number;
  presencePenalty: number;
  frequencyPenalty: number;
  mirostat: number;
  mirostatTau: number;
  mirostatEta: number;
  numCtx: number;
  numBatch: number;
  numGpu: number;
  numThread: number;
  stream: boolean;
  preset: string;
}

export interface RuntimeConfig {
  dataDir: string;
  modelsDir: string;
  sessionsDir: string;

  // v1.3.0: the active project workspace (empty = managed default).
  // Reported by the backend; switched through the Workspace surface.
  workspaceRoot?: string;

  host: string;
  port: number;

  llmBaseUrl: string;
  model: string;

  // v1.5.0: the automatic-performance posture ("auto" default | "manual").
  // AUTO recalculates the runtime profile from measured evidence on every
  // model change; MANUAL preserves explicit user values verbatim.
  performanceMode?: "auto" | "manual";
  // v1.5.0: the last-but-one selected model (informational marker).
  previousModel?: string;

  provider: string;
  remoteBaseUrl: string;
  remoteApiKey: string;
  remoteModel: string;

  llamaBinPath: string;
  llamaHost: string;
  llamaPort: number;
  llamaAutoStart: boolean;
  llamaExtraArgs: string;

  maxIterations: number;
  parallelTools: boolean;

  runTimeoutMinutes?: number;
  sandboxEnabled: boolean;
  sandboxMemory: string;
  sandboxCPU: number;

  llm: LLMConfig;

  browserExecutablePath: string;
  browserHeadless: boolean;
  browserSlowMoMs: number;

  updateSchedule: string;
  lastUpdateCheck: string;

  thinkingMode: boolean;
  enabledTools: string[];
  attachmentsBudgetKb: number;
  historyWindowPct: number;
  recallEnabled: boolean;
  recallTopK: number;

  gpuAutoOffload: boolean;
  engineCompat: number;
  // v1.1.7: why the engine last settled above compatibility level 0
  // (backend-managed; surfaced in Settings → Performance → Engine).
  engineCompatReason?: string;
  engineCompatAt?: string;
  flashAttention: boolean;
  cacheReuse: number;
  ubatchSize: number;
  threadsBatch: number;
  kvCacheQuant: string;
  mlock: boolean;
  draftModel: string;
  showPerfHud: boolean;

  visionEnabled: boolean;
  visionMmproj: string;
  // v1.2.0: projector GPU-offload posture (auto | on | off) and the
  // active task-aware runtime profile id (chat | coding | research |
  // vision | agent | low-power | maximum).
  visionMmprojOffload: string;
  runtimeProfile: string;

  maxWorkspaceMb: number;
  maxSessionsKept: number;
  maxLogMb: number;

  multiAgentDepth: number;

  continuumEnabled: boolean;
  continuumThresholdPct: number;
  continuumCarryMessages: number;
  continuumFrameworkTokens: number;

  smoothStream: boolean;
  targetFps: number;

  labEnabled: boolean;
  labWorkspaceRoot: string;
  labCommandTimeoutSec: number;
  labMaxIterations: number;
  labKeepWorkspaces: boolean;
  labAllowNetwork: boolean;

  researchEnabled: boolean;
  researchBackend: string;
  researchSearxngUrl: string;
  researchMaxResults: number;
  researchTimeoutSec: number;
  researchCacheTtlMin: number;
  researchGitHub: boolean;
  researchReddit: boolean;
  researchWeb: boolean;
  researchUserAgent: string;
}

export interface Session {
  id: string;
  title: string;
  model?: string;
  preset?: string;
  createdAt: string;
  updatedAt: string;
  // v1.2.8: the session's conversation space ("chat" | "agent"). Legacy
  // sessions without the field are "agent" (deterministic migration).
  mode?: string;
  threadId?: string;
  parentId?: string;
  chapter?: number;
  msgCount?: number;
}

// v1.2.8: cross-mode history reference wire types (mirrors the backend
// sessions.HistoryRef / histref.Hit shapes).
export type HistoryRefRange = {
  from: number;
  to: number;
};

export type HistoryRef = {
  sessionId: string;
  mode?: string;
  summaryVersion?: number;
  ranges?: HistoryRefRange[];
};

export type HistoryHit = {
  sessionId: string;
  mode: string;
  title: string;
  updatedAt: string;
  msgCount: number;
  score: number;
  summaryVersion: number;
  objective?: string;
  snippet?: string;
  currentState?: string;
};

export type SessionSummaryView = {
  sessionId: string;
  mode?: string;
  version: number;
  updatedAt?: string;
  objective?: string;
  importantUserConstraints?: string[];
  keyDecisions?: string[];
  importantFacts?: string[];
  filesAndArtifacts?: string[];
  toolsAndResearch?: string[];
  errorsAndRepairs?: string[];
  currentState?: string;
  unresolvedItems?: string[];
  nextStep?: string;
};

export type IndexedMessage = {
  index: number;
  message: ChatMessage;
};

export type MessagesPage = {
  sessionId: string;
  total: number;
  hasMore: boolean;
  nextBefore: number;
  page: number;
  messages: IndexedMessage[];
};

export interface RunRequest {
  sessionId: string;
  message: string;
  attachmentIds?: string[];
  regenerate?: boolean;

  // v1.2.5 per-request controls — these change the actual backend
  // request (tier posture + tool surface), not just the UI.
  /** "auto" | "fast" | "thinking" */
  thinking?: string;
  /** "auto" | "manual" */
  toolMode?: string;
  /** manual-mode allow-list (tool names); ignored in auto mode */
  toolAllow?: string[];

  // v1.3.6 (spec §24): explicit per-request Net Search intent. The
  // backend authorizes the existing research tool for THIS request
  // server-side; the setting is recorded in run telemetry.
  netSearch?: boolean;

  // v1.2.8: explicitly attached cross-mode history sessions. DATA, never
  // authority — only the relevant portions are retrieved per request and
  // injected with provenance labels.
  historyRefs?: HistoryRef[];
}

export interface RunResponse {
  ok: boolean;
  sessionId: string;
  runId?: string;
  status?: string;
  // v1.2.6 continuation: the authoritative run state at POST time
  // ("registered" — the run is in the active-runs map; live transport
  // attachment is a SEPARATE fact the socket's `attached` frame proves).
  state?: string;
}

export interface AbortResponse {
  ok: boolean;
  status?: string;
}

// v1.7.4 run-control contract responses (pause / edit / resume).
export interface RunControlResponse {
  ok: boolean;
  runId: string;
  revision: number;
  phase: string;
}

// One recoverable paused run (restart recovery surface, GET /api/run/paused).
export interface PausedRunSummary {
  runId: string;
  sessionId: string;
  revision: number;
  userMessage: string;
  assistantDraft: string;
  updatedAt?: string;
}

export type ActivityEvent = {
  id?: string;
  type: string;
  caption?: string;
  timestamp?: string | number;
  sessionId?: string;
  data?: unknown;
  [key: string]: unknown;
};

export interface LabWorkspace {
  id: string;
  source: string;
  path: string;
  createdAt: string;
}

export interface LabCommand {
  command: string;
  workingDir?: string;
  timeout?: number;
  maxOutputBytes?: number;
}

export interface LabCommandResult {
  command: string;
  workingDir: string;
  stdout: string;
  stderr: string;
  output: string;
  exitCode: number;
  duration: number;
  startedAt: string;
  finishedAt: string;
  timedOut: boolean;
  canceled: boolean;
  outputLimit: boolean;
  success: boolean;
}

export type LabVerificationStatus =
  "passed" | "failed" | "canceled" | "skipped";

export interface LabVerificationResult {
  name: string;
  status: LabVerificationStatus;
  result: LabCommandResult;
  error?: string;
  startedAt: string;
  finishedAt: string;
}

export interface LabVerificationSummary {
  passed: boolean;
  requiredTotal: number;
  requiredPassed: number;
  requiredFailed: number;
  optionalTotal: number;
  optionalPassed: number;
  optionalFailed: number;
  results: LabVerificationResult[];
  duration: number;
  startedAt: string;
  finishedAt: string;
  error?: string;
}

export type LabTaskStatus =
  "pending" | "running" | "succeeded" | "failed" | "canceled" | "blocked";

export interface LabTaskSnapshot {
  id: string;
  title: string;
  description: string;
  status: LabTaskStatus;
  workspace?: LabWorkspace;
  commands?: LabCommand[];
  results?: LabCommandResult[];
  metadata?: Record<string, string>;
  lastVerification?: LabVerificationSummary;
  verificationPassed: boolean;
  verifiedAt?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  error?: string;
}

export interface LabTaskSessionSnapshot {
  id: string;
  createdAt: string;
  updatedAt: string;
  task: LabTaskSnapshot;
}

export interface LabListResponse {
  tasks: LabTaskSessionSnapshot[];
}

export interface LabActionResponse {
  ok: boolean;
  result?: unknown;
  error?: string;
}

// v1.1.3: authoritative engine state (backend process state is the single
// source of truth; the UI never invents these).
export type EngineState =
  | "idle"
  | "downloading"
  | "starting"
  | "ready"
  | "running"
  | "busy"
  | "stopping"
  | "stopped"
  | "failed"
  | "remote";

export interface EngineSnapshot {
  state: EngineState;
  detail?: string;
  model?: string;
  loadedPath?: string;
  pid?: number;
  vision: boolean;
  provider: string;
  // v1.1.5: the backend that serves generation ("llama" in Phase 1 —
  // the native engine reports generation-incapable and selection falls
  // back). Informational; the badge keeps reading `state`.
  backend: string;
  // v1.1.5: supervised native engine status when the native path is
  // enabled (engineBackend: "native"). Absent by default. Local status
  // reads only — the poll never performs engine IPC.
  native?: NativeEngineStatus;
  // v1.1.6 startup flow: user-facing phase (waiting | downloading-engine |
  // loading-model | checking-capabilities | preparing-context | ready |
  // stopping | stopped | failed) and the verified-readiness proof —
  // "ready" is only reported when the serving model is verified.
  phase?: string;
  verified?: boolean;
  verifiedModel?: string;
  verifiedContext?: number;
  degraded?: boolean;
  // v1.2.0 runtime vision readiness block (loading | ready | degraded |
  // failed | projector-found | projector-missing | supported |
  // unsupported) with the evidence reason and measured projector size.
  visionState?: string;
  visionReason?: string;
  visionProjector?: string;
  visionProjectorName?: string;
  visionProjectorBytes?: number;
  visionActive?: boolean;
  // v1.2.3: live asset-download progress (llama.cpp archive, model
  // packages) from the Download Manager. Present only mid-download.
  download?: DownloadProgress;
  // v1.2.5: watchdog auto-restart attempts for the current alive episode
  // (bounded, reset after a stable healthy episode). Present when > 0 so
  // the UI can report recovery honestly next to the detail line.
  restarts?: number;
  logs?: string[];
  cacheStats?: {
    entries: number;
    bytes: number;
    maxBytes: number;
    hits: number;
    misses: number;
    evictions: number;
    hitRatio: number;
  };
  // v1.5.0 MODEL-FIRST: true when a local install has NO selected model —
  // the deterministic pre-selection state. The composer gates on it and
  // the Model Selector owns the next step; the engine never loads an
  // arbitrary model on its own.
  selectionRequired?: boolean;
  // v1.5.0: the backend-authoritative model-selection state machine
  // (select → analyzing → configuring → loading → ready | failed, plus
  // calibrating) once a selection flow has run.
  selection?: SelectionState;
  timestamp: string;
}

// v1.5.0 MODEL-FIRST: the selection flow state — every phase transition
// is decided by the backend; the UI only renders it.
export type SelectionPhase =
  | "select"
  | "analyzing"
  | "configuring"
  | "loading"
  | "calibrating"
  | "ready"
  | "failed";

export interface SelectionAppliedProfile {
  context?: number;
  threads?: number;
  threadsBatch?: number;
  gpuLayers?: number;
  gpuAutoOffload?: boolean;
  flashAttention?: boolean;
  kvCacheQuant?: string;
  ubatchSize?: number;
  runtimeProfile?: string;
  predictedClass?: string;
  predictedVramBytes?: number;
}

export interface SelectionCalibrationMeasurement {
  candidate: string;
  booted?: boolean;
  measured?: boolean;
  ttftSeconds?: number;
  genTokensPerSec?: number;
  promptTokensPerSec?: number;
  totalSeconds?: number;
  stable?: boolean;
  note?: string;
}

export interface SelectionCalibration {
  ran: boolean;
  reason?: string;
  winner?: string;
  changed?: boolean;
  measurements?: SelectionCalibrationMeasurement[];
}

export interface SelectionState {
  phase: SelectionPhase;
  model?: string;
  modelPath?: string;
  previousModel?: string;
  performanceMode: "auto" | "manual";
  applied?: SelectionAppliedProfile;
  evidence?: string[];
  calibrated?: boolean;
  calibration?: SelectionCalibration;
  calibrationNote?: string;
  error?: string;
  updatedAt: string;
}

// v1.5.0: the per-model recommendation evidence served by
// /api/models/recommendations — the ONLY source of the picker's
// "Recommended for this machine" label (never filename/size heuristics).
export interface ModelRecommendationEvidence {
  class?: string;
  speed?: string;
  context?: number;
  gpuLayers?: number;
  gpuAutoOffload?: boolean;
  predictedVramBytes?: number;
  reasons?: string[];
  notes?: string[];
  hardwareMeasured?: boolean;
}

export interface ModelsRecommendationsResponse {
  task: string;
  hardwareMeasured: boolean;
  recommendations: Record<string, ModelRecommendationEvidence>;
}

// v1.6.1: the first-class local GGUF import result. The backend validates
// the GGUF header before any bytes move, streams the file into the managed
// models directory atomically (duplicate-safe, source untouched) and
// returns the model id the existing selection flow accepts.
export interface ModelImportResult {
  ok: boolean;
  name: string;
  path: string;
  sizeBytes: number;
  duplicate: boolean;
  renamedFrom?: string;
  architecture?: string;
  quantization?: string;
  contextLength?: number;
  parameterInfo?: string;
}

// v1.6.1: the native platform GGUF file picker result (Windows comdlg32;
// other hosts answer an honest error the UI falls back from).
export interface ModelPickResult {
  canceled: boolean;
  paths: string[];
}

// v1.1.5 Phase 1: native engine status block. The state vocabulary is
// the same engine state union above; the UI badge must keep following
// the llama.cpp snapshot state until the native engine serves generation.
export interface NativeEngineStatus {
  selected: boolean;
  available: boolean;
  path?: string;
  state: EngineState;
  detail?: string;
  pid?: number;
  uptimeSeconds?: number;
  restarts?: number;
}

// v1.1.3: staged attachment metadata returned by the backend.
export interface AttachmentChunk {
  id: string;
  attId: string;
  index: number;
  hash: string;
  offset: number;
  bytes: number;
  tokens: number;
  preview: string;
}

export interface Attachment {
  id: string;
  name: string;
  kind: "text" | "image" | "binary";
  size: number;
  sha256: string;
  createdAt: string;
  chunks?: AttachmentChunk[];
  note?: string;
  sessionIds?: string[];
}

export interface UploadResponse {
  ok: boolean;
  attachments: Attachment[];
  failed: { name: string; error: string }[];
}

// v1.1.3: full session payload with persisted conversation history.
export interface SessionDetail extends Session {
  messages?: ChatMessage[];
  context?: {
    systemPrompt?: string;
    attachedFiles?: string[];
    attachmentIds?: string[];
    maxIterations?: number;
    // v1.1.6: per-session context-window policy in tokens
    // (0/absent = inherit the global configured context).
    contextTokens?: number;
    // v1.2.8: the attached cross-mode history references (stable ids —
    // the sessions are retrieved per run, never merged).
    historyRefs?: HistoryRef[];
  };
}

// v1.1.6: the backend-resolved context decision for one session. The
// backend is authoritative — the UI never computes a different truth.
export interface SessionContextOption {
  tokens: number;
  label: string;
  classification: "safe" | "caution" | "unsupported";
  available: boolean;
  reason?: string;
  modelClamped?: boolean;
  engineClamped?: boolean;
}

export interface SessionContextStatus {
  sessionId: string;
  requested: number;
  configured: number;
  sessionPolicy?: number;
  effective: number;
  modelMax?: number;
  engineMax?: number;
  usableInput: number;
  outputReserve: number;
  safetyReserve: number;
  used: number;
  remaining: number;
  pressure: number;
  classification: "safe" | "caution" | "unsupported";
  resourceReason?: string;
  reasons?: string[];
  options: SessionContextOption[];
}

export interface ChatMessage {
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  reasoning?: string;
  attachments?: string[];
  images?: string[];
  feedback?: number;
  at?: string;
}

// v1.1.7 — live performance, log viewer and connection diagnostics.

export interface CompatInfo {
  level: number;
  name: string;
  optimised: boolean;
  reason?: string;
  recordedAt?: string;
  changes?: string[];
}

export interface EnginePerfAPI {
  hasSample: boolean;
  genTokPerSec?: number;
  promptTokPerSec?: number;
  ttftSeconds?: number;
  promptTokens?: number;
  tokens?: number;
  samplesAvailable: number;
}

export interface PerfSnapshot {
  generatedAt: string;
  cpuPercent?: number | null;
  gpuPercent?: number | null;
  ram?: {
    totalBytes: number;
    availableBytes: number;
    usedBytes: number;
    usedPercent: number;
  } | null;
  vram?: {
    totalMb: number;
    usedMb: number;
    percent?: number;
    source: string;
  } | null;
  backend: string;
  engineState: string;
  model?: string;
  compat: CompatInfo;
  generation: EnginePerfAPI;
  context?: {
    usedTokens: number;
    total: number;
    percent?: number;
    source?: string;
  } | null;
  recommended?: RecommendedSettings | null;

  // v1.2.5: per-request measured timelines (Advanced/System diagnostics).
  requests?: RequestTimingEntry[];
  /** the data-ownership ladder (ACTIVE/SESSION/HOT/COLD/EXPIRED/RELEASE) */
  ownership?: OwnershipLevelEntry[];
  toolCache?: {
    resultHits: number;
    resultMisses: number;
    resultEntries: number;
    resultBytes: number;
  } | null;
}

/** One recent turn's measured timeline (v1.2.5). */
export interface RequestTimingEntry {
  at: string;
  sessionId?: string;
  tier?: string;
  finalTier?: string;
  escalations?: number;
  thinkingControl?: string;
  toolPolicyMode?: string;
  firstPromptTokens?: number;
  classifyMs?: number;
  contextMs?: number;
  promptMs?: number;
  serializationMs?: number;
  ttftMs?: number;
  generationMs?: number;
  toolMs?: number;
  verificationMs?: number;
  totalMs?: number;
  toolCalls?: number;
  verified?: string;
}

/** One ownership-ladder row (v1.2.5). */
export interface OwnershipLevelEntry {
  level: string;
  owners?: number;
  names?: string[];
  bytes?: number;
  at?: string;
  description: string;
}

export interface RecommendedSettings {
  threads: number;
  threadsBatch: number;
  gpuLayers: number;
  gpuAutoOffload: boolean;
  flashAttention: boolean;
  flashAttnState: string;
  ubatchSize: number;
  cacheReuse: number;
  context: number;
  notes?: string[];
}

// v1.2.4: Workspace work-environment surface.
export interface WorkspaceFileEntry {
  name: string;
  size: number;
  modTime: string;
}

export interface WorkspaceProjectFacts {
  root: string;
  languages?: string[];
  buildCmd?: string;
  testCmd?: string;
  lintCmd?: string;
  layout?: string;
  entryHint?: string;
  conventions?: string[];
  lessons?: string[];
  buildVerifiedAt?: string;
  testVerifiedAt?: string;
  lastObservedAt?: string;
}

// v1.3.4 (ROADMAP v1.4 slice 1): repository intelligence payloads —
// mirrored from internal/repoindex (the Go package is the authority).
export interface RepoIndexStatus {
  root: string;
  state: "empty" | "ready" | "stale";
  files?: number;
  symbols?: number;
  depEdges?: number;
  testLinks?: number;
  languages?: Record<string, number>;
  updatedAt?: string;
  gitAvailable?: boolean;
  truncated?: boolean;
  staleRoot?: boolean;
  partial?: boolean;
}

export interface RepoIndexUpdateReport {
  root: string;
  indexed: number;
  reindexed: number;
  removed: number;
  unchanged: number;
  partial?: boolean;
  truncated?: boolean;
  durationMs?: number;
  gitAvailable?: boolean;
}

export interface RepoIndexRefreshResult {
  report: RepoIndexUpdateReport;
  status: RepoIndexStatus;
}

export interface RepoSearchQuery {
  text?: string;
  symbol?: string;
  path?: string;
  language?: string;
  role?: string;
  task?: string;
  depsOf?: string;
  usedBy?: string;
  testsOf?: string;
  limit?: number;
}

export interface RepoIndexResult {
  path: string;
  language?: string;
  role?: string;
  score: number;
  evidence: string;
  symbols?: string[];
  line?: number;
  gitState?: string;
  recent?: boolean;
}

export interface RepoSearchReport {
  root: string;
  totalHits: number;
  returned: number;
  truncated?: boolean;
  results: RepoIndexResult[];
  indexAge?: string;
  durationMs?: number;
}

export interface WorkspaceSummary {
  root: string;
  isDefault: boolean;
  exists: boolean;
  recentFiles?: WorkspaceFileEntry[];
  project?: WorkspaceProjectFacts | null;
  projectCard?: string;
  recentWorkspaces?: string[];
  activeSession?: {
    id: string;
    title?: string;
    model?: string;
    messages: number;
    updatedAt?: string;
  } | null;
  runtime?: {
    backend?: string;
    engineState?: string;
    model?: string;
  } | null;
  agent?: {
    running: boolean;
    state?: string;
  } | null;
}

export interface WorkspaceSwitchResult {
  ok: boolean;
  previous?: string;
  summary: WorkspaceSummary;
  changed?: string[];
}

// v1.3.0: GitHub clone progress/result (server-driven; the UI polls at
// ~1.2 s while state === "running").
export type CloneState = "idle" | "running" | "succeeded" | "failed" | "canceled";

export interface CloneStatus {
  state: CloneState;
  phase?: "validating" | "cloning" | "verifying";
  url?: string;
  repoName?: string;
  owner?: string;
  branch?: string;
  ssh?: boolean;
  destination?: string;
  percent?: number;
  message?: string;
  startedAt?: string;
  finishedAt?: string;
  exitCode?: number;
  head?: string;
  outputTail?: string[];
  errorKind?: string;
  errorMessage?: string;
  // Server-side post-clone actions:
  switched?: boolean;
  switchNote?: string;
  summary?: WorkspaceSummary;
}

export interface LogEntry {
  index: number;
  time: string;
  level: string;
  category: string;
  message: string;
  raw: string;
}

export interface LogsResponse {
  entries: LogEntry[];
  limit: number;
}

export type NetDiagState =
  "Excellent" | "Good" | "Unstable" | "Slow" | "Offline";

export interface NetDiagResult {
  state: NetDiagState;
  reason?: string;
  latencyMs?: number;
  latency2Ms?: number;
  dnsOk: boolean;
  httpsOk: boolean;
  endpointOk: boolean;
  checkedAt: string;
  totalMs: number;
}

// v1.7.0 — Automation / Tasks: the scheduler surface (task CRUD, runs,
// task-scoped tools, task artifacts). Shapes mirror internal/scheduler
// (Task, Report, Schedule) and internal/artifacts (Meta); optional fields
// arrive omitted ("omitempty"). Backend quirk the UI must tolerate: Go
// zero time.Time values are NOT omitted by omitempty, so `nextDue` and
// scheduleV17.at can arrive as "0001-01-01T00:00:00Z" and mean "unset".

export type AutomationTrigger =
  | "manual"
  | "timer"
  | "startup"
  | "file_change"
  | "git_change"
  | "test_failure"
  | "ci_failure"
  | "build_failure"
  | "scheduled_maintenance";

export type AutomationScheduleKind = "once" | "interval" | "daily" | "weekly";

export interface AutomationSchedule {
  kind?: AutomationScheduleKind;
  at?: string; // RFC3339 (once schedules)
  timeOfDay?: string; // "HH:MM" local wall-clock (daily/weekly)
  weekday?: number; // 0=Sunday … 6=Saturday (weekly)
}

// AutomationRunReport is one settled run (GET …/runs, task.lastRun).
export interface AutomationRunReport {
  taskId: string;
  taskName: string;
  trigger: string;
  startedAt: string;
  durationMs: number;
  ok: boolean;
  output?: string;
  err?: string;
  canceled?: boolean;
}

export interface AutomationTask {
  id: string;
  name: string;
  trigger: AutomationTrigger;
  prompt: string;
  intervalSeconds?: number;
  maxRuntimeSeconds?: number;
  enabled: boolean;
  paused?: boolean;
  scheduleV17?: AutomationSchedule;
  linkedSkills?: string[];
  taskTools?: string[];
  taskTypes?: string[];
  lastRun?: AutomationRunReport;
  nextDue?: string;
  created: string;
  running: boolean;
}

// AutomationTaskInput is the create/update body (POST/PUT …/tasks).
export interface AutomationTaskInput {
  id?: string;
  name: string;
  trigger: AutomationTrigger;
  prompt: string;
  intervalSeconds?: number;
  maxRuntimeSeconds?: number;
  scheduleKind?: AutomationScheduleKind;
  at?: string; // RFC3339 (once)
  timeOfDay?: string; // "HH:MM" (daily/weekly)
  weekday?: number; // 0..6 (weekly)
}

// AutomationTaskTool is one task-scoped tool definition (the SAME
// customtools contract plus task ownership and the approval gate).
// Safe defaults: created disabled + unapproved; Approve flips both.
export interface AutomationTaskTool {
  id: string;
  name: string;
  taskId: string;
  approved: boolean;
  enabled: boolean;
  shortDescription?: string;
  description?: string;
  executionType?: string;
  permission?: string;
  timeoutSeconds?: number;
  outputLimitBytes?: number;
}

export type AutomationArtifactKind =
  | "doc"
  | "code"
  | "data"
  | "chart"
  | "image"
  | "archive"
  | "diagnostics"
  | "other";

export interface AutomationArtifact {
  id: string;
  taskId: string;
  runId?: string;
  source: string;
  created: string;
  kind: AutomationArtifactKind;
  title?: string;
  path: string;
  relPath: string;
  size: number;
  version: number;
  hash?: string;
  isCurrent?: boolean;
}

export interface AutomationArtifactInput {
  taskId: string;
  filename: string;
  content: string;
  title?: string;
}

const DEFAULT_TIMEOUT_MS = 15_000;
const LONG_OPERATION_TIMEOUT_MS = 5 * 60_000;
const uploadTimeoutMs = 2 * 60_000;

async function requestRaw(path: string): Promise<string> {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new Error(`request failed: ${response.status} ${path}`);
  }
  return response.text();
}

async function request<T>(
  path: string,
  init?: RequestInit,
  timeoutMs = DEFAULT_TIMEOUT_MS,
): Promise<T> {
  const controller = new AbortController();
  let timeoutTriggered = false;

  const timeout = window.setTimeout(() => {
    timeoutTriggered = true;
    controller.abort();
  }, timeoutMs);

  const callerSignal = init?.signal;

  const abortFromCaller = () => {
    controller.abort();
  };

  if (callerSignal) {
    if (callerSignal.aborted) {
      controller.abort();
    } else {
      callerSignal.addEventListener("abort", abortFromCaller, { once: true });
    }
  }

  try {
    const response = await fetch(`${API_BASE}${path}`, {
      ...init,
      signal: controller.signal,
      headers: {
        Accept: "application/json",
        ...(init?.body
          ? {
              "Content-Type": "application/json",
            }
          : {}),
        ...init?.headers,
      },
    });

    if (!response.ok) {
      const body = await response.text();
      let message = body;

      try {
        const parsed = JSON.parse(body) as {
          error?: string;
        };

        if (typeof parsed.error === "string" && parsed.error.trim()) {
          message = parsed.error;
        }
      } catch {
        // Preserve raw response text.
      }

      throw new Error(message || `Request failed with HTTP ${response.status}`);
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return (await response.json()) as T;
  } catch (error) {
    if (timeoutTriggered) {
      throw new Error(
        `Request timed out after ${Math.round(timeoutMs / 1000)} seconds.`,
      );
    }

    throw error;
  } finally {
    window.clearTimeout(timeout);

    if (callerSignal) {
      callerSignal.removeEventListener("abort", abortFromCaller);
    }
  }
}

export const api = {
  state(): Promise<AppState> {
    return request<AppState>("/state");
  },
  preflight: (context?: number, signal?: AbortSignal): Promise<PreflightReport> => {
    const suffix = context && context > 0 ? `?context=${context}` : "";
    return request<PreflightReport>(`/preflight${suffix}`, { signal });
  },

  sysinfo(signal?: AbortSignal): Promise<SysInfo> {
    return request<SysInfo>("/sysinfo", { signal });
  },

  presets(signal?: AbortSignal): Promise<Preset[]> {
    return request<Preset[]>("/presets", { signal });
  },

  models(signal?: AbortSignal): Promise<ModelsResponse> {
    return request<ModelsResponse>("/models", { signal });
  },

  // v1.5.0: per-model recommendation evidence for the picker — the only
  // source of the "Recommended for this machine" label. v1.5.1: the task
  // travels with the request so the evidence is computed for the ACTIVE
  // surface (chat vs agent), never the backend's silent default.
  modelsRecommendations(task?: string, signal?: AbortSignal): Promise<ModelsRecommendationsResponse> {
    const suffix = task ? `?task=${encodeURIComponent(task)}` : "";
    return request<ModelsRecommendationsResponse>(`/models/recommendations${suffix}`, { signal });
  },

  // v1.5.0 MODEL-FIRST: select a model BEFORE any engine/model load. The
  // backend drives the whole chain (analyze → configure atomically →
  // load → verify → ready, plus the bounded AUTO calibration) and the
  // returned state is backend-authoritative. v1.5.1: every surface sends
  // its intended task explicitly (chat | agent) — the backend must never
  // tune one task while the UI shows another.
  selectModel(model: string, task?: string): Promise<SelectionState> {
    return request<SelectionState>("/models/select", {
      method: "POST",
      body: JSON.stringify({ model, task }),
    });
  },

  // v1.6.1: first-class local GGUF import — the backend validates the
  // GGUF header, streams the file into the managed models directory
  // (atomic, duplicate-safe, source untouched) and returns the imported
  // model id, which immediately drives selectModel above.
  importModel(path: string): Promise<ModelImportResult> {
    return request<ModelImportResult>("/models/import", {
      method: "POST",
      body: JSON.stringify({ path }),
    });
  },

  // v1.6.1: the native platform GGUF file picker (Windows). Returns the
  // chosen absolute paths; canceled picks answer { canceled: true }.
  pickModelFiles(): Promise<ModelPickResult> {
    return request<ModelPickResult>("/models/import/pick", {
      method: "POST",
    });
  },

  tools(signal?: AbortSignal): Promise<ToolInfo[]> {
    return request<ToolInfo[]>("/tools", { signal });
  },

  // v1.6.0 P3: custom tools CRUD + test execution. Every endpoint is
  // the backend's single authority (validation, atomic persistence,
  // registry integration).
  customTools(signal?: AbortSignal): Promise<CustomToolDefinition[]> {
    return request<CustomToolDefinition[]>("/custom-tools", { signal });
  },

  createCustomTool(
    payload: Omit<CustomToolDefinition, "id" | "enabled">,
  ): Promise<CustomToolDefinition> {
    return request<CustomToolDefinition>("/custom-tools", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  updateCustomTool(
    id: string,
    payload: CustomToolDefinition,
  ): Promise<CustomToolDefinition> {
    return request<CustomToolDefinition>(`/custom-tools/${id}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  },

  deleteCustomTool(id: string): Promise<void> {
    return request<void>(`/custom-tools/${id}`, { method: "DELETE" });
  },

  testCustomTool(
    id: string,
    args: Record<string, unknown>,
  ): Promise<CustomToolTestResult> {
    return request<CustomToolTestResult>(`/custom-tools/${id}/test`, {
      method: "POST",
      body: JSON.stringify({ args }),
    });
  },

  // v1.6.0 P0: the startup maintenance gate state — truthful engine
  // maintenance lifecycle for the UI (spec §12).
  maintenance(signal?: AbortSignal): Promise<MaintenanceStatus> {
    return request<MaintenanceStatus>("/maintenance", { signal });
  },

  engineProvisionState(signal?: AbortSignal): Promise<EngineProvisionState> {
    return request<EngineProvisionState>("/engine/provision", { signal });
  },

  engineProvision(variant: string, signal?: AbortSignal): Promise<EngineProvisionResult> {
    return request<EngineProvisionResult>(
      "/engine/provision",
      {
        method: "POST",
        body: JSON.stringify({ variant }),
        signal,
      },
      // The transaction downloads a full engine package, swaps it,
      // restarts the engine and verifies the runtime backend — minutes,
      // not seconds (DEFAULT_TIMEOUT_MS is 15 s).
      10 * 60_000,
    );
  },

  config(signal?: AbortSignal): Promise<RuntimeConfig> {
    return request<RuntimeConfig>("/config", { signal });
  },

  updateConfig(payload: Record<string, unknown>): Promise<RuntimeConfig> {
    return request<RuntimeConfig>("/config", {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  },

  llama(action: "start" | "stop" | "cancel-download"): Promise<unknown> {
    return request<unknown>(
      "/llama",
      {
        method: "POST",
        body: JSON.stringify({ action }),
      },
      action === "cancel-download" ? 15_000 : LONG_OPERATION_TIMEOUT_MS,
    );
  },

  sessions(mode?: string): Promise<Session[]> {
    // v1.2.8: `mode` filters ONE conversation space; without it the full
    // list is served (compat).
    const suffix = mode === "chat" || mode === "agent" ? `?mode=${mode}` : "";
    return request<Session[]>(`/sessions${suffix}`);
  },

  session(id: string): Promise<Session> {
    return request<Session>(`/sessions/${encodeURIComponent(id)}`);
  },

  createSession(mode?: string): Promise<Session> {
    // v1.2.8: the created session is FIXED to one conversation space. An
    // empty body keeps the backend default (agent) for old clients.
    return request<Session>("/sessions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(mode ? { mode } : {}),
    });
  },

  deleteSession(id: string): Promise<void> {
    return request<void>(`/sessions/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  // v1.2.8: picker search over titles + rolling summaries. mode "chat" |
  // "agent" browses ONE space; undefined browses all (cross-mode picker).
  historySearch(q: string, mode?: string, limit = 8): Promise<HistoryHit[]> {
    const params = new URLSearchParams();
    if (q.trim() !== "") {
      params.set("q", q);
    }
    if (mode === "chat" || mode === "agent") {
      params.set("mode", mode);
    }
    params.set("limit", String(limit));
    return request<HistoryHit[]>(`/history/search?${params.toString()}`);
  },

  // v1.2.8: the session's durable rolling summary (version-0 shell when
  // no turn has settled yet — never fabricated content).
  sessionSummary(id: string): Promise<SessionSummaryView> {
    return request<SessionSummaryView>(
      `/sessions/${encodeURIComponent(id)}/summary`,
    );
  },

  // v1.2.8: lazy history paging — the newest page by default; `before`
  // (exclusive message index) fetches older pages.
  sessionMessagesPage(
    id: string,
    before?: number,
    limit = 60,
  ): Promise<MessagesPage> {
    const params = new URLSearchParams();
    if (typeof before === "number" && before >= 0) {
      params.set("before", String(before));
    }
    params.set("limit", String(limit));
    return request<MessagesPage>(
      `/sessions/${encodeURIComponent(id)}/messages?${params.toString()}`,
    );
  },

  run(payload: RunRequest): Promise<RunResponse> {
    return request<RunResponse>(
      "/run",
      {
        method: "POST",
        body: JSON.stringify(payload),
      },
      LONG_OPERATION_TIMEOUT_MS,
    );
  },

  // v1.1.3: authoritative engine snapshot (poll target).
  engine(): Promise<EngineSnapshot> {
    return request<EngineSnapshot>("/engine");
  },

  // v1.1.6: the backend-resolved context status for one session
  // (selector options, usage, pressure, resource verdict).
  sessionContext(id: string): Promise<SessionContextStatus> {
    return request<SessionContextStatus>(
      `/sessions/${encodeURIComponent(id)}/context`,
    );
  },

  // v1.1.6: set the per-session context policy (tokens; 0 = inherit the
  // global context). The choice travels with THIS session only.
  setSessionContext(
    id: string,
    contextTokens: number,
  ): Promise<SessionContextStatus> {
    return request<SessionContextStatus>(
      `/sessions/${encodeURIComponent(id)}/context`,
      {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ contextTokens }),
      },
    );
  },

  // v1.1.6 first-use: open the models directory in the OS file manager.
  openModelsFolder(): Promise<{ ok: boolean; path?: string }> {
    return request<{ ok: boolean; path?: string }>("/models/open-folder", {
      method: "POST",
    });
  },

  // v1.1.3: staged attachments.
  attachments(): Promise<{ attachments: Attachment[]; limits?: unknown }> {
    return request("/attachments");
  },

  uploadAttachments(sessionId: string, files: File[]): Promise<UploadResponse> {
    const form = new FormData();

    for (const file of files) {
      form.append("files", file);
    }

    const controller = new AbortController();

    const timeout = window.setTimeout(() => {
      controller.abort();
    }, uploadTimeoutMs);

    return fetch(
      `${API_BASE}/attachments?sessionId=${encodeURIComponent(sessionId)}`,
      {
        method: "POST",
        body: form,
        signal: controller.signal,
      },
    )
      .then(async (response) => {
        if (!response.ok) {
          const text = await response.text();

          let message = text;

          try {
            const parsed = JSON.parse(text) as { error?: string };

            if (parsed.error) {
              message = parsed.error;
            }
          } catch {
            // Raw body.
          }

          throw new Error(message || `Upload failed (HTTP ${response.status})`);
        }

        return (await response.json()) as UploadResponse;
      })
      .finally(() => {
        window.clearTimeout(timeout);
      });
  },

  deleteAttachment(id: string): Promise<{ ok: boolean }> {
    return request(`/attachments/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  sessionDetail(id: string): Promise<SessionDetail> {
    return request<SessionDetail>(
      `/sessions/${encodeURIComponent(id)}`,
      undefined,
      DEFAULT_TIMEOUT_MS,
    );
  },

  // v1.1.4: recall feedback — 👍/👎 on a past exchange steers future
  // recall scoring (the backend sidecar existed since v1.0.6 with no
  // writer).
  feedback(payload: {
    sessionId: string;
    query: string;
    liked: boolean;
    clear?: boolean;
  }): Promise<void> {
    return request<void>("/feedback", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  abort(sessionId: string): Promise<AbortResponse> {
    return request<AbortResponse>("/abort", {
      method: "POST",
      body: JSON.stringify({
        sessionId,
      }),
    });
  },

  // v1.7.4 — the backend-neutral Pause / Edit / Resume contract over the
  // EXISTING /api/run authority. One runId, one revision, idempotent
  // mutations; stale revisions are rejected with the current value.
  pauseRun(sessionId: string, runId: string): Promise<RunControlResponse> {
    return request<RunControlResponse>("/run/pause", {
      method: "POST",
      body: JSON.stringify({ sessionId, runId }),
    });
  },

  resumeRun(
    sessionId: string,
    runId: string,
    revision: number,
  ): Promise<RunControlResponse> {
    return request<RunControlResponse>("/run/resume", {
      method: "POST",
      body: JSON.stringify({ sessionId, runId, revision }),
    });
  },

  editRun(payload: {
    sessionId: string;
    runId: string;
    revision: number;
    message?: string;
    draft?: string;
  }): Promise<RunControlResponse> {
    return request<RunControlResponse>("/run/edit", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  pausedRuns(sessionId: string): Promise<PausedRunSummary[]> {
    return request<PausedRunSummary[]>(
      `/run/paused?sessionId=${encodeURIComponent(sessionId)}`,
      { method: "GET" },
    );
  },

  updateSession(
    id: string,
    payload: {
      title?: string;
      model?: string;
      context?: {
        systemPrompt?: string;
        attachedFiles?: string[];
        maxIterations?: number;
        contextTokens?: number;
        // v1.2.8: attached cross-mode history references.
        historyRefs?: HistoryRef[];
      };
      // v1.2.8.1: ATOMIC history-reference delta — replaces just the ref
      // list server-side under the store lock (no whole-context PUT, so a
      // stale read-modify-write can never revert concurrent updates).
      historyRefs?: HistoryRef[];
    },
  ): Promise<Session> {
    return request<Session>(`/sessions/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  },

  // v1.1.7: compact live performance snapshot (real measurements only;
  // unavailable values arrive null and render as N/A).
  perf(): Promise<PerfSnapshot> {
    return request<PerfSnapshot>("/perf");
  },

  // v1.3.4 (ROADMAP v1.4 slice 1): repository intelligence — index
  // status, bounded refresh and the hybrid search entry point.
  repoIndex(signal?: AbortSignal): Promise<RepoIndexStatus> {
    return request<RepoIndexStatus>("/repo/index", { signal });
  },

  repoIndexRefresh(): Promise<RepoIndexRefreshResult> {
    return request<RepoIndexRefreshResult>("/repo/index/refresh", {
      method: "POST",
    }, 30_000);
  },

  repoSearch(query: RepoSearchQuery): Promise<RepoSearchReport> {
    return request<RepoSearchReport>("/repo/search", {
      method: "POST",
      body: JSON.stringify(query),
    }, 30_000);
  },

  // v1.2.4: Workspace — one compact work-environment summary.
  workspace(): Promise<WorkspaceSummary> {
    return request<WorkspaceSummary>("/workspace");
  },

  // v1.2.4: reveal the workspace root in the OS file manager / terminal.
  workspaceReveal(
    target: "files" | "terminal" = "files",
  ): Promise<{ ok: boolean; path: string }> {
    return request("/workspace/reveal", {
      method: "POST",
      body: JSON.stringify({ target }),
    });
  },

  // v1.2.4: switch the current project folder (refused while a run is
  // active; the response states exactly what changed).
  workspaceSwitch(path: string): Promise<WorkspaceSwitchResult> {
    return request("/workspace/switch", {
      method: "POST",
      body: JSON.stringify({ path }),
    });
  },

  // v1.3.0: first-class GitHub clone workflow. POST returns immediately
  // with the first status snapshot (the clone runs server-side, tree-
  // killable, bounded); the UI polls cloneStatus at ~1.2 s until a
  // terminal state, exactly like the app-update download flow.
  cloneStart(input: {
    url: string;
    destination?: string;
    branch?: string;
    openAfter?: boolean;
  }): Promise<CloneStatus> {
    return request<CloneStatus>(
      "/workspace/clone",
      {
        method: "POST",
        body: JSON.stringify(input),
      },
      30_000,
    );
  },

  cloneStatus(): Promise<CloneStatus> {
    return request<CloneStatus>("/workspace/clone/status");
  },

  cloneCancel(): Promise<{ ok: boolean; canceling: boolean }> {
    return request("/workspace/clone/cancel", {
      method: "POST",
    });
  },

  // v1.1.7: recent, parsed, REDACTED app-log entries (bounded server-side).
  logs(limit = 512): Promise<LogsResponse> {
    return request<LogsResponse>(`/logs?limit=${limit}`);
  },

  // v1.1.7: one bounded connection diagnosis (no retries, never a gate —
  // SHEYTAN is local-first).
  netcheck(): Promise<NetDiagResult> {
    return request<NetDiagResult>("/netcheck", undefined, 20_000);
  },

  // v1.2.0: Environment Centre — the unified device/runtime/recommendation
  // view over existing telemetry. v1.2.2: accepts an AbortSignal so the
  // System tab can cancel its probes on navigation.
  environment(signal?: AbortSignal): Promise<EnvironmentPayload> {
    return request<EnvironmentPayload>("/environment", { signal }, 20_000);
  },

  // v1.2.0: verified health — one check per subsystem WITH evidence.
  // v1.2.2: accepts an AbortSignal (cancellable refresh).
  health(signal?: AbortSignal): Promise<HealthPayload> {
    return request<HealthPayload>("/health", { signal }, 20_000);
  },

  // v1.8.0: the runtime governor / system-health read model.
  governor(signal?: AbortSignal): Promise<GovernorPayload> {
    return request<GovernorPayload>("/governor", { signal }, 20_000);
  },

  // v1.2.0: evidence-based runtime recommendation for a model + task
  // profile (Detected → Calculated → Recommended; applying stays explicit).
  recommendation(model = "", task = ""): Promise<RecommendationPayload> {
    const q = new URLSearchParams();
    if (model) q.set("model", model);
    if (task) q.set("task", task);
    const qs = q.toString();
    return request<RecommendationPayload>(
      `/recommendation${qs ? `?${qs}` : ""}`,
      undefined,
      30_000,
    );
  },

  // v1.2.0: application update surface (manifest-verified).
  updateStatus(): Promise<AppUpdateStatus> {
    return request<AppUpdateStatus>("/update/status");
  },

  updateCheck(): Promise<AppUpdateStatus> {
    return request<AppUpdateStatus>(
      "/update/check",
      { method: "POST" },
      60_000,
    );
  },

  updateDownload(): Promise<AppUpdateStatus> {
    return request<AppUpdateStatus>(
      "/update/download",
      { method: "POST" },
      LONG_OPERATION_TIMEOUT_MS,
    );
  },

  // v1.2.3: stop the running staging download immediately. The .part
  // file stays on disk so a retry resumes instead of restarting.
  updateCancel(): Promise<AppUpdateStatus> {
    return request<AppUpdateStatus>("/update/cancel", { method: "POST" });
  },

  lab(): Promise<LabListResponse> {
    return request<LabListResponse>("/lab");
  },

  labTask(id: string): Promise<LabTaskSessionSnapshot> {
    return request<LabTaskSessionSnapshot>(`/lab/${encodeURIComponent(id)}`);
  },

  labAction(payload: Record<string, unknown>): Promise<LabActionResponse> {
    return request<LabActionResponse>(
      "/lab",
      {
        method: "POST",
        body: JSON.stringify(payload),
      },
      LONG_OPERATION_TIMEOUT_MS,
    );
  },

  // ---------------------------------------------------------------------
  // v1.9.0 — the ONE AI System authority (CRUD, activation, clone,
  // export/import) and the durable long-horizon Goal engine. Thin,
  // honest adapters over the backend surfaces; no client-side policy.
  // ---------------------------------------------------------------------

  systems(): Promise<SystemsPayload> {
    return request<SystemsPayload>("/systems");
  },

  system(id: string): Promise<AISystem> {
    return request<AISystem>(`/systems/${encodeURIComponent(id)}`);
  },

  createSystem(payload: Partial<AISystem>): Promise<AISystem> {
    return request<AISystem>("/systems", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
  },

  updateSystem(id: string, payload: Partial<AISystem>): Promise<AISystem> {
    return request<AISystem>(`/systems/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
  },

  deleteSystem(id: string): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/systems/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  activateSystem(id: string): Promise<SystemActivationResponse> {
    return request<SystemActivationResponse>(
      `/systems/${encodeURIComponent(id)}/activate`,
      { method: "POST" },
    );
  },

  cloneSystem(id: string): Promise<AISystem> {
    return request<AISystem>(`/systems/${encodeURIComponent(id)}/clone`, {
      method: "POST",
    });
  },

  async exportSystem(id: string): Promise<string> {
    const response = await requestRaw(
      `/systems/${encodeURIComponent(id)}/export`,
    );
    return response;
  },

  importSystem(document: string): Promise<AISystem> {
    return request<AISystem>("/systems/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: document,
    });
  },

  goals(): Promise<GoalsPayload> {
    return request<GoalsPayload>("/goals");
  },

  goal(id: string): Promise<GoalPayload> {
    return request<GoalPayload>(`/goals/${encodeURIComponent(id)}`);
  },

  createGoal(payload: { goal: string; sessionId?: string; systemId?: string; start?: boolean }): Promise<Goal> {
    return request<Goal>("/goals", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
  },

  goalAction(id: string, action: "start" | "pause" | "resume" | "cancel" | "approve" | "reject", approvalId?: string): Promise<unknown> {
    return request<unknown>(`/goals/${encodeURIComponent(id)}/${action}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(approvalId ? { approvalId } : {}),
    });
  },

  deleteGoal(id: string): Promise<{ ok: boolean }> {
    return request<{ ok: boolean }>(`/goals/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  // --- v1.7.0 Automation / Tasks -------------------------------------------

  automationTasks(signal?: AbortSignal): Promise<AutomationTask[]> {
    return request<AutomationTask[]>("/automation/tasks", { signal });
  },

  automationTask(id: string, signal?: AbortSignal): Promise<AutomationTask> {
    return request<AutomationTask>(
      `/automation/tasks/${encodeURIComponent(id)}`,
      { signal },
    );
  },

  createAutomationTask(payload: AutomationTaskInput): Promise<AutomationTask> {
    return request<AutomationTask>("/automation/tasks", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  updateAutomationTask(
    id: string,
    payload: AutomationTaskInput,
  ): Promise<AutomationTask> {
    return request<AutomationTask>(`/automation/tasks/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  },

  deleteAutomationTask(id: string): Promise<{ deleted: boolean }> {
    return request(`/automation/tasks/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  // Run now is asynchronous by default (?wait=1 blocks for the report —
  // reserved for callers that want the settled result; the panel never
  // blocks). Conflicts (paused / already running) surface as errors.
  runAutomationTask(
    id: string,
    wait = false,
  ): Promise<AutomationRunReport | { started: boolean; taskId: string }> {
    return request(
      `/automation/tasks/${encodeURIComponent(id)}/run${wait ? "?wait=1" : ""}`,
      { method: "POST" },
      LONG_OPERATION_TIMEOUT_MS,
    );
  },

  pauseAutomationTask(id: string): Promise<{ paused: boolean }> {
    return request(`/automation/tasks/${encodeURIComponent(id)}/pause`, {
      method: "POST",
    });
  },

  resumeAutomationTask(id: string): Promise<{ resumed: boolean }> {
    return request(`/automation/tasks/${encodeURIComponent(id)}/resume`, {
      method: "POST",
    });
  },

  cancelAutomationTask(id: string): Promise<{ canceled: boolean }> {
    return request(`/automation/tasks/${encodeURIComponent(id)}/cancel`, {
      method: "POST",
    });
  },

  automationTaskRuns(id: string, limit = 50): Promise<AutomationRunReport[]> {
    return request<AutomationRunReport[]>(
      `/automation/tasks/${encodeURIComponent(id)}/runs?limit=${limit}`,
    );
  },

  automationTaskTools(id: string): Promise<AutomationTaskTool[]> {
    return request<AutomationTaskTool[]>(
      `/automation/tasks/${encodeURIComponent(id)}/tools`,
    );
  },

  approveAutomationTaskTool(toolId: string): Promise<AutomationTaskTool> {
    return request<AutomationTaskTool>(
      `/automation/task-tools/${encodeURIComponent(toolId)}/approve`,
      { method: "POST" },
    );
  },

  automationArtifacts(taskId: string): Promise<AutomationArtifact[]> {
    const params = new URLSearchParams();
    params.set("task", taskId);
    return request<AutomationArtifact[]>(
      `/automation/artifacts?${params.toString()}`,
    );
  },

  createAutomationArtifact(
    payload: AutomationArtifactInput,
  ): Promise<AutomationArtifact> {
    return request<AutomationArtifact>("/automation/artifacts", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  automationArtifact(id: string): Promise<AutomationArtifact> {
    return request<AutomationArtifact>(
      `/automation/artifacts/${encodeURIComponent(id)}`,
    );
  },

  deleteAutomationArtifact(id: string): Promise<{ deleted: boolean }> {
    return request(`/automation/artifacts/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },

  automationArtifactVersions(id: string): Promise<AutomationArtifact[]> {
    return request<AutomationArtifact[]>(
      `/automation/artifacts/${encodeURIComponent(id)}/versions`,
    );
  },

  // Artifact content is RAW (markdown/text/image bytes), never JSON —
  // this path bypasses the JSON request wrapper on purpose. The backend
  // answers with a deny-all sandbox CSP; the UI never evaluates it.
  async automationArtifactContent(id: string, signal?: AbortSignal): Promise<string> {
    const response = await fetch(
      `${API_BASE}/automation/artifacts/${encodeURIComponent(id)}/content`,
      { signal },
    );

    if (!response.ok) {
      const text = await response.text();

      let message = text;

      try {
        const parsed = JSON.parse(text) as { error?: string };

        if (parsed.error) {
          message = parsed.error;
        }
      } catch {
        // Raw body (the content endpoint only speaks raw).
      }

      throw new Error(message || `Artifact content failed (HTTP ${response.status})`);
    }

    return response.text();
  },

  // The content endpoint URL for <img src> (SVG/images — the content is
  // NEVER inlined into the application DOM) and download links.
  automationArtifactContentURL(id: string): string {
    return `${API_BASE}/automation/artifacts/${encodeURIComponent(id)}/content`;
  },

  // v1.3.6 (spec §21/§26): the standalone research client is removed —
  // Net Search flows through the per-request run contract (netSearch on
  // /api/run) and the same backend service remains reachable at
  // /api/net-search. No second search implementation exists here.
};

// v1.3.0: re-exported for convenience (the implementation lives in
// clone-url.ts — a pure, node:test-importable module).
export { parseGitHubUrl } from "./clone-url";

// PreflightReport — v1.7.1: the ONE authoritative pre-run compatibility
// report served by GET /api/preflight. The frontend renders this verdict;
// it NEVER recalculates compatibility locally.
export type PreflightSeverity =
  | "ok"
  | "warning"
  | "high_pressure"
  | "critical_pressure"
  | "incompatible";

export interface PreflightMemory {
  modelBytes?: number;
  kvCacheBytes?: number;
  runtimeOverheadBytes?: number;
  totalBytes: number;
  contextTokens?: number;
}

export interface PreflightAvailable {
  ramTotalBytes?: number;
  ramAvailableBytes?: number;
  vramBytes?: number;
}

export interface PreflightReport {
  model: string;
  backend: string;
  device?: string;
  compatible: boolean;
  severity: PreflightSeverity;
  reasons?: string[];
  requirements: PreflightMemory;
  available: PreflightAvailable;
  safetyMarginPct?: number;
  recommendedAction?: string;
  unknown?: string[];
  refusalMessage?: string;
}


// ---------------------------------------------------------------------------
// v1.9.0 — AI Systems + Goals payloads.
// ---------------------------------------------------------------------------

export interface AISystem {
  systemId: string;
  name: string;
  revision: number;
  instructions: string;
  model?: string;
  reasoning?: string;
  allowedTools?: string[];
  approvalPolicy: string;
  enabledSkills?: string[];
  knowledgeRefs?: string[];
  memoryPolicy?: string;
  compactionPolicy?: string;
  runtimeProfile?: string;
  verificationPolicy: string;
  createdAt: string;
  updatedAt: string;
}

export interface SystemsPayload {
  systems: AISystem[];
  activeSystemId: string;
}

export interface SystemActivationResponse {
  ok: boolean;
  active: string;
  revision: number;
  snapshot: Record<string, unknown>;
}

export interface GoalPlanStep {
  index: number;
  objective: string;
  status: string;
  result?: string;
}

export interface GoalEvidence {
  seq: number;
  phase: string;
  action: string;
  target?: string;
  status: string;
  detail?: string;
}

export interface GoalPendingApproval {
  approvalId: string;
  toolName: string;
  args?: Record<string, unknown>;
  risk: string;
  reason?: string;
}

export interface Goal {
  goalId: string;
  sessionId?: string;
  systemId?: string;
  systemRevision?: number;
  originalGoal: string;
  phase: string;
  status: string;
  plan?: GoalPlanStep[];
  currentStep: number;
  lastAction?: string;
  nextAction?: string;
  evidence?: GoalEvidence[];
  changedFiles?: string[];
  pendingApproval?: GoalPendingApproval;
  verification?: string;
  verificationPassed: boolean;
  turnBudget: number;
  turnsUsed: number;
  replans: number;
  createdAt: string;
  updatedAt: string;
}

export interface GoalsPayload {
  goals: Goal[];
}

export interface GoalPayload {
  goal: Goal;
  driving: boolean;
  done: number;
  total: number;
}
