package llm

// variant_auto_v172_test.go — v1.7.2 (P0): the AUTO GPU candidate probe
// regression matrix (§20 "Vulkan").
//
// Driven through the REAL transaction path (the same seams as the v1.7.0
// rollback suite: platform override, variant-exists/release-list
// overrides, staged fake-engine archive, enumeration override) plus the
// v1.7.2 fake-engine modes:
//
//      vulkan-offload — the candidate prints the real offload evidence line
//      (default)      — serves health + generation, prints NO offload line
//      gen-fail       — the /completion endpoint answers 500
//
// The matrix proves: candidate startup failure, health failure, model
// load, generation failure, generation WITHOUT offload evidence,
// generation WITH real offload evidence, commit/rollback, AUTO bootstrap,
// verified-state reuse, stale-state invalidation, and the bounded failed
// probe.

import (
        "context"
        "encoding/json"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// autoProbeHarness wires the standard variant-transaction seams for one
// AUTO candidate attempt. mode selects the fake engine behaviour.
func autoProbeHarness(t *testing.T, mode string, enumOverride func(string) ([]accelerator.Device, bool, error)) (*LlamaServer, *config.Config) {
        t.Helper()

        restorePlatform := updater.SetPlatformForTest("windows", "amd64")
        t.Cleanup(restorePlatform)

        updater.SetVariantExistsForTest(func(context.Context, string, updater.AssetVariant) bool {
                return false
        })
        t.Cleanup(func() { updater.SetVariantExistsForTest(nil) })

        updater.SetReleaseListForTest(func(context.Context) ([]updater.ReleaseInfo, error) {
                return []updater.ReleaseInfo{{
                        TagName: "b11205",
                        Assets: []updater.AssetInfo{
                                {Name: "llama-b11205-bin-win-cpu-x64.zip"},
                                {Name: "llama-b11205-bin-win-vulkan-x64.zip"},
                        },
                }}, nil
        })
        t.Cleanup(func() { updater.SetReleaseListForTest(nil) })

        if enumOverride != nil {
                prev := enumerateEngineDevices
                enumerateEngineDevices = enumOverride
                t.Cleanup(func() { enumerateEngineDevices = prev })
        }

        if mode != "" {
                t.Setenv("GO_FAKE_LLAMA_MODE", mode)
        }

        cfg, _ := fakeManagedEngineConfig(t, "")
        srv := NewLlamaServer(config.NewSource(cfg))
        t.Cleanup(func() { _ = srv.Stop() })

        srv.SetStagedArchiveForTest(buildFakeEngineArchive(t, t.TempDir()))

        return srv, cfg
}

// enumVulkanDevice enumerates one real Vulkan device identity.
func enumVulkanDevice(string) ([]accelerator.Device, bool, error) {
        return []accelerator.Device{{
                Backend: "Vulkan0", Name: "Intel(R) Arc(TM) A770M Graphics", TotalMB: 16384,
                Source: "engine-enumeration",
        }}, true, nil
}

// enumNoVulkanDevice: enumeration supported, no Vulkan device.
func enumNoVulkanDevice(string) ([]accelerator.Device, bool, error) {
        return nil, true, nil
}

// enumUnsupported: the build does not implement --list-devices.
func enumUnsupported(string) ([]accelerator.Device, bool, error) {
        return nil, false, nil
}

func testProbeIdentity() string {
        return GPUProbeIdentity("windows", "amd64", "b11205", "vulkan", "Intel Arc A770M (driver 32.0.101)")
}

// TestAutoProbeVerifiedChainCommits: the FULL success chain — asset
// resolved, package staged, device enumerated (the ACTUAL identity), real
// generation, real offload evidence, COMMIT, verified state persisted.
func TestAutoProbeVerifiedChainCommits(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "vulkan-offload", enumVulkanDevice)

        outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if outcome.Err != nil {
                t.Fatalf("the verified chain must commit: %v", outcome.Err)
        }
        if outcome.Skipped {
                t.Fatal("a fresh identity must run the candidate transaction")
        }

        // The engine now carries the vulkan variant (committed).
        if got := updater.InstalledEngineVariant(cfg); got != updater.VariantVulkan {
                t.Fatalf("committed candidate must be the vulkan variant, got %q", got)
        }

        // The persisted state is verified with the evidence fields.
        st, ok := LoadGPUProbeState(cfg)
        if !ok || st.Status != GPUProbeStatusVerified {
                t.Fatalf("verified state must persist: %+v ok=%v", st, ok)
        }
        if st.Identity != testProbeIdentity() {
                t.Fatalf("state identity mismatch: %q", st.Identity)
        }
        if !strings.Contains(st.Device, "Vulkan0: Intel(R) Arc(TM) A770M Graphics") {
                t.Fatalf("the ACTUAL enumerated device identity must be recorded, got %q", st.Device)
        }
        if !strings.Contains(st.Evidence, "offloaded 33/33 layers to GPU") {
                t.Fatalf("the offload evidence line must be recorded, got %q", st.Evidence)
        }
        if st.EvidenceSource != "engine-enumeration" {
                t.Fatalf("evidence source must be the engine enumeration, got %q", st.EvidenceSource)
        }
}

