# Agent Operating Manual

This manual is the durable operating guide for any AI agent working on SHEYTAN-LA.

**Two non-negotiable rules**

1. The human’s current prompt / instructions always take precedence over any documentation, including this file and `Dev-Notes.md`.
2. Before any work, the agent must read `docs/Dev-Notes.md` and follow the Agent Start Checklist written there.

---

## 0. Development philosophy and method

SHEYTAN-LA is developed primarily through a multi-AI-agent workflow without a traditional IDE:

- Multiple AI agents (sequential or parallel) read the repository, plan, implement, verify, and document.
- The human operator reviews, decides, and is the only one who pushes.
- Agents edit files directly via tools, run verification when possible, and produce commits for human review.
- There is no required local IDE configuration and no assumption of a full graphical desktop for the agent.

The process is deliberately complete at every stage. Speed comes from discipline, not from skipping steps.

### World-class staged development process

Every non-trivial change must pass through these stages:

| Stage | Required activity | Exit criterion |
|-------|-------------------|----------------|
| **1. Orient** | Read human prompt + `Dev-Notes.md` + this manual + relevant architecture/roadmap | Success condition stated in one sentence |
| **2. Audit** | Inspect the exact code paths, tests, and docs that will be affected. Record what already exists and what is missing | Audit notes written (even if short) |
| **3. Plan** | List files to touch, tests to add/run, docs to update, evidence class expected | Plan is minimal and matches the success condition |
| **4. Implement** | Make the smallest correct change. One authority per concern. No drive-by refactors | Diff is focused |
| **5. Verify** | Run the verification ladder (or the relevant subset). Record evidence class | Tests pass or failure is honestly reported |
| **6. Document** | Update `Dev-Notes.md`, and every affected public doc, in the same change set | Docs match the code |
| **7. Hand-off** | Produce a clear summary: what changed, evidence, remaining risks, next recommended item | Human can decide without re-deriving context |

Skipping stages produces incomplete or brittle work. Agents are expected to complete every stage for the scope they accept.

### Definition of Done

A change is done only when all of the following are true:

- Success condition stated at the start is met
- Code (if any) compiles and relevant tests pass
- Evidence class is recorded (`local-static` | `hosted` | `physical`)
- `Dev-Notes.md` is updated
- Affected public documentation is updated in the same change
- No new version numbers appear in product-facing docs
- No speculative work outside the stated success condition
- Hand-off message is complete

---

## 1. What this is

SHEYTAN-LA is a local-first AI engineering laboratory — not an Ollama or LM Studio wrapper.

One Go core owns every authority exactly once (policy, runs, sessions, tools, approvals, memory, Governor) and serves its API over the secured `internal/localipc` channel.

The desktop is a single native Go + Fyne application (`ui/fyne`) running in-process against that core. There is no second UI language, no embedded browser, no Qt runtime, and no React/Vite frontend.

Managed llama.cpp and a native C++ engine serve inference behind one backend contract.

Motto: **“The model proposes. The tools execute. The laboratory verifies.”**

### Durable invariants

- One authority per concern
- No sleep-based correctness
- Build / typecheck ≠ runtime proof
- Device detection ≠ device execution
- Model output ≠ objective evidence
- No fake autonomy
- Codename-removal gate stays enabled
- Release identity flows from `internal/config.AppVersion`

---

## 2. Repository map

| Path | Owns |
|------|------|
| `cmd/sheytan-local-agent/` | Entrypoints |
| `cmd/printversion/` | CGO-free version resolver |
| `internal/` | Core authorities |
| `ui/fyne/` | One product UI |
| `native/` | C++ engine |
| `scripts/` | CI helpers, stress, gates |
| `packaging/` | Launcher and installer |
| `docs/` | All documentation (including Dev-Notes) |
| `.github/workflows/` | Pipeline |

---

## 3. Verification ladder

1. `gofmt -l .`
2. `go build -tags headless ./...`
3. `go test ./internal/... -tags headless -count=1`
4. `go test ./... -count=1`
5. Race tests on concurrency-heavy packages
6. `go vet ./...`
7. `go test ./ui/fyne/` (when UI is affected)
8. Stress suite
9. Version gate via `cmd/printversion`

---

## 4. Evidence classes

- **local-static** — compile, unit tests, cross-build
- **hosted** — green GitHub Actions job on the exact revision
- **physical** — real-hardware measurements (fail-closed host classification)

Never upgrade a lower class to a higher class by assertion.

---

## 5. Documentation discipline

- Docs must be based on code and evidence, not on intention or aspiration.
- Agents must check all relevant docs before changing behaviour.
- Every behavioural change that is user-visible or architectural must update the corresponding public doc in the same change set.
- `Dev-Notes.md` is the living task list; public docs describe the product.

---

## 6. CI and push protocol

- Agents never push.
- Operator pushes main, reads the run, then tags for release when green.
- Consolidated run-log artifact is required on every run.

---

## 7. Protected surfaces

Do not touch without an explicit item in Dev-Notes or a direct human instruction:

- One-Go-UI and local IPC seam
- Execution / evidence ladder and GPU truth model
- Runtime Governor admission path
- Goal engine terminal settlement
- Session-list generation guard and zero-session Send
- Stress suite and release-surface gates
- Codename-removal gate

---

## 8. Where to write what

| File | Purpose |
|------|---------|
| `docs/Dev-Notes.md` | Mandatory living checklist + start process |
| `docs/agent.md` | This manual |
| `docs/ARCHITECTURE.md` | What is built |
| `docs/ROADMAP.md` | Direction and capability families |
| `docs/FEATURES.md` | Implemented vs coming inventory |
| `docs/changelog.md` | Release history only |
| `docs/worklog.md` | Session log |
| `docs/retired.md` | Legacy mechanisms still under license |
| `README.md` | Current product description |

---

*Follow the human prompt first. Follow this process second. Produce evidence. Keep the repository honest.*
