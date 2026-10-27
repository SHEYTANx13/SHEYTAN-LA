# SHEYTAN-LA

**A complete local AI agent that runs on your machine.**

> The model proposes. The tools execute. The laboratory verifies.

SHEYTAN-LA is a full-featured local AI agent designed to work like the popular free and paid cloud agents you already know — chat, tools, long-horizon goals, memory, research, coding, automations — except everything stays on your computer. No account required. No data leaves your machine unless you explicitly enable a remote provider.

One Go process owns the entire runtime (runs, tools, sessions, memory, approvals, policy). The desktop is a single native Go + Fyne application running in-process against that core over a secured per-user local IPC channel. Managed llama.cpp inference, an optional native C++ engine path, controlled tools, isolated coding workspaces, Net Search, memory, recall, objective verification, and a Runtime Governor that continuously understands the machine and keeps the host responsive.

**This software is the sole original work of the Licensor (SHEYTAN and Parsaetak).** It was conceived, designed, implemented, and iterated entirely on a personal home device. The great majority of the development work was performed without a traditional IDE — by coordinating multiple free web-based AI agents that read the repository, plan changes, edit files, run verification, and produce commits for human review. No corporate team, no external contractors, no institutional funding.

**SHEYTAN™ is a trademark of SHEYTAN and Parsaetak · © 2024–2026 SHEYTAN and Parsaetak. All rights reserved.**

Licensed under a conservative mixed model — Apache-2.0 for explicitly designated open components, the **Proprietary License** for SHEYTAN-specific material. The complete licensing picture lives in `LICENSE.md`.

```text
Application:      SHEYTAN-LA (SHEYTAN Local Agent)
Executable:       SHEYTAN-LA.exe
AppUserModelID:   Parsaetak.SHEYTAN-LA
Branch:           main
```

---

## What SHEYTAN is

SHEYTAN is not an Ollama or LM Studio wrapper. It is a complete local agent laboratory: the model proposes, the tools execute, and the laboratory verifies. It is built to deliver the same class of capability users expect from modern free and paid cloud agents — persistent sessions, tool use, long-horizon goals, memory, research, coding assistance, and automations — while remaining fully local-first and under the user’s control.

The product is Windows-first and local-first. Computation, memory, and knowledge stay on the user’s machine.

### Architecture in one paragraph

One Go process owns the runtime: a single engine lifecycle over managed llama.cpp (with the native C++ engine as a validated alternative path), one run lifecycle (fresh and resumed runs share one registry, one hub, one authoritative state), one session store, one tool registry, one scheduler, one downloader, one installer/update path, and one memory authority. The UI is the single Go + Fyne application (`ui/fyne`) running as an in-process client of the same core over the secured local IPC endpoint — no second UI implementation, no embedded browser, no Qt runtime. A Runtime Governor folds live-pressure telemetry into a policy authority; it owns policy only; every action stays with its existing subsystem. Full truth: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

### Deterministic data analysis

The single `dataAnalysis` tool is the application’s data authority. It loads CSV/TSV/JSON with type inference and a parse-once numeric cache, then performs deterministic in-process analysis:

- `analyze` — schema, missingness, statistics, top categories, correlations, outliers, key findings
- `aggregate` — multi-aggregation over multiple grouping columns
- `join` — inner/left/right/full with explicit keys
- `quality` — diagnostics
- `export` — CSV/TSV/JSON artifacts

Results are compact and model-oriented (key findings + artifact paths, never raw-row dumps). Every result is byte-for-byte deterministic for the same dataset. Backend: pure Go, in-process, honest 256 MB input bound; no external engine, no SQL surface.

### Live generation surface

While a run is active, exactly one assistant surface shows what is really happening:

- Real lifecycle phase (Preparing → Thinking → Generating → Finalising)
- Latest cumulative answer with a live cursor
- The model’s own emitted reasoning when it streams one
- Bounded runtime activity (tools, context, engine state)
- A backend-truth memory line that states exactly what the memory authorities actually injected this turn

Streamed text reaches that surface through a triple-boundary flush behind one coalescing latch. The server-side run hub delivers through a bounded, conflation-aware queue that keeps the newest cumulative snapshot under backpressure. A stalled transport can no longer freeze the visible text at a stale prefix.

### Model self-knowledge

Ask “what tools do you have?”, “what can you do?”, or “what model are you running?” and the model answers from a measured description of the actual runtime — the live tool registry, the loaded model’s parsed card, the serving backend and engine build, measured hardware (GPU/NPU labeled detection-only), and which memory systems exist. The answer never triggers web research or blind recall; it is composed from the same authorities the UI itself reads.

### Runtime Governor

The Governor (`internal/governor`) is fed by the existing live-pressure monitor (one sampler, one cadence, one protection path). It computes:

- **Resource state** — measurable current reality with explicit unknowns, including the current inference workload’s memory model
- **Envelope** — what may be admitted and what should be reduced, with the adjustment class that says how a recommendation can honestly be applied
- **Admission** — heavyweight workloads and model loads are admitted against a resident budget; unknown facts refuse conservatively; the run gate consults this envelope before any engine start
- **Self-model** — verified facts with provenance, served through `/api/governor` and rendered in the System Centre

Critical-pressure protection remains cooperative cancellation of active runs — never process killing.

---

## Verified capabilities

The following are implemented and verified in this repository:

