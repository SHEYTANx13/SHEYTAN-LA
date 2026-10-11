// Package downloader implements the reusable, safety-first download
// mechanism for every remote asset the app pulls at runtime: llama.cpp
// server archives, future model packages, mmproj/projector files and
// application/component updates.
//
// Design contract (v1.2.3):
//
//   - Native Go over net/http; no browser, PowerShell, curl or wget is
//     ever shelled out to.
//   - HTTPS only by default; plain HTTP is rejected (including across
//     redirects) unless the caller explicitly opts in for local testing.
//   - Persistent HTTP connections via a shared transport (HTTP/2 where
//     available, keep-alive idle pooling).
//   - Single-stream downloads by default; byte-range resume when the
//     server supports it. Concurrency beyond one stream is deliberately
//     not attempted against hosts that have not proven Range support.
//   - The artifact is streamed to "<dest>.part", verified (SHA-256 when
//     pinned, size when declared) and only then atomically renamed into
//     place. A partially downloaded or corrupt asset is NEVER activated.
//   - The previous working file can be preserved as "<dest>.bak" for
//     rollback before the new bytes take its place.
//   - Sources are ordered: authoritative release asset → configured or
//     verified mirror → fallback. Fallback sources are used only when
//     the caller explicitly allows them (AllowFallback); an untrusted
//     mirror is never silently substituted.
//   - The last successful source selection is cached (source-cache.json)
//     so routine startup does not probe every endpoint.
//   - Cancellation (context or Job.Cancel) stops network and file
//     activity immediately; the .part file is kept so a later Run can
//     resume. Pause/Resume are supported while the server honours Range.
//   - Transient failures (network, 5xx, 429) are retried with bounded
//     exponential backoff + jitter; permanent HTTP failures (4xx except
//     408/429) move straight to the next source instead of retrying.
//   - Progress (phase, bytes, speed, ETA, source, verification, retries,
//     error reason) is observable via Job.Progress() and OnProgress.
package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Phase is the user-visible download lifecycle the UI renders.
type Phase string

const (
	PhaseResolving   Phase = "resolving"   // choosing/validating a source
	PhaseConnecting  Phase = "connecting"  // dialling / opening the response
	PhaseDownloading Phase = "downloading" // streaming to the .part file
	PhaseVerifying   Phase = "verifying"   // hashing / size checking
	PhaseInstalling  Phase = "installing"  // atomic rename into place
	PhaseReady       Phase = "ready"       // verified asset at Dest
)

// SourceTrust classifies how much a source may be trusted.
type SourceTrust string

const (
	// TrustPrimary is the authoritative source (e.g. the official
	// GitHub release asset).
	TrustPrimary SourceTrust = "primary"
	// TrustMirror is a configured or verified mirror — used when the
	// primary fails, never silently: the source label travels with
	// every progress report and the result.
	TrustMirror SourceTrust = "mirror"
	// TrustFallback is a convenience/backup source. It is SKIPPED
	// unless Options.AllowFallback is explicitly set by the caller.
	TrustFallback SourceTrust = "fallback"
)

// Source is one candidate download location.
type Source struct {
	URL   string      `json:"url"`
	Label string      `json:"label,omitempty"`
	Trust SourceTrust `json:"trust"`
}

// Options configures one download job.
type Options struct {
	// Dest is the final absolute path of the artifact.
	Dest string
	// Sources is the ordered candidate list. At least one is required.
	Sources []Source
	// SHA256 is the expected lowercase hex digest. When set, the asset
	// is verified BEFORE activation and the job refuses mismatches.
	SHA256 string
	// SizeBytes, when > 0, is verified against the downloaded size.
	SizeBytes int64
	// MaxBytes is a hard cap protecting against hostile sources
	// (0 = DefaultMaxBytes).
	MaxBytes int64
	// FileMode for the activated file (0 = 0o644; binaries use 0o755).
	FileMode os.FileMode
	// Resume enables byte-range continuation of an existing .part.
	Resume bool
	// KeepBackup renames the previous Dest to Dest+".bak" before the
	// new asset replaces it (rollback capability).
	KeepBackup bool
	// AllowFallback permits the use of TrustFallback sources.
	AllowFallback bool
	// AllowHTTP permits plain http:// (local testing only; the default
	// is HTTPS-only, enforced across redirects too).
	AllowHTTP bool
	// CacheKey enables the source cache: the last source that produced
	// a verified download for this key is tried first on the next run
	// (entries expire after SourceCacheTTL). Empty = no caching.
	CacheKey string
	// CacheDir stores .part files, sidecar metadata and the source
	// cache ("" = directory of Dest).
	CacheDir string
	// AttemptsPerSource bounds retries per source (0 = 3).
	AttemptsPerSource int
	// OnProgress receives progress updates from the transfer goroutine.
	// It must be cheap and must not call back into the job.
	OnProgress func(Progress)
}

