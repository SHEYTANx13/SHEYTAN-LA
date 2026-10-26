# Dev Notes

**Mandatory reading for every AI agent before starting any update.**

This file is the living checklist of known gaps, required updates, and verification items.  
Agents must:

1. Read this file at the start of every task.
2. Update the status of any item they touch.
3. Add new items when they discover unfinished work or technical debt.
4. Mark items `[x]` only when the change is committed **and** verified (tests / evidence).

---

## How to use this file

- Status markers: `[ ]` open · `[~]` in progress · `[x]` done & verified · `[!]` blocked / needs human
- Keep entries short and actionable.
- Prefer linking to the exact file or test that proves the item.
- Do not delete historical items — move completed ones to the “Recently closed” section at the bottom.

---

## Open items

### Documentation & process
- [ ] Keep `docs/Dev-Notes.md` itself current after every meaningful change
- [ ] Ensure every new public surface is mentioned in `README.md` and `docs/FEATURES.md`
- [ ] After any architecture change, update `docs/ARCHITECTURE.md` in the same commit
- [ ] After any retirement, update `docs/retired.md`

### CI / packaging
- [ ] Confirm hosted Windows UCRT64 build + smoke + installer remain green after next push
- [ ] Confirm Linux Xvfb desktop smoke remains green
- [ ] Verify consolidated run-log artifact is produced on both red and green runs

### Product surface
- [ ] Physical desktop verification of 3D Studio WebGL rendering (currently only Go-side contract is tested)
- [ ] Physical high-DPI / multi-monitor behaviour of the Fyne shell
- [ ] End-to-end Goal engine run on a real long-horizon task with human approval flow

### Technical debt / future hardening
- [ ] Governor is still policy-only; full closed-loop actuation over background subsystems is not implemented
- [ ] Native C++ engine default remains evidence-gated (physical Windows warm-TTFT comparison still outstanding)
- [ ] Automatic sub-agent fan-out from the Goal runner is not yet implemented
- [ ] MCP client is implemented but not yet registered into the live tool registry

---

## Recently closed

- [x] Full documentation overhaul (README, LICENSE, ARCHITECTURE, ROADMAP, FEATURES, agent, UPDATE, CONTRIBUTING, SECURITY, retired.md) — 2026-10-26
- [x] Removed all version numbers from product-facing docs
- [x] Dual-licensor language (SHEYTAN + Parsaetak) applied consistently
- [x] One-Go-UI (Fyne) migration completed and documented

---

*This file is proprietary material under the Proprietary License.*
