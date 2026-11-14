# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-14, v1.11.1 maintenance candidate)

The branch `maintenance-v1.11.1` (local, unpushed) carries the
run-43601915826 repair: the Windows IPC repair and the maintenance pass
described in [`changelog.md`](changelog.md) under the existing 1.11.1
maintenance heading. Baseline: `cbb07af` on main (the red run's commit;
the earlier green run 43455711025 is historical only). Product version
stays 1.11.1 everywhere. The full evidence inventory for this candidate
ships in `note.md` beside the source ZIP.

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
  cdproto v0.160.2 migration is COMPLETE and compile-verified (headless
  Linux build, GOOS=windows cross-build, full headless suite, 47/47
  stress scenarios), but this sandbox has no Chrome: browser-tool runtime
  behavior is NOT RUN until a Chrome-equipped lane exercises it. A
  real-browser smoke is the next hosted verification item.
- **Hosted verification of the IPC gates is pending the operator's push.**
  No green-run claim exists for the candidate.

## Known evidence (current)

- Run-43601915826 root causes fixed: the busy-pipe test fixture
  deadlock (synchronous handle + OVERLAPPED mismatch in
  `dial_windows_test.go`), the logging rotation-test handle leak
  (Windows TempDir cleanup), and the stale `GO_VERSION: "1.27"` literal
  in the release-surface gate (now derived from the go.mod toolchain
  directive with exact workflow/runtime parity). Windows lanes are
  compile-verified (`GOOS=windows go test -c`) — the runtime authority
  is the hosted lane.
- Unix transport: the integration suite runs green on Linux and now
  also proves the WS caller-cancel path and a procfs-inode-precise
  no-TCP-listener contract (`TestNoNetworkListenerIsOpened` runs green
  locally).
- Dependency integrity: `go mod verify` passes and `go mod tidy` is
  idempotent at chromedp v0.23.1 / cdproto v0.160.2. Fresh tag audit:
  winres v0.3.1 IS the latest real tag (the researched v0.3.3 does not
  exist); `fyne.io/systray` stays on the pseudo-version because Fyne
  v2.8.2's own graph requires it.
- Fyne button-fit: the rail now grows to its content minimum (the fixed
  rail width was the Windows clipping mechanism), the Models panel's
  orphaned "Open folder" button and restart-hint caption are wired, and
  the engine-update actions are a real stacked column. The matrix,
  chrome sweep and rail contract are compile-verified only in this
  sandbox (no GL headers for the Fyne CGO build) — the Linux UI lane is
  the compile/run authority.
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
