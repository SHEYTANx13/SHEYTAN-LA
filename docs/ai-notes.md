# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-16, v1.11.1 maintenance candidate)

The branch `maintenance/v1.11.1-run43718750096` (local, unpushed) carries
the run-43718750096 Windows-repair candidate. Baseline: `94a97de` on
main — the failing build run's commit AND the successful CodeQL run
43718750102's head (same SHA, branch main). The previous candidate from
`2bc66e5` WAS published and hosted-verified as designed: its pipeline
executed all independent gates (run 43718750096 — preflight, Core audit,
Fyne UI, Linux x64 green; Windows x64 red on exactly two failing steps,
final verdict red as designed), and CodeQL 43718750102 completed
all-success. This candidate repairs the four Windows headless test
failures exposed by that run and adds the §7.3 100 MB package-size
gate. Product version stays 1.11.1 everywhere. The full evidence
inventory ships in `note.md` beside the source ZIP.

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

- **Hosted verification of the four repaired Windows tests and the new
  size gates is pending the operator's push.** No green-run claim
  exists for THIS candidate. Native Windows execution (named-pipe
  busy-dial fixtures, release-contract fixtures, PE packaging) is NOT
  RUN in this Linux sandbox — the Windows lane is their runtime
  authority. Windows headless cross-vet and test-binary compile are
  clean locally (`GOOS=windows go vet -tags headless ./...`).
- **chromedp browser runtime smoke (physical class).** The v0.23.1 +
  cdproto v0.160.3 migration is COMPLETE and suite-verified, but there
  is no Chrome here: browser-tool runtime behavior is NOT RUN until a
  Chrome-equipped lane exercises it.
- **This session's sandbox lacks the OpenGL/GL dev headers** (no sudo),
  so the full CGO Fyne build/vet and the local `go test ./ui/fyne`
  sweep were NOT RUN for this candidate — they were NOT needed either:
  the candidate changes ZERO `ui/fyne` files, so the hosted Fyne UI
  gate's green result for `94a97de` (run 43718750096) carries over
  unchanged for the UI tree.

## Known evidence (current)

- Run-43718750096 root causes fixed (fixture-only; production dial
  untouched): (1) the busy-pipe fixture armed its overlapped accept
  WITHOUT waiting for the issue, so the first client could connect in
  the CreateNamedPipe→ConnectNamedPipe window — ConnectNamedPipe then
  completes INLINE with ERROR_PIPE_CONNECTED and NO event ever fires,
  but the helper still waited on the event (5s timeout vs the 2s
  readiness bound — the exact hosted ~4s failure signature). The
  fixture now (a) waits for the accept to be ISSUED before returning
  and (b) branches on all three completion modes (nil /
  ERROR_PIPE_CONNECTED / ERROR_IO_PENDING). (2) The two release-
  contract mutation fixtures used LF-only needles against a CRLF
  Windows checkout (autocrlf=true; `.gitattributes` pins only `*.go` /
  `*.bat`): the mutations silently no-opped and the healthy workflow
  passed the validators — the "broken detector" failures.
  `loadWorkflowForDAG` now normalizes BOM/CRLF→LF and every mutation
  carries an application guard (a future workflow reshape fails loudly
  as a fixture-shape error, never as a silent no-op).
- §7.3 hard 100,000,000-byte per-file payload ceiling is now ENFORCED:
  shared fail-closed `scripts/ci/package-size-audit.sh` (staged tree +
  UNCOMPRESSED archive members), invoked by BOTH lanes, plus a tree
  scan + lane pins + mechanism test in
  `internal/releasecontract/oversized_asset_policy_test.go`. Tree
  scan: 804 tracked files, largest 5,959,200 bytes — zero violations.
- Build-workflow DAG: audit/ui/build-windows/build-linux have NO
  `needs: [preflight]` (own checkout/setup, same SHA) — PROVEN HOSTED by
  run 43718750096 (all four gates executed with the Windows lane red);
  the `final-verdict` job stays red when any required gate is not
  success. Collector fixtures 25/25 locally. The Windows package was
  blocked by `BLOCKED_BY_UPSTREAM_TEST_FAILURE` — the missing ZIP is a
  downstream symptom, NOT a proven packaging defect.
- CodeQL workflow: analyze runs on `!cancelled() &&
  steps.codeql-init.outcome == 'success'` with `upload: always`;
  `collect-codeql-logs` reuses the same collector on `!cancelled()`.
  CodeQL run 43718750102 (baseline): all seven jobs SUCCESS — including
  C++ SARIF upload and the alert-report export; the ONE warning is the
  documented C++ manual-build-mode overlay-cache fallback (an
  optimization warning, NOT an analysis failure — do not switch C++ to
  build-mode: none merely to silence it). Open-alert counts were NOT
  retrieved (API rate-limited); never claim a count without the report.
- Dependency integrity: `go mod verify` passes and `go mod tidy` is
  idempotent. Every DIRECT module is at its latest known stable tag
  (fresh `go list -m -u all` on 2026-11-16); all newer versions are
  indirect/transitive (Fyne-chain and chromedp-chain pins, test-only
  objx/smetrics/check.v1) and are DECLINED with reasons in note.md —
  `fyne.io/systray` stays on the pseudo-version because no newer
  release exists and Fyne v2.8.2's own graph requires it.
- Fyne button-fit/containment: the v1.11.1 policy (short labels +
  wrapped captions, rail grows to content minimum, full-shell fit
  sweep, viewport/boundary/degenerate matrices, structural goldens,
  single-parent ownership) is in-tree and was hosted-green for
  `94a97de` in run 43718750096's Fyne UI gate; this candidate changes
  zero `ui/fyne` files.
- CodeQL: the documented actions/unpinned-tag warnings remain the
  accepted baseline under the operator's tag-only policy (zero
  commit-hash pins present); the candidate has NOT been scanned
  (unpushed) — never claim otherwise.

## Unresolved risks

- The r182→r187 Three.js refresh re-applied the vendored `generateUUID`
  CSPRNG patch; the assets are embed/compile-verified, but 3D Studio
  runtime behavior is physical class until a browser run happens.
- Dependabot security-update PRs are governed by a separate GitHub
  setting; the one-group YAML cannot cap them. The manual setting path is
  documented in `.github/dependabot.yml` and `note.md`.

## Next actionable hand-off

1. Operator reviews the candidate diff + `note.md`, pushes, and watches
   the hosted run: the four formerly failing Windows tests must pass
   natively and the new size-gate logs (`ci-logs/linux-size-audit.log`,
   `ci-logs/win-size-audit.log`) must appear green.
2. Any agent continuing here should re-verify with the smallest ladder
   prefix: `gofmt -l cmd internal ui`, `go build ./...`,
   `go test -tags headless ./internal/...`, then the touched packages —
   full ladder and evidence-class rules live in [`agent.md`](agent.md).
   On a Windows host, run `go test -tags headless ./internal/localipc/ ./internal/releasecontract/`
   natively — the two busy-dial tests are the first authority for the
   fixture repair.
3. If the hosted Windows lane is red, start from the failing test's own
   failure line, then `ci-logs/win-ipc-integration.log` and
   `data\logs\app.log` inside the smoke output — the gates are named for
   their failure stage.

---

*Proprietary material under the Proprietary License (see
[`../LICENSE.md`](../LICENSE.md)).*
