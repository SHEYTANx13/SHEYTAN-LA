//go:build !headless

// shell.go — the responsive application shell, ported from ui/fyne/shell.go
// to the go-gui stateless-view pipeline.
//
// The IA is IDENTICAL to the Fyne shell (the ONE canonical registry):
// nine primary views (chat, agent, workspace, lab, automation, system,
// settings, studio, engines) and four grouped utility sub panels
// (history, models, tools, logs). Wide windows show a left navigation
// rail; narrow windows collapse navigation to a top strip with every
// destination still reachable. A status line reports only real state:
// connection, core version, active model and live-run progress — no
// fabricated telemetry. Retry + error line appear ONLY while disconnected
// (v1.11.1 contract, kept verbatim).
//
// go-gui mapping: there is no retained widget tree — the shell is a pure
// function of shellState (the window's State value) recomputed every
// frame. show() only flips state; the next frame renders the new
// destination. The single-parent ownership contract of the Fyne shell is
// structural here: every frame mints a fresh tree, so a widget can never
// be laid out by two parents.
package goguiui

import (
	"fmt"
	"sync"

	"github.com/go-gui-org/go-gui/gui"
)

// primaryView is one navigation destination (registry entry).
type primaryView struct {
	id    string
	label string
	build func(*shellState) gui.View
}

// subPanel is a drawer-style utility surface.
type subPanel struct {
	id    string
	label string
	build func(*shellState) gui.View
}

// primaryViews — the 9-layer IA (workspace.ts contract: ids, order) plus
// the 3D Studio entry (core-owned loopback viewer) and the Engines &
// Appearance gallery. THE canonical registry: the rail, the compact top
// strip and the Ctrl+number shortcut map all derive from it.
var primaryViews = []primaryView{
	{"chat", "Chat", buildChatView},
	{"agent", "Agent", buildAgentView},
	{"workspace", "Workspace", buildWorkspaceView},
	{"lab", "Coding Lab", buildLabView},
	{"automation", "Automation", buildAutomationView},
	{"system", "System", buildSystemView},
	{"settings", "Settings", buildSettingsView},
	{"studio", "3D Studio", buildStudioView},
	{"engines", "Engines", buildEnginesView},
}

// subPanels — the secondary UTILITY surfaces, deliberately NOT peers of
// the primary destinations: the rail renders them under a "Utilities"
// heading, the compact strip reaches them through the More menu.
var subPanels = []subPanel{
	{"history", "History", buildHistoryPanel},
	{"models", "Models", buildModelsPanel},
	{"tools", "My Tools", buildToolsPanel},
	{"logs", "Logs", buildLogsPanel},
}

// shellState is THE window state. One typed value the whole shell reads;
// every mutation boundary is the state mutex (background workers mutate
// through uiDo, which lands on the frame thread in production).
type shellState struct {
	ap *App

	mu sync.Mutex

	// navigation
	active  string // active primary view id
	subOpen string // open utility panel id ("" = none)

	// connection surface (the ONLY connection truth the shell shows)
	connected   bool
	connecting  bool
	bootErr     error
	coreVersion string

	// dock pane sizes (persisted under the app data dir)
	dock DockLayoutState

	// per-view state instances (lazily created; the views carry their
	// own mutex-guarded fields where background workers touch them)
	views map[string]any

	// notification fanout (runtime → shell callbacks)
	onStream   []func()
	onSettled  []func()
	onSessions []func()
}

// newShell builds the shell state over the app.
func newShell(ap *App) *shellState {
	return &shellState{
		ap:     ap,
		active: primaryViews[0].id,
		views:  map[string]any{},
	}
}

// view returns the per-view state instance for id, creating it with mk on
// first use. View state survives navigation (one instance per id, the
// Fyne shell's "one instance per id" rule, kept).
func (s *shellState) view(id string, mk func() any) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.views[id]; ok {
		return v
	}
	v := mk()
	s.views[id] = v
	return v
}

