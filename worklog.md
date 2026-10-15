# SHEYTAN-Local-Agent — Engineering Worklog

Current release:  v1.10.3

This worklog is a session log, not a second architecture document. The
architecture truth lives in `ARCHITECTURE.md`, the release evidence in
`UPDATE.md`, the future in `ROADMAP.md`.

---
Task ID: 1 (v1.9.5 session)
Agent: v1.9.5 engineering session
Task: v1.9.5 — repair the failed v1.9.4 release attempt (Actions run
38090590263, lint blocker at scripts/windows-live-answer-acceptance.mjs),
audit the v1.9.4 diff as untrusted new code, repair every reproduced
P0/P1 (harness false-proof paths, loopback transport exposure, engine/run
admission TOCTOU, identifier-bearing ordinary logs), and ship the
complete verified source ZIP

Work Log:
- Baseline: HEAD `171f6b7` (v1.9.4) == live origin/main (re-fetched and
  compared during the session — the checkout was the tip, not a stale
  clone). GitHub REST API rate-limited (unauthenticated): the Actions
  run logs were not fetchable, so the documented lint failure was
  REPRODUCED LOCALLY instead (exact oxlint error at :67/:136).
- Reproduced and confirmed against source: duplicate `const sleep`;
  growth contract accepted completion-time samples and settled rows as
  substitutes; OBSERVE_SETTLED counted rows; proc.kill() masquerading as
  window close; trace armed after module load (hook never installed) +
  a process env var the page never read; `ws` undeclared; the harness
  referenced by NO workflow; loopback.go serving the ENTIRE API on
  127.0.0.1:0 with no authentication; ws.go accepting an absent Origin;
  config.ts startsWith prefix origin matching; triple closeLoopback in
  Close(); StartLoopback before EnsureSetup with a non-fatal bind
  failure; duplicate SetRunGate installation; updateEngineVariantTx
  single-check TOCTOU; gpuEvent printing `runId=`; runId/session fields
  in ordinary logs across server.go, runcontrol.go, history.go,
  edittx.go.
- IMPLEMENT (P0): rewrote the acceptance harness to the ordered-evidence
  contract (sample A nonblank+non-placeholder+Stop up -> strictly larger
  sample B with Stop still up -> terminal settlement; any absence
  FAILS; settlement asserts non-whitespace final Markdown, no
  placeholder, exactly one assistant reply AND the expected user
  message; reload proven via the sidebar `data-session-id` active-row
  contract; real WM_CLOSE via taskkill without /F with bounded exit and
  failure-only forced cleanup; trace armed via CDP
  addScriptToEvaluateOnNewDocument BEFORE modules load, hook presence +
  expected stages + sanitization asserted; `ws` pinned 8.18.3 +
  lockfile). Wired the harness as a REQUIRED step of the Windows CI job
  (real packaged exe + native engine built by CMake + canonical
  e2e/make-e2e-model.py fixture + desktop config), after the existing
  runtime smoke.
- IMPLEMENT (P0, transport): loopback listener narrowed to EXACTLY ONE
  route (/ws/activity -> the SAME handleActivityWS); 256-bit
  crypto/rand per-server capability token, constant-time compared,
  required on every upgrade, delivered only via GET /api/ws-origin
  ({origin, token}; token only with an origin; never logged); absent
  Origin refused on the loopback path (no bypass); Host must equal the
  listener address; forwarding headers refused; everything else 404.
  src/loopback-origin.ts: strict URL-parse validation (exact normalized
  loopback http origin, canonical-form round trip refusing 127.1-style
  shorthand), bounded 3-attempt discovery retry on desktop pages with
  an explicit visible degraded state (activityTransportDegraded +
  persistent MessageStream banner); desktop.go sequences EnsureSetup
  BEFORE StartLoopback and treats a bind failure as a hard non-zero
  exit; lifecycle.go keeps exactly ONE idempotent closeLoopback (test
  pins concurrent Close); duplicate SetRunGate removed.
- IMPLEMENT (P1, admission): llm.EngineAdmission seam — the transaction
  claims admission exclusivity first, atomically with the live-run
  verdict under the API server's runsMu (the same section registration
  uses), and holds it across the whole transaction; runs admitted under
  a claim get the deterministic 503 (errEngineMutationInProgress) with
  the persisted transcript restored (preMutationMessages snapshot); a
  run live at claim time still refuses the transaction
  (ErrEngineBusyWithRun); resume honours the claim; nil seam preserves
  the historical behavior; runsMu stays a leaf lock (no deadlock).
- IMPLEMENT (P1, logs): gpuEvent now logs an ephemeral probe ordinal
  (probe=%d) — no runId; runId=/session= removed from every ordinary
  production log line tree-wide (settle summary, pause/resume/edit-tx,
  history); durations/counters kept; format-arg consistency verified by
  go vet.
- TESTS: internal/api/loopback_v195_test.go (least-privilege surface
  walk, guard matrix incl. no-token/bad-token/absent-Origin/foreign-
  Origin, real upgrade with token+origin receiving `attached`,
  idempotent+concurrent Close, no token without loopback);
  internal/llm/admission_v195_test.go +
  internal/api/admission_v195_test.go (claim lifecycle, live-run
  refusal, refusal 503 with unchanged transcript, claim-not-taken-when-
  live); src/loopback-origin.test.ts (accept/refuse matrix incl.
  lookalikes, schemes, ports, credentials, paths, IP shorthand).

Stage Summary:
- v1.9.5 verified locally: npm ci clean; typecheck green; lint 0/0
  (the CI blocker fixed, no rule disables); test:units 236/236 (incl.
  the new loopback-origin suite); test:release 38/38; release-version
  --check green at 1.9.5 (package.json -> config.go / build/config.yml /
  SIGNATURE); npm run build + verify:web green (tracked web/static
  synchronized AFTER the version bump); go build/vet/test (headless,
  -count=1) green on ./internal/... + ./cmd/...; go test -race green on
  api/llm/agent/sessions/runtime/englease.
- UNVERIFIED here, executed by hosted CI: Windows desktop UI acceptance
  (no Windows host in the authoring environment — the gate is wired,
  runnable and unexecuted), native C++ CTest (no cmake here; no native
  source modified), the full browser E2E with real generation (no
  provisioned engine binary here; no E2E product path changed by
  v1.9.5 beyond the harness).
- Honest boundaries: no push/PR/tag/release; no remote main change; no
  permissions widened; GPU claims still execution-evidence-gated; the
  AUTO probe still records refusals as deferrals, never persisted
  failures.

---
Task ID: 1 (v1.9.4 session)
Agent: v1.9.4 engineering session
Task: v1.9.4 — P0 root cause and repair of the Windows live-answer
defect ("the answer appears only after Stop"), the two zero-session E2E
failures (Actions run 38069168867, e2e/zero-session.spec.ts:86 and :275
at expectRealAssistantReply), the engine-transaction/run TOCTOU (P1),
the publishMs metric clarification (P1), the opt-in sanitized stream
trace (P0-A), and the real-Windows acceptance harness

Work Log:
- Baseline: HEAD `5bcd17b` (v1.9.3). The authoritative Actions run
  38069168867 FAILED (Linux E2E 38/40: zero-session.spec.ts:86 and :275
  at `expectRealAssistantReply`); v1.9.3 is NOT a green release. Local
  reproduction on this session's host was WORSE (3/4 zero-session tests
  failed the same helper) — Agent mode's CI pass was timing luck.
