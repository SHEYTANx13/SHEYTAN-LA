# SHEYTAN-Local-Agent — Agent Context (CURRENT v1.10.9 handoff)

This is the concise, current handoff for an engineering agent continuing
work on SHEYTAN-LA. It states what IS (verified), what is NOT (future), the
durable invariants, and the surfaces most sensitive to regression. Full
truth: `ARCHITECTURE.md` (architecture), `ROADMAP.md` (current/future
boundary), `changelog.md` (the ONLY release history), `worklog.md`
(session log).

**Current release: v1.10.9 — the smoke path canonicalization + the
evidence-named Windows transport repair (run 38871731277 consumed: the
Windows go test suite ran hosted for the first time since v1.10.1 with
exactly one failure — `TestServeMalformedRequestIsRejectedNotCrashing`,
fixed at the accept-path root cause: the vanished-peer completion family
(`ERROR_BROKEN_PIPE`/`ERROR_PIPE_NOT_CONNECTED` beyond `ERROR_NO_DATA`) no
longer kills `Accept`/`http.Serve`/the endpoint; the latent stale
deadline-timer leak in `pipeConn.Close` is closed; the Linux smoke's
relative-path defect is fixed by canonicalizing `CORE_BIN`/`UI_BIN` to
absolute paths BEFORE any cd — the core boots past the work-directory cd
for the first time in any hosted smoke run; the Windows failure summary
now publishes Go assertion statements (`\.go:[0-9]+: ` class) anonymously).
HOSTED STATUS (do not overstate): E2E, stress, and both gofmt gates have
been green hosted since v1.10.8; the smoke gates G1–G5, the Windows
post-verification chain, the Linux acceptance/packaging chain, and the
publish job remain HOST-UNPROVEN until the first run of this revision —
fix forward only on what its annotations name.**
Its parents, v1.10.8 — the line-ending policy + E2E path repair + CI
Visibility 3.0 (the Windows gofmt gate was RIGHT — the committed tree is
gofmt-clean; the CRLF checkout class is fixed by a scoped `.gitattributes`
(`*.go text eol=lf`) plus a Windows pre-Checkout `core.autocrlf=false` pin,
the gate byte-identical; `build:e2e` builds `./cmd/sheytan-local-agent`
after the reshape emptied the root package — the full E2E stack passes
40/40 Playwright specs locally; the Browser E2E gate joins the visibility
system with tee'd ci-logs + an if:failure() summary emitting capped
::error:: annotations; the smoke summary distinguishes never-started from
died-at-gate; verify-all.mjs's Qt acceptance builds the core explicitly
from its real entrypoint and the missed-reference sweep is proven) — and
v1.10.7 — the evidence-driven gate repair + CI
Visibility 2.0 + repository shape (the Windows named-pipe client now
satisfies the overlapped-IO contract; gofmt gated on BOTH platforms; all
documentation in docs/, the core entry in cmd/sheytan-local-agent/, the
launcher source in packaging/) — and v1.10.5 — the live-stream +
Qt-host-build release — established the current behavior below. The production
desktop UI is the native Qt Quick/QML host (`ui/qt`), an IPC CLIENT of
the unchanged Go core over `internal/localipc` (per-user Unix socket /
named pipe, transport-level identity checks, versioned keyed handshake
`sheytan-ipc/1`, bounded framing). Wails is REMOVED — from the product,
the build (`build/config.yml`), the module graph (go.mod/go.sum; CI
asserts the absence) and the current-product docs; historical release
records that name Wails stay (deleting them would falsify history). The
shipped Qt surfaces are sessions/chat/live-streaming-with-Stop/
settlement-persistence-reload/repeat-send, the core states, and — new
in v1.10.2 — the AI Systems list/activation drawer (a pure
`/api/systems` client: list, activate, typed error paths; the store
keeps ALL authority). The remaining UI surfaces (AI Systems
editing/import, Goals, Coding Lab, Net Search page, repository index,
tasks/automations, downloads/diagnostics) are still served by the
documented headless/browser mode until their Qt migration lands — they
are NOT claimed done. Run teardown is deterministic: run spawns are
tracked and `Server.Close` joins them (bounded) before releasing the
stack; new run/resume registrations are refused once shutdown began.
The launcher's UI-resolution contract is unit-tested
(`internal/desktop/desktop_test.go`: `none` sentinel, override,
next-to-core layout, hard refusal). The Linux desktop smoke owns its
whole environment (Xvfb + Openbox/equivalent WM when present, self-built
close tool, self-built core when unstaged, trap cleanup) and its G1–G5
PASS under a real WM. The Windows full-suite compile gate is repaired:
the localipc contract tests build on Windows too (the test-only
`credAllowed` helper is platform-neutral now). The ONE canonical local
verification entrypoint is scripts/verify-all.mjs; the CI jobs run
their gates INLINE in build-desktop.yml and do NOT invoke verify-all.mjs
(v1.10.3 correction — the v1.10.2 wording claimed CI calls it, and the
workflow never did). The Phase 1 engine
gate is UNCHANGED: the physical-Windows warm-TTFT/native-vs-managed
baseline is NOT MEASURED (no physical hardware), so the native C++
engine default stays evidence-gated and the managed llama.cpp path
remains the default backend. The Windows TEST-ONLY CDP acceptance
switch is retired with the Wails shell; the live-answer gate is now the
Qt acceptance suite (real QML UI + real core) on both platforms. The
former loopback activity transport is retired; /api/ws-origin now
answers same-origin unconditionally for the headless/browser mode.
Measurement is FAIL-CLOSED about hosts: `cmd/shtn-perf` requires an
explicit `--host-class physical|hosted` (no default), refuses a
physical claim over detected hosted-CI markers, derives the
schema-compat `hostedCI` flag from that class, and records
RAM/hostname/evidence/notes (artifacts:
`baselines/perf-hosted-ci-v1.10.0.json`,
`baselines/perf-hosted-v1.10.2.json`). The direct devDependency `ws` is
at 8.22.0 (GHSA-58qx-3vcg-4xpx / GHSA-96hv-2xvq-fx4p closed, no
`--force`; upstream fixed them in 8.20.1 and 8.21.0 respectively —
8.22.0 was chosen, not the smallest). Version identity is exactly
`1.10.8` everywhere
(canonical gate: `node scripts/release-version.mjs --check`).

