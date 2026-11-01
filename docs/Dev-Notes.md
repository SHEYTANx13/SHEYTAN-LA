# Dev Notes

**Human-only file.**  
AI agents must **not** edit this file unless the human explicitly instructs them to do so in the current prompt.

AI agents write all working notes, progress, evidence, and hand-offs in `docs/ai-notes.md` instead.

This file is the developer’s curated list of open items, priorities, and process rules. It is the source of truth for “what needs to be done.”

---

## Agent obligations (read-only for AI)

Before any work an AI agent must:

1. Read the human’s current prompt (highest priority).
2. Read this file.
3. Read `docs/agent.md`.
4. Read `docs/ai-notes.md` (previous agent hand-off).
5. Follow the staged process and Definition of Done in `agent.md`.
6. Write all of its own notes exclusively in `ai-notes.md`.

---

## Definition of Done (enforced on every change)

- Success condition met
- Relevant tests pass / evidence recorded
- Evidence class stated (`local-static` | `hosted` | `physical`)
- `ai-notes.md` updated with hand-off
- Affected public docs updated in the same change
- No version numbers introduced into product-facing docs
- No work outside the stated success condition

---

## Open items

### Documentation & process
- [ ] Keep this file current (human)
- [ ] Ensure every new public surface appears in README + FEATURES
- [ ] Architecture changes update ARCHITECTURE.md in the same commit
- [ ] Retirements update retired.md

### CI / packaging
- [ ] Hosted Windows UCRT64 build + smoke + installer green
- [ ] Linux Xvfb desktop smoke green
- [ ] Consolidated run-log artifact on red and green runs

### Product surface
- [ ] Physical verification of 3D Studio WebGL
- [ ] Physical high-DPI / multi-monitor Fyne behaviour
- [ ] Real long-horizon Goal engine run with approval flow

### Technical debt
- [ ] Governor still policy-only
- [ ] Native engine default still evidence-gated
- [ ] Automatic sub-agent fan-out from Goal runner
- [ ] MCP client not yet in live tool registry

---

## Recently closed

- [x] Documentation overhaul and dual-licensor language
- [x] One-Go-UI (Fyne) migration
- [x] Capability families absorbed into ROADMAP; GPT6 research archived
- [x] Agent Start Checklist + Definition of Done
- [x] Separation of Dev-Notes (human) and ai-notes (AI)

---

*Proprietary material under the Proprietary License.*

---

## UI/UX Hybrid Upgrade — Antigravity + Web UIs + Google AI Studio

*Provenance: operator UI/UX research, inserted by the run #123 fix session, 2026-11-01, under explicit operator authorization in the current prompt (the one sanctioned exception to the "never edit Dev-Notes.md" rule — from the next session onward that rule applies again in full). The content below is binding.*

### Research synthesis

Sources of strength and what each contributes:

- **Antigravity 2.0** — project-centric organization, artifact side panel + review flow, multi-agent/sub-agent visibility, async + scheduled task awareness, clean sidebar density. Closest peer as a desktop agent command center.
- **ChatGPT / Claude / Gemini** — refined visual hierarchy, spacing, typography; Stop takes the place of Send while busy; excellent empty states; smooth streaming. The daily conversation quality users already expect.
- **Google AI Studio** — unified Playground (single surface, low context switching); clear left-rail mode separation; live preview + code side-by-side in Build; Design Variations (explore multiple visual directions without rewriting prompts); model/parameter controls that stay secondary; capability-priming homepage. The best lab + builder hybrid among web tools — closest conceptual match to SHEYTAN-LA's laboratory identity.
- **SHEYTAN-LA (keep and elevate)** — honest phases + memory line + evidence ladder; zero-session Send + generation guards; three-zone responsive shell; native primary + secure on-demand Workspace; System Centre / Governor; Chat vs Agent hard separation; single-authority architecture. This is the differentiator. Do not dilute it.

AI Studio signals worth adopting: mode clarity in the left rail (distinct destinations, not buried menus); a unified interaction surface that reduces context switching; the side-by-side work + result pattern; the Design Variations mindset (explore visual directions without destroying current state); controls (model, parameters, depth) available but never competing with primary content; capability-first empty/welcome states.

**Hybrid principle (binding for every phase):** native Fyne remains primary. Workspace stays on-demand, loopback-only, token-gated, CSP-strict. No second UI stack. No new authorities. Laboratory truth surfaces stay visible and honest. Raise conversation and artifact quality to Antigravity / AI Studio / web level while keeping control and observability as advantages.

### Phased plan

1. **Phase 0: Design System and Contracts.**
2. **Phase 1: Conversation Surface (Chat + Agent)** — highest ROI.
3. **Phase 2: Artifact / Workspace Integration** (Antigravity artifact panel + AI Studio side-by-side feel).
4. **Phase 3: Project / Goal / Multi-Agent Command Surface.**
5. **Phase 4: System, Models, Settings Polish** (AI Studio control-panel discipline).
6. **Phase 5: Smoothness, Measurement, Regression.**

Success metric: as fluid as the best web UIs, as orchestrable as Antigravity, as clean and mode-clear as Google AI Studio, and more honest and controllable than all of them.

### Executable workflow

