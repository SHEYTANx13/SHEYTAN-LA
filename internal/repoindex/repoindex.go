// Package repoindex implements ROADMAP v1.4 "Repository Intelligence"
// slice 1: a bounded, persistent repository index with a symbol /
// dependency graph, deterministic test/source relationships, optional
// Git-aware relevance metadata and a hybrid search API the agent
// (repo_search tool) and the Workspace surface consume.
//
// Design contract:
//
//   - ONE index per workspace root, persisted as a single JSON file
//     under the application data directory (<DataDir>/repoindex/) — the
//     same flat-file + atomic-rename architecture every other store in
//     this application uses. No second persistence system.
//   - INCREMENTAL: unchanged files are detected via (mtime, size) and
//     are never re-read; a bounded content digest (size + first
//     maxDigestBytes bytes) catches mtime-only touches. Only changed,
//     new and deleted files are re-parsed.
//   - BOUNDED: every dimension has a hard cap (files, walk depth, file
//     parse size, symbols per file, dependency edges per file, git
//     output, work budget per update). A pathological repository can
//     degrade the index's completeness — never the application.
//   - HONEST EVIDENCE: dependency edges and test relationships are
//     statement-derived structural facts (an actual import/include line,
//     a deterministic naming convention). Keyword/path relevance in
//     search results is INFERENCE and is labeled as such in every
//     result's Evidence string. Nothing is invented.
//   - Git signals (tracked/untracked/modified, recent change) are
//     relevance metadata only — never an authority for correctness —
//     and Git is entirely optional for indexing.
package repoindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bounds — every cap is a documented contract, not a magic number.
const (
	// maxIndexFiles caps the indexed file records. Files beyond the cap
	// are simply not indexed (Stats.Truncated reports it honestly).
	maxIndexFiles = 8000

	// maxWalkDepth caps directory recursion.
	maxWalkDepth = 24

	// maxParseFileBytes caps how much of ONE file the parsers may read.
	// Larger files are indexed (path/language/size) but their content is
	// not parsed; the record carries Skipped="file too large".
	maxParseFileBytes = 512 << 10

	// maxDigestBytes caps the content-digest input (first N bytes plus
	// the total size). A digest equal to the previous one within the
	// same size means unchanged content — a bounded, honest
	// approximation that catches real edits without re-reading files
	// whose (mtime, size) merely drifted.
	maxDigestBytes = 64 << 10

	// maxSymbolsPerFile / maxImportsPerFile / maxDepsPerFile cap the
	// per-record graph size.
	maxSymbolsPerFile = 400
	maxImportsPerFile = 120
	maxDepsPerFile    = 100

	// maxTestLinksPerFile caps the test-relationship list per record.
	maxTestLinksPerFile = 24

	// updateTimeBudget caps ONE Update() call's parsing work. The
	// update is resumable: a budget overrun stops the walk
	// (report.Partial) and the next call continues incrementally from
	// what was persisted.
	updateTimeBudget = 3 * time.Second

	// searchTimeBudget caps one Search() call (freshness work included).
	searchTimeBudget = 4 * time.Second

	// stalenessWindow: a search refreshes the index when it is older.
	stalenessWindow = 30 * time.Second

	// recentChangeWindow: files modified within this window carry the
	// Recent relevance signal (mtime-based; works without Git).
	recentChangeWindow = 7 * 24 * time.Hour

	// gitOutputCap caps the number of lines read from any git command.
	gitOutputCap = 4000

	// indexVersion bumps when the persisted layout changes
	// incompatibly; older files are discarded and rebuilt.
	indexVersion = 1
)

// Excluded directory names — never indexed (dependencies, build output,
// VCS internals). Every entry is a directory NAME matched exactly.
var excludedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"target":       true,
	"models":       true,
	"logs":         true,
	"caches":       true,
	"tmp":          true,
	"__pycache__":  true,
	".next":        true,
	".venv":        true,
	"venv":         true,
	"coverage":     true,
}

// Symbol is one declared symbol of a file.
type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // func, method, type, class, interface, struct, const, var, key, define, enum, namespace
	Line int    `json:"line"`
}

// Dep is one resolved in-repository dependency edge (a structural fact:
// the source file literally contains the import/include statement that
// resolved to Path).
type Dep struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // import (Go/TS/JS) | include (C/C++)
}

