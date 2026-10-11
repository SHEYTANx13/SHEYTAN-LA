package engine

// runtime.go — supervised lifecycle of the native engine host subprocess
// (v1.1.5 Phase 1).
//
// The Engine owns the shtn-engine-host process: it starts it, performs the
// protocol/ABI handshake, health-checks it, marks it ready, stops it,
// detects its death and restarts it — bounded, with backoff, exactly the
// policy the llama.cpp watchdog established (3 restarts per alive episode,
// 1s/2s/4s backoff, deliberate stops suppressed).
//
// STATE AUTHORITY: the native engine's state lives HERE, in the same
// vocabulary (llm.State*) and the same event shape (llm.EngineEvent) the
// llama.cpp engine and the API/WS layer already speak. There is no second,
// conflicting engine-state system: the API layer keeps reading one
// authoritative state per engine and one event stream per engine through
// the existing pipeline.
//
// There are no busy loops and no periodic polling: the exit watcher is
// event-driven (cmd.Wait), health is probed on demand (startup handshake
// and explicit Health calls), and restart backoff sleeps are bounded.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// HostBinaryName is the native engine host executable file name.
const HostBinaryName = "shtn-engine-host"

// maxAutoRestarts mirrors the llama.cpp watchdog policy: bounded recovery,
// then a visible terminal failure.
const maxAutoRestarts = 3

// Timeouts (all bounded; nothing waits forever).
const (
	handshakeTimeout = 15 * time.Second
	opTimeout        = 10 * time.Second
	shutdownTimeout  = 3 * time.Second
	stopGrace        = 4 * time.Second
)

// Engine supervises the native engine host subprocess.
type Engine struct {
	// binPath is the resolved host binary path (fixed at construction).
	binPath string

	mu        sync.Mutex
	state     string
	detail    string
	cmd       *exec.Cmd
	ipc       *ipcConn
	waitExit  chan struct{}
	startedAt time.Time
	restarts  int
	stopping  bool

	// Model concern (its own serialization via modelMu: model ops are
	// slower and must not block lifecycle switches).
	modelMu     sync.Mutex
	modelState  string
	modelDetail string
	modelInfo   *NativeModelInfo
	modelPlan   *NativeMemoryPlan

	// switchMu serializes Start/Stop against concurrent lifecycle calls
	// (the same shape LlamaServer uses).
	switchMu sync.Mutex

	logBuf *ringBuffer

	subsMu sync.Mutex
	subs   map[int]chan llm.EngineEvent
	subSeq int
}

// New creates a supervisor for the host binary at binPath.
func New(binPath string) *Engine {
	return &Engine{
		binPath:    binPath,
		state:      llm.StateIdle,
		modelState: ModelStateUnloaded,
		logBuf:     newRing(256),
		subs:       make(map[int]chan llm.EngineEvent),
	}
}

// DefaultHostPath resolves the default host binary location for a config:
// {DataDir}/bin/shtn-engine-host(.exe). An explicit cfg.NativeEnginePath
// override wins.
func DefaultHostPath(dataDir, override string) string {
	if override != "" {
		return override
	}

	name := HostBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	return filepath.Join(dataDir, "bin", name)
}

// Available reports whether the host binary exists (stat only — no spawn).
func (e *Engine) Available() bool {
	if e.binPath == "" {
		return false
	}

	_, err := os.Stat(e.binPath)
	return err == nil
}

// Path returns the supervised binary path.
func (e *Engine) Path() string { return e.binPath }

// State returns the current native engine state (llm.State* vocabulary).
func (e *Engine) State() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// Detail returns the latest human-readable state detail.
func (e *Engine) Detail() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.detail
}

// IsAlive reports whether the host subprocess is alive and answering.
func (e *Engine) IsAlive() bool {
	switch e.State() {
	case llm.StateReady, llm.StateRunning, llm.StateBusy:
		return true
	default:
		return false
	}
}

// Pid returns the host subprocess pid (0 when down).
func (e *Engine) Pid() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cmd != nil && e.cmd.Process != nil {
		return e.cmd.Process.Pid
	}
	return 0
}