R&D decision
(`Parsaetak/Contents` → `Research-and-Development` →
`SHEYTAN-LA-2026-10-11.md`) remains the direction authority: Go stays
the single orchestration authority; C++ owns numerical inference; the
native-engine DEFAULT promotion is NOT flipped in v1.10.2 — the
blocking evidence is the physical-Windows warm-TTFT comparison against
managed llama.cpp (checklist in ROADMAP.md); the Phase 0 baselines live
in `baselines/` (hosted class, NOT-MEASURED honesty per metric,
fail-closed host classification) + `worklog.md`; the Qt/IPC seam is
DEFINED in ARCHITECTURE.md (implemented at the chat/sessions surface
and the AI Systems list/activation surface, not yet at every panel).
Version identity is
exactly `1.10.8` everywhere (canonical gate:
`node scripts/release-version.mjs --check`). No stale 1.10.0–1.10.7
current-release claims exist outside `changelog.md` and the retained
historical note sections.

**WHAT v1.10.0 CHANGED (the v1.10.0 regression-sensitive surfaces; all
still current):**

1. **The variant transaction order** (`internal/llm/llama.go`,
   `updateEngineVariantTx`): early live-run refusal (no claim) →
   validate/supported/no-op → tag resolve → CLAIM-FREE staged download
   (`updater.DownloadEngineArchiveStaged` — non-destructive, no install
   lease; the swap half keeps the full lease/lock discipline through
   `InstallStagedFromArchiveDeferredWithVariant`) → DESTRUCTIVE
   BOUNDARY (admission claim + live verdict, atomic) → fresh
   `wasRunning` → stop → swap → start → verify → commit/rollback. The
   claim MUST NOT move back before the boundary: doing so re-opens the
   v1.9.5 regression (background probes holding run admission for
   minutes). A download failure must NOT restart the engine (nothing
   was stopped).

