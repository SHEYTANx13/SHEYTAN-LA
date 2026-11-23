# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 45165536396 repair + touch-responsive UX pass (2026-11-23) — supersedes all handoffs below

Baseline: `93ce07ef6e0bf6562092ee70b40ac7f76efd77f2` (== origin/main == the exact commit the
run 45165536396 platform build tested; CodeQL went green there per the operator). The prior
session's candidate (squash of mesh phase 2a + darwin proc fixes + artifact policy) IS
`93ce07e`; its darwin headless suite STILL failed on all three Apple lanes. Branch:
`repair/run451655-darwin-runtime-touch` (local only; NO remote operations, per the standing
manual rule). Delivery `note.md` carries the full evidence matrix.

What this session established and changed:

1. The run's ONLY captured specifics: macos-x64 failed on lazy module downloads
   (proxy.golang.org read timeouts, gogpu family, six occurrences); macos-arm64 and the iOS
   lane host failed with exit 1 and ZERO test-failure records — a suite-timeout panic or a
   package crash leaves no collector-visible trace (no `--- FAIL:`, no file:line). Hosted job
   logs were NOT retrievable (anonymous REST rate-limited from this sandbox; no Actions log
   tool in the plugin set). The exact hosted failure text remains UNKNOWN — the fix makes the
   next run name it.
2. `scripts/ci/module-preflight.sh` (NEW): `go mod download all` with exactly three bounded
   attempts + fixed 15 s backoff, fail-closed with the captured proxy error; wired into
   macos.sh / ios.sh / linux.sh / android.sh headless-tests BEFORE `go test`. Gates never
   download.
3. `summarize_headless_failure` in macos.sh/ios.sh: on suite failure the lane dumps the
   stream's own root-cause records (panic / fatal error / FAIL / timeout, bounded to 15,
   verbatim) as `::error::` annotations before the gate marker.
4. Collector: JSON-frame `panic:` / `fatal error:` output buffers per (package, test) and
   emits `[package pkg]` rows at the package FAIL verdict; raw panic/fatal lines are error
   evidence in failed/cancelled jobs. Pinned by fixture case 34 (suite 40/0).
5. proc darwin determinism: WrongPortRefused queries a kernel-proven free port (bind+close
   probe) instead of `bound+1` (a busy runner can carry a REAL foreign listener there);
   ProvenFreePort fails ONLY when the walk attributes OUR closed port to our own binary and
   skips honestly when a foreign process recycled the ephemeral port.
6. Touch UX (owner-directed): `floorTouch` (ui/fyne/widgets.go) — a one-child container with
   a visibility-aware layout — makes the 44/48 DIP UX-06 floor STRUCTURAL for rail rows,
   strip rows, More, Retry (44) and Send/Stop (48) and the empty-state sample chips (44) at
   every width. Structural goldens must be REGENERATED on the hosted GL host
   (FYNE_UPDATE_GOLDENS=1) — NOT run locally (no GL/wayland headers in this sandbox, no root;
   the ui/fyne compile gate stays the hosted linux lane). Popup-menu items remain
   driver-metric (recorded boundary in TOUCH-RESPONSIVE-MATRIX.md §1).
7. Mesh phase-3 libp2p transport stays GATED (owner decision: new dependency + two-device LAN
   demo prerequisite); nothing else from the previous session's leftover list is code work.

Next actionable step for the operator: push the branch (or approve the push) and let the
hosted platform build + CodeQL run on the candidate; the darwin lanes will either go green
or NAME their failing package in the errors/warnings report. Then regenerate the UI goldens
on any GL-capable host if the ui gate is touched there.