// StartedAt returns the time of the last successful boot (zero before).
func (e *Engine) StartedAt() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.startedAt
}

// Restarts returns the auto-restart count of the current episode.
func (e *Engine) Restarts() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.restarts
}

// Logs returns the most recent host log lines.
func (e *Engine) Logs() []string { return e.logBuf.lines() }

// SubscribeEvents registers a channel receiving every native engine state
// transition (same contract as LlamaServer.SubscribeEvents: buffered,
// slow consumers drop, the returned func unsubscribes).
func (e *Engine) SubscribeEvents() (<-chan llm.EngineEvent, func()) {
	e.subsMu.Lock()
	defer e.subsMu.Unlock()

	ch := make(chan llm.EngineEvent, 32)
	e.subSeq++
	id := e.subSeq
	e.subs[id] = ch

	return ch, func() {
		e.subsMu.Lock()
		defer e.subsMu.Unlock()

		if existing, ok := e.subs[id]; ok {
			delete(e.subs, id)
			close(existing)
		}
	}
}

// Start boots the native engine host: spawn → handshake (protocol/ABI) →
// health check → ready. Idempotent while alive/starting; failures walk to
// StateFailed with a visible detail. The context bounds the whole boot —
// unlike the llama path (where a slow model load keeps progressing after
// the caller gives up), the native Phase 1 boot has no expensive model
// loading step, so a caller timeout deterministically tears the fresh
// host down instead of leaving a zombie subprocess.
//
// An explicit Start also resets the auto-restart budget: the user asking
// for the engine starts a fresh supervision episode.
func (e *Engine) Start(ctx context.Context) error {
	return e.start(ctx, true)
}

// start is the boot path. resetRestarts=false (watchdog auto-restart)
// keeps the episode budget so a persistently crashing engine gives up
// after maxAutoRestarts recoveries instead of looping forever — the
// bounded-resource invariant, applied to supervision itself.
func (e *Engine) start(ctx context.Context, resetRestarts bool) error {
	e.switchMu.Lock()
	defer e.switchMu.Unlock()

	e.mu.Lock()
	switch e.state {
	case llm.StateReady, llm.StateRunning, llm.StateBusy, llm.StateStarting:
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	if !e.Available() {
		e.setState(llm.StateFailed)
		e.setDetailLocked(fmt.Sprintf(
			"native engine host binary not found at %s — build native/engine (CMake) or set nativeEnginePath; llama.cpp remains the engine",
			e.binPath,
		))
		return fmt.Errorf("native engine host binary not found: %s", e.binPath)
	}

	e.setState(llm.StateStarting)

	ipc, cmd, waitExit, err := e.spawn()
	if err != nil {
		e.setState(llm.StateFailed)
		e.setDetailLocked(fmt.Sprintf("spawn native engine host: %v", err))
		return fmt.Errorf("spawn native engine host: %w", err)
	}

	e.mu.Lock()
	e.cmd = cmd
	e.ipc = ipc
	e.waitExit = waitExit
	e.mu.Unlock()

	// Handshake + health bounded by their own timeouts AND the caller ctx.
	handshakeCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	if err := e.handshake(handshakeCtx, ipc); err != nil {
		e.teardownFailed(cmd, waitExit, ipc, fmt.Errorf("handshake: %w", err))
		return err
	}

	if err := e.probeOnce(handshakeCtx, ipc); err != nil {
		e.teardownFailed(cmd, waitExit, ipc, fmt.Errorf("initial health check: %w", err))
		return err
	}

	e.mu.Lock()
	e.startedAt = time.Now()
	if resetRestarts {
		e.restarts = 0
	}
	e.mu.Unlock()

	// A fresh host process maps nothing: the model concern resets on
	// every lifecycle boundary (start, restart, stop, death).
	e.resetModelForHostCycle()

	// The exit watcher is the failure detector: event-driven, no polling.
	go e.watchExit(cmd, waitExit)

	e.setState(llm.StateReady)
	e.setDetailLocked("")

	logging.Default().Info(
		"native-engine",
		"native engine ready (host pid %d, %s)",
		cmd.Process.Pid,
		e.binPath,
	)

	return nil
}

// spawn launches the host subprocess with a sanitized environment (no
// secrets cross the engine boundary — fail-closed posture) and wired
// stdin/stdout pipes plus a line-buffered stderr ring.
func (e *Engine) spawn() (*ipcConn, *exec.Cmd, chan struct{}, error) {
	cmd := proc.Command(e.binPath)
	cmd.Env = proc.SanitizedEnvironment(nil)
	cmd.Dir = filepath.Dir(e.binPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, fmt.Errorf("start host: %w", err)
	}

	ipc := newIPCConn(stdin, stdout)

	// stderr → ring buffer (host diagnostics; bounded memory).
	go func() {
		LineReader(stderr, func(line string) {
			e.logf("[host] %s", line)
		})
	}()

	// waitExit closes when the process is reaped.
	waitExit := make(chan struct{})

	go func() {
		_ = cmd.Wait()
		close(waitExit)
	}()

	return ipc, cmd, waitExit, nil
}

// handshake performs the ping round-trip and enforces protocol + ABI
// compatibility (fail closed on mismatch).
func (e *Engine) handshake(ctx context.Context, ipc *ipcConn) error {
	resp, err := ipc.call(ctx, OpPing, nil)
	if err != nil {
		return err
	}

	var ping PingResult
	if err := DecodeResult(resp, &ping); err != nil {
		return err
	}

	if ping.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf(
			"native engine protocol mismatch: host speaks v%d, Go core speaks v%d — update the native engine build",
			ping.ProtocolVersion,
			ProtocolVersion,
		)
	}

	if ping.ABIVersion != ABIVersionExpected {
		return fmt.Errorf(
			"native engine ABI mismatch: host %d, expected %d — rebuild the native engine",
			ping.ABIVersion,
			ABIVersionExpected,
		)
	}

	e.logf(
		"handshake ok: engine=%s protocol=%d abi=%d",
		ping.Engine,
		ping.ProtocolVersion,
		ping.ABIVersion,
	)

	return nil
}

