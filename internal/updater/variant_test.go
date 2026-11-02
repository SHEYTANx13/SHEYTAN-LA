package updater

// variant_test.go — v1.6.1: the backend-variant provisioning contract.
//
// Vulkan must be real, not cosmetic. These tests pin the DETERMINISTIC
// parts of that contract on every platform:
//
//   - the asset naming (the REAL upstream llama.cpp Vulkan package);
//   - unsupported variants fail loudly, never resolving to CPU;
//   - the install manifest records the variant (and v1.6.0-era manifests
//     read back as "cpu");
//   - the archive-seam installer produces a manifest carrying the variant
//     and refuses variants without an asset on this platform.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

func TestAssetNameForVariantCPU(t *testing.T) {
	for _, tag := range []string{"b10642", "b9999"} {
		if got := AssetNameForVariant(tag, VariantCPU); got != AssetName(tag) {
			t.Fatalf("CPU variant must keep the historical asset name for %s: %q vs %q", tag, got, AssetName(tag))
		}
	}
}

func TestAssetNameForVariantVulkan(t *testing.T) {
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}

	// The deterministic naming contract: what the URL looks like is pure
	// string logic; whether the platform serves it is separate.
	name := fmt.Sprintf("llama-b1234-bin-win-vulkan-%s.zip", arch)
	url := AssetURLForVariant("b1234", VariantVulkan)

	if runtime.GOOS == "windows" {
		if name == "" || url == "" {
			t.Fatal("Windows must name the Vulkan asset")
		}
		if AssetNameForVariant("b1234", VariantVulkan) != name {
			t.Fatalf("Windows Vulkan asset name = %q, want %q", AssetNameForVariant("b1234", VariantVulkan), name)
		}
		if !strings.Contains(url, "/llama.cpp/releases/download/b1234/"+name) {
			t.Fatalf("Windows Vulkan asset URL = %q", url)
		}
	} else {
		// Non-Windows: no prebuilt Vulkan asset exists upstream — the
		// honest answer is "", never a CPU URL in Vulkan clothing.
		if got := AssetNameForVariant("b1234", VariantVulkan); got != "" {
			t.Fatalf("non-Windows must not claim a Vulkan prebuilt, got %q", got)
		}
		if got := AssetURLForVariant("b1234", VariantVulkan); got != "" {
			t.Fatalf("non-Windows must not claim a Vulkan URL, got %q", got)
		}
	}
}

func TestSupportedVariantsHonesty(t *testing.T) {
	variants := SupportedVariants()

	if runtime.GOOS == "windows" {
		if len(variants) != 2 || variants[1] != VariantVulkan {
			t.Fatalf("Windows supports CPU + Vulkan, got %v", variants)
		}
	} else {
		if len(variants) != 1 || variants[0] != VariantCPU {
			t.Fatalf("non-Windows supports CPU only, got %v", variants)
		}
		if VariantSupported(VariantVulkan) {
			t.Fatal("non-Windows must not claim Vulkan support")
		}
	}
}

func TestResolveDownloadURLForVariantNeverSilentlyFallsBackToCPU(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows genuinely serves the Vulkan asset — the loud-refusal path is exercised on other platforms")
	}

	// On a platform with no Vulkan prebuilt, an EXPLICIT Vulkan request
	// must error — never resolve the CPU asset instead.
	url, tag, err := ResolveDownloadURLForVariant(context.Background(), VariantVulkan)
	if err == nil {
		t.Fatalf("explicit VULKAN on %s must fail loudly, got url=%s tag=%s", runtime.GOOS, url, tag)
	}

	for _, want := range []string{"vulkan", runtime.GOOS} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal must name %q: %v", want, err)
		}
	}

	// And the error must NOT have silently produced a CPU URL.
	if strings.Contains(url, "-bin-win-cpu-") {
		t.Fatal("VULKAN refusal must never yield the CPU asset URL")
	}
}

