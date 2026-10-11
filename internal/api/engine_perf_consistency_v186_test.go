package api

// engine_perf_consistency_v186_test.go — v1.8.6 Phase 2: /api/engine and
// /api/perf must AGREE — one accelerator authority, one evidence path,
// two consuming surfaces.
//
// THE CONTRACT (§5):
//
//   - /api/engine.execution exists and reflects current serving reality
//     WITHOUT any prior /api/perf poll (engine-first polling order);
//   - /api/perf.accelerator and /api/engine.execution carry the SAME
//     verdict (backend, executionVerified, verification);
//   - polling order cannot move the execution stage backwards: engine →
//     perf → engine (unchanged reality) yields a monotone stage;
//   - a stale memo (a resolution computed for different inputs) is
//     refreshed before any consumer can claim it — a poll-only snapshot
//     never claims a current backend.

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

type engineExecBlock struct {
        Execution *struct {
                Stage             string `json:"stage"`
                Backend           string `json:"backend"`
                Device            string `json:"device"`
                ExecutionVerified bool   `json:"executionVerified"`
                Verification      string `json:"verification,omitempty"`
                Gaps              []string `json:"gaps,omitempty"`
        } `json:"execution"`
}

type perfAccelBlock struct {
        Accelerator *struct {
                Requested         string `json:"requested"`
                Backend           string `json:"backend"`
                Selected          string `json:"selected"`
                ExecutionVerified bool   `json:"executionVerified"`
                Verification      string `json:"verification,omitempty"`
        } `json:"accelerator"`
}

func fetchEngineExecution(t *testing.T, serverURL string) *engineExecBlock {
        t.Helper()

        resp, err := http.Get(serverURL + "/api/engine")
        if err != nil {
                t.Fatalf("GET /api/engine: %v", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                t.Fatalf("GET /api/engine: status %d", resp.StatusCode)
        }

        var snap engineExecBlock
        if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
                t.Fatalf("decode /api/engine: %v", err)
        }

        if snap.Execution == nil {
                t.Fatal("the execution/evidence block is missing from /api/engine")
        }

        return &snap
}

func fetchPerfAccelerator(t *testing.T, serverURL string) *perfAccelBlock {
        t.Helper()

        resp, err := http.Get(serverURL + "/api/perf")
        if err != nil {
                t.Fatalf("GET /api/perf: %v", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                t.Fatalf("GET /api/perf: status %d", resp.StatusCode)
        }

        var payload perfAccelBlock
        if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
                t.Fatalf("decode /api/perf: %v", err)
        }

        if payload.Accelerator == nil {
                t.Fatal("the accelerator block is missing from /api/perf")
        }

        return &payload
}

// TestEngineExecutionBlockIndependentOfPerfPoll: the engine surface
// composes the ladder on ITS OWN poll (engine-first order) — the v1.8.5
// dependency on the perf page having polled first is gone.
func TestEngineExecutionBlockIndependentOfPerfPoll(t *testing.T) {
        server, _ := newTestServer(t)

        exec := fetchEngineExecution(t, server.URL)

        // No engine binary, no GPU evidence: the honest ladder is a low
        // stage with named gaps — never verified, never fabricated.
        if exec.Execution.ExecutionVerified {
                t.Fatal("no evidence exists in tests — executionVerified must be false")
        }

        if exec.Execution.Stage == "" {
                t.Fatal("the execution stage must always be reported")
        }
}

// TestEngineAndPerfAgreeOnTheAcceleratorVerdict: both surfaces read the
// ONE authority. The verdicts are DIFFERENT claims by design:
//
//   - perf.accelerator.executionVerified answers "is the SELECTED
//     backend verified" (CPU is verified by definition of execution);
//   - engine.execution.executionVerified answers "has the SERVING
//     ladder reached the verified rung" (requires backend health, a
//     verified model, real generations and execution evidence).
//
// Agreement therefore means: the engine's verification text/plan comes
// from the SAME resolution the perf surface served, and a verified
// execution ladder ALWAYS implies a verified selection (never the
// reverse). Poll order must not matter.
func TestEngineAndPerfAgreeOnTheAcceleratorVerdict(t *testing.T) {
        server, _ := newTestServer(t)

        // Engine FIRST, then perf.
        exec := fetchEngineExecution(t, server.URL)
        perf := fetchPerfAccelerator(t, server.URL)

        // Directional implication: a verified serving ladder requires a
        // verified selection (the selection authority's contract feeds the
        // ladder's rung 6/7 inputs).
        if exec.Execution.ExecutionVerified && !perf.Accelerator.ExecutionVerified {
                t.Fatalf("verdict contradiction: engine execution verified while the selection authority is unverified")
        }

        // The engine block carries the authority's verification plan verbatim
        // (same resolution object — same how/plan text).
        if exec.Execution.Verification != "" && exec.Execution.Verification != perf.Accelerator.Verification {
                t.Fatalf("verification plan disagreement: engine=%q vs perf=%q",
                        exec.Execution.Verification, perf.Accelerator.Verification)
        }

        // Poll in the REVERSE order too — same answer.
        perf2 := fetchPerfAccelerator(t, server.URL)
        exec2 := fetchEngineExecution(t, server.URL)

        if perf2.Accelerator.Verification != perf.Accelerator.Verification {
                t.Fatalf("perf resolution changed across polls without a reality change: %q vs %q",
                        perf.Accelerator.Verification, perf2.Accelerator.Verification)
        }

        if exec2.Execution.ExecutionVerified && !perf2.Accelerator.ExecutionVerified {
                t.Fatalf("reverse-order contradiction: engine=%t while perf=%t",
                        exec2.Execution.ExecutionVerified, perf2.Accelerator.ExecutionVerified)
        }
}

