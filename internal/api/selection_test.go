// selection_test.go — the v1.5.0 MODEL-FIRST contract tests.
//
// Proven here through the REAL HTTP surface (the same handler chain a
// user drives):
//
//   - a fresh install with NO selected model never implicitly loads a
//     model: /api/engine reports selectionRequired + phase
//     "model-selection", the engine stays idle, and an explicit engine
//     start is a clean 400 (no arbitrary first-GGUF boot);
//   - POST /api/models/select applies the selection ATOMICALLY (one
//     config write: model + AUTO profile together) and reports the
//     backend-authoritative phase chain;
//   - MANUAL preserves explicit user values verbatim on a model change;
//   - an unknown model fails honestly (404) without touching config;
//   - a model change while a run is active is BLOCKED (409);
//   - the per-model recommendation evidence endpoint only labels a model
//     "safe" from the recommendation engine's measured evidence.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/calibration"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
)

func writeModelFixture(t *testing.T, dir, name string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("GGUF....test-fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func postSelect(t *testing.T, server *httptest.Server, body map[string]any) (int, map[string]any) {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(server.URL+"/api/models/select", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST /api/models/select: %v", err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode select response: %v", err)
	}

	return resp.StatusCode, out
}

func getEngineSnapshot(t *testing.T, server *httptest.Server) map[string]any {
	t.Helper()

	resp, err := http.Get(server.URL + "/api/engine")
	if err != nil {
		t.Fatalf("GET /api/engine: %v", err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode engine snapshot: %v", err)
	}

	return out
}

func getConfigMap(t *testing.T, server *httptest.Server) map[string]any {
	t.Helper()

	resp, err := http.Get(server.URL + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	return out
}

// TestModelFirstFreshInstallNeverImplicitlyLoads: a fresh install (no
// selected model, models PRESENT) must reach the Model Selector state —
// never an engine boot of an arbitrary model.
func TestModelFirstFreshInstallNeverImplicitlyLoads(t *testing.T) {
	server, cfg := newTestServer(t)

	// Local models EXIST — the old behavior would have picked the first.
	writeModelFixture(t, cfg.ModelsDir, "aaa-first-alphabetical.gguf")
	writeModelFixture(t, cfg.ModelsDir, "zzz-other.gguf")

	// EnsureSetup already ran (with LlamaAutoStart=false in the helper —
	// but the PREWARM ITSELF must gate on the selection, so re-prove the
	// gate directly through the runtime prewarm seam).
	snap := getEngineSnapshot(t, server)

	if snap["selectionRequired"] != true {
		t.Fatalf("fresh install must report selectionRequired, got %+v", snap)
	}
	if snap["phase"] != "model-selection" {
		t.Fatalf("fresh install phase = %v, want model-selection", snap["phase"])
	}
	if state, _ := snap["state"].(string); state != "idle" && state != "stopped" {
		t.Fatalf("engine must stay idle/stopped before selection, got %q", state)
	}

	// An explicit engine start without a selection is a clean 400 with
	// the actionable reason — never a silent arbitrary-model boot.
	resp, err := http.Post(server.URL+"/api/llama", "application/json",
		bytes.NewReader([]byte(`{"action":"start"}`)))
	if err != nil {
		t.Fatalf("POST /api/llama: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("engine start without selection = %d, want 400", resp.StatusCode)
	}

	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(errBody.Error), []byte("no model selected")) {
		t.Fatalf("error must name the missing selection, got %q", errBody.Error)
	}
}

// TestSelectModelAppliesConfigAtomically (AUTO): the selection and the
// recommendation profile land in ONE config write — after the call,
// model + profile + previousModel are all present together, and the
// phase chain reached ready.
func TestSelectModelAppliesConfigAtomically(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")
	writeModelFixture(t, cfg.ModelsDir, "model-b.gguf")

	code, out := postSelect(t, server, map[string]any{"model": "model-a.gguf"})
	if code != http.StatusOK {
		t.Fatalf("select = %d: %v", code, out)
	}

	if out["phase"] != "ready" {
		t.Fatalf("select phase = %v, want ready (auto-start off: applied, boots on first use)", out["phase"])
	}
	if out["model"] != "model-a.gguf" {
		t.Fatalf("select model = %v", out["model"])
	}

	// The atomic write: model + profile + previousModel all present.
	cfgMap := getConfigMap(t, server)
	if cfgMap["model"] != "model-a.gguf" {
		t.Fatalf("config.model = %v", cfgMap["model"])
	}
	if cfgMap["performanceMode"] != "auto" {
		t.Fatalf("config.performanceMode = %v, want auto (the default)", cfgMap["performanceMode"])
	}

	// AUTO applied a profile (the recommendation engine produced one —
	// with a non-GGUF fixture the card is nil but the profile still
	// computes from measured hardware).
	sel, _ := out["applied"].(map[string]any)
	if sel == nil {
		t.Fatalf("AUTO selection must carry the applied profile: %v", out)
	}
	if ev, _ := out["evidence"].([]any); len(ev) == 0 {
		t.Fatalf("AUTO selection must carry the measured evidence: %v", out)
	}

	// Second selection records the previous model.
	code, out2 := postSelect(t, server, map[string]any{"model": "model-b.gguf"})
	if code != http.StatusOK {
		t.Fatalf("second select = %d: %v", code, out2)
	}
	if out2["previousModel"] != "model-a.gguf" {
		t.Fatalf("previousModel = %v, want model-a.gguf", out2["previousModel"])
	}

	cfgMap = getConfigMap(t, server)
	if cfgMap["model"] != "model-b.gguf" {
		t.Fatalf("config.model after switch = %v", cfgMap["model"])
	}
	if cfgMap["previousModel"] != "model-a.gguf" {
		t.Fatalf("config.previousModel = %v", cfgMap["previousModel"])
	}
}

// TestSelectModelManualPreservesExplicitValues: in MANUAL a model change
// never overwrites the user's explicit settings.
func TestSelectModelManualPreservesExplicitValues(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")
	writeModelFixture(t, cfg.ModelsDir, "model-b.gguf")

	// The user's explicit manual values.
	patch := map[string]any{
		"performanceMode": "manual",
		"llm": map[string]any{
			"numCtx":    1234,
			"numThread": 3,
		},
		"flashAttention": false,
	}
	raw, _ := json.Marshal(patch)
	resp, err := http.NewRequest(http.MethodPut, server.URL+"/api/config", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp.Header.Set("Content-Type", "application/json")
	httpResp, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	httpResp.Body.Close()

	code, out := postSelect(t, server, map[string]any{"model": "model-b.gguf"})
	if code != http.StatusOK {
		t.Fatalf("manual select = %d: %v", code, out)
	}

	if out["applied"] != nil {
		t.Fatalf("MANUAL selection must NOT apply a profile: %v", out["applied"])
	}

	cfgMap := getConfigMap(t, server)
	llm, _ := cfgMap["llm"].(map[string]any)
	if llm == nil || llm["numCtx"] != float64(1234) || llm["numThread"] != float64(3) {
		t.Fatalf("MANUAL must preserve explicit values, got llm=%v", llm)
	}
	if cfgMap["flashAttention"] != false {
		t.Fatalf("MANUAL must preserve flashAttention=false, got %v", cfgMap["flashAttention"])
	}
	if cfgMap["model"] != "model-b.gguf" {
		t.Fatalf("MANUAL still switches the model, got %v", cfgMap["model"])
	}
}

// TestSelectUnknownModelFailsHonestly: an unknown model is a 404 with
// the honest reason and NO config mutation.
func TestSelectUnknownModelFailsHonestly(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")

	before := getConfigMap(t, server)

	code, out := postSelect(t, server, map[string]any{"model": "ghost.gguf"})
	if code != http.StatusNotFound {
		t.Fatalf("unknown model select = %d, want 404: %v", code, out)
	}

	after := getConfigMap(t, server)
	if after["model"] != before["model"] {
		t.Fatalf("a failed selection must not mutate config: %v -> %v", before["model"], after["model"])
	}

	snap := getEngineSnapshot(t, server)
	sel, _ := snap["selection"].(map[string]any)
	if sel == nil || sel["phase"] != "failed" {
		t.Fatalf("selection state must record the honest failure: %v", sel)
	}
}

// TestSelectBlockedWhileRunActive: a model change while a generation is
// live is BLOCKED with 409 — never raced.
func TestSelectBlockedWhileRunActive(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"

	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	t.Cleanup(srv.Close)

	// Simulate ONE active run in the authoritative registry.
	srv.runsMu.Lock()
	srv.runs["session-live"] = &runState{
		cancel: func() {},
		hub:    newActivityHub(),
	}
	srv.runsMu.Unlock()

	rec := httptest.NewRequest(http.MethodPost, "/api/models/select",
		bytes.NewReader([]byte(`{"model":"model-a.gguf"}`)))
	rec.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleModelsSelect(w, rec)

	if w.Code != http.StatusConflict {
		t.Fatalf("select during a live run = %d, want 409: %s", w.Code, w.Body.String())
	}
}

// TestEngineSnapshotCarriesSelectionState: after a selection the engine
// snapshot carries the backend-authoritative selection block.
func TestEngineSnapshotCarriesSelectionState(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")

	snap := getEngineSnapshot(t, server)
	if _, has := snap["selection"]; has {
		t.Fatalf("before any selection the pre-selection state is described by selectionRequired alone: %v", snap)
	}

	code, _ := postSelect(t, server, map[string]any{"model": "model-a.gguf"})
	if code != http.StatusOK {
		t.Fatal("select failed")
	}

	snap = getEngineSnapshot(t, server)
	sel, _ := snap["selection"].(map[string]any)
	if sel == nil {
		t.Fatalf("engine snapshot must carry the selection state: %v", snap)
	}
	if sel["phase"] != "ready" {
		t.Fatalf("selection phase = %v, want ready", sel["phase"])
	}
	if sel["performanceMode"] != "auto" {
		t.Fatalf("selection performanceMode = %v", sel["performanceMode"])
	}
	// selectionRequired cleared once a model is selected.
	if snap["selectionRequired"] == true {
		t.Fatalf("selectionRequired must clear after a selection: %v", snap)
	}
}

// TestModelsRecommendationsEvidenceOnly: the per-model recommendation
// endpoint derives labels ONLY from the recommendation engine's
// evidence over measured hardware — a fixture model that is not a real
// GGUF must never be labelled "recommended" from filename/size alone.
func TestModelsRecommendationsEvidenceOnly(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")

	resp, err := http.Get(server.URL + "/api/models/recommendations")
	if err != nil {
		t.Fatalf("GET /api/models/recommendations: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recommendations = %d", resp.StatusCode)
	}

	var out struct {
		HardwareMeasured bool `json:"hardwareMeasured"`
		Recommendations  map[string]struct {
			Class            string   `json:"class"`
			Reasons          []string `json:"reasons"`
			HardwareMeasured bool     `json:"hardwareMeasured"`
		} `json:"recommendations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode recommendations: %v", err)
	}

	if !out.HardwareMeasured {
		t.Fatalf("the endpoint must report measured hardware: %+v", out)
	}

	rec, ok := out.Recommendations["model-a.gguf"]
	if !ok {
		t.Fatalf("the local model must be present: %+v", out.Recommendations)
	}
	// A non-GGUF fixture has no model card: the engine CANNOT honestly
	// assess its resources, so the verdict is EMPTY (never a filename or
	// size heuristic dressed up as "safe").
	if rec.Class == "safe" {
		t.Fatalf("a card-less fixture must never be labelled safe from size alone: %+v", rec)
	}
}

// TestModelsListMarksPreviousSelection: the models list carries the
// previous-selection marker after a switch.
func TestModelsListMarksPreviousSelection(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-a.gguf")
	writeModelFixture(t, cfg.ModelsDir, "model-b.gguf")

	if code, _ := postSelect(t, server, map[string]any{"model": "model-a.gguf"}); code != http.StatusOK {
		t.Fatal("select a failed")
	}
	if code, _ := postSelect(t, server, map[string]any{"model": "model-b.gguf"}); code != http.StatusOK {
		t.Fatal("select b failed")
	}

	resp, err := http.Get(server.URL + "/api/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out struct {
		Local []struct {
			ID       string `json:"id"`
			Previous bool   `json:"previous"`
		} `json:"local"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	for _, m := range out.Local {
		if m.ID == "model-a.gguf" && !m.Previous {
			t.Fatalf("model-a must carry the previous-selection marker: %+v", out.Local)
		}
	}
}

// TestSelectModelAppliesIntendedTask proves the selection task contract
// (spec §5): the intended Chat/Agent task travels with the selection —
// the backend never silently defaults the runtime profile while the UI
// is on a different surface.
func TestSelectModelAppliesIntendedTask(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-task.gguf")

	// Agent surface: the UI sends task=agent.
	code, body := postSelect(t, server, map[string]any{"model": "model-task.gguf", "task": "agent"})
	if code != http.StatusOK {
		t.Fatalf("agent select = %d, body=%v", code, body)
	}
	if got := liveRuntimeProfile(t, server); got != "agent" {
		t.Fatalf("config runtime profile = %q, want agent", got)
	}
	if applied, _ := body["applied"].(map[string]any); applied["runtimeProfile"] != "agent" {
		t.Fatalf("applied.runtimeProfile = %v, want agent", applied["runtimeProfile"])
	}

	// Chat surface: the UI sends task=chat.
	code, body = postSelect(t, server, map[string]any{"model": "model-task.gguf", "task": "chat"})
	if code != http.StatusOK {
		t.Fatalf("chat select = %d, body=%v", code, body)
	}
	if got := liveRuntimeProfile(t, server); got != "chat" {
		t.Fatalf("config runtime profile = %q, want chat", got)
	}
	if applied, _ := body["applied"].(map[string]any); applied["runtimeProfile"] != "chat" {
		t.Fatalf("applied.runtimeProfile = %v, want chat", applied["runtimeProfile"])
	}
}

// TestSelectModelChatAndAgentReceiveDifferentProfiles proves spec §5:
// the SAME model selected for Chat and for Agent legitimately receives
// different runtime profiles (agent targets a larger context base).
func TestSelectModelChatAndAgentReceiveDifferentProfiles(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-both.gguf")

	_, chat := postSelect(t, server, map[string]any{"model": "model-both.gguf", "task": "chat"})
	chatCtx, _ := chat["applied"].(map[string]any)["context"].(float64)

	_, agent := postSelect(t, server, map[string]any{"model": "model-both.gguf", "task": "agent"})
	agentCtx, _ := agent["applied"].(map[string]any)["context"].(float64)

	if chatCtx == 0 || agentCtx == 0 {
		t.Fatalf("both selections must apply a context, chat=%v agent=%v", chatCtx, agentCtx)
	}
	if chatCtx == agentCtx {
		t.Fatalf("chat and agent profiles must differ for the same model, both context=%v", chatCtx)
	}

	llm := getConfigMap(t, server)["llm"].(map[string]any)
	if llm["numCtx"].(float64) != agentCtx {
		t.Fatalf("live config context %v must match the last (agent) selection %v", llm["numCtx"], agentCtx)
	}
}

// TestSelectBlockedWhileCalibrating proves the exclusion gate (spec
// §2.4, §7): while a bounded calibration pass is active, model changes,
// performance mutations and engine start/stop are ALL rejected with a
// real 409 — never raced. Reads stay available.
func TestSelectBlockedWhileCalibrating(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"

	writeModelFixture(t, cfg.ModelsDir, "model-gate.gguf")

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	t.Cleanup(srv.Close)

	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	// Arm the calibration/model-change exclusion gate.
	srv.calibrating.Store(true)
	defer srv.calibrating.Store(false)

	// Model change → 409.
	code, body := postSelect(t, server, map[string]any{"model": "model-gate.gguf"})
	if code != http.StatusConflict {
		t.Fatalf("select during calibration = %d, want 409 (%v)", code, body)
	}

	// Performance mutation → 409.
	resp, err := http.Post(server.URL+"/api/config", "application/json", bytes.NewReader([]byte(`{"performanceMode":"manual"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("config mutation during calibration = %d, want 409", resp.StatusCode)
	}

	// Engine start/stop → 409.
	for _, action := range []string{"start", "stop"} {
		raw, _ := json.Marshal(map[string]any{"action": action})
		resp, err := http.Post(server.URL+"/api/llama", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("engine %s during calibration = %d, want 409", action, resp.StatusCode)
		}
	}

	// Reads stay available (the state must stay observable).
	resp, err = http.Get(server.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config READ during calibration = %d, want 200", resp.StatusCode)
	}
}

// TestRetainedCalibrationReusedOnlyWithMatchingFingerprint proves spec
// §6 end-to-end: a retained measured profile is reused only for the same
// model file, the same machine/engine fingerprint AND the same task;
// stale or corrupt records fall back to the evidence-based profile.
func TestRetainedCalibrationReusedOnlyWithMatchingFingerprint(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-retained.gguf")

	resolved, err := llm.ResolveModelPath(cfg.ModelsDir, "model-retained.gguf")
	if err != nil {
		t.Fatal(err)
	}
	mk, err := calibration.ModelKey(resolved)
	if err != nil {
		t.Fatal(err)
	}
	hwKey := calibration.HWKey(hardware.Collect(cfg))

	retainedProfile := recommendation.Recommendation{
		Task:           recommendation.TaskChat,
		Context:        9999,
		Threads:        4,
		GPULayers:      7,
		UBatchSize:     256,
		FlashAttention: false,
		KVCacheQuant:   "q8_0",
	}

	clearStore := func() {
		t.Helper()
		if err := os.Remove(filepath.Join(cfg.DataDir, calibration.StoreFileName)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	t.Cleanup(clearStore)

	t.Run("matching fingerprint reuses the measured profile", func(t *testing.T) {
		rec := calibration.Record{
			ModelPath:       resolved,
			Winner:          "GPU offload",
			Profile:         retainedProfile,
			GenTokensPerSec: 42,
			TTFTSeconds:     0.25,
			MeasuredAt:      time.Now().UTC(),
		}
		if err := calibration.Retain(cfg.DataDir, mk, hwKey, rec); err != nil {
			t.Fatalf("retain: %v", err)
		}

		code, body := postSelect(t, server, map[string]any{"model": "model-retained.gguf"})
		if code != http.StatusOK {
			t.Fatalf("select = %d, body=%v", code, body)
		}

		applied, _ := body["applied"].(map[string]any)
		if applied["context"].(float64) != 9999 || applied["gpuLayers"].(float64) != 7 {
			t.Fatalf("the retained measured profile must be applied, got %v", applied)
		}
		if body["calibrated"] != true {
			t.Fatal("a retained measurement must report calibrated=true")
		}
		if note, _ := body["calibrationNote"].(string); !strings.Contains(note, "retained verified profile") {
			t.Fatalf("the retained note must be surfaced, got %q", note)
		}

		clearStore()
	})

	t.Run("different machine fingerprint is stale", func(t *testing.T) {
		rec := calibration.Record{
			ModelPath:  resolved,
			Winner:     "GPU offload",
			Profile:    retainedProfile,
			MeasuredAt: time.Now().UTC(),
		}
		if err := calibration.Retain(cfg.DataDir, mk, hwKey+"-DIFFERENT", rec); err != nil {
			t.Fatal(err)
		}

		code, body := postSelect(t, server, map[string]any{"model": "model-retained.gguf"})
		if code != http.StatusOK {
			t.Fatalf("select = %d", code)
		}
		applied, _ := body["applied"].(map[string]any)
		if applied["context"].(float64) == 9999 {
			t.Fatal("a stale fingerprint must NOT reuse the retained profile")
		}
		if body["calibrated"] == true {
			t.Fatal("a stale fingerprint must not claim calibrated=true")
		}

		clearStore()
	})

	t.Run("different task is stale", func(t *testing.T) {
		stale := retainedProfile
		stale.Task = recommendation.TaskAgent
		rec := calibration.Record{
			ModelPath:  resolved,
			Winner:     "GPU offload",
			Profile:    stale,
			MeasuredAt: time.Now().UTC(),
		}
		if err := calibration.Retain(cfg.DataDir, mk, hwKey, rec); err != nil {
			t.Fatal(err)
		}

		code, body := postSelect(t, server, map[string]any{"model": "model-retained.gguf"})
		if code != http.StatusOK {
			t.Fatalf("select = %d", code)
		}
		applied, _ := body["applied"].(map[string]any)
		if applied["context"].(float64) == 9999 {
			t.Fatal("a chat selection must not reuse an agent-tuned record")
		}

		clearStore()
	})

	t.Run("corrupt store fails closed", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(cfg.DataDir, calibration.StoreFileName), []byte("{corrupt"), 0o644); err != nil {
			t.Fatal(err)
		}

		code, body := postSelect(t, server, map[string]any{"model": "model-retained.gguf"})
		if code != http.StatusOK {
			t.Fatalf("a corrupt store must not break selection, got %d", code)
		}
		applied, _ := body["applied"].(map[string]any)
		if applied["context"].(float64) == 9999 {
			t.Fatal("a corrupt store must never reuse anything")
		}
		if body["calibrated"] == true {
			t.Fatal("a corrupt store must not claim calibrated=true")
		}
	})
}

// TestSelectInvalidTaskFallsBackToChat documents the backend contract:
// an unknown task id never poisons the profile — it falls back to chat.
func TestSelectInvalidTaskFallsBackToChat(t *testing.T) {
	server, cfg := newTestServer(t)
	writeModelFixture(t, cfg.ModelsDir, "model-task2.gguf")

	code, body := postSelect(t, server, map[string]any{"model": "model-task2.gguf", "task": "gaming"})
	if code != http.StatusOK {
		t.Fatalf("select = %d", code)
	}
	if got := liveRuntimeProfile(t, server); got != string(recommendation.TaskChat) {
		t.Fatalf("invalid task must fall back to chat, got %q", got)
	}
	if applied, _ := body["applied"].(map[string]any); applied["runtimeProfile"] != "chat" {
		t.Fatalf("applied.runtimeProfile = %v, want chat", applied["runtimeProfile"])
	}
}

// liveRuntimeProfile reads the runtime profile from the live config.
func liveRuntimeProfile(t *testing.T, server *httptest.Server) string {
	t.Helper()
	got, _ := getConfigMap(t, server)["runtimeProfile"].(string)
	return got
}
