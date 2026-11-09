// ssrf_allowlist_policy_test.go — v1.11.1 SSRF remediation (run
// 42502043636 work order §4.1): the positive destination allowlist for
// custom-tool HTTP execution. The request URL's authority must come from
// the template's fixed host or the definition's declared allowedHosts —
// substituted parameter values alone can never select the destination,
// on the first request NOR on any redirect hop. Refused destinations
// receive NO request (the refusal precedes any network operation).
package customtools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func countingTransport(status int) (*countingRoundTripper, *http.Client) {
	rt := &countingRoundTripper{status: status}
	return rt, &http.Client{Transport: rt}
}

type countingRoundTripper struct {
	status  int
	request int
}

func (c *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	c.request++
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}, nil
}

func allowlistToolDef(template string, allowed []string) *Definition {
	return &Definition{
		ID: "t1", Name: "probe", ShortDesc: "probe", Description: "probe",
		ExecType:   ExecHTTP,
		HTTP:       &HTTPExec{Method: "GET", URL: template, AllowedHosts: allowed},
		Permission: PermNetwork,
		TimeoutSec: 5, OutputLimit: 1 << 20, Enabled: true,
	}
}

func TestHostAllowedAllowlistSemantics(t *testing.T) {
	allowed := []string{"api.example.com", "*.cdn.example.com"}

	allowedHosts := []string{
		"api.example.com",    // exact
		"API.EXAMPLE.COM.",   // case + trailing dot
		"v2.cdn.example.com", // one-level wildcard
	}
	for _, h := range allowedHosts {
		if !hostAllowed(h, allowed) {
			t.Errorf("host %q must match the allowlist %v", h, allowed)
		}
	}

	refusedHosts := []string{
		"",                         // empty
		"evil.example.com",         // not declared
		"cdn.example.com",          // wildcard never matches the bare suffix host
		"a.b.cdn.example.com",      // wildcard is single-level
		"api.example.com.evil.tld", // suffix confusion
	}
	for _, h := range refusedHosts {
		if hostAllowed(h, allowed) {
			t.Errorf("host %q must NOT match the allowlist %v", h, allowed)
		}
	}
	if hostAllowed("api.example.com", nil) {
		t.Errorf("an empty allowlist matches nothing")
	}
}

func TestEnforceEndpointAllowlistPolicy(t *testing.T) {
	const template = "https://api.example.com/v1/search?q=$q"

	// The template host is always authorized (substitution only fills
	// non-authority parts).
	if err := enforceEndpointAllowlist(template, "https://api.example.com/v1/search?q=x", nil); err != nil {
		t.Errorf("template host must stay authorized: %v", err)
	}

	// A substituted endpoint on a DIFFERENT authority is refused without
	// an explicit declaration.
	err := enforceEndpointAllowlist(template, "https://evil.example.com/v1/search?q=x", nil)
	if err == nil || !strings.Contains(err.Error(), "not an authorized destination") {
		t.Errorf("host pivot without allowedHosts must be refused, got: %v", err)
	}

	// With an explicit declaration, a declared destination is authorized.
	if err := enforceEndpointAllowlist(template, "https://api.example.com/v1/search?q=x", []string{"api.example.com"}); err != nil {
		t.Errorf("declared destination must pass: %v", err)
	}
	err = enforceEndpointAllowlist(template, "https://evil.example.com/", []string{"other.example.com"})
	if err == nil {
		t.Errorf("undeclared destination with a non-empty allowlist must be refused")
	}

	// Wildcard declarations authorize their subdomains.
	if err := enforceEndpointAllowlist(template, "https://v2.cdn.example.com/x", []string{"*.cdn.example.com"}); err != nil {
		t.Errorf("wildcard-declared destination must pass: %v", err)
	}

	// A parameterized AUTHORITY has no fixed destination: without an
	// explicit declaration the definition is invalid at request time.
	paraTemplate := "https://$host.example.com/x"
	err = enforceEndpointAllowlist(paraTemplate, "https://anything.example.com/x", nil)
	if err == nil || !strings.Contains(err.Error(), "no allowedHosts are declared") {
		t.Errorf("parameterized authority without allowedHosts must be refused, got: %v", err)
	}
	if err := enforceEndpointAllowlist(paraTemplate, "https://sub.example.com/x", []string{"*.example.com"}); err != nil {
		t.Errorf("parameterized authority with a matching declaration must pass: %v", err)
	}
}