2. **Desktop loopback activity transport** (carried from v1.9.5,
   unchanged; `internal/api/loopback.go`, `src/config.ts`,
   `src/store.ts`, `internal/api/ws.go`): the desktop process binds the
   EXISTING API handler on 127.0.0.1:0; `GET /api/ws-origin` returns the
   origin ("" = same-origin); `connectActivity` resolves it once via
   `ensureActivityWsOrigin()` and opens the socket through
   `openActivitySocket`. `allowedOrigin` accepts ONLY the exact Wails
   page origin (http/wails + `wails.localhost`) against LOOPBACK request
   hosts. Never broaden the origin check, never add a second streaming
   channel, never serve the socket from a second handler. The
   regression pins live in `internal/api/loopback_v195_test.go`
   (including a REAL WebSocket upgrade with the Wails origin receiving
   `attached`).

3. **Engine/run coordination (admission claim at the destructive
   boundary)** (`internal/llm/llama.go` `SetRunGate`/`SetAdmissionControl`/
   `ErrEngineBusyWithRun`, wired in `internal/api` `New`):
   `updateEngineVariantTx` refuses on an active run TWICE — an early
   read-only registry check before anything happens (no claim), and the
   atomic check-and-claim at the DESTRUCTIVE boundary (a run registered
   during the claim-free download window is seen by the live verdict).
   The AUTO probe treats either refusal as a DEFERRAL
   (`internal/llm/variant_auto.go`) — never a persisted bounded failure.
   Do not move the claim before the boundary (that re-opens the v1.9.5
   run-admission outage), do not skip the boundary claim (that re-opens
   the check-then-act tear), and do not persist deferrals into gpu-probe
   state.

4. **Settled-reply proof selector** (`e2e/zero-session.spec.ts`
   `expectRealAssistantReply`): settled assistant replies render
   `.message-markdown` (MessageBubble → `<Markdown>`); `.message-content`
   exists ONLY on the live bubbles. Any future settled-reply assertion
   must target the markdown surface; never accept placeholder rows.

5. **Stream trace** (`src/stream-trace.ts`): opt-in via
   `localStorage SHEYTAN_STREAM_TRACE=1`, lengths only, bounded ring,
   `window.__SHEYTAN_STREAM_TRACE_DUMP__` exposed ONLY when enabled.
   The zero-session spec attaches it on failure. Keep the disabled path
   a strict no-op and keep content OUT of the trace.

6. **Run summary metrics** (`internal/api/server.go` settle):
   `gateMs`/`ttftMs`/`persistMs`/`acceptToDoneMs`/`totalMs` +
   `events`/`deltas`/`firstDeltaMs`. `publishMs` is retired — do not
   reintroduce it.

7. **The TEST-ONLY WebView2 CDP seam** (`internal/desktop/desktop.go`,
   `acceptanceCDPPort` + the `WindowsOptions.AdditionalBrowserArgs`
   forward): the acceptance harness's CDP switch
   (`SHEYTAN_ACCEPTANCE_CDP_PORT`) MUST travel this programmatic
   channel — never `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`, which the
   Wails pure-Go loader pins to "" at package init. Keep the switch
   digits-only, bounded, inert when unset, and keep production launchers
   from ever setting it. Pins: none in Go (env-gated); the harness
   (`scripts/windows-live-answer-acceptance.mjs`) documents the
   mechanism and its sanitized CDP-timeout diagnostic.

8. **The UpdateEngineNow admission seam** (`internal/llm/llama.go`,
   `UpdateEngineNow`): early advisory refusal → boundary claim (same
   primitives as `updateEngineVariantTx`) → stop/install/start under the
   claim → balanced release on every exit. Pins:
   `internal/llm/admission_updatenow_v1100_test.go` — keep the
   begin/end balance and the "refusal never stops a serving engine"
   guarantees when touching this transaction.

