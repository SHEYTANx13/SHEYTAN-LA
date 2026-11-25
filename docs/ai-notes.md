# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 45424141601 repair: errno theory disproven, walk made self-diagnosing, go-gui v0.99.1 (2026-11-24) — supersedes all handoffs below

Baseline: `12e117a9e528bf98905992ecbd5314f8d6d644f4` (== origin/main == the exact commit the
run 45424141601 platform build tested; it is the squashed landing of the previous session's
`repair/run45323044820-identity-android-ios` work). Branch: `repair/run45424141601-walk-diag`
(local only; NO remote operations, per the standing manual rule). Delivery `note.md` carries
the full evidence matrix.

What this session established and changed:

1. **THE PREVIOUS SESSION'S ERRNO THEORY IS DISPROVEN BY THE RUN ITSELF.** The tested commit
   `12e117a` CONTAINED the errno-reset fix, yet the same attribution family failed on
   darwin/amd64 — including the regression test built to prove that theory
   (`TestListeningProcessAfterFailedProbeSucceeds`). Do NOT re-apply errno-only patches.
2. **THE DECISIVE OBSERVATION SET** (read before touching the walk again): exactly 8 tests
   failed in the suite — every test that calls `ListeningProcess` on a port with a LIVE
   listener (OwnProcessListenerAttributed, FindsSelfOwnedListener, IPv6ListenerAttributed,
   IPv6LoopbackAttributed, AttributesForeignChildListener, AfterFailedProbeSucceeds,
   DeadOwnerRefused) plus internal/llm TestAdoptionAcceptsManagedEngine (5.13s retry loop).
   ALL of these PASSED on the same lane: the record-level libproc pipeline for THIS process
   (TestTCPListenerRecordCarriesTCPKindAndCorrectPort, TestNonTCPKindRecordsSkip...),
   both free-port tests, WrongPortRefused, StartTimeBindsIncarnation (c_start_time works),
   LocalPortOfNetworkOrderConvention. Therefore the failure lives ONLY in the stages the
   passing tests never exercise: `proc_listallpids` and `proc_pidpath` on the OWNER.
3. **THE DOCUMENTED HOSTED-RUNNER EVIDENCE**: actions/runner-images#14756 (macos-15 image
   20260907.037) demonstrates `proc_pidpath` answering ENOENT — NOT ESRCH, NOT EPERM — for
   a LIVE process whose executable was unlinked while running, even as root; ENOENT can
   never mean "the process is gone" (a reaped pid answers ESRCH). This code left ENOENT in
   the untyped default class: the recycled-incarnation case in
   TestListeningProcessDeadOwnerRefused could fail loudly on a path-less incarnation, and
   the walk's inaccessible accounting carried untyped noise.
4. **WHAT THIS UPDATE CHANGED** (evidence classes in note.md; native darwin NOT RUN here):
   - `ErrProcessPathUnresolvable` (public sentinel, identity.go) + typed ENOENT mapping in
     `processExePathImpl`; the dead-owner test treats a path-less recycled incarnation as
     the honest environment skip (still-dead pids still answer typed not-found).
   - Per-record fault isolation in the walk's fd loop: one undocumented-kind/malformed
     record no longer breaks the candidate's WHOLE descriptor loop (ascending fd order
     meant one exotic socket fd could hide the true owner behind it). Only a gone
     candidate still stops the loop; the fail-closed final answer is unchanged.
   - THE WALK IS SELF-DIAGNOSING: every `ErrPortOwnerUnknown` answer carries ONE bounded
     `walk-diag` line (pids/self/self_sockets/walked/denied/gone + typed enum_err and
     list_err/record_err/path_err buckets; counts only, no pids/paths/names, ASCII,
     deterministic). Pure-Go pieces live in identity_darwin_path.go (compiled by the
     CGO-free darwin type-check builds too); native tests TestErrorClassOfTypedClasses +
     TestRenderProcWalkDiagnosticBounded run on the darwin lanes.
   - LANE-PARITY FAILURE EVIDENCE: summarize_headless_failure existed only in macos.sh and
     emitted only FAIL FRAMES — the t.Fatalf message lines never reached the annotation
     channel (why six repair sessions guessed). linux.sh/android.sh/windows.ps1 now emit
     the same bounded records + message lines; macos.sh gained the message class.
   - go-gui v0.97.0 -> v0.99.1 in the root go.mod (owner direction; go mod tidy; `go mod
     download all` re-verified as a tree no-op — the android guard stays strict).
5. **macos-arm64 `cancelled` in run 45424141601**: 0 steps started with fail-fast:false
   unchanged — hosted runner allocation, not a repository defect. Nothing to fix.
6. **go-gui primary shell = authorized DIRECTION, still NOT executed**: the migration
   surface is the whole 49-file ui/fyne shell; the macos-x64 blocker stands (go-gui's macOS
   backend requires Apple silicon). Recorded in docs/ROADMAP.md + GO_GUI_MIGRATION.md
   (v0.99.1 addendum this session). Fyne stays the wired production shell.
7. **Previous sessions' UX/runtime items are all on main** (verified in source): download
   phase+bytes+percent, Browse for .gguf dialog, full-width history search, 44-DIP
   floorTouch, ALPHA display identity, files-only no-model engine update; app.log trace for
   the tested build CLEAN.
8. **Known pre-existing sandbox quirks (NOT regressions; each reproduced on pristine bases)**:
   (a) `cmd/sheytan-local-agent` build fails on the missing GL pkg-config/X dev headers —
   hosted lanes are the authority; (b) goffi linker relocation failure for go-gui/gogpu
   desktop test links (`goffi_errno_location_stub: unhandled relocation`) — hits
   cmd/sheytan-renderprobe here, hosted lanes compile it; under `-tags headless` both
   surface areas carry no tests and the headless suite is green (76 ok / 0 FAIL this
   session, twice).

Next actionable step for the operator: push the branch (or approve the push) and let the
hosted platform build + CodeQL run on the candidate. The next hosted darwin failure — if
one survives this update — will NAME its broken stage in the annotation channel
(walk-diag + failure-message records), ending the guess cycle. If the lanes go green, the
candidate is the first verified-clean darwin state since the libproc walk landed.
