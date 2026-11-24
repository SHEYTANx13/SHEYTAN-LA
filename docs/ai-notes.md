# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 45323044820 repair + iOS retirement + go-gui direction (2026-11-24) — supersedes all handoffs below

Baseline: `d6778fd3e313cfd0aef5b285fc6febf26042273f` (== origin/main == the exact commit the
run 45323044820 platform build tested; it is the squashed landing of the previous session's
`repair/run45287161747-alpha-ux` work). Branch: `repair/run45323044820-identity-android-ios`
(local only; NO remote operations, per the standing manual rule). Delivery `note.md` carries
the full evidence matrix.

What this session established and changed:

1. **THE DARWIN ROOT CAUSE IS CGO ERRNO PERSISTENCE** (fixed at source): cgo's two-result
   form (`n, err := C.f()`) surfaces the POSIX thread's errno AT CALL RETURN as the Go error,
   and a SUCCESSFUL libproc call never resets errno. One failed candidate probe (EPERM on a
   root-owned process — the walk starts at launchd on every macOS runner — or ESRCH on a
   candidate that exited mid-walk) left errno set, and EVERY later successful helper call
   inherited the stale errno as a Go error. The whole walk collapsed to
   ErrPortOwnerUnknown → the entire attribution family + `internal/llm
   TestAdoptionAcceptsManagedEngine` failed deterministically on darwin/arm64, darwin/amd64
   and the iOS lane host while Linux stayed green. Every helper in `identity_darwin.go` now
   sets `errno = 0` BEFORE its libproc call (c_start_time already carried the pattern — the
   asymmetry was the tell). Regression test `TestListeningProcessAfterFailedProbeSucceeds`
   (identity_darwin_cgo_test.go) reproduces the poison deterministically:
   `runtime.LockOSThread()` + a failed `ProcessExePath` on a reaped pid (leaves ESRCH in the
   thread TLS) + immediate self-listener attribution MUST succeed.
2. **Dead-owner test honesty extended** (`TestListeningProcessDeadOwnerRefused`): an
   observed-dead pid recycled into an UNINSPECTABLE process (inspection denied — root-owned
   incarnation on a busy runner) is the same honest-race class as the already-handled
   successful-probe case; it records an environment skip. Still-dead pids must still answer
   typed ErrProcessNotFound; other classes still fail loudly.
3. **THE ANDROID GUARD ROOT CAUSE IS THE INCOMPLETE COMMITTED go.sum** (reproduced locally):
   `scripts/ci/module-preflight.sh` runs `go mod download all` on every lane; the committed
   `go.sum` lacked the sums `go mod download all` requires (+368 additions-only on go1.27.4
   in this sandbox, verified stable across three runs). The preflight therefore MODIFIED the
   tracked tree and the android lane's packaging cleanliness guard
   (`git status --porcelain`) failed deterministically — only android runs the guard after
   packaging, which is why it alone went red while the other lanes carried the same silent
   go.sum mutation. Fix: the completed go.sum is committed; `go mod download all` is now a
   verified tree no-op. The guard was NOT weakened.
4. **iOS lanes retired everywhere** (owner direction): five-target matrix
   (windows-x64, linux-x64, macos-arm64, macos-x64, android-arm64);
   `scripts/build/ios.sh`, `scripts/build/ios-device.sh`, `docs/platforms/IOS_SIGNING.md`
   deleted; the releasecontract suite now FAILS on any `ios-*` matrix target or retired iOS
   artifact name (retirement pinned, not just deleted). `internal/proc/identity_ios.go`
   STAYS: it is the `GOOS=ios` type-safety provider (a build-tag file, not a CI lane) — it
   keeps accidental ios builds honestly typed-unsupported.
5. **go-gui primary shell = authorized DIRECTION, NOT executed this update**: the framework
   exists (go-gui-org/go-gui, CGO-free desktop GPU rendering) but its macOS backend requires
   Apple silicon — "Intel Macs are not supported" — so the five-platform goal itself is
   unsatisfiable with go-gui today; the migration surface is the whole 49-file ui/fyne shell
   plus its golden/responsive/IPC test matrices. Direction, blockers and preconditions are
   recorded in `docs/ROADMAP.md` ("Go-Gui primary shell"). Fyne stays the primary shell.
6. **The previous session's UX/runtime items are all on main now** (verified in source this
   session): download phase+bytes+percent on the engine-update surface, `Browse for .gguf…`
   file dialog on the Models panel, full-width history search row, 44-DIP floorTouch touch
   floors, ALPHA display identity, files-only no-model engine update. The app.log runtime
   trace for the tested build is CLEAN (successful engine update b10642→b12669 with staged
   identity verification, graceful shutdown, exit 0) — no runtime errors/warnings to fix.
7. **Known pre-existing sandbox quirk (NOT a regression, reproduced on pristine bases by two
   prior sessions)**: `go test ./internal/desktop/` on linux fails to LINK the test binary
   (`goffi_errno_location_stub: unhandled relocation`, gogpu/goffi vs the sandbox linker);
   under `-tags headless` the package has no tests and the headless suite is green. The
   hosted windows lane compiles and runs it green.
8. **Reusable sandbox recipe (from the 2026-11-24 session)**: the fyne test driver builds
   here WITHOUT root by `apt-get download`ing the GL/wayland/xkb dev packages into a local
   prefix, rewriting the extracted .pc prefixes and re-pointing dangling dev symlinks
   (full package list in git history, docs/changelog.md ALPHA 2026-11-24 entry). Only needed
   when touching ui/fyne — this session did not touch it.

Next actionable step for the operator: push the branch (or approve the push) and let the
hosted platform build + CodeQL run on the candidate — the darwin lanes must go green with
the errno fix, the android guard must pass on the completed go.sum, and the run should be
the first with NO iOS lane noise at all.
