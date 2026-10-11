// Package installer ensures the local environment is set up: llama.cpp binary,
// models directory, etc. Auto-installs what it can; reports what's missing.
package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// State is the persisted snapshot of installed components.
type State struct {
	AppVersion string               `json:"appVersion"`
	LastRunAt  time.Time            `json:"lastRunAt"`
	Components map[string]Component `json:"components"`
}

type Component struct {
	Version    string            `json:"version,omitempty"`
	Status     string            `json:"status"` // "installed" | "missing" | "running" | "error"
	ObservedAt time.Time         `json:"observedAt"`
	Meta       map[string]string `json:"meta,omitempty"`
}

// Manager handles installation and update checking.
type Manager struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Manager {
	return &Manager{cfg: cfg}
}

// EnsureRun runs the full install+check pipeline. Returns the current state
// and a list of changes (diff) since last run.
//
// v1.7.4 (P0 #3, H1): EnsureRun runs before every startup maintenance gate
// and again on every UI state poll. detect() builds a FRESH snapshot — the
// filesystem knows paths, not engine builds — so the wholesale saveState
// rewrite used to wipe components.llamaServer.meta.engineTag that
// updater.RecordEngineTag had committed at update time. Every subsequent
// "update required" decision then fell back to the bundled default tag and
// re-downloaded the engine on every boot. preserveEngineIdentity below
// merges the recorded identity back in; detection results (paths,
// statuses) are still written fresh every run. (force has always been
// advisory — both former branches ran the identical detect+save pipeline.)
func (m *Manager) EnsureRun(force bool) (*State, []Change, error) {
	prev, _ := m.LoadState()
	curr := m.detect()
	m.preserveEngineIdentity(prev, curr)
	curr.AppVersion = config.AppVersion
	curr.LastRunAt = time.Now().UTC()
	_ = m.saveState(curr)
	return curr, diff(prev, curr), nil
}

// preserveEngineIdentity carries the llama.cpp engine identity
// (components.llamaServer.meta.engineTag / version) from the previous state
// into a fresh detection snapshot.
//
// v1.7.4 rules:
//   - the binary path is unchanged → carry the previous tag (and version)
//     forward verbatim — a detection pass must never downgrade an identity
//     it cannot itself observe;
//   - the binary genuinely changed to a DIFFERENT file → re-detection is
//     legitimate, but the tag is still seeded from the install manifest
//     beside the new binary (updater rewrites it at every commit);
//   - no previous state, no recorded tag, or no llamaServer component →
//     nothing to preserve (the updater decision points fall back through
//     updater.EffectiveInstalledEngineTag).
func (m *Manager) preserveEngineIdentity(prev, curr *State) {
	if prev == nil || curr == nil {
		return
	}

	p, ok := prev.Components["llamaServer"]
	if !ok {
		return
	}

	tag := p.Meta["engineTag"]
	if tag == "" {
		tag = p.Version
	}
	if tag == "" {
		return
	}

	c, ok := curr.Components["llamaServer"]
	if !ok {
		return
	}

	prevPath := p.Meta["path"]
	currPath := c.Meta["path"]

	// v1.7.4: only two CONCRETE, differing paths prove a genuine binary
	// change. A path recorded on one side only is no evidence — pre-v1.7.4
	// commits (RecordEngineTag) wrote no path at all, and treating the
	// recorded "" as "the binary changed" would drop the very identity
	// this merge exists to preserve.
	if prevPath != "" && currPath != "" && prevPath != currPath {
		// The binary genuinely moved/changed — the recorded tag belongs to
		// the OLD file. Seed from the committed manifest beside the NEW one.
		if t := updater.ManifestEngineTag(m.cfg); t != "" {
			if c.Meta == nil {
				c.Meta = map[string]string{}
			}
			c.Meta["engineTag"] = t
		}
		curr.Components["llamaServer"] = c
		return
	}

	if c.Meta == nil {
		c.Meta = map[string]string{}
	}
	if c.Meta["engineTag"] == "" {
		c.Meta["engineTag"] = tag
	}
	if c.Version == "" {
		c.Version = p.Version
	}
	curr.Components["llamaServer"] = c
}

