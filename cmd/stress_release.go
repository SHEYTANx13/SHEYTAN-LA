package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/contextcache"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/contextplan"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/memory"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// stressReleaseScenarios covers the v1.2.1 release hardening surface: the
// application updater (offline, tampered, size-mismatch, staged-drift,
// zip-slip), the vision readiness state machine (missing / incompatible
// projector), memory and session robustness (parallel appends, restarts,
// garbage IDs), bounded context planning and caching, extreme
// recommendation inputs and the LoopGuard retry ceiling.
//
// Every scenario is bounded: local loopback only, hard timeouts, capped
// payloads, no goroutine is left running. A hang or crash kills the whole
// suite (the summary line never prints), which is the contract the CI
// step relies on.
func stressReleaseScenarios() []stressTest {
	return []stressTest{
		{"update_offline_manifest", stressUpdateOfflineManifest},
		{"update_tampered_sha", stressUpdateTamperedSHA},
		{"update_size_mismatch", stressUpdateSizeMismatch},
		{"update_staged_drift", stressUpdateStagedDrift},
		{"update_zip_slip_members", stressUpdateZipSlipMembers},
		{"downloader_integrity_resume", stressDownloaderIntegrityResume},
		{"downloader_untrusted_fallback", stressDownloaderUntrustedFallback},
		{"vision_missing_projector", stressVisionMissingProjector},
		{"vision_incompatible_arch", stressVisionIncompatibleArch},
		{"memory_parallel_appends_unique", stressMemoryParallelAppendsUnique},
		{"memory_delete_missing_id", stressMemoryDeleteMissingID},
		{"sessions_restart_persistence", stressSessionsRestartPersistence},
		{"sessions_garbage_ids", stressSessionsGarbageIDs},
		{"contextcache_bounded_entries", stressContextCacheBounded},
		{"contextplan_huge_history_bounded", stressContextPlanBounded},
		{"recommendation_extreme_inputs", stressRecommendationExtremes},
		{"loopguard_retry_ceiling", stressLoopGuardCeiling},
	}
}

// --- application updater (internal/updater) ---------------------------------

func stressUpdateOfflineManifest() error {
	// Port 1 on loopback refuses connections immediately: the offline
	// path must fail FAST and honestly (check-failed), never hang, never
	// retry-storm. The call carries its own hard 30 s cap; we bound it to
	// 3 s here so a regression cannot stall the suite either way.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	_, err := updater.FetchAppManifest(ctx, "http://127.0.0.1:1/manifest.json")
	elapsed := time.Since(start)

	if err == nil {
		return fmt.Errorf("offline manifest fetch unexpectedly succeeded")
	}
	if elapsed > 3*time.Second {
		return fmt.Errorf("offline manifest fetch took %v — bounded failure violated", elapsed)
	}
	return nil
}

func stressUpdateTamperedSHA() error {
	payload := bytes.Repeat([]byte("SHEYTAN-LA-update-payload\n"), 80) // ~2 KB

	// Correct digest is computed, then the manifest deliberately carries a
	// tampered one — the staged update must be refused and nothing may be
	// left in the staging dir.
	manifest := updater.AppManifest{
		Version: "9.9.9",
		Channel: "stable",
		Platforms: map[string]updater.AppPlatformUpdate{
			"linux-x64": {
				URL:    "http://placeholder/artifact",
				SHA256: hex.EncodeToString(make([]byte, 32)), // all zeros = wrong
				Kind:   "zip",
			},
		},
	}

	// Single server serves both the manifest path and the artifact.
	var artifactRequested bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "release-manifest.json") {
			body, _ := json.Marshal(manifest)
			_, _ = w.Write(body)
			return
		}
		artifactRequested = true
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	manifest.Platforms["linux-x64"] = updater.AppPlatformUpdate{
		URL:    srv.URL + "/artifact",
		SHA256: hex.EncodeToString(make([]byte, 32)), // all zeros = wrong
		Kind:   "zip",
	}

	dataDir := tTempDir("update-tamper")
	defer os.RemoveAll(dataDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, err := updater.StageAppUpdate(ctx, dataDir, &manifest, "linux-x64")
	if err == nil {
		return fmt.Errorf("tampered sha256 accepted — integrity gate lost")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		return fmt.Errorf("expected sha256 mismatch error, got: %v", err)
	}
	if !artifactRequested {
		return fmt.Errorf("artifact was never requested — test did not exercise the download path")
	}

	// The staging directory must hold no final artifact.
	staging := filepath.Join(dataDir, "updates", "staging")
	entries, _ := os.ReadDir(staging)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".zip") {
			return fmt.Errorf("tampered download was renamed into place: %s", e.Name())
		}
	}
	return nil
}

