// session-delete-regression.test.ts — v1.8.3: DETERMINISTIC store-level
// coverage of the session-delete contract (the run-107 repair's matrix).
//
// The Actions run 36713108772 failure ("delete session removes it and
// activates a remaining one" — Expected: < 4, Received: 4) was root-caused
// to a measurement race in the browser test, NOT a product defect in the
// delete path itself. That makes the PRODUCT contract doubly important to
// pin deterministically, because the browser suite can only sample the
// real interleavings its latency allows. Every scenario below runs the
// REAL store (the exact production state machine: guard, resolution,
// per-mode memory, honest error surfacing) against a scripted HTTP
// transport where response ORDERING is forced by causality — never by
// sleeps — so the interleavings that only CI latency reproduces in a
// browser are exercised here on every run:
//
//   1. create empty/pending session → delete → absent (client + wire);
//   2. create persisted session → delete → absent;
//   3. active session deletion selects a valid replacement (same mode);
//   4. delete while a GET list is in flight → the stale response lands
//      after the DELETE completed → the deleted session stays deleted;
//   5. a stale GET started before the delete can never resurrect it;
//   6. mode-separated deletion never activates the other mode's session;
//   7. a repeated delete surfaces the server's failure honestly (the
//      v1.8.3 error surface — a failed DELETE is never swallowed);
//   8. a fresh authoritative list fetch after deletion (the "reload"
//      equivalent) keeps the deleted session absent;
//   9. a session created while the STARTUP list response is in flight is
//      never dropped by it (the v1.8.3 init guard repair).
//
// Node --test + TS type stripping. The browser-only globals the modules
// touch at import time are shimmed BEFORE the dynamic store import; the
// transport (global fetch) is scripted. No network, no timers-as-logic.

import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { register } from "node:module";

// Resolve-hook registration BEFORE any application import: the store's
// extension-less relative imports (Vite/TS style) become resolvable in
// plain Node. The hook is process-local (registered here, applied to the
// dynamic imports below) — see extensionless-ts-resolver.mjs.
register("./extensionless-ts-resolver.mjs", import.meta.url);

// ---------------------------------------------------------------------------
// Browser-global shims — installed BEFORE the store import (config.ts reads
// window.location at module scope; api.request uses window.setTimeout).
// ---------------------------------------------------------------------------

// Browser timers do not keep a page alive; Node timers do. The api
// request wrapper arms an AbortController timeout per call — an unref'd
// Node timer gives the exact browser semantics (the test process exits
// the moment its work is done instead of draining every armed timeout).
function armDetachedTimer(fn: () => void, ms?: number): NodeJS.Timeout {
  const timer = setTimeout(fn, ms ?? 0);
  timer.unref?.();
  return timer;
}

// Browser MessageChannels never keep a page alive; Node's do (assigning
// port.onmessage implicitly start()s — and RE-REFs — the port, so an
// unref in a subclass constructor is undone the moment the store's
// scheduler attaches its handler). Restoring the browser semantics
// requires unref'ing AFTER the handler lands: the prototype's onmessage
// setter is wrapped for the lifetime of this test process only.
const RealMessageChannel = globalThis.MessageChannel;

if (typeof RealMessageChannel === "function") {
  const probe = new RealMessageChannel();
  const portProto = Object.getPrototypeOf(probe.port1) as
    (MessagePort & { unref?: () => void })["constructor"]["prototype"] &
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

  // The probe carries no onmessage handler, so it holds no reference.
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
  cancelAnimationFrame: (id: unknown) =>
    clearTimeout(id as NodeJS.Timeout),
  localStorage: {
    getItem: () => null,
    setItem: () => {},
    removeItem: () => {},
  },
  addEventListener: () => {},
  removeEventListener: () => {},
};

// ---------------------------------------------------------------------------
// Scripted HTTP transport.
// ---------------------------------------------------------------------------

