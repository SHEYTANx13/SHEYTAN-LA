//go:build !headless

package fyneui

import (
	"fyne.io/fyne/v2"
)

// adaptiveDualPane is the v1.11.1 compact one-column repair for the
// conversation views. At compact widths (< compactBreakpoint logical)
// the secondary pane (session/goal list) stacks ABOVE the primary
// conversation column — the documented compact contract ("one primary
// content column, no permanent sidebars") that an unconditional HSplit
// violated: at 320-568 phone widths the split squeezed the conversation
// column to a sliver and the composer's controls rendered outside the
// visible area (the defect the button-fit matrix test exposed).
//
// At medium+ widths the side-by-side arrangement is preserved with the
// SAME proportions the former HSplit used (0.28 chat / 0.26 agent), as a
// fixed proportional split: the draggable divider of the stock HSplit is
// traded for the compact one-column guarantee — a documented, intended
// v1.11.1 change (ui-contract.md), never a silent regression.
//
// Ownership: the container owns exactly the two panes; nothing is
// duplicated between arrangements (the one-parent invariant holds — the
// panes are laid out, never re-parented).
func newAdaptiveDualPane(offset float32, secondary, primary fyne.CanvasObject) *fyne.Container {
	return fyne.NewContainerWithLayout(dualPaneLayout{offset: offset}, secondary, primary)
}

// dualPaneLayout implements the adaptive arrangement.
type dualPaneLayout struct {
	offset float32 // side-by-side share of the secondary pane (0..1)
}

func (l dualPaneLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) != 2 {
		return
	}
	secondary, primary := objs[0], objs[1]

	w, h := size.Width, size.Height
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	if w < compactBreakpoint {
		// ONE content column: the secondary list takes a bounded share on
		// top; the conversation fills the rest. Both panes get the full
		// width, so no child can be squeezed below its MinSize.
		secH := h * 0.3
		if secH > 220 {
			secH = 220
		}
		if secH < 0 {
			secH = 0
		}
		if h < secH {
			secH = h // degenerate guard: the primary pane keeps a non-negative share
		}
		secondary.Move(fyne.NewPos(0, 0))
		secondary.Resize(fyne.NewSize(w, secH))
		primary.Move(fyne.NewPos(0, secH))
		primary.Resize(fyne.NewSize(w, h-secH))
		return
	}

	secW := w * clampOffset(l.offset)
	secondary.Move(fyne.NewPos(0, 0))
	secondary.Resize(fyne.NewSize(secW, h))
	primary.Move(fyne.NewPos(secW, 0))
	primary.Resize(fyne.NewSize(w-secW, h))
}

func (l dualPaneLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	// The compact stack is the worst case: both panes at full width, one
	// above the other.
	min := fyne.NewSize(0, 0)
	for _, o := range objs {
		m := o.MinSize()
		if m.Width > min.Width {
			min.Width = m.Width
		}
		min.Height += m.Height
	}
	return min
}

func clampOffset(o float32) float32 {
	if o < 0.1 {
		return 0.1
	}
	if o > 0.9 {
		return 0.9
	}
	return o
}
