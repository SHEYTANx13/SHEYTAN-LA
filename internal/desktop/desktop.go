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

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/api"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/localipc"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/platform"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/startup"
	fyneui "github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne"
)

// fyneUIPackagePath pins the ONE UI package the launcher attaches (the
// architecture test asserts it).
const fyneUIPackagePath = "github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne"

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
// Launch order is deterministic: core up → IPC endpoint bound → Fyne UI
// attached as the ONE in-process client → run until the window closes →
// clean core shutdown.
func Run(cfg *config.Config) int {
	if cfg == nil {
		return failBoot("config", errors.New("missing configuration"))
	}

	// v1.11.1 (run 43008361835 §F2): monotonic phase telemetry for the
	// whole desktop launch. The summary is emitted to stderr at the END
	// of Run (every path returns through here), so a pristine smoke
	// copy that never creates a data root still leaves a complete,
	// sanitized timing record. The controlled diagnostic mode adds the
	// full JSON records on stdout.
	su := startup.Default()
	su.SetDiagnostic(startup.DiagnosticsEnabled())

	// Taskbar/notification identity (no-op where the concept does not
	// exist).
	_ = platform.SetAppUserModelID(config.AppUserModelID)

	su.StartPhase(startup.PhaseCoreInit)
	srv, err := api.New(cfg)
	if err != nil {
		su.EndPhase(startup.PhaseCoreInit, err)
		return failBoot("server init", err)
	}
	su.EndPhase(startup.PhaseCoreInit, nil)
	// The core shutdown is VISIBLE. Close returns nothing
	// (internal/api/lifecycle.go); the named Info line after the call is
	// the record. Deferred FIRST so it runs LAST: the shutdown phase
	// (UI close → IPC stop → core close) is measured and the startup
	// summary is emitted after the full teardown, in order.
	shutdownStart := time.Now()
	defer func() {
		srv.Close()
		logging.Default().Info("desktop", "server closed")
		su.RecordPhase(startup.PhaseShutdown, time.Since(shutdownStart), nil)
		su.Emit(os.Stderr)
	}()

	su.StartPhase(startup.PhaseCoreSetup)
	if err := srv.EnsureSetup(); err != nil {
		su.EndPhase(startup.PhaseCoreSetup, err)
		return failBoot("setup", err)
	}
	su.EndPhase(startup.PhaseCoreSetup, nil)

	// The ONE local channel to the UI client. A transport failure is a
	// HARD, VISIBLE fault: the desktop exits non-zero instead of launching
	// a UI that cannot reach the core. SHEYTAN_IPC_KEY presets the
	// ephemeral key (acceptance/dev harnesses only — the production
	// launcher generates one and never logs it).
	su.StartPhase(startup.PhaseIPCKey)
	key := os.Getenv(localipc.EnvKey)
	if key == "" {
		key, err = localipc.NewKey()
		if err != nil {
			su.EndPhase(startup.PhaseIPCKey, err)
			return failBoot("ipc key", err)
		}
	}
	su.EndPhase(startup.PhaseIPCKey, nil)

	su.StartPhase(startup.PhaseIPCServe)
	ipc, err := localipc.Serve(srv, cfg, key)
	if err != nil {
		su.EndPhase(startup.PhaseIPCServe, err)
		return failBoot("ipc serve", err)
	}
	su.EndPhase(startup.PhaseIPCServe, nil)
	defer func() {
		ipc.Stop()
		logging.Default().Info("desktop", "ipc stopped")
	}()

	logging.Default().Info("desktop", "%s v%s (Fyne desktop, one Go UI)", config.AppName, config.AppVersion)
	fmt.Printf("%s v%s (Fyne desktop, one Go UI)\n", config.AppName, config.AppVersion)
	logging.Default().Info("desktop", "core endpoint: %s", ipc.Endpoint())
	fmt.Printf("  core endpoint: %s\n", ipc.Endpoint())

	// The Fyne app is the ONLY UI: it connects to the same authenticated
	// endpoint the Qt host used, in-process. No child binary, no
	// SHEYTAN_UI_EXECUTABLE spawn override — the one-Go-UI program
	// removed the second implementation entirely.
	su.StartPhase(startup.PhaseFyneApp)
	client := fyneui.NewCoreClient(ipc.Endpoint(), key)
	app := fyneui.NewDesktopApp(client)
	su.EndPhase(startup.PhaseFyneApp, nil)

	// Termination signals close the window through the SAME idempotent
	// shutdown coordinator as the user's window close (one funnel:
	// interceptor, signal and programmatic paths all converge there);
	// the deferred shutdowns above then stop the IPC and the core in
	// order after the UI has closed.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logging.Default().Info("desktop", "signal %v — closing through the shutdown coordinator", sig)
		fmt.Println("desktop: signal", sig, "— stopping")
		app.Close()
	}()

	_ = context.Background // context reserved for the app-lifetime scope
	code := app.Run()
	logging.Default().Info("desktop", "ui exit: clean close (exit code %d)", code)
	return code
}