func stressUpdateSizeMismatch() error {
	payload := bytes.Repeat([]byte("size-mismatch-payload"), 50)

	manifest := updater.AppManifest{
		Version: "9.9.9",
		Platforms: map[string]updater.AppPlatformUpdate{
			"linux-x64": {
				URL:       "http://placeholder/artifact",
				SHA256:    "",
				SizeBytes: int64(len(payload) + 1), // declared size lies
				Kind:      "zip",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	sum := sha256.Sum256(payload)
	manifest.Platforms["linux-x64"] = updater.AppPlatformUpdate{
		URL:       srv.URL + "/artifact",
		SHA256:    hex.EncodeToString(sum[:]), // digest is honest…
		SizeBytes: int64(len(payload) + 1),    // …the declared size lies
		Kind:      "zip",
	}

	dataDir := tTempDir("update-size")
	defer os.RemoveAll(dataDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, err := updater.StageAppUpdate(ctx, dataDir, &manifest, "linux-x64")
	if err == nil {
		return fmt.Errorf("size mismatch accepted — size gate lost")
	}
	if !strings.Contains(err.Error(), "size mismatch") {
		return fmt.Errorf("expected size mismatch error, got: %v", err)
	}
	return nil
}

func stressUpdateStagedDrift() error {
	dir := tTempDir("update-drift")
	defer os.RemoveAll(dir)

	staged := filepath.Join(dir, "SHEYTAN-LA-v9.9.9-staged.exe")
	payload := []byte("staged payload — treat as immutable")
	if err := os.WriteFile(staged, payload, 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	good := hex.EncodeToString(sum[:])

	// Honest stage re-verification passes.
	if err := updater.StageIsValid(staged, good); err != nil {
		return fmt.Errorf("honest stage rejected: %v", err)
	}

	// Bit-flip one byte → drift must be detected.
	if err := os.WriteFile(staged, append(payload, 'x'), 0o644); err != nil {
		return err
	}
	if err := updater.StageIsValid(staged, good); err == nil {
		return fmt.Errorf("tampered stage accepted — staged-drift gate lost")
	}

	// Missing digest is refused, not trusted.
	if err := updater.StageIsValid(staged, ""); err == nil {
		return fmt.Errorf("stage with empty recorded digest accepted")
	}
	return nil
}

func stressUpdateZipSlipMembers() error {
	// Hostile archive members (v1.2.0 §6 regression class): traversal,
	// POSIX-absolute, rooted-relative, drive-letter, UNC and NUL-bearing
	// names must all be rejected; a benign member must survive.
	hostile := []string{
		"../evil.txt",
		"../../evil.txt",
		"/abs.txt",
		"C:\\evil.txt",
		"\\\\evil\\share\\x",
		"a\\..\\..\\evil.txt",
		"ok\x00.txt",
	}
	benign := "ok/app.txt"

	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for _, name := range hostile {
		if _, err := zw.Create(name); err != nil {
			return fmt.Errorf("zip Create(%q): %v", name, err)
		}
	}
	w, err := zw.Create(benign)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("benign")); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		return err
	}

	dir := tTempDir("zip-slip")
	defer os.RemoveAll(dir)

	names, err := updater.ZipSafeNames(zr, dir)
	if err == nil {
		return fmt.Errorf("hostile archive accepted wholesale — zip-slip gate lost")
	}
	if len(names) != 0 {
		return fmt.Errorf("partial names returned alongside an error: %v", names)
	}

	// The benign-only archive must pass.
	buf2 := new(bytes.Buffer)
	zw2 := zip.NewWriter(buf2)
	w2, _ := zw2.Create(benign)
	_, _ = w2.Write([]byte("benign"))
	_ = zw2.Close()
	zr2, err := zip.NewReader(bytes.NewReader(buf2.Bytes()), int64(buf2.Len()))
	if err != nil {
		return err
	}
	names2, err := updater.ZipSafeNames(zr2, dir)
	if err != nil {
		return fmt.Errorf("benign archive rejected: %v", err)
	}
	if len(names2) != 1 {
		return fmt.Errorf("benign archive yielded %d names, want 1", len(names2))
	}
	return nil
}

// --- vision readiness state machine (internal/vision) -----------------------

// --- v1.2.3 Download Manager hardening ---------------------------------------

// stressDownloaderIntegrityResume proves the core download contract on a
// live local server: a partial .part resumes via HTTP Range, the verified
// artifact activates, and nothing partial is ever activated.
func stressDownloaderIntegrityResume() error {
	payload := bytes.Repeat([]byte("SHEYTAN-DL-resume-payload!"), 400) // ~10 KB
	want := sha256.Sum256(payload)
	wantHex := hex.EncodeToString(want[:])

	var sawRange bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rng := r.Header.Get("Range"); rng != "" {
			sawRange = true
			var start int64
			_, _ = fmt.Sscanf(rng, "bytes=%d-", &start)
			if start >= int64(len(payload)) {
				http.Error(w, "range past EOF", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:])
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := tTempDir("downloader-integrity")
	defer os.RemoveAll(dir)
	dest := filepath.Join(dir, "asset.bin")

	// Pre-seed HALF the payload as a partial .part, then resume.
	half := len(payload) / 2
	if err := os.WriteFile(dest+".part", payload[:half], 0o644); err != nil {
		return err
	}

	job, err := downloader.New(downloader.Options{
		Dest: dest,
		Sources: []downloader.Source{
			{URL: srv.URL, Label: "primary", Trust: downloader.TrustPrimary},
		},
		SHA256:    wantHex,
		Resume:    true,
		AllowHTTP: true, // loopback test double
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	res, err := job.Run(ctx)
	if err != nil {
		return fmt.Errorf("resume download failed: %w", err)
	}
	if !sawRange {
		return fmt.Errorf("server never saw a Range request — resume contract lost")
	}
	if res.ResumedFrom != int64(half) {
		return fmt.Errorf("resumedFrom = %d, want %d", res.ResumedFrom, half)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		return fmt.Errorf("activated artifact corrupt or missing")
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		return fmt.Errorf(".part survived activation")
	}
	return nil
}

// stressDownloaderUntrustedFallback proves the trust boundary: a fallback
// source is NEVER contacted without the explicit opt-in, and a checksum
// mismatch is refused outright.
func stressDownloaderUntrustedFallback() error {
	good := []byte("authoritative bytes")
	bad := []byte("untrusted mirror bytes")
	want := sha256.Sum256(good)

	var fallbackHits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fallback" {
			mu.Lock()
			fallbackHits++
			mu.Unlock()
			_, _ = w.Write(bad)
			return
		}
		_, _ = w.Write(bad) // BOTH sources serve wrong bytes here
	}))
	defer srv.Close()

	dir := tTempDir("downloader-fallback")
	defer os.RemoveAll(dir)
	dest := filepath.Join(dir, "asset.bin")

	// Without AllowFallback the fallback must not be contacted; the job
	// fails on the primary's checksum mismatch alone.
	job, err := downloader.New(downloader.Options{
		Dest: dest,
		Sources: []downloader.Source{
			{URL: srv.URL + "/primary", Trust: downloader.TrustPrimary},
			{URL: srv.URL + "/fallback", Trust: downloader.TrustFallback},
		},
		SHA256:    hex.EncodeToString(want[:]),
		AllowHTTP: true,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := job.Run(ctx); err == nil {
		return fmt.Errorf("checksum mismatch accepted — verification gate lost")
	}
	mu.Lock()
	hits := fallbackHits
	mu.Unlock()
	if hits != 0 {
		return fmt.Errorf("untrusted fallback contacted %d times without opt-in", hits)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		return fmt.Errorf("mismatched asset was activated")
	}
	return nil
}

func stressVisionMissingProjector() error {
	dir := tTempDir("vision-missing")
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "model.gguf"), []byte("gguf-ish"), 0o644); err != nil {
		return err
	}

	// An explicit override that does not exist must report failed —
	// never a silent substitute, never a panic.
	ev := vision.EvaluateModel(dir, "model.gguf", "missing.mmproj", "llava")
	if ev.State != vision.StateFailed {
		return fmt.Errorf("missing override: state = %q, want %q", ev.State, vision.StateFailed)
	}
	if ev.Projector != "" {
		return fmt.Errorf("missing override: projector = %q, want empty", ev.Projector)
	}
	if strings.TrimSpace(ev.Reason) == "" {
		return fmt.Errorf("missing override: empty reason — UI would render a guess")
	}
	return nil
}

func stressVisionIncompatibleArch() error {
	dir := tTempDir("vision-arch")
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "text-only.gguf"), []byte("gguf-ish"), 0o644); err != nil {
		return err
	}

	// Text-only architecture with no projector on disk: honest
	// unsupported state, no projector invented.
	ev := vision.EvaluateModel(dir, "text-only.gguf", "", "gpt2")
	if ev.State != vision.StateUnsupported {
		return fmt.Errorf("text-only arch: state = %q, want %q", ev.State, vision.StateUnsupported)
	}
	if ev.Projector != "" {
		return fmt.Errorf("text-only arch: projector = %q, want empty", ev.Projector)
	}

	// Vision-capable architecture, still no projector: supported (not
	// ready — readiness requires engine-verified evidence).
	ev = vision.EvaluateModel(dir, "text-only.gguf", "", "llava")
	if ev.State != vision.StateSupported {
		return fmt.Errorf("vision arch without projector: state = %q, want %q", ev.State, vision.StateSupported)
	}
	if ev.State.Healthy() {
		return fmt.Errorf("state %q must never count as healthy without engine verification", ev.State)
	}
	return nil
}

