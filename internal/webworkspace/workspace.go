// Package webworkspace is the typed integration boundary for the Wails v3
// Web Workspace capability (master work order for run 44457547289 §6):
// a controlled, app-owned SECONDARY web workspace window for trusted
// local web content on the desktop, sharing the same core services and
// identity — never a competing primary shell, never a second application
// instance, never a claim that Wails renders Go-Gui.
//
// Integration mode (§6.2, decided by evidence): Wails v3 stable is
// desktop-first (Windows, macOS, Linux) and there is NO proven native
// child-WebView embedding path inside a Go-Gui-owned window across all
// three desktop targets — so the capability ships as a MANAGED SECONDARY
// WINDOW launched from the Go-Gui tab/command. On mobile the provider is
// a typed unsupported capability (§6.2: mobile stays outside the stable
// desktop compatibility contract).
//
// The Wails adapter is build-tagged (webworkspace) because the WebView
// backends need the platform's native webkit headers at build time; the
// default build carries the typed unsupported provider so every other
// package compiles and tests without the toolchain.
package webworkspace

import (
	"errors"
	"net/url"
	"strings"
)

// Typed capability states (fail-closed).
var (
	// ErrUnsupported is the typed answer outside the desktop+tagged
	// build (mobile, headless, or a build without the webworkspace tag).
	ErrUnsupported = errors.New("webworkspace: web workspace capability not available in this build (desktop + webworkspace build tag required)")

	// ErrPolicyViolation: the requested workspace content violates the
	// security policy — never opened, always surfaced.
	ErrPolicyViolation = errors.New("webworkspace: requested content violates the workspace security policy")
)

// Mode names the integration shape shipped for the pinned Wails release.
const (
	// ModeManagedSecondaryWindow: Wails owns a secondary, app-launched
	// window; the Go-Gui shell remains the sole primary shell.
	ModeManagedSecondaryWindow = "managed-secondary-window"

	// ModeEmbeddedTab requires the proven child-WebView embedding path
	// (§6.2) — NOT shipped; named for the contract, never claimed.
	ModeEmbeddedTab = "embedded-tab (NOT SHIPPED: no proven cross-desktop child-WebView path)"
)

// ShippedMode is the honest answer to "how is Wails integrated".
const ShippedMode = ModeManagedSecondaryWindow

// SecurityPolicy is the enforced web security boundary (§6.3). Zero
// values fail closed.
type SecurityPolicy struct {
	// AllowedOrigins lists the ONLY origins the workspace may navigate
	// to. The local trusted origin (the app's loopback asset server)
	// must be listed explicitly; navigation to anything else is refused.
	AllowedOrigins []string

	// AllowExternalLinks opens http(s) links to NON-allowed origins in
	// the system browser instead of the workspace. Arbitrary external
	// navigation inside the workspace stays disabled either way.
	AllowExternalLinks bool

	// ContentSecurityPolicy is delivered with every workspace document.
	// An empty value fails closed at Open time.
	ContentSecurityPolicy string

	// DisableDeveloperTools strips the devtools surface in workspace
	// windows (production posture; ALPHA keeps it disabled by default).
	DisableDeveloperTools bool
}

// DefaultPolicy returns the shipped policy: the local trusted origin
// only, a strict CSP, devtools off, external links to the system browser.
func DefaultPolicy(localOrigin string) SecurityPolicy {
	return SecurityPolicy{
		AllowedOrigins:        []string{localOrigin},
		AllowExternalLinks:    true,
		ContentSecurityPolicy: "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src " + localOrigin + "; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
		DisableDeveloperTools: true,
	}
}

// CheckDecision classifies one navigation target.
type CheckDecision int

const (
	DecisionOpenInWorkspace CheckDecision = iota
	DecisionOpenInBrowser
	DecisionRefuse
)

// CheckURL classifies a navigation target against the policy. The rule
// set (§6.3, in force):
//
//   - only http(s) schemes with an explicit host (no file:, no custom
//     schemes, no empty-host URLs);
//   - credentials (user:pass@) are refused;
//   - the origin must be in AllowedOrigins to open INSIDE the workspace,
//     OR (AllowExternalLinks) the target is a HAND-OFF to the system
//     browser — never workspace navigation.
func (p SecurityPolicy) CheckURL(raw string) (CheckDecision, error) {
	if strings.TrimSpace(raw) == "" {
		return DecisionRefuse, fmtErr("empty navigation target")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return DecisionRefuse, fmtErr("unparseable navigation target")
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return DecisionRefuse, fmtErr("only http(s) navigations are supported, got scheme " + u.Scheme)
	}

	if u.Host == "" {
		return DecisionRefuse, fmtErr("navigation target carries no host")
	}

	if u.User != nil {
		return DecisionRefuse, fmtErr("navigation target embeds credentials")
	}

	origin := schemeHost(u)

	for _, allowed := range p.AllowedOrigins {
		if origin == allowed {
			return DecisionOpenInWorkspace, nil
		}
	}

	if p.AllowExternalLinks {
		return DecisionOpenInBrowser, nil
	}

	return DecisionRefuse, fmtErr("origin not in the workspace allowlist: " + origin)
}

// Validate checks the policy itself: a workspace window is never opened
// without an explicit CSP and at least one allowed origin.
func (p SecurityPolicy) Validate() error {
	if strings.TrimSpace(p.ContentSecurityPolicy) == "" {
		return fmtErr("security policy carries no Content-Security-Policy — refusing to open a workspace window")
	}

	if len(p.AllowedOrigins) == 0 {
		return fmtErr("security policy allowlist is empty — refusing to open a workspace window")
	}

	for _, o := range p.AllowedOrigins {
		if strings.Contains(o, " ") || !strings.Contains(o, "://") {
			return fmtErr("workspace allowlist entry is not an origin: " + o)
		}
	}

	return nil
}

// schemeHost normalizes an URL to scheme://host:port form (default ports
// filled so "127.0.0.1" and "127.0.0.1:80" compare equal on http).
func schemeHost(u *url.URL) string {
	host := u.Hostname()

	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	return u.Scheme + "://" + host + ":" + port
}

type policyError string

func (e policyError) Error() string { return string(e) }

func fmtErr(msg string) error {
	return policyError("webworkspace: " + msg)
}
