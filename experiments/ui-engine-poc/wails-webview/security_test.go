// security_test.go — the navigation/origin/download policy: the trusted
// origin is the ONLY allowed in-app surface; blocked schemes and metadata
// endpoints fail closed; ordinary web targets degrade to the system
// browser; downloads stay bounded and traversal-free.
package main

import (
	"strings"
	"testing"
)

func TestTrustedOriginAllowed(t *testing.T) {
	v, err := DenyNavigation(TrustedOrigin)
	if err != nil || v != VerdictAllow {
		t.Fatalf("the embedded workspace origin must be allowed, got verdict=%v err=%v", v, err)
	}
}

func TestBlockedSchemesFailClosed(t *testing.T) {
	for _, target := range []string{
		"file:///etc/passwd",
		"javascript:alert(document.domain)",
		"data:text/html,<script>1</script>",
		"chrome://settings",
		"ms-xbl://whatever",
	} {
		if v, err := DenyNavigation(target); err == nil && v != VerdictDeny {
			t.Errorf("target %q must be DENIED (got %v)", target, v)
		}
	}
}

func TestUnknownWailsSurfaceDenied(t *testing.T) {
	if v, err := DenyNavigation("wails://attacker.example/steal"); err == nil || v != VerdictDeny {
		t.Errorf("an unknown wails:// surface must be denied (verdict=%v err=%v)", v, err)
	}
	if v, err := DenyNavigation("wails://SHEYTAN-WORKSPACE"); err == nil && v == VerdictAllow {
		// Case-insensitive match of the trusted origin spelling: still the
		// one embedded origin (allow), never a bypass of a DIFFERENT host.
		t.Logf("note: case-insensitive spelling resolved to the trusted origin (verdict=%v)", v)
	}
}

func TestMetadataEndpointsDenied(t *testing.T) {
	for _, host := range []string{
		"https://169.254.169.254/latest/meta-data",
		"http://metadata.google.internal/computeMetadata/v1",
		"http://localhost:9200/_cluster",
		"http://127.0.0.1:8080/admin",
		"http://10.0.0.5/props",
		"http://192.168.1.10/router",
		"http://172.16.0.1/config",
		"http://172.31.255.254/x",
		"http://0.0.0.0/",
	} {
		if v, err := DenyNavigation(host); err == nil && v != VerdictDeny {
			t.Errorf("metadata/private endpoint %q must be denied (got %v)", host, v)
		}
	}
}

func TestOrdinaryWebTargetIsAFallback(t *testing.T) {
	v, err := DenyNavigation("https://example.com/some-doc")
	if err != nil || v != VerdictFallback {
		t.Fatalf("an ordinary web target must carry the external-fallback verdict (never in-app navigation of untrusted origins), got %v err=%v", v, err)
	}
}

func TestCSPForbidsRemoteAndInlineScript(t *testing.T) {
	for _, must := range []string{"default-src 'self'", "script-src 'self'", "object-src 'none'", "frame-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(CSP, must) {
			t.Errorf("the CSP must contain %q (got %q)", must, CSP)
		}
	}
	if strings.Contains(CSP, "unsafe-inline") || strings.Contains(CSP, "*") {
		t.Errorf("the CSP must never allow unsafe-inline or wildcard origins: %q", CSP)
	}
}

func TestDownloadPolicyMIMEAndSize(t *testing.T) {
	p := DefaultDownloadPolicy
	for _, ok := range []string{"application/zip", "image/png", "application/pdf", "text/markdown; charset=utf-8"} {
		if !p.AllowsMIME(ok) {
			t.Errorf("MIME %q must be allowed", ok)
		}
	}
	for _, denied := range []string{"application/x-msdownload", "text/x-shellscript", ""} {
		if p.AllowsMIME(denied) {
			t.Errorf("MIME %q must be denied", denied)
		}
	}
	if p.MaxBytes <= 0 {
		t.Fatal("the download size ceiling must be positive (bounded downloads)")
	}
}

func TestSafeDownloadPathRejectsTraversal(t *testing.T) {
	for _, name := range []string{
		"../../escape.zip", "..\\..\\escape.zip", "/abs/escape", "a/../../b", "", ".", "nul\x00byte",
	} {
		if _, err := SafeDownloadPath("/downloads", name); err == nil {
			t.Errorf("download name %q must be rejected", name)
		}
	}
	p, err := SafeDownloadPath("/downloads", "report-2026-11-17.zip")
	if err != nil || p != "/downloads/report-2026-11-17.zip" {
		t.Fatalf("a clean filename must resolve in the workspace downloads dir, got %q err=%v", p, err)
	}
}
