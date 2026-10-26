// Package api exposes the JSON REST+WS API of the Go core
// that the UI talks to.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/aisystem"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/artifacts"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/attachments"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/continuum"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/customtools"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/gitclone"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/histref"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/installer"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	nativeengine "github.com/Parsaetak/SHEYTAN-local-agent/internal/native/engine"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recall"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/runtime"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/studio"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"

	"github.com/gorilla/websocket"
)

// Server bundles everything the HTTP handlers need.
//
// The runtime stack is the single source of truth for the agent backend.
// This keeps the HTTP/API surface feature-identical with the CLI and desktop
// runtime instead of constructing a second independent orchestrator.
type Server struct {
	// src is the live configuration source shared with the runtime stack.
	// (v1.1.4: handlers snapshot it per request; the old shared mutable
	// *Config raced the patch handler against active runs.)
	src       *config.Source
	store     *sessions.Store
	stack     *runtime.Stack
	orch      *agent.Orchestrator
	llama     *llm.LlamaServer
	native    *nativeengine.Engine
	installer *installer.Manager
	sys       *sysinfo.SysInfo
	recall    *recall.Engine // v1.0.2 persistent memory over past chats

	// continuum evaluates chapter rollover after long sessions
	// (v1.1.4: the manager was fully implemented and tested but never
	// wired into any production path before).
	continuum *continuum.Manager

	// updateCancel stops the scheduled engine-update loop on Close
	// (v1.1.4: updater.RunScheduled existed with zero callers).
	// updateDone is RunScheduled's completion channel: closed once the
	// loop observed cancellation AND any in-flight pass finished —
	// Close waits on it (bounded) so the updater can never outlive the
	// server and write staging files behind its back.
	// updateMu owns both fields (v1.7.3): repeated EnsureSetup calls
	// (test 6's repeated-startup contract) must never overwrite the
	// previous loop's cancel/done handles — an overwritten handle
	// leaks the older loop, whose late config writes then race the
	// process teardown. Publication and teardown both go through the
	// mutex; the loop itself starts at most once per server lifetime
	// (single-flight, mirroring the startup maintenance gate).
	updateMu     sync.Mutex
	updateCancel context.CancelFunc
	updateDone   <-chan struct{}

	// v1.10.1 (P0 teardown determinism, Windows run 38152579695):
	// runWG tracks every spawned run goroutine (fresh and resumed) so
	// Close can JOIN them deterministically before releasing the
	// runtime stack. The observed failure — t.TempDir RemoveAll
	// failing with "The directory is not empty" while a post-terminal
	// deferred cleanup (memory-manager run-end bookkeeping) was still
	// writing inside the data dir — is a JOIN race, not an assertion
	// problem: the run goroutine's cleanup runs AFTER the terminal
	// frame is published, and no test or shutdown path waited for it.
	// closing refuses new run registrations once Close has begun, so
	// the Add-before-Wait ordering of runWG is race-free by
	// construction (both the guard read and Add happen under runsMu;
	// Close sets closing before taking runsMu).
	runWG   sync.WaitGroup
	closing atomic.Bool

	// lastAppUpdate caches the newest app-update check (v1.2.0) so
	// /api/update/status never blocks on the network. Written only by
	// /api/update/check and /api/update/download; read by status.
	lastAppUpdate atomic.Value

	// v1.2.3: the asynchronous app-update staging job (Download Manager).
	// appUpdateJob is the live handle for progress + cancel; appUpdateStaging
	// claims the single slot; appUpdateCancel stops the transfer goroutine
	// (also called from Close so staging never outlives the server).
	appUpdateJob     atomic.Pointer[downloader.Job]
	appUpdateStaging atomic.Bool
	appUpdateCancel  atomic.Pointer[context.CancelFunc]

	// v1.3.0 GitHub clone job state — the same single-slot pattern as the
	// app-update staging above: one clone at a time, live status through
	// the atomic envelope, cancellation through the stored job.
	cloneJob     atomic.Pointer[gitclone.Job]
	cloneRunning atomic.Bool
	cloneStatus  atomic.Pointer[cloneStatusEnvelope]

	// v1.11.0: the optional 3D Studio viewer (internal/studio). The
	// server owns the lifecycle — started on demand via /api/studio/start,
	// stopped via /api/studio/stop AND at Close time (no orphaned
	// loopback listener). Lazy: nil until first start.
	studioViewer *studio.Viewer

	// active runs: sessionID → runState
	runsMu sync.Mutex
	runs   map[string]*runState

	// v1.9.5 (P1): engine-mutation admission claim (guarded by runsMu).
	// While true, new run admission is refused deterministically — the
	// llm.EngineAdmission seam that closes the engine-transaction /
	// run-admission TOCTOU. See BeginEngineMutation in selection.go.
	engineMutation bool

	// v1.5.0 MODEL-FIRST: the backend-authoritative model-selection
	// state machine (selection.go). selectionMu guards the record;
	// calibrating is the single-slot gate for the bounded automatic
	// performance check.
	selectionMu sync.Mutex
	selection   *selectionState
	calibrating atomic.Bool

	// v1.8.5→v1.8.6 EXECUTION/EVIDENCE LADDER: memo of the LAST
	// accelerator resolution composed by the ONE selection authority
	// (gatherAcceleratorEvidence + accelerator.Resolve through
	// currentAcceleratorResolution). BOTH surfaces consume it:
	//   - /api/perf builds/refreshes it on its poll;
	//   - /api/engine reads it AND refreshes it when its input signature
	//     is stale — the engine surface no longer depends on the perf
	//     page happening to poll first (the v1.8.6 unification).
	// resolutionSig records the INPUTS the memo was computed from
	// (engine tag + variant + requested profile + loaded model + offload
	// evidence + probe state): a signature mismatch means the memo is
	// STALE and is recomputed before any consumer can read it — no
	// stale poll-only snapshot can claim a current backend. This is a
	// read-through cache of the authority's output, NOT a second
	// selection/policy engine; nil = honest unknown until the first
	// resolution runs.
	resolutionMu   sync.Mutex
	lastResolution *accelerator.Resolution
	resolutionSig  string

	// outcomes (v1.2.6) is the bounded authoritative run-state registry:
	// the last few terminal outcomes per session, replayed on the idle
	// sentinel so a late-attaching socket can finalise deterministically
	// instead of staying stuck on a run it never observed.
	outcomes *runRegistry

	// v1.1.2: idle WebSocket standby registry — sessionID → connections.
	// Before this, an activity WebSocket with no active run was closed
	// immediately after one "idle" sentinel, so the UI permanently showed
	// "Offline" between runs. Idle connections now stay open, and the
	// instant a run starts they are woken and attached to the run hub.
	// v1.1.3: each standby connection also owns an engineCh fed by the
	// engine event bus so idle UIs still observe engine transitions.
	standbyMu sync.Mutex
	standby   map[string][]*standbyConn

	// engineStop terminates the engine event fan-out on Close.
	engineStop chan struct{}
	engineDone chan struct{}

	// v1.9.4 (P0): the desktop loopback activity transport — see
	// loopback.go. The packaged desktop builds serve the webview through
	// resource-request interception (HTTP only), which never carries
	// WebSocket handshakes; the loopback listener gives the SAME API
	// handler (one authority, one /ws/activity endpoint) a reachable
	// transport. Empty origin = no loopback listener (headless/browser
	// serving stays same-origin).
	loopbackMu     sync.Mutex
	loopbackSrv    *http.Server
	loopbackOrigin string
	loopbackToken  string

	// v1.6.0 P0 — THE STARTUP MAINTENANCE GATE (maintenance.go).
	// One authoritative coordinator owns the startup decision
	// "maintain the engine BEFORE ever starting it": the gate runs
	// the maintenance check (and any transactional update) to
	// completion, then releases BOTH the model prewarm and the
	// scheduled updater's first pass. Repeated EnsureSetup calls
	// reuse the same gate (no duplicate maintenance/prewarm).
	gateMu sync.Mutex
	gate   *maintenanceGate

	// prewarmOnce bounds the automatic startup prewarm to exactly
	// one per server lifetime (spec §5 test 6).
	prewarmOnce sync.Once

	// prewarmHook is a TEST seam standing in for
	// stack.PrewarmLLM (nil in production).
	prewarmHook func()

	// seams holds the maintenance gate's test seams (nil in
	// production — see maintenanceSeams).
	seams *maintenanceSeams

	// v1.6.0 P3: the custom tools store — definitions persist under
	// <DataDir>/custom-tools and register as FIRST-CLASS tools in the
	// ONE orchestrator registry (customtools.go).
	ctStore *customtools.Store

	// v1.7.0: the Automation / Tasks surfaces — task-scoped custom
	// tools (the SAME customtools executor) and the durable
	// task/run artifact registry (the SAME artifacts tracker root).
	taskTools   *customtools.TaskStore
	artRegistry *artifacts.TaskRegistry

	// maintenanceSave persists the gate's LastUpdateCheck mutation
	// (production wires config.Save; tests may stub it).
	maintenanceSave func()

	// v1.6.1 LOG DISCIPLINE: /api/perf is polled every few seconds and
	// recomputes the accelerator resolution each time. Logging the
	// full resolution at INFO on every poll flooded app.log with
	// hundreds of identical lines (the v1.6.0 "unreadable normal
	// logs" complaint). accelMu + lastAccelDesc cache the last
	// logged description: an UNCHANGED resolution logs at DEBUG (kept
	// for Advanced diagnostics), only a CHANGED resolution logs at
	// INFO. The same discipline applies to the device-enumeration
	// warning.
	accelMu           sync.Mutex
	lastAccelDesc     string
	lastAccelEnumWarn string
}

type runState struct {
	cancel context.CancelFunc
	hub    *activityHub

	// live (v1.2.6 continuation) is the AUTHORITATIVE per-run state —
	// phase, running, cumulative response/reasoning snapshots, latest
	// status, monotonic sequence, terminal outcome. A WebSocket that
	// attaches at ANY point receives a snapshot frame built from it
	// before live events continue: a reconnect never has to wait for a
	// future token to discover the run's state.
	live *runLive

	// ctrl (v1.7.4) is the per-run pause control shared with the
	// orchestrator. The pause handler sets it; the orchestrator
	// observes it at safe boundaries. It survives a pause/resume cycle
	// so a resumed run can be paused again.
	ctrl *agent.RunControl

	// ctrlMu (v1.7.5) serializes the PAUSED-run control operations
	// (edit / resume / stop-while-paused) that combine a CAS revision
	// check with durable filesystem work. An edit must never be able
	// to interleave with a resume or a stop between its revision
	// validation and its checkpoint commit — that interleaving is the
	// ordering hazard that let memory, transcript and checkpoint
	// disagree about the authoritative revision.
	ctrlMu sync.Mutex

	// liveCancel (v1.7.5) is the cancel of the CURRENT generation. A
	// resumed run builds a FRESH context (the pause was not an abort —
	// nothing from the old one carries over), so the registration-time
	// cancel no longer reaches the generation after a resume. /api/abort
	// cancels through cancelCurrent, which always reaches the live
	// generation; guarded by cancelMu because the resume goroutine
	// installs it while handler goroutines may abort.
	cancelMu   sync.Mutex
	liveCancel context.CancelFunc
}

// setLiveCancel installs the cancel of the current generation (v1.7.5).
func (rs *runState) setLiveCancel(cancel context.CancelFunc) {
	rs.cancelMu.Lock()
	rs.liveCancel = cancel
	rs.cancelMu.Unlock()
}

// cancelCurrent cancels the CURRENT generation: the registration cancel
// for a fresh run, the resumed context's cancel after a resume.
func (rs *runState) cancelCurrent() {
	rs.cancelMu.Lock()
	cancel := rs.liveCancel
	if cancel == nil {
		cancel = rs.cancel
	}
	rs.cancelMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// activityHub broadcasts activity events to all WebSocket subscribers of a
// single active run. Each client receives its own bounded, conflation-aware
// delivery queue so clients do not consume events from one another.
type activityHub struct {
	mu      sync.RWMutex
	clients map[int]*activitySub
	nextID  int
	closed  bool
}

func newActivityHub() *activityHub {
	return &activityHub{
		clients: make(map[int]*activitySub),
	}
}

func (h *activityHub) subscribe() (int, *activitySub, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		dead := newActivitySub(0)
		dead.close()
		return 0, dead, func() {}
	}

	id := h.nextID
	h.nextID++

	sub := newActivitySub(subscriberQueueCap)
	h.clients[id] = sub

	var once sync.Once

	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()

			if existing, ok := h.clients[id]; ok {
				delete(h.clients, id)
				existing.close()
			}
		})
	}

	return id, sub, unsubscribe
}

func (h *activityHub) publish(ev agent.Activity) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.clients {
		sub.offer(ev)
	}
}

func (h *activityHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}

	h.closed = true

	for id, sub := range h.clients {
		delete(h.clients, id)
		sub.close()
	}
}

// subscriberQueueCap bounds one subscriber's pending delivery queue. The
// v1.2.x hub used a 128-deep channel with the same numeric bound.
const subscriberQueueCap = 128

// isConflatableKind reports whether one activity kind is a CUMULATIVE
// snapshot or rolling telemetry whose NEWEST value subsumes every older
// buffered copy: response/assistant_delta and reasoning/thinking_delta
// captions are full cumulative snapshots by the orchestrator contract, and
// status is a rolling caption. For these kinds an older buffered copy can
// be discarded without losing information — the newest frame carries the
// entire state.
func isConflatableKind(t string) bool {
	switch t {
	case "response", "assistant_delta", "reasoning", "thinking_delta", "status":
		return true
	}
	return false
}

// activitySub is ONE subscriber's bounded, conflation-aware delivery queue.
//
// THE v1.8.5 REPAIR (the remaining P0 live-streaming bottleneck): the
// v1.2.x hub delivered through a plain buffered channel and, when a
// subscriber's buffer was FULL, DROPPED THE NEWEST EVENT ("a slow WebSocket
// must never block the agent run. Only that slow subscriber may lose an
// event."). For cumulative event kinds that policy is exactly backwards:
// the dropped event is the one carrying the FULL text so far, while the
// buffer keeps holding older, SHORTER snapshots. A subscriber whose writer
// stalls — precisely the failing runtime class where the browser's
// transport backpressures the socket while the JS event loop, React and
// the generation itself keep running — therefore watches the buffer fill
// with stale snapshots and every NEWER snapshot get discarded: streamed
// text freezes at whatever was last delivered and only reappears when the
// run ends (Stop included), the hub closes and the reconnect replay
// delivers the final run_snapshot. Same failure surface as the v1.8.4
// MessageChannel loss, on the SERVER side of the wire.
//
// The queue keeps the same non-blocking publish contract (a slow
// subscriber never blocks the agent run) but replaces drop-NEWEST with
// CONFLATION: when the queue is full, the OLDEST conflatable (cumulative)
// event is evicted first — the newest snapshot subsumes it — and only a
// queue with no conflatable events left evicts its oldest entry. Order is
// preserved; the seq replay contract is unchanged (the newest surviving
// event always carries the highest sequence); terminal events are never
// PREFERENTIALLY evicted (a done/aborted/error frame only drops when the
// queue is entirely non-conflatable, and the idle-sentinel/settlement
// recovery paths own that case exactly as before).
//
// Backpressure on the WRITE side stays bounded: the reader writes with a
// generous deadline so a wedged transport is torn down deterministically
// instead of parking the goroutine forever, and conflation bounds the
// queue's memory while it is stalled.
type activitySub struct {
	mu        sync.Mutex
	buf       []agent.Activity
	wake      chan struct{}
	closed    chan struct{}
	closedNow bool
}

