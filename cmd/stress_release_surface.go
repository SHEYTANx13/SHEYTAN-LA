package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/aicontext"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/brand"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/memory"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/releasecontract"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/resources"
)

// goModToolchainVersion extracts the exact toolchain version from the
// canonical `toolchain goX.Y.Z` directive of go.mod — the ONE version
// constant the release surface is allowed to reference. Everything else
// (workflow GO_VERSION env, setup-go pins, the runtime building this
// gate) must agree with it; no copy of the version may be hard-coded in
// a gate or fixture (the run-#40 and run-43601915826 defect class).
func goModToolchainVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	m := regexp.MustCompile(`(?m)^\s*toolchain\s+go(\S+)\s*$`).FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("%s declares no `toolchain goX.Y.Z` directive", path)
	}
	return string(m[1]), nil
}

// --- release-surface scenarios ---
//
// These scenarios pin the current CI/desktop release surface so a workflow
// or packaging regression fails the stress gate instead of shipping:
// the pinned Go 1.27 toolchain, the Fyne-era Linux dependency set
// (gcc/pkg-config + the OpenGL/X11/Wayland development libraries the
// Fyne CGO build links, including libxxf86vm-dev, plus the Xvfb/xauth
// smoke tooling), the Windows resource/icon pipeline (gen-syso) and
// windowsgui subsystem, the packaged .bat launcher, portable ZIP
// packaging under the release-contract artifact names, collision-proof
// memory IDs, and Windows-safe log rotation. The Node/React/Vite and
// Qt/CMake/QML UI toolchains are RETIRED and their reintroduction is
// banned here (v1.11.0 one-Go-UI migration).
//
// v1.10.4 (run 38569081029): the Linux dependency pin still required the
// RETIRED first-generation webview-era GTK stack (libgtk-4-dev /
// libwebkitgtk-6.0-dev) that
// v1.10.1 removed from the workflow when the desktop became the native
// Qt client — the stress gate failed on every v1.10.1+ hosted run while
// the workflow itself was correct. The pin now tracks the CURRENT
// contract (unambiguous as of v1.11.0): the REQUIRED dependency set is
// today's Fyne-era set (one Go UI — see the v1.11.0 note above); the
// phrase "Qt-era" above names the era WHEN the GTK pin was dropped, not
// a requirement on Qt. Qt/CMake/QML and GTK/WebKit stacks are both
// RETIRED and BANNED.

// workflowHasAction reports whether any `uses:` line in the workflow
// references actionID with one of the accepted refs (its verified
// immutable commit SHA). The scan trims CRLF so the check is
// line-ending-proof on every runner.
func workflowHasAction(workflow, actionID string, acceptedRefs ...string) bool {
	for _, line := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))

		const usesKey = "uses: "

		if !strings.HasPrefix(trimmed, usesKey) {
			continue
		}

		ref := trimmed[len(usesKey):]

		if !strings.HasPrefix(ref, actionID+"@") {
			continue
		}

		version := ref[len(actionID)+1:]

		// A trailing annotation (`# v3 (immutable SHA)`) is a comment,
		// not part of the ref: cut at the first whitespace.
		if i := strings.IndexAny(version, " \t"); i >= 0 {
			version = version[:i]
		}

		for _, accepted := range acceptedRefs {
			if version == accepted {
				return true
			}
		}
	}

	return false
}

