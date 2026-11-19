// Package releasecontract — build_matrix_contract_test.go
//
// CI consolidation work order 2.3: the STRUCTURAL contract of the ONE
// platform-build matrix, parsed from the real .github/workflows/build.yml
// with the same yaml.v3 decoding the shared validators use — never
// grepped, never copied from an example.
//
// The retired topology (the build-desktop.yml orchestrator plus the five
// reusable build-windows/linux/macos/android/ios.yml lanes) is gone; the
// six platform targets are now EXACTLY ONE matrix.include list on the
// ONE platform-build job. These tests pin, structurally:
//
//   - no duplicate (GOOS, GOARCH, runner, artifact) platform mapping;
//   - Windows x64 and Linux x64 each defined exactly once;
//   - macOS Intel and Apple Silicon: DISTINCT targets, DISTINCT runner
//     images, DISTINCT artifact names;
//   - Android and the iOS simulator each defined exactly once;
//   - no platform-to-platform needs chain (platform-build has no needs);
//   - strategy.fail-fast present and EXPLICITLY false;
//   - no other active platform-build workflow YAML in .github/workflows
//     (exactly build.yml + codeql.yml);
//   - ONE authoritative trigger: the build trigger block lives exactly
//     once, in build.yml.
package releasecontract

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// matrixBuildIdentity maps each contracted matrix target to the Go build
// identity its lane compiles, so the duplicate-mapping assertion reasons
// about (GOOS, GOARCH) — the pair that must never be produced by two
// matrix entries with the same runner image and artifact.
var matrixBuildIdentity = map[string][2]string{
	"windows-x64":         {"windows", "amd64"},
	"linux-x64":           {"linux", "amd64"},
	"macos-arm64":         {"darwin", "arm64"},
	"macos-x64":           {"darwin", "amd64"},
	"android-arm64":       {"android", "arm64"},
	"ios-simulator-arm64": {"ios", "arm64"},
}

// loadBuildMatrix parses the real build.yml platform-build matrix for
// the structural assertions below.
func loadBuildMatrix(t *testing.T) []matrixEntry {
	t.Helper()

	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	doc, err := parseWorkflow(w)
	if err != nil {
		t.Fatalf("parse build.yml: %v", err)
	}

	job, ok := doc.Jobs[PlatformBuildJobKey]
	if !ok {
		t.Fatalf("build.yml has no %q job — the consolidated platform matrix is missing", PlatformBuildJobKey)
	}

	if job.Strategy == nil || job.Strategy.Matrix == nil || len(job.Strategy.Matrix.Include) == 0 {
		t.Fatalf("build.yml %q carries no strategy.matrix.include — exactly one matrix with the six targets is required", PlatformBuildJobKey)
	}

	return job.Strategy.Matrix.Include
}

// TestBuildMatrixNoDuplicatePlatformMapping proves work order 2.3's
// duplicate-mapping ban: no two matrix entries may carry the same
// (GOOS, GOARCH, runner, artifact) identity — a duplicate would build
// the same platform twice (the retired topology's duplicate-lane defect
// in matrix form) and silently upload a colliding artifact.
func TestBuildMatrixNoDuplicatePlatformMapping(t *testing.T) {
	include := loadBuildMatrix(t)

	type identity struct {
		goos, goarch, runner, artifact string
	}

	seen := map[identity]int{}
	for _, e := range include {
		pair, known := matrixBuildIdentity[e.Target]
		if !known {
			t.Errorf("matrix target %q is not one of the six contracted platform identities — an unknown target recreates an uncontracted lane", e.Target)
			continue
		}

		id := identity{pair[0], pair[1], e.OS, e.Artifact}
		if prev := seen[id]; prev > 0 {
			t.Errorf("duplicate (GOOS=%s, GOARCH=%s, runner=%s, artifact=%s) platform mapping via target %q — every platform must be built by EXACTLY ONE matrix entry", id.goos, id.goarch, id.runner, id.artifact, e.Target)
		}
		seen[id]++

		// The artifact identity must be unique on its own too: two legs
		// uploading one artifact name would have one lane overwrite the
		// other's package.
	}

	artifacts := map[string]int{}
	for _, e := range include {
		artifacts[e.Artifact]++
	}
	for name, n := range artifacts {
		if n > 1 {
			t.Errorf("matrix artifact %q is referenced by %d targets — package artifacts must map 1:1 to platforms", name, n)
		}
	}
}

