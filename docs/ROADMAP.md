# Roadmap

## Purpose

This roadmap states the capability-driven direction of SHEYTAN-LA. One rule governs the whole document: **future phases are never marked complete because they are documented.** A capability is DONE only when deterministic tests, race gates, integration or E2E evidence prove it in this repository.

This file carries no version numbers. Capabilities and goals carry honest status only. The release history — which release delivered which capability, with which evidence — lives in exactly one place: [`changelog.md`](changelog.md).

**Artifact truth (single authority per artifact)**

- `changelog.md` — the only canonical release-history artifact
- `../README.md` — describes the current product only (no release history)
- `UPDATE.md` — current maintenance and update behavior
- `ARCHITECTURE.md` — architecture truth for what is built

Motto: **“The model proposes. The tools execute. The laboratory verifies.”**

---

## Delivered capabilities

Status: **DELIVERED** — every item below is proven by current repository behaviour, code, tests or CI. Per-release evidence lives in `changelog.md` and is deliberately not duplicated here.

### Adaptive runtime
- Task classification, tier system, context planning
- Staged native-engine program with execution receipts
- GPU transaction authority with honest detection-vs-execution ladder
- Deterministic `dataAnalysis` authority
- Zero-session send contract
- Pause / edit / resume run lifecycle
- Sessions, recovery and continuum machinery
- Coding Lab, scheduler, custom tools, downloader/updater
- Runtime Governor and skill stores

### AI System Builder layer
- Durable, user-owned AI System object (CRUD + clone + export + import + activation, safe default system)
- Frozen execution binding (a mid-run edit can never mutate a running configuration)
- Durable Goal engine (phases, honest progress, checkpoints, bounded replanning, terminal goals stay terminal)
- Approval authority (deterministic risk classification, exact call identity, bounded decision ledger, deny-by-default gate on goal runs)
- Bounded sub-agents and delegation
- Deterministic repository navigation (`repo_nav`)
- Progressive disclosure hardening

### One-Go-UI (Fyne)
- Single Go + Fyne desktop application running in-process against the core over secured local IPC
- Full retirement of the previous Qt/QML host and any React/Vite web frontend
- Responsive three-zone shell (wide / medium / narrow)
- Session-list generation guard, live streaming that cannot wedge, zero-session Send, monotonic context refresh
- Optional 3D Studio (read-only live topology map)
- CI visibility system, stress gate, UCRT64 Windows pipeline, Xvfb Linux desktop smoke

---

## Evidence labels used by the backlog

- **D — Documented/verified.** Supported by current repository behaviour, code, tests or CI. Can be acted on directly.
- **I — Architectural inference.** A reasonable implication of the current design, but not a measurement. Requires a measurement plan before implementation is trusted.
- **H — Hypothesis.** Requires profiling or an experiment before any implementation decision. Numbers attached to hypotheses are explicitly rejected as facts.

Only proven implemented capabilities may move from future to current; the movement happens in this file together with the evidence pointer.

---

## AI System Builder + long-horizon agentic engineering — CURRENT (partial)

The runtime is a real AI System Builder layer with the durable long-horizon Goal engine, bounded delegation, the approval authority, and deterministic repository navigation — always by extending the existing one-authority runtime, never by duplicating it.

### Current / future boundary (not yet implemented)

- Automatic sub-agent fan-out from the goal runner (the delegation engine is implemented and tested; the goal loop does not yet decompose steps into subtasks automatically)
- Protected-evaluation anti-hack guard (designed, not implemented; opt-in and context-aware when it lands)
- MCP runtime wiring (client implemented and tested but not yet registered into the tool registry)
- Oversized-output spool handles for ordinary shell/tool results
- Native-engine forward-pass arithmetic optimization beyond the resolved-weight-table repair (SIMD-friendly batched prompt processing — evidence-gated, not started)

---

## Universal Agent — FUTURE — NOT IMPLEMENTED

Controlled computer-use layer, all through explicit permission/trust boundaries:

- Browser and desktop interaction, screen, mouse/keyboard, application discovery
- Document workflows, multimodal execution, connectors, voice foundation
- Bounded parallel tool orchestration with a deterministic execution graph
- Tool-call state hashing for repeated-state / cycle detection
- Bounded reflection/recovery and pause/cancel propagation through the run lifecycle
- Artifact/handle workflows

Never replace safety or objective verification with a circuit breaker alone.

---

## AI Learning & Evaluation — FUTURE — NOT IMPLEMENTED

- Evaluation suites, baselines, experiments, candidate variants, diagnostics
- Feedback learning, skill evolution, memory optimization, routing/context optimization
- Promotion/rollback decided only by measured evidence
- Representative local-model benchmark/evaluation suites
- Chat-template compatibility matrix as living evidence
- Performance regression gates

Honesty gate: no system change becomes an “improvement” merely because the model says so — only measured evaluation evidence promotes a candidate. The protected-evaluation guard is a prerequisite.

---

## Advanced Model Intelligence — FUTURE — NOT IMPLEMENTED

- Multi-model routing, capability discovery, specialist models
- Vision/reasoning routing, draft/speculative models
- Dynamic loading and unloading, model pools, cross-model fallback
- Resource-aware selection
- Native-engine optimization only with numerical parity tests
- Optional local embedding/vector infrastructure only when RAG measurements justify it

No specific external stack is mandatory for any of this.

---

## SHEYTAN Local AI Platform — FUTURE

Strategic end state: a complete user-owned local AI platform where users create, run, understand, modify and continuously improve durable AI systems while keeping ownership of their models, knowledge, memory, skills, tools, behavior, experiments and versions.

- Scalable local workers
- Mature multi-run orchestration
- Cross-platform support
- Optional multi-process / remote workers
- Mature observability and evaluation
- Portable import/export/versioning (the AI System export/import is the first portable unit)

---

## Performance measurement framework

Use the existing RunClock / performance surfaces where they already exist. Track, where measurable:

- Classification duration, context duration, prompt preparation, serialization
- Request sent, first byte, TTFT, generation duration
- First visible streamed update, tool execution duration, verification duration, persistence duration, total turn time

Add a measurement only when it supports a real optimization — never as decoration.

Long-running stress dimensions already exercised by the suite (extend, do not weaken): 5k+ token streaming, long reasoning, hundreds of session messages, large tool outputs, large repositories, repeated Lab repair loops. No numeric targets are asserted anywhere in this roadmap without a measurement behind them.

---

## Engineering invariants (all releases)

- One authority per concern: one run lifecycle, one session store, one tool registry, one scheduler, one downloader, one installer/update path, one engine lifecycle, one memory authority, one runtime policy authority, one AI System store, one goal store, one approval classification.
- No sleep-based correctness; deterministic tests synchronize on authoritative state and events.
- Build/typecheck ≠ runtime proof; CI ≠ physical-PC runtime proof.
- Hardware detection is never reported as execution evidence; GPU/NPU claims stay separated (detection → backend availability → device enumeration → real execution).
- Numeric/unsupported engine arguments fail before process spawn; the modern llama.cpp CLI contract is verified against the binary `--help` fixtures.
- The codename-removal gate stays enabled and exact on tracked trees and clean-room drops.
- Performance claims require measurements made on this codebase; numbers from external reports are hypotheses until reproduced here.
- Architecture proposals that replace the Go runtime, the one-authority session/run/tool/memory design, or the objective-verification pipeline are rejected by default — changes to those require evidence-gated migration plans, not rewrites.
