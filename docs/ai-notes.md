# AI Notes

**This is the only file AI agents may freely update for working notes, progress, and session state.**

`docs/Dev-Notes.md` is reserved for the human developer and must not be edited by AI agents unless the human explicitly instructs it in the current prompt.

---

## Purpose

- Capture the current agent’s plan, progress, evidence, and hand-off notes.
- Avoid polluting the human-curated `Dev-Notes.md`.
- Give the next agent (or the human) a clear, short, up-to-date picture of what was done and what remains.

## Rules for AI agents

1. Read `docs/Dev-Notes.md` (human source of truth for open items) at the start of every session.
2. Write all of your own working notes, checklists, evidence logs, and status updates **only** in this file (`ai-notes.md`).
3. Keep entries short, structured, and current. Overwrite or clearly mark outdated sections instead of letting the file grow indefinitely.
4. At the end of a session, leave a clean **Hand-off** section so the next agent or the human can continue without re-deriving context.
5. Do not treat this file as a second roadmap or architecture document. Point to the real docs instead of duplicating them.

---

## Current session template (copy and fill)

```markdown
### Session — YYYY-MM-DD HH:MM (agent / model)

**Human prompt summary**
- …

**Success condition**
- …

**Plan**
- [ ] …

**Progress**
- …

**Evidence**
- class: local-static | hosted | physical
- commands / tests: …

**Docs touched**
- …

**Hand-off**
- Done: …
- Remaining: …
- Recommended next: …
- Risks: …
```

---

## Latest hand-off

### Session — 2026-10-28 (autonomous agent, Z.ai/GLM)

**Human prompt summary**
- v1.11.0 repair/upgrade in a fixed timebox: hybrid Fyne + Web Workspace presentation architecture, AI-system-aware UI preserved, llama.cpp/Vulkan observability, CI/log/run-#55 verification, complete retirement-scan cleanup of the first-generation webview desktop shell's name, one version authority, reconciled docs, auditable source ZIP. No push/PR/tag/release by the agent.

**Success condition**
- Minimal but real end-to-end vertical slice passing tests, existing functionality preserved, unresolved items documented honestly, verified archive delivered.

**Plan (as executed)**
- Orient (all tracked docs read) → reproduce/limit classify run #55 → retirement-scan cleanup → version authority check → presentation contract → Web Workspace server+assets → Fyne wiring → accelerator reason codes → Windows smoke hardening → docs → verification → ZIP.

**Progress (all shipped)**
- Retirement scan: 24 matches in 10 tracked files → 0 (incl. LIVE page-origin exception removed from `internal/api/ws.go` `allowedOrigin`); `retired.md` rows neutralized; comments/docs/.gitignore/config.yml updated. Regression gate added: `internal/releasecontract/retirement_scan_test.go` (banned name built from split fragments at runtime; walks internal/cmd/scripts/.github/build/ui/docs; ≥100-file scope guard).
- Version authority: `internal/config.AppVersion` = 1.11.0 confirmed THE authority; `TestReleaseVersionSingleSource` PASS (config.yml productVersion + SIGNATURE agree).
- Presentation contract: NEW `internal/presentation` — envelope v1 (9 kinds + unknown-degrade), bounded payloads, typed errors, deterministic `SelectSurface`, bounded upsert store (one ID = one logical artifact), race-tested.
- Web Workspace: NEW `internal/workspace` — loopback-only ephemeral bind, per-launch 256-bit fragment token (never in page HTML/state), strict CSP/nosniff/no-referrer/DENY, allow-listed embedded offline assets (view/index.html + workspace.js + workspace.css), GET-only read endpoints (`/api/workspace/blocks`, `/api/workspace/block?id=`), no command endpoints, all DOM text via textContent, unknown kinds → raw-data degrade, loading/empty/error states, safe links (http/https/mailto only). JS asset contract test bans markup-injection primitives.
- Fyne wiring: `ui/fyne/workspace_hub.go` (hub = store + server owner); App owns hub, stops it at window close; chat view registers settled assistant responses as markdown blocks (content-addressed ID) and offers the "Workspace" button; failure paths surface in the native shell and never touch native content.
- Accelerator: typed `ReasonCode` taxonomy on `Resolution` (7 codes, wire values pinned by test; code never upgrades evidence state); existing detection-vs-execution semantics unchanged.
- CI: Windows launch smoke hardened — dual observation path (MainWindowHandle OR EnumWindows-by-PID visible-window check), app stdout/stderr captured to an uploaded artifact, failure diagnostics (candidate windows, process snapshot, app log tails), 60s budget unchanged; YAML validated (7 jobs parse); all 9 collector fixtures PASS.
- Docs: README (hybrid paragraph + capability row), ARCHITECTURE (§3.1 hybrid presentation, presentation-blocks authority row, typed causes), FEATURES (3 rows), changelog v1.11.0 entry extended (single header), retired.md neutralized. Dev-Notes untouched.
- Pre-existing red gate FIXED: `TestLicenseMD*` failed at pristine HEAD — the doc overhaul had replaced LICENSE.md's full legal texts with external references while the release-contract gate (and CONTRIBUTING's promise) requires them. Restored the complete full-text LICENSE.md exactly as shipped in the v1.11.0 tag commit (e6e1968) — gate content unchanged, no test weakened.

