//go:build !headless

// App — the ONE Fyne application: wiring, window lifecycle, keyboard
// navigation and the responsive shell. Desktop widths show a navigation
// rail; compact widths collapse to a top tab strip. One design system
// across form factors.
package fyneui

import (
	"context"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/startup"
)

// AppID is the desktop identity (taskbar, settings storage).
const AppID = "la.sheytan.localagent"

// App carries the window, the runtime and the view registry.
type App struct {
	a       fyne.App
	w       fyne.Window
	client  *CoreClient
	rt      *Runtime
	shell   *shell
	ws      *wsHub
	cancel  context.CancelFunc
	rootCtx context.Context
	coord   *shutdownCoordinator

	// Shell-owned conversation view instances (Phase 2 wiring/test seam;
	// set by the view builders, never a second authority).
	chat  *chatView
	agent *agentView
}

// NewApp builds the product application over an existing core client
// (the caller owns the core lifecycle: api.New → EnsureSetup → localipc).
func NewApp(a fyne.App, client *CoreClient) *App {
	return &App{a: a, client: client, rt: NewRuntime(client), ws: newWSHub()}
}

// NewDesktopApp is the production entry: a real app with metadata.
func NewDesktopApp(client *CoreClient) *App {
	a := app.NewWithID(AppID)
	a.Settings().SetTheme(NewTheme())
	return NewApp(a, client)
}

// Run builds the window, attaches the initial state, and blocks until the
// window closes. The returned int is the process exit code.
//
// v1.11.1 lifecycle contract (run 43008361835 §F3/§5.2): every supported
// exit path — the user's window close, the OS signal, the programmatic
// Close() — routes through the ONE idempotent shutdown coordinator via the
// close interceptor; teardown is bounded and never runs on the UI goroutine.
func (ap *App) Run() int {
	ap.rootCtx, ap.cancel = context.WithCancel(context.Background())
	ap.coord = newShutdownCoordinator(ap, shutdownDeadline)

	su := startup.Default()
	su.StartPhase(startup.PhaseShellBuild)
	ap.w = ap.a.NewWindow(config.AppName)
	ap.w.SetIcon(appIcon())
	ap.shell = newShell(ap)
	ap.w.SetContent(ap.shell.root())
	su.EndPhase(startup.PhaseShellBuild, nil)
	ap.w.Resize(fyne.NewSize(1080, 720))
	ap.w.SetMaster()

	// The ONE close funnel: the user's X-close runs the interceptor on
	// the UI goroutine — it only forwards to the coordinator and never
	// blocks. Programmatic Window.Close() (the coordinator's final step)
	// bypasses the interceptor in Fyne v2.8.2, so the funnel cannot
	// re-enter itself.
	ap.w.SetCloseIntercept(func() { ap.coord.requestClose() })
	// Post-close bookkeeping ONLY (runs inside Window.Close while the
	// window still exists); never the teardown owner.
	ap.w.SetOnClosed(func() { ap.coord.markClosed() })

	// The initial primary view renders at boot — the shell must never
	// present a blank primary surface before the first navigation click
	// (v1.11.1 recovery: the lazy-build shell rework lost the initial
	// show call; the Xvfb smoke never clicks navigation and the detached
	// view tests cannot see the assembled shell, so the blank boot
	// surface shipped unnoticed).
	su.StartPhase(startup.PhaseFirstView)
	ap.showInitialView()
	su.EndPhase(startup.PhaseFirstView, nil)

	// Keyboard navigation: Ctrl+1..7 switch primary views, Ctrl+N creates
	// a session in the current mode.
	ap.registerShortcuts()

	// A close requested before the event loop starts (early signal) is
	// honored here: no window is shown, teardown already ran.
	if ap.coord.closing() {
		return 0
	}

	// The bootstrap and the runtime→UI callbacks are installed when the
	// app is ACTUALLY running (lifecycle hook): fyne.Do before the event
	// loop starts would drop the call with a loud error. Everything after
	// this hook is safely marshalled onto the UI loop.
	events := ap.a.Lifecycle().SetOnStarted
	events(func() {
		// First interactive UI state: the event loop is running and
		// the shell is live — recorded as elapsed-since-entry.
		su.RecordElapsed(startup.PhaseWindowReady, nil)
		ap.installRuntimeCallbacks()
		go ap.bootstrap()
		if resizeSmokeEnabled() {
			go ap.runResizeSmoke()
		}
	})

	ap.w.ShowAndRun()
	ap.rt.Shutdown()
	ap.stopWorkspace() // release the loopback listener, if it was opened
	ap.cancel()
	return 0
}

// installRuntimeCallbacks wires the runtime's background notifications
// onto the UI loop for every view the shell has built.
func (ap *App) installRuntimeCallbacks() {
	ap.rt.SetCallbacks(
		func() { uiDo(func() { ap.shell.notifyStream() }) },
		func() { uiDo(func() { ap.shell.notifySettled() }) },
		func() { uiDo(func() { ap.shell.notifySessions() }) },
	)
}

// bootstrap loads the first session list and reports connection status.
//
// Threading contract (Fyne v2.8.2): bootstrap runs OFF the UI goroutine
// (it is spawned from OnStarted), so every widget/state mutation below is
// marshalled through uiDo (fyne.Do). The Xvfb smoke caught the un-marshalled
// status writes as "Error in Fyne call thread" — the next Fyne release
// removes that unsafe path entirely.
//
// v1.11.1 (run 43008361835 §F2): each bootstrap phase is measured with the
// monotonic startup recorder — cold-start evidence exists even when no data
// root is ever created.
func (ap *App) bootstrap() {
	ctx := ap.rootCtx
	su := startup.Default()
	su.StartPhase(startup.PhaseCoreHandshake)
	if err := ap.client.Handshake(ctx); err != nil {
		su.EndPhase(startup.PhaseCoreHandshake, err)
		uiDo(func() { ap.shell.setBootError(err) })
		return
	}
	su.EndPhase(startup.PhaseCoreHandshake, nil)
	version := ap.client.Version()
	su.StartPhase(startup.PhaseSessionsRefresh)
	if err := ap.rt.RefreshSessions(ctx); err != nil {
		su.EndPhase(startup.PhaseSessionsRefresh, err)
		uiDo(func() {
			ap.shell.coreVersion = version
			ap.shell.setBootError(err)
		})
		return
	}
	su.EndPhase(startup.PhaseSessionsRefresh, nil)
	uiDo(func() {
		ap.shell.coreVersion = version
		ap.shell.setConnected()
	})
	su.RecordElapsed(startup.PhaseBackgroundReady, nil)
}

// Client exposes the core client (views use it).
func (ap *App) Client() *CoreClient { return ap.client }

// Runtime exposes the presentation runtime.
func (ap *App) Runtime() *Runtime { return ap.rt }

// Window exposes the window (tests use it).
func (ap *App) Window() fyne.Window { return ap.w }

// Workspace exposes the presentation hub (views/tests register blocks;
// the hub owns the one bounded presentation store of this process).
func (ap *App) Workspace() *wsHub { return ap.ws }

// Ctx is the app-lifetime context. Before Run() arms it (tests,
// composition probes) the nil root falls back to Background — callers
// never hand a nil Context to the HTTP layer.
func (ap *App) Ctx() context.Context {
	if ap.rootCtx != nil {
		return ap.rootCtx
	}
	return context.Background()
}
