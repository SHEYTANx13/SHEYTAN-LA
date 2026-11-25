//go:build webworkspace && !mobile

package webworkspace

// The Wails v3 adapter (build-tagged: the WebView backends need the
// platform's native webkit headers at build time; this file is compiled
// ONLY in the desktop lanes that opt into the capability). It pins the
// exact stable module version in go.mod (github.com/wailsapp/wails/v3
// v3.1.0 — verified against the module proxy at execution time; never
// @latest, never a nightly).
//
// NOTE (honest scope): the adapter links Wails' application runtime for
// the managed secondary window. This file TYPE-CHECKS the integration
// surface; its runtime verification requires the hosted desktop lanes
// (webkit2gtk on Linux, WebView2 on Windows, WKWebView on macOS) — the
// execution sandbox cannot link webkit and says so in the handoff.

import (
	"context"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// appRuntime is the ONE application object per process (the OS UI runtime
// is a per-process singleton — the same property the native toolkit
// shells have). RunHost owns the primary window; the adapter creates the
// runtime lazily on first managed-secondary-window open.
var (
	appOnce   sync.Once
	appObject *application.App
)

func appRuntime() *application.App {
	appOnce.Do(func() {
		appObject = application.New(application.Options{
			Name: "SHEYTAN-LA",
		})
	})

	return appObject
}

// WailsProvider opens managed secondary workspace windows through the
// Wails v3 runtime. It does NOT create a second app instance: the runtime
// object is a per-process singleton owned by appRuntime, and every window
// it opens carries the SHEYTAN-LA identity and this package's security
// policy.
//
// Run 45522548507 repair: the previous adapter called
// application.New(WebviewWindowOptions{...}) — an API shape that does not
// exist in the pinned v3.1.0 runtime (New takes Options; windows come
// from the Window manager). It also discarded the window value (`_ = win`)
// and never ran the runtime. The adapter now creates the real window
// through app.Window.NewWithOptions and shows it.
type WailsProvider struct{}

// NewWailsProvider returns the desktop provider (Available() = true only
// under the webworkspace build tag).
func NewWailsProvider() Provider {
	return WailsProvider{}
}

func (WailsProvider) Available() bool { return true }
func (WailsProvider) Mode() string    { return ShippedMode }

// Open validates the spec fail-closed, then creates ONE secondary
// webview window bound to the spec's policy on the shared runtime.
func (WailsProvider) Open(ctx context.Context, spec Spec) error {
	if err := ValidateSpec(spec, timeNow()); err != nil {
		return fmt.Errorf("%w: %v", ErrPolicyViolation, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	app := appRuntime()

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "workspace-" + spec.Title,
		Title:           "SHEYTAN-LA — " + spec.Title,
		Width:           orDefault(spec.Width, 1100),
		Height:          orDefault(spec.Height, 720),
		MinWidth:        360,
		MinHeight:       480,
		URL:             spec.EntryURL,
		DevToolsEnabled: !spec.Policy.DisableDeveloperTools,
	})

	if win == nil {
		return fmt.Errorf("%w: the runtime refused the secondary workspace window", ErrPolicyViolation)
	}

	win.Show()

	return nil
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}

	return v
}
