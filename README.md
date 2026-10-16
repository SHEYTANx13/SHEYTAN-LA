# SHEYTAN-LA™ (SHEYTAN Local Agent)

> **A local-first AI software-engineering laboratory.**
>
> The model proposes. The tools execute. The laboratory verifies.
> SHEYTAN measures the machine, governs the runtime and verifies the work.

SHEYTAN™ Local-Agent is a local-first desktop AI engineering environment built around a Go core (the single authority for runs, tools, sessions, memory, approvals and policy), a native Qt Quick/QML desktop UI that consumes that core over a secured per-user local IPC channel, managed llama.cpp inference, a native C++ engine path, controlled tools, isolated coding workspaces, Net Search, memory, recall, objective verification — and a Runtime Governor that continuously understands the machine and governs runtime policy while keeping the host responsive.

**SHEYTAN™ is a trademark of Parsaetak · © 2024–2026 Parsaetak. All rights reserved.**

Licensed under a **conservative mixed model** — Apache-2.0 for explicitly designated open components, the **Parsaetak Proprietary License v1.1** for SHEYTAN-specific material. The complete licensing picture — model, classification, notices, and BOTH full legal texts — lives in `LICENSE.md`, the sole licensing artifact.

```text
Application:      SHEYTAN-LA (SHEYTAN Local Agent)
Current release:  v1.10.8
Executable:       SHEYTAN-LA.exe
AppUserModelID:   Parsaetak.SHEYTAN-LA
Branch:           main
```

---

# What SHEYTAN is

SHEYTAN is NOT an Ollama/LM Studio wrapper. It is a laboratory: the model
proposes, the tools execute, and the laboratory verifies. The product is
Windows-first and local-first — computation, memory and knowledge stay on
the user's machine.

## Architecture in one paragraph

One Go process owns the runtime: a single engine lifecycle over managed
llama.cpp (with the native C++ engine as a validated alternative path), one
run lifecycle (fresh and resumed runs share one registry, one hub, one
authoritative state), one session store, one tool registry, one scheduler,
one downloader, one installer/update path and one memory authority. The
frontend is a React/TypeScript surface embedded into the binary. A Runtime
Governor folds the existing live-pressure telemetry into a policy
authority — it owns policy only; every action stays with its existing
subsystem. Full truth: `docs/ARCHITECTURE.md`.

### Deterministic data analysis (v1.8.7)

The one `dataAnalysis` tool is the application's data authority: CSV/
TSV/JSON load with type inference and a parse-once numeric cache, then
deterministic in-process analysis — `analyze` (one compact call: schema,
missingness, statistics, top categories, correlations, outliers, key
findings), `aggregate` (multi-aggregation over multiple grouping
columns), `join` (inner/left/right/full with explicit keys), `quality`
(diagnostics) and `export` (CSV/TSV/JSON artifacts). Results are compact
and model-oriented — key findings plus artifact paths, never raw-row
dumps — and every result is byte-for-byte deterministic for the same
dataset. Backend: pure Go, in-process, honest 256 MB input bound; no
external engine, no SQL surface (see `docs/ARCHITECTURE.md` §5 and
`docs/changelog.md` §v1.8.7 for the evaluated-and-rejected DuckDB path).

## The live generation surface

While a run is active, exactly ONE assistant surface shows what is really
happening — the real lifecycle phase (Preparing → Thinking → Generating →
Finalising), the latest cumulative answer with a live cursor, the model's
own emitted reasoning when it streams one, bounded runtime activity
(tools, context, engine state), and a backend-truth memory line
(`Memory: session summary · 2 recalled exchanges`) that states exactly
what the memory authorities actually injected this turn — no fabricated
counts, no invented reasoning. Streamed text reaches that surface through
a triple-boundary flush (an event-loop macrotask, a 0ms timer task and an
animation frame — three independent scheduling sources) behind one
coalescing latch, with a task controller that recovers from a lost or
suspended MessageChannel delivery instead of wedging: live text appears
without any user action, updates continuously, the run settles on its own
without depending on any single scheduling primitive, and the final
message replaces it exactly once. v1.8.5 closes the same failure class on
the SERVER side of the wire: the run hub now delivers through a bounded,
conflation-aware queue — a slow or backpressured subscriber's queue evicts
the OLDEST cumulative snapshot (subsumed by the newest) instead of
dropping the NEWEST one, so a stalled transport can no longer freeze the
visible text at a stale prefix until the run ends; every event write also
carries a generous deadline so a wedged client is torn down
 deterministically and recovers through the reconnect snapshot replay.

