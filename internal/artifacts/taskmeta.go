// taskmeta.go — v1.7.0: FIRST-CLASS TASK/RUN ARTIFACTS.
//
// Extends the existing artifact tracker (whose file detection stays
// intact) with durable artifact METADATA linked to task and run:
//
//      task id, run id, source tool, created time, type, path, size,
//      version (per task+path replacement history), content hash.
//
// Agent creation goes through the validated artifact_create operation:
// task-scoped, path-safe, bounded, ATOMIC (temp + rename), registered in
// the artifact tracker, linked to the current task/run, and never
// silently written outside the task's artifact root.
//
// Markdown is a first-class kind: it renders (the UI viewer), versions,
// and carries task/run provenance like every other kind.

package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// Bounds (the creation contract — never unbounded).
const (
	MaxArtifactBytes = 8 << 20 // one artifact body: 8 MiB
	maxRegistryLines = 2048    // registry compaction bound
)

// SafeName is the path-safe artifact filename contract.
var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Meta is one registered artifact's durable metadata.
type Meta struct {
	ID      string    `json:"id"`
	TaskID  string    `json:"taskId"`
	RunID   string    `json:"runId,omitempty"`
	Source  string    `json:"source"` // producing tool ("artifact_create", "files", …)
	Created time.Time `json:"created"`
	Kind    Kind      `json:"kind"`
	Title   string    `json:"title,omitempty"`
	Path    string    `json:"path"` // absolute
	RelPath string    `json:"relPath"`
	Size    int64     `json:"size"`
	Version int       `json:"version"`
	Hash    string    `json:"hash,omitempty"`
	// IsCurrent marks the newest version of a replaced path.
	IsCurrent bool `json:"isCurrent,omitempty"`
}

// TaskRegistry is the durable task/run artifact metadata layer.
type TaskRegistry struct {
	root string // <root>/task-artifacts/<taskID>/…

	mu    sync.Mutex
	next  int64
	items map[string]*Meta // id → meta
}

// NewTaskRegistry builds the registry; root is a data-dir child.
func NewTaskRegistry(dataDir string) *TaskRegistry {
	return &TaskRegistry{
		root:  filepath.Join(dataDir, "task-artifacts"),
		next:  1,
		items: map[string]*Meta{},
	}
}

func (r *TaskRegistry) registryPath() string {
	return filepath.Join(r.root, "registry.jsonl")
}

// Load restores the registry from disk (a missing file is an empty
// registry); `next` advances past every seen sequence number.
func (r *TaskRegistry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m Meta
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue // corrupt line: skipped honestly
		}
		r.items[m.ID] = &m
		if n := seqOf(m.ID); n >= r.next {
			r.next = n + 1
		}
	}
	return nil
}

func seqOf(id string) int64 {
	// ids: art-<seq>-<hex>
	i := strings.Index(id, "-")
	j := strings.Index(id[i+1:], "-")
	if i < 0 || j < 0 {
		return 0
	}
	n, _ := strconv.ParseInt(id[i+1:i+1+j], 10, 64)
	return n
}

// CreateRequest is one validated creation.
type CreateRequest struct {
	TaskID   string
	RunID    string
	Source   string // producing tool
	Title    string
	Filename string // path-safe relative name (subdirs allowed: "sub/x.md")
	Content  []byte
}

