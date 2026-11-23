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

## CURRENT HANDOFF — runs 45005960459 + 45005960383 repair & mesh phase 1 (2026-11-23) — supersedes the handoffs above

Branch `repair/darwin-syscallconn-codeql-trace`, base commit `9061e0768c99918e78f656c78cecddbe13e85892` (== origin/main == the exact commit both failed runs tested), clean tree at start, LOCAL branch only, NO remote operations of any kind.

Root causes fixed this pass (both REPRODUCED locally before the fix):

- **Darwin/iOS test-compile failure** (run 45005960459: macOS arm64, macOS x64 and iOS-simulator lanes, "headless core tests failed"; error: `cannot use unix (variable of interface type net.Listener) as interface{SyscallConn() (syscall.RawConn, error)} value`): `internal/proc/identity_darwin_cgo_test.go` passed static `net.Listener` values where the `SyscallConn`-capable interface is required — `*net.TCPListener`/`*net.UnixListener` implement it, the static interface does not. The file builds ONLY under `darwin && !ios && cgo`, so the Linux lanes/vet never saw it (the same visibility gap that let it reach main). SECOND defect under the same failure: the `RawConn.Control` callback was written `func(f uintptr) error`; the real contract is `func(fd uintptr)` (no error return). Fix: concrete-type assertions before every interface-param/slice-literal use + the correct Control signature. Verified by an isolated module replicating the exact statement patterns: `go vet` green for GOOS=darwin arm64+amd64, GOOS=linux, GOOS=windows; the file itself still compiles ONLY on the hosted Apple lanes (no SDK here) — `HOSTED_CI_PENDING`.
- **CodeQL Go extraction starvation** (run 45005960383: Analyze (go) fatal exit 32 at finalize, "didn't build any of it"): the `go vet ./...` step ran BEFORE init/build with CGO_ENABLED=1 on a cold runner — it compiled every package into the SHARED build cache, so the subsequent traced `go build -json` performed ZERO compile actions (locally reproduced: cold-cache build after vet emits zero JSON events) and the Go extractor saw no source. This also explains why run 44663205241 saw the goffi LINK error instead (links are not cached; compiles were). Fix: vet moved AFTER analyze with `if: always()` (Linux vet coverage preserved; analysis resilience unchanged) + the package list is now written to a file with a loud empty-list assertion (the old `mapfile < <(go list | grep -v ...)` silently swallowed a failed `go list` and would send zero packages to `go build`). Contracts untouched: triggers, go+actions matrix, security-and-quality, init-id-keyed analyze, upload: always, artifact policy — `internal/releasecontract` suite green.

**Mesh computing phase 1 (new `internal/mesh`, blueprint "SHEYTAN-LA Mesh Computing" by Parsa Tak, 2026-11-22):** typed AI-inference job model (`ai.embeddings.batch`, `ai.inference.whole_request`, `ai.readonly.batch` — a CLOSED allowlist), durable controller-owned ledger (atomic JSON persistence, attempt identity, fencing epochs, lease expiry, stale-result rejection, cancellation, retry cap with fail-closed exhaustion, restart recovery), allowlisted handler registry that structurally REFUSES every UI/UX compute type (`ErrUnsupportedUICompute`: ui.render/layout/animation, render.frame/scene, shell.exec, fs.read/write, browser.drive, code.exec — UI compute is never shared, only AI-inference compute participates), privacy-class gate (private payloads never dispatched in the first release), payload/result size bounds, deterministic FakeWorker, and the manager lifecycle wired into `runtime.Stack` (constructed DISABLED — zero side effects, no listener/discovery/worker exists; Close stops it last, bounded). 14 deterministic tests green incl. `-race`; the libp2p LAN/WAN transport is the NEXT gated phase (blueprint phase 2) and is NOT implemented here.

Docs updated: this handoff, `docs/worklog.md` (2026-11-23 row), `FILE_CATALOG.md`/`.json` regenerated via the repo generator (994 files).

Local gates green (linux/amd64, go1.27.4): gofmt repo-wide on touched dirs, `go vet` runtime+mesh, headless suite 76 ok / 0 FAIL, releasecontract suite (incl. the CodeQL + artifact contracts on the MODIFIED codeql.yml), engine suite (+`-race`), proc suite, runtime suite, ui/gogui suite, mesh suite (+`-race`), collector fixtures 39/0. NOT RUN: hosted CI on this candidate (no push/dispatch authorized — the two run fixes are `HOSTED_CI_PENDING`), native Apple SDK compile of the darwin test file (no macOS host), the CGO desktop build (no GL/Wayland headers here), Windows PowerShell fixture suites (no pwsh here).

