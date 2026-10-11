package accelerator

// execution_truth_v186_test.go — v1.8.6 Phase 2: the execution-truth
// contract for the ONE accelerator authority.
//
// THE INVARIANT under test (the Phase 2 P0):
//
//      GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU verified
//
// Concretely:
//
//   1. enumeration alone NEVER produces verified GPU execution (it
//      selects, with a pending-execution verification plan and the CPU
//      safety net);
//   2. a runtime offload line (real execution) DOES verify;
//   3. a valid, identity-matching execution receipt DOES verify (the
//      committed bounded transaction outcome);
//   4. a stale receipt (another engine tag / another variant / a failed
//      probe) can NEVER verify the current selection;
//   5. CPU remains verified by definition of execution.

import (
	"strings"
	"testing"
)

func enumeratedArc() Evidence {
	return Evidence{
		EngineDevices: []Device{
			{Backend: "Vulkan0", Name: "Intel(R) Arc(TM) A770M Graphics", TotalMB: 16384, Source: "engine-enumeration"},
		},
		EnumerationSupported: true,
		Workload:             WorkloadChat,
	}
}

func verifiedReceipt(tag, variant string) *ExecutionReceipt {
	return &ExecutionReceipt{
		Kind:      ReceiptKindVerifiedProbe,
		Line:      "offloaded 33/33 layers to GPU",
		EngineTag: tag,
		Variant:   variant,
		Device:    "Vulkan0: Intel(R) Arc(TM) A770M Graphics",
		Model:     "model.gguf",
		Status:    ReceiptStatusVerified,
		At:        "2026-10-05T00:00:00Z",
	}
}

// TestEnumerationAloneNeverVerifiesGPUExecution is THE Phase 2 P0
// regression: --list-devices is selection/provisioning evidence. It may
// select GPU_VULKAN (the candidate posture) but ExecutionVerified must
// stay FALSE with the pending plan stated and the CPU safety net on.
func TestEnumerationAloneNeverVerifiesGPUExecution(t *testing.T) {
	for _, req := range []Requested{RequestAuto, RequestGPU} {
		res := Resolve(req, enumeratedArc())

		if res.Backend != KindGPUVulkan {
			t.Fatalf("requested=%s: backend = %s, want GPU_VULKAN selected (provisionable candidate)", req, res.Backend)
		}
		if res.ExecutionVerified {
			t.Fatalf("requested=%s: enumeration alone verified GPU execution — detection is not execution", req)
		}
		if !strings.Contains(res.Verification, "pending") {
			t.Fatalf("requested=%s: verification must state the pending execution plan, got %q", req, res.Verification)
		}
		if res.Fallback != "safe" {
			t.Fatalf("requested=%s: unverified GPU selection must keep the CPU safety net, got %q", req, res.Fallback)
		}
	}
}

// TestOffloadLineVerifiesGPUExecution: the measured runtime offload line
// IS real execution evidence — verified, no fallback needed.
func TestOffloadLineVerifiesGPUExecution(t *testing.T) {
	ev := enumeratedArc()
	ev.RuntimeOffloadEvidence = "offloaded 33/33 layers to GPU"

	res := Resolve(RequestAuto, ev)

	if res.Backend != KindGPUVulkan || !res.ExecutionVerified {
		t.Fatalf("a measured offload line must verify GPU execution, got %s verified=%t", res.Backend, res.ExecutionVerified)
	}
	if res.Verification != VerificationOffloadLog {
		t.Fatalf("verification = %q, want %q", res.Verification, VerificationOffloadLog)
	}
	if res.Fallback != "none" {
		t.Fatalf("a verified selection needs no fallback, got %q", res.Fallback)
	}
}

// TestValidReceiptVerifiesGPUExecution: a committed, identity-matching
// execution receipt (the persisted bounded-transaction outcome) verifies.
func TestValidReceiptVerifiesGPUExecution(t *testing.T) {
	ev := enumeratedArc()
	ev.EngineTag = "b11205"
	ev.EngineVariant = "vulkan"
	ev.GPUExecutionReceipt = verifiedReceipt("b11205", "vulkan")

	res := Resolve(RequestAuto, ev)

	if res.Backend != KindGPUVulkan || !res.ExecutionVerified {
		t.Fatalf("a valid execution receipt must verify GPU execution, got %s verified=%t", res.Backend, res.ExecutionVerified)
	}
	if res.Verification != VerificationReceipt {
		t.Fatalf("verification = %q, want %q", res.Verification, VerificationReceipt)
	}
	if res.Device == "" {
		t.Fatal("the receipt's device identity must be attributed")
	}
}

