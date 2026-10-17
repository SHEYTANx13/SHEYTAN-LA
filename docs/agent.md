# SHEYTAN-Local-Agent — Operating Manual for Engineering Agents

This manual GUIDES an agent through this repository. It states what the
project IS, where things live, how the work is verified, what proof means,
and where to write. Every line is verifiable from the tree or from the
hosted evidence it cites. Status narrative lives in `docs/changelog.md`
(the ONLY release history), `docs/worklog.md` (session log with evidence
classes), `docs/UPDATE.md` (current release notes + maintenance behavior),
and `docs/ROADMAP.md` (current/future boundary). Architecture truth:
`docs/ARCHITECTURE.md`.

Current release identity: **1.10.13** — version-only, no codename. The
canonical gate is `node scripts/release-version.mjs --check` (package.json
is the single source of truth; the audit job resolves it at runtime and
every other job consumes it via needs-outputs).

---

## 1. What this is

SHEYTAN-LA is a local-first AI engineering laboratory — NOT an
Ollama/LM Studio wrapper. ONE Go core owns every authority exactly once
(policy, runs, sessions, tools, approvals) and serves its API over the
secured `internal/localipc` channel (per-user Unix socket / named pipe,
transport-level identity checks, versioned keyed handshake
`sheytan-ipc/1`, bounded framing). The native Qt Quick/QML host
(`ui/qt`) is a CLIENT of that core: the packaged pair is the Go core
launcher (`SHEYTAN-LA.exe`, built `-H=windowsgui` — it owns NO top-level
window) plus the spawned Qt host child (`sheytan-ui.exe`, resolved NEXT
TO the core per `internal/desktop/desktop.go`, which waits on the child
unconditionally and exits with the child's exit code). A React/TypeScript
frontend (`src/`, embedded) serves the headless/browser mode for CI,
containers and tooling. Managed llama.cpp and a native C++ engine
(`native/`) serve inference behind ONE backend contract; the native
engine default is evidence-gated (physical-Windows warm-TTFT comparison,
see ROADMAP). Wails is REMOVED from the product, the build, and the
module graph. Motto: **"The model proposes. The tools execute. The
laboratory verifies."**

Durable invariants (all releases): one authority per concern; no
sleeps/timing hacks/retry storms — deterministic tests synchronize on
authoritative state; build/typecheck ≠ runtime proof; device detection ≠
device execution; model output ≠ objective evidence; no fake autonomy
(everything bounded by permissions, approvals, workspace policy, the
Governor, cancellation, pause and verification); no fake long-context;
no model-specific hard-coding; the codename-removal gate stays enabled;
release identity flows from package.json through
`scripts/release-version.mjs` only. The R&D decision document
(`Parsaetak/Contents` → `Research-and-Development` →
`SHEYTAN-LA-2026-10-11.md`) remains the direction authority: Go stays
the single orchestration authority; C++ owns numerical inference.

## 2. Repository map

| Path | Owns |
|------|------|
| `cmd/sheytan-local-agent/` | Entrypoints: `main.go` plus `main_headless.go`, `main_other.go`, `main_stderr.go`, `main_windows.go` (the root package is EMPTY — v1.10.7 reshape) |
| `internal/` | The Go core: `api` (HTTP + IPC server), `config`, `desktop` (launcher/child lifecycle), `localipc` (THE pipe/socket transport contract), `multiagent` (bounded subtasks), `perf`, `releasecontract` (canonical package-root truth mirrored by CI), `updater`, `platform`, plus the authority packages (`agent`, `aisystem`, `goal`, `approval`, `governor`, `llm`, `sessions`, `memory`, `tools`, `accelerator`, `testfakes`, …) |
| `native/` | The C++ engine (CMake) and its parity/acceptance tests |
| `ui/qt/` | The Qt 6.8 host: `CMakeLists.txt`, `src/` (C++), `qml/` (`Main.qml`, `EmptyState.qml`), `tests/` (`tst_acceptance.cpp` — the QtTest acceptance suite driving the REAL Main.qml against the REAL core) |
| `src/` + `web/` | The embedded React/TS frontend; `web/static` is the verified embed surface |
| `scripts/` | `release-version.mjs` (identity), `verify-all.mjs` (the ONE canonical local verification entrypoint), `qt-desktop-smoke.sh` (Linux G1–G5 smoke), `fake-remote-engine.py` (acceptance fixture engine), `verify-static-assets.mjs`, `sync-web.mjs`, `codename-gate.mjs`, `stress-main/` (stress suite) |
| `packaging/` | `SHEYTAN-LA.bat` (launcher source — staged into every package) and `nsis/installer.nsi` (the NSIS installer contract) |
| `e2e/` | Playwright specs over the REAL stack (`zero-session.spec.ts`, `ai-systems.spec.ts`) + `make-e2e-model.py` fixture |
| `baselines/` | Hosted-class perf baselines (fail-closed host classification; physical claims refused without physical hardware) |
| `build/` | `config.yml` (build config; Wails is absent by contract) |
| `docs/` | ALL documentation: `ARCHITECTURE.md`, `ROADMAP.md`, `UPDATE.md`, `agent.md` (this manual), `changelog.md`, `worklog.md`, `CONTRIBUTING.md` |
| `.github/workflows/build-desktop.yml` | The pipeline: audit → Windows x64 + Linux x64 → release (tag-gated) |

## 3. The verification ladder

Run in this order before claiming anything (the audit job runs exactly
this shape; `scripts/verify-all.mjs` is the local wrapper):

1. `npm ci`
2. `npm run typecheck` — `tsc --noEmit`
3. `npm run lint` — oxlint
4. `npm run test:units` — frontend unit suites (node --test)
5. `npm run test:release` — release-version + codename-gate tests
6. `npm run build` — `tsc -b && vite build && node scripts/sync-web.mjs`
7. `node scripts/verify-static-assets.mjs --dist` — embedded-frontend checks
8. `gofmt -l .` — MUST print nothing (CI gates this on BOTH platforms)
9. `go test ./internal/... -tags headless -count=1`
10. `go test ./... -count=1`
11. `go test -race -tags headless -count=1 ./internal/api ./internal/agent ./internal/sessions ./internal/contextplan ./internal/histref ./internal/runtime` — the concurrency-heavy packages
12. `go vet ./...`
13. Windows cross-proof: `GOOS=windows go build ./...` + `GOOS=windows go vet ./...` (compile-level only — see §4)
14. `node scripts/verify-all.mjs` (profiles: `--go`, `--native` CTest, `--qt`, `--release`)
15. The stress suite (`scripts/stress-main`) — Linux CI runs it end to end
16. `bash -n scripts/qt-desktop-smoke.sh` — syntax gate for the Linux smoke
17. `node scripts/release-version.mjs --check` — identity gate

Local Qt sanity check when a Qt 6.8 toolchain exists: resolve qmake,
derive the prefix (qmake → bin parent → grandparent), assert
`lib/cmake/Qt6/Qt6Config.cmake`, then `cmake -S ui/qt -B ui/qt/build
--fresh -DCMAKE_BUILD_TYPE=Release -DCMAKE_PREFIX_PATH=<prefix>` and
build. Without a toolchain, record the evidence class honestly and let
the hosted job rule.

## 4. Evidence classes

* **Local static proof**: YAML parse + actionlint, script review,
  compile/typecheck/unit/test runs, GOOS cross-builds, CMake configure
  where a toolchain exists. This is the ONLY class available for the
  Windows C++/Qt and packaging surfaces in a Linux-only environment.
* **Hosted CI runtime proof**: a green GitHub Actions job on the exact
  revision. The hosted job is the ONLY runtime authority for Windows
  (GUI, MSVC, NSIS, windeployqt) — local green is not hosted green.
* **Physical proof**: real-hardware measurements (warm TTFT etc.). The
  perf gate is FAIL-CLOSED about hosts: `cmd/shtn-perf` requires an
  explicit `--host-class physical|hosted` and refuses a physical claim
  over detected hosted-CI markers.
* A compile proof NEVER upgrades to a runtime claim. State the class
  with every claim — in summaries, commits, and `docs/worklog.md`.
* Hosted evidence for a credential-less agent: the PUBLIC step summary
  (`$GITHUB_STEP_SUMMARY`, no login required) and the check-run
  annotations API (also anonymous). Step LOGS are admin-gated (403) —
  never depend on them. Fix-forward reads ONLY the summary +
  annotations + uploaded artifacts.

## 5. The code-before-docs discipline

Documentation NEVER closes a failure item. Every failure closes as a
code or workflow diff, or as an evidenced NOT-REPRODUCIBLE (with the
reproduction transcript). A failure item "closed" by documentation edits
alone is an automatic workflow failure. The mirror rule: do not edit
anything the evidence does not name (PROOF-BEFORE-EDIT — sweep the
affected surface and record the sweep in `docs/worklog.md` BEFORE
editing). Do not weaken a test, gate, or assertion to make a run green.
Fix forward ONLY what the run's named evidence names.

## 6. The CI contract (`.github/workflows/build-desktop.yml`)

* **Jobs**: `audit` (Source & frontend audit) → `build-windows` (Windows
  x64) + `build-linux` (Linux x64) → `release` (Publish and verify,
  `if: startsWith(github.ref, 'refs/tags/v')` — a main push can NEVER
  publish).
* **Gate semantics (v1.4.0, the run 35966462352-class repair)**: every
  gate is a REQUIRED gate — bash steps run under `set -euo pipefail`;
  pwsh steps assert `$LASTEXITCODE -ne 0` after EVERY native command.
  A green job therefore means the gated verification genuinely passed;
  failure-hiding (a native command failing silently) is the forbidden
  class.
* **ci-logs discipline (v1.10.13 W-VIS)**: every post-verification
  native command (cmake configure/build, windeployqt, the acceptance
  run, NSIS makensis) tees its own log into `ci-logs/*.log` and tracks
  `RUN`/`OK`/`FAIL` in `ci-logs/gate-order.txt`. On failure the
  end-of-job summary (positioned at the END of the job so
  `if: failure()` covers the ENTIRE job) names the failing gate from
  gate-order.txt and emits redacted, truncated tail lines as anonymous
  annotations. `gate-order.txt` is uploaded inside the failure artifact
  (W-VIS-2). Go verification keeps its own proven summary path (gofmt →
  headless → full → vet, each teed: win-gofmt.log, win-go-test-headless.log,
  win-go-test-full.log, win-go-vet.log).
* **Caps and secrets (rule 7)**: ≤ 10 annotations per step, ≤ 250 chars
  per line; NO IPC keys, capability tokens, runIds, sessionIds, or model
  content in annotations, summaries, or uploaded logs. Named-pipe
  endpoints are redacted wholesale; key-shaped assignments are masked.
  (The acceptance suite's engine key "acceptance" is a published fixed
  fixture string.)
* **The Qt prefix contract (W-PIN)**: install-qt-action@v4 prepends
  Qt's bin to PATH and exports NEITHER `Qt6_DIR` NOR
  `CMAKE_PREFIX_PATH`. The Windows Qt host configure therefore derives
  the prefix from qmake on PATH (grandparent of `<qt>/bin/qmake.exe`),
  asserts `<qt>/lib/cmake/Qt6/Qt6Config.cmake` exists, and passes
  `-DCMAKE_PREFIX_PATH` explicitly — never an env-var guess.
* **The runtime smoke contract (W-SMOKE)**: the Windows smoke asserts
  banner + boot markers in the isolated data dir, liveness, no crash
  report — then targets the ARCHITECTURE: a windowed `sheytan-ui` child
  (the core is `-H=windowsgui` and owns no window), `CloseMainWindow()`
  to the CHILD with its BOOL checked, child exit in 15s, CORE exit 0 in
  30s (the launcher's documented lifecycle), no orphan. This mirrors
  the Linux smoke's G1–G5 (Xvfb, xdotool window ownership,
  WM_DELETE_WINDOW, clean exit, no orphan, no leaked endpoint file).
* **Installer identity (W-INST-VER)**: NSIS's VIProductVersion is
  REQUIRED four-part X.X.X.X (makensis parses `%d.%d.%d.%d` and demands
  all four fields); `packaging/nsis/installer.nsi` carries
  `VIProductVersion "${VERSION}.0"` — the installer gate compares
  against `"$($env:APP_VERSION).0"`.
* **ZIP launcher contract (W-ZIP-LAUNCHER)**: the launcher source lives
  at `packaging/SHEYTAN-LA.bat` (the repo root carries none); the ZIP
  verify step gates on that path and REQUIRES the
  `${WIN_PKG_ROOT}/SHEYTAN-LA.bat` entry.
* **NSIS source contract**: the workflow pre-flight asserts seven
  fragments in installer.nsi (MUI_PAGE_DIRECTORY, "Create a &desktop
  shortcut", BST_CHECKED, WriteUninstaller, UninstallString,
  SHEYTAN_DATA_DIR, "preserve user data") and the ABSENCE of
  `RMDir /r` — user data is preserved by construction.
* **Publish contract (W-SUMS)**: the release job verifies the payload,
  writes ONE `SHA256SUMS.txt` naming ALL THREE published assets
  (`wc -l == 3` + awk assertion), generates `release-manifest.json`
  (validated JSON; consumed by `internal/updater/appupdate`), publishes
  via softprops/action-gh-release (version-only title, 5 files), then
  `gh release view` + jq verifies tag, non-draft, non-prerelease, and
  all five asset names. No manual GitHub Release — the publish job IS
  the pipeline's end.
* **The two-run protocol**: run #1 = the operator pushes main (full
  matrix green on main); run #2 = the operator pushes the `v<version>`
  tag (full matrix again, then the publish job's first execution). Each
  hosted run costs ~13–18 minutes. Version identity is FROZEN during a
  cycle — `release-version.mjs --check` must stay green; docs work is
  capped; the code fixes are the cycle.

## 7. The push protocol

The agent has NO push credentials. The agent prepares commits (code +
workflow + docs, each fix a minimal diff with its register citation)
and hands them to the OPERATOR. The operator pushes main, reads run #1,
then — once main is fully green — tags `v<version>` and pushes the tag
for run #2 (the first publish). Never ask the agent to push; never
fabricate a hosted result; record each run's verdict with its evidence
class in `docs/worklog.md` only after reading the summary/annotations.

## 8. Protected surfaces (do not churn)

* **The Linux chain**: fully green FIVE consecutive hosted runs
  (through run 39038248810, Linux x64, 9m50s, ZIP artifact produced).
  Do not restructure it to "fix" Windows.
* **Windows steps 1–21**: green two consecutive runs (Go verification
  green twice; engine variant assets, resources, executable build +
  console probe, executable verify, Qt install green at that shape).
* **The v1.10.13 visibility system**: the end-of-job failure summary +
  gate-order tracking — proven live by run 39038248810 (it named the
  failing gate anonymously from exactly three annotations).
* **Go pins**: the named-pipe transport repair chain (W1–W4 + W-C, the
  overlapped-IO contract, the WaitNamedPipe busy-dial budget), the
  desktop launcher unit pins (`internal/desktop/desktop_test.go`), the
  variant transaction order (early refusal → claim-free staged download
  → DESTRUCTIVE BOUNDARY claim → stop/swap/start/verify → commit or
  rollback), the admission seams (`UpdateEngineNow`, the governor CPU
  sampler seam, the streaming contract, `recoverRunFromIdle`).
* **The gates**: gofmt on BOTH platforms + the LF pin
  (`.gitattributes` `*.go text eol=lf` + Windows `core.autocrlf=false`),
  the E2E real-content proofs (`expectRealAssistantReply`), the stress
  suite, the acceptance python fallback (Q_OS_WINDOWS candidates
  {python, python3}), the NSIS source contract, the publish checksum
  contract.
* **Docs**: the condensed history (pre-1.9.x), the ROADMAP
  shipped-baseline paragraphs, the changelog's canon.
* Regression-sensitive detail (the full surface contracts) lives in
  `docs/ARCHITECTURE.md`, the pins in the test files themselves, and
  the per-release rationale in `docs/changelog.md`. Read those before
  touching: the variant transaction (`internal/llm/llama.go`), the
  loopback origin model (`/api/ws-origin` answers same-origin
  unconditionally for headless/browser mode), the settled-reply DOM
  selector (`.message-markdown`), typography floors (no `font-size`
  below 10px), the parity gate (`tests/test_prefill_parity.cpp` —
  bit-exact or a deliberate documented numerics change).

## 9. Where to write what

* `docs/changelog.md` — the ONLY canonical release history. New release
  entries only; no version bump without the operator's order.
* `docs/worklog.md` — the session log: Task ID, agent, task, evidence
  classes (local static / hosted / physical), sweeps, fixes, run
  verdicts. Evidence-first: quote hosted annotation lines verbatim
  BEFORE editing code.
* `docs/UPDATE.md` — current release notes + operational maintenance
  behavior for the CURRENT version.
* `docs/agent.md` — THIS manual. Durable operational truth only; keep
  status narrative out (it belongs to changelog/worklog/UPDATE).
* `docs/ROADMAP.md` — the current/future boundary. Hosted-status
  appends ONLY after the green run is read (Phase 8/9 discipline).
* `docs/ARCHITECTURE.md` — architecture truth (seams, IPC, engine
  contract). Never contradict it in code.
* Version identity: `package.json` via `scripts/release-version.mjs`
  (then `--check` must stay green). NEVER hand-edit derived surfaces —
  run the script's repair mode when the operator orders a bump.
