# SHEYTAN-LA Roadmap

## Purpose

This roadmap states the capability-driven direction of SHEYTAN-LA after the
R&D reset. One rule governs the whole document: **future phases are never
marked complete because they are documented.** A capability is DONE only
when deterministic tests, race gates, integration or E2E evidence prove it
in this repository.

This file carries NO version numbers. Capabilities and goals carry honest
status only; the release history — which release delivered which capability,
with which evidence — lives in exactly one place: `changelog.md`.

Artifact truth (single authority per artifact):

* `changelog.md` is the ONLY canonical release-history artifact;
* `../README.md` describes the CURRENT product only (no release history);
* `UPDATE.md` carries the CURRENT release notes and maintenance behavior;
* `ARCHITECTURE.md` remains the architecture truth for what IS built.

Motto: **"The model proposes. The tools execute. The laboratory verifies."**

## Delivered capabilities (compact, status-only)

Status: **DELIVERED** — every row below is proven by current repository
behaviour, code, tests or CI; per-release evidence for each row lives in
`changelog.md` and is deliberately not duplicated here.

* The adaptive runtime: task classification, tier system, context planning;
  the staged native-engine program with execution receipts; GPU transaction
  authority; the deterministic `dataAnalysis` authority; the zero-session
  send contract; the pause/edit/resume run lifecycle; sessions, recovery and
  continuum machinery; the Coding Lab; the scheduler; custom tools; the
  downloader/updater; the Governor; the skill stores.
* The AI System Builder layer: the AI System object (durable, user-owned,
  CRUD + clone + export + import + activation, safe default system); the
  frozen execution binding (a mid-run edit can never mutate a running
  configuration); the durable Goal engine (phases, honest progress,
  checkpoints, bounded replanning, terminal goals stay terminal); the
  approval authority (deterministic risk classification, exact call
  identity, bounded decision ledger, deny-by-default gate on goal runs);
  bounded sub-agents and delegation; deterministic repository navigation
  (`repo_nav`); progressive disclosure hardening.
