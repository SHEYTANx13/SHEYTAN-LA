package llm

// variant_runtime_v162_test.go — v1.6.2: the Vulkan RUNTIME TRUTH
// contract of the provisioning transaction.
//
// The transaction is stop → staged install → startup/health → RUNTIME
// BACKEND VERIFICATION → commit. The verification step must take its
// evidence from the ENGINE ITSELF (--list-devices enumeration), never
// from the asset filename, the manifest field, or a ggml-vulkan.dll on
// disk. The verified-evidence test drives the REAL chain end-to-end:
// the fake engine binary is installed through the archive seam,
// started (health-checked), and its --list-devices output (Vulkan
// devices) is what the commit records.

import (
        "context"
        "errors"
        "strings"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/accelerator"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// driveVariantTransaction runs UpdateEngineVariantNow(VULKAN) with the
// deterministic Windows/amd64 platform identity, a release list that
// serves the Vulkan asset at b11191, and the archive seam. enumOverride
// replaces the engine enumeration when non-nil (the error/unsupported
// paths); nil keeps the REAL engine probe (the fake binary answers
// --list-devices with Vulkan devices).
func driveVariantTransaction(t *testing.T, enumOverride func(string) ([]accelerator.Device, bool, error)) (string, error, *config.Config) {
        t.Helper()

        restorePlatform := updater.SetPlatformForTest("windows", "amd64")
        t.Cleanup(restorePlatform)

        updater.SetVariantExistsForTest(func(context.Context, string, updater.AssetVariant) bool {
                return false // the pinned/current tag never serves it — force the list scan
        })
        t.Cleanup(func() { updater.SetVariantExistsForTest(nil) })

        updater.SetReleaseListForTest(func(context.Context) ([]updater.ReleaseInfo, error) {
                return []updater.ReleaseInfo{{
                        TagName: "b11191",
                        Assets: []updater.AssetInfo{
                                {Name: "llama-b11191-bin-win-cpu-x64.zip"},
                                {Name: "llama-b11191-bin-win-vulkan-x64.zip"},
                        },
                }}, nil
        })
        t.Cleanup(func() { updater.SetReleaseListForTest(nil) })

        if enumOverride != nil {
                prevEnum := enumerateEngineDevices
                enumerateEngineDevices = enumOverride
                t.Cleanup(func() { enumerateEngineDevices = prevEnum })
        }

        cfg, _ := fakeManagedEngineConfig(t, "")

        srv := NewLlamaServer(config.NewSource(cfg))
        t.Cleanup(func() { _ = srv.Stop() })

        srv.SetStagedArchiveForTest(buildFakeEngineArchive(t, t.TempDir()))

        outcome, err := srv.UpdateEngineVariantNow(context.Background(), updater.VariantVulkan, nil)
        return outcome, err, cfg
}

// The real chain end-to-end: the fake engine answers --list-devices
// with Vulkan devices, so the transaction commits with RUNTIME
// evidence recorded in the outcome — and the committed manifest carries
// the vulkan identity.
func TestVariantTransactionCommitsWithRuntimeVulkanEvidence(t *testing.T) {
        outcome, err, cfg := driveVariantTransaction(t, nil)
        if err != nil {
                t.Fatalf("vulkan provisioning transaction: %v", err)
        }

        for _, want := range []string{"enumerated", "Vulkan", "runtime"} {
                if !strings.Contains(outcome, want) {
                        t.Fatalf("outcome must carry the runtime verification evidence (%q missing): %q", want, outcome)
                }
        }
        if strings.Contains(outcome, "manifest only") {
                t.Fatalf("a runtime-verified transaction must not report manifest-only identity: %q", outcome)
        }

        // The committed package identity matches the requested variant.
        if got := updater.InstalledEngineVariant(cfg); got != updater.VariantVulkan {
                t.Fatalf("committed manifest variant = %q, want vulkan", got)
        }
}

// An enumeration that CANNOT EXECUTE is a package-integrity failure:
// the transaction must fail and roll back to the last-known-good
// package.
func TestVariantTransactionRollsBackWhenEnumerationCannotExecute(t *testing.T) {
        _, err, _ := driveVariantTransaction(t, func(string) ([]accelerator.Device, bool, error) {
                return nil, false, errors.New("fork/exec: exited with a corrupted image")
        })
        if err == nil {
                t.Fatal("a verification that cannot execute must fail the transaction")
        }
        if !strings.Contains(err.Error(), "runtime backend verification failed") {
                t.Fatalf("the failure must be classified as a verification failure: %v", err)
        }
        if !strings.Contains(err.Error(), "previous package restored") {
                t.Fatalf("the failure must report the rollback: %v", err)
        }
}

// A build that does not implement --list-devices still commits (the
// package IS what was requested) — but the outcome must HONESTLY
// attribute the identity to the manifest, never to runtime evidence.
func TestVariantTransactionHonestWhenEnumerationUnsupported(t *testing.T) {
        outcome, err, _ := driveVariantTransaction(t, func(string) ([]accelerator.Device, bool, error) {
                return nil, false, nil // supported=false, no error
        })
        if err != nil {
                t.Fatalf("vulkan provisioning transaction: %v", err)
        }

        if !strings.Contains(outcome, "does not support device enumeration") {
                t.Fatalf("the outcome must honestly report the unsupported enumeration: %q", outcome)
        }
        if !strings.Contains(outcome, "manifest") {
                t.Fatalf("the identity attribution must name the manifest: %q", outcome)
        }
}

// A healthy Vulkan package on a machine with NO Vulkan device commits
// (that is exactly what was requested — the package still serves on
// CPU) with an explicit, honest "no Vulkan device here" note.
func TestVariantTransactionHonestWhenNoVulkanDeviceOnMachine(t *testing.T) {
        outcome, err, _ := driveVariantTransaction(t, func(string) ([]accelerator.Device, bool, error) {
                return []accelerator.Device{
                        {Backend: "CUDA0", Name: "Some GPU", TotalMB: 8192, Source: "engine-enumeration"},
                }, true, nil
        })
        if err != nil {
                t.Fatalf("vulkan provisioning transaction: %v", err)
        }

        if !strings.Contains(outcome, "no Vulkan device") {
                t.Fatalf("the outcome must honestly report the absent Vulkan device: %q", outcome)
        }
}
