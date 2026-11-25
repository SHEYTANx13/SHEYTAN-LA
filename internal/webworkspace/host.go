//go:build webworkspace && !mobile

package webworkspace

// The desktop HOST of the web shell: the Wails v3 runtime owning ONE
// application object per process, one primary webview window, and the
// asset-server handler that serves the adaptive shell (internal/webshell)
// plus the core API surface on the same origin. This is the primary UI
// path on every desktop platform per the adaptive-Wails-UI decision; the
// native toolkit shells (fyne, go-gui) remain selectable capabilities on
// their supported platforms.
//
// The integration surface is TYPE-CHECKED by the CGO-free webworkspace
// build (`GOOS=… CGO_ENABLED=0 go vet -tags webworkspace
// ./internal/webworkspace/`); its runtime verification (WebView2 /
// WKWebView / WebKitGTK window launch, asset-server round trip) is the
// hosted desktop lanes' responsibility and is recorded as such in the
// handoff — never claimed from a type-check.

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ErrHostMisconfigured is the typed failure for a host started without
// the two handlers it must serve (assets + API) — a miswire, not a
// runtime condition.
var ErrHostMisconfigured = errors.New("web workspace host misconfigured")

// HostOptions is the complete configuration of ONE host run. AssetHandler
// is the shell handler (webshell.Handler(apiHandler)) — the SAME origin
// serves the shell and the API so the bridge policy stays same-origin.
type HostOptions struct {
	Name         string           // application/brand name shown by the runtime
	Title        string           // primary window title
	Width        int              // primary window width (<=0 applies the default)
	Height       int              // primary window height (<=0 applies the default)
	AssetHandler http.Handler     // serves the shell + the API on one origin
	Interrupt    <-chan os.Signal // when set, a received signal quits the runtime cleanly
}

// RunHost starts the web UI runtime and blocks until the primary window
// closes. The desktop wire runs exactly ONE host per process (the OS UI
// runtime is a per-process singleton — the same property the native
// toolkit shells have); the launcher owns that guarantee.
func RunHost(opts HostOptions) error {
	if opts.AssetHandler == nil {
		return fmt.Errorf("%w: AssetHandler is nil — the host must serve the web shell and the API on one origin", ErrHostMisconfigured)
	}

	if opts.Name == "" || opts.Title == "" {
		return fmt.Errorf("%w: Name and Title are required", ErrHostMisconfigured)
	}

	app := application.New(application.Options{
		Name: opts.Name,
		Assets: application.AssetOptions{
			Handler:        opts.AssetHandler,
			DisableLogging: true,
		},
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     opts.Title,
		Width:     orDefault(opts.Width, 1180),
		Height:    orDefault(opts.Height, 760),
		MinWidth:  360,
		MinHeight: 480,
		URL:       "/",
	})

	if win == nil {
		return errors.New("the web UI runtime refused to create the primary window")
	}

	if opts.Interrupt != nil {
		go func() {
			sig := <-opts.Interrupt
			app.Quit()
			_ = sig
		}()
	}

	return app.Run()
}
