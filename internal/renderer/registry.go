// Package renderer — the engine/renderer catalog (v1.11.1, UX-02/UX-03).
// ONE registry of renderer manifests: identity, pinned dependency version,
// maturity, isolation mode, per-platform verification status, truthful
// capability matrix and known gaps. The Fyne Engine Gallery renders this
// catalog; future engine adapters declare themselves here instead of
// growing a second engine list somewhere in a UI package.
//
// The package is pure Go: no GUI framework may be imported here, so the
// domain layer and every renderer adapter can consume the same catalog.
// The registry states facts with their evidence class — a capability is
// "supported" only where it is verified, "unverified" where it is merely
// plausible, and never inferred from a detection.
package renderer

import (
	"fmt"
	"runtime"
	"strings"
)

// ID is a renderer identity. The spelling is the stable wire/catalog form.
type ID string

// The engines of the v1.11.1 renderer-convergence milestone.
const (
	Fyne         ID = "fyne"          // the RETIRED toolkit shell (deleted from the tree; id kept for renderer-name stability)
	GoGui        ID = "gogui"         // github.com/go-gui-org/go-gui v0.96.0 POC
	GoGPUUI      ID = "gogpu-ui"      // github.com/gogpu/ui (GoGPU stack) POC
	WailsWebView ID = "wails-webview" // Wails v3 WebView Web Workspace POC
)

// Maturity is the promotion vocabulary (the renderer promotion policy).
type Maturity string

const (
	MaturityProduction   Maturity = "production"
	MaturityPreview      Maturity = "preview"
	MaturityExperimental Maturity = "experimental"
	MaturityUnavailable  Maturity = "unavailable"
)

// Isolation is how the renderer is hosted relative to the one Go core.
type Isolation string

const (
	// IsolationInProcess: the renderer IS the production shell process.
	IsolationInProcess Isolation = "in-process"
	// IsolationCompanionWindow: the renderer runs as a managed companion
	// surface/window synchronized through the Go core (the safe boundary
	// for engines that own their event loop).
	IsolationCompanionWindow Isolation = "companion-window"
	// IsolationNotBuilt: no binary of this renderer ships in this package
	// (the POC lives in experiments/ and is not wired into the product).
	IsolationNotBuilt Isolation = "not-built"
)

// State is the runtime availability vocabulary of the Engine Gallery.
type State string

const (
	StateReady    State = "ready"     // the engine runs in this package
	StateNotBuilt State = "not-built" // no binary in this package (with reason)
	StateDisabled State = "disabled"  // present but disabled (with reason)
	StateFailed   State = "crashed"   // started but failed (with reason)
)

// Capability states — the truthful detection vocabulary. "supported" means
// verified; "unverified" means plausible but not demonstrated; nothing
// here upgrades a detection into a capability claim.
const (
	CapSupported   = "supported"
	CapPartial     = "partial"
	CapUnsupported = "unsupported"
	CapUnverified  = "unverified"
)

// Platform status vocabulary.
const (
	PlatformVerified    = "verified"    // tested on this exact target
	PlatformExpected    = "expected"    // plausible, not tested in this run
	PlatformUnsupported = "unsupported" // upstream marks it unsupported
)

// Capability is one named capability with its honest state.
type Capability struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// PlatformSupport is one OS/arch target with its verification status.
type PlatformSupport struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

// Manifest is one renderer's catalog entry.
type Manifest struct {
	ID           ID                    `json:"id"`
	DisplayName  string                `json:"displayName"`
	Version      string                `json:"version"`
	Maturity     Maturity              `json:"maturity"`
	Isolation    Isolation             `json:"isolation"`
	State        State                 `json:"state"`
	StateReason  string                `json:"stateReason,omitempty"`
	Platforms    []PlatformSupport     `json:"platforms"`
	Capabilities map[string]Capability `json:"capabilities"`
	KnownGaps    []string              `json:"knownGaps"`
	PinnedModule string                `json:"pinnedModule,omitempty"`
	ThemeMapping string                `json:"themeMapping"`
	Notes        string                `json:"notes,omitempty"`
}

// Supported reports whether the manifest covers the current GOOS/GOARCH,
// and with which status. An engine without an entry for this platform is
// "expected" at best — never silently claimed.
func (m Manifest) Supported(os, arch string) (string, bool) {
	for _, p := range m.Platforms {
		if p.OS == os && p.Arch == arch {
			return p.Status, true
		}
	}
	return "", false
}

