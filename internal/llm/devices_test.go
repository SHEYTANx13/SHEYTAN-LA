package llm

// devices_test.go — v1.2.6 tests for the engine device-enumeration output
// parser and the runtime offload-line parser. Pure functions with FIXTURE
// outputs modelled on llama.cpp's actual formats. (The live enumeration
// itself requires an installed engine binary — covered by the integration
// paths on machines that have one.)

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func TestParseEngineDeviceOutputVulkan(t *testing.T) {
	out := `Available devices:
  Vulkan0: Intel(R) Arc(TM) A770M Graphics (16384 MiB)
  Vulkan1: Intel(R) Iris(R) Xe Graphics (1024 MiB)
`

	devices, supported, err := ParseEngineDeviceOutput(out)
	if err != nil || !supported {
		t.Fatalf("parse failed: supported=%t err=%v", supported, err)
	}

	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(devices))
	}

	if devices[0].Backend != "Vulkan0" || devices[0].Name != "Intel(R) Arc(TM) A770M Graphics" || devices[0].TotalMB != 16384 {
		t.Fatalf("primary device = %+v", devices[0])
	}

	if devices[0].Source != "engine-enumeration" {
		t.Fatalf("source = %q", devices[0].Source)
	}
}

func TestParseEngineDeviceOutputCUDANoMemory(t *testing.T) {
	out := `Available devices:
  CUDA0: NVIDIA GeForce RTX 3090 (24576 MiB)
  Metal0: Apple M2 Max
`

	devices, supported, err := ParseEngineDeviceOutput(out)
	if err != nil || !supported {
		t.Fatalf("parse failed: %v", err)
	}

	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(devices))
	}

	// Metal line without memory — TotalMB stays 0, name intact.
	byBackend := map[string]accelerator.Device{}
	for _, d := range devices {
		byBackend[d.Backend] = d
	}

	if m := byBackend["Metal0"]; m.Name != "Apple M2 Max" || m.TotalMB != 0 {
		t.Fatalf("metal device = %+v", m)
	}
}

func TestParseEngineDeviceOutputUnsupportedFlag(t *testing.T) {
	out := `error: unknown argument --list-devices
usage: llama-server [options]`

	devices, supported, err := ParseEngineDeviceOutput(out)
	if err != nil {
		t.Fatalf("unsupported is not an error: %v", err)
	}

	if supported {
		t.Fatal("supported must be false for an unknown-argument rejection")
	}

	if devices != nil {
		t.Fatalf("devices = %v, want none", devices)
	}
}

func TestParseEngineDeviceOutputEmpty(t *testing.T) {
	_, _, err := ParseEngineDeviceOutput("   \n\n")
	if err == nil {
		t.Fatal("empty output must be an error (no evidence)")
	}
}

func TestParseOffloadLine(t *testing.T) {
	cases := map[string]struct {
		line string
		want string
		ok   bool
	}{
		"plain":     {"offloaded 33/33 layers to GPU", "offloaded 33/33 layers to GPU", true},
		"prefixed":  {"llm_load_tensors: offloaded 35/35 layers to GPU", "offloaded 35/35 layers to GPU", true},
		"partial":   {"offloaded 20/35 layers to GPU", "offloaded 20/35 layers to GPU", true},
		"cpu only":  {"offloaded 33/33 layers to CPU", "", false},
		"unrelated": {"llama_new_context_with_model: n_ctx = 8192", "", false},
		"kv line":   {"KV self size = 1024.00 MiB", "", false},
		"empty":     {"", "", false},
	}

	for name, tc := range cases {
		got, ok := ParseOffloadLine(tc.line)

		if ok != tc.ok {
			t.Fatalf("%s: ok = %t, want %t", name, ok, tc.ok)
		}

		if ok && got != tc.want {
			t.Fatalf("%s: evidence = %q, want %q", name, got, tc.want)
		}
	}
}

func TestObserveEngineLineRecordsOffloadEvidence(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	s := NewLlamaServer(config.NewSource(cfg))

	if s.OffloadEvidence() != "" {
		t.Fatal("fresh server must carry no offload evidence")
	}

	s.ObserveEngineLine("llama_model_load_internal: offloaded 33/33 layers to GPU")

	if ev := s.OffloadEvidence(); ev != "offloaded 33/33 layers to GPU" {
		t.Fatalf("offload evidence = %q", ev)
	}

	// A later, weaker line must not overwrite the strong evidence.
	s.ObserveEngineLine("some unrelated line")

	if ev := s.OffloadEvidence(); !strings.Contains(ev, "offloaded 33/33") {
		t.Fatalf("evidence was clobbered: %q", ev)
	}
}