1. **Governor CPU policy contract** (`internal/governor/governor.go`,
   pinned by tests in BOTH directions): the envelope's CPU-reduction
   branch requires a SUSTAINED rolling signal — ≥ `cpuWarmupSamples` (3)
   folded samples AND rolling average ≥ `CPUReduceAbove`. A lone spike
   (including the FIRST sample ever folded, whose EWMA equals the raw
   value) never flips the envelope; sustained high load still reduces
   background/concurrency. The folded sample count rides
   `ResourceState.CPUSamples`. `Governor.SetCPUSampler` is a TEST seam —
   production keeps `governor.CPULoadPlatform` (ONE platform sampler);
   integration tests that assert a pressure contract MUST pin the seam
   (unknown or low CPU) or they will fold the host's real load again
   (the exact run 38035650428 failure mode).
2. **Native prefill parity gate** (`tests/test_prefill_parity.cpp`): the
   claims now match the proof exactly — per-position logits through the
   chunk=1 span cadence, final-token logits for chunks 3/7/16, full KV
   bytes across every chunking AND the serial run, edge/error bounds,
   mixed prefill→decode; tied output NOT claimed (no shipped fixture is
   tied). Any change to the layer math MUST keep that gate bit-exact or
   be a deliberate, documented numerics change.
3. **Zero-session / live-stream E2E contracts**: EVERY zero-session test
   now proves the settled assistant reply is real content (non-empty
   `.message-content`, no `[data-stream-placeholder]` descendant) via
   `expectRealAssistantReply`; the count-based row assertions alone are
   NOT sufficient (the generation bubble also carries
   `.message-row.from-agent`). Keep that helper on any new transcript
   test.
4. **UI typography floors** (`src/styles.css`): no `font-size` below
   10px (metadata floor), errors/essential states ≥ 11px, interactive
   control text ≥ 12px. Do not reintroduce sub-10px text; density comes
   from hierarchy and layout, not microscopic type.

**WHAT v1.9.2 CHANGED (carried forward):**

1. **Native engine forward path** (`native/engine/src/forward.h/.cpp`,
   `generate.cpp`, `tensor.h/.cpp`): the forward pass runs on a RESOLVED
   WEIGHT TABLE (once-per-binding `Weights::resolve`; rows walked by
   `row_ptr`/`dequant_row`) and prefill runs through `Forward::prefill_span`
   (16-token chunks, last-token-only logits, cancel cadence 16). Measured
   19.08 → 3.66 ms/token (5.2×). `Forward::token()` public semantics are
   UNCHANGED. Bit-for-bit parity is pinned by `tests/test_prefill_parity.cpp`
   (per-position logits via the chunk=1 cadence, last-token logits for
   the production chunk sizes, FULL KV bytes across every chunking) — any
   change to the layer math MUST keep that gate bit-exact or be a
   deliberate, documented numerics change. The native CMake build now
   DEFAULTS to Release when no build type is given.
2. **Desktop shell** (`internal/desktop/desktop.go`): the main window binds
   `events.Common.WindowClosing` → `app.Quit()` (wails v3.0.0-beta.16
   swallows webview-window closes on both platforms — verified in the
   dependency sources). Do not remove this binding: closing the window
   would strand a zombie process with the backend running.
3. **Desktop smoke gates** (`.github/workflows/build-desktop.yml`):
   Windows requires `MainWindowHandle != 0` + a true `CloseMainWindow()`
   return + exit code 0; Linux owns a real Xvfb display, finds the app's
   window via xdotool (`--pid` + `getwindowpid` ownership), closes it with
   `WM_DELETE_WINDOW`, and requires normal-path exit. `xdotool` is a Linux
   CI dependency. These gates are the authoritative GUI-runtime evidence —
   keep them honest (Kill remains a failing-run backstop only).
4. **Zero-session recovery** (`src/store.ts` `recoverRunFromIdle`): an
   idle sentinel WITHOUT a lastRun block that lands within 250 ms of the
   attach acknowledgement (`activityAttachAckAt`) is the attach handshake,
   never recovery evidence — the measured clobber mechanism. The v1.2.6
   Path A (idle + lastRun) and late-idle semantics are unchanged.