// DefaultMaxBytes bounds a download whose size is unknown (2 GiB).
const DefaultMaxBytes int64 = 2 << 30

// SourceCacheTTL is how long a remembered source stays preferred.
const SourceCacheTTL = 24 * time.Hour

// Result reports what a completed job actually did.
type Result struct {
	Path            string        `json:"path"`
	SHA256          string        `json:"sha256,omitempty"`
	Bytes           int64         `json:"bytes"`
	Source          Source        `json:"source"`
	Verified        bool          `json:"verified"`
	SkippedExisting bool          `json:"skippedExisting,omitempty"`
	ResumedFrom     int64         `json:"resumedFrom,omitempty"`
	KeptBackup      string        `json:"keptBackup,omitempty"`
	Duration        time.Duration `json:"-"`
	Retries         int           `json:"retries"`
}

// Errors returned by Run (errors.Is-able).
var (
	ErrCancelled    = errors.New("download cancelled")
	ErrPaused       = errors.New("download paused")
	ErrNoSource     = errors.New("no usable download source")
	ErrUnverified   = errors.New("download failed verification")
	ErrScheme       = errors.New("insecure download source rejected (https only)")
	ErrTooLarge     = errors.New("download exceeds the configured size cap")
	ErrNotResumable = errors.New("server does not support resume")
)

// Progress is an immutable snapshot of job state for the UI.
type Progress struct {
	Phase       Phase   `json:"phase"`
	SourceURL   string  `json:"sourceUrl,omitempty"`
	SourceLabel string  `json:"sourceLabel,omitempty"`
	SourceTrust string  `json:"sourceTrust,omitempty"`
	Attempt     int     `json:"attempt,omitempty"`
	BytesDone   int64   `json:"bytesDone"`
	BytesTotal  int64   `json:"bytesTotal"` // -1 when unknown
	BytesPerSec float64 `json:"bytesPerSec,omitempty"`
	ETASeconds  float64 `json:"etaSeconds,omitempty"` // -1 when unknown
	Retries     int     `json:"retries"`
	Resumable   bool    `json:"resumable,omitempty"`
	Verified    bool    `json:"verified,omitempty"`
	Paused      bool    `json:"paused,omitempty"`
	Message     string  `json:"message,omitempty"` // retry reason / error
}

// Job is one cancellable, pausable download.
type Job struct {
	opts Options

	mu        sync.Mutex
	prog      Progress
	pauseCh   chan struct{} // closed on Resume
	resumable bool          // current server honours Range

	cancel  atomic.Bool
	paused  atomic.Bool
	running atomic.Bool
	// attemptCancel cancels the CURRENT in-flight HTTP request so a
	// blocked body.Read unblocks immediately on Cancel().
	attemptCancel atomic.Value // context.CancelFunc

	done chan struct{} // closed when Run exits
}

