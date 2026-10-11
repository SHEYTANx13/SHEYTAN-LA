// transfer.go — one HTTP attempt of a downloader job: scheme
// enforcement, connection reuse, byte-range resume, streaming to the
// .part file with live speed/ETA accounting.
package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"sync/atomic"
	"time"
)

// sharedTransport reuses persistent connections across jobs: keep-alive
// pooling plus HTTP/2 when the host negotiates it.
var sharedTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          16,
	MaxIdleConnsPerHost:   4,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
}

// maxRedirects bounds following Location hops.
const maxRedirects = 8

// validateScheme enforces the HTTPS-only default. Plain http:// is
// rejected unless the caller explicitly allowed it (local tests).
func validateScheme(rawURL string, allowHTTP bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("downloader: invalid source URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if allowHTTP {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrScheme, rawURL)
	default:
		return fmt.Errorf("downloader: unsupported URL scheme %q", u.Scheme)
	}
}

// IsLoopbackURL reports whether the URL points at this machine
// (127.0.0.0/8, ::1, localhost). Loopback is the one place plain HTTP is
// legitimately useful — local test doubles and self-hosted staging — so
// callers may pass AllowHTTP for it while every REMOTE source stays
// HTTPS-only.
func IsLoopbackURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// httpClient builds the per-job client whose redirect policy re-applies
// the scheme rule on EVERY hop (a https → http redirect is refused).
func httpClient(allowHTTP bool) *http.Client {
	return &http.Client{
		Transport: sharedTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("downloader: stopped after %d redirects", maxRedirects)
			}
			if err := validateScheme(req.URL.String(), allowHTTP); err != nil {
				return err
			}
			// Range continuation must survive redirects intact.
			if len(via) > 0 && via[0].Header.Get("Range") != "" {
				req.Header.Set("Range", via[0].Header.Get("Range"))
			}
			return nil
		},
	}
}

// httpErr carries a non-2xx status so retry policy can classify it.
type httpErr struct {
	status int
	url    string
}

func (e *httpErr) Error() string {
	return fmt.Sprintf("HTTP %d from %s", e.status, e.url)
}

// isPermanentHTTPError reports failures where retrying the SAME source
// cannot possibly help (4xx except 408/429).
func isPermanentHTTPError(err error) bool {
	var he *httpErr
	if errors.As(err, &he) {
		switch he.status {
		case http.StatusRequestTimeout, http.StatusTooManyRequests:
			return false
		}
		return he.status >= 400 && he.status < 500
	}
	return false
}

// backoffDelay — bounded exponential backoff with jitter:
// 500ms → 1s → 2s → 4s (capped at 8s), ±25% jitter.
func backoffDelay(attempt int) time.Duration {
	base := 500 * time.Millisecond
	for i := 1; i < attempt && base < 8*time.Second; i++ {
		base *= 2
	}
	if base > 8*time.Second {
		base = 8 * time.Second
	}
	jitter := time.Duration(int64(base) / 4)
	// Cheap, dependency-free jitter: derive from the clock.
	n := int64(time.Now().UnixNano()) % int64(jitter)
	return base - jitter + time.Duration(n)
}

// speedometer — exponentially-weighted transfer speed over ~2s.
type speedometer struct {
	start     time.Time
	lastBytes int64
	lastTime  time.Time
	ewmaBytes float64
	ewmaSecs  float64
}

func newSpeedometer() *speedometer {
	now := time.Now()
	return &speedometer{start: now, lastTime: now}
}

// sample folds the latest byte count into the EWMA and returns B/s.
func (s *speedometer) sample(bytes int64) float64 {
	now := time.Now()
	dt := now.Sub(s.lastTime).Seconds()
	db := float64(bytes - s.lastBytes)
	s.lastBytes = bytes
	s.lastTime = now
	if dt <= 0 {
		if s.ewmaSecs <= 0 {
			return 0
		}
		return s.ewmaBytes / s.ewmaSecs
	}
	const alpha = 0.25
	s.ewmaSecs = s.ewmaSecs*(1-alpha) + dt*alpha
	s.ewmaBytes = s.ewmaBytes*(1-alpha) + db*alpha
	if s.ewmaSecs <= 0 {
		return 0
	}
	return s.ewmaBytes / s.ewmaSecs
}