// Change is one entry in the per-launch diff.
type Change struct {
	Component string `json:"component"`
	Kind      string `json:"kind"` // "added" | "removed" | "changed"
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

func diff(prev, curr *State) []Change {
	if prev == nil {
		var out []Change
		for name, c := range curr.Components {
			out = append(out, Change{Component: name, Kind: "added", To: c.Status + " " + c.Version})
		}
		return out
	}
	var out []Change
	for name, c := range curr.Components {
		old, ok := prev.Components[name]
		if !ok {
			out = append(out, Change{Component: name, Kind: "added", To: c.Status + " " + c.Version})
			continue
		}
		if old.Status != c.Status || old.Version != c.Version {
			out = append(out, Change{
				Component: name,
				Kind:      "changed",
				From:      old.Status + " " + old.Version,
				To:        c.Status + " " + c.Version,
			})
		}
	}
	for name, c := range prev.Components {
		if _, ok := curr.Components[name]; !ok {
			out = append(out, Change{Component: name, Kind: "removed", From: c.Status + " " + c.Version})
		}
	}
	return out
}

// detect probes the system for every component.
func (m *Manager) detect() *State {
	s := &State{Components: map[string]Component{}}
	now := time.Now().UTC()

	// Go runtime
	s.Components["goRuntime"] = Component{
		Version:    runtime.Version(),
		Status:     "installed",
		ObservedAt: now,
	}

	// Models dir
	if finfo, err := os.Stat(m.cfg.ModelsDir); err == nil && finfo.IsDir() {
		entries, _ := os.ReadDir(m.cfg.ModelsDir)
		count := 0
		for _, e := range entries {
			if !e.IsDir() && len(e.Name()) > 5 && e.Name()[len(e.Name())-5:] == ".gguf" {
				count++
			}
		}
		s.Components["modelsDir"] = Component{
			Status:     "installed",
			Version:    fmt.Sprintf("%d model(s)", count),
			ObservedAt: now,
			Meta:       map[string]string{"path": m.cfg.ModelsDir},
		}
	} else {
		s.Components["modelsDir"] = Component{
			Status:     "missing",
			ObservedAt: now,
			Meta:       map[string]string{"path": m.cfg.ModelsDir},
		}
	}

	// llama.cpp server binary
	//
	// v1.7.4 (P0 #3, H1): probe the binary the next start would actually
	// launch (updater.EngineBinaryPath — the one path authority), not only
	// an explicitly configured LlamaBinPath. With the default empty
	// LlamaBinPath the detection used to skip the llamaServer component
	// entirely, so the state rewrite dropped the recorded engine identity
	// for managed-dir installs too.
	engineBin := m.cfg.LlamaBinPath
	if engineBin == "" {
		engineBin = updater.EngineBinaryPath(m.cfg)
	}
	if engineBin != "" {
		if _, err := os.Stat(engineBin); err == nil {
			s.Components["llamaServer"] = Component{
				Status:     "installed",
				ObservedAt: now,
				Meta:       map[string]string{"path": engineBin},
			}
		} else {
			s.Components["llamaServer"] = Component{
				Status:     "missing",
				ObservedAt: now,
				Meta:       map[string]string{"hint": "auto-downloaded on first run"},
			}
		}
	}

	// Sessions dir
	if _, err := os.Stat(m.cfg.SessionsDir); err == nil {
		s.Components["sessionsDir"] = Component{
			Status:     "installed",
			ObservedAt: now,
			Meta:       map[string]string{"path": m.cfg.SessionsDir},
		}
	}

	// Docker (optional)
	if dockerPath, err := lookPath("docker"); err == nil {
		s.Components["docker"] = Component{
			Status:     "installed",
			ObservedAt: now,
			Meta:       map[string]string{"path": dockerPath},
		}
	} else {
		s.Components["docker"] = Component{
			Status:     "missing",
			ObservedAt: now,
			Meta:       map[string]string{"hint": "optional — only needed for sandboxed code execution"},
		}
	}

	return s
}

// LoadState reads the persisted state file.
func (m *Manager) LoadState() (*State, error) {
	path := m.cfg.StatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// saveState writes the state to disk.
func (m *Manager) saveState(s *State) error {
	if err := os.MkdirAll(m.cfg.DataDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.cfg.StatePath(), data, 0o644)
}

// FormatState pretty-prints the state for CLI/UI display.
func FormatState(s *State) string {
	if s == nil {
		return "(no state — first run)"
	}
	out := fmt.Sprintf("SHEYTAN-Local-Agent v%s state:\n", s.AppVersion)
	out += fmt.Sprintf("  last run: %s\n\n", s.LastRunAt.Format(time.RFC3339))
	out += "Components:\n"
	for name, c := range s.Components {
		mark := "✓"
		if c.Status != "installed" {
			mark = "✗"
		}
		ver := c.Version
		if ver == "" {
			ver = c.Status
		}
		out += fmt.Sprintf("  %s %-15s %s\n", mark, name, ver)
	}
	return out
}
