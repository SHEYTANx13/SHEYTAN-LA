# Fyne to Go-Gui migration map (run 44457547289 §4.2)

Status: **MIGRATION IN PROGRESS — Go-Gui v0.97.0 is implemented and tested
alongside Fyne v2.8.2; the production wire flip and the Fyne retirement are
NOT complete in this pass.** Per master work order §4.4, the retirement is
left honestly incomplete instead of being claimed done: `ui/fyne` remains
the wired production shell (`internal/desktop` still constructs the Fyne
app), `ui/gogui` is the complete migration candidate with its own test
suite, and the flip is a deliberate follow-up gate (see "Flip procedure").

## Status addendum (2026-11-24, run 45424141601 repair pass)

Dependency refresh (owner direction): the in-app go-gui pin moved
`v0.97.0 -> v0.99.1` in the root go.mod (published upstream, verified
against the Go module proxy at execution time; `go mod tidy` at the
pinned toolchain). The go-gui shell packages (`ui/gogui`, renderstate,
renderer, render, platform, brand, renderboot, cmd/sheytan-mobile)
build clean at v0.99.1 on linux/amd64 go1.27.4; the migration scope
below is UNCHANGED — `ui/fyne` is still the wired production shell and
the macos-x64 blocker still stands. The older counts in the 2026-11-21
addendum refer to the v0.97.0 state and are kept for history.

## Status addendum (2026-11-21, runs 44663205295 + CodeQL 44663205241)

Re-verified counts this pass: `ui/fyne` = 49 Go files (10 view files, 22
test files) — still the wired production shell; `ui/gogui` = 33 Go files
(14 view files, 5 test files) — the candidate, whose suite passes
(`go test ./ui/gogui/` green on linux/amd64 go1.27.4). The production
wire flip is again NOT done in this pass: nothing here is claimed as a
cutover. The candidate-vs-production delta is the concrete flip scope:
the Fyne-side interaction suites (22 test files) have no Go-Gui
counterparts for most screens yet, and the launch smoke + packaging
gates (Windows PE/Xvfb/fyne-package) all still pin the Fyne binary.

## Toolkit decision

- Target: `github.com/go-gui-org/go-gui v0.97.0` (pinned in the root
  go.mod; published 2026-11-17, exact pin verified against the Go module
  proxy at execution time).
- The v0.97.0 `NativePlatform` contract requires
  `SetKeepAwake(on bool, app, reason string) error`. The go-gui stock
  backends implement it; no custom platform backend is shipped in this
  pass, so no additional implementation was required. A custom backend
  must wrap `errors.ErrUnsupported` for unsupported capability requests —
  never return nil.
- go-gui is pure Go on desktop (purego/xgb — no cgo), which keeps the
  whole UI buildable with `CGO_ENABLED=0`.

## Mapping table (old Fyne path -> new Go-Gui path -> area -> tests)

The wire contract (CoreClient DTOs, endpoints, handshake) is IDENTICAL in
both packages: `ui/gogui/client.go` is the same client surface over
`internal/localipc` as `ui/fyne/client.go`, so both shells are clients of
one core with no duplicated policy.