- P0-C REPRODUCTION AND ROOT CAUSE (test defect, not a product defect):
  local Playwright runs failed at `expectRealAssistantReply` with
  "element(s) not found", while the failure ARTIFACTS show the settled
  assistant reply rendered with real content. Proven: v1.9.3's helper
  waits on `.message-row.from-agent .message-content`, but
  `.message-content` exists ONLY on the live surfaces (GenerationBubble,
  StreamingBubble). A SETTLED assistant message renders through
  MessageBubble → `<Markdown>` → `div.message-markdown` — it never
  carries `.message-content` at all. The helper could only ever match
  inside the finalising window (the bubble's last live content), a pure
  timing race; every other suite asserts settled replies via
  `.message-bubble` for exactly this reason (chat.spec.ts:80,
  agent.spec.ts:58, run-control.spec.ts:62, model-first.spec.ts:179,
  composer.spec.ts:64). The helper was retargeted to the settled
  Markdown surface — strength PRESERVED and tightened (non-whitespace
  text asserted on the markdown surface itself; placeholder rows
  excluded belt-and-braces; the generation/streaming bubbles can never
  satisfy it).
- P0-A/B REPRODUCTION AND ROOT CAUSE (the Windows live-answer defect),
  proven from source at the exact pinned versions: Wails
  v3.0.0-beta.16 serves packaged desktop builds through the webview's
  resource-request interception. On Windows, WebView2 forwards ONLY
  `http://wails.localhost` requests into the Go handler
  (webview_window_windows.go: any request whose scheme is not "http" is
  handed to the webview's default handler); on Linux production the
  page rides the custom `wails://` URI scheme
  (linux_cgo.go:1271). WebSocket handshakes are NOT web-resource
  requests (documented WebView2 limitation), and no loopback listener
  exists in the desktop build — so the activity socket derived from
  window.location (`ws://wails.localhost/ws/activity`,
  src/config.ts WS_BASE) NEVER reached the backend: WebView2 resolves
  *.localhost to the loopback address on port 80, where nothing
  listens. The client retried 20 times and gave up silently. Live run
  frames (response/reasoning snapshots, status, done/error, run_snapshot
  replays, idle sentinels) therefore never reached the UI on ANY
  packaged desktop build: no streamed text, no done event, composer
  locked. Stop worked because abort() is HTTP: it flushes the
  accumulator synchronously (store.ts abort path) and schedules
  finalisation, whose history reload recovers the persisted reply over
  plain HTTP — exactly the user's symptom. This also explains why four
  releases of client-side scheduling repairs (v1.8.1 fast path,
  v1.8.2–v1.8.5 flush schedulers, hub conflation) never changed the
  symptom: every one of them is downstream of a socket that never
  connected on Windows. Linux E2E never saw it (real loopback
  listener); the Windows runtime smoke never exercised a chat.
- P0-B FIX (same endpoint, same authority, reachable transport):
  `internal/api/loopback.go` binds the EXISTING API handler on a
  loopback-only ephemeral listener (127.0.0.1:0) for desktop builds;
  `GET /api/ws-origin` publishes the origin over the intercepted HTTP
  surface ("" = stay same-origin — browser/headless behavior
  unchanged); the client resolves it once (config.ts
  ensureActivityWsOrigin, memoized, best-effort, loopback-only values
  honoured) and opens the activity socket against it (store.ts
  connectActivity → openActivitySocket, extracted verbatim). ws.go
  allowedOrigin accepts the Wails page origin (http/wails +
  wails.localhost exactly) against LOOPBACK request hosts only. Local-
  first posture preserved: loopback bind, ephemeral port, origin-
  checked upgrade, one handler, one /ws/activity, no second channel.
  Regression tests: internal/api/loopback_v194_test.go (origin
  discovery, REAL WebSocket upgrade through the loopback with the
  Wails origin receiving the `attached` ack, close resets, origin
  matrix incl. refusals).
- P1 ENGINE/RUN TOCTOU (confirmed from source; the user's Windows log
  showed candidate/rollback messages overlapping a run):
  autoVulkanEligibility() checks anyRunActive() ONCE, then
  AutoProvisionVulkanIfWorthy's transaction (updateEngineVariantTx) can
  run for many minutes (resolve → download → swap → verify → rollback)
  and stops a RUNNING engine with no re-check. Fix: SetRunGate seam
  (internal/llm/llama.go) installed by the API layer (anyRunActive);
  updateEngineVariantTx consults it at the top — a running engine plus
  an active run fails CLOSED with ErrEngineBusyWithRun before anything
  happens. The AUTO probe records the refusal as a DEFERRAL (never a
  persisted bounded failure — a temporary condition must not disable
  the candidate for the hardware identity). Tests:
  internal/llm/rungate_v194_test.go (refusal, state untouched, gate
  consulted once; clear-gate no-op path; nil-gate compatibility), plus
  the existing strict transaction tests still pass.
- P1 METRIC CLARIFICATION: the run summary's `publishMs` measured
  acceptedAt→donePublished (run duration to terminal publication) and
  read like a per-event socket latency. Renamed to
  `acceptToDoneMs`; added `gateMs` (acceptedAt→gateReady), `ttftMs`
  (the orchestrator's measured request_sent→first_token from
  agent.Timing), and aggregate published-event counters
  (`events`, `deltas`, `firstDeltaMs`) on runTimeline. Client-side
  boundaries (queue delay, write time, browser receipt, DOM visibility)
  are NOT derivable server-side; they live in the opt-in trace.
- P0-A DIAGNOSTICS: `src/stream-trace.ts` — opt-in (localStorage
  `SHEYTAN_STREAM_TRACE=1`), test-only, sanitized: monotonic
  performance.now timestamps, ephemeral sequence numbers, canonical
  kinds and LENGTHS ONLY (content asserted never to enter the trace by
  its unit test), bounded 512-entry ring + aggregates, exposed to test
  harnesses via window.__SHEYTAN_STREAM_TRACE_DUMP__ ONLY when enabled.
  Wired into every client boundary (ws receive/attach/snapshot, fold,
  flush, run start/end) and the GenerationBubble DOM commit
  (MessageStream). The zero-session spec attaches the dump on failure
  (testInfo.attach). Unit tests: src/stream-trace.test.ts (6).
  Server-side aggregates (events/deltas/firstDeltaMs) ride the ordinary
  run summary — counts only, no identifiers beyond the existing
  run-summary contract.
- WINDOWS ACCEPTANCE HARNESS: scripts/windows-live-answer-acceptance.mjs
  launches the real desktop exe with WebView2 CDP enabled (TEST-ONLY
  switch), asserts the exact release contract: ready surface → send →
  real non-placeholder text WHILE Stop is visible → strictly longer DOM
  text while Stop stays up → exactly one persisted answer → reload
  persists one user + one assistant reply → clean close; dumps the
  sanitized trace to the artifact dir on failure.
- VERSION: 1.9.4 through the canonical flow (package.json →
  release-version.mjs repaired config.go / build/config.yml / SIGNATURE;
  --check green).

Stage Summary:
- The Windows "answer only after Stop" defect is root-caused to an
  unreachable activity WebSocket in packaged desktop builds (Wails v3
  beta.16 interception is HTTP-only) and repaired with a loopback
  transport for the SAME API/WebSocket authority; proven server-side by
  a real WebSocket upgrade + attach ack through the loopback with the
  Wails page origin (Go test). The end-to-end Windows WebView2 proof
  REMAINS UNVERIFIED on this Linux session (no Windows host, no
  webview); the harness ships for a real Windows run and the native
  evidence is labelled unverified everywhere.
- Both zero-session E2E failures are root-caused to the v1.9.3 helper
  selector (settled rows have no .message-content); the product was
  rendering real replies all along. Fixed with strength preserved.
- Local verification this session: unit tests 226/226 (incl. the new
  stream-trace suite) and release tests 38/38; typecheck/lint clean;
  go build + vet -tags headless clean; go test ./internal/... -tags
  headless 62 pkgs ok, 0 FAIL; race gate green on api/agent/sessions/
  contextplan/histref/runtime; native cmake+ctest 13/13; browser E2E
  40/40 (both previously-failing zero-session tests green with the
  strengthened assertions). Env-bound limitation: the non-headless
  Wails desktop packages do not compile in this sandbox (GTK4/WebKitGTK
  headers absent — the documented v1.9.3 limitation, unchanged).


---
Task ID: 1 (v1.8.3 session)
Agent: v1.8.3 engineering session
Task: v1.8.3 — P0 root-cause and repair of the Linux CI session-delete
failure (Actions run 36713108772 / job 109880449134,
e2e/sessions.spec.ts:88 "Expected: < 4, Received: 4"), P0 deterministic
session-delete coverage, P1 roadmap evidence-ranking of the supplied
bottleneck reports

Work Log:
- Baseline: HEAD `c8867886` (v1.8.2). CI run 36713108772: Browser E2E
  30/31, only e2e/sessions.spec.ts:88 failing; stress/package skipped
  after the E2E failure. All other gates (source/frontend audit,
  Windows x64, Linux build deps, release metadata, native C++, Go
  verification) PASS.
- v1.8.2 diff audit: no session create/delete/list path changed (streaming,
  memory-evidence, self-model only) — the failure is a latent timing race,
  not a v1.8.2 regression.
- Reproduced with an instrumented diagnostic (request/response journal +
  server-side authoritative list snapshots + DOM identity capture; the
  browser's create-response delivery delayed to model CI latency): the
  test sampled its count baseline BEFORE the asynchronous "New session"
  create landed (its visibility gate was satisfied by pre-existing
  items); the delete then removed the correct session (verified absent
  server-side); the late create kept the count flat; final DOM == final
  server state. A pass of the same test = the poll catching the transient
  count window. Product behavior correct; measurement raced the app.
- Second, real defect found by the investigation: initializeAgentOnce
  applied its startup GET /api/sessions response WITHOUT a sessionListGuard
  ticket (the v1.7.5 repair covered refreshSessions only). The sidebar's
  New-session button is actionable during init, so a create landing in
  that window was clobbered by the stale startup list — the created
  session vanished from the sidebar. Also: AgentSidebar's
  `void deleteSession(id)` turned server DELETE failures into invisible
  unhandled rejections; and createSession's unconditional prepend could
  duplicate a row when a refresh landed between POST dispatch and
  response.
- P0 repairs: (1) agent-init routes the startup list write through the
  ONE sessionListGuard (ticket before the GET; mode-change and
  user-action detection around the eager first-install create; superseded
  responses apply only app/loading and delegate list+selection
  re-resolution to refreshSessions); (2) deleteSession surfaces failures
  through the store error state (state untouched on failure, never a fake
  success); (3) createSession prepend is idempotent by id; (4) sidebar
  rows carry data-session-id (the rendered 8-char slice collides within a
  bucket).
- Test repair (never weakened): the browser delete test now waits
  STATE-BASED for the created session (count grows by one), then asserts
  the SPECIFIC deleted id, survivor identity-set equality, the count,
  replacement activation, composer usability, and post-reload absence
  from the sidebar AND a fresh authoritative fetch. New browser E2E for
  the init race: the startup GET response is held until the create POST's
  response has REACHED the page (causality, no timers), then delivered;
  asserts the created session survives, appears exactly once, and is
  active.
- Deterministic coverage: src/session-delete-regression.test.ts (11
  scenarios over the REAL store via a scripted HTTP transport where
  response ordering is forced by causality — pending/persisted delete,
  replacement selection, delete-vs-held-GET, stale-GET non-resurrection,
  mode separation, honest duplicate-delete, post-delete fresh fetch,
  init-race survival, clean-init apply, eager first-install session) +
  src/extensionless-ts-resolver.mjs (Node resolve hook making the store
  testable outside the bundler); internal/sessions/delete_pending_test.go
  (pending delete removes every authority; persisted delete cleans
  index/file/sidecars with fresh-store no-resurrection; unknown-id honest
  failure). Registered in package.json test:units.
- Mutation verification: with the init guard neutralized (v1.8.2
  semantics), BOTH the store-level test and the browser E2E fail; with
  the repair, both pass. The coverage detects the defect class it was
  written for.
- ROADMAP.md repaired: artifact truth (changelog = sole release history;
  README current-only; UPDATE.md current release/maintenance evidence);
  Performance, Reliability & Scale Backlog with D/I/H evidence labels;
  v1.9/v1.10/v1.11/v1.12/v2.0 expanded per the mission; external report
  figures (percentages, TPS/TTFT/cache/VRAM, 3x/5x/80%) explicitly
  treated as hypotheses; incompatible prescriptions (Tokio/asyncio
  rewrites, mandatory FAISS/USearch/SQLite/bbolt/tree-sitter, assumed
  PagedAttention, NPU embedding service, dual-model VRAM swapping)
  rejected; performance measurement framework documented with no invented
  targets.
- Version truth 1.8.3 via the canonical gate (release-version.mjs
  --check green: package.json, package-lock.json, config.go,
  build/config.yml, SIGNATURE); codename gate green; README/ARCHITECTURE/
  agent.md/UPDATE.md/changelog.md synchronized to v1.8.3.

Verification (evidence classes; Linux x86-64, Go 1.26, Node 24):
- Frontend: typecheck green; oxlint 0 warnings; npm run test:units 191/191
  (incl. the 11 new store-level scenarios); production build + embedded
  sync green.
- Go: go test ./internal/... -tags headless -count=1 green (58 packages);
  go vet -tags headless ./internal/... green; race gate green
  (api/agent/sessions/contextplan/histref/runtime).
- Native: cmake configure/build + ctest 12/12 green.
- Browser E2E: FULL suite 32/32 green (real stack, real native engine,
  no skipped failures) — the repaired delete test and the new init-race
  test included; every v1.8.2 streaming/memory/self-model test unchanged
  and passing.
- Stress suite: 47 pass / 0 fail, hangs=0, crashes=0.
- NOT executed here (stated honestly): physical-Windows runtime
  acceptance. The repairs are pinned by deterministic tests and
  mutation-verified coverage, not by a physical-host observation.

Stage Summary:
- v1.8.3 complete on this revision: the run-107 session-delete failure is
  root-caused with instrumented evidence and repaired without weakening
  any test; the startup list write goes through the one generation
  authority; DELETE failures are honest; the prepend is idempotent;
  session-delete behavior is pinned deterministically at store, Go and
  browser layers with mutation-verified coverage; the roadmap carries
  the evidence-ranked backlog; version identity is exactly 1.8.3.
Task ID: 1 (v1.8.1 session)
Agent: v1.8.1 engineering session
Task: v1.8.1 — P0 real-Windows local-generation crash repair (small Gemma
model + `hi`), P0 single-frame streamed-answer visibility, P1 engine
rollback identity repair, audits, version truth

Work Log:
- Baseline: HEAD `e5a12c0` (`v1.8.0`), CI green (`36570586149`). The
  primary evidence is the user's Windows runtime log: local CPU backend,
  `gemma-4-E2B-it-Q4_K_M.gguf`, llama.cpp b11261, preflight
  `compatible=true`, then `recovered panic: runtime error: invalid memory
  address or nil pointer dereference` after `task classified: kind=chat
  complexity=8`, run settled `error` in ~57 ms, no request-sent /
  response-header / first-byte evidence.
- Reproduced the panic deterministically BEFORE any fix (synthetic
  Gemma-class GGUF: 262,144-entry tokenizer block > the parser's 8 MiB
  read bound): `ReadModelCard` fails at the LimitReader →
  `ResolveModelCapabilities` returns nil → the orchestrator's
  `resolveEffectiveContext` dereferences `caps.TokenizerFamily` between
  `task classified` and `tier selected` — the exact window and signature.
- P0 crash repairs: (1) nil-card fallback in `resolveEffectiveContext`
  (documented caps contract: configured context + conservative heuristic
  estimator); (2) GGUF metadata read bound 8 → 32 MiB
  (`ggufMetadataReadLimit`) so real Gemma-class cards parse again
  (model-aware context clamp + family estimator restored for that model
  class); (3) `ResolveModelCapabilities` made uniformly nil-config-safe
  (the vision block was guarded, the recommendation block was not — same
  deref class, found during verification); (4) gemma3n/gemma4 added to
  the tokenizer-family and chat-template lineage lists.
- Regressions (each verified to FAIL on the pre-fix code):
  `TestResolveEffectiveContextSurvivesUnreadableCard` (unit, panics
  pre-fix), `TestGemmaClassCardParsesUnderRaisedBound`,
  `TestResolveEffectiveContextGemmaClassEndToEnd`,
  `TestLocalGemmaHiChatCompletes` and
  `TestLocalChatSurvivesUnreadableModelCard` (the api package's FIRST
  local-provider run-level E2E tests — the test binary re-executes as a
  fake llama-server subprocess serving the real engine contract; the
  unreadable-card E2E settles `error` pre-fix, `done` post-fix).
- P0 streaming: the store routed streamed text through TWO render frames
  (activity batch frame → streaming flush frame). New
  `src/stream-fast-path.ts`: stream-critical events
  (response/reasoning) fold into the streaming accumulator at
  socket-receive time — ONE frame boundary; the events still join the
  timeline batch, a self-draining ledger prevents double processing;
  cumulative-replace, replay idempotence, sequence/stale-run protection
  and the synchronous done/error/abort flush are unchanged. 12
  deterministic frame-controller tests (`stream-fast-path.test.ts`), no
  sleeps. Registered in `npm run test:units`.
- Backend `emitProgress` SplitThink O(n²) assessed and NOT optimized:
  bounded by the ~8 ms emit throttle; at realistic local-model token
  rates the scan volume is negligible (worst case ~6 MB/s on a 100 KB
  response). Correctness preserved (measure, don't guess).
- P1 engine identity: the user's b10642 transient is the documented
  last-resort stamp during the verification window; the commit is
  authoritative (existing tests pin two-boot no-redownload, manifest
  fallback, corrupt-state fallback, genuinely-newer-still-updates). ONE
  real defect found and fixed: after a FAILED verification + Rollback,
  the transient stamp survived in `installed.json` while the restored
  manifest described the serving engine (state-first
  `EffectiveInstalledEngineTag` would then read the wrong build).
  `Rollback()` now re-records from the restored manifest;
  `TestRollbackRestoresRecordedIdentityFromManifest` fails pre-fix at
  exactly `b10642`, plus `TestCommitRemainsTheAuthoritativeIdentity` and
  `TestRollbackLeavesStateUntouchedWhenManifestHasNoTag`.
- P1 runtime-root migration audit: the user's
  `detected=1 merged=1 recovered=0 collisions=4 removed=1
  reloadConfig=false` IS the intended contract (deterministic newer-wins
  collisions, one authoritative root, idempotent next boot) — pinned by
  the existing migration family; no change. GPU/AUTO CPU with `numGPU=0`
  remains the honest evidence-ladder outcome; Runtime Governor untouched.
- Version truth 1.8.1 through the canonical gate
  (`node scripts/release-version.mjs --check` green); README/UPDATE/
  ARCHITECTURE/agent.md updated to v1.8.1 current with v1.8.0 compressed
  to history.

Stage Summary:
- v1.8.1 repairs the real-Windows `hi` crash at the root (both layers:
  the deref guard and the card-read bound), makes streamed answers
  visible through ONE render frame, and restores engine rollback
  identity truth. All regressions were proven against the pre-fix code.

---
Task ID: 1 (v1.8.0 session)
Agent: v1.8.0 engineering session
Task: v1.8.0 — P0 pause/resume synchronization repair, honest abort
marker, Runtime Governor vertical slice (Adaptive Runtime Intelligence),
documentation consolidation, version truth

Work Log:
- Inspected the live repository at HEAD `eab6e1b` (`v1.7.6`) and the
  failing Actions run `36553559366` (Windows x64 Go verification:
  `TestPauseResumePauseAgainThenResumeCompletes` — `run settled "done"
  (error "") while waiting for phase "paused"`).
- Root-caused the CI failure: the test reused `waitForRunResponse` after
  a resume; its `LatestResponse != ""` condition is satisfied by the
  STALE pre-resume cumulative snapshot, so the second pause was issued
  without proof the resumed generation was live. Reproduced the defect
  class locally; on Linux the failure is scheduling-dependent (the same
  structural hazard), on the Windows runner it deterministically failed.
- P0 repair (no production weakening): added `resumedGenerationEvidence`
  (strictly newer run sequence AND changed cumulative response/reasoning
  snapshot) + `waitForResumedGeneration` (fail-fast on settlement);
  `TestPauseResumePauseAgainThenResumeCompletes` now captures the
  pre-resume snapshot and requires new authoritative activity before the
  second pause. Hazard note added to `waitForRunResponse` (initial
  generation only). Regression coverage:
  `TestResumedGenerationEvidenceContract` (deterministic unit — stale
  cumulative state, seq-only bumps, shorter continuation captions),
  `TestPauseResumeRequiresNewGenerationEvidence` (integration replay of
  the CI scenario).
- P0 (latent, found during the audit): abort-after-resume was flaky at
  BASELINE (~1-in-6 on Linux). Root cause: the orchestrator's four
  context-cancelation exits published `Activity{Type: "done"}`;
  `observe()` folds any `done` as the terminal outcome, so the live
  state flipped `done` while the caller settled `aborted` — a registry/
  state divergence the one-settlement rule can never repair. Repair:
  honest `aborted` activity type end-to-end (orchestrator exits,
  `observe` folding, `captureTerminal` + resumed-path caption capture
  replacing the hardcoded "Completed", frontend typed `aborted` kind +
  dedicated store case replacing caption-string sniffing). Evidence:
  15/15 stressed abort-after-resume runs; deterministic regressions
  `TestAbortedRunStateAgreesWithOutcome`, `TestRunLiveObserveAbortedMarker`.
- Feature: Runtime Governor vertical slice (`internal/governor`) —
  ONE runtime policy authority fed by the EXISTING live monitor (same
  sampler, same cadence, same protection path; the monitor's cooperative
  critical protection unchanged). `ResourceState` (measured facts +
  explicit unknowns), sustained-duration + bounded rolling signals over
  the shipped four-level pressure vocabulary, `Envelope` with adjustment
  classes (`live`/`next-run`/`reload`), `AdmitHeavyweight`/`AdmitMemory`
  resident-budget admission (mapped size ≠ resident; unknown →
  conservative refusal), `SelfModel` with provenance. Policy only — no
  second sampler/scheduler/authority; executes nothing.
- Wiring: `Stack.StartGovernor()` on the existing poll path
  (`observeGovernor`); engine facts through the existing `llm.Backend
  Metrics` (read-only); active runs through the existing counter; CPU
  through an injected platform seam (Linux measured load average;
  Windows honestly unknown with CPU policy inert).
- Surface: `GET /api/governor` composing governor + sysinfo hardware
  (GPU/NPU labeled detection-only) + engine metrics + capabilities;
  System Centre `GovernorCard` (real values, named unknowns). API
  contract tests + frontend typecheck/lint/units/build green.
- Docs: README rewritten current + compact history; ARCHITECTURE.md
  rewritten around the v1.8 truth + compressed historical layers;
  ROADMAP.md rewritten capability-driven (v1.9+ explicitly FUTURE —
  NOT IMPLEMENTED); UPDATE.md top section = v1.8.0 with compressed
  records; agent.md = current handoff; REPORT.md marked unmistakably
  historical; worklog updated (this entry). Version identity 1.8.0 via
  the canonical gate (`release-version.mjs --check` green: package.json,
  package-lock, config.go, build/config.yml, SIGNATURE).

Verification (evidence classes; Linux x86-64, Go 1.26.0, Node 24):
- Deterministic unit: `go test ./internal/governor/ -count=3` green;
  new api regression suites green (`-count=5` targeted, family `-count=3`).
- Race: `go test ./internal/governor/ -race -count=3` green; api+agent
  full suites `-tags headless -count=2` green (176s + 40s).
- Integration: `/api/governor` contract; pause/resume/abort/edit family.
- Frontend: `npm run typecheck` (CI script), `oxlint` 0 warnings,
  `npm run test:units` green, production build + embedded sync green.
- Release: `node scripts/release-version.mjs --check` consistent at
  1.8.0; codename gate green on the tracked tree (and re-run against the
  clean-room drop at packaging).
- NOT executed here (stated honestly): physical-PC Windows runtime
  verification; native C++ build/ctest on this host (GTK/WebKit-free
  environment builds headless targets only); Browser E2E (Playwright)
  in this session.

Stage Summary:
- v1.8.0 complete on this revision: the actual CI failure is repaired at
  the root with regression coverage that makes stale cumulative-state
  reuse impossible; the abort path is honest end-to-end (flake
  eliminated); the Runtime Governor vertical slice is real, wired and
  exercised; docs are consolidated (current + history, no duplicated
  essays); version identity is exactly 1.8.0. Pre-v1.8 session records
  are summarized once below.

---
Task ID: 1 (pre-v1.8.0 record — summarized once)
Agent: v1.3.0 → v1.7.6 engineering sessions
Task: everything shipped before v1.8.0

Summary (compressed; the authoritative per-release detail lives in the
release tags, their tests, and the historical sections of UPDATE.md):
- Foundations (≤v1.2): sessions, streaming with sequence-stamped replay,
  tool registry, downloader, installer, health checks, environment
  centre, bounded authoritative run state, settlement edge.
- v1.3–v1.4: durable completion ordering (persist → summary → handoff →
  recall → rollover → terminal), idle-sentinel barrier, task state,
  cross-mode history, continuum rollover, repository index, custom
  tools, update rollback safety.
- v1.5–v1.6: multi-agent context, memory/recall tiers, Lab workspaces,
  engine-variant provisioning through the authoritative resolver,
  Vulkan asset validation on the Windows runner, clone + workspace
  surfaces, stable embedded-frontend asset contract.
- v1.7.0–v1.7.2: reliability passes (engine lifecycle, hub replay,
  stress surfaces), scheduler settlement contract, tool-registry
  snapshot isolation (the one-word-chat crash repair).
- v1.7.1: preflight gate (refuse before engine start), LiveMonitor with
  hysteresis + cooperative critical protection, context-exhaustion
  recovery (typed conditions, one bounded restart), native engine
  first-class backend, LICENSE.md consolidation.
- v1.7.4–v1.7.6: pause/edit/resume with durable checkpoints; transactional
  edits + journals + ten-window fault-injection recovery; restart
  recovery claiming the checkpoint revision; token-aware codename gate;
  generation-aware spec cache; the llama.cpp b10642/b11205 CLI contract
  verified against real `--help` fixtures.

---
Task ID: 1 (v1.8.2 session)
Agent: v1.8.2 engineering session
Task: v1.8.2 — P0 live-visibility repair (Stop-to-see bug), live
reasoning/memory/capability surfaces, P1 log redaction, identity-based
model-caps cache, README→changelog split, engine-identity log clarity,
version truth

Work Log:
- Baseline: upstream `main @ 9d481be3` (`v1.8.1`). Provisioned the
  sandbox toolchain (Go 1.27.1, Node 24, native build via plain make);
  native engine + frontend + headless server all built from THIS tree.
- REPRODUCTION (real stack): `e2e/repro-stream.mjs` drives Chromium
  against the real server + embedded frontend + native-engine fixture,
  tracing every WebSocket frame with timestamps and sampling the live
  generation bubble's DOM at 100 ms. Measured: `response` frames reach
  the browser; the fast path folds them; the rAF flush paints live text.
  The v1.8.1 pipeline is intact in a healthy renderer — every client
  failure class except the flush boundary was eliminated with evidence
  (frame trace, store-site audit, component/CSS audit, Go local-chat
  E2E for the llama path).
- ROOT CAUSE (P0): the streaming flush was armed EXCLUSIVELY on
  `requestAnimationFrame`. In the user's WebView2 runtime compositor
  frame callbacks can be throttled/suspended while the JS event loop,
  the WebSocket and React keep running — phase label + elapsed clock
  update, streamed text never appears, and Stop (synchronous flush)
  reveals everything at once. Repair: `src/stream-flush-scheduler.ts` —
  the flush is armed on an event-loop task (MessageChannel, React's own
  scheduling primitive) AND an animation frame; first to run flushes,
  the other no-ops; ONE coalescing latch; visibility never requires
  frame callbacks. Evidence: 8 deterministic scheduler tests + the
  browser harness.
- P0 live surface: factual no-reasoning state ("Thinking · this model is
  not exposing a reasoning stream" — only while thinking/preparing and
  no reasoning arrived; never fabricated); backend-truth memory line.
- P0 memory evidence: `contextplan.MemoryEvidence` composed at the
  injection site from survival-reconciled facts (summary injected +
  measured tokens; recalled exchanges actually carried; history refs;
  recall attempted) riding the EXISTING `context` activity; rendered
  verbatim (`Memory: session summary · 2 recalled exchanges` / `… · no
  recall matches`; NOTHING before the report arrives). Selection policy
  unchanged (FAST stays cheap; recall on intent; rolling summary
  automatic). Evidence: 2 Go tests + 9 frontend unit tests.
- P0 capability self-model: `taskclassify.SelfDescribe` deterministic
  intent signal (28 phrases, negative-space tested) +
  `internal/agent/selfmodel.go` — ONE bounded formatter over the
  EXISTING authorities: registry snapshot (ShortDescription-first,
  first-sentence fallback), the already-resolved model card (one read
  per run — `resolveEffectiveContextCaps`), config-backed backend
  facts, sysinfo fast snapshot, memory plan. Registered/enabled/
  offered/disabled distinctions; capability questions stay cheap (no
  research/recall/repo; one engine turn). Evidence: intent tests,
  inject/no-inject orchestrator tests, builder unit test.
- P1 log redaction: `internal/logging/redact.go` removes `runId=` /
  `runID=` / `session=` / `sessionId=` / `sessionID=` tokens at the ONE
  central sink (log formatter + crash-report panic text); token-start
  boundaries, empty-paren cleanup, idempotence; prose/URLs/durations/
  causes survive; internal identity untouched. Evidence: 18-case unit
  family + diagnostic-preservation test.
- P1 caps cache: `internal/llm/modelcaps.go` dropped the 10 s TTL for
  identity-based caching — immutable card under (path, size, mtime)
  with no expiry; config-sensitive fields re-derive from the cached
  card on a config-fingerprint change; bounded map, oldest-evicted.
  Evidence: 4-test family (no-reparse beyond TTL window, re-parse on
  identity change, re-derive on config drift, fingerprint coverage).
- P1 README/changelog: README rewritten current-only (no release
  history); `changelog.md` created as the ONE canonical history
  artifact (v1.8.2 detail + v1.8.1/v1.8.0 + compressed pre-history);
  UPDATE.md top section = v1.8.2 notes with v1.8.1 compressed to
  history; ARCHITECTURE.md + agent.md updated to v1.8.2 truth.
- P1 engine identity logs: boot probe reads `engine boot probe: binary
  build <tag> …`; deferred commit names the active build + next-boot
  probe (`staged → boot probe → committed` unambiguous).
- P1 turn latency: measured the class during the caps-cache work — the
  identity-based cache removes the per-turn GGUF metadata re-parse
  (32 MiB reads on Gemma-class cards) that the 10 s TTL always
  expired into. No benchmark numbers published (not measured on
  physical Windows here).
- E2E: `e2e/live-stream.spec.ts` (2 tests) + fixture support
  (`e2e/make-e2e-live-model.py` — a fixture-level unreachable-EOS
  model so the live window is deterministic; `startSheytan` gained
  model/generator/maxTokens options). Both tests PROVE live visibility
  while the run is live (Stop button up), text growth without user
  action, one final message, no duplicates, and an honest mid-run
  Stop.
- Version truth 1.8.2 via the canonical gate (`release-version.mjs
  --check` green: package.json, package-lock, config.go,
  build/config.yml, SIGNATURE); codename gate green.

Verification (evidence classes; Linux x86-64, Go 1.27.1, Node 24):
- go vet -tags headless ./internal/... green; `go test ./internal/...
  -tags headless -count=1` green (zero FAIL).
- Race: api/agent/llm/governor `-race` green (api re-run in isolation
  green; one earlier FAIL was CPU contention with the concurrent
  browser suite, not a race).
- Frontend: typecheck, oxlint 0 warnings, `npm run test:units` 180
  pass, production build + embedded sync green.
- Native: `make -C native/engine test` — all engine test binaries
  pass.
- Browser E2E: FULL suite 31/31 green (29 existing + 2 new
  live-visibility), real stack, no skipped-failure.
- NOT executed here (stated honestly): physical-Windows runtime
  acceptance; the WebView2 frame-throttling behavior was eliminated
  by construction (task-boundary flush) and pinned by deterministic
  tests, not by a physical-host observation.

Stage Summary:
- v1.8.2 complete on this revision: the Stop-to-see bug is repaired at
  the scheduling boundary, the live surface shows truthful reasoning
  and memory state, the model can describe its real tools and
  capabilities, human-facing logs are free of opaque identity tokens,
  repeated model-card work is gone, README is current-only with the
  history in `changelog.md`, and version identity is exactly 1.8.2.

---
Task ID: v1.8.4 session
Agent: v1.8.4 engineering session
Task: v1.8.4 — P0 root-cause and repair of four defect families from the
real Windows runtime evidence: (A) live response/thinking invisible
during generation until Stop; (B) the AUTO Vulkan candidate blocked by a
stale derived GPU posture + the b11273/b11310 boot-probe identity
contradiction; (C) zero-session Send dead end; (D) stale context-usage
races. Documentation to current state; full verification funnel; the
v1.8.4 final source ZIP.

Work Log:
- Baseline verified first: HEAD `ca44b08008f26df467d4bd9c7b70cc49729e1ffb`
  (v1.8.3) on `main` — matches the stated baseline exactly.
- P0-A root cause (from the code, deterministic): the v1.8.2
  `MessageChannelTaskController` was ONE-SHOT (port handler nulled the
  channel after the first delivery; a post while a message was in flight
  chained onto the armed callback WITHOUT reposting). One lost/delayed
  MessageChannel message wedged the controller and the flush latch
  forever — streamed text accumulated until Stop's synchronous flush,
  while the fast path's synchronous `transitionPhase` renders kept the
  phase label moving. Exactly the reported split. Also found the
  ACTIVITY flush was rAF-only (statuses/tools/done/error) — the same
  boundary-loss class for run settlement.
  Repair: `src/stream-flush-scheduler.ts` — THREE independent boundaries
  (reusable MessageChannel macrotask + 0ms timer task + animation frame)
  behind ONE coalescing latch; recoverable latest-wins task controller
  with an in-flight handshake and a deterministic microtask fallback; the
  activity flush moved onto the same scheduler class.
- P0-B root causes: (i) `internal/recommendation` wrote
  `gpuAutoOffload=false + numGpu=0` as a derived posture ("CPU-only
  until a Vulkan engine build is provisioned"); the AUTO probe gate then
  read it as an explicit OFF — the exact circularity in the runtime log.
  Repair: the recommendation never writes a derived OFF; a new
  `gpuAutoOffloadUserSet` field marks explicit user actions (toggle +
  config patch); `config.Load` repairs the legacy derived state ONCE,
  honestly noted and persisted; explicit OFF / manual layers / CPU
  profile are never touched. (ii) the deferred-commit verification
  window keyed boot identity on the RECORDED tag (still the previous
  build until Commit) — the "b11310 staged → probe reports b11273"
  contradiction. Repair: the installer writes a window-scoped
  `engine-stage-pending.json` marker at swap time; `detectCapsForBoot`
  probes/caches/reports the ACTUAL staged binary while it exists;
  Commit/Rollback clear it.
- P0-C root cause: `run()`'s lazy session creation was unreachable — the
  composer textarea and Send button were hard-disabled on
  `!activeSessionId`. Repair: composer enabled on zero-session spaces
  (model + live-run gates unchanged), truthful placeholder/footer.
- P0-D root cause: `refreshSessionContext` guarded only the session id —
  same-session out-of-order responses overwrote newer state; deletion
  never cleared/refreshed the context. Repair: monotonic request
  generation; invalidation on session switch/deletion/creation/mode
  switch/fresh runs; deletion now clears + refreshes a replacement.
- Funnel-surfaced defect (P1): `config.Save` used the FIXED
  `config.json.tmp` name — two concurrent writers rename each other's
  temp away (observed in this repo's own E2E run as a hard engine-start
  failure: "persist default engine path: rename …: no such file"). Every
  config write now uses a unique same-directory temp file.
- Tests added/updated: `src/stream-flush-scheduler.test.ts` (13 —
  including the deterministic lost-message wedge the v1.8.2 design
  fails), `src/context-refresh-race.test.ts` (7), 
  `src/zero-session-send.test.ts` (5), `e2e/zero-session.spec.ts` (4),
  the suspended-rAF live-stream E2E (1),
  `internal/updater/staged_identity_v184_test.go` (3),
  `internal/config/gpu_posture_v184_test.go` (5), recommendation
  contract updates. New store-level suites run the REAL store via the
  extensionless resolver (same harness discipline as v1.8.3).
- Docs: README current-only to v1.8.4 (triple-boundary flush, new
  verified capabilities); UPDATE.md rewritten as v1.8.4 current notes
  with compressed history; changelog.md v1.8.4 entry (factual,
  evidence-cited); ARCHITECTURE.md current for v1.8.4 (flush, zero-
  session, context generation, staged identity, GPU posture contract);
  agent.md handoff updated; ROADMAP.md only records what actually
  shipped with its evidence classes.

Verification (what actually ran here, Linux x64):
- `go build ./...` and the headless build pass; `go vet`-equivalent via
  test compilation clean.
- Go: full `go test ./internal/... -tags headless -count=1` PASS;
  race gate PASS (api, agent, sessions, contextplan, histref, runtime,
  config, updater, recommendation; one load-sensitive governor flake
  observed once under heavy parallel load, not reproducible in 3
  targeted race runs — unrelated to this change set).
- Native C++: `make -C native/engine test` all checks passed.
- Frontend: `tsc --noEmit` clean; `oxlint` clean; `npm run test:units`
  209/209; `npm run test:release` 38/38; release-version gate consistent
  at 1.8.4.
- Browser E2E (real stack: headless server + embedded frontend + native
  engine + Chromium): FULL suite 37/37 green (32 existing + 5 new).
- NOT executed here (stated honestly): GitHub Actions Windows x64
  packaging, physical-Windows/WebView2 runtime acceptance, real-GPU
  Vulkan execution evidence — the repairs are proven deterministically
  and on healthy Chromium; the physical claims remain the user's own
  acceptance step.

Stage Summary:
- v1.8.4 complete on this revision: the streaming flush cannot wedge
  (three boundaries, recoverable controller), the activity lifecycle is
  not rAF-hostage, zero-session Send works end-to-end, stale context
  responses can never land, the AUTO GPU posture is honest and
  provenance-marked, the engine-update window reports the binary that is
  actually on disk, and config writes are race-free. Version identity is
  exactly 1.8.4; no new authority was introduced anywhere.

---
Task ID: 1 (v1.8.5 PHASE 1 session)
Agent: v1.8.5 Phase 1 engineering session
Task: SHEYTAN-LA v1.8.5 Phase 1 — P0 live-streaming server-side repair, P0 reasoning-depth ladder (Low/Mid/High/Ultra) with real backend budgets, P0 Show/Hide Thinking, P1 engine Start/Stop removal, Phase 2 execution/evidence foundation, docs reconciliation, repository ZIP

Work Log:
- Baseline: HEAD `ef2e15f` (v1.8.4), clean tree. Full inspection of the streaming path end-to-end: orchestrator emitProgress (~8ms SmoothStream cadence) → publish wrapper (seq stamp + runLive.observe) → activityHub → WS writer → frontend fast-path fold → triple-boundary flush → render. Frontend v1.8.4 scheduler verified robust (self-healing MessageChannel controller, three boundaries).
- ROOT CAUSE FOUND (the remaining P0 bottleneck): the activityHub's subscriber delivery was a plain 128-deep channel whose overflow policy DROPPED THE NEWEST event ("a slow WebSocket must never block the agent run"). For cumulative response/reasoning snapshots that discards the frame carrying the FULL text while the buffer keeps stale prefixes — under transport backpressure (the same runtime class that lost MessageChannel deliveries) visible text freezes at a stale prefix until the run ends (Stop included), then the reconnect run_snapshot replay reveals everything at once. The server-side twin of the v1.8.4 wedge.
- P0 FIX: bounded, conflation-aware subscriber queues (activitySub). Overflow evicts the OLDEST conflatable event (response/assistant_delta, reasoning/thinking_delta, status — newest subsumes older); order + seq replay contract preserved; terminal events never preferentially evicted; publisher never blocks; memory bounded; per-event write deadline (30s) tears a wedged client down deterministically (reconnect + snapshot replay recover). 9 deterministic regression suites (drop-newest reproduction, terminal survival, publish-never-blocks with 20k offers to a wedged reader, close-drains-first, concurrent publishers, eviction policy); race gate green; full API suite green.
- P0 REASONING LADDER: replaced Auto/Fast/Thinking with Low/Mid/High/Ultra. Verified the llama.cpp request-level `reasoning_budget_tokens` parameter exists in BOTH managed builds by fetching and reading the ACTUAL b10642 + b11205 server sources (server-common.cpp: the OAI chat endpoint maps it into the reasoning budget sampler, applied when the chat template exposes a thinking section; the CLI fixtures independently document --reasoning-budget). Budgets: low=0 (thinking off), mid=1024 (default), high=4096, ultra=not sent (engine default — honest encoding). Implementation: ThinkingControl ladder + legacy migration at BOTH boundaries (Go NormalizeThinkingControl, TS normalizeThinkingControl); ChatRequest.ReasoningBudget (*int — explicit 0 stays on the wire; found AND fixed that the custom MarshalJSON needed the field added — caught by the wire-field-name test); orchestrator applyReasoningBudget gated !IsRemote; tier posture (low=latency floor, mid=neutral, high/ultra=nudge without context inflation); the level rides EVERY run request. 9 Go suites + tier-posture pin + 4 store-level scripted-transport suites.
- P0 SHOW/HIDE THINKING: persisted visibility-only preference (showThinking) gating the rendering of backend-reported reasoning on the live bubble, safety-net bubble and history messages; never in the payload (pinned), never touches level/budget; the store keeps folding reasoning snapshots while hidden; Chat/Agent share the semantics.
- P1 ENGINE CONTROLS: removed the user-facing Start/Stop engine toggle from AgentBody (the supervised-machinery decision); internal lifecycle (run gate EnsureLLMContext, settings restart flow, recovery, shutdown) untouched; download cancellation kept.
- PHASE 2 FOUNDATION: internal/llm/execution.go — the ONE shared execution/evidence ladder (detected → backend-available → device-selected → model-loaded → generation-executed → execution-evidence → verified) as a PURE composer over existing authorities (device presence, backend health, accelerator selection memo, verified model, measured perf ring, runtime offload line); surfaced as /api/engine's execution block; a read-through memo of the accelerator resolution (written by the ONE selection authority on /api/perf) — no second sampler/policy engine. 5 ladder suites pin detected ≠ verified, monotonicity, unknowns, wire contract.
- Docs: README (current product, v1.8.5 surfaces + honest limitations), ROADMAP (explicit Phase 1 COMPLETE / Phase 2 NEXT / Phase 3+ sections), ARCHITECTURE (subscriber delivery, execution ladder, reasoning budgets), UPDATE (v1.8.5 release notes), changelog (v1.8.5 entry), agent.md (v1.8.5 handoff + Phase 2 entry notes).
- Verification: go build headless OK; go vet changed packages OK; full go test for api/agent/llm/taskclassify OK; -race for the same four OK; frontend typecheck OK; 213/213 unit tests (incl. the 4 new reasoning suites); npm run build + sync:web + verify:web OK; release-version gate OK at 1.8.5; browser E2E on the REAL stack: live-stream 3/3 (visible-before-completion, Stop-settles-honestly, suspended-rAF), chat 5/5, composer 4/4 (incl. the thinking-control persistence/travel test). KNOWN FLAKE observed in THIS environment: zero-session.spec Chat-mode visible-before-completion fails ~50% of runs at the ROUTER level (POST /api/run answered 404 before any run/hub involvement — orthogonal to this diff; the Agent-mode zero-session test and all other suites pass deterministically; the same flake appeared across runs in different test positions). No test was weakened or skipped.
- Packaging: download/SHEYTAN-Local-Agent-v1.8.5-PHASE1-FINAL.zip (complete repository state, excluding .git/node_modules/caches/build artifacts).

Stage Summary:
- v1.8.5 Phase 1 COMPLETE: the live-streaming failure class is now closed on BOTH sides of the wire (v1.8.4 render side + v1.8.5 server side); the reasoning surface has real, verified backend meaning; thinking visibility is a first-class control; the engine left the user's critical path; Phase 2 starts from an explicit execution/evidence contract and a precise roadmap.
- Phase 2 entry: ROADMAP.md §Phase 2 + agent.md "Phase 2 entry notes".

---
Task ID: 1 (v1.8.8 session)
Agent: v1.8.8 engineering session
Task: v1.8.8 — audit-job repair, release-metadata synchronization, dependency
security, dataAnalysis hardening, E2E zero-session reload-persistence repair;
clean-room build and package of SHEYTAN-Local-Agent-v1.8.8-FINAL.zip

Work Log:
- Baseline: HEAD `e9a8448` (v1.8.7). Reproduced the CI run 37446279646
  audit-job failure exactly: `codename-gate.mjs` flagged 4 tracked lines in
  `internal/tools/data_tool_test.go` (650/658/666/672) — the v1.8.7
  TestJSONColumnOrderIsDeterministic fixtures used the retired codename as a
  JSON key. Renamed the fixture key to a codename-free alternative that
  preserves the ordering semantics (document order ≠ alphabetical order);
  the gate stays strict and the regression stays real (no exemption added).
- Release metadata: package.json 1.8.8; release-version.mjs repaired
  config.go / build/config.yml / SIGNATURE; package-lock.json root metadata
  synchronized (stale at 1.8.5 since v1.8.6); README current-release marker,
  ROADMAP CURRENT RELEASE section, UPDATE.md release notes, changelog §v1.8.8.
- npm audit: the high-severity advisory (GHSA-68fv-2mgg-jv7q,
  source-map-js <1.2.2 via vite→postcss) repaired with the semver-compatible
  lockfile-only upgrade to 1.2.2; direct dependency contract untouched;
  `npm audit` now reports 0 vulnerabilities.
- dataAnalysis hardening (the ONE data authority — no new subsystem):
  (a) export with `limit` mutated the LRU-cached dataset (cache poisoning)
  — the limited view is now a separate dataset; (b) delimiter sniff counted
  delimiters inside quoted fields — now quote-aware (countUnquoted);
  (c) analyze correlations ordering relied on sort internals for equal |r| —
  explicit (a,b) tie-breaker with SliceStable; (d) a requested correlations
  section with <2 numeric columns silently vanished — now answers honestly;
  (e) the 256 MB error text implied chunked processing — message corrected
  to the honest in-process bound; (f) join's right-index build honors
  cancellation; (g) the sample-mode PRNG global state is mutex-guarded.
  New `data_tool_hardening_test.go` pins RFC-4180 edges, splitLinesAny
  parity, delimiter sniff, numeric semantics, stats/outlier edges, aggregate
  degenerate groups, join many-to-many/empty, quality ±Inf, export cache
  containment/determinism, JSON later-keys/duplicate-keys, correlation
  tie-break.
- E2E (real stack, real engine): the zero-session reload-persistence spec
  failed deterministically. Trace + network HAR + WS frame logs root-caused
  it: run() kept `running:false` across session creation and the bounded
  attach wait, so the composer read idle while a Send was mid-startup; a
  reload in that window killed the pipeline before POST /api/run and the
  message never persisted (HAR: POST /api/sessions ok, no POST /api/run,
  messages total:0). Repair: the run-startup state (running/preparing) is
  set BEFORE the transport work in run(). Spec green in isolation (2.2s) and
  in the full suite.
- Verification evidence: frontend typecheck/lint/213 units/38 release green;
  codename gate green; release-version --check green; go test
  ./internal/... -tags headless -count=1 green; -race audit green on
  api/agent/sessions/contextplan/histref/runtime/tools/engdiscovery; native
  engine cmake clean configure + 12/12 ctest; 15 TestRealCppHost* Go
  integration tests RUN (not skipped) and pass; Playwright full suite
  36-37/38 per run, remaining failure class = live-window sampling
  sensitivity (proven pre-existing via stash experiment: live-stream:87
  fails identically on unmodified v1.8.7 code in this sandbox; the
  deterministic streaming siblings — Stop + rAF-suspended growth — pass).
- Sandbox gaps recorded honestly (no root): `go test ./...` and `go vet`
  fail only on the Wails cgo desktop packages (gtk4/webkitgtk-6.0 headers
  absent; CI installs them); Playwright --with-deps unusable (sudo) but
  chromium runs; all other gates executed for real.

Stage Summary:
- Every v1.8.8 surface reads 1.8.8 through the ONE release authority.
- The audit job's blocker is repaired at the root; platform jobs unblocked.
- The one dataAnalysis authority is hardened with regression coverage.
- The zero-session reload-persistence defect is fixed in the product, not
  the test.
- Deliverable: SHEYTAN-Local-Agent-v1.8.8-FINAL.zip (728 files, ZIP audit:
  opens, root present, required sources present, no junk, version 1.8.8,
  no stale identity, no codename leakage).

---

Task ID: v1.9.0-release
Agent: release engineering session (2026-10-08)
Task: v1.8.8 -> v1.9.0 — the AI System Builder + long-horizon agentic
engineering layer; root-cause repair of the v1.8.8 Linux zero-session
reload-persistence E2E blocker; version/doc restructure; packaging.

Work Log:
- P0 root cause (Actions 37654274420, e2e/zero-session.spec.ts reload
  test): the zero-session lazy `createSession()` POST ran while the store
  still read `running:false` — the v1.8.8 repair moved the startup state
  before the attach wait but left this ONE async window; the E2E
  "composer enabled" probe legitimately sampled INSIDE it and reloaded
  before POST /api/run (session exists server-side pending; transcript
  total:0 forever). Product fix in `src/store.ts`: startup state set
  BEFORE all transport work; lazy create carries `keepRunState` (it
  previously reset running/runPhase mid-startup); failure paths clean up
  deterministically; attachment snapshot before transport. E2E waits for
  the deterministic run-dispatch marker (optimistic user bubble) + the
  assistant row, then reloads and asserts the DURABLE transcript.
  Deterministic store regressions: zero-session-send.test.ts #6/#7 (+#4
  cleanup assertions).
- AI System authority: internal/aisystem (durable per-document store,
  atomic writes, bounded, deterministic, corruption-tolerant, reserved
  default system, non-destructive migration, revision monotonicity,
  clone/export/import, active pointer with repair). HTTP /api/systems.
  Execution binding frozen per run (systemId + systemRevision):
  instructions block, model override, reasoning preference (existing
  v1.8.5 ladder; explicit request wins), tool-surface constrain
  (ToolPolicy.AISystemConstrain — offer AND execution, remove-only),
  skills filtering, approval policy. ai_system activity event + run
  response identity.
- Goal engine: internal/goal (phases, bounded plan, per-step +
  goal-level evidence journal, checkpoints after planning/each step/
  approval/terminal, bounded replanning that keeps failure evidence,
  turn-budget parking, boot recovery marking found-live goals paused —
  idempotent, terminal stays terminal). Drive executor = ONE orchestrator
  with the frozen AI System snapshot. HTTP /api/goals (+start/pause/
  resume/cancel/approve/reject). Goals UI card.
- Approval authority: internal/approval (5 deterministic risk classes,
  policy vocabulary, exact normalized call identity + bounded ledger),
  per-run agent.WithApprovalGate seam installed ONLY by goal runs
  (deny-by-default; chat/agent behavior byte-identical), durable
  goal-level approval parking with exact-call resume + evidence on
  rejection + stale-id rejection.
- Bounded delegation: internal/multiagent/subtasks (read-only
  parallelism <=2, mutating serialized, no nested spawning, fan-out <=8,
  honest deadline blocks, deterministic subtaskId-order merge, failures
  stay failed). Race-tested.
- Repository navigation: internal/repoindex/navigation.go (`repo_nav`:
  open/navigate/read/grep; bounded ranges, provenance, honest truncation;
  same path-safety authority as repo_search). Registered in the runtime.
- Frontend: System Centre hosts the AI System selector/editor (create/
  edit/activate/clone/export/import/delete) and the Goals card (create+
  start, honest state chips, plan progress from actual plan state,
  pause/resume/cancel/approve/reject). Pure view-model unit-tested
  (ai-systems-view.test.ts). New E2E: e2e/ai-systems.spec.ts.
- Live-visibility fixture: e2e/make-e2e-live-model.py dims re-tuned
  (emb=144, layers=6, heads=6, kv=3, head_dim=24, ffn=384) after
  measured evidence: the previous dims generated the full 160-token
  reply in ~6 ms on a fast host — first response frame and done 6 ms
  apart — so the live surface legitimately never painted between two
  renderer frames (pre-existing sensitivity recorded for v1.8.7/v1.8.8
  sandboxes above); the first re-tune (emb=256/layers=10) overshot into
  a >30 s generation (observation bound expired before the first token).
  The shipped dims stream 22 response frames over a ~2 s window on this
  host: real generation, deterministic visibility on any host speed.
- Verification: go test ./internal/... -tags headless = ALL PASS; go vet
  (headless) clean; race detector green on goal/multiagent/aisystem/
  approval; npm typecheck/lint/220 unit/38 release tests green; full
  Playwright suite 40/40 (including zero-session reload and the new
  ai-systems flows) against the real headless server + real native
  engine + fixture GGUF. Sandbox gaps (unchanged, honest): no root —
  the Wails cgo desktop packages cannot compile here (gtk4/webkitgtk
  headers absent; CI installs them); no codename literals in this log.

Stage Summary:
- v1.8.8 -> v1.9.0: every surface reads 1.9.0 through the ONE release
  authority; changelog.md gains the v1.9.0 entry; ROADMAP restructured
  (compact baseline, v1.9 current/future boundary, v1.10-2.0 future);
  UPDATE.md = v1.9.0 notes; README current-only; ARCHITECTURE documents
  the v1.9 authorities; agent.md is the repaired v1.9.0 handoff.
- The Linux E2E blocker is fixed in the product; the full browser suite
  is green on the real stack; Windows-side sources carry no regression
  (platform-independent Go/TS only).
- Deliverable: download/SHEYTAN-Local-Agent-v1.9.0-FINAL.zip
  (repository tree; ZIP audit: opens, required sources present, version
  1.9.0 everywhere, no stale current identity, no codename leakage,
  exactly one LICENSE.md).

---
Task ID: v1.9.1-release
Agent: v1.9.1 engineering session (2026-10-08)
Task: v1.9.0 -> v1.9.1 — P0 root-cause + repair of the authoritative
Linux CI live-stream failure (Actions run 37704905404, Linux Browser-E2E
job 113077751580, e2e/live-stream.spec.ts "streamed text is visible
WHILE the run is live and grows without Stop"; CI 39/40), evidence-
discipline repair for the v1.9.0 "40/40" claim, Windows/Linux desktop
RUNTIME SMOKE gates in CI, packaging.

Work Log:
- EVIDENCE CLASSIFICATION FIRST: the v1.9.0 entry below records "full
  Playwright suite 40/40" — that was a LOCAL single-run claim. The
  authoritative GitHub Actions run 37704905404 shows 39/40 with the
  live-stream growth assertion failing. Correction recorded here: local
  runs are local evidence only; GitHub Actions is the only CI authority;
  the v1.9.1 verdict below is likewise local until the CI run carrying
  this revision reports.
- INSPECT: HEAD 4cdd392 (v1.9.0); the whole streaming chain was read
  (orchestrator emitProgress -> runLive.observe -> activityHub (bounded,
  conflation-aware) -> WS writer -> store fast path -> accumulator ->
  triple-boundary scheduler -> GenerationBubble DOM).
- MEASURED FIXTURE FACTS (this 2-core sandbox, real native engine):
  standalone engine-host probe — decode ~130+ tok/s (160 tokens over 21
  event frames in ~0.26 s warm), and PREFILL ~25-40 ms per prompt token
  (2-token prompt TTFT 0.02 s; ~300-token prompt TTFT 8.15 s; ~450-token
  prompt TTFT 15.9 s). Full-stack run telemetry: system briefing 440
  tokens, context 446/3.6k, TTFT 7.6 s, classifyMs 4091 (the classify
  stage includes the engine-gate wait), run total ~12.8 s, response
  frames 22 over a ~1.2 s window (p50 inter-frame 62 ms), final persisted
  reply 203 chars, exactly one assistant message.
- REPRODUCE: the failing test passed in isolation UNLOADED (20.3 s), so
  the CI failure was reproduced under MEASURED CPU CONTENTION (2 busy
  workers saturating both cores): the test FAILED with the exact
  authoritative signature — growth poll "Expected: > 0, Received: 0"
  (predicate stuck at 0: Stop visible the whole time, text never past
  the baseline) and the failure screenshot shows the run STILL LIVE at
  30 s with the bubble rendering the "…" PLACEHOLDER arm.
- ROOT CAUSE (test observation contract, NOT the product stream): the
  v1.8.2 contract sampled the live bubble's textContent and accepted ANY
  non-empty text as "streamed text". GenerationBubble renders
  PRESENTATION PLACEHOLDERS before the first content snapshot
  ("Connecting to the engine and preparing the turn…" — 48 chars — while
  preparing, "…" afterwards), so waitForLiveText passed within ~100 ms
  of Send on placeholder text; the growth poll's 30 s budget then ran
  during the engine gate + prefill phase (measured above: >30 s under
  contention), expiring before the real streaming window ever opened.
  The product chain was verified healthy end to end (WS frame + DOM
  timeline: browser received every cumulative snapshot; DOM followed at
  50-70 ms cadence; one persisted reply).
- REPAIR (observation only, never weaker):
  1) src/MessageStream.tsx: both placeholder arms now carry
     data-stream-placeholder in the DOM — presentation text is
     distinguishable from streamed content.
  2) e2e/live-stream.spec.ts: waitForLiveText + both growth observations
     require NON-placeholder snapshots; the growth baseline is the FIRST
     REAL streamed snapshot; the rAF-suspended proof uses the same
     real-content contract. All original assertions preserved.
  3) Fixture budget 160 -> 320 tokens: the engine emits one event chunk
     per 8 tokens (kEmitTokenWindow), so the budget IS the deterministic
     snapshot count of the live window (40 chunks); window duration
     still scales with host speed, snapshot count does not.
- MUTATION VERIFICATION: with the repaired test under the same 2-worker
  contention — (a) repaired contract: PASS; (b) placeholder detection
  disabled (the v1.9.0 contract restored by mutation): FAIL with the CI
  signature; (c) restored: PASS unloaded and under load. The coverage
  detects the defect class it was written for.
- CI RUNTIME SMOKE GATES (wired for the CI environment; NOT executed in
  this sandbox): build-linux gains "Desktop runtime smoke (native Wails
  binary under Xvfb)" — the REAL CGO desktop executable launched under
  Xvfb, session banner + boot markers asserted STATE-BASED in the
  isolated data dir's logs/app.log, liveness + no-crash-report, bounded
  SIGTERM shutdown — and "Headless surface runtime smoke" — the headless
  build serving the REAL embedded UI + API: event-driven /api/health
  readiness, GET / proves the embedded shell, /api/models discovery,
  clean shutdown. build-windows gains "Desktop runtime smoke (real
  executable, process + backend init)" — SHEYTAN-LA.exe launched with an
  isolated SHEYTAN_DATA_DIR, banner/liveness/no-crash state-based,
  CloseMainWindow + bounded exit (Kill only as a failing-run backstop).
  Packaging remains a DISTINCT gate: packaging success never implies GUI
  runtime success. Evidence levels are labelled explicitly in the steps.
