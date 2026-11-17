# Renderer Engine Matrix — v1.11.1

> The engine catalog as CODE: `internal/renderer/registry.go` is this
> document's authority (the gallery renders it; the tests tripwire it).
> This page is the human view of the same facts. Nothing here may claim
> more than the registry — maturity and capability states carry their
> evidence class.

## 1. The engines

| Engine | Version / pin | Maturity | Isolation | Runtime state in this package | Theme mapping |
|---|---|---|---|---|---|
| **Fyne** (the production shell) | fyne.io/fyne/v2 v2.8.2 | **production** | in-process | **ready** — the running shell | **full** — every token role maps (`ui/fyne/theme.go`) |
| **Go-Gui** | github.com/go-gui-org/go-gui v0.96.0 (immutable tag `6f2d4e1…`, checksum-verified) | preview | companion-window | not built — POC in `experiments/ui-engine-poc/go-gui` (10/10 headless tests; responsive matrix) | partial — designed (`docs/ui/DESIGN-SYSTEM.md`), wired only in the POC |
| **GoGPU UI** | POC pin per `experiments/ui-engine-poc/gogpu/go.mod` (gogpu v0.54.1) | experimental | companion-window | not built — POC in `experiments/ui-engine-poc/gogpu` (2/2 tests; software-rendered spectrum) | partial — upstream documents 4 theme systems; SHEYTAN mapping designed, unwired |
| **Wails Web Workspace** | github.com/wailsapp/wails/v3 **v3.0.0** (stable, 2026-10-26, proxy-verified) | experimental | companion-window | not built — POC in `experiments/ui-engine-poc/wails-webview` (bridge/security/feature suites green; the WebView window itself is `wails_runtime`-tagged) | partial — tokens serialize to CSS via `internal/theme`'s JSON form; designed, unwired |

## 2. Platform verification status (never inflated)

| Engine | linux/amd64 | windows/amd64 | darwin | android/ios |
|---|---|---|---|---|
| Fyne | **verified** (local + hosted gates; UI matrix under Xvfb) | **verified** (hosted run 43883548198 lanes) | expected (not tested this run) | unsupported this milestone |
| Go-Gui | expected | expected (UIA a11y implemented upstream — corrected matrix 2026-11-16) | expected | unsupported upstream (dialogs/iOS gaps) |
| GoGPU UI | expected (X11/Wayland upstream) | expected (Vulkan/DX12/GLES upstream) | expected (Metal upstream) | unsupported (Android "in review"; iOS not implied by Metal) |
| Wails | expected (WebKitGTK required) | expected (WebView2 required) | expected (WebKit; only when actually tested) | experimental upstream, outside the stable desktop contract |

Evidence vocabulary: **verified** = tested on the exact target this run;
**expected** = plausible, not tested here; **unsupported** = upstream
or milestone boundary. Mobile parity is NEVER claimed from desktop work.

## 3. Capability truths (the honest rows)

- Touch: Fyne **partial** (pointer/tap; 44 DIP hit floor enforced;
  physical touch NOT RUN); others **unverified**.
- Accessibility: Fyne **partial** (early opt-in a11y; no screen-reader
  verification here); Go-Gui **partial** (Windows UIA implemented
  upstream per the corrected matrix — hardware verification NOT RUN);
  others **unverified**.
- GPU: Fyne **partial** (GL driver; llvmpipe in CI — never claimed from
  detection); GoGPU UI **unverified** (executed-backend telemetry is
  the only claimable source; software fallback observed); Wails
  **unverified** (WebGL/WebGPU belong to the host engine).
- WebView: Wails **partial** (real browser APIs with MANDATORY feature
  detection and "Open in system browser" fallback — a WebView is NOT
  the newest Chrome/Edge/Safari on every OS); Fyne/Go-Gui/GoGPU
  **unsupported**.
- Tray: **unsupported everywhere** (Fyne v2.8.2 exposes no tray API;
  the gallery renders the disabled control WITH its reason — never a
  fake tray state).

## 4. Promotion policy (§3.6 of the milestone work order)

Fyne stays production and default. A candidate is promoted ONLY through
its full gate on ONE pinned version: platform builds AND runtime
launch; native file/permission/lifecycle; keyboard/paste/IME/CJK/RTL;
accessible semantic trees + screen readers (UIA, AT-SPI, VoiceOver,
TalkBack) with the matrix gaps closed or waived; focus/touch/safe-area;
virtualization/dialogs; same-hardware performance benchmarks; security
+ dependency + license review. **A POC compile is not a promotion** — a
single POC pass never promotes a renderer to production.

## 5. Registry tripwires (the tests that keep this page honest)

`internal/renderer/registry_test.go`: catalog validation;
production-is-Fyne-only; no mobile claims from desktop; the Go-Gui
accessibility CORRECTION (the stale claim cannot return); Wails
universal-parity ban; every non-ready state carries a reason;
manifest vocabulary fail-closed.
