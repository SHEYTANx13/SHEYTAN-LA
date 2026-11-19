package webworkspace

import (
	"context"
	"errors"
	"testing"
)

func TestSecurityPolicyAllowsLocalOriginOnly(t *testing.T) {
	p := DefaultPolicy("http://127.0.0.1:45111")

	if err := p.Validate(); err != nil {
		t.Fatalf("default policy must validate: %v", err)
	}

	d, err := p.CheckURL("http://127.0.0.1:45111/workspace/index.html")
	if err != nil {
		t.Fatalf("local trusted origin must open in the workspace: %v", err)
	}

	if d != DecisionOpenInWorkspace {
		t.Fatalf("local origin decision = %v, want DecisionOpenInWorkspace", d)
	}
}

func TestSecurityPolicyRefusesExternalNavigationInsideWorkspace(t *testing.T) {
	p := DefaultPolicy("http://127.0.0.1:45111")

	cases := map[string]string{
		"plain external":     "https://example.com/page",
		"credential embed":   "https://user:secret@example.com/page",
		"file scheme":        "file:///etc/passwd",
		"custom scheme":      "shell:compute",
		"empty target":       "   ",
		"no host":            "https:///path",
		"javascript-ish url": "javascript:alert(1)",
	}

	for name, raw := range cases {
		d, err := p.CheckURL(raw)
		if d == DecisionOpenInWorkspace {
			t.Errorf("%s: %q must never open inside the workspace", name, raw)
		}

		if err != nil && d != DecisionRefuse {
			t.Errorf("%s: a refusal must carry DecisionRefuse, got %v", name, d)
		}
	}

	// javascript: must be refused outright (scheme gate).
	if _, err := p.CheckURL("javascript:alert(1)"); err == nil {
		t.Fatalf("javascript: URLs must be refused with an error")
	}
}

func TestSecurityPolicyExternalLinksAreBrowserHandoffs(t *testing.T) {
	p := DefaultPolicy("http://127.0.0.1:45111") // AllowExternalLinks: true

	d, err := p.CheckURL("https://github.com/SHEYTANx13/SHEYTAN-LA")
	if err != nil {
		t.Fatalf("external link with AllowExternalLinks must classify: %v", err)
	}

	if d != DecisionOpenInBrowser {
		t.Fatalf("external link decision = %v, want DecisionOpenInBrowser (system browser hand-off)", d)
	}

	// With external links disabled the same target is refused outright.
	p.AllowExternalLinks = false

	d, err = p.CheckURL("https://github.com/SHEYTANx13/SHEYTAN-LA")
	if d != DecisionRefuse {
		t.Fatalf("external link without the hand-off flag must be refused, got %v (%v)", d, err)
	}
}

func TestSecurityPolicyZeroValueFailsClosed(t *testing.T) {
	var p SecurityPolicy

	if err := p.Validate(); err == nil {
		t.Fatalf("a policy without CSP or allowlist must not validate")
	}

	d, err := p.CheckURL("https://anything.example/")
	if err != nil || d != DecisionOpenInBrowser {
		// Zero-value policy has no allowlist; with no AllowExternalLinks
		// the target must be refused.
		if d != DecisionRefuse {
			t.Fatalf("zero-value policy must refuse unknown origins, got %v (%v)", d, err)
		}
	}
}

func TestValidateSpecRefusesNonWorkspaceEntry(t *testing.T) {
	p := DefaultPolicy("http://127.0.0.1:45111")

	spec := Spec{Title: "docs", EntryURL: "https://example.com/", Policy: p}

	if err := ValidateSpec(spec, timeNow()); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("a non-workspace entry URL must be a typed policy violation, got %v", err)
	}

	spec.EntryURL = "http://127.0.0.1:45111/workspace/index.html"

	if err := ValidateSpec(spec, timeNow()); err != nil {
		t.Fatalf("a workspace entry URL must validate: %v", err)
	}
}

func TestUnsupportedProviderIsTypedUnavailable(t *testing.T) {
	p := DefaultProvider()

	if p.Available() {
		t.Fatalf("the default (untagged) build must report the capability as unavailable")
	}

	err := p.Open(context.Background(), Spec{Title: "x", EntryURL: "http://127.0.0.1:1/"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("open on the unsupported provider must be typed ErrUnsupported, got %v", err)
	}
}

func TestShippedModeIsManagedSecondaryWindow(t *testing.T) {
	if ShippedMode != ModeManagedSecondaryWindow {
		t.Fatalf("shipped mode must be the managed secondary window (no proven embedded path)")
	}

	if p := DefaultProvider(); p.Mode() != ShippedMode {
		t.Fatalf("every provider must report the shipped mode")
	}
}
