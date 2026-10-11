package accelerator

// accelerator_test.go — v1.2.6 tests for the evidence-based resolver.
//
// The contract under test is the PROHIBITION: no accelerator is claimed
// without measured evidence. Fixtures model the real evidence shapes:
//   - an Intel Arc + AI Boost machine with a modern llama.cpp build
//     (enumeration supported);
//   - an older build without --list-devices (fallback ladder);
//   - a CPU-only machine;
//   - an NPU machine where every NPU gate passes (and where gates fail).

import (
        "strings"
        "testing"
)

func intelArcEvidence() Evidence {
        return Evidence{
                EngineDevices: []Device{
                        {Backend: "Vulkan0", Name: "Intel(R) Arc(TM) A770M Graphics", TotalMB: 16384, Source: "engine-enumeration"},
                },
                EnumerationSupported: true,
                NPUPresent:           true,
                NPUName:              "Intel(R) AI Boost",
                OpenVINO:             OpenVINOStatus{RuntimePresent: false, Detail: "not installed"},
                Model:                ModelFacts{Architecture: "llama", Quantization: "Q4_0", ContextLimit: 32768},
                AvailableRAMMB:       32505,
                Workload:             WorkloadChat,
        }
}

func TestResolveGPURequiresEngineEnumerationNotDLL(t *testing.T) {
        ev := intelArcEvidence()

        res := Resolve(RequestAuto, ev)

        if res.Backend != KindGPUVulkan {
                t.Fatalf("backend = %s, want GPU_VULKAN (engine enumerated the Arc device)", res.Backend)
        }

        if res.Device != "Intel(R) Arc(TM) A770M Graphics" {
                t.Fatalf("device = %q", res.Device)
        }

        if res.AutoProfile != ProfileInteractiveGPU {
                t.Fatalf("auto profile = %s, want INTERACTIVE_GPU for chat", res.AutoProfile)
        }

        if !strings.Contains(res.Reason, "enumerated") {
                t.Fatalf("reason must cite the enumeration evidence: %q", res.Reason)
        }
}

func TestResolveDLLAloneIsNotGPUEvidence(t *testing.T) {
        // v1.2.5's exact bug: Vulkan DLL present, engine build DOES support
        // enumeration and enumerates ZERO devices. The DLL must NOT win.
        ev := Evidence{
                EngineDevices:        nil,
                EnumerationSupported: true,
                VulkanBackendPresent: true, // the DLL exists...
                Workload:             WorkloadChat,
        }

        res := Resolve(RequestAuto, ev)

        if res.Backend != KindCPU {
                t.Fatalf("backend = %s, want CPU — a Vulkan DLL with zero enumerated devices is not GPU inference", res.Backend)
        }

        if !strings.Contains(res.Reason, "no accelerator") && !strings.Contains(res.Reason, "no enumerated") {
                t.Fatalf("reason must explain the CPU fallback: %q", res.Reason)
        }
}

func TestResolveEnumerationUnsupportedFallsBackWithReason(t *testing.T) {
        // An OLDER engine build: --list-devices unsupported, Vulkan backend
        // present. The documented weaker fallback applies — GPU_VULKAN with
        // the verification caveat recorded in the reason.
        ev := Evidence{
                EnumerationSupported: false,
                VulkanBackendPresent: true,
                Workload:             WorkloadChat,
        }

        res := Resolve(RequestAuto, ev)

        if res.Backend != KindGPUVulkan {
                t.Fatalf("backend = %s, want GPU_VULKAN via the documented fallback", res.Backend)
        }

        if !strings.Contains(res.Reason, "verif") {
                t.Fatalf("fallback reason must state the verification plan: %q", res.Reason)
        }
}

func TestResolveRuntimeOffloadLogBeatsEverything(t *testing.T) {
        // The engine log measured real offload — the strongest GPU evidence.
        ev := Evidence{
                EnumerationSupported:   true,
                RuntimeOffloadEvidence: "offloaded 33/33 layers to GPU",
                Workload:               WorkloadChat,
        }

        res := Resolve(RequestAuto, ev)

        if res.Backend != KindGPUVulkan {
                t.Fatalf("backend = %s, want GPU_VULKAN from the runtime log evidence", res.Backend)
        }

        if !strings.Contains(res.Reason, "offloaded 33/33") {
                t.Fatalf("reason must cite the measured offload line: %q", res.Reason)
        }
}

func TestResolveNPURefusedWithoutEveryGate(t *testing.T) {
        // NPU present + OpenVINO present, but NO benchmark → refused.
        ev := intelArcEvidence()
        ev.OpenVINO = OpenVINOStatus{RuntimePresent: true, Detail: "loadable"}
        ev.NPUBenchmarkTokPerSec = 0 // never measured

        res := Resolve(RequestAuto, ev)

        if res.Backend == KindNPUOpenVINO {
                t.Fatal("NPU selected without a measured benchmark — presence alone must never win")
        }

        // The fallback record must carry the failed gate.
        foundGate := false
        for _, fb := range res.Fallbacks {
                if strings.Contains(fb, "benchmark") {
                        foundGate = true
                }
        }

        if !foundGate {
                t.Fatalf("fallbacks must record the benchmark gate failure: %v", res.Fallbacks)
        }
}

