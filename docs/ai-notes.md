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

The branch `maintenance/v1.11.1-windows-ipc` (local, unpushed) carries
the Windows IPC repair and the maintenance pass described in
[`changelog.md`](changelog.md) under the existing 1.11.1 maintenance
heading. Baseline: `c8fc15e` on main (green run 43455711025). Product
version stays 1.11.1 everywhere. The full evidence inventory for this
candidate ships in `note.md` beside the source ZIP.

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

- **chromedp/cdproto pairing.** Every stable pairing since chromedp
  v0.17 ships the generics API rewrite (`Action[T]`, `Do`, value-returning
  actions); `internal/browser` + `internal/tools/browser.go` still use the
  v0.16 API. The pair is intentionally kept at chromedp v0.16.0 +
  cdproto pseudo-version (no stable tag carries the old API) until a
  Chrome-equipped lane can verify a real migration. Plan it as its own
  session; do not compile-only migrate.
- **Hosted verification of the IPC gates is pending the operator's push.**
  No green-run claim exists for the candidate.

## Known evidence (current)

- Windows named-pipe transport: contract tests + production CoreClient
  integration exist and run hosted; locally they are compile-verified
  (`GOOS=windows go vet`) — the runtime authority is the hosted lane.
- Unix transport: the same integration suite runs green on Linux
  (handshake, read-only APIs, WebSocket attach/normal close, wrong-key
  refusal, protocol refusal).
- Fyne button-fit matrix + responsive viewport matrix: green on Linux;
  hosted in the UI lane with a dedicated log.
- CodeQL: operator reports zero outstanding errors on main; the candidate
  has NOT been scanned (unpushed) — never claim otherwise.

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
