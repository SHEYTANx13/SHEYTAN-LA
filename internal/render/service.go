// Package render is the ONE coordinated GPU rendering/compute service for
// SHEYTAN-LA (master work order for run 44457547289 §5): typed capability
// discovery, device/session ownership, workload scheduling, resource
// lifetime and renderer telemetry — integrated into the product UI
// through the go-gui PixelSurface bridge, never a second UI toolkit and
// never a standalone demo.
//
// State model (§5.3 — the binding vocabulary; detected ≠ available ≠
// selected ≠ executed ≠ execution_verified):
//
//	detected           the host probe answered (device or software path found)
//	available          a backend is usable for this session
//	selected           a backend was chosen for the workload
//	initialized        the device/session was created and is owned here
//	executed           a real workload was submitted and completed
//	execution_verified the completed output's checksum matched the
//	                   reference expectation
//
// A state may never skip ahead: GPU execution is only ever reported
// after a real job was submitted, completed and verified (checksum
// readback); on unsupported, preview or unverified platforms the service
// falls back to the CPU path or the toolkit's native renderer and
// surfaces the reason. No utilization is ever derived from model
// selection or device names.
package render

import (
	"errors"
	"sync"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/renderstate"
)

// State is the lifecycle position of the render service's device path.
type State string

const (
	StateUndetected        State = "undetected"
	StateDetected          State = "detected"
	StateAvailable         State = "available"
	StateSelected          State = "selected"
	StateInitialized       State = "initialized"
	StateExecuted          State = "executed"
	StateExecutionVerified State = "execution_verified"
	StateFallback          State = "fallback"
)

// Backend names the execution engine a workload actually ran on.
type Backend string

const (
	// BackendGoGPUSoftware is gogpu's pure-Go software renderer: a real
	// submission/compute path of the GPU stack that needs no display and
	// no native GPU adapter (verified executable in headless CI).
	BackendGoGPUSoftware Backend = "gogpu-software"

	// BackendCPU is the pure-Go reference path used when the gogpu stack
	// cannot initialize on this host; every result it produces is
	// labeled as CPU, never as GPU execution.
	BackendCPU Backend = "cpu"
)

// Typed failures (fail-closed; the UI renders the reason, never a fake
// success).
var (
	// ErrUnsupportedPlatform: no backend of the gogpu stack can run on
	// this platform/build (per the wgpu platform matrix: iOS Metal
	// untested, Android arm64 Vulkan preview, some macOS Vulkan paths
	// unverified).
	ErrUnsupportedPlatform = errors.New("render: no verified backend for this platform")

	// ErrRendererUnavailable: the gogpu stack is linked but its renderer
	// could not be created on this host (missing loader, driver init
	// failure).
	ErrRendererUnavailable = errors.New("render: renderer creation failed")

	// ErrVerificationFailed: the job ran but the output checksum did not
	// match the reference — a real, surfaced defect, never masked.
	ErrVerificationFailed = errors.New("render: output checksum verification failed")

	// ErrJobNotFound: a telemetry request named an unknown job.
	ErrJobNotFound = errors.New("render: job not found")
)

// Snapshot is the immutable observation surface the UI reads (Engine
// Center status card, Diagnostics evidence). It never carries pixel data.
// The type is an alias of renderstate.Snapshot so every UI toolkit can
// observe the service without importing the gogpu link graph (the
// purego/goffi fakecgo collision keeps them out of one binary — see the
// renderstate package documentation).
type Snapshot = renderstate.Snapshot

// service is the process-wide render authority. Ownership: one Service
// owns the device/session for the whole app; go-gui keeps its own native
// renderer for the shell and the two never fight over a device context
// (the bridge path publishes pixels through the toolkit's PixelSurface
// instead of sharing a swapchain).
type service struct {
	mu sync.Mutex

	state          State
	backend        Backend
	fallbackReason string

	jobsSubmitted uint64
	jobsVerified  uint64
	jobsFailed    uint64

	lastJobID      string
	lastVerifiedAt time.Time

	// probeErr carries the typed discovery result for honest reporting.
	probeErr error
}

var (
	defaultMu sync.Mutex
	defaultS  *service
)

// Default returns the process-wide render service (lazy; discovery is
// explicit via Discover so startup stays off the first-frame path).
func Default() *service {
	defaultMu.Lock()
	defer defaultMu.Unlock()

	if defaultS == nil {
		defaultS = &service{state: StateUndetected}
	}

	return defaultS
}

// resetForTest swaps the process-wide instance (tests only).
func resetForTest() {
	defaultMu.Lock()
	defer defaultMu.Unlock()

	defaultS = &service{state: StateUndetected}
}

// Discover probes the host WITHOUT creating a device: it records whether
// the gogpu software path is linked and usable, and the typed reason when
// it is not. It never panics, never blocks on a GPU, and never reports a
// device that was not actually initialized.
func (s *service) Discover() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := probeSoftwareRenderer()
	if err != nil {
		s.state = StateFallback
		s.backend = BackendCPU
		s.fallbackReason = err.Error()
		s.probeErr = err

		return s.snapshotLocked()
	}

	s.state = StateAvailable
	s.backend = BackendGoGPUSoftware
	s.fallbackReason = ""
	s.probeErr = nil

	return s.snapshotLocked()
}

// snapshotLocked builds the observation (caller holds mu) and publishes
// it to the toolkit-neutral channel.
func (s *service) snapshotLocked() Snapshot {
	snap := Snapshot{
		State:          string(s.state),
		Backend:        string(s.backend),
		FallbackReason: s.fallbackReason,
		LastJobID:      s.lastJobID,
		LastVerifiedAt: s.lastVerifiedAt,
		JobsSubmitted:  s.jobsSubmitted,
		JobsVerified:   s.jobsVerified,
		JobsFailed:     s.jobsFailed,
	}

	renderstate.Publish(snap)

	return snap
}
