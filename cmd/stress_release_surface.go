package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/aicontext"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/memory"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/releasecontract"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/resources"
)

// --- release-surface scenarios ---
//
// These scenarios pin the current CI/desktop release surface so a workflow
// or packaging regression fails the stress gate instead of shipping:
// the pinned Node 24 + Go toolchains, the Qt-era Linux X11/xcb dependency
// set (X11 headers for the smoke's closewin.c build, the xcb runtime
// pieces the Qt xcb plugin loads, the REAL window manager and the
// Xvfb/xdotool/x11-utils smoke tools), the Windows resource/icon pipeline
// (gen-syso) and windowsgui subsystem, the packaged .bat launcher,
// portable ZIP packaging under the release-contract artifact names,
// collision-proof memory IDs, and Windows-safe log rotation.
//
// v1.10.4 (run 38569081029): the Linux dependency pin still required the
// RETIRED Wails-era GTK stack (libgtk-4-dev / libwebkitgtk-6.0-dev) that
// v1.10.1 removed from the workflow when the desktop became the native
// Qt client — the stress gate failed on every v1.10.1+ hosted run while
// the workflow itself was correct. The pin now tracks the CURRENT
// contract: the Qt-era dependency set is REQUIRED, the whole retired
// GTK/WebKit stack is BANNED.

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

	// 3. Current desktop CI release contract.
	workflowsDir := filepath.Join(".github", "workflows")

	workflowCandidates := []string{
		filepath.Join(workflowsDir, "build-desktop.yml"),
		filepath.Join(workflowsDir, "build-desktop.yaml"),
	}

	var workflowPath string
	for _, candidate := range workflowCandidates {
		if _, err := os.Stat(candidate); err == nil {
			workflowPath = candidate
			break
		}
	}

	if workflowPath == "" {
		matches, globErr := filepath.Glob(filepath.Join(workflowsDir, "build-*.*ml"))
		if globErr != nil {
			return fmt.Errorf("discover desktop workflow: %v", globErr)
		}

		if len(matches) == 1 {
			workflowPath = matches[0]
		}
	}

	if workflowPath == "" {
		return fmt.Errorf(
			"desktop workflow missing: expected .github/workflows/build-desktop.yml",
		)
	}

	wf, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("read workflow %q: %v", workflowPath, err)
	}

	w := string(wf)

	// 3a. The workflow must pin Go instead of floating on "stable".
	if strings.Contains(w, "go-version: 'stable'") ||
		strings.Contains(w, `go-version: stable`) {
		return fmt.Errorf(
			"CI toolchain floats on 'stable' — pin it to 1.27",
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
	if !strings.Contains(w, `GO_VERSION: "1.27"`) {
		return fmt.Errorf(
			"CI toolchain not pinned to 1.27 (expected GO_VERSION: \"1.27\" in the workflow env block)",
		)
	}

	// 3b. Both target platforms must remain part of the desktop release.
	if !strings.Contains(w, "name: Windows x64") {
		return fmt.Errorf("Windows x64 build job is missing")
	}

	if !strings.Contains(w, "name: Linux x64") {
		return fmt.Errorf("Linux x64 build job is missing")
	}

	if !strings.Contains(w, "runs-on: windows-latest") {
		return fmt.Errorf("Windows build lost its windows-latest runner")
	}

	if !strings.Contains(w, "runs-on: ubuntu-24.04") {
		return fmt.Errorf("Linux build lost its ubuntu-24.04 runner")
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
	// v1.10.14-fixed (R3/D19, re-derived 2026-10-23): upload-artifact
	// v7.0.2 and download-artifact v8.0.2 (2026-10-07) are the verified
	// latest majors; the stale @v6/@v7 floors here would fail the gate
	// on a correct workflow (the run-#40 red's twin defect).
	for _, action := range []string{
		"actions/checkout@v7",
		"actions/setup-go@v7",
		"actions/setup-node@v7",
		"actions/upload-artifact@v7",
		"actions/download-artifact@v8",
		"softprops/action-gh-release@v3",
	} {
		if !strings.Contains(w, action) {
			return fmt.Errorf(
				"workflow missing required action %s",
				action,
			)
		}
	}

	// 3e. The Node toolchain is PINNED (v1.1.1): the new React/Vite UI
	// must never depend on whatever Node version the hosted runner
	// happens to ship. Since v1.1.2 the pin lives once in the
	// workflow-level env block (NODE_VERSION: "24") and every
	// setup-node step consumes it via ${{ env.NODE_VERSION }} — the same
	// single-source-of-truth pattern GO_VERSION already uses.
	if !strings.Contains(w, `NODE_VERSION: "24"`) {
		return fmt.Errorf(
			"CI does not pin Node 24 for the frontend build (expected NODE_VERSION: \"24\" in the workflow env block)",
		)
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

	// 3f. THE CURRENT Linux CI dependency contract (v1.10.4 retarget of
	// the v1.1.1 gate): the desktop host is the NATIVE Qt Quick client
	// over localipc — the Wails-era GTK/WebKit development stack is
	// RETIRED (v1.10.1) and must never return. The workflow must install
	// what the current pipeline actually requires: the xcb runtime pieces
	// the Qt xcb plugin loads, the X11 headers the smoke needs to build
	// scripts/closewin.c with cc -lX11 (v1.10.3 R3), a REAL window
	// manager for the G3/G5 geometry gates (openbox — no WM-less GUI
	// claims), and the Xvfb/xdotool/x11-utils smoke tooling. Requiring a
	// retired package here made the stress gate fail every v1.10.1+
	// hosted run (run 38569081029, step 18) while the workflow itself was
	// correct — the "stress scenario assumes a retired transport" class.
	for _, linuxDep := range []string{
		"libx11-dev",
		"libxkbcommon-x11-0",
		"libxcb-cursor0",
		"libxcb-icccm4",
		"libxcb-keysyms1",
		"libxcb-image0",
		"libxcb-render-util0",
		"openbox",
		"xvfb",
		"xdotool",
		"x11-utils",
	} {
		if !strings.Contains(w, linuxDep) {
			return fmt.Errorf(
				"Linux CI dependencies missing %q — the Qt desktop smoke (G1–G5) needs the X11/xcb stack, a REAL window manager and the smoke tools",
				linuxDep,
			)
		}
	}

	// The whole retired Wails-era GTK/WebKit stack is banned in BOTH
	// generations (v2: libgtk-3-dev / libwebkit2gtk-4.1-dev; v3:
	// libgtk-4-dev / libwebkitgtk-6.0-dev): reinstalling any of them
	// would reintroduce the second-desktop-shell ambiguity the v1.10.x
	// Qt migration removed.
	for _, staleDep := range []string{
		"libgtk-4-dev",
		"libwebkitgtk-6.0-dev",
		"libgtk-3-dev",
		"libwebkit2gtk-4.1-dev",
	} {
		if strings.Contains(w, staleDep) {
			return fmt.Errorf(
				"retired Wails-era dependency %q still installed by Linux CI — the desktop host is the native Qt client; the GTK/WebKit stack must not return",
				staleDep,
			)
		}
	}

	// 3g. The Windows build must carry the brand resources: gen-syso
	// (icon + Parsa Tak signature + DPI-aware manifest) and the
	// windowsgui subsystem flag (no console flash on double-click).
	if !strings.Contains(w, "go run ./scripts/gen-syso") {
		return fmt.Errorf(
			"Windows build lost the gen-syso resource step (icon + version info + DPI manifest)",
		)
	}

	if !strings.Contains(w, "-H=windowsgui") &&
		!strings.Contains(w, "-H windowsgui") {
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

	requiredWorkflowFragments := []string{
		contract.WindowsZipWorkflowSlot,
		contract.LinuxZipWorkflowSlot,
		contract.WindowsInstallerSlot,
		// The staging directories must derive from the canonical
		// root variables (v1.2.1), not from repeated literals.
		releasecontract.WindowsAppDirWorkflowSlot,
		releasecontract.LinuxAppDirWorkflowSlot,
		"Create Windows ZIP",
		"Create Linux ZIP",
		"Verify Windows ZIP",
		"Verify Linux ZIP",
		// Renamed in v1.1.2 when download-artifact moved to the
		// tag-agnostic release job; the artifact-upload steps are
		// "Upload Windows package" / "Upload Linux package".
		"Upload Windows package",
		"Upload Linux package",
	}

	for _, fragment := range requiredWorkflowFragments {
		if !strings.Contains(w, fragment) {
			return fmt.Errorf(
				"desktop workflow missing release-package contract %q",
				fragment,
			)
		}
	}

	if err := checkWorkflowArtifactAgreement(w, contract); err != nil {
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
		if !strings.Contains(w, fragment) {
			return fmt.Errorf(
				"portable ZIP contract missing %q",
				fragment,
			)
		}
	}

	// 3j. The workflow must perform frontend verification before the Go
	// build, and the Windows package must ship the launcher script under
	// the contracted identity. v1.10.7: the workflow must also build the
	// core from its relocated entry package (cmd/sheytan-local-agent/).
	for _, fragment := range []string{
		"npm run typecheck",
		"npm run lint",
		"npm run build",
		"web/static/index.html",
		releasecontract.LauncherScript,
		"./cmd/sheytan-local-agent",
		"packaging/SHEYTAN-LA.bat",
	} {
		if !strings.Contains(w, fragment) {
			return fmt.Errorf(
				"frontend/package release gate missing %q",
				fragment,
			)
		}
	}

	// 3k. The current workflow intentionally does not use the broken,
	// repository-wide Prettier gate that previously stopped CI before the
	// actual build.
	if strings.Contains(w, "npm run format:check") {
		return fmt.Errorf(
			"workflow still contains the stale repository-wide format gate",
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
	artifactRe := regexp.MustCompile(`dist/[A-Za-z0-9._${}(){}:-]+?\.(?:zip|exe|msix)`)

	found := map[string]bool{}
	for _, m := range artifactRe.FindAllString(workflow, -1) {
		found[m] = true
	}

	if len(found) == 0 {
		return fmt.Errorf(
			"desktop workflow produces no dist/ artifacts — packaging contract lost",
		)
	}

	// The three contracted artifacts must appear somewhere in the
	// workflow's dist/ output set (any version-slot spelling counts).
	contracted := []string{
		contract.WindowsZipWorkflowSlot,
		contract.WindowsInstallerSlot,
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
	// contracts exist in the tree again.
	for got := range found {
		if strings.Contains(got, "SHEYTAN-Local-Agent-Windows-x64") {
			return fmt.Errorf(
				"stale legacy Windows artifact %q still produced by the workflow — the v1.2.0 contract renamed it to %q",
				got,
				contract.WindowsZipWorkflowSlot,
			)
		}
	}

	// The staging directories must match the contracted app roots via
	// the canonical root variables: the portable ZIPs and the NSIS
	// installer both build from them (v1.2.1 — slot spellings, so a
	// duplicated literal no longer satisfies this check).
	for _, slot := range []string{
		releasecontract.WindowsAppDirWorkflowSlot,
		releasecontract.LinuxAppDirWorkflowSlot,
	} {
		if !strings.Contains(workflow, slot) {
			return fmt.Errorf(
				"workflow no longer stages the application at %q — packaging identity drifted",
				slot,
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
