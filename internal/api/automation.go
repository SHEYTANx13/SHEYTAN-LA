// automation.go — v1.7.0: the Automation / Tasks API surface.
//
// A coherent scheduler API over the ONE scheduler (no second engine):
// task CRUD, run/pause/resume/cancel, chronological run history, the
// task's task-scoped tools (the SAME customtools executor) and the
// task's artifacts (the SAME artifact registry). All input is validated
// and bounded; errors are deterministic; event-fired runs execute
// asynchronously through the same bounded paths.

package api

import (
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "strings"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/artifacts"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/customtools"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/scheduler"
)

// automationPayloadBounds — every input is bounded.
const (
        autoMaxName     = 200
        autoMaxPrompt   = 64 * 1024
        autoMaxLists    = 32
        autoMaxListStr  = 200
        autoMaxRunsList = 500
)

type automationTaskView struct {
        scheduler.Task
        Running bool `json:"running"`
}

func (s *Server) sched() *scheduler.Scheduler {
        if s.stack == nil {
                return nil
        }
        return s.stack.Sched
}

// handleAutomation routes /api/automation/*.
func (s *Server) handleAutomation(w http.ResponseWriter, r *http.Request) {
        sched := s.sched()
        if sched == nil {
                writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("scheduler unavailable"))
                return
        }

        switch {
        case r.URL.Path == "/api/automation/tasks":
                switch r.Method {
                case http.MethodGet:
                        out := make([]automationTaskView, 0)
                        for _, t := range sched.Tasks() {
                                out = append(out, automationTaskView{Task: t, Running: sched.IsRunning(t.ID)})
                        }
                        writeJSON(w, out)
                case http.MethodPost:
                        s.handleAutomationCreate(w, r, sched)
                default:
                        writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                }
                return

        case strings.HasPrefix(r.URL.Path, "/api/automation/tasks/"):
                s.handleAutomationTask(w, r, sched)
                return

        default:
                writeErr(w, http.StatusNotFound, fmt.Errorf("not found"))
        }
}

// handleAutomationTask routes /api/automation/tasks/{id}[/{action}].
func (s *Server) handleAutomationTask(w http.ResponseWriter, r *http.Request, sched *scheduler.Scheduler) {
        rest := strings.TrimPrefix(r.URL.Path, "/api/automation/tasks/")
        parts := strings.Split(rest, "/")
        id := parts[0]
        var action string
        if len(parts) > 1 {
                action = parts[1]
        }

        if id == "" {
                writeErr(w, http.StatusNotFound, fmt.Errorf("not found"))
                return
        }

        if action == "" {
                switch r.Method {
                case http.MethodGet:
                        tasks := sched.Tasks()
                        for _, t := range tasks {
                                if t.ID == id {
                                        writeJSON(w, automationTaskView{Task: t, Running: sched.IsRunning(id)})
                                        return
                                }
                        }
                        writeErr(w, http.StatusNotFound, fmt.Errorf("task %q not found", id))
                case http.MethodPut:
                        s.handleAutomationUpdate(w, r, sched, id)
                case http.MethodDelete:
                        if err := sched.RemoveTask(id); err != nil {
                                writeErr(w, http.StatusNotFound, err)
                                return
                        }
                        // Task teardown: scoped tools and artifacts go with it.
                        if s.taskTools != nil {
                                _ = s.taskTools.CleanupTask(id)
                        }
                        if s.artRegistry != nil {
                                _ = s.artRegistry.CleanupTask(id)
                        }
                        writeJSON(w, map[string]bool{"deleted": true})
                default:
                        writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                }
                return
        }

        switch action {
        case "run":
                if r.Method != http.MethodPost {
                        writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                        return
                }
                ch, err := sched.RunNow(r.Context(), id)
                if err != nil {
                        writeErr(w, automationStatusFor(err), err)
                        return
                }
                // Asynchronous by default (?wait=1 settles the report inline).
                if r.URL.Query().Get("wait") == "1" {
                        report := <-ch
                        writeJSON(w, report)
                        return
                }
                writeJSON(w, map[string]any{"started": true, "taskId": id})

        case "pause":
                if r.Method != http.MethodPost {
                        writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                        return
                }
                if err := sched.Pause(id); err != nil {
                        writeErr(w, http.StatusNotFound, err)
                        return
                }
                writeJSON(w, map[string]bool{"paused": true})

        case "resume":
                if r.Method != http.MethodPost {
                        writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                        return
                }
                if err := sched.Resume(id); err != nil {
                        writeErr(w, http.StatusNotFound, err)
                        return
                }
                writeJSON(w, map[string]bool{"resumed": true})

        case "cancel":
                if r.Method != http.MethodPost {
                        writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                        return
                }
                writeJSON(w, map[string]bool{"canceled": sched.CancelRun(id)})

        case "runs":
                if r.Method != http.MethodGet {
                        writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                        return
                }
                n := autoMaxRunsList
                if q := queryInt(r, "limit", 100); q > 0 && q < n {
                        n = q
                }
                writeJSON(w, sched.Runs(id, n))

        case "tools":
                s.handleAutomationTaskTools(w, r, id)

        default:
                writeErr(w, http.StatusNotFound, fmt.Errorf("not found"))
        }
}

