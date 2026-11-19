# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-18, run-44306343338 repair candidate)

The branch `work/alpha-run443063-repair` (LOCAL ONLY, unpushed — no
remote mutation is authorized) carries the run 44306343338 repair on top
of main HEAD `7c6678f2cf96d4741fd8f48b7952aa790bd63b42` (the exact
commit that run tested; ancestry verified: `7c6678f` is a direct child
of `79b21a4`, the session-2 base; the full DAG was inspected in
preflight and nothing was discarded). Candidate commits, in order:
`caf6c83` (root causes + consolidated five-lane CI topology + contracts)
-> `9c5d9f6` (errors-warnings-only collector contract proven) ->
`55435d1` (docs pass + release-surface gate reconciliation) -> the
sealing commit carrying this snapshot (the sealed-source digest and the
exact tip SHA are recorded in the handoff note). Version
stays 1.11.1 as the numeric BUILD IDENTIFIER; the public identity is
SHEYTAN-LA ALPHA (`internal/brand.Stage`).

Completed and locally verified (evidence classes labeled):

1. **Android/iOS module graph (§4.1)** — `golang.org/x/mobile` pinned at
   `v0.0.0-20261117202452-47699113848a` (newest compatible at repair
   time; proxy-verified published 2026-11-17, GoVersion 1.26.0). The
   base's graph resolved the stale 2023-11-27 pseudo-version, whose
   `golang.org/x/tools v0.16.0` fails under Go 1.27.4 (`invalid array
   length -delta * delta` in tokeninternal) — REPRODUCED locally, fix
   proven by `go install` of gomobile AND gobind at the pin (x/tools
   resolves v0.53.0). Dependency-graph contract test pins the exact
   version (`internal/releasecontract/mobile_dependency_contract_test.go`).
2. **Windows PowerShell contract (§4.2)** — the run-443 root cause
   (reading `$LASTEXITCODE` after a pure PowerShell script) fixed at
   every site: `$?` captured immediately after .ps1 invocations;
   PackageGate.ps1/ImportAudit.ps1 invoked as native `pwsh -File`
   children so `$LASTEXITCODE` is genuine; PackageGate.Tests.ps1's
   Invoke-Gate uses the native form; new strict-mode regression fixture
   `scripts/ci/StepInvocationContract.Tests.ps1` runs on the Windows
   lane. Upstream-failure marker contract unchanged (fail-closed).
3. **Linux/macOS probe paths (§4.3)** — the compiled build-info probe
   resolves the ABSOLUTE canonical path (`realpath`) and verifies the
   executable bit BEFORE changing into the isolated temp directory
   (run-443 Linux exit 127 root cause); the richer legacy assertions
   (exit code, exact identity fields, ASCII, residue check) transferred
   with it. The macOS lane probe hardened identically.
4. **Darwin local IPC (§4.4)** — `listener_unix.go` split by OS:
   `listener_linux.go` (SO_PASSCRED + SO_PEERCRED ucred, PID known) and
   `listener_darwin.go` (SOL_LOCAL/LOCAL_PEERCRED xucred against the
   pinned x/sys v0.50.0, typed `ErrPeerCredentialsMalformed` rejection,
   peer PID never claimed — API supplies UID/GID only). Verified: Linux
   native tests + `GOOS=darwin` compile of package and tests.
5. **Mach-O engine validation (§4.5)** — `internal/engcheck` sniffs and
   validates Mach-O thin (`macho.Open`) and universal/fat binaries
   (`macho.OpenFat`), maps CPU type to GOARCH, keeps strict PE/ELF
   paths, rejects wrong-architecture/ambiguous/malformed files, closes
   all handles. Synthetic thin/fat fixtures + garbage/text/Java-magic
   rejections tested (parser runs on every host OS). darwin consumers
   (engdiscovery/llm/updater/api) compile for darwin/arm64 and amd64.