5. **E2E contracts**: the live-stream + zero-session visibility proofs
   observe REAL content only (`data-stream-placeholder` excluded); the
   zero-session observation bound is 60 s; live-stream failures attach
   compact structured diagnostics.

**THE CURRENT PROGRAM (v1.9.x):** complete the v1.9 boundary honestly —
see `ROADMAP.md` §"v1.9 current/future boundary": automatic sub-agent
fan-out from the goal runner, the protected-evaluation anti-hack guard,
MCP runtime wiring through the guarded registration pipeline, and
oversized-output spool triggers. Linux is a first-class release target:
the Linux CI path must reach Go verification → Browser E2E → stress →
build → package; nothing may be skipped because Windows passes.

---

## What SHEYTAN is

A local-first AI engineering laboratory — NOT an Ollama/LM Studio wrapper.
One Go runtime owns every authority exactly once; a React/TypeScript
frontend is embedded; managed llama.cpp and a native C++ engine path serve
inference behind one backend contract. Motto: **"The model proposes. The
tools execute. The laboratory verifies."**

## v1.9 capabilities (implemented, evidence in changelog §v1.9.0)

* **AI System object (`internal/aisystem`, `/api/systems`, System Centre
  UI).** ONE user-owned, revisioned run-configuration store: instructions,
  model override, reasoning preference, allowed tool surface, approval
  policy, skills surface, knowledge/memory/compaction policy,
  verification policy. Atomic per-document persistence, deterministic
  ordering, corruption tolerance, a reserved default system that
  reproduces the pre-v1.9 behavior exactly, non-destructive migration,
  clone/export/import. The ACTIVE system's snapshot (systemId +
  systemRevision) is frozen into every run BEFORE the run goroutine
  starts — mid-run edits can never mutate a running configuration.
* **Execution binding (v1.9).** The frozen snapshot applies server-side:
  instructions (one bounded system-prefix block), model override, effort
  preference on the EXISTING v1.8.5 ladder (low=0, mid=1024, high=4096,
  ultra=engine default; explicit request level wins), tool surface
  constrain (`ToolPolicy.AISystemConstrain` — offer AND execution, remove-
  only), skills filtering (excluded skills never inflate context),
  approval policy. Identity is observable: an `ai_system` activity event
  and the run response fields.
* **Goal engine (`internal/goal`, `/api/goals`, Goals UI card).** Durable
  long-horizon state: phases understanding → planning → acting →
  verifying → completed; waiting/exception states; bounded plan (≤12);
  per-step status/result/evidence; a goal-level evidence journal;
  checkpoints after planning, each completed step, approval boundaries
  and terminal settlement; resume WITHOUT replaying committed mutations;
  bounded replanning (≤2) that never erases failure evidence; turn
  budget parking; boot recovery that marks found-live goals paused
  (never falsely running) and is idempotent; terminal settlement from
  the orchestrator's OBJECTIVE verification report only — a model claim
  never completes a goal.
* **Approval authority (`internal/approval`, the `agent.WithApprovalGate`
  seam).** ONE deterministic risk classification (read-only /
  workspace-write / external-network / destructive / privileged);
  `auto` / `ask-risky` (default) / `ask-all`; the EXACT normalized call
  identity (`CallKey`) is the only thing an approval binds to; a bounded
  decision ledger; goal runs install a deny-by-default gate (no risky
  call executes silently); goal-level approvals persist durably and
  approve/resume the exact call; rejection is evidence; stale approvals
  are rejected. Chat/agent runs install NO gate — behavior unchanged.
* **Bounded delegation (`internal/multiagent/subtasks`).** Executable
  subtasks beside the advisory specialists: read-only parallelism ≤ 2
  (the concurrency seam can only tighten), mutating work serialized, no
  nested spawning, fan-out ≤ 8, deadlines are honest blocks,
  deterministic merge in `subtaskId` order, failures stay failed.
* **Repository navigation (`repo_nav`).** `open` / `navigate` / `read` /
  `grep` over identified sources through the SAME index and path-safety
  authority as `repo_search`; exact ranges, provenance, truncation
  state; entire files are never returned.