## SUPERSEDED HANDOFF — run 44835425398 repair (2026-11-22) — was superseded on 2026-11-23

Branch `repair/darwin-union-artifact-policy`, base commit `96db9c390c9d26df597899e25a7cdd58accd7c1d` (== origin/main HEAD == the exact inspected baseline; `git ls-remote` re-verified before the first edit), clean tree at start, local commits only, NO remote operations of any kind.

Root causes fixed this pass:
- **Darwin socket-union compiler failure** (`internal/proc/identity_darwin.go:131:46`, the exact failure of the macOS arm64, macOS x64 and iOS-simulator lanes of run 44835425398): the CGo extractor read `sfi->psi.soi_proto.insi.insi_lport`, but the SDK union `soi_proto` has NO member `insi` — the TCP local port lives at `soi_proto.pri_tcp.tcpsi_ini.insi_lport` and is valid ONLY when `soi_kind == SOCKINFO_TCP` (verified against the aosm/xnu `bsd/sys/proc_info.h` declarations: union members `pri_in/pri_tcp/pri_un/pri_ndrv/pri_kern_event/pri_kern_ctl`, `struct tcp_sockinfo.tcpsi_ini`, `struct in_sockinfo.insi_lport`, kind enum `SOCKINFO_GENERIC=0,IN=1,TCP=2,UN=3,NDRV=4,KERN_EVENT=5,KERN_CTL=6`). `c_socket_fields` is now STATUS-RETURNING (0 = TCP record read through `pri_tcp.tcpsi_ini.insi_lport`; 1 = documented non-TCP kind, decoded without ANY union interpretation, `RawLPort = -1` non-port sentinel; -1 = kind outside the SDK-documented set -> new typed `errSocketKindUndocumented`, the walk fails closed). `socketRecord.KindIsTCP` gates `isTCPListenerOn` first; the Go-side ntohs convention and `localPortOf` are unchanged. The libproc stability documentation is corrected: the file header now quotes Apple's libproc.h verbatim ("private interfaces ... subject to change"), the adapter is described as the single isolated consumer, and no stable-public-API claim remains. New native CGo-scoped regression tests (`identity_darwin_cgo_test.go`): TCP record decodes the bound port through the repaired path, UDP/AF_UNIX records skip without union interpretation, own-process listener attribution, IPv6 listener attribution, proven-free-port typed no-owner. Evidence classes: source/static + C-syntax check against faithful SDK-mirroring stubs + Go-side mock-surface typecheck/vet (GOOS=darwin) are LOCAL SUPPLEMENTARY ONLY; native Apple SDK compile/test = NOT RUN (no Apple SDK on this Linux host); the CGO-free darwin and GOOS=ios variants build clean.
- **Actions artifact policy** (the work order's second requirement): `build.yml` lost the matrix `Upload build evidence (ci-logs)` step and the collector's `SHEYTAN-LA-errors-warnings.txt` / `run-diagnostics.json` uploads; `codeql.yml` lost the `SHEYTAN-LA-codeql-all-logs.txt` / `codeql-run-diagnostics.json` uploads. The platform-build workflow's ENTIRE permitted artifact surface is now the ONE matrix package upload, gated on `steps.package-eligibility.outputs.upload-package == 'true'` + `always()`, staging ONLY `dist/SHEYTAN-LA-ALPHA-*` with `if-no-files-found: error` and `overwrite: true`. The collector keeps its API-based log collection and parsing LOCAL (workspace reports + step summary), keeps `scripts/ci/consolidate-run-log.sh`, and its fail-closed verdict step still fails the REQUIRED collector gate. CodeQL keeps Go+actions scanning, the security-and-quality suite, `upload: always` SARIF, the init-keyed analyze, and the ONLY permitted artifact (the security-alert report `reports/code-scanning-alerts.txt` in `export-alert-report`). Contracts redesigned in `internal/releasecontract`: `ValidateUploadArtifactContract` is a closed parsed-YAML allowlist (name+path marker bans: ci-logs, build-evidence, errors-warnings, run-diagnostics, codeql logs/indexes, test-results, junit, coverage, .jsonl, .log; exactly-one package upload with the eligibility condition); `ValidateFailureTolerantCollector` requires the collector to upload NOTHING while still running the real script and its verdict; `ValidateCodeQLWorkflowResilience` rejects any upload outside the security report; constants renamed (`ConsolidatedLogReportFileName` / `RunDiagnosticsReportFileName` local report files, `ForbiddenBuildEvidenceArtifactName` banned value); `ValidateUploadArtifactPins` unchanged in force (immutable 40-hex pins + synced comments still required for the remaining package upload). `artifact_naming_contract_test.go` redesigned MUTATION-based: every banned evidence class is injected into the REAL parsed workflow texts and must be rejected; the positive fixture is the single eligible package upload; the CRLF case is retained. `consolidate-run-log-fixtures.sh` clause O rewritten to the no-upload allowlist; all classification/redaction/determinism/coverage fixtures unchanged and green (39/0). Stress-gate anchor updated to the shared local-report constant. Stale "six" wording reconciled to the seven-target matrix in build.yml, the DAG contract and CI-TOPOLOGY.md.
- **SHEYTAN-Engine bounded fix** (`internal/engine/safetensors.go`): the data-section escape check compared `meta.DataStart+start > meta.DataEnd` — a uint64 ADDITION that wraps for offsets near 2^64. REPRODUCED first: a container with `data_offsets [18446744073709551608, ...+4]` was ACCEPTED and its tensor MATERIALIZED over aliased header bytes (probe logged offsets [92,96] against DataStart=100). The check now compares each offset against the data-section LENGTH (no addition, no wrap); `TensorFromSafetensors` additionally rejects reversed/escaped offsets from directly-constructed info with the typed `ErrMalformedContainer` instead of panicking or aliasing. Regression tests: near-2^64 container rejected; reversed direct offsets typed-error. Engine suite green, `-race` green. Capability labels unchanged and truthful: Safetensors = METADATA_INSPECTION_ONLY + tensor materialization; pretrained inference, GGUF, LiteRT-LM execution, hardware GPU backends remain explicitly unsupported.
- **Go-Gui responsive contract**: `TestSizeClassForBreakpointMatrix` added to `ui/gogui` — pins `sizeClassFor` at every boundary (360/430/600/768/900/1024/1280/1366/1920 plus the 599.9/899.9/1279.9 edges) and across the work order's §7.3 representative sizes. Pure, deterministic, display-free; NOT claimed as a rendering proof. Suite green (now 23+ tests).

Docs updated: `docs/architecture/CI-TOPOLOGY.md` (artifact policy, seven targets), `docs/architecture/GO_GUI_MIGRATION.md` (current test evidence incl. Fyne NOT RUN reason), `docs/architecture/sheytan-engine-architecture.md` (wraparound hardening note), `docs/Dev-Notes.md`, `docs/worklog.md` (2026-11-22 row), this handoff. `FILE_CATALOG.md`/`.json` regenerated through `go run ./scripts/gen-filecatalog`. Protected docs (`docs/agent.md`, `docs/ai-notes.md`, `docs/Dev-Notes.md`, `docs/worklog.md`, `docs/retired.md`) all intact.

Local gates green (linux/amd64, go1.27.4): gofmt repo-wide (`cmd/ internal/ ui/ scripts/`), `go mod verify`, `go mod tidy -diff` clean, `go vet` on proc/releasecontract/engine/gogui, suites `internal/proc` + `internal/releasecontract` + `internal/engine` (+ `-race`) + `ui/gogui`, collector fixtures 39/0, PackageGate fixtures 76/0 (pwsh 7.4.6 in-sandbox), StepInvocationContract, PowerShell installed user-space (tarball, no root). NOT RUN: hosted CI on the exact candidate (no remote operations authorized — final hosted state is NOT `HOSTED_GREEN`), native Apple SDK compile/tests for the Darwin fix (no macOS host; supplementary stub/mock checks only), Windows/macOS/Android/iOS lanes, Android emulator, iOS device signing, Fyne/internal-desktop suite (no GL headers / pkg-config gl / Wayland headers in the sandbox, no root to install), `go test ./cmd/...` (same missing cgo graphics deps), ImportAudit (Windows-only). Final status: PARTIALLY_VERIFIED (LOCAL_VERIFIED_HOSTED_CI_PENDING for the Linux-verifiable surface; the Darwin repair needs the hosted macOS/iOS lanes to leave PARTIALLY_VERIFIED).

## SUPERSEDED HANDOFF — runs 44663205295 + CodeQL 44663205241 (2026-11-21) — was superseded on 2026-11-22

Branch `work/alpha-repair`, base commit `bc6d722f6ec9506496e603bca2232b2a9e61dc95` (== main HEAD == the inspected baseline), clean tree, no remote operations. Hosted raw job logs: NOT retrieved (unauthenticated GitHub API rate-limited from this sandbox) — every failure was instead REPRODUCED locally with the exact commands.

Root causes fixed this pass (all locally reproduced/verified):
- **Windows fixture contract**: `Gate-Token` merged the stable token and its descriptive suffix into one string, so the BLOCKED_BY_UPSTREAM assertion compared a full sentence against the bare token (REPRODUCED with pwsh 7.4.6, exact work-order message). The parser now returns `{Lines, Token, Detail}`; the fixture suite pins token+suffix independently, the structured JSON result token/detail, malformed/unknown tokens, empty output, and the NEW production state `CONTRADICTORY_PACKAGE_STATE` (upstream marker present AND a package exists — both stories cannot hold). Suite: 76/0.
- **Android entrypoint**: `cmd/sheytan-mobile` under `android && cgo` had NO `func main` (main.go is `!android`; mobile_android.go only defined androidBoot) — the documented `runtime.main_main·f: function main is undeclared` gomobile failure. New `mobile_android_main.go` bridges the pinned x/mobile app event loop to the go-gui Pattern-B Android backend (SetWindow/SurfaceCreated/Start/Render per the pinned backend.go), shared window wiring moved to `mobile_window.go`; `golang.org/x/mobile` now pinned in go.mod at the SAME version the lane installs (`v0.0.0-20261117202452-47699113848a`). VERIFIED with the REAL pinned `gomobile build -target=android/arm64 -androidapi 24` under NDK r27b: APK produced (27 MB), structural checks pass (manifest + dex + arm64-v8a-only native lib + gomobile debug cert), NO emulator runtime test.
- **Darwin header defect**: `internal/proc/identity_darwin.go` included `<sys/limits.h>` (NOT in the Apple SDK) and read the XNU-private `PID_MAX` — the exact `fatal error: 'sys/limits.h' file not found` of the three Darwin lanes. The include and the C helper are gone; the pid-space bound is the explicit Go constant `darwinPIDSpaceUpperBound` (99999, mirrors the documented XNU PID_MAX). The C prelude passes a syntax check against a faithful public-API stub; native macOS compilation remains hosted-CI evidence.
- **CodeQL Go build**: build (a) `CGO_ENABLED=1 go build -json -tags nogogpu ./...` includes cmd/sheytan-renderprobe — the ONE package importing BOTH goffi and purego+cgo — the exact documented goffi-#72 unlinkable combination (REPRODUCED verbatim: `goffi_errno_location_stub: unhandled relocation ... (SDYNIMPORT) (R_CALL)`). Build (a) now compiles every package of `./...` EXCEPT the probe; build (b) still compiles AND links the probe under its documented `CGO_ENABLED=0 -tags nofakecgo` configuration, so extraction coverage is preserved (also fixed: `internal/releasegate` is test-only and broke `go build ./...` — a doc.go makes it compilable without touching the test). The `grep -m1 -E '"ImportPath|Err"'` handler is replaced by `scripts/ci/go-build-jsonl-diagnose.py` (parses build-fail/build-output events, prints full compiler output + first path:line:col).
- **iOS device upload**: the `hashFiles(...-blocked.txt) == ''` guard did not prevent the upload against the intentionally absent IPA. The upload decision is now an explicit inspectable step output (`steps.package-eligibility.outputs.upload-package`) that reads the markers and the package glob directly; the evidence upload stays unconditional. actionlint clean on both workflows.
- **SHEYTAN-Engine.go**: `internal/engine` (package engine) — the genuine engine vertical slice: typed Engine/Model/Session/Backend interfaces, checked tensor arithmetic, scalar CPU reference kernels (correctness baseline), canonical graph IR, seeded deterministic sampling, strict Safetensors header parsing (METADATA_INSPECTION_ONLY), structured progress events, bounded admission queue, benchmark harness with the §5.8 machine-readable record, and the REAL end-to-end fixture execution (`tiny-fixture-1`: real weights + real kernels + real sampling; weight-sensitivity test proves non-canned execution). 31 tests green. See docs/architecture/sheytan-engine-architecture.md.

Go-Gui production cutover: STILL NOT DONE — `ui/fyne` (49 files, 22 test files) remains the wired production shell, `ui/gogui` (33 files, 14 views, 5 test files) is the verified candidate whose suite passes. The flip is the deliberate next gate; do not claim it.

Local gates green (linux/amd64, go1.27.4): gofmt repo-wide, vet (environment-limited to the sandbox's missing GL/Wayland dev headers), headless suite, ui/gogui suite, engine suite, collector fixtures 39/0, PackageGate fixtures 76/0, StepInvocationContract, module tidy/verify, actionlint, the pinned gomobile APK build + structural checks. NOT RUN: hosted CI on the exact candidate, darwin/Windows/iOS native lanes, Android emulator launch, ImportAudit fixtures (Windows-only SystemRoot fixture). Final status: PARTIALLY VERIFIED ALPHA.

## SUPERSEDED HANDOFF — run 44547031698 + CodeQL 44547031334 (2026-11-20) — was superseded on 2026-11-21

Branch `work/44547031698-candidate`, base commit `600fd15a29de9d360cf9e513bcf22f0eabae1520` (== main HEAD), clean tree, no remote operations. Hosted raw job logs: NOT retrieved (unauthenticated GitHub API rate-limited) — recorded as an inaccessible evidence source; every §3 failure was re-verified in source instead.

Per-platform CGO/build configuration (the §19 Appendix-A constraint matrix, now verified against the pinned dependencies):
- **Linux desktop (fyne, CGO_ENABLED=1)**: builds `-tags nogogpu` — goffi v0.6.8 documents (issue #72) that goffi beside other cgo code fails to link on Linux with `unhandled relocation ... (SDYNIMPORT)`; the render service is excluded on Linux and publishes the honest not-wired state through internal/renderstate. CPU/Go-Gui path remains fully functional.
- **macOS / Windows desktop (CGO_ENABLED=1)**: fyne + goffi in one binary — upstream documents the combination as supported on these platforms; the plain desktop build is the per-run combined-link regression proof.
- **cgo-free targets (CGO_ENABLED=0)**: purego + goffi in one binary duplicate `_cgo_init`; the documented Workaround A is `-tags nofakecgo`. PROVEN in this session: cmd/sheytan-renderprobe (ONE binary importing both stacks) LINKS AND RUNS under `CGO_ENABLED=0 go build -tags nofakecgo`; the negative control without the tag fails with the verbatim `duplicated definition of symbol _cgo_init` error. The probe's `-render` mode executed the real software verification job through the combined link (backend=gogpu-software, state=execution_verified, 24.8 ms — CPU-class evidence, never a GPU claim).
- **Darwin desktop (GOOS=darwin, CGO)**: internal/proc now carries the libproc CGo bridge compiled against the ACTUAL SDK headers (proc_listallpids / proc_pidinfo PROC_PIDLISTFDS+PROC_PIDTBSDINFO / proc_pidfdinfo PROC_PIDFDSOCKETINFO / proc_pidpath); the previous hand-transcribed SYS_PROC_INFO implementation had a definite flavor bug (PROC_PIDTBSDINFO written as 3 — the header value is 4) and an unverifiable socket_fdinfo layout. CGO-free darwin builds and GOOS=ios carry typed-unsupported/contained identity providers (the iOS sandbox forbids process-wide ownership inspection).
- **Android/iOS**: the APK command is `gomobile build` (the pinned doc.go: build compiles the app, bind builds a library/AAR), artifact `SHEYTAN-LA-ALPHA-android-arm64.apk`, arm64-v8a single ABI, debug-signed by gomobile's embedded debug certificate (no keystore file exists). The iOS device IPA lane (ios-device-arm64) is signing-gated: without the five Apple secrets it records BLOCKED BY MISSING APPLE SIGNING CONFIGURATION and never fabricates an IPA.

GoGPU adoption evidence (§10.1): the software path is REAL and executed through the combined link (probe -render). Adoption remains NOT granted: the workload/value comparison, lifecycle/device-loss/resize gates and per-backend hardware proof are still open. GoGPU is optional and never blocks startup (nogogpu builds boot clean).

Wails scope: unchanged — internal/webworkspace is the typed service/policy adapter; the experiments/ui-engine-poc/wails-webview POC module builds; Wails mobile stays experimental and outside the stable matrix.

Fyne retirement status: NOT STARTED — Fyne is still the production shell; ui/gogui builds and its tests pass but per-screen parity is incomplete.

Local gates green (linux/amd64, go1.27.4): gofmt, vet (cgo-affected desktop packages excluded environmentally), stress 47/47, internal headless suite 74/74, releasecontract suite, ui/gogui suite, collector fixtures 39/0 byte-identical, module verify. NOT RUN: hosted CI on the exact candidate, darwin-native tests, Windows/macOS lanes, Android emulator, iOS simulator/device install, PowerShell fixture suites (no pwsh here). Final status: PARTIALLY VERIFIED ALPHA.
