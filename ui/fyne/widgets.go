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
// PageHeader builds the page title row. v1.11.1 fit repair: the
// subtitle WRAPS below the title (the former side-by-side HBox placed
// the header's minimum width past every compact viewport — header text
// clipped, actions pushed out).
func PageHeader(title, subtitle string, actions ...fyne.CanvasObject) fyne.CanvasObject {
	t := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	var head fyne.CanvasObject = t
	if subtitle != "" {
		s := widget.NewLabel(subtitle)
		s.Wrapping = fyne.TextWrapWord
		s.TextStyle = fyne.TextStyle{Italic: true}
		s.Importance = widget.MediumImportance
		head = container.NewVBox(t, s)
	}
	row := container.NewBorder(nil, nil, nil, container.NewHBox(actions...), head)
	return container.NewVBox(row, widget.NewSeparator())
}

// cardFit builds a titled card whose explanatory caption is a WRAPPED
// label INSIDE the content. widget.NewCard subtitles never wrap: a long
// subtitle drives the card's MinSize past every compact width, clipping
// the card content (the v1.11.1 fit-matrix defect class). The caption
// keeps its explanatory value directly adjacent to the controls.
func cardFit(title, caption string, content fyne.CanvasObject) *widget.Card {
	if caption != "" {
		cap := widget.NewLabel(caption)
		cap.Wrapping = fyne.TextWrapWord
		cap.TextStyle = fyne.TextStyle{Italic: true}
		cap.Importance = widget.MediumImportance
		content = container.NewVBox(cap, content)
	}
	return widget.NewCard(title, "", content)
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
// v1.11.1 renderer-convergence repair: the label is created EMPTY with
// Wrapping applied BEFORE SetText. Creating it with the full text first
// (widget.NewLabel) measures the unwrapped line and sizes the label to
// it; a later Refresh re-measures the wrap AT that creation width, so the
// min never shrinks and every owning container caches an inflated
// MinSize — the exact mechanism that laid the Engines gallery's scroll
// content out to 388 DIP wide inside a 304 DIP viewport (TestButtonFitMatrix:
// the theme Select stretched to 364). With the wrap armed before the text
// arrives, the re-measured minimum is the WRAPPED minimum (the longest
// word), and the scroll content never inflates past the viewport.
func wrappedLabel(s string) *widget.Label {
	l := widget.NewLabel("")
	l.Wrapping = fyne.TextWrapWord
	if s != "" {
		l.SetText(s)
	}
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

// touchFloorLayout is the structural UX-06 hit-area floor (run 45165536396
// UX pass). The 44/48 DIP touch contract (docs/ui/TOUCH-RESPONSIVE-MATRIX.md
// §1, tokens TouchTargetMin/TouchTargetPreferred/RowRelaxed) was
// documented, but only the Engines window controls enforced it
// structurally — the navigation chrome and the composer shipped at the
// Fyne default height (~36 DIP), below the floor on every phone-targeted
// size class. floorTouch WRAPS the real control in a one-child container
// (never re-types it): the wrapped widget stays an authentic
// *widget.Button for the button-fit walk and the tap path, while the
// layout enforces the floor at EVERY width — phone, tablet and big
// screens alike. The child fills the floored area, so the whole surface
// is tappable; a HIDDEN child contributes a zero minimum (no reserved
// gap for the disconnected-state Retry).
type touchFloorLayout struct {
	minHeight float32
}

func (l *touchFloorLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	w, h := float32(0), float32(0)
	for _, o := range objects {
		if !o.Visible() {
			continue // a hidden floored control reserves no space
		}

		s := o.MinSize()
		if s.Width > w {
			w = s.Width
		}

		hh := s.Height
		if hh < l.minHeight {
			hh = l.minHeight
		}

		if hh > h {
			h = hh
		}
	}

	return fyne.NewSize(w, h)
}

// Layout stretches the child across the FULL floored area: the entire
// surface is the tap target, not a small control floating inside it.
func (l *touchFloorLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

// floorTouch wraps content with the given minimum hit-area height.
func floorTouch(content fyne.CanvasObject, minHeight float32) fyne.CanvasObject {
	return container.New(&touchFloorLayout{minHeight: minHeight}, content)
}
