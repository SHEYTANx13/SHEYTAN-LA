package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Retirement scanning — v1.11.1 repair (run 42502043636 work order §3.1/§3.5F).
//
// The two retirement scans below pin the live APPLICATION surface, not the
// project's written history. The reviewed run failed all three platform
// lanes on the same false positive: historical/explanatory Markdown under
// docs/ mentions the retired stacks, and the old scanner walked Markdown as
// if it were live application source. Markdown documentation is NOT active
// application source (work order §3.5E): it is excluded from the live scan
// everywhere (the docs tree is dropped from the roots AND any *.md /
// *.markdown file inside a live root is skipped).
//
// The scans remain FAIL-CLOSED on real retirement regressions. The positive
// fixture tests below build temporary trees containing every representative
// banned form — .qml, .qrc, .pro, .pri, .qmlproject, Qt Designer .ui, Qt
// CMake targets/macros (qt_add_executable/qt_add_qml_module/AUTOMOC), Qt
// application symbols and the retired webview-shell name — and require the
// scanner to flag each one, while the same terms inside Markdown-only
// documentation must pass, and generic CMake / non-Qt C++ must stay outside
// the Qt bans (the native inference engine is not a Qt UI).
//
// Platform discipline: every reported path is normalized with
// filepath.ToSlash so Linux and Windows report identical paths.

// liveSourceRoots is the precise inventory of ACTIVE application source,
// executable assets, build configuration, workflows, packaging and
// deployment/installer inputs. docs/ is documentation, not live source, and
// is deliberately absent: historical notes about the retired stacks must not
// fail the live-tree gate.
var liveSourceRoots = []string{"internal", "cmd", "ui", "packaging", "build", "scripts", ".github"}

// retiredScanSkipDirs are generated/vendor/fixture trees that are not live
// application source in any of the live roots.
var retiredScanSkipDirs = map[string]bool{
	"node_modules": true,
	"testdata":     true,
	"vendor":       true,
}

// isMarkdownDoc reports whether a file name is Markdown documentation.
// Markdown files are historical/explanatory content — never live
// application source — so they never trip the live-tree retirement scans.
func isMarkdownDoc(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}

