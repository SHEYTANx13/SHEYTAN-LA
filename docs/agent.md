# Agent Operating Manual

This is the durable operating guide for any AI agent working on SHEYTAN-LA.

**Non-negotiable rules**

1. The human’s current prompt always takes absolute precedence over every document, including this one.
2. `docs/Dev-Notes.md` is **human-only**. AI agents must not edit it unless the human explicitly orders it in the current prompt.
3. AI agents write all working notes, progress, evidence, and hand-offs exclusively in `docs/ai-notes.md`.
4. Work must be fast, focused, and free of double work. Attention and tokens are limited; use them deliberately.

---

## 0. Efficient multi-agent development process

SHEYTAN-LA is developed primarily by coordinating multiple AI agents without a traditional IDE. The human operator reviews and is the only one who pushes.

### Token, attention, and effort rules

- Load the **minimum necessary context**: human prompt → `Dev-Notes.md` (read-only) → `ai-notes.md` (previous hand-off) → `agent.md` → only the specific code/docs required for the success condition.
- Do **not** re-read or re-summarize large docs that have not changed and are not relevant to the current success condition.
- Prefer one clear success condition per session. Finish it completely (including docs and hand-off) before starting another.
- Avoid exploratory refactors, drive-by cleanups, or “while I’m here” changes.
- Do not duplicate information that already lives in ARCHITECTURE, ROADMAP, FEATURES, or Dev-Notes. Point to it instead.
- When verification is needed, run the smallest relevant subset of the verification ladder first. Escalate only if the small set is green and more coverage is required.
- If blocked, stop early, record the blocker in `ai-notes.md`, and hand off. Do not burn tokens looping.

### Staged process (complete, no skipped stages, no double work)

| Stage | Activity | Exit criterion |
|-------|----------|----------------|
| 1. Orient | Read human prompt + Dev-Notes (read-only) + ai-notes + this manual | One-sentence success condition |
| 2. Audit | Inspect only the code/docs that the success condition actually touches | Short audit note in ai-notes |
| 3. Plan | Minimal file list + tests + docs to update | Plan matches success condition exactly |
| 4. Implement | Smallest correct change | Focused diff |
| 5. Verify | Relevant tests / checks only | Evidence class recorded |
| 6. Document | Update affected public docs + write hand-off in ai-notes | Docs match code; ai-notes current |
| 7. Hand-off | Clear summary for human or next agent | Next step is obvious |

Never perform the same analysis or rewrite the same section twice in one session unless new evidence forces it.

### Definition of Done

- Success condition met
- Relevant tests pass / evidence recorded
- Evidence class stated
- `ai-notes.md` contains an up-to-date hand-off
- Affected public docs updated in the same change
- `Dev-Notes.md` left untouched (unless human ordered otherwise)
- No version numbers added to product-facing docs
- No work outside the stated success condition

---

## 1. What this is

SHEYTAN-LA is a local-first AI engineering laboratory. One Go core owns every authority exactly once and serves it over secured local IPC. The desktop is a single in-process Go + Fyne application. Managed llama.cpp + optional native C++ engine.

Motto: **“The model proposes. The tools execute. The laboratory verifies.”**

### Durable invariants

- One authority per concern
- No sleep-based correctness
- Build success ≠ runtime proof
- Detection ≠ execution
- Model output ≠ objective evidence
- Codename-removal gate enabled
- Version identity from `internal/config.AppVersion` only

---

## 2. Repository map (high level)

- `cmd/` — entrypoints and printversion
- `internal/` — all core authorities
- `ui/fyne/` — one product UI
- `native/` — C++ engine
- `docs/` — documentation (Dev-Notes = human, ai-notes = AI)
- `.github/workflows/` — CI

---

## 3. Verification ladder (use the smallest sufficient prefix)

1. `gofmt -l .`
2. `go build -tags headless ./...`
3. Relevant package tests
4. Race tests only on concurrency-critical packages when touched
5. UI tests only when `ui/fyne` is touched
6. Full stress / release-surface only when the change affects those contracts

---

## 4. Evidence classes

- `local-static` — compile + unit tests
- `hosted` — green GitHub Actions on the exact revision
- `physical` — real hardware (fail-closed)

Never inflate the class.

---

## 5. Documentation rules

- Docs must reflect code and evidence, never intention alone.
- AI agents update public docs when behaviour or architecture changes, and write session state only in `ai-notes.md`.
- `Dev-Notes.md` remains the human’s prioritised backlog.

---

## 6. Push protocol

Agents never push. Operator pushes and tags.

---

## 7. Protected surfaces

Touch only with an explicit human request or a clear item in Dev-Notes:

- One-Go-UI + local IPC
- Execution/evidence ladder + GPU truth
- Governor admission
- Goal engine terminal settlement
- Session-list generation guard + zero-session Send
- Stress / release-surface gates
- Codename-removal gate

---

## 8. File responsibilities

| File | Who writes | Purpose |
|------|------------|---------|
| `docs/Dev-Notes.md` | Human only | Curated open items and process rules |
| `docs/ai-notes.md` | AI agents | Working notes, evidence, hand-offs |
| `docs/agent.md` | Human (+ rare AI under instruction) | This manual |
| `docs/ARCHITECTURE.md` | Both (when architecture changes) | What is built |
| `docs/ROADMAP.md` | Both (when direction changes) | Capability direction |
| `docs/FEATURES.md` | Both | Implemented vs coming |
| `README.md` | Both | Current product description |
| `docs/changelog.md` | Human / release process | Release history |

---

*Human prompt first. Minimum context. One success condition. Evidence. Clean hand-off in ai-notes.md. No double work.*