// FileRecord is the bounded index record of one repository file.
type FileRecord struct {
	// Path is slash-separated and relative to the workspace root —
	// deterministic and path-safe (never absolute, never escaping).
	Path string `json:"path"`

	Language string `json:"language"`
	Role     string `json:"role"` // source | test | config | doc | asset | other

	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`

	// Digest is the bounded content digest; empty when the file was
	// never content-parsed (bounds skip).
	Digest string `json:"digest,omitempty"`

	// Skipped explains why content was NOT parsed (bounds/binary).
	Skipped string `json:"skipped,omitempty"`

	Package string   `json:"package,omitempty"` // Go package name
	Symbols []Symbol `json:"symbols,omitempty"`
	Imports []string `json:"imports,omitempty"` // raw specifiers (verified textual facts)
	Deps    []Dep    `json:"deps,omitempty"`    // resolved in-repo edges (verified structural facts)
	Tests   []string `json:"tests,omitempty"`   // related test files (deterministic naming/package evidence)

	// GitState is relevance metadata, not truth: "" unknown,
	// "tracked", "untracked", "modified".
	GitState string `json:"gitState,omitempty"`

	// Recent marks a modification within recentChangeWindow.
	Recent bool `json:"recent,omitempty"`
}

// Index is the persisted, per-root repository index.
type Index struct {
	Version      int           `json:"version"`
	Root         string        `json:"root"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	GitAvailable bool          `json:"gitAvailable"`
	Files        []*FileRecord `json:"files"`

	// Derived, in-memory only (never persisted): path -> record and
	// the reverse dependency map. Rebuilt after every load/update.
	byPath     map[string]*FileRecord
	importedBy map[string][]string
}

// Stats is the measured index summary (derived on demand).
type Stats struct {
	Files      int            `json:"files"`
	Languages  map[string]int `json:"languages"`
	Symbols    int            `json:"symbols"`
	DepEdges   int            `json:"depEdges"`
	TestLinks  int            `json:"testLinks"`
	Skipped    int            `json:"skipped"`
	Truncated  bool           `json:"truncated"`
	HasGraph   bool           `json:"hasGraph"`
	HasSymbols bool           `json:"hasSymbols"`
}

// Stats derives the summary from the index records.
func (idx *Index) Stats() Stats {
	st := Stats{Languages: map[string]int{}}
	for _, rec := range idx.Files {
		st.Files++
		st.Languages[rec.Language]++
		st.Symbols += len(rec.Symbols)
		st.DepEdges += len(rec.Deps)
		st.TestLinks += len(rec.Tests)
		if rec.Skipped != "" {
			st.Skipped++
		}
		if len(rec.Symbols) > 0 {
			st.HasSymbols = true
		}
		if len(rec.Deps) > 0 || len(rec.Tests) > 0 {
			st.HasGraph = true
		}
	}
	st.Truncated = len(idx.Files) >= maxIndexFiles
	return st
}

// rebuild derives the in-memory lookup structures. Called after every
// load and update. Pure map work over bounded records.
func (idx *Index) rebuild() {
	idx.byPath = make(map[string]*FileRecord, len(idx.Files))
	idx.importedBy = make(map[string][]string)
	for _, rec := range idx.Files {
		idx.byPath[rec.Path] = rec
	}
	for _, rec := range idx.Files {
		for _, dep := range rec.Deps {
			idx.importedBy[dep.Path] = append(idx.importedBy[dep.Path], rec.Path)
		}
	}
	// Deterministic order for every consumer.
	for k := range idx.importedBy {
		sort.Strings(idx.importedBy[k])
	}
}

// record returns the record for a normalized path (or nil).
func (idx *Index) record(path string) *FileRecord {
	if idx.byPath == nil {
		idx.rebuild()
	}
	return idx.byPath[path]
}

// Status is the cheap, walk-free index state report.
type Status struct {
	Root         string         `json:"root"`
	State        string         `json:"state"` // empty | ready | stale
	Files        int            `json:"files"`
	Symbols      int            `json:"symbols"`
	DepEdges     int            `json:"depEdges"`
	TestLinks    int            `json:"testLinks"`
	Languages    map[string]int `json:"languages,omitempty"`
	UpdatedAt    string         `json:"updatedAt,omitempty"`
	GitAvailable bool           `json:"gitAvailable,omitempty"`
	Truncated    bool           `json:"truncated,omitempty"`
	StaleRoot    bool           `json:"staleRoot,omitempty"` // index belongs to a different root
	Partial      bool           `json:"partial,omitempty"`   // last update hit its work budget
}