func TestNormalizeAssetVariant(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want AssetVariant
	}{
		{"", VariantCPU},
		{"cpu", VariantCPU},
		{"CPU", VariantCPU},
		{"vulkan", VariantVulkan},
		{"Vulkan", VariantVulkan},
		{"GPU_VULKAN", VariantVulkan},
		{"weird", VariantCPU},
	} {
		if got := NormalizeAssetVariant(tc.in); got != tc.want {
			t.Fatalf("NormalizeAssetVariant(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInstalledEngineVariantReadsManifest(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.LlamaBinPath = ""

	binDir := EngineBinDir(cfg)
	_ = os.MkdirAll(binDir, 0o755)

	// No manifest: CPU (the historical default).
	if got := InstalledEngineVariant(cfg); got != VariantCPU {
		t.Fatalf("absent manifest must read as cpu, got %q", got)
	}

	// v1.6.0-era manifest WITHOUT the variant field: reads back as "cpu".
	legacy := map[string]any{
		"tag":         "b10642",
		"sha256":      "abc",
		"size":        123,
		"installedAt": "2026-01-01T00:00:00Z",
		"source":      "https://example/llama-b10642-bin-win-cpu-x64.zip",
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(binDir, installManifestName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := InstalledEngineVariant(cfg); got != VariantCPU {
		t.Fatalf("legacy manifest must read as cpu, got %q", got)
	}

	// v1.6.1 manifest WITH the variant field.
	legacy["variant"] = "vulkan"
	data, _ = json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(binDir, installManifestName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := InstalledEngineVariant(cfg); got != VariantVulkan {
		t.Fatalf("variant manifest must read as vulkan, got %q", got)
	}
}

// TestWindowsArm64VulkanReconciledAgainstOfficialEvidence (v1.11.1): the
// official b12032 release (2026-11-01) publishes
// llama-b12032-bin-win-vulkan-arm64.zip — the pre-b12032 matrix (b11191
// evidence) wrongly kept Windows ARM64 CPU-only. The reconciliation:
// windows/arm64 names BOTH asset variants and advertises CPU+Vulkan in
// the support matrix (the ZIP transaction is the same code path as x64).
func TestWindowsArm64VulkanReconciledAgainstOfficialEvidence(t *testing.T) {
	restore := SetPlatformForTest("windows", "arm64")
	defer restore()

	if got := AssetNameForVariant("b12032", VariantVulkan); got != "llama-b12032-bin-win-vulkan-arm64.zip" {
		t.Fatalf("windows/arm64 Vulkan asset name = %q, want llama-b12032-bin-win-vulkan-arm64.zip (official b12032 evidence)", got)
	}
	if got := AssetURLForVariant("b12032", VariantVulkan); got != "https://github.com/ggml-org/llama.cpp/releases/download/b12032/llama-b12032-bin-win-vulkan-arm64.zip" {
		t.Fatalf("windows/arm64 Vulkan asset URL = %q", got)
	}
	if got := AssetNameForVariant("b12032", VariantCPU); got != "llama-b12032-bin-win-cpu-arm64.zip" {
		t.Fatalf("windows/arm64 CPU asset name = %q, want llama-b12032-bin-win-cpu-arm64.zip", got)
	}
	if !VariantSupported(VariantVulkan) {
		t.Fatal("windows/arm64 must advertise the Vulkan variant (official b12032 win-vulkan-arm64.zip)")
	}
	variants := SupportedVariants()
	if len(variants) != 2 || variants[0] != VariantCPU || variants[1] != VariantVulkan {
		t.Fatalf("windows/arm64 supports CPU + Vulkan, got %v", variants)
	}
}

// TestLinuxVulkanStaysHonestDespiteUpstreamAssets (v1.11.1): official
// b12032 publishes llama-b12032-bin-ubuntu-vulkan-x64.tar.gz — but this
// build's transactional installer is ZIP-based; the support matrix and
// the naming layer must NOT advertise Linux Vulkan until a secure
// tar.gz transaction exists.
func TestLinuxVulkanStaysHonestDespiteUpstreamAssets(t *testing.T) {
	restore := SetPlatformForTest("linux", "amd64")
	defer restore()

	if got := AssetNameForVariant("b12032", VariantVulkan); got != "" {
		t.Fatalf("linux must not claim a Vulkan prebuilt (upstream is .tar.gz; the zip-based transaction does not consume it), got %q", got)
	}
	if VariantSupported(VariantVulkan) {
		t.Fatal("linux must not advertise the Vulkan variant until a secure tar.gz transaction exists")
	}
}

// TestLatestTagForVariantReadOnlyResolution (v1.11.1): the read-only
// latest-tag resolution follows the strict variant resolver — the exact
// asset must exist on a real release from the list, and the error
// separates no-asset from network failure.
func TestLatestTagForVariantReadOnlyResolution(t *testing.T) {
	// The variant asset names are platform-dependent — drive the Windows
	// x64 contract deterministically on any host.
	restorePlatform := SetPlatformForTest("windows", "amd64")
	defer restorePlatform()
	prevList := releaseListProbe
	restorePrevList := func() { SetReleaseListForTest(prevList) }
	SetReleaseListForTest(func(ctx context.Context) ([]ghRelease, error) {
		return []ghRelease{
			{TagName: "b12032", Assets: []ghAsset{{Name: AssetNameForVariant("b12032", VariantVulkan)}}},
			{TagName: "b12031", Assets: []ghAsset{{Name: AssetNameForVariant("b12031", VariantCPU)}}},
		}, nil
	})
	defer restorePrevList()

	tag, err := LatestTagForVariant(context.Background(), VariantVulkan)
	if err != nil || tag != "b12032" {
		t.Fatalf("LatestTagForVariant(vulkan) = (%q, %v), want (b12032, nil)", tag, err)
	}

	// A release list with NO variant asset resolves to the honest EMPTY
	// tag — never a CPU answer. (The injected-probe path reports the miss
	// as ("", nil); the REAL resolver wraps the same miss in errNoAsset —
	// the API layer treats both shapes as no-asset and distinguishes them
	// from network failures.)
	SetReleaseListForTest(func(ctx context.Context) ([]ghRelease, error) {
		return []ghRelease{{TagName: "b12031", Assets: []ghAsset{{Name: AssetNameForVariant("b12031", VariantCPU)}}}}, nil
	})
	defer restorePrevList()
	if tag, err := LatestTagForVariant(context.Background(), VariantVulkan); tag != "" {
		t.Fatalf("no-asset release list must yield the honest empty tag, got (%q, %v)", tag, err)
	} else if err != nil && !IsNoAssetError(err) {
		t.Fatalf("unexpected error class for a no-asset miss: %v", err)
	}
}
