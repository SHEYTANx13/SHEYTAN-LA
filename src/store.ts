import { create } from "zustand";

import {
  api,
  type ActivityEvent as APIActivityEvent,
  type AppState,
  type Attachment,
  type ChatMessage,
  type EngineSnapshot,
  type HistoryHit,
  type HistoryRef,
  type LabListResponse,
  type LabTaskSessionSnapshot,
  type ModelsResponse,
  type Preset,
  type Session,
  type SysInfo,
  type ToolInfo,
  type SessionContextStatus,
} from "./api";
import {
  mergeHistoryRefs,
  normalizeHistoryRefs,
  removeHistoryRef as removeHistoryRefById,
} from "./history-ref";
import { parseEndedAt } from "./run-recovery";
import {
  crossModePickerFilter,
  resolveActiveForMode,
  resolveModeSwitchTarget,
} from "./mode-sessions";
import { activityWebSocketURL, ensureActivityWsOrigin } from "./config";
import {
  recordRunEnd,
  recordRunStart,
  recordStreamTrace,
} from "./stream-trace";
import { resolveInitialView, viewModeBinding } from "./workspace";
import {
  isLivePhase,
  nextPhase,
  planFinalisation,
  type RunEventKind,
  type RunPhase,
} from "./run-phase";
import { createSessionListGuard } from "./session-list-guard";
import {
  AnimationFrameController,
  MessageChannelTaskController,
  StreamFlushScheduler,
  TimeoutTaskController,
} from "./stream-flush-scheduler";
import {
  extractMemoryEvidence,
  type MemoryEvidenceState,
} from "./memory-evidence";
import {
  normalizeEventKind,
  normalizeThinkingControl,
  normalizeToolAllowlist,
  normalizeToolPolicyMode,
  type ThinkingControl,
  type ToolPolicyMode,
  // v1.3.6: Net Search control state + wire-event evidence helpers.
  type NetSearchState,
  isResearchToolStart,
  isResearchToolEnd,
  researchEndFailed,
  extractNetSearchResultCount,
} from "./run-events";
import {
  applyReasoningSnapshot,
  applyResponseSnapshot,
  createStreamingAccumulator,
  flush as flushAccumulator,
  isEmpty as accumulatorIsEmpty,
  mergeSnapshot,
} from "./stream-accumulator";
import { StreamingFastPath } from "./stream-fast-path";

export type ConnectionState =
  "idle" | "connecting" | "connected" | "disconnected" | "error";

// v1.1.8: top-level mode separation. Chat is the calm conversation
// surface; Agent is the engineering/action surface. Both reuse the same
// sessions, model runtime, and engine infrastructure underneath — only
// the exposed controls differ.
export type WorkspaceMode = "chat" | "agent";

const MODE_STORAGE_KEY = "sheytan.mode";

function initialWorkspaceMode(): WorkspaceMode {
  // v1.6.0 repair (spec §10): the boot conversation space follows the
  // SAME deterministic initial-view resolution the App renders from
  // (explicit URL hash > remembered workspace view > Chat), so the
  // store's mode and the shell's view can never disagree at first
  // paint. The legacy sheytan.mode key is honored only when the resolved
  // view is a shared layer (workspace/system/settings) that does not
  // bind a conversation space.
  const bound = viewModeBinding(resolveInitialView());
  if (bound) {
    return bound;
  }

  try {
    const stored = window.localStorage.getItem(MODE_STORAGE_KEY);
    return stored === "agent" ? "agent" : "chat";
  } catch {
    return "chat";
  }
}

function persistWorkspaceMode(mode: WorkspaceMode): void {
  try {
    window.localStorage.setItem(MODE_STORAGE_KEY, mode);
  } catch {
    // Storage can be unavailable (private mode) — mode stays in memory.
  }
}

// v1.2.8.1: the PER-MODE active session selection persists like the mode
// itself. v1.2.8 kept activeSessionByMode memory-only, so a reload landed
// on the newest session of the startup space instead of the conversation
// the user had actually selected (and the other space's selection was
// forgotten entirely).
const ACTIVE_SESSIONS_STORAGE_KEY = "sheytan.activeSessionByMode";

function initialActiveSessionByMode(): Record<WorkspaceMode, string | null> {
  try {
    const raw = window.localStorage.getItem(ACTIVE_SESSIONS_STORAGE_KEY);

    if (!raw) {
      return { chat: null, agent: null };
    }

    const parsed = JSON.parse(raw) as Partial<
      Record<WorkspaceMode, string | null>
    >;

    return {
      chat: typeof parsed.chat === "string" ? parsed.chat : null,
      agent: typeof parsed.agent === "string" ? parsed.agent : null,
    };
  } catch {
    return { chat: null, agent: null };
  }
}

function persistActiveSessionByMode(
  map: Record<WorkspaceMode, string | null>,
): void {
  try {
    window.localStorage.setItem(
      ACTIVE_SESSIONS_STORAGE_KEY,
      JSON.stringify(map),
    );
  } catch {
    // in-memory only
  }
}

// v1.2.5: per-request composer controls persist the same way — the
// thinking control and the manual tool selection survive reloads and are
// sent with EVERY run request (they change the actual backend behaviour).
const THINKING_STORAGE_KEY = "sheytan.thinking";
const TOOLMODE_STORAGE_KEY = "sheytan.toolMode";
const TOOLALLOW_STORAGE_KEY = "sheytan.toolAllow";
// v1.3.6: the Net Search control persists like the other composer
// controls (it is sent with every run while enabled).
const NETSEARCH_STORAGE_KEY = "sheytan.netSearch";

