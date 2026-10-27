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