func automationStatusFor(err error) int {
        switch {
        case err == scheduler.ErrTaskPaused, err == scheduler.ErrAlreadyRunning:
                return http.StatusConflict
        default:
                if strings.Contains(err.Error(), "not found") {
                        return http.StatusNotFound
                }
                return http.StatusBadRequest
        }
}

// automationTaskPayload is the create/update body (bounded).
type automationTaskPayload struct {
        ID           string   `json:"id"`
        Name         string   `json:"name"`
        Trigger      string   `json:"trigger"`
        Prompt       string   `json:"prompt"`
        IntervalSecs int64    `json:"intervalSeconds,omitempty"`
        MaxRuntimeS  int64    `json:"maxRuntimeSeconds,omitempty"`
        ScheduleKind string   `json:"scheduleKind,omitempty"`
        At           string   `json:"at,omitempty"`         // RFC3339 (once)
        TimeOfDay    string   `json:"timeOfDay,omitempty"`  // HH:MM (daily/weekly)
        Weekday      *int     `json:"weekday,omitempty"`    // 0=Sun..6=Sat
        LinkedSkills []string `json:"linkedSkills,omitempty"`
        TaskTools    []string `json:"taskTools,omitempty"`
        TaskTypes    []string `json:"taskTypes,omitempty"`
}

func (p *automationTaskPayload) validate() error {
        if strings.TrimSpace(p.Prompt) == "" {
                return fmt.Errorf("prompt is required")
        }
        if len(p.Prompt) > autoMaxPrompt {
                return fmt.Errorf("prompt too large (max %d bytes)", autoMaxPrompt)
        }
        if len(p.Name) > autoMaxName {
                return fmt.Errorf("name too long (max %d)", autoMaxName)
        }
        for _, list := range [][]string{p.LinkedSkills, p.TaskTools, p.TaskTypes} {
                if len(list) > autoMaxLists {
                        return fmt.Errorf("at most %d entries per list", autoMaxLists)
                }
                for _, item := range list {
                        if len(item) > autoMaxListStr {
                                return fmt.Errorf("list entry too long (max %d)", autoMaxListStr)
                        }
                }
        }
        switch p.Trigger {
        case "timer", "scheduled_maintenance":
                if p.IntervalSecs <= 0 && p.ScheduleKind == "" {
                        return fmt.Errorf("timer tasks need intervalSeconds (>= 300)")
                }
        case "manual", "startup", "file_change", "git_change",
                "test_failure", "ci_failure", "build_failure":
        default:
                return fmt.Errorf("unknown trigger %q", p.Trigger)
        }
        switch p.ScheduleKind {
        case "", "interval", "once", "daily", "weekly":
        default:
                return fmt.Errorf("unknown scheduleKind %q", p.ScheduleKind)
        }
        return nil
}

