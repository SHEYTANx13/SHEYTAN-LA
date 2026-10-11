// downloader_test.go — behavioural tests for the reusable Download
// Manager: happy path, verification, resume, cancellation, pause,
// mirror failover, trust boundaries, retry classification, the source
// cache and rollback preservation.
package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- 1. happy path: download → verify → activate -------------------------

func TestRunDownloadsVerifiesActivates(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("SHEYTAN downloader happy path payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, err := New(Options{
		Dest:       dest,
		Sources:    []Source{{URL: srv.URL, Label: "primary", Trust: TrustPrimary}},
		SHA256:     digest(payload),
		SizeBytes:  int64(len(payload)),
		AllowHTTP:  true,
		OnProgress: func(Progress) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Path != dest || res.Bytes != int64(len(payload)) || !res.Verified {
		t.Fatalf("bad result: %+v", res)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(payload) {
		t.Fatal("activated file content mismatch")
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal(".part must not survive activation")
	}
	p := job.Progress()
	if p.Phase != PhaseReady || !p.Verified {
		t.Fatalf("final progress: %+v", p)
	}
}

// --- 2. existing verified file → skip the network ------------------------

func TestRunSkipsExistingVerifiedFile(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("already here")
	dest := filepath.Join(dir, "asset.bin")
	writeFile(t, dest, payload)

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(payload)
	}))
	defer srv.Close()

	job, _ := New(Options{
		Dest:      dest,
		Sources:   []Source{{URL: srv.URL}},
		SHA256:    digest(payload),
		AllowHTTP: true,
	})
	res, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.SkippedExisting || requests != 0 {
		t.Fatalf("expected skip with zero requests: %+v requests=%d", res, requests)
	}
}

// --- 3. checksum mismatch → refused, never activated ---------------------

func TestRunRefusesChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("corrupt bytes on the wire"))
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{
		Dest:      dest,
		Sources:   []Source{{URL: srv.URL}},
		SHA256:    digest([]byte("expected payload")),
		AllowHTTP: true,
	})
	_, err := job.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("want sha256 mismatch, got %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("corrupt asset must never be activated")
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal("bad .part must be removed after verification failure")
	}
}

// --- 4. verification failure on one source → failover to the next -------

func TestRunFailsOverToMirrorOnBadChecksum(t *testing.T) {
	dir := t.TempDir()
	good := []byte("good mirror bytes")
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bad primary bytes"))
	}))
	defer bad.Close()
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(good)
	}))
	defer goodSrv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{
		Dest: dest,
		Sources: []Source{
			{URL: bad.URL, Label: "primary"},
			{URL: goodSrv.URL, Label: "mirror", Trust: TrustMirror},
		},
		SHA256:    digest(good),
		AllowHTTP: true,
	})
	res, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Source.Label != "mirror" {
		t.Fatalf("expected mirror to win, got %+v", res.Source)
	}
}

// --- 5. resume: partial .part continues via Range -------------------------

func TestRunResumesPartialDownload(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(strings.Repeat("abcdefgh", 4096)) // 32 KiB
	half := int64(len(payload) / 2)

	var sawRange atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rng := r.Header.Get("Range"); rng != "" {
			sawRange.Store(true)
			var start int64
			fmt.Sscanf(rng, "bytes=%d-", &start)
			if start >= int64(len(payload)) {
				http.Error(w, "range past EOF", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
			w.Write(payload[start:])
			return
		}
		w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	writeFile(t, dest+".part", payload[:half]) // pre-seed a partial file

	job, _ := New(Options{
		Dest:      dest,
		Sources:   []Source{{URL: srv.URL}},
		SHA256:    digest(payload),
		Resume:    true,
		AllowHTTP: true,
	})
	res, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sawRange.Load() || res.ResumedFrom != half {
		t.Fatalf("expected Range resume from %d: sawRange=%v res=%+v", half, sawRange.Load(), res)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(payload) {
		t.Fatal("resumed file corrupted")
	}
}

// --- 6. cancellation stops network + file activity immediately ------------

func TestCancelStopsImmediately(t *testing.T) {
	dir := t.TempDir()
	release := make(chan struct{})
	var served atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Store(true)
		w.Header().Set("Content-Length", "1000000")
		flusher := w.(http.Flusher)
		flusher.Flush()
		io.CopyN(w, strings.NewReader(strings.Repeat("x", 4096)), 4096)
		<-release // hold the connection open
	}))
	defer srv.Close()
	defer close(release)

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{Dest: dest, Sources: []Source{{URL: srv.URL}}, AllowHTTP: true})

	done := make(chan error, 1)
	go func() {
		_, err := job.Run(context.Background())
		done <- err
	}()

	// Wait until bytes are flowing, then cancel.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job.Progress().BytesDone > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	job.Cancel()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("want cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return promptly after Cancel")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("cancelled download must not be activated")
	}
}

