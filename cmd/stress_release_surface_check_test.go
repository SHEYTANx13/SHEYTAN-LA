//go:build headless

// Run 44306343338 repair evidence: the release-surface scenario of the
// stress suite is exercised HERE under the headless tag (the full stress
// scenario additionally compiles the Fyne UI and therefore runs on the
// hosted audit lane where the GL/X11 prerequisites exist). The check is
// pure text/YAML validation and runs identically in both configurations.
package cmd

import "testing"

func TestReleaseSurfaceContractLocalHeadless(t *testing.T) {
	if err := stressReleaseSurface(); err != nil {
		t.Fatalf("release-surface contract: %v", err)
	}
}