* The desktop migration: the Go core + Qt host architecture over the
  secured local transport; the full Qt/QML host (Wails removed); the
  Windows named-pipe transport repair chain; the CI visibility system
  (tee'd logs, failure summaries, anonymous annotations); the LF policy and
  gofmt gates; the green browser E2E suite; the stress gate; the fully
  green Linux pipeline; Linux packaging and ZIP structural proof.
* The theme system: ONE theme token source (a QML module singleton) shared
  by every authored view and by the QuickControls2 custom style — one
  palette, one spacing scale, one typography scale, one corner-radius
  scale; every authored motion gated behind the persisted
  animationsEnabled setting.

## Evidence labels used by the backlog

* **D — Documented/verified.** Supported by current repository behaviour,
  code, tests or CI. Can be acted on directly.
* **I — Architectural inference.** A reasonable implication of the current
  design, but NOT a measurement. Requires a measurement plan before
  implementation is trusted.
* **H — Hypothesis.** Requires profiling or an experiment before ANY
  implementation decision. Numbers attached to hypotheses are explicitly
  REJECTED as facts.

Only proven implemented capabilities may move from future to current; the
movement happens in this file together with the evidence pointer.

---

## The ONE-UI migration program — **ACTIVE (spike phase)**

**End state (the operator's standing goal):** ONE Go UI — built with Fyne —
replacing every other UI language, launched and controlled from the one Go
CLI, one codebase unified across screen sizes. The desktop product stops
depending on a second UI language entirely when the migration completes;
until then the Qt host stays the shipped desktop surface and is never
stubbed, masked or weakened to make the migration look further along than
the evidence says it is.

**Phases (each gates the next; nothing is complete by documentation):**

* **Spike — CURRENT.** An isolated, compiling Fyne shell exists as code:
  `ui/fyne` (the product theme tokens mirrored from the ONE Qt token
  source; a size-driven shell window; navigation; one placeholder panel;
  reflow on resize, no fixed pixel geometry). The spike is build-tag
  isolated (`fyne`) and launches through the ONE CLI — `sheytan spike`
  (the //go:build fyne hook inside the shipped cmd tree; the standalone
  cmd/fyne-spike main package is retired; the untagged binary's dispatch
  is unchanged). The shipped surface and the Qt host stay untouched by the
  spike; the hosted workflow carries ADDITIVE fyne-tag compile gates on
  both platform jobs (spike claim only). Status honestly recorded per the
  verification matrix: the spike compiles for the native OS in an
  environment with the GL/Wayland/X11 dev headers; where those headers are
  absent the compile is UNVERIFIED with the failing line recorded, and the
  cross-platform verdict is exactly the kind of claim only a hosted or
  operator run may settle next.
* **Parity — NOT STARTED.** The Fyne surface reaches the Qt host's
  capability surface row by row, each row landing with its own acceptance
  assertion; the parity manifest grows a Fyne target column per row; the
  multi-screen-size unification (one codebase from small windows to full
  desktop) is part of this phase's definition of done.
* **Qt retirement — NOT STARTED.** The Qt host is removed only after the
  parity phase is proven hosted AND the operator's physical retest passes
  on every platform; retirement is a deletion with evidence, never a
  degradation.

The migration never forks the product: ONE core, ONE transport, ONE theme
token source — the Fyne shell binds the SAME tokens and the SAME live
transport the shipped desktop uses, or it does not ship.

---

## AI System Builder + long-horizon agentic engineering — **CURRENT (partial; boundary below)**

The runtime is the first real **AI System Builder** layer with the durable
long-horizon Goal engine, bounded delegation, the approval authority, and
deterministic repository navigation — always by EXTENDING the existing
one-authority runtime, never by duplicating it. The delivered surface:
`internal/aisystem` (durable store under `<DataDir>/ai-systems/`, atomic
writes, bounded, deterministic ordering, corruption-tolerant; stable
`systemId`, monotonic `revision`; CRUD + clone + export + import +
activation with correct active-system fallback; reserved default system
byte-identical to the pre-migration runtime; HTTP `/api/systems` + item /
activate / clone / export / import; the full `internal/aisystem` test
battery); the frozen execution binding (instructions, model override,
reasoning ladder, tool surface constrained server-side at offer AND
execution, skills surface, approval policy — published as an `ai_system`
activity and travelling on the run response); the durable Goal engine
(`internal/goal`, phases understanding → planning → acting → verifying →
completed with waiting/exception states, honest progress, checkpoints,
bounded replanning, recoverable tool failures block the STEP, turn budget
parks instead of running away, boot recovery never falsely running);
`internal/approval` (the ONE deterministic risk classification from tool
identity + normalized arguments; policy vocabulary auto / ask-risky default
/ ask-all; exact `CallKey` identity with a bounded decision ledger; the
per-run `agent.WithApprovalGate` seam); the delegation engine
(`internal/multiagent/subtasks`, explicit budgets, read-only parallelism
that can only tighten, mutating work serialized, no nested spawning,
deterministic merge); `repo_nav` deterministic navigation through the SAME
path-safety authority as `repo_search`; progressive disclosure hardening;
the zero-session reload persistence repair with its deterministic E2E
regression pair.

### Current/future boundary (NOT implemented — future work, D-planned)

* Automatic sub-agent fan-out FROM the goal runner (the delegation engine is
  implemented and tested; the goal loop does not yet decompose steps into
  subtasks automatically).
* Protected-evaluation anti-hack guard: deterministic detection of
  protected reference/answer artifact access around Lab evaluation
  contexts — designed, not implemented; opt-in and context-aware when it
  lands.
* MCP runtime wiring (the `internal/mcp` client remains implemented and
  tested but not yet registered into the tool registry; the guarded
  registration pipeline exists — the constraint policy already covers
  MCP-class risk classification for when it lands).
* Oversized-output spool handles for ordinary shell/tool results (the
  artifact/task-registry machinery exists; the automatic spool trigger for
  arbitrary outputs is future work).
* Native-engine forward-pass arithmetic optimization BEYOND the
  resolved-weight-table repair (the per-row tensor-name resolution
  bottleneck and the unoptimized CI build are FIXED — bit-for-bit parity
  pinned by the native prefill parity gate; the remaining per-token cost is
  the genuine arithmetic). SIMD-friendly batched prompt processing over
  those loops is the next performance item — evidence-gated the same way,
  not started.

## Universal Agent — **FUTURE — NOT IMPLEMENTED**

Controlled computer-use layer, all through explicit permission/trust
boundaries: browser and desktop interaction, screen, mouse/keyboard,
application discovery, document workflows, multimodal execution,
connectors, voice foundation. Additionally:

* bounded parallel tool orchestration with a deterministic execution graph;
* tool-call state hashing for repeated-state/cycle detection — evidence-
  gated, no fixed retry count until experiments establish that policy;
* bounded reflection/recovery and pause/cancel propagation through the run
  lifecycle;
* artifact/handle workflows (consume the large-output handles).

Never replace safety or objective verification with a circuit breaker alone.

## AI Learning & Evaluation — **FUTURE — NOT IMPLEMENTED**

Evaluation suites, baselines, experiments, candidate variants, diagnostics,
feedback learning, skill evolution, memory optimization, routing/context
optimization, promotion/rollback, improvement history; representative
local-model benchmark/evaluation suites; the chat-template compatibility
matrix as living evidence; performance regression gates; candidate
promotion/rollback decided ONLY by measured evidence. The protected-
evaluation guard is a prerequisite for trustworthy evaluation runs.

Honesty gate: no system change becomes an "improvement" merely because the
model says so — only measured evaluation evidence promotes a candidate.

## Advanced Model Intelligence — **FUTURE — NOT IMPLEMENTED**

Multi-model routing, capability discovery, specialist models,
vision/reasoning routing, draft/speculative models, dynamic loading and
unloading, model pools, cross-model fallback, resource-aware selection;
native-engine optimization only with numerical parity tests; optional local
embedding/vector infrastructure only when RAG measurements justify it.

No specific external stack is mandatory for any of this.

## SHEYTAN Local AI Platform — **FUTURE**

Strategic end state: a complete user-owned local AI platform where users
create, run, understand, modify and continuously improve durable AI systems
while keeping ownership of their models, knowledge, memory, skills, tools,
behavior, experiments and versions; scalable local workers, mature
multi-run orchestration, cross-platform support, optional multi-process/
remote workers, mature observability/evaluation, portable import/export/
versioning (the AI System export/import is the first portable unit).

---

## Performance measurement framework (applies to ALL backlog work)

Use the EXISTING RunClock/performance surfaces where they already exist.
Track, where measurable: classification duration; context duration; prompt
preparation; serialization; request sent; first byte; TTFT; generation
duration; first browser-visible streamed update; WebSocket bytes; tool
execution duration; verification duration; persistence duration; total
turn time. Add a measurement ONLY when it supports a real optimization —
never as decoration.

Long-running stress dimensions already exercised by the suite (extend, do
not weaken): 5k+ token streaming; long reasoning; hundreds of session
messages; large tool outputs; large repositories; repeated Lab repair
loops. No numeric targets are asserted anywhere in this roadmap without a
measurement behind them.

---

## Engineering invariants (all releases)

* One authority per concern: one run lifecycle, one session store, one tool
  registry, one scheduler, one downloader, one installer/update path, one
  engine lifecycle, one memory authority, one runtime policy authority, one
  AI System store, one goal store, one approval classification.
* No sleep-based correctness; deterministic tests synchronize on
  authoritative state and events.
* Build/typecheck ≠ runtime proof; CI ≠ physical-PC runtime proof.
* Hardware detection is never reported as execution evidence; GPU/NPU
  claims stay separated (detection → backend availability → device
  enumeration → real execution).
* Numeric/unsupported engine arguments fail before process spawn; the
  modern llama.cpp CLI contract is verified against the binary `--help`
  fixtures.
* The codename-removal gate stays enabled and exact on tracked trees and
  clean-room drops.
* Performance claims require measurements made on THIS codebase; numbers
  from external reports are hypotheses (H) until reproduced here.
* Architecture proposals that replace the Go runtime, the one-authority
  session/run/tool/memory design, or the objective-verification pipeline
  are rejected by default — changes to those require evidence-gated
  migration plans, not rewrites.