| ui/fyne (old)              | ui/gogui (new)                  | Area / component                    | Core service interface        | Tests (gogui) |
|----------------------------|---------------------------------|-------------------------------------|-------------------------------|---------------|
| app.go                     | app.go                          | App boot, bootstrap handshake, retry | localipc + api handshake      | shell_test.go (TestShellInitialViewIsChat, TestStatusLineReflectsConnection) |
| shell.go                   | shell.go, dock.go               | Responsive shell, nav rail, dock workspace, splitters, shortcuts | presentation only | shell_test.go (nav, shortcuts, ownership), dock_test.go (persistence, splitter, sanitize) |
| panels.go                  | view_panels.go, view_models.go  | History / Models / Tools / Logs sub panels | CoreClient sessions/config/logs | fakecore_test.go-driven view tests |
| state.go                   | state.go                        | Runtime presentation store (sessions, stream, phases, pumps) | CoreClient callbacks | exercised via view tests |
| client.go                  | client.go                       | CoreClient over localipc (identical wire) | internal/localipc | fakecore_test.go |
| logic.go                   | logic.go                        | StreamAccumulator, SessionListGuard, RunPhaseMachine | pure logic | ported algorithms (shared shapes) |
| ui_marshal.go              | uido.go                         | uiDo UI-thread marshalling seam | toolkit threading | SetTestUIMarshal used by all tests |
| theme.go, theme_service.go | theme.go, theme_service.go      | Token theme projection + live switcher (hellish/obsidian/light) | internal/theme | theme_test.go (4 tests) |
| widgets.go                 | widgets.go                      | PageHeader, Loading/Empty/Error surfaces, badges | presentation | used by all views |
| composer.go                | view_chat.go (composer)         | Composer entry, Enter/Shift+Enter, primary action slot | CoreClient run | view_chat_test.go (3 composer tests) |
| adaptive_pane.go           | dock.go + shell size classes    | Responsive dual pane -> dock panes | presentation | dock_test.go |
| window_controls.go         | shell.go (titlebar area)        | Window controls panel               | platform adapters            | (status: PENDING dedicated test) |
| artifact_card.go           | widgets.go (EvidenceCard)       | Settled artifact cards              | presentation | view coverage |
| multipart.go               | multipart.go                    | Attachment writer for /api/attachments | CoreClient | shared shape |
| settings_schema.go         | settings_schema.go              | Declarative Settings registry + BuildPatch | CoreClient config | view_settings coverage |
| assets.go                  | assets/                         | Brand assets                        | presentation | n/a |
| view_chat.go               | view_chat.go                    | 1. Chat/Home                        | CoreClient sessions/run      | view_chat_test.go |
| view_agent.go              | view_agent.go                   | 7. Agents/Lab (goals, tasks, activity ring) | CoreClient goals/runs | (status: PENDING dedicated test) |
| view_lab.go                | view_lab.go                     | 7/8. Coding Lab + Automation & Tasks (queue) | CoreClient runs | (status: PENDING dedicated test) |
| view_workspace.go          | view_workspace.go               | 5/6. Workspace/Projects + Git clone/status | CoreClient workspace/gitclone | (status: PENDING dedicated test) |
| view_engines.go            | view_engines.go                 | 3. Engine Center + appearance       | renderer registry, theme     | (status: PENDING dedicated test) |
| view_studio.go             | view_studio.go                  | 3D Studio control surface           | CoreClient studio            | (status: PENDING dedicated test) |
| view_system.go             | view_system.go                  | 9. Diagnostics (System Centre)      | CoreClient diagnostics       | (status: PENDING dedicated test) |
| view_settings.go           | view_settings.go                | 10. Settings                        | CoreClient config (BuildPatch) | (status: PENDING dedicated test) |
| (none — Models panel)      | view_models.go                  | 2. Model Center                     | CoreClient systems/models    | (status: PENDING dedicated test) |
| (engine download in agent) | view_downloads.go               | 4. Downloads (engine + model progress) | CoreClient engine update | (status: PENDING dedicated test) |
| (none)                     | view_onboarding.go              | 11. First-run onboarding (NEW wizard surface) | setup state | (status: PENDING dedicated test) |
| (tool matrix in settings)  | view_approvals.go               | 12. Approvals/permissions + goal decisions | CoreClient config/GoalDecision | (status: PENDING dedicated test) |

Native adapters: `internal/platform` carries the typed provider
interfaces (DocumentProvider, ClipboardProvider, PermissionProvider,
NotificationProvider, AccessibilityProvider, PowerProvider,
LifecycleProvider, WebWorkspaceProvider) with honest unsupported
defaults; `cmd/sheytan-mobile` is the Go-Gui mobile entrypoint
(package main + func main — the gomobile BUILD shape; run 44547031698 §6:
`gomobile build -target=android/arm64` produces the debug-signed APK,
`gomobile bind` builds an AAR library and is retired from the APK lane). The mobile adapters return typed unsupported states
until go-gui's mobile services reach parity — never silent fakes.

## Test evidence

- `go test ./ui/gogui/ -count=1` (run 44835425398 pass, linux-amd64
  sandbox): PASS — dock persistence round-trip and garbage recovery,
  navigation/shortcuts/ownership, theme mapping and live switch,
  composer send/empty-send/stop-slot, settlement reload, PLUS the new
  `TestSizeClassForBreakpointMatrix` pinning the responsive breakpoint
  decision (sizeClassFor) at every boundary (360/430/600/768/900/1024/
  1280/1366/1920 and the 599.9/899.9/1279.9 edges).
- `go build ./ui/gogui/...`: clean; pure-Go, no cgo.
- Per-screen interaction tests beyond the ones above are marked PENDING
  in the table — the screens are implemented and compile, but the
  dedicated interaction tests were not completed inside this pass and
  are NOT claimed as verified.
- The Fyne production suite (internal/desktop) and the native launch
  smoke tests are NOT RUN in the run 44835425398 pass: the execution
  sandbox carries no GL headers / pkg-config gl / Wayland headers (and
  no root to install them), so the cgo Fyne stack cannot compile there —
  exactly the evidence-class boundary the work order requires stating
  instead of faking.

## Flip procedure (follow-up gate)

1. Land the PENDING per-screen interaction tests.
2. Point `internal/desktop.Run` at `goguiui.NewDesktopApp` (keep the Fyne
   wire behind a build tag for one release).
3. Run the hosted Linux lane UI suite against the Go-Gui shell on xvfb.
4. Remove `ui/fyne`, the `fyne.io/fyne/v2` dependency, the Fyne UI gate,
   and the license-table row in one commit; regenerate the file catalog.
5. Only then claim work order §4.4 "Fyne retirement" complete.
