//go:build headless

// Run 44306343338 repair evidence: the release-surface scenario of the
// stress suite is exercised HERE under the headless tag (the full stress
// scenario additionally compiles the Fyne UI and therefore runs on the
// hosted audit lane where the GL/X11 prerequisites exist). The check is
// pure text/YAML validation and runs identically in both configurations.
//
// Run 44547031698 repair: the package directory CWD made the scenario's
// repository-only preconditions silently true-by-absence
// (os.Stat(".gitignore") fails from cmd/ and the scenario returned nil
// before any check ran) — the hosted `go run ./cmd/sheytan-local-agent
// stress` path executes the FULL contract from the repository root while
// this test verified an empty prefix. The test now anchors itself at the
// module root first, so the headless lane exercises exactly the same
// assertions the hosted stress gate runs.
package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// moduleRoot walks up from the package directory to the directory that
// contains the canonical go.mod — the only CWD the release-surface
// contract is defined against.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s — the release-surface contract needs the module root", dir)
		}

		dir = parent
	}
}

func TestReleaseSurfaceContractLocalHeadless(t *testing.T) {
	root := moduleRoot(t)

	t.Chdir(root)

	if err := stressReleaseSurface(); err != nil {
		t.Fatalf("release-surface contract: %v", err)
	}
}