func (p *automationTaskPayload) toTask() scheduler.Task {
        id := p.ID
        if id == "" {
                id = fmt.Sprintf("task-%d", time.Now().UnixNano())
        }

        sk := scheduler.Schedule{Kind: scheduler.ScheduleKind(p.ScheduleKind)}
        if p.ScheduleKind == "once" {
                if at, err := time.Parse(time.RFC3339, p.At); err == nil {
                        sk.At = at
                }
        }
        if p.ScheduleKind == "daily" || p.ScheduleKind == "weekly" {
                sk.TimeOfDay = p.TimeOfDay
        }
        if p.ScheduleKind == "weekly" && p.Weekday != nil {
                sk.Weekday = *p.Weekday
        }

        return scheduler.Task{
                ID:           id,
                Name:         strings.TrimSpace(p.Name),
                Trigger:      scheduler.EventKind(p.Trigger),
                Interval:     time.Duration(p.IntervalSecs) * time.Second,
                Prompt:       p.Prompt,
                MaxRuntime:   time.Duration(p.MaxRuntimeS) * time.Second,
                Schedule:     sk,
                LinkedSkills: p.LinkedSkills,
                TaskTools:    p.TaskTools,
                TaskTypes:    p.TaskTypes,
        }
}

func (s *Server) handleAutomationCreate(w http.ResponseWriter, r *http.Request, sched *scheduler.Scheduler) {
        body, err := io.ReadAll(io.LimitReader(r.Body, autoMaxPrompt+4096))
        if err != nil {
                writeErr(w, http.StatusBadRequest, err)
                return
        }

        var p automationTaskPayload
        if err := json.Unmarshal(body, &p); err != nil {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid payload: %w", err))
                return
        }

        if err := p.validate(); err != nil {
                writeErr(w, http.StatusBadRequest, err)
                return
        }

        task := p.toTask()
        if err := sched.AddTask(task); err != nil {
                writeErr(w, http.StatusBadRequest, err)
                return
        }

        writeJSON(w, automationTaskView{Task: task, Running: false})
}

func (s *Server) handleAutomationUpdate(w http.ResponseWriter, r *http.Request, sched *scheduler.Scheduler, id string) {
        body, err := io.ReadAll(io.LimitReader(r.Body, autoMaxPrompt+4096))
        if err != nil {
                writeErr(w, http.StatusBadRequest, err)
                return
        }

        var p automationTaskPayload
        if err := json.Unmarshal(body, &p); err != nil {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid payload: %w", err))
                return
        }

        if err := p.validate(); err != nil {
                writeErr(w, http.StatusBadRequest, err)
                return
        }

        err = sched.UpdateTask(id, func(t *scheduler.Task) error {
                next := p.toTask()
                next.ID = id
                next.Created = t.Created
                next.Enabled = t.Enabled
                next.Paused = t.Paused
                next.LastRun = t.LastRun
                *t = next
                return nil
        })
        if err != nil {
                writeErr(w, http.StatusBadRequest, err)
                return
        }

        tasks := sched.Tasks()
        for _, t := range tasks {
                if t.ID == id {
                        writeJSON(w, automationTaskView{Task: t, Running: sched.IsRunning(id)})
                        return
                }
        }
        writeErr(w, http.StatusNotFound, fmt.Errorf("task %q not found", id))
}

// --- task-scoped tools ------------------------------------------------------