// probeOnce performs a single health round-trip.
func (e *Engine) probeOnce(ctx context.Context, ipc *ipcConn) error {
	resp, err := ipc.call(ctx, OpHealth, nil)
	if err != nil {
		return err
	}

	var health HealthResult
	if err := DecodeResult(resp, &health); err != nil {
		return err
	}

	if !health.Healthy {
		return fmt.Errorf("native engine reports unhealthy: %s", health.Detail)
	}

	return nil
}

// teardownFailed cleans up a failed startup.
func (e *Engine) teardownFailed(cmd *exec.Cmd, waitExit chan struct{}, ipc *ipcConn, cause error) {
	if ipc != nil {
		ipc.close(cause)
	}

	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	select {
	case <-waitExit:
	case <-time.After(3 * time.Second):
	}

	e.mu.Lock()
	if e.cmd == cmd {
		e.cmd = nil
		e.ipc = nil
		e.waitExit = nil
	}
	e.mu.Unlock()

	e.setState(llm.StateFailed)
	e.setDetailLocked(cause.Error())
}

// watchExit is the failure detector for a RUNNING engine: when the host
// dies while alive, walk to stopped and schedule the bounded auto-restart.
func (e *Engine) watchExit(cmd *exec.Cmd, waitExit chan struct{}) {
	<-waitExit

	e.mu.Lock()

	latest := e.cmd == cmd
	if latest {
		e.cmd = nil
		e.ipc = nil
		e.waitExit = nil
	}

	wasAlive := latest && isAliveState(e.state)

	e.mu.Unlock()

	if !wasAlive {
		return
	}

	// The host that just died may have had a model mapped; the
	// replacement process starts clean.
	e.resetModelForHostCycle()

	e.setState(llm.StateStopped)

	logging.Default().Error(
		"native-engine",
		"native engine host exited while running (pid %d)",
		cmd.Process.Pid,
	)

	e.scheduleAutoRestart()
}

