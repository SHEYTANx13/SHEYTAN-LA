# Changelog

**This is the only canonical release-history artifact.**  
The README describes the current product; per-release evidence lives in the release tags and the tests that shipped with them.

Every release ships with the same invariants: one authority per concern, evidence over confidence, no sleep-based correctness, honest hardware claims, codename gate enabled.

---

## 1.11.1 (maintenance pass) — 2026-11-13

**Run 43008361835 (run #144) remediation: collector self-job reconciliation, startup phase telemetry, one shutdown coordinator, responsive size-class contract, CI cache scoping, per-job diagnostics index**

No version bump — v1.11.1 remains the product version; this is a reliability/UX maintenance pass, not a feature release.

- Consolidated-log collector root-cause fix: the collector used to count its OWN in-progress job as a plain coverage gap, so the "Collector verdict" step failed every green run (run #144's five build/test/package jobs all passed; the run's final `failure` conclusion came solely from that verdict step). The collector's own job is now accounted as a separate, expected SELF category (identified via `GITHUB_JOB`/`CONSOLIDATE_SELF_JOB_NAME` with normalized name matching), reported honestly in the artifact, and excluded from the fail-closed verdict. A NON-self in-progress job still fails closed. Pinned by two new collector fixtures (production run-#144 shape → rc=0; non-self in-progress → rc=1) plus a workflow-shape assertion that the consolidate-logs display name keeps normalizing to its YAML key.
- Startup phase telemetry (internal/startup): one process-wide monotonic recorder measures config load, data migrations, logging init, core init/setup, IPC key/serve, Fyne app, shell build, first view, window-ready (elapsed-to-first-interactive), core handshake, sessions refresh, background readiness and shutdown. Sanitized stable fields only (phase, duration_ms, success, error_class, platform, build_version, cold_or_warm) — no paths, prompts or secrets; the summary goes to stderr at the end of every desktop launch so a pristine smoke copy with no data root still leaves a complete timing record. Controlled diagnostic mode (`SHEYTAN_STARTUP_DIAGNOSTICS=1`) adds the full JSON records to stdout for CI capture; wired into both launch smokes.
- One idempotent shutdown coordinator (ui/fyne/lifecycle.go): every supported exit path — user window close (SetCloseIntercept), SIGINT/SIGTERM, programmatic Close() — funnels through a single running→closing→closed state machine with an atomic CAS, so repeated and racing closes share ONE bounded teardown (root-context cancel, activity-pump drain with deadline, workspace listener release) before the window actually closes. A deadline-exceeded teardown is recorded as INCOMPLETE — never graceful. Pinned by racing-close, active-pump, deadline and pre-Run safety tests.
- Responsive size-class contract (F4): the shell's breakpoints moved to the documented contract — compact <600 (top strip + More menu, one content column), medium 600–899 (compact rail), desktop 900–1279 / wide ≥1280 (persistent wide rail; no inspector surface exists in v1.11.1, documented) — through ONE `sizeClassFor` policy function that `navPolicy` derives from. The full mandatory logical-size matrix (phone portrait/landscape, tablets, laptops, desktops, wide displays, every breakpoint boundary pair at three heights, degenerate 0x0/1x1/tiny sizes) is asserted against the REAL assembled shell in a real window with rendered-geometry checks; five representative layouts are pinned by reviewable structural goldens (font-independent; pixel goldens are unreachable through the public API of the locked Fyne v2.8.2 because the software painter is internal). A real-window resize smoke (`SHEYTAN_UI_RESIZE_SMOKE=1`) walks the matrix under Xvfb in CI and closes through the same graceful coordinator.
- CI cache scoping (F1/F7): every setup-go declares `cache-dependency-path: go.sum` (key family scoped to OS + Go version + dependency lock); each job records its cache hit/miss and toolchain facts into ci-logs (evidence, never a gate). The Windows cold-module-fetch penalty of run #144 (cache saved at job end only) is expected to resolve as a primary-key hit on the next identical run — verifiable from the recorded cache-hit output.
- Per-job diagnostics index (F6): `scripts/ci/diagnostics-index.sh` (pure bash, runs on Linux and the Windows Git Bash lane) consolidates each job's gate sequence and ci-logs inventory into `ci-logs/diagnostics-index.json` with an always-run step that never masks the original job conclusion; the builder is self-tested in the audit job (schema, gate statuses, log inventory, seeded-token redaction).
- fyneDo migration audit (F5): every background-to-UI mutation already marshals through the `uiDo`/fyne.Do seam. The `migrated_fynedo` validation build currently fails in the UI test harness (concurrent-map fatal during the models-panel suite), so the opt-in is correctly NOT enabled and no FyneApp.toml was invented; recorded as a follow-up.

---

## 1.11.1 — 2026-11-03

**Run #41415240891 remediation: true version lineage + read-only version dispatch + owned test ports + Model Studio per-system binding**

- Version lineage repaired (Failure C): internal/config.AppVersion is 1.11.1; build/config.yml productVersion, SIGNATURE (regenerated by scripts/gen-signature.go), workflow APP_VERSION declarations, printversion contracts and the fail-closed release-contract suite all agree. The commit title had said v1.11.1 while every built surface still reported 1.11.0.
- Read-only version dispatch (Failure A): version/-v/--version answer before config load, path migration, dir creation and the file log sinks — the CI version probe can no longer ship runtime state inside the release ZIP. The hosted Linux probe additionally runs from an isolated directory with a zero-residue assertion. Proven by the new cmd purity suite and a compiled-binary probe with an untouched directory.
- Owned test ports (Failure B): the freePort bind-close-return TOCTOU is retired. Fake-engine fixtures now hold an owned reservation through staging (reservePort/reReservePort), revalidate every handoff with an actionable fixture failure, and the adoption fixture's child selects and reports its own ephemeral port. Package-parallel CI stays enabled; no sleeps, no -p 1.
- Model Studio (§5.2): the frozen per-run AI System snapshot now carries the model override, runtime profile and context/memory policy preferences; the store validates the model override (bare name, no paths) and the runtime profile against the ONE recommendation task vocabulary; the Models panel gained the per-system binding card (inherit-first, one Apply action, full-record PUT that cannot wipe fields edited elsewhere).
- Historical references to 1.11.0 are history; no tag, no release, no push.
- Windows path-domain repair (run 42909094540): TaskRegistry.Create's version-archive branch sliced a NATIVE relative path (filepath.Rel output, backslash-separated on Windows) with a hard-coded "/" lookup — on Windows the lookup returned -1 and the unguarded slice panicked with "slice bounds out of range [:-1]" on every artifact REPLACEMENT write (two failed Windows test packages; EOF from the automation-artifacts API; the missing Windows packages were downstream). The first path component is now derived separator-agnostically and proven equal to the grammar-gated task id before any slicing; archive paths are built with filepath.Join (native separators); Windows-hostile logical spellings (drive-qualified/drive-relative, ADS colons, UNC, device prefixes, reserved device basenames, trailing dot/space components) are rejected in the logical domain before the native conversion while POSIX literal-name behavior is unchanged. New winmatrix path-safety suite (valid nested/multi-dot/backslash-spelled destinations, hostile-spelling rejection with outside-sentinel assertions, symlink-escape refusal through the rooted sink, drive-absolute-root round trip).
- Native parser maintainability (CodeQL security-and-quality): the one-real-case \u escape switch in detail::Scanner is a direct branch with the Unicode-escape logic extracted into read_unicode_escape() (documented contract; cursor/hex/surrogate/UTF-8 behavior byte-identical); the GGUF kTypeArray case is a short delegation to read_array_value() (documented contract; element walk, bounds, overflow guard and error outcomes unchanged); the decode/generate payload extractors carry phase and invariant documentation with the ids-array walk extracted into read_ids_array() (exact historical error strings preserved). All 12 native engine test binaries pass.

---

## 1.11.0 — 2026-10-26

**One-Go-UI (Fyne) complete + hybrid Web Workspace + CI hardening + optional 3D Studio**

- Full migration to single Go + Fyne desktop (Qt/QML and React/Vite fully retired).
- Responsive three-zone shell (wide / medium / narrow).
- Optional 3D Studio: read-only live topology viewer (loopback-only, per-launch token, strict CSP, embedded Three.js).
- Hybrid presentation architecture: versioned block contract (`internal/presentation`) + on-demand browser-hosted Web Workspace (`internal/workspace`) — read-only rich viewer with the Studio's security boundary; native shell always renders first; unknown kinds degrade safely.
- Typed accelerator resolution causes: machine-readable `reasonCode` on every resolution, never upgrading the evidence state.
- Retirement scan completed: zero references to the first-generation webview desktop shell remain in the live tree (including its page-origin WebSocket exception, removed from the activity transport); a release-contract gate keeps the tree clean.
- Consolidated one-file CI run-log artifact on every run.
- Windows pipeline migrated to MSYS2 UCRT64; launch smoke hardened with dual window-observation paths and captured application output for diagnosable red runs.
- Stress gate made headless-safe; CGO-free version resolver (`cmd/printversion`).
- Launch smokes upgraded to bounded observable readiness + clean shutdown.
- Fyne threading contract enforced; zero racy UI writes.

**Regression (local)**: full test suite green, stress 47/47, Xvfb desktop smoke clean. Hosted verification of the final commit pending operator push.

---

## Prior releases (summary)

| Version | Focus |
|---------|-------|
| 1.10.x  | Named-pipe transport hardening, Governor admission, execution/evidence ladder, busy-dial budget, visibility system, packaging contracts |
| 1.9.x   | AI System Builder, Goal engine, approval authority, native-engine forward path, measurement framework |
| 1.8.x   | Runtime Governor, dataAnalysis authority, reasoning-depth budgets, session-list generation guard, zero-session Send |
| ≤ 1.7   | Core runtime, skills, Coding Lab, scheduler, first desktop shells |

Detailed per-release narratives for versions before 1.11.0 are preserved in the git history of this file.
