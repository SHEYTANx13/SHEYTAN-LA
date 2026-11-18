# CI Topology — single orchestrator, five reusable lanes (ALPHA)

> Authority: `.github/workflows/build-desktop.yml` and the five
> reusable files it invokes. The contract tests in
> `internal/releasecontract` (workflow DAG, upload surface, artifact
> naming, runner identity, five-lane invoked-once validator) tripwire
> everything on this page. State after the run-44306343338 repair
> (2026-11-18); hosted execution of the candidate itself is NOT RUN.

## 1. The DAG

```
push/PR (main, v* tags) + workflow_dispatch
  build-desktop.yml
    preflight      (compile gate, gofmt, vet, mod integrity)
    audit          (headless suite, race, stress + release surface,
                    release-contract suite, catalog freshness,
                    collector fixtures, diagnostics index)
    ui             (Fyne UI contract/gates + Xvfb smokes)  [swaps to
                    Go-Gui gates ONLY when the migration delivers the shell]
    platform-windows -> .github/workflows/build-windows.yml   (exactly once)
    platform-linux   -> .github/workflows/build-linux.yml     (exactly once)
    platform-macos   -> .github/workflows/build-macos.yml     (exactly once;
                        arm64/x64 matrix inside, fail-fast: false)
    platform-android -> .github/workflows/build-android.yml   (exactly once)
    platform-ios     -> .github/workflows/build-ios.yml       (exactly once)
    consolidate-logs (if: !cancelled(); errors/warnings-only artifact)
    final-verdict    (strict: every required gate must be success)
    release          (tags only; consumes the reusable lanes' artifacts)
```

Rules pinned by the contracts:

- The platform lanes are INDEPENDENT gates: no `needs: [preflight]` — a
  red preflight never skips platform evidence (the run-43630164424
  lesson; the strict final verdict owns acceptance).
- Each reusable file subscribes to `workflow_call` ONLY; the
  orchestrator owns push/PR triggers. Each accepts a typed
  `go_version` input (default = the go.mod toolchain directive) and
  emits an `artifact_name` output.
- The retired legacy in-orchestrator `build-windows`/`build-linux`
  lanes never reappear (`retiredLaneJobKeys` + the five-lane validator);
  every distinct check they performed was transferred byte-exact into
  the reusable lanes.
- External actions are immutable 40-hex commit pins with synced version
  comments. `cancel-in-progress: false` — evidence is never cancelled.

## 2. Artifacts (work order §6.3, exact names)

| Lane | Artifact | Qualification the lane enforces |
|---|---|---|
| build-windows.yml | `SHEYTAN-LA-ALPHA-windows-x64-portable` (.zip) | named-pipe CoreClient integration, gen-syso + windowsgui PE verify, Mesa-pinned first-run/UI smoke, ImportAudit closure, PackageGate fail-closed, 100 MB ceiling |
| build-linux.yml | `SHEYTAN-LA-ALPHA-linux-x64-portable` (.tar.gz) | ubuntu-26.04 runtime-verified (ID/VERSION_ID/dpkg amd64), absolute build-info probe, ELF verify, Xvfb boot/window/SIGTERM-reap smoke, archive + purity verify, size gate, fail-closed package gate |
| build-macos.yml | `SHEYTAN-LA-ALPHA-macos-<arch>-portable` (.zip, arm64+x64 matrix) | per-arch native compile + tests, ad-hoc signed .app (unnotarized, stated), launch smoke |
| build-android.yml | `SHEYTAN-LA-ALPHA-android-arm64-debug-apk` | module-graph gomobile pin, debug-signed dev APK, manifest/dex checks, repo-cleanliness guard; emulator/device smoke NOT RUN (stated on the artifact) |
| build-ios.yml | `SHEYTAN-LA-ALPHA-ios-simulator-arm64` (.zip) | SIMULATOR-ONLY qualification, Mach-O + plist verify; a device IPA is never produced without signing |
| consolidate-logs | `SHEYTAN-LA-errors-warnings.txt` + `run-diagnostics.json` | the §7 errors-and-warnings-only diagnostic (see §3) |

## 3. The consolidated diagnostic (work order §7)

`scripts/ci/consolidate-run-log.sh` fetches completed job logs via the
jobs API (never the in-progress run-attempt archive), fetches per-job
check-run annotations as the authoritative annotation channel, and
writes ONE plain-text artifact that contains ONLY errors, warnings,
test failures (normalized `TEST_FAILURE:`), failed/cancelled job
metadata, missing-log-source rows and the run/coverage summary.
ASCII-only English; the substrings `===` and `---` can never appear
(runs are encoded `EQUALS_RUN(n)` / `HYPHENS_RUN(n)`); non-ASCII is
mapped or escaped; secrets and signed URLs are redacted; output is
deterministic; a red upstream run is NOT a collector failure, a
missing terminal log IS (fail-closed, report written first). The
offline fixture suite (`consolidate-run-log-fixtures.sh`, 23 cases)
proves every clause. Application runtime logging is a separate system
and keeps its structured levels.

## 4. Windows PowerShell error contract (work order §4.2)

`$?` is captured immediately after a pure PowerShell script
invocation; `$LASTEXITCODE` is read ONLY after a native executable
(`go`, `pwsh -File`, `git`, `7z`), captured immediately.
`scripts/ci/StepInvocationContract.Tests.ps1` pins the contract under
`Set-StrictMode -Version Latest` with no inherited exit-code variable;
the PackageGate fixture suite invokes the gate as a native child
process.
