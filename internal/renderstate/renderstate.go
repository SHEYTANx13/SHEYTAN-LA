// Package renderstate is the gogpu-free observation channel between the
// render service (which links the gogpu stack) and UI toolkits that
// CANNOT share a link graph with it.
//
// WHY THIS EXISTS (run 44457547289 §5.1 compatibility boundary, observed
// at execution time): go-gui v0.97.0 links github.com/ebitengine/purego
// (internal/fakecgo) and the gogpu stack links github.com/go-webgpu/goffi
// v0.6.8 (internal/fakecgo); both define the _cgo_init symbol and the
// linker refuses the combination:
//
//	link: duplicated definition of symbol _cgo_init, from
//	      github.com/go-webgpu/goffi/internal/fakecgo and
//	      github.com/ebitengine/purego/internal/fakecgo
//
// goffi v0.6.8 is the newest stable release (checked against the module
// proxy at execution time) and purego's only newer releases are
// v0.12.0-alpha prereleases, which the work order forbids. Until
// upstream resolves the fakecgo duplication, one process binary links
// EITHER go-gui's purego backend OR the gogpu stack — never both. The
// render service therefore runs in builds/processes that link gogpu
// (the core/headless binary, the Fyne-wire desktop build) and publishes
// its snapshots here; UIs that link go-gui read the latest observation
// and surface the honest unavailability reason instead of pretending.
package renderstate

import (
	"sync"
	"time"
)

// Snapshot is the render-service observation (mirrors the state model of
// internal/render; the struct lives here so UI packages never import the
// gogpu link graph).
type Snapshot struct {
	State          string    `json:"state"`
	Backend        string    `json:"backend"`
	FallbackReason string    `json:"fallbackReason,omitempty"`
	LastJobID      string    `json:"lastJobId,omitempty"`
	LastVerifiedAt time.Time `json:"lastVerifiedAt,omitempty"`
	JobsSubmitted  uint64    `json:"jobsSubmitted"`
	JobsVerified   uint64    `json:"jobsVerified"`
	JobsFailed     uint64    `json:"jobsFailed"`
	Detail         string    `json:"detail,omitempty"`
	// BuildNote carries the honest integration boundary text when the
	// observing binary cannot link the gogpu stack.
	BuildNote string `json:"buildNote,omitempty"`
}

var (
	mu        sync.RWMutex
	latest    Snapshot
	verifyFn  func() (string, error)
	verifySet bool
)

// Publish records the newest observation (called by the render service).
func Publish(s Snapshot) {
	mu.Lock()
	latest = s
	mu.Unlock()
}

// Latest returns the most recent observation (zero value when none).
func Latest() Snapshot {
	mu.RLock()
	defer mu.RUnlock()

	return latest
}

// SetVerifyFunc wires the verification-job trigger (called by the
// gogpu-linking build at startup; nil in builds that cannot link gogpu).
func SetVerifyFunc(fn func() (string, error)) {
	mu.Lock()
	verifyFn = fn
	verifySet = fn != nil
	mu.Unlock()
}

// Verify runs the wired verification job. The typed no-wire answer keeps
// the UI honest in builds that cannot link gogpu.
func Verify() (string, error) {
	mu.RLock()
	fn := verifyFn
	wired := verifySet
	mu.RUnlock()

	if !wired || fn == nil {
		return "", ErrNotWired
	}

	return fn()
}

// Wired reports whether a verification hook is wired in this build.
func Wired() bool {
	mu.RLock()
	defer mu.RUnlock()

	return verifySet
}

// ErrNotWired is the typed no-hook answer for builds that cannot link
// the gogpu stack.
const ErrNotWired = wiredError("renderstate: no verification hook in this build — the go-gui/purego and gogpu/goffi fakecgo link collision (upstream) keeps gogpu out of this binary; the render service is verified in the core build")

type wiredError string

func (e wiredError) Error() string { return string(e) }
