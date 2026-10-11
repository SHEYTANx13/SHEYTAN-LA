// Package artifacts tracks files the agent creates so the UI can show them
// the moment they exist (v1.0.4): a Files view with preview / open /
// reveal / copy-path actions, plus compact "created files" chips under each
// chat turn.
//
// Detection is deliberately tool-agnostic. Two mechanisms combined:
//
//  1. TURN SNAPSHOT DIFF — before each agent turn the watched folders
//     (workspace/, charts/, logs/screenshots/, diagnostics/) are
//     fingerprinted; after the turn the diff IS the artifact list. This
//     catches EVERY creation path: the files tool, shell redirections,
//     python scripts, browser downloads, anything.
//  2. EXPLICIT REPORT — tools with known output paths (files.write,
//     chart renderer) call Report() so files created OUTSIDE the watched
//     dirs still surface.
package artifacts

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind is a coarse artifact category used for the big type icon.
type Kind string

const (
	KindChart       Kind = "chart"       // svg charts
	KindData        Kind = "data"        // csv/tsv/json/xlsx
	KindDoc         Kind = "doc"         // md/txt/pdf/docx
	KindCode        Kind = "code"        // go/py/js/...
	KindImage       Kind = "image"       // png/jpg/webp
	KindArchive     Kind = "archive"     // zip/tar/7z
	KindDiagnostics Kind = "diagnostics" // diagnostics bundles, logs
	KindOther       Kind = "other"
)

// Artifact is one created file.
type Artifact struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Dir     string    `json:"dir"` // parent folder (for grouping)
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Kind    Kind      `json:"kind"`
}

// Classify returns the artifact kind for a filename.
func Classify(name string) Kind {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".svg":
		return KindChart
	case ".csv", ".tsv", ".json", ".jsonl", ".xlsx", ".parquet":
		return KindData
	case ".md", ".markdown", ".txt", ".pdf", ".docx", ".rtf":
		return KindDoc
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return KindImage
	case ".zip", ".tar", ".gz", ".7z", ".rar":
		return KindArchive
	case ".log", ".jsonl.gz":
		return KindDiagnostics
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".sh", ".ps1", ".bat",
		".c", ".h", ".cpp", ".hpp", ".rs", ".java", ".rb", ".sql", ".html", ".css":
		return KindCode
	default:
		return KindOther
	}
}

// Tracker fingerprints watched directories and diffs them around agent
// turns. Safe for concurrent use.
type Tracker struct {
	mu       sync.Mutex
	watch    []string
	lastSnap map[string]fileState
	reported map[string]bool // explicit reports (dedup)
}

// maxEntriesPerDir bounds one directory listing so a pathological folder
// (a sandbox gone wild) can never stall the diff.
const maxEntriesPerDir = 400

// New builds a tracker for the given watch roots.
func New(watchDirs []string) *Tracker {
	return &Tracker{
		watch:    watchDirs,
		lastSnap: map[string]fileState{},
		reported: map[string]bool{},
	}
}

// fileState is the fingerprint used to detect change.
//
// v1.2.9: size-only fingerprints could not detect SAME-SIZE content
// changes (a rewritten file of identical length — a common case for
// logs, JSON reports and code edits — was invisible). The fingerprint
// now carries size + modification metadata (ModTime, nanosecond
// resolution on mainstream filesystems) plus a BOUNDED content digest:
// the first 64 KiB hashed with FNV-1a, computed only for files up to
// 2 MiB so large artifacts never pay a full-file read (performance and
// huge files stay protected — the digest is 0 for those, and their
// change detection falls back to size+modtime).
type fileState struct {
	size    int64
	modNano int64
	digest  uint64
}

// digestCapBytes bounds the hashed prefix of one file.
const digestCapBytes = 64 << 10

// digestMaxFileBytes is the file-size ceiling for computing a digest at
// all (beyond this, hashing is skipped: size+modtime carry the signal).
const digestMaxFileBytes = 2 << 20

