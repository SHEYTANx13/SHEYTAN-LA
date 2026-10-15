# SHEYTAN-LA Roadmap

## Purpose

This roadmap states the capability-driven direction of SHEYTAN-LA after the
2026-09 R&D reset. One rule governs the whole document: **future phases are
never marked complete because they are documented.** A capability is DONE
only when deterministic tests, race gates, integration or E2E evidence prove
it in this repository.

Artifact truth (single authority per artifact):

* `changelog.md` is the ONLY canonical release-history artifact;
* `README.md` describes the CURRENT product only (no release history);
* `UPDATE.md` carries the CURRENT release notes and maintenance behavior;
* `ARCHITECTURE.md` remains the architecture truth for what IS built.

Motto: **"The model proposes. The tools execute. The laboratory verifies."**

## Shipped baseline (compact)

**v1.10.5 is the shipped baseline; release evidence is in changelog.md.**
(v1.10.5 is the live-stream + Qt-host-build release: it closes the Linux
Qt host build's missing OpenGL development dependency — the run
38584769607 step 22 blocker — and fixes the activity-stream
fragmentation defect that froze the desktop live bubble on real
generations; v1.10.4 — the stress-gate retarget — is its parent
release; its evidence stays in changelog.md.)

v1.8 delivered, with per-release evidence in `changelog.md`: the adaptive
runtime (task classification, tier system, context planning), the staged
native-engine program with execution receipts, GPU transaction authority,
the deterministic `dataAnalysis` authority, the zero-session send contract,
the pause/edit/resume run lifecycle, sessions/recovery/continuum machinery,
the Coding Lab, the scheduler, custom tools, the downloader/updater, the
Governor, and the skill stores. This roadmap deliberately carries NO detailed
v1.8.x release history — that history lives in exactly one place.

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

## v1.9.6 — the FINAL 1.9.x maintenance release (2026-10-11) — the R&D direction adopted, NO premature scope movement

v1.9.6 advances NO v1.9 scope: the automatic goal-to-subtask fan-out,
the protected-evaluation anti-hack guard and the MCP runtime-registry
wiring remain FUTURE WORK. v1.9.6 repairs the two authoritative
v1.9.5 CI failures at their roots — the Windows admission-hostage 503
regression (the engine-mutation claim now lives at the DESTRUCTIVE
boundary of the variant transaction; network/download phases are
claim-free; pinned by `internal/llm/admission_boundary_v196_test.go`)
and the Linux native-window smoke (the documented bare-Xvfb
software-renderer configuration, owned by the smoke step; the gate is
unchanged) — and adopts the R&D architecture decision
(`Parsaetak/Contents` → `Research-and-Development` →
`SHEYTAN-LA-2026-10-11.md`) as the direction for the next major cycle:
Go stays the single orchestration authority, C++ owns numerical
inference (promotion gated on capability/parity evidence — the default
is NOT flipped in v1.9.6), Qt (C++/QML) is the primary UI target for
v1.10+ over a typed coarse-grained IPC boundary. The first vertical
slice ships as code: `internal/perf` + `cmd/shtn-perf`, the R&D Phase 0
measurement baseline framework with honest NOT-MEASURED semantics.
Evidence: `changelog.md` v1.9.6, `worklog.md` (v1.9.6 session),
`internal/{api,llm}/admission_v195_test.go`,
`internal/llm/admission_boundary_v196_test.go`,
`internal/updater/install.go` (`DownloadEngineArchiveStaged`),
`internal/perf/`, `cmd/shtn-perf/`.

## v1.10.0 — the R&D migration plan (phases, gates, dependencies, risks) — v1.10.0 DELIVERED the first slice

Authoritative source: `SHEYTAN-LA-2026-10-11.md` (§8 migration roadmap,
§9 risks, §10 measurement framework). This section is the executable
plan; NO phase below advances until its entry gate is met in this
repository.