// partPath is the in-progress file for Dest.
func (j *Job) partPath() string {
	return j.opts.Dest + ".part"
}

// attempt performs ONE full download attempt against src: open (with
// Range when resuming), stream, verify, activate atomically.
func (j *Job) attempt(ctx context.Context, src Source) (Result, error) {
	// The attempt runs under a derivable context so Cancel() can unblock
	// an in-flight body.Read immediately (network + file activity stop).
	rctx, rcancel := context.WithCancel(ctx)
	defer rcancel()
	j.attemptCancel.Store(context.CancelFunc(rcancel))
	defer j.attemptCancel.Store(context.CancelFunc(nil))

	part := j.partPath()
	if err := os.MkdirAll(filepath.Dir(j.opts.Dest), 0o755); err != nil {
		return Result{}, err
	}

	// Resume bookkeeping: how much valid data is already on disk?
	var resumeFrom int64
	if j.opts.Resume {
		if fi, err := os.Stat(part); err == nil && fi.Size() > 0 {
			// A pinned size bounds how much of the .part can be sane.
			if j.opts.SizeBytes <= 0 || fi.Size() < j.opts.SizeBytes {
				resumeFrom = fi.Size()
			} else {
				_ = os.Remove(part) // oversized .part: restart clean
			}
		}
	} else {
		_ = os.Remove(part)
	}

	req, err := http.NewRequestWithContext(rctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "SHEYTAN-Local-Agent downloader")
	req.Header.Set("Accept-Encoding", "identity") // Range + transparent gzip do not mix
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}

	client := httpClient(j.opts.AllowHTTP)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil || j.cancel.Load() {
			return Result{}, ErrCancelled
		}
		return Result{}, err
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusOK:
		if resumeFrom > 0 {
			// Server ignored Range — restart from zero honestly.
			resumeFrom = 0
		}
	case resp.StatusCode == http.StatusPartialContent && resumeFrom > 0:
		j.mu.Lock()
		j.resumable = true
		j.mu.Unlock()
	default:
		return Result{}, &httpErr{status: resp.StatusCode, url: src.URL}
	}

	// Content-Length: trust it only for identity responses.
	total := int64(-1)
	if resp.ContentLength > 0 {
		total = resp.ContentLength + resumeFrom
	}
	if j.opts.SizeBytes > 0 {
		total = j.opts.SizeBytes
	}

	j.setProg(func(p *Progress) {
		p.Phase = PhaseDownloading
		p.BytesTotal = total
		p.BytesDone = resumeFrom
		p.Resumable = resumeFrom > 0
		p.SourceURL = src.URL
		p.SourceLabel = src.Label
		p.SourceTrust = string(src.Trust)
	})

	// Open the .part for append (resume) or truncate (fresh).
	flag := os.O_CREATE | os.O_WRONLY
	if resumeFrom > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flag, 0o644)
	if err != nil {
		return Result{}, err
	}

	writeErr := j.streamToFile(rctx, f, resp.Body, resumeFrom, total)
	closeErr := f.Close()

	if writeErr != nil {
		if errors.Is(writeErr, ErrCancelled) {
			// .part stays on disk for a later resumable run.
			return Result{}, ErrCancelled
		}
		if errors.Is(writeErr, ErrPaused) {
			return Result{}, ErrPaused
		}
		return Result{}, writeErr
	}
	if closeErr != nil {
		return Result{}, closeErr
	}

	// --- verify --------------------------------------------------------
	j.setProg(func(p *Progress) { p.Phase = PhaseVerifying })
	res, err := j.verifyAndActivate(part, src, resumeFrom)
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// streamToFile copies the response body to f, honouring cancel/pause,
// the size cap, and emitting throttled progress.
func (j *Job) streamToFile(ctx context.Context, f *os.File, body io.Reader, base, total int64) error {
	speed := newSpeedometer()
	var done atomic.Int64
	done.Store(base)

	// Throttle progress callbacks to ~4 Hz.
	var lastEmit atomic.Int64
	lastEmit.Store(time.Now().UnixNano())

	buf := make([]byte, 64<<10)
	for {
		if j.cancel.Load() {
			return ErrCancelled
		}
		if err := ctx.Err(); err != nil {
			return ErrCancelled
		}
		if j.paused.Load() {
			// Flush what we have so the .part is consistent, then wait.
			if err := f.Sync(); err != nil {
				return err
			}
			if err := j.waitWhilePaused(ctx); err != nil {
				return err
			}
		}

		n, rerr := body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			cur := done.Add(int64(n))
			if j.opts.MaxBytes > 0 && cur > j.opts.MaxBytes {
				return ErrTooLarge
			}
			now := time.Now().UnixNano()
			if now-lastEmit.Load() > int64(250*time.Millisecond) {
				lastEmit.Store(now)
				bps := speed.sample(cur)
				j.setProg(func(p *Progress) {
					p.BytesDone = cur
					p.BytesPerSec = bps
					if total > 0 && bps > 0 {
						p.ETASeconds = float64(total-cur) / bps
					}
				})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil || j.cancel.Load() {
				return ErrCancelled
			}
			return rerr
		}
	}

	if err := f.Sync(); err != nil {
		return err
	}

	// Fold the final bytes into progress before verification.
	final := done.Load()
	bps := speed.sample(final)
	j.setProg(func(p *Progress) {
		p.BytesDone = final
		p.BytesPerSec = bps
		if total > 0 && bps > 0 {
			p.ETASeconds = float64(total-final) / bps
		}
	})

	if j.opts.SizeBytes > 0 && final != j.opts.SizeBytes {
		return fmt.Errorf("%w: size mismatch: got %d bytes, expected %d", ErrUnverified, final, j.opts.SizeBytes)
	}
	return nil
}

