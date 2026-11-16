# Content Integrity — Mutation, Deletion and Recovery Contract

> Scope: how SHEYTAN-LA creates, edits, deletes, restores and synchronizes
> persistent application content. This is the v1.11.1 integrity-first
> milestone's contract document. Requirement provenance:
> [`REQUIREMENTS-TRACEABILITY.md`](REQUIREMENTS-TRACEABILITY.md) (Stability
> §9, Advanced UI/UX §8/§11). Runtime observability of these paths:
> [`../diagnostics/RUNTIME-DIAGNOSTICS.md`](../diagnostics/RUNTIME-DIAGNOSTICS.md).

---

## 1. Principles (binding)

1. **Destructive operations are never silent and never unrecoverable by
   default.** Deleting user-visible content from the active store is a
   recoverable state change; permanent destruction is a separate,
   explicit, second step.
2. **Exact identity only.** Mutations resolve their target by stable id
   inside the owning store — never by display name, "closest match" or
   model-supplied path. Unknown ids fail honestly; repeat deletes error.
3. **Fail closed.** A hostile id, an occupied destination, a missing
   payload or an unreadable record produces a typed error and leaves
   existing data untouched — never a guess, never an overwrite.
4. **The tombstone is metadata.** Recovery records carry id, title,
   mode, message count, sequence, actor and timestamp — never message
   payloads, prompts or secrets.
5. **Path safety is sink-level.** Every session artifact path is built by
   the id grammar + `os.Root` rooting (`internal/sessions`); the trash
   uses the same root with re-validated relative joins.
6. **A database pointer never lies about bytes.** File-backed records are
   staged, moved, then verified by reading them back before success is
   reported.

## 2. Session deletion lifecycle (IMPLEMENTED_AND_TESTED, v1.11.1)

Code: `internal/sessions/trash.go` (+ `sessions.go` Delete relocation).
API: `internal/api/server.go` (`handleSession`, `handleSessionTrash`,
`handleSessionRestore`, `handleSessionPurge`). Tests:
`internal/sessions/trash_test.go` (9 tests),
`internal/api/sessions_trash_api_test.go` (1 end-to-end test), plus the
pre-existing delete-contract tests in `internal/sessions/delete_pending_test.go`
which still pass unchanged.

| Operation | Semantics | Recoverable |
|---|---|---|
| close/dismiss a view | transient UI state only; touches no store | n/a |
| `DELETE /api/sessions/{id}` (store `Delete`) | moves `<id>.json`, `<id>.activities.jsonl`, `<id>.summary.json` into `trash/<id>/`, writes `tombstone.json`, drops the index/pending/hot entries. A pending draft (never persisted) deletes from memory only and the API reports `recoverable:false` truthfully | yes (until purge) |
| `GET /api/sessions/trash` | lists tombstones, newest first; metadata only | — |
| `POST /api/sessions/{id}/restore` | requires the tombstone; refuses an occupied destination (`ErrRestoreTargetOccupied`, HTTP 409 — never overwrites a live session); moves payload + sidecars back; re-indexes; verifies by re-reading before reporting success | undo of delete |
| `POST /api/sessions/{id}/purge` | the ONLY permanent byte destruction; requires the trash state first (`ErrNotTrashed`, 409 for a live session) and verifies its own effect | no — final |

Crash safety (staged sequence): `mkdir trash/<id>` → rename payload →
rename sidecars → atomic tombstone write → index rewrite. Any interruption
leaves the safe direction: a payload still at its live path is folded back
by the index reconcile; a payload in `trash/<id>/` without a tombstone is
rebuilt by `trashReconcileLocked` (actor `recovery`) on the next
ListDeleted/Restore/Purge — deterministically, additively, without ever
destroying anything. `reconcileLocked` skips directories, so `trash/` is
invisible to the active index by construction.

Delete/restore/purge ids all pass the single-component id grammar
(`validSessionID`) BEFORE any filesystem operation; every file operation
runs through the store-dir `os.Root`.

What is deliberately NOT claimed: no time-based retention, no automatic
trash eviction (nothing is destroyed by policy in v1.11.1 — the safest
default), no cross-store undo for non-session content classes yet (see
§4), no multi-process concurrency contract (the app is single-process by
design; the hot cache detects external writers read-side).

## 3. Mutation invariants across the store (existing, now pinned)

- **Atomic writes everywhere in `internal/sessions`** (tmp + rename) and
  idempotent index maintenance; concurrent `AppendMessage` calls are
  mutex-serialized (pre-existing, regression-tested).
- **Session-list generation guard** (`internal/api`): stale list
  responses can neither resurrect a deleted session nor drop a created
  one (pinned by existing tests).
- **`SaveMessagesKeepContext`**: a caller's stale full-object save can
  never silently revert concurrent context mutations — the context is
  re-fetched under the same store lock.
- **Safe edit for source files** (`internal/lab/safeedit.go`): staged
  edit + verify + repair paths with path-jail containment
  (`internal/lab/pathjail_v1111_test.go`).