// --- 7. pause / resume ------------------------------------------------------

func TestPauseAndResume(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(strings.Repeat("m", 1_000_000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		for i := 0; i < len(payload); i += 10_000 {
			end := i + 10_000
			if end > len(payload) {
				end = len(payload)
			}
			w.Write(payload[i:end])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(6 * time.Millisecond)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{
		Dest:      dest,
		Sources:   []Source{{URL: srv.URL}},
		SizeBytes: int64(len(payload)),
		Resume:    true,
		AllowHTTP: true,
	})

	type runRes struct {
		res Result
		err error
	}
	done := make(chan runRes, 1)
	go func() {
		res, err := job.Run(context.Background())
		done <- runRes{res, err}
	}()

	// Pause once bytes are flowing (the transfer takes ~600ms total).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job.Progress().Phase == PhaseDownloading && job.Progress().BytesDone > 30_000 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !job.Pause() {
		t.Fatal("Pause refused while downloading")
	}
	time.Sleep(150 * time.Millisecond)
	pausedAt := job.Progress().BytesDone
	if !job.Progress().Paused {
		t.Fatal("progress must report paused")
	}
	time.Sleep(100 * time.Millisecond)
	if job.Progress().BytesDone != pausedAt {
		t.Fatal("bytes must not advance while paused")
	}
	job.Resume()

	rr := <-done
	if rr.err != nil {
		t.Fatalf("Run: %v", rr.err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(payload) {
		t.Fatal("paused/resumed file corrupted")
	}
}

// --- 8. fallback sources are skipped unless explicitly allowed -------------

func TestFallbackSourceRequiresExplicitTrust(t *testing.T) {
	dir := t.TempDir()
	fallbackHits := 0
	var mu sync.Mutex
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fallbackHits++
		mu.Unlock()
		w.Write([]byte("fallback bytes"))
	}))
	defer fallback.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary bytes"))
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{
		Dest:      dest,
		Sources:   []Source{{URL: srv.URL}, {URL: fallback.URL, Trust: TrustFallback}},
		AllowHTTP: true,
	})
	if _, err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	hits := fallbackHits
	mu.Unlock()
	if hits != 0 {
		t.Fatalf("fallback must be skipped without AllowFallback, hits=%d", hits)
	}
}

// --- 9. HTTPS-only enforcement ---------------------------------------------

func TestHTTPSOnlyByDefault(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("plain HTTP source must never be contacted")
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{Dest: dest, Sources: []Source{{URL: srv.URL}}})
	_, err := job.Run(context.Background())
	if !errors.Is(err, ErrScheme) {
		t.Fatalf("want scheme rejection, got %v", err)
	}
}

func TestRedirectToHTTPRefused(t *testing.T) {
	// Redirect policy unit test without trusting a TLS server: drive
	// CheckRedirect directly.
	client := httpClient(false)
	first := &http.Request{Header: http.Header{}, URL: mustParse("https://example.com/asset")}
	first.Header.Set("Range", "bytes=5-")
	hop := &http.Request{Header: http.Header{}, URL: mustParse("http://insecure.test/asset")}
	hop.Header.Set("Range", "")
	err := client.CheckRedirect(hop, []*http.Request{first})
	if err == nil {
		t.Fatal("https → http redirect must be refused")
	}
	// Range header must be preserved across same-scheme redirects.
	hop2 := &http.Request{Header: http.Header{}, URL: mustParse("https://mirror.test/asset")}
	if err := client.CheckRedirect(hop2, []*http.Request{first}); err != nil {
		t.Fatalf("https → https redirect must pass: %v", err)
	}
	if hop2.Header.Get("Range") != "bytes=5-" {
		t.Fatal("Range header must survive redirects for resume to work")
	}
}

func mustParse(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

// --- 10. retry classification -----------------------------------------------

func TestPermanentHTTPFailureNotRetried(t *testing.T) {
	dir := t.TempDir()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{
		Dest: dest, Sources: []Source{{URL: srv.URL}},
		AttemptsPerSource: 5, AllowHTTP: true,
	})
	_, err := job.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want HTTP 404 error, got %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("404 must not be retried, hits=%d", hits.Load())
	}
}

