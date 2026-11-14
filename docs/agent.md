# Agent Operating Manual

The durable operating guide for any AI coding agent working on
SHEYTAN-LA. One success condition per session, minimum context, real
evidence, clean hand-off.

**Non-negotiable rules**

1. The human's current prompt takes absolute precedence over every
   document, including this one.
2. [`Dev-Notes.md`](Dev-Notes.md) is human-only. Never edit it without an
   explicit authorization in the current prompt; a past authorization
   never carries over.
3. Working notes, evidence and hand-offs go in
   [`ai-notes.md`](ai-notes.md) — and only there.
4. Never push, tag, open PRs, merge, dispatch workflows or write to
   `main`. The operator owns every remote mutation.

---

## 1. What this is

SHEYTAN-LA is a local-first AI engineering laboratory: one Go core that
owns every authority exactly once, served over a secured local IPC
channel, with a single in-process Go + Fyne desktop client and a managed
llama.cpp / optional native engine.

Motto: *"The model proposes. The tools execute. The laboratory verifies."*

Durable invariants:

- One authority per concern (the UI is a client).
- One local transport: `internal/localipc.DialContext` (named pipe on
  Windows, Unix socket elsewhere); no TCP fallback.
- No sleep-based correctness; retries are bounded and event-driven.
- Build success, a visible window and fake-core tests never prove
  production IPC or runtime behavior.
- Hardware truth: detected ≠ available ≠ selected ≠ executed ≠ verified.
- Version identity comes from `internal/config.AppVersion` only.

## 2. Repository preflight (before ANY edit)

1. `git status`, `git log --oneline -5`, `git branch -a` — record the
   baseline commit, worktree state and any dirty/untracked files.
2. Never overwrite pre-existing user changes; preserve unrelated dirty
   files; stop and record conflicts instead of discarding them.
3. Work on a local non-main branch created from the freshly inspected
   HEAD.
4. Read the human prompt → [`Dev-Notes.md`](Dev-Notes.md) (read-only) →
   [`ai-notes.md`](ai-notes.md) → only the code/docs the success
   condition touches.

## 3. Staged process

| Stage | Activity | Exit criterion |
|-------|----------|----------------|
| 1. Orient | Prompt + Dev-Notes + ai-notes + this manual | One-sentence success condition |
| 2. Audit | Inspect only the surfaces the condition touches | Short audit note |
| 3. Plan | Minimal file list + tests + docs to update | Plan matches the condition exactly |
| 4. Implement | Smallest correct change | Focused diff |
| 5. Verify | Smallest sufficient verification prefix, escalating only on green | Evidence class recorded |
| 6. Document | Affected public docs + ai-notes rewrite | Docs match code |
| 7. Hand-off | Summary + exact next step for the human or next agent | Nothing unexplained |

Avoid exploratory refactors and drive-by cleanups; do not duplicate
content that lives in ARCHITECTURE / ROADMAP / FEATURES / Dev-Notes —
link to it.

## 4. Verification ladder (smallest sufficient prefix)

1. `gofmt -l cmd internal ui`
2. `go build ./...` (or `-tags headless ./...` where the host lacks CGO
   prerequisites)
3. Relevant package tests
4. Race gates only on touched concurrency-critical packages
5. `ui/fyne` tests when the UI is touched (the button-fit and responsive
   matrices run there)
6. Stress / release-surface gates only when those contracts change

Evidence classes — never inflate:

- `local-static` — compile + unit tests on this host
- `hosted` — green GitHub Actions on the exact revision
- `physical` — real hardware, fail-closed

## 5. Code and test quality

- Deterministic, event-driven tests; no timing hacks; never weaken or
  delete a test to make a gate green.
- No secrets, keys, tokens, prompt bodies or private user data in logs,
  test output, docs or archive metadata.
- Preserve the named-pipe DACL, Unix peer-credential checks, handshake
  key, protocol-version check and local-only transport.
- Windows test code is compile-verified with `GOOS=windows go vet` when
  no Windows host is available; say so — the hosted lane is the runtime
  authority.

## 6. Documentation obligations

- Docs reflect implemented behavior and evidence, never intention.
- Update affected public docs in the same change; rewrite
  [`ai-notes.md`](ai-notes.md) as a current snapshot (no diary appends).
- Retirements update [`retired.md`](retired.md); architecture changes
  update [`ARCHITECTURE.md`](ARCHITECTURE.md); release history only in
  [`changelog.md`](changelog.md).
- Every relative link must resolve; every command must be runnable from
  the documented directory or state its prerequisites.

## 7. Deliverables and hand-off

- Deliver what the work order names, in the workspace `download/`
  directory; validate archives (`unzip -t`) before claiming them.
- The hand-off states: what changed, the evidence (with classes), what is
  deliberately NOT done and why, and the single next action for the
  operator.
- Naming honesty: never label a candidate GREEN unless every required
  gate that can run has run green; label unrun gates NOT RUN.

---

*Human prompt first. Minimum context. One success condition. Evidence.
Clean hand-off.*