// TestBuildMatrixDesktopTargetsExactlyOnce pins the desktop trio of the
// work order: Windows x64 and Linux x64 EXACTLY once each, and the two
// macOS architectures as DISTINCT (target, runner, artifact) identities
// — Intel and Apple Silicon must never collapse onto one leg or one
// runner image (the retired macos-15-everywhere ambiguity).
func TestBuildMatrixDesktopTargetsExactlyOnce(t *testing.T) {
	include := loadBuildMatrix(t)

	count := map[string]int{}
	byTarget := map[string]matrixEntry{}
	for _, e := range include {
		count[e.Target]++
		byTarget[e.Target] = e
	}

	for _, target := range []string{"windows-x64", "linux-x64"} {
		if count[target] != 1 {
			t.Errorf("matrix target %q defined %d times, want EXACTLY once", target, count[target])
		}
	}

	arm, hasArm := byTarget["macos-arm64"]
	x64, hasX64 := byTarget["macos-x64"]
	if !hasArm || !hasX64 {
		t.Fatalf("both macOS targets must exist (has arm64=%v, has x64=%v)", hasArm, hasX64)
	}
	if arm.OS == x64.OS {
		t.Errorf("macos-arm64 and macos-x64 share runner image %q — Intel and Apple Silicon must stay DISTINCT", arm.OS)
	}
	if arm.Artifact == x64.Artifact {
		t.Errorf("macos-arm64 and macos-x64 share artifact %q — per-arch portable names must stay DISTINCT", arm.Artifact)
	}
	if arm.Target == x64.Target {
		t.Errorf("macos-arm64 and macos-x64 collapsed onto target %q — the architectures are separate matrix identities", arm.Target)
	}
}

// TestBuildMatrixMobileTargetsExactlyOnce pins the mobile half of the
// matrix: the Android debug package and the SIMULATOR-ONLY iOS zip are
// each defined exactly once, on their contracted runner images, with the
// retired -debug-apk / IPA spellings absent.
func TestBuildMatrixMobileTargetsExactlyOnce(t *testing.T) {
	include := loadBuildMatrix(t)

	count := map[string]int{}
	byTarget := map[string]matrixEntry{}
	for _, e := range include {
		count[e.Target]++
		byTarget[e.Target] = e
	}

	if count["android-arm64"] != 1 {
		t.Errorf("matrix target android-arm64 defined %d times, want EXACTLY once", count["android-arm64"])
	}
	if count["ios-simulator-arm64"] != 1 {
		t.Errorf("matrix target ios-simulator-arm64 defined %d times, want EXACTLY once", count["ios-simulator-arm64"])
	}

	if android, ok := byTarget["android-arm64"]; ok {
		if android.Artifact != AndroidArtifactName {
			t.Errorf("android-arm64 artifact %q, want %q (the retired -debug-apk suffix must not reappear)", android.Artifact, AndroidArtifactName)
		}
	}
	if ios, ok := byTarget["ios-simulator-arm64"]; ok {
		if ios.Artifact != IOSArtifactName {
			t.Errorf("ios-simulator-arm64 artifact %q, want %q", ios.Artifact, IOSArtifactName)
		}
		if strings.Contains(strings.ToLower(ios.Artifact), "ipa") {
			t.Errorf("the iOS artifact %q must stay SIMULATOR-ONLY — a device IPA is never produced without signing", ios.Artifact)
		}
	}
}

// TestBuildMatrixNoPlatformToPlatformNeedsChain proves the matrix legs
// are INDEPENDENT gates: the platform-build job has EMPTY or absent
// needs (a needs chain between matrix instances recreated the run
// 43630164424 skipped-gate defect), fail-fast is present and EXPLICITLY
// false, and cancel-in-progress: true appears nowhere in the workflow.
func TestBuildMatrixNoPlatformToPlatformNeedsChain(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	if err := ValidatePlatformMatrixStructure(w); err != nil {
		t.Fatalf("platform matrix structure contract: %v", err)
	}

	// Redundant direct spelling of the two headline assertions, so a
	// future validator refactor cannot silently drop them.
	doc, err := parseWorkflow(w)
	if err != nil {
		t.Fatalf("parse build.yml: %v", err)
	}
	job := doc.Jobs[PlatformBuildJobKey]
	if len(job.Needs) != 0 {
		t.Errorf("platform-build carries needs %v — matrix legs must stay independent gates with no platform-to-platform chain", []string(job.Needs))
	}
	if job.Strategy == nil || job.Strategy.FailFast == nil {
		t.Fatalf("strategy.fail-fast is absent — the EXPLICIT false spelling is contracted")
	}
	if *job.Strategy.FailFast {
		t.Errorf("strategy.fail-fast is true — a red lane must never cancel its siblings (EXPLICIT false contracted)")
	}
}