// UpdateReport is the measured result of one incremental update.
type UpdateReport struct {
	Root         string `json:"root"`
	Indexed      int    `json:"indexed"`   // files in the index after the update
	Reindexed    int    `json:"reindexed"` // content actually re-parsed this call
	Removed      int    `json:"removed"`   // entries dropped (deleted files)
	Unchanged    int    `json:"unchanged"` // skipped via mtime/size/digest
	Partial      bool   `json:"partial"`   // work budget exhausted before completion
	Truncated    bool   `json:"truncated"` // maxIndexFiles reached
	DurationMs   int64  `json:"durationMs"`
	GitAvailable bool   `json:"gitAvailable"`
}

// Store is the per-install repoindex persistence + query facade. One
// instance serves every workspace root (one JSON file per root, like
// internal/projectintel).
type Store struct {
	dir string

	mu    sync.Mutex
	cache *cacheEntry
}

type cacheEntry struct {
	path  string
	size  int64
	mtime time.Time
	idx   *Index
}

// NewStore creates the store over dir (the app-data repoindex folder).
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// rootKey derives the deterministic per-root persistence key (same
// scheme as projectintel: sha256 of the cleaned root, 16 hex chars).
func rootKey(root string) string {
	clean := filepath.ToSlash(filepath.Clean(root))
	sum := sha256.Sum256([]byte(strings.ToLower(clean)))
	return hex.EncodeToString(sum[:])[:16]
}

func (s *Store) indexPath(root string) string {
	return filepath.Join(s.dir, rootKey(root)+".json")
}

// loadLocked loads the index for root (cache -> disk -> nil). The
// caller holds s.mu.
func (s *Store) loadLocked(root string) *Index {
	path := s.indexPath(root)

	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}

	if s.cache != nil && s.cache.path == path &&
		s.cache.size == fi.Size() && s.cache.mtime.Equal(fi.ModTime()) {
		return s.cache.idx
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		// A corrupt index is rebuilt from scratch — never fatal.
		return nil
	}
	if idx.Version != indexVersion || !sameRoot(idx.Root, root) {
		return nil
	}

	idx.rebuild()
	s.cache = &cacheEntry{
		path:  path,
		size:  fi.Size(),
		mtime: fi.ModTime(),
		idx:   &idx,
	}
	return s.cache.idx
}

// saveLocked persists the index atomically (tmp + rename — the house
// pattern). The caller holds s.mu.
func (s *Store) saveLocked(idx *Index) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}

	path := s.indexPath(idx.Root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	if fi, err := os.Stat(path); err == nil {
		s.cache = &cacheEntry{
			path:  path,
			size:  fi.Size(),
			mtime: fi.ModTime(),
			idx:   idx,
		}
	}
	return nil
}

// TrimCache sheds the in-memory index cache (memory-manager contract:
// returns the approximate bytes reclaimed). The persisted index is
// untouched; the next access reloads it from disk.
func (s *Store) TrimCache() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache == nil {
		return 0
	}

	reclaimed := s.cache.size
	s.cache = nil
	return reclaimed
}

// Status reports the current index state WITHOUT walking the tree.
func (s *Store) Status(root string) Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.loadLocked(root)
	if idx == nil {
		return Status{Root: root, State: "empty"}
	}

	st := idx.Stats()
	state := "ready"
	if time.Since(idx.UpdatedAt) > stalenessWindow {
		state = "stale"
	}

	out := Status{
		Root:         root,
		State:        state,
		Files:        st.Files,
		Symbols:      st.Symbols,
		DepEdges:     st.DepEdges,
		TestLinks:    st.TestLinks,
		Languages:    st.Languages,
		UpdatedAt:    idx.UpdatedAt.UTC().Format(time.RFC3339),
		GitAvailable: idx.GitAvailable,
		Truncated:    st.Truncated,
		StaleRoot:    !sameRoot(idx.Root, root),
	}
	return out
}

