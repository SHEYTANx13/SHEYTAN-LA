package llm

// gpu_activation_v186_test.go — v1.8.6 Phase 2: the launcher's GPU
// activation contract (§4 REMOVE PREMATURE GPU ACTIVATION).
//
// THE CONTRACT:
//
//   - enumeration alone (or DLL presence) NEVER turns on
//     --n-gpu-layers in a NORMAL serving launch — AUTO stays CPU-safe
//     until execution is proven;
//   - a current-boot measured offload line DOES (per-boot proof);
//   - a valid persisted execution receipt DOES (transaction proof,
//     identity-checked);
//   - a stale receipt (variant/engine mismatch, failed probe) does NOT;
//   - a candidate verification transaction MAY enable offload from
//     selection evidence (the proving mode) — the transaction's verify
//     hook still requires the offload line before commit;
//   - the CPU-forced profile stays CPU;
//   - manual user configuration (NumGPU > 0) is untouched;
//   - the offload evidence is PER-BOOT: a relaunch cannot inherit the
//     previous boot's line.

import (
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func gpuActivationEnv(t *testing.T) (*LlamaServer, *config.Config, string) {
        t.Helper()

        bin := fakeEngineBinary(t, "one")

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.LlamaBinPath = bin
        cfg.GPUAutoOffload = true
        cfg.LLM.NumGPU = 0
        cfg.LlamaHost = "127.0.0.1"
        cfg.LlamaPort = 1

        model := filepath.Join(t.TempDir(), "model.gguf")
        if err := os.WriteFile(model, []byte("x"), 0o644); err != nil {
                t.Fatalf("write model: %v", err)
        }

        srv := NewLlamaServer(config.NewSource(cfg))
        return srv, cfg, model
}

func argsHaveGPULayers(args []string) bool {
        for i := 0; i < len(args)-1; i++ {
                if args[i] == "--n-gpu-layers" && args[i+1] != "0" {
                        return true
                }
        }
        return false
}

// TestEnumerationAloneDoesNotActivateGPULayers is THE Phase 2 §4
// regression: the fake engine enumerates a Vulkan device, no execution
// evidence exists — the launcher must stay CPU-safe.
func TestEnumerationAloneDoesNotActivateGPULayers(t *testing.T) {
        srv, _, model := gpuActivationEnv(t)

        if srv.AutoGPUOffloadForTest() {
                t.Fatal("enumeration alone must not enable auto GPU offload (detection is not execution)")
        }

        args := srv.BuildArgsForTest(model, 1)
        if argsHaveGPULayers(args) {
                t.Fatalf("--n-gpu-layers activated from enumeration alone: %v", args)
        }
}

// TestDLLPresenceAloneDoesNotActivateGPULayers: the weakest posture stays
// off (the fake engine enumerates, which is already STRONGER than a DLL;
// both are selection evidence).
func TestDLLPresenceAloneDoesNotActivateGPULayers(t *testing.T) {
        srv, _, model := gpuActivationEnv(t)

        // No enumeration cache, no offload evidence, no receipt: the binary
        // exists (that is all).
        if srv.AutoGPUOffloadForTest() {
                t.Fatal("binary presence must not enable auto GPU offload")
        }

        if argsHaveGPULayers(srv.BuildArgsForTest(model, 1)) {
                t.Fatal("--n-gpu-layers activated without any evidence")
        }
}

// TestOffloadLineActivatesGPULayers: the current boot's measured line is
// real execution proof — the launcher may enable offload.
func TestOffloadLineActivatesGPULayers(t *testing.T) {
        srv, _, model := gpuActivationEnv(t)

        srv.ObserveEngineLine("llm_load_tensors: offloaded 33/33 layers to GPU")

        if !srv.AutoGPUOffloadForTest() {
                t.Fatal("a current-boot measured offload line must enable auto GPU offload")
        }

        if !argsHaveGPULayers(srv.BuildArgsForTest(model, 1)) {
                t.Fatal("--n-gpu-layers missing although execution is proven by the offload line")
        }
}

// TestVerifiedReceiptActivatesGPULayers: a persisted, identity-matching
// verified GPU probe receipt enables offload (the committed transaction
// outcome).
func TestVerifiedReceiptActivatesGPULayers(t *testing.T) {
        srv, cfg, model := gpuActivationEnv(t)

        // The fake install has no manifest: InstalledEngineTag is "" (unknown)
        // and InstalledEngineVariant is VariantCPU — a receipt without a
        // variant claim stays valid for the unknown identity.
        st := GPUProbeState{
                Status:    GPUProbeStatusVerified,
                Identity:  "test-identity",
                EngineTag: "",
                Variant:   "",
                Device:    "Vulkan0: fake",
                Evidence:  "offloaded 33/33 layers to GPU",
                Model:     "model.gguf",
                Reason:    "verified fixture",
                At:        "2026-10-05T00:00:00Z",
        }
        saveGPUProbeState(cfg, st)

        if !srv.AutoGPUOffloadForTest() {
                t.Fatal("a valid persisted execution receipt must enable auto GPU offload")
        }

        if !argsHaveGPULayers(srv.BuildArgsForTest(model, 1)) {
                t.Fatal("--n-gpu-layers missing although a valid receipt proves execution")
        }
}

// TestFailedReceiptDoesNotActivateGPULayers: a bounded FAILED probe is
// negative evidence — never a launch posture.
func TestFailedReceiptDoesNotActivateGPULayers(t *testing.T) {
        srv, cfg, model := gpuActivationEnv(t)

        saveGPUProbeState(cfg, GPUProbeState{
                Status:   GPUProbeStatusFailed,
                Identity: "test-identity",
                Reason:   "no offload line",
        })

        if srv.AutoGPUOffloadForTest() {
                t.Fatal("a failed probe receipt must never enable auto GPU offload")
        }

        if argsHaveGPULayers(srv.BuildArgsForTest(model, 1)) {
                t.Fatal("--n-gpu-layers activated from a FAILED probe")
        }
}

// TestCPUForcedProfileStaysCPU: the requested CPU profile is honored by
// the launcher even with proven execution available.
func TestCPUForcedProfileStaysCPU(t *testing.T) {
        srv, cfg, model := gpuActivationEnv(t)

        cfg.Accelerator = "CPU"
        srv.ObserveEngineLine("llm_load_tensors: offloaded 33/33 layers to GPU")

        if srv.AutoGPUOffloadForTest() {
                t.Fatal("the CPU-forced profile must keep the launcher on CPU")
        }

        if argsHaveGPULayers(srv.BuildArgsForTest(model, 1)) {
                t.Fatal("--n-gpu-layers activated under the forced CPU profile")
        }
}

// TestManualNumGPUIsRespected: manual user GPU configuration is applied
// verbatim — the proving model never overrides a user decision.
func TestManualNumGPUIsRespected(t *testing.T) {
        srv, cfg, model := gpuActivationEnv(t)

        cfg.LLM.NumGPU = 17

        args := srv.BuildArgsForTest(model, 1)

        found := false
        for i := 0; i < len(args)-1; i++ {
                if args[i] == "--n-gpu-layers" && args[i+1] == "17" {
                        found = true
                }
        }

        if !found {
                t.Fatalf("manual --n-gpu-layers 17 missing from launch args: %v", args)
        }
}

// TestCandidateProvingModeUsesSelectionEvidence: inside the bounded
// candidate verification transaction, enumeration IS allowed to enable
// offload — the transaction's verify hook still requires the offload
// line before commit (proven by variant_auto_v172_test.go).
func TestCandidateProvingModeUsesSelectionEvidence(t *testing.T) {
        srv, _, _ := gpuActivationEnv(t)

        srv.mu.Lock()
        srv.gpuCandidateProving = true
        srv.mu.Unlock()

        defer func() {
                srv.mu.Lock()
                srv.gpuCandidateProving = false
                srv.mu.Unlock()
        }()

        if !srv.AutoGPUOffloadForTest() {
                t.Fatal("the candidate proving mode may use enumeration as selection evidence")
        }
}

// TestOffloadEvidenceIsPerBoot: the evidence resets on relaunch — a new
// boot cannot inherit the previous boot's line. launchArgs performs the
// reset; the test drives it directly (the spawn itself is covered by the
// lifecycle suites).
func TestOffloadEvidenceIsPerBoot(t *testing.T) {
        srv, cfg, model := gpuActivationEnv(t)

        srv.ObserveEngineLine("llm_load_tensors: offloaded 33/33 layers to GPU")

        if srv.OffloadEvidence() == "" {
                t.Fatal("setup: the offload line must be observed")
        }

        // A relaunch resets the per-boot evidence before the new process can
        // print its own (launchArgs owns the reset; the fake binary fails to
        // become healthy, which is fine — the reset happens first).
        _ = srv.launchArgs(cfg, "definitely-not-a-binary", model, []string{"--version"})

        if srv.OffloadEvidence() != "" {
                t.Fatal("the offload evidence survived a relaunch — evidence must be per-boot")
        }
}

// TestGPUActivationLogsAreHonest: the CPU-safe refusal names the truth
// (selection evidence is not execution proof) — no silent CPU posture.
func TestGPUActivationLogsAreHonest(t *testing.T) {
        srv, _, _ := gpuActivationEnv(t)

        // Just exercise the paths — the log content discipline is covered by
        // the log suite; here we require no panic and a stable verdict.
        verdict := srv.AutoGPUOffloadForTest()
        if verdict {
                t.Fatal("no evidence → no offload")
        }

        srv.ObserveEngineLine("llm_load_tensors: offloaded 33/33 layers to GPU")
        if !srv.AutoGPUOffloadForTest() {
                t.Fatal("evidence → offload")
        }

        if !strings.Contains(srv.OffloadEvidence(), "offloaded 33/33") {
                t.Fatalf("offload evidence = %q", srv.OffloadEvidence())
        }
}
