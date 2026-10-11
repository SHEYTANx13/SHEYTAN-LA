// Package projectintel — v1.1.5 Phase 6: persistent project intelligence.
//
// A local agent works in the same projects repeatedly, yet every session
// starts from zero: the model re-discovers the language, the build command,
// the layout, and re-makes the same mistakes. This package gives SHEYTAN a
// per-project memory of MEASURED facts:
//
//   - what the project is (languages, build system, layout, entry points)
//   - which commands actually built and tested it (learned from Lab
//     verification outcomes — never from model claims)
//   - lessons: what failed in past runs and what fixed it
//
// Facts are keyed by project root and stored one JSON file per project
// under <DataDir>/projectintel/. Everything is bounded: the structural
// observation walks at most maxObserveFiles files, lessons are a bounded
// FIFO, and the rendered card is a compact block the orchestrator can
// inject like a recall block.
//
// Trust rules mirror the memory store: observed structure is provisional
// fact; commands are recorded only after they VERIFIED in the Lab — the
// same "model proposes, tools execute, lab verifies" contract as the rest
// of the system. Nothing here can mark a project "verified"; the card only
// carries what was measured.
package projectintel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// maxObserveFiles bounds the structural walk.
	maxObserveFiles = 4000
	// maxObserveDepth bounds the structural walk depth.
	maxObserveDepth = 12
	// maxLessons bounds the lesson FIFO per project.
	maxLessons = 24
	// maxLessonLen bounds one lesson line.
	maxLessonLen = 200
	// maxConventions bounds the conventions list.
	maxConventions = 12
)

// Facts is the persisted per-project record. One file per project root.
type Facts struct {
	Root        string   `json:"root"`
	Languages   []string `json:"languages,omitempty"`
	BuildCmd    string   `json:"buildCmd,omitempty"`
	TestCmd     string   `json:"testCmd,omitempty"`
	LintCmd     string   `json:"lintCmd,omitempty"`
	Layout      string   `json:"layout,omitempty"`
	EntryHint   string   `json:"entryHint,omitempty"`
	Conventions []string `json:"conventions,omitempty"`
	Lessons     []string `json:"lessons,omitempty"`

	BuildVerifiedAt time.Time `json:"buildVerifiedAt,omitempty"`
	TestVerifiedAt  time.Time `json:"testVerifiedAt,omitempty"`
	LastObservedAt  time.Time `json:"lastObservedAt,omitempty"`
}

// Store manages per-project facts files under one directory.
type Store struct {
	dir string
	mu  sync.Mutex

	// cardCache (v1.2.4): (size, mtime)-validated parse cache per project
	// root. Card() runs every turn; re-reading + re-parsing the JSON facts
	// file each time was measurable I/O for data that changes only when a
	// verified command lands or a lesson is learned.
	cardCache map[string]*cardEntry
}

// cardEntry caches parsed facts keyed by the disk identity they came from.
type cardEntry struct {
	facts Facts
	size  int64
	mod   time.Time
}

// NewStore returns a store rooted at dir (created lazily on first write).
func NewStore(dir string) *Store {
	return &Store{dir: dir, cardCache: map[string]*cardEntry{}}
}

// rootKey hashes a project root to a stable file name.
func rootKey(root string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(filepath.Clean(root))))
	return hex.EncodeToString(sum[:8])
}

// pathFor returns the facts file path for a root.
func (s *Store) pathFor(root string) string {
	return filepath.Join(s.dir, rootKey(root)+".json")
}

// Load reads the persisted facts for a root (empty Facts when absent).
func (s *Store) Load(root string) (Facts, error) {
	root = filepath.Clean(root)

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked(root)
}

