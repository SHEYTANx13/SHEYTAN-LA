//go:build !headless

// shell.go — the responsive application shell. Wide windows show a left
// navigation rail with the 8-view IA; narrow windows collapse navigation
// to a top strip (primary destinations + a “More” utilities menu). A
// status line reports only real state: connection, core version, active
// model and live-stream progress (no fabricated telemetry). The content
// area hosts the active view; the secondary utility surfaces
// (History / Models / My Tools / Logs) live in a grouped Utilities
// section of the rail and in the compact strip’s More menu.
//
// v1.11.1 OWNERSHIP CONTRACT (operator-reported duplication fix): a Fyne
// CanvasObject has exactly ONE parent. The shell tree is built so the
// rail, the compact top strip and the content border each appear exactly
// once — as DIRECT children of the responsive container — and nothing is
// additionally nested inside another container that also lays it out.
// (The pre-fix tree inserted s.rail BOTH as the content border’s left
// object and as a direct child of the responsive container, so the rail
// was laid out by two parents and painted twice at wide widths — the
// concrete cause of the duplicated/cluttered navigation the operator
// observed on a real Windows PC.)
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

// primaryViews — the 8-layer IA (workspace.ts contract: ids, order, modes)
// plus the v1.11.0 optional 3D Studio entry (read-only topology viewer;
// the view itself honors the studioEnabled setting). This is the ONE
// canonical registry for primary destinations: the rail, the compact top
// strip and the Ctrl+number shortcut map all derive from it.
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

// subPanels — the secondary UTILITY surfaces. They are deliberately NOT
// peers of the primary destinations: the rail renders them in a grouped,
// visually subordinate Utilities section, and the compact strip reaches
// them through the “More” menu. One canonical registry, one grouping.
var subPanels = []subPanel{
	{"history", "History", theme.SearchIcon, buildHistoryPanel},
	{"models", "Models", theme.ListIcon, buildModelsPanel},
	{"tools", "My Tools", theme.CheckButtonIcon, buildToolsPanel},
	{"logs", "Logs", theme.FileTextIcon, buildLogsPanel},
}

// utilitiesGroupLabel is the rail’s muted section heading for the utility
// routes — the visual subordination cue that separates secondary surfaces
// from the primary destinations. Rendered as a RichText segment so the
// caption-size theme token (not a hardcoded point size) applies.
func utilitiesGroupLabel() fyne.CanvasObject {
	rt := widget.NewRichTextWithText("Utilities")
	if len(rt.Segments) > 0 {
		if seg, ok := rt.Segments[0].(*widget.TextSegment); ok {
			seg.Style.SizeName = theme.SizeNameCaptionText
			seg.Style.ColorName = theme.ColorNameDisabled
		}
	}
	return rt
}

// subPanel is a drawer-style surface opened over the shell (icon lazy —
// see primaryView).
type subPanel struct {
	id    string
	label string
	icon  func() fyne.Resource
	build func(*App) fyne.CanvasObject
}

