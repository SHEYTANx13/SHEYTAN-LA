package updater

// ci_variant_gate_test.go — v1.6.2: the CI-facing engine-variant asset
// contract, run through the SAME authoritative resolver/asset rules as
// production (no duplicated release-tag knowledge — the v1.6.1 workflow
// hard-coded `$tag = "b10642"` and drifted from the code).
//
// The gate is NETWORK-GATED: it only runs when SHEYTAN_CI_VARIANT_GATE=1
// (the CI workflow sets it), so the regular offline test suites stay
// deterministic. On the CI Windows x64 job it verifies against the REAL
// upstream:
//
//   - exact variant asset RESOLUTION through ResolveDownloadURLForVariant
//     (pinned tag first, then the newest release that actually contains
//     the variant asset);
//   - exact asset REACHABILITY (a HEAD of the resolved URL answers
//     2xx/3xx);
//   - the support matrix ADVERTISES only what the platform naming can
//     serve, and an explicitly unsupported variant is REFUSED (never a
//     CPU fallback);
//   - a Vulkan resolution NEVER yields a CPU asset URL.
//
// HONESTY NOTE: this gate verifies ASSET RESOLUTION AND REACHABILITY —
// HTTP reachability is NOT runtime execution. The runtime truth of a
// provisioned backend is verified by the engine's own device
// enumeration inside the provisioning transaction (see
// internal/llm/variant_runtime_v162_test.go), not by this check.

import (
        "context"
        "net/http"
        "os"
        "runtime"
        "strings"
        "testing"
        "time"
)

func ciVariantGateEnabled() bool {
        return strings.EqualFold(strings.TrimSpace(os.Getenv("SHEYTAN_CI_VARIANT_GATE")), "1")
}

func TestCIVariantAssetContract(t *testing.T) {
        if !ciVariantGateEnabled() {
                t.Skip("network-gated CI contract — set SHEYTAN_CI_VARIANT_GATE=1 (the CI workflow does)")
        }

        ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
        defer cancel()

        // --- 1. The support matrix must agree with the asset naming --------
        for _, v := range SupportedVariants() {
                if err := ValidateAssetVariant(v); err != nil {
                        t.Fatalf("advertised variant %q fails validation: %v", v, err)
                }
                if AssetNameForVariant(DefaultEngineTag, v) == "" {
                        t.Fatalf("advertised variant %q names no asset at the pinned tag %s — the matrix would be cosmetic", v, DefaultEngineTag)
                }
        }

        // --- 2. Exact variant asset resolution through the PRODUCTION path --
        // The FULL contract (resolve + reach) is enforced on the reference
        // provisioning platform — windows/amd64, where the CI job runs this
        // gate. On other platforms (Linux: upstream serves .tar.gz only, the
        // zip-based installer cannot consume them — a documented limitation)
        // an advertised variant may honestly fail to resolve: the gate then
        // verifies the failure is the honest no-asset error, never a
        // wrong-variant URL.
        fullResolution := runtime.GOOS == "windows" && runtime.GOARCH == "amd64"

        for _, v := range SupportedVariants() {
                url, tag, err := ResolveDownloadURLForVariant(ctx, v)
                if err != nil {
                        if fullResolution {
                                t.Fatalf("resolve %s engine through the production resolver: %v", v, err)
                        }
                        if !strings.Contains(err.Error(), "no prebuilt") && !strings.Contains(err.Error(), "offline") && !strings.Contains(err.Error(), "no recent release") {
                                t.Fatalf("on %s/%s the %s resolution failed for an unexpected reason: %v", runtime.GOOS, runtime.GOARCH, v, err)
                        }
                        if strings.Contains(url, "-bin-win-cpu-") || strings.Contains(url, "-bin-ubuntu-") || strings.Contains(url, "-bin-macos-") {
                                t.Fatalf("a failed %s resolution must never yield an asset URL, got %s", v, url)
                        }
                        t.Logf("[ci-variant-gate] %s: honest no-asset resolution failure on %s/%s (documented limitation): %v", v, runtime.GOOS, runtime.GOARCH, err)
                        continue
                }
                if tag == "" || url == "" {
                        t.Fatalf("resolver returned empty identity for %s: url=%q tag=%q", v, url, tag)
                }

                // The resolved asset must be the EXACT variant asset for this
                // platform — never another variant's URL.
                want := AssetNameForVariant(tag, v)
                if want == "" || !strings.HasSuffix(url, "/"+want) {
                        t.Fatalf("resolved url %q is not the exact %s asset %q at tag %s", url, v, want, tag)
                }

                // --- 3. Exact asset reachability (HEAD 2xx/3xx) ---------------
                checkCtx, checkCancel := context.WithTimeout(ctx, 60*time.Second)
                req, rerr := http.NewRequestWithContext(checkCtx, http.MethodHead, url, nil)
                if rerr != nil {
                        t.Fatalf("HEAD request build: %v", rerr)
                }
                resp, derr := http.DefaultClient.Do(req)
                checkCancel()
                if derr != nil {
                        t.Fatalf("HEAD %s: %v", url, derr)
                }
                _ = resp.Body.Close()
                if resp.StatusCode < 200 || resp.StatusCode >= 400 {
                        t.Fatalf("the resolved %s asset is not reachable: HEAD %s answered HTTP %d", v, url, resp.StatusCode)
                }
                t.Logf("[ci-variant-gate] %s: tag=%s asset reachable (HTTP %d) — %s", v, tag, resp.StatusCode, url)
        }

        // --- 4. No silent CPU fallback for a non-advertised variant ---------
        // Windows/arm64 is the verified negative case (b11191 serves no
        // win-vulkan-arm64 asset); on other platforms Vulkan itself is the
        // negative case. The refusal must be deterministic and must never
        // produce a CPU URL.
        negative := AssetVariant("vulkan")
        if VariantSupported(negative) {
                // On Windows x64 Vulkan IS supported: use the arm64 matrix as the
                // deterministic negative case instead.
                if VariantSupportedOn("windows", "arm64", negative) {
                        t.Fatal("windows/arm64 must not advertise Vulkan (upstream b11191 serves no win-vulkan-arm64 asset)")
                }
                negative = AssetVariant("cuda") // never a valid variant anywhere
        }

        url, _, err := ResolveDownloadURLForVariant(ctx, negative)
        if err == nil {
                t.Fatalf("variant %q must be refused, got url=%s", negative, url)
        }
        if strings.Contains(url, "-bin-win-cpu-") || strings.Contains(url, "-bin-ubuntu-") {
                t.Fatalf("a refused variant must never yield a CPU asset URL, got %s", url)
        }

        // The strict parser rejects unknown values outright.
        if _, perr := ParseAssetVariant("banana"); perr == nil {
                t.Fatal("ParseAssetVariant must reject unknown values")
        }
}

// VariantSupportedOn is the matrix predicate for an explicit platform
// (the CI gate uses it to pin the windows/arm64 negative case without
// inferring support from the OS alone).
func VariantSupportedOn(goos, goarch string, v AssetVariant) bool {
        for _, s := range supportedVariantsFor(goos, goarch) {
                if s == v {
                        return true
                }
        }
        return false
}
