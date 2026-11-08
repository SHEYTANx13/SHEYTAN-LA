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

// TestNoQtQmlAppSourceRemains pins the v1.11.1 Qt/QML UI retirement
// end state (work-order §D5): the live tracked tree carries ZERO Qt/QML
// application-UI source — no QML/QRC/qmake project files, and none of
// the Qt app-UI framework symbols in any active application, packaging
// or workflow path.
//
// Scope: the active application UI/package paths (internal, cmd, ui,
// packaging, build, scripts, .github, docs). native/** is deliberately
// OUT of scope — the llama.cpp inference backend is C++ but not a Qt UI,
// and the guard must never reject generic CMake or the engine sources.
//
// Runtime-dependency half of the same contract (already guarded, not
// duplicated here): the stress release-surface gate bans the Qt/GTK
// toolchain packages in the Linux CI workflow, and the Windows launch
// smoke's ImportAudit.ps1 fails closed on ANY missing static import —
// a Qt runtime DLL dependency cannot pass packaging.
//
// Every banned literal is assembled from split fragments at runtime so
// this test file itself never contains one (a scan gate must not
// reintroduce the strings it bans). The scan is line-ending and
// platform-proof: it lowercases file text and matches substrings.
func TestNoQtQmlAppSourceRemains(t *testing.T) {
	root := findRepoRoot(t)

	// Qt/QML UI source and project files — a file with any of these
	// suffixes in an active application path is the retired UI returning.
	bannedExts := []string{".qml", ".qrc", ".qmlproject", ".pro", ".pri", ".ui"}

	// Qt application-UI framework and build symbols (split fragments,
	// assembled below). These are the API surface of a Qt/QML desktop
	// host — none of them can appear in a Go/Fyne-only application tree.
	frag := func(parts ...string) string { return strings.Join(parts, "") }
	bannedSymbols := []string{
		frag("QQml", "Application", "Engine"),
		frag("QQuick", "View"),
		frag("QQuick", "Widget"),
		frag("QApplic", "ation"),
		frag("QGuiApplic", "ation"),
		frag("QMain", "Window"),
		frag("QWid", "get"),
		frag("qt_add_", "qml_", "module"),
		frag("qt_add_", "resources"),
		frag("AUTO", "MOC"),
		frag("AUTO", "RCC"),
		frag("AUTO", "UIC"),
		frag("qml", "cache", "gen"),
	}

	roots := []string{"internal", "cmd", "ui", "packaging", "build", "scripts", ".github", "docs"}
	scanned := 0
	for _, top := range roots {
		base := filepath.Join(root, top)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "node_modules", "testdata", "vendor":
					return filepath.SkipAll
				}
				return nil
			}
			name := d.Name()
			lower := strings.ToLower(name)
			for _, ext := range bannedExts {
				if strings.HasSuffix(lower, ext) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("retired Qt/QML UI source file found in live tree: %s", rel)
					return nil
				}
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
			text := strings.ToLower(string(raw))
			for _, sym := range bannedSymbols {
				if strings.Contains(text, strings.ToLower(sym)) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("retired Qt/QML application-UI symbol found in live tree: %s contains a Qt app-UI framework/build symbol", rel)
					return nil
				}
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