// shell owns navigation + content + status.
type shell struct {
	app         *App
	mu          sync.Mutex
	body        *fyne.Container // swapped view content
	rail        *fyne.Container
	topNav      fyne.CanvasObject // compact top strip (navTopStrip zone)
	status      *widget.Label
	retryB      *widget.Button               // v1.11.1: safe reconnect action, shown only when disconnected
	errLine     *widget.Label                // v1.11.1: the actionable connection error, shown only when disconnected
	views       map[string]fyne.CanvasObject // cached built views (rebuilt on demand)
	active      string
	bootErr     error
	connected   bool
	connecting  bool // v1.11.1: explicit Connecting state (never guess from absence)
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

// showInitialView renders the FIRST primary view at boot. The shell's
// lazy-build rework lost the initial show call: the window then presented
// a blank primary surface until the first navigation click — exactly the
// "blank initial screen" class the recovery work order forbids.
func (ap *App) showInitialView() {
	if ap.shell == nil || len(primaryViews) == 0 {
		return
	}
	ap.shell.show(primaryViews[0].id)
}

// root assembles the full shell widget tree. Layout is responsive: the
// navPolicy resolves the navigation presentation from the canvas width —
// wide rail, compact rail, or a top navigation strip (with a More menu
// for the utility routes) — and navigation is reachable in EVERY zone
// (compact mode never silently hides it).
//
// OWNERSHIP (v1.11.1): the returned responsive container is the SINGLE
// owner of exactly three children — the rail, the compact top strip and
// the content border. The rail is NOT also the content border’s left
// object (the historical double-parent defect: the border laid it out
// AND the responsive container laid it out again, painting it twice at
// wide widths — the concrete cause of the operator’s duplicated
// navigation). The responsive layout is now the ONLY thing that sizes and
// positions the rail, and the content border’s geometry already accounts
// for it. Every destination is therefore rendered exactly once per
// viewport width.
func (s *shell) root() fyne.CanvasObject {
	s.body = container.NewStack()
	s.rail = container.NewVBox()
	s.status = widget.NewLabel("")
	s.status.TextStyle = fyne.TextStyle{Monospace: true}
	s.status.Truncation = fyne.TextTruncateEllipsis

	// v1.11.1: the safe reconnect action. Visible ONLY in the
	// disconnected state; retry is bounded by the app's single-flight
	// bootstrap guard (no retry storms).
	s.retryB = widget.NewButtonWithIcon("Retry", theme.ViewRefreshIcon(), func() {
		if ap := s.app; ap != nil {
			ap.retryBootstrap()
		}
	})
	s.retryB.Importance = widget.HighImportance
	s.retryB.Hide()

	// v1.11.1: the actionable error line — shown ONLY in the
	// disconnected state, next to the retry action.
	s.errLine = widget.NewLabel("")
	s.errLine.Importance = widget.DangerImportance
	s.errLine.Wrapping = fyne.TextWrapWord
	s.errLine.Hide()

	// Primary destinations in the rail.
	for _, v := range primaryViews {
		vv := v
		b := widget.NewButton(v.label, func() { s.show(vv.id) })
		b.Importance = widget.LowImportance
		s.rail.Add(b)
	}

	// Utilities — grouped, visually subordinate routes (History,
	// Models, My Tools, Logs) below a section heading, never a flat
	// undifferentiated list mixed with the primary destinations.
	s.rail.Add(widget.NewSeparator())
	s.rail.Add(utilitiesGroupLabel())
	for _, p := range subPanels {
		pp := p
		b := widget.NewButton(pp.label, func() { s.showSub(pp.id) })
		b.Importance = widget.LowImportance
		s.rail.Add(b)
	}

	// Compact top navigation: one button per primary view, horizontally
	// scrollable, plus a “More” button that opens the utilities menu —
	// so narrow windows keep every destination (primary AND utility)
	// reachable without duplicating any destination in the strip.
	strip := container.NewHBox()
	for _, v := range primaryViews {
		vv := v
		b := widget.NewButton(vv.label, func() { s.show(vv.id) })
		b.Importance = widget.LowImportance
		strip.Add(b)
	}
	more := widget.NewButtonWithIcon("More", theme.MoreVerticalIcon(), nil)
	more.Importance = widget.LowImportance
	more.OnTapped = func() { s.showUtilityMenu(more) }
	strip.Add(more)
	s.topNav = container.NewHScroll(strip)

	statusBar := container.NewVBox(widget.NewSeparator(), container.NewHBox(s.status, s.retryB), s.errLine)
	// ONE parent per object: the border owns ONLY the body + status
	// bar — the rail is owned by the responsive container below.
	content := container.NewBorder(nil, statusBar, nil, nil, s.body)
	// ONE parent per object: the responsive container is the sole
	// owner of the rail, the top strip and the content border.
	return container.New(&responsiveLayout{rail: s.rail, topNav: s.topNav, inner: content}, s.rail, s.topNav, content)
}

// buildUtilityMenu builds the grouped utilities menu (one item per
// secondary route, dispatching through the same showSub path as the rail
// buttons). Extracted so the menu content itself is directly testable.
func (s *shell) buildUtilityMenu() *fyne.Menu {
	items := make([]*fyne.MenuItem, 0, len(subPanels))
	for _, p := range subPanels {
		pp := p
		items = append(items, fyne.NewMenuItem(pp.label, func() { s.showSub(pp.id) }))
	}
	return fyne.NewMenu("Utilities", items...)
}

// showUtilityMenu opens the grouped utilities menu below the compact
// strip’s “More” button. The menu is a transient canvas OVERLAY — it adds
// no second navigation tree and owns no durable state; its items dispatch
// through the same showSub path as the rail buttons.
func (s *shell) showUtilityMenu(anchor fyne.CanvasObject) {
	if anchor == nil {
		return
	}
	app := fyne.CurrentApp()
	if app == nil || app.Driver() == nil {
		return
	}
	c := app.Driver().CanvasForObject(anchor)
	if c == nil {
		return // detached composition (tests) — nothing to anchor on
	}
	widget.ShowPopUpMenuAtRelativePosition(s.buildUtilityMenu(), c, fyne.NewPos(0, anchor.Size().Height), anchor)
}

// navZone is the resolved navigation presentation for one available width.
type navZone int

const (
	navRailWide    navZone = iota // >= wideBreakpoint: full rail
	navRailCompact                // >= compactBreakpoint: narrow rail
	navTopStrip                   // below compactBreakpoint: top strip
)

func (z navZone) String() string {
	switch z {
	case navRailWide:
		return "navRailWide"
	case navRailCompact:
		return "navRailCompact"
	default:
		return "navTopStrip"
	}
}

// sizeClass is the v1.11.1 responsive contract's four logical-width
// classes (work order for run 43008361835 §F4/§5.3). ONE policy definition
// shared by navigation and views — never duplicated thresholds.
type sizeClass int

const (
	// sizeCompact: phone/narrow windows (<600) — mobile-style top strip
	// with reachable compact navigation and one primary content column; no
	// permanent sidebars.
	sizeCompact sizeClass = iota
	// sizeMedium: tablets (600–899) — collapsible compact navigation rail;
	// one main pane.
	sizeMedium
	// sizeDesktop: laptops/standard desktops (900–1279) — persistent
	// wide navigation rail.
	sizeDesktop
	// sizeWide: wide desktops (>=1280) — same persistent rail; the shell
	// has no inspector/side panel surface in v1.11.1, so wide keeps the
	// desktop navigation policy (documented, not a hidden fourth layout).
	sizeWide
)

func (c sizeClass) String() string {
	switch c {
	case sizeCompact:
		return "compact"
	case sizeMedium:
		return "medium"
	case sizeDesktop:
		return "desktop"
	default:
		return "wide"
	}
}

// Responsive breakpoints and zone dimensions. ONE policy, adopted from the
// v1.11.1 responsive contract: compact <600 (top strip), 600–899 medium
// (compact rail), >=900 desktop/wide (wide rail). The product adapts to the
// ACTUAL available LOGICAL width (Fyne canvas units — never physical
// framebuffer pixels) through the same pure functions on every platform.
const (
	compactBreakpoint = float32(600)
	wideBreakpoint    = float32(900)
	railWidthWide     = float32(170)
	railWidthCompact  = float32(120)
	topStripHeight    = float32(44)
)

// sizeClassFor is the SINGLE size-class decision — a pure function over the
// logical canvas width.
func sizeClassFor(width float32) sizeClass {
	switch {
	case width >= 1280:
		return sizeWide
	case width >= wideBreakpoint:
		return sizeDesktop
	case width >= compactBreakpoint:
		return sizeMedium
	default:
		return sizeCompact
	}
}

// navPolicy is the SINGLE responsive navigation decision — derived from the
// size class so navigation and views can never disagree about thresholds.
// It is a pure function over the available width so the layout policy is
// unit-testable independently of widget trees and window systems.
func navPolicy(width float32) navZone {
	switch sizeClassFor(width) {
	case sizeCompact:
		return navTopStrip
	case sizeMedium:
		return navRailCompact
	default: // sizeDesktop, sizeWide
		return navRailWide
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
	// Build OUTSIDE the mutex: a lazy build calls ap.shell.register(),
	// which takes s.mu itself — building under the lock self-deadlocks the
	// UI thread on the FIRST navigation to a not-yet-built view (a latent
	// freeze exposed by the Phase 2 navigation tests; CI's view tests
	// built views detached from a shell, and the Xvfb smoke never clicks
	// navigation). Navigation callbacks run on the single UI event loop,
	// so the unlocked build cannot double-build one view.
	s.mu.Lock()
	v, ok := s.views[id]
	s.mu.Unlock()
	if !ok {
		for _, pv := range primaryViews {
			if pv.id == id {
				v = pv.build(s.app)
				break
			}
		}
		// v1.11.1 hardening: an unknown id must never reach the body —
		// container.NewMax(nil) panics the moment a real canvas lays the
		// stack out (showSub already guards; show now does too).
		if v == nil {
			return
		}
		s.mu.Lock()
		s.views[id] = v
		s.mu.Unlock()
	}
	s.mu.Lock()
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

// setConnecting marks the bootstrap in flight (v1.11.1: the status
// surface must show an explicit Connecting state, never a guess).
func (s *shell) setConnecting() {
	s.mu.Lock()
	s.connecting = true
	s.mu.Unlock()
	s.updateStatus()
}

// setBootError renders a typed connection failure honestly.
func (s *shell) setBootError(err error) {
	s.mu.Lock()
	s.bootErr = err
	s.connected = false
	s.connecting = false
	s.mu.Unlock()
	s.updateStatus()
}

// setConnected marks the handshake successful.
func (s *shell) setConnected() {
	s.mu.Lock()
	s.connected = true
	s.connecting = false
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
	} else if s.connecting {
		conn = "connecting"
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

	// v1.11.1: an actionable error + a safe retry surface in the
	// disconnected state (the retry button marshals through the app's
	// single-flight bootstrap guard; Connecting/Connected hide it).
	if s.connected || s.connecting || s.bootErr == nil {
		s.retryB.Hide()
		s.errLine.Hide()
	} else {
		s.retryB.Show()
		s.errLine.SetText("Not connected: " + s.bootErr.Error() + " — check that the app was started normally; Retry re-attempts the connection.")
		s.errLine.Show()
	}
}

// registerShortcuts wires desktop keyboard navigation (Ctrl+1..8 for the
// primary views in registry order — the number row exactly covers the
// eight-entry registry — and Ctrl+N new session).
func (ap *App) registerShortcuts() {
	c := ap.w.Canvas()
	keyByIndex := []fyne.KeyName{fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4, fyne.Key5, fyne.Key6, fyne.Key7, fyne.Key8}
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