- **Staged archive transactions** (`internal/tools/archive_staged_tx_test.go`,
  `archive_zipslip_test.go`): archive creation and extraction are staged,
  zip-slip-refusing operations.
- **Blob/metadata separation** (`internal/artifacts`): artifact bytes
  and task metadata are separate stores with path-safety matrices
  (`taskmeta_pathsafety_winmatrix_test.go`).
- **Memory trust boundary** (`internal/memory/trust_test.go`): memory
  mutation is user-visible and approval-gated, never hidden inference.

## 4. Content-class inventory (audited 2026-11-16)

| Content class | Owning package | Create/append | Edit | Delete/restore | Status |
|---|---|---|---|---|---|
| Sessions, messages, activity/summary sidecars | `internal/sessions` | atomic, serialized | title/context/model + edit-journaled run edits | **recoverable delete + restore + two-step purge (v1.11.1)** | IMPLEMENTED_AND_TESTED |
| Run/task records | `internal/api` (runregistry, runstate, runtransport) | durable transitions, settlement tests | state machine, edit-tx (`edittx.go`) | cancel preserves the record (not a delete) | EXISTING_AND_VERIFIED |
| Artifacts + task metadata | `internal/artifacts`, `internal/tools` | content-addressed upsert, checksummed | metadata edit | reference-aware; no cascade by default | EXISTING_AND_VERIFIED |
| Generated UI surfaces (presentation store) | `internal/presentation` | typed, versioned schema | full-graph validated snapshots | deleting a surface never deletes backing data | EXISTING_AND_VERIFIED |
| Files under lab/workspace | `internal/lab` | staged, verified | safeedit + verifier + repair | workspace-root authorized only | EXISTING_AND_VERIFIED |
| Persistent memory | `internal/memory` | proposal + approval | scoped | explicit, trust-tested | EXISTING_AND_VERIFIED |
| Provider/model profiles, config | `internal/config` | validated writes | copy-on-write patch, migrations versioned | — | EXISTING_AND_VERIFIED |
| Custom tools / automation | `internal/customtools`, `internal/api/automation.go` | registered, SSRF-policy-gated | — | explicit delete endpoints | EXISTING_AND_VERIFIED |
| Diagnostics/security logs | `internal/logging` | append-only JSONL/rotated app.log | never rewritten | rotation only, bounded, redacted | EXISTING_AND_VERIFIED |
| Attachments | `internal/attachments` | path-safety-tested | — | explicit delete | EXISTING_AND_VERIFIED |

Soft-delete/restore for classes other than sessions (artifacts, memory,
tools) is **STAGED_WITH_GATE**: the sessions implementation is the
reference pattern; extending it class-by-class is roadmap work gated on
per-class impact analysis (link: [`../../ROADMAP.md`](../../ROADMAP.md)).

## 5. Test index

- `TestDeleteMovesSessionToTrashWithTombstone` — recoverable delete, no payload leak in tombstone
- `TestRestoreRecoversSessionSidecarsAndIndex` — content + sidecars + index + post-restore usability
- `TestRestoreRefusesOccupiedDestination` — 409-class refusal destroys nothing
- `TestPurgeDeletedDestroysBytesPermanently` — two-step policy, verified destruction
- `TestDeletePendingSessionLeavesNoTrashEntry` — pending drafts are honest
- `TestTrashReconcileRebuildsMissingTombstoneAfterCrash` — crash-window reconciliation across a fresh store
- `TestTrashLifecycleAcrossFreshStores` — durability + repeatability
- `TestTrashOperationsFailClosedOnHostileIDs` — traversal/UNC/ADS/device-class ids refuse
- `TestRestoreUnknownSessionFailsHonestly` — typed `ErrNotTrashed`
- `TestSessionTrashLifecycleOverAPI` — the whole surface over HTTP incl. truthful `recoverable` reporting
- Pre-existing: `TestDeletePendingSessionRemovesEveryAuthority`,
  `TestDeletePersistedSessionCleansIndexFileAndSidecars`,
  `TestDeleteUnknownSessionFailsHonestly` (unchanged, still green — the
  observable delete contract is preserved by the recoverable
  implementation).

Race gate: `internal/sessions` is one of the seven race-gate packages;
`go test -race` is green including the new tests.

## 6. Hybrid-stack boundary (v1.11.1 hybrid-native-UI milestone)

The optional renderers added by the hybrid milestone (Go-Gui POC, GoGPU
POC, native WebView contract) have NO store access by construction — they
are client surfaces over the same typed-command boundary
([`UI-ENGINE-STACK.md`](UI-ENGINE-STACK.md)). Renderer switching or web
content cannot bypass app authorization: there is no path from any
renderer to a store. The web layer's fail-closed policy is enforced code
(`internal/webapphost`: privileged-scheme and private-network rejection,
bridge fail-closed with per-capability grants, downloads handed to the
existing safe downloader) with 8/8 policy tests green; pages are untrusted
content. The Go-Gui POC carries deterministic sample state only — no app
data path exists in it.

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