// Update runs one bounded INCREMENTAL index pass over root:
//
//   - unchanged files (mtime+size match, then digest match) are kept
//     without re-reading their content;
//   - changed/new files are content-parsed within the per-file bounds;
//   - deleted files drop their records;
//   - the dependency graph, test links and git signals are re-derived
//     (pure in-memory work over the bounded record set);
//   - the result persists atomically when anything changed.
//
// A work-budget overrun stops the pass early (report.Partial); the
// next call resumes incrementally. ctx cancellation is honored at
// every step boundary.
func (s *Store) Update(ctx context.Context, root string) (UpdateReport, error) {
	if root == "" {
		return UpdateReport{}, errors.New("repoindex: empty workspace root")
	}

	deadline := time.Now().Add(updateTimeBudget)
	started := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return UpdateReport{}, err
	}

	existing := s.loadLocked(root)

	idx := &Index{
		Version:      indexVersion,
		Root:         root,
		Files:        make([]*FileRecord, 0, 256),
		GitAvailable: existing != nil && existing.GitAvailable,
	}

	report := UpdateReport{Root: root}

	seen := make(map[string]bool, 1024)

	// ---- Pass 1: walk + incremental content refresh ----------------
	if err := walkBounded(root, func(rel string, fi os.FileInfo) error {
		if ctx.Err() != nil {
			return errStopWalk
		}
		if len(idx.Files) >= maxIndexFiles {
			report.Truncated = true
			return errStopWalk
		}
		if time.Now().After(deadline) {
			report.Partial = true
			return errStopWalk
		}

		rec := refreshRecord(existing, root, rel, fi, &report)
		if rec == nil {
			return nil
		}

		seen[rel] = true
		idx.Files = append(idx.Files, rec)
		return nil
	}); err != nil {
		return report, err
	}

	// ---- Pass 2: deletions / partial-pass resume ---------------------
	if existing != nil {
		for _, old := range existing.Files {
			if seen[old.Path] {
				continue
			}
			if report.Partial {
				// Resume mode: keep records the shortened walk
				// could not revisit (the walk order is
				// deterministic, so a partial pass covers the
				// same prefix; unvisited records persist until
				// the next unbounded pass re-derives them).
				if len(idx.Files) >= maxIndexFiles {
					report.Truncated = true
					break
				}
				idx.Files = append(idx.Files, old)
				seen[old.Path] = true
				continue
			}
			report.Removed++
		}
	}

	sortRecords(idx.Files)

	// ---- Pass 3: resolve dependency edges ----------------------------
	resolveDependencies(idx)

	// ---- Pass 4: deterministic test/source relationships -------------
	linkTests(idx)

	// ---- Pass 5: git-aware relevance metadata (optional) --------------
	applyGitSignals(ctx, idx, &report)
	report.GitAvailable = idx.GitAvailable

	// ---- Pass 6: recent-change metadata -------------------------------
	now := time.Now()
	for _, rec := range idx.Files {
		rec.Recent = !rec.Mtime.IsZero() && now.Sub(rec.Mtime) <= recentChangeWindow
	}

	idx.UpdatedAt = time.Now()
	report.Indexed = len(idx.Files)
	report.DurationMs = time.Since(started).Milliseconds()

	// ---- Persist when anything changed --------------------------------
	if changed(existing, idx) {
		if err := s.saveLocked(idx); err != nil {
			return report, err
		}
	} else if s.cache != nil && s.cache.idx == existing {
		// Identical re-derivation: keep the persisted file, refresh
		// only the in-memory freshness so Status stays honest.
		existing.UpdatedAt = idx.UpdatedAt
	}

	return report, nil
}

