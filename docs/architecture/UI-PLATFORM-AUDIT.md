# UI Platform Audit — Renderer Matrix and Candidate Verification

> The v1.11.1 UI-engine evaluation evidence base. Decision record:
> [`UI-ENGINE-DECISION.md`](UI-ENGINE-DECISION.md). Verified against the
> module proxy and the repository on 2026-11-16.

## 1. Production renderer: Fyne (unchanged for v1.11.1)

| Fact | Value | Verification |
|---|---|---|
| Module | `fyne.io/fyne/v2` **v2.8.2** | go.mod (pinned); matches the researched stability-comparator tag (released 2026-10-12); already at target → **no dependency churn** (work-order rule honored) |
| Build/test gates | `go test ./ui/fyne/` (full suite, button-fit matrix, structural goldens, responsive/boundary/degenerate matrices, resize smoke under Xvfb) | locally green 2026-11-16 under Xvfb; hosted Fyne UI gate green for baseline `124b08a` (run 43773885283) |
| Threading contract | ONE marshalling seam (`ui/fyne/ui_marshal.go`): production `fyne.Do`, test-mode mutex serialization | code + `go test -race` on critical packages |
| Core independence | zero `fyne.io/fyne` imports under `internal/` and `cmd/` | search 2026-11-16 |
| Accessibility | Fyne's a11y is an early opt-in implementation (`-tags accessibility`); the app does NOT claim full cross-platform AT coverage; actual tested technologies: none in this pass (hosted/physical gates remain the authority) | recorded honestly; see DECISION doc §4 |

## 2. Candidate: Go-Gui (POC pinned, NOT production)

