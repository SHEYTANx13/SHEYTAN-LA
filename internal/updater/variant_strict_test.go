package updater

// variant_strict_test.go — v1.6.2: the strict variant contract.
//
// Deterministic, offline (probe-injected) tests for:
//   - ParseAssetVariant: empty/whitespace/unknown rejected; cpu/vulkan
//     and documented aliases accepted after controlled normalization;
//   - the architecture-aware support matrix for every GOOS/GOARCH
//     combination (no OS-only inference);
//   - the variant-aware release resolver: pinned tag, newer matching
//     tag, CPU-only newest release, no matching variant, invalid
//     variant, offline/network failure — and NEVER a CPU resolution
//     for a Vulkan request.

import (
        "context"
        "errors"
        "fmt"
        "strings"
        "testing"
)

// --- strict parsing -------------------------------------------------------

func TestParseAssetVariantAcceptsKnownValues(t *testing.T) {
        for _, tc := range []struct {
                in   string
                want AssetVariant
        }{
                {"cpu", VariantCPU},
                {"CPU", VariantCPU},
                {"  Cpu  ", VariantCPU},
                {"\tvulkan\n", VariantVulkan},
                {"Vulkan", VariantVulkan},
                {" VULKAN ", VariantVulkan},
                {"gpu_vulkan", VariantVulkan},
                {"GPU_VULKAN", VariantVulkan},
                {"gpu-vulkan", VariantVulkan},
                {"GPU-Vulkan", VariantVulkan},
        } {
                got, err := ParseAssetVariant(tc.in)
                if err != nil {
                        t.Fatalf("ParseAssetVariant(%q) unexpected error: %v", tc.in, err)
                }
                if got != tc.want {
                        t.Fatalf("ParseAssetVariant(%q) = %q, want %q", tc.in, got, tc.want)
                }
        }
}

func TestParseAssetVariantRejectsInvalidValues(t *testing.T) {
        for _, in := range []string{
                "", "   ", "\t", "\n",
                "banana", "cu", "vulk", "vulkann", "cpuu", "0", "1", "none",
                "vulkan cpu", "gpu cuda", "cpu/vulkan", "cuda", "opencl", "metal", "auto",
                "gpu-vulkan-x", "\x00", "cpu;rm -rf", "vulkan\n cpu",
        } {
                got, err := ParseAssetVariant(in)
                if err == nil {
                        t.Fatalf("ParseAssetVariant(%q) = %q, want a deterministic error", in, got)
                }
                if got != "" {
                        t.Fatalf("ParseAssetVariant(%q) returned non-empty variant %q alongside the error", in, got)
                }
                // The error must be actionable: it names the offending input and
                // the accepted values.
                for _, want := range []string{"invalid engine variant", `"` + strings.TrimSpace(in) + `"`, `"cpu"`, `"vulkan"`} {
                        if in == "" || strings.TrimSpace(in) == "" {
                                continue // the empty input renders as "" in the message
                        }
                        if !strings.Contains(err.Error(), strings.ReplaceAll(want, "\n", "")) && !strings.Contains(err.Error(), "invalid engine variant") {
                                t.Fatalf("ParseAssetVariant(%q) error must be actionable, got: %v", in, err)
                        }
                }
        }

        // The empty input must be named too.
        if _, err := ParseAssetVariant(""); err == nil || !strings.Contains(err.Error(), "invalid engine variant") {
                t.Fatalf("empty variant must be rejected deterministically, got %v", err)
        }
}

func TestValidateAssetVariantTyped(t *testing.T) {
        for _, v := range []AssetVariant{VariantCPU, VariantVulkan} {
                if err := ValidateAssetVariant(v); err != nil {
                        t.Fatalf("ValidateAssetVariant(%q) = %v, want nil", v, err)
                }
        }
        for _, v := range []AssetVariant{"", "banana", "GPU", "cpu ", "Vulkan"} {
                if err := ValidateAssetVariant(v); err == nil {
                        t.Fatalf("ValidateAssetVariant(%q) must reject unknown values", v)
                }
        }
}

// The naming layer is fail-closed: an invalid variant never yields a
// CPU asset name.
func TestAssetNameForVariantFailClosedOnInvalidVariant(t *testing.T) {
        for _, goos := range []string{"windows", "linux", "darwin"} {
                for _, arch := range []string{"amd64", "arm64"} {
                        if got := assetNameForVariant(goos, arch, "b11191", AssetVariant("banana")); got != "" {
                                t.Fatalf("assetNameForVariant(%s,%s,banana) = %q, want \"\" (never a CPU asset)", goos, arch, got)
                        }
                }
        }
}

// --- architecture-aware support matrix -----------------------------------

