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

        clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(req.Filename)))
        if strings.HasPrefix(clean, "/") || strings.Contains(clean, "..") || clean == "." || clean == "" {
                return nil, fmt.Errorf("artifact filename %q escapes the task artifact root", req.Filename)
        }

        name := filepath.Base(clean)
        if !safeName.MatchString(name) {
                return nil, fmt.Errorf("artifact filename %q is not path-safe", name)
        }

        kind := Classify(name)

        r.mu.Lock()
        defer r.mu.Unlock()

        taskID := safeIDPat.ReplaceAllString(req.TaskID, "")
        if taskID == "" {
                return nil, fmt.Errorf("task id is not path-safe")
        }
        // Defense in depth: traversal-looking ids are REJECTED, not sanitized.
        if strings.ContainsAny(req.TaskID, "/\\") || strings.Contains(req.TaskID, "..") {
                return nil, fmt.Errorf("task id %q is not path-safe", req.TaskID)
        }

        taskRoot := filepath.Join(r.root, taskID)
        abs := filepath.Join(taskRoot, filepath.FromSlash(clean))

        // Defense in depth: the resolved path MUST stay under the task root.
        if !strings.HasPrefix(abs, taskRoot+string(filepath.Separator)) {
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

        if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
                return nil, fmt.Errorf("artifact dir: %w", err)
        }

        // ATOMIC write: temp file + rename. A REPLACEMENT first archives the
        // superseded current file so every version stays readable.
        if prevCurrent != nil {
                vdir := filepath.Join(taskRoot, ".versions")
                if err := os.MkdirAll(vdir, 0o755); err == nil {
                        ext := filepath.Ext(abs)
                        base := strings.TrimSuffix(filepath.Base(abs), ext)
                        _ = os.Rename(abs, filepath.Join(vdir, fmt.Sprintf("%s.v%d%s", base, prevCurrent.Version, ext)))
                        prevCurrent.Path = filepath.Join(vdir, fmt.Sprintf("%s.v%d%s", base, prevCurrent.Version, ext))
                        prevCurrent.IsCurrent = false
                }
        }

        tmp := abs + ".tmp"
        if err := os.WriteFile(tmp, req.Content, 0o644); err != nil {
                return nil, fmt.Errorf("write artifact: %w", err)
        }
        if err := os.Rename(tmp, abs); err != nil {
                _ = os.Remove(tmp)
                return nil, fmt.Errorf("commit artifact: %w", err)
        }

        sum := sha256.Sum256(req.Content)

        meta := &Meta{
                ID:      fmt.Sprintf("art-%d-%s", r.next, hex.EncodeToString(sum[:4])),
                TaskID:  req.TaskID,
                RunID:   req.RunID,
                Source:  req.Source,
                Created: time.Now().UTC(),
                Kind:    kind,
                Title:   strings.TrimSpace(req.Title),
                Path:    abs,
                RelPath: clean,
                Size:    int64(len(req.Content)),
                Version: version,
                Hash:    hex.EncodeToString(sum[:8]),
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

var safeIDPat = regexp.MustCompile(`[^A-Za-z0-9_-]`)

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

// CleanupTask removes a task's artifact scope (task teardown).
func (r *TaskRegistry) CleanupTask(taskID string) error {
        r.mu.Lock()
        defer r.mu.Unlock()

        safe := safeIDPat.ReplaceAllString(taskID, "")
        if safe == "" {
                return fmt.Errorf("task id required")
        }

        for id, m := range r.items {
                if m.TaskID == taskID {
                        delete(r.items, id)
                }
        }
        r.persistLocked()

        return os.RemoveAll(filepath.Join(r.root, safe))
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
