//go:build !headless

// Package desktop launches the native desktop application: the Go core
// (ONE authority for policy, runs, sessions, tools, approvals) serving its
// existing API surface over the secured local IPC channel, plus the native
// Qt Quick/QML host process that consumes it as a CLIENT (v1.10.1 Qt
// migration, R&D authority §6–§7).
//
// The former Wails/WebView shell is retired: no Wails import, no embedded
// browser, no WebView2/WebKit, no loopback WebSocket transport on the
// desktop path. The headless/API mode (headless build tag) keeps serving
// the documented HTTP API for CI, containers and tooling.
package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/localipc"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/platform"
)

// uiExecutableNames are the packaged Qt host binaries, resolved next to
// the core executable (the install layout ships both side by side).
var uiExecutableNames = map[string]string{
	"windows": "sheytan-ui.exe",
	"linux":   "sheytan-ui",
	"darwin":  "sheytan-ui",
}

// failBoot reports a fatal boot failure through EVERY visibility channel
// the process has: the named-step ERROR line in app.log (the catcher the
// operator greps) and a Windows message box naming the step, the error
// and the log path (the catcher the operator's eyes read — under
// -H=windowsgui the console channels are detached, which is exactly how
// run #36's installed launches closed instantly with zero words on
// screen and zero ERROR lines in the log). Exit codes and control flow
// are UNCHANGED — visibility only: every caller returns 1 right after.
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

// reportUIExit is the RUN-phase counterpart of failBoot (the v1.10.14-fixed
// run-phase catcher, D8): the boot catcher makes a failing launch loud, this
// makes a UI child that dies MID-SESSION loud. At the run-#37 tree the
// childDone arm recorded the exit code and returned silently — under
// -H=windowsgui nobody saw a thing, the run-#36 symptom one boot phase
// later. Exit codes and control flow are UNCHANGED — visibility only: a
// clean close logs ONE Info line; an abnormal exit logs a named ERROR line
// ("ui exit") carrying the child's code AND raises the platform message box
// naming the exit, the log path and the reason (the box's physical
// appearance is the operator's retest).
func reportUIExit(err error) {
	if err == nil {
		logging.Default().Info("desktop", "ui exit: clean close (exit code 0)")
		return
	}
	reason := uiExitReason(err)
	logging.Default().Error("desktop", "ui exit: the Qt host terminated abnormally (%s) — the [ui] lines above carry the host's own output", reason)
	showUIExitBox(reason)
}

// uiExitReason names the child's exit for both the log line and the box:
// an ExitError becomes "exit code N" — the operator's grep target; any
// other wait failure carries its own text.
func uiExitReason(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit code %d", ee.ExitCode())
	}
	return err.Error()
}

// showUIExitBox raises the operator-facing box for an abnormal UI death —
// the same channel failBoot uses, so the story (what died, why, where the
// log is) reaches the screen the console cannot. Best-effort: the named
// ERROR line in app.log remains the primary catcher.
func showUIExitBox(reason string) {
	logPath := ""
	if mgr := logging.Default(); mgr.Enabled() {
		logPath = filepath.Join(mgr.Dir(), "app.log")
	}
	text := fmt.Sprintf("SHEYTAN-LA's interface closed unexpectedly.\n\nReason: %s\n", reason)
	if logPath != "" {
		text += "\nThe details were written to the log:\n" + logPath
	} else {
		text += "\nThe log could not be located; start the app from a console to see the error."
	}
	_ = platform.MessageBox("SHEYTAN-LA — the interface closed unexpectedly", text)
}

