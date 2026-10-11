package api

// engine_variant_strict_test.go — v1.6.2: the strict variant contract at
// the API surface. POST /api/engine/provision must answer a DETERMINISTIC
// 400 for an empty/unknown variant — invalid input can never become CPU
// provisioning (the v1.6.1 defect: {"variant":"banana"} silently
// installed the CPU package with a 200).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

func TestEngineProvisionInvalidVariantIsDeterministic400(t *testing.T) {
	server, _ := newTestServer(t)

	for _, in := range []string{"banana", "", "   ", "cu", "vulkann", "0", "auto"} {
		body, _ := json.Marshal(map[string]string{"variant": in})
		resp, err := http.Post(server.URL+"/api/engine/provision", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /api/engine/provision: %v", err)
		}

		var out struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("variant %q: status = %d, want deterministic 400 (error: %s)", in, resp.StatusCode, out.Error)
			continue
		}
		if !strings.Contains(out.Error, "invalid engine variant") {
			t.Errorf("variant %q: error must classify the invalid variant, got %q", in, out.Error)
		}
		if !strings.Contains(out.Error, `"vulkan"`) || !strings.Contains(out.Error, `"cpu"`) {
			t.Errorf("variant %q: error must name the accepted values, got %q", in, out.Error)
		}
	}
}

func TestEngineProvisionMissingVariantFieldIs400(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Post(server.URL+"/api/engine/provision", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("POST /api/engine/provision: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body: status = %d, want 400 — a request with no variant is not a CPU request", resp.StatusCode)
	}
}

func TestEngineProvisionMalformedJSONIs400(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Post(server.URL+"/api/engine/provision", "application/json", bytes.NewReader([]byte(`{not json`)))
	if err != nil {
		t.Fatalf("POST /api/engine/provision: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed body: status = %d, want 400", resp.StatusCode)
	}
}

func TestEngineProvisionGetReportsAuthoritativeState(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/api/engine/provision")
	if err != nil {
		t.Fatalf("GET /api/engine/provision: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var out struct {
		InstalledVariant  string   `json:"installedVariant"`
		SupportedVariants []string `json:"supportedVariants"`
		Platform          string   `json:"platform"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	if out.InstalledVariant != string(updater.VariantCPU) {
		t.Fatalf("a fresh install reports the CPU package, got %q", out.InstalledVariant)
	}
	if len(out.SupportedVariants) == 0 || out.SupportedVariants[0] != "cpu" {
		t.Fatalf("supported variants must at least contain cpu, got %v", out.SupportedVariants)
	}
	if out.Platform == "" {
		t.Fatal("platform must be reported")
	}
}