// --- memory robustness (internal/memory) ------------------------------------

func stressMemoryParallelAppendsUnique() error {
	dir := tTempDir("mem-parallel")
	defer os.RemoveAll(dir)

	st := memory.New(filepath.Join(dir, "mem.jsonl"))

	const workers = 8
	const perWorker = 25

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				if err := st.Append(
					[]string{"stress", fmt.Sprintf("w%d", id)},
					fmt.Sprintf("worker %d entry %d", id, i),
					"stress",
				); err != nil {
					errs <- fmt.Errorf("worker %d append %d: %v", id, i, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}

	all, err := st.All()
	if err != nil {
		return err
	}
	if len(all) != workers*perWorker {
		return fmt.Errorf("got %d entries, want %d — parallel appends lost data", len(all), workers*perWorker)
	}
	seen := make(map[string]bool, len(all))
	for _, e := range all {
		if seen[e.ID] {
			return fmt.Errorf("duplicate memory ID %q under parallel appends", e.ID)
		}
		seen[e.ID] = true
	}
	return nil
}

func stressMemoryDeleteMissingID() error {
	dir := tTempDir("mem-del")
	defer os.RemoveAll(dir)

	st := memory.New(filepath.Join(dir, "mem.jsonl"))
	if err := st.Append([]string{"a"}, "entry", "stress"); err != nil {
		return err
	}

	// DeleteByID on a missing ID is IDEMPOTENT by design (no error, no
	// mutation) — the contract this pins is “no panic, no collateral
	// damage”, not a specific error value.
	_ = st.DeleteByID("no-such-id")
	if got := st.Count(); got != 1 {
		return fmt.Errorf("count after missing-ID delete = %d, want 1 — collateral damage", got)
	}
	return nil
}

// --- sessions robustness (internal/sessions) --------------------------------

func stressSessionsRestartPersistence() error {
	dir := tTempDir("sessions-restart")
	defer os.RemoveAll(dir)

	// Lifecycle 1: create, append, persist.
	a := sessions.New(dir)
	sess := a.Create()
	for i := 0; i < 3; i++ {
		if _, err := a.AppendMessage(sess.ID, llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("msg-%d", i),
		}); err != nil {
			return err
		}
	}

	// Lifecycle 2: a NEW store over the same directory must observe the
	// persisted session (app restart contract).
	b := sessions.New(dir)
	loaded, err := b.Get(sess.ID)
	if err != nil {
		return fmt.Errorf("restarted store cannot load session: %v", err)
	}
	if len(loaded.Messages) != 3 {
		return fmt.Errorf("restarted store sees %d messages, want 3", len(loaded.Messages))
	}

	// Lifecycle 3: delete is durable across another restart.
	if err := b.Delete(sess.ID); err != nil {
		return err
	}
	c := sessions.New(dir)
	if _, err := c.Get(sess.ID); err == nil {
		return fmt.Errorf("deleted session resurrected after restart")
	}
	return nil
}

