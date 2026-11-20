# CI Topology — ONE platform-build workflow, six-target matrix (ALPHA)

> Authority: `.github/workflows/build.yml` — the ONE platform-build
> workflow. `codeql.yml` is the only other workflow and runs as an
> independent security gate. The contract tests in
> `internal/releasecontract` (workflow DAG, platform-matrix structure,
> upload surface, artifact naming, runner identity, package-gate
> fragments) tripwire everything on this page. State after the CI
> consolidation (2026-11-19); hosted execution of the consolidated
> workflow itself is NOT RUN.

## 1. The consolidation

The retired topology — the `build-desktop.yml` orchestrator plus the
five reusable `build-windows/linux/macos/android/ios.yml` lanes wired by
`workflow_call` — is GONE. `.github/workflows` carries exactly
`build.yml` + `codeql.yml` (pinned by
`TestNoOtherActivePlatformBuildWorkflow` and `RetiredLaneWorkflowFiles`;
a reappearance recreates the multi-file topology and fails the
release-contract suite).

## 2. The DAG

```
push (main, v* tags) + pull_request (main) + workflow_dispatch
  build.yml
    platform-build   ONE matrix job, SIX include entries, no needs:
                     windows-x64        -> windows-2025     (zip)
                     linux-x64          -> ubuntu-26.04     (tar.gz)
                     macos-arm64        -> macos-15         (app zip)
                     macos-x64          -> macos-15-intel   (app zip)
                     android-arm64      -> ubuntu-26.04     (debug apk/aab)
                     ios-simulator-arm64-> macos-15         (simulator zip)
                     strategy.fail-fast: EXPLICITLY false — a red lane
                     never cancels its siblings; every lane runs to its
                     terminal state and keeps its evidence. Each leg
                     dispatches per-phase to scripts/build/{windows.ps1,
                     linux.sh, macos.sh, android.sh, ios.sh}; every
                     verification gate writes ci-logs/<target>-upstream-
                     failure.txt so the if:always() package-gate
                     reporter can name ONE root cause.
    collect-errors   needs exactly [platform-build], if: always():
                     scripts/ci/consolidate-run-log.sh writes
                     SHEYTAN-LA-errors-warnings.txt +
                     run-diagnostics.json (both archive:false,
                     retention 14, if: !cancelled()); a final
                     fail-closed step re-checks the collector rc and
                     fails on coverage gaps.
    final-verdict    needs [platform-build, collect-errors],
                     if: !cancelled(): strict loop — every required
                     gate must be success; a collector success can
                     never override a platform failure.
    release          tags only (refs/tags/v), needs all three above,
                     contents: write: downloads the windows + linux
                     packages, verifies the expected names, regenerates
                     release-manifest.json + LC_ALL=C-sorted
                     SHA256SUMS.txt, publishes the VERIFIED names.
```

Rules pinned by the contracts:

- The matrix legs are INDEPENDENT gates: the platform-build job has
  EMPTY or absent `needs` — no platform-to-platform needs chain (the
  run-43630164424 lesson; the strict final verdict owns acceptance).
  `ValidatePlatformMatrixStructure` parses the YAML and asserts exactly
  six targets with unique (target, os, artifact) triples.
- macOS Intel and Apple Silicon stay on DISTINCT runner images
  (macos-15-intel vs macos-15) with DISTINCT artifact names.
- ONE authoritative trigger block: build.yml owns
  push(main + v* tags)/pull_request(main)/workflow_dispatch.
- `concurrency.cancel-in-progress` is EXPLICITLY false — evidence is
  never cancelled. External actions are immutable 40-hex commit pins
  with synced version comments (`.github/actionlint.yaml` registers the
  pinned runner labels so actionlint stays a clean gate).

## 3. Artifacts (exact names)

