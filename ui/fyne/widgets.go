//go:build !headless

// widgets.go — the shared product UI kit: page headers, honest
// loading/empty/error surfaces, status badges and JSON evidence cards.
// Every view composes these so hierarchy, spacing and states stay coherent
// across the whole product. Nothing here fakes data: an empty surface
// never renders while loading, an error never renders as empty.
package fyneui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// PageHeader is the standard view header: title, optional subtitle, right
// slot for view actions.
func PageHeader(title, subtitle string, actions ...fyne.CanvasObject) fyne.CanvasObject {
	t := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	row := container.NewHBox(t)
	if subtitle != "" {
		s := widget.NewLabel(subtitle)
		s.TextStyle = fyne.TextStyle{Italic: true}
		s.Importance = widget.MediumImportance
		row.Add(s)
	}
	head := container.NewBorder(nil, nil, nil, container.NewHBox(actions...), row)
	return container.NewVBox(head, widget.NewSeparator())
}

// LoadingSurface is the visible in-flight state.
func LoadingSurface(what string) fyne.CanvasObject {
	sp := widget.NewProgressBarInfinite()
	sp.Stop() // start paused until laid out; view calls Start when shown
	return container.NewVBox(
		widget.NewLabel("Loading "+what+"…"),
		sp,
	)
}

// EmptySurface is the honest nothing-here state (distinct from loading and
// error — the contract the retired EmptyState.qml shipped).
func EmptySurface(icon, title, detail string) fyne.CanvasObject {
	return container.NewVBox(
		widget.NewLabelWithStyle(icon, fyne.TextAlignCenter, fyne.TextStyle{}),
		widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		wrappedLabel(detail),
	)
}

// ErrorSurface renders a typed failure with its reason — never a silent
// blank panel — plus an optional retry action.
func ErrorSurface(what string, err error, retry func()) fyne.CanvasObject {
	msg := "unknown failure"
	if err != nil {
		msg = err.Error()
	}
	label := widget.NewLabel("⚠ " + what + " failed")
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.Importance = widget.DangerImportance
	detail := wrappedLabel(msg)
	box := container.NewVBox(label, detail)
	if retry != nil {
		box.Add(widget.NewButton("Retry", retry))
	}
	return box
}

// wrappedLabel builds a word-wrapping label (no fixed geometry).
func wrappedLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	return l
}

// StatusBadge is a small colored state chip (colored dot + label). Color
// choices are semantic: success/warn/error/neutral.
func StatusBadge(text string, c color.Color) fyne.CanvasObject {
	l := widget.NewLabel(text)
	l.TextStyle = fyne.TextStyle{Monospace: true}
	return container.NewHBox(&dotWidget{color: c}, l)
}

// dotWidget paints a fixed-size colored dot (custom Fyne widget).
type dotWidget struct {
	widget.BaseWidget
	color color.Color
}

func newDot(c color.Color) *dotWidget {
	d := &dotWidget{color: c}
	d.ExtendBaseWidget(d)
	return d
}

func (d *dotWidget) CreateRenderer() fyne.WidgetRenderer {
	return &dotRenderer{dot: canvas.NewCircle(d.color)}
}

type dotRenderer struct{ dot *canvas.Circle }

func (r *dotRenderer) Layout(size fyne.Size) {
	r.dot.Resize(fyne.NewSize(10, 10))
	r.dot.Move(fyne.NewPos((size.Width-10)/2, (size.Height-10)/2))
}
func (r *dotRenderer) MinSize() fyne.Size           { return fyne.NewSize(12, 12) }
func (r *dotRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.dot} }
func (r *dotRenderer) Refresh()                     {}
func (r *dotRenderer) Destroy()                     {}

// EvidenceCard renders read-only JSON evidence (bounded), the pattern the
// System/Diagnostics views use for honest backend facts.
func EvidenceCard(title string, raw []byte) fyne.CanvasObject {
	return widget.NewCard(title, "", wrappedLabel(jsonPretty(raw, 4000)))
}

// kvRow helper for label/value lines.
func kvRow(k, v string) fyne.CanvasObject {
	key := widget.NewLabel(k)
	key.TextStyle = fyne.TextStyle{Bold: true}
	val := wrappedLabel(v)
	return container.NewHBox(key, val)
}

// infoLine returns "key: value" wrapped line.
func infoLine(k, v string) string {
	if v == "" {
		v = "—"
	}
	return fmt.Sprintf("%s: %s", k, v)
}
