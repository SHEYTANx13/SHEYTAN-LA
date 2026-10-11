import {
  isDesktopPageOrigin as isDesktopPageOriginRaw,
  isValidLoopbackHttpOrigin,
} from "./loopback-origin.ts";

export const API_BASE = "/api";

function browserOrigin(): string {
  return window.location.origin;
}

export const WS_BASE = `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`;

// ---------------------------------------------------------------------------
// v1.9.4 (P0 — the desktop live-answer transport):
//
// Packaged desktop builds serve the page through the webview's
// resource-request interception, which carries HTTP only (Windows
// WebView2 forwards just http://wails.localhost requests into the Go
// handler; Linux production serves the wails:// custom scheme). WebSocket
// handshakes are NOT web-resource requests, so the activity socket derived
// from window.location (ws://wails.localhost/ws/activity) could never
// reach the backend there: zero live frames, no streamed text, no done
// event — the answer became visible only after Stop, whose HTTP abort
// path recovers the persisted reply via the history reload.
//
// The desktop process therefore ALSO serves the same API handler on a
// loopback-only ephemeral listener (internal/api/loopback.go) and
// publishes its origin — plus the per-server capability token — through
// this discovery call, which rides the intercepted same-origin HTTP
// surface and works on every platform.
//
// Resolution is once-per-page-load and cached:
//   - an EXACT http:// loopback origin (strictly parsed — v1.9.5 closes
//     the startsWith lookalike-host hole: localhost.attacker.example and
//     127.0.0.1.evil.com are refused) is honoured for the socket;
//   - an empty origin (browser, headless E2E, every non-desktop serving
//     mode — and the scripted unit-test transports) keeps the historical
//     same-origin WS_BASE behavior unchanged;
//   - on a DESKTOP page origin, discovery failure is NOT silently cached
//     as same-origin forever: the call is retried a bounded number of
//     times and, if the transport still cannot be discovered, the page
//     enters the explicit degraded state (activityTransportDegraded) —
//     a visible, testable fault, never a quietly broken core interaction.
// ---------------------------------------------------------------------------

/** Desktop page origins (packaged builds only — never browser/headless). */
function isDesktopPageOrigin(): boolean {
  return isDesktopPageOriginRaw(window.location.protocol, window.location.hostname);
}

export let activityTransportDegraded = false;

// Test-only escape hatch: unit tests reset module state deterministically.
export function resetActivityTransportForTest(): void {
  wsOriginOverride = null;
  wsOriginResolution = null;
  activityTransportDegraded = false;
}

let wsOriginOverride: string | null = null;
let wsOriginToken: string | null = null;
let wsOriginResolution: Promise<void> | null = null;

export { isValidLoopbackHttpOrigin };

// Bounded discovery retry for packaged desktop: three attempts total, so a
// transiently slow backend cannot permanently strand the page in
// same-origin mode, while a genuinely absent transport fails VISIBLY.
const DISCOVERY_ATTEMPTS = 3;
const DISCOVERY_RETRY_MS = 400;

async function discoverOnce(): Promise<{ origin: string; token: string } | null> {
  try {
    const res = await fetch(`${API_BASE}/ws-origin`);

    if (!res.ok) return null;

    const body: unknown = await res.json();

    if (!body || typeof body !== "object") return null;

    const rec = body as { origin?: unknown; token?: unknown };

    return {
      origin: typeof rec.origin === "string" ? rec.origin : "",
      token: typeof rec.token === "string" ? rec.token : "",
    };
  } catch {
    return null;
  }
}

export function ensureActivityWsOrigin(): Promise<void> {
  if (wsOriginResolution === null) {
    wsOriginResolution = (async () => {
      for (let attempt = 0; attempt < DISCOVERY_ATTEMPTS; attempt++) {
        const disc = await discoverOnce();

        if (disc && isValidLoopbackHttpOrigin(disc.origin)) {
          wsOriginOverride = disc.origin;
          wsOriginToken = disc.token || null;
          return;
        }

        // An explicit EMPTY origin is the server's authoritative
        // "stay same-origin" answer (browser/headless/dev). Only an
        // unresolved/failed call is retried — a definite same-origin
        // verdict is accepted immediately.
        if (disc && disc.origin === "") {
          wsOriginOverride = null;
          wsOriginToken = null;
          return;
        }

        if (attempt < DISCOVERY_ATTEMPTS - 1) {
          await new Promise((r) => setTimeout(r, DISCOVERY_RETRY_MS));
        }
      }

      wsOriginOverride = null;
      wsOriginToken = null;

      if (isDesktopPageOrigin()) {
        // A packaged desktop page without a discoverable activity
        // transport is a BROKEN CORE INTERACTION — fail visibly and
        // testably, never a silently degraded app.
        activityTransportDegraded = true;
        console.error(
          "activity transport unavailable: desktop discovery failed after " +
            `${DISCOVERY_ATTEMPTS} attempts — live answer streaming is broken`,
        );
      }
    })();
  }

  return wsOriginResolution;
}

export function activityWebSocketURL(sessionId?: string | null): string {
  const parts: string[] = [];

  if (sessionId && sessionId.trim()) {
    parts.push(`sessionId=${encodeURIComponent(sessionId)}`);
  }

  // The loopback capability token travels ONLY on the desktop transport
  // URL (it is meaningless on the same-origin path and never issued for
  // it). It is a credential: never logged.
  if (wsOriginOverride && wsOriginToken) {
    parts.push(`token=${encodeURIComponent(wsOriginToken)}`);
  }

  const query = parts.length > 0 ? `?${parts.join("&")}` : "";

  return `${wsOriginOverride ?? WS_BASE}/ws/activity${query}`;
}

export function apiOrigin(): string {
  return browserOrigin();
}
