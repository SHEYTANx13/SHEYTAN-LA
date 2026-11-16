# Requirements Traceability — Stability & Advanced UI/UX Reports → Code

> Provenance note (recorded honestly): the two source reports
> (`SHEYТAN-LA-Stability-and-Roadmap.md`,
> `SHEYTAN-LA-Advanced-UI-UX.md`) were **not attached** to the 2026-11-16
> integrity-first milestone session; this matrix is built from the
> requirement-area enumeration of those reports as bound into the
> milestone work order (its §13), and each row was verified against the
> actual repository at commit `124b08a` (branch
> `work/v1.11.1-integrity-stability-uiux` for the milestone changes).
> When a report's full text becomes available, rows must be re-verified
> against it — that re-verification is an explicit open item, not a
> completed one.

Statuses (exact set): `IMPLEMENTED_AND_TESTED`, `EXISTING_AND_VERIFIED`,
`FOUNDATION_IMPLEMENTED`, `STAGED_WITH_GATE`, `BLOCKED`.

## Stability report (SHEYТAN-LA-Stability-and-Roadmap.md)

| Report § | Requirement area | Discovered code path / evidence | Status | Owning test(s) | Doc destination | Phase |
|---|---|---|---|---|---|---|
| §1–2 | Product boundary, repository audit, preservation | Repo map intact: Go core + Fyne client + managed llama.cpp/native engine; no authority duplicated (`docs/ARCHITECTURE.md` authority table) | EXISTING_AND_VERIFIED | full test gates (see note.md verification matrix) | ARCHITECTURE.md | Now |
| §3–4 | Native UX, desktop/tablet/phone, design tokens, a11y, RTL | `ui/fyne` responsive size-class policy, button-fit matrix, structural goldens, `ui_marshal.go` threading seam, theme tokens; five-platform rollout staged | EXISTING_AND_VERIFIED (supported UI) / STAGED_WITH_GATE (five-platform) | `TestResponsiveViewportMatrix`, `TestButtonFitMatrix`, `TestShellChromeButtonsFit` | `docs/ui-contract.md` | Supported UI now; five-platform staged |
| §5 | Module boundaries: core UI-framework independent | **Zero `fyne.io/fyne` imports under `internal/` and `cmd/`** (verified by search 2026-11-16); UI adapters observe state and call the local API | EXISTING_AND_VERIFIED | `go vet ./...`, `internal/...` headless gates | ARCHITECTURE.md | Now |
| §6 | Generative UI and artifacts | `internal/presentation` (typed store + validation), `internal/studio` / `internal/workspace` as read-only viewers over Go-owned data; bytes separate from conversation JSON | EXISTING_AND_VERIFIED | `presentation_test.go`, `viewer_test.go`, `server_test.go` | CONTENT-INTEGRITY.md §4 | Foundation now; modality expansion staged |
| §7 | SPCM (personal compute mesh) | No mesh code (correctly absent); boundary documented as future work with security invariants | STAGED_WITH_GATE | n/a (no code — by design) | ROADMAP.md | LAN MVP later |
| §8 | Offline inference; detected/available/selected/executed/verified GPU states | `internal/llm` (capabilities, devices, admission), `internal/native/engine`, `internal/accelerator` (execution-truth contract) | EXISTING_AND_VERIFIED | `execution_truth_v186_test.go`, `devices_missing_test.go`, `gpu_activation_v186_test.go` | engine/platform docs | Existing verified; new runtimes staged |
| §9 | Security/reliability/data ownership/self-improvement | Approval authority (`internal/approval`), os.Root path safety across stores, atomic writes, idempotent events, migrations; **v1.11.1 adds recoverable session delete/restore/purge** | IMPLEMENTED_AND_TESTED (sessions class) + EXISTING_AND_VERIFIED (controls) | `trash_test.go` (9), `sessions_trash_api_test.go`, pathsafety suites | CONTENT-INTEGRITY.md | Core controls now; full sync later |
| §10 | WebAppHost and Godot/full 3D as separate runtimes | `internal/workspace` (loopback, token-gated, CSP-strict, zero commands) and `internal/studio` (read-only three.js viewer) are exactly that boundary; no embedded browser in the shell | EXISTING_AND_VERIFIED (boundary) / STAGED_WITH_GATE (runtimes) | `workspace/server_test.go`, security suites | ARCHITECTURE.md, ROADMAP.md | Interfaces now; runtimes later |
| §11–13 | Roadmap, DoD, CI/release/dependencies/update rollback | `internal/releasecontract` (version, workflow-DAG, artifact-naming, packagegate, installer, oversized-asset contracts), independent CI gates, `internal/updater` (staged install, rollback identity) | EXISTING_AND_VERIFIED | `releasecontract` suite (green 2026-11-16), `rollback_identity_v181_test.go` | ROADMAP.md, changelog.md | CI/release safety now; five-platform release staged |
| §14–16 | Risks and agent contract; no-main, no-hacks, no false runtime claims | `docs/agent.md` (non-negotiable rules), Dev-Notes governance, evidence-class discipline (`local-static`/`hosted`/`physical`) | EXISTING_AND_VERIFIED | the gates themselves + agent manual | agent.md, Dev-Notes.md | Now |

## Advanced UI/UX report (SHEYTAN-LA-Advanced-UI-UX.md)

