# Changelog

**This is the only canonical release-history artifact.**  
The README describes the current product; per-release evidence lives in the release tags and the tests that shipped with them.

Every release ships with the same invariants: one authority per concern, evidence over confidence, no sleep-based correctness, honest hardware claims, codename gate enabled.

---

## 1.11.0 — 2026-10-26

**One-Go-UI (Fyne) complete + CI hardening + optional 3D Studio**

- Full migration to single Go + Fyne desktop (Qt/QML and React/Vite fully retired).
- Responsive three-zone shell (wide / medium / narrow).
- Optional 3D Studio: read-only live topology viewer (loopback-only, per-launch token, strict CSP, embedded Three.js).
- Consolidated one-file CI run-log artifact on every run.
- Windows pipeline migrated to MSYS2 UCRT64.
- Stress gate made headless-safe; CGO-free version resolver (`cmd/printversion`).
- Launch smokes upgraded to bounded observable readiness + clean shutdown.
- Fyne threading contract enforced; zero racy UI writes.

**Regression (local)**: full test suite green, stress 47/47, Xvfb desktop smoke clean. Hosted verification of the final commit pending operator push.

---

## Prior releases (summary)

| Version | Focus |
|---------|-------|
| 1.10.x  | Named-pipe transport hardening, Governor admission, execution/evidence ladder, busy-dial budget, visibility system, packaging contracts |
| 1.9.x   | AI System Builder, Goal engine, approval authority, native-engine forward path, measurement framework |
| 1.8.x   | Runtime Governor, dataAnalysis authority, reasoning-depth budgets, session-list generation guard, zero-session Send |
| ≤ 1.7   | Core runtime, skills, Coding Lab, scheduler, first desktop shells |

Detailed per-release narratives for versions before 1.11.0 are preserved in the git history of this file.
