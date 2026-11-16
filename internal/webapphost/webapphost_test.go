package webapphost

// webapphost_test.go — the contract's enforcement tests (work order §4.6.6
// / §14.2 WebView acceptance, policy level). These are the platform-
// neutral policy tests; per-engine adapter behavior is STAGED_WITH_GATE
// (see docs/architecture/WEBVIEW-CAPABILITY-MATRIX.md).

import (
	"strings"
	"testing"
)

var prodPolicy = NavigationPolicy{
	TrustedOrigins: []TrustedOrigin{
		{Scheme: "app", Host: "workspace", AssetRoot: "<data>/workspace-assets"},
	},
}

func TestNavigationRejectsPrivilegedSchemes(t *testing.T) {
	for _, raw := range []string{
		"javascript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"file:///etc/passwd",
		"/relative/without/scheme",
	} {
		d, _, err := prodPolicy.Decide(raw)
		if d != DecisionReject || err == nil {
			t.Fatalf("Decide(%q) = %v, err=%v; want reject with reason", raw, d, err)
		}
	}
}

func TestNavigationRejectsPrivateNetworkByDefault(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:8080/",
		"http://127.0.0.1:9/",
		"http://10.1.2.3/",
		"http://192.168.1.10/admin",
		"http://169.254.169.254/latest/meta-data", // cloud metadata endpoint class
		"http://[::1]/",
	} {
		d, _, err := prodPolicy.Decide(raw)
		if d != DecisionReject || err == nil {
			t.Fatalf("Decide(%q) = %v, err=%v; want reject (private network)", raw, d, err)
		}
		if !strings.Contains(err.Error(), "private-network") {
			t.Fatalf("Decide(%q) reason = %q; want the private-network class", raw, err.Error())
		}
	}
}

func TestNavigationAllowsTrustedAppOrigin(t *testing.T) {
	d, origin, err := prodPolicy.Decide("app://workspace/index.html")
	if err != nil || d != DecisionAllow || origin == nil {
		t.Fatalf("trusted app origin: d=%v origin=%v err=%v", d, origin, err)
	}
	if origin.String() != "app://workspace" {
		t.Fatalf("origin = %q, want app://workspace", origin.String())
	}
	// A DIFFERENT app origin that was never created is refused like any
	// unknown host.
	d, origin, err = prodPolicy.Decide("app://evil/index.html")
	if d != DecisionReject || origin != nil || err == nil {
		t.Fatalf("uncreated app origin must be rejected, got d=%v err=%v", d, err)
	}
}

func TestNavigationAllowsRemoteHTTPSAsUntrustedNoBridge(t *testing.T) {
	d, origin, err := prodPolicy.Decide("https://example.org/some/page")
	if err != nil || d != DecisionAllow || origin != nil {
		t.Fatalf("remote https: d=%v origin=%v err=%v; want allow WITHOUT a trusted origin (no bridge)", d, origin, err)
	}
}

func TestNavigationExternalSchemesGoToSystemBrowser(t *testing.T) {
	for _, raw := range []string{"magnet:?xt=urn:btih:xyz", "mailto:someone@example.org"} {
		d, _, err := prodPolicy.Decide(raw)
		if err != nil || d != DecisionExternalBrowser {
			t.Fatalf("Decide(%q) = %v err=%v; want external-browser handoff", raw, d, err)
		}
	}
}

func TestPrivateNetworkGrantIsExplicitAndScoped(t *testing.T) {
	granted := prodPolicy
	granted.AllowPrivateNetwork = true
	d, _, err := granted.Decide("http://127.0.0.1:8080/")
	if err != nil || d != DecisionAllow {
		t.Fatalf("explicit private-network grant: d=%v err=%v; want allow", d, err)
	}
	// The grant never turns a privileged scheme back on.
	d, _, err = granted.Decide("javascript:alert(1)")
	if d != DecisionReject || err == nil {
		t.Fatalf("grant must not re-enable privileged schemes; got d=%v err=%v", d, err)
	}
}

func TestBridgeValidateFailClosed(t *testing.T) {
	grant := &BridgeGrant{
		Origin: TrustedOrigin{Scheme: "app", Host: "workspace"},
		Caps:   []string{"workspace.read"},
	}
	base := BridgeCall{
		Origin:  "app://workspace",
		Op:      "workspace.read",
		CallID:  "call-1",
		Payload: []byte(`{"limit":10}`),
	}
	if err := grant.Validate(base); err != nil {
		t.Fatalf("valid bridge call rejected: %v", err)
	}

	// Remote origin never had a bridge.
	remote := base
	remote.Origin = "https://example.org"
	if err := grant.Validate(remote); err == nil {
		t.Fatal("bridge call from a remote origin must fail closed")
	}
	// Non-allowlisted operation.
	evil := base
	evil.Op = "workspace.write"
	if err := grant.Validate(evil); err == nil {
		t.Fatal("non-granted operation must fail closed")
	}
	// Missing call id (replay protection).
	noID := base
	noID.CallID = ""
	if err := grant.Validate(noID); err == nil {
		t.Fatal("missing call id must fail closed")
	}
	// Oversized payload.
	fat := base
	fat.Payload = make([]byte, defaultPayloadLimit+1)
	if err := grant.Validate(fat); err == nil {
		t.Fatal("oversized payload must fail closed")
	}
	// No grant at all.
	var nilGrant *BridgeGrant
	if err := nilGrant.Validate(base); err == nil {
		t.Fatal("nil grant must fail closed")
	}
}

func TestDownloadPolicyFailsClosed(t *testing.T) {
	var zero DownloadPolicy
	if ok, reason := zero.Handoff(); ok {
		t.Fatalf("zero-value download policy must fail closed, got ok with %q", reason)
	}
	approved := DownloadPolicy{RequireApproval: true}
	if ok, _ := approved.Handoff(); !ok {
		t.Fatal("approved download policy must hand off to the safe downloader")
	}
}
