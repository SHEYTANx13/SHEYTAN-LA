// preflight_test.go — v1.7.1 deterministic coverage for the pre-run
// compatibility gate (§4.9):
//
//   - hard incompatibilities refuse BEFORE any engine start;
//   - resource deficits reach the critical/refusal path;
//   - warnings are never misclassified as incompatibilities;
//   - native/CPU/Vulkan capability differences are honored;
//   - unknown data stays unknown (never fabricated support);
//   - the refusal message names the reason AND an alternative.
package preflight

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func baseInput() Input {
	return Input{
		ModelPath:        "/models/test-model.gguf",
		Backend:          "llama.cpp-cpu",
		Device:           "cpu",
		BackendAvailable: true,
		RequestedContext: 8192,
		Sys: llm.SystemMemory{
			TotalRAMBytes:     32 << 30,
			AvailableRAMBytes: 24 << 30,
		},
	}
}

func TestIncompatibleBackendRefusedBeforeEngineSpawn(t *testing.T) {
	in := baseInput()
	in.Backend = "native"
	in.BackendAvailable = false
	in.BackendUnavailableReason = "native engine disabled"

	report := Evaluate(in)
	if report.Compatible || report.Severity != SeverityIncompatible {
		t.Fatalf("unavailable backend must be incompatible: %+v", report)
	}
	if !strings.Contains(strings.Join(report.Reasons, "; "), "native engine disabled") {
		t.Fatalf("reason must carry the honest detail: %+v", report.Reasons)
	}
	msg := RefusalMessage(report)
	if !strings.Contains(msg, "Cannot start this model") ||
		!strings.Contains(msg, "Reason:") ||
		!strings.Contains(msg, "Alternative:") {
		t.Fatalf("refusal message incomplete: %q", msg)
	}
}

func TestUnsupportedArchitectureRefused(t *testing.T) {
	in := baseInput()
	in.Caps = &llm.ModelCapabilities{Arch: "deepseek3"} // outside the llama graph family
	in.Backend = "llama.cpp-cpu"

	report := Evaluate(in)
	if report.Severity != SeverityIncompatible {
		t.Fatalf("unsupported llama.cpp architecture must refuse: %+v", report)
	}
}

func TestNativeIncompatibleModelRefusedOnNativeOnly(t *testing.T) {
	in := baseInput()
	in.Backend = "native"
	in.Caps = &llm.ModelCapabilities{
		Arch:         "llama",
		NativeBackend: false,
		NativeReason:  "architecture not implemented in the native engine",
	}

	report := Evaluate(in)
	if report.Severity != SeverityIncompatible || !strings.Contains(report.Reasons[0], "Native Engine cannot execute") {
		t.Fatalf("native-incompatible model must refuse on native: %+v", report)
	}

	// The SAME model is fine on llama.cpp CPU (the valid fallback).
	in.Backend = "llama.cpp-cpu"
	report = Evaluate(in)
	if !report.Compatible {
		t.Fatalf("llama.cpp fallback must be compatible: %+v", report)
	}
}

func TestInsufficientResourcesRefused(t *testing.T) {
	in := baseInput()
	// 40 GB estimated footprint vs 24 GB available RAM: predictable OOM.
	in.Caps = &llm.ModelCapabilities{SizeBytes: 38 << 30, ContextLength: 32768}
	in.RequestedContext = 32768

	report := Evaluate(in)
	if report.Severity != SeverityIncompatible {
		t.Fatalf("unsatisfiable memory requirement must refuse: %+v", report)
	}
	if len(report.Reasons) == 0 {
		t.Fatal("refusal needs a reason")
	}
}

func TestCurrentPressureCriticalRefuses(t *testing.T) {
	in := baseInput()
	// Small model, but available RAM right now is nearly exhausted.
	in.Caps = &llm.ModelCapabilities{SizeBytes: 6 << 30}
	in.Sys.AvailableRAMBytes = 400 << 20 // < 10% of a 4 GB requirement + overhead

	report := Evaluate(in)
	if report.Severity != SeverityIncompatible {
		t.Fatalf("deficit vs CURRENT available memory must refuse: %+v", report)
	}
	// Either refusal path is legitimate: the resource-authority
	// classification (estimated footprint exceeds usable memory) or the
	// current-pressure check (deficit vs available right now). Both name
	// the measured numbers.
	joined := strings.Join(report.Reasons, "; ")
	if !strings.Contains(joined, "currently available memory") &&
		!strings.Contains(joined, "exceeds this machine's memory") {
		t.Fatalf("measured pressure reason missing: %+v", report.Reasons)
	}
}

func TestWarningsAreNotMisclassifiedAsIncompatible(t *testing.T) {
	in := baseInput()
	// Tight-but-feasible: 50% margin → warning/high_pressure, NOT refusal.
	in.Caps = &llm.ModelCapabilities{SizeBytes: 10 << 30}
	in.Sys.AvailableRAMBytes = 20 << 30

	report := Evaluate(in)
	if !report.Compatible {
		t.Fatalf("feasible tight fit must stay compatible: %+v", report)
	}
	if report.Severity == SeverityIncompatible {
		t.Fatalf("a warning state must never be classified incompatible: %+v", report)
	}
	if report.Severity != SeverityOK && report.Severity != SeverityWarning &&
		report.Severity != SeverityHighPressure {
		t.Fatalf("severity out of the expected set: %s", report.Severity)
	}
}

func TestEngineClampIsWarningNotRefusal(t *testing.T) {
	in := baseInput()
	in.EngineContextLimit = 4096

	report := Evaluate(in)
	if !report.Compatible {
		t.Fatalf("engine clamp must not refuse: %+v", report)
	}
	if report.Severity != SeverityWarning {
		t.Fatalf("clamp must surface as warning: %+v", report)
	}
	if !strings.Contains(report.Reasons[0], "clamped") {
		t.Fatalf("clamp reason missing: %+v", report.Reasons)
	}
}

func TestUnknownDataStaysUnknown(t *testing.T) {
	in := baseInput()
	in.Caps = nil        // no readable capabilities
	in.Card = nil        // no GGUF facts
	in.Sys.AvailableRAMBytes = 0 // deep probe not ready

	report := Evaluate(in)
	if !report.Compatible {
		t.Fatalf("unknown facts must NOT fabricate a refusal: %+v", report)
	}
	foundUnknown := false
	for _, u := range report.Unknown {
		if strings.Contains(u, "available RAM") {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatalf("unknown RAM must be listed honestly: %+v", report.Unknown)
	}
	if report.SafetyMarginPct != 0 {
		t.Fatalf("safety margin must stay unset when availability is unknown: %f", report.SafetyMarginPct)
	}
}

func TestVisionRequirementRefused(t *testing.T) {
	in := baseInput()
	in.VisionRequired = true
	in.Caps = &llm.ModelCapabilities{Multimodal: false}

	report := Evaluate(in)
	if report.Severity != SeverityIncompatible {
		t.Fatalf("vision requirement without multimodal model must refuse: %+v", report)
	}
}

func TestSafetyMarginReported(t *testing.T) {
	in := baseInput()
	in.Caps = &llm.ModelCapabilities{SizeBytes: 4 << 30}

	report := Evaluate(in)
	if report.SafetyMarginPct <= 0 || report.SafetyMarginPct >= 100 {
		t.Fatalf("safety margin should be a measured 0-100 value: %f", report.SafetyMarginPct)
	}
	if report.Requirements.TotalBytes <= 0 {
		t.Fatal("requirements must carry the estimated total")
	}
	if report.Available.RAMTotalBytes == 0 {
		t.Fatal("available must carry the measured RAM")
	}
}
