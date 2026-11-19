// Package releasecontract — mobile_dependency_contract_test.go
//
// Run 44378562071 work order §4.1.6 (SUPERSEDES the run 44306343338
// module-graph resolution): the product module imports NO x/mobile
// runtime package, so go mod tidy correctly removes any unused indirect
// pin from go.mod — keeping one there made the committed manifests drift
// from the toolchain-canonical state and failed the preflight module
// integrity gate (the recorded tidy-drift failure of run 44378562071).
// The gomobile/gobind CLI tools are instead installed by the Android and
// iOS lanes at THIS EXACT recorded version — never @latest, never a
// moving branch, never a graph re-resolution that could silently pull
// the 2023-era pseudo-version and its broken golang.org/x/tools
// v0.16.0 ("invalid array length -delta * delta" under Go 1.27.4).
package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mobileToolchainPin is the exact golang.org/x/mobile version the
// gomobile/gobind CLI tools are installed from in both mobile lanes
// (verified through the module proxy; GoVersion 1.26.0 — compatible with
// the pinned Go 1.27.4 toolchain; its graph resolves golang.org/x/tools
// v0.53.0. gomobile and gobind build successfully from it under Go
// 1.27.4).
const mobileToolchainPin = "v0.0.0-20261117202452-47699113848a"

// retiredMobileGraphVersion is the stale graph-resolved version that
// caused run 44306343338's Android failure.
const retiredMobileGraphVersion = "v0.0.0-20231127183840-76ac6878050a"

func TestMobileToolchainPin(t *testing.T) {
	root := findRepoRoot(t)

	// 1. The product module stays tidy-canonical: NO x/mobile pin in
	// go.mod. Nothing in the product graph imports it (run 44378562071
	// §4.1: the unused indirect pin WAS the committed tidy drift). If a
	// future change starts importing x/mobile runtime packages, go mod
	// tidy will keep the require line and THIS assertion — checked
	// against the live toolchain by scripts/ci/module-integrity.sh —
	// must be revisited alongside that import.
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	mod := string(data)

	if strings.Contains(mod, "golang.org/x/mobile ") {
		t.Fatalf("go.mod must NOT carry an unused golang.org/x/mobile pin — the product module imports no x/mobile runtime package and the unused indirect pin re-opens the committed tidy drift of run 44378562071 (install the CLI tools at the exact lane pin instead)")
	}

	if strings.Contains(mod, retiredMobileGraphVersion) {
		t.Fatalf("go.mod still references the retired graph version %s — it pulls golang.org/x/tools v0.16.0 (broken under Go 1.27.4)", retiredMobileGraphVersion)
	}

	// 2. Both mobile lanes install the CLI tools at the EXACT recorded
	// pin (never @latest, never module-graph re-resolution).
	for _, lane := range []string{"build-android.yml", "build-ios.yml"} {
		text, err := os.ReadFile(filepath.Join(root, ".github", "workflows", lane))
		if err != nil {
			t.Fatalf("read %s: %v", lane, err)
		}

		workflow := string(text)

		if !strings.Contains(workflow, `mobilePin="`+mobileToolchainPin+`"`) {
			t.Errorf("%s lost the exact mobile toolchain pin %s — a moving version re-opens the x/tools v0.16.0 tokeninternal compile failure", lane, mobileToolchainPin)
		}

		if !strings.Contains(workflow, `go install "golang.org/x/mobile/cmd/gomobile@${mobilePin}"`) ||
			!strings.Contains(workflow, `go install "golang.org/x/mobile/cmd/gobind@${mobilePin}"`) {
			t.Errorf("%s must install gomobile and gobind at the exact ${mobilePin} version", lane)
		}

		if strings.Contains(workflow, "gomobile@latest") || strings.Contains(workflow, "gobind@latest") {
			t.Errorf("%s installs the mobile toolchain with @latest — the version authority is the exact recorded pin", lane)
		}

		if strings.Contains(workflow, `go list -m golang.org/x/mobile`) {
			t.Errorf("%s still resolves the toolchain from the module graph — with the pin moved out of go.mod that lookup fails (module not in graph) and the old path re-opens the committed tidy drift", lane)
		}
	}
}
