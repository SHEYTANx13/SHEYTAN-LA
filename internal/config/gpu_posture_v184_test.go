package config

// gpu_posture_v184_test.go — v1.8.4 (P0-B): the derived-GPU-posture
// repair (repairDerivedGPUPosture) and its exact boundaries.
//
// THE DEFECT: the pre-1.8.4 recommendation pipeline wrote
// gpuAutoOffload=false + numGpu=0 as a DERIVED posture ("running CPU-only
// until a Vulkan engine build is provisioned"). The AUTO Vulkan candidate
// gate then read that posture as an explicit OFF and refused the candidate
// forever — the observed "GPU AUTO Vulkan candidate not considered this
// boot: GPU offload is disabled in settings (numGPU=0, auto-offload off)"
// on a machine whose requested profile was AUTO.
//
// The contract under test:
//
//   - a derived (marker-less) CPU posture under a non-CPU profile is
//     repaired to the AUTO default (true) ONCE and persisted;
//   - an EXPLICIT user OFF (gpuAutoOffloadUserSet) is NEVER touched;
//   - an explicit manual layer count (numGpu>0) is NEVER touched;
//   - an explicit CPU REQUESTED PROFILE is NEVER touched;
//   - the repair is idempotent and logs an honest note.
import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func v184WriteConfig(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func TestDerivedGPUPostureRepairedUnderAuto(t *testing.T) {
	path := v184WriteConfig(t, `{
		"accelerator": "AUTO",
		"gpuAutoOffload": false,
		"llm": {"numGpu": 0}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !cfg.GPUAutoOffload {
		t.Fatal("derived CPU posture must be repaired to the AUTO default (true)")
	}

	found := false
	for _, note := range cfg.PathNotes {
		if strings.Contains(note, "repaired derived GPU posture") {
			found = true
		}
	}
	if !found {
		t.Fatalf("repair must be reported honestly, notes: %v", cfg.PathNotes)
	}

	// The repair is persisted: a reload reads the repaired value from the
	// file (no repeated repair loop).
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.GPUAutoOffload {
		t.Fatal("reloaded config must carry the persisted repair")
	}
	for _, note := range reloaded.PathNotes {
		if strings.Contains(note, "repaired derived GPU posture") {
			t.Fatalf("the repair must NOT re-fire on a repaired config, notes: %v", reloaded.PathNotes)
		}
	}
}

func TestUserSetGPUOffIsNeverTouched(t *testing.T) {
	path := v184WriteConfig(t, `{
		"accelerator": "AUTO",
		"gpuAutoOffload": false,
		"gpuAutoOffloadUserSet": true,
		"llm": {"numGpu": 0}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.GPUAutoOffload {
		t.Fatal("an explicit user OFF must remain OFF (the explicit-OFF contract)")
	}

	for _, note := range cfg.PathNotes {
		if strings.Contains(note, "repaired derived GPU posture") {
			t.Fatalf("an explicit user OFF must never be repaired, notes: %v", cfg.PathNotes)
		}
	}
}

func TestManualLayerCountIsNeverTouched(t *testing.T) {
	path := v184WriteConfig(t, `{
		"accelerator": "AUTO",
		"gpuAutoOffload": false,
		"llm": {"numGpu": 24}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.GPUAutoOffload {
		t.Fatal("an explicit manual layer count defines the posture — the repair must not fire")
	}
}

func TestExplicitCPUProfileIsNeverTouched(t *testing.T) {
	path := v184WriteConfig(t, `{
		"accelerator": "CPU",
		"gpuAutoOffload": false,
		"llm": {"numGpu": 0}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.GPUAutoOffload {
		t.Fatal("an explicit CPU requested profile is a policy-level OFF — untouched")
	}
}

func TestFreshDefaultConfigCarriesAutoPosture(t *testing.T) {
	cfg := Default()

	if !cfg.GPUAutoOffload {
		t.Fatal("the AUTO posture must stay the documented default")
	}
	if cfg.GPUAutoOffloadUserSet {
		t.Fatal("a fresh config is never user-set")
	}
}
