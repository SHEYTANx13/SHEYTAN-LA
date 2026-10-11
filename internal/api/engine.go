package api

// Engine state surface (v1.1.3): the backend engine/process state is the
// single source of truth for the UI. This file exposes it two ways:
//
//   - GET /api/engine — the authoritative snapshot (state, model, detail…)
//   - engine transitions broadcast into every activity WebSocket (both
//     live run hubs and idle standby connections)
//
// The frontend must never invent a state it has not received from here.
//
// v1.1.5 Phase 1: the snapshot additionally reports WHICH backend serves
// generation (backend) and the supervised native engine's own status
// (native) when the native path is enabled. Since Phase 5 the native
// backend can genuinely serve generation: when it is the SELECTED backend
// and it is actually generation-capable, its state is the authoritative
// `state` for the UI badge (the llama.cpp fallback stays prewarmed in the
// background but must not own the badge while native serves). When the
// native engine is not serving (not selected, or not capable), the
// llama.cpp engine state remains authoritative exactly as before, and
// native transitions reach the activity feed as "Native engine …" captions
// without touching the badge.

import (
        "context"
        "encoding/json"
        "net/http"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// engineSnapshot is the /api/engine payload.
type engineSnapshot struct {
        State      string   `json:"state"`
        Detail     string   `json:"detail,omitempty"`
        Model      string   `json:"model,omitempty"`
        LoadedPath string   `json:"loadedPath,omitempty"`
        Pid        int      `json:"pid,omitempty"`
        Vision     bool     `json:"vision"`
        Provider   string   `json:"provider"`
        Logs       []string `json:"logs,omitempty"`
        CacheStats any      `json:"cacheStats,omitempty"`
        Timestamp  string   `json:"timestamp"`

        // Backend (v1.1.5) names the backend that serves generation
        // ("llama" or "native" — the single selection policy decides).
        Backend string `json:"backend"`

        // Candidates (v1.7.1) lists the per-backend serving-alternative
        // verdicts (Native Engine / llama.cpp CPU / llama.cpp Vulkan) from
        // the ONE selection authority — the UI renders them, never computes.
        Candidates []llm.BackendCandidateVerdict `json:"candidates,omitempty"`

        // Native (v1.1.5) carries the supervised native engine's status
        // when the native path is enabled (nil otherwise). Purely local
        // reads — the poll path never performs IPC.
        Native *nativeEngineSnapshot `json:"native,omitempty"`

        // 1.1.6 §9/§11 startup experience: a user-facing PHASE for the
        // progress flow (waiting | downloading-engine | loading-model |
        // checking-capabilities | preparing-context | ready) and the
        // verified-readiness proof. Ready requires the process to be
        // alive AND healthy AND the serving model verified — a spawned
        // subprocess alone never reports verified.
        Phase           string `json:"phase"`
        Verified        bool   `json:"verified"`
        VerifiedModel   string `json:"verifiedModel,omitempty"`
        VerifiedContext int    `json:"verifiedContext,omitempty"`
        Degraded        bool   `json:"degraded,omitempty"`

        // v1.2.0: the runtime vision readiness block from the engine's own
        // state machine — loading/ready/degraded/failed with the evidence
        // reason and the paired projector's measured size. Ready is only
        // ever reported because the engine is actually serving with the
        // projector; filenames alone never produce it.
        VisionState          string `json:"visionState,omitempty"`
        VisionReason         string `json:"visionReason,omitempty"`
        VisionProjector      string `json:"visionProjector,omitempty"`
        VisionProjectorName  string `json:"visionProjectorName,omitempty"`
        VisionProjectorBytes int64  `json:"visionProjectorBytes,omitempty"`
        VisionActive         bool   `json:"visionActive,omitempty"`

        // v1.2.3: live asset-download progress (phase, bytes, speed, ETA,
        // source, verification state) from the Download Manager while the
        // engine downloads its llama.cpp archive or a model package. Nil
        // outside downloads.
        Download *downloader.Progress `json:"download,omitempty"`

        // v1.2.5: watchdog auto-restart attempts for the current alive
        // episode (bounded by maxAutoRestarts; reset after a stable healthy
        // episode). Lets the UI report recovery honestly.
        Restarts int `json:"restarts,omitempty"`

        // v1.3.6 (spec §7): first-class engine diagnostics — binary path,
        // recorded/probed version, decoded failure classification, restart
        // count, recent stdout/stderr and the full failure report. The UI
        // renders the REAL engine truth (Ready/Starting/Updating/Failed,
        // exact failure reason) instead of a generic spinner.
        Diagnostics *llm.EngineDiagnostics `json:"diagnostics,omitempty"`

        // v1.5.0 MODEL-FIRST: the deterministic selection state. A local
        // install with NO selected model reports SelectionRequired — the
        // UI gates the composer on it and the Model Selector owns the next
        // step; the engine never loads an arbitrary model on its own.
        SelectionRequired bool `json:"selectionRequired,omitempty"`

        // Selection is the backend-authoritative model-selection state
        // machine (select → analyzing → configuring → loading → ready |
        // failed, plus calibrating) once a selection flow has started.
        Selection *selectionState `json:"selection,omitempty"`

        // Execution (v1.8.5) is the ONE shared execution/evidence ladder
        // composed from the existing authorities (device detection, backend
        // health, the accelerator selection memo, verified model loading,
        // measured generation telemetry, runtime offload lines). Stage is
        // monotone and stops at the first unproven rung — device
        // enumeration can never equal verified execution. Phase 2's deep
        // engine/backend work consumes exactly this structure.
        Execution *llm.ExecutionReport `json:"execution,omitempty"`
}

// nativeEngineSnapshot is the native engine status block (local reads
// only; no IPC on the poll path).
type nativeEngineSnapshot struct {
        Selected      bool    `json:"selected"`
        Available     bool    `json:"available"`
        Path          string  `json:"path,omitempty"`
        State         string  `json:"state"`
        Detail        string  `json:"detail,omitempty"`
        Pid           int     `json:"pid,omitempty"`
        UptimeSeconds float64 `json:"uptimeSeconds,omitempty"`
        Restarts      int     `json:"restarts,omitempty"`

        // FallbackReason (v1.3.2) is non-empty when the user selected the
        // native engine but generation was routed to llama.cpp: the
        // inspectable reason (engine down, no model, model not natively
        // executable). FallbackCount is how many generation requests were
        // routed that way. The fallback is the documented behavior — this
        // field exists so it is never a silent one.
        FallbackReason string `json:"fallbackReason,omitempty"`
        FallbackCount  int    `json:"fallbackCount,omitempty"`
}

// handleEngine serves the authoritative engine snapshot.
func (s *Server) handleEngine(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                return
        }

        writeJSON(w, s.engineSnapshot())
}

