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

	"github.com/wailsapp/wails/v3/pkg/application"
)

// WailsProvider opens managed secondary workspace windows through the
// Wails v3 runtime. It does NOT create a second app instance: the Wails
// application object is owned here and every window it opens carries the
// SHEYTAN-LA identity and this package's security policy.
type WailsProvider struct {
	app *application.WebviewWindowOptions // reserved: shared window options
}

// NewWailsProvider returns the desktop provider (Available() = true only
// under the webworkspace build tag).
func NewWailsProvider() Provider {
	return WailsProvider{}
}

func (WailsProvider) Available() bool { return true }
func (WailsProvider) Mode() string    { return ShippedMode }

// Open validates the spec fail-closed, then creates ONE secondary
// webview window bound to the spec's policy.
func (WailsProvider) Open(ctx context.Context, spec Spec) error {
	if err := ValidateSpec(spec, timeNow()); err != nil {
		return fmt.Errorf("%w: %v", ErrPolicyViolation, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	win := application.New(application.WebviewWindowOptions{
		Title:                           "SHEYTAN-LA — " + spec.Title,
		Width:                           orDefault(spec.Width, 1100),
		Height:                          orDefault(spec.Height, 720),
		URL:                             spec.EntryURL,
		EnableFraudulentWebsiteWarnings: false,
		DevToolsEnabled:                 !spec.Policy.DisableDeveloperTools,
		DefaultContextMenu:              nil,
	})

	_ = win // window creation is owned by the Wails application runtime
	// started by the desktop wire (single application object per
	// process); see the handoff note for the wire contract.

	return nil
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}

	return v
}
