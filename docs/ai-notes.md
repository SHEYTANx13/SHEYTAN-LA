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

### Session — 2026-10-27 (v1.11.0 repair/upgrade, 113-minute budget)

**Human prompt summary**
- Hybrid Fyne + Web Workspace architecture, AI-system-aware UI, llama.cpp/Vulkan observability, CI/log/run-#55 repair, full retired-webview-shell (banned-name) removal, v1.11.0 document coherence, auditable source ZIP. No push/PR/tag/release by the agent.

**Success condition**
- A minimal real end-to-end vertical slice (Go presentation contract → Web Workspace rendering → Fyne control view) passes tests, existing functionality preserved, unresolved items documented honestly, verified archive produced.

**Plan (executed)**
- [x] Orient: all tracked docs read at HEAD `28d865f3` (main); version authority = `internal/config.AppVersion`.
- [x] Failures audit: GitHub REST API rate-limited this session (shared IP) — hosted logs/artifacts for run #55 (40410335100) could NOT be retrieved (limitation, not guessed). Local reproduction found instead: HEAD's `internal/research/service.go` failed the enforced gofmt gate under the Go 1.26 toolchain (preflight would be red).
- [x] Architecture research: embedded WebView host NOT adopted (packaging/verification risk on both desktop targets, same documented decision as 3D Studio); browser-hosted on-demand Workspace adopted, mirroring the audited Studio security boundary.
- [x] `internal/presentation` (NEW): versioned envelope (v1), immutable blocks, stable IDs, closed kind set, bounded payloads (256 KiB), required provenance, deterministic `Select(kind)` renderer policy, unknown-kind degradation to raw data, `ExtractBlocks` deterministic splitter. 13 tests.
- [x] `internal/webworkspace` (NEW): loopback-only ephemeral host, per-launch 256-bit fragment token, strict CSP + nosniff/no-referrer/DENY, allow-listed embedded offline assets (page.html/app.js/app.css), GET-only, Bearer-gated `/api/blocks`, `textContent`-only DOM (asset itself grepped for markup-injecting sinks — enforced by test), zero browser-originating actions. 9 tests incl. token/401/404/405/CSP/degradation.
- [x] API wiring: `/api/presentation/{state,start,stop,blocks,publish}`; `WebWorkspaceEnabled` config (default on, Settings row added); host stopped at `Server.Close()` (no orphan listener); ONE registry shared by both surfaces.
- [x] Fyne: `Web Workspace` nav view (9th entry), client methods, "Publish current answer" (typed blocks from the settled stream), fake-core surface for tests; Prepare contract test.
- [x] Boot visibility (run #55 class fix): `desktop.Run` now shows the window BEFORE `EnsureSetup` (named per-step boot timings); `App.Prepare()`/`Run()` split in `ui/fyne/app.go`. Root cause of run #55 remains a HYPOTHESIS (window-after-slow-boot class) — hosted logs were not retrievable; the fix removes the entire class regardless of which step was slow.
- [x] llama.cpp/Vulkan observability: device-enumeration cache — failed probes now expire after a bounded failure TTL (5 min) instead of poisoning the cache for the 24 h success TTL; boot invalidates the cache (per-boot evidence contract); `InvalidateDeviceCache` exported for explicit refresh; probe seam restructured so cache tests exercise the REAL cache. 5 new tests + full `internal/llm` suite green.
- [x] Retired-shell removal: all 21 live references removed (code comments, `ws.go` origin-gate branch — also a security tightening, `.gitignore`, `build/config.yml`, `docs/retired.md` neutralized). Fragmented-token scan: **0 matches** tracked + working tree. go.mod/go.sum needed no changes.
- [x] Pre-existing reds fixed: gofmt drift (`internal/research/service.go`); LICENSE.md ↔ `brand.LicenseText` contract drift restored via the designed generator (gen-license writes brand → LICENSE.md; the recent human LICENSE.md edits had never been mirrored into the brand authority — see Risks).
- [x] Docs reconciled: README (hybrid surfaces + boot contract), ARCHITECTURE (§3 rewritten: hybrid surfaces, §3.1 presentation contract, §3.2 Web Workspace), FEATURES (implemented items), ROADMAP (delivered boundary), 3dstudio (workspace separation note), retired.md (neutral), changelog (v1.11.0 entry extended, single header), worklog (current focus). Dev-Notes untouched.

**Evidence**
- class: local-static (compile + unit tests on Go 1.26.0 linux/amd64); hosted: NOT RUN (operator pushes); physical: NOT RUN.
- Commands/results: `go build -tags headless ./...` PASS; `go build ./...` PASS (pure-Go packages; `ui/fyne` compile NOT RUN locally — no X11/GL headers in this container, no root for prerequisites); `go test -tags headless ./internal/...` PASS (full sweep); `go test -race ./internal/presentation ./internal/webworkspace` PASS; `go test ./internal/api` PASS (120 s); `./internal/llm` PASS (66 s); `./internal/research`, `./internal/releasecontract`, `./internal/releasegate`, `./cmd/printversion` PASS; collector fixtures 9/9 PASS; workflow YAML parse PASS; fragmented banned-name scan 0/0 PASS; `gofmt -l cmd internal ui` clean; version chain (`printversion --plain/--json`, `build/config.yml`, `SIGNATURE`, release contract) PASS.

**Tests NOT RUN (honest)**
- `ui/fyne` package tests + desktop binary compile (needs Fyne graphics prerequisites — hosted CI runs them).
- Windows UCRT64 build/smoke/installer, Linux Xvfb window smoke, fresh-extract build smoke — hosted gates only.
- Physical: window DPI/GPU behaviors, real GPU Vulkan offload, WebGL rendering.
- Hosted run-#55 log retrieval (GitHub API rate-limited) — root cause class fixed locally, hosted confirmation pending.

**Docs touched**
- README.md, docs/ARCHITECTURE.md, docs/FEATURES.md, docs/ROADMAP.md, docs/3dstudio.md, docs/retired.md, docs/changelog.md, docs/worklog.md, docs/ai-notes.md (this file). Dev-Notes.md untouched (human-only).

**Hand-off**
- Done: see Plan. Archive: `SHEYTAN-LA-v1.11.0-FIXED-HOSTED-UNVERIFIED.zip` (hosted acceptance not proven → FINAL name not used).
- Remaining: hosted matrix green on the pushed commit; `ui/fyne` hosted suite incl. the new view/Prepare tests; physical DPI/GPU checks remain open items (Dev-Notes).
- Recommended next: operator reviews diff → push → watch run → if green on the exact commit, re-label hosted evidence and consider the FINAL archive; re-run the fragmented-token scan at push time.
- Risks: (1) `ui/fyne` new code compiled only by inspection + gofmt parse — hosted UI suite is the real gate; (2) LICENSE.md regenerated from `brand.LicenseText` (contract direction) — the human's recent dual-licensor LICENSE.md wording was NOT mirrored into the brand authority before the drift; re-apply that language through `internal/brand` + regenerate deliberately if preferred; (3) run #55 root cause is a class fix, not a log-confirmed diagnosis; (4) Go toolchain in CI must stay ≥ 1.26 (gofmt rules).

---

*This file is proprietary material under the Proprietary License.*