// loadLocked reads facts without taking the mutex (caller holds it).
// Served from the validated parse cache when the file is unchanged.
func (s *Store) loadLocked(root string) (Facts, error) {
	path := s.pathFor(root)
	if fi, err := os.Stat(path); err == nil {
		if e := s.cardCache[rootKey(root)]; e != nil && e.size == fi.Size() && e.mod.Equal(fi.ModTime()) {
			return e.facts, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return Facts{Root: root}, nil
			}
			return Facts{Root: root}, err
		}
		var f Facts
		if err := json.Unmarshal(data, &f); err != nil {
			return Facts{Root: root}, fmt.Errorf("projectintel: parse %s: %w", root, err)
		}
		f.Root = root
		s.cardCache[rootKey(root)] = &cardEntry{facts: f, size: fi.Size(), mod: fi.ModTime()}
		return f, nil
	}
	return Facts{Root: root}, nil
}

// save writes the facts file (caller holds the lock).
func (s *Store) save(f Facts) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.pathFor(f.Root) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, s.pathFor(f.Root))
}

// Observe performs a bounded structural scan of the project root and merges
// the measured facts into the persisted record (languages, build system
// defaults, layout). Existing VERIFIED commands are never overwritten by
// observation — measurement wins over inference, and verified beats guessed.
func (s *Store) Observe(root string) (Facts, error) {
	root = filepath.Clean(root)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadLocked(root)
	if err != nil {
		return f, err
	}

	scan := scanProject(root)

	if len(scan.Languages) > 0 {
		f.Languages = scan.Languages
	}

	if f.BuildCmd == "" && scan.BuildCmd != "" {
		f.BuildCmd = scan.BuildCmd
	}

	if f.TestCmd == "" && scan.TestCmd != "" {
		f.TestCmd = scan.TestCmd
	}

	if f.LintCmd == "" && scan.LintCmd != "" {
		f.LintCmd = scan.LintCmd
	}

	if f.Layout == "" {
		f.Layout = scan.Layout
	}

	if f.EntryHint == "" {
		f.EntryHint = scan.EntryHint
	}

	f.LastObservedAt = time.Now().UTC()

	if err := s.save(f); err != nil {
		return f, err
	}

	return f, nil
}

