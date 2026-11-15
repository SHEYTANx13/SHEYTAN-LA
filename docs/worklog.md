# Worklog

Short factual chronology of notable engineering events that
[`changelog.md`](changelog.md) does not already capture. One line per
event: date, work item, outcome, evidence reference, remaining follow-up.

Architecture truth → [`ARCHITECTURE.md`](ARCHITECTURE.md). Release
history → [`changelog.md`](changelog.md). Direction →
[`ROADMAP.md`](ROADMAP.md). Current work order →
[`Dev-Notes.md`](Dev-Notes.md).

---

| Date | Event | Outcome | Evidence |
|------|-------|---------|----------|
| 2026-11-15 | Run-43630164424 CI-resilience repair candidate assembled on local branch `maintenance/v1.11.1-ci-resilience` (baseline commit `2bc66e5`) | Unused-`sh` vet defect fixed as a real chrome-coverage assertion; rail-fit probe class defect and WS caller-cancel race fixed; build-workflow DAG made independent (no `needs:[preflight]` on the four gates, new strict `final-verdict` job, `!cancelled()` collector + `run-diagnostics.json`); CodeQL analyze survives manual-build failure (`upload: always`) with a red-run `collect-codeql-logs` job; DAG/CodeQL contract tests wired into the stress gate; cdproto → v0.160.3. Local gates green (gofmt/vet/build, headless 68 pkgs, desktop sweep 72 pkgs, race 7/7, stress 47/47, collector fixtures 25/25, mod verify + tidy idempotent); hosted NOT RUN — no push authorized | `note.md` beside the delivery ZIP |
| 2026-11-14 | Run-43601915826 repair candidate assembled on local branch `maintenance-v1.11.1` (baseline commit `cbb07af`) | The three red-run root causes fixed (busy-pipe fixture deadlock, logging rotation-test handle leak, stale `GO_VERSION: "1.27"` gate literal); button-fit rail repair + orphaned Models controls wired; chromedp v0.23.1/cdproto v0.160.2 migration compile-verified; dependency-integrity + blocked-by-upstream CI gates added. Local gates green (gofmt/vet/headless suite/stress 47/47, module verify + tidy idempotent); hosted NOT RUN — no push authorized | `note.md` beside the delivery ZIP |
| 2026-11-14 | v1.11.1 Windows IPC maintenance candidate assembled (earlier pass of the same branch) | Local verification green (build, vet, gofmt, headless core suite, Fyne suite incl. button-fit matrix, localipc + logging suites); hosted NOT RUN | `note.md` beside the delivery ZIP |
| 2026-11-13 | Run 43455711025 green on main — the last baseline before the maintenance candidate | Window-only Windows smoke passed while the production client could still not dial the named pipe; the gap motivated the IPC gate rework | Actions run 43455711025; supplied app(3).log |
| 2026-11-13 | Run 43008361835 remediation landed on main (collector self-accounting, startup telemetry, shutdown coordinator, responsive contract) | Green baseline for the v1.11.1 maintenance line | changelog 1.11.1 maintenance entry |
| 2026-10-26 | One-Go-UI documentation overhaul | Product docs matched the Fyne reality | git history |

No speculative future to-do list here — open work lives in
[`Dev-Notes.md`](Dev-Notes.md).
