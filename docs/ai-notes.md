# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-17, run-43995689377 stabilization candidate)

The branch `work/v1.11.1-stabilization` (local, unpushed) carries the
v1.11.1 stabilization candidate for GitHub Actions run 43995689377
(build-desktop, attempt 1, candidate commit `f699613`). Baseline:
`af1ec7e` = origin/main HEAD = `f699613` plus two baseline-JSON
deletions (preserved). Version stays 1.11.1 everywhere. The user owns
every remote mutation; nothing has been pushed. The renderer-convergence
milestone (previous hand-off below) is already ON main (`d361692` and
after; the user confirmed the CodeQL alerts #185/#186 fixed).

## What changed in this candidate (the load-bearing facts)

1. **The exact run-43995689377 preflight failure is fixed.**
   `gofmt -l cmd internal ui` flagged exactly
   `internal/releasecontract/file_catalog_contract_test.go`;
   formatting-only fix. The whole tracked tree is additionally clean —
   including the 3 `experiments/ui-engine-poc` files that sit OUTSIDE
   the `cmd internal ui` gate scope.
2. **The Windows engine-update root cause is code-trace CONFIRMED: a
   retained `os.Root` handle.** The installer held an `os.Root` on
   `<binDir>/.engine-download` across the transaction body, so the SWAP
   step's rename of the whole `data\bin` directory hit
   ERROR_ACCESS_DENIED on Windows ("stage engine swap: rename of
   data\bin to data\bin.update-old returned Access is denied"). Fix:
   the cleanup root now lives in a short scope after the transaction;
   EVERY root the updater opens is tracked
   (`internal/updater/roottrack.go`) and the swap refuses with a typed
   error while any tracked root under the engine dir is still open
   (previous engine preserved byte-for-byte); external rename failures
   are classified (ERROR_ACCESS_DENIED vs ERROR_SHARING_VIOLATION) —
   diagnostics only, no retries, no protection disabled. Deterministic
   fixtures on every OS; the full C2 versioned-manifest architecture is
   deliberately NOT half-shipped.
3. **The false-green smoke is dead.** New `internal/smokeverdict`
   contract: ShellOK and EngineReady are independent claims;
   `SHELL SMOKE OK; ENGINE NOT READY` is the run-43995689377 class
   label; `INFERENCE: NOT RUN` is never implied by engine health; the
   `no recent release ships a prebuilt asset` Linux shape is
   EngineStateUpstreamUnavailable (upstream limit, NOT a candidate
   defect — live-observed: llama.cpp ships no linux/amd64 prebuilt).
   The Windows smoke now has a bounded maintenance settle window + a
   MANAGED-ENGINE-READY stage (shell green + engine red = RED job);
   the Linux smoke mirrors it with the upstream-unavailable labelled
   distinction. All pre-existing smoke/NSIS/package assertions stand.
4. **Execution-truth telemetry.** CPU policy selections (requested=CPU,
   AUTO→CPU fallback) are no longer execution-verified "by definition":
   verification needs the measured `CPUExecutionReceipt` (real
   generations through the installed CPU package, identity-checked).
   AUTO→CPU records `fallback=cpu` (never `none`); CPU throughput no
   longer feeds the GPU benchmark gate; the ladder's rung 6 accepts
   measured generations for a CPU-selected serving path while rung 7
   still requires the selection receipt. The observed false line
   (`requested=AUTO selected=CPU executionVerified=true` with nothing
   executed) is pinned impossible by tests.
5. **Catalog + docs.** `FILE_CATALOG.md/json` regenerated (866 files) —
   this also repairs the stale-catalog contract failure inherited from
   main (two baseline JSONs deleted without regeneration). Changelog,
   UPDATE.md (engine-swap handle lifecycle), TROUBLESHOOTING (Access
   denied / BLOCKED) and worklog updated; `Dev-Notes.md` untouched
   (human-owned).

## Known blockers / honest states (the vocabulary, not excuses)

- Hosted GitHub runs: NOT RUN for this candidate — the user owns
  publishing. NOTE for the next hosted run: the Windows lane now
  additionally requires managed-engine maintenance success (the old
  false-green), so it can legitimately turn red where it previously
  printed a misleading SMOKE OK.
- Windows-native execution of the updater fixtures: NOT RUN (no Windows
  host). The fixtures are deterministic on every OS via the in-process
  root registry; the Windows rename semantics need the hosted lane.
  The observed run DID download the b12445 win-x64 engine successfully,
  so the hosted Windows lane exercises the full fixed path.
- Linux managed engine: upstream-unavailable (no linux/amd64 prebuilt
  in the release feed) — Linux is UI-ONLY-TEST, never TEST-READY-LINUX,
  while upstream lacks the asset.
- INFERENCE-VERIFIED: NOT RUN (no model selected/executed).
- ImportAudit.Tests.ps1 locally: BLOCKED (Windows-path assumptions; the
  hosted Windows lane is its authority). PackageGate/NsisVersion
  fixtures: locally green in their lane.
- Physical touch / screen readers / real GPUs: NOT RUN anywhere.
- Local gate matrix for this candidate (all exit 0): gofmt (gate +
  whole tree), vet, build, headless sweep, race 10/10 pkgs, stress
  47/47, release contract, collector fixtures 26/0, diagnostics
  self-test, Fyne UI suite + full desktop sweep under Xvfb (software
  GL), GOOS=windows vet/headless-build, live app end-to-end. Details:
  `local-evidence/test-results.json` in the delivery ZIP + `note.md`.

## Next actionable step (for the user or the next agent)

1. The user reviews this candidate (source ZIP + note.md +
   test-results.json in download/) and decides the publish of
   `work/v1.11.1-stabilization`.
2. After publish: watch the hosted Windows lane — the engine-ready gate
   is new; if it turns red, the app.log maintenance section + the
   classified swap error name the exact cause (in-process handle vs
   AV/indexer sharing violation vs ACL).
3. Deferred by design (do not half-ship): the versioned-engine-directory
   + active-manifest architecture (full C2), the narrow elevation
   broker (app stays asInvoker; protected operations stay unavailable),
   accelerator evidence-correlation IDs.
