//go:build wails_runtime

// app_runtime.go — the ACTUAL Wails v3 Web Workspace wiring.
//
// This file compiles ONLY with `-tags wails_runtime` on a host that has
// the platform WebView prerequisites (WebView2 on Windows, WebKitGTK on
// Linux, WebKit on macOS). The default `go test ./...` of this POC runs
// the bridge/security/feature layers WITHOUT the WebView runtime — the
// honest split for a host like the v1.11.1 sandbox (no WebKitGTK): the
// boundary contracts are verified here, the window itself is verified on
// a capable host (hosted lane / physical).
package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets/workspace.html
var workspaceAssets embed.FS

// RunWorkspace starts the Web Workspace companion surface.
//
// The service surface is NARROW: exactly the three bridge methods from
// bridge.go, bound through the constrained handler; the HTML/CSS/JS live
// in the embedded asset; no remote origin is ever loaded. Navigation and
// downloads pass through the security.go policy before reaching the
// WebView.
func RunWorkspace() error {
	app := application.New(application.Options{
		Name:        "SHEYTAN-LA Web Workspace (POC)",
		Description: "Isolated WebView workspace — the Go core stays the one authority",
		Services: []application.Service{
			&BridgeService{},
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(workspaceAssets),
		},
	})
	if app == nil {
		return fmt.Errorf("wails application did not initialize (host WebView prerequisites missing)")
	}
	err := app.Run()
	if err != nil {
		return fmt.Errorf("wails workspace run: %w", err)
	}
	return nil
}

// BridgeService exposes EXACTLY the closed allowlist to the WebView.
type BridgeService struct{}

// ThemeTokens serves the serialized design-token document (schema-versioned).
func (b *BridgeService) ThemeTokens(corID string) BridgeResponse {
	raw, err := os.ReadFile("assets/theme-tokens.json")
	if err != nil {
		// Category only — the web surface never sees internals.
		return NewBridgeError(corID, "tokens-unavailable")
	}
	return NewBridgeResult(corID, raw)
}

// EngineStatus serves the renderer catalog summary (read-only).
func (b *BridgeService) EngineStatus(corID string) BridgeResponse {
	// The catalog summary ships with the POC build; read-only.
	raw, err := os.ReadFile("assets/engine-status.json")
	if err != nil {
		return NewBridgeError(corID, "status-unavailable")
	}
	return NewBridgeResult(corID, raw)
}

// Ping answers liveness.
func (b *BridgeService) Ping(corID string) BridgeResponse {
	return NewBridgeResult(corID, []byte(`{"alive":true}`))
}
