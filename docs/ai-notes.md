# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 45522548507 repair: identity re-proven path-independently, verdict-aware evidence, adaptive web shell landed (2026-11-25) — supersedes all handoffs below

Read this BEFORE touching the darwin walk or the lane scripts again. The decisive
observation set of run 45522548507 (commit `b8d16bb` — the squashed run 45424141601
repair pushed to main):

1. **The windows "9 failures" were 1 real + 8 selector false positives.** The
   b8d16bb message selector read the `-json` stream verdict-blind; its skip
   exclusion could not match the real skip messages (`: symlink creation requires
   privileges on Windows`, `: the test binary is a PE on Windows`). NEVER read the
   whole stream without collecting the failed test names from the fail events
   first. All four lane scripts now do exactly that (messages before frames, the
   consolidated channel's bounded budget carries the WHY); a synthetic-stream
   functional test pins the behavior.
2. **The darwin family needed identity WITHOUT the image path, not another errno
   mapping.** The ENOENT sentinel renamed the class; the walk still refused a live
   owner. The fix re-proves identity path-independently: self -> os.Executable
   (launch path, unlink-proof), foreign same-user -> KERN_PROCARGS2 argv[0]
   (public sysctl). Fail-closed states are unchanged. Do NOT re-apply errno-only
   patches — the walk now has three identity sources and a discriminator
   (walk-diag path_err vs enum_err) if any darwin failure survives.
3. **The run's certain defect**: TestRenderProcWalkDiagnosticBounded shipped red
   on EVERY darwin build — the fixture bypassed recordDiagError's fill-side fold
   so "other=5" could never render. Fixed by filling through the production path.
   Lesson: a darwin-tagged test that has never RUN is unproven — the CGO-free vet
   proves compilation, never behavior.
4. **The web-UI-first architecture is LANDED, the flip is GATED.** internal/webshell
   (adaptive shell: registry, 600/900/1280 classes, tokens, touch floors, palette,
   notifications, download progress with file size + speed) is testable everywhere
   (the 77-package headless suite carries it). internal/webworkspace/host.go runs it
   as the primary window (typed runtime API verified against the pinned v3.1.0
   module source; CGO-free purego vet green on linux/darwin/windows). Desktop
   selection: SHEYTAN_UI=auto|web|fyne. The fyne-file DELETION stays gated on the
   hosted flip run (build -tags webworkspace on all three desktop lanes + a real
   window launch) — do not delete fyne files before that evidence exists.
5. **Deps/actions moved as directed**: x/net 0.63.0, chromedp 0.24.0, cdproto
   0.160.8, fyne 2.8.3; setup-go v7.1.0, msys2 v2.34.0, codeql-action v4.39.4
   (from the repo's own dependabot branches, SHA-pinned). go mod download all
   byte-stable x2 — the android cleanliness guard contract holds.

Next actionable step: push the candidate branch (operator authorization required)
and let the hosted platform build + CodeQL run on it.
