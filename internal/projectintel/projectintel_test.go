package projectintel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()

	for rel, content := range files {
		full := filepath.Join(root, rel)

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func TestObserveDetectsGoProject(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":        "module example.com/x\n\ngo 1.22\n",
		"main.go":       "package main\n",
		"internal/a.go": "package a\n",
		"internal/b.go": "package b\n",
		"internal/c.go": "package c\n",
		"README.md":     "# x\n",
	})

	store := NewStore(t.TempDir())
	f, err := store.Observe(root)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	if len(f.Languages) == 0 || f.Languages[0] != "Go" {
		t.Fatalf("languages = %v, want Go first", f.Languages)
	}

	if f.BuildCmd != "go build ./..." || f.TestCmd != "go test ./..." {
		t.Fatalf("commands = %q / %q", f.BuildCmd, f.TestCmd)
	}

	if !strings.Contains(f.EntryHint, "go.mod") {
		t.Fatalf("entry hint = %q", f.EntryHint)
	}

	if !strings.Contains(f.Layout, "go.mod") {
		t.Fatalf("layout = %q", f.Layout)
	}
}

func TestObserveSkipsVendoredAndBuildDirs(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":              "module x\n",
		"main.go":             "package main\n",
		"node_modules/pkg.js": "// junk\n",
		"vendor/v.go":         "package v\n",
	})

	store := NewStore(t.TempDir())
	f, err := store.Observe(root)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// Only the two real .go files + go.mod exist for the census; the vendor
	// and node_modules trees must not be counted.
	if !strings.Contains(f.Layout, "2 files") {
		t.Fatalf("layout = %q — vendored dirs were not skipped", f.Layout)
	}
}

func TestVerifiedCommandsBeatInferred(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":  "module x\n",
		"main.go": "package main\n",
	})

	store := NewStore(t.TempDir())

	if _, err := store.Observe(root); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// The Lab verified a different, precise build command.
	if err := store.RecordVerifiedCommand(root, "build", "go build -tags headless ./internal/..."); err != nil {
		t.Fatalf("RecordVerifiedCommand: %v", err)
	}

	f, err := store.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if f.BuildCmd != "go build -tags headless ./internal/..." {
		t.Fatalf("verified command must replace inferred, got %q", f.BuildCmd)
	}

	if f.BuildVerifiedAt.IsZero() {
		t.Fatal("verification timestamp missing")
	}

	// Re-observation must NOT clobber the verified command with the
	// inferred default.
	if _, err := store.Observe(root); err != nil {
		t.Fatalf("re-Observe: %v", err)
	}

	f, _ = store.Load(root)
	if f.BuildCmd != "go build -tags headless ./internal/..." {
		t.Fatalf("re-observation clobbered verified command: %q", f.BuildCmd)
	}
}

func TestLessonsAreBoundedAndDeduped(t *testing.T) {
	root := writeProject(t, map[string]string{"a.txt": "x"})
	store := NewStore(t.TempDir())

	if err := store.Learn(root, "build fails unless CGO_ENABLED=0"); err != nil {
		t.Fatalf("Learn: %v", err)
	}

	// Duplicate is dropped.
	if err := store.Learn(root, "build fails unless CGO_ENABLED=0"); err != nil {
		t.Fatalf("Learn dup: %v", err)
	}

	// Fill past the bound.
	for i := 0; i < maxLessons+10; i++ {
		_ = store.Learn(root, strings.Repeat("l", 3)+strings.Repeat(string(rune('a'+i%26)), 1)+string(rune('0'+i%10)))
	}

	f, err := store.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(f.Lessons) != maxLessons {
		t.Fatalf("lessons = %d, want bound %d", len(f.Lessons), maxLessons)
	}

	// The oldest lesson was evicted by the newer ones.
	for _, l := range f.Lessons {
		if strings.Contains(l, "CGO_ENABLED") {
			t.Fatalf("oldest lesson must be evicted once the bound is hit: %v", f.Lessons)
		}
	}

	// The newest lessons are all present.
	if len(f.Lessons) == 0 {
		t.Fatal("newest lessons must survive the bound")
	}
}

func TestCardRendersMeasuredFacts(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":  "module x\n",
		"main.go": "package main\n",
	})

	store := NewStore(t.TempDir())

	if _, err := store.Observe(root); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	if err := store.RecordVerifiedCommand(root, "test", "go test ./... -count=1"); err != nil {
		t.Fatalf("RecordVerifiedCommand: %v", err)
	}

	if err := store.Learn(root, "tests race unless -count=1"); err != nil {
		t.Fatalf("Learn: %v", err)
	}

	card := store.Card(root)

	for _, want := range []string{
		"PROJECT INTELLIGENCE",
		"Languages: Go",
		"Test command (verified in Lab): go test ./... -count=1",
		"Lessons from past runs",
		"-count=1",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("card missing %q:\n%s", want, card)
		}
	}
}

func TestCardEmptyForUnknownProject(t *testing.T) {
	store := NewStore(t.TempDir())

	if card := store.Card(filepath.Join(t.TempDir(), "nowhere")); card != "" {
		t.Fatalf("unknown project must render no card, got %q", card)
	}
}

func TestProjectsAreIsolated(t *testing.T) {
	goRoot := writeProject(t, map[string]string{"go.mod": "module x\n", "a.go": "package a\n"})
	jsRoot := writeProject(t, map[string]string{"package.json": "{}", "a.js": "// x\n"})

	store := NewStore(t.TempDir())

	if _, err := store.Observe(goRoot); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Observe(jsRoot); err != nil {
		t.Fatal(err)
	}

	gf, _ := store.Load(goRoot)
	jf, _ := store.Load(jsRoot)

	if gf.BuildCmd != "go build ./..." {
		t.Fatalf("go project build = %q", gf.BuildCmd)
	}

	if jf.BuildCmd != "npm run build" {
		t.Fatalf("js project build = %q", jf.BuildCmd)
	}

	if err := store.Learn(goRoot, "go lesson"); err != nil {
		t.Fatal(err)
	}

	jf, _ = store.Load(jsRoot)
	if len(jf.Lessons) != 0 {
		t.Fatalf("lessons leaked across projects: %v", jf.Lessons)
	}
}

func TestRecordVerifiedCommandRejectsEmpty(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.RecordVerifiedCommand(t.TempDir(), "build", "  "); err == nil {
		t.Fatal("empty command must be rejected")
	}

	if err := store.RecordVerifiedCommand(t.TempDir(), "deploy", "x"); err == nil {
		t.Fatal("unknown kind must be rejected")
	}
}

func TestObserveMissingRootIsEmpty(t *testing.T) {
	store := NewStore(t.TempDir())

	f, err := store.Observe(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("missing root must not error: %v", err)
	}

	if !f.isEmpty() {
		t.Fatalf("missing root must yield empty facts: %+v", f)
	}
}
