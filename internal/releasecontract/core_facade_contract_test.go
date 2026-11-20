package releasecontract

// Core-facade presentation boundary (run 44547031698 §4.2): the ONE Go
// application core and the CLI build and run WITHOUT importing any
// presentation toolkit. Go-Gui (go-gui/purego), the Fyne production shell,
// and the Wails v3 WebView stack are presentation adapters layered OVER
// the core - never dependencies OF it. Views call use cases; the core
// never constructs views.
//
// This contract walks every package source file in internal/ and the
// shared cmd command package and refuses any import of a presentation
// stack. Two adapters are the explicit, documented exceptions - they ARE
// the presentation seam, not the core:
//
//   - internal/desktop       the production desktop boot (Fyne shell);
//   - internal/webworkspace  the Wails service/policy adapter.
//
// The mobile/entrypoint mains under cmd/sheytan-* legitimately start a
// shell and are outside the core boundary; the shared cmd command package
// (cobra use-case commands) is inside it.

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// presentationImportPrefixes are the module paths that pull a presentation
// toolkit into a package's link graph.
var presentationImportPrefixes = []string{
	"fyne.io/fyne",
	"github.com/go-gui-org/go-gui",
	"github.com/wailsapp/wails",
	"github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne",
	"github.com/SHEYTANx13/SHEYTAN-LA/ui/gogui",
}

// presentationFreeExceptions are the presentation SEAM packages - the only
// internal packages allowed to import a presentation stack.
var presentationFreeExceptions = map[string]bool{
	"internal/desktop":      true,
	"internal/webworkspace": true,
}

func TestCorePackagesArePresentationFree(t *testing.T) {
	t.Chdir(moduleRootDir(t))

	offenders := scanPresentationImports(t, "internal", presentationFreeExceptions)

	// The shared cmd command package (cobra commands) is part of the CLI
	// core boundary; the cmd/sheytan-* entrypoint mains legitimately
	// start a shell and are exempt.
	offenders = append(offenders, scanPresentationImports(t, "cmd", map[string]bool{
		"cmd/sheytan-local-agent": true,
		"cmd/sheytan-mobile":      true,
		"cmd/sheytan-renderprobe": true,
		"cmd/printversion":        true,
		"cmd/shtn-perf":           true,
	})...)

	for _, o := range offenders {
		t.Errorf("presentation boundary violation: %s", o)
	}

	if len(offenders) == 0 {
		t.Log("core boundary OK: no package in internal/ or the shared cmd package imports a presentation toolkit")
	}
}

// moduleRootDir walks up from the package directory to the directory
// that contains the canonical go.mod (the boundary is defined against the
// whole module, and tests run from the package directory).
func moduleRootDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s - the presentation boundary needs the module root", dir)
		}

		dir = parent
	}
}

// scanPresentationImports returns human-readable violations for every
// non-test source file under root whose package (relative directory) is
// not exempt and whose import block references a presentation prefix.
func scanPresentationImports(t *testing.T, root string, exempt map[string]bool) []string {
	t.Helper()

	var violations []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		pkgDir := filepath.ToSlash(filepath.Dir(path))
		if exempt[pkgDir] {
			return nil
		}

		inImport := false

		f, ferr := os.Open(path)
		if ferr != nil {
			return ferr
		}
		defer func() { _ = f.Close() }()

		// quotedImport extracts the module path from an import line, with
		// or without an alias prefix ("path" / alias "path").
		quotedImport := func(line string) string {
			q1 := strings.Index(line, "\"")
			if q1 < 0 {
				return ""
			}

			q2 := strings.LastIndex(line, "\"")
			if q2 <= q1 {
				return ""
			}

			return line[q1+1 : q2]
		}

		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())

			switch {
			case strings.HasPrefix(line, "import ("):
				inImport = true
				continue
			case inImport && line == ")":
				inImport = false
				continue
			case inImport:
				imp := quotedImport(line)

				for _, prefix := range presentationImportPrefixes {
					if strings.HasPrefix(imp, prefix) {
						violations = append(violations, pkgDir+" imports "+imp+" ("+path+")")
					}
				}
			case strings.HasPrefix(line, "import "):
				imp := quotedImport(line)

				for _, prefix := range presentationImportPrefixes {
					if strings.HasPrefix(imp, prefix) {
						violations = append(violations, pkgDir+" imports "+imp+" ("+path+")")
					}
				}
			}
		}

		return sc.Err()
	})

	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return violations
}