- LOCAL VERIFICATION OF THE SMOKE DESIGN: the headless-surface smoke was
  executed HERE against the real binary and passed (embedded UI +
  health + model discovery + clean shutdown). The native Wails desktop
  binary CANNOT be built in this sandbox (no gtk4/webkitgtk pkg-config
  headers, no root) — same documented boundary as v1.9.0; its smoke step
  is CI-wired and its execution is recorded nowhere until CI runs it.
- Version identity 1.9.1 through the canonical gate (release-version
  --check green); changelog.md gains the factual v1.9.1 entry; UPDATE.md
  rewritten as v1.9.1 current notes; agent.md is the v1.9.1 handoff;
  ROADMAP gains the measured native-prefill batching work item; README
  current-only.

Verification (evidence classes; LOCAL Linux x86-64 sandbox, Go 1.27,
Node 24, 2 cores — see the classification note above):
- Frontend: typecheck green; oxlint 0 warnings/0 errors (108 files);
  npm run test:units 220/220; npm run test:release 38/38 (includes the
  codename gate); production build + embedded sync green;
  verify-static-assets green.
- Go: go test ./internal/... -tags headless -count=1 = 62 packages ok
  (2 load-sensitive flake observations in internal/api on this 2-core
  box while OTHER Go suites ran concurrently: TestGovernorEndpoint-
  ServesComposedSelfModel and TestLocalGemmaHiChatCompletes (30 s
  chat deadline) — both pass in isolation, both also flaked at the
  v1.9.0 baseline the same way, and the api package passes cleanly
  when run sequentially; not v1.9.1 regressions — CI runs suites
  sequentially). go test ./... -run Test = 61 packages ok + the two
  sandbox-blocked desktop packages (root + internal/desktop) failing
  to BUILD for the documented no-GTK-headers boundary. go vet ./cmd/...
  ./internal/... clean (the same two packages are the sandbox build
  boundary; CI vets the full module with the deps installed). Race
  gate (-race, headless): api/agent/sessions/contextplan/histref/
  runtime ALL ok.
