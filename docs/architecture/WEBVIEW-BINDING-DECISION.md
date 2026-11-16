# WebView Binding Decision — v1.11.1 (no adoption; both candidates recorded)

> Work order §7.5 comparison for the desktop native-WebView adapter
> candidates. Verified through the Go module proxy 2026-11-16. No binding
> was adopted in this milestone — see the decision rule at the end.

## Candidate A: `github.com/crgimenes/glaze`

| Fact | Value |
|---|---|
| Latest tag | **v0.0.84** — `refs/tags/v0.0.84` @ `f07fcba9e1805dbd739ace8fa94337f8fd75b50b`, tagged 2026-11-16 (proxy `@latest`) |
| Mechanism | CGo-free `purego` binding to the SYSTEM engines: WKWebView (macOS), WebKitGTK (Linux), WebView2 (Windows) |
| Attractive property | no C toolchain in CI; the work order's first-choice candidate for exactly that reason |
| Material risk (documented upstream) | its Windows WebView2 environment-creation path uses an **undocumented runtime export**. The work order (§4.2) requires treating this as a material compatibility risk with a clear runtime fallback — and refusing adoption if the risk is unacceptable |
| This milestone | NOT adopted. Linux build requires WebKitGTK 4.1 dev headers (absent in the authoring environment); the Windows undocumented-export risk is unresolved; no runtime evidence exists on any desktop here |

## Candidate B: `github.com/webview/webview_go`

| Fact | Value |
|---|---|
| Latest version | `v0.0.0-20240831120633-6173450d4dd6` (2024-08-31) — no tagged release since |
| Mechanism | CGo binding over the upstream C/C++ `webview` library (WebView2 / WebKit-Cocoa / GTK+WebKitGTK) |
| Consequences | CGo in CI for every target; no update stream since 2024; the upstream documented desktop matrix is not a five-platform native adapter |
| This milestone | NOT adopted. Stale upstream, CGo build cost, and zero runtime evidence here |

## In-repo alternative that already exists (no new dependency)

`internal/workspace` (loopback, token-gated workspace server) plus the
system browser remains the shipped rich-surface path — it uses no webview
binding at all. `internal/browser` (chromedp) is the agent's automation
session, a different concern; it is not a user-facing host.

## Decision rule (binding for the adapter gate)

1. The smallest candidate that passes the SAME security/runtime tests
   wins — never the largest feature list.
2. Before any adoption, on a host that can actually build and run the
   engine: availability probe, HTTPS navigation, external-protocol
   behavior, page failure, download approval, origin isolation, bridge
   disabled for remote origins, safe shutdown, missing-runtime fallback
   (work order §14.2).
3. The glaze Windows undocumented-export risk must be closed with a
   verified runtime fallback before glaze may be pinned; otherwise
   webview_go (or a different candidate) is preferred regardless of CGO
   cost.
4. No adoption is recorded as success: v1.11.1 ships the platform-neutral
   contract (`internal/webapphost`) with zero adapters, and the matrix in
   [`WEBVIEW-CAPABILITY-MATRIX.md`](WEBVIEW-CAPABILITY-MATRIX.md) stays
   STAGED_WITH_GATE.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
