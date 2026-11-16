// Package webapphost defines the platform-neutral WebAppHost contract for
// the v1.11.1 hybrid-native-UI milestone (work order §4.6): an OPTIONAL,
// ISOLATED native web-content runtime (WebView2 on Windows, WKWebView on
// macOS, WebKitGTK on Linux) that runs real web pages in its own native
// window — never the primary application shell, never a renderer with
// direct store access.
//
// This package is the typed CONTRACT plus the SECURITY POLICY as enforced
// code. It deliberately ships no adapter: on this milestone no desktop
// adapter passed a build+runtime gate (Linux WebKitGTK dev headers absent
// in the authoring environment; Windows/macOS hosts not exercised). The
// adapter matrix is STAGED_WITH_GATE in
// docs/architecture/WEBVIEW-CAPABILITY-MATRIX.md. No empty Start() method
// and no placeholder menu item is claimed as an implemented engine.
//
// Relationship to internal/browser: internal/browser is the agent's
// chromedp browser-AUTOMATION session (a different concern). This package
// is the user-facing web-content host contract. Neither duplicates the
// other; neither writes to any store (work order §4.1: all layers dispatch
// typed commands through the existing permission/service boundary).
package webapphost

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Engine identifies the native web-content engine of a platform.
type Engine string

const (
	EngineWebView2  Engine = "webview2"  // Windows
	EngineWKWebView Engine = "wkwebview" // macOS / iOS
	EngineWebKitGTK Engine = "webkitgtk" // Linux
)

// Availability is the truthful host-state ladder (work order §9.2):
// a detected engine is not an available one; neither proves an executed
// page load. Adapters report this; nothing else may infer it.
type Availability int

const (
	Unavailable Availability = iota // runtime prerequisites missing (engine not installed)
	Detected                        // engine present on the system
	Available                       // engine loadable by this process
)

func (a Availability) String() string {
	switch a {
	case Unavailable:
		return "unavailable"
	case Detected:
		return "detected"
	case Available:
		return "available"
	default:
		return "unknown"
	}
}

// ErrNoAdapter is the typed refusal returned by any future adapter
// registration path when no engine adapter passed its gate on the current
// platform. A refusal is never a fallback to a browser binary or remote
// Chrome (work order §4.6.3).
var ErrNoAdapter = fmt.Errorf("webapphost: no native web-content adapter available on this platform")

// Decision is the outcome of a navigation request.
type Decision int

const (
	DecisionAllow           Decision = iota // load in the isolated web window
	DecisionReject                          // refused with reason (never silently)
	DecisionExternalBrowser                 // offered to the system browser instead
)

func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionReject:
		return "reject"
	case DecisionExternalBrowser:
		return "external-browser"
	default:
		return "unknown"
	}
}

// TrustedOrigin is a local app origin EXPLICITLY created by the host for
// locally generated web apps (an app:// origin with a fixed host). It is
// never a wildcard filesystem grant: the origin names one host-owned
// asset root. Only a TrustedOrigin may ever receive the privileged JS
// bridge, and only with its explicit per-capability grants.
type TrustedOrigin struct {
	Scheme string // "app"
	Host   string // fixed origin host, e.g. "workspace"
	// AssetRoot is the host-owned local asset root for this origin: a
	// distinct, bounded root — never an arbitrary filesystem path and
	// never the user's home.
	AssetRoot string
}

func (t TrustedOrigin) String() string { return t.Scheme + "://" + t.Host }

// NavigationPolicy is the binding navigation policy of work order §4.6.6.
type NavigationPolicy struct {
	// TrustedOrigins are the host-created local app origins (app://...).
	// Arbitrary remote origins are NEVER added here.
	TrustedOrigins []TrustedOrigin
	// AllowPrivateNetwork is opt-in: loopback/RFC1918/link-local targets
	// are rejected unless the host explicitly granted them. There is no
	// API to disable TLS certificate validation — that is by construction,
	// not a forgotten flag.
	AllowPrivateNetwork bool
}

// privateCIDRs are the rejected private-network ranges (loopback,
// RFC1918, link-local). Parsed once.
var privateCIDRs = mustCIDRs(
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"::1/128",
	"fc00::/7",
)

func mustCIDRs(specs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(specs))
	for _, s := range specs {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			panic("webapphost: invalid private CIDR " + s)
		}
		out = append(out, n)
	}
	return out
}