func TestSupportedVariantsMatrix(t *testing.T) {
        for _, tc := range []struct {
                goos, goarch string
                want         []AssetVariant
        }{
                {"windows", "amd64", []AssetVariant{VariantCPU, VariantVulkan}},
                {"windows", "arm64", []AssetVariant{VariantCPU}}, // b11191: no win-arm64 Vulkan prebuilt
                {"linux", "amd64", []AssetVariant{VariantCPU}},   // tar.gz-only upstream; zip installer
                {"linux", "arm64", []AssetVariant{VariantCPU}},
                {"darwin", "amd64", []AssetVariant{VariantCPU}},
                {"darwin", "arm64", []AssetVariant{VariantCPU}},
                {"freebsd", "amd64", []AssetVariant{VariantCPU}},
                {"android", "arm64", []AssetVariant{VariantCPU}},
        } {
                got := supportedVariantsFor(tc.goos, tc.goarch)
                if len(got) != len(tc.want) {
                        t.Fatalf("supportedVariantsFor(%s,%s) = %v, want %v", tc.goos, tc.goarch, got, tc.want)
                }
                for i := range got {
                        if got[i] != tc.want[i] {
                                t.Fatalf("supportedVariantsFor(%s,%s) = %v, want %v", tc.goos, tc.goarch, got, tc.want)
                        }
                }
        }
}

// The asset NAMING matrix must agree with the SUPPORT matrix: a variant
// is only advertised where the exact asset name exists (and vice versa
// for the platform/variant pairs upstream actually serves).
func TestAssetNameMatrixAgreesWithSupportMatrix(t *testing.T) {
        for _, goos := range []string{"windows", "linux", "darwin", "freebsd"} {
                for _, goarch := range []string{"amd64", "arm64", "386"} {
                        supported := supportedVariantsFor(goos, goarch)

                        for _, v := range []AssetVariant{VariantCPU, VariantVulkan} {
                                name := assetNameForVariant(goos, goarch, "b11191", v)
                                advertised := false
                                for _, s := range supported {
                                        if s == v {
                                                advertised = true
                                        }
                                }

                                if v == VariantVulkan && advertised && name == "" {
                                        t.Fatalf("%s/%s advertises Vulkan but names no asset — the matrix would be cosmetic", goos, goarch)
                                }
                                if v == VariantVulkan && !advertised && name != "" {
                                        t.Fatalf("%s/%s does not advertise Vulkan yet names asset %q — matrix and naming disagree", goos, goarch, name)
                                }
                                if v == VariantCPU && !advertised {
                                        t.Fatalf("%s/%s must always support CPU", goos, goarch)
                                }
                        }
                }
        }
}

// The verified upstream evidence at b11191: Windows x64 Vulkan asset
// name exists; Windows ARM64 Vulkan asset name does not.
func TestAssetNameForVariantUpstreamEvidence(t *testing.T) {
        if got := assetNameForVariant("windows", "amd64", "b11191", VariantVulkan); got != "llama-b11191-bin-win-vulkan-x64.zip" {
                t.Fatalf("windows/amd64 vulkan asset = %q", got)
        }
        if got := assetNameForVariant("windows", "amd64", "b11191", VariantCPU); got != "llama-b11191-bin-win-cpu-x64.zip" {
                t.Fatalf("windows/amd64 cpu asset = %q", got)
        }
        if got := assetNameForVariant("windows", "arm64", "b11191", VariantVulkan); got != "" {
                t.Fatalf("windows/arm64 vulkan asset = %q, want \"\" (upstream serves none at b11191)", got)
        }
        if got := assetNameForVariant("windows", "arm64", "b11191", VariantCPU); got != "llama-b11191-bin-win-cpu-arm64.zip" {
                t.Fatalf("windows/arm64 cpu asset = %q", got)
        }
}

// --- variant-aware release resolution ------------------------------------

// withPlatform pins the resolver's platform identity for one test and
// restores it afterwards (tests are sequential per package — no race).
func withPlatform(t *testing.T, goos, goarch string) {
        t.Helper()
        prevGOOS, prevGOARCH := runtimeGOOS, runtimeGOARCH
        runtimeGOOS, runtimeGOARCH = goos, goarch
        t.Cleanup(func() { runtimeGOOS, runtimeGOARCH = prevGOOS, prevGOARCH })
}

// resolverFixture wires the deterministic probes and restores them.
type resolverFixture struct {
        exists func(ctx context.Context, tag string, v AssetVariant) bool
        list   func(ctx context.Context) ([]ghRelease, error)
}

func installResolverFixture(t *testing.T, f resolverFixture) {
        t.Helper()
        prevExists, prevList := variantExistsProbe, releaseListProbe
        variantExistsProbe, releaseListProbe = f.exists, f.list
        t.Cleanup(func() { variantExistsProbe, releaseListProbe = prevExists, prevList })
}