* **P0 repaired (the v1.8.8 Linux E2E blocker).** The zero-session
  reload-persistence defect: `run()` now sets its startup state BEFORE
  any transport work, the lazy `createSession({keepRunState:true})` can
  no longer clobber it, failure paths clean up deterministically, and
  the E2E waits for the deterministic run-dispatch marker plus durable
  post-reload transcript. Regression pair: `zero-session-send.test.ts`
  #6/#7.
* **P0 repaired (v1.9.1, the live-stream observation contract).** The
  CI live-stream growth assertion baselined on the live bubble's
  PRESENTATION PLACEHOLDER and spent its poll budget inside the engine
  gate + prefill phase (~40 ms/prompt-token measured; a 446-token
  briefing ≈ 8 s TTFT unloaded, >30 s under contention). Placeholders
  now carry `data-stream-placeholder` in the DOM; the live-stream
  proofs require real streamed content for the baseline and observe
  strictly-longer growth inside the genuine live window; the fixture
  budget is 320 tokens (40 real engine chunks — the deterministic
  snapshot count). No product streaming path changed; the chain was
  verified healthy end to end (WS frame + DOM timeline diagnostic).
* **Measured fact to carry (native engine path).** Native prefill is
  ~25–40 ms/prompt-token on a mid host (no batching yet); decode is
  130+ tok/s on the live fixture. First-token latency on the native
  path is dominated by prompt length until prefill batching lands
  (ROADMAP performance item).

## Verified foundations (v1.8 — unchanged authorities)

* **One run lifecycle** — one registry, one hub, one `runLive` per run;
  sequence-stamped activity replay; settlement ordering (outcome recorded
  before the terminal flip) through the ONE `finishSuccessfulRun` path.
* **Pause / edit / resume** — durable atomic checkpoints; transactional
  edits with journals + startup recovery; resume = semantic continuation;
  abort publishes `aborted`, never a fake `done`.
* **Streaming contract (do not regress)** — server bounded conflation-
  aware queue → newest cumulative snapshot; sequence/replay; reconnect;
  frontend fast path; triple-boundary self-healing flush; synchronous
  terminal flush (done/error/abort).
* **v1.8.5 reasoning ladder** — low/mid/high/ultra carry REAL numeric
  thinking budgets on the local llama.cpp request path; remote providers
  never receive the field; the native path documents the level as inert
  rather than faking it.
* **Execution/evidence invariants** — `GPU detected ≠ GPU available ≠ GPU
  selected ≠ GPU executed ≠ GPU verified`; identity-bound
  `ExecutionReceipt`; staged engine identity; one accelerator authority;
  one Governor (policy only — it never executes).
* **dataAnalysis (v1.8.7/v1.8.8)** — deterministic analyze/aggregate/
  join/quality/export; 256 MB input honesty; compact model-oriented
  output. Agent/Goal work should prefer it for data tasks.
* **Sessions/continuum/recovery** — one session store (hot cache, atomic
  writes, self-healing index); continuum chapter rollover; recovery
  handoffs for context exhaustion.

## The v1.9 runtime seams (where new code must hook)

* Run options (`agent.RunOption`): `WithAISystem`, `WithApprovalGate`,
  `WithToolPolicy`, `WithThinkingMode`, `WithRunIdentity`, … — new
  per-run behavior rides options, never globals.
* Tool policy: `ToolPolicy.allows()` is the ONE offer/execution gate; the
  AI System constrain binds FIRST (nothing can re-enable an excluded
  tool).
* Approval: classify through `approval.ClassifyRisk` only; never
  re-implement risk vocabulary.
* Goal drives: the drive executor is `goal.RunFunc` bound to the ONE
  orchestrator (`api.goalRunFunc`); no second model loop.
* Storage: new durable objects follow the house pattern (one JSON per
  object, unique-temp+rename, bounded, deterministic listing,
  corruption-tolerant).

## Invariants (all releases)

* One authority per concern — including the ONE AI System store, ONE goal
  store, ONE approval classification.
* No sleeps/timing hacks/retry storms; deterministic tests synchronize on
  authoritative state.
