// capabilities_test.go — v1.7.1: the shared backend capability contract
// and the per-backend candidate verdicts (§5.3/§5.7). Reuses the
// package's existing fakeBackend selection stub.
package llm

import (
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func TestCapabilitiesOfReportsUnknownWithoutReporter(t *testing.T) {
        b := &fakeBackend{name: config.BackendLlama, capable: true}
        caps, ok := CapabilitiesOf(b)
        if ok {
                t.Fatal("a backend without the reporter must report unknown capabilities")
        }
        if caps.Identity != config.BackendLlama {
                t.Fatalf("identity fallback: %q", caps.Identity)
        }
}

func TestBackendCandidatesShapeAndHonesty(t *testing.T) {
        cfg := config.Default()
        cfg.EngineBackend = "" // llama default

        // Without a native backend instance: native unavailable, CPU
        // available, Vulkan per probe (false in test environments without
        // the packaging — the reason must say so honestly).
        candidates := BackendCandidates(cfg, nil, nil, nil)
        if len(candidates) != 3 {
                t.Fatalf("candidate count = %d, want 3 (Native, CPU, Vulkan)", len(candidates))
        }

        native, cpu, vulkan := candidates[0], candidates[1], candidates[2]
        if native.Available {
                t.Fatalf("native without a backend must be unavailable: %+v", native)
        }
        if native.Reason == "" {
                t.Fatal("native unavailability must carry a reason")
        }
        if !cpu.Available {
                t.Fatalf("llama.cpp CPU is the default serving alternative: %+v", cpu)
        }
        if vulkan.Available {
                t.Fatal("test environments have no managed Vulkan runtime")
        }
        if vulkan.Reason == "" {
                t.Fatal("vulkan unavailability must carry a reason")
        }

        // Labels are the §5.7 user-facing serving alternatives.
        if native.Label != "Native Engine" || cpu.Label != "llama.cpp CPU" ||
                vulkan.Label != "llama.cpp Vulkan" {
                t.Fatalf("labels: %+v", candidates)
        }

        // Selected row mirrors the selection authority: llama default with
        // no native instance.
        if cpu.Selected {
                t.Fatalf("llama default must be the selected row: %+v", candidates)
        }
        if native.Selected {
                t.Fatalf("native must not be selected when absent: %+v", candidates)
        }
}

func TestBackendCandidatesNativeCapableAgreesWithSelection(t *testing.T) {
        cfg := config.Default()
        cfg.EngineBackend = config.BackendNative

        native := &fakeBackend{name: config.BackendNative, capable: true, hasCapFn: true}
        candidates := BackendCandidates(cfg, native, nil, nil)

        decision := SelectGenerationBackendDetailed(cfg, native, nil)
        if decision.SelectedName != config.BackendNative {
                t.Fatalf("selection: %+v", decision)
        }
        if !candidates[0].Selected || !candidates[0].Available {
                t.Fatalf("native row must agree with selection: %+v", candidates[0])
        }
        if candidates[1].Selected {
                t.Fatalf("llama row must not claim selection when native serves: %+v", candidates[1])
        }
}

func TestBackendCandidatesNativeNotCapableMirrorsFallback(t *testing.T) {
        cfg := config.Default()
        cfg.EngineBackend = config.BackendNative

        native := &fakeBackend{
                name:     config.BackendNative,
                capable:  false,
                hasCapFn: true,
                fallback: "no model loaded",
        }
        llama := &fakeBackend{name: config.BackendLlama, capable: true, hasCapFn: true}
        candidates := BackendCandidates(cfg, native, llama, nil)

        decision := SelectGenerationBackendDetailed(cfg, native, llama)
        if decision.SelectedName == config.BackendNative || decision.FallbackReason == "" {
                t.Fatalf("selection must fall back with a reason: %+v", decision)
        }

        if candidates[0].Available {
                t.Fatal("not-capable native must be unavailable")
        }
        if candidates[0].Reason != "no model loaded" {
                t.Fatalf("native reason must mirror the probe: %q", candidates[0].Reason)
        }
        if !candidates[1].Selected {
                t.Fatalf("llama row must be the selected fallback: %+v", candidates[1])
        }
}

func TestBackendCandidatesNativeIncompatibleModelSurfacesReason(t *testing.T) {
        cfg := config.Default()
        cfg.EngineBackend = config.BackendNative

        native := &fakeBackend{name: config.BackendNative, capable: true, hasCapFn: true}
        caps := &ModelCapabilities{
                Arch:          "deepseek3",
                NativeBackend: false,
                NativeReason:  "architecture not implemented in the native engine",
        }
        candidates := BackendCandidates(cfg, native, nil, caps)

        if candidates[0].Available {
                t.Fatal("a model the native engine cannot execute must show native unavailable")
        }
        if candidates[0].Reason != caps.NativeReason {
                t.Fatalf("reason must be the model-side verdict: %q", candidates[0].Reason)
        }
}