func newActivitySub(capacity int) *activitySub {
	return &activitySub{
		buf:    make([]agent.Activity, 0, capacity+1),
		wake:   make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
}

// offer enqueues one event without ever blocking the publisher. When the
// queue is at capacity the oldest conflatable event is evicted (its newest
// sibling subsumes it); a queue with no conflatable events evicts its
// oldest entry (bounded memory — the previous behavior dropped the newest
// instead, which is strictly worse for every kind).
func (s *activitySub) offer(ev agent.Activity) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closedNow {
		return
	}

	if len(s.buf) >= subscriberQueueCap {
		s.evictOldestConflatableLocked()
	}

	s.buf = append(s.buf, ev)

	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// evictOldestConflatableLocked removes the oldest conflatable event when
// one exists, otherwise the oldest buffered event. Order of the survivors
// is preserved.
func (s *activitySub) evictOldestConflatableLocked() {
	victim := 0

	for i := range s.buf {
		if isConflatableKind(s.buf[i].Type) {
			victim = i
			break
		}
	}

	s.buf = append(s.buf[:victim], s.buf[victim+1:]...)
}

// next blocks until one event is available or the subscription is closed
// (every buffered event is drained BEFORE close is reported — a subscriber
// that lagged behind still receives the conflation survivors, ending with
// the newest cumulative snapshot and the terminal marker).
func (s *activitySub) next() (agent.Activity, bool) {
	for {
		s.mu.Lock()

		if len(s.buf) > 0 {
			ev := s.buf[0]
			s.buf = s.buf[1:]
			s.mu.Unlock()
			return ev, true
		}

		if s.closedNow {
			s.mu.Unlock()
			return agent.Activity{}, false
		}

		s.mu.Unlock()

		select {
		case <-s.wake:
		case <-s.closed:
		}
	}
}

// close marks the subscription finished and releases every blocked reader.
// Buffered events remain drainable (next drains before reporting closure).
func (s *activitySub) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closedNow {
		s.closedNow = true
		close(s.closed)
	}
}

// standbyConn carries the two channels one idle activity connection waits
// on: the run-start wake and pre-encoded engine event frames.
type standbyConn struct {
	wake     chan struct{}
	engineCh chan []byte
}

// enterStandby registers a standby connection for an idle activity
// connection. The wake channel is closed by wakeStandby the moment a run
// starts for the session; engineCh receives pre-encoded engine frames so
// idle UIs still observe engine transitions.
func (s *Server) enterStandby(sessionID string) *standbyConn {
	sc := &standbyConn{
		wake:     make(chan struct{}),
		engineCh: make(chan []byte, 8),
	}

	s.standbyMu.Lock()
	s.standby[sessionID] = append(s.standby[sessionID], sc)
	s.standbyMu.Unlock()

	return sc
}

// leaveStandby removes a standby connection that is no longer waiting (the
// client disconnected, or the connection was already woken).
func (s *Server) leaveStandby(sessionID string, sc *standbyConn) {
	s.standbyMu.Lock()
	defer s.standbyMu.Unlock()

	waiting := s.standby[sessionID]

	kept := waiting[:0]

	for _, w := range waiting {
		if w != sc {
			kept = append(kept, w)
		}
	}

	if len(kept) == 0 {
		delete(s.standby, sessionID)
	} else {
		s.standby[sessionID] = kept
	}
}

// wakeStandby releases every idle connection waiting on a session so they
// can attach to the freshly registered run hub.
func (s *Server) wakeStandby(sessionID string) {
	s.standbyMu.Lock()
	waiting := s.standby[sessionID]
	delete(s.standby, sessionID)
	s.standbyMu.Unlock()

	for _, sc := range waiting {
		close(sc.wake)
	}
}

// New constructs a fully-wired server from the canonical runtime stack.
func New(cfg *config.Config) (*Server, error) {
	stack := runtime.NewStack(cfg)
	// v1.2.4: one shared session store (owned by the runtime stack) so the
	// memory manager can bound its hot cache and every consumer sees the
	// same warmed state.
	store := stack.Sessions

	s := &Server{
		src:       stack.Src,
		store:     store,
		stack:     stack,
		orch:      stack.Orch,
		llama:     stack.Llama,
		native:    stack.Native,
		installer: installer.New(cfg),
		runs:      make(map[string]*runState),
		outcomes:  newRunRegistry(),
		standby:   make(map[string][]*standbyConn),
		// v1.2.6 continuation: startup must NEVER block on the deep
		// hardware probe. The previous code called sysinfo.Probe()
		// here — the DEEP probe (ONE batched PowerShell/CIM
		// invocation on Windows, seconds) ran synchronously inside
		// New(), before the listener even bound. The fast snapshot
		// (in-process, sub-millisecond) serves immediately; the deep
		// probe warms in the BACKGROUND (hardware.WarmDeep in
		// EnsureSetup, single-flight) and merges into every
		// ProbeFast() reader as it lands.
		sys:        sysinfo.ProbeFast(),
		recall:     stack.Recall,
		continuum:  continuum.NewManager(store, cfg.SessionsDir),
		engineStop: make(chan struct{}),
		engineDone: make(chan struct{}),

		// v1.6.0 P3: the gate persists its LastUpdateCheck
		// mutation through the live source + config file — set
		// at construction (never raced by EnsureSetup).
		maintenanceSave: func() {
			next := stack.Src.Load()
			_ = config.Save(next.ConfigPath(), next)
		},
	}

	// v1.6.0 P3: open the custom tools store under the EXISTING
	// application data root and register every ENABLED definition
	// as a first-class tool in the ONE registry. A store failure is
	// NON-FATAL (logged, surfaced through the API as unavailable).
	if ctStore, ctErr := customtools.NewStore(cfg.DataDir); ctErr != nil {
		logging.Default().Warn("api", "custom tools unavailable: %v", ctErr)
	} else {
		s.ctStore = ctStore
		s.registerCustomTools()
	}

	// v1.7.0: the task-scoped tool layer and the durable artifact
	// registry. The RUNTIME owns the instances (task runs write
	// through them); the API shares them when a stack exists and
	// opens its own otherwise (tests without a stack). Both are
	// NON-FATAL when unavailable (the API reports them honestly).
	if s.stack != nil {
		s.taskTools = s.stack.TaskTools
		s.artRegistry = s.stack.Artifacts
	}

	if s.taskTools == nil {
		if taskTools, ttErr := customtools.NewTaskStore(cfg.DataDir); ttErr != nil {
			logging.Default().Warn("api", "task tools unavailable: %v", ttErr)
		} else {
			s.taskTools = taskTools
		}
	}

	if s.artRegistry == nil {
		artRegistry := artifacts.NewTaskRegistry(cfg.DataDir)
		if err := artRegistry.Load(); err != nil {
			logging.Default().Warn("api", "artifact registry: %v", err)
		}
		s.artRegistry = artRegistry
	}

	// v1.7.6 (§7): converge any interrupted paused-run edit BEFORE anything
	// can observe it. Recovery scans the EXISTING paused-run directory for
	// edit journals, validates their integrity, and deterministically
	// completes or rolls back each one against the durable authorities
	// (transcript + checkpoint). After this point every paused run exposed
	// by /api/run/paused, /api/run/edit and /api/run/resume is coherent.
	// (Absent dir / no journals → a no-op read; per-record corruption fails
	// CLOSED — quarantined with actionable logging, never guessed.)
	s.RecoverEditTransactions()

	// v1.9.4 (P1): install the run-coordination seam on the engine
	// lifecycle owner. An engine variant transaction must never stop a
	// RUNNING engine while a run is active — the caller-side
	// eligibility check happens once, before a transaction that can
	// run for many minutes; the destructive boundary re-checks here.
	// v1.9.5 (P1): install the ADMISSION seam as well — it makes the
	// check-then-act window between the transaction's gate check and
	// its destructive boundary airtight (see llm.EngineAdmission):
	// while a transaction holds the mutation claim, new run admission
	// is refused deterministically, so no accepted run can lose its
	// serving engine across candidate/update/rollback.
	s.llama.SetRunGate(s.anyRunActive)
	s.llama.SetAdmissionControl(s)

	// v1.1.3: the engine event bus fans authoritative state transitions
	// to every WebSocket as they happen.
	go func() {
		defer close(s.engineDone)
		s.watchEngineEvents(s.engineStop)
	}()

	// v1.8.6: warm the accelerator resolution memo ONCE in the
	// background so the FIRST /api/engine poll does not pay the
	// one-time enumeration/hardware probe cost on the UI's critical
	// path. Bounded and best-effort: a failure here only means the
	// first poll computes the resolution itself (as before).
	go func() {
		defer func() { recover() }() // never fatal — it is a warm-up
		_ = s.currentAcceleratorResolution(s.llama.LoadedModel())
	}()

	return s, nil
}

// EnsureSetup runs the installer, creates directories, arms the STARTUP
// MAINTENANCE GATE (v1.6.0 P0) and releases the model prewarm and the
// scheduled engine-update loop strictly AFTER that gate completes.
//
// THE v1.5.1 DEFECT THIS REPAIRS: the prewarm used to fire here
// directly and the scheduled updater started afterwards with an
// immediate pass — so a due update could STOP the freshly-booted engine
// mid-startup (the observed "engine ready → updater stops engine →
// downloads" sequence). The gate now performs the maintenance decision
// (and any transactional update) FIRST; prewarm and the updater's
// first pass are released together, after, through explicit
// synchronization — never sleeps or timing assumptions.
func (s *Server) EnsureSetup() error {
	cfg := s.src.Load()

	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	if _, _, err := s.installer.EnsureRun(false); err != nil {
		return err
	}

	// v1.2.6: warm the deep hardware probe in the background (ONE batched
	// CIM invocation on Windows — replaced the ~7 sequential PowerShell
	// spawns that measured 5.4 s). The UI never waits for it: fast facts
	// render immediately, deep facts land when the probe finishes.
	hardware.WarmDeep()

	// v1.6.0 P0: THE STARTUP MAINTENANCE GATE — the engine maintenance
	// decision (and any transactional update) completes BEFORE any
	// engine process is started or prewarmed. The gate runs in the
	// background so the HTTP surface boots immediately and /api/maintenance
	// exposes its live phase; the prewarm below WAITS on the gate.
	gate := s.startStartupMaintenance()

	// v1.7.2 (P0): the AUTO GPU candidate bootstrap — ONE bounded
	// Vulkan probe per boot, strictly AFTER the maintenance gate
	// (never racing the maintenance transaction) and in the
	// background (never blocking startup). The eligibility gates and
	// the bounded-state logic live in gpu_autoprobe.go /
	// internal/llm/variant_auto.go; the transaction itself is the
	// existing variant authority.
	go s.maybeAutoProvisionVulkan(gate.Done())

	// v1.1.3 — THE acceptance requirement: the application owns the engine
	// lifecycle. A clean launch must reach a healthy model without any
	// manual llama.cpp intervention — but ONLY after the maintenance gate
	// released startup permission (v1.6.0 P0 ordering invariant), and only
	// with an explicit model selection (v1.5.0 model-first).
	if cfg.LlamaAutoStart && s.stack != nil {
		s.prewarmAfterGate()
	}

	// v1.1.4: the scheduled engine-update loop is live. RunScheduled was
	// fully implemented (immediate pass when due, re-check every 6 h) but
	// had zero callers — the "update (scheduled: daily/weekly/monthly)"
	// contract in the CLI help was only ever honored by manual runs. It
	// respects the UpdateSchedule setting ("off" disables it) and
	// short-circuits while offline. v1.6.0: the FIRST pass waits on the
	// startup maintenance gate — the loop can never race the prewarm it
	// might interrupt.
	// v1.7.3: the loop is SINGLE-FLIGHT. The previous unconditional
	// start overwrote updateCancel/updateDone on every repeated
	// EnsureSetup call, leaking every earlier loop — each leaked loop
	// still ran its gate-released first pass and its save() callback,
	// writing into the data dir after Close() and after test temp-dir
	// teardown (the observed "directory not empty" cleanup failures).
	// Repeated startup now reuses the ONE loop, exactly like it reuses
	// the ONE maintenance gate.
	sched := strings.ToLower(strings.TrimSpace(cfg.UpdateSchedule))
	if sched != "off" && sched != "never" {
		s.updateMu.Lock()

		if s.updateCancel == nil {
			ctx, cancel := context.WithCancel(context.Background())

			done := updater.RunScheduledAfter(
				ctx,
				s.src,
				s.llama,
				nil,
				func() {
					// persist LastUpdateCheck mutations through the source
					next := s.src.Load()
					_ = config.Save(next.ConfigPath(), next)
				},
				gate.Done(),
			)

			s.updateCancel = cancel
			s.updateDone = done
		}

		s.updateMu.Unlock()
	}

	// v1.2.4: idle maintenance loop of the runtime memory policy —
	// background cleanup runs ONLY while no agent run is active.
	if s.stack != nil && s.stack.MemMgr != nil {
		s.stack.StartMemoryManager(context.Background())
	}

	return nil
}