## Model self-knowledge

Ask "what tools do you have?", "what can you do?" or "what model are you
running?" and the model answers from a measured description of the actual
runtime — the live tool registry (with the concise description of each
offered tool, and disabled tools honestly named not-callable), the loaded
model's parsed card (architecture, quantization, parameters, context
limit, tokenizer family, chat-template support, vision only when real),
the serving backend and engine build, measured hardware with GPU/NPU
labeled detection-only, and which memory systems exist. The answer never
triggers web research or blind recall; the description is composed from
the same authorities the UI itself reads, so it cannot drift from what is
really enabled.

## Runtime Governor / self-model

The Governor (`internal/governor`) is fed by the EXISTING live-pressure
monitor — one sampler, one cadence, one protection path. It computes:

* **Resource state** — measurable current reality with explicit unknowns;
  v1.8.6 folds the CURRENT inference workload's memory model too (model
  file bytes as a FILE fact + planned KV at the serving window, from the
  existing model-card/context authorities);
* **Envelope** — what may be admitted and what should be reduced, with the
  adjustment class that says how a recommendation can honestly be applied
  (a footprint that consumes the resident budget reduces background work
  through the existing honest adjustment class, with the reason stated);
* **Admission** — heavyweight workloads and model loads are admitted
  against a resident budget (available RAM − host headroom − measured
  process RSS); unknown facts refuse conservatively; v1.8.6 the run gate
  consults this envelope BEFORE any engine start (sustained pressure
  defers the model load with the explainable reason);
* **Self-model** — verified facts with provenance (measured at, sustained
  for, what is unknown), served through `/api/governor` and rendered in the
  System Centre.

The critical-pressure protection path (cooperative cancellation of active
runs — never process killing) remains owned by the existing monitor wiring,
unchanged.

## Verified capabilities (evidence-backed)

The following are implemented and verified in this repository — each by the
evidence class named in parentheses:

* local llama.cpp inference with managed engines, the modern CLI contract
  (split `--cache-type-k`/`--cache-type-v`, `--flash-attn on|off|auto`) and
  strict numeric-argument validation before spawn (deterministic contract
  tests against binary `--help` fixtures);
* a native C++ engine path with real generation for validated
  llama-architecture models and honest fallback (unit + integration);
* pause / edit / resume with durable checkpoints, edit journaling, restart
  recovery and one authoritative run (deterministic + race + integration);
* sessions, memory, recall, skills, repository indexing, research, browser,
  sandbox, custom tools, coding lab, automations (unit/integration per
  surface);
* the session-list generation guard extended to the startup consumer:
  every list write — refresh or initialization — goes through the ONE
  monotonic generation, so stale responses can neither resurrect a
  deleted session nor drop a created one (deterministic store-level
  suite, Go store tests, and real-browser E2E with mutation-verified
  coverage; the v1.8.3 session-delete contract is pinned at all three
  layers);
* live streaming that cannot wedge: the flush scheduler arms three
  independent boundaries behind one coalescing latch and its task
  controller self-heals from a lost MessageChannel delivery
  (deterministic scheduler tests reproducing the wedge + a real-browser
  E2E that streams and settles with requestAnimationFrame suspended);
  v1.8.5 adds the server-side half: the run hub's conflation-aware
  subscriber queue keeps the NEWEST cumulative snapshot under subscriber
  backpressure and never drops it in favor of stale ones (deterministic
  hub/queue suites + race gate);
* the four-level reasoning-depth control — Low / Mid / High / Ultra —
  where every level carries a REAL numeric thinking-token budget onto
  the generation request (the llama.cpp request-level
  `reasoning_budget_tokens` parameter, verified against both managed
  engine builds' server sources: 0 = thinking off, 1024 = bounded
  default, 4096 = deep, engine default = unrestricted), gated to local
  engines, with the legacy Auto/Fast/Thinking vocabulary migrated at the
  boundary (deterministic Go + store-level suites); a separate
  Show/Hide Thinking control gates the PRESENTATION of backend-reported
  reasoning only — never generation settings (store-level payload-split
  suite);
* zero-session Send: deleting the final session leaves a usable
  zero-session state and pressing Send creates + activates a session in
  the current mode and continues the run (store-level scripted-transport
  suite + real-stack browser E2E in Chat and Agent modes, including
  visible-before-completion and reload persistence);