interface StoredSession {
  id: string;
  mode: string;
  title: string;
  updatedAt: number;
  persisted: boolean;
}

interface JournalEntry {
  method: string;
  path: string;
  status: number;
}

class ScriptedBackend {
  private nextId = 0;
  private inflight = 0;
  private heldList: {
    deliver: (ids: string[]) => void;
  } | null = null;

  sessions = new Map<string, StoredSession>();
  journal: JournalEntry[] = [];
  /** ids whose DELETE must fail with the server's not-found contract. */
  failDeletes = new Set<string>();

  /** The id the NEXT create will return (deterministic — test prediction). */
  peekNextId(): string {
    return `stest${String(this.nextId + 1).padStart(4, "0")}`;
  }

  reset(): void {
    this.sessions.clear();
    this.journal = [];
    this.failDeletes.clear();
    this.heldList = null;
  }

  /** Hold the NEXT list GET in flight until deliver() — the delete-vs-GET race, forced by causality. */
  holdNextList(): { deliver: (ids: string[]) => void } {
    let deliver: (ids: string[]) => void = () => {};
    const delivered = new Promise<string[]>((resolve) => {
      deliver = (ids: string[]) => resolve(ids);
    });

    this.heldList = {
      deliver: (ids: string[]) => deliver(ids),
    };

    void delivered; // the handler below races on the promise itself
    return this.heldList;
  }

  /** Await until every scripted request has fully settled (state-based). */
  async quiesce(): Promise<void> {
    for (let i = 0; i < 50 && this.inflight > 0; i += 1) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    await new Promise((resolve) => setTimeout(resolve, 0));
  }

  private sessionJson(s: StoredSession): Record<string, unknown> {
    return {
      id: s.id,
      mode: s.mode,
      title: s.title,
      createdAt: s.updatedAt - 1000,
      updatedAt: s.updatedAt,
      msgCount: s.persisted ? 1 : 0,
      messages: [],
    };
  }

  private respond(status: number, body: unknown): Response {
    return new Response(JSON.stringify(body === undefined ? null : body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  }

  install(): void {
    (globalThis as unknown as { fetch: unknown }).fetch = (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => this.handle(input, init);
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

      // POST /api/sessions — create (pending until its first durable write).
      if (path === "/api/sessions" && method === "POST") {
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        const mode = body.mode === "chat" ? "chat" : "agent";
        this.nextId += 1;
        const s: StoredSession = {
          id: `stest${String(this.nextId).padStart(4, "0")}`,
          mode,
          title: "",
          updatedAt: Date.now() + this.nextId,
          persisted: false,
        };
        this.sessions.set(s.id, s);
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, this.sessionJson(s));
      }

      // DELETE /api/sessions/{id}
      const deleteMatch = path.match(/^\/api\/sessions\/([^/]+)$/);

      if (deleteMatch && method === "DELETE") {
        const id = decodeURIComponent(deleteMatch[1]);

        if (this.failDeletes.has(id) || !this.sessions.has(id)) {
          this.journal.push({ method, path, status: 500 });
          return this.respond(500, { error: `session ${id} not found` });
        }

        this.sessions.delete(id);
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, { ok: true });
      }

      // GET /api/sessions (mode-filtered list; optional held response)
      if (path === "/api/sessions" && method === "GET") {
        const mode = url.searchParams.get("mode");

        if (this.heldList) {
          const held = this.heldList;
          this.heldList = null;

          const ids = await new Promise<string[]>((resolve) => {
            held.deliver = (delivered: string[]) => resolve(delivered);
          });

          const list = ids
            .map((id) => this.sessions.get(id))
            .filter((s): s is StoredSession => Boolean(s))
            .map((s) => this.sessionJson(s));

          this.journal.push({ method, path, status: 200 });
          return this.respond(200, list);
        }

        const list = [...this.sessions.values()]
          .filter((s) => !mode || s.mode === mode)
          .sort((a, b) => b.updatedAt - a.updatedAt)
          .map((s) => this.sessionJson(s));

        this.journal.push({ method, path, status: 200 });
        return this.respond(200, list);
      }

      // GET /api/sessions/{id}/messages — the lazy history pager.
      const messagesMatch = path.match(/^\/api\/sessions\/([^/]+)\/messages$/);

      if (messagesMatch && method === "GET") {
        const id = decodeURIComponent(messagesMatch[1]);
        const s = this.sessions.get(id);

        if (!s) {
          this.journal.push({ method, path, status: 404 });
          return this.respond(404, { error: `session ${id} not found` });
        }

        this.journal.push({ method, path, status: 200 });
        return this.respond(200, { messages: [], hasMore: false, nextBefore: null });
      }

      // GET /api/sessions/{id} — full session detail.
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

      // GET /api/sessions/{id}/context — per-session context policy.
      const contextMatch = path.match(/^\/api\/sessions\/([^/]+)\/context$/);

      if (contextMatch && method === "GET") {
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, { policy: "auto", attachments: [] });
      }

      // Benign surfaces the init path touches in the background.
      if (path === "/api/state" && method === "GET") {
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, {
          appName: "SHEYTAN Local Agent",
          appVersion: "1.8.3",
          state: "ok",
        });
      }

