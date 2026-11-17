# Wails Web Workspace POC (v1.11.1 renderer convergence)

The isolated WebView-hosted workspace layer — NOT a second application
state. The Go core stays the ONE policy/persistence/permission authority.

## Pin

`github.com/wailsapp/wails/v3 v3.0.0` — the stable desktop release
(2026-10-26), resolved through the module proxy. Newer stable releases
exist upstream (v3.1.0); the conservative v3.0.0 pin follows the
milestone rule ("a newer stable release may be selected only after
explicit release verification").

## What this POC proves HERE (any host, no WebView needed)

- the **bridge** speaks a strict typed schema: closed method allowlist
  (`theme.tokens`, `engine.status`, `workspace.ping`), unknown fields
  rejected, 64 KiB payload ceiling, correlation id required
  (`bridge.go`, `bridge_test.go`);
- the **security boundary**: the local embedded origin is the only
  trusted surface; `file://`/`javascript:`/`data:` and unknown schemes
  are denied; localhost/link-local/private metadata endpoints are denied
  (SSRF class); ordinary web targets degrade to "Open in system
  browser"; CSP forbids remote/inline script; downloads are
  MIME/size-bounded and traversal-free (`security.go`,
  `security_test.go`);
- the **feature-detection contract**: the full probe vocabulary
  (HTML5 … clipboard) with a declared fallback per probe — WebGPU and
  friends are NEVER claimed from detection (`features.go`,
  `features_test.go`, `assets/workspace.html` runs the probes and
  renders the honest table).

Run: `go run .` (self-check) and `go test ./...`.

## What is BLOCKED on this host

Launching the actual WebView window. The runtime wiring
(`app_runtime.go`) compiles only behind `-tags wails_runtime` on a host
with the platform WebView (WebView2 / WebKitGTK / WebKit); this sandbox
has none of them, and hosted/physical lanes own that evidence. The
runtime file mirrors the same allowlist + policy layers tested above.

## Promotion gate (docs/architecture/RENDERER-ENGINE-MATRIX.md)

WebView availability + sandbox + browser capability + IPC tests on real
runtimes (Windows/Linux first); macOS only after its WebKit runtime is
actually tested; mobile stays experimental (upstream contract).
