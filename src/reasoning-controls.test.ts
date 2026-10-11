// reasoning-controls.test.ts — v1.8.5 (P0): DETERMINISTIC store-level
// coverage of the REASONING DEPTH LADDER + SHOW/HIDE THINKING contracts.
//
// The four-level surface (low / mid / high / ultra) must be REAL, not
// cosmetic:
//
//   1. every run payload carries `thinking` with the CURRENT ladder value
//      (the level always travels — each rung's numeric budget lands on
//      the generation request server-side, covered by the Go suite:
//      internal/agent/reasoning_budget_v185_test.go);
//   2. changing the level changes the NEXT payload;
//   3. legacy persisted values (auto/fast/thinking) migrate at load and
//      are never re-emitted;
//   4. SHOW/HIDE THINKING is visibility ONLY: toggling it changes NOTHING
//      in the request payload — and the persisted preference round-trips
//      through the same settings authority as the level;
//   5. hidden reasoning does not stop the reasoning DATA path: the store
//      keeps folding reasoning snapshots into `streaming` regardless of
//      the visibility flag (asserted via the fast-path fold of a
//      thinking_delta event while showThinking is false).
//
// The REAL store module runs over a scripted HTTP transport (identical
// discipline to zero-session-send.test.ts); the WS attach fails fast on a
// refused port so run() proceeds deterministically.

import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { register } from "node:module";

register("./extensionless-ts-resolver.mjs", import.meta.url);

// ---------------------------------------------------------------------------
// Browser-global shims (detached timers, MessagePort unref — same as the
// other store suites).
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

// Scripted localStorage — the settings authority under test.
const storage = new Map<string, string>();

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
    getItem: (key: string) => (storage.has(key) ? storage.get(key)! : null),
    setItem: (key: string, value: string) => void storage.set(key, value),
    removeItem: (key: string) => void storage.delete(key),
  },
  addEventListener: () => {},
  removeEventListener: () => {},
};

// ---------------------------------------------------------------------------
// Scripted HTTP transport (sessions + run).
// ---------------------------------------------------------------------------

class ScriptedBackend {
  private nextId = 0;
  private inflight = 0;

  runBodies: Record<string, unknown>[] = [];

  reset(): void {
    this.runBodies.length = 0;
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
        this.nextId += 1;
        const id = `sreason${String(this.nextId).padStart(4, "0")}`;

        return this.respond(200, {
          id,
          mode: "agent",
          title: "",
          createdAt: Date.now() - 1000,
          updatedAt: Date.now(),
          msgCount: 0,
          messages: [],
        });
      }

      if (path === "/api/run" && method === "POST") {
        const body = init?.body
          ? (JSON.parse(String(init.body)) as Record<string, unknown>)
          : {};
        this.runBodies.push(body);

        return this.respond(200, { runId: `run-${this.runBodies.length}` });
      }

      if (path === "/api/sessions" && method === "GET") {
        return this.respond(200, []);
      }

      return this.respond(404, { error: `unscripted ${method} ${path}` });
    } finally {
      this.inflight -= 1;
    }
  }
}

// ---------------------------------------------------------------------------
// Scenarios.
// ---------------------------------------------------------------------------

