// governor_test.go — v1.8.0: the /api/governor wire contract.
//
// The system-health surface must serve REAL, composed values only: the
// governor block reflects the folded resource state (deterministic here —
// the test folds its own sample), the hardware/engine/capability blocks
// come from the existing authorities, and nothing may be a placeholder.
// The API contract also guards the version identity and the nil-safe
// posture for stacks without a governor.
package api

import (
        "encoding/json"
        "net/http"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

func TestGovernorEndpointServesComposedSelfModel(t *testing.T) {
        srv, server := newRemoteServerWithHandle(t, "http://127.0.0.1:1") // no live engine needed
        _ = srv

        // Fold ONE deterministic sample into the wired governor (the Stack's
        // StartGovernor ran inside api.New): 32 GiB total, 50% available.
        gov := srv.stack.Governor()
        if gov == nil {
                t.Fatal("api.New must wire the runtime governor (StartGovernor)")
        }

        // v1.9.3 (run 38035650428 / job 114165397477): StartGovernor wires the
        // PRODUCTION platform CPU seam (governor.CPULoadPlatform), so folding
        // a sample here also folds the HOST's real load. On a busy CI runner
        // the first EWMA sample equals the raw reading, and one reading above
        // CPUReduceAbove flipped this OK-pressure contract to class "live" —
        // host load leaking into a wire-contract test. The envelope contract
        // under test is about PRESSURE, not the host's CPU: pin the seam to an
        // honest unknown (the same posture as a platform that cannot measure
        // CPU — every CPU policy branch stays OFF) so the folded contract is
        // deterministic everywhere.
        gov.SetCPUSampler(func() (float64, bool) { return 0, false })

        gov.Observe(preflight.Sample{
                At:                timeNowUTC(),
                RAMTotalBytes:     32 << 30,
                RAMAvailableBytes: 16 << 30,
                ProcRSSBytes:      1 << 30,
                Level:             preflight.PressureOK,
        })

        resp, err := http.Get(server.URL + "/api/governor")
        if err != nil {
                t.Fatalf("GET /api/governor: %v", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                t.Fatalf("status = %d, want 200", resp.StatusCode)
        }

        var body map[string]any
        if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
                t.Fatalf("decode: %v", err)
        }

        if body["appVersion"] != config.AppVersion {
                t.Fatalf("appVersion = %v, want %s (release identity contract)", body["appVersion"], config.AppVersion)
        }

        gov2, ok := body["governor"].(map[string]any)
        if !ok || gov2["available"] != true {
                t.Fatalf("governor block must be available: %v", body["governor"])
        }

        for _, key := range []string{"state", "envelope", "selfModel"} {
                if _, ok := gov2[key]; !ok {
                        t.Fatalf("governor block missing %q: %v", key, gov2)
                }
        }

        state, ok := gov2["state"].(map[string]any)
        if !ok {
                t.Fatal("governor state must be an object")
        }

        if state["availableKnown"] != true {
                t.Fatalf("the folded sample's RAM must be reported as known: %v", state)
        }

        if state["ramTotalBytes"].(float64) != float64(32<<30) {
                t.Fatalf("ramTotalBytes = %v, want the folded value", state["ramTotalBytes"])
        }

        envelope, ok := gov2["envelope"].(map[string]any)
        if !ok {
                t.Fatal("envelope must be an object")
        }

        if envelope["admitHeavyweight"] != true {
                t.Fatalf("ok-pressure envelope must admit heavyweight work: %v", envelope)
        }

        if envelope["adjustmentClass"] != string("none") {
                t.Fatalf("ok-pressure envelope class = %v, want none", envelope["adjustmentClass"])
        }

        reasons, ok := envelope["reasons"].([]any)
        if !ok || len(reasons) == 0 {
                t.Fatal("the envelope must always explain itself over the wire")
        }

        selfModel, ok := gov2["selfModel"].(map[string]any)
        if !ok {
                t.Fatal("selfModel must be an object")
        }

        for _, key := range []string{"at", "level", "ram", "process", "engine", "cpu", "activeRuns", "sustained"} {
                if _, ok := selfModel[key]; !ok {
                        t.Fatalf("self-model missing %q: %v", key, selfModel)
                }
        }

        // The composed blocks: existing authorities, real shapes.
        hw, ok := body["hardware"].(map[string]any)
        if !ok {
                t.Fatal("hardware block missing")
        }
        if _, ok := hw["ram"]; !ok {
                t.Fatalf("hardware block must carry the RAM facts: %v", hw)
        }

        if _, ok := body["engine"].(map[string]any); !ok {
                t.Fatal("engine block missing")
        }

        caps, ok := body["capabilities"].(map[string]any)
        if !ok || len(caps) == 0 {
                t.Fatal("capabilities block missing")
        }

        // Every capability value is a REAL bool fact, never a string
        // placeholder.
        for k, v := range caps {
                if _, ok := v.(bool); !ok {
                        t.Fatalf("capability %q = %v (%T), want a bool fact", k, v, v)
                }
        }
}

func TestGovernorEndpointRejectsNonGET(t *testing.T) {
        _, server := newRemoteServerWithHandle(t, "http://127.0.0.1:1")

        resp, err := http.Post(server.URL+"/api/governor", "application/json", nil)
        if err != nil {
                t.Fatalf("POST /api/governor: %v", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusMethodNotAllowed {
                t.Fatalf("POST status = %d, want 405", resp.StatusCode)
        }
}

// timeNowUTC is a tiny local helper so the test does not import time for
// one call site.
func timeNowUTC() time.Time {
        return time.Now().UTC()
}
