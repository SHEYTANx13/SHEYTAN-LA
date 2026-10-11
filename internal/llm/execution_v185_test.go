package llm

// execution_v185_test.go — v1.8.5 Phase 1 coverage for the shared
// execution/evidence ladder (execution.go).
//
// The contract under test (the Phase 2 foundation):
//
//   1. THE CARDINAL RULE: device enumeration NEVER equals verified
//      execution — a detection-only report stops at StageDetected with
//      ExecutionVerified false and the gap named;
//   2. the ladder is MONOTONE: each stage requires every predecessor;
//      the first unproven rung stops the climb and is reported;
//   3. the full-inputs report reaches StageVerified with every field
//      populated from its authority;
//   4. unknowns stay unknown ("" fields, zero samples);
//   5. offload evidence alone (without generations/model) cannot reach
//      execution-evidence;
//   6. ExecutionVerified is TRUE only at execution-evidence or above;
//   7. the stage order table is total and stable (wire contract).

import (
	"encoding/json"
	"testing"
)

func TestExecutionLadderDetectionNeverEqualsVerified(t *testing.T) {
	// The exact regression class the ladder exists to prevent: a device
	// was enumerated (or a DLL exists) and everything else is unknown.
	rep := ComposeExecutionReport(ExecutionReportInputs{
		DevicesKnown:   true,
		BackendHealthy: false,
		DeviceSelected: "",
		VerifiedModel:  "",
	})

	if rep.Stage != StageDetected {
		t.Fatalf("detection-only report staged %q, want %q", rep.Stage, StageDetected)
	}

	if rep.ExecutionVerified {
		t.Fatal("detection-only report claims execution verified")
	}

	if rep.GenerationExecuted {
		t.Fatal("detection-only report claims a generation executed")
	}

	if len(rep.Gaps) == 0 {
		t.Fatal("detection-only report must name its gaps")
	}
}

func TestExecutionLadderMonotoneStopsAtFirstUnprovenRung(t *testing.T) {
	cases := []struct {
		name string
		in   ExecutionReportInputs
		want ExecutionStage
	}{
		{
			name: "nothing known",
			in:   ExecutionReportInputs{},
			want: StageNone,
		},
		{
			name: "detected only",
			in:   ExecutionReportInputs{DevicesKnown: true},
			want: StageDetected,
		},
		{
			name: "backend healthy but no committed device",
			in:   ExecutionReportInputs{DevicesKnown: true, BackendHealthy: true, BackendName: "llama"},
			want: StageBackendAvailable,
		},
		{
			name: "device committed but no verified model",
			in: ExecutionReportInputs{
				DevicesKnown:   true,
				BackendHealthy: true,
				BackendName:    "llama",
				DeviceSelected: "GPU_VULKAN",
			},
			want: StageDeviceSelected,
		},
		{
			name: "model loaded but no generation yet",
			in: ExecutionReportInputs{
				DevicesKnown:   true,
				BackendHealthy: true,
				BackendName:    "llama",
				DeviceSelected: "GPU_VULKAN",
				VerifiedModel:  "model.gguf",
			},
			want: StageModelLoaded,
		},
		{
			name: "generation executed but no offload evidence and selection unverified",
			in: ExecutionReportInputs{
				DevicesKnown:      true,
				BackendHealthy:    true,
				BackendName:       "llama",
				DeviceSelected:    "GPU_VULKAN",
				VerifiedModel:     "model.gguf",
				GenerationSamples: 3,
			},
			want: StageGenerationExecuted,
		},
		{
			name: "offload evidence observed but the selection's verification contract is open",
			in: ExecutionReportInputs{
				DevicesKnown:      true,
				BackendHealthy:    true,
				BackendName:       "llama",
				DeviceSelected:    "GPU_VULKAN",
				VerifiedModel:     "model.gguf",
				GenerationSamples: 3,
				OffloadEvidence:   "offloaded 33/33 layers to GPU",
			},
			// The measured offload line IS runtime execution evidence —
			// the rung holds even though the transaction-level verified
			// contract (rung 7) is still open.
			want: StageExecutionEvidence,
		},
		{
			name: "fully verified",
			in: ExecutionReportInputs{
				DevicesKnown:              true,
				BackendHealthy:            true,
				BackendName:               "llama",
				EngineTag:                 "b11205",
				DeviceSelected:            "GPU_VULKAN",
				SelectionExecutionVerified: true,
				Verification:              "measured offload line",
				VerifiedModel:             "model.gguf",
				GenerationSamples:         3,
				OffloadEvidence:           "offloaded 33/33 layers to GPU",
			},
			want: StageVerified,
		},
	}

	for _, tc := range cases {
		rep := ComposeExecutionReport(tc.in)

		if rep.Stage != tc.want {
			t.Errorf("%s: stage %q, want %q (gaps: %v)", tc.name, rep.Stage, tc.want, rep.Gaps)
		}

		// ExecutionVerified is only ever true at/above execution-evidence.
		if rep.ExecutionVerified && ExecutionStageRank(rep.Stage) < ExecutionStageRank(StageExecutionEvidence) {
			t.Errorf("%s: ExecutionVerified true below the evidence rung", tc.name)
		}
	}
}