func winAssets(tags ...string) []ghAsset {
        out := []ghAsset{}
        for _, tag := range tags {
                out = append(out,
                        ghAsset{Name: fmt.Sprintf("llama-%s-bin-win-cpu-x64.zip", tag)},
                        ghAsset{Name: fmt.Sprintf("llama-%s-bin-win-vulkan-x64.zip", tag)},
                )
        }
        return out
}

func TestResolveVariantPinnedTagHasVariant(t *testing.T) {
        withPlatform(t, "windows", "amd64")
        installResolverFixture(t, resolverFixture{
                exists: func(ctx context.Context, tag string, v AssetVariant) bool {
                        return tag == DefaultEngineTag && v == VariantVulkan
                },
                list: func(ctx context.Context) ([]ghRelease, error) {
                        return []ghRelease{{TagName: "b99999", Assets: winAssets("b99999")}}, nil
                },
        })

        url, tag, err := ResolveDownloadURLForVariant(context.Background(), VariantVulkan)
        if err != nil {
                t.Fatalf("pinned tag carries the variant: %v", err)
        }
        if tag != DefaultEngineTag {
                t.Fatalf("tag = %q, want the pinned %q", tag, DefaultEngineTag)
        }
        if !strings.Contains(url, "bin-win-vulkan-x64.zip") {
                t.Fatalf("url = %q, want the exact vulkan asset", url)
        }
}

func TestResolveVariantPinnedLacksVariantNewerTagServesIt(t *testing.T) {
        withPlatform(t, "windows", "amd64")
        installResolverFixture(t, resolverFixture{
                exists: func(ctx context.Context, tag string, v AssetVariant) bool {
                        return false // pinned/current tag never serves it
                },
                list: func(ctx context.Context) ([]ghRelease, error) {
                        return []ghRelease{
                                {TagName: "b11191", Assets: winAssets("b11191")},
                                {TagName: "b11090", Assets: winAssets("b11090")},
                        }, nil
                },
        })

        url, tag, err := ResolveDownloadURLForVariant(context.Background(), VariantVulkan)
        if err != nil {
                t.Fatalf("newer tag serves the variant: %v", err)
        }
        if tag != "b11191" {
                t.Fatalf("tag = %q, want the NEWEST serving release b11191", tag)
        }
        if !strings.Contains(url, "llama-b11191-bin-win-vulkan-x64.zip") {
                t.Fatalf("url = %q, want the exact b11191 vulkan asset", url)
        }
}

// THE v1.6.1 DEFECT: the resolver used the CPU-oriented LatestTag and
// could return a Vulkan URL for a tag that was never proven to serve
// Vulkan. The newest release here ships CPU only — the resolver must
// skip it and select the older release that actually carries Vulkan.
func TestResolveVariantNewerReleaseHasCPUButNotVulkan(t *testing.T) {
        withPlatform(t, "windows", "amd64")
        installResolverFixture(t, resolverFixture{
                exists: func(ctx context.Context, tag string, v AssetVariant) bool {
                        return false
                },
                list: func(ctx context.Context) ([]ghRelease, error) {
                        return []ghRelease{
                                {TagName: "b12000", Assets: []ghAsset{
                                        {Name: "llama-b12000-bin-win-cpu-x64.zip"}, // CPU only!
                                }},
                                {TagName: "b11191", Assets: winAssets("b11191")},
                                {TagName: "b11090", Assets: winAssets("b11090")},
                        }, nil
                },
        })

        url, tag, err := ResolveDownloadURLForVariant(context.Background(), VariantVulkan)
        if err != nil {
                t.Fatalf("an older release serves Vulkan: %v", err)
        }
        if tag != "b11191" {
                t.Fatalf("tag = %q, want b11191 (b12000 serves CPU only and must be skipped)", tag)
        }
        if !strings.Contains(url, "llama-b11191-bin-win-vulkan-x64.zip") {
                t.Fatalf("url = %q", url)
        }
}

func TestResolveVariantNoMatchingVariantExists(t *testing.T) {
        withPlatform(t, "windows", "amd64")
        installResolverFixture(t, resolverFixture{
                exists: func(ctx context.Context, tag string, v AssetVariant) bool {
                        return false
                },
                list: func(ctx context.Context) ([]ghRelease, error) {
                        return []ghRelease{
                                {TagName: "b12000", Assets: []ghAsset{{Name: "llama-b12000-bin-win-cpu-x64.zip"}}},
                                {TagName: "b11191", Assets: []ghAsset{{Name: "llama-b11191-bin-win-cpu-x64.zip"}}},
                        }, nil
                },
        })

        url, tag, err := ResolveDownloadURLForVariant(context.Background(), VariantVulkan)
        if err == nil {
                t.Fatalf("no release serves Vulkan — must fail loudly, got url=%s tag=%s", url, tag)
        }
        if strings.Contains(url, "-bin-win-cpu-") {
                t.Fatal("the failure must never yield the CPU asset URL")
        }
        for _, want := range []string{"vulkan", "no prebuilt"} {
                if !strings.Contains(err.Error(), want) {
                        t.Fatalf("refusal must mention %q: %v", want, err)
                }
        }
}

