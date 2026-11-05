package updater

// engine_asset_descriptor_v1111_test.go — v1.11.1 regression for the
// CONFIRMED Windows Vulkan provisioning failure (real user session,
// v1.11.1, app(2).log — run 41933333132):
//
//	gpuCandidateRequested ... engineTag=b12119 variant=vulkan
//	gpuAssetResolved ... variant=vulkan ... variant-aware release resolver
//	stopping engine for transactional vulkan variant provisioning
//	vulkan engine provisioning failed: extract engine b12119:
//	  open .../bin/.engine-download/llama-b12119-bin-win-cpu-x64.zip:
//	  The system cannot find the file specified.
//
// Two stacked defects, both pinned here:
//
//  1. VARIANT-BLIND STAGING NAMES (this package): the download stager
//     derived the local archive name from AssetName(tag) — the CPU
//     alias — and keyed the download cache with the bare tag, so a
//     Vulkan transfer landed under a CPU file name and a CPU cache key.
//     The fix derives the name and the cache key from the SAME
//     (tag, variant) descriptor as the URL, via the one naming
//     authority (AssetNameForVariant); unsupported combinations fail
//     CLOSED instead of falling back to another variant's name.
//
//  2. PREMATURE ARCHIVE DELETION (internal/llm, tested by the seam and
//     reviewed there): the two-phase variant transaction deleted the
//     downloaded archive when its download closure returned — before
//     the destructive window's swap ever extracted it — which is where
//     the observed "The system cannot find the file specified" came
//     from. The extraction path handed to the installer must be the
//     exact path the downloader wrote; that end-to-end coherence is
//     proven here by serving a real archive over loopback HTTP and
//     running the stager output straight into the transactional
//     installer.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEngineAssetDescriptorWindowsX64CPUAndVulkan pins the exact b12119
// asset identity for the two Windows x64 variants: separate assets,
// separate URLs, and the Vulkan descriptor NEVER carries a CPU marker.
func TestEngineAssetDescriptorWindowsX64CPUAndVulkan(t *testing.T) {
	restore := SetPlatformForTest("windows", "amd64")
	defer restore()

	const tag = "b12119"

	cases := []struct {
		variant   AssetVariant
		assetName string
	}{
		{VariantCPU, "llama-b12119-bin-win-cpu-x64.zip"},
		{VariantVulkan, "llama-b12119-bin-win-vulkan-x64.zip"},
	}

	for _, tc := range cases {
		name := AssetNameForVariant(tag, tc.variant)
		if name != tc.assetName {
			t.Fatalf("AssetNameForVariant(%s, %s) = %q, want %q", tag, tc.variant, name, tc.assetName)
		}

		url := AssetURLForVariant(tag, tc.variant)
		wantURL := "https://github.com/ggml-org/llama.cpp/releases/download/" + tag + "/" + tc.assetName
		if url != wantURL {
			t.Fatalf("AssetURLForVariant(%s, %s) = %q, want %q", tag, tc.variant, url, wantURL)
		}

		if tc.variant == VariantVulkan && strings.Contains(url, "win-cpu") {
			t.Fatalf("Vulkan descriptor must never contain a CPU asset marker: %q", url)
		}
	}
}

// TestEngineArchiveCacheKeyDistinguishesVariants: identical build + arch,
// different variant → different cache keys, each carrying the exact asset
// name. A CPU cache entry can never satisfy a Vulkan lookup.
func TestEngineArchiveCacheKeyDistinguishesVariants(t *testing.T) {
	restore := SetPlatformForTest("windows", "amd64")
	defer restore()

	const tag = "b12119"

	cpuKey := engineArchiveCacheKey(tag, VariantCPU, AssetNameForVariant(tag, VariantCPU))
	vulkanKey := engineArchiveCacheKey(tag, VariantVulkan, AssetNameForVariant(tag, VariantVulkan))

	if cpuKey == vulkanKey {
		t.Fatalf("CPU and Vulkan cache keys collide for the same build: %q", cpuKey)
	}

	if !strings.Contains(vulkanKey, string(VariantVulkan)) || !strings.Contains(vulkanKey, AssetNameForVariant(tag, VariantVulkan)) {
		t.Fatalf("Vulkan cache key must carry the variant and the exact asset name: %q", vulkanKey)
	}
}

// TestDownloadEngineArchiveStagedUsesDescriptorNameForVulkan serves a
// real engine archive over loopback HTTP and proves the stager writes —
// and returns — the VARIANT-named path, then feeds that exact path into
// the transactional installer. This is the downloader→extractor
// identity the Windows transaction lost.
func TestDownloadEngineArchiveStagedUsesDescriptorNameForVulkan(t *testing.T) {
	restore := SetPlatformForTest("windows", "amd64")
	defer restore()

	cfg := v137Config(t)
	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(archive)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := srv.URL + "/llama-b12119-bin-win-vulkan-x64.zip"

	got, err := downloadEngineArchiveStaged(ctx, url, "b12119", EngineBinDir(cfg), VariantVulkan, nil)
	if err != nil {
		t.Fatalf("variant-aware staged download: %v", err)
	}

	wantName := "llama-b12119-bin-win-vulkan-x64.zip"
	if filepath.Base(got) != wantName {
		t.Fatalf("staged Vulkan archive = %q, want the descriptor asset name %q (the CPU alias must never be used)", got, wantName)
	}
	if strings.Contains(got, "win-cpu") {
		t.Fatalf("Vulkan staging path must never contain a CPU marker: %q", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("the downloader must leave the archive at the path it returned: %v", err)
	}

	// The staged path is the EXTRACTION path — the same file flows into
	// the transactional installer unchanged.
	staged, err := InstallStagedFromArchiveDeferredWithVariant(cfg, "b12119", VariantVulkan, got)
	if err != nil {
		t.Fatalf("install from the downloader-written archive: %v", err)
	}

	staged.Commit()

	if InstalledEngineVariant(cfg) != VariantVulkan {
		t.Fatal("the transaction must commit the Vulkan variant identity")
	}
}

// TestDownloadEngineArchiveStagedFailsClosedWithoutAssetName: a platform
// with no Vulkan prebuilt must refuse BEFORE any download — never fall
// back to another variant's file name (linux amd64 serves no Vulkan zip).
func TestDownloadEngineArchiveStagedFailsClosedWithoutAssetName(t *testing.T) {
	restore := SetPlatformForTest("linux", "amd64")
	defer restore()

	cfg := v137Config(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := downloadEngineArchiveStaged(
		ctx,
		"https://github.com/ggml-org/llama.cpp/releases/download/b12119/llama-b12119-bin-win-vulkan-x64.zip",
		"b12119",
		EngineBinDir(cfg),
		VariantVulkan,
		nil,
	)
	if err == nil {
		t.Fatal("a descriptor with no prebuilt asset name must fail closed before any download")
	}

	if _, statErr := os.Stat(filepath.Join(EngineBinDir(cfg), ".engine-download")); statErr == nil {
		if entries, _ := os.ReadDir(filepath.Join(EngineBinDir(cfg), ".engine-download")); len(entries) > 0 {
			t.Fatalf("the failed-closed path must not download anything, found %d staged entries", len(entries))
		}
	}
}
