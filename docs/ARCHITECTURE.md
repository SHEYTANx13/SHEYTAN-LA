# Architecture

This document states the architecture that **is** implemented and verified in this repository. Future work is owned exclusively by `ROADMAP.md`.

Motto: **“The model proposes. The tools execute. The laboratory verifies.”**

---

## 1. One process, one runtime stack

One Go process owns the runtime (`internal/runtime.Stack`). Every authority is constructed exactly once:

| Authority | Package | Responsibility |
|-----------|---------|----------------|
| Engine lifecycle | `internal/llm` | Managed llama.cpp + native C++ backend under one `llm.Backend` contract |
| Run lifecycle | `internal/api` | Single registry, hub and authoritative state per session; pause/resume keeps one runId |
| Session store | `internal/sessions` | Durable sessions + hot cache bounded by memory manager |
| Tool registry | `internal/tools` | Generation-aware schema cache; custom tools sync into the same registry |
| Scheduler | `internal/scheduler` | Automations and timed tasks |
| Downloader / updater | `internal/updater` | Single download authority + transactional engine install |
| Memory | `internal/memmanager` | Trimmers, run-boundary tracking, cooperative cleanup |
| Policy | `internal/governor` | Resource state, admission envelope, self-model (policy only) |
| AI Systems | `internal/aisystem` | Durable user-owned systems with frozen execution binding |
| Goals | `internal/goal` | Long-horizon UNDERSTAND → PLAN → ACT → VERIFY loop |
| Approvals | `internal/approval` | Deterministic risk classification + one-use decisions |
| Presentation blocks | `internal/presentation` | Versioned block envelope + bounded store; the ONE contract the native shell and the Web Workspace both render from |

---

## 2. Engine & backend

- **Reference backend**: managed llama.cpp server with modern CLI contract (`--cache-type-k/v`, `--flash-attn on|off|auto`) and strict numeric validation that fails *before* process spawn.
- **Native C++ engine** (`native/engine`): supervised alternative path with real generation for validated llama-architecture models and honest incapability fallback. Forward pass uses a resolved weight table built once per model binding; prefill is chunked and cancellation-aware; numerical parity with the serial path is pinned by tests.
- **Transactional updates**: identity manifest + committed state; two-boot idempotency; rollback restores both binary and recorded tag.
- **Typed resolution causes** (v1.11.0): every accelerator resolution carries a machine-readable `reasonCode` (requested-cpu / npu-benchmark-verified / gpu-execution-verified / gpu-selected-pending-verify / backend-unsupported / no-matching-device / no-accelerator-evidence-cpu) next to the human-readable reason — a code never upgrades the evidence state.
- **Execution/evidence ladder** (one shared structure on `/api/engine`):
  ```
  detected → backend-available → device-selected → model-loaded
  → generation-executed → execution-evidence → verified
  ```
  Device enumeration never equals verified execution. GPU claims obey the strict separation: detected ≠ available ≠ selected ≠ executed ≠ verified.

---

## 3. Desktop (one-Go-UI)

The product desktop is a **single Go executable**:

