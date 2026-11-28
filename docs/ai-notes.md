# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 46050871166 repair: android go.sum mutation reproduced, the web-shell-era smoke contract, the ImportAudit zero-import crash, the walk-diag evidence channel, plain-text log artifact, per-element shell CSS (2026-11-28) — supersedes all handoffs below

Read this BEFORE touching the lane scripts, the preflight, the smoke
contract or the web shell again. The decisive facts of this update:

1. **ANDROID LANE — `go mod download all` MUTATES go.sum (REPRODUCED).**
   The module preflight walked the transitive METADATA graph and, when
   the committed go.sum lacks the h1:/go.mod sums for graph-only
   modules, wrote +298 lines into the TRACKED go.sum — exactly one
   residue line (` M go.sum`), caught only by the android lane's
   post-packaging cleanliness guard. Committing the completed go.sum is
   structurally IMPOSSIBLE while `module-integrity.sh` pins
   `go mod tidy -diff` empty (tidy can never record the graph-only h1:
   sums — verified empirically). The preflight now runs plain
   `go mod download` (the build-relevant set, verified a tree no-op).
   NEVER re-introduce `go mod download all` in a lane script.
2. **WINDOWS LAUNCH SMOKE — TWO SHELL ERAS.** The packaged desktop
   shell is the WEB shell (`-tags webworkspace` →
   `internal/desktop.runWebUI`): it owns NO named-pipe dial, so the
   go-gui-era records can never appear. The web host performs ONE real
   core read through the exact served handler BEFORE the window boots
   and logs `core handshake successful … transport=same-origin-http` +
   `first core read succeeded (transport=same-origin-http)`.
   `internal/smokeverdict` accepts BOTH eras with LINE-LEVEL transport
   binding (a mixed-era log still fails); `windows.ps1` mirrors the
   contract. The named-pipe era remains the truth for the ANDROID
   mobile shell (ui/gogui bootstrap).
3. **IMPORT AUDIT — @( ) IS MANDATORY.** A module with no static import
   table makes `Get-StaticImportDlls` emit ZERO pipeline objects;
   scalar assignment unwraps that to $null and `.Count` throws under
   `Set-StrictMode -Version Latest` (the hosted windows lane died at
   ImportAudit.ps1:207). The caller wraps `@(...)` and fixture 7b pins
   the zero-import class. Any new PowerShell collection assignment
   from a function call needs the same @() guard.
4. **EVIDENCE CHANNEL — WALK-DIAG LINES CARRY 1100 CHARS.** The
   consolidated report's 240-char annotation bound cut the darwin
   walk's typed evidence off exactly where it starts; five repair
   sessions guessed because the WHY never survived. `consolidate-run-
   log.sh` now bounds walk-diag lines at 1100 chars. NO speculative
   darwin code change was made in this pass — the next hosted run
   delivers the stage evidence (`pids= self= self_sockets= walked=
   denied=` counts) that decides the fix. The failing family: the
   live-port attribution tests + `TestAdoptionAcceptsManagedEngine`
   (same identity proof); free-port/wrong-port/record-decode tests
   PASS, so enumeration liveness, record decode and the matcher are
   all proven on the runner — the evidence will name the remaining
   stage.
5. **LOG ARTIFACT — PLAIN TEXT.** The errors/warnings artifact uploads
   with `archive: false` (the codeql.yml precedent): the run-page
   download IS the .txt, never a txt-in-zip.
6. **SHELL CSS — ONE FILE PER ELEMENT.** `app.css` is the ENTRY only
   (comments + ordered `@import` cascade of `assets/css/{tokens,base,
   layout,controls,rail,views,chat,panels,loading,system,adaptive,
   motion}.css`). index.html keeps the single `/app.css` reference;
   the shell asset contract test assembles the served bundle, pins the
   entry shape and scans every element file for remote-origin
   literals. `AssetNames()` walks the whole tree. Tokens moved
   byte-for-byte (zero design drift).
7. **The webshell asset contract is otherwise intact**: registry mirror
   pinned to ui/gogui, `data-view` sections, ARIA download bar,
   adaptive CSS band strings, touch floor, `prefers-reduced-motion`
   (the last cascade layer — motion.css kills every animation), and
   the same-origin-only CSP.

Local evidence: gofmt repo-wide clean; headless suite (all internal
packages) green; releasecontract (file catalog regenerated for the 12
new CSS files) green; smokeverdict + desktop + webshell + webworkspace
+ proc suites green; collector fixtures 40/0; `node --check app.js`;
YAML-valid workflows; bash-syntax lane scripts; `CGO_ENABLED=0 go vet
-tags webworkspace,purego ./internal/desktop/ ./internal/webworkspace/
./internal/webshell/` on linux and windows/amd64. NOT RUN locally:
native darwin CGO compile/tests, the GTK3/WebKit2GTK linux desktop
link, Windows pwsh fixture execution (no pwsh in this sandbox — the
hosted windows lane is the authority for ImportAudit.Tests.ps1 incl.
the new fixture 7b), the hosted platform build + CodeQL on the
candidate.

Next actionable step: push the candidate branch (operator
authorization required) and let the hosted platform build + CodeQL run
on it. The run ALSO delivers the first complete walk-diag evidence for
the darwin attribution family — if a darwin lane still fails, the
failure message now carries the WHY (fix the named stage; do not
guess), and if all lanes go green the STEM designation becomes
justifiable for the delivery naming.

---

## Historical context (still true)

- The 45634345922 flip made the web shell the product UI on every
  packaged desktop (`-tags webworkspace` everywhere, `SHEYTAN_UI=fyne`
  retired typed, ui/fyne deleted, client cluster moved verbatim to
  `internal/coreclient`, artifacts upload unpacked portable trees,
  gen-syso writes the .syso into the app package directory).
- `ui/gogui` survives as the ANDROID mobile shell (cmd/sheytan-mobile)
  and the native IA mirror the webshell contract test pins against —
  it is NOT dead code; desktops never boot it (SHEYTAN_UI=auto
  resolves to the web host).
- The 45716559459 pass fixed the darwin cgo type contract
  (`count := int(n)`), the linux `gtk3` backend tag, the stale windows
  gate path (`internal/coreclient`) and the nested android APK
  directory; its shell upgrades (splash, skeletons, Multi-Agent
  surface, live model load options, GPU & drivers card,
  Permissions-Policy/COOP/CORP hardening) are all on main via 2d23a59.
- CodeQL #366 (unvalidated dynamic method call, app.js loaders table)
  and #367 (unused variable) are closed at the root on main: the
  loader table is a `Map` with a typed `loaders.get(id)` call.
- The darwin walk has TWO enumeration sources (proc_listallpids, then
  BSD sysctl KERN_PROC_ALL), fails typed only when both refuse, and
  front-loads the walk-diag inside the evidence window. Do NOT re-apply
  errno-only patches.
- The stress gate follows the workflow forward: accepted SHAs track the
  dependabot pins; the Linux dependency contract is the web-shell era
  (GTK3 + WebKit2GTK-4.1 REQUIRED; GTK4/WebKitGTK6 and Qt banned).
- LESSON (45716559459): `CGO_ENABLED=0 go vet` on darwin EXCLUDES every
  cgo file — the hosted darwin lanes are the only compile authority for
  `identity_darwin.go`; review C-int/Go-int mixing line-by-line.
- LESSON (46050871166, this sandbox): after writing files via tools,
  RE-RUN the touched suite before trusting a pass — one stale-compile
  artifact made a byte-identical needle check fail until the test file
  itself was content-changed; gofmt -w everything edited.