**v1.10.0 delivery status (read literally):** the standing
UpdateEngineNow admission-seam item is CLOSED (the same-tag/scheduled
transaction now takes the same destructive-boundary claim; pins:
`internal/llm/admission_updatenow_v1100_test.go`). Phase 0 recorded its
first committed artifact (`baselines/perf-hosted-ci-v1.10.0.json`,
hosted-CI labeled). Phase 1 collected evidence but did NOT flip the
default (below). The Qt/IPC seam is DEFINED in ARCHITECTURE.md —
documentation only, nothing ships.

**Phase 0 — measurement baseline (IN PROGRESS).**
Instrument: `internal/perf` + `cmd/shtn-perf` (shipped). Done: the
hosted-CI posture captured (cold start, idle/peak memory measured;
generation-path metrics honestly NOT MEASURED with per-metric reasons).
v1.10.2: host classification is FAIL-CLOSED — `--host-class
physical|hosted` is mandatory (no default), a physical claim over
detected hosted-CI markers is refused, `hostedCI` is derived from the
declared class, and artifacts now also record RAM, hostname and the
evidence list (see `baselines/perf-hosted-v1.10.2.json`).
Remaining: capture the physical Windows baseline on real consumer
hardware (cold start to interactive, warm TTFT, streaming latency,
idle/peak memory, tool round-trip, file output overhead) with the
host/model/quantization metadata the envelope requires; hosted-CI
numbers stay labeled hosted-CI. GATE: a committed physical baseline
artifact; every subsequent change must beat or explain its delta
against it. DEPENDENCY: a real Windows host + a serving engine binary +
a real model. RISK: VM-vs-physical divergence — mitigated by the
envelope's hosted-CI label and limitation fields.

**Phase 1 — promote the native C++ engine to the preferred backend
where evidence proves it.** The promotion MECHANISM exists today
(`NativeBackendEnabled` selects native-first with managed llama.cpp as
the automatic fallback — see `internal/api/selection.go`); v1.10.0 does
NOT flip the default. Evidence collected in v1.10.0: native C++ CTest
13/13 on the hosted Linux CI-class host including bit-for-bit
prefill/KV parity; the browser E2E stack exercises the native engine
end-to-end with the canonical fixture model. REMAINING GATE (all items
required before the default flip, in this order):

1. warm TTFT at or better than managed llama.cpp, measured on physical
   Windows target hardware through the Phase 0 harness (the SAME host,
   model, quantization and context for both backends, nearest-rank
   P50/P95 recorded in the envelope);
2. full supported-model/request/streaming/cancellation/sampling
   coverage green through the runtime contract on that host;
3. real generation with first-token visibility, cancellation and error
   recovery proven on the packaged desktop path;
4. the committed physical Phase 0 baseline artifact for reference.

DEPENDENCY: Phase 0 numbers; the engine lease/manifest authority stays
the ONE installer authority. RISK: numerical drift — mitigated by the
mandatory parity gates; scope creep — mitigated by the phase gate (no
new features until targets).

