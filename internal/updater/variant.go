// variant.go — v1.6.2: backend-variant engine provisioning (Vulkan must
// be real, not cosmetic).
//
// THE v1.6.0 GAP: the engine provisioning path always downloaded the
// CPU-only llama.cpp Windows package (llama-<tag>-bin-win-cpu-x64.zip).
// The accelerator resolver correctly refuses to claim GPU_VULKAN without
// runtime evidence — but with a CPU-only engine installed, Vulkan
// offload can NEVER happen: the Vulkan backend DLL is never even on
// disk. The GPU profile was therefore cosmetic: selectable, honestly
// unverified, and permanently unsatisfiable.
//
// v1.6.1 made the variant a first-class provisioning concept. v1.6.2
// repairs its three correctness defects:
//
//   - STRICT PARSING (ParseAssetVariant): an explicit API/user request
//     with an empty or unknown variant is REJECTED with a deterministic
//     400 — it can never silently become CPU provisioning. The lenient
//     NormalizeAssetVariant remains ONLY for backward-compatible legacy
//     manifest reads (a v1.6.0-era manifest without/with-an-unknown
//     variant field still reads back as "cpu");
//   - VARIANT-AWARE RELEASE RESOLUTION (ResolveDownloadURLForVariant):
//     the newest tag is selected because THAT RELEASE ACTUALLY CONTAINS
//     the requested variant asset (exact asset-name match against the
//     release payload, plus reachability verification) — never because a
//     CPU-oriented LatestTag() happened to name a tag whose Vulkan URL
//     was never proven to exist. Vulkan is never converted to CPU, and
//     the current engine tag is preserved when it still serves the
//     variant instead of silently downgrading to an older package;
//   - ARCHITECTURE-AWARE SUPPORT MATRIX (supportedVariantsFor): upstream
//     release evidence at b11191 shows Windows x64 Vulkan exists
//     (llama-b11191-bin-win-vulkan-x64.zip) while the Windows ARM64
//     release list carries NO Vulkan package — so win/arm64 does not
//     advertise Vulkan. Linux/darwin keep CPU-only provisioning: their
//     current upstream packages are .tar.gz archives the zip-based
//     transactional installer does not consume (documented honestly in
//     ROADMAP.md/README.md instead of being silently claimed).
package updater

import (
        "context"
        "encoding/xml"
        "errors"
        "fmt"
        "net/http"
        "runtime"
        "strings"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
)

// runtimeGOOS/runtimeGOARCH are the platform identity used by the
// variant support matrix, asset naming and the resolver's diagnostics.
// Tests override them (with restore) to verify OTHER platforms'
// contracts deterministically on any host; production always sees the
// real runtime values.
var (
        runtimeGOOS   = runtime.GOOS
        runtimeGOARCH = runtime.GOARCH
)

// SetPlatformForTest overrides the platform identity used by the
// support matrix, asset naming and resolver diagnostics (tests only).
// The returned restore function reinstates the previous values —
// callers wire it to t.Cleanup. Cross-package tests (the llm variant
// transaction) use it to exercise the Windows Vulkan contract on any
// host.
func SetPlatformForTest(goos, goarch string) (restore func()) {
        prevGOOS, prevGOARCH := runtimeGOOS, runtimeGOARCH
        runtimeGOOS, runtimeGOARCH = goos, goarch
        return func() { runtimeGOOS, runtimeGOARCH = prevGOOS, prevGOARCH }
}

// AssetVariant is the backend family an engine package carries.
type AssetVariant string

const (
        // VariantCPU is the portable CPU-only engine package.
        VariantCPU AssetVariant = "cpu"

        // VariantVulkan is the llama.cpp Vulkan backend package (Windows
        // prebuilt: llama-<tag>-bin-win-vulkan-x64.zip). A Vulkan package
        // still runs on CPU when no device is usable — the Vulkan backend is
        // additive — but only a Vulkan package can ever satisfy GPU_VULKAN.
        VariantVulkan AssetVariant = "vulkan"
)