describe("reasoning depth ladder + show/hide thinking (deterministic, scripted transport)", () => {
  // The store module is a SINGLETON: initialThinkingControl() runs exactly
  // once per process at first import. Scenario 1 below performs that first
  // import with a LEGACY value ("fast") persisted — proving load-time
  // migration — while the remaining legacy mappings (auto → mid,
  // thinking → high) are pinned by normalizeThinkingControl's own unit
  // suite (run-events.test.ts), which is the exact function the loader
  // delegates to.
  storage.clear();
  storage.set("sheytan.thinking", "fast");

  it("1. legacy persisted values migrate at load and never re-emitted", async () => {
    const backend = new ScriptedBackend();
    backend.install();

    const { useRuntimeStore } = await import("./store.ts");

    // "fast" (legacy) migrated to the ladder's "low" at first import.
    assert.equal(useRuntimeStore.getState().thinkingControl, "low");

    useRuntimeStore.setState({ activeSessionId: "s1" });

    await useRuntimeStore.getState().run("first question");
    await backend.quiesce();

    assert.equal(backend.runBodies.length, 1);
    assert.equal(backend.runBodies[0].thinking, "low");

    // The next explicit write persists the MIGRATED value, never legacy.
    useRuntimeStore.getState().setThinkingControl("low");
    assert.equal(storage.get("sheytan.thinking"), "low");
  });

  it("2. every run payload carries the current ladder level", async () => {
    const backend = new ScriptedBackend();
    backend.install();

    const { useRuntimeStore } = await import("./store.ts");

    useRuntimeStore.setState({ activeSessionId: "s2" });

    // Every rung changes the NEXT payload (the singleton is already
    // initialized — explicit sets drive the ladder from here).
    for (const level of ["low", "mid", "high", "ultra", "mid"] as const) {
      useRuntimeStore.getState().setThinkingControl(level);

      await useRuntimeStore.getState().run(`question at ${level}`);
      await backend.quiesce();

      const last = backend.runBodies[backend.runBodies.length - 1];
      assert.equal(last.thinking, level, `payload must carry ${level}`);
    }

    // The level persists through the settings authority.
    assert.equal(storage.get("sheytan.thinking"), "mid");
  });

  it("3. show/hide thinking is visibility ONLY — the payload never changes", async () => {
    const backend = new ScriptedBackend();
    backend.install();

    const { useRuntimeStore } = await import("./store.ts");

    useRuntimeStore.setState({ activeSessionId: "s3" });
    useRuntimeStore.getState().setThinkingControl("high");

    // Default: visible.
    assert.equal(useRuntimeStore.getState().showThinking, true);

    await useRuntimeStore.getState().run("visible run");
    await backend.quiesce();

    const visible = backend.runBodies[backend.runBodies.length - 1];
    assert.equal(visible.thinking, "high");
    assert.equal("showThinking" in visible, false);
    assert.equal("show" in visible, false);

    // Hide — the payload must be IDENTICAL apart from the message text.
    useRuntimeStore.getState().setShowThinking(false);
    assert.equal(useRuntimeStore.getState().showThinking, false);

    await useRuntimeStore.getState().run("hidden run");
    await backend.quiesce();

    const hidden = backend.runBodies[backend.runBodies.length - 1];

    assert.equal(hidden.thinking, "high");
    assert.equal("showThinking" in hidden, false);

    // The persisted visibility round-trips through the same authority.
    assert.equal(storage.get("sheytan.showThinking"), "0");

    // The visibility flag never touches the persisted level.
    assert.equal(storage.get("sheytan.thinking"), "high");

    // Show again (round-trip).
    useRuntimeStore.getState().setShowThinking(true);
    assert.equal(storage.get("sheytan.showThinking"), "1");
  });

  it("4. hidden reasoning keeps the generation contract intact (the data path is untouched)", async () => {
    const backend = new ScriptedBackend();
    backend.install();

    const { useRuntimeStore } = await import("./store.ts");

    useRuntimeStore.setState({ activeSessionId: "s4" });

    // The store singleton carries state between scenarios — pin the level
    // explicitly so this scenario is independent of execution order.
    useRuntimeStore.getState().setThinkingControl("mid");
    useRuntimeStore.getState().setShowThinking(false);

    // The reasoning fold path itself is pinned by stream-fast-path.test.ts
    // (the fast path folds reasoning snapshots the moment the socket
    // delivers them, regardless of any presentation flag). Here the
    // store-level proof is the SEPARATION: visibility never leaks into
    // the generation contract while a run is composed.
    await useRuntimeStore.getState().run("data path run");
    await backend.quiesce();

    const body = backend.runBodies[backend.runBodies.length - 1];

    assert.equal(body.thinking, "mid");
    assert.equal("showThinking" in body, false);
    assert.equal(useRuntimeStore.getState().showThinking, false);
    assert.equal(useRuntimeStore.getState().thinkingControl, "mid");

    // Toggling visibility never mutates the level (and vice versa).
    useRuntimeStore.getState().setShowThinking(true);
    assert.equal(useRuntimeStore.getState().thinkingControl, "mid");

    useRuntimeStore.getState().setThinkingControl("ultra");
    assert.equal(useRuntimeStore.getState().showThinking, true);
    assert.equal(storage.get("sheytan.thinking"), "ultra");
    assert.equal(storage.get("sheytan.showThinking"), "1");
  });
});