**Phase 2 — Qt (C++/QML) shell over the unchanged Go core.**
v1.10.1 STATUS: IMPLEMENTED at the chat/sessions surface — the Wails
shell is removed and the shipped desktop pair is the Go core + the
native Qt Quick/QML host (`ui/qt`) over `internal/localipc` (per-user
Unix socket / named pipe, transport-level identity checks, versioned
keyed handshake, bounded framing). Linux proof on the session host:
real visible window + keyed handshake + real core interaction + clean
WM_DELETE_WINDOW close (scripts/qt-desktop-smoke.sh G1-G5) and the
QtTest live-answer acceptance (real QML + real core). Windows CI is
wired to the same gates (Qt 6.8.2 MSVC build + windeployqt + Qt
acceptance + windowed runtime smoke) and remains NOT VERIFIED until a
green hosted run exists.
v1.10.3 STATUS: Qt IPC HARDENED. (a) The CoreClient HTTP path completes
under real framing — Content-Length completion (no hang when the server
holds the socket open), close-delimited kept, oversized heads/bodies
refused deterministically — and the handshake requires sheytan-ipc/1
(name AND version) before connected. Ten QtTest framing/contract slots
pin these byte-level rules; on the v1.10.2 client 9 of 10 FAIL. (b) The
sessions wire contract is parsed as the core serves it (bare array,
msgCount) and pinned by Go field-contract tests. (c) The smoke aborts
correctly (fail-before-define fixed), refuses WM-less G3, and the Linux
job now installs libx11-dev + openbox and passes the REAL staged paths
(the v1.10.2 wiring survived only via silent fallbacks); the Windows
acceptance staging resolves the MSVC Release/ layout and throws when
absent. Windows CI remains NOT VERIFIED until a green hosted run exists
(the v1.10.2 leftovers that would have failed the first run — R1, R3,
R4, R16 — are repaired; nothing here flips that status without a run).
v1.10.5 STATUS: the run 38584769607 step-22 blocker (`Build Qt host
(Linux)`) was reproduced locally with the workflow's EXACT command
against the pinned Qt 6.8.2 aqtinstall layout — `Could NOT find OpenGL`
(WrapOpenGL/Qt6Gui NOT FOUND) — and root-caused to the workflow's
missing GL development packages; the workflow now installs the glvnd
dev stack + mesa-common-dev + libxkbcommon-dev, and the exact command
configure+builds `sheytan-ui` and `sheytan-ui-acceptance` (exit 0) with
the full Qt live-answer acceptance suite green against the real core
(14/14), which ALSO exposed and fixed a REAL product defect: the Qt
client's refusal of RFC6455 fragmented activity frames (the hub's
1024-byte write buffer fragments every large event) froze `liveText()`
on every real long-context run — reassembly is now implemented with
the violation refusals kept loud. The Windows `Go verification`
failure remains UNIDENTIFIED (the step log is admin-gated; the exact
CGO_ENABLED=0 commands pass locally — 64+65 packages + vet — and
GOOS=windows type-checks every package including test files; no
speculative change, no test weakening). Windows CI remains NOT
VERIFIED until a green hosted run of v1.10.5 exists; the run's own
step log is the arbiter.
Remaining surfaces NOT yet migrated to Qt (honest): AI Systems
editing/import, Goals,
Coding Lab, Net Search page, repository index, tasks/automations,
downloads/diagnostics panels — those remain on the documented
headless/browser mode. The Go API remains the ONLY authority — the
shell grows no second run registry, session store, tool registry,
downloader, scheduler or policy authority. DEPENDENCY: Phase 1 (the
inference path the shell renders must be settled first); Qt
deployment/packaging evidence (Qt 6.8.2 pinned; QML-aware deployment
via the official CMake deploy script / windeployqt). RISK: Qt
learning/packaging cost — mitigated by the thin shell; IPC latency — to
be measured with the Phase 0 harness (shared memory / zero-copy only
where profiling demonstrates a payload-size gain).

**Phase 3 — measured refinements + optional WinUI 3.** Only if
Windows-specific measurements justify a second shell; C# never becomes
the orchestrator. GATE: the Phase 0 harness shows a concrete,
reproduced win the Qt path cannot reach.

**Phase 4 — multi-OS polish.** Linux and macOS stay first-class under
the Qt shell; the same performance discipline applies everywhere.
GATE: per-OS baselines recorded with the same envelope.

## v1.9.5 — hardening release (2026-10-11) — NO scope movement

