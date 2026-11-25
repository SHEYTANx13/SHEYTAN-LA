# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## CURRENT HANDOFF — run 45634345922 repair: the UI flip completed, ui/fyne deleted, packaging un-nested, the icon pipeline fixed, the darwin walk enumeration-redundant (2026-11-25) — supersedes all handoffs below

Read this BEFORE touching the darwin walk, the lane scripts or the UI
surface again. The decisive facts of this update:

1. **THE FLIP IS DONE — no more gating.** The operator's Windows test
   proved the previous session's design shipped nothing: no lane built
   `-tags webworkspace`, so every package carried the Fyne shell. Now
   every desktop lane compiles the tag (windows.ps1, linux.sh
   `nogogpu,webworkspace`, macos.sh), the Linux `prereqs` phase
   installs `libgtk-3-dev libwebkit2gtk-4.1-dev libglib2.0-dev`, and
   `SHEYTAN_UI=auto` boots the web shell everywhere. `SHEYTAN_UI=fyne`
   answers the typed `ErrFyneRetired` — never a silent fallback.
2. **ui/fyne IS DELETED, not gated.** The web shell (internal/webshell)
   carries the full IA with REAL API-backed views for all 13
   workspaces; the fyne-free client cluster (CoreClient, Runtime,
   logic, settings schema, the IPC integration gates) moved verbatim to
   **internal/coreclient** — the windows named-pipe integration gate
   runs there now. The webshell registry is CANONICAL; ui/gogui is the
   native mirror and the IA contract test pins both. Fyne left go.mod;
   LICENSE.md was regenerated from the brand authority after the
   surface-table row changed.
3. **PACKAGING: the artifacts upload the UNPACKED portable trees**
   (`dist/SHEYTAN-LA-ALPHA-*/` — one download layer, no zip inside a
   zip); the sealed archives stay in dist/ for the gates and the
   tag-only release job re-seals them deterministically before
   publishing. The collector report is ALSO rendered into the run
   summary (256 KiB bound). **ICON: gen-syso wrote the .syso into the
   repo ROOT while Go links .syso only from the built package's
   directory — the exe shipped with the generic Go icon, no version, no
   manifest. It now writes `cmd/sheytan-local-agent/
   rsrc_windows_amd64.syso`**; the web-shell window additionally
   carries the brand mark through the host's LinuxWindow.Icon and the
   shell's /logo.png favicon.
4. **DARWIN (honest boundary: still compile-level only).** Three runs
   failed exactly the tests whose flow reaches the walk on a live port,
   while record-level and metadata probes stayed green — the only
   whole-walk-fatal stage is PROC_ALL_PIDS enumeration. The walk now
   has TWO enumeration sources (proc_listallpids, then BSD sysctl
   KERN_PROC_ALL — a different kernel entry point; kinfo_proc stays in
   C), fails typed only when both refuse, front-loads the walk-diag in
   both error messages (the consolidated channel's ~110-char window now
   carries the class), and maps raw errnos outside the gone/denied/
   ENOENT classes to the typed metadata-unsupported class (errno text
   kept in the message; consumers were already fail-closed there).
   Do NOT re-apply errno-only patches. The hosted darwin lanes remain
   the runtime authority — if a failure survives, its annotation now
   names the exact stage within the truncation window.
5. **The stress gate follows the workflow forward** (its own rule): the
   linux failure of run 45634345922 was the gate rejecting the CORRECT
   dependabot pins (setup-go v7.1.0, msys2 v2.34.0 SHAs). Accepted
   refs moved; the Linux dependency contract is now the web-shell era
   (GTK3 + WebKit2GTK-4.1 REQUIRED, third-generation GTK4/WebKitGTK6
   and Qt still banned; the second-generation ban is lifted because
   WebKit2GTK-4.1 IS the required backend). UI-suite pin: `go test ./ui/`.

Local evidence: headless suite 77 pkgs green; releasecontract,
webshell, webworkspace, coreclient green; stress 47/47 green;
cross vets (darwin arm64/amd64, windows) green; the flip compiles
CGO-free (`-tags webworkspace,purego`) on linux/darwin/windows.
NOT RUN locally: native webview window launch, native darwin CGO
suite, Windows PE/icon inspection, macOS code-sign/smoke — the hosted
lanes are the authority.

Next actionable step: push the candidate branch (operator
authorization required) and let the hosted platform build + CodeQL run
on it; the web-shell window launch evidence and the Windows PE icon
check ride that same run.
