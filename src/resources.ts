// resources.ts — v1.2.6 shared frontend resource loader.
//
// ONE source of truth for the slow background resources Settings, System,
// Agent and Performance all need (models, presets, tools, sysinfo,
// environment, health). The v1.2.5 Settings panel fetched everything with
// ONE global Promise.all — the whole page waited for the slowest endpoint
// (the Windows hardware probe measured 5.4 s), and one failed endpoint
// failed the entire view.
//
// Contract per resource (independently):
//   state:   "unavailable" → never fetched yet
//            "loading"     → a fetch is in flight
//            "ready"       → fresh data (within TTL)
//            "stale"       → showing last-known-good while a refresh runs
//            "error"       → the last fetch failed (last-known-good stays)
//
// Guarantees:
//   - request deduplication (one in-flight fetch per key, every caller
//     awaits the SAME promise);
//   - TTL with stale-while-revalidate (fresh value served instantly,
//     background refresh when older than TTL);
//   - last-known-good retention — a failed refresh NEVER blanks a view
//     that already has data;
//   - AbortController — navigating away cancels the in-flight fetch;
//   - subscribe() — components observe one resource without re-fetching
//     it per mount (the cache is shared app-wide).
//
// Pure TypeScript, no store dependency — unit-testable without a browser.

export type ResourceState =
  | "unavailable"
  | "loading"
  | "ready"
  | "stale"
  | "error";

export type ResourceSnapshot<T> = {
  state: ResourceState;
  data: T | null;
  error: string | null;
  fetchedAt: number | null;
};

type Entry<T> = {
  data: T | null;
  fetchedAt: number | null;
  error: string | null;
  inFlight: Promise<T> | null;
  listeners: Set<(snapshot: ResourceSnapshot<T>) => void>;

  // v1.2.6 continuation: SHARED REQUEST OWNERSHIP.
  // consumers counts live interests in the in-flight request: every
  // subscriber (mounted component) and every ensure() caller whose promise
  // has not settled yet. An unmount decrements — it never cancels a
  // request another consumer still needs. When the count reaches zero
  // with a request in flight, a BOUNDED grace timer arms; if nobody
  // re-subscribes before it fires, the request is aborted (unused
  // requests are eventually cancellable; nothing leaks forever).
  consumers: number;
  controller: AbortController | null;
  cancelTimer: number | null;
};

// UNUSED_REQUEST_GRACE_MS is how long an in-flight fetch with ZERO
// consumers survives before it is aborted. Long enough that a React
// re-mount (strict-mode double-mount, tab switch) re-claims it; short
// enough that an abandoned request cannot run to its full timeout.
let unusedRequestGraceMs = 10_000;

/**
 * setUnusedRequestGraceForTest overrides the abort grace window (unit
 * tests shrink it so the eventual-cancellation contract is observable
 * without waiting the production 10 s).
 */
export function setUnusedRequestGraceForTest(ms: number): void {
  unusedRequestGraceMs = ms;
}

// Default TTLs per resource class (ms). Tuned for local-loopback latency:
// these values trade freshness for near-zero request volume.
const DEFAULT_TTL_MS = 30_000;
const TTL_BY_KEY: Record<string, number> = {
  config: 15_000,
  models: 60_000,
  presets: 120_000,
  tools: 120_000,
  sysinfo: 120_000,
  environment: 60_000,
  health: 60_000,
  engine: 10_000,
};

function ttlFor(key: string): number {
  return TTL_BY_KEY[key] ?? DEFAULT_TTL_MS;
}

const entries = new Map<string, Entry<unknown>>();

function entry<T>(key: string): Entry<T> {
  let existing = entries.get(key) as Entry<T> | undefined;

  if (!existing) {
    existing = {
      data: null,
      fetchedAt: null,
      error: null,
      inFlight: null,
      listeners: new Set(),
      consumers: 0,
      controller: null,
      cancelTimer: null,
    };

    entries.set(key, existing as Entry<unknown>);
  }

  return existing;
}

// acquireConsumer registers one live interest in the entry's request and
// disarms any pending abort (someone needs the request again).
function acquireConsumer(key: string): void {
  const e = entry<unknown>(key);

  e.consumers += 1;

  if (e.cancelTimer !== null) {
    clearTimeout(e.cancelTimer);
    e.cancelTimer = null;
  }
}

// releaseConsumer drops one live interest. At ZERO consumers with a
// request still in flight, the bounded abort timer arms — the request is
// eventually cancellable, but a re-mount within the grace window re-uses
// it instead of duplicating the fetch.
function releaseConsumer(key: string): void {
  const e = entries.get(key) as Entry<unknown> | undefined;

  if (!e || e.consumers <= 0) {
    return;
  }

  e.consumers -= 1;

  if (e.consumers === 0 && e.inFlight && e.cancelTimer === null) {
    const controller = e.controller;

    e.cancelTimer = setTimeout(() => {
      e.cancelTimer = null;

      if (e.consumers === 0 && e.inFlight && controller) {
        // Abort the FETCHER; the promise's own settle path keeps the
        // last-known-good data and records the abort as the entry error
        // only when nothing was ever fetched.
        controller.abort();
      }
    }, unusedRequestGraceMs) as unknown as number;
  }
}

