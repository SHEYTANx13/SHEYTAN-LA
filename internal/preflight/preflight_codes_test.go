// preflight_codes_test.go — master engineering work order §3.2.5: every
// blocked action returns a STRUCTURED reason and next action, and every
// distinct failure class carries its own stable machine-readable Code so
// the UI can route deterministically (guided model-selection card,
// resource panel, backend diagnostics) instead of parsing prose.
//
// Observed defect this completes: run 44069246344's runtime log showed a
// run refused before engine start with model=(empty) settling as an
// undifferentiated error — the empty-model state must be a FIRST-CLASS
// distinct code that the UI can use to disable Send and route to guided
// model setup, never a post-submit chat failure.

package preflight

import (
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/llm"
)

func codesTestInput() Input {
	// NOTE: Evaluate is the PURE fit layer — the model path is used only
	// for identity/reporting here (existence is the Stack gate's
	// llm.ResolveModelPath decision, tested in internal/runtime).
	return Input{
		ModelPath:         "/models/model.gguf",
		Backend:           "llama.cpp-cpu",
		BackendAvailable:  true,
		StreamingRequired: true,
	}
}

func TestEmptyModelIsTheStackGateDecisionNotEvaluate(t *testing.T) {
	// Layering contract: Evaluate judges the MODEL/BACKEND/RESOURCE fit
	// for a GIVEN path; the empty-selection decision belongs to the
	// Stack gate (internal/runtime PreflightReport), which returns
	// CodeModelNotSelected BEFORE Evaluate is ever called. Here we pin
	// that Evaluate neither crashes nor fabricates a refusal for an
	// empty path — the distinction stays in one authority, not two.
	in := codesTestInput()
	in.ModelPath = ""
	rep := Evaluate(in)
	if !rep.Compatible {
		t.Fatalf("Evaluate judges fit, not selection: an empty path must not produce a fit refusal here (%v)", rep.Reasons)
	}
	if rep.Code != "" {
		t.Fatalf("no refusal code may be fabricated at the fit layer, got %q", rep.Code)
	}
}

func TestDistinctFailureClassesCarryDistinctCodes(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Input)
		want Code
	}{
		{
			name: "backend unavailable",
			mut:  func(in *Input) { in.BackendAvailable = false; in.BackendUnavailableReason = "engine binary missing" },
			want: CodeBackendUnavailable,
		},
		{
			name: "unsupported backend for model",
			mut: func(in *Input) {
				in.Backend = "native"
				in.Caps = &llm.ModelCapabilities{NativeBackend: false, NativeReason: "unsupported graph family"}
			},
			want: CodeUnsupportedBackend,
		},
		{
			name: "context exceeds model limit",
			mut: func(in *Input) {
				in.Caps = &llm.ModelCapabilities{ContextLength: 512, Arch: "llama"}
				in.RequestedContext = 4096
			},
			want: CodeContextExceedsModel,
		},
		{
			name: "capability mismatch (vision required)",
			mut: func(in *Input) {
				in.VisionRequired = true
				in.Caps = &llm.ModelCapabilities{Multimodal: false}
			},
			want: CodeCapabilityMismatch,
		},
	}

	seen := map[Code]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := codesTestInput()
			tc.mut(&in)
			rep := Evaluate(in)
			if rep.Compatible {
				t.Fatalf("%s must be incompatible", tc.name)
			}
			if rep.Code != tc.want {
				t.Fatalf("Code = %q, want %q — failure classes must stay distinct", rep.Code, tc.want)
			}
			if len(rep.Reasons) == 0 || rep.RecommendedAction == "" {
				t.Fatalf("every refusal carries a reason and a next action, got reasons=%v action=%q", rep.Reasons, rep.RecommendedAction)
			}
			seen[rep.Code] = true
		})
	}
	// The classes above must not collapse into one code.
	if len(seen) != len(cases) {
		t.Fatalf("distinct failure classes collapsed: %v", seen)
	}
}

func TestSuccessfulEvaluationCarriesNoCode(t *testing.T) {
	in := codesTestInput()
	rep := Evaluate(in)
	if !rep.Compatible {
		t.Fatalf("a plain compatible input must pass, got %v (%v)", rep.Reasons, rep.Unknown)
	}
	if rep.Code != "" {
		t.Fatalf("a passing report must carry no refusal code, got %q", rep.Code)
	}
}
