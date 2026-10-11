package repoindex

// repoindex_test.go — the regression contract for the Repository
// Intelligence slice: indexing, language detection, symbols,
// imports/dependencies, test/source relationships, incremental
// updates, deletion, content changes, bounds, deterministic ranking,
// path safety, persistence/reload, tool and evidence behavior.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixture is a throwaway workspace with a Store over a throwaway data
// dir.
type fixture struct {
	t    *testing.T
	root string
	data string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		t:    t,
		root: t.TempDir(),
		data: filepath.Join(t.TempDir(), "repoindex"),
	}
	t.Cleanup(func() {})
	return f
}

func (f *fixture) store() *Store {
	return NewStore(f.data)
}

func (f *fixture) write(rel, content string) {
	f.t.Helper()
	path := filepath.Join(f.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) remove(rel string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(rel))); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) update() UpdateReport {
	f.t.Helper()
	report, err := f.store().Update(context.Background(), f.root)
	if err != nil {
		f.t.Fatalf("update: %v", err)
	}
	return report
}

func (f *fixture) record(rel string) *FileRecord {
	f.t.Helper()
	st := f.store().Status(f.root)
	if st.Files == 0 {
		f.t.Fatalf("record %s: index is empty", rel)
	}
	// Load via a search over the exact path (cheap, exercises the
	// public surface).
	report, err := f.store().Search(context.Background(), f.root, Query{Path: rel, Limit: 50})
	if err != nil {
		f.t.Fatalf("record lookup: %v", err)
	}
	for _, res := range report.Results {
		if res.Path == rel {
			return f.storeRecord(rel)
		}
	}
	return nil
}

// storeRecord digs the raw record out through a fresh load (test-only
// helper that uses the internals via the same package).
func (f *fixture) storeRecord(rel string) *FileRecord {
	f.t.Helper()
	s := f.store()
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.loadLocked(f.root)
	if idx == nil {
		return nil
	}
	return idx.record(rel)
}

const goModFixture = "module example.com/fixture\n\ngo 1.26\n"

const mainGoFixture = `package main

import (
        "fmt"

        "example.com/fixture/internal/util"
)

// Runner drives the fixture.
type Runner struct{}

func (r Runner) Run(name string) error {
        fmt.Println(name)
        return nil
}

func main() {
        var r Runner
        _ = r.Run(util.Helper())
}
`

const utilGoFixture = `package util

// Helper returns a constant string.
const Version = "1.0"

func Helper() string {
        return Version
}
`

const utilGoTestFixture = `package util

import "testing"

func TestHelper(t *testing.T) {
        if Helper() == "" {
                t.Fatal("empty")
        }
}
`

const parserTsFixture = `import { helper } from "./helper";
export interface Parser {
  parse(input: string): number;
}

export class ParserImpl implements Parser {
  parse(input: string): number {
    return helper(input).length;
  }
}

export function makeParser(): Parser {
  return new ParserImpl();
}
`

const parserTestTsFixture = `import { makeParser } from "./parser";
import assert from "assert";

assert(makeParser() !== null);
`

const helperTsFixture = `export function helper(input: string): string {
  return input.trim();
}
`

const engineHeaderFixture = `#pragma once

struct Engine {
  int start();
  void stop();
};

class EngineImpl {
public:
  int boot();
};
`

const engineCppFixture = `#include "engine.h"
#include <vector>

namespace fixture {

int Engine::start() { return 0; }
void Engine::stop() {}

} // namespace fixture
`

const configJSONFixture = `{
  "modelsDir": "./models",
  "contextSize": 8192,
  "stream": true
}
`

// buildTree lays out a mixed-language fixture repository.
func (f *fixture) buildTree() {
	f.write("go.mod", goModFixture)
	f.write("main.go", mainGoFixture)
	f.write("internal/util/util.go", utilGoFixture)
	f.write("internal/util/util_test.go", utilGoTestFixture)
	f.write("src/parser.ts", parserTsFixture)
	f.write("src/parser.test.ts", parserTestTsFixture)
	f.write("src/helper.ts", helperTsFixture)
	f.write("native/engine.h", engineHeaderFixture)
	f.write("native/engine.cpp", engineCppFixture)
	f.write("config/settings.json", configJSONFixture)
}