type automationTaskToolPayload struct {
        Name         string             `json:"name"`
        ShortDesc    string             `json:"shortDescription"`
        Description  string             `json:"description"`
        Params       []customtools.Param `json:"inputParameters,omitempty"`
        ExecType     string             `json:"executionType"`
        HTTP         *customtools.HTTPExec    `json:"http,omitempty"`
        Command      *customtools.CommandExec `json:"command,omitempty"`
        Permission   string             `json:"permission"`
        TimeoutSec   int                `json:"timeoutSeconds"`
        OutputLimit  int                `json:"outputLimitBytes"`
}

func (s *Server) handleAutomationTaskTools(w http.ResponseWriter, r *http.Request, taskID string) {
        if s.taskTools == nil {
                writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("task tool store unavailable"))
                return
        }

        switch r.Method {
        case http.MethodGet:
                writeJSON(w, s.taskTools.ListForTask(taskID))

        case http.MethodPost:
                body, err := io.ReadAll(io.LimitReader(r.Body, 128*1024))
                if err != nil {
                        writeErr(w, http.StatusBadRequest, err)
                        return
                }

                var p automationTaskToolPayload
                if err := json.Unmarshal(body, &p); err != nil {
                        writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid payload: %w", err))
                        return
                }

                def := &customtools.Definition{
                        Name:        p.Name,
                        ShortDesc:   p.ShortDesc,
                        Description: p.Description,
                        Params:      p.Params,
                        ExecType:    p.ExecType,
                        HTTP:        p.HTTP,
                        Command:     p.Command,
                        Permission:  p.Permission,
                        TimeoutSec:  p.TimeoutSec,
                        OutputLimit: p.OutputLimit,
                }

                td, err := s.taskTools.CreateTaskTool(taskID, "", def)
                if err != nil {
                        writeErr(w, http.StatusBadRequest, err)
                        return
                }
                writeJSON(w, td)

        default:
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
        }
}

// handleAutomationTaskTool routes /api/automation/task-tools/{id}[/approve].
func (s *Server) handleAutomationTaskTool(w http.ResponseWriter, r *http.Request) {
        if s.taskTools == nil {
                writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("task tool store unavailable"))
                return
        }

        rest := strings.TrimPrefix(r.URL.Path, "/api/automation/task-tools/")
        parts := strings.Split(rest, "/")
        id := parts[0]
        action := ""
        if len(parts) > 1 {
                action = parts[1]
        }

        switch {
        case action == "approve" && r.Method == http.MethodPost:
                if err := s.taskTools.Approve(id); err != nil {
                        writeErr(w, http.StatusNotFound, err)
                        return
                }
                td, _ := s.taskTools.Get(id)
                writeJSON(w, td)

        case action == "" && r.Method == http.MethodDelete:
                if !s.taskTools.Delete(id) {
                        writeErr(w, http.StatusNotFound, fmt.Errorf("task tool %q not found", id))
                        return
                }
                writeJSON(w, map[string]bool{"deleted": true})

        case action == "" && r.Method == http.MethodGet:
                td, ok := s.taskTools.Get(id)
                if !ok {
                        writeErr(w, http.StatusNotFound, fmt.Errorf("task tool %q not found", id))
                        return
                }
                writeJSON(w, td)

        default:
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
        }
}

// --- artifacts surface -------------------------------------------------------