// scheduleAutoRestart mirrors the llama.cpp watchdog: at most
// maxAutoRestarts attempts per alive episode, exponential backoff
// (1s/2s/4s), deliberate stops fully suppressed, terminal failure visible.
func (e *Engine) scheduleAutoRestart() {
	e.mu.Lock()

	if e.stopping {
		e.mu.Unlock()
		return
	}

	if e.restarts >= maxAutoRestarts {
		detail := fmt.Sprintf(
			"native engine exited %d times — giving up automatic recovery",
			e.restarts,
		)

		e.mu.Unlock()

		e.setState(llm.StateFailed)
		e.setDetailLocked(detail)

		logging.Default().Error(
			"native-engine",
			"auto-restart budget exhausted; native engine reported as failed",
		)

		return
	}

	e.restarts++
	attempt := e.restarts
	delay := time.Duration(1<<(attempt-1)) * time.Second

	e.mu.Unlock()

	logging.Default().Warn(
		"native-engine",
		"native engine died — automatic restart %d/%d in %v",
		attempt,
		maxAutoRestarts,
		delay,
	)

	go func() {
		time.Sleep(delay)

		e.mu.Lock()
		stopping := e.stopping
		e.mu.Unlock()

		if stopping {
			return
		}

		if err := e.start(context.Background(), false); err != nil {
			logging.Default().Error(
				"native-engine",
				"auto-restart %d/%d failed: %v",
				attempt,
				maxAutoRestarts,
				err,
			)
			return
		}

		logging.Default().Info(
			"native-engine",
			"native engine auto-restarted successfully",
		)
	}()
}