// Create validates, writes ATOMICALLY under the task root, and registers
// the artifact with full provenance and a bumped version when the same
// task+relative path is replaced.
func (r *TaskRegistry) Create(req CreateRequest) (*Meta, error) {
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, fmt.Errorf("artifacts need a task id")
	}
	if strings.TrimSpace(req.Source) == "" {
		return nil, fmt.Errorf("artifacts need a source (producing tool)")
	}
	if len(req.Content) == 0 {
		return nil, fmt.Errorf("artifact content is empty")
	}
	if len(req.Content) > MaxArtifactBytes {
		return nil, fmt.Errorf("artifact too large (%d bytes, max %d)", len(req.Content), MaxArtifactBytes)
	}

	// clean is the LOGICAL, slash-delimited path (stored as RelPath in
	// manifests and the registry JSON). It is validated in the logical
	// domain FIRST and only then converted to a native path — the two
	// domains never share separator assumptions.
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(req.Filename)))
	if strings.HasPrefix(clean, "/") || strings.Contains(clean, "..") || clean == "." || clean == "" {
		return nil, fmt.Errorf("artifact filename %q escapes the task artifact root", req.Filename)
	}
	// v1.11.1 (Windows path repair): Windows accepts BOTH "/" and "\\"
	// as separators and treats a colon as drive/ADS syntax, so the
	// Windows-hostile spellings are rejected in the logical domain
	// before the filepath.FromSlash conversion below. On POSIX a colon
	// or backslash is an ordinary filename byte and stays accepted —
	// native OS semantics, unchanged POSIX behavior.
	if runtime.GOOS == "windows" {
		for _, comp := range strings.Split(clean, "/") {
			if comp == "" || strings.Contains(comp, ":") ||
				strings.HasSuffix(comp, ".") || strings.HasSuffix(comp, " ") ||
				isWindowsReservedName(comp) {
				return nil, fmt.Errorf("artifact filename %q is not path-safe on windows", req.Filename)
			}
		}
	}

	name := filepath.Base(clean)
	if !safeName.MatchString(name) {
		return nil, fmt.Errorf("artifact filename %q is not path-safe", name)
	}

	kind := Classify(name)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Fail closed BEFORE any path is built: the task id is a single
	// path component under the task-artifacts root and arrives
	// untrusted. Traversal-looking ids are REJECTED, not sanitized.
	if !validTaskID(req.TaskID) {
		return nil, fmt.Errorf("task id %q is not path-safe", req.TaskID)
	}
	taskID := req.TaskID

	taskRoot := filepath.Join(r.root, taskID)
	abs := filepath.Join(taskRoot, filepath.FromSlash(clean))

	// Defense in depth: the resolved path MUST stay under the task root.
	// v1.11.1: the prefix check is tightened with filepath.Rel + an
	// explicit filepath.IsLocal containment gate (a string prefix alone
	// is not a boundary), and every sink below runs through the
	// registry-root os.Root with the re-derived relative name.
	relToRoot, relErr := filepath.Rel(r.root, abs)
	if relErr != nil || relToRoot == ".." ||
		strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) ||
		!filepath.IsLocal(relToRoot) {
		return nil, fmt.Errorf("artifact path escapes the task artifact root")
	}

	// Version: same task + relative path → the replacement increments.
	version := 1
	var prevCurrent *Meta
	for _, m := range r.items {
		if m.TaskID == req.TaskID && m.RelPath == clean {
			if m.Version >= version {
				version = m.Version + 1
			}
			if m.IsCurrent {
				prevCurrent = m
			}
		}
	}

	// The registry root is created by the first write (config-derived,
	// never untrusted) — the rooted operations below require it.
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		return nil, err
	}
	root, rootErr := os.OpenRoot(r.root)
	if rootErr != nil {
		return nil, rootErr
	}
	defer root.Close()

	if err := root.MkdirAll(filepath.Dir(relToRoot), 0o755); err != nil {
		return nil, fmt.Errorf("artifact dir: %w", err)
	}

	// ATOMIC write: temp file + rename. A REPLACEMENT first archives the
	// superseded current file so every version stays readable.
	if prevCurrent != nil {
		// The archive lives under the TASK's own .versions directory
		// (the first relToRoot component is the grammar-gated task id).
		// v1.11.1 Windows fix: relToRoot is a NATIVE relative path
		// (filepath.Rel output — backslash-separated on Windows, "/" on
		// POSIX), so the first component is split with the helper below
		// instead of a hard-coded "/" lookup. On Windows that lookup
		// returned -1 and the unguarded slice panicked with
		// "slice bounds out of range [:-1]" on every replacement write.
		// The slice is taken only after the boundary is proven to exist
		// and to match the grammar-gated task id; anything else fails
		// closed with a controlled error, never a panic.
		relTaskID, ok := firstRelComponent(relToRoot)
		if !ok || relTaskID != taskID {
			return nil, fmt.Errorf("artifact path escapes the task artifact root")
		}
		// archiveDir/versioned are NATIVE os.Root-relative names built
		// with filepath.Join (native separators on every platform) —
		// the previous hard-coded "/" spellings mixed the logical and
		// native domains in metadata paths on Windows.
		archiveDir := filepath.Join(relTaskID, ".versions")
		if err := root.MkdirAll(archiveDir, 0o755); err == nil {
			ext := filepath.Ext(abs)
			base := strings.TrimSuffix(filepath.Base(abs), ext)
			versioned := filepath.Join(archiveDir, fmt.Sprintf("%s.v%d%s", base, prevCurrent.Version, ext))
			if rmErr := root.Rename(relToRoot, versioned); rmErr == nil {
				prevCurrent.Path = filepath.Join(r.root, versioned)
			}
			prevCurrent.IsCurrent = false
		}
	}

	tmpRel := relToRoot + ".tmp"
	if err := root.WriteFile(tmpRel, req.Content, 0o644); err != nil {
		return nil, fmt.Errorf("write artifact: %w", err)
	}
	if err := root.Rename(tmpRel, relToRoot); err != nil {
		_ = root.Remove(tmpRel)
		return nil, fmt.Errorf("commit artifact: %w", err)
	}

	sum := sha256.Sum256(req.Content)

	meta := &Meta{
		ID:        fmt.Sprintf("art-%d-%s", r.next, hex.EncodeToString(sum[:4])),
		TaskID:    req.TaskID,
		RunID:     req.RunID,
		Source:    req.Source,
		Created:   time.Now().UTC(),
		Kind:      kind,
		Title:     strings.TrimSpace(req.Title),
		Path:      abs,
		RelPath:   clean,
		Size:      int64(len(req.Content)),
		Version:   version,
		Hash:      hex.EncodeToString(sum[:8]),
		IsCurrent: true,
	}
	r.next++

	r.items[meta.ID] = meta
	if prevCurrent != nil {
		r.items[prevCurrent.ID] = prevCurrent
	}
	r.persistLocked()

	return meta, nil
}

