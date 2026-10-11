package llm

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// v1.8.2 IDENTITY-BASED CAPABILITY CACHE: the 10-second TTL is gone. An
// unchanged model file (path+size+mtime) must keep serving the cached
// card indefinitely — a turn separated by minutes must not re-parse the
// GGUF — while a CHANGED file identity must re-read exactly once, and a
// config change must re-derive the config-sensitive fields WITHOUT
// re-parsing the GGUF.
func TestCapsCacheServesUnchangedIdentityBeyondTTLWindow(t *testing.T) {
	cfg := config.Default()
	cfg.VisionEnabled = false

	first := ResolveModelCapabilities(cfg, fixtureGGUF)
	if first == nil {
		t.Fatal("fixture card must resolve")
	}

	// Far beyond the old 10s TTL: the identity (path+size+mtime) has not
	// changed, so the cache must still serve — without any sleep, the
	// identity check alone decides.
	second := ResolveModelCapabilities(cfg, fixtureGGUF)
	if second != first {
		t.Fatal("unchanged identity must serve the SAME cached card (no re-parse)")
	}
}

func TestCapsCacheReparseOnChangedIdentity(t *testing.T) {
	cfg := config.Default()
	cfg.VisionEnabled = false

	dir := t.TempDir()
	model := filepath.Join(dir, "model.gguf")

	// Minimal but real card source: copy the fixture.
	data, err := os.ReadFile(fixtureGGUF)
	if err != nil {
		t.Fatalf("fixture read: %v", err)
	}

	if err := os.WriteFile(model, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	first := ResolveModelCapabilities(cfg, model)
	if first == nil {
		t.Fatal("first resolve must parse")
	}

	// Change the IDENTITY (size + mtime) — a different file, whatever its
	// content: the cache must re-parse.
	if err := os.WriteFile(model, append(data, 0x00, 0x01), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(model, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	second := ResolveModelCapabilities(cfg, model)
	if second == nil {
		t.Fatal("changed identity must re-parse successfully")
	}

	if second == first {
		t.Fatal("changed identity must produce a NEW card, not the stale cache")
	}

	if second.SizeBytes == first.SizeBytes {
		t.Fatal("new card must reflect the new file size (identity actually re-read)")
	}
}

func TestCapsCacheConfigDriftReDerivesWithoutReparse(t *testing.T) {
	cfg := config.Default()
	cfg.VisionEnabled = false
	cfg.LLM.NumCtx = 8192

	first := ResolveModelCapabilities(cfg, fixtureGGUF)
	if first == nil {
		t.Fatal("fixture card must resolve")
	}

	// Same file identity, changed configuration: the recommendations are
	// config-sensitive and must re-derive (cheap), and the card must NOT
	// be the same cached object.
	cfg.LLM.NumCtx = 4096

	second := ResolveModelCapabilities(cfg, fixtureGGUF)

	if second == first {
		t.Fatal("config drift must re-derive the config-sensitive fields")
	}

	if second.RecommendedCtx != 4096 && second.ContextLength > 4096 {
		t.Fatalf("re-derived recommendation must honor the new config: rec=%d modelMax=%d",
			second.RecommendedCtx, second.ContextLength)
	}
}

func TestCapsConfigFingerprintCoversSensitiveInputs(t *testing.T) {
	cfg := config.Default()
	cfg.VisionEnabled = false

	base := capsConfigFingerprint(cfg, fixtureGGUF)

	cfg.LLM.NumCtx = 4096
	if capsConfigFingerprint(cfg, fixtureGGUF) == base {
		t.Fatal("NumCtx change must change the fingerprint")
	}

	cfg.LLM.NumCtx = 0
	cfg.VisionEnabled = true
	if capsConfigFingerprint(cfg, fixtureGGUF) == base {
		t.Fatal("vision change must change the fingerprint")
	}
}