func (s *Server) engineSnapshot() engineSnapshot {
        snap := engineSnapshot{
                State:     s.llama.State(),
                Detail:    s.llama.Detail(),
                Pid:       s.llama.Pid(),
                Timestamp: time.Now().UTC().Format(time.RFC3339),
                // v1.2.5: the CURRENT episode's watchdog attempts — the UI
                // can show an honest recovery count next to the detail
                // ("Unexpected exit — restarting 1/3").
                Restarts: s.llama.Restarts(),
        }

        // v1.1.5: effective generation backend per the selection policy.
        snap.Backend = "llama"

        if s.stack != nil && s.stack.Engine() != nil {
                snap.Backend = s.stack.Engine().Name()

                // v1.7.1 (5.7): the per-backend candidate verdicts — the same
                // selection authority, one capability truth.
                snap.Candidates = llm.BackendCandidates(
                        s.src.Load(),
                        s.stack.NativeBackend(),
                        s.stack.LlamaBackend(),
                        nil, // model-side caps are folded into the native probe
                )
        }

        // Phase 5 repair: when the native engine is the backend actually
        // serving generation (selected AND generation-capable — the same
        // policy that routed generation), the badge state, detail, pid,
        // loaded model and logs must come from THAT engine. Otherwise a
        // user with engineBackend=native whose llama.cpp fallback cannot
        // start (offline, no binary) sees "failed" while generation
        // actually works — the exact misleading-state defect this fixes.
        if snap.Backend == "native" && s.native != nil {
                snap.State = s.native.State()
                snap.Detail = s.native.Detail()
                snap.Pid = s.native.Pid()
                snap.LoadedPath = s.native.NativeModelPath()
                snap.Logs = tailStrings(s.native.Logs(), 24)
        }

        if s.src.Load().IsRemote() {
                snap.Provider = "remote"
                snap.Model = s.src.Load().EffectiveModel()

                if snap.State == llm.StateIdle {
                        // A remote provider needs no local engine: report a distinct
                        // state so the UI does not show a misleading "stopped" badge
                        // for remote mode.
                        snap.State = "remote"
                }

                return snap
        }

        snap.Provider = "local"
        snap.Model = s.src.Load().DisplayModel()
        snap.Vision = s.llama.VisionActive()

        // v1.5.0 MODEL-FIRST: a local install with no selected model is in
        // the deterministic pre-selection state — an explicit phase for the
        // UI (the composer gates on it; the Model Selector owns the next
        // step). It is NOT an engine failure: the engine stays idle and no
        // arbitrary model is ever loaded on the user's behalf.
        if s.src.Load().Model == "" {
                snap.SelectionRequired = true
        }

        // v1.5.0 MODEL-FIRST: the selection state rides along once a
        // selection flow has actually run (s.selection != nil); the
        // pre-selection state is fully described by SelectionRequired.
        s.selectionMu.Lock()
        snap.Selection = s.selection
        s.selectionMu.Unlock()

        // v1.2.3: live download progress for the UI (nil outside downloads).
        if dp := s.llama.DownloadProgress(); dp != nil {
                p := *dp
                snap.Download = &p
        }

        // v1.2.0: full runtime vision readiness block.
        if vs := s.llama.VisionStatus(); vs.State != "" {
                snap.VisionState = string(vs.State)
                snap.VisionReason = vs.Reason
                snap.VisionActive = vs.Active
                if vs.ProjectorName != "" {
                        snap.VisionProjector = vs.Projector
                        snap.VisionProjectorName = vs.ProjectorName
                        snap.VisionProjectorBytes = vs.ProjectorBytes
                }
        }

        // 1.1.6 §11: readiness must mean READY — verified model serving,
        // not merely a spawned subprocess. The phase mapping drives the
        // startup progress states in the UI.
        if snap.Backend != "native" {
                snap.VerifiedModel = s.llama.VerifiedModel()
                snap.VerifiedContext = s.llama.VerifiedContext()
                snap.Verified = s.llama.VerifiedReady() && snap.VerifiedContext > 0
        }
        snap.Phase = enginePhase(snap.State, snap.Verified, s.src.Load().IsRemote())
        if snap.SelectionRequired {
                // The deterministic pre-selection phase: the Model Selector
                // owns the next step (distinct from a mere idle engine).
                snap.Phase = "model-selection"
        }
        snap.Degraded = snap.Backend == "llama" && snap.State == llm.StateReady && !snap.Verified

        // When native serves, LoadedPath/Logs were already sourced from
        // the native engine above; only the llama fallback path (and the
        // pre-native v1.1.4 contract) populates them from llama.cpp.
        if snap.Backend != "native" {
                snap.LoadedPath = s.llama.LoadedModel()
                snap.Logs = tailStrings(s.llama.Logs(), 24)
        }

        // v1.3.6 (spec section 7): the diagnostics block rides along on
        // every llama-path snapshot (remote providers have no local engine).
        diag := s.llama.EngineDiagnostics(s.src.Load())
        snap.Diagnostics = &diag

        // v1.8.5 EXECUTION/EVIDENCE LADDER: compose the ONE shared report
        // from the existing authorities — cheap, honest reads only (the
        // accelerator resolution comes from the memo written by the /api/perf
        // selection path; hardware presence is the cached snapshot; nothing
        // spawns on this poll path). Detection is NEVER conflated with
        // verified execution: the pure composer stops the ladder at the
        // first unproven rung and names it in Gaps.
        snap.Execution = s.composeExecutionReport(snap)

        // v1.1.5: native engine status block (local reads only).
        if s.native != nil {
                native := &nativeEngineSnapshot{
                        Selected:  s.src.Load().NativeBackendEnabled(),
                        Available: s.native.Available(),
                        Path:      s.native.Path(),
                        State:     s.native.State(),
                        Detail:    s.native.Detail(),
                        Pid:       s.native.Pid(),
                        Restarts:  s.native.Restarts(),
                }

                // v1.3.2 observable fallback: when the native engine was
                // selected but is NOT the serving backend, surface the
                // recorded reason + count from the runtime seam.
                if native.Selected && snap.Backend != "native" && s.stack != nil {
                        if fb := s.stack.NativeFallback(); fb.Reason != "" {
                                native.FallbackReason = fb.Reason
                                native.FallbackCount = fb.Count
                        }
                }

                if started := s.native.StartedAt(); !started.IsZero() {
                        native.UptimeSeconds = time.Since(started).Seconds()
                }

                snap.Native = native

                // 1.1.6 §11: when the native engine SERVES generation, its
                // loaded state is the readiness proof (its ModelInfo is the
                // verified model + context source).
                if snap.Backend == "native" {
                        if res, err := s.native.ModelInfo(context.Background()); err == nil &&
                                res.Loaded && res.Model != nil {
                                snap.Verified = true
                                snap.VerifiedContext = int(res.Model.ContextLength)
                        }
                        snap.Phase = enginePhase(snap.State, snap.Verified, false)
                }
        }

        if s.stack != nil && s.stack.Cache != nil {
                snap.CacheStats = s.stack.Cache.Stats()
        }

        return snap
}