// walkLiveTree walks relRoots under root, skipping generated/fixture
// directories, Markdown documentation and oversized binary files, invoking
// visit for every remaining regular file. Paths handed to visit are absolute.
func walkLiveTree(root string, relRoots []string, visit func(path string, d os.DirEntry) error) error {
	for _, top := range relRoots {
		base := filepath.Join(root, top)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if retiredScanSkipDirs[d.Name()] {
					return filepath.SkipAll
				}
				return nil
			}
			if isMarkdownDoc(d.Name()) {
				return nil // documentation is not live application source
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() > 8<<20 {
				return nil // binaries/images are not text sources
			}
			return visit(path, d)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// reportRel returns the repo-relative, slash-normalized form of an absolute
// path so Linux and Windows scan reports are byte-identical.
func reportRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// scanRetiredWebviewShellNames returns the slash-relative paths of live
// files that carry the first-generation webview desktop shell's product
// name. The name is assembled from split fragments here so this test file
// never contains it (a scan gate must not reintroduce the string it bans).
func scanRetiredWebviewShellNames(root string, relRoots []string) ([]string, error) {
	banned := strings.Join([]string{"wa", "il", "s"}, "")
	var hits []string
	err := walkLiveTree(root, relRoots, func(path string, d os.DirEntry) error {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			hits = append(hits, reportRel(root, path))
		}
		return nil
	})
	return hits, err
}

// scanQtQmlAppSource returns the slash-relative paths of live files that are
// Qt/QML application-UI source (banned suffixes) or carry Qt application-UI
// framework/build symbols. native/** is out of scope by construction: the
// llama.cpp inference backend is C++ but not a Qt UI, and generic CMake must
// never be rejected.
func scanQtQmlAppSource(root string, relRoots []string) ([]string, error) {
	// Qt/QML UI source and project files — a file with one of these
	// suffixes in an active application path is the retired UI returning
	// (includes the compiled QML cache/type artifacts).
	bannedExts := []string{".qml", ".qmlc", ".qmltypes", ".qmlproject", ".qrc", ".pro", ".pri", ".ui"}

	// qmldir module manifests carry no extension; generated Qt codegen
	// artifacts (moc_*.cpp / qrc_*.cpp / ui_*.h) are banned by name shape.
	bannedBaseNames := []string{"qmldir"}
	generatedQtNames := []string{"moc_", "qrc_", "ui_"}

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
		frag("qt_add_", "executable"),
		frag("AUTO", "MOC"),
		frag("AUTO", "RCC"),
		frag("AUTO", "UIC"),
		frag("qml", "cache", "gen"),
	}

	var hits []string
	err := walkLiveTree(root, relRoots, func(path string, d os.DirEntry) error {
		lower := strings.ToLower(d.Name())
		for _, ext := range bannedExts {
			if strings.HasSuffix(lower, ext) {
				hits = append(hits, reportRel(root, path))
				return nil
			}
		}
		for _, base := range bannedBaseNames {
			if lower == base {
				hits = append(hits, reportRel(root, path))
				return nil
			}
		}
		for _, prefix := range generatedQtNames {
			if strings.HasPrefix(lower, prefix) && (strings.HasSuffix(lower, ".cpp") || strings.HasSuffix(lower, ".h")) {
				hits = append(hits, reportRel(root, path))
				return nil
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := strings.ToLower(string(raw))
		for _, sym := range bannedSymbols {
			if strings.Contains(text, strings.ToLower(sym)) {
				hits = append(hits, reportRel(root, path))
				return nil
			}
		}
		return nil
	})
	return hits, err
}

// TestRetiredWebviewShellNameAbsent pins the v1.11.0 retirement scan: the
// live tracked tree carries ZERO references to the first-generation
// webview desktop shell by its product name in ACTIVE application source,
// build configuration, workflows and packaging inputs. Historical Markdown
// (docs/ai-notes.md lineage notes) is not live source and is out of scope.
func TestRetiredWebviewShellNameAbsent(t *testing.T) {
	root := findRepoRoot(t)

	hits, err := scanRetiredWebviewShellNames(root, liveSourceRoots)
	if err != nil {
		t.Fatalf("retirement scan failed: %v", err)
	}
	for _, hit := range hits {
		t.Errorf("retired webview shell name found in live tree: %s", hit)
	}

	scanned := countLiveScannedFiles(t, root, liveSourceRoots)
	if scanned < 100 {
		t.Fatalf("scan covered only %d files — the scope is broken, fix the roots", scanned)
	}
}

// TestNoQtQmlAppSourceRemains pins the v1.11.1 Qt/QML UI retirement end
// state: the live tracked tree carries ZERO Qt/QML application-UI source —
// no QML/QRC/qmake project files, and none of the Qt app-UI framework
// symbols in any active application, packaging or workflow path. Historical
// Markdown (docs/ai-notes.md) is not live source and is out of scope; the
// native C++ inference engine and generic CMake are out of scope by design.
func TestNoQtQmlAppSourceRemains(t *testing.T) {
	root := findRepoRoot(t)

	hits, err := scanQtQmlAppSource(root, liveSourceRoots)
	if err != nil {
		t.Fatalf("retirement scan failed: %v", err)
	}
	for _, hit := range hits {
		t.Errorf("retired Qt/QML application-UI symbol found in live tree: %s contains a Qt app-UI framework/build symbol", hit)
	}

	scanned := countLiveScannedFiles(t, root, liveSourceRoots)
	if scanned < 100 {
		t.Fatalf("scan covered only %d files — the scope is broken, fix the roots", scanned)
	}
}

// countLiveScannedFiles walks the live roots exactly like the scanners and
// counts the files the scanners would actually inspect — the scope sanity
// gate measures REAL coverage, not raw directory entries.
func countLiveScannedFiles(t *testing.T, root string, relRoots []string) int {
	t.Helper()
	scanned := 0
	err := walkLiveTree(root, relRoots, func(path string, d os.DirEntry) error {
		scanned++
		return nil
	})
	if err != nil {
		t.Fatalf("coverage walk failed: %v", err)
	}
	return scanned
}

// TestRetirementScannerPositiveFixtures is the fail-closed guarantee the
// repaired scanner must keep: every representative banned artifact form,
// placed in an ACTIVE source path of a temporary fixture tree, is flagged —
// while generic CMake, non-Qt C++ and the same terms inside Markdown
// documentation stay clean. Table-driven, so adding a banned form means
// adding a row, not editing scanner logic.
func TestRetirementScannerPositiveFixtures(t *testing.T) {
	frag := func(parts ...string) string { return strings.Join(parts, "") }
	webviewName := frag("wa", "il", "s")
	qApplication := frag("QApplic", "ation")
	qWidget := frag("QWid", "get")
	automoc := frag("AUTO", "MOC")
	qtAddQmlModule := frag("qt_add_", "qml_", "module")
	qtAddExecutable := frag("qt_add_", "executable")
	qmlApplicationEngine := frag("QQml", "Application", "Engine")

	qtScannerCases := []struct {
		name    string
		relPath string
		content string
	}{
		{"qml source file", "ui/legacy/main.qml", "import QtQuick 2.0\nRectangle { }\n"},
		{"qml cache artifact", "ui/legacy/main.qmlc", "binary-ish cache"},
		{"qml types artifact", "ui/legacy/plugin.qmltypes", "{}"},
		{"qmldir module manifest", "ui/legacy/qmldir", "singleton Main 1.0 Main.qml\n"},
		{"generated moc artifact", "ui/legacy/moc_main.cpp", "#include \"main.h\"\n"},
		{"generated rcc artifact", "ui/legacy/qrc_res.cpp", "#include \"res.qrc\"\n"},
		{"generated uic artifact", "ui/legacy/ui_form.h", "#include \"form.ui\"\n"},
		{"qrc resource file", "ui/legacy/res.qrc", "<RCC><qresource></qresource></RCC>\n"},
		{"qmake project file", "ui/legacy/app.pro", "TEMPLATE = app\n"},
		{"qmake include file", "ui/legacy/app.pri", "SOURCES += main.cpp\n"},
		{"qmlproject file", "ui/legacy/app.qmlproject", "import QmlProject 1.0\n"},
		{"qt designer form", "ui/legacy/form.ui", "<?xml version=\"1.0\"?>\n<ui version=\"4.0\"><widget class=\"" + qWidget + "\"></widget></ui>\n"},
		{"qt cmake qml module", "CMakeLists.txt", qtAddQmlModule + "(app URI 1.0)\n"},
		{"qt cmake executable + automoc", "CMakeLists.txt", qtAddExecutable + "(app main.cpp)\nset(CMAKE_" + automoc + " ON)\n"},
		{"qt symbol in go source", "internal/app/legacy_host.go", "package app\n\nvar _ = " + qApplication + "(nil)\nvar _ = " + qmlApplicationEngine + "{}\n"},
	}
	for _, tc := range qtScannerCases {
		t.Run("qt/"+tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixtureFile(t, root, tc.relPath, tc.content)
			hits, err := scanQtQmlAppSource(root, []string{"."})
			if err != nil {
				t.Fatalf("scan failed: %v", err)
			}
			if len(hits) != 1 || hits[0] != filepath.ToSlash(tc.relPath) {
				t.Fatalf("scanner must flag exactly %q in an active path, got %v", filepath.ToSlash(tc.relPath), hits)
			}
		})
	}

	t.Run("qt/generic cmake and native c++ stay clean", func(t *testing.T) {
		root := t.TempDir()
		writeFixtureFile(t, root, "native/CMakeLists.txt", "cmake_minimum_required(VERSION 3.16)\nadd_library(engine engine.cpp)\nfind_package(Threads REQUIRED)\n")
		writeFixtureFile(t, root, "native/engine.cpp", "#include <cstring>\nsize_t shard(size_t n) { return n / 2; }\n")
		hits, err := scanQtQmlAppSource(root, []string{"."})
		if err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("generic CMake / non-Qt C++ must never trip the Qt retirement scan, got %v", hits)
		}
	})

	t.Run("qt/markdown-only history passes", func(t *testing.T) {
		root := t.TempDir()
		writeFixtureFile(t, root, "docs/ai-notes.md",
			"# history\n\nThe retired stack used "+qApplication+", "+qWidget+" and "+automoc+
				"; the migration notes mention "+qtAddQmlModule+" and "+qmlApplicationEngine+".\n")
		hits, err := scanQtQmlAppSource(root, []string{"."})
		if err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("Markdown-only Qt history must pass the live-source scan, got %v", hits)
		}
	})

	webviewCases := []struct {
		name    string
		relPath string
	}{
		{"active go source", "internal/app/shell.go"},
		{"active workflow", ".github/workflows/build.yml"},
		{"active packaging script", "scripts/pkg.sh"},
	}
	for _, tc := range webviewCases {
		t.Run("webview/"+tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixtureFile(t, root, tc.relPath, "// legacy host: "+webviewName+"\n")
			hits, err := scanRetiredWebviewShellNames(root, []string{"."})
			if err != nil {
				t.Fatalf("scan failed: %v", err)
			}
			if len(hits) != 1 || hits[0] != filepath.ToSlash(tc.relPath) {
				t.Fatalf("scanner must flag exactly %q in an active path, got %v", filepath.ToSlash(tc.relPath), hits)
			}
		})
	}

	t.Run("webview/markdown-only history passes", func(t *testing.T) {
		root := t.TempDir()
		writeFixtureFile(t, root, "docs/ai-notes.md", "# history\n\nSession notes mention the "+webviewName+" shell retirement.\n")
		hits, err := scanRetiredWebviewShellNames(root, []string{"."})
		if err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("Markdown-only webview history must pass the live-source scan, got %v", hits)
		}
	})
}

