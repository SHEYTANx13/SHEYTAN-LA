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
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/platform"
)

// uiExecutableNames are the packaged Qt host binaries, resolved next to
// the core executable (the install layout ships both side by side).
var uiExecutableNames = map[string]string{
        "windows": "sheytan-ui.exe",
        "linux":   "sheytan-ui",
        "darwin":  "sheytan-ui",
}

// uiSpawnTimeout bounds the wait for the Qt host process to attach its
// IPC handshake. The UI may take longer on a cold machine — this is only
// a liveness watchdog, not a gate on UI functionality.
const uiSpawnTimeout = 0 // disabled: the launcher waits on the child unconditionally

// Run starts the SHEYTAN desktop application and blocks until it exits.
//
// Launch order is deterministic: core up → IPC endpoint bound → UI child
// spawned with the endpoint + ephemeral key → wait for either the child
// exiting or a termination signal → clean core shutdown.
func Run(cfg *config.Config) int {
        if cfg == nil {
                fmt.Println("desktop: missing configuration")
                return 1
        }

        // Taskbar/notification identity (no-op where the concept does not
        // exist).
        _ = platform.SetAppUserModelID(config.AppUserModelID)

        srv, err := api.New(cfg)
        if err != nil {
                fmt.Println("desktop: initialize server:", err)
                return 1
        }
        defer srv.Close()

        if err := srv.EnsureSetup(); err != nil {
                fmt.Println("desktop: setup:", err)
                return 1
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
                        fmt.Println("desktop: ipc key:", err)
                        return 1
                }
        }

        ipc, err := localipc.Serve(srv, cfg, key)
        if err != nil {
                fmt.Println("desktop: local IPC unavailable:", err)
                return 1
        }
        defer ipc.Stop()

        uiPath, err := uiExecutable()
        if err != nil {
                fmt.Println("desktop:", err)
                return 1
        }

        fmt.Printf("%s v%s (Qt desktop)\n", config.AppName, config.AppVersion)
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
        // The UI's stdout/stderr flow into the core's log streams so a QML
        // error is visible in the packaged runtime log.
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr

        if err := cmd.Start(); err != nil {
                fmt.Println("desktop: launch Qt host:", err)
                return 1
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
                if err != nil {
                        var ee *exec.ExitError
                        if errors.As(err, &ee) {
                                exitCode = ee.ExitCode()
                        } else {
                                fmt.Println("desktop: ui wait:", err)
                                exitCode = 1
                        }
                }
        case sig := <-sigCh:
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

        return exitCode
}

// uiExecutable resolves the packaged Qt host next to the running core.
func uiExecutable() (string, error) {
        self, err := os.Executable()
        if err != nil {
                return "", fmt.Errorf("resolve core executable: %w", err)
        }

        name := uiExecutableNames[runtime.GOOS]
        if name == "" {
                return "", fmt.Errorf("no Qt host binary name for %s", runtime.GOOS)
        }

        path := filepath.Join(filepath.Dir(self), name)
        if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
                return path, nil
        }

        // Explicit override keeps local runs testable before packaging; the
        // "none" sentinel is handled by the caller (no-spawn serving mode).
        if alt := os.Getenv("SHEYTAN_UI_EXECUTABLE"); alt != "" {
                if alt == "none" {
                        return "none", nil
                }
                if st, statErr := os.Stat(alt); statErr == nil && !st.IsDir() {
                        return alt, nil
                }
                return "", fmt.Errorf("SHEYTAN_UI_EXECUTABLE=%s does not exist", alt)
        }

        return "", fmt.Errorf("Qt host %s not found next to the core (%s) — set SHEYTAN_UI_EXECUTABLE for development runs", name, filepath.Dir(self))
}
