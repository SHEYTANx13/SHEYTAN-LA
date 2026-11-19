//go:build !headless

// app.go — the ONE go-gui application: wiring, window lifecycle and the
// responsive shell boot. Ported from ui/fyne/app.go with the go-gui
// equivalents:
//
//   - fyne.App.NewWindow + ShowAndRun  → gui.NewWindow + backend.Run
//   - fyne.Do                          → uiDo (QueueCommand seam, uido.go)
//   - Lifecycle().SetOnStarted         → WindowCfg.OnInit (the window is
//     live when OnInit runs; workers spawn from the bootstrap goroutine)
//   - SetCloseIntercept/SetOnClosed    → WindowCfg.OnCloseRequest + the
//     shutdown coordinator (single funnel, teardown off the UI thread)
package goguiui

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/localipc"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/startup"
)

//go:embed assets/logo-512.png
var logoFS embed.FS

func init() {
	b, err := logoFS.ReadFile("assets/logo-512.png")
	if err == nil {
		logoPNG = b
	}
}

// logoPNG holds the embedded product logo (set in init from logoFS).
var logoPNG []byte

// AppID is the desktop identity (taskbar, settings storage) — the same
// product identity the Fyne host ships.
const AppID = "la.sheytan.localagent"

// App carries the window, the runtime, the view registry and the theme
// service state.
type App struct {
	w       *gui.Window
	client  *CoreClient
	rt      *Runtime
	ws      *wsHub
	themes  *themeService
	cancel  context.CancelFunc
	rootCtx context.Context

	// shell holds the responsive navigation + connection surface state
	// (the window's typed State value; the view functions read it).
	shell *shellState

	// dockStore persists the docking layout (per-pane sizes) under the
	// app data dir; tests inject a temp path.
	dockStore dockStore

	// title is the window title (product name, exact ASCII).
	title string

	// bootMu/bootInFlight serialize the bootstrap/retry path (v1.11.1
	// contract kept): manual Retry clicks can never stack bootstraps.
	bootMu       sync.Mutex
	bootInFlight atomic.Bool

	// windowReady mirrors the Fyne OnStarted hook: QueueCommand-marshalled
	// callbacks are only reliable once the event loop runs.
	windowReady atomic.Bool

	// closeRequested is set by the close interceptor.
	closeRequested atomic.Bool
}

// transportName names the local IPC transport for the structured
// handshake records (windows-named-pipe / unix-domain-socket). The
// endpoint path itself is NOT logged; the handshake key NEVER appears in
// any log line.
func transportName() string {
	if runtime.GOOS == "windows" {
		return "windows-named-pipe"
	}
	return "unix-domain-socket"
}

// NewApp builds the product application over an existing core client
// (the caller owns the core lifecycle: api.New → EnsureSetup → localipc).
func NewApp(client *CoreClient) *App {
	ap := &App{
		client:    client,
		rt:        NewRuntime(client),
		ws:        newWSHub(),
		themes:    newThemeService(),
		title:     config.AppName,
		dockStore: defaultDockStore(),
	}
	ap.shell = newShell(ap)
	return ap
}

// NewDesktopApp is the production entry: a real app with metadata. The
// theme service (initialized in NewApp) owns the theme — NewDesktopApp no
// longer sets it directly, so boot and every later switch share ONE path.
func NewDesktopApp(client *CoreClient) *App {
	return NewApp(client)
}

// Run builds the window, attaches the shell, and blocks until the window
// closes. The returned int is the process exit code.
//
// Boot sequence (mirrors ui/fyne/app.go): shell build → initial view
// before the first frame → bootstrap handshake OFF the UI goroutine →
// backend.Run blocks until close → bounded teardown.
func (ap *App) Run() int {
	ap.rootCtx, ap.cancel = context.WithCancel(context.Background())

	su := startup.Default()
	su.StartPhase(startup.PhaseShellBuild)

	// The window state IS the shell: go-gui regenerates the view tree
	// from it every frame.
	w := gui.NewWindow(gui.WindowCfg{
		State:     ap.shell,
		Title:     ap.title,
		Width:     1080,
		Height:    720,
		MinWidth:  360,
		MinHeight: 320,
		IconPNG:   brandIconPNG(),
		OnInit: func(w *gui.Window) {
			w.SetView(ap.shell.rootView)
			// First interactive UI state: the view loop is live.
			su.RecordElapsed(startup.PhaseWindowReady, nil)
			ap.windowReady.Store(true)
			uiDoWindow.Store(w)
			ap.installRuntimeCallbacks()
			startDockPersist(ap)
			go ap.bootstrap()
		},
		OnEvent: shortcutOnEvent(ap.shell),
		OnCloseRequest: func(w *gui.Window) {
			// ONE close funnel: the coordinator decides; Close() proceeds.
			ap.closeRequested.Store(true)
			w.Close()
		},
	})
	ap.w = w
	// Restore persisted docking sizes before the first frame.
	if state, ok := ap.dockStore.Load(); ok {
		ap.shell.dock = state
	}
	ap.shell.showInitial()
	su.EndPhase(startup.PhaseShellBuild, nil)

	backend.Run(w)

	ap.rt.Shutdown()
	ap.stopWorkspace()
	ap.cancel()
	return 0
}