| Matrix target | Artifact | Qualification the lane enforces |
|---|---|---|
| windows-x64 | `SHEYTAN-LA-ALPHA-windows-x64-portable` (.zip) | named-pipe CoreClient integration, gen-syso + windowsgui PE verify, Mesa-pinned first-run/UI smoke, ImportAudit closure, PackageGate fail-closed, 100 MB ceiling |
| linux-x64 | `SHEYTAN-LA-ALPHA-linux-x64-portable` (.tar.gz) | ubuntu-26.04 runtime-verified (ID/VERSION_ID/dpkg amd64), absolute build-info probe, ELF verify, Xvfb boot/window/SIGTERM-reap smoke, archive + purity verify, size gate, fail-closed package gate |
| macos-arm64 / macos-x64 | `SHEYTAN-LA-ALPHA-macos-<arch>-portable` (.zip) | per-arch native compile + tests, ad-hoc signed .app (unnotarized, stated), launch smoke |
| android-arm64 | `SHEYTAN-LA-ALPHA-android-arm64` | pinned gomobile/gobind (v0.0.0-20261117202452-47699113848a, never @latest), fail-closed NDK, debug-signed dev package, manifest/dex checks, repo-cleanliness guard; emulator/device smoke NOT RUN (stated on the artifact) |
| ios-simulator-arm64 | `SHEYTAN-LA-ALPHA-ios-simulator-arm64` (.zip) | SIMULATOR-ONLY qualification, Mach-O + plist verify; a device IPA is never produced without signing |
| ios-device-arm64 | `SHEYTAN-LA-ALPHA-ios-device-arm64` (.ipa) | run 44547031698 §6.3: the matrix entry is structural; execution is honestly gated on the five Apple signing secrets — without them the lane records BLOCKED BY MISSING APPLE SIGNING CONFIGURATION (`PACKAGE_GATE_RESULT BLOCKED_BY_MISSING_APPLE_SIGNING`, the job stays green, no IPA is fabricated); with them: real iphoneos arm64 build, per-job temporary keychain (deleted even on failure), ad-hoc export, fail-closed signature/team/slice/profile verification. Physical install requires the device UDID inside the ad-hoc profile and stays NOT RUN without a registered device |
| collect-errors | `SHEYTAN-LA-errors-warnings.txt` + `run-diagnostics.json` | the §7 errors-and-warnings-only diagnostic (see §4) |

Per target, the platform-build job also uploads
`build-evidence-${{ matrix.target }}-${{ github.run_attempt }}`
(ci-logs/, if-no-files-found: warn) so red lanes keep their gate logs.


### 3a. Structured package verdicts (run 44547031698 §7.5)

Every platform gate emits exactly ONE machine-readable verdict line the
collector aggregates:

    PACKAGE_GATE_RESULT OK <verified artifact path>
    PACKAGE_GATE_RESULT BLOCKED_BY_UPSTREAM_TEST_FAILURE <first failed gate>
    PACKAGE_GATE_RESULT BLOCKED_BY_MISSING_APPLE_SIGNING <required secret names>
    PACKAGE_GATE_RESULT PACKAGE_MISSING <expected path>

The top-level package verdict is OK only when EVERY platform-build matrix
job reported OK — a blocked or silent lane can never be outvoted by a green
sibling again (the hosted run 44547031698 report said package=OK beside four
blocked gates). The shared artifact upload is skipped while the per-target
upstream-failure or blocked marker exists, so a blocked package never
produces the duplicate `No files were found with the provided path` noise;
the evidence upload (ci-logs/) stays unconditional.

### Carried-over-as-FIXED packaging defects

1. **macOS upload path**: the retired `build-macos.yml` line 209
   uploaded `dist/macos-<arch>/SHEYTAN-LA-ALPHA-macos-<arch>.zip` while
   the produced file carried the `-portable` suffix — the upload would
   have found no files. The consolidated macOS lanes write the zip to
   the dist ROOT under the artifact-derived
   `dist/SHEYTAN-LA-ALPHA-macos-<arch>-portable.zip` name, and the
   matrix upload stages `dist/SHEYTAN-LA-ALPHA-*` with
   `if-no-files-found: error`.
2. **Release publish names**: the retired orchestrator's release job
   published `dist/SHEYTAN-LA-v${APP_VERSION}-windows-x64.zip`-style
   names no lane ever produced. The release job now verifies and
   publishes exactly `dist/SHEYTAN-LA-ALPHA-windows-x64-portable.zip`
   and `dist/SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz` plus the
   generated `release-manifest.json` and `SHA256SUMS.txt`.

## 4. The consolidated diagnostic (work order §7)

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
offline fixture suite (`consolidate-run-log-fixtures.sh`) proves every
clause — clause O pins the build.yml shape (six matrix targets exactly
once, one errors-warnings upload with archive:false, collector wired,
retired names absent). Application runtime logging is a separate system
and keeps its structured levels.

## 5. Windows PowerShell error contract (work order §4.2)

`$?` is captured immediately after a pure PowerShell script
invocation; `$LASTEXITCODE` is read ONLY after a native executable
(`go`, `pwsh -File`, `git`, `7z`), captured immediately.
`scripts/ci/StepInvocationContract.Tests.ps1` pins the contract under
`Set-StrictMode -Version Latest` with no inherited exit-code variable;
the PackageGate fixture suite invokes the gate as a native child
process.