// currentPlatformStatus resolves the manifest's status on the running
// platform.
func (m Manifest) currentPlatformStatus() (string, bool) {
	return m.Supported(runtime.GOOS, runtime.GOARCH)
}

// StatusLine is the gallery's one-line summary for a manifest on this
// machine: maturity, runtime state and platform verification in one
// honest sentence.
func (m Manifest) StatusLine() string {
	platform, known := m.currentPlatformStatus()
	if !known {
		platform = PlatformExpected
	}
	line := fmt.Sprintf("%s · %s · this platform: %s", m.Maturity, m.State, platform)
	return line
}

var maturityVocabulary = map[Maturity]bool{
	MaturityProduction:   true,
	MaturityPreview:      true,
	MaturityExperimental: true,
	MaturityUnavailable:  true,
}

var isolationVocabulary = map[Isolation]bool{
	IsolationInProcess:       true,
	IsolationCompanionWindow: true,
	IsolationNotBuilt:        true,
}

var stateVocabulary = map[State]bool{
	StateReady:    true,
	StateNotBuilt: true,
	StateDisabled: true,
	StateFailed:   true,
}

var capVocabulary = map[string]bool{
	CapSupported:   true,
	CapPartial:     true,
	CapUnsupported: true,
	CapUnverified:  true,
}

var platformVocabulary = map[string]bool{
	PlatformVerified:    true,
	PlatformExpected:    true,
	PlatformUnsupported: true,
}

// Validate checks one manifest: vocabulary, uniqueness of capability
// keys, a reason for every not-built/disabled/crashed state, and a
// non-empty known-gaps list whenever a capability is reduced below
// supported (no silent degradation).
func (m Manifest) Validate() error {
	if strings.TrimSpace(string(m.ID)) == "" || strings.TrimSpace(m.DisplayName) == "" {
		return fmt.Errorf("renderer %q: id and display name are required", m.ID)
	}
	if !maturityVocabulary[m.Maturity] {
		return fmt.Errorf("renderer %q: unknown maturity %q", m.ID, m.Maturity)
	}
	if !isolationVocabulary[m.Isolation] {
		return fmt.Errorf("renderer %q: unknown isolation %q", m.ID, m.Isolation)
	}
	if !stateVocabulary[m.State] {
		return fmt.Errorf("renderer %q: unknown state %q", m.ID, m.State)
	}
	if m.State != StateReady && strings.TrimSpace(m.StateReason) == "" {
		return fmt.Errorf("renderer %q: state %q requires a stateReason", m.ID, m.State)
	}
	seen := map[string]bool{}
	for name, cap := range m.Capabilities {
		if seen[name] {
			return fmt.Errorf("renderer %q: duplicate capability %q", m.ID, name)
		}
		seen[name] = true
		if !capVocabulary[cap.State] {
			return fmt.Errorf("renderer %q: capability %q has unknown state %q", m.ID, name, cap.State)
		}
		if cap.State != CapSupported && strings.TrimSpace(cap.Detail) == "" {
			return fmt.Errorf("renderer %q: capability %q below supported requires a detail", m.ID, name)
		}
	}
	for _, p := range m.Platforms {
		if !platformVocabulary[p.Status] {
			return fmt.Errorf("renderer %q: platform %s/%s has unknown status %q", m.ID, p.OS, p.Arch, p.Status)
		}
	}
	reduced := false
	for _, cap := range m.Capabilities {
		if cap.State != CapSupported {
			reduced = true
		}
	}
	if reduced && len(m.KnownGaps) == 0 {
		return fmt.Errorf("renderer %q: capabilities are reduced but knownGaps is empty — degradations must be documented", m.ID)
	}
	if strings.TrimSpace(m.ThemeMapping) == "" {
		return fmt.Errorf("renderer %q: themeMapping is required (full/partial/none + note)", m.ID)
	}
	return nil
}

