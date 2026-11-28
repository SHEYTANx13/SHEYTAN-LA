//go:build webworkspace && !mobile && !headless

package desktop

// The web UI host wire: the adaptive web shell (internal/webshell) served
// through the typed web workspace runtime as the PRIMARY desktop shell on
// every platform this build carries the host. The core API and the shell
// share ONE loopback origin through the asset handler, so the shell's
// same-origin CSP and the core's authentication boundary hold unchanged.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/api"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/brand"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/localipc"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/webshell"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/webworkspace"
)

// webUIHostAvailable reports that this build links the typed web UI host.
func webUIHostAvailable() bool { return true }

// shellWebTransport is the transport token the web era logs with its
// shell-evidence records. The packaged desktop shell dials NO named pipe:
// it talks to the core over the ONE loopback origin through the served
// handler, and that is exactly what the records must say (the
// internal/smokeverdict contract and the launch smoke accept both eras —
// the gogui named-pipe bootstrap stays the truth for the mobile shell).
const shellWebTransport = "same-origin-http"

// proveShellCoreRead performs ONE real core read through the EXACT
// handler the shell will use (the same-origin asset+API wrapper around
// the core's HTTP surface) and returns the health answer's status. This
// is the web era's handshake-equivalent: the gogui bootstrap proved
// "the shell can read the core" by dialing the pipe; the web shell proves
// the same claim by reading the core's health surface over the origin it
// will serve — before the window boots, so a shell that cannot reach its
// core never shows a dead window (the same hard-transport-fault contract
// desktop.Run applies to the IPC serve stage).
func proveShellCoreRead(srv *api.Server) (int, error) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	webshell.Handler(srv.Handler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		return rec.Code, fmt.Errorf("the shell core read answered %d (want %d)", rec.Code, http.StatusOK)
	}

	return rec.Code, nil
}

// runWebUI boots the web shell in the primary window and blocks until it
// closes. The startup telemetry and the deferred core/IPC shutdown remain
// owned by Run — this function only owns the UI runtime and its signal
// funnel.
func runWebUI(srv *api.Server) int {
	logging.Default().Info("desktop", "%s v%s (web UI host — the adaptive web shell)", config.AppName, brand.DisplayVersion)
	fmt.Printf("%s v%s (web UI host — the adaptive web shell)\n", config.AppName, brand.DisplayVersion)

	// Shell-era handshake evidence (run 46050871166 repair): ONE real
	// read of the core through the exact served handler, logged with the
	// same record vocabulary the smoke contract asserts. A non-200 is a
	// typed boot failure, never a window with a dead core behind it.
	if _, err := proveShellCoreRead(srv); err != nil {
		logging.Default().Error("ipc", "core handshake failed transport=%s error=%v", shellWebTransport, err)

		return failBoot("shell core read", err)
	}

	logging.Default().Info("ipc", "core handshake successful protocol=%s protocolVersion=%d coreVersion=%s transport=%s",
		localipc.ProtocolName, localipc.ProtocolVersion, config.AppVersion, shellWebTransport)
	logging.Default().Info("ipc", "first core read succeeded (transport=%s)", shellWebTransport)

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	err := webworkspace.RunHost(webworkspace.HostOptions{
		Name:  config.AppName,
		Title: config.AppName + " — " + brand.DisplayVersion,
		// The brand mark for the window surfaces that read it here (the
		// Linux taskbar; Windows carries it in the PE version resource
		// through the gen-syso pipeline). The asset ships inside the
		// shell bundle, so the icon and the interface are ONE build
		// artifact.
		Icon: webshell.MustAsset("logo.png"),
		// ONE origin: the shell and the core API served by the same
		// handler, the same authentication boundary as every other mode.
		AssetHandler: webshell.Handler(srv.Handler()),
		Interrupt:    sigCh,
	})

	if err != nil {
		logging.Default().Error("desktop", "web UI host failed: %v", err)

		return failBoot("web ui host", err)
	}

	logging.Default().Info("desktop", "ui exit: clean close (web UI host)")

	return 0
}
