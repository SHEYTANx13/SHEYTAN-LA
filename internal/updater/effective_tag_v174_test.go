package updater

// v1.7.4 (P0 #3): the EFFECTIVE engine-tag resolution regressions.
//
// Every "update required" decision point compares against
// EffectiveInstalledEngineTag. These tests pin its fallback ladder:
//
//  1. the tag recorded in installed.json wins;
//  2. a state file without a tag falls back to the committed
//     engine-install.json manifest beside the managed binary;
//  3. neither source knows the tag → "" (callers then apply their own
//     DefaultEngineTag fallback, unchanged).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func effectiveTagConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.LlamaBinPath = "" // EngineBinDir resolves to <DataDir>/bin

	return cfg
}

func writeEngineManifest(t *testing.T, cfg *config.Config, tag string) {
	t.Helper()

	binDir := EngineBinDir(cfg)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	manifest := `{"tag":"` + tag + `","sha256":"deadbeef","size":1024,"source":"test"}`

	if err := os.WriteFile(filepath.Join(binDir, installManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeStateWithEngineTag(t *testing.T, cfg *config.Config, tag string) {
	t.Helper()

	st := stateFile{Components: map[string]component{}}

	if tag != "" {
		st.Components["llamaServer"] = component{
			Status: "installed",
			Meta:   map[string]string{"engineTag": tag},
		}
	} else {
		// A tag-less legacy/detection-rewritten component.
		st.Components["llamaServer"] = component{
			Status: "installed",
			Meta:   map[string]string{"path": filepath.Join(EngineBinDir(cfg), "llama-server")},
		}
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}

	if err := os.WriteFile(cfg.StatePath(), data, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// TestEffectiveInstalledEngineTag is the fallback-ladder table.
func TestEffectiveInstalledEngineTag(t *testing.T) {
	cases := []struct {
		name        string
		stateTag    string // "" → tag-less component; "absent" → no state file
		stateAbsent bool
		manifestTag string // "" → no manifest
		want        string
	}{
		{
			name:        "recorded tag wins over the manifest",
			stateTag:    "b11223",
			manifestTag: "b99999",
			want:        "b11223",
		},
		{
			name:        "missing state file falls back to the manifest",
			stateAbsent: true,
			manifestTag: "b11223",
			want:        "b11223",
		},
		{
			name:        "tag-less state falls back to the manifest",
			stateTag:    "",
			manifestTag: "b11223",
			want:        "b11223",
		},
		{
			name:        "neither source knows the tag",
			stateAbsent: true,
			want:        "",
		},
		{
			name:     "tag-less state and no manifest",
			stateTag: "",
			want:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := effectiveTagConfig(t)

			if !tc.stateAbsent {
				writeStateWithEngineTag(t, cfg, tc.stateTag)
			}

			if tc.manifestTag != "" {
				writeEngineManifest(t, cfg, tc.manifestTag)
			}

			if got := EffectiveInstalledEngineTag(cfg); got != tc.want {
				t.Fatalf("EffectiveInstalledEngineTag = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestManifestEngineTagIgnoresTaglessManifest pins the manifest reader's
// honesty: a manifest that exists but carries no tag is NOT an identity.
func TestManifestEngineTagIgnoresTaglessManifest(t *testing.T) {
	cfg := effectiveTagConfig(t)

	binDir := EngineBinDir(cfg)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(binDir, installManifestName), []byte(`{"sha256":"x"}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if got := ManifestEngineTag(cfg); got != "" {
		t.Fatalf("a tag-less manifest must not yield a tag, got %q", got)
	}

	if got := EffectiveInstalledEngineTag(cfg); got != "" {
		t.Fatalf("EffectiveInstalledEngineTag = %q, want empty", got)
	}
}