func stressReleaseSurface() error {
	// Release-surface floor: 1.1.2. This is a minimum rather than an exact
	// pin so future compatible point releases cannot silently downgrade
	// the release surface.
	if versionLessThan(config.AppVersion, "1.1.2") {
		return fmt.Errorf(
			"AppVersion = %q, want >= 1.1.2",
			config.AppVersion,
		)
	}

	if brand.SignedBy != "Parsa Tak" {
		return fmt.Errorf(
			"SignedBy = %q, want %q",
			brand.SignedBy,
			"Parsa Tak",
		)
	}

	if aicontext.ContextVersion < 10 {
		return fmt.Errorf(
			"ContextVersion = %d, want >= 10",
			aicontext.ContextVersion,
		)
	}

	// The repository-only checks apply only when running from a source
	// checkout. A packaged end-user binary does not normally contain these
	// source-control files beside it.
	if _, err := os.Stat(".gitignore"); err != nil {
		return nil
	}

	// 1. Critical source packages must physically exist in the repository.
	for _, p := range []string{
		"internal/sessions/sessions.go",
		"internal/sandbox/sandbox.go",
		"internal/releasegate/releasegate_test.go",
		"internal/desktop/desktop.go",
	} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf(
				"critical source %s missing from the tree: %v",
				p,
				err,
			)
		}
	}

	// 1b. v1.10.7 REPOSITORY SHAPE (R-A/R-B, operator specification,
	// two-sided like every gate here): the seven documentation files
	// live in docs/, the core entry set lives in
	// cmd/sheytan-local-agent/, the launcher script source lives in
	// packaging/, and the OLD root paths are GONE. The audit job's
	// shell check pins the same inventory; this Go-side pin keeps the
	// stress battery authoritative on a source checkout.
	for _, p := range []string{
		"docs/ARCHITECTURE.md",
		"docs/CONTRIBUTING.md",
		"docs/ROADMAP.md",
		"docs/UPDATE.md",
		"docs/agent.md",
		"docs/changelog.md",
		"docs/worklog.md",
		"cmd/sheytan-local-agent/main.go",
		"cmd/sheytan-local-agent/main_headless.go",
		"cmd/sheytan-local-agent/main_other.go",
		"cmd/sheytan-local-agent/main_stderr.go",
		"cmd/sheytan-local-agent/main_windows.go",
		"packaging/SHEYTAN-LA.bat",
	} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf(
				"v1.10.7 repository shape: %s missing from the tree: %v",
				p,
				err,
			)
		}
	}
	for _, p := range []string{
		"ARCHITECTURE.md",
		"CONTRIBUTING.md",
		"ROADMAP.md",
		"UPDATE.md",
		"agent.md",
		"changelog.md",
		"worklog.md",
		"main.go",
		"main_headless.go",
		"main_other.go",
		"main_stderr.go",
		"main_windows.go",
		"SHEYTAN-LA.bat",
	} {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf(
				"v1.10.7 repository shape: %s still at the repo root — it moved in the v1.10.7 reshape",
				p,
			)
		}
	}

	// 2. Runtime directories in .gitignore must be root-anchored.
	//
	// An unanchored "sessions/" pattern also matches internal/sessions/,
	// which was the release-integrity regression this gate was created to
	// catch.
	gi, err := os.ReadFile(".gitignore")
	if err != nil {
		return fmt.Errorf("read .gitignore: %v", err)
	}

	runtimeDirs := map[string]bool{
		"sessions/":        true,
		"sandbox/":         true,
		"data/":            true,
		"logs/":            true,
		"models/":          true,
		"workspace/":       true,
		"charts/":          true,
		"browser-profile/": true,
		"build/":           true,
		"dist-stage/":      true,
	}

	for i, raw := range strings.Split(string(gi), "\n") {
		line := strings.TrimSpace(raw)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if runtimeDirs[line] {
			return fmt.Errorf(
				".gitignore line %d: pattern %q is UNANCHORED — it can match source directories such as internal/%s. Prefix it with '/'",
				i+1,
				line,
				strings.TrimSuffix(line, "/"),
			)
		}
	}

	// 3. Current desktop CI release contract. The CI consolidation merged
	// the retired build-desktop.yml orchestrator + five reusable lanes
	// into the ONE platform-build workflow.
	workflowsDir := filepath.Join(".github", "workflows")

	workflowCandidates := []string{
		filepath.Join(workflowsDir, "build.yml"),
		filepath.Join(workflowsDir, "build.yaml"),
	}

	var workflowPath string
	for _, candidate := range workflowCandidates {
		if _, err := os.Stat(candidate); err == nil {
			workflowPath = candidate
			break
		}
	}

	if workflowPath == "" {
		return fmt.Errorf(
			"platform build workflow missing: expected .github/workflows/build.yml",
		)
	}

	wf, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("read workflow %q: %v", workflowPath, err)
	}

	w := string(wf)

	// CI consolidation: the platform build lanes live in the five
	// per-phase scripts under scripts/build/, dispatched by the
	// platform-build matrix job of build.yml. Platform-specific
	// release-surface anchors below are validated against the LANE
	// scripts, and the combined-text checks see the workflow + all five
	// lane scripts.
	lanePaths := []string{
		filepath.Join("scripts", "build", "windows.ps1"),
		filepath.Join("scripts", "build", "linux.sh"),
		filepath.Join("scripts", "build", "macos.sh"),
		filepath.Join("scripts", "build", "android.sh"),
		filepath.Join("scripts", "build", "ios.sh"),
	}
	laneTexts := make([]string, 0, len(lanePaths))
	for _, lp := range lanePaths {
		laneBytes, laneErr := os.ReadFile(lp)
		if laneErr != nil {
			return fmt.Errorf("read lane script %q: %v (the consolidated topology requires all five lane scripts)", lp, laneErr)
		}
		laneTexts = append(laneTexts, string(laneBytes))
	}
	winLane := laneTexts[0]
	allWorkflows := w + strings.Join(laneTexts, "\n")

	// 3a. The workflow must pin Go instead of floating on "stable".
	if strings.Contains(w, "go-version: 'stable'") ||
		strings.Contains(w, `go-version: stable`) {
		return fmt.Errorf(
			"CI toolchain floats on 'stable' — pin it to the exact go.mod toolchain release",
		)
	}

	// Both jobs consume the shared GO_VERSION env, so the pin itself
	// lives in the workflow-level env block.
	// v1.10.14-fixed (R1, run-#41): this check kept demanding the 1.26
	// pin after the workflow had correctly moved to the verified 1.27
	// line — run #40's stress gate died on exactly this stale literal
	// (46 pass / 1 fail, exit 1). The gate must follow the workflow
	// forward, never pin it to a superseded toolchain (the same rule
	// 3d's comment pins for the action set).
	// v1.11.1 (run 43601915826, work order §3.4): the same defect class
	// struck AGAIN one level deeper — the gate demanded the obsolete
	// `GO_VERSION: "1.27"` literal while the workflow, the go.mod
	// toolchain directive and the hosted runtime had all correctly
	// moved to the exact patch go1.27.4 (46 pass / 1 fail again). The
	// expected version is no longer hard-coded anywhere: it is DERIVED
	// from the canonical `toolchain` directive in go.mod, and this gate
	// requires exact parity with the workflow GO_VERSION env and with
	// the Go runtime actually building this gate. Exactly one version
	// constant exists — the toolchain directive.
	wantToolchain, err := goModToolchainVersion("go.mod")
	if err != nil {
		return fmt.Errorf("canonical toolchain directive: %v", err)
	}

	goVersionRef := regexp.MustCompile(`GO_VERSION:\s*"([^"]+)"`).FindStringSubmatch(w)
	if goVersionRef == nil {
		return fmt.Errorf("workflow env block missing the GO_VERSION pin")
	}
	if goVersionRef[1] != wantToolchain {
		return fmt.Errorf(
			"workflow GO_VERSION %q does not match the go.mod toolchain %q — exactly one canonical Go version must exist",
			goVersionRef[1], wantToolchain,
		)
	}
	if runtimeVersion := strings.TrimPrefix(runtime.Version(), "go"); runtimeVersion != wantToolchain {
		return fmt.Errorf(
			"this gate runs on go%s but the canonical toolchain is %q — run the release surface under the exact go.mod toolchain",
			runtimeVersion, wantToolchain,
		)
	}

	// 3b. All SIX target platforms must remain part of the release, in
	// the ONE platform-build matrix (the CI consolidation: exactly six
	// targets, unique (target, os, artifact) triples, fail-fast
	// EXPLICITLY false, windows-2025/ubuntu-26.04 pinned, macOS Intel and
	// Apple Silicon distinct, and the job's needs empty — every matrix
	// instance is an independent gate).
	if err := releasecontract.ValidatePlatformMatrixStructure(w); err != nil {
		return fmt.Errorf("platform matrix contract: %w", err)
	}

	if !strings.Contains(w, "runs-on: ${{ matrix.os }}") {
		return fmt.Errorf("the platform-build job must run on the matrix runner image (runs-on: ${{ matrix.os }})")
	}

	// v1.11.1 (run 42127523923, work order §4): the Linux x64 runner
	// contract is validated by the SHARED parser-based validator — the
	// same function the release-contract tests run — against the PARSED
	// matrix image of the linux-x64 target, the retired 24.04 image, and
	// the runtime OS identity guard. The stale ubuntu-24.04 literal once
	// caused the release_surface red: the workflow correctly migrated to
	// the pinned ubuntu-26.04 x64 runner with an explicit
	// ID=ubuntu/VERSION_ID=26.04/amd64 guard, while this gate kept
	// demanding the retired image (the gate-must-follow-the-current-
	// contract defect class of run-#40 and run-38569081029).
	if err := releasecontract.ValidateLinuxRunnerContract(w); err != nil {
		return fmt.Errorf("Linux build runner contract: %w", err)
	}

	// 3c. The desktop workflow builds from main and supports manual execution.
	if !strings.Contains(w, "branches:") ||
		!strings.Contains(w, "- main") {
		return fmt.Errorf(
			"CI main branch trigger is missing",
		)
	}

	if !strings.Contains(w, "workflow_dispatch:") {
		return fmt.Errorf(
			"workflow_dispatch trigger is missing",
		)
	}

	if strings.Contains(w, "- master") {
		return fmt.Errorf(
			"stale master branch trigger found in the desktop workflow",
		)
	}

	// 3d. Validate the action versions used by the current workflow.
	//
	// v1.1.2 upgraded every action off the Node.js-20 runtime that
	// GitHub deprecated (2025-09-19 changelog) — the runner logs for the
	// v1.1.1 era runs carried the "Node.js 20 is deprecated" warning on
	// checkout@v4 / setup-go@v5 / setup-node@v4. The stress gate must
	// follow the workflow forward, not pin it to the deprecated set.
	// v1.10.14-fixed (R3/D19, re-derived 2026-10-23): download-artifact
	// v8.0.2 (2026-10-07) is the verified latest major; the stale @v6
	// floors here would fail the gate on a correct workflow (the
	// run-#40 red's twin defect).
	// v1.11.1 policy reversal (2026-11-17, CodeQL run 43883548572,
	// alerts #185/#186): external action references are IMMUTABLE
	// 40-hex COMMIT PINS with a synced release-tag comment — the
	// shared validator below now REJECTS mutable tags/branch refs on
	// every external uses: line. Presence is still checked by PARSING
	// the uses: reference (the run-41901967798 defect class: literal
	// Contains spelling rejected a CORRECT workflow).
	for _, required := range []struct {
		id   string
		refs []string
	}{
		{id: "actions/checkout", refs: []string{"3d3c42e5aac5ba805825da76410c181273ba90b1"}},
		{id: "actions/setup-go", refs: []string{"b7ad1dad31e06c5925ef5d2fc7ad053ef454303e"}},
		{id: "actions/download-artifact", refs: []string{"9000827ccba6bdab643e8b6fd33ac0654aef8333"}},
		{id: "softprops/action-gh-release", refs: []string{"8a0cd651e5b2365bd640854a3fa5dfc70e4875f3"}},
		{id: "msys2/setup-msys2", refs: []string{"ec48f7c5447b3140e2b088413ae3a55687bccb6e"}},
	} {
		if !workflowHasAction(allWorkflows, required.id, required.refs...) {
			return fmt.Errorf(
				"workflow missing required action %s (accepted: the verified immutable commit SHA)",
				required.id,
			)
		}
	}

	// 3d-v2 (v1.11.1): the shared immutable-SHA action policy + upload
	// contract validators — 40-hex pins with synced version comments,
	// per-job/per-step scoping, exactly one consolidated log upload,
	// exact package payloads.
	if err := releasecontract.ValidateUploadArtifactPins(w); err != nil {
		return fmt.Errorf("action immutable-SHA policy: %w", err)
	}

	if err := releasecontract.ValidateUploadArtifactContract(w); err != nil {
		return fmt.Errorf("upload-artifact contract: %w", err)
	}

	// 3d-v3 (v1.11.1, run 43630164424 repair, work order §9.6): the
	// workflow FAILURE-RESILIENCE contracts, parsed from this same
	// workflow file — preflight must not gate the four independent
	// validation jobs, the collector and final verdict must be
	// failure-tolerant, and no required gate may hide behind
	// continue-on-error. The same validators run as Go contract tests
	// with scheduling-simulation fixtures in
	// internal/releasecontract/workflow_dag_contract_test.go.
	if err := releasecontract.ValidateIndependentGatesDAG(w); err != nil {
		return fmt.Errorf("independent-gates DAG contract: %w", err)
	}

	if err := releasecontract.ValidateFailureTolerantCollector(w); err != nil {
		return fmt.Errorf("failure-tolerant collector contract: %w", err)
	}

	if err := releasecontract.ValidateFinalVerdictJob(w); err != nil {
		return fmt.Errorf("final-verdict contract: %w", err)
	}

	// 3d-v4 (v1.11.1, run 43630164424 repair): the CodeQL workflow's
	// resilience contract — independent triggers, analyze after a
	// failed manual build when init succeeded, upload: always, and
	// the failure-tolerant CodeQL log collector.
	codeqlPath := filepath.Join(workflowsDir, "codeql.yml")
	codeqlYAML, codeqlErr := os.ReadFile(codeqlPath)
	if codeqlErr != nil {
		return fmt.Errorf("read CodeQL workflow %q: %v", codeqlPath, codeqlErr)
	}

	if err := releasecontract.ValidateCodeQLWorkflowResilience(string(codeqlYAML)); err != nil {
		return fmt.Errorf("CodeQL resilience contract: %w", err)
	}

	// 3e. The Node/frontend stack is RETIRED (v1.11.0 one-Go-UI): the
	// workflow must not reinstall it — no Node pin, no setup-node, no
	// npm/vite/web-static fragments. A reappearance means a second UI
	// implementation is trying to come back.
	for _, staleFrontend := range []string{
		"NODE_VERSION:",
		"actions/setup-node",
		"npm run typecheck",
		"npm run lint",
		"npm run build",
		"npm ci",
		"web/static/index.html",
		"sync:web",
	} {
		if strings.Contains(w, staleFrontend) {
			return fmt.Errorf(
				"retired frontend-era fragment %q found in the workflow — the one-Go-UI pipeline must not reinstall the Node/React/Vite stack",
				staleFrontend,
			)
		}
	}

	// 3e2. Canonical package-root variables (v1.2.1): the workflow must
	// define ONE root per platform and derive every staging dir, ZIP
	// name, ZIP entry check, artifact name and release-metadata
	// reference from them. This is the fix for run 34871838054 (Linux
	// ZIP verified against SHEYTAN-LA/ while it was created under
	// SHEYTAN-Local-Agent/): duplicated root literals can no longer
	// drift apart because the gate requires the canonical variables
	// AND the parameterized verification spellings (3h/3i below).
	for _, envLine := range []string{
		releasecontract.WorkflowWinRootEnvLine,
		releasecontract.WorkflowLinuxRootEnvLine,
	} {
		if !strings.Contains(w, envLine) {
			return fmt.Errorf(
				"workflow is missing the canonical package-root variable %q — packaging paths must derive from WIN_PKG_ROOT / LINUX_PKG_ROOT, not duplicated literals",
				envLine,
			)
		}
	}

	// 3f. THE CURRENT Linux CI dependency contract (v1.11.0 retarget):
	// the desktop host is the ONE Fyne client — its CGO build needs
	// gcc/pkg-config and the OpenGL/X11/Wayland development libraries
	// (libxxf86vm-dev included: its absence killed the run-#51 Fyne
	// spike's link step), and the desktop smoke needs Xvfb + xauth.
	// Requiring a retired package here made the stress gate fail hosted
	// runs while the workflow itself was correct (run 38569081029) — the
	// pin tracks the CURRENT contract, never a retired transport.
	for _, linuxDep := range []string{
		"gcc",
		"pkg-config",
		"libgl1-mesa-dev",
		"libegl1-mesa-dev",
		"libx11-dev",
		"libxcursor-dev",
		"libxrandr-dev",
		"libxinerama-dev",
		"libxi-dev",
		"libxxf86vm-dev",
		"libwayland-dev",
		"libxkbcommon-dev",
		"xvfb",
		"xauth",
	} {
		if !strings.Contains(w, linuxDep) {
			return fmt.Errorf(
				"Linux CI dependencies missing %q — the Fyne desktop build (CGO/X11/OpenGL/Wayland) and the Xvfb smoke need the documented prerequisite set",
				linuxDep,
			)
		}
	}

	// The whole retired webview-era GTK/WebKit stack is banned in BOTH
	// generations (second generation: libgtk-3-dev / libwebkit2gtk-4.1-dev;
	// third generation: libgtk-4-dev / libwebkitgtk-6.0-dev): reinstalling any of them
	// would reintroduce the second-desktop-shell ambiguity the v1.10.x
	// Qt migration removed.
	for _, staleDep := range []string{
		"libgtk-4-dev",
		"libwebkitgtk-6.0-dev",
		"libgtk-3-dev",
		"libwebkit2gtk-4.1-dev",
		"qt6-base-dev",
		"qtbase5-dev",
		"qtdeclarative5-dev",
		"qt6-declarative-dev",
		"libqt5webengine",
	} {
		if strings.Contains(w, staleDep) {
			return fmt.Errorf(
				"retired UI-toolchain dependency %q still installed by Linux CI — the desktop host is the ONE Fyne client; the GTK/WebKit/Qt stacks must not return",
				staleDep,
			)
		}
	}

	// 3g. The Windows build must carry the brand resources: gen-syso
	// (icon + Parsa Tak signature + DPI-aware manifest) and the
	// windowsgui subsystem flag (no console flash on double-click).
	if !strings.Contains(winLane, "go run ./scripts/gen-syso") {
		return fmt.Errorf(
			"Windows build lost the gen-syso resource step (icon + version info + DPI manifest)",
		)
	}

	if !strings.Contains(winLane, "-H=windowsgui") &&
		!strings.Contains(winLane, "-H windowsgui") {
		return fmt.Errorf(
			"Windows build lost the -H=windowsgui subsystem flag",
		)
	}

	// 3h. The workflow must build and validate the portable ZIPs and the
	// NSIS installer — with names that agree with the SINGLE release
	// artifact contract (internal/releasecontract). The check is
	// two-sided: the contracted names must appear in the workflow, AND
	// every `dist/…` artifact the workflow produces must be one the
	// contract recognizes. That detects future renaming drift in either
	// direction instead of pinning a second hard-coded copy of the
	// naming truth here.
	contract := releasecontract.For(config.AppVersion)

	// Master work order §10 (installer retirement): the portable ZIPs are
	// the ONLY supported packages; every retired installer spelling is a
	// forbidden fragment below.
	// Run 44306343338 §6.2/§6.3 consolidation: the package lifecycle
	// lives in the reusable LANES (one authoritative build path per
	// platform), so the fragments are validated against the combined
	// workflow set; the orchestrator only downloads and publishes.
	requiredWorkflowFragments := []string{
		contract.WindowsZipWorkflowSlot,
		contract.LinuxZipWorkflowSlot,
		// The staging directories must derive from the canonical
		// root variables (v1.2.1), not from repeated literals.
		releasecontract.WindowsAppDirWorkflowSlot,
		releasecontract.LinuxAppDirWorkflowSlot,
		"Create Windows ZIP",
		"Verify Windows ZIP",
		"Upload the portable ZIP",
		"Upload the portable archive",
	}

	for _, fragment := range requiredWorkflowFragments {
		if !strings.Contains(allWorkflows, fragment) {
			return fmt.Errorf(
				"desktop workflow missing release-package contract %q",
				fragment,
			)
		}
	}

	// Master work order §10 (installer retirement): the workflow must no
	// longer produce, reference or validate ANY installer artifact. A
	// reappearing installer path, NSIS script or makensis invocation is a
	// contract violation, never a supported packaging lane.
	for _, forbidden := range []string{
		"-windows-x64-installer.exe",
		"packaging/nsis",
		"makensis",
		"Install NSIS",
		"Build the NSIS installer",
		"NSIS install/uninstall smoke",
	} {
		if strings.Contains(w, forbidden) {
			return fmt.Errorf(
				"desktop workflow still references the retired installer surface %q — installer production is removed (master work order §10)",
				forbidden,
			)
		}
	}

	if err := checkWorkflowArtifactAgreement(allWorkflows, contract); err != nil {
		return err
	}

	// 3i. ZIPs must preserve the portable application folder structure.
	// The gate checks the PARAMETERIZED spellings (canonical root
	// variable + concrete leaf), so the verification steps in the
	// workflow demonstrably derive their expectations from the same
	// canonical variables that build the ZIP. The concrete entry
	// identities (what the sealed ZIP must actually contain) remain
	// pinned by the contract's *Entries methods and the Windows/Linux
	// build jobs' staging assertions.
	for _, fragment := range append(
		contract.RequiredWindowsZipWorkflowEntries(),
		contract.RequiredLinuxZipWorkflowEntries()...,
	) {
		if !strings.Contains(allWorkflows, fragment) {
			return fmt.Errorf(
				"portable ZIP contract missing %q",
				fragment,
			)
		}
	}

	// 3j. The workflow must verify the ONE UI and the core before the
	// packaging jobs, and the Windows package must ship the launcher
	// script under the contracted identity. v1.10.7: the workflow must
	// also build the core from its relocated entry package
	// (cmd/sheytan-local-agent/). v1.11.0: the UI verification IS the Go
	// UI suite (no frontend toolchain exists anymore).
	for _, fragment := range []string{
		"go test ./ui/fyne/",
		"go test ./internal/... -tags headless",
		"go test ./internal/releasecontract/",
		"Xvfb",
		releasecontract.LauncherScript,
		"./cmd/sheytan-local-agent",
		"packaging/SHEYTAN-LA.bat",
	} {
		if !strings.Contains(allWorkflows, fragment) {
			return fmt.Errorf(
				"UI/package release gate missing %q",
				fragment,
			)
		}
	}

	// 3k. The workflow's APP_VERSION must derive from the ONE Go
	// authority (internal/config.AppVersion) through the CGO-free
	// resolver cmd/printversion (--plain / --json). Run #54
	// (40394318382) proved that resolving the version through the full
	// agent binary is load-bearing fragile: `go run
	// ./cmd/sheytan-local-agent version` compiles ui/fyne (CGO), so on
	// a fresh runner without the graphics development headers the
	// resolution step — and every step that shares that pattern — dies
	// before printing anything. The shell must not re-parse the
	// presentation format either: no `| head -1 |` pipelines and no sed
	// extraction of the human-readable banner. The retired
	// package.json / release-version.mjs authorities must not return.
	// The version resolution lives in the reusable LANES after the §6.2
	// consolidation (both desktop lanes resolve and cross-check it).
	if !strings.Contains(allWorkflows, "go run ./cmd/printversion --plain") {
		return fmt.Errorf(
			"workflow does not resolve APP_VERSION from the Go authority via the CGO-free resolver (expected `go run ./cmd/printversion --plain` with CGO_ENABLED=0)",
		)
	}
	for _, staleParse := range []string{
		"version | head -1 | sed",
		"| head -1 | sed -n",
	} {
		if strings.Contains(allWorkflows, staleParse) {
			return fmt.Errorf(
				"workflow re-parses the agent's presentation output with %q — resolve the version via cmd/printversion --plain (machine-readable, fail-closed) instead of scraping the banner",
				staleParse,
			)
		}
	}
	if strings.Contains(w, "release-version.mjs") ||
		strings.Contains(w, "package.json") {
		return fmt.Errorf(
			"workflow still references the retired Node version authority (release-version.mjs / package.json)",
		)
	}

	// 3l. Headless audit surface (run #54, 40394318382): the audit job
	// is the HEADLESS core gate and its runner never installs the
	// Fyne/CGO graphics prerequisites — the stress gate there MUST run
	// under the `headless` build tag so it compiles without ui/fyne.
	// The untagged invocation on a fresh Linux runner is exactly how
	// run #54's "Stress + release-surface gate" step died in ~1s (cgo
	// glfw compile: missing graphics headers), while the same suite
	// passed 47/47 on a host that had the headers.
	if !strings.Contains(w, "go run -tags headless ./cmd/sheytan-local-agent stress") {
		return fmt.Errorf(
			"audit job must run the stress gate headless (expected `go run -tags headless ./cmd/sheytan-local-agent stress`) — the audit runner has no Fyne/CGO graphics prerequisites and the desktop build would fail to compile there (run #54 root cause)",
		)
	}

	// 3m. Windows toolchain environment (msys2 October 2026 news):
	// MINGW64 is deprecated and its leaf packages are being dropped —
	// the workflow must provision UCRT64 with the ucrt package set and
	// the /ucrt64/bin PATH, and must not regress to the MINGW64
	// spellings.
	for _, ucrt := range []string{
		"msystem: UCRT64",
		"mingw-w64-ucrt-x86_64-gcc",
		"/ucrt64/bin",
		"C:\\msys64\\ucrt64\\bin",
	} {
		if !strings.Contains(winLane, ucrt) {
			return fmt.Errorf(
				"Windows CI must provision the UCRT64 toolchain (missing %q) — MINGW64 is deprecated per msys2's October 2026 announcement",
				ucrt,
			)
		}
	}
	for _, mingw64 := range []string{
		"msystem: MINGW64",
		"mingw-w64-x86_64-",
		"/mingw64/bin",
		"C:\\msys64\\mingw64\\bin",
	} {
		if strings.Contains(winLane, mingw64) {
			return fmt.Errorf(
				"deprecated MINGW64 fragment %q still in the workflow — migrate to UCRT64 (mingw-w64-ucrt-x86_64-*, /ucrt64/bin) per msys2's October 2026 deprecation",
				mingw64,
			)
		}
	}

	// 3n. Consolidated run-log contract (run #54 defect: the collector
	// merged all per-job log parts into ONE flat directory, so
	// identically named logs — gate-order.txt — overwrote each other
	// before concatenation, and if-no-files-found: ignore could drop
	// the mandatory artifact silently).
	// v1.11.1 single-artifact rework (run #41548925352), updated by the
	// run 44306343338 §7 contract: the per-job log parts, their
	// run-id-suffixed consolidated name and the verbose all-logs artifact
	// are RETIRED — the collector pulls every job's log from the paginated
	// jobs API and uploads the ONE errors/warnings-only plain-text
	// diagnostic named by releasecontract.ConsolidatedLogArtifactName
	// (archive:false); the attempt number lives ONLY inside the report
	// (CONSOLIDATE_ATTEMPT). The retired name is BANNED by the
	// artifact-naming contract test, so a stale anchor here contradicted
	// the release contract and failed every correct workflow. The anchors
	// below ARE the current contract, shared with internal/releasecontract.
	// Run 44306343338 §7: the consolidated artifact is the
	// errors/warnings-only plain-text diagnostic; the verbose all-logs
	// artifact and its "===" decorated section markers are retired. The
	// anchor uses the SHARED releasecontract constant so this gate and
	// the contract tests can never drift apart again.
	for _, consolidation := range []string{
		releasecontract.ConsolidatedLogArtifactName,
		"CONSOLIDATE_ATTEMPT: ${{ github.run_attempt }}",
		"scripts/ci/consolidate-run-log.sh",
	} {
		if !strings.Contains(w, consolidation) {
			return fmt.Errorf(
				"consolidated run-log contract missing %q — the one-file log must be stable, complete and uploaded on red runs too",
				consolidation,
			)
		}
	}

	// The report-format markers are owned by the COLLECTOR SCRIPT the
	// workflow invokes (v1.11.1 rework moved them out of the workflow
	// text) — pin them against the script file, not the workflow.
	collector, err := os.ReadFile("scripts/ci/consolidate-run-log.sh")
	if err != nil {
		return fmt.Errorf("read collector script: %v", err)
	}

	for _, marker := range []string{
		// Run 44306343338 §7: the report's simple headings carry no
		// decorative separators; the missing-source heading is the
		// plain MISSSING LOG SOURCES section (grep the script for it).
		"MISSING LOG SOURCES",
		// Run 44306343338 §7 headings: simple names, no separators. The
		// error/test-failure classifiers are pinned by their function
		// names instead of decorative heading spellings.
		"classify_job_log",
	} {
		if !strings.Contains(string(collector), marker) {
			return fmt.Errorf(
				"scripts/ci/consolidate-run-log.sh must keep emitting the report marker %q — the consolidated report must name missing sources and errors",
				marker,
			)
		}
	}
	if strings.Contains(w, "merge-multiple: true") {
		return fmt.Errorf(
			"log-part download uses merge-multiple: true — identically named logs from different jobs (gate-order.txt) overwrite each other before consolidation",
		)
	}

	return nil
}