// composeExecutionReport builds the v1.8.5→v1.8.6 execution/evidence
// ladder for the engine snapshot from CHEAP, honest reads of the
// existing authorities — no spawning, no new probes, no second policy
// engine:
//
//   - detected:     hardware presence (cached snapshot) or the Vulkan
//     backend's presence — pure DETECTION, never execution;
//   - backend-available: the serving engine's authoritative state;
//   - device-selected:   the accelerator resolution through the ONE
//     accessor (currentAcceleratorResolution — the same authority and
//     evidence path /api/perf uses; a stale memo is refreshed HERE, so
//     the engine surface no longer depends on the perf page having
//     polled first);
//   - model-loaded:      the engine's verified-model proof;
//   - generation-executed: the measured perf ring (real generations only);
//   - execution-evidence: the runtime offload line the engine printed;
//   - verified:          the selection's execution-verification contract.
//
// The pure composer (llm.ComposeExecutionReport) enforces monotonicity;
// this helper only GATHERS inputs. Unknowns stay unknown. A later poll
// can only ever reflect CURRENT reality: the memo refresh is keyed to
// the exact engine/model/evidence identity (acceleratorInputSignature),
// so the stage cannot move backwards because a UI poll happened later —
// only because the serving reality itself changed.
func (s *Server) composeExecutionReport(snap engineSnapshot) *llm.ExecutionReport {
        cfg := s.src.Load()

        hw := hardware.Snapshot(cfg)

        devicesKnown := len(hw.GPUs) > 0 || hw.NPU != nil || llm.VulkanAvailable(cfg)

        backendHealthy := snap.State == llm.StateReady ||
                snap.State == llm.StateRunning ||
                snap.State == llm.StateBusy

        // Native-serving snapshots keep their own verified-model proof
        // (snap.VerifiedModel above); the llama path fills it too.
        deviceSelected := ""
        selectionVerified := false
        verification := ""

        // v1.8.6: the ONE accessor — /api/engine refreshes a stale memo
        // itself instead of reading whatever the perf poll left behind.
        if res := s.currentAcceleratorResolution(snap.LoadedPath); res != nil {
                deviceSelected = string(res.Selected)
                selectionVerified = res.ExecutionVerified
                verification = res.Verification
        }

        perf := llm.SnapshotEnginePerf()

        report := llm.ComposeExecutionReport(llm.ExecutionReportInputs{
                DevicesKnown:               devicesKnown,
                BackendHealthy:             backendHealthy,
                BackendName:                snap.Backend,
                EngineTag:                  updater.InstalledEngineTag(cfg),
                DeviceSelected:             deviceSelected,
                SelectionExecutionVerified: selectionVerified,
                Verification:               verification,
                VerifiedModel:              snap.VerifiedModel,
                GenerationSamples:          perf.SamplesAvailable,
                OffloadEvidence:            s.llama.OffloadEvidence(),
        })

        return &report
}