6. **Darwin RAM/process probes (§4.6/§4.7)** — host RAM via the
   `hw.memsize` sysctl (pure Go, pinned x/sys); process RSS via Mach
   `task_info(TASK_VM_INFO)` for the self process under a
   `darwin && cgo` tag with a typed `ErrProcRAMUnsupported` fallback
   (values are never fabricated as zero); Linux `/proc` paths moved to
   Linux-only tags. Listening-process ownership on darwin is a typed
   `ErrPortOwnerUnsupported` (fail-closed) — the honest answer until a
   libproc fd walk is implemented AND verified on a native runner.
   Renderer matrix (§4.8) now covers every supported OS/arch pair per
   renderer (incl. darwin/arm64) with truthful notes; the catalog test
   tripswire enforces coverage.
7. **CI topology (§6)** — the legacy in-orchestrator Windows/Linux
   build lanes RETIRED; every distinct check transferred byte-exact
   into `build-windows.yml` / `build-linux.yml` (named-pipe CoreClient
   integration, gen-syso, windowsgui PE verify, Mesa-pinned launch
   smoke, ImportAudit + PackageGate contracts, upstream markers,
   size gates, richer build-info probe, Xvfb boot/window/term smoke,
   diagnostics index). Exactly five reusable lanes, each invoked once,
   workflow_call-only, typed inputs + artifact-name outputs, §6.3
   artifact names, cancel-in-progress: false. DAG/upload/naming/
   runner/version contracts updated and strengthened (five-lane
   invoked-once validator). Concurrency does not cancel evidence.
8. **Errors-warnings-only artifact (§7)** — the collector produces
   `SHEYTAN-LA-errors-warnings.txt` (ASCII-only English; no === or ---
   substrings — runs encoded EQUALS_RUN(n)/HYPHENS_RUN(n); "--- FAIL:"
   normalized to TEST_FAILURE with detail; metadata-authoritative
   failures; check-run annotation channel vs echoed-source
   classification; secrets/signed URLs redacted; MISSING LOG SOURCES +
   coverage=INCOMPLETE; self-job accounted SELF; packaging BLOCKED
   reports the FIRST upstream failure; deterministic). The offline
   fixture suite proves 23 cases (clause-to-case mapped in its header).
   Application runtime logging is untouched (separate systems).
9. **Identity (§14)** — the U+0422 Cyrillic homoglyph in the product
   name found in REQUIREMENTS-TRACEABILITY.md FIXED; an exact-ASCII
   identity scan test (T-position homoglyph detector + retired-name
   ban) tripswires the whole tracked tree.

Local verification (linux/amd64, go1.27.4, evidence class 2/3): full
headless suite green (`go test ./internal/... -tags headless`),
release-contract suite green, release-surface gate green under
headless, gofmt clean, catalog regeneration a byte-level no-op,
cross-compiles green for darwin/arm64 + darwin/amd64 + windows/amd64
(headless tag; the Fyne desktop build needs the GL/X11 headers that
only the hosted lanes install) and android/arm64 for internal/...
(module-graph build check only — NOT an APK, no NDK here).

NOT DONE (blocked on authorized time/resources, recorded honestly):

- **Go-Gui primary-shell migration (§5, Gate C)** — NOT STARTED in the
  main module; the 49-file Fyne UI remains the one production shell.
  The go-gui POC stays in `experiments/ui-engine-poc/` (its module pins
  go-gui v0.96.0; the work order's v0.97.0 upgrade is a follow-up that
  must re-verify the module and update the renderer matrix).
- Platform UI smoke, mobile adapters, download-manager UX, Git clone
  UX, smart continuation, unified queue UX, security broker surfaces
  (§5.3/§8-§12) — the service packages exist (downloader, gitclone,
  scheduler, approval, sandbox) but the user-facing UX layer was not
  built in this timebox.
- llama.cpp managed-engine asset pinning (§4.10) — asset-resolution
  review and exact-release pinning not performed this session.
- Hosted CI: NOT RUN — no push/dispatch is authorized from this
  session. Every hosted-evidence claim above is labeled accordingly.

## Local gate matrix for this candidate