func TestResolveNPUWinsOnlyWithFullEvidence(t *testing.T) {
        ev := intelArcEvidence()
        ev.OpenVINO = OpenVINOStatus{RuntimePresent: true, Detail: "loadable"}
        ev.NPUBenchmarkTokPerSec = 42.0
        ev.GPUBenchmarkTokPerSec = 25.0 // measured NPU beats measured GPU

        res := Resolve(RequestAuto, ev)

        if res.Backend != KindNPUOpenVINO {
                t.Fatalf("backend = %s, want NPU_OPENVINO (every gate passed AND the benchmark won)", res.Backend)
        }

        if res.AutoProfile != ProfileLowPowerNPU {
                t.Fatalf("auto profile = %s, want LOW_POWER_NPU", res.AutoProfile)
        }

        if res.Device != "Intel(R) AI Boost" {
                t.Fatalf("device = %q", res.Device)
        }
}

func TestResolveNPURefusedWhenGPUThroughputHigher(t *testing.T) {
        ev := intelArcEvidence()
        ev.OpenVINO = OpenVINOStatus{RuntimePresent: true, Detail: "loadable"}
        ev.NPUBenchmarkTokPerSec = 10.0
        ev.GPUBenchmarkTokPerSec = 60.0 // GPU wins on measurement

        res := Resolve(RequestAuto, ev)

        if res.Backend != KindGPUVulkan {
                t.Fatalf("backend = %s, want GPU_VULKAN (measured GPU throughput is higher)", res.Backend)
        }
}

func TestResolveNPUModelGates(t *testing.T) {
        ev := intelArcEvidence()
        ev.OpenVINO = OpenVINOStatus{RuntimePresent: true, Detail: "loadable"}
        ev.NPUBenchmarkTokPerSec = 42.0
        ev.GPUBenchmarkTokPerSec = 25.0

        // Quantization gate: Q4_K_M (K-quant) is not in the measured set.
        ev.Model.Quantization = "Q4_K_M"

        if res := Resolve(RequestAuto, ev); res.Backend != KindGPUVulkan {
                t.Fatalf("K-quant model must not select NPU, got %s", res.Backend)
        }

        // Architecture gate: mamba is not in the measured set.
        ev.Model.Quantization = "Q4_0"
        ev.Model.Architecture = "mamba"

        if res := Resolve(RequestAuto, ev); res.Backend != KindGPUVulkan {
                t.Fatalf("mamba model must not select NPU, got %s", res.Backend)
        }
}

func TestResolveForcedProfiles(t *testing.T) {
        if res := Resolve(RequestCPU, intelArcEvidence()); res.Backend != KindCPU {
                t.Fatalf("forced CPU ignored: %s", res.Backend)
        }

        // Forced GPU with zero evidence still falls back to CPU — an explicit
        // request cannot conjure hardware that was never measured.
        ev := Evidence{Workload: WorkloadChat}

        if res := Resolve(RequestGPU, ev); res.Backend != KindCPU {
                t.Fatalf("forced GPU without evidence must fall back to CPU, got %s", res.Backend)
        }
}

func TestResolveWorkloadProfiles(t *testing.T) {
        ev := intelArcEvidence()
        ev.Workload = WorkloadVision

        if res := Resolve(RequestAuto, ev); res.AutoProfile != ProfileVisionGPU {
                t.Fatalf("vision workload profile = %s, want VISION_GPU", res.AutoProfile)
        }

        ev.Workload = WorkloadLong

        if res := Resolve(RequestAuto, ev); res.AutoProfile != ProfileMaximum {
                t.Fatalf("long-prompt workload profile = %s, want MAXIMUM", res.AutoProfile)
        }
}

func TestNormalizeRequested(t *testing.T) {
        for in, want := range map[string]Requested{
                "":      RequestAuto,
                "auto":  RequestAuto,
                "AUTO":  RequestAuto,
                " gpu ": RequestGPU,
                "NPU":   RequestNPU,
                "cpu":   RequestCPU,
                "bogus": RequestAuto,
        } {
                if got := NormalizeRequested(in); got != want {
                        t.Fatalf("NormalizeRequested(%q) = %s, want %s", in, got, want)
                }
        }
}

// --- v1.2.6 continuation: EXPLICIT EVIDENCE STATE --------------------------
//
// The explainable contract: selected / available / executionVerified /
// verification / fallback. "Vulkan DLL exists" must never render as
// "GPU execution verified" — the two claims are different fields.

