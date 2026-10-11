// context-refresh-race.test.ts — v1.8.4 (P0-D): DETERMINISTIC store-level
// coverage of the monotonic context-refresh generation.
//
// THE DEFECT: refreshSessionContext guarded only on "the returned session
// id is still the active one". Two concurrent requests for the SAME
// session could resolve out of order — the older response landing last —
// and the context UI kept displaying obsolete usage (the reported
// "context UI can temporarily display stale/old usage values"). Deletion
// also never cleared or refreshed the context surface, so a deleted
// session's usage could remain visible.
//
// The scenarios below run the REAL store (the exact production state
// machine) against a scripted HTTP transport where response ORDERING is
// forced by causality — never by sleeps:
//
//   1. same-session out-of-order responses: old A → newer B; B resolves
//      first, A resolves afterward → the UI reflects B;
//   2. the same race with the responses arriving while a NEWER refresh
//      (C) is already in flight → only C may land;
//   3. session switch: a held response of the OLD session can never
//      populate the NEW session's context;
//   4. deletion of the active session clears the visible context and
//      refreshes the replacement's;
//   5. session creation clears the visible context (never carry the
//      previous session's usage into a fresh one);
//   6. a fresh run invalidates pre-run responses (finaliseRun's
//      post-run refresh is the authority).
//
// Node --test + TS type stripping; browser globals shimmed BEFORE the
// store import (the same harness discipline as
// session-delete-regression.test.ts).

import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { register } from "node:module";

register("./extensionless-ts-resolver.mjs", import.meta.url);

// ---------------------------------------------------------------------------
// Browser-global shims (see session-delete-regression.test.ts for the full
// rationale — Node timers and MessageChannels must never keep the
// process alive).
// ---------------------------------------------------------------------------

function armDetachedTimer(fn: () => void, ms?: number): NodeJS.Timeout {
  const timer = setTimeout(fn, ms ?? 0);
  timer.unref?.();
  return timer;
}

// The store's v1.8.4 flush schedulers own MessageChannels: assigning
// port1.onmessage implicitly start()s — and RE-REFs — the port, which
// would keep this test process alive forever. Restoring browser
// semantics requires unref'ing AFTER the handler lands: the prototype's
// onmessage setter is wrapped for the lifetime of this test process only
// (the same shim discipline as session-delete-regression.test.ts).
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

// ---------------------------------------------------------------------------
// Scripted HTTP transport with CAUSAL response control for the context
// endpoint: a test can hold the next GET /sessions/{id}/context for a
// given session and deliver it at an exact point in the interleaving.
// ---------------------------------------------------------------------------

interface StoredSession {
  id: string;
  mode: string;
  title: string;
  updatedAt: number;
}

type ContextStatus = Record<string, unknown>;

function contextStatus(sessionId: string, used: number): ContextStatus {
  return {
    sessionId,
    requested: 0,
    configured: 8192,
    effective: 8192,
    usableInput: 7000,
    outputReserve: 1024,
    safetyReserve: 128,
    used,
    remaining: 8192 - used,
    pressure: used / 8192,
    classification: "safe",
  };
}

class ScriptedBackend {
  private nextId = 0;
  private inflight = 0;

  sessions = new Map<string, StoredSession>();
  journal: { method: string; path: string; status: number }[] = [];

  /** Held context responses: id → queue of deterministic gates. */
  private heldContext = new Map<
    string,
    {
      deliver: (status: ContextStatus) => void;
      promise: Promise<ContextStatus>;
    }[]
  >();

  holdNextContext(id: string): { deliver: (status: ContextStatus) => void } {
    let deliver: (status: ContextStatus) => void = () => {};
    const promise = new Promise<ContextStatus>((resolve) => {
      deliver = resolve;
    });

    const queue = this.heldContext.get(id) ?? [];
    queue.push({ deliver, promise });
    this.heldContext.set(id, queue);

    return { deliver };
  }

  reset(): void {
    this.sessions.clear();
    this.journal.length = 0;
    this.heldContext.clear();
  }