- Native: clean cmake configure + build + ctest 12/12 green.
- Browser E2E: FULL suite 40/40 green (5.6 min, real headless server +
  real native engine + fixture GGUF, no skipped failures) — the
  repaired live-stream spec (3 tests) included; additionally the
  live-stream growth test was verified under 2-worker CPU contention
  (PASS) and with the placeholder detection mutated away (FAIL with
  the CI signature — the mutation evidence above).
- Stress: 47 pass / 0 fail, hangs=0, crashes=0.
- Headless-surface runtime smoke: executed HERE against the real
  binary — PASS (embedded UI + /api/health overall=ok + model
  discovery + clean SIGTERM shutdown).
- NOT executed here (stated honestly): GitHub Actions (no runner
  access from this sandbox), the native Wails desktop smoke (GTK4/
  WebKitGTK headers absent — the binary cannot even build here),
  Windows (no Windows). The authoritative v1.9.1 CI verdict is the
  Actions run that carries this revision; until it reports, no CI
  claim is made for v1.9.1.

Stage Summary:
- v1.9.0 -> v1.9.1: the authoritative live-stream CI failure is
  root-caused (placeholder-baseline observation contract racing the
  measured prefill phase — NOT a product streaming defect; the chain was
  verified healthy end to end), repaired with strengthened (never
  weakened) assertions, mutation-verified coverage, and a deterministic
  fixture snapshot budget; the v1.9.0 "40/40" claim is re-classified as
  local-only evidence with the CI 39/40 recorded as authoritative;
  desktop runtime smoke gates (Linux Xvfb + Windows process/backend-init)
  are wired into CI with explicit evidence-level labels; version
  identity is exactly 1.9.1 through the canonical gate.
- Full local regression green on this revision (see Verification above);
  the CI verdict for this revision is pending by construction (no local
  run may claim it).
- Deliverable: download/SHEYTAN-Local-Agent-v1.9.1-FINAL.zip (complete
  reproducible source tree; exclusions per the release contract; ZIP
  audit recorded in this entry).

---

## v1.9.3 session (2026-10-10) — the Governor race-gate root cause (reproduced on real load), the honest prefill-parity contract, the closed zero-session proof standard, the UI microtext floors

Authoritative starting point: Actions run 38035650428 (v1.9.2, HEAD
e5e1b79) — job 114165397477: the Go race gate
(`go test -race -tags headless -count=1 ./internal/api ./internal/agent
./internal/sessions ./internal/contextplan ./internal/histref
./internal/runtime`) failed `TestGovernorEndpointServesComposedSelfModel`
(internal/api/governor_test.go:93) with
`ok-pressure envelope class = live, want none`; agent, sessions,
contextplan, histref and runtime passed; Windows/Linux/publish jobs were
skipped behind the audit gate. Local HEAD verified identical: e5e1b79.

Work Log:
- Environment: cloned the repo at the authoritative HEAD; Go 1.26.1
  (matches the workflow's GO_VERSION "1.26"); cmake 4.4.4 (native
  engine); npm ci for the frontend toolchain.
- REPRODUCTION (true, not simulated): `governor.CPULoadPlatform` on
  Linux reads /proc/loadavg load1 normalized by cores. Spinning 4 busy
  loops on the 2-core verification host drove the real 1-minute loadavg
  to 2.87 (≈143% of core capacity) and the ORIGINAL v1.9.2 code failed
  the exact test with the exact CI signature
  (`governor_test.go:93: ok-pressure envelope class = live, want none`).
  Mechanism confirmed: runtime.StartGovernor wires the platform CPU seam
  into the governor the API test folds samples into; the test folded ONE
  OK-pressure RAM sample; rollingSeries.observe sets its FIRST EWMA
  sample equal to the raw value; one high first sample crossed
  CPUReduceAbove (85) and envelopeLocked flipped the class to `live`.
- FIX (internal/governor/governor.go): pinned the CPU policy contract in
  code — the envelope's CPU-reduction branch requires a SUSTAINED
  rolling signal (≥ cpuWarmupSamples = 3 folded samples AND average ≥
  CPUReduceAbove); the folded sample count rides the new
  ResourceState.CPUSamples and the reduction reason names the sample
  count. A lone spike (including the first sample ever folded) can no
  longer flip the envelope; sustained high load (~45 s at the 15 s
  cadence) still reduces background/concurrency. Added the
  mutex-guarded `Governor.SetCPUSampler` seam (production keeps
  CPULoadPlatform as the ONE platform sampler).