v1.9.5 advances NO v1.9 scope: the automatic goal-to-subtask fan-out,
the protected-evaluation anti-hack guard and the MCP runtime-registry
wiring remain FUTURE WORK. v1.9.5 repairs the failed v1.9.4 release
attempt (the CI lint blocker and the acceptance harness's false-proof
paths), closes the v1.9.4 loopback transport's least-privilege and
authentication gaps (single-route listener + per-server capability
token + strict origin/host validation + visible desktop degradation),
makes the engine/run admission coordination atomic (the EngineAdmission
seam), and enforces the identifier-free ordinary-log contract tree-wide.
Every change ships with a focused regression test; the Windows desktop
UI acceptance gate is wired into CI as a required step and its first
hosted execution is explicitly UNVERIFIED from the authoring
environment. Evidence: `changelog.md` v1.9.5, `worklog.md` (v1.9.5
session), `internal/api/loopback_v195_test.go`,
`internal/{api,llm}/admission_v195_test.go`, `src/loopback-origin.test.ts`.

## v1.9.4 — reliability release (2026-10-10) — NO scope movement

v1.9.4 advances NO v1.9 scope: the automatic goal-to-subtask fan-out, the
protected-evaluation anti-hack guard and the MCP runtime-registry wiring
remain FUTURE exactly as bounded below (docs alone are never evidence).
The release repairs reliability only: the desktop activity-transport
root cause (the packaged builds' activity WebSocket was unreachable —
now loopback-served by the same authority), the settled-reply E2E proof
selector, an engine-transaction/run coordination gate, honest run
metrics, an opt-in sanitized stream trace and the real-Windows
acceptance harness. Evidence: `changelog.md` v1.9.4,
`worklog.md` (v1.9.4 session).

---

## v1.9 — AI System Builder + long-horizon agentic engineering — **CURRENT (partial; boundary below)**

v1.9 turns the runtime into the first real **AI System Builder** layer and
adds the durable long-horizon Goal engine, bounded delegation, the approval
authority, and deterministic repository navigation — always by EXTENDING the
existing one-authority runtime, never by duplicating it.

### Implemented in v1.9.0 (evidence-backed — D)

* **AI System object (first-class, user-owned).** `internal/aisystem` —
  durable store under `<DataDir>/ai-systems/` (atomic writes, bounded,
  deterministic ordering, corruption-tolerant), stable `systemId`, name,
  monotonic `revision`, instructions, model override, reasoning preference,
  allowed tool surface, approval policy, skills surface, knowledge/memory/
  compaction policy, verification policy, timestamps; CRUD + clone + export
  + import + activation with correct active-system fallback; reserved
  default system whose behavior is byte-identical to the pre-v1.9 runtime
  (safe non-destructive migration; fresh installs get exactly one valid
  default). HTTP: `/api/systems` (+ item/activate/clone/export/import).
  Tests: `internal/aisystem/aisystem_test.go` (fresh-install default,
  reload persistence, revision increments, freeze isolation, clone/export/
  import round trip, reservation, validation bounds, corruption tolerance,
  deterministic ordering, dangling-pointer repair).
* **Execution binding.** The ACTIVE system's snapshot is frozen into every
  run (`systemId` + `systemRevision`) before the run goroutine starts —
  a mid-run edit can never mutate a running configuration. The binding
  applies: instructions (system-prefix block), model override, reasoning
  preference (the EXISTING v1.8.5 ladder: low=0, mid=1024, high=4096,
  ultra=engine default; explicit request level wins), tool surface
  (`ToolPolicy.AISystemConstrain` — server-side, offer AND execution, never
  a prompt-side wish), skills surface (irrelevant/excluded skills never
  inflate context), approval policy. The frozen identity is published as an
  `ai_system` activity and travels on the run response. Tests:
  agent/api suites; frontend view-model tests.
