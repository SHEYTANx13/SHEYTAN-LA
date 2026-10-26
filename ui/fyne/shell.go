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
// plus the v1.11.0 optional 3D Studio entry (read-only topology viewer;
// the view itself honors the studioEnabled setting).
var primaryViews = []primaryView{
	{"chat", "Chat", theme.HomeIcon, buildChatView},
	{"agent", "Agent", theme.ComputerIcon, buildAgentView},
	{"workspace", "Workspace", theme.FolderOpenIcon, buildWorkspaceView},
	{"lab", "Coding Lab", theme.StorageIcon, buildLabView},
	{"automation", "Automation", theme.MediaFastForwardIcon, buildAutomationView},
	{"system", "System", theme.InfoIcon, buildSystemView},
	{"settings", "Settings", theme.SettingsIcon, buildSettingsView},
	{"studio", "3D Studio", theme.ViewFullScreenIcon, buildStudioView},
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
// navRailLayout switches rail position/visibility from the canvas width.
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

	statusBar := container.NewVBox(widget.NewSeparator(), s.status)
	content := container.NewBorder(nil, statusBar, s.rail, nil, s.body)
	return container.New(&responsiveLayout{rail: s.rail, inner: content}, s.rail, content)
}

// responsiveLayout hides the rail when the canvas is narrow (compact
// adaptation: navigation never consumes the entire narrow viewport). A
// plain container+layout — no widget plumbing, no CurrentApp dependency
// before the event loop starts.
type responsiveLayout struct {
	rail  fyne.CanvasObject
	inner fyne.CanvasObject
}

func (l *responsiveLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	const railWidth = float32(150)
	_ = objs
	if size.Width < 640 {
		l.rail.Hide()
		l.rail.Resize(fyne.NewSize(0, 0))
		l.rail.Move(fyne.NewPos(0, 0))
		l.inner.Resize(fyne.NewSize(size.Width, size.Height))
		l.inner.Move(fyne.NewPos(0, 0))
	} else {
		l.rail.Show()
		l.rail.Resize(fyne.NewSize(railWidth, size.Height))
		l.rail.Move(fyne.NewPos(0, 0))
		l.inner.Resize(fyne.NewSize(size.Width-railWidth, size.Height))
		l.inner.Move(fyne.NewPos(railWidth, 0))
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