// TestAutoProbeVerifiedStateReused: a verified state for the SAME identity
// is reused — no second transaction, no second download.
func TestAutoProbeVerifiedStateReused(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "vulkan-offload", enumVulkanDevice)

        // Pre-write the verified state for the current identity.
        saveGPUProbeState(cfg, GPUProbeState{
                Status: GPUProbeStatusVerified, Identity: testProbeIdentity(),
                EngineTag: "b11205", Variant: "vulkan",
                Device: "Vulkan0: Intel(R) Arc(TM) A770M Graphics",
                Evidence: "offloaded 33/33 layers to GPU", EvidenceSource: "engine-enumeration",
                Reason: "previous probe", At: "2026-09-01T00:00:00Z",
        })

        outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if !outcome.Skipped {
                t.Fatal("a verified state for the same identity must be reused, never re-probed")
        }
        if outcome.State == nil || outcome.State.Status != GPUProbeStatusVerified {
                t.Fatalf("reuse must surface the verified state: %+v", outcome.State)
        }
        // Still the CPU package — no transaction ran.
        if got := updater.InstalledEngineVariant(cfg); got != updater.VariantCPU {
                t.Fatalf("no transaction may run on reuse, variant=%q", got)
        }
}

// TestAutoProbeBoundedFailedProbe: a failed probe for the same identity is
// NEVER retried — no transaction, no download loop.
func TestAutoProbeBoundedFailedProbe(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "vulkan-offload", enumVulkanDevice)

        // A previously failed probe for the current identity.
        saveGPUProbeState(cfg, GPUProbeState{
                Status: GPUProbeStatusFailed, Identity: testProbeIdentity(),
                Reason: "no Vulkan device enumerated",
                At:     "2026-09-01T00:00:00Z",
        })

        outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if !outcome.Skipped {
                t.Fatal("a failed probe for the same identity must be bounded — never retried")
        }
        if outcome.State == nil || outcome.State.Status != GPUProbeStatusFailed {
                t.Fatalf("bounded skip must surface the failed state: %+v", outcome.State)
        }
        if got := updater.InstalledEngineVariant(cfg); got != updater.VariantCPU {
                t.Fatalf("no transaction may run after a bounded failure, variant=%q", got)
        }
}

// TestAutoProbeStaleIdentityInvalidates: a failure recorded for a
// DIFFERENT hardware/engine identity does not block the new probe.
func TestAutoProbeStaleIdentityInvalidates(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "vulkan-offload", enumVulkanDevice)

        // A failed probe from the OLD hardware identity (driver update, GPU
        // swap, engine update — any identity change).
        oldIdentity := GPUProbeIdentity("windows", "amd64", "b10642", "vulkan", "Intel Arc A770M (driver 31.0.100)")
        saveGPUProbeState(cfg, GPUProbeState{
                Status: GPUProbeStatusFailed, Identity: oldIdentity,
                Reason: "no Vulkan device enumerated (old driver)",
                At:     "2026-08-01T00:00:00Z",
        })

        outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if outcome.Skipped {
                t.Fatal("a stale-identity failure must not block the current probe")
        }
        if outcome.Err != nil {
                t.Fatalf("the fresh probe must verify: %v", outcome.Err)
        }

        st, _ := LoadGPUProbeState(cfg)
        if st.Identity != testProbeIdentity() {
                t.Fatalf("the new state must carry the CURRENT identity, got %q", st.Identity)
        }
}

