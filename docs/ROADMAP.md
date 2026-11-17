# Roadmap

## Purpose

This roadmap states the capability-driven direction of SHEYTAN-LA. One rule governs the whole document: **future phases are never marked complete because they are documented.** A capability is DONE only when deterministic tests, race gates, integration or E2E evidence prove it in this repository.

This file carries no version numbers. Capabilities and goals carry honest status only. The release history lives in exactly one place: [`changelog.md`](changelog.md).

**Artifact truth**

- `changelog.md` — only canonical release-history artifact
- `../README.md` — current product only
- `UPDATE.md` — current maintenance behavior
- `ARCHITECTURE.md` — architecture truth for what is built
- `Dev-Notes.md` — the current developer work-order handler (human-owned)

Motto: **“The model proposes. The tools execute. The laboratory verifies.”**

---

## Delivered capabilities

Status: **DELIVERED** — proven by current repository behaviour, code, tests or CI.

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
- Durable, user-owned AI System object (CRUD + clone + export + import + activation)
- Frozen execution binding
- Durable Goal engine (phases, checkpoints, bounded replanning, terminal goals stay terminal)
- Approval authority (deterministic risk classification, one-use decisions)
- Bounded sub-agents and delegation
- Deterministic repository navigation (`repo_nav`)
- Progressive disclosure hardening

### One-Go-UI (Fyne)
- Single Go + Fyne desktop running in-process over secured local IPC
- Full retirement of Qt/QML and React/Vite
- Responsive three-zone shell
- Session-list generation guard, non-wedging live streaming, zero-session Send
- Optional 3D Studio
- CI visibility, stress gate, UCRT64 Windows, Xvfb Linux desktop smoke

### Renderer convergence (v1.11.1, 2026-11-17 — delivered)
- The shared design-token catalog (`internal/theme`, six themes,
  WCAG-gated, JSON form for web surfaces) and the live switcher
  (apply/cancel/save contract — navigation-preserving)
- The renderer/engine registry (`internal/renderer`: manifests,
  maturity, per-platform verification, capability truths, gaps) with
  fail-closed vocabulary tests
- The Engine Gallery in the Fyne shell (ninth destination, Ctrl+9):
  registry-driven cards, honest not-built states with reasons, no fake
  launch buttons
- The window-controls trio with guaranteed 44×44 DIP hit areas; real
  graceful-close funnel; minimize/tray disabled WITH reasons
- Isolated POCs: Go-Gui v0.96.0 (10/10), GoGPU (2/2), Wails v3.0.0
  Web Workspace (bridge/security/feature suites green; the WebView
  window is `wails_runtime`-tagged)
- Complete tracked-file catalog (`FILE_CATALOG.md`/`.json` + generator
  + coverage contract test)
