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
| Qt/QML desktop host (`ui/qt/`) | Retired (v1.11.0) | Single in-process Go + Fyne app (`ui/fyne`) | One-Go-UI migration; behavioral contracts moved to Go tests | Never reintroduce a second desktop shell |
| React + Vite + Playwright frontend (`src/`, `web/`) | Retired (v1.11.0) | The Fyne UI; rich viewing via the on-demand Web Workspace | One-Go-UI migration | Do not recreate a Node/React build for static assets |
| First-generation webview desktop shell | Retired | In-process Fyne client over local IPC | Superseded twice over | Its page-origin WebSocket exception was removed with it |
| Loopback WebSocket activity transport (desktop) | Retired for desktop | `internal/localipc` (named pipe / Unix socket) | Desktop is in-process; browser/headless modes use ordinary HTTP over their own origin | Desktop must never open a network listener |
| `SHEYTAN_UI_EXECUTABLE` child-spawn resolution | Retired | In-process UI | No second UI process exists | — |
| Qt CMake / qmake packaging path | Retired | UCRT64 + NSIS + ZIP contracts (`packaging/`) | One-Go-UI migration | — |
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