// enginePhase maps the raw engine state onto the 1.1.6 §9 startup flow
// states the UI renders as real progress:
//
//      Starting → Downloading engine → Loading model →
//      Checking capabilities → Preparing context → Ready
func enginePhase(state string, verified bool, remote bool) string {
        if remote {
                return "ready"
        }

        switch state {
        case llm.StateIdle:
                return "waiting"
        case llm.StateDownloading:
                return "downloading-engine"
        case llm.StateStarting:
                return "loading-model"
        case llm.StateReady, llm.StateRunning, llm.StateBusy:
                if verified {
                        return "ready"
                }
                return "checking-capabilities"
        case llm.StateStopping:
                return "stopping"
        case llm.StateStopped, llm.StateFailed:
                return state
        default:
                return state
        }
}

// engineActivity converts one engine transition into an agent.Activity so
// it flows through the normal hub machinery. v1.2.3: live Download Manager
// progress (when present) rides along so WS consumers can render real
// bytes/speed/ETA instead of a bare "downloading" label.
func engineActivity(ev llm.EngineEvent) agent.Activity {
        detail := map[string]any{
                "state":    ev.State,
                "previous": ev.Previous,
                "model":    ev.Model,
                "detail":   ev.Detail,
        }
        if ev.Download != nil {
                detail["download"] = *ev.Download
        }
        return agent.Activity{
                Type:      "engine",
                Caption:   engineCaption(ev.State),
                Timestamp: ev.Timestamp,
                Detail:    detail,
        }
}

