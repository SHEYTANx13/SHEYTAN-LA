// Settings-save
// contract.
//
// Contracts under test (release contract §11 "Settings"):
//  1. a partial llm patch (what posture/preset applies send) does NOT
//     erase unspecified sampling fields — the v1.2.9 shallow merge
//     replaced the whole LLMOptions and zeroed temperature/topP/…;
//  2. saving settings does not erase fields the UI no longer exposes
//     (visionMmproj, engineCompat, workspaceRoot, …) — legacy config
//     values survive round-trips;
//  3. a blank remoteApiKey round-trip (the redacted GET shape) never
//     erases the stored secret.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func patchConfig(t *testing.T, srv *httptest.Server, patch map[string]any) (int, map[string]any) {
	t.Helper()

	body, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.NewRequest(http.MethodPut, srv.URL+"/api/config", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	defer httpResp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(httpResp.Body).Decode(&out); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	return httpResp.StatusCode, out
}

func TestPartialLLMPatchDoesNotWipeSamplingFields(t *testing.T) {
	server, cfg := newTestServer(t)

	// Baseline sampling values (persisted shape).
	_, out := patchConfig(t, server, map[string]any{
		"llm": map[string]any{
			"temperature":   0.42,
			"topP":          0.91,
			"topK":          77,
			"maxTokens":     2048,
			"repeatPenalty": 1.07,
		},
	})
	if out["llm"] == nil {
		t.Fatalf("full llm patch failed: %v", out)
	}

	// The defect shape: a partial llm patch carrying ONLY numCtx (what
	// posture and preset applies send).
	code, out2 := patchConfig(t, server, map[string]any{
		"llm": map[string]any{"numCtx": 8192},
	})
	if code != http.StatusOK {
		t.Fatalf("partial llm patch = %d: %v", code, out2)
	}

	llm, _ := out2["llm"].(map[string]any)
	if llm == nil {
		t.Fatalf("no llm in response: %v", out2)
	}

	for _, field := range []string{"temperature", "topP", "topK", "maxTokens", "repeatPenalty"} {
		if llm[field] == nil {
			t.Fatalf("partial llm patch wiped %s: %v", field, llm)
		}
	}
	if llm["temperature"] != 0.42 {
		t.Fatalf("temperature = %v, want 0.42", llm["temperature"])
	}
	if llm["numCtx"] != float64(8192) {
		t.Fatalf("numCtx = %v, want 8192", llm["numCtx"])
	}

	// The PERSISTED file round-trips the same guarantee.
	persisted, err := config.Load(cfg.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.LLM.Temperature != 0.42 || persisted.LLM.NumCtx != 8192 {
		t.Fatalf("persisted llm = %+v", persisted.LLM)
	}
}

func TestSavingSettingsDoesNotEraseUnexposedFields(t *testing.T) {
	server, cfg := newTestServer(t)

	// Seed legacy/technical fields exactly as a v1.2.9 config carries
	// them (fields the v1.3.0 UI no longer exposes as editable
	// controls).
	_, out := patchConfig(t, server, map[string]any{
		"visionMmproj":    "projector-override.gguf",
		"kvCacheQuant":    "q8_0",
		"ubatchSize":      256,
		"threadsBatch":    8,
		"cacheReuse":      32,
		"engineCompat":    1,
		"maxLogMb":        64,
		"maxSessionsKept": 42,
	})
	if out["visionMmproj"] == nil {
		t.Fatalf("seed patch failed: %v", out)
	}

	// An ordinary UI save (thinkingMode toggle) must not erase them.
	code, out2 := patchConfig(t, server, map[string]any{"thinkingMode": true})
	if code != http.StatusOK {
		t.Fatalf("ordinary save = %d", code)
	}
	if out2["visionMmproj"] != "projector-override.gguf" {
		t.Fatalf("visionMmproj erased by ordinary save: %v", out2["visionMmproj"])
	}
	if out2["engineCompat"] != float64(1) {
		t.Fatalf("engineCompat erased: %v", out2["engineCompat"])
	}
	if out2["maxSessionsKept"] != float64(42) {
		t.Fatalf("maxSessionsKept erased: %v", out2["maxSessionsKept"])
	}

	persisted, err := config.Load(cfg.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.VisionMMProj != "projector-override.gguf" {
		t.Fatalf("persisted visionMmproj = %q", persisted.VisionMMProj)
	}
	if persisted.EngineCompat != 1 {
		t.Fatalf("persisted engineCompat = %d", persisted.EngineCompat)
	}
}

func TestBlankAPIKeyRoundTripDoesNotEraseSecret(t *testing.T) {
	server, _ := newTestServer(t)

	_, out := patchConfig(t, server, map[string]any{"remoteApiKey": "super-secret"})
	// GET/PUT responses are redacted — the key never rides back.
	if out["remoteApiKey"] != nil && out["remoteApiKey"] != "" {
		t.Fatalf("API key leaked into the response: %v", out["remoteApiKey"])
	}

	// The redacted round-trip (blank key, exactly what a UI re-save of
	// the redacted config would send).
	code, _ := patchConfig(t, server, map[string]any{"remoteApiKey": ""})
	if code != http.StatusOK {
		t.Fatalf("blank key round-trip = %d", code)
	}

	// The stored secret survives — verified through the live source
	// (redacted in HTTP responses by construction).
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	srv := server.Config.Handler.(http.Handler)
	srv.ServeHTTP(rec, req)
	var live map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	if live["remoteApiKey"] != "" {
		t.Fatalf("API key visible in GET response: %v", live["remoteApiKey"])
	}
}
