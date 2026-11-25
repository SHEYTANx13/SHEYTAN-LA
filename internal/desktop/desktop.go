//go:build !headless

// Package desktop launches the native desktop application: the Go core
// (ONE authority for policy, runs, sessions, tools, approvals) serving its
// existing API surface over the secured local IPC channel, plus the ONE
// adaptive web shell rendered in the primary window by the typed
// web workspace host (the 2026-11-25 adaptive web UI platform decision,
// completed by the run 45634345922 update).
//
// The retired shells: the Qt Quick/QML host child process (retired
// v1.11.0), the React/WebView shell before that, and — this update — the
// Fyne toolkit shell (deleted once the web shell carried its full
// surface; the retirement is recorded in docs/retired.md). The web shell
// and the core API share ONE loopback origin through the asset handler,
// so the shell's same-origin CSP and the core's authentication boundary
// hold unchanged.
//
// The headless/API mode (headless build tag) keeps serving the documented
// HTTP API for CI, containers and tooling with no GUI dependency.
package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/api"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/brand"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/localipc"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/platform"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/startup"
)

// webShellPackagePath pins the ONE shell package the launcher renders in
// the primary window (the architecture test asserts it).
const webShellPackagePath = "github.com/SHEYTANx13/SHEYTAN-LA/internal/webshell"

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

// Run starts the SHEYTAN desktop application and blocks until the
// primary window closes.
//
// Launch order is deterministic: core up → IPC endpoint bound → the ONE
// UI shell (the adaptive web shell through the typed web workspace host)
// rendered in the primary window → run until the window closes → clean
// core shutdown.
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

	// Run 44457547289 §5: the GoGPU render service verification runs in
	// the BACKGROUND (never on the first-frame path); its observation is
	// published to the toolkit-neutral channel the UIs read. The wiring
	// stays behind the nogogpu build seam (the gogpu/goffi stack cannot
	// link beside other cgo code on Linux per the pinned goffi v0.6.8
	// documentation, goffi issue #72): the default build links the render
	// service, -tags nogogpu builds publish the honest not-wired
	// observation instead.
	startRenderVerification()

	// Run 45634345922 update: the UI shell selection. The web UI host IS
	// the shell on every desktop platform when this build carries it (the
	// webworkspace build tag - every packaged desktop build does).
	// SHEYTAN_UI=fyne names a RETIRED shell and fails boot with the typed
	// retirement error; a build without the host never silently fakes it:
	// an explicit web request fails boot with the typed build-miswire
	// error.
	mode, merr := resolveUIMode()
	if merr != nil {
		return failBoot("ui mode", merr)
	}

	if mode == uiModeWeb || (mode == uiModeAuto && webUIHostAvailable()) {
		return runWebUI(srv)
	}

	// v1.11.1: the transport KIND is logged, never the endpoint path —
	// on Windows the pipe name embeds the account name and on Unix the
	// path embeds the uid; neither belongs in a log file an operator
	// might share. The handshake record (ui bootstrap) carries the
	// protocol identity; diagnostics carry the resolved log directory.
	transport := "unix-domain-socket"
	if runtime.GOOS == "windows" {
		transport = "windows-named-pipe"
	}
	logging.Default().Info("desktop", "%s v%s (web UI host — core transport: %s)", config.AppName, brand.DisplayVersion, transport)
	fmt.Printf("%s v%s (web UI host — core transport: %s)\n", config.AppName, brand.DisplayVersion, transport)

	// Unreachable with the current mode set (auto resolves to the web
	// host when present and fails typed otherwise); the return keeps the
	// control flow exhaustive for future shell additions.
	return failBoot("ui mode", ErrWebUINotCompiled)
}