- Immutable-SHA action pins in both workflows (CodeQL run-43883548572
  alerts #185/#186 closed at source; hosted scan verification pending
  the user's publish)
- The NSIS 3.13 version-gate repair (run-43883548198 root cause) with
  its fixture suite

---

## Evidence labels

- **D** — Documented/verified by current code, tests or CI
- **I** — Architectural inference (needs measurement plan)
- **H** — Hypothesis (needs experiment before any implementation decision)

---

### UI/UX hybrid upgrade (direction absorbed from the operator research, 2026-11-01)

Native Fyne stays primary; the Workspace stays on-demand, loopback-only,
token-gated, CSP-strict; no second UI stack; no new authorities. Phases
0–2 (design tokens + contracts; conversation surface; artifact/workspace
integration) are DELIVERED and pinned in
[`ui-contract.md`](ui-contract.md). Remaining direction:

- **Phase 3 — Project / Goal / multi-agent command surface**: elevate the
  Agent/Goal view (plan → act → verify stages, checkpoint visibility,
  approval parking); surface parallel/sub-agent activity only if the
  runtime already exposes it; optional low-risk project-like grouping (a
  label/filter on sessions/goals, never a second store); keep Chat vs
  Agent separation and mode clarity.
- **Phase 4 — System, Models, Settings polish**: laboratory surfaces with
  control-panel discipline (controls secondary but always available) and
  the evidence ladder rendered with the Phase 0 hierarchy.
- **Phase 5 — Smoothness, measurement, regression**: profile
  streaming/layout/refresh under realistic load; extend smoke/golden
  checks; every "smoother" claim backed by a test or an observable
  before/after.

Success metric: as fluid as the best web UIs, as orchestrable as the
best agent consoles, as mode-clear as the best studios — and more honest
and controllable than all of them.

---

## Current / near-term boundary (partially delivered)

- Automatic sub-agent fan-out from the Goal runner
- Protected-evaluation anti-hack guard
- MCP runtime wiring into the live tool registry
- Oversized-output spool handles
- Native-engine forward-pass arithmetic optimization (SIMD batched, evidence-gated)

### Renderer convergence — remaining (gated, honest)

- **Hosted verification of the v1.11.1 candidate** (the user owns the
  publish): the Windows NSIS fix producing the real installer; the
  CodeQL scan confirming alerts #185/#186 fixed; hosted Fyne gates on
  the ninth destination.
- **Companion-window launch path** for the preview engines (a managed
  surface from the gallery, synchronized through the Go core) — after
  a supported host/interop POC proves the boundary.
- **Physical/hardware verification**: Narrator/NVDA (Go-Gui's corrected
  Windows UIA), AT-SPI, VoiceOver, real touchscreens, real GPU adapters
  (executed-backend telemetry only), IME/CJK fixtures.
- **Wails WebView window** on a capable host (`wails_runtime` tag),
  then its promotion gate (availability + sandbox + capability + IPC
  tests on real runtimes; macOS only after its WebKit is actually
  tested; mobile stays experimental).
- Custom theme editing (validated against `internal/theme`'s
  contract); per-frame telemetry; promotion-gate matrices per engine.

---

## Capability families — FUTURE (not implemented)

These families are the complete high-level inventory of user-facing outcomes the product aims to support over time. None are claimed as present until evidence exists in the tree.

### Conversation & writing (P0 foundation — largely present, continuous improvement)
- High-quality answering, drafting, revision, summarization, translation, structured outputs
- Context and instruction handling with honest token/context budgets

### Tool use & multi-step execution (P0 — largely present)
- Typed tool calling with schemas, policy, cancellation
- Planning, multi-step execution, verification of completion

### Coding & repository work (P0 — present via Coding Lab)
- Inspect, patch, build, test, review inside sandboxed workspaces

### Search & deep research (P1)
- Multi-source web research with verifiable citations
- Network and SSRF controls, honest source attribution

### Files, documents & retrieval (P1)
- Ingestion, search, range reading, citation of local files and documents
- Access control and provenance

### Data analysis & artifacts (P1 — core dataAnalysis present)
- Profile, transform, aggregate, join, export
- Charts, tables, and interactive artifacts with validated schemas

### Spreadsheet support (P1/P2)
- Read/write of common workbook formats through the artifact authority

### Multimodal (P2)
- Image understanding (vision-capable backends)
- Image generation / editing (optional provider or model)
- Audio transcription and speech generation / voice
- Bounded video / frame analysis with timestamps

### Memory, personalization & projects (P1)
- Approved durable memory with consent, retention, and deletion controls
- Projects and long-running resumable tasks with checkpoint schemas

### Scheduling & reminders (P2)
- Transparent scheduled work through the existing scheduler, with policy and consent

### Capability self-knowledge & diagnostics (P0 — partially present)
- Live, evidence-based answers to “what can you do?”
- Safe, redacted diagnostics and failure explanations

### Security, privacy & accessibility (ongoing)
- Tight execution bounds, permission model, redaction
- Accessible UI patterns and truthful capability reporting

### Universal Agent / computer-use (longer term)
- Controlled browser and desktop interaction, screen, mouse/keyboard, application discovery
- Document workflows, multimodal execution, connectors, voice foundation
- Bounded parallel tool orchestration, cycle detection, reflection/recovery

### AI Learning & Evaluation (longer term)
- Evaluation suites, baselines, experiments, promotion/rollback by measured evidence only
- Protected-evaluation guard as prerequisite

### Advanced Model Intelligence (longer term)
- Multi-model routing, specialist models, dynamic pools, resource-aware selection
- Optional local embedding/vector infrastructure only when measurements justify it

### SHEYTAN Local AI Platform (strategic end state)
- User-owned durable AI systems with full lifecycle ownership
- Scalable local workers, mature multi-run orchestration, portable import/export/versioning

---

## Performance measurement framework

Use existing RunClock / performance surfaces. Add measurements only when they support real optimization. No numeric targets without measurements made on this codebase.

---

## Engineering invariants (all releases)

- One authority per concern
- No sleep-based correctness
- Build success ≠ runtime proof; CI ≠ physical-host proof
- Hardware detection never reported as execution evidence
- Numeric/unsupported engine arguments fail before process spawn
- Codename-removal gate stays enabled
- Performance claims require measurements on this codebase
- Architecture proposals that replace the Go runtime or one-authority design are rejected by default