* Build/typecheck ≠ runtime proof; device detection ≠ device execution;
  model output ≠ objective evidence.
* No fake autonomy: everything stays bounded by permissions, approvals,
  workspace policy, the Governor, cancellation, pause and verification.
* No fake long-context: use the actual model/engine/effective context.
* No model-specific hard-coding; unsupported backend capabilities remain
  unsupported with honest fallback (never prompt-side emulation).
* The codename-removal gate stays enabled; release identity flows from
  package.json through `scripts/release-version.mjs` only.

## Code formatting convention

* Go sources are gofmt-canonical (tab-indented; the v1.10.7 tree-wide
  reformat repaired the 1d3b1f2-era tab→space mangling). CI GATES
  `gofmt -l .` inside the Go verification steps on BOTH platforms (a
  violation fails the job) — v1.10.8 additionally pins the LF
  line-ending policy (`.gitattributes` `*.go text eol=lf` + the Windows
  `core.autocrlf=false` checkout pin) so the Windows gate observes the
  true LF tree. Run `gofmt -w .` before committing any Go edit; match
  the file's existing gofmt style (v1.10.3 record, register R12;
  superseded gofmt-gate wording from the pre-v1.10.7 era is retired).

## Linux / Windows evidence boundaries

* v1.10.9 RUN TRUTH (do not overstate in any future edit): run
  https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38871731277
  (v1.10.8, commit a1041fe) — the breakthrough run: the Windows gofmt gate
  passed for the first time (the LF pin worked), the Windows go test suite
  ran hosted for the FIRST time since v1.10.1 with exactly ONE failure —
  Windows step 14 `--- FAIL: TestServeMalformedRequestIsRejectedNotCrashing
  (0.06s)` (package internal/localipc, artifact
  windows-go-verification-logs-attempt1) — the Linux Browser E2E passed,
  and the stress suite passed on its first hosted run at the new shape.
  Linux step 24: the Qt smoke's core NEVER STARTED — `SMOKE FAIL: core
  died at boot` with the decisive bash ENOENT line naming the relative
  path executed after the cd (artifact linux-verification-logs-attempt1).
  v1.10.9's fixes consume that evidence: the smoke path canonicalization
  (L-A), the accept-path vanished-peer classification (W-A/C1, INFERENCE —
  the exact assertion-line paste was unavailable and the annotations API
  was rate-limited from the working IP), the Close timer stop (W-B), and
  the assertion-line annotation upgrade (V-A.2). Local proof is
  compile-level for the Windows transport (Linux-only environment) and
  run-level for the smoke path (the core booted past the cd locally,
  honestly bounded at G2 — no Qt toolchain/openbox/xdotool here). The
  smoke gates G1–G5, Windows steps 16–32, Linux steps 26–33, and the
  publish job have NEVER executed hosted — v1.10.9 does not claim
  otherwise; the next run is their first exercise, and fix-forward reads
  ONLY its annotations and summaries.
* v1.10.8 RUN TRUTH (do not overstate in any future edit): run
  https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38823624040
  (v1.10.7, commit 22049b9) — the Visibility 2.0 annotations WORKED and
  both failures are root-caused with primary evidence: Windows step 13
  "Go verification" failed at the gofmt gate, and the gate was RIGHT
  (the committed tree is gofmt-clean byte-for-byte — the eight annotated
  cmd/ files are the first eight alphabetically, a listing artifact of
  the CRLF checkout: runner default core.autocrlf=true, no
  .gitattributes, gofmt normalizes to LF); Linux step 17 "Browser E2E"
  failed because build:e2e still built the emptied ROOT package
  (reproduced locally: exit 1, "no Go files"). The Windows go test
  suite has STILL never run hosted at the repaired transport (W1–W4 +
  W-C) — no hosted run since v1.10.1 has executed it, and v1.10.8 does
  not claim otherwise. v1.10.8's fixes are environment repairs and
  path repairs, proven by local reproduction: .gitattributes + the
  Windows checkout pin (mechanism demonstrated: CRLF working-tree file
  → gofmt names it; LF tree → clean), the build:e2e target
  (before: exit 1; after: exit 0 and 40/40 Playwright specs on the
  real stack), E2E tee + failure summary, the smoke never-started
  discrimination, and the verify-all.mjs repair with sweep proof. No
  hosted green is claimed for v1.10.8: its own hosted result is
  recorded here only after the operator's push executes the run.
* v1.10.7 RUN TRUTH (parent record; do not overstate in any future edit): run
  https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38731387959
  (v1.10.6, commit 48cee27) — audit SUCCEEDED (3m7s), the Linux Go
  verification gate GREEN for the first time, the stress suite and the
  Qt host build green; Windows x64 FAILED step 13 "Go verification"
  (output line 138, the SIXTH consecutive run) and Linux x64 FAILED
  step 23 "Qt desktop acceptance smoke" (output line 55, identical in
  v1.10.5 and v1.10.6); the publish job never ran. Both failing jobs'
  check-run annotations read anonymously carry bare exit codes only
  ("Process completed with exit code 1.") — the failing test names and
  gate identity live in the two auth-walled failure artifacts, NOT in
  any anonymously readable channel, so v1.10.7's Windows fixes are
  read-verified source defects (the client pipe handle opened without
  FILE_FLAG_OVERLAPPED while pipeConn IO is overlapped — present since
  v1.10.1; plus the never-enforced future read deadline) and its Linux
  fixes are first-hand reproductions on the same Qt 6.8.2 minor (the
  smoke passes ALL of G1–G5 locally once the xcb plugin dependency set
  is complete). v1.10.7 installs CI Visibility 2.0 on top of the
  v1.10.6 failure-visibility: the failure-summary steps emit ::error::
  annotations (failing test names on Windows, the failing gate +
  ui.log/core.log tails on Linux, failing gates on audit) that are
  anonymously machine-readable via the check-run annotations API — the
  next red run diagnoses itself for a credential-less agent. No hosted
  green is claimed for v1.10.7: its own hosted result is recorded here
  only after the operator's push executes the run.
* v1.10.6 RUN TRUTH (parent record): Windows x64 failed step 13 "Go
  verification" in five consecutive hosted runs through
  https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38714950052
  (v1.10.5, commit 8d8e660); Linux x64 failed step 23 "Qt desktop
  acceptance smoke" on the smoke's first hosted execution in the same
  run. v1.10.6 fixes the verified defect classes in code (the named-pipe
  transport W1–W4, the smoke environment L1–L3) and installs CI
  failure-visibility: every Go-verification command tees to its own log
  and on failure appends failing test names + log tails to
  $GITHUB_STEP_SUMMARY (public without login) plus uploaded artifacts —
  that summary output, not the admin-gated log, is the authoritative
  read for a credential-less agent. No hosted green is claimed for
  v1.10.6; local GOOS=windows build/vet is compile proof only.
* Linux CI path: source/frontend audit → Go verification (headless) →
  native engine build/tests → Browser E2E (real headless server + real
  engine + fixture GGUF) → stress → executable build → version smoke →
  desktop RUNTIME SMOKE (v1.9.2: a REAL Xvfb display the step owns, the
  app's real pid, the real top-level window located via xdotool with a
  getwindowpid ownership check, a real WM_DELETE_WINDOW close, and a
  normal-path exit — status 0) → ZIP verification. Nothing is skipped
  because the other platform is green.
* Windows x64 keeps its green release path (installer, execution
  evidence, discovery and data tests) plus the v1.9.2 RUNTIME SMOKE
  step: the real desktop executable is launched, a real top-level
  window is proven (`MainWindowHandle != 0`), `CloseMainWindow()`'s
  BOOL is checked (a delivered close), and exit code 0 is required
  through the normal path — with the exact unmet evidence level
  reported when no window exists. Runtime smoke is a DISTINCT gate
  from build/package — packaging success never implies GUI runtime
  success.
* The Browser E2E fixtures run the REAL stack; no mock-only substitute
  for backend behavior is accepted. The zero-session reload flow and the
  v1.9 AI System / Goal UI flows are covered by
  `e2e/zero-session.spec.ts` and `e2e/ai-systems.spec.ts`.
