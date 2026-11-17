//go:build !headless

// window_controls.go — the coherent three-control window system
// (v1.11.1 UX-05, §4.3 of the work order).
//
// DESIGN DECISION (the work order's own fallback rule): Fyne v2.8.2
// exposes NO public window-minimize or system-tray API (verified against
// the locked sources — only Close/Hide/FullScreen are public). The shell
// therefore keeps the NATIVE OS titlebar, whose caption buttons provide
// the real OS close/minimize behavior, and styles the in-app control
// trio honestly:
//
//   - Close SHEYTAN-LA — REAL: routes through App.RequestClose(), the ONE
//     graceful shutdown coordinator funnel (unsaved-content and active-task
//     confirmation policy preserved — never a kill).
//   - Minimize — DISABLED with the reason rendered as a visible label
//     (never hover-only): Fyne v2.8.2 has no minimize API; the native
//     titlebar minimize is the real path.
//   - Send to tray — DISABLED with its reason: no tray API and no tray
//     integration module in this milestone; a hidden window without a tray
//     icon would orphan the process — never a fake tray state.
//
// Each control is a custom widget (the repo's dotWidget idiom) with:
//   - a GUARANTEED 44×44 logical-DIP hit area (MinSize floor, UX-06);
//   - a recognizable glyph + a distinct SHEYTAN surface treatment;
//   - keyboard focus (Focusable) with a visible focus ring, Enter/Space
//     activation;
//   - hover/pressed/disabled states; disabled is high-contrast muted;
//   - accessible behavior via the visible label + focusable surface.
package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// titlebarButton is one window control: an interactive 44×44 DIP glyph
// tile. It is a custom widget (not a Button) because the hit-area floor
// and the glyph-only presentation must be guaranteed by MinSize, not
// hoped-for from a Button's content metrics.
type titlebarButton struct {
	widget.BaseWidget

	glyph string // the rendered glyph (✕ / — / ◣)
	label string // the accessible/action name ("Close SHEYTAN-LA")

	action func() // nil when disabled

	disabled bool
	reason   string // why the control is unavailable (rendered adjacent, never hover-only)

	focus   bool
	hover   bool
	pressed bool
}

// newTitlebarButton builds one control. action == nil marks the honest
// disabled state; reason is REQUIRED for disabled controls (tested).
func newTitlebarButton(glyph, label, reason string, action func()) *titlebarButton {
	b := &titlebarButton{
		glyph:    glyph,
		label:    label,
		action:   action,
		disabled: action == nil,
		reason:   reason,
	}
	b.ExtendBaseWidget(b)
	return b
}

// MinSize guarantees the touch floor: never below 44×44 logical DIPs
// (48 preferred is applied by the layout at touch-priority positions).
func (b *titlebarButton) MinSize() fyne.Size {
	m := b.BaseWidget.MinSize()
	w, h := m.Width, m.Height
	if w < TouchTargetMin {
		w = TouchTargetMin
	}
	if h < TouchTargetMin {
		h = TouchTargetMin
	}
	return fyne.NewSize(w, h)
}

// Tapped activates the control (touch and pointer share one path).
func (b *titlebarButton) Tapped(_ *fyne.PointEvent) {
	if b.disabled || b.action == nil {
		return
	}
	b.action()
	b.Refresh()
}

// Cursor reports the pointer affordance.
func (b *titlebarButton) Cursor() desktop.Cursor {
	if b.disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

// Hover states (visible affordance without hover-only dependence — the
// action itself is always a direct tap/keyboard activation).
func (b *titlebarButton) MouseIn(*desktop.MouseEvent) {
	b.hover = true
	b.Refresh()
}

func (b *titlebarButton) MouseMoved(*desktop.MouseEvent) {}

func (b *titlebarButton) MouseOut() {
	b.hover = false
	b.Refresh()
}

// MouseDown/MouseUp render the pressed state for pointer presses.
func (b *titlebarButton) MouseDown(*desktop.MouseEvent) {
	b.pressed = true
	b.Refresh()
}

func (b *titlebarButton) MouseUp(*desktop.MouseEvent) {
	b.pressed = false
	b.Refresh()
}

// Focusable: keyboard focus + Enter/Space activation (§4.3 keyboard
// contract; the focus ring is drawn by the renderer).
func (b *titlebarButton) FocusGained() {
	b.focus = true
	b.Refresh()
}

func (b *titlebarButton) FocusLost() {
	b.focus = false
	b.Refresh()
}

func (b *titlebarButton) TypedRune(_ rune) {}

func (b *titlebarButton) TypedKey(ev *fyne.KeyEvent) {
	if b.disabled || b.action == nil {
		return
	}
	if ev.Name == fyne.KeyReturn || ev.Name == fyne.KeySpace {
		b.action()
	}
}

// isDisabled reports the honest state (tests assert it).
func (b *titlebarButton) isDisabled() bool { return b.disabled }

// titlebarRenderer paints one control: rounded control surface, centered
// glyph, focus ring. State changes recolor through the CURRENT app theme
// (so theme switching repaints the controls like every other widget).
type titlebarRenderer struct {
	button *titlebarButton
	bg     *canvas.Rectangle
	glyph  *canvas.Text
	ring   *canvas.Rectangle
}

func (r *titlebarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.glyph, r.ring}
}

