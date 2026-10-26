# 3D Studio — the optional system-topology viewer (v1.11.0)

The 3D Studio is an OPTIONAL, read-only visualization surface: a 3D map of
the live system topology — host hardware, inference engine, runtime
governor, session store and tool registry — rendered with Three.js inside
the user's default browser. It is reachable from the navigation rail
("3D Studio") and can be disabled in Settings → General → "3D Studio"
(the `studioEnabled` config field, `true` by default).

This document is the focused contract for the feature: what it is, what it
can never do, its security boundary, its resource posture, and the exact
asset-update procedure.

## Boundary and authority

* The studio is a VIEWER, not a second application. It exposes exactly one
  data endpoint (`/api/view` on its own loopback server) and ZERO
  commands. There is no write path, no tool execution, no approval flow
  and no configuration mutation anywhere in the viewer surface — not
  behind a flag, not behind a token: the endpoints do not exist.
* The ONE core authority (`internal/api`) owns the viewer lifecycle. The
  UI only calls `/api/studio/state`, `/api/studio/start` and
  `/api/studio/stop` through the same authenticated local IPC channel as
  every other feature. Server `Close()` stops the viewer — the loopback
  listener never outlives the core process.
* The topology DTO is assembled by `Server.StudioTopology` from the SAME
  read models the Runtime Governor surface serves (`sysinfo.ProbeFast`,
  the engine lifecycle owner's `Metrics`, the governor's `State`, the
  session store, the tool registry snapshot). The adapter never starts or
  probes an engine, never invents a value, and preserves the product's
  detection-vs-execution honesty (GPU/NPU nodes are labeled detection
  evidence).

## Platform shape (honest)

The viewer is browser-hosted: "Open 3D Studio" starts the loopback server
and opens the user's default browser. The Fyne window deliberately does
NOT embed a web runtime — a maintained embedded webview (WebKitGTK /
WebView2) was not verifiable on both desktop targets in the v1.11.0
timebox, so the honest slice shipped: the same viewer, the same offline
assets, the same security boundary, hosted by the browser the user
already trusts. This is a documented decision, not a hidden limitation;
the feature remains irrelevant to the shell when unused (nothing starts
until "Open 3D Studio" is pressed, nothing polls while stopped).

## Security boundary

* **Bind**: `127.0.0.1:0` only — loopback, ephemeral port. No
  configuration surface can move the bind to an external interface.
* **Token**: every viewer start generates a 256-bit crypto-random token.
  The launch URL carries it in the URL FRAGMENT (`#t=…`), which browsers
  never send to servers — so the token cannot land in server logs. The
  core's state endpoint and logs never contain it. `GET /api/view`
  requires `Authorization: Bearer <token>`; wrong or missing tokens get
  a typed 401.
* **Static assets carry no secrets** and are served without the token;
  they contain no user data and no product telemetry.
* **CSP** on every response: `default-src 'none'; script-src 'self';
  style-src 'self'; connect-src 'self'; img-src 'self' data:;
  font-src 'self'; base-uri 'none'; form-action 'none';
  frame-ancestors 'none'` — the page cannot contact any other origin,
  cannot be framed, and cannot inline remote content. Plus
  `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
  `X-Frame-Options: DENY`, `Cache-Control: no-store` on data.
* **No injection surface**: the page builds all DOM text via
  `textContent` (never `innerHTML`), so the topology DTO cannot inject
  markup even in principle. The viewer code is plain ES modules — no
  eval, no remote imports (the CSP would block them regardless).
* **Allow-listed assets only**: unknown paths 404; there is no directory
  listing and no filesystem join (traversal probes are covered by tests).
* **Read-only methods**: non-GET requests to viewer routes get 405.

## Resource posture

* **Offline**: Three.js r182 (0.182.0, MIT) is vendored under
  `internal/studio/assets/` and embedded into the binary with `go:embed`.
  No CDN, no network fetch, no Node at runtime. The only network traffic
  the viewer can generate is `fetch('/api/view')` to its own origin
  (CSP-enforced).
* **Render-on-demand**: no continuous animation loop. A frame is drawn on
  load, on camera interaction, on resize and on explicit refresh; the
  loop pauses entirely while the tab is hidden (`visibilitychange`).
  Renderer pixel ratio is capped at 2 and the renderer is created with
  `powerPreference: 'low-power'`.
* **Honest degradation**: without WebGL the page says so (typed reason)
  and the textual topology equivalent remains fully usable — the 3D view
  is never faked.
* **Dispose**: geometries, materials and the renderer are disposed on
  reload and on page unload.

## Asset update procedure (reproducible)

1. Pick the target release from `https://github.com/mrdoob/three.js/tags`
   (the current pin is **0.182.0** — see `ThreeJSVersion` in
   `internal/studio/doc.go`).
2. Download from that exact tag (npm mirror of the release):
   `build/three.core.js`, `build/three.module.js`,
   `examples/jsm/controls/OrbitControls.js`, and the package `LICENSE`.
3. Replace the three files under `internal/studio/assets/` and update
   `LICENSE-THREE.txt`; bump `ThreeJSVersion` in `internal/studio/doc.go`.
4. Re-run the gates: `go test ./internal/studio/ ./internal/api/ ./ui/fyne/`.
   The viewer tests assert the embedded assets are present and served,
   and the security contract holds for the new files.

## Verification status (honest)

* PASS locally: studio package tests (security, lifecycle, schema),
  API contract tests (state/start/stop/disabled/topology DTO), UI view
  tests (enabled / disabled / core-error), full `go build ./...`,
  headless + full test sweeps, race gate on the touched packages.
* PASS locally (Linux, Xvfb): the real binary boots with the 8-entry
  navigation, the viewer starts/stops through the API contract, and the
  page serves with its security headers (curl-level checks in tests).
* NOT RUN: rendering the actual WebGL scene on physical desktop hardware
  (Windows and Linux with real GPUs) — the hosted CI runners provide no
  GPU/browser pair for this, and this environment has no browser. The
  Go-side contract (assets, token, CSP, DTO, lifecycle) is fully tested;
  the pixel-level WebGL output is not claimed as verified.
* NOT RUN: Windows-hosted verification of the feature (the release
  pipeline must prove it after the operator push).
