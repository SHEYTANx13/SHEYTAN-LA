// Package releasecontract — mobile_dependency_contract_test.go
//
// Run 44306343338 work order §4.1.6: the gomobile toolchain is resolved
// from the module graph (`go list -m golang.org/x/mobile`), so the pin
// in go.mod IS the Android/iOS tooling version. The graph silently
// regressing to the 2023-era pseudo-version pulls golang.org/x/tools
// v0.16.0, whose internal/tokeninternal fails to compile under Go
// 1.27.4 ("invalid array length -delta * delta") — the exact Android
// lane failure reproduced and repaired in this cycle. This contract
// makes that regression impossible to reintroduce silently.
package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mobileModulePin is the exact, newest-at-repair-time golang.org/x/mobile
// version verified through the module proxy (published 2026-11-17,
// GoVersion 1.26.0 — compatible with the pinned Go 1.27.4 toolchain;
// its graph resolves golang.org/x/tools v0.53.0). gomobile and gobind
// build successfully from it under Go 1.27.4 (verified locally during
// the repair).
const mobileModulePin = "v0.0.0-20261117202452-47699113848a"

// retiredMobileGraphVersion is the stale graph-resolved version that
// caused run 44306343338's Android failure.
const retiredMobileGraphVersion = "v0.0.0-20231127183840-76ac6878050a"

func TestMobileModuleGraphPin(t *testing.T) {
	root := findRepoRoot(t)

	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	mod := string(data)

	if !strings.Contains(mod, "golang.org/x/mobile "+mobileModulePin) {
		t.Fatalf("go.mod must pin golang.org/x/mobile at %s exactly — the gomobile toolchain version IS the module graph version, and any drift re-opens the x/tools v0.16.0 tokeninternal compile failure", mobileModulePin)
	}

	if strings.Contains(mod, retiredMobileGraphVersion) {
		t.Fatalf("go.mod still references the retired graph version %s — it pulls golang.org/x/tools v0.16.0 (broken under Go 1.27.4)", retiredMobileGraphVersion)
	}

	// The workflows must keep resolving gomobile from the module graph
	// (never @latest, never a floating pseudo-version spelling).
	for _, lane := range []string{"build-android.yml", "build-ios.yml"} {
		text, err := os.ReadFile(filepath.Join(root, ".github", "workflows", lane))
		if err != nil {
			t.Fatalf("read %s: %v", lane, err)
		}
		workflow := string(text)
		if !strings.Contains(workflow, `go list -m golang.org/x/mobile`) {
			t.Errorf("%s lost the module-graph gomobile resolution step", lane)
		}
		if strings.Contains(workflow, "gomobile@latest") || strings.Contains(workflow, "gobind@latest") {
			t.Errorf("%s installs the mobile toolchain with @latest — the version authority is the pinned module graph", lane)
		}
	}
}