// ParseAssetVariant STRICTLY parses an explicit variant request (the
// API surface, user settings, CLI flags). It is the only parser those
// paths may use: after controlled normalization (trim + lowercase) it
// accepts exactly "cpu", "vulkan" and the documented Vulkan aliases —
// everything else, INCLUDING the empty string, is rejected.
//
// The returned error is deterministic and actionable; the API layer
// maps it to HTTP 400 verbatim. Invalid input can never become CPU
// provisioning through this path.
func ParseAssetVariant(input string) (AssetVariant, error) {
        switch strings.ToLower(strings.TrimSpace(input)) {
        case "cpu":
                return VariantCPU, nil
        case "vulkan", "gpu_vulkan", "gpu-vulkan":
                return VariantVulkan, nil
        default:
                return "", fmt.Errorf(
                        "invalid engine variant %q — accepted values: \"cpu\", \"vulkan\" (aliases: \"gpu_vulkan\", \"gpu-vulkan\")",
                        input,
                )
        }
}

// ValidateAssetVariant reports whether an already-typed variant value is
// one of the known variants. Provisioning authorities call it as the
// fail-closed gate: an unknown AssetVariant value (e.g. constructed from
// an unparsed string) is rejected instead of being normalized to CPU.
func ValidateAssetVariant(v AssetVariant) error {
        switch v {
        case VariantCPU, VariantVulkan:
                return nil
        default:
                return fmt.Errorf(
                        "invalid engine variant %q — accepted values: \"cpu\", \"vulkan\"", string(v),
                )
        }
}

// NormalizeAssetVariant canonicalizes a variant string with the
// historical lenient rules ("" and unknown values read as the CPU
// default).
//
// v1.6.2 SCOPE: this function exists ONLY for backward-compatible
// LEGACY MANIFEST READS — a v1.6.0-era install manifest without a
// variant field (or with an unknown historical value) must keep
// resolving to the CPU package that was actually installed. Explicit
// API/user requests MUST use ParseAssetVariant; internal provisioning
// authorities MUST use ValidateAssetVariant. Never wire this lenient
// parser to a user-facing request path.
func NormalizeAssetVariant(s string) AssetVariant {
        switch strings.ToLower(strings.TrimSpace(s)) {
        case "vulkan", "gpu_vulkan", "gpu-vulkan":
                return VariantVulkan
        default:
                return VariantCPU
        }
}

// supportedVariantsFor lists the variants a platform can provision (in
// preference order), as verified against the upstream release evidence
// recorded at b11191 (2026-09):
//
//   Windows amd64: cpu + vulkan  (llama-<tag>-bin-win-vulkan-x64.zip served)
//   Windows arm64: cpu only      (no win-vulkan-arm64 asset in the release)
//   other OSes:    cpu only      (upstream serves .tar.gz packages there —
//                                 the zip-based installer does not consume
//                                 them; see ROADMAP.md)
//
// "Provisionable" still means the EXACT release asset must resolve —
// VariantExists/ResolveDownloadURLForVariant verify it per tag. The
// matrix only states which variant families this build will ATTEMPT.
func supportedVariantsFor(goos, goarch string) []AssetVariant {
        if goos == "windows" && goarch == "amd64" {
                return []AssetVariant{VariantCPU, VariantVulkan}
        }
        return []AssetVariant{VariantCPU}
}

// SupportedVariants lists the variants this host platform can provision
// (in preference order). See supportedVariantsFor for the evidence.
func SupportedVariants() []AssetVariant {
        return supportedVariantsFor(runtimeGOOS, runtimeGOARCH)
}

// VariantSupported reports whether this host platform may provision
// variant. It is the static capability matrix, NOT a per-release asset
// guarantee — the provisioning transaction still verifies the exact
// asset exists before installing anything.
func VariantSupported(v AssetVariant) bool {
        for _, s := range SupportedVariants() {
                if s == v {
                        return true
                }
        }
        return false
}