// TestRetirementScannerSkipsDocsTreeFixtures pins the scope decision
// end-to-end: a docs/ tree that mentions BOTH retired stacks passes both
// scans, while the same content under an active root fails both — on one
// fixture tree, with slash-normalized reported paths.
func TestRetirementScannerSkipsDocsTreeFixtures(t *testing.T) {
	frag := func(parts ...string) string { return strings.Join(parts, "") }
	qApplication := frag("QApplic", "ation")
	webviewName := frag("wa", "il", "s")
	activeQt := "ui/legacy/main.pro"
	activeWebview := "internal/app/legacy.go"

	root := t.TempDir()
	writeFixtureFile(t, root, "docs/ai-notes.md", "# history\n\n"+qApplication+" and the "+webviewName+" shell are retired.\n")
	writeFixtureFile(t, root, activeQt, "TEMPLATE = app\n")
	writeFixtureFile(t, root, activeWebview, "package app\n// "+webviewName+" host\n")

	qtHits, err := scanQtQmlAppSource(root, []string{"."})
	if err != nil {
		t.Fatalf("qt scan failed: %v", err)
	}
	if len(qtHits) != 1 || qtHits[0] != filepath.ToSlash(activeQt) {
		t.Fatalf("qt scan must flag only the active %q, got %v", filepath.ToSlash(activeQt), qtHits)
	}

	webHits, err := scanRetiredWebviewShellNames(root, []string{"."})
	if err != nil {
		t.Fatalf("webview scan failed: %v", err)
	}
	if len(webHits) != 1 || webHits[0] != filepath.ToSlash(activeWebview) {
		t.Fatalf("webview scan must flag only the active %q, got %v", filepath.ToSlash(activeWebview), webHits)
	}
}

// writeFixtureFile creates path (slash-separated, relative to root) with the
// given content inside a fixture tree, creating parent directories.
func writeFixtureFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir for fixture %s: %v", relPath, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", relPath, err)
	}
}
