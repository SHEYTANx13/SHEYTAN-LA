package api

// modelstudio_systems_v1111_test.go — run #41415240891 Model Studio (§5.2)
// at the API boundary: per-AI-System model assignment and runtime-profile
// binding through the EXISTING /api/systems store (no second agent-profile
// authority), server-side validation in Go, and the frozen per-run
// snapshot carrying the Model Studio fields.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/aisystem"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

func newModelStudioTestServer(t *testing.T) *httptest.Server {
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

	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)
	return server
}

// putSystemModelStudio PUTs the model-assignment surface of one system.
func putSystemModelStudio(t *testing.T, server *httptest.Server, id, body string) (*http.Response, map[string]any) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/systems/"+id, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/systems/%s: %v", id, err)
	}
	defer resp.Body.Close()

	var out map[string]any
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		out = map[string]any{}
	}
	return resp, out
}

func getSystem(t *testing.T, server *httptest.Server, id string) (int, aisystem.System) {
	t.Helper()

	resp, err := http.Get(server.URL + "/api/systems/" + id)
	if err != nil {
		t.Fatalf("GET /api/systems/%s: %v", id, err)
	}
	defer resp.Body.Close()

	var sys aisystem.System
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&sys); err != nil {
			t.Fatalf("decode system: %v", err)
		}
	}
	return resp.StatusCode, sys
}

func TestSystemModelStudioRoundTrip(t *testing.T) {
	server := newModelStudioTestServer(t)

	body := `{
		"systemId": "default",
		"name": "Default",
		"model": "qwen3-coder.gguf",
		"runtimeProfile": "coding",
		"compactionPolicy": "conservative",
		"memoryPolicy": "read-only",
		"approvalPolicy": "ask-risky"
	}`

	resp, _ := putSystemModelStudio(t, server, "default", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT model-assignment surface = %d, want 200", resp.StatusCode)
	}

	code, sys := getSystem(t, server, "default")
	if code != http.StatusOK {
		t.Fatalf("GET after PUT = %d", code)
	}
	if sys.Model != "qwen3-coder.gguf" {
		t.Errorf("stored model override = %q, want the assigned value", sys.Model)
	}
	if sys.RuntimeProfile != "coding" {
		t.Errorf("stored runtime profile = %q, want coding", sys.RuntimeProfile)
	}
	if sys.CompactionPolicy != "conservative" || sys.MemoryPolicy != "read-only" {
		t.Errorf("stored policies = %q/%q, want conservative/read-only", sys.CompactionPolicy, sys.MemoryPolicy)
	}

	// The per-run snapshot freezes the assignment (activate returns it).
	act, err := http.Post(server.URL+"/api/systems/default/activate", "application/json", nil)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	defer act.Body.Close()

	var activated struct {
		OK       bool              `json:"ok"`
		Active   string            `json:"active"`
		Revision int               `json:"revision"`
		Snapshot aisystem.Snapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(act.Body).Decode(&activated); err != nil {
		t.Fatalf("decode activate: %v", err)
	}
	if !activated.OK || activated.Snapshot.Model != "qwen3-coder.gguf" || activated.Snapshot.RuntimeProfile != "coding" {
		t.Fatalf("activated snapshot must freeze the model + profile binding, got %+v", activated)
	}

	// Inherit is the EMPTY value, distinct from a missing model: clearing
	// the override returns the system to the inherit state.
	resp2, _ := putSystemModelStudio(t, server, "default", `{
		"systemId": "default",
		"name": "Default",
		"model": "",
		"runtimeProfile": "",
		"approvalPolicy": "ask-risky"
	}`)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("PUT inherit state = %d, want 200", resp2.StatusCode)
	}

	code, sys = getSystem(t, server, "default")
	if code != http.StatusOK || sys.Model != "" || sys.RuntimeProfile != "" {
		t.Fatalf("inherit round-trip failed: code=%d model=%q profile=%q", code, sys.Model, sys.RuntimeProfile)
	}
}

func TestSystemModelOverrideRejectsPathsAndJunk(t *testing.T) {
	server := newModelStudioTestServer(t)

	for _, bad := range []string{"../escape.gguf", "dir/model.gguf", "model with space.gguf"} {
		body, _ := json.Marshal(map[string]any{
			"systemId": "default",
			"name":     "Default",
			"model":    bad,
		})
		resp, _ := putSystemModelStudio(t, server, "default", string(body))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("model override %q: status = %d, want 400 (server-side validation is the contract)", bad, resp.StatusCode)
		}

		// The rejected write must not persist.
		if code, sys := getSystem(t, server, "default"); code == http.StatusOK && sys.Model == bad {
			t.Errorf("model override %q was persisted despite the rejection", bad)
		}
	}
}

func TestSystemProfileValidatedAgainstOneTaskVocabulary(t *testing.T) {
	server := newModelStudioTestServer(t)

	// Unknown profiles are rejected server-side.
	resp, _ := putSystemModelStudio(t, server, "default",
		`{"systemId":"default","name":"Default","runtimeProfile":"turbo"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown runtime profile: status = %d, want 400", resp.StatusCode)
	}

	// Every REAL recommendation task id round-trips.
	for _, task := range []string{"chat", "coding", "research", "vision", "agent", "low-power", "maximum"} {
		body, _ := json.Marshal(map[string]any{
			"systemId":       "default",
			"name":           "Default",
			"runtimeProfile": task,
		})
		r, _ := putSystemModelStudio(t, server, "default", string(body))
		if r.StatusCode != http.StatusOK {
			t.Errorf("supported runtime profile %q: status = %d, want 200", task, r.StatusCode)
		}
	}
}
