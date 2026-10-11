// zero-session-send.test.ts — v1.8.4 (P0-C): DETERMINISTIC store-level
// coverage of the ZERO-SESSION SEND contract.
//
// THE DEFECT: run() already created a session lazily on the first
// message, but the composer's textarea and Send button were hard-disabled
// on !activeSessionId — with zero sessions the lazy creation was
// unreachable and the user was stuck ("Send/chat is blocked instead of
// creating a session").
//
// The composer gate itself is a UI concern (covered by the browser E2E);
// the scenarios below pin the STORE half of the contract with the REAL
// store module and a scripted HTTP transport:
//
//   1. zero sessions → run("…") creates a session in the CURRENT mode,
//      makes it active, and continues the SAME send (POST /api/run
//      carries the created session's id; the optimistic user bubble
//      renders; the run lifecycle starts);
//   2. deletion of the final session leaves a VALID zero-session state
//      (no phantom transcript, no phantom context, idle lifecycle);
//   3. zero sessions after deletion → run("…") works (the repaired
//      end-to-end path a real user hits);
//   4. a failed creation surfaces honestly: run() rejects with "No
//      active session." — never a silent no-op;
//   5. the created session survives a session-list refresh (the reload
//      equivalent) and stays active.

import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { register } from "node:module";

register("./extensionless-ts-resolver.mjs", import.meta.url);

// ---------------------------------------------------------------------------
// Browser-global shims — identical discipline to
// session-delete-regression.test.ts (detached timers; MessagePort
// onmessage setter wrapped to unref, because the store's v1.8.4 flush
// schedulers own MessageChannels whose ports would otherwise keep this
// process alive).
// ---------------------------------------------------------------------------

function armDetachedTimer(fn: () => void, ms?: number): NodeJS.Timeout {
  const timer = setTimeout(fn, ms ?? 0);
  timer.unref?.();
  return timer;
}

const RealMessageChannel = globalThis.MessageChannel;

if (typeof RealMessageChannel === "function") {
  const probe = new RealMessageChannel();
  const portProto = Object.getPrototypeOf(probe.port1) as (MessagePort & {
    unref?: () => void;
  })["constructor"]["prototype"] &
    object;
  const descriptor = Object.getOwnPropertyDescriptor(
    portProto as object,
    "onmessage",
  );

  if (descriptor?.set && descriptor.get) {
    Object.defineProperty(portProto, "onmessage", {
      get(this: MessagePort) {
        return descriptor.get!.call(this);
      },
      set(this: MessagePort & { unref?: () => void }, value: unknown) {
        descriptor.set!.call(this, value);
        this.unref?.();
      },
      configurable: true,
      enumerable: descriptor.enumerable,
    });
  }
}

(globalThis as unknown as { window: unknown }).window = {
  location: {
    origin: "http://127.0.0.1:1",
    protocol: "http:",
    host: "127.0.0.1:1",
    hash: "",
  },
  setTimeout: (fn: () => void, ms?: number) => armDetachedTimer(fn, ms),
  clearTimeout: (id: unknown) => clearTimeout(id as NodeJS.Timeout),
  requestAnimationFrame: (fn: () => void) =>
    armDetachedTimer(fn, 0) as unknown as number,
  cancelAnimationFrame: (id: unknown) => clearTimeout(id as NodeJS.Timeout),
  localStorage: {
    getItem: () => null,
    setItem: () => {},
    removeItem: () => {},
  },
  addEventListener: () => {},
  removeEventListener: () => {},
};

// Node's global WebSocket is exercised for real here: connectActivity
// targets ws://127.0.0.1:1/... which refuses immediately, releasing the
// attach contract deterministically (run() proceeds past it by design —
// the runId replay + grace re-check own the resync).

// ---------------------------------------------------------------------------
// Scripted HTTP transport (sessions + run + context).
// ---------------------------------------------------------------------------

interface StoredSession {
  id: string;
  mode: string;
  title: string;
  updatedAt: number;
}

class ScriptedBackend {
  private nextId = 0;
  private inflight = 0;

  sessions = new Map<string, StoredSession>();
  journal: { method: string; path: string; status: number }[] = [];
  runBodies: Record<string, unknown>[] = [];
  failSessionCreates = false;
  // v1.9.0: deterministic observability for the zero-session reload fix —
  // the create POST can be delayed (so the test can sample the store while
  // the lazy create is provably in flight) and observed at dispatch time.
  sessionCreateDelayMs = 0;
  onSessionCreate: (() => void) | null = null;

