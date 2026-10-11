package api

// perf_accelerator_test.go — v1.2.6: /api/perf must carry the measured
// accelerator resolution (requested/resolved/backend/device/reason), and
// on THIS machine (no GPU engine provisioned in tests) the honest verdict
// is CPU with a recorded reason — never a fabricated GPU claim.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPerfCarriesAcceleratorResolution(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/api/perf")
	if err != nil {
		t.Fatalf("GET /api/perf: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var payload struct {
		Accelerator *struct {
			Requested   string   `json:"requested"`
			Backend     string   `json:"backend"`
			Device      string   `json:"device"`
			AutoProfile string   `json:"autoProfile"`
			GPULayers   int      `json:"gpuLayers"`
			Reason      string   `json:"reason"`
			Fallbacks   []string `json:"fallbacks"`
		} `json:"accelerator"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if payload.Accelerator == nil {
		t.Fatal("accelerator block missing from /api/perf")
	}

	if payload.Accelerator.Requested != "AUTO" {
		t.Fatalf("requested = %q, want AUTO (default)", payload.Accelerator.Requested)
	}

	// On the test machine no engine binary is provisioned: enumeration has
	// no evidence, no runtime offload line exists — the honest verdict is
	// CPU with the reason recorded.
	if payload.Accelerator.Backend != "CPU" {
		t.Fatalf("backend = %q, want CPU (no GPU evidence exists in tests)", payload.Accelerator.Backend)
	}

	if strings.TrimSpace(payload.Accelerator.Reason) == "" {
		t.Fatal("resolution reason is empty — the WHY is mandatory")
	}

	if payload.Accelerator.AutoProfile != "CPU_SAFE" {
		t.Fatalf("auto profile = %q, want CPU_SAFE", payload.Accelerator.AutoProfile)
	}
}