// handleAutomationArtifacts lists / reads task artifacts with provenance.
func (s *Server) handleAutomationArtifacts(w http.ResponseWriter, r *http.Request) {
        if s.artRegistry == nil {
                writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("artifact registry unavailable"))
                return
        }

        switch {
        case r.URL.Path == "/api/automation/artifacts":
                switch r.Method {
                case http.MethodGet:
                        taskID := r.URL.Query().Get("task")
                        if taskID == "" {
                                writeErr(w, http.StatusBadRequest, fmt.Errorf("task query parameter required"))
                                return
                        }
                        writeJSON(w, s.artRegistry.List(taskID))

                case http.MethodPost:
                        // The artifact creation surface (the same registry the
                        // artifact_create agent tool writes through): bounded,
                        // path-safe, atomic, versioned.
                        body, err := io.ReadAll(io.LimitReader(r.Body, artifacts.MaxArtifactBytes+4096))
                        if err != nil {
                                writeErr(w, http.StatusBadRequest, err)
                                return
                        }

                        var p struct {
                                TaskID   string `json:"taskId"`
                                Filename string `json:"filename"`
                                Content  string `json:"content"`
                                Title    string `json:"title"`
                        }
                        if err := json.Unmarshal(body, &p); err != nil {
                                writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid payload: %w", err))
                                return
                        }

                        meta, err := s.artRegistry.Create(artifacts.CreateRequest{
                                TaskID:   p.TaskID,
                                Source:   "api",
                                Title:    p.Title,
                                Filename: p.Filename,
                                Content:  []byte(p.Content),
                        })
                        if err != nil {
                                writeErr(w, http.StatusBadRequest, err)
                                return
                        }
                        writeJSON(w, meta)

                default:
                        writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                }
                return

        case strings.HasPrefix(r.URL.Path, "/api/automation/artifacts/"):
                rest := strings.TrimPrefix(r.URL.Path, "/api/automation/artifacts/")
                parts := strings.Split(rest, "/")
                id := parts[0]
                action := ""
                if len(parts) > 1 {
                        action = parts[1]
                }

                meta, ok := s.artRegistry.Get(id)
                if !ok {
                        writeErr(w, http.StatusNotFound, fmt.Errorf("artifact %q not found", id))
                        return
                }

                switch action {
                case "":
                        if r.Method == http.MethodDelete {
                                if !s.artRegistry.Delete(id) {
                                        writeErr(w, http.StatusNotFound, fmt.Errorf("artifact %q not found", id))
                                        return
                                }
                                writeJSON(w, map[string]bool{"deleted": true})
                                return
                        }
                        writeJSON(w, meta)

                case "content":
                        // Isolated preview: the content is served with a DENY-ALL
                        // sandbox CSP and never inside the application origin —
                        // artifact JavaScript never executes in the app.
                        data, err := s.artRegistry.Read(id)
                        if err != nil {
                                writeErr(w, http.StatusNotFound, err)
                                return
                        }
                        w.Header().Set("Content-Type", contentTypeFor(meta))
                        w.Header().Set("Content-Security-Policy",
                                "default-src 'none'; script-src 'none'; object-src 'none'; frame-ancestors 'none'")
                        w.Header().Set("X-Content-Type-Options", "nosniff")
                        _, _ = w.Write(data)

                case "versions":
                        writeJSON(w, s.artRegistry.Versions(meta.TaskID, meta.RelPath))

                default:
                        writeErr(w, http.StatusNotFound, fmt.Errorf("not found"))
                }
                return
        }
}

func contentTypeFor(m artifacts.Meta) string {
        switch m.Kind {
        case "doc":
                if strings.HasSuffix(m.RelPath, ".md") || strings.HasSuffix(m.RelPath, ".markdown") {
                        return "text/markdown; charset=utf-8"
                }
                return "text/plain; charset=utf-8"
        case "code":
                if strings.HasSuffix(m.RelPath, ".html") {
                        // HTML previews are sandboxed client-side; served as
                        // text/plain-wrapped source by default (deny active content).
                        return "text/plain; charset=utf-8"
                }
                return "text/plain; charset=utf-8"
        case "chart":
                return "image/svg+xml"
        case "image":
                if strings.HasSuffix(m.RelPath, ".png") {
                        return "image/png"
                }
                if strings.HasSuffix(m.RelPath, ".jpg") || strings.HasSuffix(m.RelPath, ".jpeg") {
                        return "image/jpeg"
                }
                return "application/octet-stream"
        default:
                return "application/octet-stream"
        }
}

func queryInt(r *http.Request, key string, def int) int {
        v := r.URL.Query().Get(key)
        if v == "" {
                return def
        }
        n := 0
        if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
                return def
        }
        return n
}
