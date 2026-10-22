// Package releasecontract is the SINGLE authoritative definition of the
// release artifact naming for SHEYTAN-LA.
//
// The rule:
//
//   - The workflow defines ONE canonical package-root variable per
//     platform (WIN_PKG_ROOT / LINUX_PKG_ROOT) and derives every staging
//     directory, ZIP creation path, ZIP entry check, artifact name and
//     release-metadata reference from them. No root literal is repeated.
//   - This contract mirrors those variables and exposes the workflow-slot
//     spellings the gate requires to see in the workflow text.
//   - The stress gate (cmd/stress_release_surface.go) enforces agreement in both
//     directions: the workflow must consume the canonical variables, and
//     every dist/ artifact it produces must match the contract slots.
//     When a future release renames artifacts again, the contract changes
//     HERE and the gate detects any drift instead of silently pinning a
//     second copy of the truth.
package releasecontract

import (
	"fmt"
	"strings"
)

// App roots and executable identity. Both app roots follow the
// v1.10.14-fixed ONE-naming-map rule (D7/I11): the package roots spell the
// product identically on the release page. The Linux EXECUTABLE inside the
// package keeps its long historical name — only the package root, the ZIP
// name and the artifact display names follow the map.
const (
	WindowsAppRoot = "SHEYTAN-LA"
	LinuxAppRoot   = "SHEYTAN-LA"
	WindowsExeName = "SHEYTAN-LA.exe"
	LinuxExeName   = "SHEYTAN-Local-Agent"
	LauncherScript = "SHEYTAN-LA.bat"
)

// Canonical workflow package-root variables. These are the exact
// env declarations the workflow must carry: one canonical root per
// platform, from which every packaging path is derived. The stress gate
// requires these exact lines so a future edit that reintroduces a
// duplicated root literal fails the suite instead of the release.
const (
	WorkflowWinRootEnvLine   = `WIN_PKG_ROOT: "` + WindowsAppRoot + `"`
	WorkflowLinuxRootEnvLine = `LINUX_PKG_ROOT: "` + LinuxAppRoot + `"`
)

// Workflow reference spellings for the canonical roots. The workflow
// cannot import Go, so the gate checks these textual slots instead; the
// shell spellings carry the artifact-agreement regexes (the GitHub
// Actions `${{ … }}` spelling contains spaces and is deliberately not
// part of the agreement regex set).
const (
	WinRootPwsh   = "${env:WIN_PKG_ROOT}"       // PowerShell steps
	WinRootBash   = "${WIN_PKG_ROOT}"           // bash steps (release job)
	WinRootGHA    = "${{ env.WIN_PKG_ROOT }}"   // with: blocks
	LinuxRootBash = "${LINUX_PKG_ROOT}"         // bash steps
	LinuxRootGHA  = "${{ env.LINUX_PKG_ROOT }}" // with: blocks
)

// Contract is the resolved artifact-name set for one version.
type Contract struct {
	Version string

	// Portable ZIPs — ONE pattern for every platform: ROOT-vVER-<os>-<arch>.
	WindowsZip string // SHEYTAN-LA-v1.3.1-windows-x64.zip
	LinuxZip   string // SHEYTAN-LA-v1.2.1-linux-x64.zip

	// Windows NSIS installer.
	WindowsInstaller string // SHEYTAN-LA-v1.3.1-windows-x64-installer.exe

	// Workflow placeholders: the exact spellings used inside
	// build-desktop.yml, with the version slot left to the workflow env
	// and the package root slot left to the canonical root variables.
	WindowsZipWorkflowSlot string // ${env:WIN_PKG_ROOT}-v${env:APP_VERSION}-windows-x64.zip
	WindowsInstallerSlot   string // ${env:WIN_PKG_ROOT}-v${env:APP_VERSION}-windows-x64-installer.exe
	LinuxZipWorkflowSlot   string // ${LINUX_PKG_ROOT}-v${APP_VERSION}-linux-x64.zip
}

