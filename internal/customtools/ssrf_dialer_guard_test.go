// Package customtools — ssrf_dialer_guard_test.go
//
// v1.11.1 (run 41901967798, CodeQL go/request-forgery alert #288): the
// endpoint validator sees HOSTNAMES, so a hostile DNS answer could still
// point a benign name at a forbidden address. The connection-time guard
// (safeToolDialer.Control) re-classifies every RESOLVED address after
// DNS and before any packet is sent. These tests pin the blocked
// classes and the preserved local-first allowlist.
package customtools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestGuardResolvedAddressBlocksForbiddenClasses: link-local unicast,
// link-local multicast, their IPv4-mapped IPv6 spellings and the
// documented IPv6 metadata endpoint are refused at connect time.
func TestGuardResolvedAddressBlocksForbiddenClasses(t *testing.T) {
	for _, addr := range []string{
		"169.254.169.254:80",          // cloud metadata (AWS/GCP/Azure link-local)
		"169.254.170.2:80",            // ECS task metadata endpoint
		"[fe80::1]:443",               // link-local unicast
		"[fe80::a00:27ff:fe00:0]:443", // link-local unicast (EUI-64)
		"[ff02::1]:443",               // link-local multicast
		"[::ffff:169.254.169.254]:80", // IPv4-mapped metadata
		"[fd00:ec2::254]:80",          // AWS IMDSv6 unique-local endpoint
	} {
		if err := guardResolvedAddress(addr); err == nil {
			t.Errorf("resolved address %s must be REFUSED at connect time", addr)
		}
	}
}

// TestGuardResolvedAddressAllowsLocalFirstTargets: the local-first trust
// boundary is preserved — loopback, RFC1918 and unique-local (non-
// metadata) destinations stay reachable for first-party local services.
func TestGuardResolvedAddressAllowsLocalFirstTargets(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:8080",
		"[::1]:8080",
		"10.0.0.5:443",
		"192.168.1.10:443",
		"172.16.0.1:443",
		"[fd00:ec2::1234]:80", // unique-local but NOT the metadata endpoint
	} {
		if err := guardResolvedAddress(addr); err != nil {
			t.Errorf("local-first target %s must stay allowed, got: %v", addr, err)
		}
	}
}

// TestSafeToolDialerRefusesMetadataBeforeConnect: the Control hook runs
// after resolution and BEFORE any packet is sent, so a dial attempt to a
// forbidden address fails fast with the guard error — no connection is
// ever opened. A literal address makes resolution instant, keeping the
// test deterministic.
func TestSafeToolDialerRefusesMetadataBeforeConnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := safeToolDialer.DialContext(ctx, "tcp", "169.254.169.254:80")
	if err == nil {
		t.Fatal("dial to the cloud metadata address must be refused by the connection-time guard")
	}
}

// TestNewToolHTTPClientKeepsLocalFirstServing: the default tool client
// still reaches a loopback httptest server (first-party local services
// are a supported tool target) — the guard blocks forbidden classes,
// never the local-first allowlist.
func TestNewToolHTTPClientKeepsLocalFirstServing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "local-first ok")
	}))
	defer srv.Close()

	client := newToolHTTPClient(5 * time.Second)

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("loopback httptest server must stay reachable through the guarded client: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil || string(body) != "local-first ok" {
		t.Fatalf("unexpected response through the guarded client: %q err=%v", body, err)
	}
}