- TEST DETERMINISM (internal/api/governor_test.go): the OK-pressure
  wire-contract test now pins the CPU seam to an honest unknown before
  folding its sample — host load cannot leak into the pressure contract.
- POLICY TESTS (internal/governor/governor_test.go):
  TestSustainedCPUHigherThanThresholdReduces now folds a genuinely
  sustained signal (3 samples — its name means it); NEW
  TestLoneCPUSpikeDoesNotFlipEnvelope pins both directions (one high
  sample → class none; the same load sustained past the warm-up floor →
  class live).
- FIX VERIFICATION under real load: the fixed code passed the exact
  failing test with the host at 96–218% measured load (loadavg 1.92–4.35
  on 2 cores). The race gate command then ran GREEN on all six packages
  (api 102s under -race, agent, sessions, contextplan, histref,
  runtime). go vet ./... green; the full ./internal/... -tags headless
  suite green.
- NATIVE PARITY HONESTY (native/engine/tests/test_prefill_parity.cpp):
  the v1.9.2 header claimed per-position logits parity but the test
  compared only the full prompt's FINAL logits (the span path projects
  only span-final logits by design). The v1.9.3 gate proves and claims:
  per-position logits through the chunk=1 span cadence (bit-for-bit at
  all 20 fixture positions), final-token logits for chunks 3/7/16, full
  KV bytes identical across every chunking AND the serial run,
  chunk-boundary invariance of the final logits, edge/error bounds,
  mixed prefill→decode continuity; tied output explicitly NOT claimed
  (every shipped fixture carries output.weight); cancellation documented
  as the between-chunks generate-loop contract. CMakeLists.txt and
  forward.h comments aligned. Clean native configure/build/ctest:
  13/13 passed locally.
- ZERO-SESSION PROOF STANDARD (e2e/zero-session.spec.ts): the three
  tests v1.9.2 did not cover used count-based assertions that the
  placeholder-carrying generation bubble (it also carries
  .message-row.from-agent) or an empty persisted reply could satisfy;
  all now prove the settled reply is REAL content via
  expectRealAssistantReply (non-empty .message-content, no
  [data-stream-placeholder] descendant). e2e/live-stream.spec.ts: the
  rAF-suspended test additionally proves the persisted reply non-empty.
  No assertion weakened anywhere.
- UI MICROTEXT FLOORS (src/styles.css): all 62 font-size declarations at
  6–9px raised to the documented floors — dense secondary metadata ≥
  10px; essential labels/states/errors ≥ 11px (topbar status, engine
  status, error banner, Lab inline error/failure, automation form
  error); interactive control text ≥ 12px (Stop button, text/secondary
  buttons, composer + Lab/Automation textareas). No healthy size
  touched. The audited min-width conflict was re-examined: the ≤1050px
  breakpoint already releases body min-width to 0 and the native window
  clamps at MinWidth 1024 (desktop.go), so the layout contract holds;
  no structural layout change was made.
- VERSION/DOCS: package.json bumped to 1.9.3 and
  scripts/release-version.mjs repaired config.go, build/config.yml and
  SIGNATURE (--check green); README.md, UPDATE.md (v1.9.3 notes),
  changelog.md (v1.9.3 entry), ARCHITECTURE.md (the CPU policy
  contract), agent.md (v1.9.3 handoff), ROADMAP.md unchanged (future
  work only), this worklog entry.
- LOCAL EVIDENCE BOUNDARY (honest): verified locally — race gate green
  (all six packages), full headless Go suite green, go vet green,
  frontend typecheck/lint (0/0)/units (220/220)/build/release gates
  green, clean native CMake configure/build/ctest 13/13, the
  reproduction pair (original fails / fixed passes under real load).
  NOT verified locally — a green Actions run (local pass does not
  establish CI), the Windows/Linux desktop runtime smokes (no Windows;
  no GTK4/WebKitGTK in the sandbox), the full Playwright browser E2E
  suite (CI-bound). The authoritative v1.9.3 verdict is the Actions run
  that carries this revision.
- Deliverable: download/SHEYTAN-Local-Agent-v1.9.3-FINAL.zip (complete
  reproducible source tree; exclusions per the release contract; ZIP
  audit recorded in this entry).

---

## v1.9.2 session (2026-10-10) — the measured live-stream/TTFT engine repair, the desktop window-close lifecycle fix, the zero-session live-surface fix

Authoritative starting point: Actions run 37869632400 (v1.9.1, HEAD
f687489) — Linux job 113625251074: Browser E2E 37/40, ALL THREE
e2e/live-stream.spec.ts tests expired at the 60 s wait for REAL
non-placeholder streamed text; Windows job 113625251146: the desktop
runtime smoke process survived CloseMainWindow() + 15 s. The v1.9.1
release-level success claim is thereby corrected: local 40/40 was LOCAL
evidence only, and the placeholder-baseline diagnosis, while correct,
was not the complete root cause.

REPRODUCE (Linux, 2-core sandbox, the full real stack built from this
tree):
- The three live-stream tests PASS in isolation against a Release-built
  engine (matching the v1.9.1 local claim) and FAIL with the EXACT CI
  signature (60 s expiry, run still live, zero real snapshots) with the
  engine built the way CI builds it. Authoritative reproduction, not a
  guess.
