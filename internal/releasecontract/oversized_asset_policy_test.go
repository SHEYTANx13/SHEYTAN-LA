package releasecontract

// oversized_asset_policy_test.go — v1.11.1 maintenance (run 43718750096
// work order §7.3): the hard 100,000,000-byte (100 MB decimal) PER-FILE
// ceiling on every GitHub build/release payload, enforced and pinned at
// three layers:
//
//  1. TestSourceTreeHasNoOversizedFiles — the worktree scan: no tracked
//     source file may exceed the ceiling (the source ZIP is cut from this
//     tree; large optional assets — model weights, engine archives,
//     backend runtime bundles — belong to the in-app managed-downloads
//     flow, never to a repository/package payload).
//  2. TestPackagingLanesEnforceSizeCeiling — the lane scan: BOTH the
//     Linux and Windows packaging lanes of the REAL build workflow must
//     audit staged files BEFORE the archive is built and every archive
//     member's UNCOMPRESSED size AFTER it (a member that compresses to
//     under the limit is still a violation; compressed size is never a
//     compliance argument).
//  3. TestPackageSizeAuditScriptGate — the mechanism: the shared
//     fail-closed scripts/ci/package-size-audit.sh must actually fail on
//     an over-limit member and pass a compliant tree, proven with tiny
//     fixtures through its --limit test seam (the production default
//     stays hard-pinned at 100000000, asserted below).
//
// The gate never truncates, stubs or silently drops a violating file: a
// violation names the exact path and size so a human classifies it
// (required runtime component => the packaging architecture must be
// reworked; optional asset => move it to managed downloads). Exactly
// 100,000,000 bytes is NOT greater than the threshold and is allowed.

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// MaxPayloadFileBytes is the hard per-file ceiling for every GitHub
// build/release payload (work order §7.3): 100,000,000 bytes decimal,
// measured UNCOMPRESSED. Exactly this size is allowed; margin preferred.
const MaxPayloadFileBytes int64 = 100_000_000

// sizeAuditScriptPath is the shared fail-closed gate script both
// packaging lanes invoke (Linux directly; the Windows lane implements
// the identical contract in PowerShell against the same threshold).
var sizeAuditScriptPath = filepath.Join("scripts", "ci", "package-size-audit.sh")

