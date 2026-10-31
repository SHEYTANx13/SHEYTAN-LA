package updater

// v1.11.0 (run #119 session): the release-manifest HOST pin.
//
// AppManifestURL previously hardcoded the Go MODULE-path repo
// (github.com/SHEYTANx13/SHEYTAN-LA) as the manifest host. That
// repo is a frozen v1.10.1 mirror of the Qt-era tree: zero tags, zero
// releases — an in-app update check against it can never find anything.
// Releases (and the release-manifest.json asset the release job
// generates from github.repository) live on the CODE repo
// github.com/SHEYTANx13/SHEYTAN-LA. This test pins the host/repo and the
// release-download shape so the host cannot drift silently again.

import (
	"strings"
	"testing"
)

func TestAppManifestURLHostIsCodeRepo(t *testing.T) {
	const wantRepo = "github.com/SHEYTANx13/SHEYTAN-LA"
	if !strings.Contains(AppManifestURL, wantRepo) {
		t.Fatalf("AppManifestURL does not point at the code repo %q: %q", wantRepo, AppManifestURL)
	}
}

func TestAppManifestURLDoesNotPointAtModulePathMirror(t *testing.T) {
	// The MODULE path (go.mod line 1) is NOT a release host. Its repo is
	// a frozen mirror with no releases; pin the absence explicitly.
	if strings.Contains(AppManifestURL, "Parsaetak/SHEYTAN-local-agent/releases") {
		t.Fatalf("AppManifestURL points at the frozen module-path mirror (no releases live there): %q", AppManifestURL)
	}
}

func TestAppManifestURLReleaseDownloadShape(t *testing.T) {
	// Same URL shape the release job's manifest publishing relies on:
	// <repo>/releases/latest/download/<asset>. Schema untouched — only
	// the host was wrong.
	const want = "https://github.com/SHEYTANx13/SHEYTAN-LA/releases/latest/download/release-manifest.json"
	if AppManifestURL != want {
		t.Fatalf("AppManifestURL = %q, want %q (exact release-download shape for release-manifest.json)", AppManifestURL, want)
	}
}
