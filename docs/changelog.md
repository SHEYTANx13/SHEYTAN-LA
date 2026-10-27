# Changelog

**This is the only canonical release-history artifact.**  
The README describes the current product; per-release evidence lives in the release tags and the tests that shipped with them.

Every release ships with the same invariants: one authority per concern, evidence over confidence, no sleep-based correctness, honest hardware claims, codename gate enabled.

---

## 1.11.0 — 2026-10-26 (extended 2026-10-27)

**One-Go-UI (Fyne) complete + hybrid Web Workspace + CI hardening + optional 3D Studio**

- Full migration to single Go + Fyne desktop (Qt/QML and React/Vite fully retired).
- Responsive three-zone shell (wide / medium / narrow).
- Optional 3D Studio: read-only live topology viewer (loopback-only, per-launch token, strict CSP, embedded Three.js).
- Hybrid rendering surfaces (v1.11.0 extension): typed presentation contract (`internal/presentation` — versioned immutable blocks, deterministic renderer selection, bounded payloads, required provenance, unknown-kind degradation) and the on-demand Web Workspace (`internal/webworkspace` — loopback read-only rich-content renderer, per-launch fragment token, strict CSP, offline assets, zero browser-originating commands), with the Fyne control view and chat publish path.
- Boot visibility contract: the desktop window is built and shown before core setup completes; named per-step boot timings (fixes the run #55 "live process, no window" smoke failure class).
- Consolidated one-file CI run-log artifact on every run.
- Windows pipeline migrated to MSYS2 UCRT64.
- Stress gate made headless-safe; CGO-free version resolver (`cmd/printversion`).
- Launch smokes upgraded to bounded observable readiness + clean shutdown.
- Fyne threading contract enforced; zero racy UI writes.
- Last live traces of the retired first-generation webview shell removed from the tracked tree (origin-gate branch, comments, docs); regression scan is zero-match.
- Device-enumeration evidence freshness: bounded failure window for failed capability probes, boot-time cache invalidation, explicit refresh through the canonical probe.
- Release-contract hygiene restored: gofmt drift and the LICENSE.md ↔ brand generator drift fixed at the contract's designed direction.

**Regression (local)**: full headless suite green (all `internal/...`), race gates green on the touched concurrent packages, release/version/license contracts green, collector fixtures 9/9, YAML parse green. Hosted verification of the final commit pending operator push.

---

## Prior releases (summary)

| Version | Focus |
|---------|-------|
| 1.10.x  | Named-pipe transport hardening, Governor admission, execution/evidence ladder, busy-dial budget, visibility system, packaging contracts |
| 1.9.x   | AI System Builder, Goal engine, approval authority, native-engine forward path, measurement framework |
| 1.8.x   | Runtime Governor, dataAnalysis authority, reasoning-depth budgets, session-list generation guard, zero-session Send |
| ≤ 1.7   | Core runtime, skills, Coding Lab, scheduler, first desktop shells |

Detailed per-release narratives for versions before 1.11.0 are preserved in the git history of this file.
