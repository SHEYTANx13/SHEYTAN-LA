import {
  type ChangeEvent,
  type ClipboardEvent,
  type DragEvent,
  type FormEvent,
  type KeyboardEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { api, type EngineState, type RuntimeConfig } from "./api";
import ComposerControls from "./ComposerControls";
import { DownloadProgressPanel } from "./DownloadProgress";
import { MaintenanceBanner } from "./MaintenanceBanner";
import { initializeAgent } from "./agent-init";
import { isPausedPhase } from "./run-phase";
import MessageStream, { AttachmentChip } from "./MessageStream";
import ActivityStream from "./ActivityStream";
import HistoryPicker from "./HistoryPicker";
import AgentTaskPanel from "./AgentTaskPanel";
import { describeRange, refLabel } from "./history-ref";
import ModelPicker from "./ModelPicker";
import PerfStrip from "./PerfStrip";
import { useRuntimeStore } from "./store";
import { visionBadge } from "./vision";

// engineBadge maps the authoritative backend engine states to a visible
// label + severity. The UI NEVER invents a state: unknown backend states
// render verbatim with the neutral style.
function engineBadge(state: EngineState | undefined, provider: string): {
  label: string;
  severity: "good" | "warn" | "bad" | "neutral";
  busy: boolean;
} {
  if (state === "remote") {
    return { label: "Remote endpoint", severity: "good", busy: false };
  }

  switch (state) {
    case "ready":
      return { label: "Engine ready", severity: "good", busy: false };

    case "running":
      return { label: "Engine running", severity: "good", busy: false };

    case "busy":
      return { label: "Inference running", severity: "good", busy: true };

    case "starting":
      return { label: "Engine starting…", severity: "warn", busy: true };

    case "downloading":
      return { label: "Downloading engine…", severity: "warn", busy: true };

    case "stopping":
      return { label: "Engine stopping…", severity: "warn", busy: true };

    case "failed":
      return { label: "Engine failed", severity: "bad", busy: false };

    case "stopped":
      return { label: "Engine stopped", severity: "neutral", busy: false };

    case "idle":
      return {
        label: provider === "remote" ? "Remote provider" : "Engine idle",
        severity: "neutral",
        busy: false,
      };

    default:
      return { label: state ?? "Unknown", severity: "neutral", busy: false };
  }
}

// v1.1.6 §9: user-facing labels for the backend startup phases.
const PHASE_LABELS: Record<string, string> = {
  "downloading-engine": "Downloading engine…",
  "loading-model": "Loading model…",
  "checking-capabilities": "Checking capabilities…",
  "preparing-context": "Preparing context…",
  "model-selection": "Waiting for a model choice…",
  calibrating: "Calibrating performance…",
  stopping: "Stopping…",
  stopped: "Engine stopped",
  failed: "Engine failed",
};

// formatBytes renders the estimated-memory figure.
function formatBytes(bytes: number): string {
  if (bytes >= 1024 ** 3) {
    return `${(bytes / 1024 ** 3).toFixed(1)} GiB`;
  }
  if (bytes >= 1024 ** 2) {
    return `${Math.round(bytes / 1024 ** 2)} MiB`;
  }
  return `${bytes} B`;
}

// v1.1.9: shared option builder — the chat rail and the agent runtime
// panel previously duplicated identical <select> option JSX.
function modelSelectOptions(
  localModels: ReturnType<typeof useRuntimeStore.getState>["models"],
) {
  const models = localModels?.local ?? [];

  return (
    <>
      <option value="">
        {models.length === 0 ? "No local GGUF models found" : "Select local model"}
      </option>

      {models.map((model) => (
        <option key={model.id} value={model.id}>
          {model.name}
          {model.quantization ? ` · ${model.quantization}` : ""}
          {model.parameterInfo ? ` · ${model.parameterInfo}` : ""}
        </option>
      ))}
    </>
  );
}

function AgentBody() {
  const models = useRuntimeStore((state) => state.models);
  const sessions = useRuntimeStore((state) => state.sessions);
  const activeSessionId = useRuntimeStore((state) => state.activeSessionId);
  const loading = useRuntimeStore((state) => state.loading);
  const error = useRuntimeStore((state) => state.error);
  const running = useRuntimeStore((state) => state.running);
  const engine = useRuntimeStore((state) => state.engine);

  // v1.7.4 run control: the authoritative pause state machine phase, the
  // accepted partial draft and the checkpoint revision — all strictly
  // backend-derived (run_snapshot / status Detail), never local truth.
  const runPhase = useRuntimeStore((state) => state.runPhase);
  const pausedDraft = useRuntimeStore((state) => state.pausedDraft);
  const pauseRun = useRuntimeStore((state) => state.pauseRun);
  const resumeRun = useRuntimeStore((state) => state.resumeRun);
  const editRun = useRuntimeStore((state) => state.editRun);
  const composerPaused = isPausedPhase(runPhase);

  // Local edit buffer for the paused partial answer — synced from the
  // backend-derived draft every time the run enters PAUSED (reconnect
  // included), never while the run is live.
  const [draftEdit, setDraftEdit] = useState("");

  useEffect(() => {
    if (composerPaused) {
      setDraftEdit(pausedDraft ?? "");
    }
  }, [composerPaused, pausedDraft]);

  // v1.5.0 MODEL-FIRST: a fresh install with no selected model. The
  // composer gates on this honest backend signal and the Model Selector
  // owns the next step (the engine never loads an arbitrary model).
  const selectionRequired = engine?.selectionRequired === true;

  // v1.8.5: the user-facing Start/Stop ENGINE TOGGLE IS REMOVED — the
  // engine is supervised machinery, not a manual control: startup,
  // engine updates, restart-after-change, recovery and shutdown are
  // owned by the internal lifecycle paths (startup gate,
  // EnsureLLMContext run gate, the settings restart flow, shutdown), and
  // the UI REPRESENTS engine state (the runtime pill in the header, the
  // engine badge in Settings, the live phase in the generation timeline)
  // instead of requiring the user to operate it.
  const pendingAttachments = useRuntimeStore((state) => state.pendingAttachments);
  const attachmentsUploading = useRuntimeStore((state) => state.attachmentsUploading);

  const createSession = useRuntimeStore((state) => state.createSession);
  const deleteSession = useRuntimeStore((state) => state.deleteSession);
  const run = useRuntimeStore((state) => state.run);
  const abort = useRuntimeStore((state) => state.abort);
  const regenerate = useRuntimeStore((state) => state.regenerate);
  const uploadFiles = useRuntimeStore((state) => state.uploadFiles);
  const removePendingAttachment = useRuntimeStore(
    (state) => state.removePendingAttachment,
  );
  const refreshModels = useRuntimeStore((state) => state.refreshModels);
  // v1.2.2: ownership instead of raw connect/disconnect. AgentBody
  // ACQUIRES the activity socket + engine poll on mount and RELEASES on
  // unmount — but a LIVE run keeps both alive across workspace tab
  // switches (the backend hub has no event replay; dropping the socket
  // mid-run previously lost `done` and froze the composer forever).
  const acquireActivity = useRuntimeStore((state) => state.acquireActivity);
  const releaseActivity = useRuntimeStore((state) => state.releaseActivity);
  const acquireEnginePolling = useRuntimeStore(
    (state) => state.acquireEnginePolling,
  );
  const releaseEnginePolling = useRuntimeStore(
    (state) => state.releaseEnginePolling,
  );

  // v1.4.0: the composer draft moved to the runtime store. The
  // workspace <main key> remounts this component on every view switch —
  // a component-local draft was destroyed by the Agent → Settings →
  // Agent round trip. The store-backed draft survives remounts and mode
  // switches.
  const message = useRuntimeStore((state) => state.composerDraft);
  const setMessage = useRuntimeStore((state) => state.setComposerDraft);
  const [config, setConfig] = useState<RuntimeConfig | null>(null);
  const [modelBusy, setModelBusy] = useState(false);
  const [modelError, setModelError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [showActivity, setShowActivity] = useState(false);

  // v1.2.8: cross-mode history references + the picker's open state.
  const historyRefs = useRuntimeStore((state) => state.historyRefs);
  const detachHistoryRef = useRuntimeStore((state) => state.detachHistoryRef);
  const setHistoryPickerOpen = useRuntimeStore(
    (state) => state.setHistoryPickerOpen,
  );

  // v1.1.8: Chat / Agent mode surfaces. Both modes share THIS component's
  // session/model/engine wiring — only what is VISIBLE differs. Chat gets
  // a slim model rail and the full-screen picker; Agent keeps the runtime
  // panel, activity, and telemetry.
  const mode = useRuntimeStore((state) => state.mode);
  const chatMode = mode === "chat";

  // Picker visibility: automatic when no usable model exists, overridable
  // by the user (null = follow the automatic state).
  const [pickerOverride, setPickerOverride] = useState<boolean | null>(null);
  // v1.5.0: selectionRequired (backend: no model selected) also forces
  // the picker — the model-first flow.
  const needPicker =
    (models?.local ?? []).length === 0 ||
    !config?.model ||
    engine?.selectionRequired === true;
  const pickerOpen = pickerOverride ?? needPicker;

  useEffect(() => {
    let cancelled = false;

    void initializeAgent()
      .then(async () => {
        const runtimeConfig = await api.config();

        if (cancelled) {
          return;
        }

        setConfig(runtimeConfig);
        acquireActivity();
        acquireEnginePolling();
      })
      .catch(() => {
        // Initialization records the main error in the runtime store.
      });

    return () => {
      cancelled = true;
      // v1.2.2: release (not hard-disconnect). A live run keeps the
      // socket + poll; the store releases them when the run settles.
      // This is what makes tab switching mid-generation safe: no stale
      // sockets, no lost `done`, no duplicate connections on return.
      releaseActivity();
      releaseEnginePolling();
    };
  }, [acquireActivity, releaseActivity, acquireEnginePolling, releaseEnginePolling]);

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId) ?? null,
    [sessions, activeSessionId],
  );

  const localModels = models?.local ?? [];

  const badge = engineBadge(engine?.state, engine?.provider ?? "local");

  // v1.1.6 §10: the card facts of the model that will serve (selection
  // order: chosen config model → engine-served model).
  const selectedModelCard = useMemo(() => {
    const wanted = config?.model || engine?.model || activeSession?.model;
    if (!wanted) {
      return null;
    }
    return (
      localModels.find((m) => m.id === wanted) ??
      localModels.find((m) => wanted.startsWith(m.id)) ??
      null
    );
  }, [localModels, config?.model, engine?.model, activeSession?.model]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const value = message.trim();

    // v1.7.4 PAUSED: the composer edits the paused run — submitting
    // applies the edited user prompt (and any draft edit) and resumes the
    // SAME run. It never starts a second run over a parked one.
    if (composerPaused) {
      event.preventDefault();

      if (draftEdit.trim() !== (pausedDraft ?? "")) {
        const ok = await editRun({
          draft: draftEdit,
          message: value || undefined,
        });

        if (!ok) {
          // Stale revision — the next run_snapshot re-syncs the paused
          // state; nothing is resumed from outdated data.
          return;
        }
      } else if (value) {
        const ok = await editRun({ message: value });

        if (!ok) {
          return;
        }
      }

      setMessage("");
      await resumeRun();
      return;
    }

    if (!value || running) {
      return;
    }

    setMessage("");

    try {
      await run(value);
    } catch {
      // Store exposes the runtime error.
    }
  }

  async function handleNewSession() {
    try {
      await createSession();
    } catch {
      // Store exposes the runtime error.
    }
  }

  async function handleDeleteSession() {
    if (!activeSessionId) {
      return;
    }

    try {
      await deleteSession(activeSessionId);
    } catch {
      // Store exposes the runtime error.
    }
  }

  async function handleRegenerate() {
    try {
      await regenerate();
    } catch {
      // Store exposes the runtime error.
    }
  }

  // v1.5.0 MODEL-FIRST: the switch goes through the backend selection
  // flow — ONE call that resolves the model, applies the configuration
  // atomically (the AUTO profile recalculated for the new model; MANUAL
  // preserves explicit values), starts or swaps the engine without a
  // wasteful stop/start when the model already matches, and verifies
  // the selected model is actually serving. The old three-step client
  // dance (patch config → stop → start) is gone.
  async function switchModel(nextModel: string): Promise<boolean> {
    if (!nextModel || nextModel === config?.model || modelBusy) {
      return false;
    }

    // Changing the model while a run is active must be blocked — the
    // backend enforces the same lock (409), the UI refuses up front.
    if (running) {
      setModelError(
        "A run is active — model changes are locked while a generation is in flight.",
      );
      return false;
    }

    setModelBusy(true);
    setModelError(null);

    try {
      // v1.5.1: the ACTIVE SURFACE's task travels with the selection —
      // Chat selects with TaskChat, Agent with TaskAgent. The backend
      // must never tune `chat` while the user is working in the Agent
      // surface (spec §5): the same model may legitimately receive a
      // different runtime profile per task.
      await api.selectModel(nextModel, mode);

      const nextConfig = await api.config();
      setConfig(nextConfig);

      await refreshModels();

      return true;
    } catch (switchError) {
      setModelError(
        switchError instanceof Error
          ? switchError.message
          : "Unable to switch model.",
      );

      return false;
    } finally {
      setModelBusy(false);
    }
  }

  // v1.8.5: toggleEngine is REMOVED from the ordinary workflow (manual
  // engine operation was a workflow hazard, not a feature): the engine
  // boots on first use through the run gate (EnsureLLMContext), restarts
  // through the settings restart flow after engine-affecting changes,
  // and recovers through the internal supervision paths. The engine
  // STATE stays visible (the badge below + the runtime pill). Download
  // cancellation — an asset-level action, not engine lifecycle — remains
  // user-facing.

  function handleFileSelection(event: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.target.files ?? []);

    event.target.value = "";

    if (files.length === 0 || !activeSessionId) {
      return;
    }

    stageWithPreviews(files);
  }

  const onPickFiles = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  // v1.2.0 — composer image support. Paste and drag/drop feed the SAME
  // existing upload backend (store.uploadFiles → /api/attachments) — no
  // second upload implementation. Image previews are client-side object
  // URLs keyed by name+size, revoked when the attachment is removed.
  const previewMapRef = useRef<Map<string, string>>(new Map());
  const [, forcePreviewTick] = useState(0);

  const previewFor = useCallback(
    (attachment: { name: string; size: number }): string | undefined => {
      return previewMapRef.current.get(`${attachment.name}:${attachment.size}`);
    },
    [],
  );

  const registerPreviews = useCallback((files: File[]) => {
    let added = false;

    for (const file of files) {
      if (file.type.startsWith("image/")) {
        const url = URL.createObjectURL(file);
        previewMapRef.current.set(`${file.name}:${file.size}`, url);
        added = true;
      }
    }

    if (added) {
      forcePreviewTick((tick) => tick + 1);
    }
  }, []);

  const stageWithPreviews = useCallback(
    (files: File[]) => {
      registerPreviews(files);
      void uploadFiles(files).catch(() => {
        // Store exposes the upload error.
      });
    },
    [registerPreviews, uploadFiles],
  );

  useEffect(() => {
    const map = previewMapRef.current;

    return () => {
      for (const url of map.values()) {
        URL.revokeObjectURL(url);
      }
      map.clear();
    };
  }, []);

  const handlePaste = useCallback(
    (event: ClipboardEvent<HTMLTextAreaElement>) => {
      const files = Array.from(event.clipboardData?.files ?? []);
      if (files.length === 0 || !activeSessionId) {
        return; // normal text paste
      }
      event.preventDefault();
      stageWithPreviews(files);
    },
    [activeSessionId, stageWithPreviews],
  );

  const handleDrop = useCallback(
    (event: DragEvent<HTMLElement>) => {
      event.preventDefault();
      setDragActive(false);

      const files = Array.from(event.dataTransfer?.files ?? []);
      if (files.length === 0 || !activeSessionId) {
        return;
      }
      stageWithPreviews(files);
    },
    [activeSessionId, stageWithPreviews],
  );

  const [dragActive, setDragActive] = useState(false);

  const handleDragOver = useCallback((event: DragEvent<HTMLElement>) => {
    event.preventDefault();
    if (event.dataTransfer?.types?.includes("Files")) {
      setDragActive(true);
    }
  }, []);

  const handleDragLeave = useCallback((event: DragEvent<HTMLElement>) => {
    event.preventDefault();
    setDragActive(false);
  }, []);

  // v1.2.0: Enter sends, Shift+Enter inserts a newline (Ctrl/Cmd+Enter
  // still works for muscle memory), and the textarea auto-grows to the
  // content up to a bounded height.
  const composerRef = useRef<HTMLTextAreaElement | null>(null);

  const autoGrow = useCallback(() => {
    const el = composerRef.current;
    if (!el) {
      return;
    }
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 220)}px`;
  }, []);

  useEffect(() => {
    autoGrow();
  }, [message, autoGrow]);

  const handleComposerKeyDown = useCallback(
    (event: KeyboardEvent<HTMLTextAreaElement>) => {
      if (
        event.key === "Enter" &&
        !event.shiftKey &&
        !event.ctrlKey &&
        !event.metaKey &&
        !event.nativeEvent.isComposing
      ) {
        event.preventDefault();
        event.currentTarget.form?.requestSubmit();
      }
    },
    [],
  );

  // v1.2.0: the runtime vision state of the SERVING engine (not a guess
  // from model names). The composer uses it for the honest image hint.
  const vision = visionBadge(
    engine?.visionState,
    engine?.visionReason,
    engine?.visionProjectorName,
  );
  const visionUsable =
    vision.tone === "good" || engine?.visionState === "projector-found";

  const canAttach = Boolean(activeSessionId) && !attachmentsUploading;

  useEffect(() => {
    function handleNewSessionRequest() {
      void handleNewSession();
    }

    window.addEventListener("sheytan:new-session", handleNewSessionRequest);

    return () => {
      window.removeEventListener(
        "sheytan:new-session",
        handleNewSessionRequest,
      );
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [createSession]);

  return (
    <>
      <section
        className={`workspace-content${chatMode ? " mode-chat" : ""}`}
      >
        {chatMode && (
          <div className="chat-rail">
            <span
              className={`status-dot severity-${badge.severity} ${
                badge.busy ? "pulsing" : ""
              }`}
            />

            <span className="chat-rail-state">{badge.label}</span>

            <select
              className="runtime-select chat-rail-select"
              value={config?.model ?? ""}
              onChange={(event) => void switchModel(event.target.value)}
              disabled={
                modelBusy ||
                config?.provider !== "local" ||
                localModels.length === 0
              }
              aria-label="Model"
            >
              {modelSelectOptions(models)}
            </select>

            <button
              type="button"
              className="text-button"
              onClick={() => setPickerOverride(!pickerOpen)}
            >
              {pickerOpen ? "Hide models" : "All models"}
            </button>

            {/* v1.2.0: the runtime vision badge — Ready only when the engine
                is actually serving with a verified projector. */}
            <span
              className={`vision-chip tone-${vision.tone}`}
              title={vision.title}
            >
              <span className="vision-chip-symbol">{vision.symbol}</span>
              Vision {vision.label}
            </span>
          </div>
        )}

        {/* v1.1.8: the model picker owns the content area whenever it is
            open (automatic while no usable model exists). Otherwise the
            conversation stream fills the same space. */}
        {pickerOpen ? (
          <ModelPicker
            activeModel={
              config?.model ?? engine?.model ?? activeSession?.model ?? null
            }
            busy={modelBusy}
            task={mode}
            onUse={(id) => {
              void switchModel(id).then((applied) => {
                if (applied) {
                  setPickerOverride(false);
                }
              });
            }}
            onClose={() => setPickerOverride(false)}
          />
        ) : (
          <MessageStream />
        )}

        {!chatMode && (
        <aside className="runtime-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">RUNTIME</span>
              <strong>System</strong>
            </div>
          </div>

          {/* v1.2.8: the agent pipeline panel — the REAL task state
              (goal/step/files/commands/tests/verification) maintained by
              the backend from observed tool traffic. Renders nothing when
              the run has produced no task evidence. */}
          <AgentTaskPanel />

          <div className="runtime-section">
            <span className="eyebrow">ENGINE</span>

            <div className="engine-status">
              <span
                className={`status-dot severity-${badge.severity} ${
                  badge.busy ? "pulsing" : ""
                }`}
              />

              <div>
                <strong>{badge.label}</strong>

                <span>
                  {engine?.model
                    ? engine.model
                    : engine?.provider === "remote"
                      ? "Remote endpoint"
                      : "Local llama.cpp"}
                </span>
              </div>
            </div>

            {/* v1.1.6 §9: real startup progress states while the engine
                prepares — the UI stays usable during the whole flow. */}
            {engine?.phase && engine.phase !== "ready" && engine.phase !== "waiting" && (
              <span className="runtime-hint engine-phase">
                {PHASE_LABELS[engine.phase] ?? engine.phase}
              </span>
            )}

            {/* v1.6.0 P0/P4 (spec §12): the startup maintenance gate's
                REAL lifecycle — truthful phase sequence instead of a
                generic "Updating…", and an engine restart during the
                transaction is never presented as a crash. */}
            <MaintenanceBanner />

            {/* v1.1.6 §11: never hide degraded startup behind a green
                status — if "ready" could not be verified, say so. */}
            {engine?.degraded && (
              <span className="runtime-hint engine-phase engine-phase-warn">
                Degraded startup: the serving model could not be verified
                {engine.detail ? ` — ${engine.detail}` : ""}
              </span>
            )}

            {/* v1.2.3: live engine-asset download (llama.cpp archive,
                model packages) with measured bytes/speed/ETA from the
                Download Manager, cancellable mid-flight. */}
            {engine?.download && (
              <DownloadProgressPanel
                progress={engine.download}
                compact
                onCancel={() => void api.llama("cancel-download").catch(() => {})}
                cancelLabel="Stop download"
              />
            )}

            <div className="header-actions">
              <button
                type="button"
                className="text-button"
                onClick={() => setShowActivity((value) => !value)}
              >
                {showActivity ? "Hide activity" : "Show activity"}
              </button>
            </div>
          </div>

          {/* v1.1.6 §10 first use: when no valid model exists, give the
              user a clear path instead of a dead dropdown. v1.1.8: the
              button opens the real model picker (no DOM query hack). */}
          {localModels.length === 0 && !config?.model && (
            <div className="runtime-section first-use">
              <span className="eyebrow">GET STARTED</span>

              <strong className="first-use-title">No model selected</strong>

              <span className="runtime-hint">
                Place a GGUF model in the models folder, then choose it
                here. Path: <code>{config?.modelsDir || "models/"}</code>
              </span>

              <div className="header-actions">
                <button
                  type="button"
                  className="secondary-button"
                  onClick={() => setPickerOverride(true)}
                >
                  Choose model
                </button>

                <button
                  type="button"
                  className="text-button"
                  onClick={() => {
                    void api.openModelsFolder().catch(() => {
                      /* best effort; the path is shown above */
                    });
                  }}
                >
                  Open models folder
                </button>
              </div>
            </div>
          )}

          <div className="runtime-section">
            <span className="eyebrow">MODEL</span>

            <select
              className="runtime-select"
              value={config?.model ?? ""}
              onChange={(event) => void switchModel(event.target.value)}
              disabled={
                modelBusy ||
                config?.provider !== "local" ||
                localModels.length === 0
              }
              aria-label="Model"
            >
              {modelSelectOptions(models)}
            </select>

            <div className="session-detail">
              <span>ACTIVE</span>

              <strong>
                {engine?.model ||
                  config?.model ||
                  activeSession?.model ||
                  "No model selected"}
              </strong>
            </div>

            {/* v1.1.6 §10: the model card facts — backend-measured, no
                engine internals during normal startup. v1.1.9: renamed
                class (runtime-model-facts) — it previously collided with
                the picker's inline facts line of the same name. */}
            {selectedModelCard && (
              <div className="runtime-model-facts">
                {selectedModelCard.architecture && (
                  <div className="session-detail">
                    <span>Architecture</span>
                    <strong>{selectedModelCard.architecture}</strong>
                  </div>
                )}
                {selectedModelCard.quantization && (
                  <div className="session-detail">
                    <span>Quantization</span>
                    <strong>{selectedModelCard.quantization}</strong>
                  </div>
                )}
                {!!selectedModelCard.contextLength && (
                  <div className="session-detail">
                    <span>Context maximum</span>
                    <strong>{selectedModelCard.contextLength.toLocaleString()} tok</strong>
                  </div>
                )}
                {!!selectedModelCard.estimatedMemoryBytes && (
                  <div className="session-detail">
                    <span>Estimated memory</span>
                    <strong>{formatBytes(selectedModelCard.estimatedMemoryBytes)}</strong>
                  </div>
                )}
                <div className="session-detail">
                  <span>Backend</span>
                  <strong>{engine?.backend ?? "llama"}</strong>
                </div>
                <div className="session-detail">
                  <span>Status</span>
                  <strong>{badge.label}</strong>
                </div>
              </div>
            )}

            {modelBusy ? (
              <span className="runtime-hint">Switching model…</span>
            ) : null}
          </div>

          {/* v1.1.9: session identity is bookkeeping, not product UI — the
              raw ID moved out of sight (sessions live in the sidebar);
              only the actions remain. */}
          <div className="runtime-section">
            <span className="eyebrow">SESSION</span>

            <div className="header-actions">
              <button
                type="button"
                className="secondary-button"
                onClick={() => void handleNewSession()}
              >
                New session
              </button>

              <button
                type="button"
                className="secondary-button"
                onClick={() => void handleDeleteSession()}
                disabled={!activeSessionId}
              >
                Delete session
              </button>
            </div>
          </div>

          {/* v1.1.8: compact live telemetry in the Agent surface — same
              /api/perf source as the diagnostic HUD, never fabricated. */}
          <PerfStrip />

          {showActivity ? <ActivityStream /> : null}
        </aside>
        )}
      </section>

      <form
        className={`composer${dragActive ? " drag-active" : ""}`}
        onSubmit={handleSubmit}
        onDrop={handleDrop}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
      >
        {dragActive ? (
          <div className="composer-drop-hint">Drop images or files to attach</div>
        ) : null}

        {/* v1.5.0 MODEL-FIRST: no model selected yet — the honest gate.
            The button opens the Model Selector; the composer unlocks as
            soon as a selection is applied. */}
        {selectionRequired ? (
          <div className="composer-selection-gate" role="status">
            <span>
              Choose a model to start — local models are listed without
              loading any of them; the engine starts after you select one.
            </span>

            <button
              type="button"
              className="secondary-button"
              onClick={() => setPickerOverride(true)}
            >
              Choose model
            </button>
          </div>
        ) : null}

        {pendingAttachments.length > 0 || attachmentsUploading ? (
          <div className="composer-attachments">
            {attachmentsUploading ? (
              <span className="runtime-hint">Staging attachments…</span>
            ) : null}

            {pendingAttachments.map((attachment) => (
              <AttachmentChip
                key={attachment.id}
                attachment={attachment}
                previewUrl={previewFor(attachment)}
                onRemove={(id) => {
                  const url = previewMapRef.current.get(
                    `${attachment.name}:${attachment.size}`,
                  );
                  if (url) {
                    URL.revokeObjectURL(url);
                    previewMapRef.current.delete(
                      `${attachment.name}:${attachment.size}`,
                    );
                  }
                  void removePendingAttachment(id);
                }}
              />
            ))}
          </div>
        ) : null}

        {historyRefs.length > 0 ? (
          <div className="composer-history-refs" aria-label="Attached history references">
            {historyRefs.map((ref) => (
              <span
                key={ref.sessionId}
                className="history-ref-chip"
                title={describeRange(ref)}
              >
                <span className="history-ref-icon">⧉</span>

                <span className="history-ref-label">{refLabel(ref)}</span>

                <button
                  type="button"
                  className="history-ref-remove"
                  onClick={() => void detachHistoryRef(ref.sessionId)}
                  aria-label={`Detach history reference ${ref.sessionId}`}
                >
                  ×
                </button>
              </span>
            ))}

            <button
              type="button"
              className="text-button"
              onClick={() => setHistoryPickerOpen(true)}
            >
              + More
            </button>
          </div>
        ) : null}

        {/* v1.2.0: honest vision gating — an image staged for a model whose
            engine has NO projector gets a visible warning, not a silent
            downgrade. Text attachments are unaffected. */}
        {pendingAttachments.some((a) => a.kind === "image") &&
        !visionUsable &&
        engine?.visionState !== undefined ? (
          <div className="composer-vision-hint" role="status">
            {vision.symbol} The served model has no verified vision projector
            ({vision.label.toLowerCase()}) — images will be ignored.
          </div>
        ) : null}

        {/* v1.2.5: per-request controls — Thinking selector, tool policy,
            and the live status chip (backend-measured tier telemetry). */}
        <ComposerControls />

        <div className="composer-shell">
          <input
            ref={fileInputRef}
            type="file"
            multiple
            hidden
            onChange={handleFileSelection}
            aria-label="Attach files"
          />

          <textarea
            ref={composerRef}
            value={message}
            onChange={(event) => setMessage(event.target.value)}
            placeholder={
              selectionRequired
                ? "Choose a model to begin..."
                : !activeSessionId
                  ? chatMode
                    ? "Message SHEYTAN — sending starts a new session..."
                    : "Describe what SHEYTAN should forge — sending starts a new session..."
                  : chatMode
                    ? "Message SHEYTAN..."
                    : "Describe what SHEYTAN should forge..."
            }
            // v1.8.4 (P0-C): a ZERO-SESSION space no longer disables the
            // composer — pressing Send is exactly how the first session is
            // created (run() creates + activates it lazily and continues
            // the same send). The model gate (selectionRequired) and the
            // live-run gate keep their pre-1.8.4 semantics.
            disabled={(running && !composerPaused) || selectionRequired}
            rows={1}
            onKeyDown={(event) => {
              // v1.2.0: Enter = send, Shift+Enter = newline. Ctrl/Cmd+Enter
              // keeps its historical send binding.
              if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
                return;
              }
              handleComposerKeyDown(event);
            }}
            onPaste={handlePaste}
          />

          {/* v1.7.4 PAUSED PANEL: the checkpointed partial answer is
              editable (replace or trim) and the composer above edits the
              paused user prompt. Everything here acts on the SAME runId +
              revision — the backend rejects stale revisions with the
              current value, and the next run_snapshot re-syncs. */}
          {composerPaused ? (
            <div className="paused-run-panel">
              <div className="paused-run-title">
                Paused — edit the partial answer, then resume
              </div>

              <textarea
                className="paused-draft-editor"
                value={draftEdit}
                rows={4}
                onChange={(event) => setDraftEdit(event.target.value)}
                placeholder="The partial answer written before the pause…"
              />

              <div className="paused-run-actions">
                <button type="submit" className="send-button">
                  Update &amp; Resume
                </button>

                <button
                  type="button"
                  className="text-button"
                  onClick={() => void resumeRun()}
                >
                  Resume without changes
                </button>
              </div>
            </div>
          ) : null}

          <div className="composer-footer">
            <div className="composer-footer-left">
              <button
                type="button"
                className="text-button"
                onClick={onPickFiles}
                disabled={!canAttach}
                title="Attach files (text, code, images) — or paste / drag images"
              >
                {attachmentsUploading ? "Staging…" : "＋ Attach"}
              </button>

              {/* v1.2.8: the cross-mode history picker — attach other Chat /
                  Agent conversations as provenance-labeled references. */}
              <button
                type="button"
                className="text-button"
                onClick={() => setHistoryPickerOpen(true)}
                disabled={!activeSessionId}
                title="Attach another conversation (Chat or Agent) as a history reference"
              >
                ⧉ History
              </button>

              <span>
                {activeSessionId
                  ? "Enter to send · Shift+Enter for newline"
                  : selectionRequired
                    ? "Create a session first"
                    : "Sending starts a new session"}
              </span>
            </div>

            <div className="composer-footer-actions">
              {!running && activeSessionId ? (
                <button
                  type="button"
                  className="text-button"
                  onClick={() => void handleRegenerate()}
                  disabled={running}
                  title="Re-run the last user message"
                >
                  Regenerate
                </button>
              ) : null}

              {/* v1.7.4: PAUSE while the run is live — the orchestrator stops
                  at the next safe boundary; PAUSING/RESUMING keep the button
                  disabled so conflicting mutations are never double-sent. */}
              {running && !composerPaused ? (
                <button
                  type="button"
                  className="text-button"
                  onClick={() => void pauseRun()}
                  disabled={runPhase === "pausing" || runPhase === "resuming"}
                  title="Pause the run at the next safe boundary — the partial answer is checkpointed"
                >
                  {runPhase === "pausing" ? "Pausing…" : "Pause"}
                </button>
              ) : null}

              {running && !composerPaused ? (
                <button
                  type="button"
                  className="stop-button"
                  onClick={() => void abort()}
                >
                  Stop
                </button>
              ) : null}

              {composerPaused ? (
                <button
                  type="button"
                  className="text-button"
                  onClick={() => void resumeRun()}
                  title="Resume the paused run from its checkpoint"
                >
                  Resume
                </button>
              ) : null}

              {composerPaused ? (
                <button
                  type="button"
                  className="stop-button"
                  onClick={() => void abort()}
                  title="Stop the paused run and discard its draft"
                >
                  Stop
                </button>
              ) : null}

              {!running ? (
                <button
                  type="submit"
                  className="send-button"
                  // v1.8.4 (P0-C): Send is enabled in a zero-session space
                  // — it creates + activates a session in the current mode
                  // and continues the run (see run()'s lazy creation).
                  disabled={!message.trim() || loading || selectionRequired}
                >
                  {chatMode ? "Send" : "Forge →"}
                </button>
              ) : null}
            </div>
          </div>
        </div>
      </form>

      {modelError ? (
        <div className="error-banner" role="alert">
          <span>{modelError}</span>
        </div>
      ) : null}

      {error ? (
        <div className="error-banner" role="alert">
          <span>{error}</span>
        </div>
      ) : null}

      {/* v1.2.8: the cross-mode history picker dialog. */}
      <HistoryPicker />
    </>
  );
}

export default AgentBody;