| Report § | Requirement area | Discovered code path / evidence | Status | Owning test(s) | Doc destination | Phase |
|---|---|---|---|---|---|---|
| §1–4 | Renderer strategy; Go-Gui candidate; Gio contingency; native-Go rule | Fyne v2.8.2 production (`ui/fyne`, pinned, hosted gate green); **Go-Gui v0.96.0 POC (v1.11.1)** under `experiments/ui-engine-poc/go-gui` (nested module, 5/5 headless app tests + screenshot); Gio not started (contingency only) | FOUNDATION_IMPLEMENTED (POC) | POC `main_test.go` (5 PASS), `go test ./ui/fyne/` gate | UI-PLATFORM-AUDIT.md, UI-ENGINE-DECISION.md | POC now; switch gated |
| §5 | Generative UI schema/catalog/validation | `internal/presentation` typed store; no model-supplied code/callbacks anywhere | EXISTING_AND_VERIFIED | `presentation_test.go` | CONTENT-INTEGRITY.md §4 | Foundation now |
| §6 | Event/stream lifecycle: IDs, sequence, dedup, replay | `internal/api/ws.go` hub (conflation-tested), run transport/settlement, chat settle order | EXISTING_AND_VERIFIED | `hub_conflation_v185_test.go`, `run_settlement_test.go`, `chat_settle_order_test.go` | api docs | Hardened now |
| §7 | Provider capability registry; manual choice preserved | `internal/llm/capabilities.go`, `modelcaps.go`, `internal/recommendation` (evidence-based, AUTO-only bias; manual untouched) | EXISTING_AND_VERIFIED | `capabilities_test.go`, `preferfastest_v1111_test.go` | capability docs | Now |
| §8 | Context/retrieval/memory provenance and control | `internal/memory` (trust boundary), `internal/recall`, `internal/contextcache`, `internal/histref`, `internal/contextplan` | EXISTING_AND_VERIFIED | `trust_test.go`, `recall_test.go`, `contextplan` suites | CONTENT-INTEGRITY.md §4 | Mutation safety now |
| §9 | Multimodal artifacts: metadata+blob separation, bounded decode | `internal/artifacts`, `internal/tools` (staged tx), `internal/vision`, `internal/attachments` | EXISTING_AND_VERIFIED (core) / STAGED_WITH_GATE (new decoders) | `archive_staged_tx_test.go`, `artifacts_test.go`, `vision` suites | CONTENT-INTEGRITY.md §4 | Store foundations now |
| §10 | 3D scenes/games as separate validated runtime | `internal/studio` read-only viewer only; no scene-script execution | STAGED_WITH_GATE (runtime) / EXISTING_AND_VERIFIED (boundary) | `viewer_test.go` | ROADMAP.md | Staged |
| §11 | Agent UI/source improvement: preview/accept/revert, isolated worktree | `internal/lab` (safeedit, verifier, repair, pathjail) | EXISTING_AND_VERIFIED | `safeedit_test.go`, `repair_test.go`, `pathjail_v1111_test.go` | CONTENT-INTEGRITY.md §4 | Safety contract now |
| §12 | Tools/plugins/MCP/computer use: typed schemas, authorization | `internal/customtools` (SSRF policy + dialer guard), `internal/tools`, `internal/mcp`, `internal/computer`, `internal/sandbox` | EXISTING_AND_VERIFIED (core) / STAGED_WITH_GATE (plugin market) | `ssrf_policy_test.go`, `ssrf_dialer_guard_test.go`, `customtools` suites | SECURITY.md | Existing tool safety now |
| §13 | Security/privacy: injection containment, secure storage, SSRF/path safety | `internal/logging/redact.go`, SSRF allowlist, os.Root path safety across stores, prompt-injection tests | EXISTING_AND_VERIFIED | `redact_test.go`, pathsafety suites (9 packages), `goals_log_injection_v1111_test.go` | SECURITY.md, RUNTIME-DIAGNOSTICS.md | Now |
| §14 | Design system, accessibility, i18n, RTL, reduced motion | `ui/fyne/theme.go` tokens, `ui-contract.md` (15 landed contract items), Persian/RTL content paths exercised in tests; full AT tree coverage NOT claimed (Fyne a11y is early opt-in) | EXISTING_AND_VERIFIED (existing controls) / STAGED_WITH_GATE (full AT matrix) | `ui/fyne` suites incl. goldens | ui-contract.md | Existing UI now |
| §15–17 | Concurrency, frame budgets, UI-thread ownership, benchmarks | `ui_marshal.go` (the ONE fyne.Do seam, test-mode serialization), bounded workers, startup phase telemetry (`internal/startup`), bench tests in core packages; P50/P95/P99 UI frame stats NOT measured in this pass | EXISTING_AND_VERIFIED (ownership) / STAGED_WITH_GATE (frame-time percentiles) | `workspace_hub_test.go`, `lifecycle_test.go`, startup tests | UI-ENGINE-DECISION.md, RUNTIME-DIAGNOSTICS.md | Ownership now; measurement staged |
| §18–21 | Implementation order, pinned audited dependencies, deliverables, truthful milestone report | go.mod: every direct dep pinned at audited tags (Fyne v2.8.2, x/sys v0.50.0, …); `go mod verify` + tidy idempotent; delivery = source ZIP + completed note.md | EXISTING_AND_VERIFIED | `go mod verify`, version purity tests, releasecontract | changelog.md, note.md | Now |

## Open verification items (never marked done without evidence)

1. **Source-report full-text cross-check** — BLOCKED (reports not
   attached to this session; matrix built from the work order's §13
   enumeration).
2. **Code Scanning live alert inventory** — BLOCKED (unauthenticated
   GitHub API rate-limited from the execution sandbox; no token
   available). See `download/code-scanning-alert-inventory.md`.
3. **Go-Gui platform gates beyond the headless POC** — STAGED_WITH_GATE
   (accessibility trees, IME/CJK, native dialogs, physical devices).
4. **Trash/restore for non-session content classes** — STAGED_WITH_GATE.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