// assetNameForVariant returns the prebuilt llama.cpp asset name for the
// given OS/arch at the given tag carrying the requested backend variant,
// or "" when no prebuilt asset exists (e.g. Vulkan outside Windows x64,
// or an INVALID variant value — the naming layer is fail-closed and
// never hands out a CPU asset for an unknown variant request).
func assetNameForVariant(goos, goarch, tag string, v AssetVariant) string {
        var arch string
        switch goarch {
        case "amd64":
                arch = "x64"
        case "arm64":
                arch = "arm64"
        default:
                return ""
        }

        // Fail closed on unknown variant values: the strict parsers
        // (ParseAssetVariant/ValidateAssetVariant) reject them earlier,
        // but the naming layer must never silently convert one to CPU.
        if err := ValidateAssetVariant(v); err != nil {
                return ""
        }

        switch goos {
        case "windows":
                if v == VariantVulkan {
                        // Upstream evidence (b11191): the Windows Vulkan
                        // prebuilt is published for x64 only — arm64 never
                        // gets a Vulkan asset name to probe.
                        if arch != "x64" {
                                return ""
                        }
                        return fmt.Sprintf("llama-%s-bin-win-vulkan-%s.zip", tag, arch)
                }
                return fmt.Sprintf("llama-%s-bin-win-cpu-%s.zip", tag, arch)
        case "darwin":
                if v == VariantVulkan {
                        return "" // no Vulkan prebuilt upstream — never claim one
                }
                return fmt.Sprintf("llama-%s-bin-macos-%s.zip", tag, arch)
        case "linux":
                if v == VariantVulkan {
                        return "" // see supportedVariantsFor: tar.gz-only upstream
                }
                return fmt.Sprintf("llama-%s-bin-ubuntu-%s.zip", tag, arch)
        }
        return ""
}

// AssetNameForVariant returns the prebuilt llama.cpp asset name for this
// OS/arch at the given tag carrying the requested backend variant, or ""
// when no prebuilt asset exists (e.g. Vulkan outside Windows x64, or an
// invalid variant value).
func AssetNameForVariant(tag string, v AssetVariant) string {
        return assetNameForVariant(runtimeGOOS, runtimeGOARCH, tag, v)
}

// AssetURLForVariant builds the download URL for the variant asset at a
// tag ("" when the platform has no such prebuilt).
func AssetURLForVariant(tag string, v AssetVariant) string {
        a := AssetNameForVariant(tag, v)
        if a == "" {
                return ""
        }
        return "https://github.com/ggml-org/llama.cpp/releases/download/" + tag + "/" + a
}

// variantExistsProbe is the swappable asset-reachability probe behind
// VariantExists (tests inject a deterministic answer; production always
// HEAD-checks the real URL).
var variantExistsProbe func(ctx context.Context, tag string, v AssetVariant) bool

// SetVariantExistsForTest overrides the variant-asset probe (tests only).
func SetVariantExistsForTest(fn func(ctx context.Context, tag string, v AssetVariant) bool) {
        variantExistsProbe = fn
}

// VariantExists HEAD-checks the variant asset at the given tag.
func VariantExists(ctx context.Context, tag string, v AssetVariant) bool {
        if variantExistsProbe != nil {
                return variantExistsProbe(ctx, tag, v)
        }

        url := AssetURLForVariant(tag, v)
        if url == "" {
                return false
        }

        checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
        defer cancel()

        req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, url, nil)
        if err != nil {
                return false
        }

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return false
        }
        _ = resp.Body.Close()

        return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// ResolveDownloadURLForVariant resolves (url, tag) for a fresh engine
