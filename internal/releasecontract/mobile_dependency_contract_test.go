// Package releasecontract — mobile_dependency_contract_test.go
//
// Run 44378562071 work order §4.1.6 (SUPERSEDES the run 44306343338
// module-graph resolution), as AMENDED on 2026-11-21 (runs
// 44663205295 + 44663205241): the Android entrypoint repair gave the
// product module a REAL x/mobile runtime import (cmd/sheytan-mobile/
// mobile_android_main.go imports golang.org/x/mobile/app + the
// event packages to bridge the NativeActivity glue to the go-gui
// Pattern-B Android backend), so go.mod now CARRIES the pin — at the
// SAME exact recorded version the lanes install the gomobile/gobind CLI
// tools from. The original hazard (an UNUSED pin drifting from the
// tidy-canonical state, silently pulling the 2023-era pseudo-version
// with its broken golang.org/x/tools v0.16.0, "invalid array length
// -delta * delta" under Go 1.27.4) is now guarded from BOTH sides: the
// pin must exist AND be genuinely imported, at the exact lane version —
// never @latest, never a moving branch, never a graph re-resolution.
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

	// 1. (Amended 2026-11-21) go.mod MUST carry the x/mobile pin at the
	// EXACT lane version, AND the import must be real: the Android
	// entrypoint imports the runtime, so an absent pin breaks the build
	// and an unused pin would re-open the run-44378562071 tidy drift.
	// Both sides of the invariant are pinned here; the tidy-canonical
	// state itself is checked against the live toolchain by
	// scripts/ci/module-integrity.sh.
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	mod := string(data)

	pinLine := "golang.org/x/mobile " + mobileToolchainPin
	if !strings.Contains(mod, pinLine) {
		t.Fatalf("go.mod must pin golang.org/x/mobile at the exact lane version %s — the Android entrypoint (cmd/sheytan-mobile/mobile_android_main.go) imports the runtime; a missing or drifting pin re-opens the run 44378562071 tidy drift", mobileToolchainPin)
	}

	entrypoint, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("cmd/sheytan-mobile/mobile_android_main.go")))
	if err != nil {
		t.Fatalf("read the Android entrypoint: %v", err)
	}
	if !strings.Contains(string(entrypoint), `"golang.org/x/mobile/app"`) {
		t.Fatalf("go.mod carries the x/mobile pin but cmd/sheytan-mobile/mobile_android_main.go no longer imports golang.org/x/mobile/app — an unused pin re-opens the run 44378562071 tidy drift; move the pin back into the lane scripts")
	}

	if strings.Contains(mod, retiredMobileGraphVersion) {
		t.Fatalf("go.mod still references the retired graph version %s — it pulls golang.org/x/tools v0.16.0 (broken under Go 1.27.4)", retiredMobileGraphVersion)
	}

	// 2. Both mobile lanes install the CLI tools at the EXACT recorded
	// pin (never @latest, never module-graph re-resolution). The CI
	// consolidation moved the lanes to the scripts/build/ dispatch
	// scripts.
	for _, lane := range []string{androidLaneRelPath} {
		text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(lane)))
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

		// (Amended 2026-11-21: the module IS in the graph now — the
		// entrypoint imports it — so the old "go list -m would fail"
		// prohibition no longer applies. The lane pin equality above is
		// the binding contract.)
	}
}