**Evidence**
- class: local-static + local-runtime (Linux Xvfb); hosted: NOT RUN (operator push required). GitHub API 403 (rate limit) — run 40410335100 logs NOT retrievable from this environment; classification: limitation, not permission to guess.
- `go build -tags headless ./...` PASS · `go build ./ui/fyne/` + full desktop binary build PASS (Fyne CGO stack via local dev-lib prefix)
- `go test -race ./internal/presentation/` PASS · `go test -race ./internal/workspace/` PASS · `go test ./internal/api/` PASS · `go test ./internal/accelerator/` PASS · `go test ./ui/fyne/` PASS (full suite incl. new hub tests) · release contract PASS · collector fixtures 9/9 PASS
- Xvfb desktop smoke (real binary, this Linux host): window "SHEYTAN-Local-Agent" observed in the X tree, 8s soak alive, clean SIGTERM exit — local-runtime evidence, not hosted, not physical-Windows.
- gofmt -l on changed files: clean (pre-existing `internal/research/service.go` gofmt-1.26 drift left untouched — hosted CI's older gofmt accepts it; NOT my change).
- `go vet` on changed packages: PASS (presentation/workspace/accelerator/releasecontract/ui/fyne).

**Tests NOT RUN (honest)**
- Hosted Windows/Linux CI matrix (operator push required). Hosted re-check of run #55. Windows physical window smoke (EnumWindows path compiled-in but not executed here). `go test ./...` FULL sweep (timeboxed subset above; remaining packages unchanged or covered by headless api sweep). Race on ui/fyne (harness constraint here; suite ran without -race). WebGL/browser rendering of the Workspace page (no browser pair in this environment — Go-side contract fully tested; mirrors the 3D Studio honesty posture). Stress/release-surface gates (unchanged by this session; covered in CI).

**Docs touched**
- README.md, docs/ARCHITECTURE.md, docs/FEATURES.md, docs/changelog.md, docs/retired.md, docs/ai-notes.md (this hand-off). Dev-Notes.md NOT touched.

**Hand-off**
- Done: everything in Progress above; archive SHEYTAN-LA-v1.11.0-FIXED-HOSTED-UNVERIFIED.zip prepared per contract (single SHEYTAN-LA/ root, no .git/secrets/build outputs).
- Remaining: operator push → hosted Windows+Linux matrix → if green on the exact commit, rename decision to FINAL per the release contract; physical DPI/GPU verification remains open as before.
- Recommended next: (1) operator push + watch the Windows launch smoke (now diagnosable); (2) confirm run #55 class failure reproduces or clears with the new dual-observation diagnostics; (3) consider a workspace "actions" surface only as a typed, Go-authorized, security-reviewed follow-up.
- Risks: hosted Windows smoke may still fail if the runner session genuinely cannot map a visible window — the hardened step will now SHOW the real cause instead of a bare timeout (that is the intended behavior: diagnosis over timeout-extension). Browser-side JS rendering of complex markdown is intentionally minimal (headings/bold/italic/code/fences/lists/links); math/citations are future work and must go through the typed capabilities field, not ad-hoc HTML.

---

*This file is proprietary material under the Proprietary License.*
