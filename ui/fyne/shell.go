//go:build fyne

// Shell — the spike's minimal shell window (run-#50 R4b).
//
// Responsive by construction: the shell listens to window resize and
// reflows — the navigation strip wraps with the window width, the content
// paragraph word-wraps, and the status line reports the live canvas size
// (spike instrumentation proving the resize wiring). No element carries a
// fixed pixel geometry; every inset comes from the token scale. Widgets
// are never re-parented at runtime (a fyne correctness hazard) — the
// topology is valid at every width.
package fyneui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Shell is the spike's one-window shell: navigation plus a swappable
// content area, reflowing with the window.
type Shell struct {
	window fyne.Window
}

// navItems — the shell's navigation destinations. The parity phase maps
// these onto the Qt host's real views (Agent, Workspace, Lab, Automation,
// System, Settings); the spike ships three so the boundary is obvious.
var navItems = []struct {
	Label string
	Icon  fyne.Resource
}{
	{Label: "Agent", Icon: theme.HomeIcon()},
	{Label: "Workspace", Icon: theme.FolderOpenIcon()},
	{Label: "Settings", Icon: theme.SettingsIcon()},
}

// placeholderPanel builds the ONE placeholder panel the spike ships: it
// names the phase boundary honestly (spike, not parity) and reflows its
// paragraph with the window width.
func placeholderPanel(title string) fyne.CanvasObject {
	head := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	head.Wrapping = fyne.TextWrapWord
	body := widget.NewLabel(
		"SPIKE panel — the one-Go-UI migration program starts here. " +
			"This shell proves the Fyne window, the product theme tokens, " +
			"size-driven layout and navigation. Capability parity with the " +
			"Qt host is the program's next phase; nothing here replaces the " +
			"shipped desktop application yet.",
	)
	body.Wrapping = fyne.TextWrapWord
	return container.NewVBox(
		head,
		widget.NewSeparator(),
		body,
	)
}

// NewShell builds and shows the spike window for the given app.
func NewShell(app fyne.App) *Shell {
	w := app.NewWindow("SHEYTAN-LA — one-Go-UI spike")
	shell := &Shell{window: w}

	// Navigation: one tab per destination, strip on top — fyne wraps tab
	// strips as the window narrows, so this topology reflows at every size.
	items := make([]*container.TabItem, 0, len(navItems))
	for _, it := range navItems {
		label := it.Label
		items = append(items, container.NewTabItemWithIcon(label, it.Icon,
			placeholderPanel(label)))
	}
	nav := container.NewAppTabs(items...)
	nav.SetTabLocation(container.TabLocationTop)

	// status: live canvas size — spike instrumentation, updated on every
	// reflow (fyne re-runs the root layout on window resize).
	status := widget.NewLabel("")
	status.TextStyle = fyne.TextStyle{Monospace: true}

	content := container.NewBorder(nav, status, nil, nil)
	root := container.New(&resizeAwareLayout{status: status}, content)
	w.SetContent(root)

	// initial hint only — the user resizes freely and everything reflows
	w.Resize(fyne.NewSize(960, 640))

	w.Show()
	return shell
}

// resizeAwareLayout keeps the content full-size and publishes the live
// canvas size to the status line on every layout pass — the spike's proof
// that resize reflows the whole tree (no fixed pixel geometry anywhere).
type resizeAwareLayout struct {
	status *widget.Label
}

func (l *resizeAwareLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
	l.status.SetText(fmt.Sprintf("canvas %dx%d dp — resize reflows this layout", int(size.Width), int(size.Height)))
}

func (l *resizeAwareLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	m := fyne.NewSize(0, 0)
	for _, o := range objs {
		if s := o.MinSize(); s.Width > m.Width || s.Height > m.Height {
			m = m.Max(s)
		}
	}
	return m
}