| Fact | Value | Verification |
|---|---|---|
| Researched versions | reports said v0.91.0; a separate snapshot said v0.96.0 — **both exist**; v0.96.0 is the current latest | `go list -m -versions github.com/go-gui-org/go-gui` (proxy, 2026-11-16) |
| Pinned candidate | **v0.96.0** — tag `refs/tags/v0.96.0` @ `6f2d4e16e58b25fc2bec3d642e6552433a4e7ae4`, sum `h1:VIHHfFXiMJ43Ru9L16GLqbR10k3y2TJuhKowSS62agI=`, Go 1.26.0, MIT, tagged 2026-11-13 | `go list -m -json github.com/go-gui-org/go-gui@v0.96.0` — immutable tag + checksum verified through the module proxy; NOT a moving branch |
| Rendering model | pure Go, cgo-free OpenGL (Linux/Windows), Metal (macOS), WebGL/WASM (browser); no browser runtime, no JS/HTML/CSS | module README + license inspected in the module cache |
| POC | `experiments/ui-engine-poc/go-gui` — nested module OUTSIDE the production dependency graph; reference slice: window + nav sidebar, chat surface + composer, scrolling long-content message list, Persian/Arabic mixed-direction text with embedded LTR URL/code, resizable Splitter inspector pane, table artifact view, keyboard focus; headless app-level tests (type/click/render) + software-rendered screenshot | `go test ./...` **10/10 PASS** (2026-11-16, hybrid milestone): the original 5 plus the mandated responsive matrix (1280x720, 1024x768, 800x600, 640x480 — both surfaces render contained), per-size screenshots, a 2x sampling proxy (NOT a device-scaling claim), close/reopen, and typing focus at the 640x480 floor; `go vet` clean; parent `go list ./...` shows zero experiment packages (isolation proven) |
| Containment finding (hybrid milestone) | at 640x480 the `Send` button lost its visible area inside the starved splitter pane (MinSize 420+260 could not fit beside the rail) — a REAL defect of the exact class the responsive matrix exists to catch; fixed in POC code (not by weakening the test): below a 900-unit breakpoint the workspace renders one content column, mirroring the Fyne shell's compact contract; resolution is window-creation-time in the POC (live resize reflow stays a promotion-gate item) | `TestComposerFocusAtSmallestMandatedSize` + `TestResponsiveMatrixRendersContained/640x480` (green after the fix; red before it) |
| Advertised capabilities (unverified here) | 50+ widgets, docking, virtualized lists/tables, shaders, animation, bidi text, headless testing, time-travel debugging | vendor claims — reasons to prototype, not production readiness |
| Published platform status — **corrected 2026-11-17** (the maintainer's 16-Nov-2026 Native Platform Matrix edit) | Windows accessibility IS implemented: a Windows UI Automation provider serves Narrator and NVDA on amd64 and arm64 (`UiaRaiseNotificationEvent` needs Windows 10 1709+ for announcements); Windows desktop dialogs supported. REMAINING documented gaps: iOS accessibility tree sync/announcements, iOS URI opening, native open/save/folder/message/confirm dialogs on Android and iOS, iOS autocorrect/marked-text (CJK)/clipboard | the corrected Go-Gui Native Platform Matrix (Mike Ward, 2026-11-16) — the earlier “Windows accessibility not implemented” line is STALE and must not be repeated; the iOS/mobile gaps remain promotion blockers |
| Corrected-by | v1.11.1 renderer-convergence milestone (2026-11-17): the stale claim was retired from [`UI-ENGINE-DECISION.md`](UI-ENGINE-DECISION.md), the renderer registry (`internal/renderer`, tripwired by `TestGoGuiAccessibilityCorrection`) and this audit | evidence class: upstream matrix correction incorporated; no hardware screen-reader verification has been run here (NOT RUN — hosted/physical gates own that) |

## 2b. Candidate: GoGPU graphics surface (POC pinned, separate surface)

| Fact | Value |
|---|---|
| Pinned candidate | `github.com/gogpu/gogpu` **v0.54.1** — tag @ `01b7d29ce5678a034483bcaee6c6b5822110a08a`, tagged 2026-11-08; module-graph pins wgpu v0.34.9 / gputypes v0.8.2 / naga v0.21.4 (NOT hand-forced — the latest standalone wgpu tag v0.34.15 exists but overriding the pin gogpu requires would be an unaudited change) |
| POC | `experiments/ui-engine-poc/gogpu` — deterministic spectrum (LCG data, pure function of the frame index), positioned bars via one texture + `DrawTextureScaled`, explicit `OnClose` resource cleanup; `-screenshot` headless path and `-frames N` bounded soak |
| Verified on this host | build/vet green; tests 2/2 (determinism + real non-blank software render + render determinism); `-screenshot` produced a real 960x600 software frame; bounded 3-frame windowed soak under a manual Xvfb display (`adapter selected name="Software Renderer" type=CPU`), exit 0 |
| Never claimed | GPU HARDWARE NOT RUN (no Vulkan ICD, no /dev/dri on this host); frame percentiles NOT MEASURED; zero-copy/interop with Go-Gui — none exists, by design (see [`GO-GUI-GOGPU-INTEROP.md`](GO-GUI-GOGPU-INTEROP.md)) |

## 2c. Candidate: Wails v3 WebView Web Workspace (POC pinned, NOT production)

| Fact | Value |
|---|---|
| Pinned candidate | `github.com/wailsapp/wails/v3` **v3.0.0** — the stable desktop release (2026-10-26), resolved through the module proxy; newer stable releases exist (v3.1.0) — the conservative pin follows the milestone rule |
| POC | `experiments/ui-engine-poc/wails-webview` — nested module OUTSIDE the production graph; bridge (closed method allowlist, strict schema, 64 KiB ceiling, correlation ids), security policy (trusted-origin-only, file/javascript/data blocked, localhost/link-local/private metadata endpoints denied, CSP, bounded traversal-free downloads) and the feature-detection manifest (17 probes, every one carrying its honest fallback — WebGPU NEVER claimed from detection) |
| Verified on this host | `go test ./...` **PASS** (bridge + security + feature suites) + `go run .` self-check (2026-11-17) |
| Never claimed here | the WebView WINDOW itself — BLOCKED on this host (no WebKitGTK/Windows): the runtime wiring compiles only behind `-tags wails_runtime` on a WebView-capable host; hosted/physical lanes own that evidence |
| Boundary | HTML/CSS/JS allowed ONLY inside this isolated workspace; the Go core stays the ONE policy/persistence/permission authority — see [`WEBVIEW-SECURITY.md`](WEBVIEW-SECURITY.md) and [`RENDERER-HOST.md`](RENDERER-HOST.md) |

## 3. Contingency: Gio (not started, deliberately)

Gio is evaluated ONLY if both Fyne and Go-Gui fail a critical requirement
and a representative comparative POC justifies it. No Gio code, module or
experiment exists in this repository, and none was added in v1.11.1 — a
third active renderer implementation would violate the one-authority
rule.

## 4. A2UI (protocol reference only)

A2UI 1.0 (stable 2026-11-11) is a JSON/streaming protocol where agents
describe UI and each application renders its own approved native
components; its public launch renderers are web/Flutter/SwiftUI/Jetpack —
**no Go renderer**. For this product it is a design reference only: the
app stays native Go, adds no browser/JS layer for it, and the existing
typed presentation store (`internal/presentation`) remains the single
declarative-UI schema authority (validated graphs, registered actions,
fail-closed fallback to the last valid view). A full generative-UI
editor/runtime remains gated roadmap work.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
