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

---

## 2. Engine & backend

- **Reference backend**: managed llama.cpp server with modern CLI contract (`--cache-type-k/v`, `--flash-attn on|off|auto`) and strict numeric validation that fails *before* process spawn.
- **Native C++ engine** (`native/engine`): supervised alternative path with real generation for validated llama-architecture models and honest incapability fallback. Forward pass uses a resolved weight table built once per model binding; prefill is chunked and cancellation-aware; numerical parity with the serial path is pinned by tests.
- **Transactional updates**: identity manifest + committed state; two-boot idempotency; rollback restores both binary and recorded tag.
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
- Transport: Unix domain socket (SO_PEERCRED) on Linux/macOS; named pipe (current-user DACL + post-connect SID check) on Windows.
- Handshake: strict `sheytan-ipc/1` + ephemeral key header on every request.
- No TCP listener, no embedded web frontend, no Qt runtime, no React/Vite.
- Launcher (`internal/desktop`) owns the full lifecycle: bind IPC → attach Fyne window → clean teardown on close/signal.
- Behavioral contracts (zero-session send, session-list generation guard, live streaming that cannot wedge, reasoning ladder, settings IA) are pinned by Go tests under `ui/fyne`.
- Linux desktop smoke runs the real binary under Xvfb in CI.

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