// Handler returns the http.Handler with all routes mounted.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// No static UI: the ONE product UI is the in-process Fyne application
	// (v1.11.0 one-Go-UI migration). The former embedded React bundle
	// (web/static) and its file-server route are retired; unknown paths
	// fall through to the API mux and 404.

	// REST API
	mux.HandleFunc("/api/state", s.handleState)

	// v1.10.1: the activity-transport discovery endpoint now answers
	// SAME-ORIGIN unconditionally — the desktop UI is the in-process
	// Fyne client over the local IPC channel (it never consults this
	// endpoint), and the browser/headless mode always streams over its
	// own origin. The former loopback transport answer (a real origin
	// + token for the Wails page) is retired with Wails itself.
	mux.HandleFunc("/api/ws-origin", s.handleWSOrigin)

	// v1.8.0: the Runtime Governor / system-health surface — the ONE
	// runtime-policy read model composed from the existing authorities.
	mux.HandleFunc("/api/governor", s.handleGovernor)
	mux.HandleFunc("/api/sysinfo", s.handleSysinfo)
	mux.HandleFunc("/api/presets", s.handlePresets)
	mux.HandleFunc("/api/models", s.handleModels)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSession)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/llama", s.handleLlama)
	mux.HandleFunc("/api/engine", s.handleEngine)
	mux.HandleFunc("/api/engine/rediscover", s.handleEngineRediscover)
	// v1.6.1: backend-variant provisioning (real Vulkan engine
	// packages — never a silent CPU fallback for explicit requests).
	mux.HandleFunc("/api/engine/provision", s.handleEngineProvision)
	mux.HandleFunc("/api/attachments", s.handleAttachments)
	mux.HandleFunc("/api/attachments/", s.handleAttachments)
	mux.HandleFunc("/api/run", s.handleRun)
	mux.HandleFunc("/api/abort", s.handleAbort)

	// v1.7.4: ONE coherent run-control contract over the EXISTING run
	// authorities — pause/edit/resume for any model/backend, plus the
	// restart-recovery listing. No second transport, no second state.
	mux.HandleFunc("/api/run/pause", s.handleRunPause)
	mux.HandleFunc("/api/run/edit", s.handleRunEdit)
	mux.HandleFunc("/api/run/resume", s.handleRunResume)
	mux.HandleFunc("/api/run/paused", s.handlePausedRuns)
	mux.HandleFunc("/api/feedback", s.handleFeedback)
	mux.HandleFunc("/api/tools", s.handleTools)

	// v1.6.0 P3: custom tools — the user-facing surface for creating,
	// listing, updating, testing and deleting local custom tools.
	// The tools themselves surface through /api/tools with
	// source="custom" metadata (the ONE registry).
	mux.HandleFunc("/api/custom-tools", s.handleCustomTools)
	mux.HandleFunc("/api/custom-tools/", s.handleCustomTool)
	// v1.9.0: the ONE AI System authority — CRUD, activation, clone,
	// export/import. The active system's snapshot is frozen into every
	// run at /api/run (see handleRun).
	mux.HandleFunc("/api/systems", s.handleAISystems)
	mux.HandleFunc("/api/systems/import", s.handleAISystemImport)
	mux.HandleFunc("/api/systems/active", s.handleAISystemActive)
	mux.HandleFunc("/api/systems/", s.handleAISystemItem)
	// v1.9.0: the durable long-horizon Goal engine — create/drive/
	// pause/resume/cancel/approve over the ONE orchestrator.
	mux.HandleFunc("/api/goals", s.handleGoals)
	mux.HandleFunc("/api/goals/", s.handleGoalItem)

	// v1.7.0: the Automation / Tasks surface — one scheduler API for
	// tasks, runs, scoped tools and artifacts (the chronological
	// timeline is served FROM PERSISTED SCHEDULER STATE, never a
	// UI-only construct).
	mux.HandleFunc("/api/automation/tasks", s.handleAutomation)
	mux.HandleFunc("/api/automation/tasks/", s.handleAutomation)
	mux.HandleFunc("/api/automation/task-tools/", s.handleAutomationTaskTool)
	mux.HandleFunc("/api/automation/artifacts", s.handleAutomationArtifacts)
	mux.HandleFunc("/api/automation/artifacts/", s.handleAutomationArtifacts)

	// v1.11.0: the optional 3D Studio — start/stop/state of the loopback
	// read-only topology viewer. The viewer itself (its own port + token)
	// is INTERNAL to this server; these endpoints are the ONLY control
	// surface and they carry no tool or mutation authority.
	mux.HandleFunc("/api/studio/state", s.handleStudioState)
	mux.HandleFunc("/api/studio/start", s.handleStudioStart)
	mux.HandleFunc("/api/studio/stop", s.handleStudioStop)
	mux.HandleFunc("/api/lab", s.handleLab)
	mux.HandleFunc("/api/lab/", s.handleLabTask)
	mux.HandleFunc("/api/research", s.handleResearch)
	// v1.3.6 (spec §26): the user-facing Net Search endpoint. It is
	// the SAME handler and the SAME research service — one coherent
	// surface, no duplicate implementation. /api/research stays as
	// the compatibility shim.
	mux.HandleFunc("/api/net-search", s.handleResearch)
	mux.HandleFunc("/api/models/open-folder", s.handleModelsFolder)

	// v1.5.0 MODEL-FIRST: the explicit model-selection flow (the
	// engine-start contract — select BEFORE load). GET returns the
	// backend-authoritative selection state; POST drives the whole
	// chain (analyze → configure atomically → load → verify → ready,
	// plus the bounded AUTO calibration).
	mux.HandleFunc("/api/models/select", s.handleModelsSelect)

	// v1.6.1: first-class local GGUF import — validate the header,
	// stream the file into the managed models directory atomically,
	// then (by default) drive the SAME selection chain as
	// /api/models/select. /api/models/import/pick opens the native
	// platform file dialog (Windows) so the user never has to find
	// the folder manually.
	mux.HandleFunc("/api/models/import", s.handleModelsImport)
	mux.HandleFunc("/api/models/import/pick", s.handleModelsImportPick)

	// v1.5.0: per-model recommendation evidence for the picker — the
	// "Recommended for this machine" chip requires this measured
	// evidence, never a filename/size heuristic.
	mux.HandleFunc("/api/models/recommendations", s.handleModelsRecommendations)

	// v1.2.8: history surfaces — picker search (cross-mode capable),
	// rolling session summaries and lazy history paging.
	mux.HandleFunc("/api/history/search", s.handleHistorySearch)

	// v1.2.4: Workspace surface — one compact work-environment view plus
	// the reveal/switch actions.
	mux.HandleFunc("/api/workspace", s.handleWorkspace)
	mux.HandleFunc("/api/workspace/reveal", s.handleWorkspaceReveal)
	mux.HandleFunc("/api/workspace/switch", s.handleWorkspaceSwitch)

	// v1.3.0: first-class GitHub clone workflow (start/status/cancel,
	// auto-switch on success — see clone.go).
	mux.HandleFunc("/api/workspace/clone", s.handleCloneStart)
	mux.HandleFunc("/api/workspace/clone/status", s.handleCloneStatus)
	mux.HandleFunc("/api/workspace/clone/cancel", s.handleCloneCancel)

	// v1.3.4 (ROADMAP v1.4 slice 1): repository intelligence — index
	// status, bounded refresh and the hybrid search entry point.
	mux.HandleFunc("/api/repo/index", s.handleRepoIndex)
	mux.HandleFunc("/api/repo/index/refresh", s.handleRepoIndexRefresh)
	mux.HandleFunc("/api/repo/search", s.handleRepoSearch)

	// v1.1.7: compact live performance, in-app logs and connection
	// diagnostics — read-only surfaces over existing infrastructure.
	mux.HandleFunc("/api/perf", s.handlePerf)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/netcheck", s.handleNetcheck)

	// v1.6.0 P0: the startup maintenance gate's explicit state —
	// phase, reason, detail, prewarm permission (spec §4).
	mux.HandleFunc("/api/maintenance", s.handleMaintenance)

	// v1.2.0: Environment Centre, verified health, the recommendation
	// engine and the application update surface.
	mux.HandleFunc("/api/environment", s.handleEnvironment)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/recommendation", s.handleRecommendation)
	mux.HandleFunc("/api/update/status", s.handleUpdateStatus)
	mux.HandleFunc("/api/update/check", s.handleUpdateCheck)
	mux.HandleFunc("/api/update/cancel", s.handleUpdateCancel)
	mux.HandleFunc("/api/update/download", s.handleUpdateDownload)

	// WebSocket: real-time agent activity for a session
	mux.HandleFunc("/ws/activity", s.handleActivityWS)

	return withCORS(mux)
}

// withCORS allows only approved local origins during development and
// same-origin requests in normal operation.
//
// Wildcard origins are intentionally prohibited because this API can expose
// local runtime state and perform privileged local operations.
func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))

		if origin != "" {
			if !allowedOrigin(origin, r.Host) {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		h.ServeHTTP(w, r)
	})
}

// --- State ---

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	st, _, _ := s.installer.EnsureRun(false)

	writeJSON(w, map[string]any{
		"appName":    config.AppName,
		"appVersion": config.AppVersion,
		"state":      st,
	})
}

func (s *Server) handleSysinfo(w http.ResponseWriter, r *http.Request) {
	// v1.2.6: interactive surface — NEVER blocks on the deep probe.
	// Serves the fast snapshot (+ deep facts when the background probe
	// already landed). The deep probe warms at startup; a caller that
	// needs the full facts uses sysinfo.Probe() directly.
	writeJSON(w, sysinfo.ProbeFast())
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, llm.Presets())
}