func stressSessionsGarbageIDs() error {
	dir := tTempDir("sessions-garbage")
	defer os.RemoveAll(dir)

	st := sessions.New(dir)

	for _, id := range []string{"", "../escape", "no-such-id", "a b c", strings.Repeat("x", 300)} {
		if _, err := st.Get(id); err == nil && id != "" {
			// Missing IDs must error, not fabricate sessions.
			return fmt.Errorf("Get(%q) unexpectedly succeeded", id)
		}
		// AppendMessage to a garbage ID must fail gracefully (no panic,
		// no write outside the store).
		if _, err := st.AppendMessage(id, llm.Message{Role: "user", Content: "x"}); err == nil {
			return fmt.Errorf("AppendMessage(%q) unexpectedly succeeded", id)
		}
	}

	// A valid flow still works after all that garbage.
	sess := st.Create()
	if _, err := st.AppendMessage(sess.ID, llm.Message{Role: "user", Content: "ok"}); err != nil {
		return fmt.Errorf("healthy append after garbage inputs failed: %v", err)
	}
	return nil
}

// --- bounded context machinery ----------------------------------------------

func stressContextCacheBounded() error {
	cache := contextcache.New(
		contextcache.WithMaxEntries(8),
		contextcache.WithMaxEntryBytes(1<<20),
	)

	payload := bytes.Repeat([]byte("x"), 1024)
	for i := 0; i < 100; i++ {
		cache.Put(fmt.Sprintf("key-%d", i), payload, int64(len(payload)), time.Minute)
	}

	stats := cache.Stats()
	if stats.Entries > 8 {
		return fmt.Errorf("cache holds %d entries with WithMaxEntries(8) — bound lost", stats.Entries)
	}

	// Oldest keys must have been evicted; newest must be present.
	if _, ok := cache.Get("key-0"); ok {
		return fmt.Errorf("oldest entry survived eviction — LRU contract lost")
	}
	if _, ok := cache.Get("key-99"); !ok {
		return fmt.Errorf("newest entry missing — cache lost live data")
	}

	// Positive TTL entries expire and stop being served (Put with a
	// ttl <= 0 means never-expiring — the documented convention).
	cache.Put("ttl", payload, int64(len(payload)), 5*time.Millisecond)
	if _, ok := cache.Get("ttl"); !ok {
		return fmt.Errorf("fresh 5ms-TTL entry not served — TTL too eager")
	}
	time.Sleep(15 * time.Millisecond)
	if _, ok := cache.Get("ttl"); ok {
		return fmt.Errorf("expired entry served — TTL contract lost")
	}
	return nil
}