function snapshot<T>(key: string): ResourceSnapshot<T> {
  const e = entry<T>(key);

  if (e.data === null && e.fetchedAt === null) {
    if (e.inFlight) {
      return { state: "loading", data: null, error: null, fetchedAt: null };
    }

    return {
      state: e.error ? "error" : "unavailable",
      data: null,
      error: e.error,
      fetchedAt: null,
    };
  }

  if (e.inFlight) {
    return { state: "stale", data: e.data, error: null, fetchedAt: e.fetchedAt };
  }

  if (e.error && e.data !== null) {
    return { state: "stale", data: e.data, error: null, fetchedAt: e.fetchedAt };
  }

  if (e.error) {
    return { state: "error", data: null, error: e.error, fetchedAt: null };
  }

  return { state: "ready", data: e.data, error: null, fetchedAt: e.fetchedAt };
}

function notify<T>(key: string): void {
  const e = entry<T>(key);
  const snap = snapshot<T>(key);

  for (const listener of e.listeners) {
    try {
      listener(snap);
    } catch {
      // A broken listener must never break the resource layer.
    }
  }
}

/**
 * ensure starts (or joins) the fetch for one resource and resolves with
 * the freshest data available after it settles.
 *
 * - no cached data → always fetch (deduplicated);
 * - cached data still inside its TTL → resolve cached, NO network;
 * - cached data older than TTL → resolve cached immediately (stale) and
 *   refresh in the background (stale-while-revalidate).
 */
export function ensure<T>(
  key: string,
  fetcher: (signal: AbortSignal) => Promise<T>,
  options?: { force?: boolean },
): Promise<T | null> {
  const e = entry<T>(key);

  if (e.inFlight) {
    return e.inFlight.then(
      () => e.data,
      () => e.data,
    );
  }

  const fresh =
    e.fetchedAt !== null && Date.now() - e.fetchedAt < ttlFor(key);

  if (e.data !== null && fresh && !options?.force) {
    return Promise.resolve(e.data);
  }

  const controller = new AbortController();

  e.controller = controller;

  // NOTE (ownership model): ensure() does NOT count as a consumer. Every
  // production caller (useResource, refresh) fire-and-forgets the promise
  // — the DISPLAYED interest is the subscriber count (mounted
  // components). A request with zero subscribers and no settlement is
  // unused and eventually abortable; the natural settlement path below
  // clears the state for everyone.

  const promise: Promise<T> = new Promise<T>((resolve, reject) => {
    fetcher(controller.signal).then(resolve, reject);
  }).then(
    (data) => {
      e.data = data;
      e.fetchedAt = Date.now();
      e.error = null;
      e.inFlight = null;
      e.controller = null;
      notify(key);
      return data;
    },
    (error: unknown) => {
      // A failed refresh keeps the last-known-good data (stale), or
      // records the error when nothing was ever fetched.
      e.error =
        error instanceof Error ? error.message : "Resource unavailable.";
      e.inFlight = null;
      e.controller = null;
      notify(key);

      if (e.data !== null) {
        return e.data;
      }

      throw error;
    },
  );

  e.inFlight = promise;
  notify(key);

  return promise.then(
    (data) => data,
    () => e.data,
  );
}

/**
 * get returns the current snapshot WITHOUT starting a fetch.
 */
export function get<T>(key: string): ResourceSnapshot<T> {
  return snapshot<T>(key);
}

/**
 * subscribe observes one resource; the listener fires on every state
 * change. Returns the unsubscribe function.
 */
export function subscribe<T>(
  key: string,
  listener: (snapshot: ResourceSnapshot<T>) => void,
): () => void {
  const e = entry<T>(key);

  e.listeners.add(listener);

  // A mounted subscriber is a consumer of any in-flight request.
  acquireConsumer(key);

  try {
    listener(snapshot<T>(key));
  } catch {
    // Initial emission errors are the listener's problem.
  }

  let unsubscribed = false;

  return () => {
    if (unsubscribed) return;
    unsubscribed = true;

    e.listeners.delete(listener);

    // Unmount: drop THIS consumer only — a request another consumer needs
    // is never cancelled here; a request nobody needs arms the bounded
    // abort timer (see releaseConsumer).
    releaseConsumer(key);
  };
}

/**
 * invalidate marks one resource's cached data as expired so the next
 * ensure() refreshes it.
 */
export function invalidate(key: string): void {
  const e = entry<unknown>(key);

  if (e) {
    e.fetchedAt = null;
  }
}

/**
 * reset drops one resource entirely (used after a settings save that
 * changes the resource's server-side shape).
 */
export function reset(key: string): void {
  const e = entries.get(key) as Entry<unknown> | undefined;

  if (e?.cancelTimer !== null && e?.cancelTimer !== undefined) {
    clearTimeout(e.cancelTimer);
    e.cancelTimer = null;
  }

  entries.delete(key);
}