// modelInfo is the rich local-model descriptor consumed by the UI's model
// pickers. Before v1.1.2 the API shipped bare filename strings while the
// frontend expected {id, name, path, sizeBytes} objects — every <option>
// rendered with an undefined value, so selecting a model silently wrote
// "undefined" into the config. The wire format now matches the contract.
//
// v1.1.4: architecture / quantization / context capacity arrive from the
// GGUF header (llm.ReadModelCard) — a full parser that existed in the
// repository with ZERO callers while the README documented exactly these
// fields. Headers are read from a bounded 8 MB prefix and cached by
// path+mtime so repeated polls never re-parse model files.
type modelInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider,omitempty"`
	Path      string `json:"path,omitempty"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`

	Architecture  string `json:"architecture,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	ContextLength int    `json:"contextLength,omitempty"`
	ParameterInfo string `json:"parameterInfo,omitempty"`

	// 1.1.6 §10 first-use model card: estimated memory footprint
	// (weights + KV-cache at the recommended context + runtime
	// overhead) and the model-class-recommended context, so the UI
	// can present Architecture / Quantization / Context maximum /
	// Estimated memory / Backend / Status without guessing.
	EstimatedMemoryBytes int64 `json:"estimatedMemoryBytes,omitempty"`
	RecommendedContext   int   `json:"recommendedContext,omitempty"`

	// v1.1.8 model-picker capability facts, measured by
	// llm.ResolveModelCapabilities and served from the same bounded
	// cache as the header card (never guessed in the UI):
	//   - Multimodal:    a vision projector is paired with this model
	//   - NativeBackend: the native C++ engine can execute this arch
	//   - ChatTemplate:  the architecture ships a usable chat template
	//                    (prerequisite for tool calling)
	//   - EstimatedVRAMBytes: VRAM estimate at the recommended context
	Multimodal         bool   `json:"multimodal,omitempty"`
	NativeBackend      bool   `json:"nativeBackend,omitempty"`
	ChatTemplate       bool   `json:"chatTemplate,omitempty"`
	NativeReason       string `json:"nativeReason,omitempty"`
	EstimatedVRAMBytes int64  `json:"estimatedVRAMBytes,omitempty"`

	// Serving (v1.1.5 Phase 6): true when the ACTIVE backend is
	// currently serving THIS model — the honest "currently serving"
	// marker for the picker. The backend itself is reported once at
	// the response level.
	Serving bool `json:"serving,omitempty"`

	// Previous (v1.5.0): true when this model is the LAST-BUT-ONE
	// selection (config.previousModel) — the picker's "previous
	// selection" marker. Purely informational; it never resolves or
	// loads anything.
	Previous bool `json:"previous,omitempty"`

	// v1.2.0: the evidence-based vision state machine + projector
	// evidence (state, reason, paired mmproj path/size). Embedded so
	// the JSON stays flat for the UI.
	visionFields
}

// modelCardCache memoizes GGUF header reads for handleModels.
//
// v1.1.5 Phase 3: entries are keyed by path+SIZE+mtime (a same-size
// rewrite used to serve stale metadata) and the map is bounded — past the
// cap it is reset wholesale (model files change rarely; the read is cheap
// and buffered).
var (
	modelCardMu    sync.Mutex
	modelCardCache = map[string]modelCardEntry{}
)

type modelCardEntry struct {
	modTime time.Time
	size    int64
	info    *llm.ModelCard

	// v1.1.8: ResolveModelCapabilities re-parses the same GGUF header the
	// card read already covers. It used to run UNCACHED for every local
	// model on every /api/models poll; caching it here removes that
	// duplicated capability calculation from the hot polling path.
	caps *llm.ModelCapabilities
}

// modelCardCacheCap bounds the card cache; a models directory with more
// entries than this re-parses on demand (correct, just slower).
const modelCardCacheCap = 512

// modelCardFor returns cached GGUF metadata and capability card for path
// (nil card when unreadable — a broken header must not hide the file from
// the picker; caps stay nil in that case).
func modelCardFor(path string, cfg *config.Config) (*llm.ModelCard, *llm.ModelCapabilities) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}

	key := path + "\x00" + strconv.FormatInt(fi.Size(), 10)

	modelCardMu.Lock()
	cached, ok := modelCardCache[key]
	modelCardMu.Unlock()

	if ok && cached.size == fi.Size() && cached.modTime.Equal(fi.ModTime()) {
		return cached.info, cached.caps
	}

	card, _ := llm.ReadModelCard(path)
	caps := llm.ResolveModelCapabilities(cfg, path)

	modelCardMu.Lock()
	if len(modelCardCache) >= modelCardCacheCap {
		modelCardCache = map[string]modelCardEntry{}
	}
	modelCardCache[key] = modelCardEntry{modTime: fi.ModTime(), size: fi.Size(), info: card, caps: caps}
	modelCardMu.Unlock()

	return card, caps
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	cfg := s.src.Load()
	local := llm.ListLocalModels(cfg.ModelsDir)

	loaded, err := s.llama.ListLoadedModels()
	if err != nil {
		loaded = nil
	}

	localInfos := make([]modelInfo, 0, len(local))

	// v1.1.8: one host memory probe per poll, shared by every model card.
	mem := s.systemMemory()

	for _, name := range local {
		info := modelInfo{
			ID:       name,
			Name:     strings.TrimSuffix(name, ".gguf"),
			Provider: "local",
			Path:     filepath.Join(cfg.ModelsDir, name),
		}

		if fi, statErr := os.Stat(info.Path); statErr == nil {
			info.SizeBytes = fi.Size()
		}

		// v1.1.8: card AND caps come from the same bounded cache;
		// the per-poll ResolveModelCapabilities call is gone.
		card, caps := modelCardFor(info.Path, cfg)

		if card != nil && card.Arch != "" {
			info.Architecture = card.Arch
			info.Quantization = card.Quant
			info.ContextLength = card.ContextLength
			info.ParameterInfo = card.FormatParams()

			// 1.1.6 §10: the estimated footprint uses the model
			// class' recommended context, not the global setting,
			// so the picker stays honest for any machine.
			// v1.1.8: the host memory probe is hoisted out of the
			// per-model loop — one read per poll, not N.
			if caps != nil {
				info.RecommendedContext = caps.RecommendedCtx
				info.EstimatedMemoryBytes = llm.AssessContextResource(
					card, info.SizeBytes, caps.RecommendedCtx,
					cfg.EffectiveKVCacheQuant(), mem,
				).EstimatedTotalBytes

				// v1.1.8 model-picker capability facts —
				// measured, cached, never guessed client-side.
				info.Multimodal = caps.Multimodal
				info.NativeBackend = caps.NativeBackend
				info.ChatTemplate = caps.ChatTemplate
				info.NativeReason = caps.NativeReason
				info.EstimatedVRAMBytes = caps.EstimatedVRAMBytes
			}
		}

		// v1.2.0: per-model vision readiness (state machine +
		// projector evidence). Computed for EVERY local model so
		// the picker can render honest vision states before any
		// boot. When the engine is actively serving this model
		// WITH a projector, the state is upgraded to the runtime
		// truth (ready/verified beats pre-boot evidence).
		info.visionFields = visionForModel(
			cfg.ModelsDir, info.Path, cfg.VisionMMProj, info.Architecture,
		)
		if info.Serving || filepath.Clean(info.Path) == filepath.Clean(s.llama.LoadedModel()) {
			if vs := s.llama.VisionStatus(); vs.Active {
				info.VisionState = vision.StateReady
				info.VisionReason = vs.Reason
				info.MMProjVerified = true
			}
		}

		localInfos = append(localInfos, info)
	}

	loadedInfos := make([]modelInfo, 0, len(loaded))

	for _, id := range loaded {
		loadedInfos = append(loadedInfos, modelInfo{
			ID:       id,
			Name:     id,
			Provider: "local",
		})
	}

	// v1.1.5 Phase 6 (honest model status): report WHICH backend is
	// serving and WHICH model it serves, and mark that model in the
	// local list. The UI vocabulary stays honest: discovered (listed
	// here), loaded (llama.cpp loaded list), serving (the active
	// backend is generating with it right now). Capability wording
	// (native-capable / llama-capable) is owned by /api/engine, which
	// has the validated capability state.
	backend := "llama"

	if s.stack != nil && s.stack.Engine() != nil {
		backend = s.stack.Engine().Name()
	}

	servingPath := ""

	switch backend {
	case "native":
		if s.native != nil {
			servingPath = s.native.NativeModelPath()
		}
	default:
		servingPath = s.llama.LoadedModel()
	}

	if servingPath != "" {
		for i := range localInfos {
			if filepath.Clean(localInfos[i].Path) == filepath.Clean(servingPath) {
				localInfos[i].Serving = true
			}
		}
	}

	// v1.5.0: the "previous selection" marker (the model this selection
	// replaced) — resolved by NAME equality like the picker's own
	// active-model concept.
	if prev := cfg.PreviousModel; prev != "" {
		for i := range localInfos {
			if localInfos[i].ID == prev || localInfos[i].Name == prev {
				localInfos[i].Previous = true
			}
		}
	}

	writeJSON(w, map[string]any{
		"local":        localInfos,
		"loaded":       loadedInfos,
		"llamaRunning": s.llama.IsRunning(),
		"backend":      backend,
		"servingPath":  servingPath,
	})
}

func (s *Server) handleLlama(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"state": s.llama.State(),
			"logs":  s.llama.Logs(),
		})

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

		var body struct {
			Action string `json:"action"`
		}

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		switch body.Action {
		case "start":
			// v1.5.1: the calibration exclusion gate — starting an
			// engine under an active calibration would race its
			// verified restart cycles (spec §2.4).
			if s.calibrating.Load() {
				writeErr(w, http.StatusConflict, fmt.Errorf(
					"an automatic calibration is in progress — engine start is locked until it settles",
				))
				return
			}

			// v1.5.0 MODEL-FIRST: an explicit engine start with
			// no selected model is a clean 400 — never a silent
			// boot of an arbitrary model (the Model Selector
			// owns the selection).
			if !s.src.Load().IsRemote() && s.src.Load().Model == "" {
				writeErr(w, http.StatusBadRequest, fmt.Errorf(
					"%w — pick a model first (POST /api/models/select)",
					llm.ErrNoModelSelected,
				))
				return
			}

			// v1.1.5: when the native path is enabled, the
			// engine toggle brings BOTH engines up: the native
			// engine AND the llama.cpp engine.
			//
			// v1.1.5 repair (toggle usability): the toggle must
			// produce a USABLE native engine — start the host AND
			// load the selected model natively. Starting the host
			// alone left it generation-incapable, so every run
			// still gated on llama.cpp: on a native-only
			// deployment (no llama.cpp binary) the "engine start"
			// control could never lead to a working run — the
			// exact "a button is not a feature" defect class.
			// Synchronous + bounded (matches the toggle's
			// existing synchronous llama.Start contract); the
			// launch prewarm uses the same Stack seam.
			nativeServes := false

			if s.native != nil && s.src.Load().NativeBackendEnabled() {
				nativeCtx, nativeCancel := context.WithTimeout(
					context.Background(),
					60*time.Second,
				)

				if err := s.stack.EnsureNativeReady(nativeCtx); err != nil {
					// Logged inside EnsureNativeReady; the
					// llama result below stays authoritative
					// when native did not come up.
					logging.Default().Warn(
						"native-engine",
						"engine toggle: native engine not ready: %v",
						err,
					)
				}

				nativeCancel()

				if gc, ok := s.stack.NativeBackend().(llm.GenerationCapable); ok && gc.GenerationCapable() {
					nativeServes = true
				}
			}

			if err := s.llama.Start(); err != nil {
				if nativeServes {
					// Native serves generation; llama.cpp is
					// the FALLBACK — a failed fallback must
					// not fail the toggle (the same ordering
					// the Phase 5 run-gate repair applies to
					// EnsureLLM/EnsureLLMContext).
					logging.Default().Warn(
						"engine",
						"llama.cpp did not start (native engine serves generation): %v",
						err,
					)
				} else {
					writeErr(w, http.StatusInternalServerError, err)
					return
				}
			}

		case "cancel-download":
			// v1.2.3: stop an in-flight engine asset download
			// (llama.cpp archive) immediately; the .part file stays
			// so a retry resumes instead of restarting from zero.
			cancelled := s.llama.CancelDownload()
			writeJSON(w, map[string]any{
				"cancelled": cancelled,
				"state":     s.llama.State(),
			})
			return

		case "stop":
			// v1.5.1: the calibration exclusion gate — stopping the
			// engine mid-pass would kill the measurement and could
			// leave a candidate profile unverified (spec §2.4).
			if s.calibrating.Load() {
				writeErr(w, http.StatusConflict, fmt.Errorf(
					"an automatic calibration is in progress — engine stop is locked until it settles",
				))
				return
			}

			// v1.1.5: stop both engines (native bounded, errors
			// logged only — the llama stop result is the
			// authoritative one).
			if s.native != nil {
				stopCtx, cancel := context.WithTimeout(
					context.Background(),
					10*time.Second,
				)

				if err := s.native.Stop(stopCtx); err != nil {
					logging.Default().Warn(
						"native-engine",
						"native engine stop: %v",
						err,
					)
				}

				cancel()
			}

			_ = s.llama.Stop()

		default:
			writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown action %q", body.Action))
			return
		}

		writeJSON(w, map[string]any{
			"state": s.llama.State(),
		})

	default:
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

// --- Sessions ---

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.store.List()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}

		// v1.2.8: mode-separated histories. `?mode=chat|agent`
		// filters the list to ONE conversation space; without the
		// parameter the full list is served (compat: the CLI and
		// existing tests rely on it).
		if mode := strings.TrimSpace(r.URL.Query().Get("mode")); mode == sessions.ModeChat || mode == sessions.ModeAgent {
			filtered := make([]*sessions.Session, 0, len(list))
			for _, st := range list {
				if st == nil {
					continue
				}
				stMode := st.Mode
				if stMode == "" {
					stMode = sessions.DefaultMode
				}
				if stMode == mode {
					filtered = append(filtered, st)
				}
			}
			list = filtered
		}

		writeJSON(w, list)

	case http.MethodPost:
		// v1.2.8: the creation request MAY carry the conversation
		// space ({"mode":"chat"|"agent"}). An empty body keeps the
		// default (agent) — old clients behave identically.
		mode := ""
		var body struct {
			Mode string `json:"mode,omitempty"`
		}
		if r.Body != nil {
			if data, err := io.ReadAll(io.LimitReader(r.Body, 4<<10)); err == nil && len(strings.TrimSpace(string(data))) > 0 {
				_ = json.Unmarshal(data, &body)
			}
		}
		mode = body.Mode

		sess := s.store.CreateInMode(mode)
		writeJSON(w, sess)

	default:
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/sessions/")

	if rest == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("missing session id"))
		return
	}

	// 1.1.6: per-session context policy subresource.
	if id, sub := splitSessionPath(rest); sub == "context" {
		s.handleSessionContext(w, r, id)
		return
	}

	// v1.2.8: history subresources — the durable summary and the lazy
	// message pager.
	if id, sub := splitSessionPath(rest); sub == "summary" {
		s.handleSessionSummary(w, r, id)
		return
	}

	if id, sub := splitSessionPath(rest); sub == "messages" {
		s.handleSessionMessages(w, r, id)
		return
	}

	id := rest

	switch r.Method {
	case http.MethodGet:
		sess, err := s.store.Get(id)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}

		writeJSON(w, sess)

	case http.MethodDelete:
		if err := s.store.Delete(id); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, map[string]any{
			"ok": true,
		})

	case http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		var body struct {
			Title       *string           `json:"title,omitempty"`
			Context     *sessions.Context `json:"context,omitempty"`
			Model       *string           `json:"model,omitempty"`
			HistoryRefs *[]histref.Ref    `json:"historyRefs,omitempty"`
		}

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		if body.Title != nil {
			if err := s.store.UpdateTitle(id, *body.Title); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}

		if body.Context != nil {
			// v1.2.8.1: history references arriving through the
			// full-context PUT are VALIDATED server-side —
			// normalized (dedup + cap), self-references dropped,
			// same-mode references enforced against the store
			// index. The client picker is a convenience, never
			// the authority; this was previously a write-only
			// sink that accepted arbitrary reference lists.
			ctx := *body.Context
			ctx.HistoryRefs = s.validateHistoryRefs(id, s.store.ModeOf(id), ctx.HistoryRefs)

			if err := s.store.UpdateContext(id, ctx); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}

		if body.Model != nil {
			if err := s.store.SetModel(id, *body.Model); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}

		// v1.2.8.1: atomic history-reference delta. The picker's
		// attach/detach sends ONLY the new ref list (after server-
		// side validation) and the store applies it under its lock —
		// no GET-modify-PUT of the whole context, so a concurrent
		// token-policy or attachment update can never be silently
		// reverted by a stale full-context write.
		if body.HistoryRefs != nil {
			refs := s.validateHistoryRefs(id, s.store.ModeOf(id), *body.HistoryRefs)

			if err := s.store.UpdateContextFunc(id, func(c *sessions.Context) (bool, error) {
				changed := !equalHistoryRefs(c.HistoryRefs, refs)
				if changed {
					c.HistoryRefs = refs
				}
				return changed, nil
			}); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}

		sess, err := s.store.Get(id)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}

		writeJSON(w, sess)

	default:
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

// redactedConfig returns a copy safe for API responses.
// Secrets are never returned to the browser.
func (s *Server) redactedConfig() config.Config {
	cfg := *s.src.Load()
	cfg.RemoteAPIKey = ""
	return cfg
}

// mergeConfigPatch applies a JSON object as a partial configuration update
// and publishes the result through the config source.
//
// Fields omitted from the request retain their current values.
//
// v1.1.4: the patch is applied to a PRIVATE COPY under the source's write
// lock and the new value is atomically published (copy-on-write). The old
// implementation wrote `*s.cfg = updated` in place on the shared pointer —
// a genuine data race against every run goroutine and the engine manager,
// which read the same struct concurrently. Handlers and runs now snapshot
// via s.src.Load() and observe either the old or the new value, never a
// half-written one.
func (s *Server) mergeConfigPatch(data []byte) (*config.Config, error) {
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(data, &patch); err != nil {
		return nil, err
	}
	if patch == nil {
		return nil, fmt.Errorf("configuration patch must be a JSON object")
	}

	currentData, err := json.Marshal(s.src.Load())
	if err != nil {
		return nil, err
	}

	var current map[string]json.RawMessage
	if err := json.Unmarshal(currentData, &current); err != nil {
		return nil, err
	}

	for key, value := range patch {
		// A blank remoteApiKey coming from a redacted GET response must never
		// erase the stored secret. A non-empty value can intentionally replace
		// the configured key.
		if key == "remoteApiKey" {
			var candidate string
			if err := json.Unmarshal(value, &candidate); err == nil && candidate == "" {
				continue
			}
		}

		// v1.3.0: "llm" is merged FIELD-BY-FIELD, not replaced wholesale.
		// The v1.2.9 behavior replaced the whole LLMOptions object, so a
		// partial llm patch (preset apply, posture apply, System apply —
		// all of which send only the fields they change) silently zeroed
		// every unspecified sampling field (temperature→0, topP→0…).
		// Deep-merging here keeps every partial patch additive.
		if key == "llm" {
			mergedLLM, err := mergeLLMPatch(current["llm"], value)
			if err != nil {
				return nil, fmt.Errorf("patch llm: %w", err)
			}
			current["llm"] = mergedLLM
			continue
		}

		current[key] = value
	}

	merged, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}

	var updated config.Config
	if err := json.Unmarshal(merged, &updated); err != nil {
		return nil, err
	}

	// v1.8.4 (P0-B): a patch that carries gpuAutoOffload is an EXPLICIT
	// user action on the GPU posture (the settings toggle, the System
	// Centre posture controls). Marking it here is what makes the
	// explicit-OFF contract enforceable: only a user-set posture is
	// ever treated as a real OFF by the AUTO candidate gate and the
	// derived-posture repair never touches it.
	if _, ok := patch["gpuAutoOffload"]; ok {
		updated.GPUAutoOffloadUserSet = true
	}

	// v1.6.1 (P0): the sampling-value gate on the LIVE configuration. A
	// patch carrying a parser-invalid sampling value (repeatPenalty=0, a
	// negative temperature, a non-finite topP…) is REJECTED with an
	// actionable diagnostic instead of being stored — the stored value
	// would be handed to the engine as a launch argument and rejected
	// there ("repeat-penalty must be finite and greater than 0"), after
	// which the failure fed the compatibility ladder (the v1.6.0 retry
	// storm). Validation lives in internal/config/sampling.go — the same
	// authority the engine boot gate uses.
	if problems := config.ValidateSamplingOptions(updated.LLM); len(problems) > 0 {
		return nil, &config.SamplingProblemsError{Problems: problems}
	}

	s.src.Store(&updated)
	return &updated, nil
}

// mergeLLMPatch merges a partial llm object into the current one:
// present keys from the patch win, absent keys keep their current
// values. Either side being absent degrades to the v1.2.9 wholesale
// replacement (safe: nothing to preserve).
func mergeLLMPatch(currentRaw, patchRaw json.RawMessage) (json.RawMessage, error) {
	if len(patchRaw) == 0 || string(patchRaw) == "null" {
		return currentRaw, nil
	}

	var patchObj map[string]json.RawMessage
	if err := json.Unmarshal(patchRaw, &patchObj); err != nil {
		return nil, err
	}
	if patchObj == nil {
		return currentRaw, nil
	}

	var currentObj map[string]json.RawMessage
	if len(currentRaw) > 0 && string(currentRaw) != "null" {
		if err := json.Unmarshal(currentRaw, &currentObj); err != nil {
			return nil, err
		}
	}
	if currentObj == nil {
		currentObj = map[string]json.RawMessage{}
	}

	for k, v := range patchObj {
		currentObj[k] = v
	}

	return json.Marshal(currentObj)
}

// handleConfig patches the live configuration through the copy-on-write
// source.
//
// GET never exposes RemoteAPIKey.
//
// PUT/POST behave as patch operations: only fields supplied by the caller are
// changed; unspecified settings remain untouched.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.redactedConfig())

	case http.MethodPut, http.MethodPost:
		// v1.5.1: the calibration/model-change exclusion gate — runtime
		// performance mutations are locked while a bounded calibration is
		// actively changing the profile (spec §2.4, §7: prevent
		// conflicting performance mutations).
		if s.calibrating.Load() {
			writeErr(w, http.StatusConflict, fmt.Errorf(
				"an automatic calibration is in progress — performance changes are locked until it settles",
			))
			return
		}

		// v1.1.4: bounded body — the config endpoint previously
		// accepted unbounded request bodies.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		var raw json.RawMessage

		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()

		if err := decoder.Decode(&raw); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		if len(raw) == 0 || string(raw) == "null" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("configuration patch must be a JSON object"))
			return
		}

		next, err := s.mergeConfigPatch(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		if err := config.Save(next.ConfigPath(), next); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}

		if err := next.EnsureDirs(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, s.redactedConfig())
	default:
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
	}
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]any, 0)

	// v1.7.2-repair: iterate the immutable registry SNAPSHOT (Tools()
	// returns a private copy — see the Orchestrator contract) and emit
	// a deterministically ORDERED list: map iteration order is
	// randomized by Go, so sort by tool name for stable responses.
	snapshot := s.orch.Tools()
	names := make([]string, 0, len(snapshot))
	for name := range snapshot {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		t := snapshot[name]
		// v1.1.7: the Options UI shows the SHORT one-line description;
		// the full operational spec (used by the model) stays in
		// `detail` — documented, not duplicated, never truncated.
		short, full := t.Description(), t.Description()
		if sd, ok := any(t).(interface {
			ShortDescription() string
		}); ok {
			short = sd.ShortDescription()
		}

		entry := map[string]any{
			"name":        t.Name(),
			"description": short,
			"detail":      full,
		}

		// v1.6.0 P3 (spec §17): source metadata — builtin vs custom —
		// through the ONE registry. No second registry exists.
		if _, isCustom := any(t).(*customtools.Tool); isCustom {
			entry["source"] = "custom"
		} else {
			entry["source"] = "builtin"
		}

		out = append(out, entry)
	}

	writeJSON(w, out)
}

// --- Run / Abort ---

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	// Bound request parsing; the run itself is async and bounded elsewhere.
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)

	var body struct {
		SessionID     string   `json:"sessionId"`
		Message       string   `json:"message"`
		AttachmentIDs []string `json:"attachmentIds,omitempty"`
		Regenerate    bool     `json:"regenerate,omitempty"`

		// v1.2.5 per-request controls — they shape the ACTUAL backend
		// request (tier posture, tool surface), never just the UI.
		// v1.8.5: the reasoning-depth ladder "low" | "mid" |
		// "high" | "ultra" (legacy "auto"/"fast"/"thinking" are
		// accepted and normalized at the orchestrator boundary).
		// Each level carries a REAL numeric thinking budget onto
		// the generation request — see agent.applyReasoningBudget.
		Thinking  string   `json:"thinking,omitempty"`
		ToolMode  string   `json:"toolMode,omitempty"`  // "auto" | "manual"
		ToolAllow []string `json:"toolAllow,omitempty"` // manual-mode allow-list

		// v1.3.6 (spec §24): explicit per-request Net Search intent.
		// Server-side enforcement: the existing research tool is
		// authorized for THIS request only.
		NetSearch bool `json:"netSearch,omitempty"`

		// v1.2.8: cross-mode history references — sessions the user
		// EXPLICITLY attached as context. DATA, never authority: the
		// retrieved portions are provenance-tagged and never
		// authorize execution by themselves.
		HistoryRefs []histref.Ref `json:"historyRefs,omitempty"`

		// v1.9.0: explicit AI System selection for THIS run. Empty = the
		// ACTIVE system (the documented select-once, next-runs-use-it
		// contract). The snapshot is frozen below, before the run
		// goroutine starts — a mid-run edit can never mutate the run.
		SystemID string `json:"systemId,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	// v1.2.5: the run clock anchors to the moment the request landed.
	receivedAt := time.Now()

	if body.SessionID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId required"))
		return
	}

	if body.Message == "" && !body.Regenerate {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("message required (or regenerate=true)"))
		return
	}

	sess, err := s.store.Get(body.SessionID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	// v1.9.5 (P1): deterministic admission refusal while an engine
	// variant transaction holds the mutation claim. This early check
	// keeps the friendly contract (no user message is ever persisted
	// for a refused run); the AIRTIGHT check below re-evaluates the
	// same runsMu-guarded claim inside the registration critical
	// section, so the check-then-act window is closed regardless of
	// timing.
	if s.llama != nil && s.llama.EngineMutationActive() {
		writeErr(w, http.StatusServiceUnavailable, errEngineMutationInProgress)
		return
	}

	// v1.9.0: the AI System binding is resolved ONCE, here — before
	// the run goroutine starts and before the response is written.
	// Explicit request systemId wins (404 when missing — an honest,
	// deterministic contract); otherwise the ACTIVE system. A
	// failed ACTIVE-pointer resolution is non-fatal: the run
	// proceeds with the pre-v1.9 behavior — the runtime never
	// refuses to run over a configuration object.
	var aiSnap *aisystem.Snapshot
	if s.stack != nil && s.stack.Systems != nil {
		if body.SystemID != "" {
			sys, sysErr := s.stack.Systems.Get(body.SystemID)
			if sysErr != nil {
				writeErr(w, http.StatusNotFound, fmt.Errorf("ai system %q not found", body.SystemID))
				return
			}
			snap := sys.Snapshot()
			aiSnap = &snap
		} else if sys, sysErr := s.stack.Systems.Active(); sysErr == nil {
			snap := sys.Snapshot()
			aiSnap = &snap
		}
	}

	// v1.2.8: resolve the attached history references. Self-references
	// are dropped (the active transcript is already here); unknown or
	// foreign ids are silently skipped by Resolve (never fabricated).
	//
	// v1.2.8.1: (a) The persisted session context is the FALLBACK
	// authority — a request that omits historyRefs (regenerate, CLI,
	// any client) runs with the SAME references the session already
	// carries, so a regenerated answer can never silently lose the
	// cross-mode context of the original run. Explicit request refs
	// win: the union is body-first, deduplicated and capped by
	// NormalizeRefs. (b) Cross-mode semantics are enforced SERVER-SIDE
	// (never trusted from the picker): a reference whose SOURCE session
	// — resolved from the store index, not from the client payload — is
	// not in the opposite conversation space is dropped here, and dead
	// references (source vanished) are pruned from the context.
	refsUnion := make([]histref.Ref, 0, len(body.HistoryRefs)+len(sess.Context.HistoryRefs))
	refsUnion = append(refsUnion, body.HistoryRefs...)
	refsUnion = append(refsUnion, sess.Context.HistoryRefs...)
	requestedRefs := s.validateHistoryRefs(sess.ID, sessions.NormalizeMode(sess.Mode), refsUnion)

	// Persist the surviving references (bounded, deduplicated) ATOMICALLY
	// so later turns keep retrieving them even when the client omits the
	// refs. The store lock serializes this against every other context
	// mutation (attachment association, token policy, picker PUT).
	if !equalHistoryRefs(sess.Context.HistoryRefs, requestedRefs) {
		if err := s.store.UpdateContextFunc(sess.ID, func(c *sessions.Context) (bool, error) {
			changed := !equalHistoryRefs(c.HistoryRefs, requestedRefs)
			if changed {
				c.HistoryRefs = requestedRefs
			}
			return changed, nil
		}); err != nil {
			logging.Default().Warn("api", "persist history refs: %v", err)
		}
	}

	// v1.9.5 (P1): transcript snapshot taken before THIS request
	// mutates session messages. If the airtight admission check below
	// refuses the run (a claim taken between the early check and this
	// point), the snapshot restores the persisted transcript exactly —
	// a refused run never leaves a dangling user message behind.
	preMutationMessages := slices.Clone(sess.Messages)

	// Regenerate: drop trailing assistant output so the previous user
	// turn re-runs; the message body is optional in that case.
	if body.Regenerate {
		for len(sess.Messages) > 0 {
			last := sess.Messages[len(sess.Messages)-1]

			if last.Role == "assistant" {
				sess.Messages = sess.Messages[:len(sess.Messages)-1]
				continue
			}

			if last.Role == "tool" {
				sess.Messages = sess.Messages[:len(sess.Messages)-1]
				continue
			}

			break
		}
	}

	// Resolve staged attachments for this turn: explicit ids from the
	// request plus the session's persisted association.
	attIDs := s.attachmentIDsForSession(body.AttachmentIDs)

	for _, id := range s.attachmentIDsForSession(sess.Context.AttachmentIDs) {
		known := false

		for _, knownID := range attIDs {
			if knownID == id {
				known = true
				break
			}
		}

		if !known {
			attIDs = append(attIDs, id)
		}
	}

	// Append the user message (display carries attachment names).
	if body.Message != "" {
		userMsg := llm.Message{
			Role:    "user",
			Content: body.Message,
		}

		if len(attIDs) > 0 && s.stack != nil && s.stack.Attachments != nil {
			for _, id := range attIDs {
				if att, ok := s.stack.Attachments.Get(id); ok {
					userMsg.Attachments = append(
						userMsg.Attachments,
						att.Name,
					)
				}
			}
		}

		sess.Messages = append(sess.Messages, userMsg)

		// If first user message, derive a title.
		if len(sess.Messages) == 1 {
			title := body.Message
			if len(title) > 60 {
				title = title[:60] + "..."
			}

			// v1.4.0 (found by the browser E2E): the derived
			// title MUST also land on the local copy — the
			// SaveMessagesKeepContext call below persists the
			// CALLER'S session object, and a copy still carrying
			// the empty title clobbered the store's freshly
			// updated title right after UpdateTitle. The sidebar
			// then showed "Untitled" for every first message.
			sess.Title = title

			_ = s.store.UpdateTitle(sess.ID, title)
		}
	}

	// Persist attachment association on the session context so later turns
	// keep the files reusable. v1.2.8.1: applied ATOMICALLY through the
	// store's context mutation (no read-modify-write race with a
	// concurrent picker attach or token-policy update).
	if len(attIDs) > 0 {
		if err := s.store.UpdateContextFunc(sess.ID, func(c *sessions.Context) (bool, error) {
			changed := false

			for _, id := range attIDs {
				known := false

				for _, knownID := range c.AttachmentIDs {
					if knownID == id {
						known = true
						break
					}
				}

				if !known {
					c.AttachmentIDs = append(c.AttachmentIDs, id)
					changed = true
				}
			}

			return changed, nil
		}); err != nil {
			logging.Default().Warn("api", "persist attachment association: %v", err)
		}

		for _, id := range attIDs {
			if s.stack != nil && s.stack.Attachments != nil {
				s.stack.Attachments.Associate(id, sess.ID)
			}
		}
	}

	// v1.1.4: a failed pre-run persistence was previously invisible —
	// the user message could silently vanish while the run continued.
	// v1.2.8.1: the save preserves the STORE's current context (fetched
	// under the same lock) — the run's context deltas were applied
	// atomically above; a whole-object Save from this (possibly stale)
	// copy would silently revert them.
	if err := s.store.SaveMessagesKeepContext(sess); err != nil {
		logging.Default().Warn(
			"api",
			"persist session before run: %v",
			err,
		)
	}

	// Build the LLM message list (with optional system prompt).
	var messages []llm.Message

	if sess.Context.SystemPrompt != "" {
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: sess.Context.SystemPrompt,
		})
	}

	// Legacy AttachedFiles (server-side paths) still flow through the
	// chunking engine; the staged attachments pipeline is layered on top.
	attachedImages, _ := chunking.SplitAttachments(sess.Context.AttachedFiles)

	if len(sess.Context.AttachedFiles) > 0 {
		note := chunking.ComposeUserMessage(
			"",
			sess.Context.AttachedFiles,
			s.src.Load().AttachmentsBudgetBytes(),
		)

		messages = append(messages, llm.Message{
			Role:    "system",
			Content: note,
		})
	}

	messages = append(messages, sess.Messages...)

	// The freshest user message carries the current turn's images —
	// both legacy paths and newly staged image attachments.
	stagedImagePaths := make([]string, 0, 4)

	if len(attIDs) > 0 && s.stack != nil && s.stack.Attachments != nil {
		for _, id := range attIDs {
			if att, ok := s.stack.Attachments.Get(id); ok &&
				att.Kind == attachments.KindImage {
				stagedImagePaths = append(
					stagedImagePaths,
					s.stack.Attachments.StagePath(id),
				)
			}
		}
	}

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			messages[i].Images = append(messages[i].Images, attachedImages...)
			messages[i].Images = append(messages[i].Images, stagedImagePaths...)
			break
		}
	}

	// v1.1.3: staged attachments enter the real context pipeline here —
	// relevant chunks are retrieved for the current query and injected
	// as a bounded, provenance-tagged block BEFORE the fresh user turn
	// (stable prompt prefix preserves the engine KV cache). Raw binaries
	// never reach the prompt. The block is content-cached: identical
	// attachments + query skip re-chunking entirely.
	if len(attIDs) > 0 && s.stack != nil && s.stack.Attachments != nil {
		query := body.Message

		if query == "" {
			// Regenerate: retrieve against the last user turn.
			for i := len(sess.Messages) - 1; i >= 0; i-- {
				if sess.Messages[i].Role == "user" {
					query = sess.Messages[i].Content
					break
				}
			}
		}

		budget := s.src.Load().AttachmentsBudgetBytes()

		// v1.1.5 Phase 3: retrieval is instrumented with MEASURED
		// values only (chunks considered/selected, object reads,
		// bytes). Logged per turn for diagnosability; the wire/UI
		// contract is unchanged.
		block, retrievalStats := s.stack.Attachments.RetrieveWithStats(
			r.Context(),
			query,
			attIDs,
			budget,
		)

		logging.Default().Info(
			"api",
			"attachment retrieval: attachments=%d chunksConsidered=%d chunksSelected=%d bytes=%d objectReads=%d objectBytes=%d cacheHit=%t",
			retrievalStats.AttachmentsConsidered,
			retrievalStats.ChunksConsidered,
			retrievalStats.ChunksSelected,
			retrievalStats.BytesComposed,
			retrievalStats.ObjectReads,
			retrievalStats.ObjectBytesRead,
			retrievalStats.CacheHit,
		)

		if block != "" {
			blockMsg := llm.Message{
				Role:    "system",
				Content: "[staged attachments relevant to this turn]\n\n" + block,
			}

			// Insert before the last user message.
			insertAt := len(messages)

			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					insertAt = i
					break
				}
			}

			messages = append(
				messages[:insertAt],
				append(
					[]llm.Message{blockMsg},
					messages[insertAt:]...,
				)...,
			)
		}
	}

	// v1.2.8: retrieve the relevant portions of the attached history
	// references for THIS turn and load the session's rolling summary.
	// Both blocks are pre-bounded and provenance-tagged; the planner
	// still decides whether they travel (history-refs are the first
	// retrieval section dropped under pressure). The summary never
	// replaces the transcript — it is a derived aid.
	var histRefBlocks []llm.Message

	if len(requestedRefs) > 0 {
		query := body.Message

		if query == "" {
			// Regenerate: retrieve against the last user turn.
			for i := len(sess.Messages) - 1; i >= 0; i-- {
				if sess.Messages[i].Role == "user" {
					query = sess.Messages[i].Content
					break
				}
			}
		}

		// Budget: the per-reference default, scaled to the ref count
		// (bounded by design — the picker caps refs at MaxRefsPerRun).
		histRefBlocks = histref.Resolve(
			s.store,
			requestedRefs,
			query,
			histref.DefaultBlockTokens*len(requestedRefs),
		)

		if len(histRefBlocks) > 0 {
			logging.Default().Info(
				"history",
				"retrieved %d history block(s) for session %s (refs=%d queryBytes=%d)",
				len(histRefBlocks), sess.ID, len(requestedRefs), len(query),
			)
		}
	}

	var summaryBlock string

	if sum, err := s.store.SummaryForRun(sess.ID); err == nil {
		summaryBlock = sessions.RenderSummaryBlock(sum)
	}

	// Spawn the run.
	//
	// v1.1.4: RunTimeoutMinutes (default 60) bounds the whole turn.
	// Previously a run was bounded ONLY by maxIterations and the LLM
	// client's per-call timeout — a pathological turn could legally run
	// for hours while holding the run slot.
	runCtx := context.Background()
	budgetCancel := func() {}

	if budget := s.src.Load().EffectiveRunTimeout(); budget > 0 {
		var budgetCtx context.Context
		budgetCtx, budgetCancel = context.WithTimeout(runCtx, budget)
		runCtx = budgetCtx
	}

	ctx, cancel := context.WithCancel(runCtx)
	hub := newActivityHub()

	// v1.2.6: one runId across the WHOLE path (UI → POST → registry →
	// engine gate → orchestrator → persistence → WS delivery). Every
	// activity frame of this run carries it; the outcome registry and the
	// logs use it, so a stalled run can be attributed end-to-end.
	runID := newRunID()

	// v1.2.6 continuation: the AUTHORITATIVE live state. Every published
	// activity of this run is sequence-stamped and folded into it; every
	// WebSocket attachment replays from it.
	live := newRunLive(runID, sess.ID, time.Now())

	// v1.7.4: the per-run pause control (shared with the orchestrator).
	ctrl := agent.NewRunControl()

	// v1.2.6: the API-side stage timeline — runAccepted → runRegistered →
	// engineGateStart/Ready → (orchestrator stages via RunClock) →
	// assistantPersisted → donePublished. Measured values only; the
	// summary line is logged once when the run settles.
	tl := runTimeline{
		runID:      runID,
		sessionID:  sess.ID,
		acceptedAt: receivedAt,
	}

	s.runsMu.Lock()

	// v1.9.5 (P1): the AIRTIGHT admission check — the same runsMu
	// critical section that registers the run also evaluates the
	// engine-mutation claim, making claim and registration mutually
	// exclusive by construction (see BeginEngineMutation). On refusal
	// the pre-mutation transcript snapshot is restored so the rejected
	// turn leaves no dangling user message, and the caller receives
	// the deterministic 503.
	if s.engineMutation {
		s.runsMu.Unlock()

		// The run never starts: release the context handles the
		// accepted path would have owned (vet: no leaked timer).
		cancel()
		budgetCancel()

		sess.Messages = preMutationMessages

		if err := s.store.SaveMessagesKeepContext(sess); err != nil {
			logging.Default().Warn(
				"api",
				"rollback transcript after admission refusal: %v",
				err,
			)
		}

		writeErr(w, http.StatusServiceUnavailable, errEngineMutationInProgress)
		return
	}

	// There can be only one active run per session. Cancel the previous run
	// before replacing its state.
	if old, ok := s.runs[sess.ID]; ok {
		// v1.7.5: a PAUSED run being replaced is stopped for good — its
		// durable checkpoint is consumed here (§13: the checkpoint is
		// deleted at the correct terminal boundary). Left behind, the
		// stale record would stay resumable OVER the new run: a later
		// POST /api/run/resume would re-register the old run in place
		// of the live one (recoverPausedRun replaces the entry).
		if old.live.isPaused() {
			s.deletePausedRecord(old.live.runID)
		}

		old.cancel()
		old.hub.close()
	}

	// v1.10.1: refuse registrations once Close has begun — the runWG
	// join in Close requires every Add to happen-before its Wait, and
	// both this guard and the Add below run under runsMu against a
	// closing flag that Close sets before its own runsMu critical
	// section, so the ordering is total. The rejected turn leaves NO
	// dangling user message (the same rollback standard as the
	// engine-mutation refusal above).
	if s.closing.Load() {
		s.runsMu.Unlock()

		// The run never starts, so the contexts created for it are
		// released here (vet: no leaked cancel).
		budgetCancel()
		cancel()

		sess.Messages = preMutationMessages

		if err := s.store.SaveMessagesKeepContext(sess); err != nil {
			logging.Default().Warn(
				"api",
				"rollback transcript after shutdown refusal: %v",
				err,
			)
		}

		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("server is shutting down; no new runs are accepted"))
		return
	}

	s.runs[sess.ID] = &runState{
		cancel: cancel,
		hub:    hub,
		live:   live,
		ctrl:   ctrl,
	}
	s.runWG.Add(1)

	s.runsMu.Unlock()

	tl.registeredAt = time.Now()

	// v1.2.4: mark the run active for the memory policy — coordinated
	// cleanup (cache shedding, GC) is deferred while a generation is live,
	// and runs as soon as the run ends (success, error or abort; the defer
	// below is cancellation-aware).
	if s.stack.MemMgr != nil {
		s.stack.MemMgr.TrackRunStart()
	}

	// v1.1.2: release every activity connection parked in standby for
	// this session so they attach to the new run hub immediately.
	s.wakeStandby(sess.ID)

	// runSettled records the terminal outcome exactly once (the goroutine
	// below has several exit paths; every one of them settles the run).
	var settled sync.Once

	// v1.9.4: the run goroutine publishes the orchestrator's measured
	// timeline here after RunDetailed returns; settle() reads it for
	// the summary line. Written and read on the SAME goroutine (every
	// settle path — including the panic-recover path — runs inside the
	// run goroutine), so no synchronization is required. Zero on early
	// settles (engine-gate refusals never reached the orchestrator).
	var runTiming agent.Timing

	outcome := runOutcome{
		RunID:     runID,
		StartedAt: tl.registeredAt,
		Outcome:   "error",
	}

	settle := func(result string, caption string, persisted bool, replyText, reasonText string) {
		settled.Do(func() {
			outcome.EndedAt = time.Now()
			outcome.Outcome = result
			outcome.Caption = caption
			outcome.Persisted = persisted
			outcome.ReplyChars = len(replyText)
			outcome.ReasonChars = len(reasonText)

			// v1.2.7: record the outcome BEFORE the authoritative
			// state flips terminal. This closes the last visibility
			// gap of the v1.2.6 design: a socket that observes the
			// run as terminal (live.snapshot().Running == false,
			// set by settleTerminal below) can then never miss the
			// lastRun block of its idle sentinel — the outcome is
			// already in the bounded registry at that moment.
			// The happens-before edge runs through settleTerminal's
			// mutex, so every reader of the terminal state also
			// observes the recorded outcome.
			if s.outcomes != nil {
				s.outcomes.record(sess.ID, outcome)
			}

			// v1.2.6 continuation: the AUTHORITATIVE terminal
			// state (with the persisted reply snapshots a
			// late-attaching socket replays).
			live.settleTerminal(result, caption, persisted, replyText, reasonText)

			ms := func(a, b time.Time) int64 {
				if a.IsZero() || b.IsZero() || b.Before(a) {
					return 0
				}
				return b.Sub(a).Milliseconds()
			}

			// v1.9.4 METRIC CLARIFICATION (P1): the previous
			// summary labelled acceptedAt→donePublished as
			// "publishMs", which read as a per-event WebSocket
			// write/publish latency — it never was one. The
			// line now names every boundary honestly:
			//
			//   gateMs          acceptedAt → engine gate ready
			//   ttftMs          the orchestrator's MEASURED
			//                   request_sent → first_token
			//                   (agent.Timing stage clock; 0
			//                   when no token was reached)
			//   persistMs       gate ready → reply persisted
			//   acceptToDoneMs  acceptedAt → done published
			//                   (the mislabelled publishMs —
			//                   dominated by generation time)
			//   totalMs         acceptedAt → settled
			//   events/deltas/  aggregate published-event
			//   firstDeltaMs    counters (counts only)
			//
			// Client-side boundaries (socket queue delay, write
			// time, browser receipt, DOM visibility) are NOT
			// derivable here; they live in the opt-in test-only
			// stream trace (src/stream-trace.ts), never in the
			// ordinary application log. All durations are
			// monotonic wall-clock ms; identifiers follow the
			// existing run-summary contract.
			logging.Default().Info(
				"run",
				// v1.9.5 LOG CONTRACT: no runId/session in
				// ordinary logs — outcome + identifier-free
				// durations/counters only.
				"run settled: outcome=%s gateMs=%d ttftMs=%d persistMs=%d acceptToDoneMs=%d totalMs=%d replyChars=%d events=%d deltas=%d firstDeltaMs=%d",
				result,
				ms(tl.acceptedAt, tl.gateReady),
				runTiming.TTFTMs,
				ms(tl.gateReady, tl.persistedAt),
				ms(tl.acceptedAt, tl.donePublished),
				ms(tl.acceptedAt, outcome.EndedAt),
				len(replyText),
				tl.events,
				tl.deltas,
				ms(tl.acceptedAt, tl.firstDeltaAt),
			)
		})
	}

	go func() {
		// v1.10.1: join bookkeeping FIRST in LIFO defer order — this
		// Done must fire only after every deferred cleanup above has
		// finished (telemetry persistence, registry release, hub
		// close), so Close's wait observes the goroutine FULLY done.
		defer s.runWG.Done()

		// v1.7.4: true when the run ended PAUSED — the run stays
		// registered with an open hub and a durable checkpoint; the
		// deferred cleanup must NOT tear those down.
		paused := false

		defer func() {
			// v1.7.4 P0 — GENERATION MUST NEVER SILENTLY DIE:
			// this goroutine previously had NO recover, so any
			// panic in the orchestrator path killed the entire
			// desktop process with no error activity and no
			// crash report (the supplied v1.7.3 Windows runtime
			// death signature). A recovered panic settles the
			// run as an honest error, publishes the failure,
			// writes a Windows-diagnosable crash report file and
			// leaves the parent app ALIVE for the next chat.
			if r := recover(); r != nil && !paused {
				buf := make([]byte, 16384)
				n := goruntime.Stack(buf, false)
				path := logging.Default().Crash(r, buf[:n])

				logging.Default().Error(
					"run",
					// v1.9.5 LOG CONTRACT: no runId in ordinary logs.
					"internal panic recovered — run settled as error, app stays alive (crash report: %s): %v",
					path, r,
				)

				settle(
					"error",
					fmt.Sprintf("Internal error — the run stopped safely. Crash report: %s", filepath.Base(path)),
					false, "", "",
				)

				a := agent.Activity{
					Type:      "error",
					RunID:     runID,
					Caption:   "The run stopped safely after an internal error. You can retry — the app is still running.",
					Timestamp: time.Now(),
					Seq:       live.nextSeq(),
				}
				live.observe(a)
				hub.publish(a)
			}

			// release the run-budget timer with the run itself
			budgetCancel()

			if paused {
				// PAUSED run: the registry entry, the hub and
				// the durable checkpoint stay — resume and
				// reconnect continue from exactly here.
				logging.Default().Info("run",
					"run paused: checkpoint kept, hub left open")
				return
			}

			// v1.2.4: run ended — the memory manager releases one-shot
			// state (image cache, cold session cache) and persists
			// coalesced telemetry, with measured before/after stats.
			if s.stack.MemMgr != nil {
				s.stack.MemMgr.TrackRunEnd()
			}

			s.runsMu.Lock()

			// Only remove the run if this goroutine still owns the current
			// session entry. A newer run may already have replaced it.
			if current, ok := s.runs[sess.ID]; ok && current.hub == hub {
				delete(s.runs, sess.ID)
			}

			s.runsMu.Unlock()

			hub.close()
			cancel()
		}()

		// v1.1.3: gate every run on a real, healthy engine. A cold start
		// here streams the authoritative engine transitions to the UI
		// (starting → ready) while the request waits, bounded by the
		// engine gate timeout.
		// v1.2.6: the gate stages are MEASURED (engineGateStart/Ready) so
		// a stalled run reveals WHERE it is stalled.
		tl.gateStart = time.Now()

		gateCtx, gateCancel := context.WithTimeout(ctx, 3*time.Minute)

		// v1.2.6 continuation: EVERY activity of this run flows through
		// ONE publisher — sequence-stamped (monotonic, per-run) and
		// folded into the authoritative live state BEFORE it reaches
		// any subscriber. The replay contract (snapshot at seq N +
		// events with seq > N) depends on this single path.
		publish := func(a agent.Activity) {
			a.RunID = runID
			a.Seq = live.nextSeq()
			live.observe(a)
			hub.publish(a)

			// v1.9.4: aggregate published-event counters for the
			// settle summary — counts and one timestamp, never
			// captions (see runTimeline).
			tl.events++
			switch a.Type {
			case "response", "assistant_delta", "reasoning", "thinking_delta":
				tl.deltas++
				if tl.firstDeltaAt.IsZero() {
					tl.firstDeltaAt = time.Now()
				}
			}
		}

		// v1.10.13 Task 30 (run #33 R2 root cause): the orchestrator's
		// terminal `done`/`complete` activities hold here until the durable
		// transcript write commits. The pinned defect: those frames are
		// published INSIDE RunDetailed, while the assistant reply's
		// AppendMessage runs in finishSuccessfulRun — after RunDetailed
		// returns. A client that reloads the transcript when the settle
		// frame arrives (the Qt client's handleActivityFrame reloads
		// EXACTLY once on done/complete) races the append; on a fast local
		// pipe the reload wins, reads a transcript without the reply, and
		// a plain chat publishes nothing after the append — so the stale
		// one-message model persists past every bound (the Windows
		// run-#26/#33 chat-persist 'firstPersisted' FALSE class).
		// v1.2.9's own rule already states the contract: "every durable
		// write happens BEFORE the terminal publication". Enforced at this
		// ONE broadcast boundary: relative frame order and the monotonic
		// seq contract are unchanged (seq stamps assign at flush); the
		// frames are re-ordered behind the durability they announce —
		// never dropped: every post-orchestrator exit path below reaches
		// a flush, and a run that panics after a hold settles as error,
		// whose terminal truth makes the held pre-panic frames
		// deliberately redundant.
		var heldTerminal []agent.Activity
		flushHeldTerminal := func() {
			for _, a := range heldTerminal {
				publish(a)
			}
			heldTerminal = nil
		}

		// v1.6.0 P0 ordering invariant (repair): maintenance
		// completion < engine prewarm/start — the STARTUP gate owns
		// that ordering for the boot prewarm, and the run gate must
		// honor it too: a run submitted while the maintenance check
		// (or transactional engine update) is still in flight WAITS
		// for the gate instead of racing an engine start against the
		// maintenance decision. Bounded by the run's own engine-gate
		// deadline; a gate that never existed (setup-less callers)
		// skips the wait.
		if g := s.gateValue(); g != nil {
			select {
			case <-g.Done():
			case <-gateCtx.Done():
				gateCancel()

				settle("error", fmt.Sprintf(
					"Engine unavailable: startup maintenance still in progress: %v",
					gateCtx.Err(),
				), false, "", "")

				publish(agent.Activity{
					Type:      "error",
					RunID:     runID,
					Caption:   "Engine unavailable: startup maintenance still in progress",
					Timestamp: time.Now(),
				})

				return
			}
		}

		// v1.7.1 PRE-RUN COMPATIBILITY GATE: evaluate the ONE authoritative
		// preflight report BEFORE any engine start. A hard incompatibility
		// refuses the run here — PREFLIGHT → REFUSE → NO ENGINE START — with
		// the honest reason and a valid alternative, never a crash to watch.
		if pre := s.stack.PreflightReport(0); !pre.Compatible &&
			pre.Severity == preflight.SeverityIncompatible {
			gateCancel()
			msg := preflight.RefusalMessage(pre)
			logging.Default().Warn("preflight", "run refused before engine start: model=%s backend=%s", pre.Model, pre.Backend)
			settle("error", msg, false, "", "")
			publish(agent.Activity{
				Type:      "error",
				RunID:     runID,
				Caption:   msg,
				Timestamp: time.Now(),
			})
			return
		}

		if err := s.stack.EnsureLLMContext(gateCtx); err != nil {
			gateCancel()

			settle("error", fmt.Sprintf("Engine unavailable: %v", err), false, "", "")

			publish(agent.Activity{
				Type:  "error",
				RunID: runID,
				Caption: fmt.Sprintf(
					"Engine unavailable: %v",
					err,
				),
				Timestamp: time.Now(),
			})

			return
		}

		tl.gateReady = time.Now()

		gateCancel()

		// v1.2.6: every activity of THIS run carries the runId — one
		// identifier from POST /api/run to the last WS frame. The
		// publish wrapper (above) owns the stamping now; stampRun
		// remains for the few direct hub publishes below it.
		stampRun := func(a agent.Activity) agent.Activity {
			a.RunID = runID
			return a
		}

		// terminalCaption captures the orchestrator's own final
		// `done`/`complete` caption ("Completed", "Aborted by user",
		// "Run stopped…") — the honest terminal label, reused verbatim
		// by the outcome record instead of being re-derived.
		terminalCaption := ""

		// v1.8.0: the orchestrator's abort paths publish `aborted` (never
		// `done`), so the honest terminal label is captured from that
		// type too.
		captureTerminal := func(a agent.Activity) {
			if a.Type == "done" || a.Type == "complete" || a.Type == "aborted" {
				tl.donePublished = time.Now()

				if a.Caption != "" {
					terminalCaption = a.Caption
				}
			}
		}

		// 1.1.6 §4: the run carries the session's own context
		// policy — the orchestrator plans, gates and sends the
		// request under THIS chat's resolved window.
		res, err := s.orch.RunDetailed(
			ctx,
			messages,
			func(a agent.Activity) {
				// v1.10.13 Task 30: the orchestrator's terminal
				// frames hold behind the durable append (see
				// flushHeldTerminal above). Their session
				// activity log keeps the origin moment — the
				// same AppendActivity the unheld path performs;
				// only the CLIENT broadcast defers.
				if a.Type == "done" || a.Type == "complete" {
					captureTerminal(a)
					if err := s.store.AppendActivity(
						sess.ID,
						sessions.ActivityEntry{
							Type:      a.Type,
							Caption:   a.Caption,
							Timestamp: a.Timestamp,
						},
					); err != nil {
						logging.Default().Warn(
							"api",
							"append activity: %v",
							err,
						)
					}
					heldTerminal = append(heldTerminal, a)
					return
				}
				publish(a)

				captureTerminal(a)

				// Persist milestone events only. Streaming response/reasoning
				// deltas are intentionally not persisted individually because
				// each persistence operation rewrites the session JSON.
				// v1.2.5: the status/thinking-marker deltas join the
				// skip list (tiny, high-frequency, no persisted value).
				switch a.Type {
				case "response", "thinking", "reasoning",
					"status", "thinking_start", "thinking_end":
					return
				}

				// v1.1.4: persistence failures surface as a
				// visible warning instead of vanishing.
				if err := s.store.AppendActivity(
					sess.ID,
					sessions.ActivityEntry{
						Type:      a.Type,
						Caption:   a.Caption,
						Timestamp: a.Timestamp,
					},
				); err != nil {
					logging.Default().Warn(
						"api",
						"append activity: %v",
						err,
					)
				}
			},
			agent.WithSessionContext(sess.Context.ContextTokens),
			// v1.9.0: the wire-boundary reasoning resolution — the
			// request's explicit level wins; an unset/auto request
			// adopts the AI System effort preference (the SAME
			// v1.8.5 ladder: low=0, mid=1024, high=4096, ultra=engine
			// default; never a fabricated budget).
			agent.WithThinkingMode(body.Thinking),
			agent.WithToolPolicy(body.ToolMode, body.ToolAllow),
			agent.WithNetSearch(body.NetSearch),
			// v1.9.0: the FROZEN AI System binding — instructions,
			// model override, tool-surface constrain, approval and
			// verification policies ride this one snapshot.

			agent.WithReceivedAt(receivedAt),
			agent.WithSessionSummaryBlock(summaryBlock),
			agent.WithHistoryBlocks(histRefBlocks),
			// v1.7.1: identity for the context-exhaustion recovery record.
			agent.WithRunIdentity(sess.ID, sess.ThreadID, runID),
			// v1.7.4: the pause control — the orchestrator stops at
			// safe boundaries when the pause handler flips it.
			agent.WithRunControl(ctrl),
		)

		// v1.9.4: publish the measured timeline for the settle
		// summary (same goroutine — see the runTiming note above).
		runTiming = res.Timing

		// v1.9.4: publish the measured timeline for the settle
		// summary (same goroutine — see the runTiming note above).
		runTiming = res.Timing

		// v1.7.4 PAUSE: the run stopped at a SAFE BOUNDARY — not an
		// error, not a completion. The durable checkpoint is
		// persisted BEFORE the state promises PAUSED, and the run
		// (registry entry + hub + runLive) stays alive for resume.
		if err == nil && res.Paused {
			// v1.10.13 Task 30: a paused run cannot hold terminal
			// frames (the orchestrator pauses at its boundaries,
			// before any done publish) — flush defensively so no
			// early return can ever silently drop a held frame.
			flushHeldTerminal()
			// v1.7.5 ZOMBIE CHECKPOINT GUARD: a run whose session
			// slot was taken by a newer run must never checkpoint.
			// The replacement cancelled this context and consumed
			// any earlier checkpoint; writing one now would park a
			// paused record that is resumable over the live run.
			s.runsMu.Lock()
			current, stillOwns := s.runs[sess.ID]
			s.runsMu.Unlock()

			if !stillOwns || current.hub != hub {
				settle("aborted", "Run replaced by a newer request before it could pause", false, res.Text, res.Reasoning)
				return
			}

			paused = true

			bootCfg := s.src.Load()

			rec := &pausedRunRecord{
				Version:             1,
				CreatedAt:           time.Now(),
				UpdatedAt:           time.Now(),
				RunID:               runID,
				SessionID:           sess.ID,
				OriginalUserMessage: body.Message,
				AssistantDraft:      res.Text,
				ReasoningDraft:      res.Reasoning,
				ToolState:           string(res.PauseToolState),
				PendingToolCalls:    res.PendingToolCalls,
				RoundTail:           completeRoundTail(res.RoundTail, res.PendingToolCalls),
				Model:               bootCfg.EffectiveModel(),
				Backend:             bootCfg.ProviderKind(),
				Thinking:            body.Thinking,
				ToolMode:            body.ToolMode,
				ToolAllow:           body.ToolAllow,
				NetSearch:           body.NetSearch,
				ContinuationMode:    "semantic",
			}

			if err := s.savePausedRecord(rec); err != nil {
				// An unpersistable pause must never masquerade as
				// paused: settle honestly — the prefix is lost
				// unless the model re-runs it.
				settle("error", "pause checkpoint failed: "+err.Error(), false, res.Text, res.Reasoning)

				publish(stampRun(agent.Activity{
					Type:      "error",
					Caption:   "The run could not be paused safely: " + err.Error(),
					Timestamp: time.Now(),
				}))

				return
			}

			// Checkpoint is durable — only NOW does the state say PAUSED.
			live.confirmPaused(res.Text, "paused at a safe boundary")

			publish(stampRun(agent.Activity{
				Type:    "status",
				Caption: "Paused — you can edit or resume this run",
				Detail: map[string]any{
					"phase":    "paused",
					"revision": live.currentRevision(),
					// v1.7.5: the accepted draft rides the
					// confirmation — the live client renders
					// the paused editor without a reconnect.
					"pausedDraft": live.snapshot().PausedDraft,
				},
				Timestamp: time.Now(),
			}))

			logging.Default().Info("run",
				// v1.9.5 LOG CONTRACT: no runId/session in ordinary logs.
				"run paused at a safe boundary (toolState=%s draftChars=%d)",
				res.PauseToolState, len(res.Text))

			return
		}

		if err != nil {
			// v1.10.13 Task 30: flush before the error settles —
			// if the orchestrator ever held a terminal frame and
			// then returned an error (a contract violation), the
			// client still sees the true sequence done→error;
			// nothing is silently swallowed (I3).
			flushHeldTerminal()

			settle("error", err.Error(), false, res.Text, res.Reasoning)

			publish(stampRun(agent.Activity{
				Type:      "error",
				Caption:   err.Error(),
				Timestamp: time.Now(),
			}))

			return
		}

		// v1.2.6: settle the outcome from REAL signals — the run
		// context's cancellation state (abort) and the orchestrator's
		// own terminal caption, never a re-derived guess.
		resultOutcome := "done"
		if ctx.Err() != nil {
			resultOutcome = "aborted"
		}

		if terminalCaption == "" {
			terminalCaption = "Completed"
		}

		// v1.7.4: the durable settlement (assistant append →
		// summary → handoff → recall → continuum → terminal
		// publication → post-terminal UI events) is ONE shared
		// method, exercised identically by a fresh run and a
		// resumed run — one lifecycle authority, no duplicated
		// settlement logic.
		s.finishSuccessfulRun(successRunParams{
			sess:            sess,
			userMessage:     body.Message,
			res:             res,
			resultOutcome:   resultOutcome,
			terminalCaption: terminalCaption,
			tl:              &tl,
			settle:          settle,
			publish:         publish,
			stampRun:        stampRun,
			flushTerminal:   flushHeldTerminal,
		})
	}()

	// v1.2.6 continuation: POST completion must never imply the live
	// transport was attached — but it MUST hand back the authoritative
	// run identity so the client can attribute every subsequent frame
	// (and discard stale ones from an older run of the same session).
	response := map[string]any{
		"ok":        true,
		"sessionId": sess.ID,
		"runId":     runID,
		"state":     "registered",
	}
	// v1.9.0: the frozen binding travels with the response so the
	// client can display exactly which configuration this run
	// executes under.
	if aiSnap != nil {
		response["systemId"] = aiSnap.SystemID
		response["systemRevision"] = aiSnap.SystemRevision
	}
	writeJSON(w, response)
}