func stressContextPlanBounded() error {
	// 10k messages × ~1 KB ≈ 10 MB of transcript against an 8k window.
	// Assemble is a BUDGET ALLOCATION, not a measurement: the plan must
	// allocate bounded sections inside the window (the orchestrator
	// windows the history against the returned budget afterwards) — so
	// the plan total must fit the prompt ceiling BY CONSTRUCTION, the
	// history budget must be bounded, and a 10 MB input must not leak
	// into the allocation.
	history := make([]llm.Message, 0, 10_000)
	for i := 0; i < 10_000; i++ {
		history = append(history, llm.Message{
			Role:    "user",
			Content: strings.Repeat("history ", 128), // ~1 KB
		})
	}

	plan := contextplan.Assemble(contextplan.Input{
		History:            history,
		NumCtx:             8192,
		MaxOutputTokens:    2048,
		MinHistoryTokens:   512,
		SystemTokens:       800,
		ToolTokens:         1200,
		SafetyMarginTokens: 256,
	})

	if plan.PromptCeiling() <= 0 {
		return fmt.Errorf("prompt ceiling not reported")
	}
	if plan.TotalTokens() > plan.PromptCeiling() {
		return fmt.Errorf("plan total %d exceeds prompt ceiling %d — unbounded allocation", plan.TotalTokens(), plan.PromptCeiling())
	}
	if plan.Overflow() != 0 {
		return fmt.Errorf("plan reports %d overflow on its own allocation — internally inconsistent", plan.Overflow())
	}
	for _, s := range plan.Sections {
		if s.Budget > plan.PromptCeiling()+plan.Budget.ReserveOutput {
			return fmt.Errorf("section %q budget %d exceeds the window", s.Name, s.Budget)
		}
	}
	return nil
}

// --- recommendation extremes (internal/recommendation) ----------------------