// TestRunHTTPRefusesDestinationPivot pins the end-to-end flow: a
// parameterized-authority definition whose substituted host is not
// declared is refused BEFORE any request is issued (blocked destinations
// receive no request).
func TestRunHTTPRefusesDestinationPivot(t *testing.T) {
	rt, client := countingTransport(200)
	def := allowlistToolDef("https://$host.example.com/x", []string{"api.example.com"})
	tool := NewTool(def)
	tool.SetClientForTest(client)

	_, err := tool.runHTTP(context.Background(), map[string]any{"host": "evil"})
	if err == nil {
		t.Fatalf("a substituted host outside allowedHosts must fail the request")
	}
	if rt.request != 0 {
		t.Fatalf("refused destination must receive NO request (saw %d)", rt.request)
	}
	if !strings.Contains(err.Error(), "not an authorized destination") {
		t.Errorf("refusal must name the allowlist policy, got: %v", err)
	}
}

// TestRunHTTPAllowsTemplateHostAndDeclaredHosts pins the positive paths:
// substitution within the template authority executes, and a declared
// allowedHosts destination executes.
func TestRunHTTPAllowsTemplateHostAndDeclaredHosts(t *testing.T) {
	// Template host + query substitution.
	rt, client := countingTransport(200)
	tool := NewTool(allowlistToolDef("https://api.example.com/v1/search?q=$q", nil))
	tool.SetClientForTest(client)
	if _, err := tool.runHTTP(context.Background(), map[string]any{"q": "x y"}); err != nil {
		t.Fatalf("template-host request must execute, got: %v", err)
	}
	if rt.request != 1 {
		t.Fatalf("exactly one request expected, saw %d", rt.request)
	}

	// Declared destination host.
	rt2, client2 := countingTransport(200)
	tool2 := NewTool(allowlistToolDef("https://$host.example.com/x", []string{"*.example.com"}))
	tool2.SetClientForTest(client2)
	if _, err := tool2.runHTTP(context.Background(), map[string]any{"host": "api"}); err != nil {
		t.Fatalf("declared destination must execute, got: %v", err)
	}
	if rt2.request != 1 {
		t.Fatalf("exactly one request expected, saw %d", rt2.request)
	}
}

// TestRunHTTPRedirectPolicyReappliesAllowlist pins the per-hop allowlist:
// a redirect from an authorized host to an undeclared host is refused —
// the hop never executes.
func TestRunHTTPRedirectPolicyReappliesAllowlist(t *testing.T) {
	redirector := &redirectRoundTripper{
		target: "https://evil.example.com/loot",
		first:  &http.Response{StatusCode: http.StatusFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))},
	}
	client := &http.Client{Transport: redirector}
	tool := NewTool(allowlistToolDef("https://api.example.com/v1/x", nil))
	tool.SetClientForTest(client)

	_, err := tool.runHTTP(context.Background(), nil)
	if err == nil {
		t.Fatalf("redirect to an undeclared host must fail")
	}
	if !strings.Contains(err.Error(), "not an authorized destination") {
		t.Errorf("the per-hop allowlist must refuse the redirect, got: %v", err)
	}
	if redirector.request != 1 {
		t.Fatalf("only the first hop may be issued (saw %d requests)", redirector.request)
	}
}

type redirectRoundTripper struct {
	target  string
	first   *http.Response
	request int
}

func (r *redirectRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.request++
	if r.request == 1 {
		r.first.Header.Set("Location", r.target)
		return r.first, nil
	}
	return nil, errors.New("no further hops should be issued")
}

func TestValidateAllowedHostEntries(t *testing.T) {
	bad := []string{
		"",
		"http://api.example.com",
		"api.example.com/path",
		"api.example.com:443",
		"user@api.example.com",
		"*.*.example.com",
		"*.example.com/path",
		"under score.example.com",
	}
	for _, entry := range bad {
		if err := validateAllowedHostEntry(entry); err == nil {
			t.Errorf("allowedHosts entry %q must be rejected", entry)
		}
	}
	good := []string{"api.example.com", "*.api.example.com", "API.example.com", "[2001:db8::1]", "192.0.2.1"}
	for _, entry := range good {
		if err := validateAllowedHostEntry(entry); err != nil {
			t.Errorf("allowedHosts entry %q must be accepted, got: %v", entry, err)
		}
	}
}