func TestExecutionLadderFullReport(t *testing.T) {
	rep := ComposeExecutionReport(ExecutionReportInputs{
		DevicesKnown:              true,
		BackendHealthy:            true,
		BackendName:               "llama",
		EngineTag:                 "b11205",
		DeviceSelected:            "GPU_VULKAN",
		SelectionExecutionVerified: true,
		Verification:              "measured offload line",
		VerifiedModel:             "model.gguf",
		GenerationSamples:         7,
		OffloadEvidence:           "offloaded 33/33 layers to GPU",
	})

	if rep.Stage != StageVerified {
		t.Fatalf("stage %q, want verified", rep.Stage)
	}

	if rep.Backend != "llama" || rep.EngineTag != "b11205" ||
		rep.Device != "GPU_VULKAN" || rep.Model != "model.gguf" {
		t.Fatalf("authority fields lost: %+v", rep)
	}

	if !rep.GenerationExecuted || !rep.ExecutionVerified {
		t.Fatalf("verified report flags wrong: %+v", rep)
	}

	if rep.OffloadEvidence == "" || rep.Verification == "" {
		t.Fatalf("evidence fields lost: %+v", rep)
	}

	if len(rep.Gaps) != 0 {
		t.Fatalf("verified report must have no gaps: %v", rep.Gaps)
	}
}

func TestExecutionLadderUnknownsStayUnknown(t *testing.T) {
	rep := ComposeExecutionReport(ExecutionReportInputs{DevicesKnown: true})

	if rep.Backend != "" || rep.EngineTag != "" || rep.Device != "" ||
		rep.Model != "" || rep.OffloadEvidence != "" || rep.Verification != "" {
		t.Fatalf("unknown fields fabricated: %+v", rep)
	}
}

func TestExecutionLadderWireContract(t *testing.T) {
	// The JSON field names are the Phase 2 wire contract — pin them.
	body, err := json.Marshal(&ExecutionReport{Stage: StageDetected})
	if err != nil {
		t.Fatal(err)
	}

	for _, fragment := range []string{`"stage":"detected"`, `"generationExecuted":false`, `"executionVerified":false`} {
		if !containsStr(string(body), fragment) {
			t.Fatalf("wire body %s missing %s", body, fragment)
		}
	}

	// The stage order table is total and stable.
	if len(executionStageOrder) != 8 {
		t.Fatalf("stage order changed length: %d", len(executionStageOrder))
	}

	expected := []ExecutionStage{
		StageNone, StageDetected, StageBackendAvailable, StageDeviceSelected,
		StageModelLoaded, StageGenerationExecuted, StageExecutionEvidence, StageVerified,
	}

	for i, s := range expected {
		if executionStageOrder[i] != s {
			t.Fatalf("stage order[%d] = %q, want %q", i, executionStageOrder[i], s)
		}

		if ExecutionStageRank(s) != i {
			t.Fatalf("rank(%q) = %d, want %d", s, ExecutionStageRank(s), i)
		}
	}

	// Unknown stages rank as none (fail-closed).
	if ExecutionStageRank("bogus") != 0 {
		t.Fatal("unknown stage must rank as none")
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}

	return false
}
