// main.go — the POC self-check entry (NO build tag): validates the
// bridge/security/feature contracts in-process and exits 0/1. This runs
// on any host WITHOUT WebView prerequisites; the actual WebView wiring
// (app_runtime.go) compiles only behind the `wails_runtime` tag on a
// capable host.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// selfcheck validates the three boundary layers that must hold before any
// WebAppHost promotion. Each failure is printed with its layer.
func selfcheck() error {
	// 1. Bridge: a well-formed request decodes; garbage and oversized
	// payloads fail closed.
	good, _ := json.Marshal(BridgeRequest{Method: "workspace.ping", CorID: "selfcheck-1"})
	if _, err := DecodeBridgeRequest(good); err != nil {
		return fmt.Errorf("bridge layer: a well-formed request was rejected: %w", err)
	}
	oversized := make([]byte, maxBridgePayloadBytes+1)
	if _, err := DecodeBridgeRequest(oversized); err == nil {
		return fmt.Errorf("bridge layer: an oversized payload was accepted")
	}

	// 2. Security: the trusted origin passes; the blocked schemes and the
	// metadata endpoints fail closed.
	if v, err := DenyNavigation(TrustedOrigin); err != nil || v != VerdictAllow {
		return fmt.Errorf("security layer: the trusted origin was not allowed (verdict=%v err=%v)", v, err)
	}
	for _, blocked := range []string{
		"file:///etc/passwd", "javascript:alert(1)", "data:text/html,hi",
		"https://169.254.169.254/latest/meta-data", "http://localhost:8080/admin",
		"wails://something-else",
	} {
		if v, err := DenyNavigation(blocked); err == nil && v == VerdictAllow {
			return fmt.Errorf("security layer: blocked target %q was allowed", blocked)
		}
	}

	// 3. Features: every probe carries a fallback policy.
	if !EveryFeatureHasFallback() {
		return fmt.Errorf("feature layer: a probe is missing its fallback policy")
	}
	for _, want := range []string{"webgpu", "webrtc", "clipboard", "filesystem-access"} {
		if _, ok := FeatureByName(want); !ok {
			return fmt.Errorf("feature layer: required probe %q missing", want)
		}
	}
	return nil
}

func main() {
	if err := selfcheck(); err != nil {
		fmt.Fprintln(os.Stderr, "WAILS-WORKSPACE-POC selfcheck: FAIL —", err)
		os.Exit(1)
	}
	fmt.Println("WAILS-WORKSPACE-POC selfcheck: PASS (bridge + security + feature contracts)")
	fmt.Println("WAILS-WORKSPACE-POC runtime: NOT RUN on this host — WebView window requires the wails_runtime tag and a platform WebView")
}