// Stop performs the deliberate shutdown: ask the host to stop (bounded),
// close stdin, wait a bounded grace, then kill. The stopping flag
// suppresses the exit watcher's auto-restart.
func (e *Engine) Stop(ctx context.Context) error {
	_ = ctx

	e.switchMu.Lock()
	defer e.switchMu.Unlock()

	e.mu.Lock()
	e.stopping = true
	cmd := e.cmd
	ipc := e.ipc
	e.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		e.mu.Lock()
		e.stopping = false
		e.mu.Unlock()

		e.setState(llm.StateStopped)
		return nil
	}

	e.setState(llm.StateStopping)

	// Graceful ask (bounded; failures are fine — the kill path follows).
	if ipc != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		_, _ = ipc.call(shutdownCtx, OpShutdown, nil)
		cancel()
		ipc.close(nil)
	}

	// Closing stdin signals EOF to the host's read loop.
	if stdin := ipcStdin(ipc); stdin != nil {
		_ = stdin.Close()
	}

	deadline := time.Now().Add(stopGrace)

	for time.Now().Before(deadline) {
		e.mu.Lock()
		alive := e.cmd == cmd
		e.mu.Unlock()

		if !alive {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	e.mu.Lock()
	alive := e.cmd == cmd
	if alive {
		_ = cmd.Process.Kill()
	}
	waitExit := e.waitExit
	if e.cmd == cmd {
		e.cmd = nil
		e.ipc = nil
		e.waitExit = nil
	}
	e.stopping = false
	e.mu.Unlock()

	// v1.4.0 (run 35996462352): after a forced kill, WAIT (bounded) for
	// the reap goroutine to collect the process before Stop returns. A
	// killed-but-unreaped child can still hold file handles on Windows —
	// callers (and test TempDir cleanup) deleting the model files
	// immediately after Stop previously raced the OS releasing those
	// handles and failed with "Access is denied".
	if alive && waitExit != nil {
		select {
		case <-waitExit:
		case <-time.After(3 * time.Second):
		}
	}

	e.resetModelForHostCycle()

	e.setState(llm.StateStopped)

	return nil
}

// Health performs an active health round-trip and returns the report.
func (e *Engine) Health(ctx context.Context) (llm.HealthReport, error) {
	report := llm.HealthReport{
		State:     e.State(),
		Alive:     e.IsAlive(),
		Detail:    e.Detail(),
		Timestamp: time.Now().UTC(),
	}

	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return report, fmt.Errorf("native engine is not running (state %s)", report.State)
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(probeCtx, OpHealth, nil)
	if err != nil {
		return report, fmt.Errorf("native engine health probe: %w", err)
	}

	var health HealthResult
	if err := DecodeResult(resp, &health); err != nil {
		return report, err
	}

	if !health.Healthy {
		return report, fmt.Errorf("native engine reports unhealthy: %s", health.Detail)
	}

	report.Alive = true
	return report, nil
}

// Hardware fetches the native hardware profile: the host's own detection
// (CPU/RAM/architecture) merged with the Go-side sysinfo probe (GPU/VRAM
// detection the C++ skeleton does not perform yet).
func (e *Engine) Hardware(ctx context.Context) (llm.HardwareInfo, error) {
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return llm.HardwareInfo{}, fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(probeCtx, OpHardware, nil)
	if err != nil {
		return llm.HardwareInfo{}, fmt.Errorf("native engine hardware probe: %w", err)
	}

	var hw HardwareResult
	if err := DecodeResult(resp, &hw); err != nil {
		return llm.HardwareInfo{}, err
	}

	return MergeHardware(hw), nil
}

// MetricsSnapshot is the native engine metrics reading (measured values
// only; see metrics.go).
func (e *Engine) MetricsSnapshot(ctx context.Context) (llm.Metrics, error) {
	e.mu.Lock()
	ipc := e.ipc
	startedAt := e.startedAt
	e.mu.Unlock()

	m := llm.Metrics{
		Backend:     "native",
		EngineState: e.State(),
		Pid:         e.Pid(),
		Restarts:    e.Restarts(),
	}

	if !startedAt.IsZero() {
		m.UptimeSeconds = time.Since(startedAt).Seconds()
	}

	if ipc == nil {
		// Measured lifecycle facts only; no process to ask.
		return m, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(probeCtx, OpMetrics, nil)
	if err != nil {
		return m, fmt.Errorf("native engine metrics probe: %w", err)
	}

	var result MetricsResult
	if err := DecodeResult(resp, &result); err != nil {
		return m, err
	}

	ApplyMetricsResult(&m, result)

	return m, nil
}

// Cancel requests cooperative cancellation of one in-flight generation
// request. The protocol round-trip is real; in Phase 1 the host answers
// "no active requests" because no generation exists yet.
func (e *Engine) Cancel(ctx context.Context, requestID string) error {
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	payload, err := json.Marshal(CancelPayload{RequestID: requestID})
	if err != nil {
		return err
	}

	resp, err := ipc.call(probeCtx, OpCancel, payload)
	if err != nil {
		return fmt.Errorf("native engine cancel: %w", err)
	}

	var result CancelResult
	if err := DecodeResult(resp, &result); err != nil {
		return err
	}

	if !result.Cancelled {
		return fmt.Errorf("native engine did not cancel: %s", result.Reason)
	}

	return nil
}

// --- internal state machinery ---

func isAliveState(state string) bool {
	switch state {
	case llm.StateReady, llm.StateRunning, llm.StateBusy:
		return true
	default:
		return false
	}
}

func (e *Engine) setState(state string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.setStateLocked(state)
}

func (e *Engine) setStateLocked(state string) {
	previous := e.state

	if previous == state {
		return
	}

	e.state = state

	ev := llm.EngineEvent{
		State:     state,
		Previous:  previous,
		Model:     "",
		Detail:    e.detail,
		Timestamp: time.Now(),
	}

	// Fan out outside e.mu (same discipline as LlamaServer).
	e.subsMu.Lock()
	subs := make([]chan llm.EngineEvent, 0, len(e.subs))
	for _, ch := range e.subs {
		subs = append(subs, ch)
	}
	e.subsMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (e *Engine) setDetailLocked(detail string) {
	e.mu.Lock()
	e.detail = detail
	e.mu.Unlock()
}

func (e *Engine) logf(format string, args ...any) {
	e.logBuf.add(fmt.Sprintf(format, args...))
}

// --- IPC connection ---

// ipcConn is the request/response multiplexer over the host's
// stdin/stdout: serialized writers, a single reader goroutine dispatching
// to pending callers by id, bounded per-op timeouts, plus STREAMING
// calls (Phase 5): a generate request receives intermediate event frames
// (same id, "event" member) on its event channel and exactly one final
// frame on its outcome channel.
type ipcConn struct {
	wmu    sync.Mutex // serializes frame writes
	stdin  io.WriteCloser
	stdout io.Reader

	pendingMu sync.Mutex
	pending   map[int64]*ipcPending

	nextID atomic.Int64

	closed   chan struct{}
	closeOne sync.Once
}

// ipcPending is one registered caller. events is non-nil ONLY for
// streaming calls; the final frame always arrives on ch.
//
// Lifecycle rules (the concurrency contract):
//   - events is NEVER closed (a concurrent in-flight dispatch send must
//     never panic); consumers finish through the abort signal and a
//     bounded drain; late stragglers fit in the 256-frame buffer.
//   - abort wakes the consumer; deliver (atomic) selects whether
//     buffered events are still delivered to onEvent (final path: yes —
//     the caller only returns after every buffered event is delivered;
//     abandon path: no — the caller is gone).
type ipcPending struct {
	ch     chan *ipcOutcome
	events chan *Response // streamed event frames (nil for plain calls)
	abort  chan struct{}
	once   sync.Once

	deliver atomic.Bool // consumer still forwards events to onEvent
}

// signalAbort wakes the consumer exactly once (idempotent).
func (p *ipcPending) signalAbort() {
	p.once.Do(func() { close(p.abort) })
}

type ipcOutcome struct {
	resp *Response
	err  error
}

func newIPCConn(stdin io.WriteCloser, stdout io.Reader) *ipcConn {
	c := &ipcConn{
		stdin:   stdin,
		stdout:  stdout,
		pending: make(map[int64]*ipcPending),
		closed:  make(chan struct{}),
	}

	go c.readLoop()

	return c
}

func (c *ipcConn) readLoop() {
	for {
		payload, err := ReadFrame(c.stdout)
		if err != nil {
			c.failAll(err)
			return
		}

		resp, derr := DecodeResponse(payload)
		if derr != nil {
			// Unparseable frame: fail the caller(s) conservatively rather
			// than desynchronize the stream.
			c.failAll(derr)
			return
		}

		c.dispatch(resp)
	}
}

func (c *ipcConn) dispatch(resp *Response) {
	c.pendingMu.Lock()
	p, ok := c.pending[resp.ID]
	if ok && resp.IsFinal() {
		delete(c.pending, resp.ID)
	}
	c.pendingMu.Unlock()

	if !ok {
		// Unknown id (timed-out caller) — drop.
		return
	}

	if !resp.IsFinal() && p.events != nil {
		// Streaming event frame: delivered to the consumer's event
		// channel. While the consumer still wants events (deliver), a
		// full channel blocks the read loop — intentional backpressure,
		// the consumer must drain. Once the consumer has ABANDONED the
		// stream (deliver=false: caller gone or callback failed), the
		// frame is dropped when the buffer is full instead of blocking:
		// a straggler burst larger than the 256-frame buffer can no
		// longer wedge the read loop ahead of the cancelled final frame
		// (v1.3.3 — the "no wedged engine state" invariant).
		if p.deliver.Load() {
			p.events <- resp
		} else {
			select {
			case p.events <- resp:
			default:
			}
		}
		return
	}

	p.ch <- &ipcOutcome{resp: resp}
}

func (c *ipcConn) failAll(err error) {
	c.closeOnce(nil)

	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]*ipcPending)
	c.pendingMu.Unlock()

	for _, p := range pending {
		// Connection lost: stop delivering (the caller gets an error),
		// wake the consumer.
		p.deliver.Store(false)
		p.signalAbort()
		select {
		case p.ch <- &ipcOutcome{err: fmt.Errorf("native engine connection lost: %w", err)}:
		default:
		}
	}
}

func (c *ipcConn) closeOnce(err error) {
	c.closeOne.Do(func() {
		close(c.closed)
		_ = c.stdin.Close()
	})
}

func (c *ipcConn) close(err error) {
	c.failAll(err)
}

// call performs one bounded round-trip.
func (c *ipcConn) call(ctx context.Context, op string, payload json.RawMessage) (*Response, error) {
	select {
	case <-c.closed:
		return nil, fmt.Errorf("native engine connection is closed")
	default:
	}

	id := c.nextID.Add(1)

	p := &ipcPending{
		ch:    make(chan *ipcOutcome, 1),
		abort: make(chan struct{}),
	}

	c.pendingMu.Lock()
	c.pending[id] = p
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	req := &Request{ID: id, Op: op, Payload: payload}

	c.wmu.Lock()
	err := EncodeRequest(c.stdin, req)
	c.wmu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("send %s: %w", op, err)
	}

	select {
	case out := <-p.ch:
		if out.err != nil {
			return nil, out.err
		}
		if !out.resp.OK {
			return nil, fmt.Errorf("%s failed: %s", op, out.resp.Error)
		}
		return out.resp, nil

	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", op, ctx.Err())

	case <-c.closed:
		return nil, fmt.Errorf("%s: connection lost", op)
	}
}

