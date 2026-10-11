// tasktools.go — v1.7.0: TASK-SCOPED custom tools.
//
// Extends the ONE customtools system (same definitions, same schema
// validation, same executors, same permission controls, same output
// limits, same secret-redaction rules, same registry adapter) with tools
// that belong to a TASK/RUN:
//
//      CREATE → VALIDATE → OPTIONAL APPROVAL → REGISTER → EXECUTE →
//      CAPTURE RESULT → CLEAN UP / PROMOTE
//
// Task tools are stored under the existing custom-tools root (a
// task-scoped subdirectory — no second storage root) and remain strictly
// separate from globally persisted custom tools: they never appear in the
// global list, and they disappear with the task unless promoted.
//
// Defaults are safe: a newly created task tool is DISABLED and
// UNAPPROVED. Execution requires the explicit approval gate (the same
// permission posture as global custom tools); unapproved definitions are
// validated for schema honesty but never executed.

package customtools

import (
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "sort"
        "strings"
        "sync"
        "time"
)

// TaskDefinition is one task-scoped tool definition: the SAME Definition
// contract plus task/run ownership and the approval gate.
type TaskDefinition struct {
        Definition
        TaskID   string `json:"taskId"`
        RunID    string `json:"runId,omitempty"`
        Approved bool   `json:"approved"`
        // CreatedAt carries the task-scoped creation instant.
        CreatedAt time.Time `json:"taskCreatedAt,omitempty"`
}

// RegistrarFuncs adapts the ONE orchestrator registry (cycle-free: the
// agent side constructs these from *agent.Orchestrator).
type RegistrarFuncs struct {
        Register   func(t *Tool)
        Unregister func(name string)
}

// TaskStore manages task-scoped definitions under
// <dataDir>/custom-tools/task-scoped/<taskID>/<name>.json.
type TaskStore struct {
        root string

        mu    sync.Mutex
        cache map[string]*TaskDefinition // tool id → definition
}

// NewTaskStore opens (and lazily creates) the task-scoped layer.
func NewTaskStore(dataDir string) (*TaskStore, error) {
        root := filepath.Join(dataDir, "custom-tools", "task-scoped")

        if err := os.MkdirAll(root, 0o755); err != nil {
                return nil, fmt.Errorf("task tools dir: %w", err)
        }

        s := &TaskStore{root: root, cache: map[string]*TaskDefinition{}}
        s.reload()
        return s, nil
}

func (s *TaskStore) taskDir(taskID string) string {
        safe := safeID.ReplaceAllString(taskID, "")
        if safe == "" {
                safe = "task"
        }
        return filepath.Join(s.root, safe)
}

func (s *TaskStore) reload() {
        entries, err := os.ReadDir(s.root)
        if err != nil {
                return
        }

        for _, te := range entries {
                if !te.IsDir() {
                        continue
                }

                files, err := os.ReadDir(filepath.Join(s.root, te.Name()))
                if err != nil {
                        continue
                }

                for _, f := range files {
                        if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".json") {
                                continue
                        }

                        data, err := os.ReadFile(filepath.Join(s.root, te.Name(), f.Name()))
                        if err != nil || len(data) > MaxDefinitionBytes {
                                continue
                        }

                        var td TaskDefinition
                        if err := json.Unmarshal(data, &td); err != nil {
                                continue
                        }

                        normalize(&td.Definition)
                        s.cache[td.ID] = &td
                }
        }
}

