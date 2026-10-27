# Worklog

Session log for engineering work on SHEYTAN-LA.  
Architecture truth → `ARCHITECTURE.md`  
Release history → `changelog.md`  
Direction → `ROADMAP.md`

---

## Current focus (2026-10-27 / 1.11.0)

**Hybrid UI + repair session (this session)**
- Web Workspace shipped: `internal/presentation` (typed block contract) + `internal/webworkspace` (loopback read-only renderer) + Fyne control view and chat publish path.
- Boot visibility contract: window shown before core setup; per-step boot timings (run #55 class).
- All live retired-webview-shell references removed (origin-gate branch, comments, docs); regression scan green.
- Device-enumeration cache: bounded failure TTL + boot-time invalidation + explicit refresh seam.
- Pre-existing reds fixed: gofmt drift in `internal/research/service.go`; LICENSE.md ↔ brand contract drift restored through the designed generator.

---

## Previous focus (2026-10-26 / 1.11.0)

**Documentation overhaul (this session)**  
- Full rewrite of README, SECURITY, ROADMAP, FEATURES, CONTRIBUTING, agent, UPDATE, 3dstudio, ARCHITECTURE.  
- Removed all historical version numbers from product-facing docs.  
- Minimized changelog and worklog to essential current state.  
- All titles are proper document titles (no filename-as-title).  
- Reflects the completed one-Go + Fyne reality and current product surface.

**Product state at HEAD**  
- One Go process + in-process Fyne UI over secured local IPC.  
- Optional 3D Studio.  
- Runtime Governor, Goal engine, AI System Builder, dataAnalysis authority, execution/evidence ladder all in place.  
- CI: UCRT64 Windows, Xvfb Linux desktop smoke, consolidated run-log artifact, headless-safe stress gate.

---

## Recent cycles (summary)

| Task | Outcome |
|------|---------|
| 48   | CI root-cause fixes (stress headless, printversion, BUILD-INFO, consolidated logs, UCRT64, responsive shell) |
| 47   | Workflow parse fix + 3D Studio shipped + Fyne threading contract |
| 46–45| Path / link / configure class fixes during late Qt retirement |
| ≤ 44 | One-Go-UI migration, transport hardening, Governor, Goal engine, packaging contracts |

Full historical narratives remain in the git history of this file.
