package accelerator

// v161_contract_test.go — v1.6.1: the explicit-request honesty contract.
//
// "Explicit VULKAN must not silently fall back to CPU" cuts BOTH ways:
//
//   - when the evidence PROVES a Vulkan device, an explicit GPU request
//     selects GPU_VULKAN with executionVerified=true;
//   - when it does NOT, the CPU fallback is DOCUMENTED — the resolution
//     carries the requested profile, the refusal reason and the fallback
//     chain, so no surface can render "GPU" while silently serving CPU;
//   - AUTO selects Vulkan ONLY on runtime evidence (enumeration or a
//     measured offload line) — never on DLL presence alone.

import (
        "strings"
        "testing"
)

func TestExplicitGPURequestWithProvenVulkanDeviceIsVerified(t *testing.T) {
        res := Resolve(RequestGPU, intelArcEvidence())

        if res.Backend != KindGPUVulkan || res.Selected != KindGPUVulkan {
                t.Fatalf("explicit GPU with an enumerated Vulkan device must select GPU_VULKAN, got %s", res.Backend)
        }

        // v1.8.6 TRUTH MODEL: an enumerated device SELECTS for an explicit GPU
        // request, but --list-devices is selection/provisioning evidence, NOT
        // execution proof — the selection stays unverified with the CPU
        // safety net until a measured offload line or execution receipt
        // exists (see execution_truth_v186_test.go for the verified rungs).
        if res.ExecutionVerified {
                t.Fatal("engine enumeration is NOT runtime execution evidence — executionVerified must be false")
        }

        if res.Fallback != "safe" {
                t.Fatalf("the unverified selection must declare the CPU safety net, got %q", res.Fallback)
        }

        if !strings.Contains(res.Verification, "pending") {
                t.Fatalf("verification must state the pending execution plan: %q", res.Verification)
        }
}

func TestExplicitGPURequestWithoutEvidenceFallsBackLoudly(t *testing.T) {
        res := Resolve(RequestGPU, Evidence{Workload: WorkloadChat})

        if res.Backend != KindCPU {
                t.Fatalf("no evidence → CPU (the safe fallback), got %s", res.Backend)
        }

        // LOUD, not silent: the resolution says what was requested, that it
        // was refused, and why.
        if res.Requested != RequestGPU {
                t.Fatalf("the requested profile must stay visible, got %s", res.Requested)
        }
        if !strings.Contains(res.Reason, "no accelerator proven usable") {
                t.Fatalf("the refusal reason must be explicit: %q", res.Reason)
        }
        if len(res.Fallbacks) == 0 {
                t.Fatal("the failed GPU gate must be recorded in the fallback chain")
        }
        if !strings.Contains(strings.Join(res.Fallbacks, "; "), "no enumerated GPU device") {
                t.Fatalf("fallback chain must name the GPU refusal: %v", res.Fallbacks)
        }

        // The CPU fallback itself is verified (by definition) — but the
        // REQUEST was GPU: any UI rendering this resolution shows both facts.
        if !res.ExecutionVerified {
                t.Fatal("CPU execution is definitionally verified")
        }
}

func TestAutoSelectsVulkanOnlyWithRuntimeEvidence(t *testing.T) {
        // AUTO + DLL presence alone: selectable but explicitly UNVERIFIED —
        // never rendered as GPU support.
        dllOnly := Evidence{
                EnumerationSupported: false,
                VulkanBackendPresent: true,
                Workload:             WorkloadChat,
        }

        res := Resolve(RequestAuto, dllOnly)
        if res.Backend != KindGPUVulkan {
                t.Fatalf("documented weaker fallback selects GPU_VULKAN, got %s", res.Backend)
        }
        if res.ExecutionVerified {
                t.Fatal("DLL presence is NOT runtime evidence — executionVerified must be false")
        }
        if res.Fallback != "safe" {
                t.Fatalf("the unverified selection must declare the CPU safety net, got %q", res.Fallback)
        }
        if !strings.Contains(res.Verification, "pending") {
                t.Fatalf("verification must state the pending plan: %q", res.Verification)
        }

        // AUTO + zero evidence: CPU, honestly.
        res = Resolve(RequestAuto, Evidence{Workload: WorkloadChat})
        if res.Backend != KindCPU || res.AutoProfile != ProfileCPUSafe {
                t.Fatalf("AUTO without any evidence must resolve CPU_SAFE, got %s/%s", res.Backend, res.AutoProfile)
        }
        if !res.ExecutionVerified {
                t.Fatal("CPU execution is definitionally verified")
        }

        // AUTO + runtime offload evidence: verified GPU.
        res = Resolve(RequestAuto, Evidence{
                RuntimeOffloadEvidence: "offloaded 33/33 layers to GPU",
                Workload:               WorkloadChat,
        })
        if res.Backend != KindGPUVulkan || !res.ExecutionVerified {
                t.Fatalf("AUTO with a measured offload line must select verified GPU_VULKAN, got %s (verified=%t)",
                        res.Backend, res.ExecutionVerified)
        }
}

func TestDescribeNeverHidesTheRequestedProfile(t *testing.T) {
        res := Resolve(RequestGPU, Evidence{Workload: WorkloadChat})

        d := res.Describe()

        // The one-line summary carries the honest story: what was asked, what
        // was selected, whether execution is proven, and why.
        for _, want := range []string{"requested=GPU", "selected=CPU", "executionVerified=true"} {
                if !strings.Contains(d, want) {
                        t.Fatalf("Describe must carry %q, got: %s", want, d)
                }
        }
}
