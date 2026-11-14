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
| 2026-11-14 | v1.11.1 Windows IPC maintenance candidate assembled on local branch `maintenance/v1.11.1-windows-ipc` (5 commits on `c8fc15e`) | Local verification green (build, vet, gofmt, headless core suite, Fyne suite incl. button-fit matrix, localipc + logging suites; Windows tree cross-compiled with mingw); hosted NOT RUN — no push authorized | `note.md` beside the delivery ZIP |
| 2026-11-13 | Run 43455711025 green on main — the last baseline before the maintenance candidate | Window-only Windows smoke passed while the production client could still not dial the named pipe; the gap motivated the IPC gate rework | Actions run 43455711025; supplied app(3).log |
| 2026-11-13 | Run 43008361835 remediation landed on main (collector self-accounting, startup telemetry, shutdown coordinator, responsive contract) | Green baseline for the v1.11.1 maintenance line | changelog 1.11.1 maintenance entry |
| 2026-10-26 | One-Go-UI documentation overhaul | Product docs matched the Fyne reality | git history |

No speculative future to-do list here — open work lives in
[`Dev-Notes.md`](Dev-Notes.md).
