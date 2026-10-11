package repoindex

// deps.go — dependency-edge resolution and deterministic test/source
// relationships. Both passes are PURE in-memory work over the bounded
// record set: no file I/O (the module path comes from the go.mod
// record parsed during pass 1).
//
// Evidence honesty: an edge exists only when a real import/include
// statement resolves to a real repository file (verified structural
// fact). Test links exist only via deterministic naming conventions.
// Nothing here is a model guess.

import (
	"path"
	"sort"
	"strings"
)

// resolveDependencies resolves every record's raw Imports into
// in-repository Dep edges. Full re-derivation on every update keeps
// the graph consistent when NEW files create NEW resolution targets
// for OLD importers (cost is bounded map work, no content re-reads).
func resolveDependencies(idx *Index) {
	moduleName := goModuleName(idx)
	goDirs := buildGoDirMap(idx)

	for _, rec := range idx.Files {
		rec.Deps = nil
		if len(rec.Imports) == 0 {
			continue
		}

		dir := path.Dir(rec.Path)

		for _, spec := range rec.Imports {
			if len(rec.Deps) >= maxDepsPerFile {
				break
			}

			switch rec.Language {
			case "go":
				for _, target := range resolveGoImport(goDirs, moduleName, spec) {
					rec.Deps = append(rec.Deps, Dep{Path: target, Kind: "import"})
				}
			case "typescript", "javascript":
				for _, target := range resolveTSImport(idx, dir, spec) {
					rec.Deps = append(rec.Deps, Dep{Path: target, Kind: "import"})
				}
			case "c", "cpp":
				if target := resolveCInclude(idx, dir, spec); target != "" {
					rec.Deps = append(rec.Deps, Dep{Path: target, Kind: "include"})
				}
			}
		}

		sortDeps(rec.Deps)
	}

	idx.rebuild() // refresh the reverse-dependency map
}

func sortDeps(deps []Dep) {
	sort.Slice(deps, func(i, j int) bool { return deps[i].Path < deps[j].Path })
}

// goModuleName extracts the module path from the indexed go.mod record
// (parsed during pass 1 by parseGoMod — no additional I/O here).
func goModuleName(idx *Index) string {
	rec := idx.record("go.mod")
	if rec == nil || rec.Language != "gomod" {
		return ""
	}
	return rec.Package
}

// buildGoDirMap maps directory -> .go records (built once per pass).
func buildGoDirMap(idx *Index) map[string][]*FileRecord {
	m := make(map[string][]*FileRecord)
	for _, rec := range idx.Files {
		if rec.Language != "go" {
			continue
		}
		d := path.Dir(rec.Path)
		m[d] = append(m[d], rec)
	}
	return m
}

// importedByOf returns the sorted files that depend on path.
func importedByOf(idx *Index, target string) []string {
	return idx.importedBy[target]
}

// ---------------------------------------------------------------------------
// Go import resolution
// ---------------------------------------------------------------------------