| Gate | Result | Evidence |
|---|---|---|
| Headless suite (internal/...) | GREEN | `go test ./internal/... -tags headless` exit 0, linux/amd64 go1.27.4 |
| Release-contract suite | GREEN | `go test ./internal/releasecontract/` exit 0 (DAG, uploads, naming, runner, identity, mobile pin, catalog) |
| Release-surface gate | GREEN (headless) | `go test -tags headless -run TestReleaseSurfaceContractLocalHeadless ./cmd/`; full stress scenario needs GUI prereqs (hosted audit lane) |
| Collector fixture suite | GREEN | `bash scripts/ci/consolidate-run-log-fixtures.sh` 23/0 |
| gomobile/gobind at the pin | GREEN | `go install` both tools, exit 0 |
| Cross-compiles | GREEN | darwin/arm64, darwin/amd64, windows/amd64 (`-tags headless ./...`), android/arm64 (internal/...) |
| gofmt + catalog freshness | GREEN | `gofmt -l` empty after formatting; regeneration byte-identical |
| Native Windows/macOS/Android/iOS execution | NOT RUN | requires hosted runners / NDK / Xcode — never claimed locally |
| Hosted CI for the candidate | NOT RUN | operator must authorize push + run |

## Next actionable step (for the user or the next agent)

1. Review the local candidate `work/alpha-run443063-repair` (tip below),
   then authorize a push + hosted run of build-desktop.yml to convert
   the NOT-RUN platform gates into real evidence (Gate E/H).
2. Authorize the Go-Gui migration slice (§5) as the next work order
   phase: pin go-gui v0.97.0, port the 12-screen inventory with the
   written mapping tests, then retire Fyne ONLY at parity.
3. llama.cpp asset pinning (§4.10) and the darwin libproc fd-walk for
   port ownership (§4.7) are the two highest-value follow-up repairs
   that this session deliberately did NOT fake.

## CURRENT HANDOFF — run 44378562071 repair (2026-11-19) — supersedes the run-443063 handoff above

- Repository/branch: `github.com/SHEYTANx13/SHEYTAN-LA`, local branch `work/alpha-run443785-repair`, base `1342a104ce1e00a3da57d5b3561362fd8cbc01c5` (== origin/main HEAD at session start; direct parent `7c6678f`). Candidate tip: see `docs/worklog.md` 2026-11-19 row and the delivery note. Hosted CI on the candidate: **NOT RUN** (no push authorized by this work order).
- Fixed root causes (all reproduced or verified in source): (1) go.mod tidy drift — unused indirect x/mobile pin removed, module-integrity gate scripted; (2) Windows gofmt — undefined `env.GO_VERSION` in build-windows.yml (unpinned toolchain → gofmt version skew), plus gofmt first-failure marker so PackageGate reports BLOCKED_BY_UPSTREAM_FAILURE once; (3) collector file:line:/tool-error classification gated on job conclusion (Core audit t.Log false positives); (4) darwin AF_UNIX sun_path overflow — validator + short test roots; (5) free-port typed answer; fail-closed lease verification and adoption evidence on darwin; (6) engine temp-root residue reporting made truthful.
- Exact blockers (do NOT represent as done): Go-Gui §5 primary-UI migration NOT STARTED — `ui/fyne` remains the ONLY production shell (55 tracked files under ui/); darwin libproc port-owner fd walk NOT IMPLEMENTED (typed unsupported retained; `TestAdoptionAcceptsManagedEngine` will still fail on darwin until a real darwin identity path exists); Android step 8 ("Verify the APK") exact cause NOT REPRODUCED — the job-log API was unreachable from the session (unauthenticated rate limit; HTML log view requires sign-in) and the sandbox has no Android SDK/NDK; the lane's toolchain pin fix removes the cascade the go.mod fix would have caused, nothing more. PowerShell fixture suites NOT RUN (no pwsh in sandbox). Windows/macOS/Android/iOS native lanes NOT RUN. iOS `abandoned` was hosted-runner acquisition, untouched here.
- Next: authorize push + hosted run of build-desktop.yml on the candidate; then the Go-Gui §5 migration slice remains the gating item for TEST-READY ALPHA (Fyne retirement only at parity).
