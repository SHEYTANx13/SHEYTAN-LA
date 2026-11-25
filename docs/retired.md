# Retired Mechanisms

The authoritative register of mechanisms that were once part of
SHEYTAN-LA and are no longer in the active product. It exists to prevent
accidental reintroduction. Facts only; history remains in git.

**All material listed here stays under the Proprietary License (see
[`../LICENSE.md`](../LICENSE.md)) — retirement releases no rights.**

**Still-live browser surfaces (do not confuse with the retired shells
below):** the read-only Web Workspace (`internal/workspace`) and the
optional browser-hosted 3D Studio (`internal/studio`) are ACTIVE,
on-demand, loopback/token/CSP-guarded viewer surfaces. Using JavaScript
in a browser page is not, by itself, a retired mechanism.

---

| Mechanism / path | Status | Replacement / current equivalent | Reason | Migration caveat |
|---|---|---|---|---|
| Fyne toolkit desktop shell (`ui/fyne/`, `SHEYTAN_UI=fyne`) | Retired (run 45634345922 update) | The adaptive web shell (`internal/webshell`) rendered by the typed web workspace host (`internal/webworkspace`, Wails v3.1.0) — the primary window on every desktop platform; the transport/state client survives as `internal/coreclient` | The packaged builds never carried the web host, so the operator still saw the retired shell; the web shell now carries the full IA (real API-backed views) and every desktop build compiles it (`-tags webworkspace`) | `SHEYTAN_UI=fyne` fails boot with the typed `ErrFyneRetired` instead of silently choosing another shell; the go-gui mirror (`ui/gogui`) stays the native capability; the renderer id `fyne` survives only as a stable name in `internal/renderer` |
| Qt/QML desktop host (`ui/qt/`) | Retired (v1.11.0) | Single in-process Go UI, now the adaptive web shell (`internal/webshell` + `internal/webworkspace`) | One-Go-UI migration; behavioral contracts moved to Go tests | Never reintroduce a second desktop shell |
| iOS CI lanes (`ios-simulator-arm64`, `ios-device-arm64`; `scripts/build/ios.sh`, `scripts/build/ios-device.sh`; `docs/platforms/IOS_SIGNING.md`) | Retired (run 45323044820 repair, owner direction: iOS is not a current delivery target at this stage) | The FIVE-target platform matrix (windows-x64, linux-x64, macos-arm64, macos-x64, android-arm64); the releasecontract suite now FAILS on any `ios-*` matrix target or retired iOS artifact name | The simulator lane never reached its own gates (the darwin host suite gated it) and the device lane was permanently blocked on owner-side Apple signing secrets (IOS_DISTRIBUTION_P12_BASE64 and family); both consumed hosted macOS runner time without producing a qualified package | Reintroduction needs: real Apple signing configuration, the ios.sh/ios-device.sh lane scripts restored from git history, the releasecontract matrix pins re-widened, and the darwin-host suite (the iOS lane host gate) green |
| React + Vite + Playwright frontend (`src/`, `web/`) | Retired (v1.11.0) | The adaptive web shell; rich viewing via the on-demand Web Workspace | One-Go-UI migration | Do not recreate a Node/React build for static assets |
| First-generation webview desktop shell | Retired | The adaptive web shell over the same-origin web workspace host | Superseded twice over | Its page-origin WebSocket exception was removed with it |
| Loopback WebSocket activity transport (desktop) | Retired for desktop | `internal/localipc` (named pipe / Unix socket) | Desktop is in-process; browser/headless modes use ordinary HTTP over their own origin | Desktop must never open a network listener |
| `SHEYTAN_UI_EXECUTABLE` child-spawn resolution | Retired | In-process UI | No second UI process exists | — |
| Qt CMake / qmake packaging path | Retired | UCRT64 + portable-ZIP contracts (`packaging/`) | One-Go-UI migration | — |
| NSIS installer production (`packaging/nsis/`, `scripts/ci/NsisVersion*.ps1`, makensis CI steps) | Retired (master work order §10) | Portable ZIP only — the ONE supported Windows/Linux package; `PackageGate.ps1` rejects installer remnants (`INSTALLER_REMNANT`) | Portable/development artifacts only; no installer generation | Never reintroduce an installer build, upload or smoke step; a stale `*-installer.exe` in `dist/` fails the package gate |
| Numeric product version as user-facing branding (app title, About, banners, package display names) | Retired (master work order §12.1) | `internal/brand.Stage` = "ALPHA" — the single user-facing identity constant; `internal/config.AppVersion` stays the numeric BUILD IDENTIFIER for CI/packaging plumbing (cmd/printversion, BUILD-INFO) | ALPHA is the only product-stage label until the product is fully functional | Do not render AppVersion in user-facing surfaces; printversion keeps its strict semver resolver contract |
| MSYS2 MINGW64 toolchain path | Retired (Oct 2026) | UCRT64 exclusively | Upstream deprecation | Never switch back |
| Fixed-attempt busy-dial on named pipes | Superseded (v1.10.12) | Deadline-bounded busy-dial budget (tightened by the caller's context deadline in v1.11.1) | Attempt caps lost availability races | See `internal/localipc/dial_windows.go` |
| Silent CPU fallback for explicit GPU variant requests | Retired | Explicit requests fail loudly | Honesty contract | — |
| Sleep-based readiness / correctness | Retired | Event-driven waits, bounded deadlines | Determinism contract | Any new sleep-for-correctness is a defect |
| Embedded webview for 3D Studio | Not adopted | Browser-hosted viewer with loopback/token/CSP boundary | Security decision | — |
| Version numbers in product-facing docs | Retired | Version history only in `changelog.md` | Staleness discipline | Dependency/asset inventories may carry exact versions |
| Filename-as-title doc convention | Retired | Proper human titles | Professional docs standard | — |
| Separate license version identifier ("v1.1") | Retired | The Proprietary License, unversioned | Simplification | — |

---

Invariants that survive every retirement: one authority per concern;
evidence over confidence; no sleep-based correctness; honest hardware
claims; local-first posture; Proprietary License coverage of all past,
present and future proprietary material.