// New validates options and returns a Job.
func New(opts Options) (*Job, error) {
	if opts.Dest == "" {
		return nil, fmt.Errorf("downloader: Dest is required")
	}
	if len(opts.Sources) == 0 {
		return nil, fmt.Errorf("downloader: at least one source is required")
	}
	for i := range opts.Sources {
		if opts.Sources[i].URL == "" {
			return nil, fmt.Errorf("downloader: source with empty URL")
		}
		if opts.Sources[i].Trust == "" {
			opts.Sources[i].Trust = TrustPrimary
		}
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.FileMode == 0 {
		opts.FileMode = 0o644
	}
	if opts.AttemptsPerSource <= 0 {
		opts.AttemptsPerSource = 3
	}
	if opts.CacheDir == "" {
		opts.CacheDir = filepath.Dir(opts.Dest)
	}
	j := &Job{
		opts:    opts,
		pauseCh: make(chan struct{}),
		done:    make(chan struct{}),
	}
	j.prog = Progress{Phase: PhaseResolving, BytesTotal: -1, ETASeconds: -1}
	return j, nil
}

// Progress returns the current snapshot.
func (j *Job) Progress() Progress {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.prog
}

// Cancel stops network + file activity immediately. The .part file is
// preserved so a later Run can resume.
func (j *Job) Cancel() {
	j.cancel.Store(true)
	if f, ok := j.attemptCancel.Load().(context.CancelFunc); ok && f != nil {
		f() // unblock an in-flight body.Read right now
	}
	j.Resume() // wake a paused job so Run can exit promptly
}

// Pause suspends the transfer (downloading phase only). Returns false
// when the job is not currently downloading or already stopping.
func (j *Job) Pause() bool {
	if j.cancel.Load() || !j.running.Load() {
		return false
	}
	j.mu.Lock()
	inPhase := j.prog.Phase == PhaseDownloading
	j.mu.Unlock()
	if !inPhase {
		return false
	}
	if j.paused.CompareAndSwap(false, true) {
		j.setProg(func(p *Progress) { p.Paused = true })
		return true
	}
	return false
}

// Resume continues a paused transfer (and wakes Run for shutdown).
func (j *Job) Resume() {
	if j.paused.CompareAndSwap(true, false) {
		j.setProg(func(p *Progress) { p.Paused = false })
	}
	j.mu.Lock()
	ch := j.pauseCh
	j.pauseCh = make(chan struct{})
	j.mu.Unlock()
	close(ch)
}

func (j *Job) waitWhilePaused(ctx context.Context) error {
	for j.paused.Load() {
		if j.cancel.Load() {
			return ErrCancelled
		}
		if err := ctx.Err(); err != nil {
			return ErrCancelled
		}
		j.mu.Lock()
		ch := j.pauseCh
		j.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ErrCancelled
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil
}

func (j *Job) setProg(fn func(*Progress)) {
	j.mu.Lock()
	fn(&j.prog)
	snap := j.prog
	j.mu.Unlock()
	if j.opts.OnProgress != nil {
		j.opts.OnProgress(snap)
	}
}

func (j *Job) note(msg string) {
	j.setProg(func(p *Progress) { p.Message = msg })
}

// Run executes the whole lifecycle: resolve → connect → download →
// verify → install → ready. Call it exactly once per Job.
func (j *Job) Run(ctx context.Context) (Result, error) {
	if !j.running.CompareAndSwap(false, true) {
		return Result{}, errors.New("downloader: job already ran")
	}
	defer close(j.done)
	started := time.Now()

	// --- existing valid artifact? --------------------------------------
	res, ok, err := j.checkExisting()
	if err != nil {
		return Result{}, err
	}
	if ok {
		j.setProg(func(p *Progress) {
			p.Phase = PhaseReady
			p.Verified = true
			p.BytesDone = res.Bytes
			p.BytesTotal = res.Bytes
			p.Message = "already present and verified"
		})
		res.Duration = time.Since(started)
		return res, nil
	}

	if err := ctx.Err(); err != nil {
		return Result{}, ErrCancelled
	}

	// Source cache: a previously verified source for this key is tried
	// first so routine startup does not probe every endpoint.
	sources := j.opts.Sources
	if j.opts.CacheKey != "" {
		sources = reorderSources(j.opts.Sources, loadSourceCache(j.opts.CacheDir, j.opts.CacheKey))
	}

	totalRetries := 0
	var lastErr error

	for _, src := range sources {
		if j.cancel.Load() {
			return Result{}, ErrCancelled
		}
		if err := ctx.Err(); err != nil {
			return Result{}, ErrCancelled
		}
		if src.Trust == TrustFallback && !j.opts.AllowFallback {
			continue // never substitute an untrusted mirror silently
		}
		if err := validateScheme(src.URL, j.opts.AllowHTTP); err != nil {
			lastErr = err
			j.note(err.Error())
			continue
		}

		res, retries, err := j.runSource(ctx, src)
		totalRetries += retries
		if err == nil {
			if j.opts.CacheKey != "" {
				recordSourceCache(j.opts.CacheDir, j.opts.CacheKey, src)
			}
			res.Duration = time.Since(started)
			res.Retries = totalRetries
			return res, nil
		}
		lastErr = err
		if errors.Is(err, ErrCancelled) || ctx.Err() != nil {
			return Result{}, ErrCancelled
		}
		// This source failed (bad bytes / permanent HTTP / exhausted
		// retries) — report why and move on to the next candidate.
		j.note(err.Error())
	}

	if lastErr == nil {
		lastErr = ErrNoSource
	}
	return Result{}, lastErr
}

// reorderSources puts a freshly-cached successful source first (when it
// is still inside the TTL and actually belongs to the list).
func reorderSources(sources []Source, hit *cachedSource) []Source {
	if hit == nil || !hit.fresh() {
		return sources
	}
	idx := -1
	for i, s := range sources {
		if s.URL == hit.URL {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return sources
	}
	out := make([]Source, 0, len(sources))
	out = append(out, sources[idx])
	out = append(out, sources[:idx]...)
	out = append(out, sources[idx+1:]...)
	return out
}

// checkExisting detects an already-present verified artifact and skips
// the download entirely.
func (j *Job) checkExisting() (Result, bool, error) {
	fi, err := os.Stat(j.opts.Dest)
	if err != nil || fi.IsDir() {
		return Result{}, false, nil // nothing usable present
	}
	if j.opts.SHA256 != "" {
		got, err := hashFile(j.opts.Dest)
		if err != nil || got != j.opts.SHA256 {
			return Result{}, false, nil // unreadable/stale — redownload
		}
		return Result{
			Path:            j.opts.Dest,
			SHA256:          got,
			Bytes:           fi.Size(),
			Source:          Source{Label: "local cache", Trust: TrustPrimary},
			Verified:        true,
			SkippedExisting: true,
		}, true, nil
	}
	if j.opts.SizeBytes > 0 && fi.Size() == j.opts.SizeBytes {
		return Result{
			Path:            j.opts.Dest,
			Bytes:           fi.Size(),
			Source:          Source{Label: "local cache", Trust: TrustPrimary},
			Verified:        true, // size-verified only
			SkippedExisting: true,
		}, true, nil
	}
	return Result{}, false, nil
}

// runSource runs the bounded retry loop for one source.
func (j *Job) runSource(ctx context.Context, src Source) (Result, int, error) {
	attempts := j.opts.AttemptsPerSource
	retries := 0
	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		if j.cancel.Load() {
			return Result{}, retries, ErrCancelled
		}
		if err := j.waitWhilePaused(ctx); err != nil {
			return Result{}, retries, err
		}

		j.setProg(func(p *Progress) {
			p.Phase = PhaseConnecting
			p.SourceURL = src.URL
			p.SourceLabel = src.Label
			p.SourceTrust = string(src.Trust)
			p.Attempt = attempt
		})

		res, err := j.attempt(ctx, src)
		if err == nil {
			return res, retries, nil
		}
		lastErr = err

		switch {
		case errors.Is(err, ErrCancelled):
			return Result{}, retries, ErrCancelled
		case errors.Is(err, ErrUnverified), errors.Is(err, ErrNotResumable):
			// The source served bad bytes or broke the Range contract:
			// do not burn retries on it.
			return Result{}, retries, err
		case isPermanentHTTPError(err):
			// 4xx (except 408/429): retrying cannot help.
			return Result{}, retries, err
		}

		if attempt < attempts && !j.cancel.Load() && ctx.Err() == nil {
			retries++
			delay := backoffDelay(attempt)
			j.setProg(func(p *Progress) {
				p.Retries = retries
				p.Message = fmt.Sprintf("retrying in %s (%v)", delay.Truncate(time.Millisecond), err)
			})
			if !sleepCancellable(ctx, delay, func() bool { return j.cancel.Load() }) {
				return Result{}, retries, ErrCancelled
			}
		}
	}
	return Result{}, retries, lastErr
}

func sleepCancellable(ctx context.Context, d time.Duration, cancelled func() bool) bool {
	deadline := time.After(d)
	tick := time.After(25 * time.Millisecond)
	for {
		select {
		case <-deadline:
			return true
		case <-ctx.Done():
			return false
		case <-tick:
			if cancelled() {
				return false
			}
			tick = time.After(25 * time.Millisecond)
		}
	}
}
