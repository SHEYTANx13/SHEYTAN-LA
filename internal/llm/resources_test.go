package llm

import (
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func cfgWithCtx(n int) *config.Config {
	cfg := config.Default()
	cfg.LLM.NumCtx = n
	return cfg
}

// TestResolveSessionContextInherits: policy 0 = inherit the global value.
func TestResolveSessionContextInherits(t *testing.T) {
	cfg := cfgWithCtx(8192)
	ec := ResolveSessionContext(cfg, nil, 0, 0)

	if ec.Effective != 8192 {
		t.Fatalf("effective = %d, want 8192 (global inherited)", ec.Effective)
	}
	if ec.Requested != 8192 {
		t.Fatalf("requested = %d, want 8192", ec.Requested)
	}
	if ec.SessionPolicy != 0 {
		t.Fatalf("sessionPolicy = %d, want 0", ec.SessionPolicy)
	}
}

// TestResolveSessionContextNarrowsPerChat: one chat narrows to 4K while
// the global stays 16K — and nothing else changes.
func TestResolveSessionContextNarrowsPerChat(t *testing.T) {
	cfg := cfgWithCtx(16384)
	ec := ResolveSessionContext(cfg, nil, 0, 4096)

	if ec.Effective != 4096 {
		t.Fatalf("effective = %d, want 4096", ec.Effective)
	}
	if ec.SessionPolicy != 4096 {
		t.Fatalf("sessionPolicy = %d, want 4096", ec.SessionPolicy)
	}
	if ec.Configured != 16384 {
		t.Fatalf("configured = %d, want 16384 (global untouched)", ec.Configured)
	}
	if len(ec.Reasons) == 0 {
		t.Fatal("narrowing must leave an explanatory trail")
	}
}

// TestResolveSessionContextRaisesToConfigured: chat B asks 32K on a 32K
// global — the session policy serves the full window.
func TestResolveSessionContextRaisesToConfigured(t *testing.T) {
	cfg := cfgWithCtx(32768)
	caps := &ModelCapabilities{ContextLength: 131072}

	ec := ResolveSessionContext(cfg, caps, 0, 32768)

	if ec.Effective != 32768 {
		t.Fatalf("effective = %d, want 32768", ec.Effective)
	}
	if ec.SessionPolicy != 32768 {
		t.Fatalf("sessionPolicy = %d, want 32768", ec.SessionPolicy)
	}
}

// TestResolveSessionContextNeverExceedsEngine: a 128K request on a 16K
// engine resolves to 16K, honestly.
func TestResolveSessionContextNeverExceedsEngine(t *testing.T) {
	cfg := cfgWithCtx(16384)
	caps := &ModelCapabilities{ContextLength: 131072}

	ec := ResolveSessionContext(cfg, caps, 16384, 131072)

	if ec.Effective != 16384 {
		t.Fatalf("effective = %d, want 16384 (engine window clamp)", ec.Effective)
	}
	if ec.EngineLimit != 16384 {
		t.Fatalf("engineLimit = %d, want 16384", ec.EngineLimit)
	}
}

// TestResolveSessionContextNeverExceedsModel: model max smaller than the
// requested session policy wins.
func TestResolveSessionContextNeverExceedsModel(t *testing.T) {
	cfg := cfgWithCtx(16384)
	caps := &ModelCapabilities{ContextLength: 4096}

	ec := ResolveSessionContext(cfg, caps, 0, 8192)

	if ec.Effective != 4096 {
		t.Fatalf("effective = %d, want 4096 (model max clamp)", ec.Effective)
	}
	if ec.ModelMax != 4096 {
		t.Fatalf("modelMax = %d, want 4096", ec.ModelMax)
	}
}

// TestResolveSessionContextFloor: absurdly small requests stay usable.
func TestResolveSessionContextFloor(t *testing.T) {
	cfg := cfgWithCtx(16384)
	ec := ResolveSessionContext(cfg, nil, 0, 64)

	if ec.Effective != 1024 {
		t.Fatalf("effective = %d, want 1024 (absolute floor)", ec.Effective)
	}
}

// --- resource assessment ---

// TestAssessContextResourceUnsupported: the estimated footprint exceeds
// the machine → unsupported, with a reason that explains why.
func TestAssessContextResourceUnsupported(t *testing.T) {
	card := &ModelCard{Layers: 32, EmbeddingLen: 4096}
	// KV: 2*32*4096*2 B/token = 512 KiB/token → 128K tok ≈ 64 GiB.
	sys := SystemMemory{TotalRAMBytes: 16 << 30, AvailableRAMBytes: 8 << 30}

	assessed := AssessContextResource(card, 4<<30, 131072, "f16", sys)

	if assessed.Class != ResourceUnsupported {
		t.Fatalf("class = %s, want unsupported", assessed.Class)
	}
	if assessed.Reason == "" {
		t.Fatal("unsupported verdict must carry a user-facing reason")
	}
	if assessed.EstimatedTotalBytes <= assessed.SystemRAMBytes {
		t.Fatal("estimate must reflect the oversized footprint")
	}
}

// TestAssessContextResourceSafe: a small window on a capable machine.
func TestAssessContextResourceSafe(t *testing.T) {
	card := &ModelCard{Layers: 32, EmbeddingLen: 4096}
	sys := SystemMemory{TotalRAMBytes: 32 << 30, AvailableRAMBytes: 16 << 30}

	assessed := AssessContextResource(card, 3<<30, 4096, "f16", sys)

	if assessed.Class != ResourceSafe {
		t.Fatalf("class = %s, want safe (got reason %s)", assessed.Class, assessed.Reason)
	}
	if assessed.KVCacheBytes <= 0 {
		t.Fatal("KV estimate missing")
	}
}

// TestAssessContextResourceUnknownRAM: no measurement → caution, never
// invented absolutes.
func TestAssessContextResourceUnknownRAM(t *testing.T) {
	card := &ModelCard{Layers: 32, EmbeddingLen: 4096}

	assessed := AssessContextResource(card, 4<<30, 65536, "", SystemMemory{})

	if assessed.Class != ResourceCaution {
		t.Fatalf("class = %s, want caution when RAM unknown", assessed.Class)
	}
}

// TestContextOptionsLadder: every rung is classified, model-clamped
// rungs are unavailable with an honest reason.
func TestContextOptionsLadder(t *testing.T) {
	card := &ModelCard{Layers: 32, EmbeddingLen: 4096}
	sys := SystemMemory{TotalRAMBytes: 64 << 30, AvailableRAMBytes: 32 << 30}

	options := ContextOptions(card, 3<<30, "f16", sys, 16384, 16384)

	if len(options) != len(ContextOptionSizes) {
		t.Fatalf("options = %d, want %d", len(options), len(ContextOptionSizes))
	}

	for _, opt := range options {
		if opt.Tokens <= 16384 {
			if !opt.Available {
				t.Fatalf("%s should be available: %s", opt.Label, opt.Reason)
			}
		}
		if opt.Tokens > 16384 && !opt.ModelClamp {
			t.Fatalf("%s must be model-clamped", opt.Label)
		}
		if opt.Tokens > 16384 && opt.Available {
			t.Fatalf("%s must be unavailable beyond the model limit", opt.Label)
		}
	}

	// Labels follow the §4 spec shape.
	if ContextLabel(4096) != "4K" || ContextLabel(16384) != "16K" ||
		ContextLabel(131072) != "128K" {
		t.Fatal("context labels drifted from the 4K…128K ladder")
	}
}