* **Durable Goal engine.** `internal/goal` — goal state (goalId, original
  goal, system binding, phase, plan, current step, per-step status/result/
  evidence, evidence journal, changed files, verification, effort, turn
  budget, timestamps, terminal state) persisted per goal with atomic
  writes; phases `understanding → planning → acting → verifying →
  completed` with waiting/exception states (`waiting_for_approval`,
  `waiting_for_resource`, `paused`, `blocked`, `failed`); progress derives
  from actual plan state (no decorative percentages); checkpoints after
  planning, each completed step, approval boundaries and terminal
  settlement; bounded replanning (default 2) with the failure evidence kept
  at goal level (a plan revision can never erase the failure record);
  recoverable tool failures block the STEP, never destroy the goal; turn
  budget parks instead of running away; boot recovery marks any goal found
  live with no run behind it as paused (never falsely running); terminal
  goals stay terminal. The drive executor binds to the ONE orchestrator
  with the frozen AI System snapshot. HTTP: `/api/goals` (+ start/pause/
  resume/cancel/approve/reject). UI: the Goals card in the System Centre.
  Tests: `internal/goal/goal_test.go` (full journey, honest fallback plan,
  pause/resume without replaying committed mutations, cancel/reload,
  recovery idempotence, approval parking + stale rejection, bounded replan,
  turn budget, reload preservation of plan/approval/evidence, ordering).
* **Approval authority.** `internal/approval` — the ONE deterministic risk
  classification (`read-only`, `workspace-write`, `external-network`,
  `destructive`, `privileged/host-level`) from tool identity + normalized
  arguments; policy vocabulary (`auto`, `ask-risky` default, `ask-all`);
  exact normalized call identity (`CallKey`) with a bounded decision ledger
  (an approval is valid ONLY for the exact call); the per-run
  `agent.WithApprovalGate` seam (chat/agent runs install none — behavior
  unchanged; goal runs install a deny-by-default gate so no risky call
  executes silently). Goal-level approvals persist durably
  (`waiting_for_approval` + pending exact call survives reload; approve
  resumes the exact call; rejection is evidence; stale approvals are
  rejected explicitly). Tests: `internal/approval/approval_test.go`,
  goal approval tests.
* **Bounded sub-agents / delegation engine.** `internal/multiagent/subtasks`
  — executable delegation ALONGSIDE the existing advisory specialists:
  explicit budgets per subtask (tool surface, read/write capability,
  context, deadline), read-only parallelism ≤ 2 (the concurrency seam can
  only tighten, never widen), mutating work strictly serialized, no nested
  spawning (the executor is a leaf), bounded fan-out (≤ 8), deadlines are
  honest blocks, results merge deterministically in `subtaskId` order,
  failures stay failed with unresolved issues, parent receives concise
  structured evidence (never raw transcripts). Tests:
  `internal/multiagent/subtasks_test.go`.
* **Repository navigation.** `repo_nav` (`internal/repoindex/navigation.go`)
  — deterministic `open` / `navigate` (line/symbol context) / `read`
  (exact range) / `grep` (exact pattern, honest total match count) over
  identified sources; every result carries path identity, exact range,
  provenance and truncation state; entire files are never returned (line +
  byte bounds); all paths through the SAME path-safety authority as
  `repo_search` (traversal/absolute/UNC/volume forms refused identically
  on Linux and Windows). Registered in the runtime next to `repo_search`.
  Tests: `internal/repoindex/navigation_test.go`.
* **Progressive disclosure hardening.** The composer's skill activation
  filters through the AI System's enabled-skills surface; the tool surface
  constrain applies at offer AND execution; irrelevant skills never inflate
  the context (the v1.7 progressive markdown-skill disclosure is unchanged).
* **P0 — the zero-session reload persistence defect (Linux E2E blocker).**
  Root cause: `run()`'s lazy zero-session `createSession()` executed while
  the composer still read idle — an enabled-composer observation could
  legally sample INSIDE the startup window and reload before
  `POST /api/run` was dispatched (session existed server-side pending;
  transcript permanently empty). v1.9.0 sets the run-startup state BEFORE
  any transport work, makes the lazy create preserve it
  (`keepRunState`), cleans the state on every failure path, and
  snapshots attachments before transport. The E2E now waits for the
  deterministic "run left the client" marker (the optimistic user bubble)
  plus settlement before reloading, and asserts the durable transcript
  (user AND assistant rows) after reload. Deterministic regression pair:
  `src/zero-session-send.test.ts` #6/#7.