// download carrying the requested backend variant.
//
// v1.6.2 contract (the variant-aware release resolver):
//
//   1. the variant value is validated STRICTLY — an unknown value is an
//      error, never a CPU resolution;
//   2. the pinned DefaultEngineTag is returned when its exact variant
//      asset is verified to exist (HEAD);
//   3. otherwise the release LIST is walked newest-first and the NEWEST
//      tag whose release payload actually CONTAINS the exact variant
//      asset name is selected — a release that only ships the CPU
//      package is skipped, never returned for a Vulkan request (the
//      v1.6.1 defect: the CPU-oriented LatestTag() could name a tag
//      whose Vulkan URL was never proven to exist);
//   4. an explicit variant request that cannot be satisfied returns an
//      actionable error distinguishing "no release serves this variant"
//      from "the network check failed" — it NEVER silently resolves to
//      the CPU package.
//
// CPU behavior is unchanged (the newest release carrying the platform
// CPU asset wins, exactly like LatestTag).
func ResolveDownloadURLForVariant(ctx context.Context, v AssetVariant) (string, string, error) {
        if err := ValidateAssetVariant(v); err != nil {
                return "", "", err
        }

        if !VariantSupported(v) {
                return "", "", fmt.Errorf(
                        "no prebuilt llama.cpp %s engine package exists for %s/%s — the %s backend variant is not served by upstream releases for this platform; use the CPU engine (it stays the safe fallback) or build a Vulkan llama-server from source and set llamaBinPath",
                        v, runtimeGOOS, runtimeGOARCH, v,
                )
        }

        // 1) The pinned tag, when its exact variant asset is really there.
        if url := AssetURLForVariant(DefaultEngineTag, v); url != "" {
                probeCtx, probeCancel := context.WithTimeout(ctx, 30*time.Second)

                if VariantExists(probeCtx, DefaultEngineTag, v) {
                        probeCancel()
                        return url, DefaultEngineTag, nil
                }

                probeCancel()
        }

        // 2) The newest release that ACTUALLY contains the variant asset.
        tag, err := resolveNewestTagWithVariant(ctx, v)
        if err != nil {
                return "", "", err
        }

        if tag != "" {
                if url := AssetURLForVariant(tag, v); url != "" {
                        return url, tag, nil
                }
        }

        // 3) The release list was reachable but nothing serves the variant.
        return "", "", fmt.Errorf(
                "no prebuilt llama.cpp %s engine package found for %s/%s in the recent releases — upstream does not currently serve this variant for this platform; keep the CPU engine or set llamaBinPath to a self-built %s llama-server",
                v, runtimeGOOS, runtimeGOARCH, v,
        )
}

// resolveNewestTagWithVariant returns the newest release tag that
// actually carries the exact variant asset for this platform, or "" when
// no recent release serves it. The error distinguishes network failure
// (errNoAsset NOT set) from a reachable-but-unserveable release list
// (errNoAsset) — the caller must never report a network failure as "no
// prebuilt asset exists".
func resolveNewestTagWithVariant(ctx context.Context, v AssetVariant) (string, error) {
        if releaseListProbe != nil {
                releases, err := releaseListProbe(ctx)
                if err != nil {
                        return "", err
                }
                return firstWithVariantAsset(releases, v), nil
        }

        if netcheck.IsOffline() {
                return "", fmt.Errorf("offline — skipping update check")
        }

        if tag, err := newestTagFromAPIWithVariant(ctx, v); err == nil && tag != "" {
                return tag, nil
        } else if err != nil && !errors.Is(err, errNoAsset) {
                logging.Default().Warn("updater", "release list via API failed: %v (trying atom feed)", err)
        }

        return newestTagFromAtomWithVariant(ctx, v)
}

// newestTagFromAPIWithVariant pages the GitHub release list (newest
// first) and returns the first tag whose assets include the platform's
// VARIANT asset (the CPU path is exactly the historical LatestTag
// behavior).
func newestTagFromAPIWithVariant(ctx context.Context, v AssetVariant) (string, error) {
        return pageReleasesForAsset(ctx, func(tag string) string {
                return AssetNameForVariant(tag, v)
        })
}