// firstRelComponent splits the first component off a cleaned RELATIVE
// path inside the registry root (the filepath.Rel form). Both "/" and
// backslash are considered boundaries because the first component here
// is always the grammar-gated task id (validTaskID rejects both), so
// the earliest boundary is the true one on every platform: Windows
// Rel output is backslash-separated (and Windows APIs accept "/" too),
// POSIX output is "/"-separated and a backslash is an ordinary filename
// byte that cannot appear before the first "/" here. It reports false
// when no component boundary exists (a single-component path is never
// a valid artifact destination under a task directory) or the first
// component would be empty — the caller fails closed instead of
// slicing blindly.
func firstRelComponent(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	i := strings.IndexAny(p, `/\`)
	if i <= 0 {
		return "", false
	}
	return p[:i], true
}

// isWindowsReservedName reports whether a logical path component is a
// Windows reserved device basename (with or without an extension),
// which must never reach filesystem creation through the managed root.
// Applied only on Windows hosts (see the runtime.GOOS gate in Create).
func isWindowsReservedName(comp string) bool {
	stem := comp
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// validTaskID is the ONE grammar for a task id used as a single
// filesystem path component (v1.11.1 path-injection remediation —
// single-component identifier class, same grammar as
// internal/sessions). Task ids arrive from API callers and agent tool
// arguments (untrusted); every path built from one must stay inside the
// task-artifacts root. The grammar accepts the ordinary task ids the
// store and runtime generate, and rejects: empty/oversized ids, path
// separators (POSIX and Windows), NUL, volume/ADS colons, leading dots
// (".." included), whitespace-padded ids and non-grammar characters.
// Traversal-looking ids are REJECTED, never sanitized.
func validTaskID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	if strings.ContainsAny(id, "/\\") {
		return false
	}
	if strings.ContainsAny(id, "\x00:") {
		return false
	}
	if strings.HasPrefix(id, ".") {
		return false
	}
	if strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func (r *TaskRegistry) persistLocked() {
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		return
	}

	lines := make([]string, 0, len(r.items))
	for _, m := range r.items {
		if s, err := marshalJSON(m); err == nil {
			lines = append(lines, s)
		}
	}

	// Bounded compaction: newest entries survive.
	if len(lines) > maxRegistryLines {
		lines = lines[len(lines)-maxRegistryLines/2:]
	}

	tmp := r.registryPath() + ".tmp"
	if os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600) == nil {
		_ = os.Rename(tmp, r.registryPath())
	}
}

// List returns a task's artifacts, newest first.
func (r *TaskRegistry) List(taskID string) []Meta {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Meta
	for _, m := range r.items {
		if m.TaskID == taskID {
			out = append(out, *m)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].Version > out[j].Version
	})
	return out
}

// ListRun returns one run's artifacts.
func (r *TaskRegistry) ListRun(runID string) []Meta {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Meta
	for _, m := range r.items {
		if m.RunID == runID {
			out = append(out, *m)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

// Get returns one artifact's metadata.
func (r *TaskRegistry) Get(id string) (Meta, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.items[id]
	if !ok {
		return Meta{}, false
	}
	return *m, true
}

// Read returns one artifact's content (bounded by the creation contract;
// a manually enlarged file is truncated honestly).
func (r *TaskRegistry) Read(id string) ([]byte, error) {
	m, ok := r.Get(id)
	if !ok {
		return nil, fmt.Errorf("artifact %q not found", id)
	}

	data, err := os.ReadFile(m.Path)
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	if len(data) > MaxArtifactBytes {
		data = data[:MaxArtifactBytes]
	}
	return data, nil
}

// Versions returns the full replacement history of one task+path.
func (r *TaskRegistry) Versions(taskID, relPath string) []Meta {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Meta
	for _, m := range r.items {
		if m.TaskID == taskID && m.RelPath == relPath {
			out = append(out, *m)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// Delete removes one artifact version's file and metadata.
func (r *TaskRegistry) Delete(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.items[id]
	if !ok {
		return false
	}

	delete(r.items, id)
	r.persistLocked()
	_ = os.Remove(m.Path)
	return true
}

// CleanupTask removes a task's artifact scope (task teardown). The
// task id FAILS CLOSED through the task-id grammar before any path is
// built — a hostile id is an error, never a path outside the root.
func (r *TaskRegistry) CleanupTask(taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !validTaskID(taskID) {
		return fmt.Errorf("task id %q is not path-safe", taskID)
	}

	for id, m := range r.items {
		if m.TaskID == taskID {
			delete(r.items, id)
		}
	}
	r.persistLocked()

	// v1.11.1 sink-level rooting: the task-tree removal runs through the
	// registry-root os.Root with the IsLocal-gated relative name. A
	// missing root has nothing to remove (RemoveAll semantics).
	if _, statErr := os.Stat(r.root); statErr != nil {
		return nil
	}
	rel := taskID
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("artifact path escapes the task artifact root")
	}
	root, rootErr := os.OpenRoot(r.root)
	if rootErr != nil {
		return rootErr
	}
	defer root.Close()
	return root.RemoveAll(rel)
}

// --- small JSON helpers (avoid importing encoding/json twice in files) ---

func parseJSON(s string, v any) error {
	return jsonUnmarshal([]byte(s), v)
}

func marshalJSON(v any) (string, error) {
	b, err := jsonMarshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
