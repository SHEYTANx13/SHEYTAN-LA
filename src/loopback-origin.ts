// loopback-origin.ts — v1.9.5 (P0): the PURE validation core of the
// desktop activity-transport discovery. DOM-free by design so the unit
// suite pins every lookalike/scheme/credential/path refusal without a
// browser shenanigan layer; config.ts consumes it.
//
// SECURITY CONTRACT: the frontend honours a discovered activity-socket
// origin ONLY when it is an exact, normalized http: LOOPBACK origin —
// hostname exactly 127.0.0.1 or localhost, an explicit valid port, no
// credentials, and no path/query/fragment. The v1.9.4 startsWith prefix
// match accepted lookalike hosts (localhost.attacker.example,
// 127.0.0.1.evil.com) and credential-carrying URLs; this parser refuses
// all of them.

export function isValidLoopbackHttpOrigin(origin: string): boolean {
  let u: URL;

  try {
    u = new URL(origin);
  } catch {
    return false;
  }

  if (u.protocol !== "http:") return false;
  if (u.username !== "" || u.password !== "") return false;
  if (u.pathname !== "/" && u.pathname !== "") return false;
  if (u.search !== "" || u.hash !== "") return false;

  const host = u.hostname.toLowerCase();
  if (host !== "127.0.0.1" && host !== "localhost") return false;

  const port = Number(u.port);
  if (!Number.isInteger(port) || port < 1 || port > 65535) return false;

  // CANONICAL-FORM round trip: the URL parser silently normalizes IP
  // shorthand (http://127.1:PORT → 127.0.0.1) and mapped addresses. The
  // contract accepts only the exact textual loopback forms, so the
  // input must re-serialize to itself.
  if (u.origin !== origin && u.origin + "/" !== origin) {
    return false;
  }

  return true;
}

/**
 * The packaged desktop page origins (never browser/headless/dev): the
 * Wails custom scheme, or the WebView2-intercepted wails.localhost host.
 * On such a page, a failed transport discovery is a VISIBLE degraded
 * state — never a silently cached same-origin fallback.
 */
export function isDesktopPageOrigin(protocol: string, hostname: string): boolean {
  // Defensive on partial test shims: an absent protocol/hostname is
  // never a desktop page (real browsers and WebView2 always provide
  // both on window.location).
  if (typeof protocol !== "string" || typeof hostname !== "string") {
    return false;
  }

  const proto = protocol.toLowerCase();
  const host = hostname.toLowerCase();

  return proto === "wails:" || host === "wails.localhost";
}
