package hardware

import (
	"encoding/json"
	"strings"
	"testing"
)

// v1.2.2 regression: the Environment Centre frontend reads
// `gpus.length` on the /api/environment payload. A nil Go slice marshals
// as JSON `null`, which threw inside React render and — before the error
// boundaries — blanked the whole application (the System-tab black
// screen). Collect must ALWAYS marshal GPUs as an array.
func TestCollectGPUsNeverMarshalsAsNull(t *testing.T) {
	// nil config is valid: the backend probe path is exercised with no
	// llama-server present (fresh install), which is exactly the machine
	// shape that produced the crash.
	p := Collect(nil)

	if p.GPUs == nil {
		t.Fatalf("Collect returned a nil GPU slice — it would marshal as JSON null")
	}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}

	if strings.Contains(string(raw), `"gpus":null`) {
		t.Fatalf("gpus marshaled as null: %s", raw)
	}

	if !strings.Contains(string(raw), `"gpus":[]`) && !strings.Contains(string(raw), `"gpus":[{`) {
		t.Fatalf("gpus is neither empty array nor populated array: %s", raw)
	}
}

// The same contract for the API-facing environment payload shape: a
// machine with no detected GPUs still yields "gpus": [] on the wire.
func TestProfileGPUJSONContract(t *testing.T) {
	p := Profile{GPUs: []GPU{}}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}

	if !strings.Contains(string(raw), `"gpus":[]`) {
		t.Fatalf("expected gpus to marshal as an empty array, got: %s", raw)
	}
}
