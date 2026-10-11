package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/termshell"
)

// LinuxSim is the v1.0.6 built-in Linux-like environment: a busybox-style
// shell simulator JAILED to the app folder. It is the "whenever necessary"
// scratch environment — deterministic, dependency-free and safe by
// construction (no real process spawns, no path outside the app folder is
// reachable), so the model can ls/grep/pipe its way through the workspace
// even on machines where a real shell is locked down.
//
// One engine instance persists across calls: the working directory survives,
// so the agent can cd into a folder and keep working there.
type LinuxSim struct {
	engine *termshell.Engine
}

// NewLinuxSim builds the simulator jailed to root.
func NewLinuxSim(root string) *LinuxSim {
	return &LinuxSim{engine: termshell.New(root)}
}

// Engine exposes the shared engine (the Terminal view renders the same
// instance so the user can watch and replay what the agent did).
func (t *LinuxSim) Engine() *termshell.Engine { return t.engine }

func (t *LinuxSim) Name() string { return "linux" }

func (t *LinuxSim) Description() string {
	return "Run a command in the built-in Linux-like shell simulator (safe, no real processes). Supports: ls, cd, cat, echo, mkdir, touch, rm, cp, mv, head, tail, wc, grep, find, du, df, tree, sort, uniq, rev, ps, uname, env, export, history, stat, neofetch + pipes (e.g. cat file | grep -i error | wc -l). Jailed to the app folder: ~ = the app root, nothing outside it is reachable. The working directory persists between calls. Prefer this for quick file inspection; use the shell tool when a REAL command (git, python) is required."
}

func (t *LinuxSim) Parameters() any {
	return struct {
		Command string `json:"command"`
	}{}
}

func (t *LinuxSim) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	if strings.TrimSpace(p.Command) == "" {
		return "", fmt.Errorf("command is required")
	}
	out := t.engine.Exec(p.Command)
	if out == termshell.ClearMarker {
		return "(screen cleared)", nil
	}
	if out == "" {
		return "(no output)", nil
	}
	return out, nil
}