* a monotonic context-refresh generation: stale context responses can
  never overwrite newer context state across out-of-order responses,
  session switches, deletions, creations, mode switches and fresh runs
  (causally ordered store-level race suite);
* the AUTO GPU posture contract: explicit user OFF, explicit manual
  layer counts and the CPU requested profile are respected; a derived
  legacy CPU posture is repaired once, honestly noted; the engine-update
  verification window reports and probes the byte-verified staged binary
  through a window-scoped identity marker (deterministic updater +
  config + recommendation suites);
* live resource monitoring with hysteresis and cooperative critical
  protection (deterministic + race);
* the Runtime Governor policy loop with deterministic explanations
  (deterministic + race + API contract tests);
* the runtime self-model for capability questions, composed from the live
  registry and the parsed model card (deterministic + orchestrator-level
  E2E over a fake engine).

Windows CI runs the full Go/frontend/native gates on GitHub-hosted runners;
Linux CI runs the headless gates. **Neither is a claim of physical-PC
runtime verification** — CI is CI.

## Installation

Windows-first. Download the release asset for your machine's variant,
run `SHEYTAN-LA.exe`. First run performs setup (data root, engine
provisioning through the authoritative variant resolver — no silent CPU
fallback for explicit variant requests). `SHEYTAN-LA.bat` offers a console
session; `SHEYTAN-LA.exe serve` runs the headless server. Models are managed
in-app (downloader is the ONE download authority); GGUF files can also be
imported from disk.

Build from source: Go 1.26, Node 24, `npm ci`, `npm run build`,
`go build` (Windows desktop; `-tags headless` for CI/containers/servers).

## Configuration

Live configuration is one concurrency-safe source (`config.Source`) shared
by every consumer — engine variant, model, sampling, thinking, tool policy,
net-search, update schedule, data root. The UI's Settings surface edits the
same source; `/api/config` is the machine surface. Context planning adapts
the window per session policy; the Runtime Governor's envelope is a policy
read-model, not a configuration file.

## Truthful limitations

* The Governor is a POLICY authority: it computes envelopes, admission
  decisions and explanations. Enforcement lives in the existing subsystems
  and is wired where control points exist (v1.8.6: the run gate's model-load
  admission); it is not yet a full closed-loop actuator over every
  background subsystem.
* CPU load is measured on Linux (normalized load average) and, since
  v1.8.6, on Windows (the kernel32 GetSystemTimes delta through the one
  shared priming/delta seam — first sample primes, deltas are real,
  failures stay unknown); other platforms report unknown and CPU policy
  stays off — never a fabricated utilization number.
* GPU/NPU facts on all surfaces are DETECTION-level; execution evidence is
  the measured runtime offload line or a still-valid execution receipt
  from the committed bounded transaction. The v1.8.6 truth model enforces
  `GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU
  verified` end-to-end: `--list-devices` enumeration SELECTS (provisioning
  posture) but never VERIFIES; the launcher's AUTO offload activates only
  on proven execution; a candidate transaction may boot Vulkan to prove
  itself (real generation + measured offload line required before commit);
  the offload evidence is per-boot (a restart re-proves). The
  execution/evidence ladder (`detected → backend-available →
  device-selected → model-loaded → generation-executed →
  execution-evidence → verified`) is surfaced on `/api/engine` and agrees
  with `/api/perf`'s accelerator block by construction (one authority, one
  evidence path). Physical GPU execution is still only claimed when the
  runtime evidence exists on real hardware — no GPU claim from a device
  name, a DLL or a build success.
* The reasoning-depth budgets are REAL request-level parameters on the
  llama.cpp serving backend, applied when the model's chat template
  exposes a thinking section — a non-thinking model simply ignores the
  budget (no reasoning is fabricated); the native C++ path has no
  reasoning-budget control and the level is inert there, documented
  rather than faked.
* The engine is supervised machinery, not a manual control (v1.8.5): it
  boots on first use, restarts after engine-affecting changes and recovers
  on its own; the UI represents its state. Internal lifecycle operations
  (startup gate, engine updates, restart, recovery, shutdown) remain.
* CI (Windows and Linux) is build/test evidence, not physical-host runtime
  evidence.
* The v1.9+ roadmap (AI System builder, computer-use, evaluation framework,
  advanced model routing) is NOT implemented — see `docs/ROADMAP.md`.

## Release history

Release history lives ONLY in `docs/changelog.md`. This README describes the
current product; per-release evidence lives in the release tags, the tests
that shipped with them, and `docs/UPDATE.md`.