// Window exposes the window (tests use it; nil before Run).
func (ap *App) Window() *gui.Window { return ap.w }

// ShellState exposes the window state (the mobile host boots the same
// shell without the desktop Run loop; it owns the backend lifecycle).
func (ap *App) ShellState() any { return ap.shell }

// ShellRoot exposes the root view function (mobile host wiring).
func (ap *App) ShellRoot() func(*gui.Window) gui.View { return ap.shell.rootView }

// Client exposes the core client (views use it).
func (ap *App) Client() *CoreClient { return ap.client }

// Runtime exposes the presentation runtime.
func (ap *App) Runtime() *Runtime { return ap.rt }

// Workspace exposes the presentation hub.
func (ap *App) Workspace() *wsHub { return ap.ws }

// Themes exposes the theme service (the Engines & Appearance surface and
// tests drive live preview / save / cancel through it).
func (ap *App) Themes() *themeService { return ap.themes }

// configResolveRoot is the package-local alias for the canonical data
// root resolution (theme service + dock store use it).
func configResolveRoot() (string, bool, string) { return config.ResolveRoot() }

// Ctx is the app-lifetime context. Before Run() arms it (tests,
// composition probes) the nil root falls back to Background.
func (ap *App) Ctx() context.Context {
	if ap.rootCtx != nil {
		return ap.rootCtx
	}
	return context.Background()
}

// installRuntimeCallbacks wires the runtime's background notifications
// through the uiDo seam and registers the view dispatches (chat + agent
// stream/settlement callbacks — the ONE dispatch, installed once).
func (ap *App) installRuntimeCallbacks() {
	ap.rt.SetCallbacks(
		func() { uiDo(func() { ap.shell.notifyStream() }) },
		func() { uiDo(func() { ap.shell.notifySettled() }) },
		func() { uiDo(func() { ap.shell.notifySessions() }) },
	)
	wireChatCallbacks(ap.shell)
	wireAgentCallbacks(ap.shell)
}

// bootstrap loads the first session list and reports connection status.
//
// Threading contract: bootstrap runs OFF the UI goroutine; every state
// mutation below is marshalled through uiDo.
func (ap *App) bootstrap() {
	ctx := ap.Ctx()
	su := startup.Default()

	if ap.bootInFlight.Swap(true) {
		return // a bootstrap is already running — never stack them
	}
	defer ap.bootInFlight.Store(false)

	uiDo(func() { ap.shell.setConnecting() })

	su.StartPhase(startup.PhaseCoreHandshake)
	if err := ap.client.Handshake(ctx); err != nil {
		su.EndPhase(startup.PhaseCoreHandshake, err)
		logging.Default().Error("ipc", "core handshake failed transport=%s error=%v", transportName(), err)
		uiDo(func() { ap.shell.setBootError(err) })
		return
	}
	su.EndPhase(startup.PhaseCoreHandshake, nil)
	version := ap.client.Version()
	logging.Default().Info("ipc", "core handshake successful protocol=%s protocolVersion=%d coreVersion=%s transport=%s",
		localipc.ProtocolName, localipc.ProtocolVersion, version, transportName())

	su.StartPhase(startup.PhaseSessionsRefresh)
	if err := ap.rt.RefreshSessions(ctx); err != nil {
		su.EndPhase(startup.PhaseSessionsRefresh, err)
		logging.Default().Error("ipc", "first session refresh failed after a successful handshake operation=sessions error=%v", err)
		uiDo(func() {
			ap.shell.coreVersion = version
			ap.shell.setBootError(err)
		})
		return
	}
	su.EndPhase(startup.PhaseSessionsRefresh, nil)
	logging.Default().Info("ipc", "first session refresh succeeded (connected) transport=%s", transportName())
	uiDo(func() {
		ap.shell.coreVersion = version
		ap.shell.setConnected()
	})
	su.RecordElapsed(startup.PhaseBackgroundReady, nil)
}

// retryBootstrap re-runs the connection bootstrap (the shell's Retry
// action). Single-flight: concurrent clicks coalesce into one attempt.
func (ap *App) retryBootstrap() {
	if ap.bootInFlight.Load() {
		return
	}
	logging.Default().Info("ipc", "manual connection retry requested transport=%s", transportName())
	go ap.bootstrap()
}

// openURI hands a URL to the OS default handler through the native
// platform backend when one is attached (the go-gui analogue of the Fyne
// app's OpenURL). Tests and detached probes get a typed unsupported
// error — never a fake success.
func openURI(raw string) error {
	w, _ := uiDoWindow.Load().(*gui.Window)
	if w == nil {
		return fmt.Errorf("openURI: no window attached")
	}
	np := w.NativePlatformBackend()
	if np == nil {
		return errUnsupportedOpen
	}
	return np.OpenURI(raw)
}

// errUnsupportedOpen is the typed failure for platforms without a URL
// handler (headless tests, desktops without a backend).
var errUnsupportedOpen = errors.New("openURI: no native platform handler on this backend")

// brandIconPNG returns the embedded product logo bytes (the same asset
// gen-syso compiles into the Windows version resource — one brand source).
func brandIconPNG() []byte { return logoPNG }