func TestTransient5xxRetriedThenSuccess(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("eventually consistent")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 2 {
			http.Error(w, "flaky", http.StatusInternalServerError)
			return
		}
		w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{
		Dest: dest, Sources: []Source{{URL: srv.URL}},
		SHA256: digest(payload), AllowHTTP: true,
	})
	res, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Retries != 2 || hits.Load() != 3 {
		t.Fatalf("expected 2 retries across 3 attempts: retries=%d hits=%d", res.Retries, hits.Load())
	}
}

// --- 11. source cache: the known-good source is tried first -----------------

func TestSourceCacheReordersOnSecondRun(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("cached source payload")

	var primaryHits, mirrorHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits.Add(1)
		http.Error(w, "primary is down", http.StatusBadGateway)
	}))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		w.Write(payload)
	}))
	defer mirror.Close()

	dest := filepath.Join(dir, "asset.bin")
	opts := Options{
		Dest: dest,
		Sources: []Source{
			{URL: primary.URL, Label: "primary"},
			{URL: mirror.URL, Label: "mirror", Trust: TrustMirror},
		},
		SHA256:    digest(payload),
		CacheKey:  "test-engine",
		CacheDir:  dir,
		AllowHTTP: true,
	}

	job1, _ := New(opts)
	if _, err := job1.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mirrorHits.Load() != 1 {
		t.Fatal("mirror should have served run 1")
	}
	afterRun1 := primaryHits.Load() // run 1 retried the dead primary

	// Remove the verified artifact so run 2 actually downloads again
	// (otherwise checkExisting skips the network entirely).
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}

	job2, _ := New(opts)
	if _, err := job2.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Run 2 must go straight to the cached mirror; the flaky primary
	// must NOT be probed again.
	if primaryHits.Load() != afterRun1 {
		t.Fatalf("cached source must be preferred: primary hits run1=%d run2=%d",
			afterRun1, primaryHits.Load())
	}
	if mirrorHits.Load() != 2 {
		t.Fatalf("mirror should have served run 2: hits=%d", mirrorHits.Load())
	}
}

// --- 12. rollback backup preserved on replacement ---------------------------

func TestKeepBackupPreservesPreviousVersion(t *testing.T) {
	dir := t.TempDir()
	old := []byte("previous working version")
	new := []byte("new verified version")
	dest := filepath.Join(dir, "asset.bin")
	writeFile(t, dest, old)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(new)
	}))
	defer srv.Close()

	job, _ := New(Options{
		Dest: dest, Sources: []Source{{URL: srv.URL}},
		SHA256: digest(new), KeepBackup: true, AllowHTTP: true,
	})
	res, err := job.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(dest + ".bak")
	if err != nil || string(bak) != string(old) {
		t.Fatalf("rollback backup must hold the previous file: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(new) || res.KeptBackup == "" {
		t.Fatal("new version must be activated")
	}
}

// --- 13. progress reports speed, ETA, source and phases ----------------------

func TestProgressReportsSpeedAndPhases(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(strings.Repeat("p", 300_000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		for i := 0; i < len(payload); i += 15_000 {
			end := i + 15_000
			if end > len(payload) {
				end = len(payload)
			}
			w.Write(payload[i:end])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}))
	defer srv.Close()

	var sawDownloading, sawSpeed bool
	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{
		Dest:      dest,
		Sources:   []Source{{URL: srv.URL, Label: "primary", Trust: TrustPrimary}},
		AllowHTTP: true,
		OnProgress: func(p Progress) {
			if p.Phase == PhaseDownloading && p.BytesDone > 0 {
				sawDownloading = true
			}
			if p.BytesPerSec > 0 && p.BytesTotal == int64(len(payload)) {
				sawSpeed = true
			}
		},
	})
	if _, err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !sawDownloading || !sawSpeed {
		t.Fatalf("progress incomplete: downloading=%v speed=%v", sawDownloading, sawSpeed)
	}
}

// --- 14. oversize cap ---------------------------------------------------------

func TestOversizeRefused(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("z", 10_000)))
	}))
	defer srv.Close()

	dest := filepath.Join(dir, "asset.bin")
	job, _ := New(Options{
		Dest: dest, Sources: []Source{{URL: srv.URL}},
		MaxBytes: 1_000, AllowHTTP: true,
	})
	_, err := job.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want size-cap rejection, got %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("oversized asset must never be activated")
	}
}