  reset(): void {
    this.sessions.clear();
    this.journal.length = 0;
    this.runBodies.length = 0;
    this.failSessionCreates = false;
    this.sessionCreateDelayMs = 0;
    this.onSessionCreate = null;
  }

  async quiesce(): Promise<void> {
    const deadline = Date.now() + 5_000;

    while (this.inflight > 0 && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    await new Promise((resolve) => setTimeout(resolve, 0));
  }

  install(): void {
    (globalThis as unknown as { fetch: unknown }).fetch = (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => this.handle(input, init);
  }

  private respond(status: number, body: unknown): Response {
    return new Response(JSON.stringify(body === undefined ? null : body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  }

  private sessionJson(s: StoredSession): Record<string, unknown> {
    return {
      id: s.id,
      mode: s.mode,
      title: s.title,
      createdAt: s.updatedAt - 1000,
      updatedAt: s.updatedAt,
      msgCount: 0,
      messages: [],
    };
  }

  private async handle(
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> {
    this.inflight += 1;

    try {
      const rawUrl = typeof input === "string" ? input : String(input);
      const url = new URL(rawUrl, "http://127.0.0.1:1");
      const method = (
        init?.method ?? (input instanceof Request ? input.method : "GET")
      ).toUpperCase();
      const path = url.pathname;

      if (path === "/api/sessions" && method === "POST") {
        if (this.failSessionCreates) {
          this.journal.push({ method, path, status: 500 });
          return this.respond(500, { error: "backend refuses session create" });
        }

        this.onSessionCreate?.();

        if (this.sessionCreateDelayMs > 0) {
          await new Promise((resolve) =>
            setTimeout(resolve, this.sessionCreateDelayMs),
          );
        }

        const body = init?.body ? JSON.parse(String(init.body)) : {};
        const mode = body.mode === "chat" ? "chat" : "agent";
        this.nextId += 1;
        const s: StoredSession = {
          id: `szero${String(this.nextId).padStart(4, "0")}`,
          mode,
          title: "",
          updatedAt: Date.now() + this.nextId,
        };
        this.sessions.set(s.id, s);
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, this.sessionJson(s));
      }

      const deleteMatch = path.match(/^\/api\/sessions\/([^/]+)$/);
      if (deleteMatch && method === "DELETE") {
        const id = decodeURIComponent(deleteMatch[1]);
        if (!this.sessions.has(id)) {
          this.journal.push({ method, path, status: 500 });
          return this.respond(500, { error: `session ${id} not found` });
        }
        this.sessions.delete(id);
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, { ok: true });
      }

      if (path === "/api/sessions" && method === "GET") {
        const mode = url.searchParams.get("mode");
        const list = [...this.sessions.values()]
          .filter((s) => !mode || s.mode === mode)
          .sort((a, b) => b.updatedAt - a.updatedAt)
          .map((s) => this.sessionJson(s));
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, list);
      }

      if (path === "/api/run" && method === "POST") {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        this.runBodies.push(body);
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, {
          ok: true,
          sessionId: String(body.sessionId ?? ""),
          runId: `run-${this.runBodies.length}`,
          state: "registered",
        });
      }

      const contextMatch = path.match(/^\/api\/sessions\/([^/]+)\/context$/);
      if (contextMatch && method === "GET") {
        const id = decodeURIComponent(contextMatch[1]);
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, {
          sessionId: id,
          requested: 0,
          configured: 8192,
          effective: 8192,
          usableInput: 7000,
          outputReserve: 1024,
          safetyReserve: 128,
          used: 0,
          remaining: 8192,
          pressure: 0,
          classification: "safe",
        });
      }

      const messagesMatch = path.match(/^\/api\/sessions\/([^/]+)\/messages$/);
      if (messagesMatch && method === "GET") {
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, {
          messages: [],
          hasMore: false,
          nextBefore: null,
        });
      }

      const detailMatch = path.match(/^\/api\/sessions\/([^/]+)$/);
      if (detailMatch && method === "GET") {
        const id = decodeURIComponent(detailMatch[1]);
        const s = this.sessions.get(id);
        if (!s) {
          this.journal.push({ method, path, status: 404 });
          return this.respond(404, { error: `session ${id} not found` });
        }
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, {
          ...this.sessionJson(s),
          historyRefs: [],
          context: null,
        });
      }

