package llm

import (
	"path/filepath"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// fixtureGGUF points at the real (tiny) GGUF fixture shipped with the
// native engine tests — a genuine GGUF header, not a mock.
const fixtureGGUF = "../../native/engine/tests/fixtures/tiny-llama-app.gguf"

func TestResolveModelCapabilitiesFromRealFixture(t *testing.T) {
	cfg := config.Default()
	cfg.VisionEnabled = false

	caps := ResolveModelCapabilities(cfg, fixtureGGUF)
	if caps == nil {
		t.Fatal("real GGUF fixture must produce a capability card")
	}
	if caps.Arch == "" {
		t.Fatal("architecture missing from the card")
	}
	if caps.SizeBytes <= 0 {
		t.Fatal("size missing")
	}
	if caps.FileName == "" || caps.Path == "" {
		t.Fatal("identity missing")
	}
	// The card is the single authority consumed by planner/launcher/UI;
	// recommended values must be sane for a small model.
	if caps.RecommendedCtx <= 0 {
		t.Fatal("recommended context missing")
	}
	if caps.RecommendedGenBudget < 512 {
		t.Fatal("generation budget floor missing")
	}
}

func TestResolveModelCapabilitiesMissingFile(t *testing.T) {
	cfg := config.Default()
	if caps := ResolveModelCapabilities(cfg, filepath.Join(t.TempDir(), "nope.gguf")); caps != nil {
		t.Fatal("missing file must yield nil (callers fall back to config)")
	}
}

func TestResolveEffectiveContextSmallModelClamp(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.NumCtx = 16384 // configured 16k

	caps := &ModelCapabilities{
		Arch:          "llama",
		ContextLength: 4096, // model trained for 4k
	}

	ec := ResolveEffectiveContext(cfg, caps, 0)
	if ec.Effective != 4096 {
		t.Fatalf("effective = %d, want the model limit 4096", ec.Effective)
	}
	if ec.ModelMax != 4096 || len(ec.Reasons) == 0 {
		t.Fatalf("decision not explained: %+v", ec)
	}
}

func TestResolveEffectiveContextEngineLimitWins(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.NumCtx = 32768

	caps := &ModelCapabilities{ContextLength: 32768}
	ec := ResolveEffectiveContext(cfg, caps, 8192)
	if ec.Effective != 8192 {
		t.Fatalf("effective = %d, want the engine limit 8192", ec.Effective)
	}
}

func TestResolveEffectiveContextNeverRaises(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.NumCtx = 4096

	caps := &ModelCapabilities{ContextLength: 131072}
	ec := ResolveEffectiveContext(cfg, caps, 0)
	if ec.Effective != 4096 {
		t.Fatalf("effective = %d — the configured value is a ceiling, never a floor", ec.Effective)
	}
}

func TestLaunchContextSizeUsesModelLimit(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.NumCtx = 16384

	// The tiny fixture's training limit governs the launch window.
	got := launchContextSize(cfg, fixtureGGUF)
	if got >= 16384 {
		t.Fatalf("launch ctx = %d — the GGUF clamp did not apply", got)
	}
}

func TestNativeArchitectureSupport(t *testing.T) {
	if ok, _ := NativeArchitectureSupport("llama"); !ok {
		t.Fatal("llama arch must be natively supported")
	}
	if ok, reason := NativeArchitectureSupport("qwen2"); ok || reason == "" {
		t.Fatal("non-llama arch must be honestly declined with a reason")
	}
}