// Run starts the SHEYTAN desktop application and blocks until it exits.
//
// Launch order is deterministic: core up → IPC endpoint bound → UI child
// spawned with the endpoint + ephemeral key → wait for either the child
// exiting or a termination signal → clean core shutdown.
func Run(cfg *config.Config) int {
	if cfg == nil {
		return failBoot("config", errors.New("missing configuration"))
	}

	// Taskbar/notification identity (no-op where the concept does not
	// exist).
	_ = platform.SetAppUserModelID(config.AppUserModelID)

	srv, err := api.New(cfg)
	if err != nil {
		return failBoot("server init", err)
	}
	// v1.10.14-fixed (D8): the core shutdown is VISIBLE. Close returns
	// nothing (internal/api/lifecycle.go) — the run-#38 defect captured
	// it as a value and killed the compile, so the catcher shipped as
	// dead code. The named Info line after the call is the record.
	defer func() {
		srv.Close()
		logging.Default().Info("desktop", "server closed")
	}()

	if err := srv.EnsureSetup(); err != nil {
		return failBoot("setup", err)
	}

	// The ONE local channel to the Qt host. A transport failure is a HARD,
	// VISIBLE fault: the desktop exits non-zero instead of launching a UI
	// that cannot reach the core (same failure semantics as v1.9.5).
	// SHEYTAN_IPC_KEY presets the ephemeral key (acceptance/dev harnesses
	// only — the production launcher generates one and never logs it).
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
	// v1.10.14-fixed (D8): the IPC stop is VISIBLE. Stop returns
	// nothing (internal/localipc/serve.go) — the run-#38 defect
	// captured it as a value and killed the compile. The named Info
	// line after the call is the record.
	defer func() {
		ipc.Stop()
		logging.Default().Info("desktop", "ipc stopped")
	}()

	uiPath, err := uiExecutable()
	if err != nil {
		// THE run-#36 installed path: the installer shipped only the
		// core, so this resolution failed on every installed launch
		// and the message died on a detached stdout. The named-step
		// ERROR line + the message box now carry it.
		return failBoot("ui resolve", err)
	}

	// v1.10.14-fixed (D8): the banner and endpoint prints are invisible
	// under -H=windowsgui — each console line gains its paired log line.
	logging.Default().Info("desktop", "%s v%s (Qt desktop)", config.AppName, config.AppVersion)
	fmt.Printf("%s v%s (Qt desktop)\n", config.AppName, config.AppVersion)
	logging.Default().Info("desktop", "core endpoint: %s", ipc.Endpoint())
	fmt.Printf("  core endpoint: %s\n", ipc.Endpoint())

	// SHEYTAN_UI_EXECUTABLE=none keeps the core serving WITHOUT spawning a
	// UI (acceptance harnesses that host the QML in-process). A refusal to
	// spawn is still fatal on every normal launch — only the explicit
	// "none" sentinel disables the child.
	if uiPath == "none" {
		<-make(chan struct{}) // block: serve until terminated
		return 0
	}

	cmd := exec.Command(uiPath)
	cmd.Env = append(os.Environ(),
		localipc.EnvEndpoint+"="+ipc.Endpoint(),
		localipc.EnvKey+"="+key,
	)
	// v1.10.14: the UI's stdout/stderr flow through the log adapter
	// (tagged "ui") into app.log — NOT into the core's os.Stdout/
	// os.Stderr anymore. The old comment claimed they "flow into the
	// core's log streams"; under -H=windowsgui they flowed nowhere,
	// and every Qt host warning was lost with the console.
	uiStdout := newUILogWriter(false)
	uiStderr := newUILogWriter(true)
	cmd.Stdout = uiStdout
	cmd.Stderr = uiStderr

	if err := cmd.Start(); err != nil {
		return failBoot("ui spawn", err)
	}

	// Termination signals: stop the child, then the core, then exit.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	childDone := make(chan error, 1)
	go func() {
		childDone <- cmd.Wait()
	}()

	exitCode := 0
	select {
	case err := <-childDone:
		// v1.10.14-fixed (D8): the exit is REPORTED — a clean close logs
		// ONE Info line; an abnormal child death logs the named ERROR line
		// and raises the operator's box. The exit code and the control
		// flow below are UNCHANGED.
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exitCode = ee.ExitCode()
			} else {
				return failBoot("ui wait", err)
			}
		}
		reportUIExit(err)
	case sig := <-sigCh:
		logging.Default().Info("desktop", "signal %v — stopping ui and core", sig)
		fmt.Println("desktop: signal", sig, "— stopping ui and core")
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-childDone:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-childDone
		}
		exitCode = 0
	}

	// Flush the UI streams' final partial lines before the sinks close.
	uiStdout.Flush()
	uiStderr.Flush()

	return exitCode
}

// uiExecutable resolves the Qt host to launch. v1.10.3 (R13) the EXPLICIT
// SHEYTAN_UI_EXECUTABLE override wins over a packaged UI next to the core —
// the v1.10.2 next-to-core-first order silently ignored an explicit
// override whenever any UI sat beside the core, contradicting the smoke's
// documented resolution order (override → staged → repo tree). The "none"
// sentinel is handled by the caller (no-spawn serving mode).
func uiExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve core executable: %w", err)
	}

	if alt := os.Getenv("SHEYTAN_UI_EXECUTABLE"); alt != "" {
		if alt == "none" {
			return "none", nil
		}
		if st, statErr := os.Stat(alt); statErr == nil && !st.IsDir() {
			return alt, nil
		}
		return "", fmt.Errorf("SHEYTAN_UI_EXECUTABLE=%s does not exist", alt)
	}

	name := uiExecutableNames[runtime.GOOS]
	if name == "" {
		return "", fmt.Errorf("no Qt host binary name for %s", runtime.GOOS)
	}

	path := filepath.Join(filepath.Dir(self), name)
	if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
		return path, nil
	}

	return "", fmt.Errorf("Qt host %s not found next to the core (%s) — set SHEYTAN_UI_EXECUTABLE for development runs", name, filepath.Dir(self))
}
