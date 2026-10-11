// Package releasegate pins the v1.0.11 lesson in CI: a .gitignore pattern
// that matches source paths silently drops packages from commits — which is
// precisely how internal/sessions and internal/sandbox went missing and
// broke the v1.0.9 and v1.0.10 builds on GitHub (the code compiled locally
// because the files were on disk; git simply never tracked them).
//
// The gate walks the real source tree and asks git itself (check-ignore)
// whether any file that exists on disk is invisible to git. It runs as part
// of `go test ./internal/...` in the workflow, so a re-broken .gitignore
// fails the build BEFORE a broken release can be pushed.
package releasegate

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the repository root via git, or "" when git / the repo
// is unavailable (e.g. running the test suite from an exported source zip).
func repoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// keepExts are the extensions that constitute committed source. Build
// artifacts (.exe, .syso, .zip, .test) are intentionally ignored and never
// walked here.
var keepExts = map[string]bool{
	".go": true, ".js": true, ".css": true, ".html": true, ".svg": true,
	".md": true, ".yml": true, ".yaml": true, ".mjs": true, ".sh": true,
	".bat": true, ".mod": true, ".sum": true, ".txt": true,
}

// skipDirs are directories whose contents are intentionally NOT committed
// (test-generated screenshots land in build/shots on every headless run;
// committing them would bloat the repo with churn).
var skipDirs = map[string]string{
	"shots": "build",
}

func sourceFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	// "build" is walked because build/config.yml is committed Wails
	// source. A blanket `/build/` .gitignore pattern swallowed it for
	// v1.1.2: the file existed locally, git never tracked it, and CI
	// failed with ENOENT. Walking it here makes that failure loud.
	for _, top := range []string{"internal", "cmd", "web", "scripts", ".github", "build"} {
		base := filepath.Join(root, top)
		filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable subtree — not our problem
			}
			if d.IsDir() {
				if parent, ok := skipDirs[d.Name()]; ok && strings.HasPrefix(path, filepath.Join(root, parent)) {
					return fs.SkipDir
				}
				return nil
			}
			if !keepExts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
	}
	for _, f := range []string{
		"go.mod", "go.sum", "main.go", "main_other.go", "main_windows.go",
		"README.md", "LICENSE", "sheytan-local-agent.bat", ".gitignore",
	} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			files = append(files, f)
		}
	}
	return files
}

// TestGitignoreNeverSwallowsSource fails if any existing source file is
// matched by a .gitignore pattern. Without this gate a harmless-looking
// edit like adding `sessions/` (unanchored) hides internal/sessions/ from
// git add and ships a repo that cannot build on GitHub.
func TestGitignoreNeverSwallowsSource(t *testing.T) {
	root := repoRoot()
	if root == "" {
		t.Skip("git repository not available (exported source zip?) — gitignore gate skipped")
	}
	files := sourceFiles(t, root)
	if len(files) < 20 {
		t.Fatalf("source walk found only %d files under %s — the gate itself is broken", len(files), root)
	}

	// git check-ignore exits 0 when at least one path is ignored, 1 when
	// none are. Batch paths to stay far below any argv limit.
	const chunk = 50
	for i := 0; i < len(files); i += chunk {
		end := i + chunk
		if end > len(files) {
			end = len(files)
		}
		cmd := exec.Command("git", append([]string{"check-ignore", "--"}, files[i:end]...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf(".gitignore is swallowing source files — this is the exact class of bug that dropped internal/sessions and internal/sandbox from the v1.0.9/v1.0.10 releases and broke CI. Ignored paths:\n%s", out)
		}
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Skipf("git check-ignore unavailable (%v) — gate skipped", err)
		}
	}
}

// TestCriticalPackagesExist asserts the two packages whose absence broke
// v1.0.9/v1.0.10 actually contain Go source in the tree this test runs in.
// In CI the tree IS the pushed commit, so a release that forgot a package
// can never go green.
func TestCriticalPackagesExist(t *testing.T) {
	for _, pkg := range []string{
		filepath.Join("..", "sessions"),
		filepath.Join("..", "sandbox"),
	} {
		entries, err := os.ReadDir(pkg)
		if err != nil {
			t.Fatalf("critical package %s is missing from the tree: %v", pkg, err)
		}
		goFiles := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				goFiles++
			}
		}
		if goFiles == 0 {
			t.Fatalf("critical package %s contains no .go files", pkg)
		}
	}
}