func (s *Server) handleAbort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}

	// v1.1.4: bounded body AND a decode failure that previously
	// returned {ok:true} while aborting nothing.
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

	var body struct {
		SessionID string `json:"sessionId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("abort: %w", err))
		return
	}

	if body.SessionID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId required"))
		return
	}

	s.runsMu.Lock()
	rs, ok := s.runs[body.SessionID]
	s.runsMu.Unlock()

	if ok {
		// v1.7.4: a PAUSED run has no generation to cancel — the
		// goroutine already returned. Stopping it settles the run
		// as aborted, closes the hub, drops the registry entry and
		// deletes the checkpoint. The transcript keeps the user
		// message; no partial draft ever becomes an assistant
		// message.
		if rs.live.isPaused() {
			// v1.7.5: serialize against an in-flight edit/resume
			// transaction (the control mutex is bounded file work,
			// never the generation) and RE-CHECK — the run may have
			// been resumed while this stop waited.
			rs.ctrlMu.Lock()

			if rs.live.isPaused() {
				// v1.7.4: the bounded outcome registry must carry this
				// terminal too — every other settle path records the
				// outcome BEFORE the authoritative state flips
				// terminal (the v1.2.7 ordering). Without it the idle
				// sentinel after stopping a paused run carried no
				// lastRun block and the UI could never finalise the
				// stop.
				if s.outcomes != nil {
					s.outcomes.record(body.SessionID, runOutcome{
						RunID:     rs.live.runID,
						StartedAt: rs.live.startedAt,
						EndedAt:   time.Now(),
						Outcome:   "aborted",
						Caption:   "Run stopped while paused",
					})
				}

				rs.live.settleTerminal("aborted", "Run stopped while paused", false, "", "")

				s.runsMu.Lock()
				if current, ok := s.runs[body.SessionID]; ok && current == rs {
					delete(s.runs, body.SessionID)
				}
				s.runsMu.Unlock()

				rs.hub.close()
				s.deletePausedRecord(rs.live.runID)

				logging.Default().Info("run", "paused run stopped")

				rs.ctrlMu.Unlock()

				writeJSON(w, map[string]any{
					"ok": true,
				})
				return
			}

			// The run resumed while the stop waited — fall through to
			// the generation cancel below.
			rs.ctrlMu.Unlock()
		}

		// v1.7.5: cancel the CURRENT generation (a resumed run owns a
		// fresh context — the registration cancel no longer reaches it).
		rs.cancelCurrent()
	}

	writeJSON(w, map[string]any{
		"ok": true,
	})
}

// --- Recall feedback ---

// handleFeedback records the user's 👍/👎 verdict for one past exchange
// (v1.1.4). The recall feedback sidecar (SetFeedback / FeedbackFor and
// the liked×1.25 / disliked×0.6 scoring boosts) existed since v1.0.6 with
// NO write path — the steering could never fire. The frontend sends the
// query text of the exchange it is rating; the deterministic capsule id
// is derived exactly like IndexTurn does.
func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

	var body struct {
		SessionID string `json:"sessionId"`
		Query     string `json:"query"`
		Liked     bool   `json:"liked"`
		Clear     bool   `json:"clear,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	if body.SessionID == "" || body.Query == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId and query required"))
		return
	}

	if s.recall == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("recall engine unavailable"))
		return
	}

	id := recall.CapsuleID(body.SessionID, body.Query)

	fb := 0
	if !body.Clear {
		if body.Liked {
			fb = 1
		} else {
			fb = -1
		}
	}

	if err := s.recall.SetFeedback(id, fb); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, map[string]any{
		"ok": true,
	})
}