- Server log evidence from the failing runs: promptBytes=1785
  estTokens=445-446 (the GO estimate), and the native generation still
  "before the first token" at 59-66 s (cancelled by the test's expiry).
  The chain engine -> hub -> WS -> fast path -> accumulator -> scheduler
  -> DOM was separately verified healthy (probe: every cumulative
  snapshot rendered at 50-70 ms cadence).

MEASURE (standalone C++ benchmarks against the live fixture GGUF,
Release and default builds):
- The E2E prompt encodes to 1681 ENGINE tokens (the fixture vocab has 5
  merges: ~1 token per byte; the Go-side estimate is 3.8x lower). The
  prefill budget question is decided by the ENGINE count, not the Go
  estimate.
- Serial per-token forward cost: 19.08 ms (-O2) and 19.02 ms (-O0,
  CI's exact configure) — the optimization level is NOT the dominant
  factor. Per-token arithmetic is ~1 ms; the rest is PER-ROW TENSOR-NAME
  RESOLUTION: ~8064 weight rows per token, each resolved through an
  ostringstream-built name plus std::map lookups (shape2d + find x2 +
  layout) inside row_span/row_f32.
- First token therefore ~= 1681 x 19 ms ~= 32 s standalone; under
  browser-suite contention on 2-4 cores it exceeds the 60 s budget —
  all three tests expire inside the gate+prefill phase. The v1.9.1
  fixture budget increase (160 -> 320) cannot affect TTFT (confirmed).

ROOT CAUSE (stream): serial prefill over a per-row-name-resolution
forward path. FIX (in the engine, at the proven boundary):
- tensor.h/.cpp: `Weights::resolve` validates a 2-D tensor ONCE (name,
  shape, type, WHOLE-tensor byte range through the same checked
  arithmetic) and returns a stable ResolvedTensor record; `row_ptr` +
  `dequant_row` own the per-row pointer math and the ONE dequantization
  switch (row_f32 keeps its behavior).
- forward.h/.cpp: `resolve_model` builds the per-layer resolved table
  (7 matrices/layer + the output projection) once per binding;
  `run_layers` is the single numerics authority for token() AND
  prefill_span; `matvec_resolved` replaces per-row name resolution.
  Measured: 19.08 -> 3.66 ms/token (5.2x), prefill AND decode.
- generate.cpp: the prefill loop calls `Forward::prefill_span` in
  bounded 16-token chunks (cancellation cadence unchanged, mu_ held per
  chunk exactly like the serial path), with the vocabulary logits
  projection ONLY for the final prompt token (intermediate prompt
  logits were never consumed; on real-vocab models the output
  projection is the largest per-token cost — the fixture's 50-token
  vocab makes it neutral there).
- CMakeLists.txt: CMAKE_BUILD_TYPE defaults to Release when unset (a
  bare `cmake -S -B` — CI's exact invocation — previously shipped an
  unflagged -O0 engine; the recorded measurement shows -O0 == -O2 for
  the name-resolution-dominated F32 fixture, and unoptimized inference
  arithmetic was never a supported configuration).
- PARITY GATE: tests/test_prefill_parity.cpp — prefill_span's
  last-token logits and the FULL KV cache bytes must match the serial
  token() path BIT-FOR-BIT across chunk sizes 1/3/7/16, plus edge cases
  (empty span OK, null span INVALID_ARG, out-of-vocab INVALID_ARG,
  position overflow CONTEXT_OVERFLOW, last-token logits with a null
  buffer INVALID_ARG) and mixed span -> token() decode continuity.
  Native suite: 13/13 (12 previous + this gate).

ROOT CAUSE (Windows smoke -> PRODUCT defect): the wails v3.0.0-beta.16
sources show a webview window's WM_CLOSE (Windows) / GTK close-request
(Linux) only EMITS the platform WindowClosing event and destroys the
window; the application event loop keeps running with zero windows
(Windows quits only from the hidden "__wails_hidden_mainthread"
window's WM_CLOSE/WM_DESTROY; no quit-on-last-window logic exists for
webview windows on either platform). SHEYTAN-LA is single-window with
no tray: closing the main window stranded a zombie process with the
backend running and the deferred srv.Close() un-executed — the
authoritative smoke failure was a REAL product defect, not a smoke
measurement artifact. FIX: internal/desktop/desktop.go binds
events.Common.WindowClosing -> app.Quit() (listener-order safe on both
dispatch orders — the default close listener and this binding are
idempotent in either order). Desktop lifecycle is thus: visible close
-> Quit -> app.Run returns -> deferred cleanup.

SMOKE GATE REPAIRS (evidence level made precise, never weakened):
- Windows: requires MainWindowHandle != 0 (Refresh-bounded wait),
  checks CloseMainWindow()'s BOOL (false => fail "no window received
  the close request"), requires exit code 0 through the normal path,
  and reports the exact unmet evidence level when no window exists.
  Kill remains the failing-run backstop only.
- Linux: the step owns a real Xvfb display on a dedicated display
  number (the xvfb-run wrapper's PID was never the app's PID; SIGTERM
  on the wrapper killed the X server, not the app lifecycle), locates
  the app's REAL top-level window via `xdotool search --onlyvisible
  --pid` + a `getwindowpid` OWNERSHIP CHECK, closes it with the WM's
  own WM_DELETE_WINDOW client message, requires normal-path exit
  (status 0), and keeps SIGTERM only as the failing-run backstop.
  xdotool added to the Linux dependency step. YAML validated.

P1 DISCOVERY + FIX — the zero-session live-surface clobber (exposed by
the 5.2x engine speedup; a REAL product defect, 5/5 measured):
- Symptom: after deleting the last session and sending, the live
  generation surface died ~90 ms after Send (bubble + Stop up at t0,
  stop gone at +90 ms, "aborted" at +160 ms) while the backend run
  completed normally 14 s later; the zero-session visible-before-
  completion E2E failed accordingly.
- Evidence chain (temporary in-page tracers, removed after the fix):
  the server's attach sequence attached -> idle -> run_snapshot lands
  just after run() sets its startup state; the attach-time idle
  sentinel (no session, no lastRun — composed BEFORE the run existed)
  hit recoverRunFromIdle's final fallback ("lost") because the grace
  branch saw run evidence that predates/belongs to no run; the armed
  grace re-check would have re-clobbered at +2.7 s (the real run's
  first gate evidence arrives at ~4.6 s). Bisection: without the
  prior-session deletion the flow is textbook (10.7 s live surface,
  clean settle).
- FIX: the attach-handshake exclusion — a lastRun-less idle sentinel
  within 250 ms of the attach acknowledgement
  (activityAttachAckAt, set when the `attached` frame settles) is
  handshake, never recovery evidence. Path A (idle + lastRun -> the
  v1.2.6 fast-run recovery) and late-idle semantics are unchanged.
  Post-fix harness runs 3/3: live surface holds 10-15.5 s, settles
  normally.

E2E CONTRACT CORRECTIONS (never weaker):
- zero-session.spec.ts: the visible-before-completion MutationObserver
  now excludes data-stream-placeholder arms (presentation text is never
  model output) and observes within a 60 s bound (the gate+prefill
  phase legitimately precedes the first snapshot; the v1.9.1 30 s bound
  expired INSIDE that phase under load — measured ~13 s unloaded).
- live-stream.spec.ts: a poll timeout now attaches compact structured
  diagnostics (the live-surface snapshot + the server log tail — the
  timeline evidence the authoritative Actions logs lacked) without
  dumping prompt/answer content; the header documents the v1.9.2 root
  cause with the measured numbers.

VERIFY (local evidence classes; Linux x86-64 sandbox, Go 1.26, Node 24,
2 cores — LOCAL ONLY, no Actions claim):
- Frontend: typecheck green; oxlint 0/0 (106 files); test:units 220/220;
  test:release 38/38; production build + embedded sync + verify:web
  green.
- Go: internal/... -tags headless 62/62 packages ok (first run, no
  flakes); ./... green except the documented sandbox build boundary
  (internal/desktop + the root package cannot BUILD without
  gtk4/webkitgtk pkg-config headers — no root in this sandbox; CI
  installs them); go vet clean on every compilable package; -race
  (api/agent/multiagent) clean.
- Native: clean cmake configure (default now lands Release) + build +
  ctest 13/13.
- Browser E2E: the FULL suite 40/40 green, 4.4 min (real headless
  server + real native engine + fixture GGUF; the three live-stream
  proofs included; the corrected zero-session proofs included). An
  interim full-suite run surfaced the zero-session clobber (the P1
  above); the final run is post-fix.
- Stress: 47/47, hangs=0, crashes=0 (after the functional gates).
- NOT executed here (stated honestly): GitHub Actions (no runner
  access from this sandbox); the desktop runtime smokes (the Wails
  binary cannot even build without the GTK4/WebKitGTK headers; no
  Windows). The authoritative v1.9.2 CI verdict is the Actions run
  that carries this revision; until it reports, no CI claim is made.

Version identity 1.9.2 through the canonical gate (release-version
--check green; package.json -> config.go / build/config.yml / SIGNATURE
repaired by the canonical script). README current-only; UPDATE.md
rewritten as v1.9.2 current notes; changelog.md carries the factual
v1.9.2 entry; ROADMAP's prefill item replaced by the POST-v1.9.2
arithmetic-optimization item (the name-resolution bottleneck is fixed
and measured); ARCHITECTURE current for v1.9.2 (the resolved forward
path + the desktop lifecycle); agent.md is the v1.9.2 handoff.

ZIP AUDIT (download/SHEYTAN-Local-Agent-v1.9.2-FINAL.zip, 747 files,
extracted to a clean room):
- opens/extracts; root SHEYTAN-Local-Agent/ present;
- required sources present: go.mod, main.go, main_windows.go,
  internal/{desktop,agent,api}/..., src/{store,MessageStream}.tsx,
  native/engine/{src/forward.cpp,src/generate.cpp,CMakeLists.txt,
  tests/test_prefill_parity.cpp}, e2e/{live-stream,zero-session}.spec.ts
  + fixtures + the live-model generator,
  .github/workflows/build-desktop.yml, scripts/release-version.mjs,
  web/static/index.html (the embedded frontend carries the store fix);
- exact version 1.9.2: package.json / internal/config/config.go
  AppVersion / build/config.yml productVersion / SIGNATURE first line;
- no stale current-release identity (README/agent.md/worklog all v1.9.2;
  UPDATE.md = the v1.9.2 notes);
- exactly one LICENSE.md (root);
- junk scan clean: no .git, no node_modules, no native build dir, no
  e2e/.build, no e2e/.artifacts, no dist, no *.log, no ZIPs inside;
  GGUF files only the COMMITTED test fixtures (tests/fixtures/);
- codename gate: zero codename references (run on the extracted copy);
- release-version --check: consistent (run on the extracted copy);
- CLEAN-ROOM BUILD from the extracted copy: native cmake configure
  (CMAKE_BUILD_TYPE=Release by default — the CI-class bare configure) +
  build + ctest 13/13; go build -tags headless OK;
  go test ./internal/api -tags headless ok; npm ci + test:units
  220/220. The archive is a complete, independently buildable source
  tree.

---

## v1.9.6 session (2026-10-11) — the FINAL 1.9.x release: the Windows admission-boundary repair, the Linux smoke environment repair, the R&D alignment + the measurement baseline

Task ID: v1.9.6-final (single-agent session)
Agent: release engineering agent (authoring host: Linux x86-64 VM, no
root, no GTK4/WebKitGTK headers, no Windows, no GUI)

Work Log:
- INSPECT: cloned live `main` — HEAD is exactly the baseline commit
  `070a05930f35ac09536ead74cc7cf2f376b8c2a4` (v1.9.5), clean tree; the
  R&D decision doc was read in full from `Parsaetak/Contents` branch
  `Research-and-Development` (`SHEYTAN-LA-2026-10-11.md`). GitHub API
  access to the failing run was rate-limited (unauthenticated), so the
  authoritative failure facts were taken from the mission record
  (exact test names, job IDs, messages) and re-derived from source.
- REPRODUCE (Windows 503): both authoritative failures re-derived from
  the code path; the causal chain was proven deterministically —
  `updateEngineVariantTx` (v1.9.5) takes the admission claim at
  TRANSACTION start; the background AUTO Vulkan probe
  (`go s.maybeAutoProvisionVulkan(gate.Done())` in `EnsureSetup`) is
  fully eligible on windows/amd64 (AUTO default, GPUAutoOffload=true
  default, GPU detected on CI runners, CI online, resolvable fixture
  model; `VariantSupported(Vulkan)` is windows-only) and held the claim
  across the network resolve/download → POST /api/run → 503. Linux is
  structurally immune (platform matrix gate).
- ROOT CAUSE + FIX (Windows): moved the admission claim to the
  DESTRUCTIVE boundary of the transaction; split the staged download
  from the swap via the new `updater.DownloadEngineArchiveStaged`
  (non-destructive half, no install lease; the swap half keeps the full
  lease/lock discipline through
  `InstallStagedFromArchiveDeferredWithVariant`); added the
  unconditional early advisory live-run refusal (read-only; v1.9.4
  semantics preserved at the boundary via the nil-seam re-check); a
  download failure no longer restarts the engine (nothing was stopped);
  fresh `wasRunning` re-read at the boundary. Both local-chat E2E
  fixtures pin an explicit CPU profile (the unrelated background probe
  is out of the pinned contract).
- VERIFY (Windows): `TestLocalGemmaHiChatCompletes` +
  `TestLocalChatSurvivesUnreadableModelCard` PASS, `-count=5`
  (determinism); new `internal/llm/admission_boundary_v196_test.go`
  pins (claim observed INSIDE the destructive window via the
  verification hook; begin/end balance; mid-download registration
  refused at the boundary without claiming; nil-seam boundary re-check);
  revised `admission_v195_test.go` (non-destructive exits never claim)
  + `rungate_v194_test.go` (single early consult); the FULL archive-seam
  runtime choreography suite (commit/rollback/honest-manifest) passes
  UNCHANGED against the new order.
- ROOT CAUSE + FIX (Linux smoke): the smoke shell is unchanged since
  v1.9.2 and earlier releases were skipped behind upstream gate
  failures — run 38095637817 was the gate's FIRST real execution; the
  "NO visible top-level window" + alive-process signature is the
  documented bare-Xvfb failure class (WebKitGTK 6.0 DMABUF renderer /
  GTK4 GL renderer stalling without GPU/GLX/EGL/compositor). Fix owned
  by the smoke step: `WEBKIT_DISABLE_DMABUF_RENDERER=1`,
  `WEBKIT_DISABLE_COMPOSITING_MODE=1`, `GSK_RENDERER=cairo` + xwininfo
  root-tree failure diagnostics (`x11-utils` added to deps). The gate
  (--onlyvisible + pid ownership + WM_DELETE_WINDOW + exit 0) is
  UNCHANGED and remains required.
- R&D slice: `internal/perf` (metric set of R&D §10; NOT-MEASURED
  semantics; nearest-rank P50/P95 with exact-value unit tests; targets
  recorded as gaps) + `cmd/shtn-perf` (JSON artifact + concise report;
  cold start + memory measured on the authoring host; generation-path
  metrics honestly NOT MEASURED without a real engine/model). NO
  performance target claimed as met.
- DOCS: canonical version gate 1.9.6 (package.json →
  release-version.mjs → config.go / build/config.yml / SIGNATURE,
  --check green); changelog v1.9.6 entry (the ONLY release chronology);
  UPDATE.md rewritten as current release notes; README current-release
  line; ARCHITECTURE engine/run coordination revised + strategic
  direction section; ROADMAP v1.9.6 section + the full v1.10.0 plan
  (phases/gates/dependencies/risks + the UpdateEngineNow standing gate
  item); agent.md handoff; native/engine/README promotion-status section.
- REGRESSION (local): `go build -tags headless ./internal/... ./cmd/... .`
  clean; `go test ./internal/... -tags headless -count=1` ALL packages
  ok; `go vet` clean on touched packages; `internal/perf` unit suite
  green. Frontend and C++ suites: executed in Phase 9 of this session
  and recorded below (see the stage summary).

Stage Summary:
- v1.9.6 is code-complete on the authoring host with a minimal, clean
  source diff. The two authoritative CI failures are root-caused and
  repaired with deterministic regression pins; the R&D direction is
  documented as authoritative with the first slice shipped as code.
- Evidence boundaries kept honest: Windows live-answer acceptance,
  Linux native window smoke, packaged E2E and physical-hardware
  performance numbers are UNVERIFIED locally by construction; the
  authoritative verdict is the Actions run carrying this revision (no
  push/tag was performed from this session).

## v1.10.0 session (2026-10-11) — the FIRST R&D-cycle release: the Windows CDP transport repair, the Linux WM repair, the UpdateEngineNow admission seam, the evidence-gated Phase 1 decision, the honest Phase 0 baseline, the Qt/IPC seam definition

Task: open the R&D engineering cycle. Deliver v1.10.0 against the
authoritative failing run `38135751846` (v1.9.6, commit `33884e1`,
FAILURE — Source & frontend audit SUCCEEDED 2m58s; Windows x64 FAILED
13m57s at `Windows live-answer acceptance` AFTER a green desktop runtime
smoke; Linux x64 FAILED 10m47s at `Desktop runtime smoke (native Wails
binary under Xvfb)`; Publish skipped). Per-job/per-step conclusions were
pulled live from the GitHub API; the log payloads are not retrievable
unauthenticated (HTTP 403), so the failure mechanisms are DERIVED from
source + step-level timing, labeled as derivations throughout.

ROOT CAUSE 1 (Windows, derived and source-proven): the acceptance
harness's CDP switch traveled through `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`,
but the pinned wails v3.0.0-beta.16 pure-Go WebView2 loader
(`internal/webview2/webviewloader`, `windows && !native_webview2loader`)
runs `preventEnvAndRegistryOverrides()` at package init and pins that
variable to "" (`os.Setenv`) — the switch was erased before any WebView2
environment existed, the CDP endpoint never opened, and the harness
consumed its full bounded 60 s attach window (matching the ~75 s step
total: ~15–25 s numpy/fixture prep + 60 s + cleanup). Fix: the desktop
shell forwards the TEST-ONLY `SHEYTAN_ACCEPTANCE_CDP_PORT` through
`application.Options.Windows.AdditionalBrowserArgs` (the channel Wails
composes itself into `CreateCoreWebView2EnvironmentWithOptions`); the
harness sets exactly that variable; digits-only shape check; sanitized
length-only CDP-timeout diagnostics. Windows build + vet verified by
cross-compilation (`GOOS=windows go build/vet ./internal/desktop/`).

ROOT CAUSE 2 (Linux, evidence-classified): both v1.9.5 and v1.9.6 failed
the same gate identically DESPITE the v1.9.6 renderer switches, and the
v1.9.6 dump shows the app pid owning created-but-unmapped windows — the
GTK4 top-level does not map under bare Xvfb without a window manager on
this runner stack. Fix: the smoke step installs and starts `openbox`,
proves WM ownership via the EWMH `_NET_SUPPORTING_WM_CHECK` root
property (bounded poll), cleans it up in the existing trap, and the
failure dump gains WM-ownership evidence. Gate unchanged; product
unchanged for CI. NOT reproduced locally (no GTK4/WebKitGTK dev headers
and no root in this container) — labeled a derivation from this run's
own evidence.

ADMISSION SEAM (P0): `UpdateEngineNow` now takes the same
destructive-boundary claim as the variant transaction: early advisory
refusal (no claim) → claim atomically with the live verdict before the
first destructive step → balanced release on every exit → nil-seam
boundary re-check. Pins: `internal/llm/admission_updatenow_v1100_test.go`
(five deterministic pins: claim held across the destructive window
observed from the claim seam itself — running at Begin, StateReady at
End; begin/end balance; live-at-boundary refusal keeps the engine
serving; early refusal consults the gate exactly once; nil-seam
re-check consults exactly twice). All v1.9.6/v1.9.5 pins green
unchanged (9/9 in the focused run).

R&D PHASE 1 (promotion decision): native C++ CTest 13/13 locally
(clean cmake 4.4.4 configure + build; `test_prefill_parity` green =
bit-for-bit prefill/KV parity; forward/generate/tokenizer/tensor
suites green). `NativeBackendEnabled` keeps native as an explicit
opt-in; managed llama.cpp remains the default. The default flip is
BLOCKED on specific evidence: the warm-TTFT comparison native vs
managed llama.cpp on physical Windows target hardware (R&D Phase 0
measurement contract). Decision recorded in UPDATE.md / ROADMAP.md /
native/engine/README.md.

R&D PHASE 0 (baseline): `baselines/perf-hosted-ci-v1.10.0.json`
captured via the shipped `cmd/shtn-perf` (hostedCI=true, code SHA
33884e1 + v1.10.0 working tree): cold_start_to_interactive_ms P50/P95
1.4 (api.New + EnsureSetup boundary), idle/peak memory 16.5 MB
(server-only RSS posture); engine_ready / warm_ttft /
stream_chunk_to_visible / stream_duration / tool_round_trip /
file_output_overhead NOT MEASURED with per-metric reasons. Targets
remain targets.

QT/IPC SEAM: defined in ARCHITECTURE.md (typed coarse-grained local
boundary, versioned message shapes, capability-token auth reusing the
loopback discipline, no second authority, shared memory only on
profiled wins, explicit NOT-ready list). No Qt code ships.

REGRESSION (local, exact commands in this session's transcript):
frontend `npm run typecheck` clean; `npm run lint` 0 findings;
`npm run test:units` 236/236; `npm run test:release` 38/38;
`npm run build` + `npm run verify:web` green (bundle rebuilt at the
canonical 1.10.0 identity); native cmake+ctest 13/13; Go focused
admission suites 9/9; `go vet` clean on touched packages; Windows
cross-build of the repaired shell clean. UNVERIFIED locally: the two
desktop gates (no Windows host; no GTK headers), the full browser-E2E
matrix, the physical-hardware baseline, and any Actions verdict for
this revision (no push performed from this session).

Stage Summary:
- v1.10.0 is code-complete on the session host with a minimal diff:
  `internal/desktop/desktop.go` (CDP seam), `scripts/windows-live-answer-acceptance.mjs`
  (transport + diagnostics), `.github/workflows/build-desktop.yml`
  (openbox install + WM-owned smoke), `internal/llm/llama.go`
  (UpdateEngineNow seam), `internal/llm/admission_updatenow_v1100_test.go`
  (pins), `baselines/perf-hosted-ci-v1.10.0.json` (baseline), docs
  (UPDATE/changelog/README/ARCHITECTURE/ROADMAP/agent/worklog/native
  README), canonical version 1.10.0 everywhere.
- The stale worklog header ("Current release: v1.9.5") — a v1.9.6
  reconciliation miss — is fixed; this file now states the truth for
  v1.10.0.


---

## Session — v1.10.1: full Qt/QML migration + v1.10.0 failure repair (2026-10-11)

Scope: the run `38152579695` P0 repairs, the R&D Phase 2 Qt migration,
the complete Wails removal, the canonical test entrypoint, version 1.10.1.

Work Log:
- Live inspection: HEAD `c9494dc` (`v1.10.0`) confirmed on `main`; run
  `38152579695` (failure) job evidence read: Windows `internal/api`
  teardown failure in `TestWebSocketSeesTerminalErrorNotSilence`,
  Linux desktop-runtime-smoke 1x1-window failure.
- Root cause A (Windows): the WS test returned at the terminal error
  frame while the run goroutine's deferred cleanup (TrackRunEnd →
  registry release → hub close) was still executing; on Windows,
  t.TempDir's RemoveAll raced it ("directory is not empty"). Repair:
  (a) the test now waits for the authoritative outcome AND the run
  registry release (bounded poll over existing seams); (b)
  `Server.Close` JOINS all run goroutines (bounded) and refuses new
  run/resume registrations after shutdown began (`closing` flag +
  runWG, ordering made total under runsMu).
- Root cause B (Linux): the Wails/GTK window never mapped under
  Xvfb+Openbox (1x1). Resolved by migration, not another workaround.
- Qt Phase 2 implemented: `internal/localipc` (per-user UDS + SO_PEERCRED
  / named pipe + current-user DACL + client-SID check, keyed versioned
  handshake, stale-endpoint recovery, bounded framing, structured
  errors, clean idempotent stop) + `ui/qt` (CMake, Qt 6.8.2, CoreClient
  HTTP+RFC6455 over QLocalSocket, Main.qml chat UI) + `internal/desktop`
  launcher rewrite. The existing api.Server handlers serve the channel —
  no policy duplicated in C++.
- Wails removal: imports/module requirement/transitive graph gone
  (CI asserts absence), loopback activity transport retired,
  `/api/ws-origin` answers same-origin unconditionally,
  build/config.yml rewritten, CDP harness replaced by the QtTest
  acceptance suite.
- Tests: `internal/localipc` suite (9 tests: handshake happy/403/401,
  real API tunneling, malformed request resilience, oversized-header
  bound, stale-endpoint recovery, foreign-uid denial, idempotent stop);
  `ui/qt/tests/tst_acceptance.cpp` (QtTest: real QML + real core + real
  fixture engine: handshake → session → send → ordered live growth with
  Stop → exactly one settle → exactly one persisted assistant reply →
  reload persistence → repeat send); `scripts/qt-desktop-smoke.sh`
  (G1-G5 Linux gates); teardown regression tests preserved unchanged.
- CI: build-desktop.yml — Windows Qt build + windeployqt + Qt
  acceptance; Linux Qt build + G1-G5 smoke + Qt acceptance; negative
  Wails dependency gate; GTK/WebKitGTK deps removed; YAML validated.
- Docs: changelog (canonical v1.10.1 entry), UPDATE, README,
  ARCHITECTURE (Current for v1.10.1 + the Qt lifecycle truth),
  ROADMAP baseline, agent handoff, native/engine README stack diagram.
- Version identity: package.json 1.10.1 → release-version.mjs repaired
  config.go/build/config.yml/SIGNATURE; --check green; codename gate
  green.

Verified on the session host (Linux x64, Debian trixie, Go 1.26.0,
Qt 6.8.2, cmake 3.31.10, Xvfb): go build ./... (Linux + windows/amd64
cross) clean; go vet ./... clean; go test ./... -count=1 PASS (exit 0,
full suite); go test -race (six concurrency-heavy packages) PASS
(exit 0); localipc suite PASS; the two previously-failing api tests
PASS; Qt acceptance suite PASS; qt-desktop-smoke G1-G5 PASS (real
1280x840 window, keyed handshake, core interaction, clean WM-close
exit, no orphan, no leaked endpoint); release-version --check PASS;
codename gate PASS.

NOT RUN / NOT VERIFIED on the session host (honest boundary):
Windows tests and packaging (no Windows host — Go windows/amd64
cross-build is clean, Qt host is prepared for MSVC but unverified
locally); native engine CTest locally (CI runs it; local disk/time
budget went to the migration); the physical-Windows R&D Phase 1
baseline (NOT MEASURED — no physical hardware; the engine default
stays evidence-gated); a hosted-CI Actions verdict for this revision
(no push performed from this session); remaining Qt surfaces beyond
the chat/sessions core (documented, not claimed).

Stage Summary:
- v1.10.1 is code-complete on the session host. Primary deliverable:
  `download/SHEYTAN-Local-Agent-v1.10.1-FINAL.zip` (source archive,
  audited). Version identity 1.10.1 everywhere.

---
Task ID: 1-2-3 (audit + reproduce + plan)
Agent: Super Z (v1.10.2 hardening session)
Task: Phase 1 LIVE AUDIT, Phase 2 GATE REPRODUCTION, Phase 3 ROOT CAUSE + ORDERED PLAN

Work Log:
- HEAD = cb11a238 (v1.10.1) = the audited baseline; worktree clean; no local tags.
- Toolchain provisioned user-space (no root): Go 1.26.0, CMake 4.4.4/Ninja, Qt 6.8.2 (aqt, linux_gcc_64), Xvfb present, xdotool/xwininfo/openbox extracted via dpkg -x into /home/z/xtools.
- go vet ./... clean; go build ./... clean; go test ./... -count=1 PASS (full suite).
- Focused localipc contract suite PASS (9 tests: uid scoping, stale endpoints, keyed handshake happy/mismatch, real API tunneling, malformed/oversized rejection, clean stop, no-TCP-listener).
- QtTest live-answer acceptance PASS (3/3) on Qt 6.8.2 + real core + fixture engine: keyed handshake, session create, live text growth with Stop visible, exactly-one terminal settle, one persisted assistant reply, reload, repeat send.
- GitHub Actions API query: 403 rate-limited from this IP → hosted-CI verdict NOT QUERYABLE this session (no push performed; per rules).
- npm audit: HIGH — ws 8.18.3 (direct devDependency): GHSA-58qx-3vcg-4xpx (uninitialized memory disclosure), GHSA-96hv-2xvq-fx4p (memory-exhaustion DoS). Smallest compatible fix: direct bump to 8.22.0 (same major), no --force.

ROOT CAUSE TABLE (Phase 3):
| # | Area | Cause / Evidence | Fix priority |
|---|------|------------------|--------------|
| R1 | docs drift | README.md "Current release: v1.10.0" vs canonical 1.10.1 | P3 (Phase 8) |
| R2 | smoke portability | scripts/qt-desktop-smoke.sh hardcodes XTOOLS=/home/z/xcb-libs (machine-specific; broken default anywhere else) and references $WORK/closewin without building it; no WM ownership (spec: smoke owns its WM) | P2 (Phase 5) |
| R3 | perf fail-open | cmd/shtn-perf --hosted-ci defaults true and is never cross-checked against host evidence; physical<->hosted can be silently swapped; RAM/hostname not recorded | P1 (Phase 6) |
| R4 | desktop lifecycle tests | internal/desktop has zero test files (uiExecutable resolution untested) | P2 (Phase 6) |
| R5 | remaining Qt surfaces | AI Systems, Goals, Coding Lab, Net Search, repoindex, tasks, downloads/diagnostics still headless/web (ROADMAP L141-143). Highest-value safe slice: AI Systems list+activation (GET /api/systems, POST /api/systems/{id}/activate — typed, bounded, existing authority) | P2 (Phase 7) |
| R6 | version identity | all canonical surfaces at 1.10.1; ui/qt/CMakeLists project VERSION not covered by release-version.mjs (manual sync needed) | P3 (Phase 8) |
| R7 | Windows Qt gates | MSVC/windeployqt/windowed smoke/QtTest-on-Windows cannot run here (no Windows host). Hostable proof: GOOS=windows cross-build of the core + static review of listener_windows.go + workflow. Honest status: NOT RUN / PHYSICAL HARDWARE UNVERIFIED | P1 honest-report (Phase 4) |

ORDERED PLAN: R7(honest hostable subset) → R2 smoke hardening+WM → R3 perf fail-closed → R4 desktop tests → R5 AI Systems Qt surface + acceptance slot → R6/R1 version+docs → ws bump → full regression → baselines → ZIP.

Stage Summary:
- Baseline reproduced green locally (Go + localipc + Qt acceptance). Real defects found: R2, R3, R4 (+R1 doc drift, ws advisory). Windows gate remains honestly NOT RUN (no physical/hosted Windows).

---
Task ID: 4-10 (implement + regression + package)
Agent: Super Z (v1.10.2 hardening session)
Task: Phases 4-10 — Windows hostable subset, smoke hardening, perf fail-closed, desktop tests, AI Systems Qt surface, docs/version/security, full regression, clean ZIP

Work Log:
- R8/P0 Windows gate: `GOOS=windows go vet ./internal/localipc/` reproduced `undefined: credAllowed` (test-only helper behind !windows tag used unconditionally by serve_test.go → the Windows job's `go test ./...` could not even build). Moved credAllowed to platform-neutral endpoint.go. After: Windows vet + full cross-build clean; Linux suite unchanged. No test weakened.
- R2 smoke: scripts/qt-desktop-smoke.sh — XTOOLS/XLIBS default to system PATH (opt-in overrides only), self-builds closewin from scripts/closewin.c (-lX11), self-builds the core when unstaged, UI fallback to the developer build tree, Openbox/equivalent WM ownership (WINMGR_BIN, private-prefix XDG dirs resolved so themes load), trap-based cleanup of every spawned process. G1-G5 PASS under Xvfb+Openbox: keyed handshake, core-spawned UI pid, visible 1280x840 top-level, real core interaction, clean WM_DELETE_WINDOW exit, no orphan, no leaked endpoint (ran twice, deterministic).
- R3 perf fail-closed: --hosted-ci bool (default true) replaced by MANDATORY --host-class physical|hosted; DetectHostEvidence (8 well-known CI markers); CheckHostClassification refuses physical-over-hosted-evidence; hostedCI derived from the declared class (normalize at JSON/Report); RAM/hostname/evidence/notes recorded. Pins in internal/perf/perf_test.go. Verified refusals: no class → refused; physical+GITHUB_ACTIONS → refused. New artifact baselines/perf-hosted-v1.10.2.json (hosted session host, honestly noted).
- R4: new internal/desktop/desktop_test.go (5 tests: none sentinel, override, override-missing refusal, next-to-core resolution, hard refusal when nothing resolvable). Portability gate initially flagged my #!/bin/sh placeholder fixtures — fixed to plain bytes (gate discipline respected).
- R5 AI Systems on Qt: CoreClient systems/activeSystemId properties + refreshSystems/activateSystem over existing /api/systems handlers; Main.qml AI Systems drawer (list, active badge, activate, refresh); acceptance slot aiSystemsListActivate (seeded typed list, authoritative activation flip, typed 404 with unchanged authoritative state). Acceptance 4/4.
- R6/R1 version+docs: package.json 1.10.2 → release-version.mjs repaired config.go/build/config.yml/SIGNATURE; ui/qt/CMakeLists.txt project VERSION 1.10.2; README (stale 1.10.0 block), ARCHITECTURE (current-for + desktop-lifecycle paragraph), ROADMAP (shipped-baseline line, Phase 0 + Phase 2 v1.10.2 status), UPDATE.md (v1.10.2 notes; v1.10.1 retained as history; baseline/verification sections rewritten), changelog.md (v1.10.2 entry), agent.md (v1.10.2 handoff). Stale-wording sweep clean.
- Security: ws 8.18.3 → 8.22.0 (direct devDependency; GHSA-58qx-3vcg-4xpx, GHSA-96hv-2xvq-fx4p; no --force). npm audit --audit-level=high: 0 findings.
- Regression: node test:units 236/236 + test:release PASS; go vet/build clean; go test ./... -count=1 65 pkgs PASS; go test -race on 5 heavy packages PASS; headless build PASS; GOOS=windows build+vet PASS; native/engine cmake build clean + CTest 13/13 (incl. prefill parity); Qt clean reconfigure/build at 1.10.2 + acceptance 4/4; qt-desktop-smoke G1-G5 PASS under Openbox; E2E (playwright, real native engine host) 40/40 PASS; canonical `node scripts/verify-all.mjs` 16/16 PASS.
- Actions status: NOT RUN this session (no push per rules); the unauthenticated Actions API query was 403 rate-limited from this IP — even run STATUS was not readable. Windows MSVC/windeployqt/QtTest-on-Windows and the physical-Windows Phase 1 baseline remain NOT RUN / NOT MEASURED (no Windows host; no physical hardware).

Stage Summary:
- v1.10.2 is code-complete on the session host. Deliverable: download/SHEYTAN-LA-v1.10.2-FINAL.zip (clean tracked tree via git archive, audited clean-room). Version identity 1.10.2 everywhere. Native-engine default NOT flipped; remaining Qt surfaces honestly listed.

---
Task ID: 1 (v1.10.3 session)
Agent: v1.10.3 engineering session
Task: v1.10.3 — the Qt IPC hardening release: make the Qt CoreClient's
HTTP/WebSocket paths correct under real framing, make the smoke and the
two hosted CI jobs runnable, prove the Windows staging path, pin the IPC
handshake and field contracts, and reconcile the evidence narrative
(register R1–R17 audited against 1d3b1f2 = live main HEAD at session
start)

Work Log:
- Baseline: cloned origin/main at `1d3b1f2` (v1.10.2) — HEAD matched the
  audited baseline commit exactly; every register item re-confirmed by
  reading the committed tree before fixing.
- Environment (hosted container, NOT GitHub Actions, NOT physical
  hardware): Go 1.27.2 (module builds go 1.26 language), Qt 6.8.2 via
  aqtinstall (the CI pin), CMake 4.4.4 + Ninja, Node 24; GL/X11 stacks
  staged from distro debs into a local prefix because the host has no
  root. GitHub Actions API rate-limited from this IP — live Actions
  status could NOT be retrieved; the runnability claims rest on the
  local reproduction + workflow wiring fixes, stated honestly below.

Per-item results (register → outcome → evidence):

- R1 (P0, smoke fail-after-use) — FIXED+VERIFIED. Reproduced first:
  missing-core/no-go printed "fail: command not found" and CONTINUED,
  dying later with "core died at boot". After the fix the same probe
  aborts immediately: "SMOKE FAIL: core binary … missing and no 'go'
  on PATH to build it", exit 1.
- R2 (P2, traps/pkill/WINMGR) — FIXED. EXIT-only cleanup + INT→130 /
  TERM→143 traps; pkill patterns derived from $CORE_BIN/$UI_BIN/$WORK
  plus the repo-tree fallback; bare WINMGR_BIN resolved via
  `command -v`; environ key read retried (bounded); Xvfb/engine/core
  readiness is event-based (socket/conn/log-line polls), fixed sleeps
  removed where replacement was exact.
- R3 (P0, Linux CI deps) — FIXED. libx11-dev + openbox added to the
  apt-get list (closewin builds with `cc -lX11` locally against staged
  headers; G3 refuses WM-less, so openbox is REQUIRED, not optional).
- R4 (P1, wrong staged paths) — FIXED. CORE_BIN and SHEYTAN_CORE_BIN
  now pass the REAL staged core name (SHEYTAN-Local-Agent); sheytan-ui
  is copied to $RUNNER_TEMP/sheytan-ui with an explicit `test -x`
  BEFORE the smoke runs — the silent repo-tree fallback can no longer
  mask the wiring.
- R5 (P1, exact-count flake) — FIXED+VERIFIED. Reproduced with
  TF_BUILD=1 (test failed: "evidence must contain exactly the set
  markers, got [GITHUB_ACTIONS TF_BUILD CI]"); after the fix the same
  env passes (membership asserted; foreign markers exercised in-test).
- R6 (P2, stale comment) — FIXED. readCodeSHA comment now describes the
  SHEYTAN_PERF_CODE_SHA contract. The three fail-closed CLI refusal
  probes were executed (no flag → 1; --host-class bogus → 1; physical
  with CI=1 → 1).
- R7 (P1, silent refreshSystems) — FIXED+VERIFIED. Non-200 now surfaces
  "systems refresh failed (status N)"; regression slot
  systemsRefreshSurfacesTypedError FAILS on the v1.10.2 client, PASSES
  on v1.10.3. refreshSessions/deleteSession got the same treatment.
- R8 (P2, raw id in request line) — FIXED+VERIFIED. Store charset
  verified URL-safe (`default`, `sys-`+lowercase hex); ids are
  percent-encoded defensively (path segments + the activity WS query);
  activateSystemEncodesSegment asserts the encoded wire path.
- R9 (P3, badge/EmptyState/double-refresh) — R9a FIXED (badge visible
  bound to text); R9b (EmptyState centered in ListView contentItem) and
  R9c (onOpened + connectToCore double refresh) are DOCUMENTED ONLY per
  register (harmless; noted in the changelog, no code change).
- R10 (P0, evidence drift) — FIXED+RE-MEASURED. The artifact was
  re-measured on this host (same declared evidence class as the v1.10.2
  record, notes carry the honest host description) with
  SHEYTAN_PERF_CODE_SHA=1d3b1f2… stamped and a trailing newline:
  cold_start 1.339 ms, idle/peak 14.3203125 MB. UPDATE.md quotes the
  artifact character for character; the stale 1.2 ms / 14.8 MB claims
  are corrected in place with a dated correction note.
- R11 (P2, "smallest compatible fix") — REWORDED in changelog.md,
  UPDATE.md, agent.md (upstream fixed in ws 8.20.1 / 8.21.0; 8.22.0
  chosen; dependency unchanged — `npm audit --audit-level=high` clean,
  0 vulnerabilities).
- R12 (RECORD, indent convention) — RECORDED in agent.md
  ("Code formatting convention": space-indented Go by convention, no
  gofmt gate, no mass reformat performed).
- R13 (P2, launcher override order) — FIXED+VERIFIED. Override now wins
  over next-to-core; TestUIExecutableOverrideWinsOverNextToCore added;
  the redundant t.Setenv("")+os.Unsetenv pairs removed.
- R14 (P2, verify-all claim) — FIXED. agent.md now states the CI gates
  are inline in build-desktop.yml and verify-all.mjs is the LOCAL
  canonical entrypoint.
- R15 (P3, this entry) — DONE.
- R16 (P2, Windows acceptance staging) — FIXED. Release/ resolved
  before the flat path; hard throw when neither exists; a post-copy
  existence check; never SilentlyContinue.
- R17 (P0, handshake version) — FIXED+VERIFIED. Name AND version==1
  required before connected; the regression slot
  handshakeRejectsWrongProtocolVersion FAILS on the v1.10.2 client
  ("!core.connected() … a sheytan-ipc/2 core must NOT be accepted" —
  the old client ACCEPTED v2) and PASSES on v1.10.3.

Beyond the register (discovered by executing the new gates):

- CoreClient HTTP framing: the v1.10.2 client had NO Content-Length
  handling — bodies completed only on server close (any response that
  held the socket open hung until the 20 s timeout). Reproduced in the
  before/after QtTest run: 9 of 10 framing slots FAIL on the v1.10.2
  client (including 20-second hangs), all PASS on v1.10.3. Oversize
  heads/bodies refused deterministically; a completed head beyond the
  64 KiB bound is now also refused (only the incomplete case was
  checked before).
- WS policy: fragmentation refused with a typed error (v1.10.2
  silently DROPPED continuation payloads), ping→masked pong,
  close→masked close reply + teardown, and the frame loop no longer
  starves frames queued behind a control frame (the old loop broke on
  ANY empty-payload frame).
- Sessions wire contract: GET /api/sessions serves a BARE array with
  id / title(omitempty) / msgCount — the v1.10.2 client parsed a
  wrapped {"sessions":[…]} envelope and a messageCount field, both
  hidden by the silent skip; the sidebar never loaded its list and
  every session rendered "0 messages". Client + QML fixed; three Go
  field-contract pins added (internal/api/qt_field_contract_test.go).
- Smoke G3/G5 window selection: the Qt host maps a 1x1 helper window
  alongside the real 1280x840 main window; the v1.10.2 `head -1` picked
  the helper and would have failed a green build's gates. Every visible
  window of the UI pid is now checked and G5 closes the window G3
  verified (found by actually running the smoke end-to-end).

Verification evidence (this session, hosted container — classes kept
separate):

- Go: `go build ./...`; FULL `go test ./... -run Test -count=1` PASS;
  `go test ./internal/... -tags headless -count=1` PASS; `go vet ./...`
  clean; `go test -race -tags headless ./internal/api ./internal/agent
  ./internal/sessions ./internal/contextplan ./internal/histref
  ./internal/runtime` PASS; `GOOS=windows go build ./...` +
  `GOOS=windows go vet ./...` clean (final state re-run).
- Qt: cmake configure + build of ui/qt green; QtTest acceptance 14/14
  PASS with the REAL core + fixture engine (offscreen platform): the
  two live-answer slots (chat stream with cumulative live growth while
  Stop is visible → exactly one settle → persistence reload → repeat
  send; AI Systems list/activate/typed-404) plus the ten framing/
  contract slots. Before/after proof recorded above.
- Native: `native/engine` cmake Release build + ctest 13/13 PASS.
- Frontend: npm ci; typecheck clean; oxlint 0 findings; test:units
  236/236; test:release pass; release build + verify:web green;
  npm audit 0 high/critical.
- Linux smoke: scripts/qt-desktop-smoke.sh G1–G5 ALL PASS locally under
  Xvfb + Openbox (staged debs; SMOKE_WORK/display overridden): keyed
  handshake, core-spawned UI pid, REAL visible 1280x840 window, UI
  holding 2 channel sockets + real session create/read through the
  core, real WM_DELETE_WINDOW close, UI exit, core stop, no orphan
  process, no leaked endpoint file. The no-go/missing-core abort path
  re-proven after the fix. The G3 WM-less refusal is in place (no
  GUI claims without a WM).
- Hosted CI: NOT RUN from this session (Actions API rate-limited from
  this IP; no push/PR per the operating rules). The two platform jobs
  are wired to the REAL staged paths and required deps; the first
  hosted run must still prove them.
- Windows Qt hosted gate: NOT VERIFIED (requires the hosted runner);
  the R16 staging path is repaired and compile-gated (GOOS=windows
  build/vet green), which is the maximum provable without the runner.
- Physical hardware: UNVERIFIED (no physical host; the native-engine
  default stays evidence-gated; generation paths remain NOT MEASURED
  without a real model).

Stage Summary:
- v1.10.3 closes R1–R17 (R9b/c documented-only per register), plus the
  framing, WS, and sessions-contract defects the new gates exposed.
- All local gates green at final state; hosted CI and physical-Windows
  remain honestly NOT RUN / NOT VERIFIED.
- Deliverable: download/SHEYTAN-LA-v1.10.3-FINAL.zip (complete tracked
  tree, no .git, no build artifacts).

---

Task ID: 1 (single-agent session)
Agent: Super Z (GLM engineering session, hosted Linux container)
Task: v1.10.4 — turn the two red hosted-CI gates of run 38569081029 into an
honest state (fix what is provably broken, prove everything provable, label
what cannot be proven here), align the docs with the Qt reality, deliver the
clean-room source ZIP under download/.

Work Log:
- INSPECT: cloned live main HEAD = b0b3aba (v1.10.3, exactly the failing
  run's head_sha; clean worktree). Confirmed run 38569081029 jobs via the
  Actions API BEFORE the rate limit hit: audit success; Windows job
  115870797382 failed step 13 (Go verification); Linux job 115870797589
  failed step 18 (Stress suite) while its Go verification (step 15) PASSED;
  publish skipped. Version surfaces read at 1.10.3.
- LOG EXTRACTION BLOCKED: the Actions job-log download API returns 403
  "Must have admin rights to Repository" for anonymous callers; the
  unauthenticated API rate pool was exhausted for the session's egress IPs;
  the legacy checks log endpoint 404s. The exact Windows error line is
  therefore UNOBTAINABLE from this environment (documented, not guessed).
- REPRODUCE (Linux stress, exact CI command): CGO_ENABLED=1
  `go build -trimpath -ldflags="-s -w" -o /tmp/sheytan-stress
  ./scripts/stress-main && /tmp/sheytan-stress stress` → FAIL-BEFORE
  captured: `release_surface FAIL: Linux CI dependencies missing
  "libgtk-4-dev" — Wails v3 needs GTK4 + WebKitGTK 6.0`, 46 pass / 1 fail,
  exit 1. (An earlier run from a foreign CWD also failed on
  `internal/sessions/sessions.go missing` — a CWD artifact of the sandbox,
  not a product defect; the scenario correctly gates repo checks on
  finding .gitignore in the CWD.)
- ROOT CAUSE (Linux): cmd/stress_release_surface.go §3f still REQUIRED the
  Wails-era GTK stack that v1.10.1 removed from the workflow when the
  desktop became the native Qt client. The workflow was correct; the stress
  contract was stale — the "stress scenario assumes a retired transport"
  class the mission predicted.
- FIX (Linux): §3f retargeted to the CURRENT Qt-era contract — REQUIRE
  libx11-dev, libxkbcommon-x11-0, libxcb-{cursor0,icccm4,keysyms1,image0,
  render-util0}, openbox, xvfb, xdotool, x11-utils (11 pins, was 2); BAN
  libgtk-4-dev, libwebkitgtk-6.0-dev, libgtk-3-dev, libwebkit2gtk-4.1-dev
  (4 bans, was 2). No coverage weakened; coverage strengthened.
  PASS-AFTER: exact CI command → 47 pass / 0 fail, exit 0; `-race` clean on
  cmd, sessions, memory, sandbox, tools, agent.
- REPRODUCE (Windows): compile/link PROVEN clean at b0b3aba AND at v1.10.4:
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`, `go vet ./...`
  with AND without `-tags headless` (the exact step-13 environment), plus a
  FULL test-binary LINK check for every package (`go test -run XXXNONE`)
  — zero build failures. The identical three step-13 commands pass on
  Linux CI (step 15 of the failing run) and locally. A user-space Wine
  10.0 attempt (no sudo; deb extraction into a relocated loader layout) was
  made to EXECUTE the Windows test binaries and abandoned within budget —
  wine's wineserver/ntdll lookup cannot be satisfied relocated.
- WINDOWS DISPOSITION: every test file changed since v1.10.1 reviewed
  package-by-package for Windows-runtime portability (api qt field
  contract, desktop launcher resolution, perf host classification, localipc
  endpoint, engcheck PE sniffing, llm import lock, updater variant naming,
  native/engine liveness probe) — no defect found; the failure class is a
  Windows-host runtime assertion that cannot be identified without the
  admin-gated log. v1.10.4 makes NO speculative Windows change and NO test
  weakening; WINDOWS GO VERIFICATION stays NOT VERIFIED until a hosted
  rerun.
- IDENTITY: package.json → 1.10.4; release-version.mjs synced config.go,
  build/config.yml, SIGNATURE; `--check` green.
- DOC TRUTH: README current-release block → v1.10.4; ARCHITECTURE currency
  header → v1.10.4, v1.9.5 loopback activity transport marked RETIRED (the
  Qt desktop mounts NO loopback WS listener — desktop.go), Qt/IPC seam
  marked SHIPPED since v1.10.1; ROADMAP shipped baseline → v1.10.4 with the
  accurate Windows verification label; UPDATE.md retitled with the
  v1.10.4 notes (v1.10.3 kept as parent record); changelog.md v1.10.4 entry
  added (canonical history); agent.md handoff → v1.10.4; stress-main header
  comment no longer describes the removed Wails shell.
- REGRESSION (v1.10.3 wins re-verified): qt_field_contract tests green (in
  the full suite); Main.qml binds modelData.msgCount (the messageCount hit
  is the defect-history comment); smoke script G1–G5 gates + WM requirement
  unchanged; workflow staging pins now ENFORCED by the retargeted
  release_surface scenario; sessions/systems wire contracts green.
- FULL GATES (local, this session): Go — internal headless CGO=0 (64 pkgs
  ok), full `./... -run Test` CGO=0 (0 FAIL), vet CGO=0, internal headless
  CGO=1 (0 FAIL), -race on stress packages, stress exact command 47/47;
  GOOS=windows build + vet(±headless) + full test-binary link (0 build
  failures). Frontend — npm ci, typecheck, oxlint 0/0, test:units 236/236,
  test:release, build, verify:web, embedded index.html, npm audit 0
  vulnerabilities. Native — cmake 3.31.6 (user-space) fresh configure +
  Release build + ctest 13/13 PASS.
- NOT RUN HERE (evidence classes kept separate): Qt host cmake build +
  QtTest acceptance + Linux qt-desktop-smoke.sh G1–G5 (no Qt 6.8/Xvfb/WM in
  this container; ui/qt and scripts/qt-desktop-smoke.sh are UNCHANGED since
  the v1.10.3 session where they passed locally); hosted CI (no push/PR/tag
  per the operating rules); browser E2E (optional, cut for budget).

Stage Summary:
- v1.10.4 = the honest reliability bridge: Linux stress gate FIXED
  (proven), Windows gate evidence recorded (NOT claimed fixed), zero
  surface migration, zero default flips, zero test weakening.
- All local gates green at final state; hosted CI NOT RUN from this
  session; Windows Qt + physical hardware remain NOT VERIFIED.
- Deliverable: download/SHEYTAN-LA-v1.10.4-FINAL.zip (complete tracked
  tree, single top-level SHEYTAN-LA/, no .git, no build artifacts).

---
Task ID: 1 (single-agent session)
Agent: Super Z (GLM engineering session, hosted Linux container)
Task: v1.10.5 — BOTH-PLATFORM GREEN OR NO ZIP. Make the Linux "Build Qt
host (Linux)" gate (run 38584769607 step 22) green with the workflow's
EXACT command, keep every known-good gate green, state the Windows "Go
verification" evidence class honestly, and deliver the clean-room source
ZIP under download/ — or stop with NO ZIP.

Work Log:
- INSPECT: cloned live main HEAD = 354122d (v1.10.4, exactly the failing
  run's head; clean worktree). Full workflow YAML read; the exact failing
  commands extracted: Windows step 13 = CGO_ENABLED=0 `go test
  ./internal/... -tags headless -count=1` + `go test ./... -run Test
  -count=1` + `go vet ./...` (pwsh Invoke-Gate discipline); Linux step 22
  = cmake -S ui/qt -B ui/qt/build -G Ninja -DCMAKE_BUILD_TYPE=Release
  -DCMAKE_PREFIX_PATH=$Qt6_DIR (Qt 6.8.2 via aqtinstall) + cmake --build.
- LOG EXTRACTION BLOCKED (documented, not guessed): the Actions job-log
  API requires admin rights for anonymous callers (403 "Must have admin
  rights"); the web UI gates logs behind sign-in; annotations carry only
  "Process completed with exit code 1". Run HISTORY recovered from the
  public runs/steps pages: v1.10.1, v1.10.2, v1.10.3, v1.10.4 — ALL FOUR
  Windows jobs failed at "Go verification"; the Linux job's Go
  verification PASSED every time (failure moved: v1.10.3 stress → v1.10.4
  Qt host build).
- REPRODUCE (Windows, maximum provable off-Windows): CGO_ENABLED=0
  internal headless 64 packages ok; full ./... -run Test 65 packages ok
  (re-run at the final tree state after every change); CGO_ENABLED=0
  go vet ./... clean; GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
  clean (full type-check incl. test files); GOOS=windows headless build
  clean. NO Windows-host runtime assertion can be identified without the
  admin-gated log; NO speculative Windows change was made.
- REPRODUCE (Linux Qt host, exact CI command): FAIL-BEFORE captured —
  `Could NOT find OpenGL (missing: OPENGL_opengl_LIBRARY
  OPENGL_glx_LIBRARY OPENGL_INCLUDE_DIR)` → WrapOpenGL NOT FOUND →
  Qt6Gui NOT FOUND. Root cause: the workflow installed only the GL
  runtime (libgl1, libegl1); CMake FindOpenGL needs the development
  headers + link-time .so symlinks. FIX: workflow now installs libgl-dev,
  libglx-dev, libopengl-dev, libegl-dev, mesa-common-dev,
  libxkbcommon-dev. PASS-AFTER: exact configure + build exit 0;
  sheytan-ui + sheytan-ui-acceptance produced; ctest found no registered
  tests (the workflow's ctest call is `|| true` non-gating, unchanged).
- REAL PRODUCT DEFECT found & fixed via the acceptance gate: the Qt
  live-answer acceptance failed with liveText() empty while the core log
  showed the run settling done with 20 delta events. A raw RFC6455 probe
  over the IPC unix socket proved the hub fragments every activity event
  > 1024 bytes (gorilla WriteBufferSize: 1024 → fragment(0x1,FIN=0,1024)
  + continuation(0x0,FIN=1)); the v1.10.3 client policy refused all
  fragmentation and closed the stream mid-run. FIX: readWsFrame now
  reassembles fragmented text messages (bounded by kMaxWsFrame; fragment
  state cleared on close); orphan continuations and interleaved messages
  still fail loudly with typed errors. Contract test revised
  wsRefusesFragmentedFrames → wsReassemblesFragmentedMessages (a real
  event split mid-JSON must reach liveText; an orphan continuation must
  still be refused). PASS-AFTER: full acceptance suite 14/14 exit 0
  against the REAL core (chatStreamPersistRepeatAndClose green;
  suite wall-time 30s → 4.7s).
- REGRESSION (all at the final tree state): go test ./internal/...
  -tags headless (CGO=0) exit 0; go test ./... -run Test (CGO=0) exit 0;
  go vet ./... (CGO=0) exit 0; stress suite EXACT CI command exit 0
  (47/47 — the one earlier FAIL was the documented foreign-CWD artifact);
  releasecontract + releasegate green after the version bump; npm ci,
  typecheck, oxlint 0/0, test:units 236/236, test:release, build,
  verify:web all exit 0; embedded frontend regenerated at web/static.
- NOT RUN HERE (evidence classes kept separate): qt-desktop-smoke.sh
  G1–G5 (no Xvfb/WM/xdotool in this container, no root); browser E2E
  (optional, cut for budget); hosted CI (no push/PR/tag per the
  operating rules); physical hardware (no physical host — UNVERIFIED,
  unchanged).
- IDENTITY: package.json → 1.10.5; release-version.mjs synced config.go,
  build/config.yml, SIGNATURE; `--check` green; core binary reports
  SHEYTAN-Local-Agent v1.10.5. Docs aligned: changelog (new v1.10.5
  entry), UPDATE.md (v1.10.5 notes, v1.10.4 kept as parent), README
  current-release, ARCHITECTURE currency header, ROADMAP shipped
  baseline + v1.10.5 STATUS, agent.md handoff.
- Windows `Go verification` disposition: exact step-13 commands GREEN
  locally with the step's exact CGO_ENABLED=0 environment; the hosted
  Windows job remains NOT VERIFIED until a push proves it. Per the ship
  rule this session records: local exact-command green (required for the
  ZIP) + honest hosted-CI status + zero test weakening (one contract
  test REVISED to the RFC-correct behavior, its violation refusals
  intact and re-pinned).

Stage Summary:
- v1.10.5 = the live-stream + Qt-host-build release: the step-22 Linux
  blocker root-caused and fixed at the workflow dependency level, the
  Qt activity-stream fragmentation defect fixed with byte-level probe
  evidence, all local gates green, hosted-Windows honestly NOT VERIFIED.
- Deliverable: download/SHEYTAN-LA-v1.10.5-FINAL.zip (complete tracked
  tree, single top-level SHEYTAN-LA/, no .git, no build artifacts).

---

## v1.10.6 REGISTER RE-VERIFICATION (live HEAD == 8d8e660, main)

Verified against live main HEAD (identical to baseline 8d8e660; no drift):

- W1 VERIFIED — internal/localipc/listener_windows.go:47-75: listenEndpoint() returns with NO pipe instance; first CreateNamedPipe is lazy inside Accept() (line 141). Immediate dial → ERROR_FILE_NOT_FOUND. Unix binds synchronously (listener_unix.go); Windows must too.
- W2 VERIFIED — listener_windows.go:251-257 (connectTimeoutMS=5000 at line 27): Accept returns the 5s connect timeout as a FATAL error → http.Serve exits → Windows IPC endpoint dies after 5 idle seconds.
- W3 VERIFIED — listener_windows.go:189-205: Close() never CancelIoExes a pending overlapped ConnectNamedPipe; a clean Stop can hang on the accept.
- W4 VERIFIED — serve_test.go:95-105 TestCredAllowedDeniesForeignUid is vacuous on Windows (os.Getuid()==-1 degenerates both asserts); verifyClientIsCurrentUser (listener_windows.go:264-295) has zero test coverage; malformed/oversized tests (serve_test.go:262,288) re-dial racing the next pipe instance creation.
- W5 CLASSIFIED BY INSPECTION (HEAD):
  - net.Listen("unix") — serve_test.go:117 only; PathSeparator skip guard present. SAFE.
  - /proc reads — production readers are platform-suffixed (perf_linux.go, governor/cpu_linux.go, proc/alive_unix.go, proc/identity_unix.go, resources_other.go) with honest fallbacks; test reachability guarded (phase5_bench_test.go:177 Skipf; cim_parse_test.go:126 OS=="linux" guard). SAFE.
  - exec of unix tools in tests — git only (releasegate_test.go, repoindex_test.go); git ships on windows-latest. SAFE.
  - internal/sandbox Job-Object tests — NO test files exist in internal/sandbox. NOT-PRESENT.
  - t.TempDir/RemoveAll teardown races — v1.10.0-era internal/api WS class already repaired (cleanup LIFO: srv.Close before TempDir RemoveAll; WS conns closed per-test); spot-checks confirm correct ordering at HEAD. SAFE as-found.
  - CGO_ENABLED=0 (Windows Go-verification env) — localipc is pure golang.org/x/sys/windows; no cgo on this path. NOT the cause.
- L1 CLASSIFIED — Qt Quick scene-graph GL context under Xvfb: software-rendering exports (QT_QUICK_BACKEND=software, QT_OPENGL=software, LIBGL_ALWAYS_SOFTWARE=1) must be set for the UI process. Env reaches the UI by construction: desktop.go:112 passes os.Environ() to the Qt child.
- L2 VERIFIED — scripts/qt-desktop-smoke.sh: UI stdout/stderr inherit the core's streams (desktop.go:118-119 → core.log line 190); $WORK/ui.log is never created, so failure paths at lines 210/273/325 cat an empty file.
- L3 VERIFIED — qt-desktop-smoke.sh:137: openbox readiness is a fixed `sleep 1`, not an event.
- L4 CONFIRMED — workflow smoke step wiring (LD_LIBRARY_PATH, QT_QPA_PLATFORM_PLUGIN_PATH, staged CORE_BIN/UI_BIN, xvfb/openbox/xdotool/x11-utils install) is correct; harden environment + diagnostics only; G1-G5 unchanged.
- P1 PROVEN AT HEAD — package.json:44 "ws": "8.22.0"; zero require("ws")/from "ws" imports tree-wide (hits are a URL-scheme literal src/LogViewer.tsx:90, a Go URL prefix runregistry_test.go:135, a dir name markdown_test.go:119). Frontend uses the browser-native WebSocket.
- P2 PROVEN AT HEAD — e2e/repro-stream.mjs referenced only by worklog.md:589 + changelog.md:2008 (both historical mentions, allowed residue), no workflow step, no package.json script, no verify-all.mjs entry. e2e/fixtures/sheytan-server.ts is shared with spec files and SURVIVES.
- P3 SWEEP AT HEAD — 21 files mention wails: all are (a) gates (verify-all.mjs absence gate, cmd/stress_release_surface.go GTK-stack ban, releasegate_test.go), (b) origin-REJECTION security code (src/loopback-origin.ts, internal/api/ws.go retired-origin handling + their tests/bundle), (c) retired-era comments (desktop.go, headless.go, gen-syso, backend.go, server.go), or (d) historical changelog/worklog/docs mentions. Nothing outside the protected classes; no blind deletion required. internal/llm/backend.go:13 carries a stale architecture comment — corrected to the Qt-client chain in this release.
- D1 VERIFIED — UPDATE.md/agent.md/changelog.md describe gates as fixed/runnable while Windows failed five consecutive hosted runs (through run 38714950052) and the Linux smoke had never run hosted until its first failure. v1.10.6 docs state the run truth with URLs and the failure-visibility mechanism.

OUTCOME REGISTER (v1.10.6 code actions):
- W1-W4: full transport repair in listener_windows.go (synchronous first instance; indefinite accept wait aborted by CancelIoEx on Close; next-instance pre-creation ahead of demand; sidAllowed seam + windows-only tests; TestCredAllowedDeniesForeignUid gets a documented platform split).
- W5: classifications above (no code change justified beyond the transport tests; all candidates SAFE/NOT-PRESENT with the evidence quoted).
- L1/L2/L3: smoke hardening (software-rendering exports, ui.log capture via a smoke-owned exec wrapper around the REAL UI binary, event-based xprop WM-readiness poll).
- CI: failure-visibility install (tee to logs; if:failure steps append failing-test names + log tails to $GITHUB_STEP_SUMMARY and upload artifacts; no secrets/keys in outputs).
- P1/P2: purge executed after the proofs above.
- D1: docs rewritten with run URLs + visibility mechanism; hosted result for v1.10.6 itself is recorded honestly (this environment has no GitHub credentials — push not possible; hosted green for v1.10.6 is NOT claimed).

---

## v1.10.6 IMPLEMENTATION OUTCOMES (evidence per item)

LOCAL EVIDENCE (this environment, Go 1.26.9 / Node 24 / linux amd64):
- W1–W4 transport repair: `CGO_ENABLED=0 GOOS=windows go build ./...` PASS; `CGO_ENABLED=0 GOOS=windows go vet ./...` PASS (compile proof ONLY — never runtime proof). Linux `go build ./...` + `-tags headless` PASS; `go vet ./...` PASS.
- Linux localipc suite unchanged and green: default tags 9/9 PASS (incl. TestCredAllowedDeniesForeignUid platform split, TestStaleEndpointRemovedLiveEndpointKept, TestStopIsCleanAndIdempotent), headless tags PASS.
- Full local regression: `go test ./internal/... -tags headless -count=1` — 71 packages, ALL PASS (api 89.9s, llm 65.9s, agent 20.9s, native/engine 16.4s); `go test ./... -run Test -count=1` — 77 packages, ALL PASS; race gate `-race -tags headless` on api/agent/sessions/contextplan/histref/runtime ALL PASS (CGO_ENABLED=1); stress suite 47 pass / 0 fail / 0 hangs / 0 crashes.
- Frontend: npm ci clean (0 vulnerabilities), typecheck PASS, lint 0 warnings/errors, test:units PASS (fail 0), test:release PASS (fail 0), npm run build PASS, verify-static-assets --dist PASS, verify:web PASS.
- verify-all.mjs: PASS with 2 environment-limited NOT RUNs (native CTest — no cmake binary in this environment; Qt host suites — no Qt6 CMake package).
- Browser E2E: NOT RUN (environment-limited) — the harness honestly refuses without the REAL native engine host; cmake is unavailable here so `native/engine/build/shtn-engine-host` cannot be built locally ("The browser suite proves the REAL stack; it never runs green without it."). Hosted Linux builds the engine before E2E; the suite is unchanged.
- Qt desktop smoke G1–G5: NOT RUN locally (no Xvfb WM/xdotool/openbox/Qt 6.8.2 in this environment); the script is bash -n validated and its environment hardening (L1–L3) is the hosted-run fix. qt-desktop-smoke.sh syntax PASS.
- Windows-only tests (TestSIDAllowed*, TestServeSurvivesIdleBeyondFormerConnectTimeout, TestBurstDialsNeverRaceInstanceCreation, TestStopDoesNotHangOnPendingAccept): compile-verified via GOOS=windows vet/build; they EXECUTE in the hosted Windows job — runtime proof belongs to that run, per the evidence-class rule.

PURGE OUTCOMES:
- P1 ws removed from package.json + package-lock.json regenerated (npm install --package-lock-only; zero "ws" entries; npm ci clean; 0 vulnerabilities).
- P2 e2e/repro-stream.mjs deleted (git rm) after the zero-reference proof at HEAD; e2e/fixtures/sheytan-server.ts untouched (shared fixture).
- P3 no deletions: all remaining wails mentions classified gates/security-rejection/retired-era-comments/protected-historical (see register above). internal/llm/backend.go stale architecture comment corrected. ui/qt/CMakeLists.txt project version aligned 1.10.3 → 1.10.6 (no gate reads it; identity consistency).

IDENTITY: package.json 1.10.6 → release-version.mjs repaired config.go AppVersion, build/config.yml productVersion, SIGNATURE first line; `--check` PASS ("release metadata consistent").

HOSTED (not executable from this environment — no GitHub credentials):
- Push to main: NOT POSSIBLE from the repair environment (credential-less). The failure-visibility install ships in this tree so the next hosted run publishes its own diagnosis to $GITHUB_STEP_SUMMARY + artifacts.
- No hosted green is claimed for v1.10.6. The gate table in the final report marks hosted rows accordingly.