**Pre-flight (mandatory, every phase session):** read docs/Dev-Notes.md, docs/ai-notes.md, docs/ARCHITECTURE.md, docs/FEATURES.md, docs/ROADMAP.md, and ui/fyne/*.go (especially shell.go, view_chat.go, view_agent.go, panels.go, theme.go, widgets.go, state.go); confirm current HEAD and toolchain; run the existing UI tests + stress gate and record the baseline; write a hand-off into docs/ai-notes.md before any code change.

**Phase 0 — Design System and Contracts (do first).** Success condition: a single, documented design contract that every later phase obeys. Tasks: extend ui/fyne/theme.go with design tokens — spacing scale (4/8/12/16/24/32), typography hierarchy (title, section, body, mono, status), color roles (surface, elevated, accent, success, warning, danger, muted) for dark + light, radius and elevation rules, icon size + density rules. Define interaction contracts: composer Enter = send, Shift+Enter = newline, focus returns after send, Stop replaces Send while busy (web + AI Studio pattern); the live bubble never becomes a final message (existing settlement rule); empty states must prime capability (sample prompts / mode chips) without violating zero-session Send; the status line remains truthful only; primary views stay distinct and reachable (AI Studio left-rail discipline). Document the contract in docs/ui-contract.md. No major visual change yet beyond tokens if needed. Evidence: theme compiles, existing tests still green, contract written.

**Phase 1 — Conversation Surface (Chat + Agent).** Success condition: Chat and Agent feel as fluid and refined as top web UIs and as clean as the AI Studio Playground, while keeping SHEYTAN truth surfaces. Tasks in order: (1) visual hierarchy and spacing — apply Phase 0 tokens to transcript, live bubble, composer, session list; increase breathing room; role labels, timestamps, tool/activity lines secondary to content. (2) Composer upgrade — Stop takes the place of Send while busy; thinking depth + net search become compact chips or secondary controls; clear focus management and keyboard contract. (3) Live generation surface — keep phases, cumulative answer, reasoning, activity, memory line; make the live bubble visually distinct and calm (no jank, no flicker); preserve the triple-boundary flush + conflation queue. (4) Empty / zero-session state — capability-priming empty state for the current mode without violating zero-session Send. (5) Session list — cleaner titles, message counts, active indicator; keep the generation guard. Do not touch Workspace or System in this phase. Evidence: UI tests + manual smoke of send/stop/settle/zero-session; ai-notes updated.

**Phase 2 — Artifact / Workspace Integration.** Success condition: rich content feels first-class (Antigravity artifact panel + AI Studio side-by-side preview feel) without making the native shell depend on the browser. Tasks: make "Open Workspace" / rich block affordance more prominent and contextual after settlement (markdown/code/diff/table/chart); improve back-link and status when Workspace is open (native shell still owns navigation and controls); keep presentation.SelectSurface pure and deterministic; optional lightweight native preview cards for settled rich blocks that deep-link into Workspace; keep the security boundary (loopback, fragment token, CSP, zero commands) intact. Evidence: presentation + workspace tests green; no security regression.

**Phase 3 — Project / Goal / Multi-Agent Command Surface.** Success condition: Goal and multi-agent work feels closer to Antigravity mission control while staying inside existing Goal + multiagent authorities. Tasks: elevate the Agent / Goal view (clearer plan → act → verify stages, checkpoint visibility, approval parking); surface parallel / sub-agent activity IF the runtime already exposes it (do not invent new orchestration); optional low-risk project-like grouping (a lightweight project label or filter on sessions/goals, never a second store); keep the Automation view consistent with the new visual language; preserve Chat vs Agent hard separation and mode clarity. Do not break single-authority or freeze-binding rules. Evidence: goal / approval / multiagent tests green; ai-notes updated.

**Phase 4 — System, Models, Settings Polish.** Success condition: laboratory surfaces feel premium and truthful (AI Studio control-panel discipline + SHEYTAN evidence ladder). Tasks: models panel — clearer selection state machine + evidence ladder presentation, model/parameter controls secondary but always available; System Centre — Governor resource state / envelope / admission rendered with the new hierarchy; settings — grouped by user outcome, reduced cognitive load, advanced internals secondary; apply Phase 0 tokens consistently across all surfaces. Evidence: related tests + visual consistency check.

**Phase 5 — Smoothness, Measurement, Regression.** Success condition: smoother perceived run + no regressions on core contracts. Tasks: profile streaming / layout / refresh paths under realistic load and fix jank if found; ensure Stop, settlement, zero-session, generation guards, and the evidence ladder remain correct; add or extend UI smoke / golden checks for the new visual contracts where practical; final stress gate + UI suite green; update FEATURES.md / ARCHITECTURE.md only for real new surfaces; final ai-notes hand-off.

**Global rules for every phase:** one authority per concern — never move policy or state into the UI; native Fyne primary, Workspace secondary and secure; prefer pure layout / theme / presentation changes over new runtime surfaces; every claim of smoother or better must be backed by a test or an observable before/after; write ai-notes after every batch (clean rewrite preferred); do not invent Antigravity or AI Studio features that require new backend authorities — map only to existing Goal / multiagent / presentation / Workspace / Governor; preserve the three-zone responsive shell and mode clarity.

**Definition of done (whole workflow):** all phases complete or explicitly deferred with reason; existing contracts (zero-session, generation guards, evidence ladder, hybrid security, Chat vs Agent) still hold; UI tests + stress gate green; visual language consistent across Chat, Agent, System, Models, Workspace entry points; mode clarity and capability priming closer to Google AI Studio; conversation fluidity closer to web leaders; orchestration closer to Antigravity; ai-notes carries the clean final hand-off and evidence class; the human reviews and decides the next release cut.

**How to run it (operator's execution order — binding):** give the agent Phase 0 ONLY; review the design contract; unlock Phase 1 (the biggest daily UX win); proceed phase by phase — never one uncontrolled pass. After Phases 1 and 2 the product already feels substantially closer to Antigravity + AI Studio + web quality while keeping the laboratory edge.