// catalog — the ONE renderer registry. Facts pinned by the v1.11.1
// renderer-convergence run; every reduced claim carries its evidence
// boundary in Detail/Note/KnownGaps.
var catalog = []Manifest{
	{
		ID:          Fyne,
		DisplayName: "Fyne",
		Version:     "v2.8.2 (production shell)",
		Maturity:    MaturityProduction,
		Isolation:   IsolationInProcess,
		State:       StateReady,
		Platforms: []PlatformSupport{
			{OS: "linux", Arch: "amd64", Status: PlatformVerified, Note: "local + hosted gates; UI matrix under Xvfb"},
			{OS: "linux", Arch: "arm64", Status: PlatformExpected, Note: "fyne supports linux/arm64; not built or tested in this run"},
			{OS: "windows", Arch: "amd64", Status: PlatformVerified, Note: "hosted build run 43883548198 gates; UCRT64 CGO toolchain"},
			{OS: "windows", Arch: "arm64", Status: PlatformExpected, Note: "UCRT64 CGO toolchain publishes arm64; not built or tested in this run"},
			{OS: "darwin", Arch: "amd64", Status: PlatformExpected, Note: "not tested in this run"},
			{OS: "darwin", Arch: "arm64", Status: PlatformExpected, Note: "not tested in this run"},
			{OS: "android", Arch: "arm64", Status: PlatformUnsupported, Note: "no mobile target in this milestone"},
			{OS: "ios", Arch: "arm64", Status: PlatformUnsupported, Note: "no mobile target in this milestone"},
		},
		Capabilities: map[string]Capability{
			"touch":    {State: CapPartial, Detail: "pointer/tap supported; 44 DIP hit-target floor enforced in the shell; real touch hardware unverified in this run"},
			"keyboard": {State: CapSupported, Detail: "shortcuts + focus navigation tested"},
			"a11y":     {State: CapPartial, Detail: "Fyne accessibility flag exists; screen-reader trees not verified in this run"},
			"gpu":      {State: CapPartial, Detail: "GL driver; software llvmpipe in CI — hardware acceleration depends on the driver, never claimed from detection"},
			"webview":  {State: CapUnsupported, Detail: "no web surface in the Fyne shell"},
			"dialogs":  {State: CapSupported, Detail: "native file dialogs via the Fyne app driver"},
			"tray":     {State: CapUnsupported, Detail: "Fyne v2.8.2 exposes no in-process system-tray API"},
		},
		KnownGaps: []string{
			"mobile platforms unsupported in this milestone",
			"system-tray integration requires a separate module (not added in v1.11.1)",
		},
		ThemeMapping: "retired with the fyne shell — the web shell carries the token authority (internal/theme)",
		Notes:        "The default, launch-critical production shell. Keep-first policy: no renderer replaces Fyne before the full promotion gate passes.",
	},
	{
		ID:           GoGui,
		DisplayName:  "Go-Gui",
		Version:      "v0.96.0 (immutable tag 6f2d4e16e58b25fc2bec3d642e6552433a4e7ae4, checksum-verified)",
		Maturity:     MaturityPreview,
		Isolation:    IsolationCompanionWindow,
		State:        StateNotBuilt,
		StateReason:  "the POC lives in experiments/ui-engine-poc/go-gui (headless app tests 10/10) and is not built into this package — no fake preview button",
		PinnedModule: "github.com/go-gui-org/go-gui@v0.96.0",
		Platforms: []PlatformSupport{
			{OS: "windows", Arch: "amd64", Status: PlatformExpected, Note: "desktop POC basis; Windows UIA a11y verified upstream (Narrator/NVDA, amd64/arm64, announcements need Windows 10 1709+)"},
			{OS: "windows", Arch: "arm64", Status: PlatformExpected, Note: "UIA provider published for arm64"},
			{OS: "linux", Arch: "amd64", Status: PlatformExpected, Note: "not tested in this run"},
			{OS: "linux", Arch: "arm64", Status: PlatformExpected, Note: "not built or tested in this run; no upstream evidence contradicting desktop support"},
			{OS: "darwin", Arch: "amd64", Status: PlatformExpected, Note: "not tested in this run"},
			{OS: "darwin", Arch: "arm64", Status: PlatformExpected, Note: "Metal-backed native path per the upstream Native Platform Matrix; NOT verified on darwin/arm64 in this run — pending pinned go-gui darwin/arm64 build and runtime evidence (work order §4.8.3)"},
			{OS: "android", Arch: "arm64", Status: PlatformUnsupported, Note: "native open/save/folder/message/confirm dialogs not implemented upstream"},
			{OS: "ios", Arch: "arm64", Status: PlatformUnsupported, Note: "iOS a11y tree sync, URI opening, autocorrect/CJK marked text and clipboard wiring not implemented upstream"},
		},
		Capabilities: map[string]Capability{
			"touch":    {State: CapUnverified, Detail: "touch/IME hints documented upstream; no physical touch verification in this run"},
			"keyboard": {State: CapPartial, Detail: "POC drives keyboard entry through the framework test driver; native IME unverified"},
			"a11y":     {State: CapPartial, Detail: "Windows UIA (Narrator/NVDA) implemented upstream per the corrected Native Platform Matrix (16 Nov 2026) — NOT verified on hardware in this run; iOS a11y unavailable"},
			"gpu":      {State: CapPartial, Detail: "native GPU rendering documented upstream; backend unverified in this run"},
			"webview":  {State: CapUnsupported, Detail: "HTML preview is not a web browser"},
			"dialogs":  {State: CapPartial, Detail: "Windows desktop dialogs supported upstream; Android/iOS dialogs not implemented"},
		},
		KnownGaps: []string{
			"Android/iOS native dialogs not implemented upstream",
			"iOS accessibility tree sync and announcements unavailable",
			"iOS URI opening unimplemented",
			"iOS autocorrect, CJK marked-text conversion and clipboard wiring unimplemented",
			"no companion-window launch path in the v1.11.1 package (POC only)",
		},
		ThemeMapping: "partial — token mapping designed in docs/ui/DESIGN-SYSTEM.md; wired only inside the POC",
		Notes:        "Preview maturity REQUIRES the Windows desktop verification pass before any companion-window promotion; the pre-correction claim that Windows accessibility was missing is retired (Windows UIA is implemented upstream since the Native Platform Matrix of 16 Nov 2026).",
	},
	{
		ID:           GoGPUUI,
		DisplayName:  "GoGPU UI",
		Version:      "POC pin (experiments/ui-engine-poc/gogpu)",
		Maturity:     MaturityExperimental,
		Isolation:    IsolationCompanionWindow,
		State:        StateNotBuilt,
		StateReason:  "the GPU-surface POC lives in experiments/ui-engine-poc/gogpu and is not built into this package; backend selection is only claimed with executed-backend evidence",
		PinnedModule: "github.com/gogpu/ui (see experiments/ui-engine-poc/gogpu/go.mod for the exact pin)",
		Platforms: []PlatformSupport{
			{OS: "windows", Arch: "amd64", Status: PlatformExpected, Note: "Vulkan/DX12/GLES per upstream platform matrix"},
			{OS: "windows", Arch: "arm64", Status: PlatformExpected, Note: "upstream matrix covers Windows without an arch restriction; not built or tested in this run"},
			{OS: "linux", Arch: "amd64", Status: PlatformExpected, Note: "X11/Wayland (Vulkan/GLES) per upstream matrix"},
			{OS: "linux", Arch: "arm64", Status: PlatformExpected, Note: "upstream matrix covers Linux without an arch restriction; not built or tested in this run"},
			{OS: "darwin", Arch: "amd64", Status: PlatformExpected, Note: "Metal per upstream matrix; not tested in this run"},
			{OS: "darwin", Arch: "arm64", Status: PlatformExpected, Note: "Metal per upstream matrix; separate runtime matrix from Go-Gui — not built or verified on darwin/arm64 in this run (work order §4.8.4)"},
			{OS: "android", Arch: "arm64", Status: PlatformUnsupported, Note: "upstream marks Android support 'in review' — not a supported app host today"},
			{OS: "ios", Arch: "arm64", Status: PlatformUnsupported, Note: "a Metal/WGPU backend does not imply a supported iOS app host"},
		},
		Capabilities: map[string]Capability{
			"touch":    {State: CapUnverified, Detail: "no physical touch verification in this run"},
			"keyboard": {State: CapUnverified, Detail: "POC surface only"},
			"a11y":     {State: CapUnverified, Detail: "upstream documents accessibility roles; no screen-reader verification in this run"},
			"gpu":      {State: CapUnverified, Detail: "backend selection (Vulkan/DX12/Metal/GLES/software) is claimed only from executed-backend telemetry, never from device detection"},
			"webview":  {State: CapUnsupported, Detail: "not a web runtime"},
		},
		KnownGaps: []string{
			"Android support still 'in review' upstream — do not infer iOS support from WGPU/Metal",
			"no executed-backend telemetry in this run (software fallback where forced)",
			"no companion-window launch path in the v1.11.1 package (POC only)",
		},
		ThemeMapping: "partial — upstream documents 4 theme systems; the SHEYTAN token mapping is designed but unwired",
		Notes:        "Experimental: bounded desktop POC only; never a requirement on the Fyne production build (no Vulkan/DX12/Metal/WGPU/CGO-Rust-FFI dependency for the app).",
	},
	{
		ID:           WailsWebView,
		DisplayName:  "Wails Web Workspace",
		Version:      "v3.0.0 (stable desktop release, 2026-10-26)",
		Maturity:     MaturityExperimental,
		Isolation:    IsolationCompanionWindow,
		State:        StateNotBuilt,
		StateReason:  "the Web Workspace POC lives in experiments/ui-engine-poc/wails-webview (bridge/security/feature-detection contracts green under go test; the WebView window itself compiles only with the wails_runtime tag on a platform WebView host) and is not built into this package",
		PinnedModule: "github.com/wailsapp/wails/v3@v3.0.0 (verified stable release, 2026-10-26, resolved through the module proxy)",
		Platforms: []PlatformSupport{
			{OS: "windows", Arch: "amd64", Status: PlatformExpected, Note: "WebView2 runtime required — feature detection mandatory, never universal-parity claims"},
			{OS: "windows", Arch: "arm64", Status: PlatformExpected, Note: "WebView2 on arm64; not built or tested in this run"},
			{OS: "linux", Arch: "amd64", Status: PlatformExpected, Note: "GTK/WebKitGTK required"},
			{OS: "linux", Arch: "arm64", Status: PlatformExpected, Note: "WebKitGTK arm64; not built or tested in this run"},
			{OS: "darwin", Arch: "amd64", Status: PlatformExpected, Note: "WebKit; added only when actually tested"},
			{OS: "darwin", Arch: "arm64", Status: PlatformExpected, Note: "WebKit arm64; separate runtime matrix from Go-Gui — not built or verified on darwin/arm64 in this run (work order §4.8.4)"},
			{OS: "android", Arch: "arm64", Status: PlatformUnsupported, Note: "mobile support is experimental upstream and outside the stable desktop contract"},
			{OS: "ios", Arch: "arm64", Status: PlatformUnsupported, Note: "mobile support is experimental upstream and outside the stable desktop contract"},
		},
		Capabilities: map[string]Capability{
			"webview":  {State: CapPartial, Detail: "real browser APIs inside the platform WebView; HTML5/CSS/JS/WebGL/codec support varies by host engine — feature detection with an 'Open in system browser' fallback is mandatory"},
			"touch":    {State: CapUnverified, Detail: "host WebView touch behavior, not verified in this run"},
			"keyboard": {State: CapPartial, Detail: "host WebView input; IME behavior varies by engine"},
			"a11y":     {State: CapUnverified, Detail: "WebView content accessibility follows the host engine"},
			"gpu":      {State: CapUnverified, Detail: "WebGL/WebGPU depend on the host engine; never claimed from detection"},
		},
		KnownGaps: []string{
			"WebView is not equivalent to shipping the newest Chrome/Edge/Safari on every OS",
			"no Web Workspace surface in the v1.11.1 package (POC only)",
			"untrusted origins must never receive the privileged bridge (docs/architecture/WEBVIEW-SECURITY.md)",
		},
		ThemeMapping: "partial — tokens serialize to CSS via internal/theme Theme.JSON(); designed, unwired",
		Notes:        "The isolated WebAppHost layer: HTML/CSS/JS allowed ONLY here; never a second app-state or permission authority.",
	},
}

// Catalog returns the renderer registry (a defensive copy).
func Catalog() []Manifest {
	out := make([]Manifest, len(catalog))
	copy(out, catalog)
	return out
}

// Get resolves one manifest by ID.
func Get(id ID) (Manifest, bool) {
	for _, m := range catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Manifest{}, false
}

// ValidateCatalog validates every manifest and rejects duplicate IDs.
func ValidateCatalog() error {
	seen := map[ID]bool{}
	for _, m := range catalog {
		if seen[m.ID] {
			return fmt.Errorf("duplicate renderer id %q", m.ID)
		}
		seen[m.ID] = true
		if err := m.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ProductionIDs returns the engines with production maturity (the
// promotion policy's single source of truth for "which renderer is
// production").
func ProductionIDs() []ID {
	var out []ID
	for _, m := range catalog {
		if m.Maturity == MaturityProduction {
			out = append(out, m.ID)
		}
	}
	return out
}