// TestAutoProbeNoVulkanDeviceRollsBack: enumeration supported but NO
// Vulkan device → candidate fails → rollback to the CPU package →
// bounded failed state with the exact reason.
func TestAutoProbeNoVulkanDeviceRollsBack(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "vulkan-offload", enumNoVulkanDevice)

        outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if outcome.Err == nil {
                t.Fatal("a candidate with no usable Vulkan device must FAIL the probe")
        }
        if !strings.Contains(outcome.Err.Error(), "NO usable Vulkan device") {
                t.Fatalf("the exact missing evidence layer must be named: %v", outcome.Err)
        }

        // The CPU package is restored (rollback).
        if got := updater.InstalledEngineVariant(cfg); got != updater.VariantCPU {
                t.Fatalf("rollback must restore the CPU package, got %q", got)
        }

        st, ok := LoadGPUProbeState(cfg)
        if !ok || st.Status != GPUProbeStatusFailed {
                t.Fatalf("bounded failed state must persist: %+v", st)
        }

        // And the failure is bounded for this identity.
        again := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if !again.Skipped {
                t.Fatal("the failed probe must not repeat for the same identity")
        }
}

// TestAutoProbeGenerationWithoutOffloadEvidenceRollsBack: the candidate
// serves health AND a real generation, but produces NO GPU-offload line —
// execution is UNVERIFIED; the probe fails honestly, never claims
// GPU_VULKAN, and rolls back.
func TestAutoProbeGenerationWithoutOffloadEvidenceRollsBack(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "", enumVulkanDevice) // no offload mode

        outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if outcome.Err == nil {
                t.Fatal("generation without offload evidence must FAIL the AUTO probe (unverified execution)")
        }
        if !strings.Contains(outcome.Err.Error(), "NO GPU-offload evidence") {
                t.Fatalf("the unverified-execution reason must be exact: %v", outcome.Err)
        }

        if got := updater.InstalledEngineVariant(cfg); got != updater.VariantCPU {
                t.Fatalf("rollback must restore the CPU package, got %q", got)
        }

        st, _ := LoadGPUProbeState(cfg)
        if st.Status != GPUProbeStatusFailed || !strings.Contains(st.Reason, "GPU-offload") {
                t.Fatalf("bounded failed state must record the missing evidence layer: %+v", st)
        }
}

// TestAutoProbeGenerationFailureRollsBack: the candidate's generation
// endpoint fails → candidate failure → rollback + bounded state.
func TestAutoProbeGenerationFailureRollsBack(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "gen-fail", enumVulkanDevice)

        outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if outcome.Err == nil {
                t.Fatal("a broken generation path must fail the probe")
        }
        if !strings.Contains(outcome.Err.Error(), "generation probe failed") {
                t.Fatalf("the generation failure must be named: %v", outcome.Err)
        }
        if got := updater.InstalledEngineVariant(cfg); got != updater.VariantCPU {
                t.Fatalf("rollback must restore the CPU package, got %q", got)
        }
}

// TestAutoProbeEnumerationUnsupportedRequiresOffloadEvidence: when the
// build does not support --list-devices, the offload line alone still
// verifies (it is the stronger runtime execution evidence); the device
// identity is attributed to the engine log, never invented.
func TestAutoProbeEnumerationUnsupportedRequiresOffloadEvidence(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "vulkan-offload", enumUnsupported)

        outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if outcome.Err != nil {
                t.Fatalf("offload evidence with unsupported enumeration must still verify: %v", outcome.Err)
        }

        st, _ := LoadGPUProbeState(cfg)
        if st.Status != GPUProbeStatusVerified {
                t.Fatalf("state must be verified: %+v", st)
        }
        if st.EvidenceSource != "engine-log" {
                t.Fatalf("evidence source must be the engine log, got %q", st.EvidenceSource)
        }
        if !strings.Contains(st.Device, "unattributed") {
                t.Fatalf("the device identity must be honest about the missing enumeration, got %q", st.Device)
        }
}

