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

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// AppID is the desktop identity (taskbar, settings storage).
const AppID = "la.sheytan.localagent"

// App carries the window, the runtime and the view registry.
type App struct {
	a        fyne.App
	w        fyne.Window
	client   *CoreClient
	rt       *Runtime
	shell    *shell
	cancel   context.CancelFunc
	rootCtx  context.Context
	prepared bool // window built+shown by Prepare (boot-visibility contract)
}

// NewApp builds the product application over an existing core client
// (the caller owns the core lifecycle: api.New → EnsureSetup → localipc).
func NewApp(a fyne.App, client *CoreClient) *App {
	return &App{a: a, client: client, rt: NewRuntime(client)}
}

// NewDesktopApp is the production entry: a real app with metadata.
func NewDesktopApp(client *CoreClient) *App {
	a := app.NewWithID(AppID)
	a.Settings().SetTheme(NewTheme())
	return NewApp(a, client)
}

// Run builds the window, attaches the initial state, and blocks until the
// window closes. The returned int is the process exit code.
func (ap *App) Run() int {
	ap.rootCtx, ap.cancel = context.WithCancel(context.Background())
	if ap.prepared {
		// Prepare() already built and SHOWED the window (boot-visibility
		// contract): the process owns a real top-level window from the
		// first seconds of launch, even while slower core steps (setup,
		// maintenance) complete. Run the event loop directly.
		ap.w.ShowAndRun()
		ap.rt.Shutdown()
		ap.cancel()
		return 0
	}
	ap.w = ap.a.NewWindow(config.AppName)
	ap.w.SetIcon(appIcon())
	ap.shell = newShell(ap)
	ap.w.SetContent(ap.shell.root())
	ap.w.Resize(fyne.NewSize(1080, 720))
	ap.w.SetMaster()

	// Keyboard navigation: Ctrl+1..8 switch primary views, Ctrl+N creates
	// a session in the current mode.
	ap.registerShortcuts()

	// The bootstrap and the runtime→UI callbacks are installed when the
	// app is ACTUALLY running (lifecycle hook): fyne.Do before the event
	// loop starts would drop the call with a loud error. Everything after
	// this hook is safely marshalled onto the UI loop.
	ap.installOnStarted()

	ap.w.ShowAndRun()
	ap.rt.Shutdown()
	ap.cancel()
	return 0
}

// installOnStarted registers the OnStarted lifecycle hook (bootstrap +
// runtime callbacks). Split from Run so the Prepare() boot path registers
// it before the event loop starts too.
func (ap *App) installOnStarted() {
	events := ap.a.Lifecycle().SetOnStarted
	events(func() {
		ap.installRuntimeCallbacks()
		go ap.bootstrap()
	})
}

// Prepare builds and SHOWS the window without starting the event loop
// (v1.11.0 boot-visibility contract, run #55 class: the Windows smoke
// observed a live process with no window because every core boot step
// ran BEFORE the window existed — any slow step kept the process
// windowless for its whole duration). Prepare inverts that order: the
// window exists and is shown FIRST, then the caller finishes booting the
// core and finally hands control to Run(). Prepare is idempotent-guarded
// (a second call is a clean no-op) and safe to call only before Run().
func (ap *App) Prepare() error {
	if ap.prepared {
		return nil
	}
	ap.rootCtx, ap.cancel = context.WithCancel(context.Background())
	ap.w = ap.a.NewWindow(config.AppName)
	ap.w.SetIcon(appIcon())
	ap.shell = newShell(ap)
	ap.w.SetContent(ap.shell.root())
	ap.w.Resize(fyne.NewSize(1080, 720))
	ap.w.SetMaster()
	ap.registerShortcuts()
	ap.installOnStarted()
	// Show (not ShowAndRun): the window becomes a real top-level window
	// now; it paints as soon as Run() starts the event loop.
	ap.w.Show()
	ap.prepared = true
	return nil
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
func (ap *App) bootstrap() {
	ctx := ap.rootCtx
	if err := ap.client.Handshake(ctx); err != nil {
		uiDo(func() { ap.shell.setBootError(err) })
		return
	}
	version := ap.client.Version()
	if err := ap.rt.RefreshSessions(ctx); err != nil {
		uiDo(func() {
			ap.shell.coreVersion = version
			ap.shell.setBootError(err)
		})
		return
	}
	uiDo(func() {
		ap.shell.coreVersion = version
		ap.shell.setConnected()
	})
}

// Client exposes the core client (views use it).
func (ap *App) Client() *CoreClient { return ap.client }

// Runtime exposes the presentation runtime.
func (ap *App) Runtime() *Runtime { return ap.rt }

// Window exposes the window (tests use it).
func (ap *App) Window() fyne.Window { return ap.w }

// Ctx is the app-lifetime context.
func (ap *App) Ctx() context.Context { return ap.rootCtx }

// Close closes the window (programmatic shutdown path).
//
// Threading contract (Fyne v2.8.2): Close may be called from ANY goroutine
// — the SIGTERM handler in internal/desktop, tests, future programmatic
// exits — so the window close is marshalled onto the UI loop (fyne.Do via
// uiDo). The Xvfb smoke traced the remaining "Error in Fyne call thread"
// to the signal path calling this from its own goroutine.
func (ap *App) Close() {
	if ap.w != nil {
		uiDo(ap.w.Close)
	}
}