func TestResolveVariantInvalidVariantRejected(t *testing.T) {
        withPlatform(t, "windows", "amd64")
        installResolverFixture(t, resolverFixture{
                exists: func(ctx context.Context, tag string, v AssetVariant) bool { return true },
                list:   func(ctx context.Context) ([]ghRelease, error) { return nil, nil },
        })

        for _, v := range []AssetVariant{"", "banana", "GPU", "auto"} {
                url, tag, err := ResolveDownloadURLForVariant(context.Background(), v)
                if err == nil {
                        t.Fatalf("invalid variant %q must be rejected, got url=%s tag=%s", v, url, tag)
                }
                if url != "" || tag != "" {
                        t.Fatalf("invalid variant %q must yield no resolution, got url=%s tag=%s", v, url, tag)
                }
                if !strings.Contains(err.Error(), "invalid engine variant") {
                        t.Fatalf("error must classify as invalid variant: %v", err)
                }
        }
}

func TestResolveVariantNetworkFailureIsNotNoAsset(t *testing.T) {
        withPlatform(t, "windows", "amd64")
        netErr := errors.New("dial tcp: lookup api.github.com: no such host")
        installResolverFixture(t, resolverFixture{
                exists: func(ctx context.Context, tag string, v AssetVariant) bool { return false },
                list: func(ctx context.Context) ([]ghRelease, error) {
                        return nil, netErr
                },
        })

        url, tag, err := ResolveDownloadURLForVariant(context.Background(), VariantVulkan)
        if err == nil {
                t.Fatalf("network failure must surface as an error, got url=%s tag=%s", url, tag)
        }
        if !strings.Contains(err.Error(), netErr.Error()) {
                t.Fatalf("the network failure must be preserved verbatim for offline diagnosis: %v", err)
        }
        if strings.Contains(err.Error(), "does not currently serve") {
                t.Fatalf("a network failure must NOT be reported as 'no prebuilt asset exists': %v", err)
        }
}

// CPU resolution stays backward compatible: the newest release carrying
// the platform CPU asset wins when the pinned tag does not serve it.
func TestResolveVariantCPUBackwardCompatible(t *testing.T) {
        withPlatform(t, "windows", "amd64")
        installResolverFixture(t, resolverFixture{
                exists: func(ctx context.Context, tag string, v AssetVariant) bool { return false },
                list: func(ctx context.Context) ([]ghRelease, error) {
                        return []ghRelease{
                                {TagName: "b12000", Assets: []ghAsset{
                                        {Name: "llama-b12000-bin-ubuntu-x64.zip"}, // some other platform
                                }},
                                {TagName: "b11191", Assets: winAssets("b11191")},
                        }, nil
                },
        })

        url, tag, err := ResolveDownloadURLForVariant(context.Background(), VariantCPU)
        if err != nil {
                t.Fatalf("CPU resolution must stay backward compatible: %v", err)
        }
        if tag != "b11191" {
                t.Fatalf("tag = %q, want b11191 (the first release with THIS platform's CPU asset)", tag)
        }
        if !strings.Contains(url, AssetNameForVariant("b11191", VariantCPU)) {
                t.Fatalf("url = %q", url)
        }
}

// FirstWithVariantAsset: the deterministic list-walking core.
func TestFirstWithVariantAsset(t *testing.T) {
        withPlatform(t, "windows", "amd64")
        releases := []ghRelease{
                {TagName: "b12000", Assets: []ghAsset{{Name: "llama-b12000-bin-win-cpu-x64.zip"}}},
                {TagName: "b11191", Assets: winAssets("b11191")},
                {TagName: "", Assets: winAssets("b00000")}, // untitled entries skipped
        }

        if got := FirstWithVariantAsset(releases, VariantVulkan); got != "b11191" {
                t.Fatalf("FirstWithVariantAsset(vulkan) = %q, want b11191", got)
        }
        if got := FirstWithVariantAsset(releases, VariantCPU); got != "b12000" {
                t.Fatalf("FirstWithVariantAsset(cpu) = %q, want b12000", got)
        }
        if got := FirstWithVariantAsset(nil, VariantVulkan); got != "" {
                t.Fatalf("FirstWithVariantAsset(nil) = %q, want \"\"", got)
        }
        if got := FirstWithVariantAsset(releases, AssetVariant("banana")); got != "" {
                t.Fatalf("FirstWithVariantAsset(banana) = %q, want \"\"", got)
        }
}