// For resolves the artifact-name contract for a semantic version (with or
// without a leading "v").
func For(version string) Contract {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")

	return Contract{
		Version: v,

		WindowsZip:       fmt.Sprintf("%s-v%s-windows-x64.zip", WindowsAppRoot, v),
		LinuxZip:         fmt.Sprintf("%s-v%s-linux-x64.zip", LinuxAppRoot, v),
		WindowsInstaller: fmt.Sprintf("%s-v%s-windows-x64-installer.exe", WindowsAppRoot, v),

		WindowsZipWorkflowSlot: WinRootPwsh + "-v${env:APP_VERSION}-windows-x64.zip",
		WindowsInstallerSlot:   WinRootPwsh + "-v${env:APP_VERSION}-windows-x64-installer.exe",
		LinuxZipWorkflowSlot:   LinuxRootBash + "-v${APP_VERSION}-linux-x64.zip",
	}
}

// WindowsAppDir is the staging directory (relative to the repository root)
// the workflow assembles the portable Windows application into. Kept as a
// concrete constant for tooling and tests; the workflow itself derives the
// same path from the canonical WIN_PKG_ROOT (see the workflow slot below).
const WindowsAppDir = "dist/windows/app/" + WindowsAppRoot

// LinuxAppDir is the concrete Linux staging directory for tooling/tests.
const LinuxAppDir = "dist/linux/app/" + LinuxAppRoot

// Workflow staging-directory slots: the exact spellings the workflow must
// contain. Requiring the SLOT (not the concrete name) is what makes the
// canonical variable contract enforceable — a workflow that reintroduces a
// duplicated root literal in the staging path no longer matches.
const (
	WindowsAppDirWorkflowSlot = "dist/windows/app/" + WinRootPwsh
	LinuxAppDirWorkflowSlot   = "dist/linux/app/" + LinuxRootBash
)

// RequiredWindowsZipEntries lists the entries the Windows portable ZIP
// must contain, expressed against the contracted app root. This is the
// CONCRETE truth (what the sealed ZIP must hold after the workflow's
// variables expand).
func (c Contract) RequiredWindowsZipEntries() []string {
	return []string{
		WindowsAppRoot + "/" + WindowsExeName,
		WindowsAppRoot + "/README.txt",
		WindowsAppRoot + "/BUILD-INFO.txt",
		WindowsAppRoot + "/models/README.txt",
		WindowsAppRoot + "/workspace/README.txt",
	}
}

// RequiredLinuxZipEntries lists the entries the Linux portable ZIP must
// contain, expressed against the contracted Linux root. Every Linux entry
// is rooted at LinuxAppRoot, the single Linux identity — a verifier that
// mixes roots agrees with itself while disagreeing with the ZIP it
// verifies, which is the exact failure class this contract prevents.
func (c Contract) RequiredLinuxZipEntries() []string {
	return []string{
		LinuxAppRoot + "/" + LinuxExeName,
		LinuxAppRoot + "/README.txt",
		LinuxAppRoot + "/BUILD-INFO.txt",
		LinuxAppRoot + "/models/README.txt",
		LinuxAppRoot + "/workspace/README.txt",
	}
}

// RequiredWindowsZipWorkflowEntries lists the parameterized entry
// spellings the workflow's Windows verification step must contain: the
// canonical WIN_PKG_ROOT slot followed by the concrete leaf names. The
// stress gate requires these so the verifier demonstrably derives its
// expectations from the canonical variable instead of a duplicated root.
func (c Contract) RequiredWindowsZipWorkflowEntries() []string {
	return []string{
		WinRootPwsh + "/" + WindowsExeName,
		WinRootPwsh + "/README.txt",
		WinRootPwsh + "/BUILD-INFO.txt",
		WinRootPwsh + "/models/README.txt",
		WinRootPwsh + "/workspace/README.txt",
	}
}

// RequiredLinuxZipWorkflowEntries lists the parameterized entry spellings
// the workflow's Linux verification steps must contain: the canonical
// LINUX_PKG_ROOT slot followed by the concrete leaf names.
func (c Contract) RequiredLinuxZipWorkflowEntries() []string {
	return []string{
		LinuxRootBash + "/" + LinuxExeName,
		LinuxRootBash + "/README.txt",
		LinuxRootBash + "/BUILD-INFO.txt",
		LinuxRootBash + "/models/README.txt",
		LinuxRootBash + "/workspace/README.txt",
	}
}