// resolveGoImport resolves one Go import specifier against the module
// layout: <module>/<pkg-dir> maps to the .go files of that directory
// (the index is file-granular; a Go import is package-granular — every
// file of the imported package is an honest edge, bounded by
// maxDepsPerFile). Stdlib and external modules resolve to nothing.
func resolveGoImport(goDirs map[string][]*FileRecord, moduleName, spec string) []string {
	if moduleName == "" {
		return nil
	}
	if spec != moduleName && !strings.HasPrefix(spec, moduleName+"/") {
		return nil
	}

	sub := strings.TrimPrefix(spec, moduleName)
	sub = strings.TrimPrefix(sub, "/")
	if sub == "" {
		sub = "."
	}
	targetDir := path.Clean(sub)
	if targetDir == ".." || strings.HasPrefix(targetDir, "../") {
		return nil
	}

	recs := goDirs[targetDir]
	if len(recs) == 0 {
		return nil
	}

	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.Path)
		if len(out) >= maxDepsPerFile {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// TS/JS relative-import resolution
// ---------------------------------------------------------------------------

// tsExtensions are the probe extensions for extensionless relative
// imports (directory entries resolve through index.*).
var tsExtensions = []string{
	".ts", ".tsx", ".mts", ".cts",
	".js", ".jsx", ".mjs", ".cjs",
	".json", ".css",
}

// resolveTSImport resolves one TS/JS specifier against the importer's
// directory. Only RELATIVE specifiers (./ ../) are resolvable with
// statement-level certainty; bare specifiers are external packages
// (recorded as raw imports, never edges).
func resolveTSImport(idx *Index, importerDir, spec string) []string {
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") &&
		spec != "." && spec != ".." {
		return nil
	}

	base := path.Clean(path.Join(importerDir, spec))
	if base == ".." || strings.HasPrefix(base, "../") {
		return nil // escapes the root — never an edge
	}

	// Direct hit (specifier carried a real extension).
	if rec := idx.record(base); rec != nil {
		return []string{rec.Path}
	}

	// Extension probing.
	for _, ext := range tsExtensions {
		if rec := idx.record(base + ext); rec != nil {
			return []string{rec.Path}
		}
	}

	// Directory index probing (./util → util/index.ts).
	for _, ext := range tsExtensions {
		if rec := idx.record(base + "/index" + ext); rec != nil {
			return []string{rec.Path}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// C/C++ quoted-include resolution
// ---------------------------------------------------------------------------

// resolveCInclude resolves a quoted #include against the importer's
// directory first, then conventional include roots (repo root,
// include/, src/). Angled includes ("<stdio.h>") are system headers
// and never resolve.
func resolveCInclude(idx *Index, importerDir, spec string) string {
	if strings.HasPrefix(spec, "<") {
		return ""
	}

	candidates := []string{
		path.Clean(path.Join(importerDir, spec)),
		path.Clean(spec),
		path.Join("include", path.Clean(spec)),
		path.Join("src", path.Clean(spec)),
	}
	for _, cand := range candidates {
		if cand == ".." || strings.HasPrefix(cand, "../") {
			continue
		}
		if rec := idx.record(cand); rec != nil && (rec.Language == "c" || rec.Language == "cpp") {
			return rec.Path
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Test/source relationships (deterministic)
// ---------------------------------------------------------------------------

// linkTests derives the deterministic test relationship edges:
//
//	Go:    foo.go ↔ foo_test.go (same directory — the naming
//	       convention holds for both internal (package foo) and
//	       external (package foo_test) test files)
//	TS/JS: foo.ts → foo.test.ts / foo.spec.ts in the same directory
//	       or the sibling __tests__/ directory
//
// The edge is stored SYMMETRICALLY (source.Tests and test.Tests) — the
// relationship is a fact about the pair; search Reason strings
// describe the direction per hit.
func linkTests(idx *Index) {
	for _, rec := range idx.Files {
		rec.Tests = nil
	}

	// Group Go/TS/JS records by directory once.
	byDir := make(map[string][]*FileRecord)
	for _, rec := range idx.Files {
		switch rec.Language {
		case "go", "typescript", "javascript":
			byDir[path.Dir(rec.Path)] = append(byDir[path.Dir(rec.Path)], rec)
		}
	}

	for d, recs := range byDir {
		// Test candidates of this directory, keyed by source stem.
		goTests := map[string]*FileRecord{}
		tsTests := map[string]*FileRecord{}

		for _, rec := range recs {
			base := path.Base(rec.Path)
			if rec.Language == "go" && strings.HasSuffix(base, "_test.go") {
				goTests[strings.TrimSuffix(base, "_test.go")] = rec
				continue
			}
			if rec.Language == "typescript" || rec.Language == "javascript" {
				if stem, ok := tsTestStem(base); ok {
					tsTests[stem] = rec
				}
			}
		}

		// Sibling __tests__/ directory for TS/JS.
		siblingDir := "__tests__"
		if d != "" && d != "." {
			siblingDir = d + "/__tests__"
		}
		for _, rec := range byDir[siblingDir] {
			if stem, ok := tsTestStem(path.Base(rec.Path)); ok {
				tsTests[stem] = rec
			}
		}

		for _, rec := range recs {
			if rec.Role == "test" {
				continue // test records link via their sources
			}

			switch rec.Language {
			case "go":
				stem := strings.TrimSuffix(path.Base(rec.Path), ".go")
				if test, ok := goTests[stem]; ok {
					addTestLink(rec, test)
					addTestLink(test, rec)
				}
			case "typescript", "javascript":
				stem := path.Base(rec.Path)
				if dot := strings.LastIndex(stem, "."); dot > 0 {
					stem = stem[:dot]
				}
				if test, ok := tsTests[stem]; ok {
					addTestLink(rec, test)
					addTestLink(test, rec)
				}
			}
		}
	}
}

// tsTestStem reports the source stem of a TS/JS test file name
// ("parser.test.ts" → "parser", true).
func tsTestStem(base string) (string, bool) {
	dot := strings.LastIndex(base, ".")
	if dot <= 0 {
		return "", false
	}
	stem := base[:dot]
	if marker := lastIndexOfTestMarker(stem); marker > 0 {
		return stem[:marker], true
	}
	return "", false
}

// addTestLink appends a bounded, de-duplicated test link.
func addTestLink(rec, test *FileRecord) {
	if len(rec.Tests) >= maxTestLinksPerFile {
		return
	}
	for _, existing := range rec.Tests {
		if existing == test.Path {
			return
		}
	}
	rec.Tests = append(rec.Tests, test.Path)
}

// lastIndexOfTestMarker finds ".test" / ".spec" in a file stem.
func lastIndexOfTestMarker(stem string) int {
	for _, marker := range []string{".test", ".spec"} {
		if idx := strings.LastIndex(stem, marker); idx > 0 {
			return idx
		}
	}
	return -1
}