// register adds view callbacks (constructor time; dispatch starts only
// after the app's OnInit hook). Views never clobber each other: the
// registry is append-only and the App installs ONE dispatch.
func (s *shellState) register(onStream, onSettled, onSessions func()) {
	if s == nil {
		return // detached view construction (tests) — no dispatcher
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

func (s *shellState) fanout(fs []func()) {
	s.mu.Lock()
	cp := append([]func(){}, fs...)
	s.mu.Unlock()
	for _, f := range cp {
		f()
	}
}

func (s *shellState) notifyStream()   { s.fanout(s.onStream) }
func (s *shellState) notifySettled()  { s.fanout(s.onSettled) }
func (s *shellState) notifySessions() { s.fanout(s.onSessions) }

// --- connection surface (setConnecting/setBootError/setConnected) ---

func (s *shellState) setConnecting() {
	s.mu.Lock()
	s.connecting = true
	s.mu.Unlock()
}

func (s *shellState) setBootError(err error) {
	s.mu.Lock()
	s.bootErr = err
	s.connected = false
	s.connecting = false
	s.mu.Unlock()
}

func (s *shellState) setConnected() {
	s.mu.Lock()
	s.connected = true
	s.connecting = false
	s.bootErr = nil
	s.mu.Unlock()
}

// connectionSnapshot reads the connection surface under the lock.
func (s *shellState) connectionSnapshot() (connected, connecting bool, bootErr error, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected, s.connecting, s.bootErr, s.coreVersion
}

// show switches to a primary view (state flip; the frame renders it).
func (s *shellState) show(id string) {
	for _, pv := range primaryViews {
		if pv.id == id {
			s.mu.Lock()
			s.active = id
			s.subOpen = ""
			s.mu.Unlock()
			return
		}
	}
	// An unknown id must never reach the body (v1.11.1 hardening, kept).
}

// showSub opens a drawer-style utility surface.
func (s *shellState) showSub(id string) {
	for _, p := range subPanels {
		if p.id == id {
			s.mu.Lock()
			s.subOpen = id
			s.mu.Unlock()
			return
		}
	}
}

// showInitial renders the FIRST primary view at boot (the shell must
// never present a blank primary surface before the first navigation —
// the v1.11.1 recovery contract, kept).
func (s *shellState) showInitial() {
	if len(primaryViews) == 0 {
		return
	}
	s.show(primaryViews[0].id)
}

// activeView returns the currently shown primary id.
func (s *shellState) activeView() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// openSub returns the open utility panel id ("" when closed).
func (s *shellState) openSub() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subOpen
}

// --- size classes + navigation policy (SAME numbers as the Fyne shell) ---

type sizeClass int

const (
	sizeCompact sizeClass = iota // <600 — top strip
	sizeMedium                   // 600–899 — compact rail
	sizeDesktop                  // 900–1279 — wide rail + inspector/drawer
	sizeWide                     // >=1280 — wide rail + inspector/drawer
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

// Responsive breakpoints (logical units) — ONE policy, adopted from the
// v1.11.1 responsive contract and mirrored by internal/theme Bands().
const (
	compactBreakpoint = float32(600)
	wideBreakpoint    = float32(900)
	railWidthWide     = float32(170)
	railWidthCompact  = float32(120)
	topStripHeight    = float32(44)
)

// sizeClassFor is the SINGLE size-class decision — a pure function over
// the logical window width.
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

type navZone int

const (
	navRailWide navZone = iota
	navRailCompact
	navTopStrip
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

// navPolicy is the SINGLE responsive navigation decision — derived from
// the size class so navigation and views can never disagree.
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

// --- view functions ---

// rootView is the window root: responsive navigation + dock workspace +
// status line. Every frame regenerates from state — no cached trees, no
// double ownership.
func (s *shellState) rootView(w *gui.Window) gui.View {
	ww, wh := w.WindowSize()
	width := float32(ww)
	if width <= 0 {
		width = float32(wh) // degenerate guard (pre-layout probes)
		if width <= 0 {
			width = 1080
		}
	}
	class := sizeClassFor(width)

	body := s.bodyView(w, class)
	status := s.statusView()

	switch navPolicy(width) {
	case navRailWide:
		return gui.Row(gui.ContainerCfg{
			Sizing:  gui.FillFill,
			Content: []gui.View{s.railView(class), gui.Column(gui.ContainerCfg{Sizing: gui.FillFill, Content: []gui.View{body, status}})},
		})
	case navRailCompact:
		return gui.Row(gui.ContainerCfg{
			Sizing:  gui.FillFill,
			Content: []gui.View{s.railView(class), gui.Column(gui.ContainerCfg{Sizing: gui.FillFill, Content: []gui.View{body, status}})},
		})
	default: // navTopStrip
		return gui.Column(gui.ContainerCfg{
			Sizing:  gui.FillFill,
			Content: []gui.View{s.topStripView(), body, status},
		})
	}
}

// railView builds the left navigation rail (primary destinations, then a
// separator + the grouped Utilities section).
func (s *shellState) railView(class sizeClass) gui.View {
	primary := make([]gui.View, 0, len(primaryViews))
	for _, v := range primaryViews {
		vv := v
		variant := gui.ButtonSecondary
		if s.activeView() == vv.id {
			variant = gui.ButtonPrimary
		}
		primary = append(primary, gui.Button(gui.ButtonCfg{
			ID:      absID("nav", vv.id),
			Label:   vv.label,
			Variant: variant,
			OnClick: func(gui.EventCtx) { s.show(vv.id) },
		}))
	}
	utils := make([]gui.View, 0, len(subPanels))
	for _, p := range subPanels {
		pp := p
		utils = append(utils, gui.Button(gui.ButtonCfg{
			ID:      absID("nav-sub", pp.id),
			Label:   pp.label,
			OnClick: func(gui.EventCtx) { s.showSub(pp.id) },
		}))
	}
	content := append(primary,
		gui.Separator(gui.SeparatorCfg{}),
		gui.Text(gui.TextCfg{Text: "Utilities", TextStyle: gui.CurrentTheme().TextStyleCaption}),
	)
	content = append(content, utils...)
	return gui.Column(gui.ContainerCfg{
		ID:      absID("rail"),
		Sizing:  gui.FitFill,
		Spacing: gui.SpacingPx(4),
		Padding: gui.PadAll(6),
		Content: content,
	})
}

// topStripView builds the compact top navigation: one button per primary
// view plus a More button that opens the utilities surface (every
// destination reachable at compact widths — never silently hidden).
func (s *shellState) topStripView() gui.View {
	items := make([]gui.View, 0, len(primaryViews)+1)
	for _, v := range primaryViews {
		vv := v
		variant := gui.ButtonSecondary
		if s.activeView() == vv.id {
			variant = gui.ButtonPrimary
		}
		items = append(items, gui.Button(gui.ButtonCfg{
			ID:      absID("strip", vv.id),
			Label:   vv.label,
			Variant: variant,
			OnClick: func(gui.EventCtx) { s.show(vv.id) },
		}))
	}
	items = append(items, gui.Button(gui.ButtonCfg{
		ID:      absID("strip-more"),
		Label:   "More",
		OnClick: func(gui.EventCtx) { s.showSub("history") }, // the grouped utilities entry point
	}))
	return gui.Row(gui.ContainerCfg{
		ID:      absID("strip"),
		Sizing:  gui.FixedFit,
		Height:  topStripHeight,
		Spacing: gui.SpacingPx(4),
		Padding: gui.PadAll(4),
		Content: items,
	})
}

// statusView is the status line: connection, core version, active model,
// plus the v1.11.1 retry + error line ONLY while disconnected.
func (s *shellState) statusView() gui.View {
	connected, connecting, bootErr, version := s.connectionSnapshot()
	conn := "disconnected"
	if connected {
		conn = "connected"
	} else if connecting {
		conn = "connecting"
	}
	ver := version
	if ver == "" {
		ver = "v" + "unknown"
	}
	model := ""
	if sess, ok := s.ap.rt.ActiveSession(); ok && sess.Model != "" {
		model = " · model " + sess.Model
	}
	mode := s.ap.rt.Mode()

	views := []gui.View{
		gui.Separator(gui.SeparatorCfg{}),
		gui.Text(gui.TextCfg{
			ID:        absID("status"),
			Text:      fmt.Sprintf("%s · %s · mode %s%s", conn, ver, mode, model),
			TextStyle: gui.CurrentTheme().TextStyleCode,
		}),
	}
	if !connected && !connecting && bootErr != nil {
		views = append(views,
			gui.Row(gui.ContainerCfg{
				Sizing:  gui.FillFit,
				Spacing: gui.SpacingPx(6),
				Content: []gui.View{
					gui.Button(gui.ButtonCfg{
						ID:      absID("retry"),
						Label:   "Retry",
						Variant: gui.ButtonPrimary,
						OnClick: func(gui.EventCtx) { s.ap.retryBootstrap() },
					}),
					wrappedText("Not connected: " + bootErr.Error() + " — check that the app was started normally; Retry re-attempts the connection."),
				},
			}),
		)
	}
	return gui.Column(gui.ContainerCfg{
		ID:      absID("statusbar"),
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingPx(4),
		Padding: gui.PadAll(4),
		Content: views,
	})
}

// bodyView assembles the dock workspace (all classes) plus the right
// contextual inspector and the bottom activity drawer on desktop classes.
func (s *shellState) bodyView(w *gui.Window, class sizeClass) gui.View {
	if sv := s.openSub(); sv != "" {
		return s.subPanelView(sv)
	}
	dock := s.dockView(w)
	if class != sizeDesktop && class != sizeWide {
		return dock
	}
	// Desktop classes: the dock is flanked by the contextual inspector
	// (right) and the activity drawer sits below (vertical splitter).
	// Simple but functional: the inspector mirrors the active view's
	// context card; the drawer streams the activity ring captions.
	main := gui.Splitter(gui.SplitterCfg{
		ID: absID("dock-inspector"),
		First: gui.SplitterPaneCfg{
			Content: []gui.View{dock},
			MinSize: 420,
		},
		Second: gui.SplitterPaneCfg{
			Content: []gui.View{s.inspectorView()},
			MinSize: 200,
		},
		Ratio:    gui.SomeF(s.dock.InspectorRatio),
		OnChange: func(ratio float32, _ gui.SplitterCollapsed, _ gui.EventCtx) { s.dock.setInspectorRatio(ratio) },
	})
	// KNOWN LIMITATION (v1 port, honest gap): the bottom activity drawer
	// is a fixed-height strip, not a splitter-resizable pane. A vertical
	// splitter's first pane clamped its CROSS-axis width to MinSize (or
	// collapsed without it) under the no-measurer layout, squeezing the
	// dock to a sliver; the drawer resize contract is deferred until the
	// splitter cross-axis contract is re-verified against go-gui on a
	// native runner. The dock pane tree and the inspector splitter carry
	// the required resizable docking behavior.
	_ = s.dock.DrawerRatio
	return gui.Column(gui.ContainerCfg{
		ID:      absID("dock-drawer"),
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingPx(4),
		Content: []gui.View{
			main,
			s.activityDrawerView(),
		},
	})
}

// inspectorView is the right contextual inspector: the live run context
// (phase + stream status) — presentation truth only.
func (s *shellState) inspectorView() gui.View {
	acc := s.ap.rt.Stream()
	content := []gui.View{
		gui.Text(gui.TextCfg{Text: "Inspector", TextStyle: gui.CurrentTheme().TextStyleTitleSmall}),
		kvRow("Phase", s.ap.rt.Phase().String()),
		kvRow("Status", orDash(acc.Status)),
	}
	if acc.Response != "" {
		content = append(content, card("Live response", "provisional", wrappedText(acc.Response)))
	}
	if len(acc.Activities) > 0 {
		rows := make([]gui.View, 0, len(acc.Activities))
		for i := len(acc.Activities) - 1; i >= 0 && len(rows) < 8; i-- {
			rows = append(rows, gui.Text(gui.TextCfg{Text: "· " + acc.Activities[i], TextStyle: gui.CurrentTheme().TextStyleCaption, Mode: gui.TextModeWrap}))
		}
		content = append(content, card("Recent activity", "", rows...))
	}
	return gui.Column(gui.ContainerCfg{
		ID:      absID("inspector"),
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingPx(6),
		Padding: gui.PadAll(8),
		Content: content,
	})
}

// activityDrawerView is the bottom drawer: the bounded activity ring
// captions (newest first, capped to the same bound the accumulator uses).
func (s *shellState) activityDrawerView() gui.View {
	acc := s.ap.rt.Stream()
	var rows []gui.View
	if len(acc.Activities) == 0 {
		rows = []gui.View{gui.Text(gui.TextCfg{Text: "Activity — no live run", TextStyle: gui.CurrentTheme().TextStyleCaption})}
	} else {
		rows = make([]gui.View, 0, 6)
		for i := len(acc.Activities) - 1; i >= 0 && len(rows) < 6; i-- {
			rows = append(rows, gui.Text(gui.TextCfg{Text: "· " + acc.Activities[i], TextStyle: gui.CurrentTheme().TextStyleCaption, Mode: gui.TextModeWrap}))
		}
	}
	return gui.Column(gui.ContainerCfg{
		ID:      absID("activity-drawer"),
		Sizing:  gui.FillFill,
		Spacing: gui.SpacingPx(2),
		Padding: gui.PadAll(4),
		Content: rows,
	})
}

// subPanelView renders an open utility surface with a Back action.
func (s *shellState) subPanelView(id string) gui.View {
	var label string
	var build func(*shellState) gui.View
	for _, p := range subPanels {
		if p.id == id {
			label, build = p.label, p.build
			break
		}
	}
	if build == nil {
		return EmptySurface("?", "Unknown panel", "The requested utility surface does not exist.")
	}
	return gui.Column(gui.ContainerCfg{
		Sizing: gui.FillFill,
		Content: []gui.View{
			gui.Row(gui.ContainerCfg{
				Sizing: gui.FillFit,
				Content: []gui.View{
					gui.Button(gui.ButtonCfg{
						ID:      absID("sub-back"),
						Label:   "← Back",
						OnClick: func(gui.EventCtx) { s.show(s.activeView()) },
					}),
					gui.Text(gui.TextCfg{Text: label, TextStyle: gui.CurrentTheme().TextStyleTitleSmall}),
				},
			}),
			build(s),
		},
	})
}

// orDash renders empty strings as the em-dash placeholder (honest blank).
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// keyboardShortcuts wires desktop keyboard navigation: Ctrl+1..9 switch
// primary views (the number row exactly covers the nine-entry registry),
// Ctrl+N creates a session in the current mode. Installed through the
// window's OnEvent hook (see App.Run wiring in shortcutOnEvent).
func shortcutOnEvent(s *shellState) func(*gui.Event, *gui.Window) {
	keyByIndex := []gui.KeyCode{gui.Key1, gui.Key2, gui.Key3, gui.Key4, gui.Key5, gui.Key6, gui.Key7, gui.Key8, gui.Key9}
	return func(ev *gui.Event, w *gui.Window) {
		if ev == nil || ev.Type != gui.EventKeyDown {
			return
		}
		if ev.Modifiers&gui.ModCtrl == 0 {
			return
		}
		for i, key := range keyByIndex {
			if ev.KeyCode == key && i < len(primaryViews) {
				vv := primaryViews[i]
				s.show(vv.id)
				ev.IsHandled = true
				return
			}
		}
		if ev.KeyCode == gui.KeyN {
			go func() {
				if _, err := s.ap.rt.CreateSession(s.ap.Ctx(), ""); err == nil {
					uiDo(func() { s.notifySessions() })
				}
			}()
			ev.IsHandled = true
		}
	}
}
