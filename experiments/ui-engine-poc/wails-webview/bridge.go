// Package wailspoc — the Wails Web Workspace POC (v1.11.1 §3.4/§6).
//
// ISOLATION: this is the explicitly requested WebView-hosted workspace
// layer, NOT a second application state. The Go core remains the ONE
// policy/persistence/permission authority; this POC proves the boundary
// contracts that must hold before any WebAppHost promotion:
//
//   - the bridge speaks a STRICT typed message schema (unknown
//     fields/methods rejected; payloads bounded);
//   - untrusted origins never receive the privileged bridge;
//   - navigation to file://, javascript:, data: and metadata endpoints is
//     blocked for privileged surfaces;
//   - WebView features are DETECTED, never assumed — every capability
//     carries an honest fallback ("Open in system browser").
//
// What is verified on this host: module resolution (wails v3.0.0 stable,
// resolved through the module proxy), the bridge/security/feature layers
// under `go test ./...`. What is BLOCKED here: launching the actual
// WebView window — the sandbox has no WebKitGTK (Linux) and no Windows
// host; the runtime wiring lives in app_runtime.go behind the
// `wails_runtime` build tag and compiles only on a host with the
// platform WebView prerequisites (SEC/UX-04 acceptance stays with the
// hosted lanes and physical verification).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// maxBridgePayloadBytes bounds every bridge message (oversized payloads are
// a classic WebView memory/DoS surface; the boundary is enforced BEFORE
// any decode).
const maxBridgePayloadBytes = 64 * 1024

// BridgeMethod is one allowed bridge call. The allowlist is closed: an
// unknown method is a protocol violation, never a fallthrough.
type BridgeMethod string

const (
	MethodThemeTokens  BridgeMethod = "theme.tokens"  // read-only: serialized design tokens
	MethodEngineStatus BridgeMethod = "engine.status" // read-only: renderer catalog summary
	MethodPing         BridgeMethod = "workspace.ping"
)

var bridgeMethods = map[BridgeMethod]bool{
	MethodThemeTokens:  true,
	MethodEngineStatus: true,
	MethodPing:         true,
}

// BridgeRequest is the typed wire form. Unknown fields are rejected by
// decoding into a strict struct first (see DecodeBridgeRequest).
type BridgeRequest struct {
	Method string          `json:"method"`
	CorID  string          `json:"corId"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// DecodeBridgeRequest validates one inbound payload BYTES-first:
//   - size ceiling enforced before decode;
//   - JSON must be a single object;
//   - unknown FIELDS are rejected (strict schema — no silent
//     forward-compatibility that would let a web page smuggle options);
//   - the method must be in the closed allowlist;
//   - the correlation id must be present and non-empty.
func DecodeBridgeRequest(payload []byte) (BridgeRequest, error) {
	if len(payload) > maxBridgePayloadBytes {
		return BridgeRequest{}, fmt.Errorf("bridge payload %d bytes exceeds the %d ceiling", len(payload), maxBridgePayloadBytes)
	}
	var strict map[string]json.RawMessage
	if err := json.Unmarshal(payload, &strict); err != nil {
		return BridgeRequest{}, fmt.Errorf("bridge payload is not a JSON object: %w", err)
	}
	for field := range strict {
		switch field {
		case "method", "corId", "body":
		default:
			return BridgeRequest{}, fmt.Errorf("bridge request carries unknown field %q (strict schema)", field)
		}
	}
	var req BridgeRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return BridgeRequest{}, fmt.Errorf("bridge request decode: %w", err)
	}
	if strings.TrimSpace(req.Method) == "" {
		return BridgeRequest{}, errors.New("bridge request has no method")
	}
	if !bridgeMethods[BridgeMethod(req.Method)] {
		return BridgeRequest{}, fmt.Errorf("bridge method %q is not in the closed allowlist", req.Method)
	}
	if strings.TrimSpace(req.CorID) == "" {
		return BridgeRequest{}, errors.New("bridge request has no correlation id (idempotency/trace key)")
	}
	return req, nil
}

// BridgeResponse is the typed reply envelope. Errors are categories, not
// stack traces: the web surface never sees Go internals.
type BridgeResponse struct {
	CorID   string          `json:"corId"`
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"` // a CATEGORY, never internals
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewBridgeError builds the error category envelope.
func NewBridgeError(corID, category string) BridgeResponse {
	return BridgeResponse{CorID: corID, OK: false, Error: category}
}

// NewBridgeResult builds the success envelope.
func NewBridgeResult(corID string, payload json.RawMessage) BridgeResponse {
	return BridgeResponse{CorID: corID, OK: true, Payload: payload}
}
