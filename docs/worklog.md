# Worklog

Session log for engineering work on SHEYTAN-LA.  
Architecture truth → `ARCHITECTURE.md`  
Release history → `changelog.md`  
Direction → `ROADMAP.md`

---

## Current focus (2026-11-13 / 1.11.1 maintenance pass)

**Run 43008361835 (run #144) reliability/UX maintenance work order**

- Root-caused the run's final `failure` conclusion: all five build/test/package jobs passed; the Consolidate logs job failed ONLY at its "Collector verdict" step because the collector counted its own (necessarily in-progress) job as a coverage gap. Fixed with a dedicated SELF accounting category; pinned by fixtures.
- Startup phase telemetry (internal/startup), one idempotent shutdown coordinator (ui/fyne/lifecycle.go), responsive size-class contract 600/900/1280 with the full logical-size matrix + structural goldens + a real Xvfb resize smoke, per-job diagnostics-index.json, setup-go cache scoping with recorded hit/miss evidence.
- Full detail: docs/changelog.md (1.11.1 maintenance pass) and docs/ui-contract.md contracts #12/#13.

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