// refreshRecord produces the record for one walked file: unchanged
// records are re-used without reading content; changed/new records get
// a bounded digest + parse. Returns nil for files that must not be
// indexed.
func refreshRecord(existing *Index, root, rel string, fi os.FileInfo, report *UpdateReport) *FileRecord {
	info := fileStat{size: fi.Size(), mtime: fi.ModTime()}
	language := DetectLanguage(rel)
	if language == "" {
		language = "other"
	}
	role := DetectRole(rel, language)

	if existing != nil {
		if old := existing.record(rel); old != nil {
			// Fast path: mtime+size equality means unchanged —
			// the file is never even opened.
			if old.Size == info.size && old.Mtime.Equal(info.mtime) {
				report.Unchanged++
				fresh := *old
				fresh.Language = language
				fresh.Role = role
				fresh.Recent = false
				return &fresh
			}
		}
	}

	// Content-level check: bounded digest. Equal digest + equal size
	// means unchanged content (mtime-only touch) — no re-parse.
	digest, err := contentDigest(root, rel, info.size)
	if err != nil {
		return nil
	}

	if existing != nil {
		if old := existing.record(rel); old != nil && old.Digest != "" &&
			old.Digest == digest && old.Size == info.size {
			report.Unchanged++
			fresh := *old
			fresh.Mtime = info.mtime
			fresh.Language = language
			fresh.Role = role
			fresh.Recent = false
			return &fresh
		}
	}

	// Real change (or first sight): parse within bounds.
	report.Reindexed++
	rec := &FileRecord{
		Path:     rel,
		Language: language,
		Role:     role,
		Size:     info.size,
		Mtime:    info.mtime,
		Digest:   digest,
	}

	if info.size > maxParseFileBytes {
		rec.Skipped = "file too large"
		return rec
	}

	parsed := ParseFile(root, rel, language)
	rec.Package = parsed.Package
	rec.Symbols = parsed.Symbols
	rec.Imports = parsed.Imports
	if parsed.Binary {
		rec.Skipped = "binary content"
	}
	return rec
}

// changed reports whether the newly built index differs from the
// persisted one.
func changed(old, newIdx *Index) bool {
	if old == nil {
		return true
	}
	if len(old.Files) != len(newIdx.Files) || old.GitAvailable != newIdx.GitAvailable {
		return true
	}
	for i, rec := range newIdx.Files {
		o := old.Files[i]
		if o.Path != rec.Path || o.Size != rec.Size || o.Digest != rec.Digest {
			return true
		}
		if o.Language != rec.Language || o.Role != rec.Role || o.Package != rec.Package {
			return true
		}
		if o.GitState != rec.GitState {
			return true
		}
		if len(o.Symbols) != len(rec.Symbols) || len(o.Imports) != len(rec.Imports) ||
			len(o.Deps) != len(rec.Deps) || len(o.Tests) != len(rec.Tests) {
			return true
		}
	}
	return false
}

// walkBounded walks root depth- and name-bounded, calling fn for every
// regular file with its slash-normalized root-relative path. Walk
// order is deterministic (WalkDir is lexical; the skip list is stable).
func walkBounded(root string, fn func(rel string, fi os.FileInfo) error) error {
	stopped := false
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if stopped {
			return errStopWalk
		}
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable subtree: skip honestly, keep walking
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}

		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if depth(rel) >= maxWalkDepth {
				return filepath.SkipDir
			}
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		fi, statErr := d.Info()
		if statErr != nil {
			return nil
		}

		if fn(rel, fi) == errStopWalk {
			stopped = true
			return errStopWalk
		}
		return nil
	})
	if stopped || errors.Is(err, errStopWalk) {
		return nil
	}
	return err
}

// depth counts path segments of a slash-relative path.
func depth(rel string) int {
	n := 1
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			n++
		}
	}
	return n
}

var errStopWalk = errors.New("repoindex: stop walk")

// fileStat is the (size, mtime) pair the incremental detector keys on.
type fileStat struct {
	size  int64
	mtime time.Time
}

// contentDigest computes the bounded content digest: sha256(size ||
// first maxDigestBytes bytes). Never reads more than maxDigestBytes.
func contentDigest(root, rel string, size int64) (string, error) {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	fmt.Fprintf(h, "%d\x00", size)

	buf := make([]byte, 32*1024)
	read := int64(0)
	for read < maxDigestBytes {
		n, rerr := f.Read(buf)
		if n > 0 {
			chunk := int64(n)
			if read+chunk > maxDigestBytes {
				chunk = maxDigestBytes - read
			}
			h.Write(buf[:chunk])
			read += chunk
		}
		if rerr != nil {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sortRecords(files []*FileRecord) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
}

// sameRoot compares two workspace roots path-semantically.
func sameRoot(a, b string) bool {
	return strings.EqualFold(
		filepath.ToSlash(filepath.Clean(a)),
		filepath.ToSlash(filepath.Clean(b)),
	)
}