// TestAutoProbeAlreadyVulkanNeverReprobes: a defensive no-op — once the
// vulkan variant is INSTALLED, a later probe call (even with a FRESH
// identity, so the bounded-state gates cannot answer it) is skipped with
// a reason, and NO verified state is fabricated from a no-op
// transaction.
func TestAutoProbeAlreadyVulkanNeverReprobes(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "vulkan-offload", enumVulkanDevice)

        // First probe: verifies and commits the vulkan package.
        first := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity())
        if first.Err != nil {
                t.Fatalf("the first probe must verify: %v", first.Err)
        }
        if got := updater.InstalledEngineVariant(cfg); got != updater.VariantVulkan {
                t.Fatalf("the committed variant must be vulkan, got %q", got)
        }

        // Second probe with a FRESH identity (e.g. a driver update changed the
        // fingerprint): the bounded-state gates pass, so ONLY the
        // already-installed guard can prevent a needless second download.
        freshIdentity := GPUProbeIdentity("windows", "amd64", "b11205", "vulkan", "Intel Arc A770M (driver 99.0.999)")
        second := srv.AutoProvisionVulkanIfWorthy(context.Background(), freshIdentity)
        if !second.Skipped {
                t.Fatalf("an already-vulkan installation must skip the re-probe, got %+v", second)
        }
        if !strings.Contains(second.Note, "already carries the vulkan variant") {
                t.Fatalf("the skip reason must name the already-installed guard: %q", second.Note)
        }
}

// TestGPUProbeIdentityComponents: the fingerprint covers OS/arch + GPU
// identity + engine tag + variant — every component that can invalidate
// the persisted outcome.
func TestGPUProbeIdentityComponents(t *testing.T) {
        base := GPUProbeIdentity("windows", "amd64", "b11205", "vulkan", "gpuA (driver 1)")
        if GPUProbeIdentity("linux", "amd64", "b11205", "vulkan", "gpuA (driver 1)") == base {
                t.Fatal("OS change must invalidate")
        }
        if GPUProbeIdentity("windows", "arm64", "b11205", "vulkan", "gpuA (driver 1)") == base {
                t.Fatal("arch change must invalidate")
        }
        if GPUProbeIdentity("windows", "amd64", "b11300", "vulkan", "gpuA (driver 1)") == base {
                t.Fatal("engine tag change must invalidate")
        }
        if GPUProbeIdentity("windows", "amd64", "b11205", "cpu", "gpuA (driver 1)") == base {
                t.Fatal("variant change must invalidate")
        }
        if GPUProbeIdentity("windows", "amd64", "b11205", "vulkan", "gpuB (driver 1)") == base {
                t.Fatal("GPU identity change must invalidate")
        }
        if GPUProbeIdentity("windows", "amd64", "b11205", "vulkan", "gpuA (driver 2)") == base {
                t.Fatal("driver change must invalidate")
        }
}

// TestAutoProbeStatePersistsAtomically: the state file is valid JSON with
// the evidence fields (the /api/engine/provision surface renders it).
func TestAutoProbeStatePersistsAtomically(t *testing.T) {
        srv, cfg := autoProbeHarness(t, "vulkan-offload", enumVulkanDevice)

        if outcome := srv.AutoProvisionVulkanIfWorthy(context.Background(), testProbeIdentity()); outcome.Err != nil {
                t.Fatalf("verified chain: %v", outcome.Err)
        }

        data, err := os.ReadFile(filepath.Join(cfg.DataDir, "gpu-probe.json"))
        if err != nil {
                t.Fatalf("state file: %v", err)
        }
        var st GPUProbeState
        if err := json.Unmarshal(data, &st); err != nil {
                t.Fatalf("state file must be valid JSON: %v", err)
        }
        if st.Status != GPUProbeStatusVerified || st.Reason == "" || st.At == "" {
                t.Fatalf("persisted state must carry the full evidence record: %+v", st)
        }

        // The API snapshot exposes the same state.
        if snap := srv.AutoGPUProbeSnapshot(); snap == nil || snap.Status != GPUProbeStatusVerified {
                t.Fatalf("snapshot must expose the verified state: %+v", snap)
        }
}
