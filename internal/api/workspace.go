package api

// workspace.go — v1.2.4 Workspace surface.
//
// The Workspace is the app's "current work environment" in ONE view:
// current project folder → what is in it → the active session/task → the
// model & runtime state → what changed. This file provides the read side
// (one compact summary endpoint), the action side (reveal in the OS file
// manager / terminal, switch project root) and the persistence plumbing
// for recent workspaces.
//
// Safety rules for switching (mirrors the release contract):
//   - refused while a run is active — switching must never corrupt a live
//     generation or tear tool cwd out from under it;
//   - the new root must exist and be a directory;
//   - the agent's tools rebind through a single atomic SetBaseDir call —
//     there are no watchers/timers/sockets pinned to the old root;
//   - project intelligence re-observes the new root with a bounded walk;
//   - the change is persisted and announced (the response states exactly
//     what changed).

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/projectintel"
)

// maxRecentFiles bounds the "recent files" scan of the workspace root.
const maxRecentFiles = 8

// workspaceFile is one recent file entry in the workspace summary.
type workspaceFile struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// workspaceSummary is the GET /api/workspace payload: the compact
// Project → files → task → model → runtime → state view.
type workspaceSummary struct {
	Root      string `json:"root"`
	IsDefault bool   `json:"isDefault"` // true when on the legacy scratch workspace
	Exists    bool   `json:"exists"`

	RecentFiles []workspaceFile          `json:"recentFiles,omitempty"`
	Project     *projectintel.Facts      `json:"project,omitempty"`
	ProjectCard string                   `json:"projectCard,omitempty"`
	Recents     []string                 `json:"recentWorkspaces,omitempty"`
	Session     *workspaceSessionSummary `json:"activeSession,omitempty"`
	Runtime     *workspaceRuntimeSummary `json:"runtime,omitempty"`
	Agent       *workspaceAgentState     `json:"agent,omitempty"`
}

