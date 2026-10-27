//go:build !headless

// shell.go — the responsive application shell. Wide windows show a left
// navigation rail with the 7-view IA; narrow windows collapse navigation
// to a top strip. A status line reports only real state: connection,
// core version, active model and live-stream progress (no fabricated
// telemetry). The content area hosts the active view; sub-surfaces
// (History / Models / My Tools / Logs) open as panels with a back affordance.
package fyneui

import (
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// primaryView is one navigation destination. The icon is a LAZY lookup:
// theme icon functions call fyne.CurrentApp(), which is nil during
// package init — the lookups run when the shell is built (app exists).
type primaryView struct {
	id    string
	label string
	icon  func() fyne.Resource
	build func(*App) fyne.CanvasObject
}

// primaryViews — the 7-layer IA (workspace.ts contract: ids, order, modes)
// plus the v1.11.0 optional 3D Studio entry (read-only topology viewer)
// and the v1.11.0 hybrid-UI Web Workspace entry (typed rich-content
// renderer). Each view honors its own enabling setting.
var primaryViews = []primaryView{
	{"chat", "Chat", theme.HomeIcon, buildChatView},
	{"agent", "Agent", theme.ComputerIcon, buildAgentView},
	{"workspace", "Workspace", theme.FolderOpenIcon, buildWorkspaceView},
	{"lab", "Coding Lab", theme.StorageIcon, buildLabView},
	{"automation", "Automation", theme.MediaFastForwardIcon, buildAutomationView},
	{"system", "System", theme.InfoIcon, buildSystemView},
	{"settings", "Settings", theme.SettingsIcon, buildSettingsView},
	{"studio", "3D Studio", theme.ViewFullScreenIcon, buildStudioView},
	{"webws", "Web Workspace", theme.GridIcon, buildWebWorkspaceView},
}

// subPanel is a drawer-style surface opened over the shell (icon lazy —
// see primaryView).
type subPanel struct {
	id    string
	label string
	icon  func() fyne.Resource
	build func(*App) fyne.CanvasObject
}

var subPanels = []subPanel{
	{"history", "History", theme.SearchIcon, buildHistoryPanel},
	{"models", "Models", theme.ListIcon, buildModelsPanel},
	{"tools", "My Tools", theme.CheckButtonIcon, buildToolsPanel},
	{"logs", "Logs", theme.FileTextIcon, buildLogsPanel},
}

// shell owns navigation + content + status.
type shell struct {
	app         *App
	mu          sync.Mutex
	body        *fyne.Container // swapped view content
	rail        *fyne.Container
	topNav      fyne.CanvasObject // compact top strip (navTopStrip zone)
	status      *widget.Label
	views       map[string]fyne.CanvasObject // cached built views (rebuilt on demand)
	active      string
	bootErr     error
	connected   bool
	coreVersion string
	onStream    []func()
	onSettled   []func()
	onSessions  []func()
}

// register adds view callbacks (constructor time; dispatch starts only
// after the app's OnStarted hook). Views never clobber each other: the
// registry is append-only and the App installs ONE dispatch.
func (s *shell) register(onStream, onSettled, onSessions func()) {
	if s == nil {
		// Views can be constructed detached from a shell (tests,
		// composition probes): callbacks simply have no dispatcher.
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if onStream != nil {
		s.onStream = append(s.onStream, onStream)
	}
	if onSettled != nil {
		s.onSettled = append(s.onSettled, onSettled)
	}
	if onSessions != nil {
		s.onSessions = append(s.onSessions, onSessions)
	}
}

func (s *shell) notifyStream() {
	s.mu.Lock()
	fs := append([]func(){}, s.onStream...)
	s.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

func (s *shell) notifySettled() {
	s.mu.Lock()
	fs := append([]func(){}, s.onSettled...)
	s.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

func (s *shell) notifySessions() {
	s.mu.Lock()
	fs := append([]func(){}, s.onSessions...)
	s.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

func newShell(ap *App) *shell {
	return &shell{
		app:   ap,
		views: map[string]fyne.CanvasObject{},
	}
}

// root assembles the full shell widget tree. Layout is responsive: the
// navPolicy resolves the navigation presentation from the canvas width —
// wide rail, compact rail, or a top navigation strip — and navigation is
// reachable in EVERY zone (compact mode never silently hides it).
func (s *shell) root() fyne.CanvasObject {
	s.body = container.NewStack()
	s.rail = container.NewVBox()
	s.status = widget.NewLabel("")
	s.status.TextStyle = fyne.TextStyle{Monospace: true}
	s.status.Truncation = fyne.TextTruncateEllipsis

	// Primary rail buttons + sub-panel buttons.
	for _, v := range primaryViews {
		vv := v
		b := widget.NewButton(v.label, func() { s.show(vv.id) })
		b.Importance = widget.LowImportance
		s.rail.Add(b)
	}
	s.rail.Add(widget.NewSeparator())
	for _, p := range subPanels {
		pp := p
		b := widget.NewButton(pp.label, func() { s.showSub(pp.id) })
		b.Importance = widget.LowImportance
		s.rail.Add(b)
	}

	// Compact top navigation: one button per primary view, horizontally
	// scrollable, so narrow windows keep every destination reachable.
	strip := container.NewHBox()
	for _, v := range primaryViews {
		vv := v
		b := widget.NewButton(vv.label, func() { s.show(vv.id) })
		b.Importance = widget.LowImportance
		strip.Add(b)
	}
	s.topNav = container.NewHScroll(strip)

	statusBar := container.NewVBox(widget.NewSeparator(), s.status)
	content := container.NewBorder(nil, statusBar, s.rail, nil, s.body)
	return container.New(&responsiveLayout{rail: s.rail, topNav: s.topNav, inner: content}, s.rail, s.topNav, content)
}

// navZone is the resolved navigation presentation for one available width.
type navZone int

const (
	navRailWide    navZone = iota // >= wideBreakpoint: full rail
	navRailCompact                // >= compactBreakpoint: narrow rail
	navTopStrip                   // below compactBreakpoint: top strip
)

// Responsive breakpoints and zone dimensions. One policy, three zones:
// the product adapts to the ACTUAL available width rather than a single
// preset breakpoint (360-wide laptop windows through 1920+ desktops and
// high-DPI scaling all resolve through the same pure function).
const (
	compactBreakpoint = float32(640)
	wideBreakpoint    = float32(1000)
	railWidthWide     = float32(170)
	railWidthCompact  = float32(120)
	topStripHeight    = float32(44)
)

// navPolicy is the SINGLE responsive decision — a pure function over the
// available width so the layout policy is unit-testable independently of
// widget trees and window systems.
func navPolicy(width float32) navZone {
	switch {
	case width >= wideBreakpoint:
		return navRailWide
	case width >= compactBreakpoint:
		return navRailCompact
	default:
		return navTopStrip
	}
}

// responsiveLayout applies navPolicy to the shell geometry: it sizes and
// positions the rail, the compact top strip and the content area, with
// guards against zero/negative available sizes (tiny windows, collapsed
// splitters, pre-layout zero passes).
type responsiveLayout struct {
	rail   fyne.CanvasObject
	topNav fyne.CanvasObject
	inner  fyne.CanvasObject
}

func (l *responsiveLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	_ = objs
	w := size.Width
	h := size.Height
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	switch navPolicy(w) {
	case navRailWide:
		railW := railWidthWide
		if w < railW {
			railW = w // degenerate guard: never overflow the canvas
		}
		l.topNav.Hide()
		l.topNav.Resize(fyne.NewSize(0, 0))
		l.rail.Show()
		l.rail.Resize(fyne.NewSize(railW, h))
		l.rail.Move(fyne.NewPos(0, 0))
		l.inner.Resize(fyne.NewSize(w-railW, h))
		l.inner.Move(fyne.NewPos(railW, 0))
	case navRailCompact:
		railW := railWidthCompact
		if w < railW {
			railW = w
		}
		l.topNav.Hide()
		l.topNav.Resize(fyne.NewSize(0, 0))
		l.rail.Show()
		l.rail.Resize(fyne.NewSize(railW, h))
		l.rail.Move(fyne.NewPos(0, 0))
		l.inner.Resize(fyne.NewSize(w-railW, h))
		l.inner.Move(fyne.NewPos(railW, 0))
	default: // navTopStrip
		l.rail.Hide()
		l.rail.Resize(fyne.NewSize(0, 0))
		l.rail.Move(fyne.NewPos(0, 0))
		stripH := topStripHeight
		if h < stripH {
			stripH = h // degenerate guard: content keeps a non-negative share
		}
		l.topNav.Show()
		l.topNav.Resize(fyne.NewSize(w, stripH))
		l.topNav.Move(fyne.NewPos(0, 0))
		l.inner.Resize(fyne.NewSize(w, h-stripH))
		l.inner.Move(fyne.NewPos(0, stripH))
	}
}

func (l *responsiveLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(360, 320) // product minimum
}

// show switches to a primary view (rebuilt on demand; one instance per id
// so state survives navigation without shadowing the runtime).
func (s *shell) show(id string) {
	s.mu.Lock()
	v, ok := s.views[id]
	if !ok {
		for _, pv := range primaryViews {
			if pv.id == id {
				v = pv.build(s.app)
				break
			}
		}
		s.views[id] = v
	}
	s.active = id
	s.mu.Unlock()
	s.body.Objects = []fyne.CanvasObject{container.NewMax(v)}
	s.body.Refresh()
	s.updateStatus()
}

// showSub opens a drawer-style surface (History/Models/Tools/Logs).
func (s *shell) showSub(id string) {
	var v fyne.CanvasObject
	for _, p := range subPanels {
		if p.id == id {
			v = p.build(s.app)
			break
		}
	}
	if v == nil {
		return
	}
	back := widget.NewButton("← Back", func() { s.show(s.active) })
	back.Importance = widget.HighImportance
	panel := container.NewBorder(container.NewHBox(back), nil, nil, nil, v)
	s.body.Objects = []fyne.CanvasObject{container.NewMax(panel)}
	s.body.Refresh()
}

// setBootError renders a typed connection failure honestly.
func (s *shell) setBootError(err error) {
	s.mu.Lock()
	s.bootErr = err
	s.connected = false
	s.mu.Unlock()
	s.updateStatus()
}

// setConnected marks the handshake successful.
func (s *shell) setConnected() {
	s.mu.Lock()
	s.connected = true
	s.bootErr = nil
	s.mu.Unlock()
	s.updateStatus()
}

func (s *shell) updateStatus() {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn := "disconnected"
	if s.connected {
		conn = "connected"
	}
	ver := s.coreVersion
	if ver == "" {
		ver = "v" + "unknown"
	}
	model := ""
	if sess, ok := s.app.rt.ActiveSession(); ok && sess.Model != "" {
		model = " · model " + sess.Model
	}
	mode := s.app.rt.Mode()
	s.status.SetText(fmt.Sprintf("%s · %s · mode %s%s", conn, ver, mode, model))
}

// registerShortcuts wires desktop keyboard navigation (Ctrl+1..N views in
// order, Ctrl+N new session). Views beyond the mapped number row simply
// have no shortcut — never a broken binding on an empty key.
func (ap *App) registerShortcuts() {
	c := ap.w.Canvas()
	keyByIndex := []fyne.KeyName{fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4, fyne.Key5, fyne.Key6, fyne.Key7}
	for i, v := range primaryViews {
		if i >= len(keyByIndex) {
			break // number row exhausted; the view stays reachable by pointer
		}
		vv := v
		key := keyByIndex[i]
		c.AddShortcut(&desktop.CustomShortcut{KeyName: key, Modifier: fyne.KeyModifierControl},
			func(fyne.Shortcut) {
				ap.shell.show(vv.id)
			})
	}
	c.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyN, Modifier: fyne.KeyModifierControl},
		func(fyne.Shortcut) {
			go func() {
				if _, err := ap.rt.CreateSession(ap.Ctx(), ""); err == nil {
					ap.shell.updateStatus()
				}
			}()
		})
}

// appIcon returns the product icon resource (embedded brand asset).
func appIcon() fyne.Resource {
	return brandIconResource()
}
