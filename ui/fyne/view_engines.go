//go:build !headless

// view_engines.go — the Engine Gallery (v1.11.1 UX-01..UX-05).
//
// ONE registry-driven surface: every engine in internal/renderer renders
// as a card with its truthful maturity, runtime state (with the
// operator-facing reason for every non-ready engine), per-platform
// verification for THIS machine, capabilities (honest states only) and
// known gaps. The Fyne production engine is the live reference; the
// not-built engines show their POC boundary — no fake launch buttons, no
// static screenshot pretending to be interactive (the work order's §0
// honesty rule).
//
// The Appearance section carries the live theme switcher (theme_service.go
// contract: select = live preview, Save = the only persistence path,
// Cancel = restore saved) and the window-controls trio (window_controls.go).
//
// No client/API calls happen here: the catalog is compiled-in truth, so
// the view builds detached from a live core (test-friendly, bootable
// before connect).
package fyneui

import (
	"fmt"
	"image/color"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/renderer"
	themetokens "github.com/SHEYTANx13/SHEYTAN-LA/internal/theme"
)

// enginesView is the Engines & Appearance surface.
type enginesView struct {
	ap *App

	// Theme switcher (UX-01).
	themeSel  *widget.Select
	themeLine *widget.Label
	saveB     *widget.Button
	cancelB   *widget.Button

	// Window controls (UX-05) — exposed for the hit-area/funnel tests.
	closeCtl *titlebarButton
	minCtl   *titlebarButton
	trayCtl  *titlebarButton
}

// buildEnginesView assembles the gallery. Reachable as the "engines"
// primary view from the shell registry (and Ctrl+9).
func buildEnginesView(ap *App) fyne.CanvasObject {
	v := &enginesView{ap: ap}

	// ---- Theme switcher (UX-01) -------------------------------------
	// Selecting applies LIVE (preview); Save persists; Cancel restores.
	themeNames := make([]string, 0, 6)
	nameToID := map[string]string{}
	for _, t := range themetokens.Catalog() {
		themeNames = append(themeNames, t.Name)
		nameToID[t.Name] = t.ID
	}
	currentName := v.themeNameForID(ap.Themes().Active())
	// Build the status line BEFORE SetSelected: the Select fires its
	// OnChanged callback immediately during build, and the callback
	// reaches refreshThemeLine — a nil label here panicked the build.
	v.themeLine = wrappedLabel("")
	v.saveB = widget.NewButton("Save theme", func() { v.saveTheme() })
	v.cancelB = widget.NewButton("Cancel", func() { v.cancelTheme() })
	v.themeSel = widget.NewSelect(themeNames, func(name string) {
		if id, ok := nameToID[name]; ok {
			v.applyTheme(id)
		}
	})
	v.themeSel.SetSelected(currentName)
	v.refreshThemeLine()

	// ---- Engine cards (UX-02/UX-03) ----------------------------------
	cards := container.NewVBox()
	for _, m := range renderer.Catalog() {
		cards.Add(v.engineCard(m))
	}

	// ---- Window controls (UX-05) -------------------------------------
	// The trio panel is built here so the tests reach the SAME widgets the
	// operator does; the close action routes through the App funnel.
	controls := v.buildWindowControls()

	root := container.NewBorder(
		PageHeader("Engines", "Renderer catalog, themes and window controls — truthful states only"),
		nil, nil, nil,
		container.NewScroll(container.NewVBox(
			cardFit("Theme", "Live preview on select; Save is the only persistence path; Cancel restores the saved theme.", container.NewVBox(
				v.themeSel,
				container.NewHBox(v.cancelB, v.saveB),
				v.themeLine,
			)),
			cards,
			cardFit("Window controls", "Close is real (graceful shutdown funnel). Minimize/tray are disabled with their technical reasons — never fake states.", controls),
		)),
	)
	return root
}

// ---- Theme switching (UX-01 acceptance) -------------------------------

// themeNameForID maps a catalog id to its display name (Select works on
// names; unknown ids keep the selection empty rather than inventing one).
func (v *enginesView) themeNameForID(id string) string {
	t, ok := themetokens.ByID(id)
	if !ok {
		return ""
	}
	return t.Name
}

// applyTheme switches the rendered theme live (no core reset, no
// navigation reset — pinned by TestThemeSwitchKeepsNavigation).
func (v *enginesView) applyTheme(id string) {
	if err := v.ap.Themes().Apply(id); err != nil {
		v.themeLine.SetText("Theme switch failed: " + err.Error())
		return
	}
	v.refreshThemeLine()
}

// cancelTheme restores the previously saved theme.
func (v *enginesView) cancelTheme() {
	v.ap.Themes().Cancel()
	v.themeSel.SetSelected(v.themeNameForID(v.ap.Themes().Active()))
	v.refreshThemeLine()
}

// saveTheme persists the previewed theme.
func (v *enginesView) saveTheme() {
	v.ap.Themes().Save()
	v.refreshThemeLine()
}

// refreshThemeLine renders the honest apply/cancel/save state.
func (v *enginesView) refreshThemeLine() {
	s := v.ap.Themes()
	if s.IsPreviewing() {
		v.themeLine.SetText(fmt.Sprintf("Previewing %q (unsaved) — Save persists it, Cancel restores %q.",
			s.Active(), s.Saved()))
		return
	}
	v.themeLine.SetText(fmt.Sprintf("Saved theme: %q.", s.Saved()))
}

// ---- Engine cards (UX-02/UX-03) ---------------------------------------

