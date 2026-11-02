package api

// engine_update_test.go — v1.11.1: the official llama.cpp engine
// update-check / user-triggered update surface. The contract under test:
//
//   - CHECK is read-only: current / update-available are resolved from
//     the ONE variant-aware resolver against the official release list
//     (exact asset names, never synthesized URLs);
//   - OFFLINE is its own honest class (never "no asset", never "current");
//   - NO-ASSET only follows a reachable release list without a consumable
//     asset (a network failure is the ERROR class);
//   - INSTALL refuses while a run is active or a calibration is in
//     progress (the same gates as variant provisioning);
//   - INSTALL of the CURRENT tag is a NO-OP — the engine is never
//     restarted for nothing;
//   - the single-flight slot rejects a second concurrent install.
//
// The full stop → stage → verify → restart → commit/rollback transaction
// is the engine-owned UpdateEngineNow flow pinned by the existing
// updater/llm transaction suites; this surface only wires it.
//
// HOST-AGNOSTIC fixtures: the release-list entries carry the asset names
// AssetNameForVariant derives for THIS host (and deliberately foreign
// names for the miss cases) — no SetPlatformForTest override here: that
// seam writes package-global platform vars, which races the AUTO Vulkan
// probe goroutines earlier api tests leave behind under -race.
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/netcheck"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/updater"
)

// newEngineUpdateTestServer builds a server + test client with a
// deterministic official release list. matchingAsset is the exact CPU
// asset name for THIS host at the given tag.
func newEngineUpdateTestServer(t *testing.T, installedTag string, entries []updater.ReleaseListEntryForTest) (*httptest.Server, *Server) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	t.Cleanup(srv.Close)

	if installedTag != "" {
		updater.RecordEngineTag(cfg, installedTag)
		srv.src.Store(cfg)
	}

	restoreList := updater.SetReleaseListEntriesForTest(entries)
	t.Cleanup(restoreList)

	netcheck.SetProbe(func() bool { return true }) // online
	t.Cleanup(func() { netcheck.SetProbe(nil) })

	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)
	return server, srv
}

// hostCPUAsset is the exact CPU asset name this host's naming layer
// derives for the tag (the host-agnostic fixture anchor).
func hostCPUAsset(t *testing.T, tag string) string {
	t.Helper()
	name := updater.AssetNameForVariant(tag, updater.VariantCPU)
	if name == "" {
		t.Skipf("host platform derives no CPU asset name at %s — the host-agnostic fixture cannot run", tag)
	}
	return name
}

func getEngineUpdateCheck(t *testing.T, server *httptest.Server) engineUpdateCheck {
	t.Helper()
	resp, err := http.Get(server.URL + "/api/engine/update/check")
	if err != nil {
		t.Fatalf("GET /api/engine/update/check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/engine/update/check = %d", resp.StatusCode)
	}
	var st engineUpdateCheck
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("decode check: %v", err)
	}
	return st
}

func postEngineUpdate(t *testing.T, server *httptest.Server, body string) (*http.Response, engineUpdateCheck) {
	t.Helper()
	resp, err := http.Post(server.URL+"/api/engine/update", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/engine/update: %v", err)
	}
	var st engineUpdateCheck
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			t.Fatalf("decode install response: %v", err)
		}
	}
	return resp, st
}

func TestEngineUpdateCheckCurrentUsesExactAssetResolution(t *testing.T) {
	server, _ := newEngineUpdateTestServer(t, "b12032", []updater.ReleaseListEntryForTest{
		{Tag: "b12032", Assets: []string{hostCPUAsset(t, "b12032")}},
		{Tag: "b12031", Assets: []string{hostCPUAsset(t, "b12031")}},
	})

	st := getEngineUpdateCheck(t, server)
	if st.Status != "current" {
		t.Fatalf("status = %q (message %q), want current", st.Status, st.Message)
	}
	if st.InstalledTag != "b12032" || st.LatestTag != "b12032" {
		t.Fatalf("installed=%q latest=%q, want b12032/b12032", st.InstalledTag, st.LatestTag)
	}
	if st.AssetName != hostCPUAsset(t, "b12032") {
		t.Fatalf("asset name = %q — the check must resolve the EXACT official asset name (%q)", st.AssetName, hostCPUAsset(t, "b12032"))
	}
	if st.Source != "ggml-org/llama.cpp" {
		t.Fatalf("source = %q, want the canonical official project", st.Source)
	}
	if st.Platform == "" {
		t.Fatal("the check must report the platform (goPlatform)")
	}
	if st.CheckedAt == "" {
		t.Fatal("the check must carry its UTC timestamp")
	}
}

func TestEngineUpdateCheckUpdateAvailable(t *testing.T) {
	server, _ := newEngineUpdateTestServer(t, "b12031", []updater.ReleaseListEntryForTest{
		{Tag: "b12032", Assets: []string{hostCPUAsset(t, "b12032")}},
		{Tag: "b12031", Assets: []string{hostCPUAsset(t, "b12031")}},
	})

	st := getEngineUpdateCheck(t, server)
	if st.Status != "update-available" {
		t.Fatalf("status = %q (message %q), want update-available", st.Status, st.Message)
	}
	if st.LatestTag != "b12032" {
		t.Fatalf("latest = %q, want b12032 (newest release carrying the exact variant asset)", st.LatestTag)
	}
	if !strings.Contains(st.Message, "b12031 → b12032") {
		t.Fatalf("the message must state installed → latest honestly, got %q", st.Message)
	}
}