func TestEvidenceStateEngineEnumerationVerified(t *testing.T) {
        res := Resolve(RequestAuto, intelArcEvidence())

        if res.Selected != KindGPUVulkan || res.Backend != KindGPUVulkan {
                t.Fatalf("selected = %s, want GPU_VULKAN", res.Selected)
        }

        // v1.8.6 TRUTH MODEL: enumeration is SELECTION evidence, never
        // execution proof. --list-devices lists devices; it offloads nothing.
        if res.ExecutionVerified {
                t.Fatal("engine enumeration is NOT runtime execution evidence — executionVerified must be false (v1.8.6: detected ≠ executed)")
        }

        if res.Verification != VerificationPendingExecution {
                t.Fatalf("verification = %q, want the pending-execution plan", res.Verification)
        }

        if res.Fallback != "safe" {
                t.Fatalf("fallback = %q, want safe (CPU is the guaranteed path while unverified)", res.Fallback)
        }

        // The explainable Describe() line carries the evidence state.
        if !strings.Contains(res.Describe(), "executionVerified=false") {
                t.Fatalf("Describe() = %q — the evidence state is not explainable", res.Describe())
        }
}

func TestEvidenceStateOffloadLogVerified(t *testing.T) {
        ev := Evidence{
                RuntimeOffloadEvidence: "offloaded 33/33 layers to GPU",
                Workload:               WorkloadChat,
        }

        res := Resolve(RequestAuto, ev)

        if res.Backend != KindGPUVulkan || !res.ExecutionVerified {
                t.Fatal("the measured offload line is runtime execution evidence — must be verified GPU")
        }

        if res.Verification != VerificationOffloadLog {
                t.Fatalf("verification = %q, want the offload-log evidence", res.Verification)
        }
}

func TestEvidenceStateDLLFallbackSelectedButUnverified(t *testing.T) {
        ev := Evidence{
                EnumerationSupported: false,
                VulkanBackendPresent: true,
                Workload:             WorkloadChat,
        }

        res := Resolve(RequestAuto, ev)

        // The documented weaker fallback still SELECTS GPU — but it is
        // explicitly NOT verified, with the verification plan stated and the
        // safe fallback declared.
        if res.Backend != KindGPUVulkan {
                t.Fatalf("backend = %s, want the documented GPU fallback", res.Backend)
        }

        if res.ExecutionVerified {
                t.Fatal("Vulkan DLL presence is NOT execution evidence — executionVerified must be false")
        }

        if res.Verification != VerificationPendingLog {
                t.Fatalf("verification = %q, want the pending-log verification plan", res.Verification)
        }

        if res.Fallback != "safe" {
                t.Fatalf("fallback = %q, want safe (CPU is the guaranteed path)", res.Fallback)
        }

        if !strings.Contains(res.Describe(), "executionVerified=false") {
                t.Fatalf("Describe() = %q — the unverified state is not visible", res.Describe())
        }
}

func TestEvidenceStateCPUAlwaysVerified(t *testing.T) {
        // Zero evidence → CPU_SAFE, and CPU is definitionally verified.
        res := Resolve(RequestAuto, Evidence{})

        if res.Backend != KindCPU || res.Selected != KindCPU {
                t.Fatalf("selected = %s, want CPU under zero evidence", res.Selected)
        }

        if !res.ExecutionVerified {
                t.Fatal("CPU selection must be execution-verified (it is the definitionally available path)")
        }

        if res.Verification != VerificationCPUExecution {
                t.Fatalf("verification = %q, want the CPU execution statement", res.Verification)
        }

        // Forced CPU is equally verified.
        forced := Resolve(RequestCPU, intelArcEvidence())

        if forced.Backend != KindCPU || !forced.ExecutionVerified {
                t.Fatal("forced CPU must be verified CPU")
        }
}

func TestEvidenceStateAvailableList(t *testing.T) {
        // CPU is always available; the enumerated Arc adds GPU_VULKAN; the
        // detected NPU adds NPU_OPENVINO (availability ≠ selection ≠ verified).
        res := Resolve(RequestAuto, intelArcEvidence())

        if len(res.Available) < 3 {
                t.Fatalf("available = %v — CPU, GPU_VULKAN and NPU_OPENVINO must all be listed", res.Available)
        }

        has := func(kind Kind) bool {
                for _, k := range res.Available {
                        if k == kind {
                                return true
                        }
                }
                return false
        }

        if !has(KindCPU) || !has(KindGPUVulkan) || !has(KindNPUOpenVINO) {
                t.Fatalf("available list incomplete: %v", res.Available)
        }

        // Zero evidence: CPU only.
        empty := Resolve(RequestAuto, Evidence{})

        if len(empty.Available) != 1 || empty.Available[0] != KindCPU {
                t.Fatalf("zero-evidence available = %v, want [CPU] only", empty.Available)
        }
}

func TestEvidenceStateNPUVerifiedOnlyThroughFullGates(t *testing.T) {
        ev := intelArcEvidence()
        ev.OpenVINO = OpenVINOStatus{RuntimePresent: true, Detail: "loader found"}
        ev.NPUBenchmarkTokPerSec = 12.5
        ev.GPUBenchmarkTokPerSec = 8.0

        res := Resolve(RequestNPU, ev)

        if res.Backend != KindNPUOpenVINO {
                t.Fatalf("backend = %s, want NPU_OPENVINO (every gate passed)", res.Backend)
        }

        if !res.ExecutionVerified || res.Verification != VerificationNPUBench {
                t.Fatalf("NPU selection must carry the benchmark verification: verified=%t verification=%q", res.ExecutionVerified, res.Verification)
        }
}
