# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 45287161747 repair + ALPHA identity + engine/model UX (2026-11-24) — supersedes all handoffs below

Baseline: `c8c2858ba9f528b8a8f29c5a256033e619adce8d` (== origin/main == the exact commit the
run 45287161747 platform build tested; that run is the FIRST where the previous session's
diagnosability work made the darwin failures VISIBLE — real `--- FAIL:` records). Branch:
`repair/run45287161747-alpha-ux` (local only; NO remote operations, per the standing manual
rule). Delivery `note.md` carries the full evidence matrix.

What this session established and changed:

1. **IPv6 liveness pre-probe (REPRODUCED on linux, not just inferred)**: `ListeningProcess`
   dialed only `127.0.0.1`; a `[::1]`-only listener was "proven absent" (ok=false, nil
   error) and the ownership walk never ran → `TestIPv6ListenerAttributed` +
   `TestAdoptionAcceptsManagedEngine` + the whole darwin attribution family failed. Fixed:
   both loopback families probed; an inconclusive family downgrades to not-proven.
   Regression test `TestListeningProcessIPv6LoopbackAttributed` (windows = honest skip,
   IPv4-table-only contract).
2. **Walk resilience**: `listProcSocketFDs` now GROWS on proc_pidinfo's needed-bytes answer
   (the old code typed-truncated the whole walk when one same-user process held >512 fds);
   per-candidate faults (unreadable fd list, undocumented socket kind, unresolvable path)
   are recorded and skipped — a proven owner still returns exactly, the fail-closed
   ErrPortOwnerUnknown stands when nothing is proven.
3. **Darwin test-environment honesty**: AF_UNIX socket under `t.TempDir()` breaches the
   darwin sun_path(104) limit on hosted long TMPDIRs (mechanism reproduced on linux at 124
   chars) → short MkdirTemp pattern; dead-owner metadata probes on an observed-dead pid may
   answer a RECYCLED pid on busy runners → successful probe = honest skip, wrong typed
   class = fail; `mustOwnExe` moved to the untagged darwin test file (CGO-free darwin test
   build compiles again — pre-existing split).
4. **ALPHA display identity (operator direction)**: desktop banner, headless banner and IPC
   `coreVersion` carry `brand.DisplayVersion`; the numeric 1.11.1 identifier stays ONLY in
   packaging plumbing (version contract: build/config.yml productVersion == AppVersion —
   unchanged and green).
5. **Engine update UX**: user-triggered update with NO model now follows the startup-gate
   files-only policy (spec §2 Case D) instead of failing and rolling back a good download;
   the live update message carries phase + bytes + percent (reuses `humanBytes`).
6. **Model picker**: Models panel `Browse for .gguf…` opens the platform file dialog
   (extension-filtered) and fills the existing absolute-path import field — no core change.
7. **Search sizing**: history search entry owns its full row (Border layout, mode select
   right-aligned); logs filter and repo-index search already own full-width rows.
8. **UI goldens**: regenerated LOCALLY this session (the dev-header extraction trick —
   Debian debs extracted into a local prefix with pkg-config shim + CGO_CFLAGS — made the
   fyne test driver buildable WITHOUT root; recorded here so future sessions can repeat
   it: libwayland-dev libxkbcommon-dev libegl-dev libgles-dev libglvnd-dev libopengl-dev
   libgl-dev libglx-dev libxcursor-dev libxrandr-dev libxi-dev libxinerama-dev
   libxfixes-dev libxrender-dev libxxf86vm-dev, dangling dev-symlinks re-pointed at the
   system .so.N, prefix-rewritten .pc files). The 30→53-line golden delta is exactly the
   prior session's deliberate 44-DIP floorTouch geometry.
9. **Known pre-existing sandbox quirk (NOT a regression, reproduced on pristine c8c2858)**:
   `go test ./internal/desktop/` on linux fails to LINK the test binary
   (`goffi_errno_location_stub: unhandled relocation`, gogpu/goffi vs the sandbox linker).
   The hosted windows lane compiles and runs it green; under `-tags headless` the package
   has no tests and the full headless suite is green.

Next actionable step for the operator: push the branch (or approve the push) and let the
hosted platform build + CodeQL run on the candidate — the darwin lanes must now either go
green or name their exact remaining failure through three independent mechanisms (lane
evidence dump, collector classification, preflight error capture).