func (r *titlebarRenderer) Destroy() {}

func (r *titlebarRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	gw := r.glyph.MinSize()
	r.glyph.Move(fyne.NewPos((size.Width-gw.Width)/2, (size.Height-gw.Height)/2))
	r.ring.Resize(size)
	r.ring.Move(fyne.NewPos(0, 0))
}

func (r *titlebarRenderer) MinSize() fyne.Size {
	return fyne.NewSize(TouchTargetMin, TouchTargetMin)
}

func (r *titlebarRenderer) Refresh() {
	b := r.button

	// Surface: control → hover → pressed; disabled renders the muted
	// surface with a muted glyph.
	surface, glyphColor := currentPalette().Control, currentPalette().Text
	switch {
	case b.disabled:
		surface, glyphColor = currentPalette().Elevated, currentPalette().Muted
	case b.pressed:
		surface = currentPalette().Accent
		glyphColor = currentPalette().AccentText
	case b.hover:
		surface = currentPalette().ControlHover
	}
	r.bg.FillColor = surface
	r.bg.CornerRadius = RadiusControl
	r.bg.Hidden = false

	r.glyph.Text = b.glyph
	r.glyph.TextSize = IconL
	r.glyph.Color = glyphColor

	// Focus ring: the theme focus color, drawn only while focused.
	r.ring.StrokeColor, r.ring.StrokeWidth = color.Transparent, float32(0)
	if b.focus {
		r.ring.StrokeColor = currentPalette().Accent
		r.ring.StrokeWidth = 2
		r.ring.CornerRadius = RadiusControl
	}
	canvas.Refresh(r.bg)
	canvas.Refresh(r.glyph)
	canvas.Refresh(r.ring)
}

// currentPalette resolves the role set for the CURRENT theme variant —
// theme switching repaints the controls with every other surface.
func currentPalette() roleSet {
	return PaletteFor(themeVariantNow())
}

// themeVariantNow reports the current theme variant without importing the
// app wiring here (the engine gallery's theme service already switched
// the app theme; Fyne's Settings carries the variant on the theme
// object).
func themeVariantNow() fyne.ThemeVariant {
	app := fyne.CurrentApp()
	if app == nil {
		return theme.VariantDark
	}
	if t := app.Settings().Theme(); t != nil {
		if at, ok := t.(*appTheme); ok && at.active.Variant == "light" {
			return theme.VariantLight
		}
	}
	return theme.VariantDark
}

// CreateRenderer wires the widget into its renderer.
func (b *titlebarButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.Rectangle{}
	glyph := canvas.NewText(b.glyph, currentPalette().Text)
	glyph.Alignment = fyne.TextAlignCenter
	ring := canvas.Rectangle{}
	r := &titlebarRenderer{button: b, bg: &bg, glyph: glyph, ring: &ring}
	r.Refresh()
	return r
}

// windowControlsPanel renders the three-control system with the visible
// reason lines for the honest disabled states. closeAction is injected so
// tests pin the graceful-close funnel without arming the coordinator.
func windowControlsPanel(closeAction func()) fyne.CanvasObject {
	closeB := newTitlebarButton("✕", "Close SHEYTAN-LA", "", closeAction)
	minB := newTitlebarButton("—", "Minimize to taskbar",
		"Fyne v2.8.2 exposes no window-minimize API — the native OS titlebar minimize is the real path", nil)
	trayB := newTitlebarButton("◣", "Send to tray",
		"No system-tray API or integration module in v1.11.1 — a hidden window without a tray icon would orphan the process; never a fake tray state", nil)

	minReason := wrappedLabel(minB.reason)
	minReason.Importance = widget.MediumImportance
	trayReason := wrappedLabel(trayB.reason)
	trayReason.Importance = widget.MediumImportance

	return container.NewVBox(
		container.NewHBox(closeB, minB, trayB),
		minReason,
		trayReason,
	)
}