- Core (policy/orchestration authority) + Fyne UI (`ui/fyne`) running **in-process** over `internal/localipc`.
- Transport: Unix domain socket (SO_PEERCRED) on Linux/macOS; named pipe (current-user DACL + post-connect SID check) on Windows. The ONE canonical client dial is `internal/localipc.DialContext` — a context-aware, platform-aware function shared by the Fyne HTTP and WebSocket transports (v1.11.1 maintenance: the former private Unix-only dial in the UI broke every tab on Windows; source-contract tests now fail the build on any regression, and integration tests drive the production CoreClient against the real `localipc.Serve` endpoint on both platforms).
- Handshake: strict `sheytan-ipc/1` + ephemeral key header on every request; a wrong key or mismatched protocol is a typed refusal, never a fallback.
- IPC observability (v1.11.1 maintenance): the desktop bootstrap writes STRUCTURED handshake records to app.log — success names the protocol, core version and transport (`windows-named-pipe` / `unix-domain-socket`); failure and first-refresh errors are logged with the transport identity. The endpoint path (account-derived on Windows) and the key are never logged. The shell status surface shows Connecting / Connected / Disconnected with the actionable error and a single-flight Retry action.
- No TCP listener, no embedded web frontend, no Qt runtime, no React/Vite.
- Launcher (`internal/desktop`) owns the full lifecycle: bind IPC → attach Fyne window → clean teardown on close/signal.
- **One shutdown coordinator** (v1.11.1 maintenance pass, run 43008361835): the user's window close, SIGINT/SIGTERM and programmatic closes all funnel through one idempotent running→closing→closed coordinator; teardown is bounded (deadline-bounded activity-pump drain, workspace listener release) and a deadline-exceeded teardown is recorded INCOMPLETE — never graceful. The core/IPC servers close in reverse order after the UI closes.
- **Startup phase telemetry** (`internal/startup`): one monotonic recorder measures every bootstrap phase (config, migrations, logging, core, IPC, Fyne app, shell, first view, first-interactive, handshake, sessions, background readiness, shutdown) with sanitized stable fields only; the summary lands on stderr even when no data root is ever created. Controlled diagnostic mode `SHEYTAN_STARTUP_DIAGNOSTICS=1` adds JSON records to stdout (CI smoke capture).
- **Responsive size-class policy** (v1.11.1 maintenance pass): one `sizeClassFor` function resolves compact <600 / medium 600–899 / desktop 900–1279 / wide ≥1280 logical canvas units; navigation derives from it (top strip + More menu / compact rail / persistent wide rail). The full logical-size matrix and five structural goldens are pinned by tests; a real-window resize smoke walks the matrix under Xvfb in CI.
- Behavioral contracts (zero-session send, session-list generation guard, live streaming that cannot wedge, reasoning ladder, settings IA) are pinned by Go tests under `ui/fyne`.
- Linux desktop smoke runs the real binary under Xvfb in CI (launch/soak/SIGTERM smoke plus the resize-matrix smoke).
- **Button-fit + one-column compact layout** (v1.11.1 maintenance): every action label is short with its explanatory detail in an adjacent wrapped caption; card captions wrap inside the cards (`cardFit` — `widget.NewCard` subtitles never wrap and clipped at compact widths); the chat/agent views use an adaptive dual pane that stacks the session/goal list above the conversation below the 600-unit breakpoint (the documented compact one-column contract). `TestButtonFitMatrix` walks every destination at every representative logical size asserting no compressed buttons, no out-of-bounds rigid controls and no unreachable actions.
- **Windows gate semantics** (v1.11.1 maintenance): a hosted Windows pass requires the named-pipe CoreClient integration test AND a launch smoke that observes the window AND the successful-handshake record AND a real core read AND logging persistence AND a graceful close; force-termination is a failure, never a pass.

### 3.2 Log catcher (observability)

`internal/logging` owns every sink under `<data-root>/logs/` (`app.log`, `tools.jsonl`, `llm.jsonl`, `crashes/`):

- Boot order: bootstrap logger (stderr + bounded memory) → runtime-data migrations → canonical sinks open → session banner → one startup record naming the resolved log directory and active sinks → bootstrap replay.
- Sink health is observable (`Manager.Health`): a failed or nil app sink never silently discards events — lines are preserved in a bounded emergency ring, surfaced on rate-limited/deduplicated stderr, and reported through `/api/logs` (`dir` + `sinkHealth`) so the Logs panel and Settings diagnostics show the resolved directory, fallback state and sink problems honestly.
- Partial sink opens roll back (every opened handle closed, errors joined); rotation and JSONL write failures are reported; `Close` writes its own outcome record and returns joined errors from every sink.
- If the configured logs directory cannot be opened, a documented per-user private fallback (`os.UserCacheDir()/SHEYTAN-LA/logs`) is used after the primary failure is recorded — the fallback is announced in the startup record, never presented as the canonical sink. The bootstrap stderr + memory logger remains the last resort.

### 3.1 Hybrid presentation: on-demand Web Workspace

The native shell remains the primary surface for navigation, forms, approvals, run controls and diagnostics. Content kinds that benefit from browser-grade rendering (markdown, code, diffs, tables, chart specifications) can additionally be opened in the **Web Workspace** (`internal/workspace`) — an on-demand, browser-hosted read-only viewer:

- **One contract**: the runtime issues versioned `presentation.Block` envelopes (kinds: text, status, markdown, code, diff, table, chart-spec, artifact, 3d) into ONE bounded Go-owned store. One block ID maps to one logical artifact; invalid payloads are refused with typed errors and the native rendering is unaffected.
- **Deterministic selection**: `presentation.SelectSurface` is a pure function of the kind — never model-directed, never re-decided per token. The Fyne shell ALWAYS renders first; the Workspace is additive.
- **Safe degradation**: unknown kinds are accepted flagged with a typed degrade record and render as raw data — never a blank or crashed session.
- **Security boundary** (same family as the 3D Studio): 127.0.0.1 ephemeral bind, per-launch 256-bit fragment token (never logged, never in page HTML), strict CSP, allow-listed embedded offline assets, GET-only read endpoints, all DOM text via `textContent` — model output is data, never trusted HTML.
- **No second authority**: the Workspace is a viewer over the presentation store, like the Studio is a viewer over the topology. It holds no session store, no tool registry, no permission system, and (in this slice) exposes zero command endpoints.
- The desktop shell deliberately does not embed a web runtime; an embedded webview was not verifiable on both desktop targets in the shipping timebox. The browser the user already trusts hosts the page — a documented decision, not a hidden limitation.

---

---

## 4. Session & run lifecycle

```
POST /api/run → register (one registry entry + hub + runLive)
→ phases: preparing → thinking → generating → finalising
→ terminal: done | error | aborted → registry release
```

- Publisher stamps every activity with a monotonic per-run sequence and folds it into `runLive` before broadcast.
- Engine variant transactions refuse an active run at two points: early read-only check (claim-free) and the destructive boundary (atomic claim + live-run verdict).

---

## 5. Runtime Governor

Fed by the single live-pressure monitor (one sampler, one cadence).

- **Resource state** — measurable reality with explicit unknowns, including current inference workload footprint.
- **Envelope** — what may be admitted / what should be reduced.
- **Admission** — model loads and heavyweight work are gated against a resident budget; unknown facts refuse conservatively.
- **Self-model** — verified facts with provenance, exposed on `/api/governor`.

Critical pressure → cooperative cancellation of active runs (never process killing).

---

## 6. Data, tools & skills

- **dataAnalysis** — single pure-Go authority: load (CSV/TSV/JSON + type inference), analyze, aggregate, join, quality, export. Byte-for-byte deterministic; 256 MB input bound; no external engine or SQL surface.
- **Skills** — JSON store + Markdown SKILL.md packages with progressive disclosure and token-budgeted injection.
- **Tools** — one registry; custom tools, Coding Lab, sandbox, browser, Net Search, repository navigation (`repo_nav`) all share the same path-safety and approval authority.

---

## 7. AI System Builder & Goal engine

- **AI System** — durable, user-owned object (CRUD, clone, export/import, activation). Execution binding is frozen at run start; mid-run edits cannot mutate a running configuration.
- **Goal engine** — checkpointed UNDERSTAND → PLAN → ACT → VERIFY loop with turn budget, bounded replans, terminal goals that stay terminal, and boot recovery that never falsely marks a goal running.
- **Approvals** — deterministic risk classification from tool identity + normalized arguments; one-use decisions bound to exact call identity.

---

## 8. Optional 3D Studio

Read-only live topology viewer (host, engine, Governor, sessions, tools). Loopback-only server, per-launch 256-bit fragment token, strict CSP, embedded Three.js, zero commands. Full contract: `docs/3dstudio.md`.

---

## 9. Engineering invariants

- One authority per concern.
- No sleep-based correctness; tests synchronize on authoritative state.
- Build success ≠ runtime proof; CI ≠ physical-host proof.
- Hardware detection is never reported as execution evidence.
- Numeric/unsupported engine arguments fail before process spawn.
- Codename-removal gate stays enabled.
- Performance claims require measurements made on this codebase.

---

*Living architecture truth table. Describes only what is implemented and verified. Direction → `ROADMAP.md`. Release history → `changelog.md`.*
