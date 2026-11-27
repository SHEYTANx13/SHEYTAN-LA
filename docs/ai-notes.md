# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 45716559459 repair: the five lane failures rooted and fixed, CodeQL #366/#367 closed, the shell loading experience and system/model surfaces upgraded (2026-11-27) — supersedes all handoffs below

Read this BEFORE touching the lane scripts, the darwin walk or the web
shell again. The decisive facts of this update:

1. **DARWIN LANES (both macos) — cgo TYPE CONTRACT.** The sysctl
   enumeration source added to `internal/proc/identity_darwin.go`
   compared the raw `C.int` answer against Go ints (`n > capPIDs`,
   `i < n`) — cgo types are DISTINCT Go types; the comparisons do not
   compile. Fixed with one explicit conversion (`count := int(n)`).
   LESSON: `CGO_ENABLED=0 go vet` on darwin EXCLUDES every cgo file —
   it never type-checked identity_darwin.go at all. Any future darwin
   cgo change must be reviewed line-by-line for C-int/Go-int mixing
   (the hosted darwin lanes are the only true compile authority).
2. **LINUX LANE — WAILS BACKEND TAG.** wails v3.1.0's DEFAULT linux cgo
   backend compiles against `gtk4 webkitgtk-6.0`
   (`pkg/application/linux_cgo.go`); the GTK3/WebKit2GTK-4.1 backend —
   the stack the dependency contract REQUIRES and `prereqs` installs —
   is opt-in through the upstream `gtk3` tag
   (`pkg/application/linux_cgo_gtk3.go`). The linux desktop build is
   now `-tags nogogpu,webworkspace,gtk3`. NEVER drop the `gtk3` tag on
   linux: without it the build fails at pkg-config (`gtk4 not found`),
   and GTK4/WebKitGTK6 stay banned by the release-surface contract.
3. **WINDOWS LANE — STALE GATE PATH.** windows.ps1's ipc-integration
   phase still ran `go test ./ui/fyne/` — deleted by the 45634345922
   flip. It now runs the production CoreClient integration suite from
   `internal/coreclient/` (the one tracked home).
4. **ANDROID LANE — PACKAGE DIRECTORY.** The canonical APK path is
   nested (`dist/SHEYTAN-LA-ALPHA-android-arm64/
   SHEYTAN-LA-ALPHA-android-arm64.apk`); `gomobile build -o` does not
   mkdir. The package phase creates the directory and the build.yml
   package gate inspects the SAME nested path (the gate still pointed
   at the retired flat path — fixed in the same change).
5. **CODEQL #366 (High) / #367 (Note) — CLOSED AT THE ROOT.** The shell
   route loader table is a `Map` and `show()` calls `loaders.get(id)` —
   a typed method call, never a dynamic property access with a
   route-derived key. The unused `badgeEl` in `loadModels` is now used:
   the models backend lands in the conversation badge, refreshed in
   place (the old code ALSO appended `backend …` onto the shared status
   line every 20 s poll — a duplicate pile-up bug that is gone).
6. **SHELL UPGRADES — every control maps to a REAL endpoint.** Boot
   splash (staged status, dismissed by the FIRST healthy `/api/health`
   answer + a 6 s fail-open timeout — never a trap); skeleton shimmer on
   first paint only (sessions/goals/models); view-entry fade; in-flight
   `aria-busy` button states. Models carries LIVE load options
   (`llm.numThread`, `llm.numGpu`, `llm.flashAttention` through
   `mergeConfigPatch`'s llm deep-merge; `gpuAutoOffload` marks the
   explicit-user posture). System carries a "GPU & drivers" card from
   the REAL hardware probe (`gpus[].vendor/name/driverVer/vramBytes`)
   with the DETECTED → AVAILABLE → SELECTED → EXECUTED → VERIFIED
   honesty contract stated; OS driver installation is NEVER performed by
   the app — engine/driver update checks run against official upstream
   sources on the Engines surface. Security hardening beyond the
   same-origin CSP: `Permissions-Policy` (no powerful features granted),
   `Cross-Origin-Opener-Policy: same-origin`,
   `Cross-Origin-Resource-Policy: same-origin`. The webshell serving
   contract tests still pin the four original headers; the new three are
   additive.
7. **The webshell asset contract is intact**: registry mirror pinned to
   ui/gogui, `data-view` sections, ARIA download bar, adaptive CSS band
   strings, touch floor, `prefers-reduced-motion` (which also kills all
   new animation), and the same-origin-only CSP scan (zero remote-origin
   literals in the assets).

Local evidence (see the run note for the exact commands): gofmt clean;
headless suite green; releasecontract/webshell/webworkspace/coreclient
green; stress green; cross vets green; `node --check app.js` green;
build.yml YAML-valid; lane scripts bash-syntax-valid. NOT RUN locally:
the GTK3 cgo desktop link (sandbox has no webkit dev libraries), native
webview window launch, native darwin CGO compile, windows PE inspection,
android NDK build — the hosted lanes remain the authority.

Next actionable step: push the candidate branch (operator authorization
required) and let the hosted platform build + CodeQL run on it; the
web-shell window launch evidence and the Windows PE icon check ride
that same run.

---

## Historical context (still true)

- The 45634345922 flip made the web shell the product UI on every
  packaged desktop (`-tags webworkspace` everywhere, `SHEYTAN_UI=fyne`
  retired typed, ui/fyne deleted, client cluster moved verbatim to
  `internal/coreclient`, artifacts upload unpacked portable trees,
  gen-syso writes the .syso into the app package directory).
- The darwin walk has TWO enumeration sources (proc_listallpids, then
  BSD sysctl KERN_PROC_ALL), fails typed only when both refuse, and
  front-loads the walk-diag inside the ~110-char evidence truncation
  window. Do NOT re-apply errno-only patches.
- The stress gate follows the workflow forward: accepted SHAs track the
  dependabot pins; the Linux dependency contract is the web-shell era
  (GTK3 + WebKit2GTK-4.1 REQUIRED; GTK4/WebKitGTK6 and Qt banned).