// maturityColor maps the maturity vocabulary to semantic status colors.
func maturityColor(m renderer.Maturity) color.Color {
	switch m {
	case renderer.MaturityProduction:
		return Palette.Success
	case renderer.MaturityPreview:
		return Palette.Warn
	case renderer.MaturityExperimental:
		return Palette.Info
	default:
		return Palette.Muted
	}
}

// engineCard renders one manifest: identity, version, maturity badge,
// state+reason, this-platform line, capabilities (honest), known gaps,
// and the action row. NOT-built engines render a disabled action with
// their reason — the gallery never offers an action the package cannot
// perform (§4.1 "never fake a launchable engine").
//
// FIT CONTRACT (TestButtonFitMatrix): every text that can exceed a compact
// viewport either WRAPS or truncates — the bold engine NAME wraps (its
// unwrapped measurement drove a 300 DIP content minimum), and the card
// TITLE carries no decorative suffix (fyne measures card titles
// unwrapped; "Wails Web Workspace engine" drove a 388.5 DIP card minimum
// inside a 304 DIP viewport — the horizontal-escape defect class).
func (v *enginesView) engineCard(m renderer.Manifest) fyne.CanvasObject {
	body := container.NewVBox()

	// Identity + maturity badge — the name WRAPS so a long engine name can
	// never set the scroll content's minimum width.
	name := widget.NewLabel("")
	name.Wrapping = fyne.TextWrapWord
	name.TextStyle = fyne.TextStyle{Bold: true}
	name.SetText(m.DisplayName)
	body.Add(container.NewHBox(
		name,
		StatusBadge(string(m.Maturity), maturityColor(m.Maturity)),
	))
	body.Add(wrappedLabel("Version: " + m.Version))
	if m.PinnedModule != "" {
		body.Add(wrappedLabel("Pin: " + m.PinnedModule))
	}

	// Runtime state + reason (the honest unavailable marking).
	stateLine := fmt.Sprintf("State: %s", m.State)
	if m.StateReason != "" {
		stateLine += " — " + m.StateReason
	}
	body.Add(wrappedLabel(stateLine))

	// This-platform verification line.
	platform, known := m.Supported(runtime.GOOS, runtime.GOARCH)
	if !known {
		platform = renderer.PlatformExpected
	}
	platLine := fmt.Sprintf("This platform (%s/%s): %s", runtime.GOOS, runtime.GOARCH, platform)
	body.Add(wrappedLabel(platLine))
	body.Add(widget.NewSeparator())

	// Capabilities (honest states only — the registry vocabulary is the
	// display vocabulary; nothing here upgrades a detection).
	body.Add(widget.NewLabelWithStyle("Capabilities", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	for _, name := range capabilityOrder {
		cap, ok := m.Capabilities[name]
		if !ok {
			continue
		}
		line := fmt.Sprintf("  %s: %s", name, cap.State)
		if cap.Detail != "" {
			line += " — " + cap.Detail
		}
		l := wrappedLabel(line)
		l.TextStyle = fyne.TextStyle{Monospace: true}
		body.Add(l)
	}

	// Known gaps.
	if len(m.KnownGaps) > 0 {
		body.Add(widget.NewLabelWithStyle("Known gaps", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		for _, g := range m.KnownGaps {
			body.Add(wrappedLabel("  • " + g))
		}
	}
	body.Add(widget.NewSeparator())

	// Theme mapping note (UX-01 cross-engine contract).
	body.Add(wrappedLabel("Theme mapping: " + m.ThemeMapping))

	// Action row: the ONLY real action for the production engine is its
	// status (it IS the shell); not-built engines show a disabled action —
	// never a fake launch button. The button label stays SHORT: a button's
	// MinSize follows its text, and a long reason on a button inflates the
	// scroll content width at compact sizes (the button-fit matrix catches
	// exactly that). The full reason stays in the wrapped body above.
	action := widget.NewButton("Activate engine", nil)
	if m.State != renderer.StateReady {
		action.Disable()
		action.Text = "Unavailable — reason above"
	}
	if m.State == renderer.StateReady {
		action.Text = "Active — the production shell"
		action.Disable() // already active; an "activate" action would be a lie
	}
	body.Add(action)

	return cardFit(m.DisplayName, "Isolation: "+string(m.Isolation), body)
}

// capabilityOrder fixes the capability display order (deterministic card
// layout across themes/sizes — structural goldens depend on it).
var capabilityOrder = []string{"touch", "keyboard", "a11y", "gpu", "webview", "dialogs", "tray"}

// ---- Window controls (UX-05) -------------------------------------------

// buildWindowControls renders the trio with the app's graceful-close
// funnel and keeps the buttons on the view for tests.
func (v *enginesView) buildWindowControls() fyne.CanvasObject {
	ap := v.ap
	v.closeCtl = newTitlebarButton("✕", "Close SHEYTAN-LA", "", func() { ap.RequestClose() })
	v.minCtl = newTitlebarButton("—", "Minimize to taskbar",
		"Fyne v2.8.2 exposes no window-minimize API — the native OS titlebar minimize is the real path", nil)
	v.trayCtl = newTitlebarButton("◣", "Send to tray",
		"No system-tray API or integration module in v1.11.1 — a hidden window without a tray icon would orphan the process; never a fake tray state", nil)

	minReason := wrappedLabel(v.minCtl.reason)
	minReason.Importance = widget.MediumImportance
	trayReason := wrappedLabel(v.trayCtl.reason)
	trayReason.Importance = widget.MediumImportance

	return container.NewVBox(
		container.NewHBox(v.closeCtl, v.minCtl, v.trayCtl),
		minReason,
		trayReason,
	)
}
