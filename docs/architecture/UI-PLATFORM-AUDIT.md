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
| POC | `experiments/ui-engine-poc/go-gui` — nested module OUTSIDE the production dependency graph; reference slice: window + nav sidebar, chat surface + composer, scrolling long-content message list, Persian/Arabic mixed-direction text with embedded LTR URL/code, resizable Splitter inspector pane, table artifact view, keyboard focus; headless app-level tests (type/click/render) + software-rendered screenshot | `go test ./...` 5/5 PASS (2026-11-16); `poc-slice.png` rendered 126,746 bytes; `go vet` clean; parent `go list ./...` shows zero experiment packages (isolation proven) |
| Advertised capabilities (unverified here) | 50+ widgets, docking, virtualized lists/tables, shaders, animation, bidi text, headless testing, time-travel debugging | vendor claims — reasons to prototype, not production readiness |
| Published platform gaps (release-critical for the five-platform direction) | Windows accessibility-tree synchronization / UI Automation **not implemented**; iOS accessibility tree + URI opening **not implemented**; native open/save/folder/message/confirm dialogs **unavailable on Android and iOS**; iOS autocorrect, marked text/CJK composition and clipboard behavior incomplete in the matrix | the Go-Gui Native Platform Matrix as cited by the research input — these gaps are why the candidate is NOT promoted |

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