// verifyAndActivate hashes the .part, checks the pinned digest/size and
// atomically renames it to Dest (keeping a rollback backup when asked).
func (j *Job) verifyAndActivate(part string, src Source, resumedFrom int64) (Result, error) {
	got, err := hashFile(part)
	if err != nil {
		return Result{}, err
	}
	if j.opts.SHA256 != "" && got != j.opts.SHA256 {
		// Corrupt bytes: the .part is worthless — remove it so a retry
		// starts clean instead of resuming garbage.
		_ = os.Remove(part)
		return Result{}, fmt.Errorf("%w: sha256 mismatch (got %s)", ErrUnverified, got[:16])
	}

	fi, err := os.Stat(part)
	if err != nil {
		return Result{}, err
	}
	if j.opts.MaxBytes > 0 && fi.Size() > j.opts.MaxBytes {
		_ = os.Remove(part)
		return Result{}, ErrTooLarge
	}

	j.setProg(func(p *Progress) {
		p.Phase = PhaseInstalling
		p.Verified = j.opts.SHA256 == "" || got == j.opts.SHA256
	})

	// Preserve the previous working version for rollback.
	keptBackup := ""
	if j.opts.KeepBackup {
		if _, err := os.Stat(j.opts.Dest); err == nil {
			bak := j.opts.Dest + ".bak"
			_ = os.Remove(bak)
			if err := os.Rename(j.opts.Dest, bak); err == nil {
				keptBackup = bak
			}
		}
	}

	// Atomic activation: same-directory rename, then fsync the dir so
	// the entry survives a crash.
	if err := os.Rename(part, j.opts.Dest); err != nil {
		if keptBackup != "" {
			_ = os.Rename(keptBackup, j.opts.Dest) // roll back
		}
		return Result{}, err
	}
	if err := os.Chmod(j.opts.Dest, j.opts.FileMode); err != nil {
		return Result{}, err
	}
	if d, err := os.Open(filepath.Dir(j.opts.Dest)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	j.setProg(func(p *Progress) {
		p.Phase = PhaseReady
		p.Verified = true
		p.BytesDone = fi.Size()
		if p.BytesTotal <= 0 {
			p.BytesTotal = fi.Size()
		}
	})

	return Result{
		Path:        j.opts.Dest,
		SHA256:      got,
		Bytes:       fi.Size(),
		Source:      src,
		Verified:    true,
		ResumedFrom: resumedFrom,
		KeptBackup:  keptBackup,
	}, nil
}

// hashFile streams a file through SHA-256 without loading it into RAM.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