// checkWorkflowArtifactAgreement derives the artifact names the workflow
// ACTUALLY produces and compares them against the release contract. The
// generic regexes make no assumption about the current naming: if a future
// release renames the artifacts in the workflow, this check fails until the
// single contract (internal/releasecontract) is updated with it — the exact
// drift that broke the v1.2.0 stress gate is now structurally impossible to
// reintroduce silently.
func checkWorkflowArtifactAgreement(
	workflow string,
	contract releasecontract.Contract,
) error {
	// Every artifact path the workflow writes under dist/ (PowerShell
	// `${env:APP_VERSION}`, bash `${APP_VERSION}` and `${{ env.APP_VERSION }}`
	// spellings are all captured by the placeholder wildcard).
	artifactRe := regexp.MustCompile(`dist/[A-Za-z0-9._${}(){}:/-]+?\.(?:zip|exe|msix|tar\.gz)`)

	found := map[string]bool{}
	for _, m := range artifactRe.FindAllString(workflow, -1) {
		found[m] = true
	}

	if len(found) == 0 {
		return fmt.Errorf(
			"desktop workflow produces no dist/ artifacts — packaging contract lost",
		)
	}

	// The two contracted artifacts must appear somewhere in the
	// workflow's dist/ output set (any version-slot spelling counts);
	// the installer artifact is RETIRED (master work order §10) and its
	// reappearing shape is a stale-remnant contract violation.
	contracted := []string{
		contract.WindowsZipWorkflowSlot,
		contract.LinuxZipWorkflowSlot,
	}

	for _, want := range contracted {
		matched := false

		for got := range found {
			if artifactNameMatchesSlot(got, want) {
				matched = true
				break
			}
		}

		if !matched {
			return fmt.Errorf(
				"workflow artifacts %v disagree with the release contract %q — update internal/releasecontract and the workflow together",
				sortedKeys(found),
				want,
			)
		}
	}

	// No stale legacy naming may survive: any dist/ artifact still
	// carrying the retired v1.1.x Windows identity means two competing
	// contracts exist in the tree again, and any retired installer
	// artifact means the §10 installer retirement regressed.
	for got := range found {
		if strings.Contains(got, "SHEYTAN-Local-Agent-Windows-x64") {
			return fmt.Errorf(
				"stale legacy Windows artifact %q still produced by the workflow — the v1.2.0 contract renamed it to %q",
				got,
				contract.WindowsZipWorkflowSlot,
			)
		}
		if strings.Contains(got, "-installer.exe") {
			return fmt.Errorf(
				"stale installer artifact %q still produced by the workflow — installer production is retired (master work order §10)",
				got,
			)
		}
	}

	// The staging directories must match the contracted app roots via
	// the canonical root variables: the portable ZIP builds from them
	// (v1.2.1 — slot spellings, so a
	// duplicated literal no longer satisfies this check).
	// The staging slots live in the reusable LANES after the §6.2
	// consolidation; the orchestrator stages nothing itself.
	laneIdx := strings.Index(workflow, "jobs:")
	_ = laneIdx
	for _, pair := range []struct{ lane, slot string }{
		{"build-windows.yml", releasecontract.WindowsAppDirWorkflowSlot},
		{"build-linux.yml", releasecontract.LinuxAppDirWorkflowSlot},
	} {
		if !strings.Contains(workflow, pair.slot) {
			return fmt.Errorf(
				"workflow no longer stages the application at %q — packaging identity drifted",
				pair.slot,
			)
		}
	}

	return nil
}

