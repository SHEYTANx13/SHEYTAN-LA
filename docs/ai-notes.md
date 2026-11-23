# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — runs 45086860202 + 45086860126 repair, mesh phase 2a, log-artifact policy (2026-11-23) — supersedes all handoffs below

All earlier session handoffs were PRUNED from this file on 2026-11-23
(2026-11-18 .. 2026-11-22 states, incl. the runs 45005960459/45005960383
mesh-phase-1 handoff): they are superseded snapshots and live in git
history (`git log -- docs/ai-notes.md`). Only the current state is kept
here, per the file's own snapshot rule.

Baseline: commit `1007c35e78105c8bf2cde42bc4d98092d93f0801` (== origin/main
== the exact commit both failed runs tested). Branch + candidate tip: see
the delivery `note.md` of this session. NO remote operations were
performed (no push, no PR, no tag, no release).

Root causes fixed this pass (evidence class stated per item):

1. **CodeQL "Analyze (go)" exit 32 — REPRODUCED LOCALLY with
   codeql-cli 2.27.5** (runs 45005960383 AND 45086860126): `go build
   -json` is INVISIBLE to the CodeQL Go extractor. A cold traced
   `go build -json ./...` compiles every package and the database still
   finalizes empty ("CodeQL detected code written in Go but could not
   process any of it"), while the SAME command without `-json` produces
   a populated database. The prior fix (vet moved after analyze, run
   45005960383) removed one cache-warming path but the hosted run still
   starved: the `-json` flag ITSELF was the extraction blocker. Fix:
   the traced builds in `codeql.yml` run PLAIN (exact file:line:col
   compiler diagnostics land in the job log); `-json` is reserved for
   the FAILURE path only (one bounded rerun feeding
   `scripts/ci/go-build-jsonl-diagnose.py`). Local proof:
   `codeql database create` on a minimal module — plain build creates
   the database, `-json` build reproduces the fatal error verbatim.
2. **Darwin headless-suite runtime failures — fixed at source, hosted
   proof pending** (runs 45005960459 compile error was fixed by the
   prior session; run 45086860202 failed again at RUNTIME — the cgo
   bridge tests executed for the first time): (a) `processExePathImpl`
   leaked the walk-internal `errProcGone` for a vanished PID while the
   public contract (and `TestListeningProcessDeadOwnerRefused`, and the
   Windows/Linux implementations) require `ErrProcessNotFound` — now
   translated exactly like `processStartTimeImpl` already did; (b) the
   native tests compared `proc_pidpath` output to `os.Executable()`
   with raw string equality — `proc_pidpath` answers the canonical
   vnode path (`/private/var/...`) while `os.Executable()` answers the
   exec path (`/var/...`; TMPDIR lives behind the /var symlink on every
   macOS host) — replaced with the repo's canonical `SameExecutable`
   comparison. Compile-level verification: full
   `CGO_ENABLED=1 GOOS=darwin CGO cross-vet of ./internal/... is CLEAN
   (zig 0.13.0 as the darwin C compiler, `go test -c` compiled the
   whole proc test binary — only the final link hit zig's missing
   libresolv stub, an environmental limit, not a repo defect).
3. **Errors/warnings log artifact — owner-directed policy change
   (2026-11-23)**: the run-44835425398 local-only rule kept the curated
   report on the runner, so a red run's diagnosis was invisible from
   the run page (the owner's exact complaint: "build did not even
   generate log ... as .txt"). Now BOTH workflows upload the ONE
   curated errors/warnings report as an artifact
   (`SHEYTAN-LA-errors-warnings` / `SHEYTAN-LA-codeql-all-logs`,
   always(), if-no-files-found: error, overwrite: true, exact single
   file). Everything else stays banned (raw logs, verbose compiler
   output, diagnostics indexes, test reports) —
   `internal/releasecontract` re-pinned: `ValidateUploadArtifactContract`
   is now a two-artifact closed allowlist (matrix package + curated
   report), `ValidateFailureTolerantCollector` requires EXACTLY the one
   report upload, `ValidateCodeQLWorkflowResilience` permits the
   security report + the CodeQL collector report; mutation fixtures and
   collector-fixture clause O updated (39/0 green).

**Mesh computing phase 2a (new `internal/mesh/role.go` + `peer.go` +
`role_peer_test.go`)**: dynamic dual-role operation per the owner work
order ("each device can be both server and user based on need"; the
phone-as-interface scenario). `NegotiateRole` decides
controller/worker/both from a typed `DeviceCapability` + owner
preference, narrowed by the static `Config.Role` ceiling (explicit
contradictions are typed errors; auto proposals clamp to the ceiling);
`Manager.NegotiateRole` applies it (refused while disabled) and
`Status().Role` reflects it. The `PeerExchange` seam + in-process
`LoopbackPeerExchange` connect TWO managers: the provider
re-validates every envelope with its OWN gates (UI/UX compute,
private payloads and oversized envelopes cannot cross), executes
through its allowlisted registry, and the requester commits the
verified result in ITS OWN ledger via
`Coordinator.DispatchToPeer` (durable pending row BEFORE any peer
I/O, fencing epoch, typed fail-closed refusal). The real LAN/Internet
transport (blueprint phase 3, libp2p) remains the NEXT gated phase —
no networking dependency was added; mesh stays disabled by default.

Docs updated: this file (pruned + rewritten), `docs/worklog.md`
(2026-11-23 row), `docs/changelog.md` (ALPHA section), file catalog
regenerated. Protected docs (`docs/agent.md`, `docs/Dev-Notes.md`,
`docs/retired.md`) untouched.
