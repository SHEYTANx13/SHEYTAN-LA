// ssrf_policy_test.go — v1.11.1 recovery regression coverage for the
// custom-tool HTTP endpoint policy (CodeQL go/request-forgery remediation,
// alert #2 at internal/customtools/exec.go): https-only endpoints,
// metadata/link-local refusal, and a redirect policy that revalidates
// every hop and refuses confused-deputy pivots into local/private hosts.
package customtools

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// TestValidateToolEndpointPolicy pins the first-request policy: https
// only, non-empty host, and cloud-metadata/link-local refusal. Loopback
// and private LAN destinations stay ALLOWED — local-first tools call
// first-party local services.
func TestValidateToolEndpointPolicy(t *testing.T) {
	allowed := []string{
		"https://api.example.com/v1?query=1",
		"https://127.0.0.1:8080/local-api",
		"https://192.168.1.10/lan-service",
		"https://localhost:9/x",
		"https://[::1]:8080/local",
	}
	for _, u := range allowed {
		if err := validateToolEndpoint(u); err != nil {
			t.Errorf("endpoint %q must be allowed, got: %v", u, err)
		}
	}

	refused := []string{
		"http://api.example.com/",                              // cleartext downgrade
		"ftp://example.com/file",                               // non-http(s) scheme
		"https://169.254.169.254/latest/meta-data/",            // AWS IMDS
		"https://[fd00:ec2::254]/latest/meta-data/",            // IMDSv6
		"https://metadata.google.internal/computeMetadata/v1/", // GCP metadata
		"https://[fe80::1%25eth0]/x",                           // link-local v6
		"https:///no-host",                                     // empty host
	}
	for _, u := range refused {
		if err := validateToolEndpoint(u); err == nil {
			t.Errorf("endpoint %q must be refused", u)
		}
	}
}

// TestToolRedirectPolicy pins the per-hop policy: no scheme downgrade,
// endpoint policy on every hop, same-host hops allowed, and redirects
// from a public origin into loopback/private hosts refused.
func TestToolRedirectPolicy(t *testing.T) {
	origin := mustURL(t, "https://api.example.com/start")

	// Same-host https redirect: allowed.
	same := &http.Request{URL: mustURL(t, "https://api.example.com/next")}
	if err := toolRedirectPolicy(same, []*http.Request{{URL: origin}}); err != nil {
		t.Fatalf("same-host redirect must be allowed, got: %v", err)
	}

	// Public https redirect: allowed.
	other := &http.Request{URL: mustURL(t, "https://other.example.com/next")}
	if err := toolRedirectPolicy(other, []*http.Request{{URL: origin}}); err != nil {
		t.Fatalf("public redirect must be allowed, got: %v", err)
	}

	// Scheme downgrade: refused.
	plain := &http.Request{URL: mustURL(t, "http://api.example.com/next")}
	if err := toolRedirectPolicy(plain, []*http.Request{{URL: origin}}); err == nil {
		t.Fatal("https→http redirect downgrade must be refused")
	}

	// Confused-deputy pivot into loopback: refused.
	loop := &http.Request{URL: mustURL(t, "https://127.0.0.1:9/admin")}
	if err := toolRedirectPolicy(loop, []*http.Request{{URL: origin}}); err == nil {
		t.Fatal("redirect from a public origin into loopback must be refused")
	}

	// Pivot into an RFC1918 address: refused.
	lan := &http.Request{URL: mustURL(t, "https://10.0.0.5/admin")}
	if err := toolRedirectPolicy(lan, []*http.Request{{URL: origin}}); err == nil {
		t.Fatal("redirect from a public origin into a private address must be refused")
	}

	// Metadata hop: refused even from a private origin.
	privOrigin := mustURL(t, "https://192.168.1.10/lan")
	meta := &http.Request{URL: mustURL(t, "https://169.254.169.254/latest/meta-data/")}
	if err := toolRedirectPolicy(meta, []*http.Request{{URL: privOrigin}}); err == nil {
		t.Fatal("metadata redirect hop must be refused")
	}
}

// TestValidateToolEndpointStructuralPolicy — v1.11.1 (run 41933333132,
// work order §8.1): userinfo refusal, host-syntax validation and the
// public-port policy are part of the endpoint trust boundary.
func TestValidateToolEndpointStructuralPolicy(t *testing.T) {
	allowed := []string{
		"https://api.example.com/v1",       // https default port
		"https://api.example.com:443/v1",   // explicit 443
		"https://127.0.0.1:8080/local-api", // local-first keeps arbitrary ports
		"https://192.168.1.10:9000/lan",    // private LAN keeps arbitrary ports
	}
	for _, u := range allowed {
		if err := validateToolEndpoint(u); err != nil {
			t.Errorf("endpoint %q must be allowed, got: %v", u, err)
		}
	}

	refused := []string{
		"https://user:pass@api.example.com/", // embedded userinfo
		"https://api.example.com:8080/v1",    // public host, non-443 port
		"https://api.example.com:22/v1",      // public host, ssh port
		"https://exa mple.com/",              // whitespace in host
		"https://example.com%2F@evil/",       // percent-escape tricks in the authority
		"https://[fd00:ec2::254]:8080/x",     // metadata v6 with a non-443 port
	}
	for _, u := range refused {
		if err := validateToolEndpoint(u); err == nil {
			t.Errorf("endpoint %q must be refused", u)
		}
	}
}

// TestToolRedirectPolicyLimit — v1.11.1: a custom CheckRedirect REPLACES
// net/http's default 10-hop cap, so the tool policy enforces its own
// small limit: five hops are allowed, the sixth is refused.
func TestToolRedirectPolicyLimit(t *testing.T) {
	origin := mustURL(t, "https://api.example.com/start")

	sameHop := func(n int) []*http.Request {
		via := []*http.Request{{URL: origin}}
		for i := 0; i < n; i++ {
			via = append(via, &http.Request{URL: mustURL(t, "https://api.example.com/hop")})
		}
		return via
	}

	if err := toolRedirectPolicy(&http.Request{URL: mustURL(t, "https://api.example.com/next")}, sameHop(3)); err != nil {
		t.Fatalf("the fifth hop (origin + 3 recorded + current) must be allowed, got: %v", err)
	}
	if err := toolRedirectPolicy(&http.Request{URL: mustURL(t, "https://api.example.com/next")}, sameHop(4)); err == nil {
		t.Fatal("the sixth redirect hop must be refused (a custom CheckRedirect replaces the default 10-cap)")
	}
}

// TestNewToolHTTPClientNoEnvironmentProxy — the tool client must never
// inherit HTTP(S)_PROXY: a proxy is a second destination authority
// outside every endpoint policy this tool enforces.
func TestNewToolHTTPClientNoEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://pivot.example.com:3128")
	t.Setenv("HTTP_PROXY", "http://pivot.example.com:3128")

	c := newToolHTTPClient(time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("the tool transport must not carry an environment proxy (Proxy must be nil)")
	}
	if c.CheckRedirect == nil {
		t.Fatal("the tool client must enforce the redirect policy")
	}
}
