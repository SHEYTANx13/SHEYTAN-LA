# Runtime Diagnostics — Observability, Health and Support Bundles

> What the app measures, what it logs, what it refuses to log, and how a
> user exports safe evidence. Verified against the repository 2026-11-16.
> CI-side diagnostics (per-run gate index, consolidated logs) live in
> `scripts/ci/diagnostics-index.sh` and `scripts/ci/consolidate-run-log.sh`.

## 1. Log sinks (one manager, `internal/logging`)

| Sink | Content | Rotation |
|---|---|---|
| `logs/app.log` | human-readable app events | rotated at 2 MB, 5 files kept |
| `logs/llm.jsonl` | structured LLM/provider events | bounded, JSONL |
| `logs/tools.jsonl` | structured tool events | bounded, JSONL |

Guarantees (test-pinned): append-only records; rotation never erases
history beyond the bound; sink health is itself reported (bounded
emergency ring, rate-limited stderr fallback, rollback of partial opens,
rotation/write/close failure records); the startup banner names the
resolved directory and active sinks. Opaque run/session identifiers are
redacted per the human-facing rule (`redact.go`) — never keys, tokens,
prompt bodies, response bodies or raw environment.

## 2. Startup phase telemetry (`internal/startup`)

One monotonic recorder measures every bootstrap phase — config, runtime
data migrations, logging, core services, IPC, Fyne app construction,
shell, first view, first interactive, handshake, sessions, background
readiness, shutdown — with sanitized stable fields, landing on stderr
even when no data root exists. `SHEYTAN_STARTUP_DIAGNOSTICS=1` adds JSON
records for CI capture. Honest behavior: the UI never waits for optional
provider/model discovery; readiness is reported only after required local
persistence and core services are usable.

## 3. Health-state distinctions (binding vocabulary)

- **Hardware/inference truth ladder**: detected ≠ available ≠ selected ≠
  executed ≠ verified (`internal/accelerator` execution-truth contract,
  `internal/llm` devices/admission). A detected GPU is never reported as
  used; an explicit GPU selection that falls back reports degraded, not
  ready.
- **Engine states** are real backend states (idle/failed/busy/…) —
  fabricating "ready" is a tested failure (`api` engine endpoint tests).
- **IPC connection surface**: Connecting / Connected / Disconnected with
  the actionable typed error and a single-flight retry (`ui-contract.md`
  item 14).

## 4. Support bundle (`sheytan diagnostics`)

`cmd/diagnostics.go` exports `<data>/diagnostics/sheytan-diagnostics.zip`
containing logs, stats, crash records, a sysinfo snapshot and the
REDACTED config — generated only on explicit user action, never
auto-uploaded. Excluded by contract: prompts, response bodies, API keys,
tokens, cookies, auth headers, raw environment, private files, model
payloads. Redaction is deterministic and test-covered
(`internal/logging/redact_test.go`, fault-injection suite,
diagnostics-index self-test: JSON schema, gate statuses, log inventory
and secret redaction — green 2026-11-16).

## 5. Failure behavior

Tool/provider/engine failures surface a stable error class with
actionable context, not a raw stack; panics are recovered only at
top-level task boundaries with redacted stacks; cancellation is distinct
from failure; a diagnostics failure never crashes the app; a task is
never reported complete before its result/artifact is persisted and
verified (run-settlement tests).

## 6. What was verified in the v1.11.1 integrity pass (2026-11-16)

- `bash scripts/ci/diagnostics-index.sh --self-test` — PASS (schema,
  gate states, log inventory, secret redaction).
- `bash scripts/ci/consolidate-run-log-fixtures.sh` — 25/25 PASS.
- `go test ./internal/logging/ ./internal/startup/` — PASS (headless
  gates).
- Stress suite — 47/47 PASS (`go run -tags headless
  ./cmd/sheytan-local-agent stress`).

NOT RUN in this pass: physical GPU/driver enumeration reporting on real
hardware, UI-frame percentile capture, assistive-technology behavior —
hosted/physical classes remain their authority.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