// artifactNameMatchesSlot reports whether a workflow artifact path is the
// given contract slot with any version placeholder spelling.
func artifactNameMatchesSlot(got, slot string) bool {
	// Strip the dist/ staging prefix — the contract names are bare
	// file names, the workflow references them under dist/.
	got = strings.TrimPrefix(got, "dist/")

	// Normalize both sides around the version placeholder: replace the
	// version slot with a wildcard and compare the fixed skeleton.
	skeleton := slot

	for _, ph := range []string{
		"${env:APP_VERSION}",
		"${APP_VERSION}",
		"${{ env.APP_VERSION }}",
	} {
		skeleton = strings.ReplaceAll(skeleton, ph, "*VERSION*")
	}

	gotSkeleton := got

	for _, ph := range []string{
		"${env:APP_VERSION}",
		"${APP_VERSION}",
		"${{ env.APP_VERSION }}",
	} {
		gotSkeleton = strings.ReplaceAll(gotSkeleton, ph, "*VERSION*")
	}

	if gotSkeleton == skeleton {
		return true
	}

	// Fall back: the slot with any placeholder content collapsed.
	collapse := func(s string) string {
		re := regexp.MustCompile(`\$\{[^}]*\}`)
		return re.ReplaceAllString(s, "*VERSION*")
	}

	return collapse(got) == collapse(slot)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}

	return out
}

