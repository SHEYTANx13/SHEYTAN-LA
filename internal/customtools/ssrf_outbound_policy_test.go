// Package customtools — ssrf_outbound_policy_test.go
//
// v1.11.1 (run 42127523923, work order §5.4/§5.7): hardening of the
// connection-time guard. These tests pin the FULL forbidden class table
// (link-local/metadata, all multicast, unspecified, non-routable
// reserved blocks — including their IPv4-mapped IPv6 spellings), the
// DNS-rebinding behavior (a hostile resolution is refused at connect
// time before any packet), the redirect policy (scheme downgrade and
// public→local pivot refusal), and the endpoint structural policy
// (userinfo, ports, schemes, control characters).
package customtools

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestGuardResolvedAddressBlocksExtendedClasses: the §5.4 forbidden
// classes beyond link-local/metadata — all multicast, the unspecified
// address, the reserved blocks (v4 and v6) and their IPv4-mapped IPv6
// spellings — are refused at connect time.
func TestGuardResolvedAddressBlocksExtendedClasses(t *testing.T) {
	for _, addr := range []string{
		"224.0.0.1:443",           // IPv4 multicast (local network scope)
		"239.255.255.250:1900",    // SSDP/UPnP multicast discovery
		"[ff05::1]:443",           // site-local multicast
		"[ff0e::1]:443",           // global-scope multicast
		"0.0.0.0:80",              // unspecified ("this host")
		"0.1.2.3:443",             // inside the reserved 0.0.0.0/8 block
		"[::]:80",                 // IPv6 unspecified
		"240.0.0.1:443",           // reserved block
		"255.255.255.255:443",     // limited broadcast (inside 240.0.0.0/4)
		"198.18.0.1:443",          // benchmarking block
		"192.0.2.1:443",           // TEST-NET-1 (documentation)
		"198.51.100.7:443",        // TEST-NET-2 (documentation)
		"203.0.113.9:443",         // TEST-NET-3 (documentation)
		"192.0.0.8:443",           // IETF protocol assignments
		"[2001:db8::1]:443",       // IPv6 documentation prefix
		"[100::1:2:3]:443",        // discard-only range
		"[::ffff:224.0.0.1]:443",  // IPv4-mapped multicast
		"[::ffff:0.0.0.0]:80",     // IPv4-mapped unspecified
		"[::ffff:240.0.0.1]:443",  // IPv4-mapped reserved
		"[::ffff:198.18.0.1]:443", // IPv4-mapped benchmarking
		"[::ffff:192.0.2.1]:443",  // IPv4-mapped documentation
	} {
		if err := guardResolvedAddress(addr); err == nil {
			t.Errorf("resolved address %s must be REFUSED at connect time", addr)
		}
	}
}

// TestGuardResolvedAddressStillAllowsLocalFirstAndPublic: the preserved
// local-first surface (loopback, RFC1918, unique-local, CGNAT overlay)
// and ordinary public addresses keep connecting.
func TestGuardResolvedAddressStillAllowsLocalFirstAndPublic(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:8080",
		"[::1]:8080",
		"10.1.2.3:443",
		"192.168.0.5:443",
		"172.16.9.9:443",
		"[fd00:ec2::1234]:80",
		"100.64.1.2:443", // CGNAT shared space (overlay networks) — local-first
		"8.8.8.8:443",
		"1.1.1.1:443",
		"[2606:4700:4700::1111]:443",
	} {
		if err := guardResolvedAddress(addr); err != nil {
			t.Errorf("address %s must stay allowed, got: %v", addr, err)
		}
	}
}

// TestDialGuardRefusesHostileResolutionBeforeConnect: the DNS-rebinding
// shape (§5.7) — a benign HOSTNAME whose "resolution" (here: a fake
// lookup, no production resolver involved) returns the cloud metadata
// address must be refused by the connect-time guard BEFORE any packet is
// sent. The guard runs on the resolved address, so no matter how the
// name was resolved, the connection cannot proceed.
func TestDialGuardRefusesHostileResolutionBeforeConnect(t *testing.T) {
	// Fake DNS: the hostile answer for the benign name.
	hostileAnswer := "169.254.169.254:443"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// The dialer under test shares safeToolDialer's Control hook — the
	// same classification any guarded connection runs — dialed against
	// the fake resolution result. No network traffic occurs: Control
	// fires before connect.
	_, err := safeToolDialer.DialContext(ctx, "tcp", hostileAnswer)
	if err == nil {
		t.Fatal("a hostile resolution pointing at the metadata endpoint must be refused before connect")
	}

	if !strings.Contains(err.Error(), "tool connection guard") {
		t.Fatalf("refusal must come from the connect-time guard, got: %v", err)
	}
}

// TestToolRedirectPolicyRefusesSchemeDowngradeAndPivot: every redirect
// hop is revalidated — an http:// downgrade target and a public→
// metadata pivot are refused; a same-host https hop is allowed.
func TestToolRedirectPolicyRefusesSchemeDowngradeAndPivot(t *testing.T) {
	first, err := http.NewRequest(http.MethodGet, "https://example.com/start", nil)
	if err != nil {
		t.Fatal(err)
	}

	downgrade, err := http.NewRequest(http.MethodGet, "http://example.com/downgraded", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := toolRedirectPolicy(downgrade, []*http.Request{first}); err == nil {
		t.Fatal("an https→http redirect downgrade must be refused")
	}

	pivot, err := http.NewRequest(http.MethodGet, "https://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := toolRedirectPolicy(pivot, []*http.Request{first}); err == nil {
		t.Fatal("a public→metadata redirect pivot must be refused")
	}

	sameHost, err := http.NewRequest(http.MethodGet, "https://example.com/next", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := toolRedirectPolicy(sameHost, []*http.Request{first}); err != nil {
		t.Fatalf("a same-host https hop must stay allowed, got: %v", err)
	}
}

// TestValidateToolEndpointStructuralRefusals: userinfo, non-https
// schemes, opaque/authority-less forms, public non-443 ports and control
// characters are refused; loopback and 127.0.0.1 arbitrary ports stay
// allowed (local-first).
func TestValidateToolEndpointStructuralRefusals(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/",                    // scheme downgrade at entry
		"ftp://example.com/file",                 // unsupported scheme
		"file:///etc/passwd",                     // unsupported scheme
		"https://user:secret@example.com/",       // embedded userinfo
		"https://user@example.com/",              // userinfo without password
		"https://example.com:8443/",              // public endpoint, non-443 port
		"https://example.com:80/",                // public endpoint, http port
		"https:opaque-without-authority",         // no host
		"https://exa mple.com/",                  // space in the authority
		"https://exa%6Dple.com%2F@evil.example/", // percent-escape confusion + userinfo
	} {
		if err := validateToolEndpoint(raw); err == nil {
			t.Errorf("endpoint %q must be refused", raw)
		}
	}

	for _, raw := range []string{
		"https://example.com/",           // public https, default port
		"https://example.com:443/",       // public https, explicit 443
		"https://127.0.0.1:9101/metrics", // local-first, arbitrary port
		"https://192.168.1.5:8443/",      // LAN service, arbitrary port
	} {
		if err := validateToolEndpoint(raw); err != nil {
			t.Errorf("endpoint %q must stay allowed, got: %v", raw, err)
		}
	}
}
