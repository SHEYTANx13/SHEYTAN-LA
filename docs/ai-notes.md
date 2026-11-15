# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-15, v1.11.1 maintenance candidate)

The branch `maintenance/v1.11.1-ci-resilience` (local, unpushed) carries
the run-43630164424 CI-resilience repair on top of the earlier Windows
IPC repair line. Baseline: `2bc66e5` on main — the red build run's
commit AND the failing CodeQL run 43630164359's head (verified same
SHA; the go-lane failure is the same unused-`sh` vet defect). Product
version stays 1.11.1 everywhere. The full evidence inventory ships in
`note.md` beside the source ZIP.

## Live architectural constraints (unchanged, load-bearing)

- **One authority per concern.** The Go core owns sessions, runs, models,
  tools, approvals, policy, logging and persistence. The Fyne UI is a
  client. Never add a second backend, UI stack or policy implementation.
- **The ONE local transport.** Every in-process client dials through
  `internal/localipc.DialContext` (Windows named pipe / Unix domain
  socket). No TCP fallback, no second dial implementation, no weakened
  DACL/peer-credential/protocol checks. Source-contract tests in
  `ui/fyne/client_source_contract_test.go` fail the build on regressions.
- **No sleep-based correctness; bounded everything.** Retries are
  deadline-bounded (the Windows busy-dial budget), event-driven waits
  only.
- **Evidence classes are `local-static`, `hosted`, `physical`.** Build
  success and a visible window prove nothing about IPC or runtime
  behavior. The hosted Windows smoke now requires an IPC handshake
  record — keep that meaning.
- **Version identity from `internal/config.AppVersion` only.** No
  version numbers in product-facing docs; no new tags without the human.

## Active blockers

- **chromedp browser runtime smoke (physical class).** The v0.23.1 +
  cdproto v0.160.3 migration is COMPLETE and suite-verified (full
  desktop sweep, 72 packages, on this Linux sandbox), but there is no
  Chrome here: browser-tool runtime behavior is NOT RUN until a
  Chrome-equipped lane exercises it. A real-browser smoke is the next
  hosted verification item.
- **Hosted verification of the independent-gates pipeline and the IPC
  gates is pending the operator's push.** No green-run claim exists for
  the candidate. Native Windows tests (named-pipe dial/listener,
  CoreClient Windows twin, PE packaging) are NOT RUN in this Linux
  sandbox — the Windows lane is their runtime authority.

## Known evidence (current)

- Run-43630164424 root causes fixed: (1) the unused `sh` at
  `ui/fyne/button_fit_test.go:234` — now a real chrome-coverage
  assertion (the sweep must reach the visible rail/strip); (2) the
  never-run rail-fit probe defect — raw 600x800 is BELOW the 600
  breakpoint after the test canvas applies theme padding (effective
  584), so the probe now uses 700x800 with a fail-loud zone guard; (3)
  the WebSocket caller-cancel race in `ui/fyne/state.go` — a canceled
  attach returned the incidental close error instead of
  `context.Canceled`; the pump now reports the context as the canonical
  cause (the Windows twin asserts the same contract).
- Build-workflow DAG: audit/ui/build-windows/build-linux have NO
  `needs: [preflight]` (own checkout/setup, same SHA); a `final-verdict`
  job fails unless every required gate concluded success; the collector
  runs on `!cancelled()` and additionally uploads `run-diagnostics.json`
  (collector `--json-out` mode). Pinned by parser-based contract tests
  (`workflow_dag_contract.go`) + scheduling fixtures wired into the
  stress gate (47/47) and the release-contract suite — the old DAG is a
  failing test. Collector offline fixtures: 25/25.
- CodeQL workflow: analyze runs on `!cancelled() &&
  steps.codeql-init.outcome == 'success'` with `upload: always`;
  `collect-codeql-logs` reuses the same collector on `!cancelled()`.
  CodeQL run 43630164359 (baseline): FAILURE — Analyze (go) exit 1 on
  the same unused-`sh` defect; exact alert counts NOT retrievable here
  (API rate-limited); the run link is in note.md for the operator.
- Unix transport: the integration suite runs green on Linux and now
  also proves the WS caller-cancel path and a procfs-inode-precise
  no-TCP-listener contract (`TestNoNetworkListenerIsOpened` runs green
  locally).
- Dependency integrity: `go mod verify` passes and `go mod tidy` is
  idempotent at chromedp v0.23.1 / cdproto v0.160.3. Fresh tag audit
  (2026-11-15): winres v0.3.1 IS the latest real published tag (the
  researched v0.3.3 does not exist); `fyne.io/systray` stays on the
  pseudo-version because Fyne v2.8.2's own graph requires it.
- Fyne button-fit: the rail grows to its content minimum, short labels
  + wrapped captions policy landed, and the matrix/chrome/rail tests
  now COMPILE AND PASS in this sandbox (the GL/CGO stack was assembled
  user-locally: dev headers extracted from Debian packages + bridged
  runtime libs). Full `go test ./ui/fyne` green; the hosted Linux lane
  remains the CI authority.
- CodeQL: the two documented actions/unpinned-tag warnings remain the
  accepted baseline under the tag-only policy; the candidate has NOT
  been scanned (unpushed) — never claim otherwise.

## Unresolved risks

- The r182→r187 Three.js refresh re-applied the vendored `generateUUID`
  CSPRNG patch; the assets are embed/compile-verified, but 3D Studio
  runtime behavior is physical class until a browser run happens.
- Dependabot security-update PRs are governed by a separate GitHub
  setting; the one-group YAML cannot cap them. The manual setting path is
  documented in `.github/dependabot.yml` and `note.md`.

## Next actionable hand-off

1. Operator reviews the candidate diff + `note.md`, pushes, and watches
   the first hosted run of the new gates (Windows integration gate, IPC
   smoke, button-fit stage).
2. Any agent continuing here should re-verify with the smallest ladder
   prefix: `gofmt -l cmd internal ui`, `go build ./...`,
   `go test -tags headless ./internal/...`, then the touched packages —
   full ladder and evidence-class rules live in [`agent.md`](agent.md).
3. If the hosted Windows lane is red, start from
   `ci-logs/win-ipc-integration.log` and `data\logs\app.log` inside the
   smoke output — the gates are named for their failure stage.

---

*Proprietary material under the Proprietary License (see
[`../LICENSE.md`](../LICENSE.md)).*
