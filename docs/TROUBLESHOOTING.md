# Troubleshooting

For desktop users and support. Each entry: symptom → likely cause → safe
diagnostic → recovery. Every path, setting and behavior below is
confirmed by the source tree; nothing asks you to expose keys, tokens or
private logs publicly.

Audience: users first, maintainers second. Architecture context:
[`ARCHITECTURE.md`](ARCHITECTURE.md). Security reporting:
[`../SECURITY.md`](../SECURITY.md).

---

## Where the logs live

| Install type | Location |
|---|---|
| Portable / default | `<application folder>\data\logs\` (Windows) or `data/logs/` beside the executable (Linux) |
| Fallback (only when that folder could not be opened) | the per-user cache directory: `%LocalAppData%\SHEYTAN-LA\logs` (Windows) or `~/.cache/SHEYTAN-LA/logs` (Linux/macOS) |

- `app.log` — readable app/core/session/IPC/lifecycle log (rotated).
- `tools.jsonl`, `llm.jsonl` — structured tool/LLM records (redacted,
  bounded). Empty until real calls occur — that is normal.
- `crashes/` — bounded panic reports.

The app itself tells you the resolved directory: the Logs page shows
"Log folder: …" (with a FALLBACK/sink-problem note when applicable), the
first lines after every session banner name the directory and active
sinks, and the same caption appears in Settings → diagnostics.

---

## The application starts but every tab shows errors / stays empty

**Likely cause** — the UI cannot reach the local core over the local IPC
channel (named pipe on Windows, Unix socket on Linux). Since every tab
shares one client, they fail together.

**Diagnostic** — open the **Logs** page: a broken connection shows a
typed error line ("logs unavailable — the core connection is broken or
the request failed: …"), never "(no entries match)". Check
`data\logs\app.log` for the IPC records:

- `core handshake successful protocol=sheytan-ipc protocolVersion=1
  coreVersion=… transport=windows-named-pipe` (or `unix-domain-socket`)
  → the transport is healthy; the fault is elsewhere.
- `core handshake failed transport=… error=…` → the transport itself
  refused the connection.

**Recovery** — the status bar shows **Disconnected** with the actionable
error and a **Retry** button (a bounded, user-initiated retry — clicking
it twice cannot stack attempts). If Retry keeps failing: start the app
from a normal shortcut (not a copied/renamed executable in an odd
location), check that no second SHEYTAN-LA instance from a different
folder is running (each per-user endpoint belongs to one install), and
reboot if a previous instance crashed mid-shutdown — the endpoint is
per-user and vanishes with its process, so stale endpoints do not
persist across reboots.

## "Not connected" with a socket/pipe error right after an update

**Likely cause** — the application folder changed (new directory = new
data root on first run) or antivirus/endpoint security briefly blocked
the new executable from opening its per-user pipe.

**Diagnostic** — same as above; the error text names the transport
failure.

**Recovery** — Retry once; if it persists, allow the new executable in
your security software and start the app again. There is no port to
open: the desktop never uses TCP.

## The Logs page shows "FALLBACK ACTIVE" or "sink problem"

**Likely cause** — the configured `data\logs` folder could not be opened
(permissions, disk full, the folder was made read-only) and the log
catcher moved to the per-user fallback, or a write failed mid-session.

**Diagnostic** — the Logs page caption names the active folder and the
problem; `app.log` (in whichever location is active) starts with the
"primary log directory unavailable" record.

**Recovery** — restore write permission to the application's `data`
folder (or move the portable install to a writable location) and
restart. Log events made while a sink was broken are preserved in
memory and surfaced — they are not silently lost — but they do not
retroactively appear in a file.

## Updater / "Check for updates" fails with a network error

**Likely cause** — outbound network access is denied (firewall, offline
machine, DNS policy). The app logs a warning like "connectex: An attempt
was made to access a socket in a way forbidden by its access
permissions".

**This is non-fatal and expected offline.** The application remains
fully usable; engine updates simply cannot be checked or installed
until network access exists. There is a clear retry path (Check now
again). Local IPC is unaffected — a network failure never breaks the
core connection.

## The window opens but closes immediately / exits with an error box

**Diagnostic** — the startup failure dialog names the failed step and
the log path; `app.log` carries `boot failed at <step>: <error>`.

**Recovery** — address the named step (most common: a locked or
read-only `data` folder, or a corrupted config.json — rename it to
`config.json.bak` and let the app recreate defaults; your sessions and
models are untouched).

## An engine update was installed but the engine did not come back

**Likely cause** — the transactional updater rolled the engine back
after verification failed (this is the safety design: the previous
package is restored byte-for-byte).

**Diagnostic** — `app.log` records the engine-update transaction phases
and any rollback; the Models page engine card shows the truthful state.

**Recovery** — retry the install (Check now → Install update); if it
repeatedly rolls back, the release asset or local disk is the problem —
the log names which.

## Buttons or text look cut off at very small window sizes

The supported minimum logical width is 320 (phone-class portrait); at
that width and above, every action is reachable and no label clips (a
matrix test enforces this in CI). Below ~240 logical units the app
remains stable but is not laid out for usability — resize wider. On
very high-DPI displays the OS scale factor drives logical size; if
everything looks tiny and cramped, raise the display scale in your OS
settings.

## Collecting evidence for a bug report

Use Settings → diagnostics (redacted export) or attach `data\logs\`
(app.log + the two .jsonl files) from the ACTIVE log location shown on
the Logs page. Do not paste your `config.json` publicly — it can carry
your remote provider API key (the configuration API redacts it, but the
file on disk does not). Never share the IPC key: it is ephemeral,
per-launch, and never appears in logs by design.

---

*Maintainers: when adding a user-visible failure mode, add its entry
here in the same change — the symptom must be one a user can actually
observe, and every path must exist in the source tree.*
