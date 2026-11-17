package renderer

import (
	"runtime"
	"testing"
)

// TestCatalogValidates: every manifest satisfies the contract —
// vocabulary, reasons for reduced states, documented degradations.
func TestCatalogValidates(t *testing.T) {
	if err := ValidateCatalog(); err != nil {
		t.Fatalf("renderer catalog validation failed: %v", err)
	}
}

// TestProductionIsFyneOnly: the promotion policy pins Fyne as the ONE
// production renderer; nothing else is production until its gate passes.
func TestProductionIsFyneOnly(t *testing.T) {
	prod := ProductionIDs()
	if len(prod) != 1 || prod[0] != Fyne {
		t.Fatalf("production renderers = %v, want [fyne] only (a renderer is promoted ONLY through the promotion gate)", prod)
	}
	fyne, ok := Get(Fyne)
	if !ok {
		t.Fatal("the fyne manifest is missing")
	}
	if fyne.State != StateReady {
		t.Fatalf("fyne runtime state is %q, want ready (the production shell)", fyne.State)
	}
}

// TestNoEngineClaimsMobileFromDesktop: desktop evidence never promotes a
// renderer to mobile support — every manifest marks android/ios
// unsupported, unverified or upstream-experimental, never "verified".
func TestNoEngineClaimsMobileFromDesktop(t *testing.T) {
	for _, m := range Catalog() {
		for _, p := range m.Platforms {
			if p.OS != "android" && p.OS != "ios" {
				continue
			}
			if p.Status == PlatformVerified {
				t.Errorf("renderer %q claims VERIFIED %s/%s — mobile parity must never be claimed from desktop work", m.ID, p.OS, p.Arch)
			}
		}
	}
}

// TestGoGuiAccessibilityCorrection: the stale "Windows accessibility
// unimplemented" claim must NOT reappear — the corrected Native Platform
// Matrix (Mike Ward, 16 Nov 2026) implements Windows UIA for Narrator/NVDA
// on amd64/arm64, while the iOS gaps remain documented.
func TestGoGuiAccessibilityCorrection(t *testing.T) {
	m, ok := Get(GoGui)
	if !ok {
		t.Fatal("the gogui manifest is missing")
	}
	a11y, ok := m.Capabilities["a11y"]
	if !ok {
		t.Fatal("gogui manifest has no a11y capability")
	}
	for _, banned := range []string{"not implemented on Windows", "Windows accessibility unimplemented"} {
		if contains(a11y.Detail, banned) || contains(m.Notes, banned) {
			t.Errorf("gogui manifest repeats the stale claim %q (corrected upstream 2026-11-16)", banned)
		}
	}
	if !contains(a11y.Detail, "UIA") || !contains(a11y.Detail, "Narrator") {
		t.Errorf("gogui a11y detail must carry the corrected Windows UIA/Narrator-NVDA facts, got %q", a11y.Detail)
	}
	joined := joinGaps(m.KnownGaps)
	for _, gap := range []string{"iOS accessibility", "iOS URI opening", "iOS autocorrect"} {
		if !contains(joined, gap) {
			t.Errorf("gogui known gaps must document %q (remaining upstream matrix gap)", gap)
		}
	}
}

// TestWailsNeverClaimsUniversalWebParity: the WebView renderer must
// document host-engine variance and feature detection — never universal
// browser parity.
func TestWailsNeverClaimsUniversalWebParity(t *testing.T) {
	m, ok := Get(WailsWebView)
	if !ok {
		t.Fatal("the wails-webview manifest is missing")
	}
	joined := joinGaps(m.KnownGaps) + " " + m.Capabilities["webview"].Detail
	for _, required := range []string{"feature detection", "not equivalent"} {
		if !contains(joined, required) {
			t.Errorf("wails manifest must document %q — got %q", required, joined)
		}
	}
}

// TestEveryNonReadyStateHasReason: not-built/disabled/crashed states carry
// an operator-facing reason (the gallery renders it).
func TestEveryNonReadyStateHasReason(t *testing.T) {
	for _, m := range Catalog() {
		if m.State == StateReady {
			if m.StateReason != "" {
				t.Errorf("renderer %q: a ready state needs no reason", m.ID)
			}
			continue
		}
		if m.StateReason == "" {
			t.Errorf("renderer %q: state %q without a reason (the gallery would show an unexplained disabled control)", m.ID, m.State)
		}
	}
}

// TestCatalogCoversTheMilestoneEngines: the four renderer-convergence
// engines are all present.
func TestCatalogCoversTheMilestoneEngines(t *testing.T) {
	for _, id := range []ID{Fyne, GoGui, GoGPUUI, WailsWebView} {
		if _, ok := Get(id); !ok {
			t.Errorf("engine %q missing from the catalog", id)
		}
	}
}

// TestCurrentPlatformResolvable: every manifest resolves SOME status on
// the running platform (unknown platforms resolve as expected-not-known,
// never as a silently supported claim).
func TestCurrentPlatformResolvable(t *testing.T) {
	for _, m := range Catalog() {
		status, known := m.Supported(runtime.GOOS, runtime.GOARCH)
		if !known {
			t.Errorf("renderer %q has no platform entry for %s/%s — the gallery needs a truthful line for the running host", m.ID, runtime.GOOS, runtime.GOARCH)
			continue
		}
		if status == "" {
			t.Errorf("renderer %q: empty platform status for %s/%s", m.ID, runtime.GOOS, runtime.GOARCH)
		}
		if s := m.StatusLine(); s == "" {
			t.Errorf("renderer %q: StatusLine produced nothing", m.ID)
		}
	}
}

// TestValidateRejectsBrokenManifests: fail-closed manifest validation.
func TestValidateRejectsBrokenManifests(t *testing.T) {
	base, _ := Get(Fyne)

	noReason := base
	noReason.State = StateNotBuilt
	noReason.StateReason = ""
	if err := noReason.Validate(); err == nil {
		t.Error("a not-built manifest without a reason must be rejected")
	}

	badMaturity := base
	badMaturity.Maturity = "almost-ready"
	if err := badMaturity.Validate(); err == nil {
		t.Error("an unknown maturity must be rejected")
	}

	badCap := base
	badCap.Capabilities["gpu"] = Capability{State: "turbo", Detail: ""}
	if err := badCap.Validate(); err == nil {
		t.Error("an unknown capability state must be rejected")
	}

	silentReduction := Manifest{
		ID: "x", DisplayName: "X", Maturity: MaturityExperimental,
		Isolation: IsolationNotBuilt, State: StateNotBuilt, StateReason: "poc",
		Capabilities: map[string]Capability{
			"touch": {State: CapUnverified, Detail: "no evidence"},
		},
		ThemeMapping: "none",
	}
	if err := silentReduction.Validate(); err == nil {
		t.Error("a manifest with reduced capabilities and empty knownGaps must be rejected")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func joinGaps(gaps []string) string {
	out := ""
	for i, g := range gaps {
		if i > 0 {
			out += "; "
		}
		out += g
	}
	return out
}