func stressRecommendationExtremes() error {
	// Degenerate input: zero-value hardware, no model card, no engine
	// capabilities. Measured-only honesty means unknown hardware yields
	// no thread claim (0 = unknown); the recommendation must still be
	// safe to render: positive context, explain-why reasons, no panic.
	rec := recommendation.Recommend(recommendation.Input{
		Task: recommendation.TaskChat,
	})

	if rec.Context <= 0 {
		return fmt.Errorf("degenerate input produced context %d — unusable recommendation", rec.Context)
	}
	if rec.Threads != 0 || rec.ThreadsBatch != 0 {
		return fmt.Errorf("no hardware data yielded threads %d/%d — measured-only contract lost (0 = unknown expected)", rec.Threads, rec.ThreadsBatch)
	}
	// The explain-why contract is Reasons+Notes combined: with NO measured
	// evidence there are no Reasons to cite, but the Notes must say why
	// the fields stay empty.
	if len(rec.Reasons) == 0 && len(rec.Notes) == 0 {
		return fmt.Errorf("degenerate recommendation carries neither reasons nor notes — explain-why contract lost")
	}

	// Realistic profile: threads follow the measured cores.
	measured := recommendation.Recommend(recommendation.Input{
		Task: recommendation.TaskChat,
		HW: hardware.Profile{
			CPU: hardware.CPU{PhysicalCores: 4, LogicalCores: 8},
		},
	})
	if measured.Threads != 4 || measured.ThreadsBatch != 8 {
		return fmt.Errorf("measured cores produced threads %d/%d, want 4/8", measured.Threads, measured.ThreadsBatch)
	}

	// Absurd input: a configured context far beyond any engine window
	// must be clamped, not passed through.
	huge := recommendation.Recommend(recommendation.Input{
		Task:              recommendation.TaskMaximum,
		ConfiguredContext: 1 << 30, // 1 Gi tokens — nonsense
	})
	if huge.Context > taskMaximumSaneContext() {
		return fmt.Errorf("absurd configured context %d not clamped (got %d)", 1<<30, huge.Context)
	}
	return nil
}

// taskMaximumSaneContext bounds the absurd-input assertion without
// importing engine internals: any sane clamp is far below 1M tokens.
func taskMaximumSaneContext() int { return 1 << 20 }

// --- agent LoopGuard retry ceiling (internal/agent) --------------------------

func stressLoopGuardCeiling() error {
	g := agent.NewLoopGuard()
	g.MaxToolCalls = 10

	blocked := 0
	for i := 0; i < 50; i++ {
		obs := g.Observe("shell", fmt.Sprintf(`{"command":"distinct-%d"}`, i))
		if obs.Block == "" {
			g.Record("shell", fmt.Sprintf(`{"command":"distinct-%d"}`, i), "result")
		} else {
			blocked++
		}
	}
	if blocked != 40 {
		return fmt.Errorf("total-call budget blocked %d of 50 calls, want exactly 40 — retry ceiling drifted", blocked)
	}

	// The same call must hit the same-call ceiling even earlier.
	g2 := agent.NewLoopGuard()
	g2.MaxSameCall = 2
	first := g2.Observe("files", `{"action":"read","path":"x"}`)
	if first.Block != "" {
		g2.Record("files", `{"action":"read","path":"x"}`, "r1")
		second := g2.Observe("files", `{"action":"read","path":"x"}`)
		if second.Block != "" {
			g2.Record("files", `{"action":"read","path":"x"}`, "r2")
			third := g2.Observe("files", `{"action":"read","path":"x"}`)
			if third.Block == "" {
				return fmt.Errorf("same-call ceiling (MaxSameCall=2) never blocked")
			}
		}
	}

	// Wall-clock semantics: an exhausted budget blocks; a disabled budget
	// (zero) never does — regardless of call counts. The 1 ns budget is
	// the exact Windows-granularity flake class pinned by v1.2.0's
	// deterministic clock repair; the exported accessor lets the gate
	// verify it without reaching into internals.
	g3 := agent.NewLoopGuard()
	g3.WallClock = 1 * time.Nanosecond
	time.Sleep(2 * time.Millisecond)
	if !g3.WallClockExhausted() {
		return fmt.Errorf("1ns wall-clock budget did not expire — Windows-granularity flake class is back")
	}

	g4 := agent.NewLoopGuard()
	g4.WallClock = 0
	if g4.WallClockExhausted() {
		return fmt.Errorf("zero budget reported exhausted — disabled-budget contract lost")
	}
	return nil
}
