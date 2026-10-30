package accelerator

import (
	"strings"
	"testing"
)

// TestResolveReasonCodeAlwaysSet pins the v1.11.0 typed-cause contract:
// every Resolution outcome carries a non-empty ReasonCode that matches the
// evidence state it reports. A code never upgrades the evidence state.
func TestResolveReasonCodeAlwaysSet(t *testing.T) {
	cases := []struct {
		name      string
		requested Requested
		ev        Evidence
		want      ReasonCode
		verified  bool
	}{
		{
			name:      "explicit CPU request",
			requested: RequestCPU,
			ev:        Evidence{Workload: WorkloadChat},
			want:      ReasonCodeRequestedCPU,
			verified:  true,
		},
		{
			name:      "enumerated GPU pending verify",
			requested: RequestAuto,
			ev:        intelArcEvidence(),
			want:      ReasonCodeGPUPendingVerify,
			verified:  false,
		},
		{
			name:      "runtime offload evidence verifies",
			requested: RequestAuto,
			ev:        verifiedOffloadEvidence(),
			want:      ReasonCodeGPUVerified,
			verified:  true,
		},
		{
			name:      "backend unsupported falls to CPU typed",
			requested: RequestAuto,
			ev: Evidence{
				EngineDevices:        nil,
				EnumerationSupported: false,
				VulkanBackendPresent: false,
				Workload:             WorkloadChat,
			},
			want:     ReasonCodeBackendUnsupported,
			verified: true, // CPU fallback is verified by definition
		},
		{
			name:      "DLL alone selects pending-verify, never verified",
			requested: RequestAuto,
			ev: Evidence{
				EngineDevices:        nil,
				EnumerationSupported: false,
				VulkanBackendPresent: true, // the DLL exists, no enumeration
				Workload:             WorkloadChat,
			},
			want:     ReasonCodeGPUPendingVerify,
			verified: false,
		},
		{
			name:      "backend supported but zero devices",
			requested: RequestAuto,
			ev: Evidence{
				EngineDevices:        nil,
				EnumerationSupported: true,
				VulkanBackendPresent: true,
				Workload:             WorkloadChat,
			},
			want:     ReasonCodeNoMatchingDevice,
			verified: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Resolve(tc.requested, tc.ev)
			if res.ReasonCode == "" {
				t.Fatal("ReasonCode must always be set")
			}
			if res.ReasonCode != tc.want {
				t.Fatalf("ReasonCode = %q, want %q (reason: %q)", res.ReasonCode, tc.want, res.Reason)
			}
			if res.Reason == "" {
				t.Fatal("human-readable Reason must accompany the code")
			}
			if res.ExecutionVerified != tc.verified {
				t.Fatalf("ExecutionVerified = %v, want %v — the code must never upgrade the evidence state", res.ExecutionVerified, tc.verified)
			}
		})
	}
}

// verifiedOffloadEvidence is the strongest honest evidence set: engine
// enumeration supported, a device present, AND a measured runtime offload
// line from the current boot.
func verifiedOffloadEvidence() Evidence {
	ev := intelArcEvidence()
	ev.RuntimeOffloadEvidence = "offloaded 33/33 layers to GPU"
	return ev
}

// TestReasonCodeStableValues pins the wire values — they are consumed by
// the engine diagnostics surfaces and must not drift silently.
func TestReasonCodeStableValues(t *testing.T) {
	for code, want := range map[ReasonCode]string{
		ReasonCodeRequestedCPU:       "requested-cpu",
		ReasonCodeNPUVerified:        "npu-benchmark-verified",
		ReasonCodeGPUVerified:        "gpu-execution-verified",
		ReasonCodeGPUPendingVerify:   "gpu-selected-pending-verify",
		ReasonCodeNoAccelEvidence:    "no-accelerator-evidence-cpu",
		ReasonCodeBackendUnsupported: "backend-unsupported",
		ReasonCodeNoMatchingDevice:   "no-matching-device",
	} {
		if string(code) != want {
			t.Errorf("code value drifted: %q != %q", code, want)
		}
		if strings.Contains(string(code), " ") {
			t.Errorf("codes are machine tokens, must not contain spaces: %q", code)
		}
	}
}
