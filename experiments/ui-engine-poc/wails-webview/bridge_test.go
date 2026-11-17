// bridge_test.go — the typed bridge contract: strict schema, closed
// method allowlist, bounded payloads, category-only errors.
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestBridgeAcceptsWellFormedRequest(t *testing.T) {
	req, err := DecodeBridgeRequest(mustJSON(t, BridgeRequest{Method: "theme.tokens", CorID: "c-1"}))
	if err != nil {
		t.Fatalf("a well-formed request must decode: %v", err)
	}
	if req.Method != "theme.tokens" || req.CorID != "c-1" {
		t.Fatalf("decoded request drifted: %+v", req)
	}
}

func TestBridgeRejectsUnknownField(t *testing.T) {
	raw := []byte(`{"method":"workspace.ping","corId":"c-2","smuggled":true}`)
	if _, err := DecodeBridgeRequest(raw); err == nil {
		t.Fatal("an unknown field must be rejected (strict schema — no smuggled options)")
	} else if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("the rejection must name the strict-schema rule, got: %v", err)
	}
}

func TestBridgeRejectsUnknownMethod(t *testing.T) {
	raw := mustJSON(t, BridgeRequest{Method: "fs.readFile", CorID: "c-3"})
	if _, err := DecodeBridgeRequest(raw); err == nil {
		t.Fatal("a method outside the closed allowlist must be rejected")
	}
	// The allowlist never exposes file-system or exec surfaces.
	for _, banned := range []string{"fs.readFile", "exec", "net.fetch", "secret.get"} {
		if _, err := DecodeBridgeRequest(mustJSON(t, BridgeRequest{Method: banned, CorID: "c"})); err == nil {
			t.Errorf("bridge method %q must be REJECTED", banned)
		}
	}
}

func TestBridgeRejectsOversizedPayload(t *testing.T) {
	oversized := make([]byte, maxBridgePayloadBytes+1)
	if _, err := DecodeBridgeRequest(oversized); err == nil {
		t.Fatal("an oversized payload must be rejected before decode")
	}
}

func TestBridgeRejectsMalformedAndEmpty(t *testing.T) {
	for _, raw := range []string{
		`not json`, `[]`, `{"method":"workspace.ping"}`, `{"corId":"c"}`, `{"method":"","corId":"c"}`, `{}`,
	} {
		if _, err := DecodeBridgeRequest([]byte(raw)); err == nil {
			t.Errorf("payload %q must be rejected", raw)
		}
	}
}

func TestBridgeResponseEnvelope(t *testing.T) {
	err := NewBridgeError("c-9", "payload-too-large")
	if err.OK || err.Error != "payload-too-large" || err.CorID != "c-9" {
		t.Fatalf("the error envelope drifted: %+v", err)
	}
	ok := NewBridgeResult("c-9", json.RawMessage(`{"x":1}`))
	if !ok.OK || string(ok.Payload) != `{"x":1}` {
		t.Fatalf("the result envelope drifted: %+v", ok)
	}
}
