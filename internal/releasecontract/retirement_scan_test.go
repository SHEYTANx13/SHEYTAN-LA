package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRetiredWebviewShellNameAbsent pins the v1.11.0 retirement scan: the
// live tracked tree carries ZERO references to the first-generation
// webview desktop shell by its product name. The name is constructed from
// split fragments at runtime so this test file itself never contains it
// (a scan gate must not reintroduce the string it bans).
//
// Scope: the same source roots the release-gate source walk uses
// (internal, cmd, scripts, .github, build, ui, docs) — not .git history,
// not vendored module caches. Neutral phrases such as "webview desktop
// shell", the Web Workspace, and the browser-hosted 3D Studio are
// intentional and remain.
func TestRetiredWebviewShellNameAbsent(t *testing.T) {
	root := findRepoRoot(t)

	// Split fragments: assembled at runtime, never written whole.
	banned := strings.Join([]string{"wa", "il", "s"}, "")

	roots := []string{"internal", "cmd", "scripts", ".github", "build", "ui", "docs"}
	scanned := 0
	for _, top := range roots {
		base := filepath.Join(root, top)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Generated/vendor trees that are not live source (none of
				// these roots currently contain them; the guard is future-proof).
				switch d.Name() {
				case "node_modules", "testdata", "vendor":
					return filepath.SkipAll
				}
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() > 8<<20 {
				return nil // binaries/images are not text sources
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			if strings.Contains(strings.ToLower(string(raw)), banned) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("retired webview shell name found in live tree: %s", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", top, err)
		}
	}
	if scanned < 100 {
		t.Fatalf("scan covered only %d files — the scope is broken, fix the roots", scanned)
	}
}