// newestTagFromAtomWithVariant parses the public releases Atom feed (no
// rate limit) and HEAD-checks candidate tags until one serves the
// VARIANT asset.
func newestTagFromAtomWithVariant(ctx context.Context, v AssetVariant) (string, error) {
        ctx, cancel := context.WithTimeout(ctx, apiTimeout)
        defer cancel()

        req, err := http.NewRequestWithContext(ctx, http.MethodGet,
                "https://github.com/ggml-org/llama.cpp/releases.atom", nil)
        if err != nil {
                return "", err
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()
        if resp.StatusCode != 200 {
                return "", fmt.Errorf("atom feed: HTTP %d", resp.StatusCode)
        }
        var feed atomFeed
        if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
                return "", err
        }
        checked := 0
        for _, e := range feed.Entries {
                tag := strings.TrimSpace(e.Title)
                if tag == "" || !strings.HasPrefix(tag, "b") {
                        continue // skip milestone tags (vX.Y.Z) — they carry no binaries
                }
                url := AssetURLForVariant(tag, v)
                if url == "" {
                        continue
                }
                checked++
                if checked > 10 {
                        break // bounded effort
                }
                if variantURLExists(ctx, url) {
                        return tag, nil
                }
        }
        return "", fmt.Errorf("no recent release ships a prebuilt %s asset for %s/%s", v, runtimeGOOS, runtimeGOARCH)
}

// variantURLExists HEAD-checks one variant asset URL.
func variantURLExists(ctx context.Context, url string) bool {
        checkCtx, cancel := context.WithTimeout(ctx, apiTimeout)
        defer cancel()

        req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, url, nil)
        if err != nil {
                return false
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return false
        }
        _ = resp.Body.Close()
        return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// InstalledEngineVariant reads the committed install manifest's backend
// variant. Legacy compatibility: a v1.6.0-era manifest without the field
// (or with an unknown historical value) reads back as "cpu" — that is
// what was actually installed. This is the ONE sanctioned use of the
// lenient NormalizeAssetVariant.
func InstalledEngineVariant(cfg *config.Config) AssetVariant {
        m, ok := ReadInstallManifest(cfg)
        if !ok {
                return VariantCPU
        }
        return NormalizeAssetVariant(string(m.Variant))
}

// EnsureEngineVariant provisions the requested backend variant through
// the SAME transactional installer as every engine update. A no-op when
// the committed package already carries the variant; an explicit,
// actionable error when the variant cannot be provisioned — never a
// silent CPU install.
//
// The caller owns the engine lifecycle (stop before, verify after) — the
// same contract as InstallStaged.
func EnsureEngineVariant(
        ctx context.Context,
        cfg *config.Config,
        v AssetVariant,
        onProgress func(downloader.Progress),
) error {
        if err := ValidateAssetVariant(v); err != nil {
                return err
        }

        if !VariantSupported(v) {
                return fmt.Errorf(
                        "engine variant %q cannot be provisioned on %s/%s — upstream publishes no prebuilt package (keep the CPU engine, or set llamaBinPath to a self-built %s llama-server)",
                        v, runtimeGOOS, runtimeGOARCH, v,
                )
        }

        if InstalledEngineVariant(cfg) == v {
                logging.Default().Info("updater", "engine package already carries the %s backend variant — no provisioning needed", v)
                return nil
        }

        // Preserve the CURRENT engine tag when that release still serves
        // the variant (same release, different backend build) — the
        // variant swap must never silently downgrade the release. Only
        // when the current tag does not carry the variant asset is a
        // fresh, variant-aware tag resolved.
        tag := InstalledEngineTag(cfg)
        if tag == "" || !VariantExists(ctx, tag, v) {
                _, resolved, err := ResolveDownloadURLForVariant(ctx, v)
                if err != nil {
                        return err
                }
                tag = resolved
        }

        logging.Default().Info("updater",
                "provisioning the %s engine variant (tag %s) through the transactional installer", v, tag)

        staged, err := InstallStagedDeferredWithVariant(ctx, cfg, tag, v, onProgress)
        if err != nil {
                return fmt.Errorf("provision %s engine: %w", v, err)
        }

        staged.Commit()

        return nil
}