// TestExecutionStageMonotoneAcrossPollOrder: with unchanged serving
// reality, a later poll (whichever surface) can never move the stage
// backwards.
func TestExecutionStageMonotoneAcrossPollOrder(t *testing.T) {
        server, _ := newTestServer(t)

        first := fetchEngineExecution(t, server.URL)
        _ = fetchPerfAccelerator(t, server.URL)
        second := fetchEngineExecution(t, server.URL)

        rank := map[string]int{
                "none": 0, "detected": 1, "backend-available": 2, "device-selected": 3,
                "model-loaded": 4, "generation-executed": 5, "execution-evidence": 6, "verified": 7,
        }

        if rank[second.Execution.Stage] < rank[first.Execution.Stage] {
                t.Fatalf("the execution stage moved BACKWARDS across polls (%s → %s) with unchanged reality — poll-order artifact",
                        first.Execution.Stage, second.Execution.Stage)
        }
}

// TestStaleMemoCannotClaimCurrentBackend: a memo written for OTHER
// inputs (a forged GPU verdict) must not survive the signature check —
// the next consumer recomputes and serves the truthful CPU resolution.
func TestStaleMemoCannotClaimCurrentBackend(t *testing.T) {
        srv, server := newLocalServerWithHandle(t)

        // Forge a stale memo: a GPU_VULKAN verified resolution with a bogus
        // signature (as if computed for another engine/profile/model).
        srv.resolutionMu.Lock()
        srv.lastResolution = forgeGPUResolution()
        srv.resolutionSig = "stale-signature-from-another-reality"
        srv.resolutionMu.Unlock()

        exec := fetchEngineExecution(t, server.URL)

        // The stale GPU verdict must NOT be served: on the no-evidence test
        // machine the truthful selection is CPU.
        if exec.Execution.Device == "GPU_VULKAN" && exec.Execution.ExecutionVerified {
                t.Fatal("a stale memo claimed a verified GPU_VULKAN backend — the signature check failed")
        }

        // And the memo was refreshed to the current reality.
        srv.resolutionMu.Lock()
        sig := srv.resolutionSig
        srv.resolutionMu.Unlock()

        if sig == "stale-signature-from-another-reality" {
                t.Fatal("the stale memo was never refreshed")
        }
}

// TestMemoFreshWhenSignatureMatches: a fresh memo is reused verbatim
// (the read-through cache contract) — the resolution object identity is
// stable across consumers.
func TestMemoFreshWhenSignatureMatches(t *testing.T) {
        srv, server := newLocalServerWithHandle(t)

        // First poll computes + memos.
        _ = fetchPerfAccelerator(t, server.URL)

        srv.resolutionMu.Lock()
        first := srv.lastResolution
        sig := srv.resolutionSig
        srv.resolutionMu.Unlock()

        if first == nil {
                t.Fatal("the memo was not written by the perf path")
        }

        // Unchanged reality → the engine poll reuses the SAME memo value.
        _ = fetchEngineExecution(t, server.URL)

        srv.resolutionMu.Lock()
        second := srv.lastResolution
        sig2 := srv.resolutionSig
        srv.resolutionMu.Unlock()

        if sig2 != sig {
                t.Fatalf("the signature changed without a reality change: %q → %q", sig, sig2)
        }

        if second == nil || second.Backend != first.Backend ||
                second.ExecutionVerified != first.ExecutionVerified {
                t.Fatal("the fresh memo was not reused verbatim")
        }
}

// newLocalServerWithHandle mirrors newTestServer but also returns the
// internal *Server (for memo inspection).
func newLocalServerWithHandle(t *testing.T) (*Server, *httptest.Server) {
        t.Helper()

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.ModelsDir = cfg.DataDir + "/models"
        cfg.SessionsDir = cfg.DataDir + "/sessions"
        cfg.Host = "127.0.0.1"
        cfg.Port = 0
        cfg.Provider = "local"
        cfg.LlamaAutoStart = false
        cfg.UpdateSchedule = "off"

        srv, err := New(cfg)
        if err != nil {
                t.Fatalf("api.New: %v", err)
        }

        if err := srv.EnsureSetup(); err != nil {
                t.Fatalf("EnsureSetup: %v", err)
        }

        t.Cleanup(srv.Close)

        server := httptest.NewServer(srv.Handler())
        t.Cleanup(server.Close)

        return srv, server
}

// forgeGPUResolution builds a FAKE verified GPU resolution (the stale
// memo fixture — it must never survive a signature check).
func forgeGPUResolution() *accelerator.Resolution {
        res := accelerator.Resolution{
                Requested:         accelerator.RequestAuto,
                Backend:           accelerator.KindGPUVulkan,
                Selected:          accelerator.KindGPUVulkan,
                Device:            "Intel(R) Arc(TM) A770M Graphics",
                AutoProfile:       accelerator.ProfileInteractiveGPU,
                GPULayers:         -1,
                Reason:            "forged fixture",
                Available:         []accelerator.Kind{accelerator.KindCPU, accelerator.KindGPUVulkan},
                ExecutionVerified: true,
                Verification:      accelerator.VerificationOffloadLog,
                Fallback:          "none",
        }
        return &res
}
