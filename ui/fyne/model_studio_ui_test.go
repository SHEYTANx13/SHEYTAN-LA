//go:build !headless

// model_studio_ui_test.go — v1.11.1 Model Studio binding card (run
// #41415240891 §5.2/§7): the per-AI-System model assignment edited
// through the ONE store PUT. Pins: the systems fetch populates the card;
// inherit is the explicit first option; the Apply action sends the FULL
// fetched record (only the binding fields changed — nothing else can be
// wiped); the label renders the STORE's binding, not the form's; the
// Apply action re-arms on the terminal path; the systems fetch failing
// degrades visibly without killing the panel.
package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// newStudioPanelUnderTest builds the models panel against a fake core
// pre-seeded with two AI systems.
func newStudioPanelUnderTest(t *testing.T) (*modelsPanel, *fakeCore) {
	t.Helper()
	t.Cleanup(SetTestUIMarshal())

	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	srv.seedSystems([]AISystem{
		{SystemID: "default", Name: "Default", Revision: 1, ApprovalPolicy: "ask-risky"},
		{SystemID: "coder", Name: "Coder", Revision: 4, ApprovalPolicy: "ask-all"},
	}, "default")

	c := srv.client()
	ap := NewApp(test.NewApp(), c)
	t.Cleanup(func() { ap.rt.Shutdown() })

	var p *modelsPanel
	uiDoLocked(func() { _, p = buildModelsPanelFor(ap) })
	waitForText(t, func() string { return studioLabel(p) }, "Inherit is the default state")
	return p, srv
}

// studio accessors (UI-lock discipline as the other suites).
func studioLabel(p *modelsPanel) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.studioLabel.Text
}

func studioApplyDisabled(p *modelsPanel) bool {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.studioApply.Disabled()
}

func selectOptions(s *widget.Select) []string {
	return s.Options
}

// seedSystems installs the fake store contents before the panel exists.
func (f *fakeCore) seedSystems(systems []AISystem, active string) {
	f.systemsMux.Lock()
	defer f.systemsMux.Unlock()
	f.systems = systems
	f.systemsActive = active
}

func systemsPutCount(f *fakeCore) int {
	f.systemsMux.Lock()
	defer f.systemsMux.Unlock()
	return f.systemsPuts
}

func lastStoredSystem(f *fakeCore, id string) AISystem {
	f.systemsMux.Lock()
	defer f.systemsMux.Unlock()
	for _, s := range f.systems {
		if s.SystemID == id {
			return s
		}
	}
	return AISystem{}
}

func TestModelStudioCardPopulatesFromSystems(t *testing.T) {
	p, _ := newStudioPanelUnderTest(t)

	// The system select is populated with BOTH systems.
	waitFor(t, func() bool {
		uiTestMu.Lock()
		defer uiTestMu.Unlock()
		return len(p.studioSys.Options) == 2
	}, "two system options")

	// The Apply action is disabled until a system is chosen (choosing is
	// showing facts, not mutating).
	if !studioApplyDisabled(p) {
		t.Fatal("Apply must be disabled before a system is chosen")
	}
}

func TestModelStudioApplyBinding(t *testing.T) {
	p, srv := newStudioPanelUnderTest(t)

	// Choose the second system ("Coder") — its binding is inherit/inherit.
	uiDoLocked(func() { p.studioSys.SetSelectedIndex(1) })
	waitFor(t, func() bool {
		uiTestMu.Lock()
		defer uiTestMu.Unlock()
		return p.studioModel.Selected != "" && p.studioProfile.Selected != ""
	}, "binding selects populated")

	// The stored record has NO overrides — the selects show inherit.
	uiTestMu.Lock()
	gotModel := p.studioModel.Selected
	gotProfile := p.studioProfile.Selected
	uiTestMu.Unlock()
	if gotModel != studioInheritModel || gotProfile != studioInheritProfile {
		t.Fatalf("inherit binding must render the inherit options, got model=%q profile=%q", gotModel, gotProfile)
	}

	// Assign a model + profile and Apply.
	uiDoLocked(func() {
		p.studioModel.SetSelected("Model A")
		p.studioProfile.SetSelected("coding")
	})
	uiDoLocked(func() { p.applyStudio() })

	waitFor(t, func() bool { return systemsPutCount(srv) >= 1 }, "one store PUT")

	// The store holds the assignment; the label renders the STORE's
	// binding (with the bumped revision — server truth, not form truth).
	stored := lastStoredSystem(srv, "coder")
	if stored.Model != "Model A" || stored.RuntimeProfile != "coding" {
		t.Fatalf("store binding = model %q profile %q, want Model A/coding", stored.Model, stored.RuntimeProfile)
	}
	waitForText(t, func() string { return studioLabel(p) }, "rev 5")

	// Fields edited elsewhere are NOT wiped by the PUT: the approval
	// policy that came with the fetched record round-trips.
	if stored.ApprovalPolicy != "ask-all" {
		t.Fatalf("the full-record PUT must preserve approvalPolicy, got %q", stored.ApprovalPolicy)
	}

	// The Apply action re-arms on the terminal path.
	if studioApplyDisabled(p) {
		t.Fatal("Apply must re-arm after a successful apply")
	}
}

func TestModelStudioApplyErrorRearms(t *testing.T) {
	p, srv := newStudioPanelUnderTest(t)

	srv.setSystemsFail(true)

	uiDoLocked(func() { p.studioSys.SetSelectedIndex(0) })
	waitFor(t, func() bool {
		uiTestMu.Lock()
		defer uiTestMu.Unlock()
		return !p.studioApply.Disabled()
	}, "Apply armed")

	uiDoLocked(func() { p.applyStudio() })

	// The PUT fails; the action re-arms and the error surfaces (the
	// shared error label is visible).
	waitFor(t, func() bool {
		uiTestMu.Lock()
		defer uiTestMu.Unlock()
		return !p.studioApply.Disabled() && p.err.Visible()
	}, "re-armed after failure")
}

func TestModelStudioSystemsUnavailableDegradesVisibly(t *testing.T) {
	// A failing systems fetch renders the typed degradation message —
	// never a silent empty card, never a panel kill.
	t.Cleanup(SetTestUIMarshal())

	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	srv.setSystemsFail(true)

	c := srv.client()
	ap := NewApp(test.NewApp(), c)
	t.Cleanup(func() { ap.rt.Shutdown() })

	var p *modelsPanel
	uiDoLocked(func() { _, p = buildModelsPanelFor(ap) })
	waitForText(t, func() string { return studioLabel(p) }, "AI Systems unavailable")
}

// waitFor spins (bounded, mutex-yielding) until cond() is true.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		uiTestMu.Lock()
		uiTestMu.Unlock()
	}
	t.Fatalf("timed out waiting for: %s", what)
}