// CreateTaskTool validates and persists a NEW task-scoped tool. The
// result is ALWAYS disabled and unapproved — execution requires the
// explicit approval gate.
func (s *TaskStore) CreateTaskTool(taskID, runID string, d *Definition) (*TaskDefinition, error) {
        if strings.TrimSpace(taskID) == "" {
                return nil, fmt.Errorf("task tools need a task id")
        }
        if d == nil {
                return nil, fmt.Errorf("nil definition")
        }
        if vr := d.Validate(); !vr.OK {
                return nil, fmt.Errorf("invalid task tool (%s): %s", vr.Owner, vr.Error)
        }

        s.mu.Lock()
        defer s.mu.Unlock()

        for _, existing := range s.cache {
                if existing.Definition.Name == d.Name && existing.TaskID == taskID {
                        return nil, fmt.Errorf("task tool %q already exists for task %s", d.Name, taskID)
                }
        }

        now := time.Now().UTC()
        td := &TaskDefinition{
                Definition: *d,
                TaskID:     taskID,
                RunID:      runID,
                Approved:   false,
                CreatedAt:  now,
        }
        td.Enabled = false // safe default: the approval gate owns execution
        td.ID = newID()
        td.UpdatedAt = now
        td.CreatedAt = now

        data, err := json.MarshalIndent(td, "", "  ")
        if err != nil {
                return nil, fmt.Errorf("marshal task tool: %w", err)
        }
        if len(data) > MaxDefinitionBytes {
                return nil, fmt.Errorf("task tool definition too large")
        }

        dir := s.taskDir(taskID)
        if err := os.MkdirAll(dir, 0o755); err != nil {
                return nil, fmt.Errorf("task tool dir: %w", err)
        }

        path := filepath.Join(dir, safeID.ReplaceAllString(td.ID, "")+".json")
        tmp := path + ".tmp"

        if err := os.WriteFile(tmp, data, 0o644); err != nil {
                return nil, fmt.Errorf("write task tool: %w", err)
        }
        if err := os.Rename(tmp, path); err != nil {
                _ = os.Remove(tmp)
                return nil, fmt.Errorf("commit task tool: %w", err)
        }

        s.cache[td.ID] = td
        return td, nil
}

// Approve flips the approval gate AND enables the tool (the permission
// policy's explicit allowance).
func (s *TaskStore) Approve(id string) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        td, ok := s.cache[id]
        if !ok {
                return fmt.Errorf("task tool %q not found", id)
        }

        td.Approved = true
        td.Enabled = true

        return s.saveLocked(td)
}

func (s *TaskStore) saveLocked(td *TaskDefinition) error {
        data, err := json.MarshalIndent(td, "", "  ")
        if err != nil {
                return err
        }

        path := filepath.Join(s.taskDir(td.TaskID), safeID.ReplaceAllString(td.ID, "")+".json")
        tmp := path + ".tmp"

        if err := os.WriteFile(tmp, data, 0o644); err != nil {
                return err
        }
        return os.Rename(tmp, path)
}

// ListForTask returns the task's tools (deterministic order).
func (s *TaskStore) ListForTask(taskID string) []*TaskDefinition {
        s.mu.Lock()
        defer s.mu.Unlock()

        var out []*TaskDefinition
        for _, td := range s.cache {
                if td.TaskID == taskID {
                        cp := *td
                        out = append(out, &cp)
                }
        }

        sort.Slice(out, func(i, j int) bool {
                if out[i].Approved != out[j].Approved {
                        return out[i].Approved
                }
                return out[i].Definition.Name < out[j].Definition.Name
        })
        return out
}

// Get returns one task tool by id.
func (s *TaskStore) Get(id string) (*TaskDefinition, bool) {
        s.mu.Lock()
        defer s.mu.Unlock()

        td, ok := s.cache[id]
        if !ok {
                return nil, false
        }
        cp := *td
        return &cp, true
}

// Delete removes one task tool.
func (s *TaskStore) Delete(id string) bool {
        s.mu.Lock()
        defer s.mu.Unlock()

        td, ok := s.cache[id]
        if !ok {
                return false
        }

        delete(s.cache, id)
        _ = os.Remove(filepath.Join(s.taskDir(td.TaskID), safeID.ReplaceAllString(td.ID, "")+".json"))
        return true
}

// CleanupTask removes a task's whole tool scope (task teardown).
func (s *TaskStore) CleanupTask(taskID string) error {
        s.mu.Lock()
        defer s.mu.Unlock()

        for id, td := range s.cache {
                if td.TaskID == taskID {
                        delete(s.cache, id)
                }
        }

        return os.RemoveAll(s.taskDir(taskID))
}

// RegisterInto installs the task's APPROVED tools into the orchestrator's
// ONE registry (the same agent.Tool adapter used by global custom tools)
// and returns an unregister closure for the task-run teardown. Disabled
// or unapproved definitions are never offered to the model.
func (s *TaskStore) RegisterInto(reg RegistrarFuncs, taskID string) (func(), []string) {
        var registered []string

        unregister := func() {
                for _, name := range registered {
                        reg.Unregister(name)
                }
        }

        for _, td := range s.ListForTask(taskID) {
                if !td.Approved || !td.Enabled {
                        continue
                }

                def := td.Definition
                def.Enabled = true

                reg.Register(NewTool(&def))
                registered = append(registered, def.Name)
        }

        return unregister, registered
}