// nativeEngineActivity converts one native engine transition; the caption
// is explicitly "Native engine …" so the activity feed distinguishes the
// two engines while the UI badge keeps reading the llama snapshot.
func nativeEngineActivity(ev llm.EngineEvent) agent.Activity {
        act := engineActivity(ev)
        act.Caption = nativeEngineCaption(ev.State)
        return act
}

// watchEngineEvents subscribes to the engine state machines once per
// server and fans transitions out to every live run hub and standby
// connection. v1.1.5: it ALSO watches the native engine's transitions
// when the native path is enabled. It returns when stop closes (server
// shutdown).
func (s *Server) watchEngineEvents(stop <-chan struct{}) {
        events, unsubscribe := s.llama.SubscribeEvents()
        defer unsubscribe()

        var nativeEvents <-chan llm.EngineEvent
        var nativeUnsubscribe func()

        if s.native != nil {
                nativeEvents, nativeUnsubscribe = s.native.SubscribeEvents()
                defer nativeUnsubscribe()
        }

        for {
                select {
                case <-stop:
                        return

                case ev, ok := <-events:
                        if !ok {
                                return
                        }

                        s.broadcastEngineEvent(ev)

                case ev, ok := <-nativeEvents:
                        if !ok {
                                // Native subscription closed: keep serving
                                // llama events.
                                nativeEvents = nil
                                continue
                        }

                        act := nativeEngineActivity(ev)

                        s.broadcastActivity(act, ev)
                }
        }
}