// TestStaleReceiptCannotVerifyCurrentEngine: a receipt produced by
// another engine tag or another variant is STALE — never verify one
// binary and serve another.
func TestStaleReceiptCannotVerifyCurrentEngine(t *testing.T) {
	cases := []struct {
		name       string
		engineTag  string
		variant    string
		receipt    *ExecutionReceipt
		wantVerifd bool
	}{
		{
			name:       "receipt from another engine tag",
			engineTag:  "b11206",
			variant:    "vulkan",
			receipt:    verifiedReceipt("b11205", "vulkan"),
			wantVerifd: false,
		},
		{
			name:       "receipt from another variant",
			engineTag:  "b11205",
			variant:    "cpu",
			receipt:    verifiedReceipt("b11205", "vulkan"),
			wantVerifd: false,
		},
		{
			name:       "failed probe receipt (negative evidence)",
			engineTag:  "b11205",
			variant:    "vulkan",
			receipt:    &ExecutionReceipt{Kind: ReceiptKindVerifiedProbe, EngineTag: "b11205", Variant: "vulkan", Status: ReceiptStatusFailed},
			wantVerifd: false,
		},
		{
			name:       "matching identity",
			engineTag:  "b11205",
			variant:    "vulkan",
			receipt:    verifiedReceipt("b11205", "vulkan"),
			wantVerifd: true,
		},
	}

	for _, tc := range cases {
		ev := enumeratedArc()
		ev.EngineTag = tc.engineTag
		ev.EngineVariant = tc.variant
		ev.GPUExecutionReceipt = tc.receipt

		res := Resolve(RequestAuto, ev)

		if res.ExecutionVerified != tc.wantVerifd {
			t.Fatalf("%s: executionVerified = %t, want %t (reason=%q)", tc.name, res.ExecutionVerified, tc.wantVerifd, res.Reason)
		}
	}
}

// TestCPURemainsVerifiedByDefinition: the CPU rung stays verified by
// definition of execution — forced CPU and CPU fallback alike.
func TestCPURemainsVerifiedByDefinition(t *testing.T) {
	forced := Resolve(RequestCPU, enumeratedArc())
	if forced.Backend != KindCPU || !forced.ExecutionVerified {
		t.Fatalf("forced CPU must stay verified-by-definition, got %s verified=%t", forced.Backend, forced.ExecutionVerified)
	}

	fallback := Resolve(RequestAuto, Evidence{Workload: WorkloadChat})
	if fallback.Backend != KindCPU || !fallback.ExecutionVerified {
		t.Fatalf("CPU fallback must stay verified-by-definition, got %s verified=%t", fallback.Backend, fallback.ExecutionVerified)
	}
}

// TestReceiptValidForIdentityMatrix pins the receipt identity check
// itself (the stale-evidence invalidation primitive).
func TestReceiptValidForIdentityMatrix(t *testing.T) {
	var nilReceipt *ExecutionReceipt
	if nilReceipt.ValidFor("b11205", "vulkan") {
		t.Fatal("a nil receipt must never verify")
	}

	if got := (&ExecutionReceipt{}).ValidFor("", ""); !got {
		t.Fatal("an identity-less receipt with unknown current identity stays neutral-valid (unknown is not a mismatch)")
	}

	if got := verifiedReceipt("b11205", "vulkan").ValidFor("b11205", "vulkan"); !got {
		t.Fatal("matching identity must validate")
	}

	if got := verifiedReceipt("b11205", "vulkan").ValidFor("b99999", "vulkan"); got {
		t.Fatal("engine-tag mismatch must invalidate")
	}

	if got := verifiedReceipt("b11205", "vulkan").ValidFor("b11205", "cpu"); got {
		t.Fatal("variant mismatch must invalidate")
	}
}
