# AI Notes

**This is the only file AI agents may freely update for working notes, progress, and session state.**

`docs/Dev-Notes.md` is reserved for the human developer and must not be edited by AI agents unless the human explicitly instructs it in the current prompt.

---

## Purpose

- Capture the current agent’s plan, progress, evidence, and hand-off notes.
- Avoid polluting the human-curated `Dev-Notes.md`.
- Give the next agent (or the human) a clear, short, up-to-date picture of what was done and what remains.

## Rules for AI agents

1. Read `docs/Dev-Notes.md` (human source of truth for open items) at the start of every session.
2. Write all of your own working notes, checklists, evidence logs, and status updates **only** in this file (`ai-notes.md`).
3. Keep entries short, structured, and current. Overwrite or clearly mark outdated sections instead of letting the file grow indefinitely.
4. At the end of a session, leave a clean **Hand-off** section so the next agent or the human can continue without re-deriving context.
5. Do not treat this file as a second roadmap or architecture document. Point to the real docs instead of duplicating them.

---

## Current session template (copy and fill)

```markdown
### Session — YYYY-MM-DD HH:MM (agent / model)

**Human prompt summary**
- …

**Success condition**
- …

**Plan**
- [ ] …

**Progress**
- …

**Evidence**
- class: local-static | hosted | physical
- commands / tests: …

**Docs touched**
- …

**Hand-off**
- Done: …
- Remaining: …
- Recommended next: …
- Risks: …
```

---

## Latest hand-off

*(Agents: replace this section at the end of every session.)*

No active hand-off yet.

---

*This file is proprietary material under the Proprietary License.*