func isPrivateHost(host string) bool {
	h := strings.Trim(host, "[]")
	if ip := net.ParseIP(h); ip != nil {
		for _, n := range privateCIDRs {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}
	// Policy code performs NO DNS resolution (that would be a side
	// channel): only literal localhost names count as private here.
	lh := strings.ToLower(h)
	return lh == "localhost" || strings.HasSuffix(lh, ".localhost") || strings.HasSuffix(lh, ".local")
}

// trustedOriginFor reports the TrustedOrigin matching the URL, or nil.
func (p NavigationPolicy) trustedOriginFor(u *url.URL) *TrustedOrigin {
	for i := range p.TrustedOrigins {
		t := &p.TrustedOrigins[i]
		if strings.EqualFold(u.Scheme, t.Scheme) && strings.EqualFold(u.Hostname(), t.Host) {
			return t
		}
	}
	return nil
}

// Decide evaluates one navigation request. Rules (work order §4.6.6):
//
//   - privileged schemes (javascript:, data:, file:) are REJECTED — never
//     loaded, never offered externally;
//   - a host-created trusted app:// origin is ALLOWED (the only origin
//     that may later receive the bridge);
//   - loopback/private-network targets are REJECTED unless covered by a
//     trusted local app origin or an explicit private-network grant;
//   - every other http/https/about URL is ALLOWED as untrusted content:
//     it loads in the isolated web window and NEVER receives the
//     privileged bridge;
//   - unknown/custom external schemes (magnet:, mailto:, …) are offered
//     to the system browser, never loaded here.
func (p NavigationPolicy) Decide(raw string) (Decision, *TrustedOrigin, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return DecisionReject, nil, fmt.Errorf("webapphost: unparseable URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "data", "file":
		return DecisionReject, nil, fmt.Errorf("webapphost: privileged scheme %q is refused", u.Scheme)
	case "":
		return DecisionReject, nil, fmt.Errorf("webapphost: scheme-less navigation is refused")
	}
	if t := p.trustedOriginFor(u); t != nil {
		return DecisionAllow, t, nil
	}
	if strings.EqualFold(u.Scheme, "app") {
		// The app: scheme family is host-privileged: an origin the host
		// never created is refused outright — never offered externally,
		// never loaded.
		return DecisionReject, nil, fmt.Errorf("webapphost: app:// origin %q was never created by the host", u.Hostname())
	}
	host := u.Hostname()
	if isPrivateHost(host) {
		if p.AllowPrivateNetwork {
			return DecisionAllow, nil, nil
		}
		return DecisionReject, nil, fmt.Errorf("webapphost: private-network target %q is refused (no trusted local app origin covers it)", host)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "about":
		return DecisionAllow, nil, nil // untrusted remote content — no bridge
	default:
		return DecisionExternalBrowser, nil, nil
	}
}

// BridgeGrant is one explicit per-capability grant for a trusted origin.
// The model cannot grant itself anything; grants exist only in host code.
type BridgeGrant struct {
	Origin TrustedOrigin
	Caps   []string // allowlisted capability names, e.g. "workspace.read"
}

// BridgeCall is one JS→Go bridge call request from a page.
type BridgeCall struct {
	Origin  string // the page's origin
	Op      string // named operation from the grant's allowlist
	CallID  string // operation id — the replay-protection key
	Payload []byte // bounded, schema-validated JSON payload
}

// defaultPayloadLimit bounds one bridge payload (64 KiB).
const defaultPayloadLimit = 64 * 1024

// Validate enforces the bridge contract (§4.6.6): only a trusted origin
// with an explicit grant; only allowlisted named operations; bounded
// payload; non-empty call id (replay protection). Remote origins fail
// closed — they never had a bridge.
func (g *BridgeGrant) Validate(c BridgeCall) error {
	if g == nil {
		return fmt.Errorf("webapphost: no bridge grant")
	}
	if c.Origin != g.Origin.String() {
		return fmt.Errorf("webapphost: bridge call from non-granted origin %q", c.Origin)
	}
	if c.CallID == "" {
		return fmt.Errorf("webapphost: bridge call id is required (replay protection)")
	}
	if c.Op == "" {
		return fmt.Errorf("webapphost: bridge operation is required")
	}
	allowed := false
	for _, a := range g.Caps {
		if a == c.Op {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("webapphost: bridge operation %q is not in the grant's allowlist", c.Op)
	}
	if len(c.Payload) > defaultPayloadLimit {
		return fmt.Errorf("webapphost: bridge payload %d exceeds the %d-byte bound", len(c.Payload), defaultPayloadLimit)
	}
	return nil
}

// DownloadPolicy: downloads NEVER go through the web engine's own
// downloader; they are handed to the application's existing safe
// downloader with an approved destination flow (§4.6.6). This type is the
// typed handoff for future adapters — it is not a second downloader
// implementation.
type DownloadPolicy struct {
	// RequireApproval: the zero value FAILS CLOSED — downloads always
	// require explicit approval in production posture.
	RequireApproval bool
}

// Handoff returns the approved-destination decision for a download
// request. It is true only when the flow proceeds through the existing
// safe downloader's approval; there is no silent-accept mode.
func (d DownloadPolicy) Handoff() (proceedToSafeDownloader bool, reason string) {
	if !d.RequireApproval {
		return false, "webapphost: downloads always require explicit approval (fail closed)"
	}
	return true, ""
}
