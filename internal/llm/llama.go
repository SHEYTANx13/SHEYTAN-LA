package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/engdiscovery"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/englease"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// Engine lifecycle states (v1.1.3 spec): the backend process state is
// authoritative and the UI may never invent any of these.
//
//	idle        fresh manager, nothing attempted yet
//	downloading engine binary is being fetched
//	starting    subprocess spawned, model loading (health pending)
//	ready       healthy: /health 200, model loaded, no inference in flight
//	running     subprocess alive and serving (superset of ready/busy)
//	busy        inference request in flight
//	stopping    deliberate shutdown in progress
//	stopped     terminated deliberately (or died without auto-restart armed)
//	failed      terminal failure after bounded retries
const (
	StateIdle        = "idle"
	StateDownloading = "downloading"
	StateStarting    = "starting"
	StateReady       = "ready"
	StateRunning     = "running"
	StateBusy        = "busy"
	StateStopping    = "stopping"
	StateStopped     = "stopped"
	StateFailed      = "failed"
)

// aliveStates are the states in which the subprocess is alive and the
// HTTP endpoint is expected to answer.
var aliveStates = map[string]bool{
	StateRunning: true,
	StateReady:   true,
	StateBusy:    true,
}

// EngineEvent is one state transition published to subscribers (API/WS).
type EngineEvent struct {
	State     string    `json:"state"`
	Previous  string    `json:"previous"`
	Model     string    `json:"model,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	// Download carries live asset-download progress (phase, bytes,
	// speed, ETA, source, verification) while the state is
	// "downloading" — the v1.2.3 Download Manager surfaces REAL
	// measured progress instead of a bare state label. Nil outside
	// downloads.
	Download *downloader.Progress `json:"download,omitempty"`
}

// LlamaServer manages a llama.cpp server subprocess. This is the standalone
// inference engine that replaces LM Studio's local server.
type LlamaServer struct {
	// src is the live configuration source. Engine starts capture one
	// consistent snapshot; health probes read the current value.
	// (v1.1.4: replaced the shared mutable *Config which raced the HTTP
	// config patcher.)
	src     *config.Source
	cmd     *exec.Cmd
	mu      sync.Mutex
	state   string
	detail  string
	logBuf  *ringBuffer
	errRing *ringBuffer

	loaded   string // absolute path of the model currently loaded by the subprocess
	switchMu sync.Mutex

	// mmproj is the multimodal projector the engine was launched with.
	// Empty when the engine runs text-only.
	mmproj string

	// visionState / visionReason carry the v1.2.0 vision readiness state
	// machine for the RUNNING (or last) boot: loading → ready when the
	// projector survives a verified boot, degraded after the bounded
	// text-only retry, failed when an explicit override could not resolve
	// or the boot failed outright. Between boots it holds the evidence-
	// backed capability evaluation for the configured model. Guarded by mu.
	visionState  vision.State
	visionReason string

	// engineUpdateTried prevents repeated engine self-updates during one
	// application run.
	engineUpdateTried bool

	// restarts counts auto-restart attempts for the CURRENT alive episode;
	// reset on each successful Start. Bounded by maxAutoRestarts.
	restarts int

	// gen is the lifecycle episode counter (v1.2.5). Incremented on every
	// successful boot; a watchdog armed for episode N may only act while
	// gen == N. This is what makes a stale restart from an OLDER engine
	// episode unable to resurrect an engine after a newer lifecycle has
	// started or stopped.
	gen uint64

	// watchdog ownership (v1.2.5). Exactly one pending restart can be
	// armed at a time; Stop() cancels it and waits for the goroutine to
	// exit, so after Stop() completes no delayed watchdog can call Start().
	// Guarded by mu. watchStop is closed to cancel; watchDone is closed by
	// the watchdog goroutine on exit.
	watchArmed bool
	watchStop  chan struct{}
	watchDone  chan struct{}

	// startedAt is the time of the last successful boot (v1.1.5: the
	// measured uptime base for backend Metrics).
	startedAt time.Time

	// stopping arms the deliberate-shutdown flag so the exit watcher's
	// auto-restart is suppressed while Stop() runs.
	stopping bool

	// exitDone is the CURRENT process's watcher-done channel: closed by
	// the watcher goroutine after cmd.Wait() has returned. Stop() waits
	// on it (bounded) so a killed child is fully reaped — and on Windows
	// its executable unlocked — before Stop reports completion.
	// Guarded by mu. Nil when no process is owned.
	exitDone chan struct{}

	// caps is the engine capability profile in force for the CURRENT or
	// last boot (Phase 7). Guarded by mu. Resolved per boot from the
	// persisted verified profile / --help detection, then persisted again
	// after a verified successful startup.
	caps *EngineCaps

	// verifiedModel / verifiedCtx record what the startup state machine
	// actually verified after /health went 200 (Phase 7): the served model
	// list and the engine-reported context capability. Empty/0 when the
	// verification probe was unavailable — the UI must never present a
	// stronger claim than what was verified.
	verifiedModel string
	verifiedCtx   int

	// offloadEvidence (v1.2.6) is the MEASURED runtime offload line the
	// engine printed during the CURRENT boot (e.g. "offloaded
	// 33/33 layers to GPU"). Empty = no offload observed yet — an honest
	// unknown, never a guess. Guarded by mu.
	//
	// v1.8.6 PER-BOOT TRUTH: the line is RESET at every launch
	// (launchArgs) — evidence belongs to the process boot that
	// produced it, so a restart or model swap can never inherit the
	// previous boot's GPU execution claim. Each boot re-proves.
	offloadEvidence string

	// gpuCandidateProving (v1.8.6) is true ONLY while a bounded GPU
	// candidate transaction is PROVING the Vulkan variant inside
	// updateEngineVariantTx (extraVerify != nil): the candidate launch
	// may enable GPU offload from SELECTION evidence (enumeration) —
	// the transaction's verification hook then REQUIRES the measured
	// offload line, so the proof is still enforced downstream. A
	// normal serving launch never sets this: serving AUTO stays
	// CPU-safe until execution is proven. Guarded by mu.
	gpuCandidateProving bool

	// runGate (v1.9.4) is the run-coordination seam installed by the
	// API layer (anyRunActive): it reports whether an agent/chat run
	// is live RIGHT NOW. updateEngineVariantTx consults it at the
	// DESTRUCTIVE boundary — before stopping a RUNNING engine — so an
	// engine-changing transaction can never tear the serving engine
	// out from under an active generation. The caller-side eligibility
	// check evaluates the same condition once, BEFORE a transaction
	// that can legitimately run for many minutes (download → swap →
	// verify → rollback); without this re-check the gate is
	// check-then-act across that whole window. Guarded by mu.
	runGate func() bool

	// admission (v1.9.5) is the run-ADMISSION half of the same
	// coordination: while an engine-mutation claim is held, new run
	// admission is refused by the API layer, closing the TOCTOU
	// window between the destructive-boundary check and the stop.
	// Guarded by mu.
	admission EngineAdmission

	// mutationClaimed (v1.9.5) mirrors an held admission claim for
	// the advisory EngineMutationActive signal. Guarded by mu.
	mutationClaimed bool

	// subs receive every state transition (never blocked; slow subscribers
	// drop events). Registered via SubscribeEvents.
	subsMu sync.Mutex
	subs   map[int]chan EngineEvent
	subSeq int

	// dlJob / dlProgress expose the v1.2.3 Download Manager job while
	// an engine asset (llama.cpp archive, future model packages) is
	// being fetched: measured progress for the UI and a cancellation
	// handle. dlProgress is nil outside downloads.
	dlJob      atomic.Pointer[downloader.Job]
	dlProgress atomic.Pointer[downloader.Progress]

	// v1.3.6 (spec §4/§7): first-class failure diagnostics. lastPreflight
	// records the last preflight evidence; lastFailure the last decoded
	// startup failure (exit code, decoded Windows loader class, tails).
	// Both are guarded by mu and surfaced through EngineDiagnostics and
	// /api/engine — the UI must render the REAL failure, never a spinner.
	lastPreflight *EnginePreflight
	lastFailure   *EngineFailureReport

	// updateCancel (v1.3.6) cancels an in-flight provisioning operation
	// driven through the unified installer (CancelDownload). Guarded by mu.
	updateCancel context.CancelFunc

	// testStagedArchive (test seam, v1.3.6): when non-empty,
	// UpdateEngineNow runs the transaction from this archive instead
	// of downloading — deterministic CI without a network.
	testStagedArchive string

	// pendingEngineUpdate holds the DEFERRED-COMMIT staged install of the
	// auto-update-for-model path (v1.3.7). The compat ladder's next pass is
	// the startup verification: it COMMITS when the engine reaches ready and
	// a function-exit deferred rollback restores the last-known-good package
	// when it does not. Touched only under switchMu (startLocked paths).
	pendingEngineUpdate *updater.StagedInstall
}

// maxAutoRestarts bounds the watchdog's automatic recovery attempts per
// alive episode. Retries are bounded by design: after this many backoff
// restarts the engine reports failed and waits for explicit user action.
const maxAutoRestarts = 3

type ringBuffer struct {
	mu   sync.Mutex
	buf  []string
	head int
	size int
}

func newRing(n int) *ringBuffer {
	return &ringBuffer{
		buf:  make([]string, n),
		size: n,
	}
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

func (r *ringBuffer) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buf = make([]string, r.size)
	r.head = 0
}

// NewLlamaServer returns a manager bound to the live config source.
func NewLlamaServer(src *config.Source) *LlamaServer {
	return &LlamaServer{
		src:     src,
		state:   StateIdle,
		logBuf:  newRing(500),
		errRing: newRing(64),
		subs:    make(map[int]chan EngineEvent),
	}
}

// ErrEngineBusyWithRun is the v1.9.4 coordination refusal: an
// engine-changing transaction reached its destructive boundary (stopping
// a RUNNING engine) while a run is active. The transaction fails CLOSED
// — the serving engine is never torn down under an active generation.
var ErrEngineBusyWithRun = errors.New(
	"a run is active — engine variant transaction deferred to protect the live generation")

// SetRunGate installs the run-coordination seam (the API layer's
// anyRunActive). The gate is consulted by updateEngineVariantTx at the
// destructive boundary; nil (tests without an API layer) preserves the
// historical behavior.
func (s *LlamaServer) SetRunGate(gate func() bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runGate = gate
}

// EngineAdmission (v1.9.5, P1) is the admission-control half of the
// engine/run coordination. It closes the check-then-act window between
// the transaction's destructive-boundary gate check and the Stop() that
// follows it: while a transaction holds the mutation claim, run
// admission is refused deterministically, so a run accepted before the
// claim is visible to the claim's atomic `live` verdict, and a run
// admitted after the claim never exists. Implementations must make
// BeginEngineMutation and run registration linearizable (the API server
// takes both under the same runsMu critical section).
//
// The returned `live` verdict is the authoritative "a run is already
// active" answer for the destructive boundary: when true the caller
// must NOT take the claim and must refuse the transaction.
type EngineAdmission interface {
	// BeginEngineMutation atomically (with respect to run admission)
	// records the engine-mutation claim and reports whether a run is
	// already live. When live is true no claim was taken.
	BeginEngineMutation() (live bool)
	// EndEngineMutation releases a claim taken by a successful
	// BeginEngineMutation. Never called when live was true.
	EndEngineMutation()
}

// SetAdmissionControl installs the admission-control seam. nil (tests
// without an API layer) preserves the historical behavior: the
// destructive-boundary run gate alone.
func (s *LlamaServer) SetAdmissionControl(a EngineAdmission) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.admission = a
}

// EngineMutationActive reports whether an engine-mutation claim is
// currently held — the fast advisory signal admission pre-checks use
// for a deterministic early refusal. The airtight coordination remains
// the BeginEngineMutation critical section itself.
func (s *LlamaServer) EngineMutationActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.mutationClaimed
}

// runActiveNow reports the installed gate's verdict under mu. A nil gate
// (never wired) reports false — the transaction then behaves exactly as
// before v1.9.4.
func (s *LlamaServer) runActiveNow() bool {
	s.mu.Lock()
	gate := s.runGate
	s.mu.Unlock()

	if gate == nil {
		return false
	}

	return gate()
}

// InvalidSamplingConfigError is the v1.6.1 P0 classification: the launch
// was refused because a sampling value in the configuration is
// parser-invalid (the in-the-wild "--repeat-penalty 0" rejection).
//
// It is DETERMINISTIC: no compatibility level, option repair, engine
// update or accelerator fallback can ever make the value work, so the
// compatibility ladder and the retry loop are never entered for it. The
// message names the field, the violated rule and the fix so the user can
// act immediately.
type InvalidSamplingConfigError struct {
	Problems []config.SamplingProblem
}

func (e *InvalidSamplingConfigError) Error() string {
	out := "engine start refused — invalid sampling configuration (fix these values, then start again):"

	for _, p := range e.Problems {
		out += "\n  " + p.String()
	}

	return out
}

// validateSamplingForLaunch is the authoritative pre-launch gate: it runs
// BEFORE any engine process exists — before the ownership lease decision,
// before the engine download/provision path, before the capability probe
// and long before the compatibility ladder. See internal/config/sampling.go
// for the defect narrative and the exact rule set.
//
// v1.6.2 audit closure: the raw user extra arguments (llamaExtraArgs) are
// linted here too — a malformed numeric argument in the extra string is
// as deterministic as a bad repeatPenalty and must fail the same way
// (zero spawns, no ladder, no accelerator downgrade for a config typo).
func validateSamplingForLaunch(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}

	problems := config.ValidateSamplingOptions(cfg.LLM)
	problems = append(problems, config.ValidateExtraArgs(cfg.LlamaExtraArgs)...)

	if len(problems) > 0 {
		return &InvalidSamplingConfigError{Problems: problems}
	}

	return nil
}

// SubscribeEvents registers a channel receiving every engine state
// transition. The returned func unsubscribes. Channels are buffered;
// slow consumers drop transitions rather than block the engine.
func (s *LlamaServer) SubscribeEvents() (<-chan EngineEvent, func()) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	ch := make(chan EngineEvent, 32)
	s.subSeq++
	id := s.subSeq
	s.subs[id] = ch

	return ch, func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()

		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(existing)
		}
	}
}

// Detail returns the latest human-readable engine detail (failure reason,
// recovery note). Empty when nothing noteworthy happened.
func (s *LlamaServer) Detail() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.detail
}

// SetDetail records a human-readable state detail.
func (s *LlamaServer) SetDetail(detail string) {
	s.mu.Lock()
	s.detail = detail
	s.mu.Unlock()
}

// IsAlive reports whether the subprocess is alive and serving.
func (s *LlamaServer) IsAlive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return aliveStates[s.state]
}

// MarkBusy flips the healthy engine between ready and busy around an
// inference request. It is a no-op unless the engine is alive — remote
// providers and adopted engines never report busy through this path.
//
// v1.1.4: the whole transition runs under one mutex acquisition. The
// previous version checked aliveness, released the lock, then re-locked in
// setState — a process death inside that window could be overwritten by a
// stale busy/ready transition.
func (s *LlamaServer) MarkBusy(busy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !aliveStates[s.state] || s.stopping {
		return
	}

	switch {
	case busy && s.state != StateBusy:
		s.setStateLocked(StateBusy)
	case !busy && s.state == StateBusy:
		s.setStateLocked(StateReady)
	}
}

// State returns the current server state.
func (s *LlamaServer) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.state
}

// Logs returns the most recent log lines from the subprocess.
func (s *LlamaServer) Logs() []string {
	return s.logBuf.lines()
}

// IsRunning returns true if the subprocess is alive and listening.
// Both the coarse "running" and the fine-grained "ready"/"busy" states
// count: callers only care whether the endpoint is expected to answer.
func (s *LlamaServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return aliveStates[s.state]
}

// ensureBinary makes sure the llama.cpp server binary exists and returns
// its effective path. An empty configured LlamaBinPath is resolved to the
// default location and persisted through the config source (v1.1.4: the
// old code mutated the shared Config in place).
//
// v1.3.6 (spec §14): this is NO LONGER a competing download
// implementation. Provisioning is tiered through the ONE authority:
//
//  1. a valid managed binary already in place → use it (no re-download);
//  2. a validated discovered candidate (System Engine Discovery,
//     Tier 0/1) → import it into the managed directory;
//  3. otherwise → download through updater.InstallStaged — the same
//     transactional installer the scheduled updater uses.
func (s *LlamaServer) ensureBinary(cfg *config.Config) (string, error) {
	binPath := expectedEngineBinPath(cfg)

	if cfg.LlamaBinPath != binPath {
		next := s.src.Update(func(c *config.Config) {
			c.LlamaBinPath = binPath
		})

		if err := config.Save(next.ConfigPath(), next); err != nil {
			return "", fmt.Errorf("persist default engine path: %w", err)
		}
	}

	if _, err := os.Stat(binPath); err == nil {
		// v1.7.4 (P0 #3, H2): the blind re-stamp recorded the BUNDLED
		// default tag whenever the state file carried none — even when
		// the binary beside this very manifest was a NEWER committed
		// build — poisoning the identity every "update required"
		// decision reads. The install manifest next to the binary is
		// the honest identity; the bundled default is the last resort
		// for a binary with no manifest at all.
		if updater.InstalledEngineTag(cfg) == "" {
			tag := updater.ManifestEngineTag(cfg)

			if tag == "" {
				tag = updater.DefaultEngineTag

				logging.Default().Warn("engine",
					"no engine tag recorded and no install manifest beside %s — recording the bundled default tag %s",
					binPath, tag)
			} else {
				logging.Default().Info("engine",
					"no engine tag recorded — recording tag %s from the install manifest beside the engine",
					tag)
			}

			updater.RecordEngineTag(cfg, tag)
		}

		return binPath, nil
	}

	// 2) System Engine Discovery (Tier 0/1 quick pass) — BEFORE the
	// offline gate and BEFORE any download (v1.3.7 ordering fix).
	//
	// The v1.3.6 order put netcheck.IsOffline() first, which REJECTED a
	// usable local engine merely because the network was unavailable —
	// the exact case the discovery tier exists for ("reuse an
	// already-installed compatible engine instead of downloading").
	// Discovery and import are entirely local (PATH / exe dir / known
	// dirs / the persisted discovery cache + a validated copy); they
	// need zero network. The effective order is now the documented one:
	//
	//      valid managed engine
	//      → valid locally discovered/importable engine
	//      → network download
	//      → genuine failure
	if imported := s.tryImportDiscoveredEngine(cfg); imported {
		if _, err := os.Stat(binPath); err == nil {
			return binPath, nil
		}
	}

	if netcheck.IsOffline() {
		return "", fmt.Errorf(
			"no llama.cpp server binary is installed and no usable local engine was discovered; "+
				"the network appears to be OFFLINE, so the engine cannot be downloaded automatically. "+
				"Reconnect once so the server can be fetched, or place a prebuilt llama-server(.exe) into %s and retry",
			filepath.Dir(binPath),
		)
	}

	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		return "", err
	}

	s.setState(StateDownloading)

	// 3) Download through the single transactional installer.
	ctx, cancel := context.WithTimeout(context.Background(), engineDownloadTimeout)
	defer cancel()

	s.mu.Lock()
	s.updateCancel = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.updateCancel = nil
		s.mu.Unlock()
	}()

	url, tag, err := updater.ResolveDownloadURL(ctx)
	if err != nil {
		return "", err
	}

	s.logf("Downloading llama.cpp engine %s from %s through the unified installer", tag, url)

	if _, err := updater.InstallStaged(ctx, cfg, tag, s.publishDownloadProgress); err != nil {
		if errors.Is(err, downloader.ErrCancelled) || errors.Is(ctx.Err(), context.Canceled) {
			return "", fmt.Errorf("engine download cancelled")
		}

		return "", fmt.Errorf("provision llama.cpp engine: %w", err)
	}

	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf(
			"llama.cpp binary not found at %s after provisioning",
			binPath,
		)
	}

	return binPath, nil
}

// tryImportDiscoveredEngine runs the quick (Tier 0/1) discovery pass and
// imports a validated candidate when one exists. Returns whether an
// import produced a usable managed binary. Full-system discovery
// (Tier 2) never runs on the boot path — it is background-only (spec
// §11: no startup full-disk scan).
func (s *LlamaServer) tryImportDiscoveredEngine(cfg *config.Config) bool {
	cand, err := engdiscovery.QuickFind(cfg, llamaBinaryName())
	if err != nil || cand == nil {
		return false
	}

	s.logf("discovered existing engine candidate: %s — importing into the managed directory", cand.Path)

	result, ierr := updater.ImportCandidate(cfg, filepath.Dir(cand.Path), filepath.Base(cand.Path))
	if ierr != nil {
		s.logf("discovered candidate rejected: %v", ierr)

		return false
	}

	if cand.Tag != "" {
		updater.RecordEngineTag(cfg, cand.Tag)
	}

	logging.Default().Info("engine", "imported discovered engine: %s", result.Outcome)

	return true
}

const engineDownloadTimeout = 10 * time.Minute

const modelLoadTimeout = 180 * time.Second
const engineCompatMax = 3

func compatLevelName(level int) string {
	switch level {
	case 1:
		return "template compat (--jinja)"
	case 2:
		return "no speed flags"
	case 3:
		return "safe mode (CPU)"
	default:
		return "full speed"
	}
}

// Start boots the llama.cpp server as a subprocess if not already running.
// Serialized on switchMu so a prewarm and a first-send can never race.
func (s *LlamaServer) Start() error {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()

	return s.startLocked()
}

// startLocked is the real boot path; the caller holds switchMu.
//
// The compatibility ladder retries progressively safer launch profiles.
// When the engine reports an unsupported model architecture, the bundled
// llama.cpp engine may be updated once and the ladder retried.
//
// adoptExisting must never be called while s.mu is held. The previous
// implementation could deadlock by attempting to lock s.mu again after
// adopting an existing engine.
func (s *LlamaServer) startLocked() error {
	s.mu.Lock()

	if aliveStates[s.state] || s.state == StateStarting {
		s.mu.Unlock()
		return nil
	}

	s.mu.Unlock()

	// v1.1.4: one consistent snapshot for the whole boot. A concurrent
	// Settings PATCH can no longer produce a half-old, half-new launch.
	cfg := s.src.Load()

	// v1.6.1 (P0): the DETERMINISTIC CONFIGURATION GATE — the first
	// thing the boot does. A parser-invalid sampling value (the
	// in-the-wild repeatPenalty=0 that produced "error while handling
	// argument \"--repeat-penalty\": repeat-penalty must be finite and
	// greater than 0") is refused HERE: before the ownership lease,
	// before the engine download path, before the capability probe and
	// long before the compatibility ladder — none of which can ever
	// make an invalid value work. This is a configuration failure,
	// NEVER an accelerator/compatibility incompatibility: no ladder
	// descent, no option-repair loop, no compat-level persistence, no
	// retry storm. One classified, actionable error.
	if serr := validateSamplingForLaunch(cfg); serr != nil {
		s.mu.Lock()
		s.detail = serr.Error()
		s.mu.Unlock()

		s.setState(StateFailed)

		logging.Default().Error("engine", "%v", serr)

		return serr
	}

	// v1.11.0 evidence freshness: a boot starts a NEW evidence world —
	// the cached device enumeration (a per-boot fact by contract) is
	// invalidated so every boot probes the real current hardware state
	// instead of replaying a memo from before a restart. Binary updates
	// were already covered by the path+size+mtime cache key.
	if bin := engineBinPath(cfg); bin != "" {
		InvalidateDeviceCache(bin)
	}

	// v1.3.6 (spec §16): claim the cross-process ownership lease BEFORE
	// any boot/adoption decision. One installation has exactly ONE
	// managing owner: a second app instance (live, verified through
	// pid+exe+start-time) is refused — never silently adopted, never
	// killed. The lease is re-entrant for this process; a stale file is
	// recovered only after the recorded holder fails verification.
	lease, leaseErr := englease.Acquire(engineLeaseDir(cfg), englease.OwnerLease, "engine-owner")
	if leaseErr != nil {
		s.setState(StateFailed)

		return fmt.Errorf("engine start refused: %w", leaseErr)
	}

	defer lease.Release()

	portInUse := PortInUse(
		cfg.LlamaHost,
		cfg.LlamaPort,
	)

	if portInUse {
		if s.adoptExisting(cfg) {
			s.setState(StateReady)
			return nil
		}

		s.setState(StateFailed)

		return fmt.Errorf(
			"port %d is already in use by another program — "+
				"change LlamaPort in config.json or close the other app",
			cfg.LlamaPort,
		)
	}

	s.setState(StateStarting)

	binPath, err := s.ensureBinary(cfg)
	if err != nil {
		s.setState(StateFailed)
		return err
	}

	// v1.3.6 (spec §6): ENGINE PRE-FLIGHT — validate the candidate
	// BEFORE any model launch: exists, architecture, dependency
	// closure, bounded --version probe. A loader-class failure here
	// (0xC0000139 and friends) is terminal for this boot: fast,
	// classified, actionable — never a compatibility-ladder storm.
	if _, pfail := s.preflightBinary(cfg, binPath); pfail != nil {
		s.mu.Lock()
		s.detail = pfail.Error()
		s.mu.Unlock()

		s.setState(StateFailed)

		logging.Default().Error("engine", "engine preflight failed: %v", pfail)

		return pfail
	}

	// Phase 7 startup state machine — before any launch attempt the
	// engine's own CLI contract is known: a persisted VERIFIED profile
	// wins, otherwise the binary is probed once via --help (tag
	// heuristic as last resort). Every launch profile below is built
	// from and validated against this profile.
	caps := s.detectCapsForBoot(cfg, binPath)

	s.mu.Lock()
	s.caps = caps
	s.verifiedModel = ""
	s.verifiedCtx = 0
	s.mu.Unlock()

	modelPath, err := ResolveModelPath(
		cfg.ModelsDir,
		cfg.Model,
	)
	if err != nil {
		s.setState(StateFailed)
		return err
	}

	mmproj := ""

	if cfg.VisionEnabled {
		if p := vision.FindProjector(
			cfg.ModelsDir,
			modelPath,
			cfg.VisionMMProj,
		); p != "" {
			mmproj = p
			s.logf(
				"vision projector paired: %s",
				filepath.Base(p),
			)
		}
	}

	s.mu.Lock()
	s.mmproj = mmproj
	s.mu.Unlock()

	// v1.2.0: seed the vision state machine from the pre-boot evidence
	// evaluation (architecture + projector discovery + override check)
	// BEFORE the engine starts, so the UI shows honest states at every
	// point of the boot instead of guessing from filenames.
	var modelArch string
	if capsModel := ResolveModelCapabilities(cfg, modelPath); capsModel != nil {
		modelArch = capsModel.Arch
	}
	eval := vision.EvaluateModel(cfg.ModelsDir, modelPath, cfg.VisionMMProj, modelArch)
	if !cfg.VisionEnabled {
		reason := eval.Reason
		if reason == "" {
			reason = "vision disabled in settings"
		} else {
			reason += " — vision disabled in settings"
		}
		s.setVision(eval.State, reason)
	} else {
		s.setVision(eval.State, eval.Reason)
	}
	if cfg.VisionEnabled && mmproj != "" {
		s.setVision(vision.StateLoading,
			"projector "+filepath.Base(mmproj)+" paired — verifying with engine boot")
	}

	visionRetryDone := false

	for {
		startLevel := cfg.EngineCompat

		if startLevel < 0 || startLevel > engineCompatMax {
			startLevel = 0
		}

		// v1.1.7: boot-start snapshot of the persisted compat level
		// so the success path can tell a fresh descent (stamp the
		// reason) from a routine re-boot at the same level (leave
		// the stamp alone — re-stamping every boot would keep
		// re-blocking the retry-up gate below forever).
		persistedLevel := cfg.EngineCompat

		var lastErr error
		var lastReason string
		retryAttempted := false

		// persistBootOutcome records the final active configuration:
		// the compatibility level, WHY it is above full speed (when
		// it is), and the verified capability profile. The reason
		// stamp is refreshed only when the level actually changed or
		// a full-speed retry was attempted this boot.
		persistBootOutcome := func(level int, reason string) {
			stamp := level != persistedLevel || retryAttempted

			if cur := s.src.Load(); cur.EngineCompat != level || stamp {
				next := s.src.Update(func(c *config.Config) {
					c.EngineCompat = level

					if level > 0 {
						c.EngineCompatReason = reason
						c.EngineCompatAt = time.Now().UTC().Format(time.RFC3339)
					} else {
						c.EngineCompatReason = ""
						c.EngineCompatAt = ""
					}
				})

				if err := config.Save(
					next.ConfigPath(),
					next,
				); err != nil {
					s.logf("persist engine compat: %v", err)
				}
			}

			// Phase 7: persist the capability profile ONLY
			// after a verified successful startup, including any
			// surgical repairs applied along the way.
			s.mu.Lock()
			effective := s.caps
			s.mu.Unlock()
			s.persistVerifiedCaps(cfg, effective)
		}

		// v1.1.7 bounded full-speed retry-up: when the ONLY recorded
		// reason for the persisted compatibility level is an option
		// rejection, and the now-verified capability profile
		// postdates that record and validates the level-0 profile,
		// give full speed ONE chance before resuming the ladder.
		// See shouldRetryFullSpeed for the exact gate.
		if startLevel > 0 {
			level0Args := s.buildArgsWithCaps(cfg, modelPath, 0, caps)

			if retry, why := shouldRetryFullSpeed(cfg, caps, level0Args); retry {
				retryAttempted = true

				s.logf(
					"capability profile changed since the compatibility downgrade (%s) — retrying the full-speed profile once",
					why,
				)
				logging.Default().Info("engine",
					"retrying full-speed launch (compat 0) before the persisted level %d — recorded reason %q no longer applies to the verified capability profile",
					startLevel, why)

				if err := s.launchWithRepair(cfg, binPath, modelPath, 0, caps); err == nil {
					persistBootOutcome(0, "")

					s.logf("full-speed profile restored — compatibility mode no longer required")

					logging.Default().Info("engine",
						"engine restored to the full-speed profile (compat 0) for %s",
						filepath.Base(modelPath))

					s.mu.Lock()
					episodeOK := s.beginEpisodeLocked()
					s.mu.Unlock()

					if !episodeOK {
						// A deliberate Stop() raced the boot: the shutdown
						// owns the lifecycle — kill the fresh child.
						s.abortFreshBoot()

						return fmt.Errorf(
							"engine start aborted: shutdown in progress",
						)
					}

					// v1.2.0: the projector survived a VERIFIED
					// boot — the only honest path to ready.
					if mmproj != "" {
						s.setVision(vision.StateReady,
							"mmproj verified: engine serving with the paired projector "+filepath.Base(mmproj))
					}

					// v1.3.7: the ladder VERIFIED the engine — a pending
					// model-architecture update commits exactly here.
					s.commitPendingEngineUpdate()

					s.setState(StateReady)
					return nil
				}

				// Remember WHY the retry failed so a successful
				// boot further down the ladder records an honest
				// reason instead of an empty one.
				lastReason = compatReasonFromError(err)

				logging.Default().Warn("engine",
					"full-speed retry failed (%v) — resuming the compatibility ladder at level %d",
					err, startLevel)
				s.logf("full-speed retry failed — resuming at compatibility level %d", startLevel)
			}
		}

		for pass := 0; pass < 2; pass++ {
			for level := startLevel; level <= engineCompatMax; level++ {
				err := s.launchWithRepair(cfg, binPath, modelPath, level, caps)

				if err == nil {
					// Never overwrite a recorded reason with an
					// empty one (a boot that succeeds at the
					// persisted level without any new failure
					// has no fresh reason of its own).
					reasonForPersist := lastReason
					if reasonForPersist == "" {
						reasonForPersist = cfg.EngineCompatReason
					}

					persistBootOutcome(level, reasonForPersist)

					if level > 0 {
						// v1.6.0 (honest diagnostics): an empty
						// reason is reported as the EXPLICIT
						// value "unknown" — never the fake
						// diagnosis "reason unrecorded", which
						// stated the absence of a record as
						// though it were a diagnosis itself.
						why := lastReason
						if why == "" {
							why = "unknown"
						}

						s.logf(
							"engine started in compatibility mode %d (%s) — some speed flags disabled (recorded reason: %s)",
							level,
							compatLevelName(level),
							why,
						)

						logging.Default().Warn(
							"engine",
							"started in compatibility mode %d (%s) for %s — recorded reason: %s",
							level,
							compatLevelName(level),
							filepath.Base(modelPath),
							why,
						)
					}

					// A successful boot starts a fresh recovery episode.
					s.mu.Lock()
					episodeOK := s.beginEpisodeLocked()
					s.mu.Unlock()

					if !episodeOK {
						// A deliberate Stop() raced the boot: the shutdown
						// owns the lifecycle — kill the fresh child.
						s.abortFreshBoot()

						return fmt.Errorf(
							"engine start aborted: shutdown in progress",
						)
					}

					// v1.2.0: verified boot WITH the projector
					// — vision is genuinely ready.
					if mmproj != "" {
						s.setVision(vision.StateReady,
							"mmproj verified: engine serving with the paired projector "+filepath.Base(mmproj))
					}

					// v1.3.7: the ladder VERIFIED the engine — a pending
					// model-architecture update commits exactly here.
					s.commitPendingEngineUpdate()

					s.setState(StateReady)
					return nil
				}

				lastErr = err
				lastReason = compatReasonFromError(err)

				logging.Default().Error(
					"engine",
					"startup attempt failed (compat %d, %s): %v",
					level,
					compatLevelName(level),
					err,
				)

				if _, died := err.(*exitFailure); !died {
					s.setState(StateFailed)
					return err
				}

				// v1.3.6 (spec §5): CLASSIFY → STOP RETRIES.
				// A loader-class exit (0xC0000139, missing
				// DLL, bad image, ...) is deterministic: every
				// remaining compat level would execute the
				// SAME broken binary before argument parsing
				// is even reached. Break the ladder and the
				// pass loop; the diagnostic carries the class.
				if isLoaderFailure(err) {
					s.logf("loader-class startup failure — stopping compatibility retries (deterministic binary failure)")

					lf, classMatched := loaderFailure{}, false

					rep := &EngineFailureReport{
						Phase:       "launch",
						AttemptID:   nextAttemptID(),
						Context:     AttemptFirstLaunch,
						OS:          goOS,
						Arch:        goArch,
						ExePath:     binPath,
						ModelPath:   modelPath,
						CompatLevel: level,
						ClassSummary: compactLines(
							strings.Split(err.Error(), "\n"), 4,
						),
						At: time.Now().UTC(),
					}

					// v1.3.6 (spec §19): first-class evidence — the
					// launch-phase report carries the same identity
					// fields the preflight report does. The failure
					// class is only named when exit code or output
					// actually identifies it (never guessed).
					if efd, ok := err.(*exitFailure); ok {
						rep.ExitCode = efd.code
						rep.ExitCodeHex = exitCodeHex(efd.code)

						if classified, ok2 := classifyLoaderExit(efd.code); ok2 {
							lf, classMatched = classified, true
						}
					}

					if !classMatched {
						if classified, ok2 := loaderClassFromText(err.Error()); ok2 {
							lf = classified
						}
					}

					if ident, ierr := engcheck.IdentityOf(binPath); ierr == nil {
						rep.ExeSHA256 = ident.SHA256
						rep.ExeSize = ident.Size
						rep.ExeMtime = ident.ModTime
					}

					if classMatched || lf.Kind != "" {
						rep.FailureClass = string(lf.Kind)
					}

					s.mu.Lock()
					s.recordFailureReportLocked(rep)
					s.mu.Unlock()

					s.setState(StateFailed)
					return err
				}

				time.Sleep(250 * time.Millisecond)
			}

			if pass == 0 &&
				needsNewerEngine(lastErr) &&
				s.updateEngineForModel(cfg) {
				// v1.3.7: the deferred package swap is verified by the NEXT
				// ladder pass (this very loop). Commit happens on the ready
				// exits above; ANY other startLocked exit — launch failure,
				// loader-class failure, vision-retry exhaustion — restores
				// the previous package through this deferred rollback (a
				// no-op once the update committed).
				defer s.rollbackPendingEngineUpdate()

				startLevel = 0
				s.setState(StateStarting)
				continue
			}

			break
		}

		if mmproj != "" && !visionRetryDone {
			visionRetryDone = true

			projectName := filepath.Base(mmproj)
			mmproj = ""

			s.mu.Lock()
			s.mmproj = ""
			s.mu.Unlock()

			// v1.2.0: the projector is REAL but the engine
			// rejected it with every launch profile — a degraded,
			// honestly-reported state, never a silent failure.
			s.setVision(vision.StateDegraded,
				"projector "+projectName+" failed with every launch profile; running text-only")

			s.logf(
				"all profiles failed with the vision projector — retrying text-only",
			)

			logging.Default().Warn(
				"engine",
				"vision projector %s failed with every profile; restarting without vision",
				projectName,
			)

			s.setState(StateStarting)
			continue
		}

		// v1.2.0: terminal boot failure — keep the vision story
		// honest about what died.
		if state, _ := s.visionSnapshot(); state == vision.StateLoading {
			s.setVision(vision.StateFailed,
				"engine boot failed while loading the vision projector")
		}

		s.setState(StateFailed)
		return lastErr
	}
}

// VisionActive reports whether the running engine carries a multimodal
// projector.
func (s *LlamaServer) VisionActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return aliveStates[s.state] && s.mmproj != ""
}

// ProjectorPath returns the active projector file.
func (s *LlamaServer) ProjectorPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.mmproj
}

// setVision records a vision state-machine transition (v1.2.0). Every
// caller passes a reason grounded in real evidence: pairing, a verified
// boot, projector rejection, or an unresolvable override.
func (s *LlamaServer) setVision(state vision.State, reason string) {
	s.mu.Lock()
	s.visionState = state
	s.visionReason = reason
	s.mu.Unlock()
}

// visionSnapshot reads the current vision state under the lock.
func (s *LlamaServer) visionSnapshot() (vision.State, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.visionState, s.visionReason
}

// VisionStatus is the runtime vision readiness surface consumed by
// /api/engine (v1.2.0). Ready is only ever reported because the engine is
// actually serving WITH the projector; the projector byte size is measured
// from disk at snapshot time.
type VisionStatus struct {
	Active         bool         `json:"active"`
	State          vision.State `json:"state"`
	Reason         string       `json:"reason,omitempty"`
	Projector      string       `json:"projector,omitempty"`
	ProjectorName  string       `json:"projectorName,omitempty"`
	ProjectorBytes int64        `json:"projectorBytes,omitempty"`
}

// VisionStatus returns the current vision readiness of the engine. Before
// the first boot it reports the evidence-backed capability evaluation for
// the configured model; during a boot it reports loading; after a verified
// boot with the projector it reports ready.
func (s *LlamaServer) VisionStatus() VisionStatus {
	state, reason := s.visionSnapshot()

	s.mu.Lock()
	mmproj := s.mmproj
	alive := aliveStates[s.state]
	engineReady := s.state == StateReady || s.state == StateRunning ||
		s.state == StateBusy
	s.mu.Unlock()

	if !state.Known() {
		state = vision.StateSupported
		reason = "no boot has evaluated vision yet"
	}

	// A projector in flight on a verified engine is READY — the engine
	// state machine only reaches Ready after /health + model
	// verification succeeded with that exact launch configuration.
	if alive && mmproj != "" && state == vision.StateLoading && engineReady {
		state = vision.StateReady
		reason = "mmproj verified: engine serving with the paired projector " +
			filepath.Base(mmproj)
	}

	vs := VisionStatus{
		Active: alive && mmproj != "",
		State:  state,
		Reason: reason,
	}

	if mmproj != "" {
		vs.Projector = mmproj
		vs.ProjectorName = filepath.Base(mmproj)
		if fi, err := os.Stat(mmproj); err == nil {
			vs.ProjectorBytes = fi.Size()
		}
	}

	return vs
}

// Pid returns the engine subprocess pid.
func (s *LlamaServer) Pid() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}

	return 0
}

// StartedAt returns the time of the last successful boot (zero before the
// first boot). v1.1.5: measured uptime base for backend metrics.
func (s *LlamaServer) StartedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.startedAt
}

// Restarts returns the auto-restart count of the current alive episode.
// v1.1.5: measured value surfaced through backend metrics.
func (s *LlamaServer) Restarts() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.restarts
}

// ProbeHealth performs a REAL active health check against the managed
// llama.cpp /health endpoint (one bounded HTTP GET), unlike State()/
// IsAlive() which only read the cached state machine. v1.1.5: extracted
// behind the backend contract so Health() is an honest probe for both
// engines (the same endpoint waitReadySignaled polls during startup).
func (s *LlamaServer) ProbeHealth(ctx context.Context) error {
	if s.IsRunning() {
		return s.probeHealthEndpoint(ctx)
	}

	// Not alive per the authoritative state machine: a probe would
	// answer from some OTHER process that grabbed the port.
	state := s.State()

	return fmt.Errorf(
		"engine is not running (state %s)",
		state,
	)
}

// probeHealthEndpoint GETs http://{host}:{port}/health with a bounded
// client and treats HTTP 200 as healthy.
func (s *LlamaServer) probeHealthEndpoint(ctx context.Context) error {
	cfg := s.src.Load()

	url := fmt.Sprintf(
		"http://%s:%d/health",
		cfg.LlamaHost,
		cfg.LlamaPort,
	)

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		probeCtx,
		http.MethodGet,
		url,
		nil,
	)

	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("health probe: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"health probe: unexpected status %s",
			resp.Status,
		)
	}

	return nil
}

// buildArgs assembles the launch command line using the server's current
// capability profile (see buildArgsWithCaps).
func (s *LlamaServer) buildArgs(
	cfg *config.Config,
	modelPath string,
	level int,
) []string {
	s.mu.Lock()
	caps := s.caps
	s.mu.Unlock()

	return s.buildArgsWithCaps(cfg, modelPath, level, caps)
}

// buildArgsWithCaps assembles the llama.cpp server command line for a
// compatibility level, honoring the engine capability profile: Speed Pack
// options are emitted only when the engine build supports them, in the
// exact layout that build supports (Phase 7 --flash-attn fix).
//
//	0 — everything: Speed Pack flags, GPU offload, user extra args
//	1 — everything + --jinja
//	2 — --jinja + GPU, but no speed flags
//	3 — bare: model, host/port, context, threads
//
// v1.1.4: the optional sampling knobs (min-p, repeat-last-n, presence and
// frequency penalty, mirostat tau/eta) are only emitted when the user set a
// non-zero value, so default installs keep the exact launch contract the
// stress suite pins while Settings sampling edits actually reach the engine.
func (s *LlamaServer) buildArgsWithCaps(
	cfg *config.Config,
	modelPath string,
	level int,
	caps *EngineCaps,
) []string {
	base := []string{
		"--model",
		modelPath,
		"--host",
		cfg.LlamaHost,
		"--port",
		fmt.Sprintf("%d", cfg.LlamaPort),
		// Phase 7 model-aware context: the launch context is the minimum
		// of the configured limit and the loaded GGUF model's own context
		// training limit (see modelcaps.go) — a small model can no longer
		// be handed a window bigger than it actually has.
		"--ctx-size",
		fmt.Sprintf("%d", launchContextSize(cfg, modelPath)),
		"--batch-size",
		fmt.Sprintf("%d", cfg.LLM.NumBatch),
		"--threads",
		fmt.Sprintf("%d", threadsFor(cfg)),
		"--temp",
		fmt.Sprintf("%v", cfg.LLM.Temperature),
		"--top-p",
		fmt.Sprintf("%v", cfg.LLM.TopP),
		"--top-k",
		fmt.Sprintf("%d", cfg.LLM.TopK),
		"--repeat-penalty",
		fmt.Sprintf("%v", cfg.LLM.RepeatPenalty),
	}

	if cfg.LLM.MinP > 0 {
		base = append(base, "--min-p", fmt.Sprintf("%v", cfg.LLM.MinP))
	}

	if cfg.LLM.RepeatLastN > 0 {
		base = append(
			base,
			"--repeat-last-n",
			fmt.Sprintf("%d", cfg.LLM.RepeatLastN),
		)
	}

	if cfg.LLM.PresencePenalty != 0 {
		base = append(
			base,
			"--presence-penalty",
			fmt.Sprintf("%v", cfg.LLM.PresencePenalty),
		)
	}

	if cfg.LLM.FrequencyPenalty != 0 {
		base = append(
			base,
			"--frequency-penalty",
			fmt.Sprintf("%v", cfg.LLM.FrequencyPenalty),
		)
	}

	s.mu.Lock()
	mmproj := s.mmproj
	s.mu.Unlock()

	if mmproj != "" {
		base = append(base, "--mmproj", mmproj)

		// v1.2.0: explicit projector offload posture. "off" appends
		// --no-mmproj-offload (recent llama.cpp builds); "auto" and
		// "on" add nothing — the engine default is to offload the
		// projector when VRAM allows. An unsupported flag is caught
		// by the capability adapter's verified repair path, so this
		// never hard-codes an assumption about the engine build.
		if cfg.VisionMMProjOffload == "off" {
			base = append(base, "--no-mmproj-offload")
		}
	}

	if cfg.LLM.Mirostat > 0 {
		base = append(
			base,
			"--mirostat",
			fmt.Sprintf("%d", cfg.LLM.Mirostat),
		)

		if cfg.LLM.MirostatTau > 0 {
			base = append(
				base,
				"--mirostat-tau",
				fmt.Sprintf("%v", cfg.LLM.MirostatTau),
			)
		}

		if cfg.LLM.MirostatEta > 0 {
			base = append(
				base,
				"--mirostat-eta",
				fmt.Sprintf("%v", cfg.LLM.MirostatEta),
			)
		}
	}

	if cfg.LLM.Seed != 0 {
		base = append(
			base,
			"--seed",
			fmt.Sprintf("%d", cfg.LLM.Seed),
		)
	}

	if level == engineCompatMax {
		return base
	}

	gpuLayers := cfg.LLM.NumGPU

	if gpuLayers <= 0 && s.autoGPUOffload(cfg) {
		gpuLayers = 99
	}

	if gpuLayers > 0 {
		base = append(
			base,
			"--n-gpu-layers",
			fmt.Sprintf("%d", gpuLayers),
		)

		// v1.2.6 continuation: DETERMINISTIC DEVICE SELECTION. When
		// GPU offload is on, the engine's OWN enumeration named the
		// devices it can drive — pass the best one explicitly
		// (e.g. --device Vulkan0) instead of letting the engine
		// guess a default (the multi-GPU laptop case: an Intel iGPU
		// and a discrete Arc must not be chosen by accident).
		// Fail-closed on every condition: no caps, no --device
		// support, no enumeration, no device → nothing appended
		// (the pre-v1.2.6 behavior).
		if caps != nil && caps.DeviceFlag {
			if dev, ok := s.deterministicDevice(cfg); ok {
				base = append(base, "--device", dev)
			}
		}
	}

	if level <= 1 {
		base = append(base, SpeedArgsWithCaps(cfg, caps)...)
	} else if caps == nil || caps.NoWebUI {
		base = append(base, "--no-webui")
	}

	if level >= 1 && (caps == nil || caps.Jinja) {
		base = append(base, "--jinja")
	}

	if level <= 2 &&
		strings.TrimSpace(cfg.LlamaExtraArgs) != "" {
		base = append(
			base,
			strings.Fields(cfg.LlamaExtraArgs)...,
		)
	}

	return base
}

type procExit struct {
	done chan struct{}
	err  error
}

// launchWithRepair launches ONE compatibility level with Phase 7 surgical
// option repair: validate the profile → spawn → on a classified option
// failure repair ONLY the offending option and retry at the same level
// (bounded). Failures that are not option problems (model load, host
// environment, unknown architecture) are returned untouched so the outer
// compatibility ladder / engine-update logic handles them.
func (s *LlamaServer) launchWithRepair(
	cfg *config.Config,
	binPath string,
	modelPath string,
	level int,
	caps *EngineCaps,
) error {
	if caps == nil {
		caps = defaultCapsForTag(currentEngineTag(cfg))
	}

	effective := caps.Clone()

	for attempt := 0; ; attempt++ {
		args := s.buildArgsWithCaps(cfg, modelPath, level, effective)

		// Phase 7: validate option/value pairs BEFORE spawning. The
		// historical regression (--flash-attn --cache-reuse) fails
		// validation here and can therefore never reach the engine.
		if problems := argProblems(args, effective); len(problems) > 0 {
			logging.Default().Error("engine",
				"launch profile rejected by pre-launch validation (compat %d): %s",
				level, strings.Join(problems, "; "))
			s.logf("launch profile validation failed: %s",
				strings.Join(problems, "; "))
			return fmt.Errorf(
				"launch profile failed pre-launch validation: %s",
				strings.Join(problems, "; "))
		}

		err := s.launchArgs(cfg, binPath, modelPath, args)
		if err == nil {
			s.mu.Lock()
			s.caps = effective
			s.mu.Unlock()
			return nil
		}

		// Non-exit failures (spawn problems, readiness timeouts)
		// are not option-layout problems — no surgical repair.
		if _, died := err.(*exitFailure); !died {
			return err
		}

		if attempt >= maxOptionRepairs {
			return err
		}

		sf := ClassifyStartupFailure(err.Error())
		if sf == nil || sf.Kind != FailOptionLayout && sf.Kind != FailUnknownOption {
			return err
		}

		repaired := repairCapsFor(effective, sf, args)
		if repaired == nil {
			return err
		}

		logRepair(level, effective, repaired, sf)
		s.logf("repairing option layout and retrying (compat %d, attempt %d/%d)",
			level, attempt+1, maxOptionRepairs)

		effective = repaired
	}
}

// launchArgs spawns the server with one explicit argument vector and waits
// until it is healthy, the model is verified and the context capability is
// recorded (Phase 7 verified startup state machine).
func (s *LlamaServer) launchArgs(
	cfg *config.Config,
	binPath string,
	modelPath string,
	args []string,
) error {
	// v1.8.6 PER-BOOT EVIDENCE RESET: the offload line belongs to the
	// process boot that prints it. A restart or model swap must never
	// inherit the previous boot's GPU execution evidence — each boot
	// re-proves (ObserveEngineLine re-fills it when the new process
	// actually offloads). This is the stale-evidence guard for the
	// strongest per-boot proof.
	s.mu.Lock()
	s.offloadEvidence = ""
	s.mu.Unlock()

	cmd := proc.Command(
		binPath,
		args...,
	)

	cmd.Dir = cfg.DataDir

	cmd.Stdout = newLineWriter(func(line string) {
		s.logf("[llama.cpp] %s", line)

		// v1.2.6: capture the runtime offload evidence (the real
		// "offloaded N/M layers to GPU" line) for the accelerator
		// posture + /api/perf.
		s.ObserveEngineLine(line)
	})

	s.errRing.reset()

	cmd.Stderr = newLineWriter(func(line string) {
		s.logf("[llama.cpp!] %s", line)
		s.errRing.add(line)
		s.ObserveEngineLine(line)
	})

	proc.Hide(cmd)

	if err := cmd.Start(); err != nil {
		return &exitFailure{
			err: fmt.Errorf("start llama.cpp: %w", err),
		}
	}

	exit := &procExit{
		done: make(chan struct{}),
	}

	s.mu.Lock()
	s.cmd = cmd
	s.exitDone = exit.done
	s.mu.Unlock()

	go func() {
		exit.err = cmd.Wait()

		// v1.2.5 exit classification: the watcher snapshots whether
		// the shutdown was DELIBERATE (Stop() armed s.stopping before
		// this classification ran) in the SAME critical section that
		// computes liveness. Intent is never inferred after the fact:
		// a deliberate stop must NEVER be logged as a crash, counted
		// as an unexpected restart, or reported as an error.
		s.mu.Lock()

		latest := s.cmd == cmd

		if latest {
			s.cmd = nil
			s.exitDone = nil
		}

		deliberate := s.stopping
		wasAlive :=
			latest &&
				s.loaded == modelPath &&
				aliveStates[s.state]

		if wasAlive {
			s.loaded = ""
		}

		// v1.2.5 diagnostics snapshot: everything needed to diagnose
		// a REAL death, captured atomically with the classification.
		diag := exitDiagnostics{
			model:       filepath.Base(modelPath),
			state:       s.state,
			stopping:    deliberate,
			visionState: string(s.visionState),
			visionMsg:   s.visionReason,
		}

		if cmd.Process != nil {
			diag.pid = cmd.Process.Pid
		}

		if s.caps != nil {
			diag.tag = s.caps.Tag
		}

		diag.stderrTail = s.errRing.lines()

		s.mu.Unlock()

		diag.exit = exitDescription(exit.err)

		switch {
		case wasAlive && deliberate:
			// Deliberate shutdown: the normal stopped transition.
			// No ERROR, no restart, no restart-count increment.
			// Stop() drives the state machine to stopped; this
			// only makes the transition honest in the log.
			logging.Default().Info(
				"engine",
				"engine stopped (deliberate shutdown, %s)",
				diag.exit,
			)
		case wasAlive:
			// Unexpected process death: ERROR + bounded recovery.
			s.setState(StateStopped)

			// v1.2.5 honest detail for /api/engine and the UI.
			s.SetDetail("Unexpected exit: " + diag.exit)

			logging.Default().Error(
				"engine",
				"engine exited unexpectedly: %s\n%s",
				diag.exit,
				diag.render(s.logBuf.lines()),
			)

			// v1.1.3 bounded auto-recovery, v1.2.5 lifecycle-
			// owned: the watchdog restarts the engine a bounded
			// number of times with backoff so a crashed engine
			// recovers transparently, while a fundamentally
			// broken setup surfaces as `failed` instead of
			// looping forever.
			s.scheduleAutoRestart(modelPath)
		}

		close(exit.done)
	}()

	if err := s.waitReadySignaled(
		cfg,
		modelLoadTimeout,
		exit,
	); err != nil {
		s.mu.Lock()

		c := s.cmd

		if s.cmd == cmd {
			s.cmd = nil
			s.exitDone = nil
		}

		s.mu.Unlock()

		if c != nil && c.Process != nil {
			_ = c.Process.Kill()
		}

		select {
		case <-exit.done:
		case <-time.After(3 * time.Second):
		}

		return err
	}

	s.mu.Lock()

	if s.cmd != cmd {
		s.mu.Unlock()

		select {
		case <-exit.done:
		case <-time.After(time.Second):
		}

		return &exitFailure{
			err:  fmt.Errorf("llama.cpp died immediately after becoming ready"),
			tail: s.errRing.lines(),
		}
	}

	s.loaded = modelPath
	s.mu.Unlock()

	// Phase 7 verified startup state machine: /health 200 proves the
	// PROCESS serves; it does not prove the MODEL is loaded and the
	// CONTEXT capability is real. Both are verified here, best-effort
	// and honestly recorded — an unverifiable claim is never claimed.
	s.verifyStartupState(cfg, modelPath)

	return nil
}

// verifyStartupState probes the freshly-ready engine for model-loaded and
// context-capability evidence. Bounded and non-fatal: older builds may not
// expose the endpoints, in which case the verification fields stay empty
// and the UI sees exactly that.
func (s *LlamaServer) verifyStartupState(cfg *config.Config, modelPath string) {
	client := &http.Client{Timeout: 2 * time.Second}
	base := fmt.Sprintf("http://%s:%d", cfg.LlamaHost, cfg.LlamaPort)

	// 1. Verify the model is actually served: GET /v1/models.
	var verifiedModel string
	if resp, err := client.Get(base + "/v1/models"); err == nil {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var parsed struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &parsed) == nil && len(parsed.Data) > 0 {
				verifiedModel = parsed.Data[0].ID
			}
		}
	}

	// 2. Verify the context capability: GET /props (n_ctx when exposed).
	var verifiedCtx int
	if resp, err := client.Get(base + "/props"); err == nil {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			verifiedCtx = parsePropsContext(body)
		}
	}

	// 3. Fall back to the model card when the engine does not report:
	// the GGUF training limit is evidence too (launchContextSize already
	// clamped --ctx-size to it).
	if verifiedCtx <= 0 {
		if caps := ResolveModelCapabilities(cfg, modelPath); caps != nil {
			verifiedCtx = launchContextSize(cfg, modelPath)
		}
	}

	s.mu.Lock()
	s.verifiedModel = verifiedModel
	s.verifiedCtx = verifiedCtx
	s.mu.Unlock()

	if verifiedModel != "" {
		s.logf("verified model loaded: %s", verifiedModel)
	}
	if verifiedCtx > 0 {
		s.logf("verified context capability: %d tokens", verifiedCtx)
	}
}

// parsePropsContext extracts n_ctx from llama.cpp /props JSON (several
// historical shapes are tolerated).
func parsePropsContext(body []byte) int {
	var shapes struct {
		DefaultGenerationSettings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
		NCtx int `json:"n_ctx"`
	}
	if json.Unmarshal(body, &shapes) != nil {
		return 0
	}
	if shapes.DefaultGenerationSettings.NCtx > 0 {
		return shapes.DefaultGenerationSettings.NCtx
	}
	return shapes.NCtx
}

// VerifiedModel returns the model id the engine actually serves (empty
// when the verification probe could not run).
func (s *LlamaServer) VerifiedModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifiedModel
}

// VerifiedContext returns the engine-verified context window in tokens
// (0 when unverified).
func (s *LlamaServer) VerifiedContext() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifiedCtx
}

// EngineContextLimit returns the engine-verified context window ONLY when
// the subprocess is currently alive (starting/running/ready/busy). A dead
// engine reports 0 — a stale window from a previous boot must never clamp
// a plan for a boot that may use a different --ctx-size.
func (s *LlamaServer) EngineContextLimit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !aliveStates[s.state] {
		return 0
	}
	return s.verifiedCtx
}

// VerifiedReady reports whether the current boot proved real readiness:
// process alive AND /health 200 AND the serving model verified via
// /v1/models. "Ready" in the UI must mean this, not merely a spawned
// subprocess.
func (s *LlamaServer) VerifiedReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return aliveStates[s.state] && s.verifiedModel != ""
}

// Caps returns the capability profile in force for the current/last boot.
func (s *LlamaServer) Caps() *EngineCaps {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps.Clone()
}

// waitReadySignaled polls /health until 200, but aborts immediately when
// the subprocess exits or the timeout passes.
func (s *LlamaServer) waitReadySignaled(
	cfg *config.Config,
	timeout time.Duration,
	exit *procExit,
) error {
	deadline := time.Now().Add(timeout)

	url := fmt.Sprintf(
		"http://%s:%d/health",
		cfg.LlamaHost,
		cfg.LlamaPort,
	)

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	for time.Now().Before(deadline) {
		if resp, err := client.Get(url); err == nil {
			_ = resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-exit.done:
			return s.exitError(exit.err)

		case <-time.After(400 * time.Millisecond):
		}
	}

	select {
	case <-exit.done:
		return s.exitError(exit.err)

	default:
	}

	tail := s.tailLines(6)

	msg := fmt.Sprintf(
		"llama.cpp did not become ready within %v "+
			"(the model may be too large for this machine)",
		timeout,
	)

	if tail != "" {
		msg += "\n\nEngine output (last lines):\n" + tail
	}

	return fmt.Errorf("%s", msg)
}

func (s *LlamaServer) exitError(err error) error {
	code := 0
	if c := exitCodeOf(err); c != nil {
		code = *c
	}

	return &exitFailure{
		err:  explainExit(err),
		tail: s.errRing.lines(),
		code: code,
	}
}

type exitFailure struct {
	err  error
	tail []string

	// code is the raw process exit code when the child actually ran and
	// exited (0 = unknown/not a process exit). v1.3.6: loader-class
	// detection and diagnostics decode from this field.
	code int
}

func explainExit(err error) error {
	if err == nil {
		return fmt.Errorf("llama.cpp exited during startup")
	}

	// v1.3.6 (spec §4): decode Windows loader/runtime failures into
	// first-class diagnostics with advice instead of the weak
	// "exit status 0xc0000139".
	if code := exitCodeOf(err); code != nil {
		if lf, ok := classifyLoaderExit(*code); ok {
			return loaderFailureError(lf, "")
		}
	}

	if strings.Contains(err.Error(), "3221225781") ||
		strings.Contains(err.Error(), "0xc0000135") {
		return fmt.Errorf(
			"llama.cpp could not start: a required Windows DLL is missing. "+
				"Install the free Microsoft Visual C++ Redistributable (64-bit) "+
				"from https://aka.ms/vs/17/release/vc_redist.x64.exe "+
				"and start again: %v",
			err,
		)
	}

	return fmt.Errorf(
		"llama.cpp exited during startup: %v",
		err,
	)
}

func (e *exitFailure) Error() string {
	msg := "llama.cpp exited during startup"

	if e.err != nil {
		msg = e.err.Error()
	}

	if tail := compactLines(e.tail, 12); tail != "" {
		msg += "\n\nEngine output (last lines):\n" + tail
	}

	return msg
}

func (e *exitFailure) Unwrap() error {
	return e.err
}

func (s *LlamaServer) tailLines(n int) string {
	return compactLines(s.errRing.lines(), n)
}

func compactLines(lines []string, n int) string {
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	var kept []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		if len(line) > 200 {
			line = line[:200] + "…"
		}

		kept = append(kept, line)
	}

	return strings.Join(kept, "\n")
}

func needsNewerEngine(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	for _, sig := range []string{
		"unknown model architecture",
		"unknown architecture",
		"unsupported architecture",
		"unknown model type",
		"unrecognized",
	} {
		if strings.Contains(msg, sig) {
			return true
		}
	}

	return false
}

// commitPendingEngineUpdate commits the deferred model-architecture
// engine update once the compat ladder has VERIFIED the new engine
// (StateReady). Called only from startLocked paths (switchMu held);
// a no-op when no deferred update is pending. v1.3.7.
func (s *LlamaServer) commitPendingEngineUpdate() {
	if s.pendingEngineUpdate == nil {
		return
	}

	staged := s.pendingEngineUpdate
	s.pendingEngineUpdate = nil

	staged.Commit()

	logging.Default().Info("engine",
		"model-architecture engine update verified ready — build %s committed as the active engine (next boot probes it)",
		staged.Tag())
}

// rollbackPendingEngineUpdate restores the previous engine package after
// the compat ladder FAILED to verify the deferred model-architecture
// update (startLocked exit). Called only from startLocked paths
// (switchMu held); a no-op when nothing is pending or the update already
// committed. v1.3.7.
func (s *LlamaServer) rollbackPendingEngineUpdate() {
	if s.pendingEngineUpdate == nil {
		return
	}

	staged := s.pendingEngineUpdate
	s.pendingEngineUpdate = nil

	if err := staged.Rollback(); err != nil {
		logging.Default().Warn("engine",
			"rollback of the unverified model-architecture engine update failed: %v", err)
		return
	}

	logging.Default().Warn("engine",
		"model-architecture engine update failed startup verification — previous package restored")
}

func (s *LlamaServer) updateEngineForModel(cfg *config.Config) bool {
	if s.engineUpdateTried {
		return false
	}

	s.engineUpdateTried = true

	if netcheck.IsOffline() {
		s.logf(
			"model needs a newer engine but the machine is offline — skipping auto-update",
		)
		return false
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Minute,
	)
	defer cancel()

	latest, err := updater.LatestTag(ctx)
	if err != nil || latest == "" {
		s.logf(
			"could not find a newer engine: %v",
			err,
		)
		return false
	}

	// v1.7.4 (P0 #3, B): the EFFECTIVE installed tag — the recorded tag, or
	// the committed manifest beside the binary when the state file lost it.
	current := updater.EffectiveInstalledEngineTag(cfg)

	if current == "" {
		current = updater.DefaultEngineTag
	}

	if latest == current {
		s.logf(
			"engine %s is already the newest release — "+
				"the model is still not loadable",
			current,
		)
		return false
	}

	s.logf(
		"this model needs a newer llama.cpp — updating %s → %s",
		current,
		latest,
	)

	logging.Default().Info(
		"engine",
		"auto-updating engine for new model architecture: %s → %s",
		current,
		latest,
	)

	s.setState(StateDownloading)

	// v1.3.7: the model-architecture auto-update now uses the SAME
	// deferred-commit transactional install as every other engine update
	// (the v1.3.6 path passed eng=nil, which selected the legacy
	// stop/download/copy/restart choreography: InstallStaged committed
	// IMMEDIATELY — no startup verification, no rollback, and the
	// last-known-good package was deleted by Commit before the new binary
	// had ever been launched). The ladder's next pass below IS the startup
	// verification: commitPendingEngineUpdate commits on ready, the
	// function-exit rollback restores the previous package on failure.
	staged, err := s.runEngineInstall(ctx, cfg, latest, s.publishDownloadProgress)
	if err != nil {
		s.logf(
			"engine auto-update failed: %v",
			err,
		)

		logging.Default().Warn(
			"engine",
			"auto-update failed: %v",
			err,
		)

		return false
	}

	// The deferred install swapped the new package in WITHOUT committing
	// the tag: the ladder's verification pass decides (see the call site in
	// the compat ladder).
	s.pendingEngineUpdate = staged

	next := s.src.Update(func(c *config.Config) {
		c.EngineCompat = 0
	})

	if err := config.Save(next.ConfigPath(), next); err != nil {
		s.logf("persist engine compat reset: %v", err)
	}

	return true
}

// autoGPUOffload decides GPU layer offload from MEASURED evidence
// (v1.2.6).
//
// Decision ladder, strongest evidence first:
//
//  1. the installed engine's OWN --list-devices enumerated a device →
//     offload is genuinely available;
//  2. a PREVIOUS boot's runtime log measured real offload
//     (OffloadEvidence non-empty) → the same launch posture is proven;
//  3. the engine build does not support enumeration but a Vulkan backend
//     sits beside the binary → the documented weaker fallback (the offload
//     line parsing verifies it at the next boot, feeding ladder step 2).
//
// The v1.2.5 rule — "Vulkan DLL exists + any adapter present" — is gone:
// a DLL beside the binary proves nothing about the devices the engine can
// drive, and a WMI adapter is not an engine capability.

// deterministicDevice returns the ENGINE-ENUMERATED device the launcher
// should pass via --device for deterministic accelerator selection
// (v1.2.6 continuation). ok=false whenever the selection cannot be made
// from the engine's OWN enumeration — an inferred hardware name is never
// substituted (fail-closed: without enumeration evidence the engine keeps
// its default device choice, exactly like every release before this).
//
// Selection rule: the enumerated device with the MOST reported memory
// (multi-GPU machines: the discrete accelerator beats the integrated one).
// The device string is the engine's own backend name (e.g. "Vulkan0").
func (s *LlamaServer) deterministicDevice(cfg *config.Config) (string, bool) {
	bin := cfg.LlamaBinPath
	if bin == "" {
		bin = filepath.Join(cfg.DataDir, "bin", llamaBinaryName())
	}

	devices, supported, err := EnumerateEngineDevices(bin)
	if err != nil || !supported || len(devices) == 0 {
		return "", false
	}

	best := devices[0]
	for _, d := range devices[1:] {
		if d.TotalMB > best.TotalMB {
			best = d
		}
	}

	if best.Backend == "" {
		return "", false
	}

	logging.Default().Info("engine",
		"deterministic device selection: --device %s (%s, %d MB, %d enumerated)",
		best.Backend, best.Name, best.TotalMB, len(devices))

	return best.Backend, true
}

// autoGPUOffload decides whether AUTO may turn on GPU offload
// (--n-gpu-layers 99) for THIS launch. v1.8.6 TRUTH MODEL:
//
//	GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU verified
//
// Two very different modes:
//
//   - CANDIDATE PROVING (s.gpuCandidateProving, set only inside the
//     bounded variant transaction while the verification hook runs):
//     enumeration/selection evidence MAY enable offload — the
//     transaction's autoProbeVerify hook then REQUIRES real generation
//     AND the measured offload line, so an enumeration-only candidate
//     still cannot commit. This is exactly "a candidate transaction may
//     temporarily boot Vulkan to prove it".
//
//   - NORMAL SERVING: offload is enabled ONLY by PROVEN execution —
//     the CURRENT boot's measured offload line, or a still-valid
//     persisted GPU execution receipt (the committed bounded
//     transaction outcome, engine-tag+variant identity-checked).
//     Enumeration alone and DLL presence are NOT sufficient: AUTO stays
//     CPU-safe until the accelerator has actually been proven.
//
// Manual configuration is untouched by this function: an explicit
// cfg.LLM.NumGPU > 0 is applied verbatim in buildArgs (the user's own
// posture), and CPU-forced mode (accelerator profile CPU) never enables
// auto-offload because the CPU profile is checked by the launcher first.
func (s *LlamaServer) autoGPUOffload(cfg *config.Config) bool {
	if !cfg.GPUAutoOffload {
		return false
	}

	// v1.8.6: the CPU-forced profile stays CPU — exactly as before, and
	// now enforced at the launcher too (a CPU request with a stale
	// gpuAutoOffload=true must not silently serve GPU).
	if accelerator.NormalizeRequested(cfg.Accelerator) == accelerator.RequestCPU {
		logging.Default().Info("engine",
			"GPU offload OFF: the requested accelerator profile is CPU (forced) — AUTO honors it")
		return false
	}

	if s.gpuProvingActive() {
		ok := s.provisionableGPUPresent(cfg)
		logging.Default().Info("engine",
			"GPU offload ON (candidate proving): selection evidence present — the transaction's verification hook requires the measured offload line before commit")
		return ok
	}

	if proven, why := s.gpuExecutionProven(cfg); proven {
		logging.Default().Info("engine",
			"GPU offload ON: proven GPU execution (%s)", why)
		return true
	}

	// Enumeration or DLL presence alone: AUTO stays CPU-safe — the
	// honest posture until real execution evidence exists.
	logging.Default().Info("engine",
		"GPU offload OFF: no proven GPU execution for this engine identity (enumeration/DLL presence is selection evidence, not execution proof) — AUTO stays CPU-safe")
	return false
}

// gpuProvingActive reports whether the current launch happens inside a
// bounded GPU candidate verification transaction.
func (s *LlamaServer) gpuProvingActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gpuCandidateProving
}

// provisionableGPUPresent reports SELECTION-level evidence: the engine
// enumerated at least one device, or the current boot already measured
// offload, or the documented enumeration-unsupported DLL fallback. Valid
// for candidate proving ONLY — never for a verified serving claim.
func (s *LlamaServer) provisionableGPUPresent(cfg *config.Config) bool {
	bin := cfg.LlamaBinPath
	if bin == "" {
		bin = filepath.Join(cfg.DataDir, "bin", llamaBinaryName())
	}

	devices, supported, err := EnumerateEngineDevices(bin)

	switch {
	case err == nil && supported && len(devices) > 0:
		return true
	case s.OffloadEvidence() != "":
		return true
	case err == nil && !supported && s.hasVulkanBackend(cfg):
		return true
	default:
		if err != nil {
			logging.Default().Warn("engine", "device enumeration failed: %v", err)
		}
		return false
	}
}

// gpuExecutionProven reports whether GPU execution is PROVEN for the
// CURRENT engine identity — the only basis a normal AUTO serving launch
// may enable GPU offload:
//
//  1. the CURRENT boot printed a measured offload line (the strongest
//     per-boot proof — refreshed on every model load); or
//  2. a persisted, still-valid GPU execution receipt exists: the
//     committed bounded transaction outcome whose engine tag and variant
//     still match the installed engine (stale receipts are rejected —
//     never verify one binary and serve another).
//
// Enumeration and DLL presence are deliberately absent: they are
// selection evidence, not execution proof.
func (s *LlamaServer) gpuExecutionProven(cfg *config.Config) (bool, string) {
	if evidence := s.OffloadEvidence(); evidence != "" {
		return true, "current-boot measured offload line: " + evidence
	}

	st, ok := LoadGPUProbeState(cfg)
	if ok && st.Status == GPUProbeStatusVerified {
		tag := updater.InstalledEngineTag(cfg)
		variant := string(updater.InstalledEngineVariant(cfg))

		receipt := accelerator.ExecutionReceipt{
			Kind:      accelerator.ReceiptKindVerifiedProbe,
			Line:      st.Evidence,
			EngineTag: st.EngineTag,
			Variant:   st.Variant,
			Device:    st.Device,
			Model:     st.Model,
			Status:    st.Status,
		}

		if receipt.ValidFor(tag, variant) {
			return true, fmt.Sprintf(
				"committed GPU execution receipt for engine %s/%s (device %q, evidence %q)",
				st.EngineTag, st.Variant, st.Device, st.Evidence)
		}

		logging.Default().Info("engine",
			"persisted GPU probe receipt is stale for the current engine identity (%s/%s vs receipt %s/%s) — GPU execution unproven until re-proven",
			tag, variant, st.EngineTag, st.Variant)
	}

	return false, ""
}

func (s *LlamaServer) hasVulkanBackend(cfg *config.Config) bool {
	return VulkanAvailable(cfg)
}

// VulkanAvailable reports whether a Vulkan backend sits beside the engine
// binary named by cfg (v1.2.0: shared with the hardware intelligence layer
// so GPU-posture recommendations rest on the same evidence as launch).
func VulkanAvailable(cfg *config.Config) bool {
	bin := cfg.LlamaBinPath

	if bin == "" {
		bin = filepath.Join(
			cfg.DataDir,
			"bin",
			llamaBinaryName(),
		)
	}

	if _, err := os.Stat(
		filepath.Join(
			filepath.Dir(bin),
			"ggml-vulkan.dll",
		),
	); err == nil {
		return true
	}

	if runtime.GOOS != "windows" {
		if _, err := os.Stat(
			filepath.Join(
				filepath.Dir(bin),
				"libggml-vulkan.so",
			),
		); err == nil {
			return true
		}
	}

	return false
}

func (s *LlamaServer) HasVulkanBackendForTest() bool {
	return s.hasVulkanBackend(s.src.Load())
}

func (s *LlamaServer) AutoGPUOffloadForTest() bool {
	return s.autoGPUOffload(s.src.Load())
}

func (s *LlamaServer) BuildArgsForTest(
	modelPath string,
	level int,
) []string {
	return s.buildArgs(s.src.Load(), modelPath, level)
}

func (s *LlamaServer) SetProjectorForTest(path string) {
	s.mu.Lock()
	s.mmproj = path
	s.mu.Unlock()
}

func (s *LlamaServer) ProjectorPathForTest() string {
	return s.ProjectorPath()
}

func MakeExitFailureForTest(
	err error,
	tail []string,
) error {
	return &exitFailure{
		err:  explainExit(err),
		tail: tail,
	}
}

func IsExitFailureForTest(err error) bool {
	_, ok := err.(*exitFailure)
	return ok
}

func NeedsNewerEngineForTest(err error) bool {
	return needsNewerEngine(err)
}

func CompactLinesForTest(
	lines []string,
	n int,
) string {
	return compactLines(lines, n)
}

// adoptExisting moved to adoption.go (v1.3.6): adoption now REQUIRES
// process-identity proof (listening PID + executable path equal to the
// managed engine binary) instead of trusting any /health 200 on the
// port.

// Stop terminates the subprocess gracefully: SIGTERM, bounded grace
// period, then kill — followed by a BOUNDED wait until the exit watcher
// reports cmd.Wait() has returned.
//
// v1.2.0 (Windows): Kill() only REQUESTS termination; the process object
// (and the lock the dying process holds on its executable) survives until
// Wait() reaps it. Stop() previously returned right after Kill, so the
// fake-engine test binary could still be alive when `go test` tried to
// remove or rewrite it — the observed "unlinkat llm.test.exe: Access is
// denied" plus orphan llm.test/conhost processes. Deterministic ownership
// means Stop does not return until the child is reaped or the bounded
// wait is exhausted.
func (s *LlamaServer) Stop() error {
	s.mu.Lock()

	cmd := s.cmd
	done := s.exitDone
	s.stopping = true

	// v1.2.5: a deliberate stop cancels any pending watchdog restart and
	// waits (bounded) for that goroutine to exit. After Stop returns, no
	// delayed watchdog can call Start().
	s.watchArmed = false
	watchStop, watchDone := s.watchStop, s.watchDone
	s.watchStop = nil
	s.watchDone = nil

	if cmd == nil || cmd.Process == nil {
		s.cmd = nil
		s.exitDone = nil
		s.loaded = ""
		s.mu.Unlock()

		// v1.2.5: even with no live process, a watchdog armed by an
		// earlier unexpected death must be canceled — this stop is
		// deliberate and owns the lifecycle now.
		if watchStop != nil {
			close(watchStop)
		}

		if watchDone != nil {
			select {
			case <-watchDone:
			case <-time.After(2 * time.Second):
			}
		}

		s.setState(StateStopped)
		return nil
	}

	s.mu.Unlock()

	s.setState(StateStopping)

	_ = cmd.Process.Signal(syscall.SIGTERM)

	// Bounded grace: wait for the watcher's close(exit.done) — a real
	// signal of Wait() completion — instead of blind-polling process
	// identity. The 50ms fallback poll covers a watcher that has not
	// observed the exit yet.
	deadline := time.Now().Add(4 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-done:
			deadline = time.Time{} // already reaped
		default:
		}

		s.mu.Lock()
		alive := s.cmd == cmd
		s.mu.Unlock()

		if !alive {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	s.mu.Lock()
	alive := s.cmd == cmd
	s.mu.Unlock()

	if alive && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}

	// Reap deterministically: after Kill, the child must be observed
	// exiting before this call returns (bounded — a wedged child must
	// not hang the caller forever, but the OS gets a fair window to
	// release the executable).
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}

	s.mu.Lock()

	if s.cmd == cmd {
		s.cmd = nil
		s.exitDone = nil
	}

	s.loaded = ""
	s.mu.Unlock()

	// v1.2.5: cancel the pending watchdog (if any) and wait for its
	// goroutine to exit — a deliberate shutdown never leaves a delayed
	// restart behind. After the child is reaped, the exit watcher has
	// already observed stopping=true, so it classified this exit as
	// deliberate (no ERROR, no restart) — the cancel below only covers
	// a watchdog armed BEFORE this Stop began.
	if watchStop != nil {
		close(watchStop)
	}

	if watchDone != nil {
		select {
		case <-watchDone:
		case <-time.After(2 * time.Second):
		}
	}

	s.setState(StateStopped)

	return nil
}

// EnsureRunning boots the engine if it is not already running.
func (s *LlamaServer) EnsureRunning() error {
	if s.IsRunning() {
		return nil
	}

	return s.Start()
}

// Restart stops the server and boots it again.
func (s *LlamaServer) Restart() error {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()

	if err := s.Stop(); err != nil {
		return err
	}

	// v1.4.0: replace the fixed 300 ms pause with the REAL readiness
	// condition — the engine port must be FREE again before the new
	// process can bind it. The old elapsed-time proxy raced on slow
	// machines (new engine failed to bind) and taxed every restart on
	// fast ones. Bounded: 5 s of polling, then the start attempt runs
	// anyway and reports its own bind error honestly.
	restartCfg := s.src.Load()
	waitPortFree(restartCfg.LlamaHost, restartCfg.LlamaPort, 5*time.Second)

	return s.startLocked()
}

// waitPortFree blocks until nothing is listening on host:port or the
// bound expires. It polls the OS listener table through net.Dial — a
// real state check, never a sleep-and-hope.
func waitPortFree(host string, port int, bound time.Duration) {
	if host == "" {
		host = "127.0.0.1"
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	deadline := time.Now().Add(bound)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)

		if err != nil {
			return // nothing accepted the dial — the port is free
		}

		_ = conn.Close()

		time.Sleep(50 * time.Millisecond)
	}
}

// UpdateEngineNow is the ENGINE-OWNED exclusive update operation
// (v1.3.6, spec §1/§2). It is the only way the updater touches engine
// files while this LlamaServer is alive: it holds switchMu for the WHOLE
// transaction so Start/Restart/prewarm can never interleave, stops the
// engine itself, installs the staged candidate through the ONE
// provisioning authority (updater), restarts the EXACT installed binary
// and verifies readiness before committing.
//
// UpdateEngineNow transaction (spec §7 — COMMIT and CLEANUP happen only
// after the installed binary is started and verified):
//
//	take lifecycle lock → STOP current engine →
//	stage+validate+swap (updater, deferred commit) → on failure:
//	restart last-known-good → on swap success: START exact installed
//	binary → verify ready → COMMIT tag + manifest + remove previous
//	package — or on startup failure: ROLLBACK (previous package
//	restored byte-for-byte) → restart last-known-good → RETURN ERROR
//	(never nil after an unverified engine, spec §13/§14).
//
// A download/install failure NEVER destroys the working installation:
// the previous binary stays on disk and is restarted.
func (s *LlamaServer) UpdateEngineNow(
	ctx context.Context,
	tag string,
	onProgress func(downloader.Progress),
) (string, error) {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()

	// v1.3.6 (spec §16): hold the cross-process ownership lease for the
	// whole maintenance window — another SHEYTAN instance (or the CLI
	// updater) must never manage this installation concurrently. The
	// lease is re-entrant for this process.
	cfg := s.src.Load()

	lease, leaseErr := englease.Acquire(engineLeaseDir(cfg), englease.OwnerLease, "engine-owner")
	if leaseErr != nil {
		return "", fmt.Errorf("engine update refused: %w", leaseErr)
	}

	defer lease.Release()

	// v1.10.0 RUN-COORDINATION — EARLY advisory refusal (no claim
	// taken), the SAME semantics the variant transaction has had since
	// v1.9.4/v1.9.6: a run registered right now refuses the update
	// before ANYTHING happens — before the lifecycle stop, before the
	// install window, before any state transition. The registry read
	// is read-only here so this fast path can never leak a claim.
	if s.runActiveNow() {
		logging.Default().Warn("updater",
			"engine update refused: a run is active — the serving engine is never stopped under a live generation")

		return "", ErrEngineBusyWithRun
	}

	// v1.10.0 ADMISSION CLAIM — AT THE DESTRUCTIVE BOUNDARY. This
	// closes the standing v1.9.6 admission-seam gap: UpdateEngineNow
	// (the same-tag/scheduled update transaction) stopped the engine
	// WITHOUT the run-admission claim the variant transaction takes,
	// so a run could be admitted onto an engine that was about to be
	// stopped (or was down for the install window) instead of being
	// refused deterministically (HTTP 503, errEngineMutationInProgress).
	// The SAME claim primitives, the SAME balance-on-every-exit
	// discipline and the SAME live-verdict refusal semantics as
	// updateEngineVariantTx: the claim is taken atomically with the
	// live-run verdict here — immediately before the first destructive
	// step (the stop) — and released exactly once on every exit path.
	// Unlike the variant transaction there is NO claim-free download
	// window to protect: this transaction's install phase runs with
	// the engine already stopped, so the claim correctly spans the
	// whole window in which the product cannot serve. Callers treat
	// the refusal as a deferral (the AUTO probe pattern): a temporary
	// condition, never persisted as a bounded failure.
	s.mu.Lock()
	admission := s.admission
	s.mu.Unlock()

	if admission != nil {
		if live := admission.BeginEngineMutation(); live {
			logging.Default().Warn("updater",
				"engine update refused at its destructive boundary: a run is active — the serving engine is never stopped under a live generation")

			return "", ErrEngineBusyWithRun
		}

		s.mu.Lock()
		s.mutationClaimed = true
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			s.mutationClaimed = false
			s.mu.Unlock()

			admission.EndEngineMutation()
		}()
	} else if s.runActiveNow() {
		// Historical seam (no admission authority — some tests,
		// headless): the destructive-boundary re-check alone closes
		// the between-checks registration race, exactly as the
		// variant transaction's nil-seam path does.
		logging.Default().Warn("updater",
			"engine update refused at its destructive boundary: a run is active — the serving engine is never stopped under a live generation")

		return "", ErrEngineBusyWithRun
	}

	wasRunning := s.IsRunning()

	s.mu.Lock()
	s.stopping = true // suppress the exit watcher's auto-restart during the swap
	s.mu.Unlock()

	if wasRunning {
		s.setState(StateUpdating)

		logging.Default().Info("updater", "stopping engine for transactional update to %s", tag)

		if err := s.Stop(); err != nil {
			s.mu.Lock()
			s.stopping = false
			s.mu.Unlock()

			return "", fmt.Errorf("stop engine before update: %w", err)
		}
	} else {
		s.setState(StateUpdating)
	}

	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()

	staged, err := s.runEngineInstall(ctx, cfg, tag, onProgress)

	if err != nil {
		// Rollback: the staged installer leaves the last-known-good
		// binary on disk untouched — restart it so the engine stays
		// usable (best effort; the error carries the real cause).
		logging.Default().Warn("updater",
			"engine update to %s failed (%v) — restarting last-known-good engine",
			tag, err)

		// NOTE: no setState(StateStarting) here — startLocked owns its
		// state transitions and its idempotence gate refuses to boot
		// when the state already says "starting".

		if startErr := s.startLocked(); startErr != nil {
			logging.Default().Warn("updater",
				"last-known-good restart also failed: %v", startErr)
		}

		return "", fmt.Errorf("install engine %s: %w", tag, err)
	}

	// NOTE: no setState(StateStarting) — startLocked performs the full
	// transition chain itself (starting → ready) and its entry gate
	// rejects a boot when the state already claims one is in flight.

	// START EXACT INSTALLED BINARY: startLocked resolves to the path
	// the staged installer just verified (cfg.LlamaBinPath is either
	// explicitly configured or defaults into the managed bin dir).
	if startErr := s.startLocked(); startErr != nil {
		// v1.3.6 (spec §13/§14): an installed-but-unverifiable engine is
		// NEVER reported as success. The transaction is not committed:
		// the previous package is restored byte-for-byte and restarted,
		// and the update returns an ERROR.
		logging.Default().Warn("updater",
			"engine %s installed but startup verification failed (%v) — rolling back to the previous package",
			tag, startErr)

		// v1.7.0: stop/reap any candidate remnant BEFORE the rollback
		// touches the bin tree (a no-op when startLocked's own failure
		// path already killed the child — but it also cancels an armed
		// watchdog, so a delayed auto-restart can never race the
		// rollback rename with a fresh process holding Windows locks).
		s.stopCandidateForRollback("startup-verification rollback")

		if probe := variantRollbackProbe; probe != nil {
			probe()
		}

		if rbErr := staged.Rollback(); rbErr != nil {
			logging.Default().Warn("updater",
				"rollback to previous engine package failed: %v", rbErr)

			if startErr2 := s.startLocked(); startErr2 != nil {
				logging.Default().Warn("updater",
					"last-known-good restart after failed rollback also failed: %v", startErr2)
			}

			s.setState(StateFailed)

			return "", fmt.Errorf(
				"engine %s installed but startup verification failed: %v; rollback also failed: %w",
				tag, startErr, rbErr,
			)
		}

		restartErr2 := s.startLocked()
		if restartErr2 != nil {
			logging.Default().Warn("updater",
				"last-known-good restart after rollback also failed: %v", restartErr2)

			// v1.7.0: failed is honest ONLY when nothing serves — a
			// successful restart stays ready (the LKG engine owns the
			// port again and /health verified it).
			s.setState(StateFailed)
		}

		if restartErr2 != nil {
			return "", fmt.Errorf(
				"engine %s installed but startup verification failed: %v (previous package restored; last-known-good restart failed: %v)",
				tag, startErr, restartErr2,
			)
		}

		return "", fmt.Errorf(
			"engine %s installed but startup verification failed: %w (previous package restored)",
			tag, startErr,
		)
	}

	// Verified ready → only now does the transaction commit (spec §7:
	// START → HEALTH → COMMIT → CLEANUP).
	staged.Commit()

	return staged.Result().Outcome, nil
}

// StateUpdating is the explicit maintenance state (v1.3.6, spec §1):
// the engine directory is owned by the updater; no other lifecycle
// operation may touch it.
const StateUpdating = "updating"

// runEngineInstall drives the unified installer's DEFERRED-COMMIT path
// — from the test-seamed archive when set (deterministic tests),
// otherwise the real staged download. The caller decides Commit vs
// Rollback after startup verification (spec §7/§13/§14).
func (s *LlamaServer) runEngineInstall(
	ctx context.Context,
	cfg *config.Config,
	tag string,
	onProgress func(downloader.Progress),
) (*updater.StagedInstall, error) {
	return s.runEngineInstallVariant(ctx, cfg, tag, updater.VariantCPU, onProgress)
}

// runEngineInstallVariant is the backend-variant-aware install seam
// (v1.6.1: the Windows Vulkan engine package). Same deferred-commit
// contract; the manifest records the variant.
func (s *LlamaServer) runEngineInstallVariant(
	ctx context.Context,
	cfg *config.Config,
	tag string,
	variant updater.AssetVariant,
	onProgress func(downloader.Progress),
) (*updater.StagedInstall, error) {
	s.mu.Lock()
	archive := s.testStagedArchive
	s.mu.Unlock()

	if archive != "" {
		return updater.InstallStagedFromArchiveDeferredWithVariant(cfg, tag, variant, archive)
	}

	return updater.InstallStagedDeferredWithVariant(ctx, cfg, tag, variant, onProgress)
}

// variantRollbackProbe, when non-nil, is invoked by the variant
// provisioning transaction at the moment the rollback path is about to
// restore the previous package — AFTER the candidate engine has been
// stopped and reaped, BEFORE the first filesystem mutation. Regression
// coverage uses it to prove the no-live-process invariant deterministically
// (the Windows "Access is denied" rollback class happens exactly when a
// running candidate still owns the managed bin tree).
var variantRollbackProbe func()

// stopCandidateForRollback stops the just-started candidate engine and
// reaps it BEFORE a rollback may touch the managed bin tree.
//
// v1.7.0 (Windows rollback hardening): on Windows a running llama-server
// holds its executable image and DLL closure locked inside the managed
// bin dir — a Rollback() attempted under those locks fails
// ("rename ...bin.update-old ...bin: Access is denied") and leaves the
// installation half-restored. Stop() is the lifecycle-owned stop
// (SIGTERM → bounded grace → Kill → deterministic reap on exitDone): when
// it returns, the child has been observed exiting and its file handles
// are released. No sleeps, no taskkill, no polling loops — and a process
// that already exited (startLocked's failure path killed it) makes this a
// no-op, so the same call is correct on every path and every platform.
func (s *LlamaServer) stopCandidateForRollback(phase string) {
	if stopErr := s.Stop(); stopErr != nil {
		logging.Default().Warn("updater",
			"candidate engine stop before %s: %v (proceeding — reap is best-effort)", phase, stopErr)
	}

	// The deliberate candidate stop is complete and the transaction owns
	// the lifecycle again: clear the deliberate-shutdown marker so the
	// last-known-good restart is a legitimate NEW episode — the same reset
	// the pre-install stop performs before the boot attempt.
	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()
}

// UpdateEngineVariantNow is the v1.6.1 backend-variant provisioning
// transaction: stop the engine (Windows file locks), provision the
// requested backend variant through the SAME staged installer and lease
// discipline as UpdateEngineNow, restart, verify, commit — or roll the
// previous package back byte-for-byte.
//
// An explicit VULKAN request that cannot be provisioned on this platform
// fails LOUDLY with an actionable error; it NEVER silently installs the
// CPU package instead (the caller decides whether CPU is an acceptable
// fallback — AUTO does, an explicit request does not).
func (s *LlamaServer) UpdateEngineVariantNow(
	ctx context.Context,
	variant updater.AssetVariant,
	onProgress func(downloader.Progress),
) (string, error) {
	return s.updateEngineVariantTx(ctx, variant, onProgress, nil)
}

// variantVerifyFunc extends the runtime backend verification of the
// variant transaction (v1.7.2). It runs AFTER the serving health check and
// the engine's own device enumeration, and BEFORE the commit decision: an
// error rolls the transaction back through the exact same stop/reap/
// rollback/restart choreography as any other verification failure. The
// AUTO GPU candidate probe installs one to enforce the stricter
// execution-evidence contract (§4/§8).
type variantVerifyFunc func(cfg *config.Config, variant updater.AssetVariant, note string) (string, error)

// updateEngineVariantTx is the ONE transactional variant authority (the
// body of the former UpdateEngineVariantNow, v1.7.2 parameterized with an
// optional verification extension).
//
// v1.11.0 (R2/R3, run-#48, Task ID 43): THE DOWNLOAD-WINDOW RELEASE. The
// transaction previously held switchMu across its ENTIRE body — including
// the non-destructive network phases (release resolution + the archive
// download, minutes on a real network). Every user-facing engine path
// serializes on the SAME mutex — Start() (the first-send boot, the
// model-select boot) and Restart() — so the operator's shipped app sat
// with a broken model selection and dead chat for the whole probe
// download while the log showed gpuAssetResolved and no completion line
// (the run-#47 physical evidence). The v1.9.6 claim placement fixed the
// run-ADMISSION outage but not the mutex one: this repair finishes that
// contract — the lock now covers ONLY the validation and the destructive
// boundary, the download window holds NOTHING, and the destructive
// boundary re-validates what the window may have changed. The v1.9.6
// admission-claim placement, the run-active refusals and the rollback
// choreography are unchanged.
func (s *LlamaServer) updateEngineVariantTx(
	ctx context.Context,
	variant updater.AssetVariant,
	onProgress func(downloader.Progress),
	extraVerify variantVerifyFunc,
) (string, error) {
	s.switchMu.Lock()

	cfg := s.src.Load()

	lease, leaseErr := englease.Acquire(engineLeaseDir(cfg), englease.OwnerLease, "engine-owner")
	if leaseErr != nil {
		s.switchMu.Unlock()
		return "", fmt.Errorf("engine variant provisioning refused: %w", leaseErr)
	}

	defer lease.Release()

	// v1.9.4 RUN-COORDINATION — EARLY advisory refusal (no claim taken):
	// a run registered RIGHT NOW refuses the transaction before ANYTHING
	// happens — no variant validation walk, no asset resolution, no
	// download, no stop. The registry read is the same authority the
	// admission claim's live verdict uses (the API layer's run registry);
	// it is consulted read-only here so this fast path can never leak a
	// claim. v1.9.6: evaluated unconditionally (a registered run whose
	// engine has not finished booting is exactly the tearing hazard too).
	if s.runActiveNow() {
		s.switchMu.Unlock()
		logging.Default().Warn("updater",
			"engine variant transaction refused: a run is active — the serving engine is never stopped under a live generation")

		return "", ErrEngineBusyWithRun
	}

	// The gate: refuse impossible variants BEFORE stopping anything —
	// an explicit VULKAN request on an unserved platform must not
	// bounce the running engine for nothing. v1.6.2: the typed value
	// itself is validated STRICTLY first — an unknown variant string
	// that reached this layer is a caller bug, never a CPU request.
	if err := updater.ValidateAssetVariant(variant); err != nil {
		s.switchMu.Unlock()
		return "", err
	}

	if !updater.VariantSupported(variant) {
		s.switchMu.Unlock()
		return "", fmt.Errorf(
			"engine variant %q cannot be provisioned on %s — upstream llama.cpp publishes no prebuilt package for this platform; keep the CPU engine or point llamaBinPath at a self-built %s llama-server",
			variant, runtimeOS(), variant,
		)
	}

	// Already there: report the no-op honestly.
	if updater.InstalledEngineVariant(cfg) == variant {
		s.switchMu.Unlock()
		return fmt.Sprintf("engine already carries the %s backend variant", variant), nil
	}

	// v1.11.0 (R2/R3, run-#48): the NON-DESTRUCTIVE WINDOW — switchMu is
	// RELEASED across the release resolution and the archive download.
	// The engine keeps serving and every user-facing Start()/Restart()
	// proceeds while the candidate downloads; the destructive boundary
	// below re-validates under the re-acquired lock (another transaction
	// may have committed the same variant — an honest no-op — and a run
	// may have become active — the advisory refusal applies again).
	s.switchMu.Unlock()

	tag, stagedSwap, dlFail := func() (string, func() (*updater.StagedInstall, error), error) {
		// Prefer the CURRENT engine tag (same release, different backend
		// build) — the variant swap must not silently downgrade the
		// release. Resolve a fresh tag only when the current one does not
		// carry the variant asset.
		tag := updater.InstalledEngineTag(cfg)
		if tag == "" || !updater.VariantExists(ctx, tag, variant) {
			_, resolved, rerr := updater.ResolveDownloadURLForVariant(ctx, variant)
			if rerr != nil {
				return "", nil, fmt.Errorf("resolve %s engine release: %w", variant, rerr)
			}
			tag = resolved
		}

		// v1.9.6 NON-DESTRUCTIVE DOWNLOAD PHASE — claim-free by design. The
		// archive transfer (which can run for minutes on a real network)
		// never stops the engine and never touches the live bin tree, so it
		// must never hold the run-admission claim either: a user chat typed
		// during the download is ADMITTED and served by the current engine,
		// and the transaction below refuses itself at its destructive
		// boundary (the AUTO probe records that as a DEFERRAL, never a
		// bounded failure) instead of holding the product hostage with
		// "engine maintenance in progress". The v1.9.5 regression class —
		// every local-chat E2E on windows/amd64 503-ing behind the
		// background AUTO Vulkan probe's claim — was exactly this window.
		// The archive seam (tests) skips the transfer entirely.
		s.mu.Lock()
		seamArchive := s.testStagedArchive
		s.mu.Unlock()

		var stagedSwap func() (*updater.StagedInstall, error)

		if seamArchive == "" {
			archivePath, dlErr := updater.DownloadEngineArchiveStaged(ctx, cfg, tag, variant, onProgress)
			if dlErr != nil {
				// Nothing was stopped and no live tree was touched —
				// the engine keeps serving; no last-known-good restart
				// is needed (the pre-v1.9.6 order bounced a running
				// engine for a pure download failure).
				return "", nil, fmt.Errorf("install %s engine: %w", variant, dlErr)
			}

			defer func() { _ = os.Remove(archivePath) }()

			stagedSwap = func() (*updater.StagedInstall, error) {
				return updater.InstallStagedFromArchiveDeferredWithVariant(cfg, tag, variant, archivePath)
			}
		} else {
			stagedSwap = func() (*updater.StagedInstall, error) {
				return updater.InstallStagedFromArchiveDeferredWithVariant(cfg, tag, variant, seamArchive)
			}
		}

		return tag, stagedSwap, nil
	}()
	if dlFail != nil {
		// switchMu is NOT held on this path (the release above) — the
		// engine kept serving the whole window.
		return "", dlFail
	}

	// RE-ACQUIRE for the destructive boundary (stop → swap → start →
	// verify → rollback/commit). Everything below holds the SAME
	// serialization contract the pre-v1.11.0 body had.
	s.switchMu.Lock()
	defer s.switchMu.Unlock()

	// Re-validation after the download window (see the release comment):
	// the committed-elsewhere no-op and the run-active advisory.
	if updater.InstalledEngineVariant(s.src.Load()) == variant {
		return fmt.Sprintf("engine already carries the %s backend variant (installed during the download window)", variant), nil
	}
	if s.runActiveNow() {
		logging.Default().Warn("updater",
			"engine variant transaction refused: a run is active — the serving engine is never stopped under a live generation")

		return "", ErrEngineBusyWithRun
	}

	// v1.9.5/v1.9.6 ADMISSION CLAIM — AT THE DESTRUCTIVE BOUNDARY. The
	// claim is taken HERE — atomically with the live-run verdict, in the
	// same critical section the API registers runs under — and held for
	// the actually-destructive window ONLY (stop → swap → start →
	// verify → rollback/commit). The v1.9.5 placement (transaction
	// start) made the claim span the non-destructive network phases and
	// turned every background AUTO probe into a minutes-long
	// run-admission outage on Windows; the TOCTOU it closes lives
	// entirely inside the destructive window, so the narrower claim
	// closes it identically: a run either registers before the boundary
	// (the live verdict sees it; the transaction refuses — the download
	// was the only cost) or its admission observes the claim and is
	// refused deterministically (HTTP 503,
	// errEngineMutationInProgress) for the seconds the swap needs.
	// Without the seam (nil — tests, headless), the v1.9.4
	// destructive-boundary run gate alone applies.
	s.mu.Lock()
	admission := s.admission
	s.mu.Unlock()

	if admission != nil {
		if live := admission.BeginEngineMutation(); live {
			logging.Default().Warn("updater",
				"engine variant transaction refused at its destructive boundary: a run is active — the serving engine is never stopped under a live generation")

			return "", ErrEngineBusyWithRun
		}

		s.mu.Lock()
		s.mutationClaimed = true
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			s.mutationClaimed = false
			s.mu.Unlock()

			admission.EndEngineMutation()
		}()
	} else if s.runActiveNow() {
		logging.Default().Warn("updater",
			"engine variant transaction refused at its destructive boundary: a run is active — the serving engine is never stopped under a live generation")

		return "", ErrEngineBusyWithRun
	}

	// Destructive phase — the engine may be touched only now, under the
	// claim. wasRunning is re-read HERE: the download window may have
	// booted (or stopped) the engine since the transaction began.
	wasRunning := s.IsRunning()

	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()

	if wasRunning {
		s.setState(StateUpdating)

		logging.Default().Info("updater", "stopping engine for transactional %s variant provisioning (%s)", variant, tag)

		if err := s.Stop(); err != nil {
			s.mu.Lock()
			s.stopping = false
			s.mu.Unlock()

			return "", fmt.Errorf("stop engine before variant provisioning: %w", err)
		}
	} else {
		s.setState(StateUpdating)
	}

	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()

	staged, err := stagedSwap()
	if err != nil {
		logging.Default().Warn("updater",
			"%s engine provisioning failed (%v) — restarting last-known-good engine", variant, err)

		if startErr := s.startLocked(); startErr != nil {
			logging.Default().Warn("updater",
				"last-known-good restart also failed: %v", startErr)
		}

		return "", fmt.Errorf("install %s engine: %w", variant, err)
	}

	// v1.8.6 CANDIDATE-PROVING MODE: while the verification extension
	// (the AUTO GPU candidate's execution-evidence contract) runs, the
	// candidate launch may enable GPU offload from SELECTION evidence
	// (enumeration) — autoProbeVerify then REQUIRES the measured offload
	// line + a real generation before commit, so enumeration alone can
	// never produce a verified serving posture. The flag is strictly
	// transaction-scoped: the deferred reset covers EVERY exit path
	// (rollback, restart, error, commit) so a later normal launch can
	// never inherit proving semantics.
	if extraVerify != nil {
		s.mu.Lock()
		s.gpuCandidateProving = true
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			s.gpuCandidateProving = false
			s.mu.Unlock()
		}()
	}

	if startErr := s.startLocked(); startErr != nil {
		logging.Default().Warn("updater",
			"%s engine installed but startup verification failed (%v) — rolling back to the previous package",
			variant, startErr)

		// v1.7.0: stop/reap any candidate remnant BEFORE the rollback
		// touches the bin tree (same Windows-lock discipline as the
		// verification-failure path; also cancels an armed watchdog).
		s.stopCandidateForRollback("startup-verification rollback")

		if probe := variantRollbackProbe; probe != nil {
			probe()
		}

		if rbErr := staged.Rollback(); rbErr != nil {
			s.setState(StateFailed)

			return "", fmt.Errorf(
				"%s engine installed but startup verification failed: %v; rollback also failed: %w",
				variant, startErr, rbErr,
			)
		}

		restartErr2 := s.startLocked()
		if restartErr2 != nil {
			logging.Default().Warn("updater",
				"last-known-good restart after rollback also failed: %v", restartErr2)

			// v1.7.0: failed only when nothing serves (see above).
			s.setState(StateFailed)
		}

		if restartErr2 != nil {
			return "", fmt.Errorf(
				"%s engine installed but startup verification failed: %v (previous package restored; last-known-good restart failed: %v)",
				variant, startErr, restartErr2,
			)
		}

		return "", fmt.Errorf(
			"%s engine installed but startup verification failed: %w (previous package restored)",
			variant, startErr,
		)
	}

	// v1.6.2 — RUNTIME BACKEND VERIFICATION before commit: the health
	// endpoint proves the process serves; it does NOT prove which
	// backend family the package really carries. A filename, a
	// manifest field or an on-disk ggml-vulkan.dll is not runtime
	// evidence either. The engine's OWN --list-devices enumeration
	// (devices.go) is the authoritative signal — the same authority
	// the accelerator surface uses for GPU claims.
	verifyNote, verifyErr := s.verifyRuntimeBackendForVariant(cfg, variant)

	// v1.7.2: the optional verification EXTENSION (the AUTO GPU
	// candidate's execution-evidence contract) runs inside the same
	// transaction boundary — its failure takes the identical rollback
	// path, and its note is appended to the committed outcome.
	if verifyErr == nil && extraVerify != nil {
		note2, err2 := extraVerify(cfg, variant, verifyNote)
		if err2 != nil {
			verifyErr = err2
		} else if strings.TrimSpace(note2) != "" {
			verifyNote = strings.TrimSpace(verifyNote)
			if verifyNote == "" {
				verifyNote = note2
			} else {
				verifyNote = verifyNote + " — " + note2
			}
		}
	}

	if verifyErr != nil {
		logging.Default().Warn("updater",
			"%s engine installed and started but runtime backend verification failed (%v) — rolling back to the previous package",
			variant, verifyErr)

		// v1.7.0 — STOP THE CANDIDATE BEFORE THE ROLLBACK. The
		// verification ran against a RUNNING candidate engine; on
		// Windows that process owns locked executables/DLLs inside the
		// managed bin tree, and a rollback attempted under those locks
		// fails with "rename ...bin.update-old ...bin: Access is
		// denied". Lifecycle-owned stop (SIGTERM → bounded grace →
		// Kill → deterministic reap), then — and only then — the
		// byte-for-byte restore. It also cancels any watchdog the
		// candidate's death may have armed, so no delayed auto-restart
		// can race the rollback.
		s.stopCandidateForRollback("runtime-verification rollback")

		if probe := variantRollbackProbe; probe != nil {
			probe()
		}

		if rbErr := staged.Rollback(); rbErr != nil {
			// Rollback failed even with no live process: surface the
			// original verification failure PLUS the rollback
			// evidence, and still try to bring the engine back up.
			if startErr2 := s.startLocked(); startErr2 != nil {
				logging.Default().Warn("updater",
					"last-known-good restart after failed verification rollback also failed: %v", startErr2)
			}

			s.setState(StateFailed)

			return "", fmt.Errorf(
				"%s engine installed but runtime backend verification failed: %v; rollback also failed: %w",
				variant, verifyErr, rbErr,
			)
		}

		// Restore → restart last-known-good → health: startLocked owns
		// the full boot (preflight → launch → /health ready), so a nil
		// return here IS the restart-health evidence.
		restartErr := s.startLocked()
		if restartErr != nil {
			logging.Default().Warn("updater",
				"last-known-good restart after verification rollback also failed: %v", restartErr)

			// v1.7.0: failed only when nothing serves (see above).
			s.setState(StateFailed)
		}

		if restartErr != nil {
			return "", fmt.Errorf(
				"%s engine installed and started but runtime backend verification failed: %v (previous package restored; last-known-good restart failed: %v)",
				variant, verifyErr, restartErr,
			)
		}

		return "", fmt.Errorf(
			"%s engine installed and started but runtime backend verification failed: %w (previous package restored)",
			variant, verifyErr,
		)
	}

	staged.Commit()

	logging.Default().Info("updater",
		"engine %s (%s backend) verified and committed — runtime backend verification: %s",
		tag, variant, verifyNote)

	outcome := staged.Result().Outcome
	if verifyNote != "" {
		outcome = outcome + " — " + verifyNote
	}

	return outcome, nil
}

// verifyRuntimeBackendForVariant produces the RUNTIME backend evidence
// for a freshly installed engine package, from the engine itself — never
// from the asset filename, the install manifest or a backend DLL's
// presence on disk.
//
//	CPU:    the serving health check already proves the base runtime;
//	        there is no additional CPU-specific runtime signal to collect
//	        ("--list-devices" enumerates accelerators, not the CPU).
//
//	Vulkan: the engine's own device enumeration (EnumerateEngineDevices →
//	        llama-server --list-devices) is the authority. Three honest
//	        outcomes:
//	          - Vulkan device(s) enumerated → CONFIRMED runtime backend;
//	          - enumeration supported but no Vulkan device on this
//	            machine → the package is committed (it still runs on CPU;
//	            that is exactly what was requested) with an explicit
//	            note that GPU offload stays unavailable here;
//	          - enumeration not implemented by this build → committed
//	            with the backend identity honestly attributed to the
//	            install manifest (the accelerator surface keeps
//	            executionVerified evidence-gated regardless).
//
//	A verification that cannot EXECUTE (the enumeration invocation
//	itself errors) is a package-integrity failure: the caller rolls
//	back to the last-known-good package.
func (s *LlamaServer) verifyRuntimeBackendForVariant(cfg *config.Config, variant updater.AssetVariant) (string, error) {
	if variant != updater.VariantVulkan {
		return "cpu runtime verified by the serving health check", nil
	}

	bin := updater.EngineBinaryPath(cfg)

	devices, supported, err := enumerateEngineDevices(bin)
	if err != nil {
		return "", fmt.Errorf("engine backend enumeration failed: %w", err)
	}

	if !supported {
		return "this engine build does not support device enumeration — the Vulkan backend identity is recorded from the install manifest only; GPU_VULKAN execution claims stay evidence-gated", nil
	}

	vulkan := 0
	example := ""
	for _, d := range devices {
		if strings.HasPrefix(d.Backend, "Vulkan") {
			vulkan++
			if example == "" {
				example = d.Backend + ": " + d.Name
			}
		}
	}

	if vulkan == 0 {
		return "the Vulkan engine package is installed and healthy, but the engine enumerated no Vulkan device on this machine — GPU offload stays unavailable until a Vulkan-capable device/driver is present (CPU fallback continues to serve)", nil
	}

	return fmt.Sprintf("the engine enumerated %d Vulkan device(s) (e.g. %s) — the Vulkan backend is real at runtime", vulkan, example), nil
}

// runtimeOS is a tiny seam so the unsupported-variant error text stays
// testable on every platform.
func runtimeOS() string { return goOS }

// SetStagedArchiveForTest points UpdateEngineNow at a local archive
// (test-only seam; production always downloads through InstallStaged).
func (s *LlamaServer) SetStagedArchiveForTest(path string) {
	s.mu.Lock()
	s.testStagedArchive = path
	s.mu.Unlock()
}

// LoadedModel returns the absolute path of the model currently served.
func (s *LlamaServer) LoadedModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loaded
}

// setState records one state transition and publishes an EngineEvent to
// every subscriber.
func (s *LlamaServer) setState(st string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setStateLocked(st)
}

// setStateLocked performs a transition while s.mu is held. Callers that
// already hold the mutex (MarkBusy) use this to keep the aliveness check
// and the transition atomic.
func (s *LlamaServer) setStateLocked(st string) {
	previous := s.state

	if previous == st {
		return
	}

	s.state = st

	detail := s.detail
	model := ""

	if s.loaded != "" {
		model = filepath.Base(s.loaded)
	}

	ev := EngineEvent{
		State:     st,
		Previous:  previous,
		Model:     model,
		Detail:    detail,
		Timestamp: time.Now(),
	}
	// Live download progress rides along with "downloading"
	// transitions; leaving the state clears it so the UI never shows
	// stale progress.
	if st == StateDownloading {
		ev.Download = s.dlProgress.Load()
	}

	// The subscriber fan-out must never run under s.mu: a slow subscriber
	// would block the state machine. The snapshot+unlock below keeps the
	// non-blocking sends outside the state lock.
	s.subsMu.Lock()
	subs := make([]chan EngineEvent, 0, len(s.subs))
	for _, ch := range s.subs {
		subs = append(subs, ch)
	}
	s.subsMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// exitDiagnostics is the v1.2.5 evidence bundle captured when the engine
// dies unexpectedly: enough to diagnose the death without a second run.
type exitDiagnostics struct {
	exit        string
	pid         int
	tag         string
	model       string
	state       string
	stopping    bool
	visionState string
	visionMsg   string
	stderrTail  []string
}

// render formats the diagnostics block appended to the unexpected-exit log.
func (d exitDiagnostics) render(stdoutTail []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "  pid=%d tag=%s model=%s state-before=%s busy=%v stopRequested=%v",
		d.pid, orUnknown(d.tag), d.model, d.state, d.state == StateBusy, d.stopping)

	fmt.Fprintf(&b, "\n  vision=%s", orUnknown(d.visionState))

	if d.visionMsg != "" {
		fmt.Fprintf(&b, " (%s)", d.visionMsg)
	}

	if len(d.stderrTail) > 0 {
		fmt.Fprintf(&b, "\n  stderr tail:\n    %s",
			strings.Join(d.stderrTail, "\n    "))
	}

	if len(stdoutTail) > 0 {
		tail := stdoutTail

		if len(tail) > 8 {
			tail = tail[len(tail)-8:]
		}

		fmt.Fprintf(&b, "\n  stdout tail:\n    %s",
			strings.Join(tail, "\n    "))
	}

	return b.String()
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}

	return s
}

// exitDescription renders an exec error as human evidence: the exit code
// or the terminating signal (unix), never a bare guess. Windows reports
// exit codes only (see exitdetail_windows.go).
func exitDescription(err error) string {
	if err == nil {
		return "clean exit (code 0)"
	}

	var exitErr *exec.ExitError

	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return fmt.Sprintf("code %d", code)
		}

		if sig := exitSignalPart(err); sig != "" {
			return sig
		}
	}

	return err.Error()
}

// restartBudgetResetAfter is the stability window a boot must reach before
// it counts as a genuinely healthy episode for the watchdog budget. A
// crash-loop (boot → ready → die within seconds) NEVER resets the counter,
// so repeated crashes exhaust the bounded budget and surface as `failed`
// instead of recovering forever. Variable, not const: tests shrink it.
var restartBudgetResetAfter = 60 * time.Second

// beginEpisodeLocked starts a fresh alive episode: a new generation token,
// an armed-stop reset and a cleared detail. The restart budget resets ONLY
// when the previous episode was genuinely stable — a short-lived boot that
// immediately died again keeps its place on the bounded ladder. Callers
// hold s.mu and invoke this ONLY after a verified successful boot.
//
// Returns false when a deliberate Stop() raced the boot: the caller must
// NOT open a new episode and must kill the fresh child instead (the
// shutdown owns the lifecycle).
func (s *LlamaServer) beginEpisodeLocked() bool {
	if s.stopping {
		return false
	}

	s.gen++

	if s.startedAt.IsZero() || time.Since(s.startedAt) >= restartBudgetResetAfter {
		s.restarts = 0
	}

	s.stopping = false
	s.detail = ""
	s.startedAt = time.Now()

	return true
}

// abortFreshBoot kills a child that became healthy while a deliberate Stop()
// raced the boot. Deterministic: the child is reaped (bounded) and the state
// machine lands on stopped — a shutdown can never be overwritten by a boot
// that lost the race.
func (s *LlamaServer) abortFreshBoot() {
	s.mu.Lock()
	c := s.cmd
	d := s.exitDone
	s.cmd = nil
	s.exitDone = nil
	s.loaded = ""
	s.mu.Unlock()

	if c != nil && c.Process != nil {
		_ = c.Process.Kill()
	}

	if d != nil {
		select {
		case <-d:
		case <-time.After(3 * time.Second):
		}
	}

	s.setState(StateStopped)
}

// cancelWatchdog cancels any pending watchdog restart and waits (bounded)
// for its goroutine to exit. Called from Stop(): after Stop returns, no
// delayed watchdog can later call Start(). Safe to call any time.
func (s *LlamaServer) cancelWatchdog() {
	s.mu.Lock()
	stopCh, doneCh := s.watchStop, s.watchDone
	s.watchArmed = false
	s.watchStop = nil
	s.watchDone = nil
	s.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}

	if doneCh != nil {
		select {
		case <-doneCh:
		case <-time.After(2 * time.Second):
			// The watchdog is mid-Start(); the boot itself honors
			// the stopping flag at its ladder checkpoints, so no
			// engine can survive a completed shutdown because of
			// this.
		}
	}
}

// scheduleAutoRestart implements the watchdog's bounded recovery: restart
// the engine with exponential backoff (1s, 2s, 4s) up to maxAutoRestarts
// times per alive episode. When the budget is spent the engine reports
// failed and only an explicit user action may retry. Deliberate stops are
// honored: the stopping flag suppresses recovery entirely.
//
// v1.2.5 lifecycle ownership: the pending restart is an OWNED, CANCELABLE
// timer (no naked time.Sleep), armed for one specific generation; only one
// restart can be armed at a time; Stop() cancels and reaps it; a stale
// watchdog from an older episode re-validates its generation before acting
// and can never resurrect a newer lifecycle.
func (s *LlamaServer) scheduleAutoRestart(modelPath string) {
	s.mu.Lock()

	if s.stopping {
		s.mu.Unlock()
		return
	}

	if s.watchArmed {
		// Only one restart attempt can be scheduled at a time.
		s.mu.Unlock()
		return
	}

	if s.restarts >= maxAutoRestarts {
		s.detail = fmt.Sprintf(
			"Unexpected exit: engine failed %d times — automatic recovery exhausted: %s",
			s.restarts,
			s.tailLines(3),
		)

		s.mu.Unlock()

		s.setState(StateFailed)

		logging.Default().Error(
			"engine",
			"auto-restart budget exhausted; engine reported as failed",
		)

		return
	}

	s.restarts++

	attempt := s.restarts
	delay := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s, 4s
	gen := s.gen

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	s.watchArmed = true
	s.watchStop = stopCh
	s.watchDone = doneCh

	s.mu.Unlock()

	logging.Default().Warn(
		"engine",
		"engine died — automatic restart %d/%d in %v",
		attempt,
		maxAutoRestarts,
		delay,
	)

	s.SetDetail(fmt.Sprintf(
		"Unexpected exit — restarting %d/%d in %v",
		attempt, maxAutoRestarts, delay,
	))

	go func() {
		defer close(doneCh)

		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-stopCh:
			// Canceled: deliberate shutdown (or an explicit
			// teardown) while the restart was pending. No restart.
			return
		}

		s.mu.Lock()
		stale := s.stopping || s.gen != gen
		s.watchArmed = false
		s.watchStop = nil
		s.watchDone = nil
		s.mu.Unlock()

		if stale {
			// A deliberate stop happened during the backoff, or a
			// newer engine episode started: this watchdog is
			// history and must not resurrect anything.
			return
		}

		if err := s.Start(); err != nil {
			logging.Default().Error(
				"engine",
				"auto-restart %d/%d failed: %v",
				attempt,
				maxAutoRestarts,
				err,
			)

			// The next death (or this failure's terminal state) advances
			// the bounded ladder; nothing unbounded ever loops here.
			return
		}

		logging.Default().Info(
			"engine",
			"engine auto-restarted successfully (model %s)",
			filepath.Base(modelPath),
		)
	}()
}

func (s *LlamaServer) logf(
	format string,
	args ...interface{},
) {
	line := fmt.Sprintf(format, args...)
	s.logBuf.add(line)
}

// publishDownloadProgress stores the latest measured progress and fans it
// out to engine-event subscribers (throttled upstream to ~4 Hz). It runs
// on the download goroutine; fan-out is non-blocking by contract.
func (s *LlamaServer) publishDownloadProgress(p downloader.Progress) {
	snapshot := p
	s.dlProgress.Store(&snapshot)

	ev := EngineEvent{
		State:     StateDownloading,
		Timestamp: time.Now(),
		Download:  &snapshot,
	}

	s.subsMu.Lock()
	subs := make([]chan EngineEvent, 0, len(s.subs))
	for _, ch := range s.subs {
		subs = append(subs, ch)
	}
	s.subsMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// DownloadProgress returns the live engine-asset download progress, or
// nil when nothing is being downloaded.
func (s *LlamaServer) DownloadProgress() *downloader.Progress {
	return s.dlProgress.Load()
}

// CancelDownload aborts an in-flight engine provisioning operation
// (v1.3.6: the unified installer runs under a cancellable context; the
// .part file is kept so a retry can resume instead of restarting).
func (s *LlamaServer) CancelDownload() bool {
	s.mu.Lock()
	cancel := s.updateCancel
	s.mu.Unlock()

	if cancel == nil {
		return false
	}

	cancel()
	return true
}

// ErrNoModelSelected is the v1.5.0 model-first contract error: an
// engine/model load requires an EXPLICIT model selection. It is returned
// by ResolveModelPath when no model name is configured — the resolution
// NEVER falls back to an arbitrary "first .gguf in the directory" — and
// by the runtime prewarm/run gates for the same reason. Callers use
// errors.Is to distinguish "the user must choose a model" from engine
// failures.
var ErrNoModelSelected = errors.New("no model selected")

// ResolveModelPath resolves a configured model name (or path) to a real
// GGUF under modelsDir.
//
// v1.5.0 MODEL-FIRST CONTRACT: an empty name NEVER resolves. The previous
// behavior — returning the first .gguf in directory order — made a fresh
// install (Model="", LlamaAutoStart=true) silently prewarm an ARBITRARY
// model before the user had selected one. Resolution order for a real
// name: absolute path → exact (case-fold) file match → substring match →
// fuzzy token match.
func ResolveModelPath(
	modelsDir,
	name string,
) (string, error) {
	name = strings.TrimSpace(name)

	if name != "" {
		if abs, err := filepath.Abs(name); err == nil {
			if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
				return abs, nil
			}
		}
	}

	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		if name == "" {
			return "",
				fmt.Errorf(
					"%w — and the models dir %s is empty or missing "+
						"(drop a .gguf file inside it, then pick one)",
					ErrNoModelSelected,
					modelsDir,
				)
		}

		return "",
			fmt.Errorf(
				"cannot read models dir %s: %w",
				modelsDir,
				err,
			)
	}

	var files []string

	for _, e := range entries {
		if e.IsDir() ||
			!strings.HasSuffix(
				strings.ToLower(e.Name()),
				".gguf",
			) {
			continue
		}

		files = append(files, e.Name())
	}

	if name == "" {
		if len(files) == 0 {
			return "",
				fmt.Errorf(
					"%w — no .gguf files in %s yet (drop a model file inside it, then pick one)",
					ErrNoModelSelected,
					modelsDir,
				)
		}

		return "",
			fmt.Errorf(
				"%w — %d local model(s) are available in %s; pick one in the Model Selector first",
				ErrNoModelSelected,
				len(files),
				modelsDir,
			)
	}

	if len(files) == 0 {
		return "",
			fmt.Errorf(
				"no .gguf files in %s — drop a model file inside it and try again",
				modelsDir,
			)
	}

	for _, f := range files {
		if strings.EqualFold(f, name) {
			return filepath.Join(modelsDir, f), nil
		}
	}

	lower := strings.ToLower(name)

	for _, f := range files {
		if strings.Contains(
			strings.ToLower(f),
			lower,
		) {
			return filepath.Join(modelsDir, f), nil
		}
	}

	tokens := fuzzyTokens(name)

	if len(tokens) > 0 {
		for _, f := range files {
			lf := strings.ToLower(f)
			all := true

			for _, tok := range tokens {
				if !strings.Contains(lf, tok) {
					all = false
					break
				}
			}

			if all {
				return filepath.Join(modelsDir, f), nil
			}
		}
	}

	return "",
		fmt.Errorf(
			"no .gguf in %s matches %q (have: %s)",
			modelsDir,
			name,
			strings.Join(files, ", "),
		)
}

func fuzzyTokens(name string) []string {
	var toks []string

	for _, field := range strings.Fields(
		strings.ToLower(name),
	) {
		var cur strings.Builder

		for _, r := range field {
			if (r >= 'a' && r <= 'z') ||
				(r >= '0' && r <= '9') {
				cur.WriteRune(r)
			}
		}

		if cur.Len() > 0 {
			toks = append(toks, cur.String())
		}
	}

	return toks
}

func llamaBinaryName() string {
	switch runtime.GOOS {
	case "windows":
		return "llama-server.exe"
	case "darwin":
		return "llama-server"
	default:
		return "llama-server"
	}
}

func ListLocalModels(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var out []string

	for _, e := range entries {
		if e.IsDir() ||
			!strings.HasSuffix(
				strings.ToLower(e.Name()),
				".gguf",
			) {
			continue
		}

		if vision.IsMMProj(e.Name()) {
			continue
		}

		out = append(out, e.Name())
	}

	return out
}

func (s *LlamaServer) ListLoadedModels() ([]string, error) {
	if !s.IsRunning() {
		return nil,
			fmt.Errorf("llama.cpp server not running")
	}

	cfg := s.src.Load()

	url := fmt.Sprintf(
		"http://%s:%d/v1/models",
		cfg.LlamaHost,
		cfg.LlamaPort,
	)

	// v1.1.4: bounded request. The previous plain http.Get could hang an
	// HTTP handler forever when the engine stopped answering mid-poll.
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(
		resp.Body,
	).Decode(&body); err != nil {
		return nil, err
	}

	var out []string

	for _, model := range body.Data {
		out = append(out, model.ID)
	}

	return out, nil
}

func PortInUse(host string, port int) bool {
	addr := fmt.Sprintf(
		"%s:%d",
		host,
		port,
	)

	l, err := net.Listen(
		"tcp",
		addr,
	)
	if err != nil {
		return true
	}

	_ = l.Close()
	return false
}

type lineWriter struct {
	buf []byte
	cb  func(string)
}

func newLineWriter(
	cb func(string),
) *lineWriter {
	return &lineWriter{
		cb: cb,
	}
}

func (w *lineWriter) Write(
	p []byte,
) (int, error) {
	w.buf = append(w.buf, p...)

	for {
		i := -1

		for j, b := range w.buf {
			if b == '\n' {
				i = j
				break
			}
		}

		if i < 0 {
			break
		}

		line := strings.TrimRight(
			string(w.buf[:i]),
			"\r",
		)

		w.buf = w.buf[i+1:]
		w.cb(line)
	}

	return len(p), nil
}