// versionLessThan reports whether semantic version a < b.
//
// The comparison intentionally uses three numeric dot-separated components.
// Missing components count as zero, and a leading "v" is ignored.
func versionLessThan(a, b string) bool {
	pa, pb := parseVersionParts(a), parseVersionParts(b)

	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}

	return false
}

func parseVersionParts(v string) [3]int {
	var out [3]int

	v = strings.TrimPrefix(strings.TrimSpace(v), "v")

	for i, part := range strings.SplitN(v, ".", 3) {
		n := 0

		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}

			n = n*10 + int(r-'0')
		}

		out[i] = n
	}

	return out
}

// tTempDir creates a scratch directory for stress scenarios.
func tTempDir(name string) string {
	dir, err := os.MkdirTemp("", "sheytan-"+name+"-*")
	if err != nil {
		return name
	}
	return dir
}

// stressMemoryUniqueIDs: rapid Appends must produce distinct IDs even
// when the OS clock returns the same instant for consecutive calls (Windows
// granularity), and DeleteByID must remove EXACTLY one entry.
//
// The old timestamp-only scheme made two entries share an ID so one
// DeleteByID wiped both.
func stressMemoryUniqueIDs() error {
	dir := tTempDir("mem-unique-ids")
	defer os.RemoveAll(dir)

	st := memory.New(filepath.Join(dir, "mem.jsonl"))

	const n = 300

	for i := 0; i < n; i++ {
		if err := st.Append(
			[]string{"stress"},
			fmt.Sprintf("rapid entry %d", i),
			"stress",
		); err != nil {
			return fmt.Errorf("append %d: %v", i, err)
		}
	}

	all, err := st.All()
	if err != nil {
		return err
	}

	if len(all) != n {
		return fmt.Errorf(
			"got %d entries, want %d",
			len(all),
			n,
		)
	}

	seen := make(map[string]bool, n)

	for _, e := range all {
		if seen[e.ID] {
			return fmt.Errorf(
				"duplicate memory ID %q after %d rapid appends",
				e.ID,
				n,
			)
		}

		seen[e.ID] = true
	}

	if err := st.DeleteByID(all[0].ID); err != nil {
		return err
	}

	if got := st.Count(); got != n-1 {
		return fmt.Errorf(
			"delete removed %d entries, want exactly 1",
			n-got,
		)
	}

	return nil
}