// fileDigest returns a bounded FNV-1a digest of the first 64 KiB of
// path (0 when the file is too large to hash or unreadable).
func fileDigest(path string, size int64) uint64 {
	if size <= 0 || size > digestMaxFileBytes {
		return 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	buf := make([]byte, min(digestCapBytes, size))
	n, _ := io.ReadFull(f, buf)
	if n <= 0 {
		return 0
	}
	var h uint64 = 14695981039346656037
	for _, b := range buf[:n] {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

func snapshot(roots []string) map[string]fileState {
	out := map[string]fileState{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		n := 0
		for _, e := range entries {
			if n >= maxEntriesPerDir {
				break
			}
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			n++
			path := filepath.Join(root, e.Name())
			out[path] = fileState{
				size:    info.Size(),
				modNano: info.ModTime().UnixNano(),
				digest:  fileDigest(path, info.Size()),
			}
		}
	}
	return out
}

// BeginTurn records the current state of the watched folders.
func (t *Tracker) BeginTurn() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastSnap = snapshot(t.watch)
	t.reported = map[string]bool{}
}

// Report explicitly registers a created file (tools with known outputs).
// Reported paths surface in EndTurn even when they live outside the
// watched dirs. Missing files are ignored.
func (t *Tracker) Report(path string) {
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		return // deleted or moved on — nothing to show
	}
	t.mu.Lock()
	t.reported[abs(path)] = true
	t.mu.Unlock()
}

func abs(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, p)
	}
	return filepath.Clean(p)
}

// changedFile reports whether the AFTER fingerprint differs from BEFORE
// under the v1.2.9 rules:
//   - new file, or size change → changed;
//   - small files (digest != 0): the bounded CONTENT digest decides —
//     a pure mtime touch with identical content is NOT a change, while
//     a same-size rewrite (even with a forged mtime) IS;
//   - large files (digest == 0, hashing skipped): mtime carries the
//     signal (a rewrite always bumps it).
func changedFile(prev, cur fileState) bool {
	if prev.size != cur.size {
		return true
	}
	if cur.digest != 0 || prev.digest != 0 {
		return prev.digest != cur.digest
	}
	return prev.modNano != cur.modNano
}

// EndTurn diffs against the BeginTurn snapshot and returns every file that
// appeared (or changed) during the turn, newest first. Change detection
// follows changedFile (v1.2.9: same-size rewrites detected, pure mtime
// touches on unchanged content ignored).
func (t *Tracker) EndTurn() []Artifact {
	t.mu.Lock()
	before := t.lastSnap
	reported := t.reported
	t.mu.Unlock()

	if before == nil {
		before = map[string]fileState{}
	}
	now := snapshot(t.watch)

	seen := map[string]bool{}
	var out []Artifact
	add := func(path string) {
		if seen[path] {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		seen[path] = true
		out = append(out, Artifact{
			Path:    path,
			Name:    filepath.Base(path),
			Dir:     filepath.Dir(path),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Kind:    Classify(path),
		})
	}

	for path := range now {
		prev, existed := before[path]
		if !existed || changedFile(prev, now[path]) {
			add(path)
		}
	}
	for path := range reported {
		add(path)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out
}

// Scan lists every file currently present in the watched roots, newest
// first (the Files view content). Capped at `limit` entries.
func (t *Tracker) Scan(limit int) []Artifact {
	t.mu.Lock()
	roots := append([]string{}, t.watch...)
	t.mu.Unlock()
	if limit <= 0 {
		limit = 200
	}
	now := snapshot(roots)
	var out []Artifact
	for path := range now {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		out = append(out, Artifact{
			Path:    path,
			Name:    filepath.Base(path),
			Dir:     filepath.Dir(path),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Kind:    Classify(path),
		})
		if len(out) >= limit*2 { // over-collect, then trim after sort
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// WatchDirs returns the configured roots (UI "open folder" actions).
func (t *Tracker) WatchDirs() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string{}, t.watch...)
}