// ---------------------------------------------------------------------------
// Language + role detection
// ---------------------------------------------------------------------------

func TestDetectLanguageAndRole(t *testing.T) {
	cases := []struct {
		path     string
		language string
		role     string
	}{
		{"main.go", "go", "source"},
		{"util_test.go", "go", "test"},
		{"src/parser.ts", "typescript", "source"},
		{"src/parser.test.ts", "typescript", "test"},
		{"src/a.spec.js", "javascript", "test"},
		{"lib/index.mjs", "javascript", "source"},
		{"config/settings.json", "json", "config"},
		{"native/engine.cpp", "cpp", "source"},
		{"native/engine.h", "c", "source"},
		{"go.mod", "gomod", "config"},
		{"README.md", "markdown", "doc"},
		{"ci.yml", "yaml", "config"},
		{"unknown.xyz", "", "source"},
	}

	for _, tc := range cases {
		lang := DetectLanguage(tc.path)
		if lang != tc.language {
			t.Errorf("DetectLanguage(%q) = %q, want %q", tc.path, lang, tc.language)
		}
		role := DetectRole(tc.path, lang)
		if role != tc.role {
			t.Errorf("DetectRole(%q) = %q, want %q", tc.path, role, tc.role)
		}
	}
}

// ---------------------------------------------------------------------------
// Parsers
// ---------------------------------------------------------------------------

func TestParseGo(t *testing.T) {
	res := parseGo(strings.Split(mainGoFixture, "\n"))

	if res.Package != "main" {
		t.Errorf("package = %q, want main", res.Package)
	}

	wantSymbols := map[string]string{
		"Runner": "struct",
		"Run":    "method",
		"main":   "func",
	}
	got := map[string]string{}
	for _, sym := range res.Symbols {
		got[sym.Name] = sym.Kind
	}
	for name, kind := range wantSymbols {
		if got[name] != kind {
			t.Errorf("symbol %s = %q, want %q", name, got[name], kind)
		}
	}

	var hasUtil bool
	for _, imp := range res.Imports {
		if imp == "example.com/fixture/internal/util" {
			hasUtil = true
		}
	}
	if !hasUtil {
		t.Errorf("module import not extracted: %v", res.Imports)
	}
}

func TestParseTSJS(t *testing.T) {
	res := parseTSJS(strings.Split(parserTsFixture, "\n"))

	syms := map[string]string{}
	for _, sym := range res.Symbols {
		syms[sym.Name] = sym.Kind
	}
	if syms["Parser"] != "interface" || syms["ParserImpl"] != "class" || syms["makeParser"] != "function" {
		t.Errorf("symbols wrong: %v", syms)
	}

	found := false
	for _, imp := range res.Imports {
		if imp == "./helper" {
			found = true
		}
	}
	if !found {
		t.Errorf("relative import missing: %v", res.Imports)
	}
}

func TestParseC(t *testing.T) {
	header := parseC(strings.Split(engineHeaderFixture, "\n"))
	hNames := map[string]string{}
	for _, sym := range header.Symbols {
		hNames[sym.Name] = sym.Kind
	}
	if hNames["Engine"] != "struct" || hNames["start"] != "func" ||
		hNames["EngineImpl"] != "class" || hNames["boot"] != "func" {
		t.Errorf("C header symbols wrong: %v", hNames)
	}

	cpp := parseC(strings.Split(engineCppFixture, "\n"))
	cNames := map[string]string{}
	for _, sym := range cpp.Symbols {
		cNames[sym.Name] = sym.Kind
	}
	if cNames["fixture"] != "namespace" || cNames["start"] != "method" || cNames["stop"] != "method" {
		t.Errorf("C++ qualified methods wrong: %v", cNames)
	}

	if len(cpp.Imports) != 2 || cpp.Imports[0] != "engine.h" || cpp.Imports[1] != "<vector>" {
		t.Errorf("C imports wrong: %v", cpp.Imports)
	}
}