// TestSourceTreeHasNoOversizedFiles walks the repository working tree
// (skipping .git and generated build/log outputs, which the lanes audit
// separately) and fails closed on any file whose size exceeds the
// ceiling. The largest tracked file is logged as evidence for note.md
// (the source-ZIP cut re-runs the same scan via the script).
func TestSourceTreeHasNoOversizedFiles(t *testing.T) {
	root := findRepoRoot(t)

	var violations []string
	var largest int64
	var largestRel string
	skipped := map[string]bool{".git": true, "ci-logs": true, "dist": true}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipped[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		if info.Size() > largest {
			largest, largestRel = info.Size(), rel
		}
		if info.Size() > MaxPayloadFileBytes {
			violations = append(violations, fmt.Sprintf("%s: %d bytes", rel, info.Size()))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}

	t.Logf("largest source file: %s (%d bytes) — ceiling %d bytes", largestRel, largest, MaxPayloadFileBytes)

	if len(violations) > 0 {
		t.Fatalf("%d source-tree file(s) exceed the %d-byte per-file ceiling (classify each — required runtime component vs optional downloadable asset — and fix the packaging; never truncate or silently drop):\n  %s",
			len(violations), MaxPayloadFileBytes, strings.Join(violations, "\n  "))
	}
}

// TestPackagingLanesEnforceSizeCeiling pins the lane-side enforcement in
// the REAL build workflow: the Linux lane invokes the shared audit
// script against BOTH the staging tree and the finished archive, the
// Windows lane carries the equivalent PowerShell staging-tree and
// archive-member scans, and the shared script's production default stays
// pinned to the exact 100,000,000-byte threshold.
func TestPackagingLanesEnforceSizeCeiling(t *testing.T) {
	root := findRepoRoot(t)

	script, err := os.ReadFile(filepath.Join(root, sizeAuditScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v (the shared size-audit gate script is missing — both lanes depend on it)", sizeAuditScriptPath, err)
	}
	if !strings.Contains(string(script), "LIMIT=100000000") {
		t.Fatalf("%s no longer pins the production default LIMIT=100000000 — the hard ceiling must stay exact (do not raise it to make a package pass)", sizeAuditScriptPath)
	}

	// The CI consolidation: both size gates live in the platform-build
	// job of the ONE build.yml (per-target conditional steps).
	w := readTreeFile(t, workflowRelPath)
	linW := w

	// Linux lane: the shared script must audit the staging tree AND the
	// finished archive's uncompressed members.
	linuxStep := workflowStepBody(t, linW, "Payload size gate (100 MB per file)")
	for _, need := range []struct{ fragment, why string }{
		{"bash scripts/ci/package-size-audit.sh", "the Linux lane must invoke the shared fail-closed size-audit script"},
		{`--dir "dist/SHEYTAN-LA-ALPHA-linux-x64-portable/${LINUX_PKG_ROOT}"`, "the staging tree must be audited BEFORE the archive is built"},
		{`tar -tzvf "dist/SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz"`, "every archive member's UNCOMPRESSED size must be audited AFTER the archive is built"},
		{"tee ci-logs/linux-size-audit.log", "the audit transcript must reach the consolidated run-log artifact"},
	} {
		if !strings.Contains(linuxStep, need.fragment) {
			t.Errorf("Linux size gate lost a required mechanism: %q — %s", need.fragment, need.why)
		}
	}

	// Windows lane: the PowerShell equivalents must audit the staging
	// tree and the finished archive's members against the same threshold.
	winStaged := workflowStepBody(t, w, "Staged payload size gate (100 MB per file)")
	for _, need := range []struct{ fragment, why string }{
		{"$sizeLimit = 100000000", "the Windows staging scan must use the exact shared threshold"},
		{"Get-ChildItem -Recurse -File $root", "the staging tree must be audited recursively"},
		{"ci-logs/win-size-audit.log", "the audit transcript must reach the consolidated run-log artifact"},
	} {
		if !strings.Contains(winStaged, need.fragment) {
			t.Errorf("Windows staged size gate lost a required mechanism: %q — %s", need.fragment, need.why)
		}
	}

	winVerify := workflowStepBody(t, w, "Verify Windows ZIP")
	for _, need := range []struct{ fragment, why string }{
		{"$sizeLimit = 100000000", "the Windows archive scan must use the exact shared threshold"},
		{"$entry.Length -gt $sizeLimit", "every archive member's UNCOMPRESSED length must be checked against the ceiling"},
		{"package size gate: member", "a violating member must fail the verify step (never truncate or silently drop)"},
	} {
		if !strings.Contains(winVerify, need.fragment) {
			t.Errorf("Windows ZIP size gate lost a required mechanism: %q — %s", need.fragment, need.why)
		}
	}
}

// TestPackageSizeAuditScriptGate proves the shared script's fail-closed
// mechanism end to end with tiny fixtures via the --limit test seam: the
// violation path names the offending member and exits non-zero; a
// compliant tree passes with the member inventory printed. The
// production default limit stays 100000000 and is asserted separately
// above — this test does not (and must not) weaken it.
func TestPackageSizeAuditScriptGate(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skipf("unzip is unavailable on this host — the script's archive-member audit cannot be exercised here (NOT RUN, not green)")
	}
	if _, err := exec.LookPath("zip"); err != nil {
		t.Skipf("zip is unavailable on this host — the fixture archive cannot be built here (NOT RUN, not green)")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh is unavailable on this host (NOT RUN, not green)")
	}

	root := findRepoRoot(t)
	script := filepath.Join(root, sizeAuditScriptPath)

	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	big := filepath.Join(staged, "big.bin")
	if err := os.WriteFile(big, make([]byte, 300), 0o644); err != nil {
		t.Fatalf("write big.bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staged, "small.txt"), []byte("small"), 0o644); err != nil {
		t.Fatalf("write small.txt: %v", err)
	}

	// Build a fixture archive containing the same two members.
	zipPath := filepath.Join(dir, "pack.zip")
	if out, err := exec.Command("zip", "-q", "-j", zipPath, big, filepath.Join(staged, "small.txt")).CombinedOutput(); err != nil {
		t.Fatalf("zip fixture: %v (%s)", err, out)
	}

	run := func(extraArgs ...string) (string, error) {
		args := append([]string{script}, extraArgs...)
		out, err := exec.Command("sh", args...).CombinedOutput()
		return string(out), err
	}

	// Fail-closed path: a member over the (test-seam) limit must be named
	// and must exit non-zero — for BOTH the staged-tree and archive modes.
	out, err := run("--dir", staged, "--limit", "200")
	if err == nil {
		t.Fatalf("staged scan passed a 300-byte file under a 200-byte limit — the gate is not fail-closed (output: %s)", out)
	}
	if !strings.Contains(out, "big.bin") || !strings.Contains(out, "300 bytes") {
		t.Fatalf("violation report must name the offending file and its size, got: %s", out)
	}

	out, err = run("--zip", zipPath, "--limit", "200")
	if err == nil {
		t.Fatalf("archive scan passed a 300-byte uncompressed member under a 200-byte limit — compressed size is never a compliance argument (output: %s)", out)
	}
	if !strings.Contains(out, "big.bin") {
		t.Fatalf("violation report must name the offending member, got: %s", out)
	}

	// Pass path: the same fixtures under the production-shaped limit,
	// with the member inventory (counts, totals, largest member) printed.
	out, err = run("--dir", staged, "--zip", zipPath, "--limit", "1000")
	if err != nil {
		t.Fatalf("compliant tree failed the gate: %v (output: %s)", err, out)
	}
	for _, want := range []string{"2 files", "big.bin", "PASS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("compliant audit summary must report %q, got: %s", want, out)
		}
	}

	// Fail-closed on a missing archive: no silent skip.
	if _, err := run("--zip", filepath.Join(dir, "missing.zip")); err == nil {
		t.Fatal("a missing archive must fail the gate, not pass silently")
	}
}
