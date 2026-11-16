# WebView Security — The Native Web-Content Runtime Contract (v1.11.1)

> The binding security policy for the optional native WebView layer
> (work order §4.6). The enforced code is `internal/webapphost`
> (platform-neutral contract + policy); its tests are the authority for
> every rule listed here. Verified 2026-11-16.

## 1. Position (what this layer is and is not)

The native WebView is a browser-content runtime for REAL web pages, in its
own native window (WebView2 on Windows, WKWebView on macOS, WebKitGTK on
Linux). It is:

- NOT the primary application shell (Fyne stays production);
- NOT a renderer with store access — pages are untrusted content;
- NOT a substitute for the in-app Go workspace server
  (`internal/workspace`, loopback + token-gated), which remains the
  existing rich-surface path;
- NEVER a silently bundled browser engine: adapters use the OS engine.

## 2. Navigation policy (enforced in `internal/webapphost`)

| Rule | Enforcement |
|---|---|
| `javascript:`, `data:`, `file:` and scheme-less URLs | REJECTED — never loaded, never offered externally (`TestNavigationRejectsPrivilegedSchemes`) |
| `app://` origins the host never created | REJECTED — the app: scheme family is host-privileged |
| Host-created trusted `app://` origin (e.g. `app://workspace`) | ALLOWED — the only origin that may ever receive the bridge |
| Loopback, RFC1918, link-local (incl. `169.254.169.254`) and literal localhost | REJECTED by default; only an explicit private-network grant re-enables, and it never re-enables privileged schemes (`TestNavigationRejectsPrivateNetworkByDefault`, `TestPrivateNetworkGrantIsExplicitAndScoped`) |
| Other `http`/`https`/`about` | ALLOWED as untrusted content — no bridge, ever (`TestNavigationAllowsRemoteHTTPSAsUntrustedNoBridge`) |
| Unknown external schemes (`magnet:`, `mailto:`, …) | offered to the system browser, never loaded here |
| TLS certificate validation | stays ON. There is NO API to bypass it — by construction, not a forgotten flag |

## 3. JS bridge policy (named, versioned, fail-closed)

- Only a host-created trusted `app://` origin may receive a bridge, and
  only with an explicit per-capability grant (`BridgeGrant`). The model
  cannot grant itself anything.
- Every call is named (allowlist), carries a required call id (replay
  protection key), and is payload-bounded (64 KiB default).
  `TestBridgeValidateFailClosed` pins: remote origin refused,
  non-granted operation refused, missing call id refused, oversized
  payload refused, nil grant refused.
- No provider/API secrets in window globals, cookies or WebView storage.
- Separate profile/storage per workspace; explicit clear-data action.

## 4. Downloads and external content

- Downloads NEVER use the web engine's own downloader: they hand off to
  the application's existing safe downloader with an approved destination
  flow (`DownloadPolicy` — zero value fails closed:
  `TestDownloadPolicyFailsClosed`).
- Unsupported site features get a visible reason and an offer to open in
  the system browser — never a promise of Chrome/Edge/Safari parity, and
  never a claim of Chrome extension APIs, DRM/proprietary codecs, or
  browser sync/password stores.

## 5. Adapter status (never inflated)

No adapter shipped in v1.11.1: the Linux WebKitGTK 4.1 development
headers are absent in the authoring environment (no privileged install
available), and Windows/macOS hosts were not exercised. Every adapter row
is STAGED_WITH_GATE in
[`WEBVIEW-CAPABILITY-MATRIX.md`](WEBVIEW-CAPABILITY-MATRIX.md). No
placeholder menu item or empty Start() exists — the contract package ships
policy and tests only.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