function initialNetSearch(): boolean {
  try {
    return window.localStorage.getItem(NETSEARCH_STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

function persistNetSearch(v: boolean): void {
  try {
    window.localStorage.setItem(NETSEARCH_STORAGE_KEY, v ? "1" : "0");
  } catch {
    // in-memory only
  }
}

function initialThinkingControl(): ThinkingControl {
  try {
    // v1.8.5: persisted values pass through the same normalization as the
    // wire — legacy auto/fast/thinking settings migrate onto the
    // low/mid/high/ultra ladder at read time (never stored back as
    // legacy values by this session's writes).
    return normalizeThinkingControl(
      window.localStorage.getItem(THINKING_STORAGE_KEY),
    );
  } catch {
    return "mid";
  }
}

function persistThinkingControl(v: ThinkingControl): void {
  try {
    window.localStorage.setItem(THINKING_STORAGE_KEY, v);
  } catch {
    // in-memory only
  }
}

// v1.8.5 SHOW/HIDE THINKING — a pure VISIBILITY preference. It never
// changes generation settings: the reasoning level/budget travel with
// every request regardless of this flag, the store keeps folding
// reasoning snapshots, and only the PRESENTATION is gated. Persisted
// through the same settings authority as the other composer controls.
const SHOWTHINKING_STORAGE_KEY = "sheytan.showThinking";

function initialShowThinking(): boolean {
  try {
    // Absent key → visible (the previous implicit behavior).
    return window.localStorage.getItem(SHOWTHINKING_STORAGE_KEY) !== "0";
  } catch {
    return true;
  }
}

function persistShowThinking(v: boolean): void {
  try {
    window.localStorage.setItem(SHOWTHINKING_STORAGE_KEY, v ? "1" : "0");
  } catch {
    // in-memory only
  }
}

function initialToolPolicyMode(): ToolPolicyMode {
  try {
    return normalizeToolPolicyMode(
      window.localStorage.getItem(TOOLMODE_STORAGE_KEY),
    );
  } catch {
    return "auto";
  }
}

function persistToolPolicyMode(v: ToolPolicyMode): void {
  try {
    window.localStorage.setItem(TOOLMODE_STORAGE_KEY, v);
  } catch {
    // in-memory only
  }
}

function initialToolAllowlist(): string[] {
  try {
    const raw = window.localStorage.getItem(TOOLALLOW_STORAGE_KEY);
    if (!raw) {
      return [];
    }
    return normalizeToolAllowlist(JSON.parse(raw));
  } catch {
    return [];
  }
}

function persistToolAllowlist(list: string[]): void {
  try {
    window.localStorage.setItem(TOOLALLOW_STORAGE_KEY, JSON.stringify(list));
  } catch {
    // in-memory only
  }
}

export type ActivityEvent = {
  id: string;
  type: string;
  timestamp: number;
  data: Record<string, unknown>;
};

type RuntimeState = {
  app: AppState | null;
  sysinfo: SysInfo | null;
  models: ModelsResponse | null;
  /** v1.2.3: true while /api/models is in flight (skeleton, not fake empty). */
  modelsLoading: boolean;
  presets: Preset[];
  tools: ToolInfo[];

  sessions: Session[];
  activeSessionId: string | null;

  connection: ConnectionState;
  loading: boolean;
  error: string | null;

  activity: ActivityEvent[];
  running: boolean;

  // v1.2.2: the live generation timeline. `runPhase` is the lifecycle
  // state machine (run-phase.ts) driven ONLY by real backend events;
  // `runStartedAt` powers the elapsed clock; `runNote` carries the
  // backend's own done/error caption verbatim (never invented).
  runPhase: RunPhase;
  runStartedAt: number | null;
  runNote: string | null;

  // v1.7.4 run control: the accepted partial answer + the monotonic
  // checkpoint revision while the run is paused (both strictly backend-
  // derived: run_snapshot.pausedDraft / run_snapshot.revision). The
  // paused draft is editable through editRun; resume replays the SAME
  // revision the UI acted on — a stale client is rejected server-side.
  pausedDraft: string | null;
  runRevision: number;

  // v1.2.5: live status chip (backend "status" events only — measured
  // tier/token telemetry, never fabricated) + the tier-escalation trail
  // of the current/last run.
  liveStatus: string | null;
  tierEscalations: string[];

  // v1.8.2: the live MEMORY indicator — strictly backend truth. The
  // value mirrors the plan's MemoryEvidence the orchestrator publishes
  // on the `context` activity (which systems exist, which were actually
  // injected this turn, the measured recall counts). Null until the
  // current run's context report arrives; cleared on every fresh run.
  memoryEvidence: MemoryEvidenceState | null;

  // v1.2.5: the thinking panel's open/fold state, driven ONLY by the
  // backend's thinking_start/thinking_end markers (plus run end).
  thinkingPanelOpen: boolean;

  // v1.2.5: per-request composer controls (persisted; sent with every
  // run — they shape the actual backend request).
  thinkingControl: ThinkingControl;
  setThinkingControl: (v: ThinkingControl) => void;
  // v1.8.5 SHOW/HIDE THINKING — visibility-only. Never changes the
  // request; reasoning data keeps flowing and stays in the store.
  showThinking: boolean;
  setShowThinking: (v: boolean) => void;
  toolPolicyMode: ToolPolicyMode;
  setToolPolicyMode: (v: ToolPolicyMode) => void;
  toolAllowlist: string[];
  setToolAllowed: (name: string, allowed: boolean) => void;

  // v1.3.6 (spec §23/§25): per-request Net Search control. `netSearch`
  // is the persisted user intent (sent with every run);
  // `netSearchState` renders the compact in-composer evidence states
  // (off/enabled/searching/results/failed) from REAL wire events;
  // `netSearchResultCount` is the stated count from the search output
  // when known — never invented.
  netSearch: boolean;
  setNetSearch: (v: boolean) => void;
  netSearchState: NetSearchState;
  netSearchResultCount: number | null;

  // v1.2.2: socket/poll ownership. AgentBody acquires on mount and
  // releases on unmount; a LIVE run keeps the activity socket and the
  // engine poll alive across workspace tab switches (the backend hub has
  // no replay — dropping the socket mid-run lost every event, including
  // `done`, which froze the composer forever).
  acquireActivity: () => void;
  releaseActivity: () => void;
  acquireEnginePolling: () => void;
  releaseEnginePolling: () => void;

  // v1.1.3: authoritative engine state (polled + WS-pushed).
  engine: EngineSnapshot | null;

  // v1.1.6: backend-resolved context status for the active session
  // (selector options, usage, pressure). Backend is authoritative.
  sessionContext: SessionContextStatus | null;
  sessionContextError: string | null;
  setSessionContext: (tokens: number) => Promise<void>;
  refreshSessionContext: () => Promise<void>;

  // v1.1.3: real conversation history for the active session plus the
  // streaming assistant bubble.
  //
  // v1.2.2 WIRE CONTRACT FIX: the orchestrator's emitProgress publishes
  // CUMULATIVE snapshots (each `response`/`reasoning` caption is the FULL
  // text so far). The old store appended every caption, duplicating the
  // streamed text and re-duplicating it after reconnects. The streaming
  // state now holds the LATEST authoritative snapshot per stream (see
  // stream-accumulator.ts).
  messages: ChatMessage[];
  streaming: { content: string; reasoning: string } | null;

  // v1.4.0: the transcript surface must distinguish LOADING history,
  // EMPTY history, LOADED history and LOADING FAILURE — a fetch in
  // flight (or a failed fetch) previously rendered the same
  // "conversation is empty" invitation, which claimed an empty history
  // that was never established. `historyStatus` is per active session
  // load; `historyError` carries the failure caption for the retry
  // affordance. Loading-failure is NEVER converted into an empty
  // conversation.
  historyStatus: "loading" | "ready" | "error";
  historyError: string | null;

  // v1.4.0: the composer draft lives in the store, not component state.
  // The workspace <main> remounts on every view switch; a component-
  // local draft was silently destroyed by the Agent → Settings → Agent
  // round trip. The store survives remounts (and mode switches).
  composerDraft: string;
  setComposerDraft: (v: string) => void;

  // v1.1.3: staged attachments for the composer.
  pendingAttachments: Attachment[];
  attachmentsUploading: boolean;

  lab: LabListResponse | null;
  labLoading: boolean;
  labError: string | null;
  activeLabTaskId: string | null;
  activeLabTask: LabTaskSessionSnapshot | null;

  // v1.3.6 (spec §21/§22): the Research workspace slice is removed —
  // Net Search reuses the same backend service through the run contract
  // and the composer control; no separate workspace state exists.

  refreshSysinfo: () => Promise<void>;
  refreshModels: () => Promise<void>;
  refreshPresets: () => Promise<void>;
  refreshSessions: () => Promise<void>;
  refreshTools: () => Promise<void>;
  refreshAgentResources: () => Promise<void>;
  refreshEngine: () => Promise<void>;
  startEnginePolling: () => void;
  // v1.1.4: the engine poll previously ran for the app's LIFETIME once
  // started (no stop function existed) — even on other views.
  stopEnginePolling: () => void;

  // v1.1.4: recall feedback (thumbs up/down on past exchanges) — the
  // backend steering existed since v1.0.6 with no write path.
  sendFeedback: (query: string, liked: boolean) => Promise<void>;

  loadSession: (id: string) => Promise<void>;

  refreshLab: () => Promise<void>;
  loadLabTask: (id: string) => Promise<void>;

  // v1.9.0: `keepRunState` — the lazy zero-session create inside run()
  // must not reset the run-startup state (running/runPhase/runStartedAt)
  // that run() set BEFORE dispatching the create. Resetting it re-opened
  // the exact composer-idle window the v1.8.8 repair closed (the reload
  // could still land before POST /api/run fired).
  createSession: (opts?: { keepRunState?: boolean }) => Promise<Session>;
  selectSession: (id: string | null) => void;
  deleteSession: (id: string) => Promise<void>;

  run: (message: string) => Promise<void>;
  abort: () => Promise<void>;
  regenerate: () => Promise<void>;

  // v1.7.4: backend-neutral Pause / Edit / Resume over the ONE /api/run
  // authority. Editing is allowed only while paused; every accepted edit
  // bumps the revision the UI must present on resume.
  pauseRun: () => Promise<void>;
  resumeRun: () => Promise<void>;
  editRun: (payload: { message?: string; draft?: string }) => Promise<boolean>;

  uploadFiles: (files: File[]) => Promise<void>;
  removePendingAttachment: (id: string) => Promise<void>;

  runLabAction: (payload: Record<string, unknown>) => Promise<unknown>;

  connectActivity: () => void;
  disconnectActivity: () => void;
  clearActivity: () => void;

  // v1.1.8: Chat / Agent mode switch (persisted per device).
  mode: WorkspaceMode;
  setMode: (mode: WorkspaceMode) => void;

  // v1.2.8: MODE-SEPARATED HISTORIES. One active session PER mode —
  // switching mode switches the conversation space, it never silently
  // switches (or merges) the active conversation.
  activeSessionByMode: Record<WorkspaceMode, string | null>;

  // v1.2.8: cross-mode history references attached to the ACTIVE session.
  // DATA, never authority — the backend retrieves only the relevant
  // portions per run and labels provenance.
  historyRefs: HistoryRef[];
  attachHistoryRefs: (refs: HistoryRef[]) => Promise<void>;
  detachHistoryRef: (sessionId: string) => Promise<void>;

  // v1.2.8: history picker state (search + mode filter + results).
  historyPickerOpen: boolean;
  setHistoryPickerOpen: (open: boolean) => void;
  historyHits: HistoryHit[];
  historySearchLoading: boolean;
  historySearchMode: WorkspaceMode | "all";
  setHistorySearchMode: (mode: WorkspaceMode | "all") => void;
  searchHistory: (q: string) => Promise<void>;

  // v1.2.8: bounded agent task state (rendered by the agent pipeline
  // panel; restored from run snapshots on reconnect).
  agentTask: Record<string, unknown> | null;

  // v1.2.8: lazy history paging — the newest page loads first, older
  // messages are fetched on demand ("Load earlier").
  olderHasMore: boolean;
  olderLoading: boolean;
  olderNextBefore: number | null;
  loadOlderMessages: () => Promise<void>;

  // v1.2.8: session rename (the backend PUT existed; the UI now uses it).
  renameSession: (id: string, title: string) => Promise<void>;
};

const MAX_ACTIVITY_EVENTS = 500;

// v1.7.5 STALE SESSION-LIST PROTECTION: one monotonic generation for every
// GET /api/sessions request plus every session mutation. A list response may
// only land while its ticket is still current — a stale response (started
// before a create/delete/rename/mode switch, or superseded by a newer
// refresh) can never overwrite newer state, so a deleted session can never
// be resurrected in the sidebar. The guard owns NO session data; the store
// remains the one authoritative list. Pure-module coverage:
// session-list-guard.test.ts.
//
// v1.8.3: EXPORTED so agent-init.ts routes the STARTUP session-list write
// through the SAME one-authority generation counter (the v1.7.5 repair
// covered refreshSessions only — the init consumer wrote its response
// unguarded, so a user mutation that landed while the initial GET was on
// the wire could be clobbered by the stale init response: a just-created
// session vanished from the sidebar, or a stale selection was resurrected).
export const sessionListGuard = createSessionListGuard();

let enginePollTimer: number | null = null;

// v1.2.2: engine-poll ownership refcount (0 = nobody needs the poll).
let enginePollConsumers = 0;

let socket: WebSocket | null = null;

// openActivitySocket creates and wires the activity WebSocket for one
// session. v1.9.4: extracted verbatim from connectActivity so the connect
// path can resolve the desktop loopback origin (see config.ts
// ensureActivityWsOrigin) BEFORE the socket is constructed — the socket
// URL itself is unchanged for every same-origin serving mode.
function openActivitySocket(sessionId: string): void {
  const ws = new WebSocket(activityWebSocketURL(sessionId));

  socket = ws;

  ws.onopen = () => {
    if (socket !== ws || activitySessionId !== useRuntimeStore.getState().activeSessionId) {
      return;
    }

    // v1.1.4: a successful (re)connection resets the backoff ladder.
    reconnectAttempts = 0;

    // v1.9.4 (P0-A): boundary evidence — the transport is open.
    recordStreamTrace("ws_state", { state: "open" });

    useRuntimeStore.setState({
      connection: "connected",
    });
  };

  ws.onmessage = (event) => {
    if (socket !== ws || activitySessionId !== useRuntimeStore.getState().activeSessionId) {
      return;
    }

    try {
      const payload = JSON.parse(event.data) as APIActivityEvent;

      // v1.2.6 continuation: the attach acknowledgement — the server
      // confirmed this socket is live and session-bound. Resolves the
      // deterministic attach contract (never queued as activity).
      if ((payload as Record<string, unknown>).type === "attached") {
        recordStreamTrace("ws_state", { state: "attached" });
        settleActivityAttach();
        return;
      }

      // v1.2.6 continuation: the AUTHORITATIVE RUN SNAPSHOT — sent on
      // every attachment (mid-run reconnect included). Replays the
      // cumulative response/reasoning snapshots and the terminal state
      // without waiting for a future token.
      if ((payload as Record<string, unknown>).type === "run_snapshot") {
        // v1.9.4 (P0-A): snapshot boundary — kind, wire seq and snapshot
        // lengths only (never content).
        recordStreamTrace("ws_frame", {
          kind: "run_snapshot",
          wireSeq:
            typeof (payload as Record<string, unknown>).sequence === "number"
              ? ((payload as Record<string, unknown>).sequence as number)
              : undefined,
          contentChars:
            typeof (payload as Record<string, unknown>).latestResponse ===
            "string"
              ? ((payload as Record<string, unknown>).latestResponse as string)
                  .length
              : 0,
          reasoningChars:
            typeof (payload as Record<string, unknown>).latestReasoning ===
            "string"
              ? ((payload as Record<string, unknown>).latestReasoning as string)
                  .length
              : 0,
        });
        handleRunSnapshot(payload as unknown as Record<string, unknown>);
        return;
      }

      // Stale-run filter: drop drains of a replaced run and
      // duplicate/replayed sequences before they reach the pipeline.
      if (isStaleRunEvent(payload as Record<string, unknown>)) {
        return;
      }

      const activity = normalizeActivity(payload);

      // v1.9.4 (P0-A): receive boundary — canonical kind, wire seq and
      // caption LENGTH only (never the caption itself).
      recordStreamTrace("ws_frame", {
        kind: activity.type,
        wireSeq:
          typeof payload.seq === "number" ? payload.seq : undefined,
        captionChars:
          typeof activity.data.caption === "string"
            ? (activity.data.caption as string).length
            : 0,
        queueDepth: pendingActivity.length,
      });

      // v1.8.1 SINGLE-FRAME STREAMING: stream-critical content
      // (response/reasoning) folds into the streaming accumulator NOW —
      // visible on the next rendering frame, ONE boundary instead of
      // two. The event still joins the timeline batch below; the
      // fast-path ledger makes the batch flush skip its conversation
      // processing (no double handling). Everything else — status,
      // tools, done/error/abort — keeps the existing batching.
      streamFastPath.deliver(
        activity.id,
        activity.type,
        activity.data.caption,
      );

      queueActivity(activity);
    } catch {
      // Ignore malformed activity frames.
    }
  };

  ws.onerror = () => {
    if (socket !== ws || activitySessionId !== useRuntimeStore.getState().activeSessionId) {
      return;
    }

    recordStreamTrace("ws_state", { state: "error" });

    useRuntimeStore.setState({
      connection: "error",
    });
  };

  ws.onclose = () => {
    // The socket died — release anyone waiting on the deterministic
    // attach contract FIRST, unconditionally: the deferred belongs to
    // whichever socket was current, and a new connectActivity() always
    // arms a fresh one (failActivityAttach is a no-op when already
    // settled).
    failActivityAttach();

    if (socket !== ws || activitySessionId !== useRuntimeStore.getState().activeSessionId) {
      return;
    }

    socket = null;

    useRuntimeStore.setState({
      connection: "disconnected",
    });

    // v1.1.4: auto-reconnect while the session is still active. Without
    // this, ANY mid-run drop (backend restart, transient network blip)
    // permanently killed event delivery — `running` could never clear.
    if (reconnectAttempts >= RECONNECT_MAX_ATTEMPTS) {
      return;
    }

    const delay = Math.min(
      RECONNECT_BASE_DELAY_MS * 2 ** reconnectAttempts,
      RECONNECT_MAX_DELAY_MS,
    );

    reconnectAttempts += 1;

    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = null;

      const current = useRuntimeStore.getState();

      if (
        current.activeSessionId &&
        current.activeSessionId === activitySessionId
      ) {
        current.connectActivity();
      }
    }, delay);
  };
}

// v1.2.6 continuation: the DETERMINISTIC ATTACH CONTRACT.
//
// POST completion must never imply the live transport was attached. The
// backend sends an explicit `attached` frame as the FIRST message after
// the protocol upgrade; connectActivity records a deferred that resolves
// on that frame, and run()/regenerate() AWAIT it before POSTing /api/run.
// No sleeps, no guessed grace windows — the acknowledgement is the proof.
let activityAttachResolve: (() => void) | null = null;
let activityAttachReject: (() => void) | null = null;
let activityAttachPromise: Promise<void> | null = null;

// v1.9.2 (the zero-session live-surface clobber): the client-side clock of
// the LAST attach acknowledgement. The server's attach sequence is
// `attached` → `idle` → `run_snapshot`; the idle sentinel in that sequence
// is HANDSHAKE — it was composed before any run of this socket existed and
// says nothing about a run started afterwards. It must never be consumed
// as run-liveness evidence (see recoverRunFromIdle).
let activityAttachAckAt = 0;

// beginActivityAttach arms the attach acknowledgement for a new socket.
function beginActivityAttach(): void {
  activityAttachPromise?.catch(() => {
    // A previous deferred's rejection is handled by its own awaiter.
  });

  activityAttachPromise = new Promise<void>((resolve, reject) => {
    activityAttachResolve = resolve;
    activityAttachReject = reject;
  });
}

// settleActivityAttach resolves the pending acknowledgement (the
// `attached` frame arrived — the transport is live and session-bound).
function settleActivityAttach(): void {
  if (activityAttachResolve) {
    // v1.9.2: recorded when the acknowledgement SETTLES (the attach
    // handshake window opens here).
    activityAttachAckAt = Date.now();
    activityAttachResolve();
    activityAttachResolve = null;
    activityAttachReject = null;
  }
}

// failActivityAttach rejects the pending acknowledgement (the socket died
// before the server acknowledged the attachment).
function failActivityAttach(): void {
  if (activityAttachReject) {
    activityAttachReject();
    activityAttachResolve = null;
    activityAttachReject = null;
  }
}

// ACTIVITY_ATTACH_COMPAT_MS bounds how long run()/regenerate() wait for
// the `attached` acknowledgement. The acknowledgement is the PRIMARY
// mechanism (deterministic, no sleeps); this bound exists ONLY so a
// backend that predates the `attached` frame (mixed deployment) can never
// hang the composer — after the bound the POST proceeds and the runId
// replay + bounded grace re-check own the resync, exactly like v1.2.6.
const ACTIVITY_ATTACH_COMPAT_MS = 5000;

// waitForActivityAttached resolves when the CURRENT activity socket has
// been acknowledged by the server (the `attached` frame), or immediately
// when a socket for the active session is already connected AND
// acknowledged. It REJECTS when the socket dies first — callers proceed
// anyway (the runId replay + the bounded grace re-check recover); the
// await exists to remove the race, not to gate the POST.
function waitForActivityAttached(): Promise<void> {
  let base: Promise<void>;

  if (
    socket &&
    socket.readyState === WebSocket.OPEN &&
    activityAttachResolve === null &&
    activityAttachPromise
  ) {
    // Already acknowledged (resolve consumed) — attach is proven.
    base = activityAttachPromise;
  } else if (activityAttachPromise) {
    base = activityAttachPromise;
  } else {
    base = Promise.reject(new Error("activity socket not connecting"));
  }

  // Compatibility bound: first settlement wins (ack, close, or bound).
  return Promise.race([
    base,
    new Promise<void>((resolve) => {
      window.setTimeout(resolve, ACTIVITY_ATTACH_COMPAT_MS);
    }),
  ]);
}

// v1.2.6 continuation: AUTHORITATIVE RUN REPLAY tracking.
//
// activeRunId is the run the UI is currently bound to (from the POST
// response or an adopted run_snapshot); lastRunSeq is the highest sequence
// number folded into the visible state. Events from other runs (stale
// drains of a replaced run's hub) and duplicate/replayed events (seq ≤
// lastRunSeq — already inside the latest cumulative snapshot) are dropped
// BEFORE they reach the pipeline.
let activeRunId: string | null = null;
let lastRunSeq = 0;

// resetRunReplayTracking resets the run-scoped filters for a fresh run.
function resetRunReplayTracking(runId: string | null): void {
  activeRunId = runId;
  lastRunSeq = 0;
}

// isStaleRunEvent reports whether one activity frame is stale for the
// run the UI is bound to: a different (older) run's drain, or a sequence
// number already folded into the current cumulative state.
function isStaleRunEvent(payload: Record<string, unknown>): boolean {
  const frameRunId = typeof payload.runId === "string" ? payload.runId : "";

  if (frameRunId && activeRunId && frameRunId !== activeRunId) {
    return true;
  }

  const seq = typeof payload.seq === "number" ? payload.seq : 0;

  if (seq > 0 && seq <= lastRunSeq) {
    return true;
  }

  // v1.2.8.1: only frames attributable to the tracked run (or arriving
  // while no run is bound yet) advance the dedup watermark. The previous
  // unconditional bump let a foreign frame WITHOUT a runId raise the
  // watermark and silently drop legitimate later frames of the tracked
  // run.
  if (seq > 0 && frameRunId) {
    if (!activeRunId || frameRunId === activeRunId) {
      lastRunSeq = seq;
    }
  }

  return false;
}
let activitySequence = 0;
let activitySessionId: string | null = null;

// v1.8.4 MONOTONIC CONTEXT-REFRESH GENERATION (P0-D).
//
// sessionContext is written ONLY by the newest refresh request of the
// newest relevant state. Every refreshSessionContext() call claims a
// strictly increasing generation; a response may land only when its
// generation is still current AND its session is still active. Relevant
// state changes (session switch, session deletion, session creation,
// mode switch, a fresh run's state transitions) BUMP the generation —
// via an explicit invalidate or by issuing the next refresh — so a
// response that belonged to a superseded state can never mutate
// sessionContext after the newer authoritative state exists. The backend
// remains the ONE context authority; this is purely response ordering.
let contextRefreshGeneration = 0;

// invalidateSessionContext drops every in-flight context response without
// starting a new one (deletion/creation/mode boundaries — the caller
// decides what the visible context should hold afterwards).
function invalidateSessionContext(): void {
  contextRefreshGeneration += 1;
}

// v1.2.2: activity-socket ownership refcount. While any AgentBody is
// mounted the count is ≥ 1. A live run holds an implicit lease even at
// count 0 so tab switches mid-generation never drop the event stream.
let activityConsumers = 0;

// v1.1.4: automatic WebSocket reconnection. The old store gave up on the
// first close — a mid-run disconnect left `running` stuck true forever (the
// dead-composer bug's last live variant: no `done` event could ever arrive).
let reconnectTimer: number | null = null;
let reconnectAttempts = 0;

const RECONNECT_BASE_DELAY_MS = 1500;
const RECONNECT_MAX_DELAY_MS = 15000;
const RECONNECT_MAX_ATTEMPTS = 20;

// v1.2.6: bounded idle-grace re-check. When an idle sentinel arrives inside
// the attach-race grace window with no run evidence yet, it may be EITHER the
// harmless standby marker OR the only signal that the run already finished
// (fast run + late socket). The v1.2.5 code dropped it and never re-checked
// — the "no visible answer" bug. The re-check fires once when the grace
// window expires; if there is STILL no run evidence by then, the run is
// recovered from the authoritative history.
let idleGraceRecheckTimer: number | null = null;

function clearIdleGraceRecheck(): void {
  if (idleGraceRecheckTimer !== null) {
    window.clearTimeout(idleGraceRecheckTimer);
    idleGraceRecheckTimer = null;
  }
}

function clearReconnectTimer(): void {
  if (reconnectTimer !== null) {
    window.clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

// v1.8.4: the ACTIVITY timeline flush moved off its rAF-only schedule
// onto the same triple-boundary scheduler class as the streaming flush.
// Statuses, tool events and the LIFECYCLE events (done/error/aborted)
// ride this path — a suspended frame callback alone must never be able
// to starve run settlement (the v1.8.2 fix covered streaming only).
let pendingActivity: ActivityEvent[] = [];
let flushingActivity: ActivityEvent[] = [];
let pendingRunning: boolean | undefined;

// --- v1.8.1: SINGLE-FRAME STREAMING FAST PATH -------------------------------
//
// Stream-critical events (response/reasoning content) previously waited
// TWO render-frame boundaries before becoming visible: the activity
// batch frame AND the streaming flush frame. The fast path folds them
// into the streaming accumulator THE MOMENT the WebSocket delivers
// them, so the first (and every subsequent) streamed snapshot is visible
// on the NEXT rendering frame — ONE boundary, no artificial delays, no
// per-token renders (the streaming flush still coalesces everything
// arriving within one frame).
//
// The event still joins the activity timeline batch; the fast-path
// ledger (inside StreamingFastPath) makes the batch flush skip its
// conversation processing so nothing is processed twice. Lifecycle
// events (done/error/aborted) keep the existing path — they flush the
// accumulator SYNCHRONOUSLY when the batch reaches them.
const streamFastPath = new StreamingFastPath({
  foldContent: (caption) => queueStreamingContent(caption),
  foldReasoning: (caption) => queueStreamingReasoning(caption),
  phaseResponseDelta: () => transitionPhase("response_delta"),
  phaseReasoningDelta: () => transitionPhase("reasoning_delta"),
  markRunEvidence: () => {
    runEventsReceived = true;
  },
});

// --- Phase 4: streaming coalescing ---------------------------------------
//
// High token rates (100+ tokens/sec) can swamp React with one setState
// per token, each re-rendering the whole message tree. The streaming
// coalescer accumulates response/reasoning chunks into a single buffer
// and flushes on ONE coalescing boundary — so no matter how fast the
// model emits, the UI updates at most once per boundary turn (never one
// render per token).
//
// Coalescing only batches the CONTENT payload; lifecycle events
// (done/error/session) are still delivered immediately because they
// close the streaming bubble and must reset `running`.
//
// v1.2.2: the buffer holds the LATEST CUMULATIVE snapshot per stream
// (backend contract — see stream-accumulator.ts), not appended deltas.
// v1.8.2 DUAL-BOUNDARY FLUSH SCHEDULING — the P0 repair for "streamed
// text visible only after Stop". The previous scheduler armed the flush
// EXCLUSIVELY on requestAnimationFrame. In the user's WebView2 runtime
// the compositor's frame callbacks can be throttled or suspended while
// the event loop, the WebSocket and React's own scheduler keep running —
// phase labels updated, elapsed clock ticked, but streamed text never
// appeared, and pressing Stop (which flushes synchronously) revealed
// everything at once. The scheduler below arms the flush on BOTH an
// event-loop task (MessageChannel — delivered the moment the current JS
// turn ends, in every renderer state where JavaScript runs) AND an
// animation frame. Whichever runs first flushes; the other is a no-op.
// Exactly one coalescing latch remains, so folds arriving within one
// turn/frame still produce ONE render — never one per token. See
// stream-flush-scheduler.ts (pure module + unit tests).
const streamAccumulator = createStreamingAccumulator();

// v1.8.4: THREE boundaries (MessageChannel macrotask + 0ms timer task +
// animation frame) behind the ONE coalescing latch, and a RECOVERABLE
// MessageChannel controller — the v1.8.3 runtime evidence showed a single
// lost MessageChannel delivery could wedge the v1.8.2 latch forever (text
// visible only after Stop) and the activity path had the same exposure.
// See stream-flush-scheduler.ts for the failure-class analysis.
const streamFlushScheduler = new StreamFlushScheduler(
  new MessageChannelTaskController(),
  new TimeoutTaskController(),
  new AnimationFrameController(),
  flushStreaming,
);

function resetPendingStreaming(): void {
  streamFlushScheduler.cancel();

  streamAccumulator.content = null;
  streamAccumulator.reasoning = null;
}

// flushStreaming writes the latest cumulative snapshots to the store in
// ONE setState, then resets the buffers. v1.8.2: it runs on the dual
// boundary above (event-loop task + animation frame) instead of a frame
// callback alone — see stream-flush-scheduler.ts. Idempotent by the
// empty-guard: whichever boundary runs first drains, the other no-ops.
function flushStreaming(): void {
  if (accumulatorIsEmpty(streamAccumulator)) {
    return;
  }

  const update = flushAccumulator(streamAccumulator);

  const current = useRuntimeStore.getState().streaming;

  // v1.9.4 (P0-A): the flush boundary — the LENGTHS being committed to
  // the store (never the text). The merged lengths identify whether a
  // flush actually advanced the visible snapshot.
  recordStreamTrace("accumulator_flush", {
    contentChars: (update.content !== null
      ? update.content
      : (current?.content ?? "")
    ).length,
    reasoningChars: (update.reasoning !== null
      ? update.reasoning
      : (current?.reasoning ?? "")
    ).length,
  });

  useRuntimeStore.setState({
    streaming: mergeSnapshot(current, update),
  });

  // Phase 4 perf HUD: count this as one coalesced stream update so the
  // HUD can measure update frequency (should be <= display refresh rate,
  // never one-per-token). The recordStreamUpdate import is dynamic so
  // this file stays decoupled from perf-hud.ts when the HUD is disabled.
  recordStreamUpdateSafe();
}

// captureRunBaseline records how many assistant messages exist when the
// first streamed token of THIS run arrives. The finalisation path uses it
// to decide whether the authoritative history actually gained a reply —
// and, when it did not, to preserve the partial output locally instead of
// silently losing the entire response (v1.2.2).
let runAssistantBaseline: number | null = null;

function captureRunBaselineIfNeeded(): void {
  if (runAssistantBaseline === null) {
    runAssistantBaseline = useRuntimeStore
      .getState()
      .messages.filter((message) => message.role === "assistant").length;
  }
}

// flushStreaming's legacy inline body was replaced by the cumulative
// snapshot merge above (v1.2.2); the buffers reset inside flushAccumulator.

// recordStreamUpdateSafe is a thin wrapper around perf-hud's counter.
// Kept as a separate function so the store never throws if the perf-hud
// module fails to load (it's a diagnostic; never let it break the app).
let recordStreamUpdateFn: (() => void) | null = null;

// setStreamUpdateRecorder is exported so main.tsx can wire the perf-hud
// counter into the store after both modules load (avoids a circular
// import: store.ts ↔ perf-hud.ts).
export function setStreamUpdateRecorder(fn: (() => void) | null): void {
  recordStreamUpdateFn = fn;
}

function recordStreamUpdateSafe(): void {
  if (recordStreamUpdateFn !== null) {
    try {
      recordStreamUpdateFn();
    } catch {
      // Swallow — the HUD is diagnostic only.
    }
  }
}

function scheduleStreamingFlush(): void {
  streamFlushScheduler.schedule();
}

// queueStreamingContent folds one cumulative `response` snapshot (the
// caption IS the full text so far) and schedules a frame-aligned flush.
function queueStreamingContent(snapshot: string): void {
  if (!snapshot) return;

  captureRunBaselineIfNeeded();
  applyResponseSnapshot(streamAccumulator, snapshot);

  // v1.9.4 (P0-A): fast-path fold boundary (length only).
  recordStreamTrace("fastpath_fold", { contentChars: snapshot.length });

  scheduleStreamingFlush();
}

// queueStreamingReasoning folds one cumulative `reasoning` snapshot.
function queueStreamingReasoning(snapshot: string): void {
  if (!snapshot) return;

  captureRunBaselineIfNeeded();
  applyReasoningSnapshot(streamAccumulator, snapshot);

  // v1.9.4 (P0-A): fast-path fold boundary (length only).
  recordStreamTrace("fastpath_fold", {
    reasoningChars: snapshot.length,
  });

  scheduleStreamingFlush();
}

function createActivityID(): string {
  activitySequence += 1;

  return `${Date.now()}-${activitySequence}`;
}

function normalizeActivity(payload: unknown): ActivityEvent {
  if (payload && typeof payload === "object") {
    const value = payload as Record<string, unknown>;

    const rawTimestamp = value.timestamp;

    let timestamp = Date.now();

    if (typeof rawTimestamp === "number") {
      timestamp = rawTimestamp;
    } else if (typeof rawTimestamp === "string") {
      const parsed = Date.parse(rawTimestamp);

      if (!Number.isNaN(parsed)) {
        timestamp = parsed;
      }
    }

    return {
      id: createActivityID(),
      type: typeof value.type === "string" ? value.type : "activity",
      timestamp,
      data: value,
    };
  }

  return {
    id: createActivityID(),
    type: "activity",
    timestamp: Date.now(),
    data: {
      value: payload,
    },
  };
}

function resetPendingActivity(): void {
  // v1.8.4: disarm the activity flush through its scheduler (the armed
  // task/timer callbacks become latch no-ops; the frame callback is
  // cancelled).
  activityFlushScheduler.cancel();

  // v1.8.1: the fast-path ledger is per-socket state — a reconnect or
  // session switch starts a clean ledger (the pending batch it
  // referenced is being dropped below anyway).
  streamFastPath.reset();

  pendingActivity.length = 0;
  flushingActivity.length = 0;
  pendingRunning = undefined;
}

// v1.8.4: flushActivity runs on the activity scheduler's triple boundary
// instead of a bare rAF callback. Same drain-then-clear semantics as
// before (batch swap, session guard, one setActivityBatch per window).
function flushActivity(): void {
  if (pendingActivity.length === 0 && pendingRunning === undefined) {
    return;
  }

  const batch = pendingActivity;

  pendingActivity = flushingActivity;
  flushingActivity = batch;

  const running = pendingRunning;
  pendingRunning = undefined;

  if (
    !activitySessionId ||
    activitySessionId !== useRuntimeStore.getState().activeSessionId
  ) {
    flushingActivity.length = 0;

    return;
  }

  setActivityBatch(batch, running);

  batch.length = 0;
}

// v1.8.4: the activity flush scheduler — declared after flushActivity
// (function declaration hoisting keeps the reference valid).
const activityFlushScheduler = new StreamFlushScheduler(
  new MessageChannelTaskController(),
  new TimeoutTaskController(),
  new AnimationFrameController(),
  flushActivity,
);

function scheduleActivityFlush(): void {
  activityFlushScheduler.schedule();
}

function queueActivity(activity: ActivityEvent): void {
  pendingActivity.push(activity);

  const running = activity.data.running;

  if (typeof running === "boolean") {
    pendingRunning = running;
  }

  scheduleActivityFlush();
}

function setActivityBatch(
  batch: ActivityEvent[],
  running: boolean | undefined,
): void {
  useRuntimeStore.setState((state) => {
    let activity = state.activity;

    if (batch.length > 0) {
      if (batch.length >= MAX_ACTIVITY_EVENTS) {
        activity = batch.slice(-MAX_ACTIVITY_EVENTS);
      } else if (state.activity.length + batch.length <= MAX_ACTIVITY_EVENTS) {
        activity = [...state.activity, ...batch];
      } else {
        const keep = MAX_ACTIVITY_EVENTS - batch.length;

        activity = [...state.activity.slice(-keep), ...batch];
      }
    }

    if (running === undefined) {
      return {
        activity,
      };
    }

    return {
      activity,
      running,
    };
  });

  // v1.1.3: route conversation-relevant events into the message pipeline
  // (streaming bubbles + the run-end bookkeeping that used to leave the
  // composer permanently disabled after one message).
  //
  // v1.8.1: stream-critical events were ALREADY processed by the fast
  // path at socket-receive time — the ledger skip guarantees they are
  // never processed (or displayed) twice.
  for (const event of batch) {
    if (streamFastPath.consumeBatchEvent(event.id)) {
      continue;
    }

    handleConversationEvent(event);
  }
}

// transitionPhase advances the run lifecycle machine (run-phase.ts) from a
// real wire event. Unknown/stale events are no-ops — the machine itself
// decides which transitions are legal from which phase.
function transitionPhase(event: RunEventKind): void {
  const state = useRuntimeStore.getState();
  const next = nextPhase(state.runPhase, event);

  if (next !== state.runPhase) {
    useRuntimeStore.setState({ runPhase: next });
  }
}

// --- v1.2.2: run finalisation ----------------------------------------------
//
// done/error/abort/disconnect/idle must ALWAYS leave the UI in a correct
// non-running state with the run's output preserved. The backend persists
// the assistant reply AFTER emitting `done` (and not at all on error or
// abort), so finalisation:
//   1. reloads the authoritative history;
//   2. confirms it actually gained a new assistant message;
//   3. if it did NOT, promotes the streamed partial into the conversation
//      locally so the user never loses the entire response.
let runFinalizeTimer: number | null = null;
let runOutcome: "done" | "aborted" | "lost" | null = null;

// runEventsReceived: did THIS run deliver any hub event over the socket?
// Distinguishes "socket never attached / run gone" from "still preparing"
// when an idle sentinel arrives (see recoverRunFromIdle).
let runEventsReceived = false;

const RUN_EVENT_TYPES = new Set([
  "response",
  "reasoning",
  "thinking",
  "context",
  "tool_start",
  "tool_end",
  "done",
  "error",
  "session",
  // v1.2.5 additions.
  "assistant_delta",
  "thinking_delta",
  "thinking_start",
  "thinking_end",
  "complete",
  "status",
  "escalation",
]);

function clearRunFinalizeTimer(): void {
  if (runFinalizeTimer !== null) {
    window.clearTimeout(runFinalizeTimer);
    runFinalizeTimer = null;
  }

  // v1.2.6: the idle-grace re-check shares the run's lifetime — it must
  // never outlive the run bookkeeping it guards.
  clearIdleGraceRecheck();
}

// releaseIdleOwnedResources drops the activity socket and the engine poll
// when no mounted component owns them AND no run is live — the implicit
// run lease is over. Prevents both duplicate sockets after tab switches
// and post-run resource leaks.
function releaseIdleOwnedResources(): void {
  const state = useRuntimeStore.getState();

  if (
    activityConsumers === 0 &&
    !state.running &&
    !isLivePhase(state.runPhase)
  ) {
    state.disconnectActivity();
  }

  if (
    enginePollConsumers === 0 &&
    !state.running &&
    !isLivePhase(state.runPhase)
  ) {
    state.stopEnginePolling();
  }
}

// promotePartialOutput appends the streamed partial as a local assistant
// message so errors/aborts/lost-runs never discard the entire response.
// The message is marked via its `at` stamp + a visible note appended to
// the content (never presented as a fully persisted reply).
//
// v1.10.14-fixed (R3): the preserved row is ALSO recorded in
// promotedPartial so a later authoritative reload that still lacks the
// reply can re-attach it — the transcript never drops below exactly one
// assistant row between the two confirmation attempts. finalAttempt
// derives from it (no separate flag to drift).
let promotedPartial: {
  role: "assistant";
  content: string;
  reasoning?: string;
} | null = null;

function promotePartialOutput(note: string | null): void {
  const state = useRuntimeStore.getState();
  const partial = state.streaming;

  if (!partial || (!partial.content && !partial.reasoning)) {
    return;
  }

  const marker = note ? `\n\n— ${note}` : "";

  const row: {
    role: "assistant";
    content: string;
    reasoning?: string;
  } = {
    role: "assistant",
    content: (partial.content || "*(no answer text arrived)*") + marker,
    ...(partial.reasoning ? { reasoning: partial.reasoning } : {}),
  };

  useRuntimeStore.setState({
    messages: [...state.messages, row],
    streaming: null,
  });

  promotedPartial = row;
}

// finaliseRun performs the authoritative-history confirmation for one run
// end. Called via setTimeout AFTER the backend had time to persist, and —
// v1.10.14-fixed (R3) — re-armed ONCE as a bounded retry when the first
// reload raced the persist: the run then stays "finalising" (the composer
// stays honestly locked) instead of settling on a transcript that does
// not contain the reply (the run-#39 "exactly one assistant message" red
// class, e2e/live-stream.spec.ts:266). The decision itself is the PURE
// planFinalisation contract; every settled path shows EXACTLY ONE
// assistant row: the authoritative reply, or the preserved partial.
async function finaliseRun(sessionId: string): Promise<void> {
  const state = useRuntimeStore.getState();

  if (state.activeSessionId !== sessionId) {
    // Session switched/rolled over — nothing to confirm here anymore.
    return;
  }

  const hadPartial = Boolean(
    state.streaming && (state.streaming.content || state.streaming.reasoning),
  );
  // A partial preserved by an earlier attempt counts even though this
  // attempt's authoritative reload is about to replace the messages.
  const hasPartial = hadPartial || promotedPartial !== null;

  await state.loadSession(sessionId);

  const after = useRuntimeStore.getState();

  if (after.activeSessionId !== sessionId) {
    return;
  }

  // v1.1.6: the turn changed the session's context usage — refresh the
  // authoritative status (used/pressure).
  void after.refreshSessionContext();

  // The reload replaced the messages array, so this count is the
  // HISTORY's assistant count (any preserved partial row was wiped with
  // it and is re-attached below when the decision says so).
  const assistants = after.messages.filter(
    (message) => message.role === "assistant",
  ).length;

  const historyGainedReply =
    runAssistantBaseline === null ? true : assistants > runAssistantBaseline;

  const decision = planFinalisation(
    {
      historyGainedReply,
      hasPartial,
      finalAttempt: promotedPartial !== null,
    },
    runOutcome ?? "done",
  );

  if (decision.preservePartial) {
    if (promotedPartial !== null) {
      // The reload wiped the preserved row while history STILL lacks the
      // reply — re-attach it (exactly-one-row contract, v1.2.2 partial
      // preservation preserved across attempts).
      useRuntimeStore.setState({
        messages: [...after.messages, promotedPartial],
      });
    } else {
      promotePartialOutput(
        runOutcome === "done"
          ? "partial reply preserved locally (session history did not record it)"
          : runOutcome === "aborted"
            ? "stopped — partial reply preserved locally"
            : "run ended while the view was away — partial reply preserved locally",
      );
    }
  }

  if (decision.clearStreaming) {
    useRuntimeStore.setState({ streaming: null });
  }

  useRuntimeStore.setState({
    // "finalising" keeps the run LIVE for the bounded retry; "lost" (run
    // ended while detached) settles as Stopped — honest: the run is no
    // longer active and its state was re-synced from history.
    runPhase: decision.phase,
  });

  if (decision.armRetry) {
    scheduleRunFinalisation(sessionId, 500);

    return;
  }

  // v1.9.4 (P0-A): close the client-side run window with the settled
  // phase (trace-only; no product behavior).
  recordRunEnd(decision.phase === "complete" ? "complete" : "aborted");

  runOutcome = null;
  runAssistantBaseline = null;
  promotedPartial = null;
  releaseIdleOwnedResources();
}

// scheduleRunFinalisation arms the confirm pass (and one retry — the
// backend writes the reply right after `done`; a slow disk can beat the
// first reload by a hair).
function scheduleRunFinalisation(sessionId: string, delayMs: number): void {
  clearRunFinalizeTimer();

  runFinalizeTimer = window.setTimeout(() => {
    runFinalizeTimer = null;

    const state = useRuntimeStore.getState();

    if (state.activeSessionId !== sessionId || state.running) {
      return;
    }

    // v1.10.14-fixed (R3): the bounded retry is armed by finaliseRun
    // itself (planFinalisation's armRetry). The old .then() re-entry here
    // required runPhase "finalising" AFTER finaliseRun returned — which
    // its unconditional terminal settle made impossible, so the designed
    // second attempt never ran (dead code).
    void finaliseRun(sessionId);
  }, delayMs);
}

// recoverRunFromIdle handles the backend's `idle` sentinel ("No active
// run"): the UI believed a run was live but the backend has none
// registered — the run finished while this (or a previous) socket was
// detached. Resync from the authoritative history instead of staying
// stuck with running=true and a frozen composer.
//
// RACE GUARD: the backend also sends the idle sentinel when a socket
// PARKS IN STANDBY — including the attach race window right after Send
// (POST /run accepted → run registered → standby sockets woken). An
// idle frame arriving within the grace window while this run has seen
// no events yet is that harmless standby marker, not evidence the run
// is gone; recovering then would abort a live run.
//
// v1.2.6 fixes the hole this guard left:
//   1. The sentinel now carries the backend's AUTHORITATIVE last-run
//      outcome (runId, endedAt, result, persisted). When the recorded
//      run ended at/after this run started, that is PROOF the run is
//      over — recover immediately, grace window or not.
//   2. A grace-ignored sentinel now arms a BOUNDED re-check at the end
//      of the grace window: if no run evidence arrived by then either,
//      the run finalises — the composer can never stay frozen forever.
const RUN_IDLE_GRACE_MS = 2500;

// readLastRunOutcome extracts the v1.2.6 authoritative last-run block from an
// idle frame (absent on older backends — treated as no evidence).
function readLastRunOutcome(
  event: ActivityEvent,
): { endedAt: number; outcome: string; runId?: string } | null {
  const lastRun = (event.data as { lastRun?: unknown }).lastRun;

  if (!lastRun || typeof lastRun !== "object") {
    return null;
  }

  const value = lastRun as Record<string, unknown>;
  const endedAt = parseEndedAt(value.endedAt);
  const outcome = value.outcome;

  // v1.2.8 ROOT-CAUSE FIX: the backend marshals runOutcome.EndedAt
  // (time.Time) as an RFC3339 STRING on the wire (pinned by the Go
  // contract test), while this parser previously accepted ONLY an
  // epoch-ms number — the authoritative fast-path recovery in
  // recoverRunFromIdle could therefore never fire and every recovery
  // silently fell through to the grace re-check. Both wire shapes are
  // now accepted; no backend contract is broken.
  if (endedAt === null || typeof outcome !== "string") {
    return null;
  }

  const runId = typeof value.runId === "string" ? value.runId : undefined;

  return { endedAt, outcome, runId };
}

// handleRunSnapshot folds the backend's AUTHORITATIVE run snapshot into
// the UI (v1.2.6 continuation). The server sends this frame on EVERY
// WebSocket attachment — a reconnect during generation therefore renders
// the run's current state immediately (cumulative snapshots are
// replacement-safe by contract) instead of waiting for a future token.
//
// Terminal snapshots finalise the run exactly like the done/error paths:
// the composer unlocks, the reply snapshot is preserved, and the
// authoritative history reload confirms what was persisted.
function handleRunSnapshot(payload: Record<string, unknown>): void {
  const state = useRuntimeStore.getState();

  const runId = typeof payload.runId === "string" ? payload.runId : "";
  const running = payload.running === true;
  const sequence = typeof payload.sequence === "number" ? payload.sequence : 0;
  const phase = typeof payload.phase === "string" ? payload.phase : "";
  const terminal =
    typeof payload.terminalOutcome === "string"
      ? payload.terminalOutcome
      : "";
  const latestResponse =
    typeof payload.latestResponse === "string" ? payload.latestResponse : "";
  const latestReasoning =
    typeof payload.latestReasoning === "string" ? payload.latestReasoning : "";

  // Adopt the run identity: a socket that attached without a POST (page
  // reload mid-run, chapter switch) binds to whatever run is live.
  if (runId) {
    activeRunId = runId;
    lastRunSeq = sequence;
  }

  // v1.2.8: restore the bounded task-state view from the snapshot — a
  // reconnect mid-run recovers the whole working state (goal, step,
  // files, commands, tests, verification), not just the streamed text.
  if (payload.task && typeof payload.task === "object") {
    useRuntimeStore.setState({
      agentTask: payload.task as Record<string, unknown>,
    });
  }

  // v1.7.4 RUN CONTROL: a paused/pausing/resuming run re-renders its
  // draft and stays parked. The run is NOT terminal (running stays true
  // server-side), so this must be handled BEFORE the terminal branch —
  // a reconnect during PAUSED restores the draft and the revision
  // exactly, and the composer is editable again.
  if (phase === "paused" || phase === "pausing" || phase === "resuming") {
    const pausedDraft =
      typeof payload.pausedDraft === "string" ? payload.pausedDraft : "";
    const revision =
      typeof payload.revision === "number" ? payload.revision : 0;

    if (latestResponse) {
      queueStreamingContent(latestResponse);
    }

    if (latestReasoning) {
      queueStreamingReasoning(latestReasoning);
    }

    flushStreaming();

    // Backend-observed states only (the snapshot IS the authoritative
    // wire event; the pure machine's no-op rules cannot express
    // idle → paused, so the store adopts the observed phase directly).
    // `running` stays TRUE while paused — the run is still registered;
    // editability and the control buttons key on runPhase, never on a
    // fabricated idle state.
    useRuntimeStore.setState({
      running: true,
      runPhase: phase,
      pausedDraft: phase === "paused" ? pausedDraft : null,
      runRevision: revision,
    });

    return;
  }

  // Terminal state: finalise through the same path the done/error events
  // use (the composer unlocks, the history reload confirms persistence).
  if (!running || terminal) {
    const outcome = terminal || "done";

    // Replay whatever the run produced so the partial output is visible.
    if (latestResponse) {
      queueStreamingContent(latestResponse);
    }

    if (latestReasoning) {
      queueStreamingReasoning(latestReasoning);
    }

    flushStreaming();

    if (outcome === "error") {
      transitionPhase("error");

      useRuntimeStore.setState({
        running: false,
        runPhase: "error",
        error:
          typeof payload.error === "string" && payload.error
            ? payload.error
            : "Agent run failed.",
      });
    } else {
      transitionPhase("done");

      runOutcome = outcome === "aborted" ? "aborted" : "done";

      useRuntimeStore.setState({
        running: false,
        runNote: null,
      });
    }

    const sessionId = useRuntimeStore.getState().activeSessionId;

    if (sessionId) {
      scheduleRunFinalisation(sessionId, 50);
    } else {
      useRuntimeStore.setState({ streaming: null });
      runOutcome = null;
      runAssistantBaseline = null;
      releaseIdleOwnedResources();
    }

    return;
  }

  // LIVE run: replay the cumulative snapshots and advance the phase
  // machine from OBSERVED evidence only. A snapshot for a run the UI is
  // not tracking (fresh page load) still renders — the run is real and
  // the backend is authoritative.
  if (!state.running && !isLivePhase(state.runPhase)) {
    // The UI was idle (page reload mid-run): open a fresh timeline.
    promotedPartial = null;
    useRuntimeStore.setState({
      running: true,
      runPhase: "preparing",
      runStartedAt: Date.now(),
      runNote: null,
      liveStatus: null,
      tierEscalations: [],
    });
  }

  runEventsReceived = true;

  if (latestReasoning) {
    queueStreamingReasoning(latestReasoning);
    transitionPhase("reasoning_delta");
  }

  if (latestResponse) {
    queueStreamingContent(latestResponse);
    transitionPhase("response_delta");
  }

  if (phase === "thinking" && !latestResponse) {
    transitionPhase("thinking_activity");
  }

  // The backend's own status line, replayed verbatim.
  const status =
    typeof payload.latestStatus === "string" ? payload.latestStatus : "";

  if (status) {
    useRuntimeStore.setState({ liveStatus: status });
  }
}

function recoverRunFromIdle(event: ActivityEvent): void {
  const state = useRuntimeStore.getState();

  // v1.9.2 THE ATTACH-HANDSHAKE EXCLUSION (root-caused with a full frame +
  // transition trace of the zero-session flow, Actions-class evidence):
  // the server's attach sequence is `attached` -> `idle` -> `run_snapshot`.
  // The idle sentinel in THAT sequence is composed before any run of this
  // socket exists. In the zero-session flow the run() startup state
  // (running + preparing) is set BEFORE the lazy create resolves, so the
  // handshake idle is processed a few ms AFTER the run started — and the
  // final fallback below then declared a live, healthy run "lost" ~90ms
  // after Send (measured 5/5 runs: running=false at +90ms, aborted at
  // +160ms, live surface gone, backend run completing 14s later). The
  // grace re-check would re-clobber at +2.7s (the real run's first gate
  // evidence arrives at ~4.6s), so excluding the handshake sentinel here
  // is the fix for BOTH paths. A sentinel carrying a lastRun block is NOT
  // handshake noise — it is the v1.2.6 authoritative fast-run recovery
  // and keeps flowing through Path A below. A LATE idle (well after the
  // attach window) is real "no active run" evidence and keeps the
  // original semantics.
  const lastRunPresent = readLastRunOutcome(event) !== null;
  if (
    !lastRunPresent &&
    activityAttachAckAt !== 0 &&
    Date.now() - activityAttachAckAt <= 250
  ) {
    return;
  }

  const runStartedAt = state.runStartedAt;
  const sawRunEvidence = state.streaming !== null || runEventsReceived;

  // v1.2.6 path 1 — authoritative evidence: the backend's own last-run
  // record ended at/after this run started, so the run is DEFINITIVELY over.
  // This is exactly the fast-run-late-socket case that used to strand the
  // UI: the run finished before the socket attached, the sentinel was the
  // only signal, and the grace guard dropped it.
  const lastRun = readLastRunOutcome(event);

  if (
    lastRun &&
    runStartedAt !== null &&
    lastRun.endedAt + 500 >= runStartedAt
  ) {
    if (runOutcome === null) {
      switch (lastRun.outcome) {
        case "error":
          runOutcome = "lost";
          break;
        case "aborted":
          runOutcome = "aborted";
          break;
        default:
          runOutcome = "done";
          break;
      }
    }

    flushStreaming();

    useRuntimeStore.setState({ running: false });

    if (state.activeSessionId) {
      scheduleRunFinalisation(state.activeSessionId, 50);
    } else {
      useRuntimeStore.setState({ streaming: null, runPhase: "aborted" });
      runOutcome = null;
      runAssistantBaseline = null;
      releaseIdleOwnedResources();
    }

    return;
  }

  if (
    runStartedAt !== null &&
    !sawRunEvidence &&
    Date.now() - runStartedAt < RUN_IDLE_GRACE_MS
  ) {
    // Standby-entry marker during the attach race — ignore for now, but
    // arm the BOUNDED re-check: if the run produces no evidence by the end
    // of the grace window, it is recovered (never frozen forever).
    if (idleGraceRecheckTimer === null) {
      const dueAt = runStartedAt + RUN_IDLE_GRACE_MS + 200;
      const delay = Math.max(0, dueAt - Date.now());

      idleGraceRecheckTimer = window.setTimeout(() => {
        idleGraceRecheckTimer = null;

        const now = useRuntimeStore.getState();

        if (
          now.running &&
          now.runStartedAt === runStartedAt &&
          !now.streaming &&
          !runEventsReceived
        ) {
          // Still no evidence of the run the UI is waiting for — recover
          // from the authoritative history.
          runOutcome = runOutcome ?? "lost";

          useRuntimeStore.setState({ running: false });

          if (now.activeSessionId) {
            scheduleRunFinalisation(now.activeSessionId, 50);
          } else {
            useRuntimeStore.setState({ streaming: null, runPhase: "aborted" });
            runOutcome = null;
            runAssistantBaseline = null;
            releaseIdleOwnedResources();
          }
        }
      }, delay);
    }

    return;
  }

  if (runOutcome === null) {
    runOutcome = "lost";
  }

  flushStreaming();

  useRuntimeStore.setState({ running: false });

  if (state.activeSessionId) {
    scheduleRunFinalisation(state.activeSessionId, 50);
  } else {
    useRuntimeStore.setState({ streaming: null, runPhase: "aborted" });
    runOutcome = null;
    runAssistantBaseline = null;
    releaseIdleOwnedResources();
  }
}

// handleConversationEvent mirrors activity stream events into the real
// conversation view and repairs the run state machine.
//
// Phase 4: streaming response/reasoning snapshots are COALESCED through
// queueStreamingContent / queueStreamingReasoning and flushed on a rAF
// boundary. This means a model emitting 200 tokens/sec no longer
// triggers 200 React renders/sec — the UI updates at most once per
// frame, naturally capped by the display's refresh rate. Lifecycle
// events (done/error/session) bypass the coalescer and reset state
// immediately so `running` clears without delay.
function handleConversationEvent(event: ActivityEvent): void {
  if (RUN_EVENT_TYPES.has(event.type)) {
    runEventsReceived = true;
  }

  // v1.2.5: one canonical mapping for the whole wire vocabulary — legacy
  // names and the new aliases flow through the SAME handlers, so nothing
  // is processed (or displayed) twice.
  const kind = normalizeEventKind(event.type);

  switch (kind) {
    case "assistant_delta": {
      const content =
        typeof event.data.caption === "string" ? event.data.caption : "";

      if (content) {
        queueStreamingContent(content);
        transitionPhase("response_delta");
      }

      break;
    }

    case "thinking_delta": {
      const reasoning =
        typeof event.data.caption === "string" ? event.data.caption : "";

      if (reasoning) {
        queueStreamingReasoning(reasoning);
        transitionPhase("reasoning_delta");
      }

      break;
    }

    case "thinking_open": {
      // The backend began a reasoning burst — open the panel (actual
      // backend event, never fabricated).
      useRuntimeStore.setState({ thinkingPanelOpen: true });
      transitionPhase("thinking_activity");
      break;
    }

    case "thinking_close": {
      // Reasoning finished — fold the panel by default.
      useRuntimeStore.setState({ thinkingPanelOpen: false });
      transitionPhase("thinking_activity");
      break;
    }

    case "status": {
      // v1.2.5: the backend's own live status line (measured tier/token
      // telemetry) — displayed verbatim, never invented.
      const caption =
        typeof event.data.caption === "string" ? event.data.caption : "";

      if (caption) {
        useRuntimeStore.setState({ liveStatus: caption });
      }

      // v1.7.4 run-control phases ride the status Detail (backend-derived
      // only): "pausing" acknowledges the request, "paused" confirms the
      // durable checkpoint, "resuming" confirms the resume handoff.
      // v1.7.5 WIRING REPAIR: the phase travels inside the activity's
      // DETAIL object (agent.Activity.Detail → wire "detail"), not at the
      // top level — the previous top-level read never matched, so a live
      // PAUSED confirmation never transitioned the phase and the paused
      // panel could not appear without a reconnect.
      const activityRecord =
        event.data && typeof event.data === "object"
          ? (event.data as Record<string, unknown>)
          : undefined;

      const detailRecord =
        activityRecord?.detail && typeof activityRecord.detail === "object"
          ? (activityRecord.detail as Record<string, unknown>)
          : undefined;

      const phaseDetail =
        (detailRecord?.phase as string | undefined) ??
        (activityRecord?.phase as string | undefined);

      if (phaseDetail === "pausing") {
        transitionPhase("pause_requested");
      } else if (phaseDetail === "paused") {
        transitionPhase("paused");

        // v1.7.5: the accepted draft rides the confirmation — the paused
        // editor is populated without a reconnect snapshot.
        const confirmedDraft = detailRecord?.pausedDraft;

        useRuntimeStore.setState({
          liveStatus: caption,
          ...(typeof confirmedDraft === "string"
            ? { pausedDraft: confirmedDraft, runRevision: Number(detailRecord?.revision ?? 0) || useRuntimeStore.getState().runRevision }
            : {}),
        });
      } else if (phaseDetail === "resuming") {
        transitionPhase("resume_requested");
      } else {
        transitionPhase("thinking_activity");
      }
      break;
    }

    case "escalation": {
      // v1.2.5: a tier move with its evidence reason — kept for the
      // timeline and the post-run summary.
      const caption =
        typeof event.data.caption === "string" ? event.data.caption : "";

      useRuntimeStore.setState((state) => ({
        tierEscalations:
          caption && !state.tierEscalations.includes(caption)
            ? [...state.tierEscalations, caption]
            : state.tierEscalations,
      }));

      transitionPhase("thinking_activity");
      break;
    }

    case "task": {
      // v1.2.8: the bounded agent task-state snapshot — the agent pipeline
      // panel renders the REAL runtime state (goal, current step, files,
      // commands, tests, verification). Detail carries the bounded state.
      if (event.data.detail && typeof event.data.detail === "object") {
        useRuntimeStore.setState({
          agentTask: event.data.detail as Record<string, unknown>,
        });
      }

      transitionPhase("thinking_activity");
      break;
    }

    case "handoff": {
      // v1.2.8: agent.md handoff written at settlement — a timeline
      // event only; the durable state lives in the workspace file.
      transitionPhase("thinking_activity");
      break;
    }

    case "tool_start":
    case "tool_end":
    case "context":
    case "perf":
    case "failure":
    case "plan":
    case "progress": {
      // Progressive activity alongside the generation — these advance
      // Preparing → Thinking but never demote Generating.
      transitionPhase("thinking_activity");

      // v1.8.2: the context provenance report carries the MEMORY
      // EVIDENCE record (plan.MemoryEvidence) — what the memory
      // authorities actually injected THIS turn. Stored verbatim; the
      // live bubble renders exactly this, never a fabricated count.
      if (kind === "context") {
        const evidence = extractMemoryEvidence(event.data);

        useRuntimeStore.setState({
          memoryEvidence: evidence,
        });
      }

      // v1.3.6 (spec §25): Net Search evidence states from REAL wire
      // events — searching while the research tool executes, results or
      // failure when it settles, with the stated result count when the
      // output carries one. No fabricated counts, no inferred intent.
      {
        const data: unknown = event.data;
        const caption: unknown = event.data.caption;

        if (kind === "tool_start" && isResearchToolStart(data)) {
          useRuntimeStore.setState({
            netSearchState: "searching",
            netSearchResultCount: null,
          });
        } else if (kind === "tool_end" && isResearchToolEnd(caption)) {
          const resultText: unknown = event.data.detail;
          const failed = researchEndFailed(resultText);
          const count = extractNetSearchResultCount(resultText);

          useRuntimeStore.setState({
            netSearchState: failed ? "failed" : "results",
            netSearchResultCount: count,
          });
        }
      }

      break;
    }

    case "idle": {
      // Sent by the backend whenever this socket has no run attached.
      // Only meaningful when the UI believes a run is live — see
      // recoverRunFromIdle.
      const state = useRuntimeStore.getState();

      if (state.running || isLivePhase(state.runPhase)) {
        recoverRunFromIdle(event);
      }

      break;
    }

    case "session": {
      // v1.1.4: Continuum chapter rollover — the backend distilled the
      // conversation into a fresh chapter session and tells the UI here.
      // Follow the thread into the new chapter automatically.
      const nextSessionId =
        typeof event.data.sessionId === "string" ? event.data.sessionId : "";
      const currentId = useRuntimeStore.getState().activeSessionId;

      if (nextSessionId && nextSessionId !== currentId) {
        // Drop any pending streaming chunks — the chapter is closing.
        resetPendingStreaming();
        clearRunFinalizeTimer();
        runOutcome = null;
        runAssistantBaseline = null;

        useRuntimeStore.setState({
          running: false,
          streaming: null,
          runPhase: "complete",
          runNote: null,
        });
        void useRuntimeStore.getState().selectSession(nextSessionId);
        void useRuntimeStore.getState().refreshSessions();
      }

      break;
    }

    case "aborted": {
      // v1.8.0: the backend's HONEST abort marker. The orchestrator now
      // publishes "aborted" (never "done") when the run context is
      // canceled, so the stop path is decided by the event TYPE — no more
      // caption-string sniffing on done events. The outcome is terminal:
      // release the composer, settle the timeline on "Stopped", and run
      // the normal finalisation (the backend persisted the partial answer
      // through finishSuccessfulRun, so history reload finds it).
      flushStreaming();

      useRuntimeStore.setState({ thinkingPanelOpen: false });

      const abortCaption =
        typeof event.data.caption === "string" ? event.data.caption : "";

      const abortedState = useRuntimeStore.getState();
      const abortedSessionId = abortedState.activeSessionId;

      transitionPhase("aborted");

      runOutcome = "aborted";

      useRuntimeStore.setState({
        running: false,
        runNote: abortCaption || null,
      });

      if (abortedSessionId) {
        scheduleRunFinalisation(abortedSessionId, 400);
      } else {
        useRuntimeStore.setState({ streaming: null, runPhase: "aborted" });
        runOutcome = null;
        runAssistantBaseline = null;
        releaseIdleOwnedResources();
      }

      break;
    }

    case "done":
    case "complete":
    case "error": {
      // THE v1.1.2 dead-composer fix: a finished or failed run must
      // always release the composer. The old code only reset `running`
      // on error paths, so a successful reply left it disabled forever.
      //
      // v1.2.2: flush pending snapshots FIRST so the final content is
      // visible, then run the full finalisation state machine. The
      // streaming bubble stays visible through "Finalising" until the
      // authoritative history (or the preserved partial) replaces it —
      // no blink, no lost output.
      flushStreaming();

      // v1.2.5: the run ended — fold the thinking panel and freeze the
      // status chip (it will refresh on the next run).
      useRuntimeStore.setState({ thinkingPanelOpen: false });

      const caption =
        typeof event.data.caption === "string" ? event.data.caption : "";

      const state = useRuntimeStore.getState();
      const sessionId = state.activeSessionId;

      if (event.type === "error") {
        // Terminal failure: the backend did NOT persist a reply. Unlock
        // the composer, surface the backend's own caption, and keep the
        // partial output as a local assistant message.
        transitionPhase("error");

        useRuntimeStore.setState({
          running: false,
          error: caption || "Agent run failed.",
          runNote: caption || null,
        });

        promotePartialOutput(
          caption
            ? `run failed — partial reply preserved locally (${caption})`
            : "run failed — partial reply preserved locally",
        );

        runOutcome = null;
        runAssistantBaseline = null;
        releaseIdleOwnedResources();

        break;
      }

      transitionPhase("done");

      // v1.2.2: the backend's own abort captions mark stopped runs so the
      // timeline settles on "Stopped", not "Complete".
      const captionLower = caption.toLowerCase();
      const abortedByBackend =
        captionLower === "aborted by user" ||
        captionLower.startsWith("run stopped");

      runOutcome = abortedByBackend ? "aborted" : "done";

      useRuntimeStore.setState({
        running: false,
        runNote: caption || null,
      });

      if (sessionId) {
        scheduleRunFinalisation(sessionId, 400);
      } else {
        useRuntimeStore.setState({ streaming: null, runPhase: "complete" });
        runOutcome = null;
        runAssistantBaseline = null;
        releaseIdleOwnedResources();
      }

      break;
    }

    default:
      break;
  }
}

// v1.2.8.1: boot-time mode + per-mode selections, resolved ONCE so the
// initial state (active session of the startup space) and the persisted
// map agree.
const bootMode = initialWorkspaceMode();
const bootSessions = initialActiveSessionByMode();

export const useRuntimeStore = create<RuntimeState>((set, get) => ({
  app: null,
  sysinfo: null,
  models: null,
  modelsLoading: false,
  presets: [],
  tools: [],

  sessions: [],
  // (activeSessionId is initialized from the persisted per-mode map at
  // the mode block below — v1.2.8.1.)

  connection: "idle",
  loading: false,
  error: null,

  activity: [],
  running: false,

  // v1.2.2: generation timeline state.
  runPhase: "idle",
  runStartedAt: null,
  runNote: null,

  // v1.7.4 run control state.
  pausedDraft: null,
  runRevision: 0,

  // v1.2.5: live status + escalation trail + composer controls.
  liveStatus: null,
  tierEscalations: [],
  memoryEvidence: null,
  thinkingPanelOpen: false,
  thinkingControl: initialThinkingControl(),
  showThinking: initialShowThinking(),
  toolPolicyMode: initialToolPolicyMode(),
  toolAllowlist: initialToolAllowlist(),
  netSearch: initialNetSearch(),
  netSearchState: initialNetSearch() ? "enabled" : "off",
  netSearchResultCount: null,

  engine: null,

  // v1.1.6: per-session context status (fetched per active session).
  sessionContext: null,
  sessionContextError: null,

  messages: [],
  streaming: null,

  // v1.4.0: explicit transcript surface states (see the interface
  // comment). "ready" + zero messages = a genuinely empty history —
  // the only state allowed to render the empty-conversation invite.
  historyStatus: "ready",
  historyError: null,

  // v1.4.0: store-held composer draft (survives view-switch remounts).
  composerDraft: "",

  pendingAttachments: [],
  attachmentsUploading: false,

  lab: null,
  labLoading: false,
  labError: null,
  activeLabTaskId: null,
  activeLabTask: null,

  refreshSysinfo: async () => {
    try {
      const sysinfo = await api.sysinfo();

      set({ sysinfo });
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to refresh system information.",
      });
    }
  },

  refreshModels: async () => {
    // v1.2.3: surface the in-flight state so UIs render a skeleton
    // instead of mistaking "not loaded yet" for "no models".
    set({ modelsLoading: true });
    try {
      const models = await api.models();

      set({ models, modelsLoading: false });
    } catch (error) {
      set({
        modelsLoading: false,
        error:
          error instanceof Error ? error.message : "Failed to refresh models.",
      });
    }
  },

  // v1.2.5: composer control actions — persisted + sent with every run.
  setThinkingControl: (v) => {
    const control = normalizeThinkingControl(v);
    persistThinkingControl(control);
    set({ thinkingControl: control });
  },

  // v1.8.5: SHOW/HIDE THINKING — pure visibility. Persisted through the
  // same authority; NEVER part of the run payload, never touches the
  // reasoning level, the budget or the tier posture.
  setShowThinking: (v) => {
    persistShowThinking(v);
    set({ showThinking: v });
  },

  setToolPolicyMode: (v) => {
    const mode = normalizeToolPolicyMode(v);
    persistToolPolicyMode(mode);
    set({ toolPolicyMode: mode });
  },

  setToolAllowed: (name, allowed) => {
    const clean = name.trim();
    if (!clean) {
      return;
    }

    const current = get().toolAllowlist;
    const key = clean.toLowerCase();

    const next = allowed
      ? current.some((n) => n.toLowerCase() === key)
        ? current
        : [...current, clean]
      : current.filter((n) => n.toLowerCase() !== key);

    persistToolAllowlist(next);
    set({ toolAllowlist: next });
  },

  // v1.3.6 (spec §25): the Net Search control is a REAL action — the
  // intent travels with the next run and the state chip always reflects
  // what actually happened, never a decorative toggle.
  setNetSearch: (v) => {
    persistNetSearch(v);
    set({
      netSearch: v,
      netSearchState: v ? "enabled" : "off",
      netSearchResultCount: null,
    });
  },

  // v1.4.0: the composer draft is store-held so a workspace view switch
  // (which remounts the composer) cannot destroy what the user typed.
  setComposerDraft: (v) => {
    set({ composerDraft: v });
  },

  refreshPresets: async () => {
    try {
      const presets = await api.presets();

      set({ presets });
    } catch (error) {
      set({
        error:
          error instanceof Error ? error.message : "Failed to refresh presets.",
      });
    }
  },

  refreshSessions: async () => {
    // v1.2.8.1: capture the mode BEFORE the await — a refresh started in
    // one mode must never land after a mode switch and overwrite the new
    // space's list/selection with the old mode's sessions.
    const mode = get().mode;

    // v1.7.5: take a generation ticket BEFORE the await. Any mutation
    // (create/delete/rename/mode switch) or newer refresh invalidates this
    // ticket, so this response can only land when nothing newer happened
    // while it was in flight — the stale response can never resurrect a
    // deleted session or drop a created one.
    const ticket = sessionListGuard.begin();

    try {
      // v1.2.8: the sidebar shows ONE conversation space. The full list is
      // still reachable through the history picker (cross-mode search).
      const sessions = await api.sessions(mode);

      const after = get();

      if (after.mode !== mode) {
        return; // the user switched modes while this fetch was in flight
      }

      if (!sessionListGuard.isActive(ticket)) {
        return; // a mutation or a newer refresh superseded this response
      }

      const previous = after.activeSessionId;

      // The remembered selection only survives when it still exists IN
      // THIS mode's space (never silently switched to another space).
      const activeSessionId = resolveActiveForMode(
        sessions,
        mode,
        previous,
      );

      set((state) => ({
        sessions,
        activeSessionId,
        activeSessionByMode: {
          ...state.activeSessionByMode,
          [mode]: activeSessionId,
        },
      }));

      // v1.2.8.1: when the re-resolution CHANGED the active id (remembered
      // session deleted, or first entry into a space with no memory), the
      // conversation follows the new selection — the UI must never show an
      // empty conversation merely because the session list refreshed
      // asynchronously.
      if (activeSessionId && activeSessionId !== previous) {
        if (activityConsumers > 0) {
          get().connectActivity();
        }

        void get().loadSession(activeSessionId);
        void get().refreshSessionContext();
      }
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to refresh sessions.",
      });
    }
  },

  refreshTools: async () => {
    try {
      const tools = await api.tools();

      set({ tools });
    } catch (error) {
      set({
        error:
          error instanceof Error ? error.message : "Failed to refresh tools.",
      });
    }
  },

  refreshAgentResources: async () => {
    set({
      error: null,
    });

    try {
      const [sysinfo, presets, tools] = await Promise.all([
        api.sysinfo(),
        api.presets(),
        api.tools(),
      ]);

      set({
        sysinfo,
        presets,
        tools,
      });
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to load Agent resources.",
      });
    }
  },

  refreshEngine: async () => {
    try {
      const engine = await api.engine();

      // v1.2.4: skip the state write when the snapshot is unchanged. The
      // poll fires every 2.5 s; a fresh object identity used to re-render
      // every component that selects `engine` (AgentBody, AgentHeader,
      // ModelPicker) even when nothing changed.
      const prev = useRuntimeStore.getState().engine;
      if (prev && JSON.stringify(prev) === JSON.stringify(engine)) {
        return;
      }

      set({ engine });
    } catch {
      // Engine endpoint unreachable — connection state already reflects
      // backend health; leave the last known engine snapshot in place.
    }
  },

  startEnginePolling: () => {
    if (enginePollTimer !== null) {
      return;
    }

    void get().refreshEngine();

    enginePollTimer = window.setInterval(() => {
      void useRuntimeStore.getState().refreshEngine();
    }, 2500);
  },

  stopEnginePolling: () => {
    if (enginePollTimer !== null) {
      window.clearInterval(enginePollTimer);
      enginePollTimer = null;
    }
  },

  // v1.2.2: ownership API. acquire/release bracket the mounted lifetime
  // of the Agent surface. A LIVE run keeps the socket + poll alive across
  // workspace tab switches (the backend activity hub has no replay);
  // they are released the moment the run settles if nothing is mounted.
  acquireActivity: () => {
    activityConsumers += 1;

    if (get().activeSessionId) {
      get().connectActivity();
    }
  },

  releaseActivity: () => {
    activityConsumers = Math.max(0, activityConsumers - 1);

    const state = get();

    if (
      activityConsumers === 0 &&
      !state.running &&
      !isLivePhase(state.runPhase)
    ) {
      state.disconnectActivity();
    }
  },

  acquireEnginePolling: () => {
    enginePollConsumers += 1;

    get().startEnginePolling();
  },

  releaseEnginePolling: () => {
    enginePollConsumers = Math.max(0, enginePollConsumers - 1);

    const state = get();

    if (
      enginePollConsumers === 0 &&
      !state.running &&
      !isLivePhase(state.runPhase)
    ) {
      state.stopEnginePolling();
    }
  },

  // v1.1.6: fetch the backend-resolved context status for the active
  // session. Switching sessions restores each chat's own policy — the
  // status is per-session and never mutates other chats.
  refreshSessionContext: async () => {
    const id = get().activeSessionId;

    if (!id) {
      // v1.8.4: no active session — every in-flight context response is
      // invalidated with the generation bump, and the visible context is
      // cleared (a stale session's usage must never survive its session).
      invalidateSessionContext();
      set({ sessionContext: null, sessionContextError: null });
      return;
    }

    // v1.8.4 MONOTONIC REQUEST GENERATION: every refresh claims a strictly
    // newer generation. Responses settle only when they are BOTH (a) still
    // the newest request AND (b) bound to the still-active session. Two
    // requests for the SAME session can now resolve in any order without
    // the older response overwriting the newer state (the pre-1.8.4 guard
    // checked only the session id, so A-start/B-start/B-settles/A-settles
    // landed A's older usage last).
    const generation = ++contextRefreshGeneration;

    try {
      const status = await api.sessionContext(id);

      const state = useRuntimeStore.getState();

      if (
        generation !== contextRefreshGeneration ||
        state.activeSessionId !== id
      ) {
        return;
      }

      set({ sessionContext: status, sessionContextError: null });
    } catch (err) {
      if (
        generation !== contextRefreshGeneration ||
        useRuntimeStore.getState().activeSessionId !== id
      ) {
        return;
      }

      set({
        sessionContext: null,
        sessionContextError:
          err instanceof Error ? err.message : "context status unavailable",
      });
    }
  },

  // v1.1.6: set THIS session's context policy; the backend persists it
  // with the session and returns the resolved status.
  setSessionContext: async (tokens) => {
    const id = get().activeSessionId;
    if (!id) {
      return;
    }
    try {
      const status = await api.setSessionContext(id, tokens);
      if (useRuntimeStore.getState().activeSessionId !== id) {
        return;
      }
      set({ sessionContext: status, sessionContextError: null });
    } catch (err) {
      set({
        sessionContextError:
          err instanceof Error ? err.message : "could not set the context",
      });
    }
  },

  loadSession: async (id) => {
    // v1.4.0: switching to a different session must show an explicit
    // LOADING state — the previous session's messages are cleared and
    // the history surface renders "loading history" until the page
    // arrives (or fails). A failed fetch sets historyStatus "error" and
    // NEVER renders the empty-conversation invitation for a history
    // that was never established.
    const switching = useRuntimeStore.getState().activeSessionId !== id;

    if (switching) {
      set({
        messages: [],
        historyStatus: "loading",
        historyError: null,
        olderHasMore: false,
        olderNextBefore: null,
      });
    }

    try {
      // v1.2.8: LAZY HISTORY PAGING — load the newest page instead of the
      // whole transcript. Older pages fetch on demand (loadOlderMessages);
      // the authoritative full session remains on the backend.
      //
      // v1.2.8.1: when reloading the session the user is ALREADY reading
      // (e.g. run finalisation reloads after `done`), the fetched page
      // covers everything currently visible so the older pages the user
      // expanded are not silently collapsed back to the newest page.
      // The backend page cap (200) bounds the request.
      const prev = useRuntimeStore.getState();
      const preservePages =
        prev.activeSessionId === id && prev.messages.length > 60;
      const limit = preservePages
        ? Math.min(200, Math.max(60, prev.messages.length + 1))
        : 60;

      const page = await api.sessionMessagesPage(id, undefined, limit);

      // Only apply if the session is still the active one.
      if (useRuntimeStore.getState().activeSessionId !== id) {
        return;
      }

      set({
        messages: page.messages.map((entry) => entry.message),
        olderHasMore: page.hasMore,
        olderNextBefore: page.hasMore ? page.nextBefore : null,
        historyStatus: "ready",
        historyError: null,
      });
    } catch (error) {
      // v1.4.0: a failed history fetch is a LOADING FAILURE, not an
      // empty conversation. The previous catch converted every error
      // into `messages: []` — the "fresh session not yet persisted"
      // case is real, but a dead backend or a network failure produced
      // the identical fake-empty view. The empty transcript is now only
      // ever established by a SUCCESSFUL page fetch (or a genuinely
      // fresh session the backend confirms); failures surface as an
      // explicit retryable error state.
      if (useRuntimeStore.getState().activeSessionId === id) {
        set({
          historyStatus: "error",
          historyError:
            error instanceof Error
              ? error.message
              : "Failed to load conversation history.",
        });
      }
    }

    // v1.2.8: restore the attached cross-mode history references with
    // the session (best-effort — a session without refs stays empty).
    try {
      const detail = await api.sessionDetail(id);

      if (useRuntimeStore.getState().activeSessionId === id) {
        set({
          historyRefs: normalizeHistoryRefs(
            detail.context?.historyRefs,
            id,
          ),
        });
      }
    } catch {
      // Refs are optional metadata; silence is correct here.
    }
  },

  refreshLab: async () => {
    set({
      labLoading: true,
      labError: null,
    });

    try {
      const lab = await api.lab();

      const activeLabTaskId = get().activeLabTaskId;

      const activeLabTask = activeLabTaskId
        ? (lab.tasks.find((item) => item.id === activeLabTaskId) ?? null)
        : null;

      set({
        lab,
        labLoading: false,
        activeLabTask,
      });
    } catch (error) {
      set({
        labLoading: false,
        labError:
          error instanceof Error
            ? error.message
            : "Failed to refresh Coding Lab.",
      });
    }
  },

  loadLabTask: async (id) => {
    const taskId = id.trim();

    if (!taskId) {
      set({
        activeLabTaskId: null,
        activeLabTask: null,
      });

      return;
    }

    set({
      activeLabTaskId: taskId,
      labLoading: true,
      labError: null,
    });

    try {
      const activeLabTask = await api.labTask(taskId);

      set({
        activeLabTask,
        labLoading: false,
      });
    } catch (error) {
      set({
        labLoading: false,
        labError:
          error instanceof Error
            ? error.message
            : "Failed to load Coding Lab task.",
      });
    }
  },

  createSession: async (opts?: { keepRunState?: boolean }) => {
    // v1.2.8: the new session is created in the CURRENT conversation
    // space and becomes that space's active session.
    const session = await api.createSession(get().mode);

    // v1.7.5: the authoritative create succeeded — any list GET that was
    // already on the wire is now stale (it cannot contain the new session
    // and must never overwrite the state below).
    sessionListGuard.invalidate();

    // v1.1.4: createSession previously only prepended the session and
    // switched the id — the socket stayed bound to the OLD session (the
    // stale-guard then silently discarded every event for the new one)
    // and messages/streaming/running were never reset. First message on
    // a fresh session never streamed and the composer stuck "running".
    get().disconnectActivity();

    // Phase 4: drop any pending streaming chunks for the OLD session.
    resetPendingStreaming();

    // v1.8.4 (P0-D): a new session invalidates every in-flight context
    // response of the previous state; the fresh session's context is
    // unknown until its own refresh lands.
    invalidateSessionContext();

    // v1.2.2: a fresh session ends any in-flight timeline for the old one.
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetRunReplayTracking(null);

    set((state) => ({
      // v1.8.3: the prepend is IDEMPOTENT. A session-list refresh whose
      // response lands between the POST's dispatch and its arrival can
      // already contain the created session (the backend registered it
      // the moment the POST was served) — prepending unconditionally
      // then produced a DUPLICATE row (same id twice). The new session
      // is always the newest first row and appears exactly once.
      sessions: [session, ...state.sessions.filter((s) => s.id !== session.id)],
      activeSessionId: session.id,
      activeSessionByMode: {
        ...state.activeSessionByMode,
        [state.mode]: session.id,
      },
      error: null,
      activity: [],
      messages: [],
      // v1.4.0: a newly created session IS genuinely empty — the empty
      // invite is truthful here without a history fetch.
      historyStatus: "ready",
      historyError: null,
      streaming: null,
      // v1.9.0 (zero-session reload P0): when run() lazily creates the
      // session, the run-startup state set BEFORE the create must survive
      // it — otherwise `running` flickers back to false mid-startup and
      // the composer reads idle while POST /api/run is still in flight.
      ...(opts?.keepRunState
        ? {}
        : {
            running: false,
            runPhase: "idle" as const,
            runStartedAt: null,
            runNote: null,
          }),
      pendingAttachments: [],
      historyRefs: [],
      agentTask: null,
      // v1.8.4 (P0-D): the new session's context is genuinely unknown —
      // never carry the previous session's usage into it.
      sessionContext: null,
      sessionContextError: null,
      olderHasMore: false,
      olderNextBefore: null,
    }));

    // v1.2.2: only a mounted consumer keeps the new session's socket.
    if (activityConsumers > 0) {
      get().connectActivity();
    }

    return session;
  },

  selectSession: (id) => {
    if (get().activeSessionId === id) {
      return;
    }

    get().disconnectActivity();

    // v1.2.2: switching sessions ends the previous run's timeline.
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetRunReplayTracking(null);

    set((state) => ({
      activeSessionId: id,
      // v1.2.8: remember the selection PER MODE so switching modes never
      // silently switches the active conversation of either space.
      activeSessionByMode: {
        ...state.activeSessionByMode,
        [state.mode]: id,
      },
      error: null,
      activity: [],
      messages: [],
      // v1.4.0: switching sessions enters the LOADING state — the
      // transcript is unknown until the page fetch settles.
      historyStatus: "loading",
      historyError: null,
      streaming: null,
      running: false,
      runPhase: "idle",
      runStartedAt: null,
      runNote: null,
      pendingAttachments: [],
      historyRefs: [],
      agentTask: null,
      olderHasMore: false,
      olderNextBefore: null,
    }));

    // Phase 4: drop any pending streaming chunks for the OLD session.
    resetPendingStreaming();

    if (id) {
      // v1.2.2: only a mounted consumer (Agent surface) keeps a live
      // socket — switching chapters from a background view must not
      // leak an ownerless connection.
      if (activityConsumers > 0) {
        get().connectActivity();
      }

      void get().loadSession(id);
      void get().refreshSessionContext();
    } else {
      set({ sessionContext: null, sessionContextError: null });
    }
  },

  deleteSession: async (id) => {
    // v1.8.3: a failed DELETE is surfaced HONESTLY (the same error
    // surface renameSession uses) — never swallowed. The v1.8.2 sidebar
    // invoked this as `void deleteSession(id)`, so a server-side failure
    // became an invisible unhandled rejection: the session stayed in the
    // list with no explanation, violating the never-hide-a-DELETE-error
    // rule. The store keeps its state untouched on failure; the error
    // banner carries the server's own message.
    let deleted = false;

    try {
      await api.deleteSession(id);
      deleted = true;
    } catch (error) {
      set({
        error:
          error instanceof Error ? error.message : "Failed to delete session.",
      });
    }

    if (!deleted) {
      return;
    }

    // v1.7.5: the authoritative DELETE succeeded — invalidate every list
    // GET that started before this instant. A stale in-flight response
    // (which still contains the deleted session) can never land after
    // this, so the sidebar can never resurrect it.
    sessionListGuard.invalidate();

    if (get().activeSessionId === id) {
      get().disconnectActivity();
    }

    set((state) => {
      const sessions = state.sessions.filter((session) => session.id !== id);

      // v1.2.8: the replacement selection stays INSIDE the current mode's
      // space (the list is already mode-filtered; this re-resolves).
      const activeSessionId = resolveActiveForMode(
        sessions,
        state.mode,
        state.activeSessionId === id ? null : state.activeSessionId,
      );

      return {
        sessions,
        activeSessionId,
        activeSessionByMode: {
          ...state.activeSessionByMode,
          [state.mode]: activeSessionId,
        },
        // v1.1.4: the deleted session's conversation previously stayed
        // on screen (and kept streaming state) until the next manual switch.
        messages: state.activeSessionId === id ? [] : state.messages,
        activity: state.activeSessionId === id ? [] : state.activity,
        streaming: null,
        running: false,
        runPhase: state.activeSessionId === id ? "idle" : state.runPhase,
        runStartedAt: state.activeSessionId === id ? null : state.runStartedAt,
        runNote: state.activeSessionId === id ? null : state.runNote,
        // v1.8.4 (P0-D): the deleted session's usage must never remain
        // visible — cleared here, refreshed below when a replacement
        // session became active.
        sessionContext:
          state.activeSessionId === id ? null : state.sessionContext,
        sessionContextError:
          state.activeSessionId === id ? null : state.sessionContextError,
      };
    });

    // v1.8.4 (P0-D): every in-flight context response is invalidated by a
    // deletion (its session may be gone); a replacement session starts a
    // fresh authoritative refresh.
    invalidateSessionContext();

    const nextId = get().activeSessionId;

    if (nextId) {
      // v1.2.2: same ownership guard as selectSession.
      if (activityConsumers > 0) {
        get().connectActivity();
      }

      void get().loadSession(nextId);
      void get().refreshSessionContext();
    }
  },

  run: async (message) => {
    // v1.9.0 (zero-session reload P0, root cause): EVERY async startup
    // step — including the lazy session create — now happens while the
    // composer provably reads "running". The v1.8.8 repair moved the
    // running:true set before the attach wait but left ONE async window
    // open in the zero-session path specifically: the lazy
    // createSession() POST ran while running was still false, so an
    // enabled-composer observation (the E2E "wait for run to settle"
    // probe) could legitimately sample INSIDE it and reload the app
    // before POST /api/run was ever dispatched — the session existed
    // (server-side pending) but its transcript was permanently empty.
    // The run-startup state is therefore set FIRST, the lazy create
    // carries keepRunState so it cannot clobber it, and every failure
    // path cleans the state up deterministically.
    if (!message.trim()) {
      return;
    }

    // v1.9.0: snapshot the pending attachments BEFORE any transport work —
    // the lazy create path clears pendingAttachments internally, so the
    // optimistic bubble and the run payload must read the snapshot.
    const pendingAttachmentsSnapshot = get().pendingAttachments;
    const attachmentNames = pendingAttachmentsSnapshot.map((item) => item.name);
    const attachmentIds = pendingAttachmentsSnapshot.map((item) => item.id);

    // v1.2.2: reset this run's bookkeeping BEFORE the optimistic bubble —
    // the generation timeline becomes visible the instant Send is
    // accepted (no dead visual gap while the engine gate warms up).
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetPendingStreaming();
    resetRunReplayTracking(null);

    // v1.8.4 (P0-D): a fresh run is a relevant state transition — every
    // in-flight context response that predates the run can never
    // overwrite newer state during or after it (finaliseRun issues the
    // authoritative post-run refresh).
    invalidateSessionContext();

    // v1.9.4 (P0-A): anchor the client-side run window (trace-only).
    recordRunStart();

    // v1.8.8 (the E2E reload-persistence race repair): the run-startup
    // state becomes visible HERE — before the transport work below, not
    // after it. run() previously kept `running:false` across the attach
    // wait, so the composer read as idle while a Send was mid-startup;
    // a reload in that window killed the pipeline before the run POST
    // ever fired and the message silently never happened (proven by the
    // zero-session reload E2E trace: no POST /api/run, total:0 transcript).
    promotedPartial = null;
    set({
      running: true,
      error: null,
      runPhase: "preparing",
      runStartedAt: Date.now(),
      runNote: null,
      // v1.2.5: fresh run — fresh status line and escalation trail.
      // v1.8.2: fresh run — fresh memory evidence (the new turn's
      // context report replaces it).
      liveStatus: null,
      tierEscalations: [],
      memoryEvidence: null,
      // v1.2.8: fresh run — fresh task-state view.
      agentTask: null,
    });

    // v1.2.8.1: a session is created LAZILY on the first message when the
    // current space has none (the documented v1.2.8 behavior — the
    // composer previously threw "No active session." instead).
    if (!get().activeSessionId) {
      try {
        // v1.9.0: keepRunState — see the run-startup note above.
        await get().createSession({ keepRunState: true });
      } catch {
        // v1.9.0: a failed lazy create is a FAILED run start — the
        // composer must not stay locked (the historical fall-through
        // left running:true set with no run to settle it).
        set({
          running: false,
          runPhase: "idle",
          runStartedAt: null,
          runNote: null,
          liveStatus: null,
        });
        throw new Error("No active session.");
      }
    }

    const sessionId = get().activeSessionId;

    if (!sessionId) {
      set({
        running: false,
        runPhase: "idle",
        runStartedAt: null,
        runNote: null,
        liveStatus: null,
      });
      throw new Error("No active session.");
    }

    // v1.2.6 continuation: the DETERMINISTIC ATTACH CONTRACT — the POST
    // fires only after the server acknowledged this session's socket
    // (`attached` frame). The v1.2.6 code merely started the connection
    // (CONNECTING) and POSTed immediately: a fast run could finish before
    // the socket ever attached, leaving the recovery paths to paper over
    // the race. Awaiting the acknowledgement REMOVES the race. A rejected
    // acknowledgement (socket died) proceeds anyway — the runId replay
    // and the bounded grace re-check remain the safety net for older
    // backends and hard failures; neither is the primary mechanism here.
    get().connectActivity();

    try {
      await waitForActivityAttached();
    } catch {
      // No acknowledged transport — the run still POSTs; recovery paths
      // (run_snapshot replay, idle grace re-check) own the resync.
    }

    // v1.1.3: optimistic user bubble — the conversation shows the sent
    // message immediately, before any streaming event arrives.
    // v1.9.0: attachment names/ids come from the pre-transport snapshot
    // (the lazy create path clears pendingAttachments internally).
    set((state) => ({
      messages: [
        ...state.messages,
        {
          role: "user" as const,
          content: message.trim(),
          ...(attachmentNames.length > 0
            ? { attachments: attachmentNames }
            : {}),
        },
      ],
      streaming: null,
    }));

    set({ pendingAttachments: [] });

    try {
      const state = get();

      const response = await api.run({
        sessionId,
        message: message.trim(),
        ...(attachmentIds.length > 0 ? { attachmentIds } : {}),
        // v1.2.5: the composer controls travel with the request — they
        // change the actual backend behaviour (tier posture, tool surface).
        // v1.8.5: the reasoning level ALWAYS travels — every rung of the
        // low/mid/high/ultra ladder has a real numeric thinking budget
        // (the backend normalizes legacy values for older clients).
        thinking: state.thinkingControl,
        ...(state.toolPolicyMode === "manual"
          ? { toolMode: "manual", toolAllow: state.toolAllowlist }
          : {}),
        // v1.3.6 (spec §24): the explicit Net Search intent travels with
        // every request while enabled — server-side enforcement, never
        // text inference.
        ...(state.netSearch ? { netSearch: true } : {}),
        // v1.2.8: the attached cross-mode history references travel with
        // every request — the backend retrieves only the relevant portions
        // and labels provenance (the source sessions are never modified).
        ...(state.historyRefs.length > 0
          ? { historyRefs: state.historyRefs }
          : {}),
      });

      // v1.2.6 continuation: bind the UI to the authoritative runId the
      // POST returned — every subsequent frame of THIS run is accepted,
      // and stale drains of a replaced run are dropped (isStaleRunEvent).
      if (response.runId) {
        activeRunId = response.runId;
        lastRunSeq = 0;
      }

      await get().refreshSessions();
    } catch (error) {
      set({
        error: error instanceof Error ? error.message : "Agent run failed.",
        running: false,
        runPhase: "error",
        runNote:
          error instanceof Error ? error.message : "The run request failed.",
      });

      releaseIdleOwnedResources();

      throw error;
    }
  },

  regenerate: async () => {
    const sessionId = get().activeSessionId;

    if (!sessionId) {
      return;
    }

    if (get().running) {
      return;
    }

    // v1.2.2: same timeline reset as a fresh run.
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetPendingStreaming();
    resetRunReplayTracking(null);

    // v1.2.6 continuation: same deterministic attach contract as run().
    get().connectActivity();

    try {
      await waitForActivityAttached();
    } catch {
      // Proceed — the replay + grace re-check recover.
    }

    promotedPartial = null;
    set({
      running: true,
      error: null,
      streaming: null,
      runPhase: "preparing",
      runStartedAt: Date.now(),
      runNote: null,
      liveStatus: null,
      tierEscalations: [],
      // v1.8.2: fresh run — fresh memory evidence.
      memoryEvidence: null,
      // v1.2.8.1: a regenerated run is a FRESH timeline — same parity as
      // run(); the previous run's task panel must not linger.
      agentTask: null,
    });

    try {
      const state = get();

      const response = await api.run({
        sessionId,
        message: "",
        regenerate: true,
        // v1.8.5: same reasoning ladder contract as a fresh run — the
        // level (and its budget) always travels.
        thinking: state.thinkingControl,
        ...(state.toolPolicyMode === "manual"
          ? { toolMode: "manual", toolAllow: state.toolAllowlist }
          : {}),
        // v1.3.6 (spec §24): the explicit Net Search intent travels with
        // every request while enabled — server-side enforcement, never
        // text inference.
        ...(state.netSearch ? { netSearch: true } : {}),
        // v1.2.8.1 REPAIR: the regenerated turn runs with the SAME
        // cross-mode history references as the original run. The payload
        // previously omitted historyRefs entirely and the backend had no
        // fallback, so regeneration silently lost every attached history
        // context. (The backend now also falls back to the persisted
        // session context — this is the explicit belt to that brace.)
        ...(state.historyRefs.length > 0
          ? { historyRefs: state.historyRefs }
          : {}),
      });

      if (response.runId) {
        activeRunId = response.runId;
        lastRunSeq = 0;
      }

      // Drop the trailing assistant bubble optimistically; the reload on
      // done restores the authoritative history.
      set((state) => {
        const messages = [...state.messages];

        for (;;) {
          const last = messages[messages.length - 1];

          if (
            messages.length > 0 &&
            last &&
            (last.role === "assistant" || last.role === "tool")
          ) {
            messages.pop();

            continue;
          }

          break;
        }

        return { messages };
      });

      await get().refreshSessions();
    } catch (error) {
      set({
        error: error instanceof Error ? error.message : "Regenerate failed.",
        running: false,
        runPhase: "error",
        runNote:
          error instanceof Error
            ? error.message
            : "The regenerate request failed.",
      });

      releaseIdleOwnedResources();

      throw error;
    }
  },

  uploadFiles: async (files) => {
    const sessionId = get().activeSessionId;

    if (!sessionId || files.length === 0) {
      return;
    }

    set({ attachmentsUploading: true, error: null });

    try {
      const response = await api.uploadAttachments(sessionId, files);

      set((state) => ({
        pendingAttachments: [
          ...state.pendingAttachments,
          ...response.attachments,
        ],
        attachmentsUploading: false,
      }));

      if (response.failed.length > 0) {
        set({
          error: response.failed
            .map((item) => `${item.name}: ${item.error}`)
            .join("; "),
        });
      }
    } catch (error) {
      set({
        attachmentsUploading: false,
        error: error instanceof Error ? error.message : "Upload failed.",
      });

      throw error;
    }
  },

  removePendingAttachment: async (id) => {
    set((state) => ({
      pendingAttachments: state.pendingAttachments.filter(
        (item) => item.id !== id,
      ),
    }));

    try {
      await api.deleteAttachment(id);
    } catch {
      // The staged file will be cleaned with the store; removing it from
      // the composer is the user-visible contract and must not fail.
    }
  },

  sendFeedback: async (query, liked) => {
    const sessionId = get().activeSessionId;

    if (!sessionId || !query.trim()) {
      return;
    }

    await api.feedback({
      sessionId,
      query,
      liked,
    });
  },

  abort: async () => {
    const sessionId = get().activeSessionId;

    if (!sessionId) {
      set({
        running: false,
        runPhase: "aborted",
      });

      releaseIdleOwnedResources();

      return;
    }

    // v1.2.2: unlock the composer immediately; the phase stays live so
    // the generation timeline keeps showing the partial output until the
    // backend's `done`(abort) event — or a bounded fallback — finalises
    // the run with the partial preserved.
    flushStreaming();

    useRuntimeStore.setState({ running: false });

    try {
      await api.abort(sessionId);

      // v1.7.5 STOP-WHILE-PAUSED: the backend settles a parked run
      // SYNCHRONOUSLY (no generation to cancel — the checkpoint is consumed
      // and the run settles aborted inside the handler). The idle sentinel
      // that follows is deliberately ignored for a non-live phase, so the
      // client transitions here — the paused panel must not survive its
      // own stop.
      const phaseAfter = useRuntimeStore.getState().runPhase;

      if (phaseAfter === "paused") {
        transitionPhase("aborted");

        useRuntimeStore.setState({
          pausedDraft: null,
          runRevision: 0,
          running: false,
        });
      }
    } finally {
      // Fallback finalisation: if the backend's done(abort) event never
      // arrives (detached socket, engine hiccup), settle the run locally
      // after a short grace period — never a stuck live phase.
      if (runOutcome === null) {
        runOutcome = "aborted";
      }

      const phase = useRuntimeStore.getState().runPhase;

      if (isLivePhase(phase)) {
        scheduleRunFinalisation(sessionId, 1200);
      }
    }
  },

  // v1.7.4 — PAUSE: optimistic "pausing" transition from the real user
  // action, then the backend's status/snapshot events confirm the durable
  // pause. Duplicate clicks are no-ops on the backend (idempotent).
  pauseRun: async () => {
    const sessionId = get().activeSessionId;
    const runId = typeof activeRunId === "string" ? activeRunId : "";

    if (!sessionId || !runId) {
      return;
    }

    transitionPhase("pause_requested");

    try {
      await api.pauseRun(sessionId, runId);
    } catch {
      // The pause request failed — the run keeps generating; the next
      // status/delta event re-derives the live phase.
      transitionPhase("response_delta");
    }
  },

  // v1.7.4 — RESUME: continue the paused run at the revision the UI
  // observed. A stale revision is rejected server-side with the current
  // value; the next run_snapshot re-syncs the paused state.
  resumeRun: async () => {
    const sessionId = get().activeSessionId;
    const runId = typeof activeRunId === "string" ? activeRunId : "";
    const revision = get().runRevision;

    if (!sessionId || !runId) {
      return;
    }

    transitionPhase("resume_requested");

    try {
      await api.resumeRun(sessionId, runId, revision);
    } catch {
      transitionPhase("paused");
    }
  },

  // v1.7.4 — EDIT (paused only): replace the user prompt and/or the
  // partial draft. Returns true when the edit was accepted (the revision
  // advanced); stale-revision conflicts surface to the caller as false.
  editRun: async (payload: { message?: string; draft?: string }) => {
    const sessionId = get().activeSessionId;
    const runId = typeof activeRunId === "string" ? activeRunId : "";
    const revision = get().runRevision;

    if (!sessionId || !runId) {
      return false;
    }

    try {
      const response = await api.editRun({
        sessionId,
        runId,
        revision,
        message: payload.message,
        draft: payload.draft,
      });

      useRuntimeStore.setState({ runRevision: response.revision });
      return true;
    } catch {
      return false;
    }
  },

  runLabAction: async (payload) => {
    set({
      labLoading: true,
      labError: null,
    });

    try {
      const response = await api.labAction(payload);

      if (!response.ok) {
        throw new Error(response.error || "Coding Lab action failed.");
      }

      await get().refreshLab();

      const activeTaskId = get().activeLabTaskId;

      if (activeTaskId) {
        await get().loadLabTask(activeTaskId);
      }

      return response.result;
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Coding Lab action failed.";

      set({
        labLoading: false,
        labError: message,
      });

      // v1.1.4: no rethrow — the labError state IS the user-facing
      // failure surface. The previous `throw` escaped every fire-and-forget
      // call site (LabPanel's `void onAction(...)`) as an unhandled
      // promise rejection.
    }
  },

  connectActivity: () => {
    const sessionId = get().activeSessionId;

    if (!sessionId) {
      return;
    }

    if (
      socket &&
      activitySessionId === sessionId &&
      (socket.readyState === WebSocket.OPEN ||
        socket.readyState === WebSocket.CONNECTING)
    ) {
      return;
    }

    resetPendingActivity();

    if (socket) {
      socket.close();
      socket = null;
    }

    clearReconnectTimer();
    reconnectAttempts = 0;

    activitySessionId = sessionId;

    set({
      connection: "connecting",
    });

    // v1.2.6 continuation: arm the deterministic attach acknowledgement —
    // resolved by the server's first `attached` frame (see ws.onmessage).
    beginActivityAttach();

    // v1.9.4 (P0 — the desktop live-answer transport): resolve the
    // activity socket origin ONCE (desktop loopback vs same-origin) and
    // then open the socket. The bookkeeping above ran synchronously, so
    // concurrent callers dedupe on activitySessionId/socket state; the
    // attach deferred below bounds any discovery delay (one same-origin
    // HTTP GET, typically sub-millisecond on the desktop).
    void ensureActivityWsOrigin().then(() => {
      // A newer connect attempt (or a disconnect) superseded this one.
      if (socket !== null || activitySessionId !== sessionId) {
        return;
      }

      openActivitySocket(sessionId);
    });
  },

  disconnectActivity: () => {
    activitySessionId = null;

    // v1.2.6 continuation: dropping the socket must also release anyone
    // waiting on its attach acknowledgement (ws.onclose fires only for a
    // live transport; an already-null socket leaves the deferred armed).
    failActivityAttach();

    resetPendingActivity();

    clearReconnectTimer();
    reconnectAttempts = 0;

    if (socket) {
      socket.close();
      socket = null;
    }

    set({
      connection: "idle",
    });
  },

  clearActivity: () => {
    resetPendingActivity();

    set({
      activity: [],
    });
  },

  // v1.2.8: MODE-SEPARATED history switching. The old setMode flipped a
  // display flag over ONE shared transcript; two spaces that share one
  // active conversation are NOT two histories. Now:
  //   - the current mode's active session is remembered in
  //     activeSessionByMode (never lost, persisted across reloads);
  //   - v1.2.8.1: the OTHER mode's remembered session IS the active one
  //     (resolved by setMode from the per-mode memory — never from the
  //     stale session list of the previous mode), and its transcript is
  //     loaded immediately; a fresh session is created lazily on the
  //     first message when the space has none;
  //   - the run timeline resets for the newly active session: the backend
  //     run of the previous session (if any) KEEPS RUNNING server-side and
  //     reconnecting to it later replays its authoritative snapshot —
  //     switching the visible space never restarts or cancels a run.
  mode: bootMode,
  activeSessionId: bootSessions[bootMode],
  activeSessionByMode: bootSessions,
  historyRefs: [],
  historyPickerOpen: false,
  historyHits: [],
  historySearchLoading: false,
  // v1.2.8.1: the cross-mode picker defaults to the OTHER space — the
  // whole point of the picker is attaching history from the other mode;
  // the backend now rejects same-mode references outright.
  historySearchMode: crossModePickerFilter(bootMode),
  agentTask: null,
  olderHasMore: false,
  olderLoading: false,
  olderNextBefore: null,
  setMode: (mode) => {
    const state = get();

    if (state.mode === mode) {
      persistWorkspaceMode(mode);
      return;
    }

    // Remember where the CURRENT space was (so returning restores it).
    const remembered: Record<WorkspaceMode, string | null> = {
      ...state.activeSessionByMode,
      [state.mode]: state.activeSessionId,
    };

    // v1.7.5: a mode switch poisons every in-flight list GET of the OLD
    // mode — its response is stale the moment the space changes.
    sessionListGuard.invalidate();

    // v1.8.4 (P0-D): a mode switch also poisons every in-flight context
    // response of the old space (the cleared sessionContext below must
    // never be re-populated by a response that predates the switch).
    invalidateSessionContext();

    // A run live in the current session keeps running on the backend;
    // this UI switch only changes the visible space.
    get().disconnectActivity();
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetRunReplayTracking(null);
    resetPendingStreaming();

    persistWorkspaceMode(mode);

    // v1.2.8.1 REPAIR (the v1.2.8 empty-conversation regression): the
    // target mode's active session is the PER-MODE MEMORY, not a filter
    // over the current `sessions` array. Since v1.2.8 the list is always
    // single-mode (the CURRENT mode's), so the old filter produced an
    // empty subset for the TARGET mode and nextActive was ALWAYS null —
    // no transcript load, no activity socket, no context policy. The
    // remembered id is validated against the (refreshed) list
    // asynchronously; refreshSessions re-resolves and loads if the
    // remembered session no longer exists.
    const nextActive = resolveModeSwitchTarget(remembered, mode);

    set({
      mode,
      activeSessionId: nextActive,
      activeSessionByMode: remembered,
      error: null,
      activity: [],
      messages: [],
      // v1.4.0: a mode switch changes the conversation space — the
      // transcript is unknown until the (mode-local) session's history
      // is fetched.
      historyStatus: nextActive ? "loading" : "ready",
      historyError: null,
      streaming: null,
      running: false,
      runPhase: "idle",
      runStartedAt: null,
      runNote: null,
      liveStatus: null,
      tierEscalations: [],
      pendingAttachments: [],
      historyRefs: [],
      agentTask: null,
      olderHasMore: false,
      olderNextBefore: null,
      sessionContext: null,
      sessionContextError: null,
      // v1.2.8.1: the picker retargets the new OTHER space.
      historySearchMode: crossModePickerFilter(mode),
    });

    // Load the target space's list (it also re-validates the selection).
    void get().refreshSessions();

    if (nextActive) {
      // v1.2.8.1: reconnect the run transport for the RETURNED session —
      // a live run in this space replays its authoritative snapshot here
      // (the backend run was never cancelled by the mode switch).
      if (activityConsumers > 0) {
        get().connectActivity();
      }

      void get().loadSession(nextActive);
      void get().refreshSessionContext();
    }
  },

  attachHistoryRefs: async (refs) => {
    const state = get();

    const sessionId = state.activeSessionId;
    if (!sessionId) {
      return;
    }

    const merged = mergeHistoryRefs(state.historyRefs, refs, sessionId);

    set({ historyRefs: merged, historyPickerOpen: false });

    // Persist on the session context (mirrors the staged attachment ids):
    // the references survive reloads and travel with later turns.
    // v1.2.8.1: the delta goes through the dedicated `historyRefs` PUT
    // field, applied ATOMICALLY server-side under the store lock — the
    // previous GET-whole-context-modify-PUT could silently revert a
    // concurrent context update (token policy, attachment association).
    try {
      await api.updateSession(sessionId, { historyRefs: merged });
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to persist the attached history references.",
      });
    }
  },

  detachHistoryRef: async (refSessionId) => {
    const state = get();

    const sessionId = state.activeSessionId;
    if (!sessionId) {
      return;
    }

    const next = removeHistoryRefById(state.historyRefs, refSessionId);

    set({ historyRefs: next });

    // v1.2.8.1: atomic `historyRefs` delta (see attachHistoryRefs).
    try {
      await api.updateSession(sessionId, { historyRefs: next });
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to update the attached history references.",
      });
    }
  },

  setHistoryPickerOpen: (open) => {
    set({ historyPickerOpen: open });

    if (open) {
      // Seed the picker with the newest sessions across BOTH spaces.
      void get().searchHistory("");
    }
  },

  setHistorySearchMode: (filter) => {
    set({ historySearchMode: filter });

    void get().searchHistory("");
  },

  searchHistory: async (q) => {
    const mode = get().historySearchMode;

    set({ historySearchLoading: true });

    try {
      const hits = await api.historySearch(
        q,
        mode === "all" ? undefined : mode,
        12,
      );

      set({ historyHits: hits, historySearchLoading: false });
    } catch (error) {
      set({
        historySearchLoading: false,
        historyHits: [],
        error:
          error instanceof Error
            ? error.message
            : "Failed to search history.",
      });
    }
  },

  loadOlderMessages: async () => {
    const state = get();

    const sessionId = state.activeSessionId;

    if (!sessionId || !state.olderHasMore || state.olderLoading) {
      return;
    }

    const before = state.olderNextBefore;
    if (before === null || before <= 0) {
      set({ olderHasMore: false, olderNextBefore: null });
      return;
    }

    set({ olderLoading: true });

    try {
      const page = await api.sessionMessagesPage(sessionId, before);

      // Only apply if the session is still the active one (a switch must
      // never splice pages of another conversation).
      if (useRuntimeStore.getState().activeSessionId !== sessionId) {
        // v1.2.8.1: ALWAYS clear the loading flag — the previous early
        // return leaked `olderLoading: true`, which permanently disabled
        // the "Load earlier" button (also for the session the user
        // switched TO, since the flag is store-global).
        set({ olderLoading: false });
        return;
      }

      set((current) => ({
        messages: [
          ...page.messages.map((entry) => entry.message),
          ...current.messages,
        ],
        olderHasMore: page.hasMore,
        olderNextBefore: page.hasMore ? page.nextBefore : null,
        olderLoading: false,
      }));
    } catch {
      // v1.2.8.1: clear unconditionally — a failed page load for a session
      // the user already left must not wedge the NEW session's button.
      set({ olderLoading: false });
    }
  },

  renameSession: async (id, title) => {
    const clean = title.trim();

    if (!clean) {
      return;
    }

    try {
      const updated = await api.updateSession(id, { title: clean });

      // v1.7.5: the rename landed server-side — in-flight list GETs from
      // before it must not overwrite the new title.
      sessionListGuard.invalidate();

      set((state) => ({
        sessions: state.sessions.map((session) =>
          session.id === id ? { ...session, title: updated.title } : session,
        ),
      }));
    } catch (error) {
      set({
        error:
          error instanceof Error ? error.message : "Failed to rename session.",
      });
    }
  },
}));

export function getRuntimeState(): RuntimeState {
  return useRuntimeStore.getState();
}

// v1.2.8.1: the per-mode active-session map persists on every change
// (mode switches, selections, creations, deletions, re-resolutions) — one
// subscription instead of a persist call at every mutation site.
useRuntimeStore.subscribe((state, prevState) => {
  if (state.activeSessionByMode !== prevState.activeSessionByMode) {
    persistActiveSessionByMode(state.activeSessionByMode);
  }
});