  async quiesce(): Promise<void> {
    for (let i = 0; i < 50 && this.inflight > 0; i += 1) {
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
        const body = init?.body ? JSON.parse(String(init.body)) : {};
        const mode = body.mode === "chat" ? "chat" : "agent";
        this.nextId += 1;
        const s: StoredSession = {
          id: `sctx${String(this.nextId).padStart(4, "0")}`,
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

      const contextMatch = path.match(/^\/api\/sessions\/([^/]+)\/context$/);
      if (contextMatch && method === "GET") {
        const id = decodeURIComponent(contextMatch[1]);
        const queue = this.heldContext.get(id);

        if (queue && queue.length > 0) {
          const held = queue.shift()!;

          // Deliver ONLY when the test releases the gate — causal
          // ordering, never sleeps.
          const status = await held.promise;

          this.journal.push({ method, path, status: 200 });
          return this.respond(200, status);
        }

        this.journal.push({ method, path, status: 200 });
        return this.respond(200, contextStatus(id, 0));
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

      const messagesMatch = path.match(/^\/api\/sessions\/([^/]+)\/messages$/);
      if (messagesMatch && method === "GET") {
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, {
          messages: [],
          hasMore: false,
          nextBefore: null,
        });
      }

      if (path === "/api/run" && method === "POST") {
        this.journal.push({ method, path, status: 200 });
        return this.respond(200, {
          ok: true,
          sessionId: "unknown",
          runId: "run-ctx-race",
          state: "registered",
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
// Harness (ONE shared store module instance — the production topology).
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

describe("context refresh race (deterministic, scripted transport)", () => {
  it("1. same-session out-of-order responses: newer B wins over older A", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    const created = await useRuntimeStore.getState().createSession();

    // Request A starts (held) with OLD usage…
    const gateA = backend.holdNextContext(created.id);
    const refreshA = useRuntimeStore.getState().refreshSessionContext();

    // …and is superseded by a newer request B (held)…
    const gateB = backend.holdNextContext(created.id);
    const refreshB = useRuntimeStore.getState().refreshSessionContext();

    // …which RESOLVES FIRST with the newer state.
    gateB.deliver(contextStatus(created.id, 999));
    await refreshB;
    await backend.quiesce();

    assert.equal(
      useRuntimeStore.getState().sessionContext?.used,
      999,
      "the newer response must be visible",
    );

    // A resolves LAST with the obsolete usage — it must never overwrite B.
    gateA.deliver(contextStatus(created.id, 111));
    await refreshA;
    await backend.quiesce();

    assert.equal(
      useRuntimeStore.getState().sessionContext?.used,
      999,
      "the older response must never overwrite the newer state",
    );
  });

  it("2. a response superseded by an even newer in-flight refresh never lands", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    const created = await useRuntimeStore.getState().createSession();

    const gateA = backend.holdNextContext(created.id);
    const refreshA = useRuntimeStore.getState().refreshSessionContext();
    const gateB = backend.holdNextContext(created.id);
    const refreshB = useRuntimeStore.getState().refreshSessionContext();
    const gateC = backend.holdNextContext(created.id);
    const refreshC = useRuntimeStore.getState().refreshSessionContext();

    // C resolves first, then A (stale) — A must not land while C's newer
    // generation is in flight either.
    gateC.deliver(contextStatus(created.id, 777));
    await refreshC;

    gateA.deliver(contextStatus(created.id, 111));
    await refreshA;
    await backend.quiesce();

    assert.equal(useRuntimeStore.getState().sessionContext?.used, 777);

    // B (also superseded by C) lands last — still must not overwrite C.
    gateB.deliver(contextStatus(created.id, 555));
    await refreshB;
    await backend.quiesce();

    assert.equal(useRuntimeStore.getState().sessionContext?.used, 777);
  });

  it("3. a held response of the OLD session can never populate the NEW session", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    const first = await useRuntimeStore.getState().createSession();
    const second = await useRuntimeStore.getState().createSession();

    // Old session's refresh in flight…
    const gateOld = backend.holdNextContext(first.id);
    const refreshOld = useRuntimeStore.getState().refreshSessionContext();

    // …the user switches to the other session, whose own refresh lands.
    useRuntimeStore.getState().selectSession(second.id);
    await backend.quiesce();

    assert.equal(useRuntimeStore.getState().activeSessionId, second.id);

    // The old response lands after the switch — the UI must reflect the
    // NEW session's authoritative context (its own refresh, used=0), not
    // the old session's usage.
    gateOld.deliver(contextStatus(first.id, 424242));
    await refreshOld;
    await backend.quiesce();

    const ctx = useRuntimeStore.getState().sessionContext;
    assert.ok(ctx, "the new session's context is visible");
    assert.notEqual(ctx?.sessionId, first.id);
    assert.equal(ctx?.used, 0);
  });

  it("4. deleting the active session clears the visible context", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    const created = await useRuntimeStore.getState().createSession();
    await useRuntimeStore.getState().refreshSessionContext();
    await backend.quiesce();

    assert.ok(useRuntimeStore.getState().sessionContext);

    await useRuntimeStore.getState().deleteSession(created.id);
    await backend.quiesce();

    assert.equal(useRuntimeStore.getState().activeSessionId, null);
    assert.equal(
      useRuntimeStore.getState().sessionContext,
      null,
      "a deleted session's usage must never remain visible",
    );
  });

  it("5. deleting the active session refreshes a replacement's context", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    const first = await useRuntimeStore.getState().createSession();
    const replacement = await useRuntimeStore.getState().createSession();

    // Make `first` active again (the create above switched to `second`).
    useRuntimeStore.getState().selectSession(first.id);
    await backend.quiesce();

    // The pre-delete context belongs to `first`…
    await useRuntimeStore.getState().refreshSessionContext();
    await backend.quiesce();
    assert.equal(
      useRuntimeStore.getState().sessionContext?.sessionId,
      first.id,
    );

    await useRuntimeStore.getState().deleteSession(first.id);
    await backend.quiesce();

    // …and the replacement session's OWN context must be what is visible.
    assert.equal(useRuntimeStore.getState().activeSessionId, replacement.id);

    const ctx = useRuntimeStore.getState().sessionContext;
    assert.ok(
      ctx,
      "a replacement session refreshes its context after a delete",
    );
    assert.equal(ctx?.sessionId, replacement.id);
  });

  it("6. session creation clears the previous session's usage", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    const first = await useRuntimeStore.getState().createSession();
    await useRuntimeStore.getState().refreshSessionContext();
    await backend.quiesce();
    assert.ok(useRuntimeStore.getState().sessionContext);

    await useRuntimeStore.getState().createSession();

    assert.equal(
      useRuntimeStore.getState().sessionContext,
      null,
      "a fresh session starts with unknown context, never the previous session's usage",
    );
    assert.notEqual(useRuntimeStore.getState().activeSessionId, first.id);
  });

  it("7. a fresh run invalidates pre-run context responses", async () => {
    backend.reset();
    const { useRuntimeStore } = await runtime();
    resetRuntime();

    const created = await useRuntimeStore.getState().createSession();

    // A pre-run refresh is in flight…
    const gate = backend.holdNextContext(created.id);
    const refresh = useRuntimeStore.getState().refreshSessionContext();

    // …and the user sends a message (the run invalidates it).
    const runPromise = useRuntimeStore.getState().run("hello there");

    // Wait for the run POST to be journaled (causal — the invalidation
    // happens before the POST is even issued).
    for (let i = 0; i < 100; i += 1) {
      if (
        backend.journal.some(
          (e) => e.method === "POST" && e.path === "/api/run",
        )
      ) {
        break;
      }
      await new Promise((resolve) => setTimeout(resolve, 0));
    }

    // The stale pre-run response lands mid-run — it must not overwrite.
    gate.deliver(contextStatus(created.id, 31337));
    await refresh;

    assert.equal(
      useRuntimeStore.getState().sessionContext?.used ?? 0,
      0,
      "a pre-run response must not land after the run invalidated it",
    );

    await runPromise;
    await backend.quiesce();
  });
});
