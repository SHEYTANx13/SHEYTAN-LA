# Dev Notes

**Mandatory reading for every AI agent before starting any work.**

This file is the living checklist of known gaps, required updates, and verification items.

**Critical rule:** Agents must follow **both** this file **and** the human’s current prompt / instructions. The human prompt always takes precedence when there is any conflict. Documentation never overrides an explicit human instruction.

---

## Agent Start Checklist (do this every session)

1. Read the human’s current prompt completely.
2. Read this file (`Dev-Notes.md`) completely.
3. Read `docs/agent.md` (especially the staged development process and definition of done).
4. Skim `docs/ARCHITECTURE.md` and `docs/ROADMAP.md` if the task touches architecture or direction.
5. Pick **one** clear success condition (prefer an open item below, or the exact request in the human prompt).
6. State the success condition, the files you will touch, and the evidence you will produce.
7. Implement only what is required for that success condition.
8. Verify (see definition of done).
9. Update this file (mark items) and any affected public docs in the same change set.
10. Hand off with evidence class and a short summary. Do not push.

---

## Definition of Done (every change must satisfy)

A change is done only when **all** of the following are true:

- [ ] Success condition stated at the start is met
- [ ] Code (if any) compiles and relevant tests pass
- [ ] Evidence class is recorded: `local-static` | `hosted` | `physical`
- [ ] `Dev-Notes.md` updated (items marked or new items added)
- [ ] Affected public docs updated in the same change (`README`, `ARCHITECTURE`, `FEATURES`, `ROADMAP`, `retired`, etc.)
- [ ] No new version numbers introduced into product-facing docs
- [ ] No speculative refactors or drive-by cleanups outside the stated success condition
- [ ] Hand-off message includes: what changed, evidence, remaining risks

---

## Open items

### Documentation & process
- [ ] Keep `Dev-Notes.md` itself current after every meaningful change
- [ ] Ensure every new public surface is mentioned in `README.md` and `FEATURES.md`
- [ ] After any architecture change, update `ARCHITECTURE.md` in the same commit
- [ ] After any retirement, update `retired.md`

### CI / packaging
- [ ] Confirm hosted Windows UCRT64 build + smoke + installer remain green after next push
- [ ] Confirm Linux Xvfb desktop smoke remains green
- [ ] Verify consolidated run-log artifact is produced on both red and green runs

### Product surface
- [ ] Physical desktop verification of 3D Studio WebGL rendering
- [ ] Physical high-DPI / multi-monitor behaviour of the Fyne shell
- [ ] End-to-end Goal engine run on a real long-horizon task with human approval flow

### Technical debt / future hardening
- [ ] Governor is still policy-only; full closed-loop actuation not implemented
- [ ] Native C++ engine default remains evidence-gated (physical Windows warm-TTFT still outstanding)
- [ ] Automatic sub-agent fan-out from the Goal runner not yet implemented
- [ ] MCP client implemented but not yet registered into the live tool registry

---

## Recently closed

- [x] Full documentation overhaul (README, LICENSE, ARCHITECTURE, ROADMAP, FEATURES, agent, UPDATE, CONTRIBUTING, SECURITY, retired.md)
- [x] Dual-licensor language (SHEYTAN + Parsaetak)
- [x] One-Go-UI (Fyne) migration completed and documented
- [x] Capability families from GPT6 research absorbed into ROADMAP; research file archived
- [x] Agent Start Checklist + Definition of Done added

---

*This file is proprietary material under the Proprietary License.*