// stressTrimLogsRotate: an over-budget log folder must actually shrink
// (bytes freed > 0), the rotated file must end at a line boundary, and no
// .rot temp files may be left behind.
//
// The rotateTail implementation once held the source file open across the rename, which
// Windows refuses — TrimLogs previously swallowed the error and freed
// nothing.
func stressTrimLogsRotate() error {
	dir := tTempDir("logs-rotate")
	defer os.RemoveAll(dir)

	// ~6.7 MB single log (64-byte lines) against a 1 MB budget.
	var b strings.Builder
	line := strings.Repeat("g", 63) + "\n"

	for i := 0; i < 105_000; i++ {
		b.WriteString(line)
	}

	logPath := filepath.Join(dir, "app.log")

	if err := os.WriteFile(
		logPath,
		[]byte(b.String()),
		0o644,
	); err != nil {
		return err
	}

	freed := resources.TrimLogs(dir, 1)

	if freed <= 0 {
		return fmt.Errorf(
			"TrimLogs freed %d bytes on an over-budget folder, want > 0",
			freed,
		)
	}

	fi, err := os.Stat(logPath)
	if err != nil {
		return err
	}

	if fi.Size() > 2<<20 {
		return fmt.Errorf(
			"log still %d bytes after trim",
			fi.Size(),
		)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}

	if len(data) > 0 && data[len(data)-1] != '\n' {
		return fmt.Errorf(
			"rotated log must end at a line boundary",
		)
	}

	if _, err := os.Stat(logPath + ".rot"); err == nil {
		return fmt.Errorf(
			"rotate left a .rot temp file behind",
		)
	}

	return nil
}