// streamCall performs one STREAMING call (the generate op): event frames
// are delivered to onEvent until the final frame arrives, which becomes
// the return value. There is NO per-op timeout by design (generation is
// bounded by max_tokens plus the caller's context and the stall
// watchdog); a context abort or a consumer error asks the host to cancel
// the request cooperatively and switches the consumer to drain-only mode
// (bounded; the engine finishes the request with a "cancelled" final
// frame that the abandon path waits for).
func (c *ipcConn) streamCall(ctx context.Context, op string,
	payload json.RawMessage,
	onEvent func(*Response) error) (*Response, error) {

	select {
	case <-c.closed:
		return nil, fmt.Errorf("native engine connection is closed")
	default:
	}

	id := c.nextID.Add(1)

	p := &ipcPending{
		ch:     make(chan *ipcOutcome, 1),
		events: make(chan *Response, 256),
		abort:  make(chan struct{}),
	}
	p.deliver.Store(true)

	c.pendingMu.Lock()
	c.pending[id] = p
	c.pendingMu.Unlock()

	unregister := func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}

	req := &Request{ID: id, Op: op, Payload: payload}

	c.wmu.Lock()
	err := EncodeRequest(c.stdin, req)
	c.wmu.Unlock()

	if err != nil {
		unregister()
		return nil, fmt.Errorf("send %s: %w", op, err)
	}

	consumerErr := make(chan error, 1)
	consumerDone := make(chan struct{})

	// Event consumer: forwards streamed frames to onEvent until aborted.
	// On abort it drains what remains (delivering ONLY while the caller
	// is still interested) and exits — the read loop can never wedge.
	go func() {
		defer close(consumerDone)
		for {
			select {
			case ev := <-p.events:
				if !p.deliver.Load() || onEvent == nil {
					continue
				}
				if err := onEvent(ev); err != nil {
					consumerErr <- err
					// Stop delivering; keep draining so the read loop
					// never blocks on a full buffer.
					p.deliver.Store(false)
				}

			case <-p.abort:
				// Drain the remainder (bounded: buffered events + at
				// most one in-flight dispatch send; the 256-frame
				// buffer absorbs any straggler). Deliver while the
				// caller still wants events.
				for {
					select {
					case ev := <-p.events:
						if p.deliver.Load() && onEvent != nil {
							if err := onEvent(ev); err != nil {
								select {
								case consumerErr <- err:
								default:
								}
								p.deliver.Store(false)
							}
						}
					default:
						return
					}
				}
			}
		}
	}()

	// waitForConsumer returns once every buffered event has been handed
	// to onEvent (bounded — a stuck consumer callback is the caller's
	// own code; 5 s is generous).
	waitForConsumer := func() {
		select {
		case <-consumerDone:
		case <-time.After(5 * time.Second):
		}
	}

	// abandon: the caller gave up — stop delivering, cancel the request
	// on the host (cooperative; the engine emits a cancelled final frame)
	// and wait for that final frame so the host-side lane finishes.
	// v1.7.5: the CAUSE error is threaded (not just its text) so sentinel
	// classification — the pause path's errors.Is(err, context.Canceled) —
	// survives the unwind.
	abandon := func(cause error) (*Response, error) {
		p.deliver.Store(false)
		unregister()
		p.signalAbort()
		waitForConsumer()

		if op == OpGenerate {
			if m, merr := json.Marshal(struct {
				RequestID string `json:"requestId"`
			}{RequestID: requestIDOf(payload)}); merr == nil {
				cancelCtx, cancel := context.WithTimeout(context.Background(), opTimeout)
				_, _ = c.call(cancelCtx, OpCancel, m)
				cancel()
			}
		}

		// The host lane writes the final frame after cancellation; it is
		// dropped here (unregistered) — the engine is free to serve the
		// next request immediately.
		return nil, fmt.Errorf("%s: %w", op, cause)
	}

	for {
		select {
		case out := <-p.ch:
			unregister()
			p.signalAbort()

			// Deliver every buffered event before returning (the final
			// frame ends the stream; the consumer drains + delivers).
			p.deliver.Store(true)
			waitForConsumer()

			if out.err != nil {
				return nil, out.err
			}
			if !out.resp.OK {
				return nil, fmt.Errorf("%s failed: %s", op, out.resp.Error)
			}
			return out.resp, nil

		case err := <-consumerErr:
			_ = err
			return abandon(errors.New("consumer aborted the stream"))

		case <-ctx.Done():
			return abandon(ctx.Err())

		case <-c.closed:
			p.deliver.Store(false)
			unregister()
			p.signalAbort()
			waitForConsumer()
			return nil, fmt.Errorf("%s: connection lost", op)
		}
	}
}

