//go:build webworkspace && !mobile && !headless

package desktop

// The web UI host wire: the adaptive web shell (internal/webshell) served
// through the typed web workspace runtime as the PRIMARY desktop shell on
// every platform this build carries the host. The core API and the shell
// share ONE loopback origin through the asset handler, so the shell's
// same-origin CSP and the core's authentication boundary hold unchanged.

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/api"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/brand"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/webshell"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/webworkspace"
)

// webUIHostAvailable reports that this build links the typed web UI host.
func webUIHostAvailable() bool { return true }

// runWebUI boots the web shell in the primary window and blocks until it
// closes. The startup telemetry and the deferred core/IPC shutdown remain
// owned by Run — this function only owns the UI runtime and its signal
// funnel.
func runWebUI(srv *api.Server) int {
	logging.Default().Info("desktop", "%s v%s (web UI host — the adaptive web shell)", config.AppName, brand.DisplayVersion)
	fmt.Printf("%s v%s (web UI host — the adaptive web shell)\n", config.AppName, brand.DisplayVersion)

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
