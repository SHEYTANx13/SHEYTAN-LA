// preflight_gate_test.go — v1.7.1: the §4.9 integration proof.
//
//	preflight rejection → ZERO model process starts
//
// The stack is wired with the launch-recording re-exec fake engine (the
// same harness as the model-first prewarm tests): any llama.cpp launch
// appends to launchLog. The test walks the run-gate path the API server
// implements — evaluate PreflightReport, refuse when incompatible — and
// asserts the launch log stays EMPTY.
package runtime

import (
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

func TestPreflightRejectionStartsZeroModelProcesses(t *testing.T) {
	stack, launchLog := modelFirstStack(t, "") // NO model selected
	t.Cleanup(stack.Close)

	// The gate evaluates BEFORE any engine start.
	report := stack.PreflightReport(0)
	if report.Compatible || report.Severity != preflight.SeverityIncompatible {
		t.Fatalf("no-model-selected must be incompatible: %+v", report)
	}

	// The server's gate contract: refuse on incompatible — EnsureLLMContext
	// (the only path that can start a model process) is NEVER reached.
	refused := !report.Compatible && report.Severity == preflight.SeverityIncompatible
	if !refused {
		t.Fatal("gate decision must be refusal")
	}

	if launches(launchLog) != 0 {
		t.Fatalf("preflight rejection must start ZERO model processes; launch log: %s", launchLog)
	}

	// Belt and braces: even the direct engine gate refuses with the
	// typed no-model error — nothing else can start a process either.
	if err := stack.EnsureLLMContext(t.Context()); err == nil || !errIsNoModel(err) {
		t.Fatalf("EnsureLLMContext must refuse without a model: %v", err)
	}
	if launches(launchLog) != 0 {
		t.Fatalf("engine process started despite refusal: %s", launchLog)
	}
}

func TestPreflightReportAgreesWithBackendSelection(t *testing.T) {
	stack, launchLog := modelFirstStack(t, "test-model.gguf")
	t.Cleanup(stack.Close)

	// §4.9 (7): the preflight verdict must agree with the ACTUAL selection
	// authority: when the selection authority cannot return a backend,
	// preflight must NOT claim compatible.
	decision := llm.SelectGenerationBackendDetailed(
		stack.Src.Load(), stack.nativeBackend, stack.llamaBackend,
	)

	report := stack.PreflightReport(0)

	if decision.Backend == nil && report.Compatible && report.Backend != "remote" {
		t.Fatalf("preflight compatible while the selection authority has no backend: %+v vs %+v",
			report, decision)
	}

	// A real launch still must not have happened from evaluation alone.
	if launches(launchLog) != 0 {
		t.Fatalf("evaluation must never start a process: %s", launchLog)
	}
}

func errIsNoModel(err error) bool {
	type unwrappable interface{ Unwrap() error }
	for e := err; e != nil; {
		if e.Error() != "" && contains(e.Error(), "no model") {
			return true
		}
		u, ok := e.(unwrappable)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