// requestIDOf extracts the requestId from a generate payload (for the
// cancel round-trip). Returns "" when absent.
func requestIDOf(payload json.RawMessage) string {
	var probe struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return ""
	}
	return probe.RequestID
}

// ipcStdin exposes the stdin writer for EOF-signaling on stop.
func ipcStdin(c *ipcConn) io.WriteCloser {
	if c == nil {
		return nil
	}
	return c.stdin
}

// --- small utilities ---

// ringBuffer is a fixed-size line ring (bounded memory for host logs).
type ringBuffer struct {
	mu   sync.Mutex
	buf  []string
	head int
	size int
}

func newRing(n int) *ringBuffer {
	return &ringBuffer{buf: make([]string, n), size: n}
}

func (r *ringBuffer) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buf[r.head] = s
	r.head = (r.head + 1) % r.size
}

func (r *ringBuffer) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, r.size)

	for i := 0; i < r.size; i++ {
		idx := (r.head + i) % r.size
		if r.buf[idx] != "" {
			out = append(out, r.buf[idx])
		}
	}

	return out
}

// LineReader reads newline-delimited text and invokes fn per line. Bounded
// line length (8 KiB) so a runaway host cannot grow memory.
func LineReader(r io.Reader, fn func(line string)) {
	if r == nil {
		return
	}

	const maxLine = 8 << 10

	buf := make([]byte, 0, 512)
	chunk := make([]byte, 1024)

	for {
		n, err := r.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)

			for {
				idx := -1
				for i, b := range buf {
					if b == '\n' {
						idx = i
						break
					}
				}

				if idx < 0 {
					break
				}

				line := string(buf[:idx])
				buf = buf[idx+1:]

				if len(line) > maxLine {
					line = line[:maxLine] + "…"
				}

				fn(line)
			}

			if len(buf) > maxLine {
				buf = buf[len(buf)-maxLine:]
			}
		}

		if err != nil {
			if len(buf) > 0 {
				fn(string(buf))
			}
			return
		}
	}
}
