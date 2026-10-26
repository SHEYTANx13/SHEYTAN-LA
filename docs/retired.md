# Retired Mechanisms

This document records major functions, subsystems, and mechanisms that were previously part of SHEYTAN-LA and have been retired, replaced, or removed from the active product surface.

**All material listed here remains under the Proprietary License** (see `LICENSE.md`). Retirement from the active tree does not release any intellectual-property rights. Code that exists only in git history is still covered by the same license terms.

---

## UI shells (retired)

| Mechanism | Status | Notes |
|-----------|--------|-------|
| Qt / QML desktop host (`ui/qt/`) | Fully retired | Replaced by the single Go + Fyne application (`ui/fyne`). All behavioral contracts were ported to Go tests. |
| Wails desktop shell | Fully retired | Earlier webview-based desktop host. Removed before the Qt era. |
| React + Vite + Playwright frontend (`src/`, `web/`) | Fully retired | Embedded web frontend used for headless/browser mode and early desktop experiments. No longer present in the tree or CI. |
| Loopback WebSocket activity transport | Retired for desktop | Previously used by Wails/Qt-era shells. Desktop now uses pure local IPC; browser/headless mode still uses ordinary HTTP. |

---

## Build & packaging (retired)

| Mechanism | Status | Notes |
|-----------|--------|-------|
| MSYS2 MINGW64 toolchain path | Retired | Windows CI and local builds now use UCRT64 exclusively. |
| Qt CMake / qmake packaging path | Fully retired | No longer present after one-Go-UI migration. |
| `SHEYTAN_UI_EXECUTABLE` child-spawn resolution | Fully retired | Desktop is now in-process; no separate UI executable is spawned. |
| Older NSIS / ZIP layout assumptions | Superseded | Current packaging contracts are the sole authority. |

---

## Runtime & engine (retired or superseded)

| Mechanism | Status | Notes |
|-----------|--------|-------|
| Silent CPU fallback for explicit GPU variant requests | Retired | Explicit variant requests never fall back silently. |
| Fixed-attempt busy-dial on named pipes | Superseded | Replaced by deadline-bounded busy-dial budget. |
| Sleep-based readiness / correctness tests | Retired | All correctness is deterministic and event-driven. |
| Multiple competing tool registries or session stores | Never allowed | One-authority rule has always been enforced; any experimental duplicates were removed. |
| Embedded webview for 3D Studio | Not adopted | Documented decision: browser-hosted viewer with strict security boundary instead. |

---

## Documentation & process (retired)

| Mechanism | Status | Notes |
|-----------|--------|-------|
| Version numbers in product-facing docs (README, etc.) | Retired | Version history lives only in `changelog.md`. |
| Filename-as-title convention (e.g. “SECURITY.md”) | Retired | All docs now use proper human titles. |
| Separate license version identifier (“v1.1”) | Retired | License is simply the Proprietary License. |
| Long historical narratives in `changelog.md` / `worklog.md` | Minimized | Essential current entries kept; full history remains in git. |

---

## Principles that remain

Even after retirement of the mechanisms above, the following invariants continue to apply to all current and future code:

- One authority per concern
- Evidence over confidence
- No sleep-based correctness
- Honest hardware claims (detection ≠ execution)
- Local-first posture
- Proprietary License coverage of all past, present, and future proprietary material

---

*This file is itself proprietary material under the Proprietary License.*
