//go:build !headless

// Package desktop launches the native desktop application: the Go core
// (ONE authority for policy, runs, sessions, tools, approvals) serving its
// existing API surface over the secured local IPC channel, plus the ONE
// Fyne UI running in-process as its client (v1.11.0 one-Go-UI migration).
//
// The former Qt Quick/QML host child process is retired, and with it the
// React/WebView shell before that. The UI is a Fyne window built from the
// same Go codebase that ships on every platform; it connects to the core
// through the same authenticated local endpoint the Qt host used, so the
// transport identity boundary (uid peer check / per-user DACL + ephemeral
// key handshake) is preserved exactly.
//
// The headless/API mode (headless build tag) keeps serving the documented
// HTTP API for CI, containers and tooling with no GUI dependency.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/localipc"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/platform"
	fyneui "github.com/Parsaetak/SHEYTAN-local-agent/ui/fyne"
)

// fyneUIPackagePath pins the ONE UI package the launcher attaches (the
// architecture test asserts it).
const fyneUIPackagePath = "github.com/Parsaetak/SHEYTAN-local-agent/ui/fyne"

// failBoot reports a fatal boot failure through EVERY visibility channel
// the process has: the named-step ERROR line in app.log (the catcher the
// operator greps) and a Windows message box naming the step, the error
// and the log path. Exit codes and control flow are UNCHANGED —
// visibility only: every caller returns 1 right after.
func failBoot(step string, err error) int {
	logging.Default().Error("desktop", "boot failed at %s: %v", step, err)

	logPath := ""
	if mgr := logging.Default(); mgr.Enabled() {
		logPath = filepath.Join(mgr.Dir(), "app.log")
	}
	text := fmt.Sprintf("SHEYTAN-LA failed to start.\n\nStep: %s\nError: %v\n", step, err)
	if logPath != "" {
		text += "\nThe details were written to the log:\n" + logPath
	} else {
		text += "\nThe log could not be located; start the app from a console to see the error."
	}
	_ = platform.MessageBox("SHEYTAN-LA — startup failed", text)
	return 1
}

// Run starts the SHEYTAN desktop application and blocks until the Fyne
// window closes.
//
// Launch order is deterministic (v1.11.0 boot-visibility contract):
// core up → IPC endpoint bound → WINDOW BUILT AND SHOWN → remaining core
// setup (EnsureSetup) → event loop until the window closes → clean core
// shutdown.
//
// The window comes BEFORE EnsureSetup deliberately (run #55 class, CI
// run 40410335100: the Windows smoke observed a live process with no
// window for the whole 60 s budget). The previous order ran every core
// boot step before the window existed, so ANY slow step — setup,
// maintenance, a cold disk — kept the process windowless for its entire
// duration. Now the process owns a real top-level window within seconds
// of launch regardless of core speed: the fix is the boot order, not a
// longer timeout and not a liveness-only check.
func Run(cfg *config.Config) int {
	if cfg == nil {
		return failBoot("config", errors.New("missing configuration"))
	}

	// Taskbar/notification identity (no-op where the concept does not
	// exist).
	_ = platform.SetAppUserModelID(config.AppUserModelID)

	bootAt := time.Now()
	step := func(name string) {
		logging.Default().Info("desktop", "boot step %s done (elapsed %s)", name, time.Since(bootAt).Round(time.Millisecond))
	}

	srv, err := api.New(cfg)
	if err != nil {
		return failBoot("server init", err)
	}
	// The core shutdown is VISIBLE. Close returns nothing
	// (internal/api/lifecycle.go); the named Info line after the call is
	// the record.
	defer func() {
		srv.Close()
		logging.Default().Info("desktop", "server closed")
	}()
	step("core init")

	// The ONE local channel to the UI client. A transport failure is a
	// HARD, VISIBLE fault: the desktop exits non-zero instead of launching
	// a UI that cannot reach the core. SHEYTAN_IPC_KEY presets the
	// ephemeral key (acceptance/dev harnesses only — the production
	// launcher generates one and never logs it).
	key := os.Getenv(localipc.EnvKey)
	if key == "" {
		key, err = localipc.NewKey()
		if err != nil {
			return failBoot("ipc key", err)
		}
	}

	ipc, err := localipc.Serve(srv, cfg, key)
	if err != nil {
		return failBoot("ipc serve", err)
	}
	defer func() {
		ipc.Stop()
		logging.Default().Info("desktop", "ipc stopped")
	}()
	step("ipc bound")

	logging.Default().Info("desktop", "%s v%s (Fyne desktop, one Go UI)", config.AppName, config.AppVersion)
	fmt.Printf("%s v%s (Fyne desktop, one Go UI)\n", config.AppName, config.AppVersion)
	logging.Default().Info("desktop", "core endpoint: %s", ipc.Endpoint())
	fmt.Printf("  core endpoint: %s\n", ipc.Endpoint())

	// The Fyne app is the ONLY UI: it connects to the same authenticated
	// endpoint in-process. No child binary, no separate UI executable —
	// the one-Go-UI program removed the second implementation entirely.
	client := fyneui.NewCoreClient(ipc.Endpoint(), key)
	app := fyneui.NewDesktopApp(client)

	// WINDOW FIRST (see the run #55 narrative above): build and show the
	// real top-level window now, then finish the core boot, then start
	// the event loop. A Prepare failure is a hard, visible fault.
	if err := app.Prepare(); err != nil {
		return failBoot("window", err)
	}
	step("window shown")

	// Core setup AFTER the window exists: first-run setup, installer
	// state, startup maintenance gate and prewarm happen with a visible
	// window on screen. A failure here is still fatal (message box +
	// exit 1) — visibility changed, control flow did not.
	if err := srv.EnsureSetup(); err != nil {
		return failBoot("setup", err)
	}
	step("core setup")

	// Termination signals close the window; the deferred shutdowns above
	// then stop the IPC and the core in order.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logging.Default().Info("desktop", "signal %v — closing the window and stopping the core", sig)
		fmt.Println("desktop: signal", sig, "— stopping")
		app.Close()
	}()

	_ = context.Background // context reserved for the app-lifetime scope
	code := app.Run()
	logging.Default().Info("desktop", "ui exit: clean close (exit code %d)", code)
	return code
}