### v1.9 current/future boundary (NOT implemented — future work, D-planned)

* Automatic sub-agent fan-out FROM the goal runner (the delegation engine is
  implemented and tested; the goal loop does not yet decompose steps into
  subtasks automatically).
* Protected-evaluation anti-hack guard (spec §12): deterministic detection
  of protected reference/answer artifact access around Lab evaluation
  contexts — designed, not implemented; opt-in and context-aware when it
  lands.
* MCP runtime wiring (the `internal/mcp` client remains implemented and
  tested but not yet registered into the tool registry; the guarded
  registration pipeline exists — the v1.9 constraint policy above already
  covers MCP-class risk classification for when it lands).
* Oversized-output spool handles for ordinary shell/tool results (the
  artifact/task-registry machinery exists since v1.7; the automatic spool
  trigger for arbitrary outputs is future work).
* Native-engine forward-pass arithmetic optimization BEYOND v1.9.2's
  resolved-weight-table repair (the per-row tensor-name resolution
  bottleneck and the unoptimized CI build are FIXED — measured 19.08 →
  3.66 ms/token, bit-for-bit parity pinned by the native prefill_parity
  gate; the remaining per-token cost is the genuine arithmetic: the
  double-accumulator dot products and the fp16-widened attention loops).
  SIMD-friendly batched matvec/GEMM-style prompt processing over those
  loops is the next performance item — evidence-gated the same way
  (standalone engine benchmark + run telemetry TTFT), not started.

## v1.10 — Universal Agent — **FUTURE — NOT IMPLEMENTED**

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

## v1.11 — AI Learning & Evaluation — **FUTURE — NOT IMPLEMENTED**

Evaluation suites, baselines, experiments, candidate variants, diagnostics,
feedback learning, skill evolution, memory optimization, routing/context
optimization, promotion/rollback, improvement history; representative
local-model benchmark/evaluation suites; the chat-template compatibility
matrix as living evidence; performance regression gates; candidate
promotion/rollback decided ONLY by measured evidence. The v1.9 protected-
evaluation guard is a prerequisite for trustworthy evaluation runs.

Honesty gate: no system change becomes an "improvement" merely because the
model says so — only measured evaluation evidence promotes a candidate.

## v1.12 — Advanced Model Intelligence — **FUTURE — NOT IMPLEMENTED**

Multi-model routing, capability discovery, specialist models,
vision/reasoning routing, draft/speculative models, dynamic loading and
unloading, model pools, cross-model fallback, resource-aware selection;
native-engine optimization only with numerical parity tests; optional local
embedding/vector infrastructure only when RAG measurements justify it.

No specific external stack is mandatory for any of this.

## v2.0 — SHEYTAN Local AI Platform — **FUTURE**

Strategic end state: a complete user-owned local AI platform where users
create, run, understand, modify and continuously improve durable AI systems
while keeping ownership of their models, knowledge, memory, skills, tools,
behavior, experiments and versions; scalable local workers, mature
multi-run orchestration, cross-platform support, optional multi-process/
remote workers, mature observability/evaluation, portable import/export/
versioning (the v1.9 AI System export/import is the first portable unit).

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
  modern llama.cpp CLI contract (b10642/b11205) is verified against the
  binary `--help` fixtures.
* The codename-removal gate stays enabled and exact on tracked trees and
  clean-room drops.
* Performance claims require measurements made on THIS codebase; numbers
  from external reports are hypotheses (H) until reproduced here.
* Architecture proposals that replace the Go runtime, the one-authority
  session/run/tool/memory design, or the objective-verification pipeline
  are rejected by default — changes to those require evidence-gated
  migration plans, not rewrites.