// --- WebSocket: live activity ---

// wsPingInterval is how often an idle standby connection is pinged at the
// protocol level so half-open TCP sockets are detected and released.
const wsPingInterval = 25 * time.Second

// wsEventWriteDeadline (v1.8.5) bounds ONE run-event write to a live
// subscriber. Local run frames are a few KB — a healthy transport writes
// them in microseconds — so 30 s is far beyond any legitimate delivery
// while still guaranteeing a wedged/stalled client transport (the exact
// runtime class whose backpressure froze streamed text until Stop) fails
// the write deterministically instead of parking the subscriber goroutine
// forever. A failed write tears the socket down; the client's reconnect
// attaches to the authoritative run_snapshot and recovers the full state.
// Not a timeout on generation: the publisher-side offer never blocks.
const wsEventWriteDeadline = 30 * time.Second

// idleSettleWaitBudget (v1.3.7) bounds how long an idle sentinel may WAIT on
// a terminal-but-unsettled run's settlement edge before degrading to the
// legacy no-lastRun sentinel. Real settle-tails are pure durable file I/O
// (summary roll, agent.md handoff, recall index, continuum rollover — no
// LLM call), so single-digit milliseconds is the norm; the budget exists so
// a wedged durable step can never strand a WebSocket attach indefinitely.
const idleSettleWaitBudget = 10 * time.Second

