package updater

// updater_noasset_test.go — v1.2.2: network failures and genuine
// "upstream ships no prebuilt asset" are DIFFERENT failure classes.
// Engine startup must never tell a GitHub-unreachable machine that "no
// prebuilt asset exists" (the misleading Windows log symptom).

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsNoAssetError(t *testing.T) {
	if IsNoAssetError(nil) {
		t.Fatalf("nil is not a no-asset error")
	}

	if !IsNoAssetError(errNoAsset) {
		t.Fatalf("errNoAsset must classify as no-asset")
	}

	// The Atom fallback's wording (only producible after the feed was
	// fetched successfully — network failures surface as HTTP errors).
	atomWording := fmt.Errorf(
		"no recent release ships a prebuilt asset for %s/%s",
		"windows", "amd64",
	)
	if !IsNoAssetError(atomWording) {
		t.Fatalf("atom no-asset wording must classify as no-asset: %v", atomWording)
	}

	// Network failures must NOT classify as no-asset.
	netErr := errors.New("dial tcp: lookup api.github.com: no such host")
	if IsNoAssetError(netErr) {
		t.Fatalf("a DNS failure is a network error, not a missing asset")
	}

	httpErr := fmt.Errorf("HTTP 403 from %s", "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=30")
	if IsNoAssetError(httpErr) {
		t.Fatalf("an HTTP 403 rate-limit is a network error, not a missing asset")
	}

	timeoutErr := errors.New("context deadline exceeded")
	if IsNoAssetError(timeoutErr) {
		t.Fatalf("a timeout is a network error, not a missing asset")
	}
}
