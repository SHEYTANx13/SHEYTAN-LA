import { api, type Session } from "./api";
import { useRuntimeStore, sessionListGuard } from "./store";
import {
  resolveActiveForMode,
  sessionMode,
  type WorkspaceMode,
} from "./mode-sessions";

const INITIALIZATION_TTL_MS = 30_000;

let initializationPromise: Promise<void> | null = null;

let initializedAt = 0;

/**
 * v1.2.8: resolve the active session WITHIN one conversation space. The
 * remembered selection only survives when it still exists in THAT space —
 * Chat and Agent histories are independent and never cross-select.
 */
function resolveActiveInSpace(
  sessions: Session[],
  mode: WorkspaceMode,
  currentID: string | null,
): string | null {
  const inSpace = sessions.filter((session) => sessionMode(session) === mode);

  return resolveActiveForMode(inSpace, mode, currentID);
}

async function initializeAgentOnce(): Promise<void> {
  useRuntimeStore.setState({
    loading: true,
    error: null,
  });

  try {
    const mode = useRuntimeStore.getState().mode;

    // v1.8.3: the startup session-list write goes through the SAME
    // one-authority generation guard as refreshSessions (the v1.7.5
    // repair covered the refresh consumer only — this consumer wrote its
    // response unguarded). The sidebar's "New session" button is
    // actionable while the initial GET is on the wire, and a returning
    // user with a persisted per-mode selection has the composer enabled
    // from the first paint — a mutation that lands in that window must
    // never be clobbered by the stale init response (a just-created
    // session dropping out of the sidebar, or a deleted one being
    // resurrected by a selection resolved against the stale list).
    const ticket = sessionListGuard.begin();

    const [app, fetched] = await Promise.all([
      api.state(),
      api.sessions(mode),
    ]);

    // The response may only land while its ticket is still the current
    // generation AND the workspace never left the mode it was taken in.
    let sessions = fetched;
    let superseded = !sessionListGuard.isActive(ticket);

    if (!superseded && sessions.length === 0) {
      // AAA polish (v1.1.2): a fresh install starts with zero sessions,
      // which left the runtime status on "Offline" and the composer
      // inert — the app LOOKED broken on first launch. Create the
      // initial session eagerly so the workspace is immediately live:
      // WebSocket connects, activity streams, and the composer is
      // usable from the first paint.
      //
      // v1.2.8: the eager session is created in the CURRENT conversation
      // space (mode-separated histories).
      const session = await api.createSession(mode);

      // A user action may have landed while the create was in flight —
      // the pre-create ticket is the detector for exactly that window
      // (it must be read BEFORE our own invalidate below marks the
      // ticket stale ourselves).
      superseded = !sessionListGuard.isActive(ticket);

      // The direct create (this path bypasses store.createSession) is a
      // mutation for every list GET still on the wire — same contract
      // store.createSession applies. It must poison those responses
      // even when it is the LAST thing this init does. Our own apply
      // does not go through a ticket check; it is guarded by the
      // user-action detection above instead.
      sessionListGuard.invalidate();

      sessions = [session];
    }

    if (!superseded && useRuntimeStore.getState().mode !== mode) {
      superseded = true;
    }

    if (superseded) {
      // A mutation (create/delete/rename) or a mode switch superseded
      // this response: the stale list and its selection resolution must
      // never land. Apply only the non-session state and delegate the
      // list + selection re-resolution to refreshSessions (it takes its
      // own ticket and re-validates the remembered selection).
      useRuntimeStore.setState({
        app,
        loading: false,
      });

      void useRuntimeStore.getState().refreshSessions();

      return;
    }

    const current =
      useRuntimeStore.getState().activeSessionId ??
      useRuntimeStore.getState().activeSessionByMode[mode] ??
      null;

    const activeSessionId = resolveActiveInSpace(sessions, mode, current);

    useRuntimeStore.setState({
      app,
      sessions,
      activeSessionId,
      activeSessionByMode: {
        ...useRuntimeStore.getState().activeSessionByMode,
        [mode]: activeSessionId,
      },
      loading: false,
    });

    // v1.2.8.1 REPAIR: the startup space's transcript is loaded NOW. The
    // v1.2.8 initialization resolved the active session but never fetched
    // its messages — the conversation area stayed empty until the first
    // user interaction (send / click a different session) finally called
    // loadSession.
    if (activeSessionId) {
      void useRuntimeStore.getState().loadSession(activeSessionId);
      void useRuntimeStore.getState().refreshSessionContext();
    }

    void useRuntimeStore.getState().refreshModels();

    // v1.5.0: the measured hardware snapshot feeds the Model Selector's
    // sizing hints. refreshSysinfo existed since v1.1.x but was never
    // called — the store's sysinfo stayed null and the picker's RAM
    // classification was dormant. One call at init fixes the orphan.
    void useRuntimeStore.getState().refreshSysinfo();
  } catch (error) {
    useRuntimeStore.setState({
      loading: false,
      error:
        error instanceof Error ? error.message : "Failed to initialize Agent.",
    });

    throw error;
  }
}

export function initializeAgent(): Promise<void> {
  if (initializationPromise) {
    return initializationPromise;
  }

  const now = Date.now();

  if (initializedAt > 0 && now - initializedAt < INITIALIZATION_TTL_MS) {
    return Promise.resolve();
  }

  initializationPromise = initializeAgentOnce()
    .then(() => {
      initializedAt = Date.now();
    })
    .catch((error) => {
      initializedAt = 0;
      throw error;
    })
    .finally(() => {
      initializationPromise = null;
    });

  return initializationPromise;
}