// RecordVerifiedCommand records a build/test command that PASSED objective
// Lab verification against the project at root. Verified commands replace
// inferred ones; the timestamp marks the measurement.
func (s *Store) RecordVerifiedCommand(root, kind, command string) error {
	root = filepath.Clean(root)
	command = strings.TrimSpace(command)

	if command == "" {
		return errors.New("projectintel: empty command")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadLocked(root)
	if err != nil {
		return err
	}

	switch strings.ToLower(kind) {
	case "build":
		f.BuildCmd = command
		f.BuildVerifiedAt = time.Now().UTC()
	case "test":
		f.TestCmd = command
		f.TestVerifiedAt = time.Now().UTC()
	case "lint":
		f.LintCmd = command
	default:
		return fmt.Errorf("projectintel: unknown command kind %q", kind)
	}

	return s.save(f)
}

// Learn appends a lesson to the bounded FIFO. Lessons are durable "what
// failed and what fixed it" lines recalled into future runs on the same
// project.
func (s *Store) Learn(root, lesson string) error {
	root = filepath.Clean(root)
	lesson = strings.TrimSpace(lesson)

	if lesson == "" {
		return errors.New("projectintel: empty lesson")
	}

	if len(lesson) > maxLessonLen {
		lesson = lesson[:maxLessonLen-1] + "…"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadLocked(root)
	if err != nil {
		return err
	}

	// Deduplicate: the same lesson twice is noise, not knowledge.
	if contains(f.Lessons, lesson) {
		return nil
	}

	f.Lessons = append(f.Lessons, lesson)
	if len(f.Lessons) > maxLessons {
		f.Lessons = f.Lessons[len(f.Lessons)-maxLessons:]
	}

	return s.save(f)
}

// AddConvention appends a project convention note (bounded).
func (s *Store) AddConvention(root, convention string) error {
	root = filepath.Clean(root)
	convention = strings.TrimSpace(convention)

	if convention == "" {
		return errors.New("projectintel: empty convention")
	}

	if len(convention) > maxLessonLen {
		convention = convention[:maxLessonLen-1] + "…"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadLocked(root)
	if err != nil {
		return err
	}

	if contains(f.Conventions, convention) {
		return nil
	}

	f.Conventions = append(f.Conventions, convention)
	if len(f.Conventions) > maxConventions {
		f.Conventions = f.Conventions[len(f.Conventions)-maxConventions:]
	}

	return s.save(f)
}

// scanResult is the measured structural view of one project.
type scanResult struct {
	Languages []string
	BuildCmd  string
	TestCmd   string
	LintCmd   string
	Layout    string
	EntryHint string
}

// scanProject walks the project root within the observation bounds and
// derives languages, build-system commands, and a layout summary from what
// is actually on disk.
func scanProject(root string) scanResult {
	var scan scanResult

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return scan
	}

	extCount := map[string]int{}
	dirs := map[string]bool{}
	buildFiles := []string{}

	fileCount := 0

	// Bounded walk: no symlink follow, depth- and count-capped, common
	// dependency/VCS dirs skipped.
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}

		depth := strings.Count(rel, string(filepath.Separator))

		if d.IsDir() {
			if path != root {
				switch d.Name() {
				case ".git", "node_modules", "vendor", "dist",
					"build", "target", "models", "logs", "caches", "tmp":
					return filepath.SkipDir
				}
			}

			if depth >= maxObserveDepth {
				return filepath.SkipDir
			}

			return nil
		}

		if fileCount >= maxObserveFiles {
			return filepath.SkipAll
		}

		fileCount++

		name := d.Name()
		switch name {
		case "go.mod", "package.json", "CMakeLists.txt", "Makefile",
			"Cargo.toml", "pom.xml", "build.gradle", "build.gradle.kts",
			"pyproject.toml", "requirements.txt", "setup.py",
			"mix.exs", "composer.json", "Gemfile", "go.sum",
			"deno.json", "bun.lockb":
			buildFiles = append(buildFiles, name)
		}

		if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
			extCount[ext]++
		}

		if dir := filepath.Dir(rel); dir != "." {
			dirs[dir] = true
		}

		return nil
	})

	scan.Languages = languagesFromCensus(extCount)

	seen := map[string]bool{}
	for _, bf := range buildFiles {
		seen[bf] = true
	}

	switch {
	case seen["go.mod"]:
		scan.BuildCmd = "go build ./..."
		scan.TestCmd = "go test ./..."
		scan.EntryHint = "go.mod (Go module root)"
	case seen["package.json"]:
		scan.BuildCmd = "npm run build"
		scan.TestCmd = "npm test"
		scan.EntryHint = "package.json (Node project)"
	case seen["Cargo.toml"]:
		scan.BuildCmd = "cargo build"
		scan.TestCmd = "cargo test"
		scan.EntryHint = "Cargo.toml (Rust crate)"
	case seen["CMakeLists.txt"]:
		scan.BuildCmd = "cmake -S . -B build && cmake --build build"
		scan.TestCmd = "ctest --test-dir build"
		scan.EntryHint = "CMakeLists.txt (CMake)"
	case seen["pyproject.toml"], seen["setup.py"], seen["requirements.txt"]:
		scan.EntryHint = "Python project"
	case seen["Makefile"]:
		scan.BuildCmd = "make"
		scan.TestCmd = "make test"
		scan.EntryHint = "Makefile"
	}

	if len(buildFiles) > 0 {
		var markers []string
		for _, bf := range buildFiles {
			if !contains(markers, bf) {
				markers = append(markers, bf)
			}
		}

		scan.Layout = fmt.Sprintf(
			"%d files, %d subdirectories; markers: %s",
			fileCount, len(dirs), strings.Join(markers, ", "),
		)
	}

	return scan
}

