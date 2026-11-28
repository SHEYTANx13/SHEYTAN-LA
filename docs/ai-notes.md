# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 46131092469 repair: the darwin attribution family closed at the named stage (zeroed pid slots, transient sysctl), the macOS .app icon, the coarse-pointer touch floor, the UI-contract docs consolidation (2026-11-29) — supersedes all handoffs below

Read this BEFORE touching the darwin proc identity, the lane scripts or
the web shell again. The decisive facts of this update:

1. **DARWIN ENUMERATION — A PID 0 SLOT IS A KERNEL ARTIFACT, NOT DECODE
   CORRUPTION.** The run-46050871166 walk-diag channel delivered its
   design goal: the failure finally named its stage
   (`pids=0 … walked=0 … enum_err=truncated`, BOTH sources refused).
   `proc_listallpids` answered a pid 0 slot (indices 449–468 of
   ~450–470 entry tables, varying per test) — a process that exits
   between the kernel's count pass and its copy pass leaves a zeroed
   slot; `sysctl KERN_PROC_ALL` answered ENOENT (the same mid-copy
   mutation). The policy is now `sanitizePIDList`
   (identity_darwin_path.go, pure Go, pinned by
   `TestSanitizePIDListPolicy` in every darwin build): pid 0 slots are
   SKIPPED and counted (`skipped=%d` in the walk-diag line), a NEGATIVE
   pid and a >¼ zeroed majority stay typed decode-corruption failures;
   the sysctl source re-snapshots (same buffer, bounded, NO sleeps) on
   transient ENOENT/ESRCH. `TestAdoptionAcceptsManagedEngine` needs no
   llm-side change — it adopts through `proc.ListeningProcess`
   (adoption.go:82) and died with the same enumeration failure.
2. **CGO DARWIN COMPILE — the hosted lanes stay the authority.**
   `CGO_ENABLED=0` vet on darwin type-checks the pure-Go files and the
   tests, but EXCLUDES every cgo file (the 45716559459 lesson still
   holds): review identity_darwin.go cgo sections line-by-line
   (C-int/Go-int conversions).
3. **MACOS .APP ICON — built at package time.** macos.sh stage-app
   builds `sheytan.icns` from `internal/webshell/assets/logo.png` with
   sips+iconutil (present on every macOS runner image), declares
   `CFBundleIconFile`, and verify-zip fails closed on a bundle missing
   the icon. Do NOT commit image binaries; the icns is never a tracked
   artifact. Windows embeds via the gen-syso pipeline (already wired).
4. **TOUCH FLOOR — coarse pointers get the REAL floor.** The shell
   declared `--touch-floor: 44px` while pointer-era min-heights sat
   under it. Controls now carry `touch-action: manipulation`, visible
   `:active` states, and the `@media (pointer: coarse)` floors
   (controls.css/views.css) raise buttons/inputs/icon buttons/list rows
   to 44px with the 16px input font floor (no WebView focus zoom).
   Never reintroduce hover-only feedback.
5. **UI-CONTRACT DOCS — ONE HOME.** The three `docs/ui/` pages
   (ACCESSIBILITY, DESIGN-SYSTEM, TOUCH-RESPONSIVE-MATRIX) are merged
   into the rewritten web-shell-era `docs/ui-contract.md` and retired
   from the tree; all cross-links point there. Do not recreate
   per-topic UI pages; amend the one contract in the same change as the
   behavior it describes.
6. **WINDOWS LAUNCH SMOKE — TWO SHELL ERAS.** The packaged desktop
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
7. **ANDROID LANE — `go mod download all` MUTATES go.sum.** The
   module preflight runs plain `go mod download` (verified a tree
   no-op); `module-integrity.sh` pins `go mod tidy -diff` empty. NEVER
   re-introduce `go mod download all` in a lane script.
8. **IMPORT AUDIT — @( ) IS MANDATORY.** A module with no static import
   table makes `Get-StaticImportDlls` emit ZERO pipeline objects;
   scalar assignment unwraps that to $null and `.Count` throws under
   `Set-StrictMode -Version Latest`. Any new PowerShell collection
   assignment from a function call needs the same @() guard.
9. **LOG ARTIFACT — PLAIN TEXT.** The errors/warnings artifact uploads
   with `archive: false`: the run-page download IS the .txt.
10. **SHELL CSS — ONE FILE PER ELEMENT.** `app.css` is the ENTRY only
   (comments + ordered `@import` cascade of `assets/css/{tokens,base,
   layout,controls,rail,views,chat,panels,loading,system,adaptive,
   motion}.css`). The shell asset contract test assembles the served
   bundle, pins the entry shape and scans every element file for
   remote-origin literals. Tokens moved byte-for-byte (zero design
   drift); the registry mirror stays pinned to ui/gogui.
11. **CodeQL #366** (unvalidated dynamic method call, app.js loaders
   table) is closed at the root on main: the loader table is a `Map`
   with a typed `loaders.get(id)` call; the alert re-validates closed
   on the hosted scan of the next pushed commit.

Local evidence (this pass, linux/amd64 go1.27.4): gofmt clean;
`GOOS=darwin CGO_ENABLED=0 go vet ./internal/proc/` green (pure-Go
policy + new tests); headless build + full internal suite green;
webshell/webworkspace/desktop/smokeverdict suites green;
releasecontract green with the regenerated file catalog (three docs/ui
pages merged away); `node --check app.js`; `bash -n` lane scripts.
NOT RUN locally: native darwin CGO compile/tests (no macOS SDK), the
real WebView runtimes, sips/iconutil (macOS-only), pwsh fixture
execution (no pwsh), the hosted platform build + CodeQL on the
candidate (no remote operations authorized).

Next actionable step: push the candidate branch (operator
authorization required) and let the hosted platform build + CodeQL run
on it. The darwin lanes decide the enumeration fix; a green run
justifies the STEM delivery naming on that commit.

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
- CodeQL #366 (unvalidated dynamic method call) and #367 (unused
  variable) are closed at the root on main: the loader table is a
  `Map` with a typed `loaders.get(id)` call (see item 11 above).
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