// broadcastEngineEvent pushes one llama.cpp transition to run hubs and
// standby connections. Standby connections receive the pre-encoded frame
// on their dedicated channel; the standby loop writes it without
// re-parking.
func (s *Server) broadcastEngineEvent(ev llm.EngineEvent) {
        act := engineActivity(ev)
        s.broadcastActivity(act, ev)
}

// broadcastActivity pushes one engine transition activity (shared by both
// engines) to every run hub and standby connection.
func (s *Server) broadcastActivity(act agent.Activity, ev llm.EngineEvent) {
        s.runsMu.Lock()

        for _, rs := range s.runs {
                if rs != nil && rs.hub != nil {
                        rs.hub.publish(act)
                }
        }

        s.runsMu.Unlock()

        frame, err := json.Marshal(map[string]any{
                "type":      act.Type,
                "caption":   act.Caption,
                "state":     ev.State,
                "previous":  ev.Previous,
                "model":     ev.Model,
                "detail":    ev.Detail,
                "timestamp": ev.Timestamp,
        })

        if err != nil {
                return
        }

        s.standbyMu.Lock()

        for _, conns := range s.standby {
                for _, sc := range conns {
                        select {
                        case sc.engineCh <- frame:
                        default:
                        }
                }
        }

        s.standbyMu.Unlock()
}

// engineCaption renders the human sentence for one engine state.
func engineCaption(state string) string {
        switch state {
        case llm.StateIdle:
                return "Engine idle"
        case llm.StateDownloading:
                return "Downloading llama.cpp engine…"
        case llm.StateStarting:
                return "Starting llama.cpp and loading the model…"
        case llm.StateReady:
                return "Engine ready — model loaded"
        case llm.StateRunning:
                return "Engine running"
        case llm.StateBusy:
                return "Engine busy — inference in flight"
        case llm.StateStopping:
                return "Engine stopping…"
        case llm.StateStopped:
                return "Engine stopped"
        case llm.StateFailed:
                return "Engine failed — see engine logs"
        default:
                return "Engine: " + state
        }
}

// nativeEngineCaption renders the human sentence for one native engine
// state (same state vocabulary; distinct captions for the activity feed).
func nativeEngineCaption(state string) string {
        switch state {
        case llm.StateIdle:
                return "Native engine idle"
        case llm.StateStarting:
                return "Starting native engine…"
        case llm.StateReady:
                return "Native engine ready (supervised)"
        case llm.StateRunning:
                return "Native engine running"
        case llm.StateBusy:
                return "Native engine busy"
        case llm.StateStopping:
                return "Native engine stopping…"
        case llm.StateStopped:
                return "Native engine stopped"
        case llm.StateFailed:
                return "Native engine failed — llama.cpp fallback active"
        default:
                return "Native engine: " + state
        }
}

// tailStrings returns the last n strings, preserving order.
func tailStrings(in []string, n int) []string {
        if len(in) <= n {
                return in
        }

        return in[len(in)-n:]
}

// handleEngineRediscover is the explicit Repair/Rediscover action
// (v1.3.6, spec §9): re-runs the discovery ladder (managed dir → cache
// → Tier 1 → bounded Tier-2 scan), imports a validated candidate when
// one exists, and restarts the engine. It never runs automatically on
// the startup path and never downloads anything — when no local engine
// exists the honest error tells the caller a download is required.
func (s *Server) handleEngineRediscover(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                return
        }

        outcome, err := s.llama.Rediscover()
        if err != nil {
                writeJSON(w, map[string]any{
                        "ok":      false,
                        "outcome": outcome,
                        "error":   err.Error(),
                })

                return
        }

        writeJSON(w, map[string]any{
                "ok":      true,
                "outcome": outcome,
        })
}
