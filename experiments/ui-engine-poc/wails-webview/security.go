// security.go — the Web Workspace navigation/origin policy (§6 of the
// v1.11.1 work order). Pure Go so the boundary is TESTED without a
// WebView: the same functions are the runtime gate the Wails host applies
// before any navigation or bridge attach.
package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// TrustedOrigin is the ONE default-privileged origin: the local embedded
// app document. Everything else is untrusted until explicitly allowed.
const TrustedOrigin = "wails://sheytan-workspace"

// NavigationVerdict is the policy outcome for one target.
type NavigationVerdict string

const (
	VerdictAllow    NavigationVerdict = "allow"
	VerdictDeny     NavigationVerdict = "deny"
	VerdictFallback NavigationVerdict = "external-fallback" // allowed ONLY as "Open in system browser"
)

// DenyNavigation evaluates one target URL against the policy:
//
//   - the trusted origin is allowed;
//   - file://, javascript:, data: and unexpected custom schemes are denied
//     for privileged surfaces (bridge attached);
//   - localhost / link-local / private-range metadata endpoints are denied
//     (SSRF class);
//   - ordinary https/http targets are NOT navigated in-app: they carry the
//     external-fallback verdict (the operator opens them in the system
//     browser; the workspace never becomes a general web browser).
func DenyNavigation(raw string) (NavigationVerdict, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return VerdictDeny, fmt.Errorf("unparseable target %q: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "":
		return VerdictDeny, errors.New("scheme-less targets are not navigable")
	case "file", "javascript", "data":
		return VerdictDeny, fmt.Errorf("scheme %q is blocked for the privileged workspace", scheme)
	case "wails":
		if strings.EqualFold(u.String(), TrustedOrigin) {
			return VerdictAllow, nil
		}
		return VerdictDeny, fmt.Errorf("unrecognized wails:// surface %q (only the embedded workspace origin is trusted)", u.String())
	case "http", "https":
		if isMetadataEndpoint(u.Host) {
			return VerdictDeny, fmt.Errorf("host %q is a metadata/private endpoint (SSRF class)", u.Host)
		}
		// Never navigated in-app: the honest fallback.
		return VerdictFallback, nil
	default:
		return VerdictDeny, fmt.Errorf("scheme %q is not in the allowed set", scheme)
	}
}

// isMetadataEndpoint matches the cloud-metadata / link-local / loopback /
// private-range host class a privileged page must never reach.
func isMetadataEndpoint(host string) bool {
	h := strings.ToLower(host)
	stripPort := func(s string) string {
		if i := strings.LastIndex(s, ":"); i > 0 && !strings.Contains(s[i:], "]") {
			return s[:i]
		}
		return s
	}
	h = stripPort(h)
	switch {
	case h == "localhost",
		h == "169.254.169.254", h == "metadata.google.internal",
		strings.HasPrefix(h, "127."),
		strings.HasPrefix(h, "10."),
		strings.HasPrefix(h, "192.168."),
		h == "0.0.0.0":
		return true
	}
	if strings.HasPrefix(h, "172.") {
		var o1 int
		if _, err := fmt.Sscanf(h, "172.%d", &o1); err == nil && o1 >= 16 && o1 <= 31 {
			return true
		}
	}
	return false
}

// CSP is the workspace's Content-Security-Policy: no untrusted inline
// script, no remote script/style origins, no form-action leakage.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; frame-src 'none'; form-action 'self'; base-uri 'none'"

// DownloadPolicy constrains user-confirmed downloads (§6): bounded size,
// allowed MIME classes, .part staging + atomic commit, no path traversal.
type DownloadPolicy struct {
	MaxBytes     int64
	AllowedMIMEs []string
}

// DefaultDownloadPolicy is the v1.11.1 boundary.
var DefaultDownloadPolicy = DownloadPolicy{
	MaxBytes: 1 << 30, // 1 GiB ceiling
	AllowedMIMEs: []string{
		"application/zip", "application/octet-stream", "application/pdf",
		"image/png", "image/jpeg", "image/webp", "image/gif",
		"text/plain", "text/markdown", "text/csv",
		"application/json",
	},
}

// AllowsMIME reports whether one download content type is inside the policy.
func (p DownloadPolicy) AllowsMIME(mime string) bool {
	base := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(base, ";"); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	for _, allowed := range p.AllowedMIMEs {
		if base == allowed {
			return true
		}
	}
	return false
}

// SafeDownloadPath rejects traversal and absolute escapes: every download
// lands under the workspace's own downloads directory, flat, with the
// server-declared filename sanitized. An absolute path is REJECTED —
// silently re-rooting it would hide the escape attempt (pinned by
// TestSafeDownloadPathRejectsTraversal).
func SafeDownloadPath(root, name string) (string, error) {
	clean := strings.TrimSpace(name)
	clean = strings.ReplaceAll(clean, "\\", "/")
	if strings.HasPrefix(clean, "/") {
		return "", errors.New("download filename is absolute — rejected, never silently re-rooted")
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", errors.New("download filename traverses directories")
		}
	}
	if clean == "" || clean == "." {
		return "", errors.New("download filename is empty")
	}
	if strings.Contains(clean, "\x00") {
		return "", errors.New("download filename contains a NUL")
	}
	return root + "/" + clean, nil
}