// standbyRecheckInterval (v1.2.8.1) bounds how long a standby connection
// can miss a wake signal — it re-checks the runs map itself. The wake
// channel fires for runs that start AFTER the connection entered standby;
// a run that replaced another one in the same instant (old hub closed,
// wakeStandby fired before the new sc existed) is caught by this re-check.
const standbyRecheckInterval = 2 * time.Second

func (s *Server) handleActivityWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")

	if sessionID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId required"))
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	defer conn.Close()

	// v1.1.2: the read pump owns every incoming frame. It detects client
	// disconnects (read error closes clientGone) and routes abort actions
	// to whatever run is currently active for the session.
	clientGone := make(chan struct{})

	go func() {
		defer close(clientGone)

		conn.SetReadLimit(1024)

		for {
			var msg map[string]any

			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			if action, _ := msg["action"].(string); action == "abort" {
				s.runsMu.Lock()
				rs, ok := s.runs[sessionID]
				s.runsMu.Unlock()

				if ok {
					rs.cancel()
				}
			}
		}
	}()

	idleSentinel := func() map[string]any {
		// v1.3.7 SETTLEMENT BARRIER — the fix for the Actions run
		// 35749698189 failure (TestStaleRunEventsFilteredByServer:
		// "idle sentinel carries no lastRun outcome — the v1.2.6
		// terminal replay regressed").
		//
		// The race: the run's terminal VISIBILITY flips early —
		// observe() folds the orchestrator's `done` activity and
		// sets running=false — while the terminal OUTCOME is
		// recorded only later, by settle(), after every durable
		// settle-tail artifact (reply persist → summary roll →
		// agent.md handoff → recall index → continuum rollover,
		// the v1.2.9 durable completion ordering). A socket that
		// attaches inside that window reached the idle path, read
		// an EMPTY registry, and emitted a lastRun-less sentinel
		// the frontend could not finalise from.
		//
		// The barrier: when the session's registered run is
		// terminal but its settlement edge is still open, WAIT for
		// it — deterministic synchronization on the edge closed
		// by settleTerminal (which settle() calls strictly after
		// outcomes.record), never a sleep. The invariant restored:
		// a run visible as no longer running ALWAYS has a
		// recoverable outcome on the idle/lastRun path. The wait
		// is bounded so a pathological settle-tail degrades to the
		// old sentinel instead of hanging the socket forever, and
		// it releases instantly when the client is gone.
		s.runsMu.Lock()

		var live *runLive

		if rs, ok := s.runs[sessionID]; ok && rs != nil && rs.live != nil {
			live = rs.live
		}

		s.runsMu.Unlock()

		if live != nil && !live.snapshot().Running {
			select {
			case <-live.settledSignal():
				// Outcome recorded + state settled — build
				// the sentinel with the lastRun block below.
			case <-clientGone:
				// The client left while the run was still
				// settling; the caller's next write fails
				// and tears the connection down.
			case <-time.After(idleSettleWaitBudget):
				// Safety valve: a settle-tail stuck past
				// the budget (e.g. wedged durable step)
				// must not strand the socket. The sentinel
				// degrades to the legacy no-lastRun form;
				// the registry still serves later attaches.
			}
		}

		// v1.2.6: the idle sentinel now carries the AUTHORITATIVE last
		// run outcome (bounded registry). A socket that attaches after
		// a run finished — the exact case that previously left the UI
		// stuck with no visible answer — receives the evidence it needs
		// to finalise: runId, end time, outcome and whether a reply was
		// persisted. Legacy fields stay identical for old clients.
		frame := map[string]any{
			"type":    "idle",
			"caption": "No active run",
		}

		if s.outcomes != nil {
			if last, ok := s.outcomes.latest(sessionID); ok {
				frame["lastRun"] = last
			}
		}

		return frame
	}

	// v1.2.6 continuation: the DETERMINISTIC ATTACH ACKNOWLEDGEMENT.
	// The FIRST frame every socket receives after the protocol upgrade
	// is an explicit `attached` marker — the client waits for THIS frame
	// before POSTing /api/run, so "POST completed" can never silently
	// mean "the live transport was already attached" (the socket may
	// still have been CONNECTING when the POST fired — the v1.2.6 race).
	// The frame also carries the server clock so clients can interpret
	// timestamp deltas honestly.
	_ = conn.WriteJSON(map[string]any{
		"type":      "attached",
		"sessionId": sessionID,
		"serverNow": time.Now().UnixMilli(),
	})

	for {
		// Fast path: a run is already active — attach to its hub.
		s.runsMu.Lock()
		rs, ok := s.runs[sessionID]
		s.runsMu.Unlock()

		// v1.2.7: AUTHORITATIVE LIFECYCLE GATE. Map membership alone
		// is NOT "a run is active": between a run's terminal
		// settlement and its registry cleanup the entry still exists
		// while the run is already authoritatively terminal
		// (live.snapshot().Running == false — the deferred cleanup
		// releases the run budget and the memory-manager run tracking
		// BEFORE it deletes the entry). A socket attaching inside
		// that window previously received a stale terminal
		// run_snapshot and then parked on clientGone without any
		// terminal marker — the exact CI failure of run 35367243405
		// (TestStaleRunEventsFilteredByServer: "expected idle, got
		// run_snapshot"). The authoritative runLive state decides:
		// a terminal entry falls through to the standby path, whose
		// idle sentinel carries the recorded lastRun outcome.
		// runLive remains the ONE lifecycle authority — no second
		// one is introduced here.
		if ok && rs != nil && rs.live != nil && !rs.live.snapshot().Running {
			ok = false
		}

		if ok {
			// v1.2.6 continuation: AUTHORITATIVE REPLAY.
			//
			// Subscribe FIRST, then read the run's authoritative
			// snapshot. Events published between the subscribe and
			// the snapshot read carry seq ≤ snapshot.Sequence
			// (they are already folded into the cumulative
			// snapshots the frame carries) and are filtered below;
			// events published after carry seq > snapshot.Sequence
			// and are forwarded. (snapshot at N) + (events > N)
			// is therefore gapless AND duplicate-free — the
			// response/reasoning captions are cumulative by
			// contract, so the fold is idempotent.
			_, updates, unsubscribe := rs.hub.subscribe()

			snap := rs.live.snapshot()

			snapFrame := map[string]any{
				"type":            "run_snapshot",
				"runId":           snap.RunID,
				"sessionId":       snap.SessionID,
				"phase":           snap.Phase,
				"running":         snap.Running,
				"sequence":        snap.Sequence,
				"startedAt":       snap.StartedAt,
				"latestResponse":  snap.LatestResponse,
				"latestReasoning": snap.LatestReasoning,
				"latestStatus":    snap.LatestStatus,
				"terminalOutcome": snap.TerminalOutcome,
				"persisted":       snap.Persisted,
				"error":           snap.Error,
			}

			// v1.2.8.1 REPAIR: the task state was folded into the
			// authoritative snapshot (runLive.latestTask) but the
			// hand-built wire frame never carried it — a socket
			// reconnecting mid-run restored the streamed text yet
			// lost the whole task panel. The frame now carries the
			// same task block the in-process snapshot holds.
			if snap.Task != nil {
				snapFrame["task"] = snap.Task
			}

			// v1.7.4 run-control surface: the checkpoint revision
			// and the accepted partial draft ride the replay
			// frame — a reconnecting client during PAUSED
			// re-renders the draft and resumes against the SAME
			// revision (runstate.go's runSnapshot is the
			// authority; the paused store re-syncs from this
			// frame). Previously the frame dropped both fields
			// and a reconnect during PAUSED lost the draft and
			// resumed with a stale revision.
			snapFrame["revision"] = snap.Revision

			if snap.PausedDraft != "" {
				snapFrame["pausedDraft"] = snap.PausedDraft
			}

			if !snap.EndedAt.IsZero() {
				snapFrame["endedAt"] = snap.EndedAt
			}

			if err := conn.WriteJSON(snapFrame); err != nil {
				unsubscribe()
				return
			}

			served := false

			// v1.8.5: the conflation-aware subscription replaces
			// the channel range. next() blocks exactly like the
			// old channel receive, drains every survivor before
			// reporting closure, and preserves the replay filters
			// verbatim. Each event write carries a generous
			// deadline: a wedged client transport fails the write
			// deterministically instead of parking this goroutine
			// forever — the client's reconnect + run_snapshot
			// replay then recovers the full state.
			for {
				ev, ok := updates.next()
				if !ok {
					break
				}

				// Replay filter: skip events already folded
				// into the snapshot (seq ≤ N) and stale
				// events of an older run that drained after
				// this socket's run replaced it.
				if ev.RunID != "" && ev.RunID != snap.RunID {
					continue
				}

				if ev.Seq > 0 && ev.Seq <= snap.Sequence {
					continue
				}

				_ = conn.SetWriteDeadline(
					time.Now().Add(wsEventWriteDeadline))

				if err := conn.WriteJSON(ev); err != nil {
					served = true
					break
				}

				served = true
			}

			unsubscribe()

			// Hub closed (run finished) or write failed. On write
			// failure the client is gone; wait for clientGone so
			// the read pump teardown wins the race deterministically.
			if served {
				select {
				case <-clientGone:
					return
				default:
				}
			}

			// v1.2.7: a hub that closed before ANY post-snapshot
			// event existed means everything the run published is
			// already folded into the snapshot this socket holds.
			// The previous behavior parked on clientGone here,
			// stranding such a socket without a terminal marker
			// (the abort path, for one, settles without publishing
			// a terminal activity). Fall through to the idle
			// sentinel — it carries the recorded lastRun outcome —
			// and the standby loop below, which re-checks the runs
			// map and attaches to a replacement run the moment one
			// starts.

			// Run finished: fall through to standby.
			_ = conn.WriteJSON(idleSentinel())
		}

		// Standby: park the connection until a run starts, an engine
		// transition arrives, the client leaves, or a keepalive ping
		// is due.
		sc := s.enterStandby(sessionID)

		_ = conn.WriteJSON(idleSentinel())

		disconnected := false

		for !disconnected {
			select {
			case <-sc.wake:
				disconnected = false
				goto attached

			// v1.2.8.1: periodic runs-map re-check. Closes the
			// missed-wake race where a replacement run registered
			// BETWEEN the old hub's close and this connection
			// entering standby fired wakeStandby() before this sc
			// existed — the socket then parked through the ENTIRE
			// replacement run. The lifecycle gate (a terminal
			// entry falls through) is honored here too.
			case <-time.After(standbyRecheckInterval):
				s.runsMu.Lock()
				rsNow, active := s.runs[sessionID]
				s.runsMu.Unlock()

				if active && rsNow != nil && rsNow.live != nil && rsNow.live.snapshot().Running {
					disconnected = false
					goto attached
				}

			case frame := <-sc.engineCh:
				// Idle connections still observe engine state
				// transitions (authoritative backend state).
				if err := conn.WriteMessage(
					websocket.TextMessage,
					frame,
				); err != nil {
					disconnected = true
				}

			case <-clientGone:
				disconnected = true

			case <-time.After(wsPingInterval):
				_ = conn.WriteControl(
					websocket.PingMessage,
					nil,
					time.Now().Add(5*time.Second),
				)
			}
		}

		s.leaveStandby(sessionID, sc)
		return

	attached:
		s.leaveStandby(sessionID, sc)
	}
}