// languagesFromCensus maps an extension census to the dominant languages
// (those covering ≥10% of files, top 4, sorted).
func languagesFromCensus(extCount map[string]int) []string {
	total := 0
	for _, n := range extCount {
		total += n
	}

	if total == 0 {
		return nil
	}

	langOf := map[string]string{
		".go": "Go", ".rs": "Rust", ".py": "Python", ".js": "JavaScript",
		".ts": "TypeScript", ".tsx": "TypeScript", ".jsx": "JavaScript",
		".java": "Java", ".kt": "Kotlin", ".c": "C", ".h": "C",
		".cpp": "C++", ".cc": "C++", ".hpp": "C++", ".cs": "C#",
		".rb": "Ruby", ".php": "PHP", ".swift": "Swift", ".ex": "Elixir",
		".sh": "Shell", ".lua": "Lua", ".vue": "Vue", ".svelte": "Svelte",
	}

	merged := map[string]int{}
	for ext, n := range extCount {
		if lang, ok := langOf[ext]; ok {
			merged[lang] += n
		}
	}

	type lc struct {
		lang  string
		count int
	}

	var out []lc
	for lang, n := range merged {
		if n*100/total >= 10 {
			out = append(out, lc{lang, n})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].lang < out[j].lang
	})

	if len(out) > 4 {
		out = out[:4]
	}

	var langs []string
	for _, c := range out {
		langs = append(langs, c.lang)
	}

	return langs
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Card renders the compact project card for prompt injection — the whole
// point of persistent project intelligence: the model starts the task
// already knowing the project. Bounded to a short block.
func (s *Store) Card(root string) string {
	root = filepath.Clean(root)

	s.mu.Lock()
	f, err := s.loadLocked(root)
	s.mu.Unlock()

	if err != nil || f.isEmpty() {
		return ""
	}

	var b strings.Builder

	b.WriteString("## PROJECT INTELLIGENCE (measured facts about this project — verify before relying)\n")

	if len(f.Languages) > 0 {
		fmt.Fprintf(&b, "- Languages: %s\n", strings.Join(f.Languages, ", "))
	}

	if f.EntryHint != "" {
		fmt.Fprintf(&b, "- Entry: %s\n", f.EntryHint)
	}

	if f.BuildCmd != "" {
		verified := ""
		if !f.BuildVerifiedAt.IsZero() {
			verified = " (verified in Lab)"
		}
		fmt.Fprintf(&b, "- Build command%s: %s\n", verified, f.BuildCmd)
	}

	if f.TestCmd != "" {
		verified := ""
		if !f.TestVerifiedAt.IsZero() {
			verified = " (verified in Lab)"
		}
		fmt.Fprintf(&b, "- Test command%s: %s\n", verified, f.TestCmd)
	}

	if f.LintCmd != "" {
		fmt.Fprintf(&b, "- Lint command: %s\n", f.LintCmd)
	}

	if f.Layout != "" {
		fmt.Fprintf(&b, "- Layout: %s\n", f.Layout)
	}

	for _, c := range f.Conventions {
		if c != "" {
			fmt.Fprintf(&b, "- Convention: %s\n", c)
		}
	}

	if len(f.Lessons) > 0 {
		b.WriteString("- Lessons from past runs (what failed and what fixed it):\n")
		for _, l := range f.Lessons {
			fmt.Fprintf(&b, "  * %s\n", l)
		}
	}

	out := b.String()

	if len(out) > 4000 {
		out = out[:4000] + "\n…(truncated)\n"
	}

	return strings.TrimRight(out, "\n")
}

// isEmpty reports whether any measured fact exists.
func (f Facts) isEmpty() bool {
	return len(f.Languages) == 0 &&
		f.BuildCmd == "" && f.TestCmd == "" && f.LintCmd == "" &&
		f.Layout == "" && f.EntryHint == "" &&
		len(f.Conventions) == 0 && len(f.Lessons) == 0
}