- Local llama.cpp inference with managed engines, modern CLI contract, and strict numeric-argument validation before spawn
- Native C++ engine path with real generation for validated llama-architecture models and honest fallback
- Pause / edit / resume with durable checkpoints, edit journaling, restart recovery, and one authoritative run
- Sessions, memory, recall, skills, repository indexing, research, browser, sandbox, custom tools, coding lab, automations
- Optional 3D Studio — a read-only 3D map of the live system topology rendered with a locally embedded Three.js release in the user’s browser (loopback-only bind, per-launch fragment token, strict CSP, zero commands, honest textual fallback without WebGL). Full contract: [`docs/3dstudio.md`](docs/3dstudio.md)
- Session-list generation guard — every list write goes through one monotonic generation so stale responses can neither resurrect a deleted session nor drop a created one
- Live streaming that cannot wedge — triple-boundary flush + server-side conflation-aware queue
- Four-level reasoning-depth control (Low / Mid / High / Ultra) with real numeric thinking-token budgets on the generation request
- Zero-session Send — deleting the final session leaves a usable zero-session state; Send creates and activates a session in the current mode
- Monotonic context-refresh generation — stale context responses can never overwrite newer context state
- AUTO GPU posture contract with honest detection-vs-execution separation
- Live resource monitoring with hysteresis and cooperative critical protection
- Runtime Governor policy loop with deterministic explanations
- Runtime self-model for capability questions, composed from the live registry and the parsed model card

Windows CI runs the full Go build/test/smoke gates (MSYS2 UCRT64 CGO for the Fyne desktop binary). Linux CI runs the headless gates plus the Fyne UI suite and the real-window Xvfb smoke. **Neither is a claim of physical-PC runtime verification** — CI is CI. Every run consolidates all job logs into one plain-text artifact.

---

## Installation

Windows-first. Download the release asset for your machine’s variant and run `SHEYTAN-LA.exe`. First run performs setup (data root, engine provisioning through the authoritative variant resolver — no silent CPU fallback for explicit variant requests).

- `SHEYTAN-LA.bat` offers a console session
- `SHEYTAN-LA.exe serve` runs the headless server

Models are managed in-app (the downloader is the single download authority). GGUF files can also be imported from disk.

### Build from source

One toolchain, no Node. Requires Go 1.26+ and the Fyne native build prerequisites:

- C compiler (gcc / MinGW-w64 / MSVC)
- `pkg-config`
- Desktop graphics development libraries

  - Linux: `libgl1-mesa-dev`, `xorg-dev` components including `libxxf86vm-dev`, `libwayland-dev`, `libxkbcommon-dev`
  - Windows: MSYS2 MinGW-w64 packages (UCRT64) per docs.fyne.io

```bash
go build ./cmd/sheytan-local-agent          # desktop binary
go build -tags headless ./cmd/sheytan-local-agent  # API-only variant
```

The UI codebase is portable to Android/iOS by architecture (Fyne targets both), but shipping those platforms requires their SDKs/toolchains and is not claimed as verified here.

---

## Configuration

Live configuration is one concurrency-safe source (`config.Source`) shared by every consumer — engine variant, model, sampling, thinking, tool policy, net-search, update schedule, data root. The UI’s Settings surface edits the same source; `/api/config` is the machine surface. Context planning adapts the window per session policy. The Runtime Governor’s envelope is a policy read-model, not a configuration file.

---

## Truthful limitations

- The Governor is a **policy** authority: it computes envelopes, admission decisions, and explanations. Enforcement lives in the existing subsystems and is wired where control points exist. It is not yet a full closed-loop actuator over every background subsystem.
- CPU load is measured on Linux (normalized load average) and on Windows (kernel32 GetSystemTimes delta). Other platforms report unknown and CPU policy stays off — never a fabricated utilization number.
- GPU/NPU facts on all surfaces are detection-level. Execution evidence is the measured runtime offload line or a still-valid execution receipt. The truth model enforces `GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU verified` end-to-end. Physical GPU execution is only claimed when the runtime evidence exists on real hardware.
- Reasoning-depth budgets are real request-level parameters on the llama.cpp serving backend. A non-thinking model simply ignores the budget. The native C++ path has no reasoning-budget control and the level is inert there.
- The engine is supervised machinery: it boots on first use, restarts after engine-affecting changes, and recovers on its own. The UI represents its state.
- CI (Windows and Linux) is build/test evidence, not physical-host runtime evidence.
- Future roadmap items (advanced computer-use, evaluation framework, multi-model routing, etc.) are not implemented. See [`docs/ROADMAP.md`](docs/ROADMAP.md).

---

## Documentation

| Document | Purpose |
|----------|---------|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Architecture truth table for what is built |
| [`docs/FEATURES.md`](docs/FEATURES.md) | Single feature inventory (implemented vs coming) |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | Capability-driven direction |
| [`docs/3dstudio.md`](docs/3dstudio.md) | Optional 3D Studio contract |
| [`docs/agent.md`](docs/agent.md) | Agent operating manual (process, efficiency rules, definition of done) |
| [`docs/Dev-Notes.md`](docs/Dev-Notes.md) | **Human-only** curated open items and priorities |
| [`docs/ai-notes.md`](docs/ai-notes.md) | **AI agents only** — working notes, evidence, hand-offs |
| [`docs/UPDATE.md`](docs/UPDATE.md) | Current maintenance and update behavior |
| [`docs/changelog.md`](docs/changelog.md) | Canonical release history |
| [`docs/retired.md`](docs/retired.md) | Retired / legacy mechanisms (still under Proprietary License) |
| [`SECURITY.md`](SECURITY.md) | Security policy and vulnerability reporting |
| [`LICENSE.md`](LICENSE.md) | Full licensing model and legal texts |

Release history lives **only** in `docs/changelog.md`. This README describes the current product.