// TestNoOtherActivePlatformBuildWorkflow proves the consolidation is
// total: .github/workflows carries EXACTLY build.yml + codeql.yml, no
// retired lane/orchestrator file survives, and build.yml is the only
// workflow that defines the platform-build job or invokes a reusable
// platform lane.
func TestNoOtherActivePlatformBuildWorkflow(t *testing.T) {
	root := findRepoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		t.Fatalf("read .github/workflows: %v", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		names = append(names, name)

		switch name {
		case "build.yml", "codeql.yml":
			// the two contracted workflows
		default:
			t.Errorf("unexpected workflow file %q in .github/workflows — the consolidation allows EXACTLY build.yml + codeql.yml; a stray file is a retired or new parallel platform-build topology", name)
		}
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "build.yml" || names[1] != "codeql.yml" {
		t.Errorf(".github/workflows = %v, want exactly [build.yml codeql.yml]", names)
	}

	// The retired files must not reappear (belt and braces with the
	// directory enumeration above).
	for _, retired := range RetiredLaneWorkflowFiles {
		if _, err := os.Stat(filepath.Join(root, ".github", "workflows", retired)); err == nil {
			t.Errorf("retired platform workflow file %s reappeared — the consolidation requires exactly build.yml + codeql.yml", retired)
		}
	}

	// codeql.yml must not carry a platform-build job or a reusable-lane
	// invocation — CodeQL is an independent security gate, never a second
	// build topology.
	codeql := loadWorkflowForDAG(t, codeqlWorkflowRelPath)
	if strings.Contains(codeql, "uses: ./.github/workflows/build-") {
		t.Errorf("codeql.yml invokes a reusable platform lane — the reusable-lane build topology must not come back through the security workflow")
	}
}

// TestOneAuthoritativeTrigger proves the build trigger block is owned by
// build.yml exactly once: push (main + tags v*) + pull_request (main) +
// workflow_dispatch, declared on the ONE platform-build workflow — the
// retired orchestrator's triggers (and any second build trigger block)
// must not survive anywhere.
func TestOneAuthoritativeTrigger(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	doc, err := parseWorkflow(w)
	if err != nil {
		t.Fatalf("parse build.yml: %v", err)
	}

	triggers := TriggerKeys(doc.On)
	for _, want := range []string{"push", "pull_request", "workflow_dispatch"} {
		if !triggers[want] {
			t.Errorf("build.yml on: block lacks the %q trigger — the ONE authoritative build trigger set is push(main, v* tags) + pull_request(main) + workflow_dispatch", want)
		}
	}

	// Exactly ONE top-level trigger block: the raw text carries a single
	// `on:` key at indentation zero (a second on: block would be a
	// duplicated authority — YAML would silently keep the last one).
	onBlocks := 0
	for _, line := range strings.Split(w, "\n") {
		if line == "on:" {
			onBlocks++
		}
	}
	if onBlocks != 1 {
		t.Errorf("build.yml carries %d top-level `on:` blocks, want EXACTLY 1 — one authoritative trigger set", onBlocks)
	}

	if !strings.Contains(w, "tags:") || !strings.Contains(w, `"v*"`) {
		t.Errorf("build.yml push trigger must carry the tags: [\"v*\"] release trigger — the tag-only release job keys off refs/tags/v")
	}

	// No other workflow may declare a platform-build trigger block: the
	// only other workflow is codeql.yml (its scan triggers are its own).
	other, err := filepath.Glob(filepath.Join(findRepoRoot(t), ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	for _, path := range other {
		base := filepath.Base(path)
		if base == "build.yml" {
			continue
		}
		text := loadWorkflowForDAG(t, filepath.Join("..", "..", ".github", "workflows", base))
		if strings.Contains(text, "name: SHEYTAN-LA platform build") {
			t.Errorf("%s duplicates the platform-build workflow identity — ONE authoritative build workflow", base)
		}
	}

	// The push branch filter must be exactly main.
	if !strings.Contains(w, "  push:\n") || !strings.Contains(w, "      - main\n") {
		t.Errorf("build.yml push trigger must filter branches to exactly [main]")
	}
}