      if ((path === "/api/models" || path === "/api/sysinfo") && method === "GET") {
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, {});
      }

      this.journal.push({ method, path, status: 404 });
      return this.respond(404, { error: `unscripted ${method} ${path}` });
    } finally {
      this.inflight -= 1;
    }
  }
}

// ---------------------------------------------------------------------------
// Harness. The STORE is ONE shared module instance for the whole file — the
// same instance every agent-init instance imports (the production
// topology: one store, one session-list guard). agent-init is imported
// FRESH per test (its own initialization promise + TTL state) so each
// scenario runs a real startup. Store state is reset between tests.
// ---------------------------------------------------------------------------

const backend = new ScriptedBackend();
backend.install();

let importNonce = 0;

type StoreModule = typeof import("./store.ts");
type InitModule = typeof import("./agent-init.ts");

let storeModule: StoreModule | undefined;

async function runtime(): Promise<StoreModule> {
  if (!storeModule) {
    // The store is imported ONCE — agent-init's own `./store` import
    // resolves to this same instance (no query → same URL → same module).
    storeModule = await import("./store.ts");
  }

  return storeModule;
}

async function freshInit(): Promise<InitModule["initializeAgent"]> {
  importNonce += 1;
  const init = await import(`./agent-init.ts?case=${importNonce}`);

  return init.initializeAgent;
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

const ids = (state: { sessions: Array<{ id: string }> }): string[] =>
  state.sessions.map((s) => s.id);

describe("session delete regression (deterministic, scripted transport)", () => {
  it("1. create empty/pending session → delete → absent from client and wire", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [],
      activeSessionId: null,
      activeSessionByMode: { chat: null, agent: null },
      error: null,
    });

    const created = await useRuntimeStore.getState().createSession();

    // Pending server-side (no durable write yet) but visible in the list.
    assert.equal(useRuntimeStore.getState().sessions.length, 1);
    assert.equal(useRuntimeStore.getState().activeSessionId, created.id);
    assert.ok(!backend.sessions.get(created.id)?.persisted);

    await useRuntimeStore.getState().deleteSession(created.id);

    assert.deepEqual(ids(useRuntimeStore.getState()), []);
    assert.equal(useRuntimeStore.getState().activeSessionId, null);
    assert.ok(!backend.sessions.has(created.id), "server-side state removed");
    assert.equal(useRuntimeStore.getState().error, null);

    const deleteCall = backend.journal.find(
      (e) => e.method === "DELETE" && e.path === `/api/sessions/${created.id}`,
    );
    assert.ok(deleteCall, "the DELETE reached the backend");
    assert.equal(deleteCall.status, 200);
  });

  it("2. create persisted session → delete → absent", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    const created = await useRuntimeStore.getState().createSession();

    // The first durable write persists it server-side.
    const stored = backend.sessions.get(created.id);

    assert.ok(stored);
    stored.persisted = true;
    stored.title = "persisted marker";

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [
        {
          id: created.id,
          mode: "chat",
          title: "persisted marker",
          createdAt: 0,
          updatedAt: stored.updatedAt,
          msgCount: 1,
        },
      ],
      activeSessionId: created.id,
      activeSessionByMode: { chat: created.id, agent: null },
    });

    await useRuntimeStore.getState().deleteSession(created.id);

    assert.deepEqual(ids(useRuntimeStore.getState()), []);
    assert.ok(!backend.sessions.has(created.id));
    assert.equal(useRuntimeStore.getState().activeSessionId, null);
  });

  it("3. deleting the ACTIVE session selects a valid replacement of the same mode", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    const newest = await useRuntimeStore.getState().createSession();
    const older = await useRuntimeStore.getState().createSession();
    const otherMode = await useRuntimeStore.getState().createSession();

    const olderStored = backend.sessions.get(older.id);
    const otherStored = backend.sessions.get(otherMode.id);

    assert.ok(olderStored && otherStored);

    // newest(1st, active), older(2nd, chat) — otherMode is an agent session
    // that must NEVER be selected into the chat space.
    otherStored.mode = "agent";

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [newest, older].map((s) => ({
        id: s.id,
        mode: "chat",
        createdAt: 0,
        updatedAt: backend.sessions.get(s.id)?.updatedAt ?? 0,
        msgCount: 0,
      })),
      activeSessionId: newest.id,
      activeSessionByMode: { chat: newest.id, agent: otherMode.id },
    });

    await useRuntimeStore.getState().deleteSession(newest.id);

    const state = useRuntimeStore.getState();

    assert.deepEqual(ids(state), [older.id], "the survivor remains");
    assert.equal(
      state.activeSessionId,
      older.id,
      "the newest REMAINING session of the SAME mode becomes active",
    );
    assert.equal(state.activeSessionByMode.chat, older.id);
    assert.equal(
      state.activeSessionByMode.agent,
      otherMode.id,
      "the other space's selection is untouched",
    );
    assert.equal(state.messages.length, 0);
  });

  it("4. delete while a GET list is in flight → the stale response lands after and cannot resurrect the session", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    const a = await useRuntimeStore.getState().createSession();
    const b = await useRuntimeStore.getState().createSession();

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [a, b].map((s) => ({
        id: s.id,
        mode: "chat",
        createdAt: 0,
        updatedAt: backend.sessions.get(s.id)?.updatedAt ?? 0,
        msgCount: 0,
      })),
      activeSessionId: a.id,
      activeSessionByMode: { chat: a.id, agent: null },
    });

    // A list GET leaves the client and is HELD on the wire (it snapshots
    // the PRE-delete state).
    const held = backend.holdNextList();
    const refresh = useRuntimeStore.getState().refreshSessions();

    // The DELETE completes while that GET is still in flight.
    await useRuntimeStore.getState().deleteSession(a.id);
    assert.ok(!backend.sessions.has(a.id), "server removed it");

    // The stale GET response (containing the deleted session) is
    // delivered only NOW — after the mutation completed.
    held.deliver([a.id, b.id]);
    await refresh;
    await backend.quiesce();

    const state = useRuntimeStore.getState();

    assert.ok(
      !ids(state).includes(a.id),
      "the stale response must never resurrect the deleted session",
    );
    assert.deepEqual(ids(state), [b.id]);
    assert.equal(state.activeSessionId, b.id);
  });

  it("5. a stale GET started before the delete + a fresh GET after both keep the session absent", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    const a = await useRuntimeStore.getState().createSession();
    const b = await useRuntimeStore.getState().createSession();

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [a, b].map((s) => ({
        id: s.id,
        mode: "chat",
        createdAt: 0,
        updatedAt: backend.sessions.get(s.id)?.updatedAt ?? 0,
        msgCount: 0,
      })),
      activeSessionId: a.id,
      activeSessionByMode: { chat: a.id, agent: null },
    });

    const held = backend.holdNextList();
    const staleRefresh = useRuntimeStore.getState().refreshSessions();

    await useRuntimeStore.getState().deleteSession(a.id);

    held.deliver([a.id, b.id]);
    await staleRefresh;

    // The FRESH refresh (the reload equivalent) re-fetches the
    // authoritative list — the deleted id must remain absent.
    await useRuntimeStore.getState().refreshSessions();

    const state = useRuntimeStore.getState();

    assert.ok(!ids(state).includes(a.id));
    assert.deepEqual(ids(state), [b.id]);
  });

  it("6. mode-separated deletion never activates the other mode's session", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    const chatOnly = await useRuntimeStore.getState().createSession();
    const agentOnly = await useRuntimeStore.getState().createSession();

    backend.sessions.get(agentOnly.id)!.mode = "agent";

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [
        {
          id: chatOnly.id,
          mode: "chat",
          createdAt: 0,
          updatedAt: 1,
          msgCount: 0,
        },
      ],
      activeSessionId: chatOnly.id,
      activeSessionByMode: { chat: chatOnly.id, agent: agentOnly.id },
    });

    // Deleting the LAST chat session must leave the chat space EMPTY —
    // never silently switch to the agent space's session.
    await useRuntimeStore.getState().deleteSession(chatOnly.id);

    const state = useRuntimeStore.getState();

    assert.deepEqual(ids(state), []);
    assert.equal(state.activeSessionId, null);
    assert.equal(state.activeSessionByMode.chat, null);
    assert.equal(state.activeSessionByMode.agent, agentOnly.id);
    assert.equal(state.mode, "chat", "the workspace never switched modes");

    // The agent space still selects its own session afterwards.
    await useRuntimeStore.getState().refreshSessions();
    assert.equal(useRuntimeStore.getState().activeSessionId, null);
  });

  it("7. a repeated delete surfaces the server's failure honestly (never a fake success)", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    const s = await useRuntimeStore.getState().createSession();

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [
        {
          id: s.id,
          mode: "chat",
          createdAt: 0,
          updatedAt: 1,
          msgCount: 0,
        },
      ],
      activeSessionId: s.id,
      activeSessionByMode: { chat: s.id, agent: null },
      error: null,
    });

    // First delete: the authoritative 200.
    await useRuntimeStore.getState().deleteSession(s.id);

    assert.deepEqual(ids(useRuntimeStore.getState()), []);
    assert.equal(useRuntimeStore.getState().error, null);

    // Second delete: the server's documented unknown-session failure.
    // The store must surface it (v1.8.3: never swallow a DELETE error)
    // and must NOT fake success.
    await useRuntimeStore.getState().deleteSession(s.id);

    const state = useRuntimeStore.getState();

    assert.ok(
      state.error && state.error.includes("not found"),
      `the server's failure is surfaced, got: ${state.error}`,
    );
    assert.deepEqual(ids(state), [], "no resurrection via the error path");
  });

  it("8. a fresh authoritative list fetch after deletion does not restore the session", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    const a = await useRuntimeStore.getState().createSession();
    const b = await useRuntimeStore.getState().createSession();

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [a, b].map((s) => ({
        id: s.id,
        mode: "chat",
        createdAt: 0,
        updatedAt: backend.sessions.get(s.id)?.updatedAt ?? 0,
        msgCount: 0,
      })),
      activeSessionId: a.id,
      activeSessionByMode: { chat: a.id, agent: null },
    });

    await useRuntimeStore.getState().deleteSession(a.id);

    // The "reload" equivalent: a fresh refresh from the authoritative
    // (post-delete) server state.
    await useRuntimeStore.getState().refreshSessions();

    const state = useRuntimeStore.getState();

    assert.ok(!ids(state).includes(a.id));
    assert.deepEqual(ids(state), [b.id]);
    assert.equal(state.activeSessionId, b.id);
  });

  it("9. a session created while the STARTUP list response is in flight is never dropped by it", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();
    const initializeAgent = await freshInit();

    // The server already holds one chat session (a returning install).
    const preexisting = await useRuntimeStore.getState().createSession();

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [],
      activeSessionId: null,
      activeSessionByMode: { chat: null, agent: null },
      loading: true,
    });

    // The startup GET is HELD on the wire.
    const held = backend.holdNextList();
    const init = initializeAgent();

    // The user acts while the startup response is in flight (the
    // sidebar's New session button is live during initialization).
    await useRuntimeStore.getState().createSession();

    const created = useRuntimeStore.getState().sessions[0];

    assert.ok(created, "the created session is in the client list");
    assert.equal(useRuntimeStore.getState().activeSessionId, created.id);

    // The stale startup response (a list that CANNOT contain the created
    // session) is delivered only now.
    held.deliver([preexisting.id]);
    await init;
    await backend.quiesce();

    const state = useRuntimeStore.getState();

    assert.ok(
      ids(state).includes(created.id),
      "the created session survived the stale startup response",
    );
    assert.ok(
      ids(state).includes(preexisting.id),
      "the re-resolution fetched the authoritative list",
    );
    assert.equal(state.loading, false);
    assert.equal(state.activeSessionId, created.id);
  });

  it("10. the startup path still applies its response when nothing races it", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();
    const initializeAgent = await freshInit();

    const preexisting = await useRuntimeStore.getState().createSession();

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [],
      activeSessionId: null,
      activeSessionByMode: { chat: null, agent: null },
      loading: true,
    });

    const init = initializeAgent();
    await init;
    await backend.quiesce();

    const state = useRuntimeStore.getState();

    assert.deepEqual(ids(state), [preexisting.id]);
    assert.equal(state.activeSessionId, preexisting.id);
    assert.equal(state.loading, false);
  });

  it("11. a fresh install still gets its eager initial session (v1.1.2 behavior preserved)", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();
    const initializeAgent = await freshInit();

    useRuntimeStore.setState({
      mode: "chat",
      sessions: [],
      activeSessionId: null,
      activeSessionByMode: { chat: null, agent: null },
      loading: true,
    });

    const init = initializeAgent();
    await init;
    await backend.quiesce();

    const state = useRuntimeStore.getState();

    assert.equal(state.sessions.length, 1, "the eager initial session exists");
    assert.equal(state.activeSessionId, state.sessions[0].id);
    assert.equal(state.loading, false);
  });
  it("12. the created-session prepend is idempotent (a refresh that already contains it cannot duplicate the row)", async () => {
    backend.reset();
    const useRuntimeStore = (await runtime()).useRuntimeStore;
    resetRuntime();

    // The id the NEXT create will return — known deterministically.
    const upcoming = backend.peekNextId();

    // Simulate the interleaving where a session-list refresh lands BETWEEN
    // the create POST's dispatch and its response: the store already holds
    // the created session (the backend registered it the moment the POST
    // was served) when createSession's own set runs.
    useRuntimeStore.setState({
      mode: "chat",
      sessions: [
        {
          id: upcoming,
          mode: "chat",
          title: "",
          createdAt: 0,
          updatedAt: 1,
          msgCount: 0,
        },
      ],
      activeSessionId: null,
      activeSessionByMode: { chat: null, agent: null },
    });

    await useRuntimeStore.getState().createSession();

    const state = useRuntimeStore.getState();

    assert.equal(
      state.sessions.filter((s) => s.id === upcoming).length,
      1,
      "the created session appears EXACTLY once — never a duplicate row",
    );
    assert.equal(state.sessions[0].id, upcoming, "it is the first row");
    assert.equal(state.activeSessionId, upcoming);
  });
});
