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
	a       fyne.App
	w       fyne.Window
	client  *CoreClient
	rt      *Runtime
	shell   *shell
	cancel  context.CancelFunc
	rootCtx context.Context
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
	ap.w = ap.a.NewWindow(config.AppName)
	ap.w.SetIcon(appIcon())
	ap.shell = newShell(ap)
	ap.w.SetContent(ap.shell.root())
	ap.w.Resize(fyne.NewSize(1080, 720))
	ap.w.SetMaster()

	// Keyboard navigation: Ctrl+1..7 switch primary views, Ctrl+N creates
	// a session in the current mode.
	ap.registerShortcuts()

	// The bootstrap and the runtime→UI callbacks are installed when the
	// app is ACTUALLY running (lifecycle hook): fyne.Do before the event
	// loop starts would drop the call with a loud error. Everything after
	// this hook is safely marshalled onto the UI loop.
	events := ap.a.Lifecycle().SetOnStarted
	events(func() {
		ap.installRuntimeCallbacks()
		go ap.bootstrap()
	})

	ap.w.ShowAndRun()
	ap.rt.Shutdown()
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
func (ap *App) bootstrap() {
	ctx := ap.rootCtx
	if err := ap.client.Handshake(ctx); err != nil {
		ap.shell.setBootError(err)
		return
	}
	ap.shell.coreVersion = ap.client.Version()
	if err := ap.rt.RefreshSessions(ctx); err != nil {
		ap.shell.setBootError(err)
		return
	}
	ap.shell.setConnected()
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
func (ap *App) Close() {
	if ap.w != nil {
		ap.w.Close()
	}
}