func TestParseJSONKeys(t *testing.T) {
	res := parseJSONKeys(strings.Split(configJSONFixture, "\n"))

	keys := map[string]bool{}
	for _, sym := range res.Symbols {
		if sym.Kind != "key" {
			t.Errorf("kind %q, want key", sym.Kind)
		}
		keys[sym.Name] = true
	}
	for _, want := range []string{"modelsDir", "contextSize", "stream"} {
		if !keys[want] {
			t.Errorf("key %q missing: %v", want, keys)
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end indexing
// ---------------------------------------------------------------------------

func TestIndexingEndToEnd(t *testing.T) {
	f := newFixture(t)
	f.buildTree()

	report := f.update()
	if report.Indexed != 10 {
		t.Fatalf("indexed %d files, want 10 (report: %+v)", report.Indexed, report)
	}
	if report.Reindexed != 10 {
		t.Errorf("reindexed %d, want 10 on first pass", report.Reindexed)
	}

	st := f.store().Status(f.root)
	if st.State != "ready" && st.State != "stale" {
		t.Errorf("state = %q", st.State)
	}
	if st.Files != 10 {
		t.Errorf("status files = %d", st.Files)
	}
	if st.Languages["go"] != 3 || st.Languages["typescript"] != 3 ||
		st.Languages["cpp"] != 1 || st.Languages["c"] != 1 || st.Languages["json"] != 1 {
		t.Errorf("language distribution wrong: %v", st.Languages)
	}
	if st.Symbols == 0 {
		t.Error("no symbols indexed")
	}
	if st.DepEdges == 0 {
		t.Error("no dependency edges resolved")
	}
	if st.TestLinks == 0 {
		t.Error("no test links derived")
	}
	if st.GitAvailable {
		t.Error("non-git fixture must not claim git availability")
	}
}

func TestGoDependencyResolution(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	rec := f.storeRecord("main.go")
	if rec == nil {
		t.Fatal("main.go not indexed")
	}

	found := false
	for _, dep := range rec.Deps {
		if dep.Path == "internal/util/util.go" && dep.Kind == "import" {
			found = true
		}
	}
	if !found {
		t.Errorf("module import not resolved to repo file: %v", rec.Deps)
	}

	// Reverse edge: util.go is imported by main.go.
	s := f.store()
	s.mu.Lock()
	idx := s.loadLocked(f.root)
	importers := idx.importedBy["internal/util/util.go"]
	s.mu.Unlock()
	if len(importers) == 0 || importers[0] != "main.go" {
		t.Errorf("reverse edge missing: %v", importers)
	}
}

func TestTSRelativeImportResolution(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	rec := f.storeRecord("src/parser.ts")
	if rec == nil {
		t.Fatal("parser.ts not indexed")
	}
	found := false
	for _, dep := range rec.Deps {
		if dep.Path == "src/helper.ts" {
			found = true
		}
	}
	if !found {
		t.Errorf("./helper not resolved: %v", rec.Deps)
	}

	// Extensionless + index probing.
	f.write("src/theme.ts", "export const dark = true;\n")
	f.write("src/box/index.ts", "export const box = 1;\n")
	f.write("src/consumer.ts", "import { dark } from \"./theme\";\nimport { box } from \"./box\";\n")
	f.update()

	rec = f.storeRecord("src/consumer.ts")
	if rec == nil {
		t.Fatal("consumer.ts not indexed")
	}
	deps := map[string]bool{}
	for _, dep := range rec.Deps {
		deps[dep.Path] = true
	}
	if !deps["src/theme.ts"] || !deps["src/box/index.ts"] {
		t.Errorf("extension probing failed: %v", rec.Deps)
	}
}

func TestCIncludeResolution(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	rec := f.storeRecord("native/engine.cpp")
	if rec == nil {
		t.Fatal("engine.cpp not indexed")
	}
	if len(rec.Deps) != 1 || rec.Deps[0].Path != "native/engine.h" || rec.Deps[0].Kind != "include" {
		t.Errorf("quoted include not resolved (system include must not): %v", rec.Deps)
	}
}

func TestTestSourceRelationships(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	// Go: util.go ↔ util_test.go (symmetric).
	src := f.storeRecord("internal/util/util.go")
	if src == nil || len(src.Tests) != 1 || src.Tests[0] != "internal/util/util_test.go" {
		t.Errorf("go source test link missing: %+v", src)
	}
	testRec := f.storeRecord("internal/util/util_test.go")
	if testRec == nil || len(testRec.Tests) != 1 || testRec.Tests[0] != "internal/util/util.go" {
		t.Errorf("go test record link missing: %+v", testRec)
	}

	// TS: parser.ts ↔ parser.test.ts.
	src = f.storeRecord("src/parser.ts")
	if src == nil || len(src.Tests) != 1 || src.Tests[0] != "src/parser.test.ts" {
		t.Errorf("ts source test link missing: %+v", src)
	}

	// __tests__ sibling directory.
	f.write("src/core.ts", "export const core = 1;\n")
	f.write("src/__tests__/core.test.ts", "import { core } from \"../core\";\n")
	f.update()

	src = f.storeRecord("src/core.ts")
	if src == nil || len(src.Tests) != 1 || src.Tests[0] != "src/__tests__/core.test.ts" {
		t.Errorf("__tests__ link missing: %+v", src)
	}
}

// ---------------------------------------------------------------------------
// Incremental updates
// ---------------------------------------------------------------------------

func TestIncrementalUpdateSkipsUnchanged(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	first := f.update()

	// Second pass: nothing changed — everything is skipped via
	// mtime+size, nothing is re-parsed, nothing is removed.
	second := f.update()
	if second.Reindexed != 0 {
		t.Errorf("reindexed %d on unchanged tree, want 0", second.Reindexed)
	}
	if second.Unchanged != first.Indexed {
		t.Errorf("unchanged %d, want %d", second.Unchanged, first.Indexed)
	}
	if second.Removed != 0 {
		t.Errorf("removed %d on unchanged tree", second.Removed)
	}
}

func TestContentChangeTriggersReparse(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	// Rewrite util.go with a new symbol. Ensure the mtime differs.
	time.Sleep(20 * time.Millisecond)
	f.write("internal/util/util.go", utilGoFixture+"\nfunc Extra() int { return 42 }\n")

	report := f.update()
	if report.Reindexed != 1 {
		t.Errorf("reindexed %d, want exactly 1 changed file", report.Reindexed)
	}

	rec := f.storeRecord("internal/util/util.go")
	if rec == nil {
		t.Fatal("record missing")
	}
	found := false
	for _, sym := range rec.Symbols {
		if sym.Name == "Extra" {
			found = true
		}
	}
	if !found {
		t.Error("new symbol not indexed after content change")
	}
}

func TestMtimeTouchKeepsContentViaDigest(t *testing.T) {
	f := newFixture(t)
	f.write("only.go", "package only\n\nfunc One() {}\n")
	f.update()

	// Touch the mtime without changing content.
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filepath.Join(f.root, "only.go"), future, future); err != nil {
		t.Fatal(err)
	}

	report := f.update()
	if report.Reindexed != 0 {
		t.Errorf("mtime-only touch re-parsed content: %d", report.Reindexed)
	}
	if report.Unchanged != 1 {
		t.Errorf("mtime-only touch not recognized as unchanged: %+v", report)
	}
}

func TestFileDeletionRemovesRecordAndEdges(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	f.remove("internal/util/util_test.go")
	report := f.update()

	if report.Removed != 1 {
		t.Errorf("removed %d, want 1", report.Removed)
	}
	if rec := f.storeRecord("internal/util/util_test.go"); rec != nil {
		t.Error("deleted file still indexed")
	}

	// The surviving source keeps no dangling test link.
	if rec := f.storeRecord("internal/util/util.go"); rec == nil || len(rec.Tests) != 0 {
		t.Errorf("stale test link survived deletion: %+v", rec)
	}

	// Status reflects the smaller tree.
	st := f.store().Status(f.root)
	if st.Files != 9 {
		t.Errorf("files after deletion = %d, want 9", st.Files)
	}
}

func TestStaleEntriesDoNotSurviveRootSwitchAndBack(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	other := t.TempDir()
	f.write2(other, "elsewhere.ts", "export const x = 1;\n")

	s := f.store()
	if _, err := s.Update(context.Background(), other); err != nil {
		t.Fatal(err)
	}

	stOther := s.Status(other)
	if stOther.Files != 1 {
		t.Errorf("other root indexed %d files, want 1", stOther.Files)
	}

	// The first root's index is untouched (per-root persistence).
	stFirst := s.Status(f.root)
	if stFirst.Files != 10 {
		t.Errorf("first root index disturbed by other root: %d", stFirst.Files)
	}
}

func (f *fixture) write2(root, rel, content string) {
	f.t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Bounds
// ---------------------------------------------------------------------------

func TestSymbolBoundPerFile(t *testing.T) {
	f := newFixture(t)

	var b strings.Builder
	b.WriteString("package many\n\n")
	for i := 0; i < maxSymbolsPerFile+80; i++ {
		fmt.Fprintf(&b, "func Fn%03d() {}\n", i)
	}
	f.write("many.go", b.String())

	f.update()

	rec := f.storeRecord("many.go")
	if rec == nil {
		t.Fatal("many.go not indexed")
	}
	if len(rec.Symbols) != maxSymbolsPerFile {
		t.Errorf("symbols = %d, want capped at %d", len(rec.Symbols), maxSymbolsPerFile)
	}
}

func TestImportBoundPerFile(t *testing.T) {
	f := newFixture(t)

	var b strings.Builder
	b.WriteString("package importer\n\nimport (\n")
	for i := 0; i < maxImportsPerFile+30; i++ {
		fmt.Fprintf(&b, "\t\"example.com/fixture/pkg%03d\"\n", i)
	}
	b.WriteString(")\n\nfunc Use() {}\n")
	f.write("importer.go", b.String())

	f.update()

	rec := f.storeRecord("importer.go")
	if rec == nil {
		t.Fatal("importer.go not indexed")
	}
	if len(rec.Imports) != maxImportsPerFile {
		t.Errorf("imports = %d, want capped at %d", len(rec.Imports), maxImportsPerFile)
	}
	if len(rec.Deps) > maxDepsPerFile {
		t.Errorf("deps %d exceeds cap %d", len(rec.Deps), maxDepsPerFile)
	}
}

func TestOversizedFileSkippedButIndexed(t *testing.T) {
	f := newFixture(t)
	f.write("huge.go", "package huge // "+strings.Repeat("x", maxParseFileBytes+1024))

	f.update()

	rec := f.storeRecord("huge.go")
	if rec == nil {
		t.Fatal("oversized file must still be indexed (path/size)")
	}
	if rec.Skipped != "file too large" {
		t.Errorf("skipped reason = %q", rec.Skipped)
	}
	if len(rec.Symbols) != 0 {
		t.Errorf("oversized file was parsed anyway (%d symbols)", len(rec.Symbols))
	}
}

func TestExcludedDirsAreNeverIndexed(t *testing.T) {
	f := newFixture(t)
	f.write("keep.go", "package keep\n")
	f.write("node_modules/dep/index.js", "module.exports = 1;\n")
	f.write("vendor/golang.org/x/x.go", "package x\n")
	f.write(".git/objects/aa/bb", "junk")
	f.write("dist/bundle.js", "// bundle\n")

	f.update()

	st := f.store().Status(f.root)
	if st.Files != 1 {
		t.Errorf("excluded dirs leaked into the index: files=%d", st.Files)
	}
}

// ---------------------------------------------------------------------------
// Deterministic ranking
// ---------------------------------------------------------------------------

func TestDeterministicRanking(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	run := func() []string {
		report, err := f.store().Search(context.Background(), f.root, Query{Symbol: "helper"})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, res := range report.Results {
			out = append(out, res.Path)
		}
		return out
	}

	first := run()
	if len(first) == 0 {
		t.Fatal("no results for symbol helper")
	}
	for i := 0; i < 5; i++ {
		next := run()
		if strings.Join(next, "|") != strings.Join(first, "|") {
			t.Fatalf("ranking is not deterministic:\n first: %v\n next:  %v", first, next)
		}
	}

	// Equal scores resolve by path ascending.
	report, err := f.store().Search(context.Background(), f.root, Query{Path: "util"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(report.Results); i++ {
		if report.Results[i-1].Path > report.Results[i].Path &&
			report.Results[i-1].Score == report.Results[i].Score {
			t.Errorf("equal-score tie not path-ordered: %s before %s",
				report.Results[i-1].Path, report.Results[i].Path)
		}
	}
}

// ---------------------------------------------------------------------------
// Search dimensions
// ---------------------------------------------------------------------------

func TestSearchDimensions(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()
	s := f.store()

	// Symbol exact.
	report, err := s.Search(context.Background(), f.root, Query{Symbol: "Helper"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) == 0 || report.Results[0].Path != "internal/util/util.go" {
		t.Errorf("symbol search failed: %+v", report)
	}

	// Language filter.
	report, _ = s.Search(context.Background(), f.root, Query{Symbol: "helper", Language: "typescript"})
	if len(report.Results) == 0 {
		t.Error("typescript symbol search returned nothing")
	}
	for _, res := range report.Results {
		if res.Language != "typescript" {
			t.Errorf("language filter leaked: %s", res.Language)
		}
	}

	// Role filter.
	report, _ = s.Search(context.Background(), f.root, Query{Text: "util", Role: "test"})
	if len(report.Results) == 0 {
		t.Error("role-filtered search returned nothing")
	}

	// depsOf / usedBy.
	report, _ = s.Search(context.Background(), f.root, Query{DepsOf: "main.go"})
	if len(report.Results) == 0 || report.Results[0].Path != "internal/util/util.go" {
		t.Errorf("depsOf failed: %+v", report)
	}
	report, _ = s.Search(context.Background(), f.root, Query{UsedBy: "internal/util/util.go"})
	if len(report.Results) == 0 || report.Results[0].Path != "main.go" {
		t.Errorf("usedBy failed: %+v", report)
	}

	// testsOf.
	report, _ = s.Search(context.Background(), f.root, Query{TestsOf: "internal/util/util.go"})
	if len(report.Results) == 0 || report.Results[0].Path != "internal/util/util_test.go" {
		t.Errorf("testsOf failed: %+v", report)
	}

	// Task relevance: cancellation-style query ranks the engine files.
	report, _ = s.Search(context.Background(), f.root, Query{Task: "parser implementation for typescript"})
	if len(report.Results) == 0 {
		t.Error("task search returned nothing")
	}
	if report.Results[0].Path != "src/parser.ts" {
		t.Errorf("task ranking off: %s first", report.Results[0].Path)
	}

	// Empty query is an error.
	if _, err := s.Search(context.Background(), f.root, Query{}); err == nil {
		t.Error("empty query must error")
	}

	// Unresolvable dependency target is an honest empty result.
	report, err = s.Search(context.Background(), f.root, Query{DepsOf: "does/not/exist.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 0 {
		t.Errorf("unresolvable depsOf must return no results: %+v", report)
	}
}

func TestSearchLimitBounded(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 30; i++ {
		f.write(fmt.Sprintf("pkg/f%02d/file.go", i), "package f\n\nfunc Target() {}\n")
	}
	f.update()

	// A limit of 50 covers all 30 hits.
	report, err := f.store().Search(context.Background(), f.root, Query{Symbol: "target", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if report.Returned != 30 || report.Truncated {
		t.Errorf("returned %d truncated=%v, want 30/false", report.Returned, report.Truncated)
	}

	// Explicit small limit truncates honestly.
	report, err = f.store().Search(context.Background(), f.root, Query{Symbol: "target", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if report.Returned != 10 || !report.Truncated || report.TotalHits != 30 {
		t.Errorf("limited search wrong: returned=%d truncated=%v total=%d",
			report.Returned, report.Truncated, report.TotalHits)
	}

	// The hard cap is never exceeded even with a huge requested limit.
	report, err = f.store().Search(context.Background(), f.root, Query{Symbol: "target", Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if report.Returned > maxResults {
		t.Errorf("returned %d exceeds hard cap %d", report.Returned, maxResults)
	}
}

// ---------------------------------------------------------------------------
// Path safety
// ---------------------------------------------------------------------------

func TestRelWithinRootRejectsEscape(t *testing.T) {
	root := t.TempDir()

	if _, err := relWithinRoot(root, "../outside.go"); err == nil {
		t.Error("../ escape accepted")
	}
	if _, err := relWithinRoot(root, "/etc/passwd"); err == nil {
		t.Error("absolute outside path accepted")
	}
	if _, err := relWithinRoot(root, root+"/../sibling.go"); err == nil {
		t.Error("absolute sibling path accepted")
	}

	rel, err := relWithinRoot(root, root+"/sub/file.go")
	if err != nil || rel != "sub/file.go" {
		t.Errorf("absolute inside path not relativized: %q %v", rel, err)
	}
	rel, err = relWithinRoot(root, "./a/b.go")
	if err != nil || rel != "a/b.go" {
		t.Errorf("relative path not normalized: %q %v", rel, err)
	}
}

// TestRelWithinRootCrossPlatformMatrix is the v1.3.5 table-driven
// regression suite for the model-facing path boundary. Every rejection
// case must be refused on EVERY platform (the previous implementation
// accepted /etc/passwd on Windows, where filepath.IsAbs cannot see a
// POSIX-rooted path); every acceptance case must normalize to the
// workspace-relative slash form.
func TestRelWithinRootCrossPlatformMatrix(t *testing.T) {
	root := t.TempDir()

	rejected := []struct {
		name string
		in   string
	}{
		{"traversal dotdot slash", "../secret"},
		{"traversal dotdot backslash", "..\\secret"},
		{"traversal nested slash", "foo/../../secret"},
		{"traversal nested backslash", "foo\\..\\..\\secret"},
		{"POSIX absolute passwd", "/etc/passwd"},
		{"POSIX absolute other", "/foo/bar"},
		{"Windows drive backslash", `C:\Windows\System32`},
		{"Windows drive slash", "C:/Windows/System32"},
		{"Windows drive other volume", `D:\secret`},
		{"Windows drive-relative", "C:secret"},
		{"Windows rooted backslash", "\\foo"},
		{"Windows rooted backslash nested", "\\foo\\bar"},
		{"UNC backslash", `\\server\share\secret`},
		{"UNC slash", "//server/share/secret"},
		{"root itself", "/"},
	}

	for _, tc := range rejected {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			if rel, err := relWithinRoot(root, tc.in); err == nil {
				t.Fatalf("path %q accepted as %q — rooted/volume/traversal forms must be refused on every platform", tc.in, rel)
			}
		})
	}

	accepted := []struct {
		name string
		in   string
		want string
	}{
		{"plain relative", "internal/api.go", "internal/api.go"},
		{"backslash relative", "src\\main.go", "src/main.go"},
		{"explicit current dir", "./internal/repoindex/tool.go", "internal/repoindex/tool.go"},
		{"nested relative", "a/b/c.go", "a/b/c.go"},
		{"inner traversal stays inside", "foo/../bar.go", "bar.go"},
		{"absolute inside root", root + "/sub/file.go", "sub/file.go"},
		{"empty means no constraint", "", ""},
	}

	for _, tc := range accepted {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			rel, err := relWithinRoot(root, tc.in)
			if err != nil {
				t.Fatalf("path %q rejected: %v", tc.in, err)
			}
			if rel != tc.want {
				t.Fatalf("path %q normalized to %q, want %q", tc.in, rel, tc.want)
			}
		})
	}

	// Absolute-outside on the host platform: relative computation
	// succeeds but lands outside the root, so the traversal check is
	// what refuses it.
	if _, err := relWithinRoot(root, root+"/../sibling.go"); err == nil {
		t.Fatal("absolute sibling escape accepted")
	}
}

func TestToolRunRejectsEscapingPaths(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	tool := NewTool(f.store(), func() string { return f.root })
	_, err := tool.Run(context.Background(), []byte(`{"depsOf": "../../etc/passwd"}`))
	if err == nil {
		t.Error("escaping depsOf accepted")
	}
}

// ---------------------------------------------------------------------------
// Persistence / reload
// ---------------------------------------------------------------------------

func TestPersistenceReload(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	// A brand-new store instance over the same data dir reloads the
	// persisted index and answers queries identically.
	s2 := NewStore(f.data)
	st := s2.Status(f.root)
	if st.State == "empty" {
		t.Fatal("persisted index not reloaded")
	}
	if st.Files != 10 || st.Symbols == 0 || st.DepEdges == 0 {
		t.Errorf("reloaded index incomplete: %+v", st)
	}

	report, err := s2.Search(context.Background(), f.root, Query{Symbol: "Runner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) == 0 || report.Results[0].Path != "main.go" {
		t.Errorf("reloaded search failed: %+v", report)
	}
}

func TestUpdateWithoutChangeDoesNotRewriteFile(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	indexPath := filepath.Join(f.data, rootKey(f.root)+".json")
	first, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	f.update()

	second, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !second.ModTime().Equal(first.ModTime()) {
		t.Error("unchanged update rewrote the persisted index file")
	}
}

// ---------------------------------------------------------------------------
// Tool + evidence block
// ---------------------------------------------------------------------------

func TestToolRunEndToEnd(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()

	tool := NewTool(f.store(), func() string { return f.root })

	out, err := tool.Run(context.Background(), []byte(`{"symbol": "Helper"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "internal/util/util.go") {
		t.Errorf("tool output missing hit:\n%s", out)
	}
	if !strings.Contains(out, "repoindex:") {
		t.Errorf("tool output missing header:\n%s", out)
	}

	out, err = tool.Run(context.Background(), []byte(`{"task": "fix the parser for typescript inputs"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "src/parser.ts") {
		t.Errorf("task-mode output missing top hit:\n%s", out)
	}
}

func TestEvidenceBlockBoundedAndEmptySafe(t *testing.T) {
	f := newFixture(t)
	f.buildTree()
	f.update()
	s := f.store()

	if got := s.EvidenceBlock(f.root, "", 400); got != "" {
		t.Errorf("empty task must produce an empty block, got %q", got)
	}

	block := s.EvidenceBlock(f.root, "repair the helper implementation in util", 400)
	if block == "" {
		t.Fatal("task with hits produced no evidence block")
	}
	if !strings.Contains(block, "internal/util/util.go") {
		t.Errorf("evidence block missing the relevant file:\n%s", block)
	}
	if len(block) > maxEvidenceBlockBytes {
		t.Errorf("evidence block %d bytes exceeds cap %d", len(block), maxEvidenceBlockBytes)
	}
}

// ---------------------------------------------------------------------------
// Git-aware signals (requires the git binary)
// ---------------------------------------------------------------------------

func TestGitSignals(t *testing.T) {
	if _, err := execLookPath("git"); err != nil {
		t.Skip("git not available")
	}

	f := newFixture(t)
	f.buildTree()
	gitInit(t, f.root)
	gitRun(t, f.root, "add", ".")
	gitRun(t, f.root, "-c", "user.email=fixture@example.com", "-c", "user.name=fixture", "commit", "-m", "init")

	// A clean committed tree: everything tracked.
	f.update()
	rec := f.storeRecord("main.go")
	if rec == nil || rec.GitState != "tracked" {
		t.Errorf("committed file should be tracked: %+v", rec)
	}

	// Modified tracked file + untracked new file.
	time.Sleep(20 * time.Millisecond)
	f.write("main.go", mainGoFixture+"\n// touched\n")
	f.write("brand_new.go", "package main\n")
	f.update()

	rec = f.storeRecord("main.go")
	if rec == nil || rec.GitState != "modified" {
		t.Errorf("edited tracked file should be modified: %+v", rec)
	}
	rec = f.storeRecord("brand_new.go")
	if rec == nil || rec.GitState != "untracked" {
		t.Errorf("new file should be untracked: %+v", rec)
	}
}

func TestGitSignalsOptional(t *testing.T) {
	// A non-git fixture indexes fine with all signals empty.
	f := newFixture(t)
	f.buildTree()
	f.update()

	st := f.store().Status(f.root)
	if st.GitAvailable {
		t.Error("non-git fixture must not claim git availability")
	}
	rec := f.storeRecord("main.go")
	if rec == nil || rec.GitState != "" {
		t.Errorf("non-git fixture must leave GitState empty: %+v", rec)
	}
}

// ---------------------------------------------------------------------------
// git helpers
// ---------------------------------------------------------------------------

func execLookPath(bin string) (string, error) {
	return exec.LookPath(bin)
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init", "-q")
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