func TestEngineUpdateCheckSkipsReleasesWithoutExactAsset(t *testing.T) {
	// b12033 exists but carries a FOREIGN asset name (an s390x tar.gz) —
	// the resolution must skip it, never synthesize its URL.
	server, _ := newEngineUpdateTestServer(t, "b12031", []updater.ReleaseListEntryForTest{
		{Tag: "b12033", Assets: []string{"llama-b12033-bin-ubuntu-s390x.tar.gz"}},
		{Tag: "b12032", Assets: []string{hostCPUAsset(t, "b12032")}},
	})

	st := getEngineUpdateCheck(t, server)
	if st.Status != "update-available" || st.LatestTag != "b12032" {
		t.Fatalf("status=%q latest=%q — the newest release WITHOUT the exact asset must be skipped", st.Status, st.LatestTag)
	}
}

func TestEngineUpdateCheckOfflineIsItsOwnHonestClass(t *testing.T) {
	server, _ := newEngineUpdateTestServer(t, "b12031", nil)
	netcheck.SetProbe(func() bool { return false })
	t.Cleanup(func() { netcheck.SetProbe(func() bool { return true }) })

	st := getEngineUpdateCheck(t, server)
	if st.Status != "offline" {
		t.Fatalf("status = %q, want offline (never 'no asset', never a fabricated 'current')", st.Status)
	}
	if st.LatestTag != "" {
		t.Fatalf("an offline check must not claim a latest tag, got %q", st.LatestTag)
	}
}

func TestEngineUpdateCheckNoAssetOnlyAfterReachableList(t *testing.T) {
	// A REACHABLE release list whose only assets are FOREIGN (an s390x
	// tar.gz matches no host's consumable asset): the honest no-asset
	// class — host-agnostic, on every gate platform.
	server, _ := newEngineUpdateTestServer(t, "", []updater.ReleaseListEntryForTest{
		{Tag: "b12032", Assets: []string{"llama-b12032-bin-ubuntu-s390x.tar.gz"}},
	})

	st := getEngineUpdateCheck(t, server)
	if st.Status != "no-asset" {
		t.Fatalf("status = %q (message %q), want no-asset (the honest class after a successful lookup)", st.Status, st.Message)
	}

	// A NETWORK failure is the ERROR class — never "no asset".
	restoreErr := updater.SetReleaseListErrorForTest(context.DeadlineExceeded)
	t.Cleanup(restoreErr)

	st2 := getEngineUpdateCheck(t, server)
	if st2.Status != "error" {
		t.Fatalf("status = %q, want error — a network failure must never be reported as no-asset", st2.Status)
	}
}

func TestEngineUpdateInstallGates(t *testing.T) {
	server, srv := newEngineUpdateTestServer(t, "b12031", []updater.ReleaseListEntryForTest{
		{Tag: "b12032", Assets: []string{hostCPUAsset(t, "b12032")}},
	})

	// A run is active → 409, nothing starts.
	srv.runsMu.Lock()
	srv.runs["session-live"] = &runState{
		cancel: func() {},
		hub:    newActivityHub(),
	}
	srv.runsMu.Unlock()
	if !srv.anyRunActive() {
		t.Fatal("fixture failed to register a run")
	}
	resp, _ := postEngineUpdate(t, server, `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("run-active gate = %d, want 409", resp.StatusCode)
	}

	// Calibration in progress → 409.
	srv.runsMu.Lock()
	delete(srv.runs, "session-live")
	srv.runsMu.Unlock()
	srv.calibrating.Store(true)
	t.Cleanup(func() { srv.calibrating.Store(false) })
	resp2, _ := postEngineUpdate(t, server, `{}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("calibration gate = %d, want 409", resp2.StatusCode)
	}
}

func TestEngineUpdateInstallCurrentTagIsNoOp(t *testing.T) {
	server, srv := newEngineUpdateTestServer(t, "b12032", []updater.ReleaseListEntryForTest{
		{Tag: "b12032", Assets: []string{hostCPUAsset(t, "b12032")}},
	})

	resp, st := postEngineUpdate(t, server, `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("no-op install = %d, want 200 (the engine is never restarted when already current)", resp.StatusCode)
	}
	if st.Status != "current" {
		t.Fatalf("status = %q, want current (no-op short-circuit)", st.Status)
	}
	if srv.engineUpdateRunning.Load() {
		t.Fatal("the no-op path must release the single-flight slot immediately")
	}
}

func TestEngineUpdateSingleFlight(t *testing.T) {
	server, srv := newEngineUpdateTestServer(t, "b12031", []updater.ReleaseListEntryForTest{
		{Tag: "b12032", Assets: []string{hostCPUAsset(t, "b12032")}},
	})

	// Claim the slot the way a running install would.
	if !srv.engineUpdateRunning.CompareAndSwap(false, true) {
		t.Fatal("fixture failed to claim the slot")
	}
	t.Cleanup(func() { srv.engineUpdateRunning.Store(false) })

	resp, _ := postEngineUpdate(t, server, `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("single-flight gate = %d, want 409 (one user-triggered engine update at a time)", resp.StatusCode)
	}

	// The check surface reports the live installing state and never races
	// a fresh lookup while a transaction runs.
	st := getEngineUpdateCheck(t, server)
	if st.Status != "installing" {
		t.Fatalf("status = %q, want installing while a transaction runs", st.Status)
	}
}