      this.journal.push({ method, path, status: 404 });
      return this.respond(404, { error: `unscripted ${method} ${path}` });
    } finally {
      this.inflight -= 1;
    }
  }
}

// ---------------------------------------------------------------------------
// Harness.
// ---------------------------------------------------------------------------

const backend = new ScriptedBackend();
backend.install();

let storeModule: typeof import("./store.ts") | undefined;

async function runtime(): Promise<typeof import("./store.ts")> {
  if (!storeModule) {
    storeModule = await import("./store.ts");
  }
  return storeModule;
}

function resetRuntime(): void {
  storeModule?.useRuntimeStore.setState({
    mode: "chat",
    sessions: [],
    activeSessionId: null,
    activeSessionByMode: { chat: null, agent: null },
    connection: "idle",
    loading: false,
    error: null,
    activity: [],
    running: false,
    runPhase: "idle",
    runStartedAt: null,
    runNote: null,
    liveStatus: null,
    tierEscalations: [],
    memoryEvidence: null,
    sessionContext: null,
    sessionContextError: null,
    messages: [],
    streaming: null,
    historyStatus: "ready",
    historyError: null,
    pendingAttachments: [],
    historyRefs: [],
    olderHasMore: false,
    olderNextBefore: null,
  });
}

describe("zero-session send (deterministic, scripted transport)", () => {
  it("1. zero sessions → Send creates + activates a session and continues the run", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    // The exact post-delete state: no sessions, nothing active.
    assert.equal(useRuntimeStore.getState().sessions.length, 0);
    assert.equal(useRuntimeStore.getState().activeSessionId, null);

    await useRuntimeStore.getState().run("hello from a blank space");
    await backend.quiesce();

    // A session was created server-side in the CURRENT mode…
    const created = [...backend.sessions.values()];
    assert.equal(created.length, 1, "exactly one session created");
    assert.equal(created[0].mode, "chat", "created in the current mode");

    // …it is active client-side…
    const state = useRuntimeStore.getState();
    assert.equal(state.activeSessionId, created[0].id);
    assert.equal(state.activeSessionByMode.chat, created[0].id);

    // …and the SAME send continued: POST /api/run carries the created id.
    assert.equal(backend.runBodies.length, 1);
    assert.equal(backend.runBodies[0].sessionId, created[0].id);
    assert.equal(backend.runBodies[0].message, "hello from a blank space");

    // The optimistic user bubble is visible, and the run lifecycle opened.
    assert.equal(state.messages.length, 1);
    assert.equal(state.messages[0].role, "user");
    assert.equal(state.messages[0].content, "hello from a blank space");
    assert.equal(state.runPhase, "preparing");
    assert.equal(state.runStartedAt !== null, true);
  });

  it("2. deleting the final session leaves a VALID zero-session state", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    const created = await useRuntimeStore.getState().createSession();
    await useRuntimeStore.getState().deleteSession(created.id);
    await backend.quiesce();

    const state = useRuntimeStore.getState();
    assert.deepEqual(state.sessions, []);
    assert.equal(state.activeSessionId, null);
    assert.equal(state.activeSessionByMode.chat, null);
    assert.deepEqual(state.messages, []);
    assert.equal(state.sessionContext, null);
    assert.equal(state.running, false);
    assert.equal(state.runPhase, "idle");
    assert.ok(!backend.sessions.has(created.id));
  });

  it("3. zero sessions after deletion → Send works (the reported user path)", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    // Build the exact user journey: one session exists, the user deletes
    // it, the space is empty, then the user sends a message.
    const first = await useRuntimeStore.getState().createSession();
    await useRuntimeStore.getState().deleteSession(first.id);
    await backend.quiesce();
    assert.equal(useRuntimeStore.getState().activeSessionId, null);

    await useRuntimeStore.getState().run("and now send again");
    await backend.quiesce();

    const created = [...backend.sessions.values()];
    assert.equal(created.length, 1, "a NEW session was created");
    assert.notEqual(created[0].id, first.id);
    assert.equal(useRuntimeStore.getState().activeSessionId, created[0].id);
    assert.equal(backend.runBodies.length, 1);
    assert.equal(backend.runBodies[0].sessionId, created[0].id);
  });

  it("4. a failed creation surfaces honestly (run rejects, no silent no-op)", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    backend.failSessionCreates = true;

    await assert.rejects(
      () => useRuntimeStore.getState().run("nobody will hear this"),
      /No active session\./,
    );

    assert.equal(backend.runBodies.length, 0, "no run POST without a session");

    // v1.9.0: the failed lazy create is a FAILED run start — the run
    // startup state set before the create must be cleaned up, otherwise
    // the composer stays locked with no run to settle it.
    const state = useRuntimeStore.getState();
    assert.equal(state.running, false, "composer unlocked after the failure");
    assert.equal(state.runPhase, "idle");
    assert.equal(state.runStartedAt, null);
  });

  it("5. the run-created session survives a session-list refresh", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    await useRuntimeStore.getState().run("persist me");
    await backend.quiesce();

    const activeAfterRun = useRuntimeStore.getState().activeSessionId;
    assert.ok(activeAfterRun);

    // The reload equivalent: the authoritative list is re-fetched; the
    // run-created session is present and stays active.
    await useRuntimeStore.getState().refreshSessions();
    await backend.quiesce();

    const state = useRuntimeStore.getState();
    assert.ok(
      state.sessions.some((s) => s.id === activeAfterRun),
      "the created session appears in the refreshed list",
    );
    assert.equal(state.activeSessionId, activeAfterRun);
  });

  // ---------------------------------------------------------------------
  // v1.9.0 (zero-session reload P0) — the deterministic regression pair
  // for the Linux E2E defect: the lazy session create ran while the store
  // still read idle, so an enabled-composer observation could legally
  // sample INSIDE run()'s startup window and reload before POST /api/run
  // was dispatched (session existed, transcript stayed empty forever).
  // ---------------------------------------------------------------------

  it("6. v1.9.0: running:true is visible SYNCHRONOUSLY, before the lazy create is dispatched", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    // Hold the create POST in flight — the assertion below runs while it
    // is provably mid-startup.
    backend.sessionCreateDelayMs = 40;

    let runningAtCreateDispatch: boolean | null = null;
    backend.onSessionCreate = () => {
      runningAtCreateDispatch = useRuntimeStore.getState().running;
    };

    const runPromise = useRuntimeStore.getState().run("lock before create");

    // SYNCHRONOUSLY after run() is invoked — before any of its awaits
    // settle — the composer state must already read running. The v1.8.8
    // code set this only AFTER the create resolved, which is exactly the
    // window the Linux E2E reload probe sampled.
    const stateNow = useRuntimeStore.getState();
    assert.equal(
      stateNow.running,
      true,
      "run() must lock the composer synchronously",
    );
    assert.equal(stateNow.runPhase, "preparing");

    await runPromise;
    await backend.quiesce();

    // The state survived the ENTIRE lazy create (createSession no longer
    // clobbers it mid-startup).
    assert.equal(
      runningAtCreateDispatch,
      true,
      "running must still be true when the create POST is dispatched",
    );
    assert.equal(backend.runBodies.length, 1, "the run continued");
    assert.equal(useRuntimeStore.getState().running, true);
  });

  it("7. v1.9.0: the created session carries the run state forward (keepRunState contract)", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    await useRuntimeStore.getState().run("keep the run alive");
    await backend.quiesce();

    const state = useRuntimeStore.getState();
    assert.equal(backend.runBodies.length, 1);
    assert.equal(state.running, true, "run state survived the lazy create");
    assert.equal(state.runPhase, "preparing");
    assert.equal(state.runStartedAt !== null, true);
    assert.equal(state.activeSessionId !== null, true);
    assert.equal(state.messages.length, 1);
    assert.equal(state.messages[0].role, "user");

    // The plain (non-run) create path keeps its historical semantics: a
    // fresh session resets the lifecycle to idle.
    await useRuntimeStore.getState().createSession();
    await backend.quiesce();

    const fresh = useRuntimeStore.getState();
    assert.equal(fresh.running, false, "plain create still resets to idle");
    assert.equal(fresh.runPhase, "idle");
    assert.equal(fresh.runStartedAt, null);
  });
});