type workspaceSessionSummary struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title,omitempty"`
	Model     string `json:"model,omitempty"`
	Messages  int    `json:"messages"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type workspaceRuntimeSummary struct {
	Backend     string `json:"backend,omitempty"`
	EngineState string `json:"engineState,omitempty"`
	Model       string `json:"model,omitempty"`
}

type workspaceAgentState struct {
	Running bool   `json:"running"`
	State   string `json:"state,omitempty"` // idle | running
}

// handleWorkspace serves GET /api/workspace.
func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	writeJSON(w, s.buildWorkspaceSummary())
}

// buildWorkspaceSummary assembles the one-call workspace view from
// infrastructure that already exists (config source, session store,
// engine snapshot, project intelligence) — no new state, no new watchers.
func (s *Server) buildWorkspaceSummary() *workspaceSummary {
	cfg := s.src.Load()
	root := cfg.EffectiveWorkspaceRoot()

	out := &workspaceSummary{
		Root:      root,
		IsDefault: strings.TrimSpace(cfg.WorkspaceRoot) == "",
		Exists:    false,
		Recents:   cfg.RecentWorkspaces,
	}

	if fi, err := os.Stat(root); err == nil && fi.IsDir() {
		out.Exists = true
		out.RecentFiles = scanRecentFiles(root, maxRecentFiles)
	}

	if s.stack != nil && s.stack.Intel != nil {
		if facts, err := s.stack.Intel.Load(root); err == nil {
			out.Project = &facts
		}
		out.ProjectCard = s.stack.Intel.Card(root)
	}

	// Active session = the most recently touched session (the one the UI
	// would restore). Stubs are enough — no full history is loaded.
	if sessions, err := s.store.List(); err == nil && len(sessions) > 0 {
		top := sessions[0]
		out.Session = &workspaceSessionSummary{
			ID:        top.ID,
			Title:     top.Title,
			Model:     top.Model,
			Messages:  top.MessageCount(),
			UpdatedAt: top.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}

	snap := s.engineSnapshot()
	out.Runtime = &workspaceRuntimeSummary{
		Backend:     snap.Backend,
		EngineState: snap.State,
		Model:       snap.Model,
	}

	s.runsMu.Lock()
	running := len(s.runs) > 0
	s.runsMu.Unlock()
	out.Agent = &workspaceAgentState{Running: running}
	if running {
		out.Agent.State = "running"
	} else {
		out.Agent.State = "idle"
	}

	return out
}

// scanRecentFiles lists the newest regular files directly inside root
// (one level — the summary is a glance, not a tree walk; project
// intelligence already walks deeper under a bound). Missing dir → nil.
func scanRecentFiles(root string, limit int) []workspaceFile {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	files := make([]workspaceFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		files = append(files, workspaceFile{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.After(files[j].ModTime) })
	if len(files) > limit {
		files = files[:limit]
	}
	return files
}

// handleWorkspaceReveal opens the OS file manager or a terminal AT the
// current workspace root. Best-effort and honest: an OS without a
// discoverable terminal returns a clear error, never a fake success.
func (s *Server) handleWorkspaceReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	var body struct {
		Target string `json:"target"` // "files" (default) | "terminal"
	}
	if r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil && err.Error() != "EOF" {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}

	cfg := s.src.Load()
	root := cfg.EffectiveWorkspaceRoot()
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		if err := os.MkdirAll(root, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	var openErr error
	switch body.Target {
	case "terminal":
		openErr = openTerminalAt(root)
	default:
		switch runtime.GOOS {
		case "windows":
			openErr = exec.Command("explorer", root).Start()
		case "darwin":
			openErr = exec.Command("open", root).Start()
		default:
			openErr = exec.Command("xdg-open", root).Start()
		}
	}
	if openErr != nil {
		writeErr(w, http.StatusInternalServerError, errString(
			"could not open "+root+": "+openErr.Error()))
		return
	}

	writeJSON(w, map[string]any{"ok": true, "path": root, "target": body.Target})
}

// openTerminalAt launches the user's terminal emulator in dir.
func openTerminalAt(dir string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", "cmd", "/K", "cd", "/d", dir).Start()
	case "darwin":
		return exec.Command("open", "-a", "Terminal", dir).Start()
	default:
		// Common Linux terminal emulators, first match wins.
		for _, term := range []string{
			"x-terminal-emulator", "gnome-terminal", "konsole",
			"xfce4-terminal", "alacritty", "kitty", "xterm",
		} {
			if path, err := exec.LookPath(term); err == nil {
				switch term {
				case "gnome-terminal":
					return exec.Command(path, "--working-directory="+dir).Start()
				case "konsole":
					return exec.Command(path, "--workdir", dir).Start()
				default:
					return exec.Command(path, "-e", "sh", "-c", "cd '"+dir+"'; exec sh").Start()
				}
			}
		}
		return exec.ErrNotFound
	}
}

// handleWorkspaceSwitch changes the CURRENT PROJECT FOLDER. The whole flow
// is refusal-first: an active run, a missing directory or a bad path never
// leaves the app half-switched.
func (s *Server) handleWorkspaceSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	path := strings.TrimSpace(body.Path)
	if path == "" {
		writeErr(w, http.StatusBadRequest, errString("path is required"))
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	abs = filepath.Clean(abs)
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		writeErr(w, http.StatusBadRequest, errString(
			"not an accessible directory: "+abs))
		return
	}

	// Refuse while a generation is live: the tools rebind atomically, but
	// a run mid-flight would observe a moved floor — that is exactly the
	// corruption class this endpoint exists to prevent.
	s.runsMu.Lock()
	running := len(s.runs) > 0
	s.runsMu.Unlock()
	if running {
		writeErr(w, http.StatusConflict, errString(
			"an agent run is active — wait for it to finish or abort it before switching workspace"))
		return
	}

	// Single atomic rebind + observe + persist — the EXACT sequence the
	// v1.3.0 clone completion reuses (switchWorkspaceRoot in clone.go).
	previous := s.src.Load().EffectiveWorkspaceRoot()
	summary, err := s.switchWorkspaceRoot(abs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, map[string]any{
		"ok":       true,
		"previous": previous,
		"summary":  summary,
		"changed":  []string{"agent working directory", "project intelligence", "config.workspaceRoot", "recentWorkspaces"},
	})
}