// --- helpers ---

// framedJSON is the ONE response-writing boundary for complete JSON
// responses: the value marshals fully, the EXACT byte count is declared as
// Content-Length, and only then do the bytes go out. This is the client's
// documented transport contract (coreclient.cpp: every body completes by
// declared Content-Length or close-delimited framing; chunked transfer
// encoding is not claimed by the client and fails JSON parsing upstream —
// a typed error, not a hang). Go's net/http buffers only ~2 KB before it
// must choose a framing: an undeclared body larger than that went out
// chunked (HTTP/1.1) — run #34's parked goals list was the project's
// first response to cross the buffer, arrived chunked on BOTH platforms,
// and the parked acceptance slot died parsing it while every smaller
// answer carried Content-Length. Declaring the length here kills the
// entire class: no JSON response of any size can reach the client
// unframed again (I6 at the transport boundary).
func framedJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// Marshaling an API response value failing is a server bug; still
		// answer framed — never swap a bad body for an unframed one.
		body = []byte(`{"error":"response encoding failed"}`)
	}
	body = append(body, '\n') // byte-identical to the previous Encoder form
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, v any) {
	framedJSON(w, http.StatusOK, v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	framedJSON(w, code, map[string]string{
		"error": err.Error(),
	})
}

func errMethodNotAllowed() error {
	return fmt.Errorf("method not allowed")
}

// equalHistoryRefs (v1.2.8.1) compares two reference lists by value — the
// persisted-context merge is skipped when nothing actually changed, so a
// run never bumps the session's UpdatedAt (and never rewrites the file)
// just because the same refs were sent again.
func equalHistoryRefs(a, b []sessions.HistoryRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].SessionID != b[i].SessionID || a[i].Mode != b[i].Mode ||
			a[i].SummaryVersion != b[i].SummaryVersion {
			return false
		}
		if len(a[i].Ranges) != len(b[i].Ranges) {
			return false
		}
		for j := range a[i].Ranges {
			if a[i].Ranges[j] != b[i].Ranges[j] {
				return false
			}
		}
	}
	return true
}

// validateHistoryRefs (v1.2.8.1) applies the SERVER-SIDE cross-mode
// contract to a reference list for one session: normalize (dedup + cap),
// drop self-references, drop same-mode references (the source mode is
// resolved from the store index — the client payload is never trusted)
// and prune dead references (source session vanished). Shared by the run
// path (body refs merged with the persisted context, body-first) and the
// full-context PUT.
//
// v1.2.9: dead references are actually PRUNED. The store index resolves
// every session to a NON-EMPTY mode (NormalizeMode never returns ""), so
// ModeOf(ref) == "" uniquely means "source session no longer exists".
// The previous check only compared a non-empty mode against the active
// mode — a vanished source survived validation and the run path then
// re-persisted it forever (the pruning was claimed but never happened).
// The surviving normalized list is persisted by the callers (run path
// and PUT path both write it back), so the dead reference disappears
// durably after one validated turn.
func (s *Server) validateHistoryRefs(sessionID, activeMode string, refs []histref.Ref) []histref.Ref {
	normalized := histref.NormalizeRefs(refs)

	kept := make([]histref.Ref, 0, len(normalized))
	for _, ref := range normalized {
		if ref.SessionID == sessionID {
			continue
		}

		srcMode := s.store.ModeOf(ref.SessionID)
		if srcMode == "" {
			// v1.2.9: the source session does not exist (deleted, or a
			// foreign id the client invented). A reference to nothing is
			// noise that would otherwise be re-persisted forever — drop
			// it and say so.
			logging.Default().Info(
				"history",
				"pruned dead history reference %s from session %s (source session not found)",
				ref.SessionID, sessionID,
			)
			continue
		}

		if srcMode == activeMode {
			logging.Default().Info(
				"history",
				"dropped same-mode history reference %s from session %s (cross-mode only)",
				ref.SessionID, sessionID,
			)
			continue
		}

		kept = append(kept, ref)
	}

	return kept
}

// effectiveThinkingControl (v1.9.0) resolves the run's reasoning level at
// the wire boundary: the request's EXPLICIT level wins; an unset ("") or
// "auto" request adopts the AI System's effort preference. The result
// still passes through the v1.8.5 normalizer inside WithThinkingMode —
// unknown values degrade to the balanced default, never a fake budget.
func effectiveThinkingControl(request string, snap *aisystem.Snapshot) string {
	raw := strings.ToLower(strings.TrimSpace(request))
	if snap != nil && snap.Reasoning != "" && (raw == "" || raw == "auto") {
		return snap.Reasoning
	}
	return request
}
