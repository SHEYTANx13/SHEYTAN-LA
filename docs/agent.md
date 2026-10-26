# Agent Operating Manual

This manual guides an engineering agent through the SHEYTAN-LA repository. It states what the project is, where things live, how the work is verified, what proof means, and where to write. Every line is verifiable from the tree or from the hosted evidence it cites.

Status narrative lives in:

- `docs/changelog.md` — the only release history
- `docs/worklog.md` — session log with evidence classes
- `docs/UPDATE.md` — current maintenance behavior
- `docs/ROADMAP.md` — current / future boundary

Architecture truth: `docs/ARCHITECTURE.md`.

---

## 1. What this is

SHEYTAN-LA is a local-first AI engineering laboratory — not an Ollama or LM Studio wrapper.

One Go core owns every authority exactly once (policy, runs, sessions, tools, approvals, memory, Governor) and serves its API over the secured `internal/localipc` channel (per-user Unix socket / named pipe, transport-level identity checks, versioned keyed handshake `sheytan-ipc/1`, bounded framing).

The desktop is a single native Go + Fyne application (`ui/fyne`) running **in-process** against that core. There is no second UI language, no embedded browser, no Qt runtime, and no React/Vite frontend.

Managed llama.cpp and a native C++ engine (`native/`) serve inference behind one backend contract. The native engine default remains evidence-gated.

Motto: **“The model proposes. The tools execute. The laboratory verifies.”**

### Durable invariants (all releases)

- One authority per concern
- No sleeps / timing hacks / retry storms — deterministic tests synchronize on authoritative state
- Build / typecheck ≠ runtime proof
- Device detection ≠ device execution
- Model output ≠ objective evidence
- No fake autonomy (everything bounded by permissions, approvals, workspace policy, the Governor, cancellation, pause and verification)
- No fake long-context
- No model-specific hard-coding
- The codename-removal gate stays enabled
- Release identity flows from the single Go authority (`internal/config.AppVersion`)

---

## 2. Repository map

| Path | Owns |
|------|------|
| `cmd/sheytan-local-agent/` | Entrypoints (desktop + headless) |
| `cmd/printversion/` | CGO-free version resolver used by CI |
| `internal/` | The Go core: `api`, `config`, `desktop`, `localipc`, `agent`, `aisystem`, `goal`, `approval`, `governor`, `llm`, `sessions`, `memory`, `tools`, `accelerator`, `studio`, `updater`, … |
| `ui/fyne/` | The one product UI (Go + Fyne) |
| `native/` | The C++ engine (CMake) and its parity/acceptance tests |
| `scripts/` | CI helpers, consolidate-run-log, stress, codename gate |
| `packaging/` | Launcher and NSIS installer |
| `baselines/` | Hosted-class perf baselines (fail-closed host classification) |
| `docs/` | All documentation |
| `.github/workflows/build-desktop.yml` | The pipeline |

---

## 3. The verification ladder

Run in this order before claiming anything:

1. `gofmt -l .` — must print nothing
2. `go build -tags headless ./...`
3. `go test ./internal/... -tags headless -count=1`
4. `go test ./... -count=1`
5. `go test -race -tags headless -count=1` on concurrency-heavy packages (`api`, `agent`, `sessions`, `runtime`, …)
6. `go vet ./...`
7. `go test ./ui/fyne/ -count=1` (requires Fyne graphics development libraries)
8. Stress suite (`go run -tags headless ./cmd/sheytan-local-agent stress`)
9. Version gate via `cmd/printversion`

Windows cross-proof (compile-level only): `GOOS=windows go build ./...` + `GOOS=windows go vet ./...`.

---

## 4. Evidence classes

- **Local static proof**: compile, typecheck, unit/test runs, GOOS cross-builds. This is the only class available for some surfaces in a Linux-only environment.
- **Hosted CI runtime proof**: a green GitHub Actions job on the exact revision. The hosted job is the only runtime authority for Windows packaging and full desktop smoke.
- **Physical proof**: real-hardware measurements. The perf gate is fail-closed about hosts: `cmd/shtn-perf` requires an explicit `--host-class physical|hosted` and refuses a physical claim over detected hosted-CI markers.

A compile proof never upgrades to a runtime claim. State the class with every claim.

---

## 5. The code-before-docs discipline

Documentation never closes a failure item. Every failure closes as a code or workflow diff, or as an evidenced NOT-REPRODUCIBLE (with the reproduction transcript). A failure item “closed” by documentation edits alone is an automatic workflow failure.

The mirror rule: do not edit anything the evidence does not name. Do not weaken a test, gate, or assertion to make a run green. Fix forward only what the run’s named evidence names.

---

## 6. The CI contract

- Jobs: audit → Windows x64 (UCRT64) + Linux x64 → release (tag-gated only)
- Every gate is a required gate
- Consolidated one-file run-log artifact on every run (red and green)
- Fyne desktop smoke under Xvfb on Linux
- No publish on a plain main push — only on a version tag

---

## 7. The push protocol

The agent has no push credentials. The agent prepares commits (code + workflow + docs) and hands them to the operator. The operator pushes main, reads the run, then — once main is fully green — tags and pushes the tag for the publish job.

Never ask the agent to push; never fabricate a hosted result; record each run’s verdict with its evidence class in `docs/worklog.md` only after reading the summary/annotations.

---

## 8. Protected surfaces (do not churn without evidence)

- The one-Go-UI (Fyne) and the local IPC seam
- The execution/evidence ladder and GPU truth model
- The Runtime Governor admission path
- The Goal engine terminal settlement guards
- The session-list generation guard and zero-session Send contract
- The stress suite and release-surface gates
- The consolidated run-log collector
- The codename-removal gate

---

## 9. Where to write what

| File | Purpose |
|------|---------|
| `docs/changelog.md` | The only canonical release history |
| `docs/worklog.md` | Session log: Task ID, evidence classes, sweeps, fixes, run verdicts |
| `docs/UPDATE.md` | Current maintenance and update behavior |
| `docs/agent.md` | This manual — durable operational truth only |
| `docs/ROADMAP.md` | Current / future boundary |
| `docs/ARCHITECTURE.md` | Architecture truth (seams, IPC, engine contract) |
| `docs/FEATURES.md` | Single feature inventory |

Version identity lives in `internal/config.AppVersion`. Never hand-edit derived surfaces.
