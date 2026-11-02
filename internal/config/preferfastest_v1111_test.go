package config

// preferfastest_v1111_test.go — v1.11.1: the speed-priority preference
// (PreferFastest) and the Smooth-streaming default, both operator
// requests. The contract under test:
//
//   - fresh default: PreferFastest is ON, SmoothStream ON, TargetFPS 120;
//   - a LEGACY config without the field keeps the ON default (the
//     default-primed unmarshal never zero-fills an absent bool);
//   - an EXPLICIT stored false STAYS false across loads — the default is
//     never re-applied over a user opt-out;
//   - a config round-trip (Save → Load) preserves both states exactly;
//   - the settings-facing JSON name is "preferFastest".
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func v1111WriteConfig(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func TestPreferFastestFreshDefaultOn(t *testing.T) {
	cfg := Default()

	if !cfg.PreferFastest {
		t.Fatal("fresh default: PreferFastest must be ON (operator request: fastest safe inference enabled by default)")
	}
	if !cfg.SmoothStream {
		t.Fatal("fresh default: SmoothStream must stay ON")
	}
	if cfg.TargetFPS != 120 {
		t.Fatalf("fresh default: TargetFPS = %d, want 120", cfg.TargetFPS)
	}
}

func TestPreferFastestLegacyAbsentFieldStaysDefaultOn(t *testing.T) {
	// A v1.11.0-era config: no preferFastest key at all.
	path := v1111WriteConfig(t, `{
                "model": "seed.gguf",
                "smoothStream": true,
                "llm": {"numCtx": 8192}
        }`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}

	if !cfg.PreferFastest {
		t.Fatal("legacy config without the field must load PreferFastest=ON (absent field never zero-fills)")
	}
	if !cfg.SmoothStream {
		t.Fatal("legacy config must keep smoothStream=true as stored")
	}
}

func TestPreferFastestExplicitFalseSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := fmt.Sprintf(`{
                "dataDir": %q,
                "model": "seed.gguf",
                "preferFastest": false,
                "smoothStream": false
        }`, dir)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	for round := 1; round <= 2; round++ {
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load round %d: %v", round, err)
		}
		if got := cfg.ConfigPath(); got != path {
			t.Fatalf("round %d: ConfigPath = %q, want %q (the fixture must stay self-contained)", round, got, path)
		}
		if cfg.PreferFastest {
			t.Fatalf("round %d: explicit stored false must stay false — the ON default may never overwrite a user opt-out", round)
		}
		if cfg.SmoothStream {
			t.Fatalf("round %d: explicit smoothStream=false must stay false", round)
		}

		// Round-trip through Save: the explicit opt-outs persist verbatim.
		if err := Save(cfg.ConfigPath(), cfg); err != nil {
			t.Fatalf("save round %d: %v", round, err)
		}
	}
}

func TestPreferFastestRoundTripTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.DataDir = dir // keep the derived roots inside the temp fixture
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reloaded.PreferFastest {
		t.Fatal("round-trip: PreferFastest=true must persist")
	}

	// The wire name is stable (the settings surface patches this key).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("probe saved config: %v", err)
	}
	if _, ok := probe["preferFastest"]; !ok {
		t.Fatal("saved config must carry the preferFastest key (the Settings patch contract)")
	}
}
