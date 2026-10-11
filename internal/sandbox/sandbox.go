// Package sandbox runs agent-generated code with resource caps.
//
// Windows: a real Job Object governor — process memory cap, CPU-rate
// control, active-process limit and KILL_ON_JOB_CLOSE (a runaway script
// can never outlive the app). Assignment happens by PID right after the
// child starts (OpenProcess + AssignProcessToJobObject).
//
// Linux: a best-effort prlimit(1) governor (CPU seconds per run; the
// address-space cap is deliberately NOT applied — modern runtimes such as
// node reserve multi-GB virtual regions and would die instantly). When
// prlimit is unavailable the command runs with the context timeout only.
//
// macOS: timeout-only (honest degradation).
//
// Every child goes through internal/proc so no console window ever flashes
// on Windows, and TMP/TEMP are pointed at the sandbox workdir so temp
// litter lands in one cleanable place.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// defaultSandboxMemMB / defaultSandboxCPUPct are the fallback caps when a
// caller passes zeros.
const (
	defaultSandboxMemMB  = 512
	defaultSandboxCPUPct = 100
	maxSandboxTimeoutSec = 600
)

// resourceGovernor is the platform cap layer: prepare may rewrite the
// command before Start (Unix prlimit wrapper — timeoutSec feeds the CPU
// budget); attach hooks the just-started process (Windows job
// assignment); close releases everything.
type resourceGovernor interface {
	prepare(timeoutSec int, name string, args []string) (string, []string)
	attach(p *os.Process) error
	close() error
}

// CodeExecSandbox is the sandboxed drop-in replacement for the plain
// codeExec tool. It owns a persistent workdir under the app folder
// (scripts run there; files the code writes stay there and are visible to
// the other tools).
type CodeExecSandbox struct {
	memMB  int
	cpuPct int

	dir     string // persistent workdir
	tempDir bool   // we created dir via MkdirTemp (Close removes it)
	mu      sync.Mutex
	seq     int      // script-name counter
	scripts []string // script files we wrote (removed after each run)
	closed  bool

	gov resourceGovernor // platform caps (nil = timeout-only)
}

// New creates a sandbox with the given caps. memMB bounds the process
// memory (Windows: hard job limit); cpuPct (1-100) bounds CPU usage.
// dir is the persistent workdir — "" creates a private temp dir. As per
// the v1.0.9+ contract, construction ALWAYS succeeds when a workdir can
// be created: platform governors degrade to timeout-only rather than
// failing the whole app (the runtime falls back to plain codeExec only
// when New itself errors).
func New(memMB, cpuPct int, dir string) (*CodeExecSandbox, error) {
	return NewCodeExecSandbox(memMB, cpuPct, dir)
}

// NewCodeExecSandbox is New under the historical name used by the runtime
// stack and the stress suite.
func NewCodeExecSandbox(
	memMB,
	cpuPct int,
	dir string,
) (*CodeExecSandbox, error) {
	tempDir := false

	if dir == "" {
		d, err := os.MkdirTemp("", "sheytan-sandbox-*")
		if err != nil {
			return nil, fmt.Errorf("sandbox workdir: %w", err)
		}

		dir = d
		tempDir = true
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("sandbox workdir: %w", err)
	}

	if memMB <= 0 {
		memMB = defaultSandboxMemMB
	}

	if cpuPct <= 0 || cpuPct > 100 {
		cpuPct = defaultSandboxCPUPct
	}

	sb := &CodeExecSandbox{
		memMB:   memMB,
		cpuPct:  cpuPct,
		dir:     dir,
		tempDir: tempDir,
	}

	// Best-effort governor: nil means timeout-only execution.
	sb.gov = newGovernor(memMB, cpuPct)

	return sb, nil
}

// WorkDir returns the sandbox workdir.
func (s *CodeExecSandbox) WorkDir() string {
	return s.dir
}

// MemMB / CPUPct expose the configured caps.
func (s *CodeExecSandbox) MemMB() int {
	return s.memMB
}

func (s *CodeExecSandbox) CPUPct() int {
	return s.cpuPct
}

// --- agent.Tool surface ---------------------------------------------------

// Name implements the agent tool interface.
func (s *CodeExecSandbox) Name() string {
	return "codeExec"
}

// Description implements the agent tool interface.
func (s *CodeExecSandbox) Description() string {
	return "Run code inside the resource-capped sandbox (memory " +
		fmt.Sprintf("%d MB", s.memMB) +
		", CPU " +
		fmt.Sprintf("%d%%", s.cpuPct) +
		") and return stdout+stderr. " +
		"lang: python (default) or node. Code runs in the sandbox workdir — " +
		"files it writes stay there for the files/dataAnalysis tools."
}

// Parameters implements the agent tool interface.
func (s *CodeExecSandbox) Parameters() any {
	return struct {
		Lang    string `json:"lang"`
		Code    string `json:"code"`
		Timeout int    `json:"timeout,omitempty"`
	}{}
}

// Run implements the agent tool interface: {lang, code, timeout}.
func (s *CodeExecSandbox) Run(
	ctx context.Context,
	args json.RawMessage,
) (string, error) {
	var p struct {
		Lang    string `json:"lang"`
		Code    string `json:"code"`
		Timeout int    `json:"timeout"`
	}

	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}

	if p.Code == "" {
		return "", fmt.Errorf("code is required")
	}

	lang := strings.ToLower(strings.TrimSpace(p.Lang))
	if lang == "" {
		lang = "python"
	}

	var bin, ext string

	switch lang {
	case "python", "py":
		py, err := pythonBin()
		if err != nil {
			return "", err
		}

		bin = py
		ext = ".py"

	case "node", "javascript", "js":
		bin = "node"
		ext = ".js"

	default:
		return "", fmt.Errorf("unsupported language: %s", lang)
	}

	if p.Timeout <= 0 {
		p.Timeout = 60
	}

	if p.Timeout > maxSandboxTimeoutSec {
		p.Timeout = maxSandboxTimeoutSec
	}

	// The tool's timeout is part of the execution contract. Derive a child
	// context from the caller context so both the caller cancellation and the
	// explicit sandbox timeout terminate the command.
	runCtx, cancel := context.WithTimeout(
		ctx,
		time.Duration(p.Timeout)*time.Second,
	)
	defer cancel()

	script, cleanup, err := s.stageScript(ext, p.Code)
	if err != nil {
		return "", err
	}
	defer cleanup()

	out, err := s.Execute(runCtx, bin, script)
	if err != nil && len(out) == 0 {
		return "", err
	}

	// Non-zero exit still returns the captured output — the agent needs
	// the traceback, not just the error.
	if err != nil {
		out = append(out, ("\n" + err.Error())...)
	}

	return string(out), nil
}

// stageScript writes the code into the workdir under a unique name and
// returns (path, cleanup).
func (s *CodeExecSandbox) stageScript(
	ext,
	code string,
) (string, func(), error) {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()
		return "", nil, fmt.Errorf("sandbox closed")
	}

	s.seq++
	seq := s.seq

	s.mu.Unlock()

	script := filepath.Join(
		s.dir,
		fmt.Sprintf("run-%04d%s", seq, ext),
	)

	if err := os.WriteFile(
		script,
		[]byte(code),
		0o644,
	); err != nil {
		return "", nil, err
	}

	cleanup := func() {
		_ = os.Remove(script)
	}

	return script, cleanup, nil
}

// Execute runs an arbitrary executable inside the sandbox caps and
// returns its combined output. It is the low-level surface the smoke
// tests use directly (independent of the JSON tool contract).
func (s *CodeExecSandbox) Execute(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	s.mu.Lock()
	closed := s.closed
	dir := s.dir
	gov := s.gov
	s.mu.Unlock()

	if closed {
		return nil, fmt.Errorf("sandbox closed")
	}

	// Derive the CPU budget from the caller's deadline when present.
	timeoutSec := 60

	if dl, ok := ctx.Deadline(); ok {
		if sec := int(time.Until(dl).Seconds()) + 1; sec > 0 {
			timeoutSec = sec
		}
	}

	if gov != nil {
		name, args = gov.prepare(
			timeoutSec,
			name,
			args,
		)
	}

	cmd := proc.CommandContext(
		ctx,
		name,
		args...,
	)

	cmd.Dir = dir

	// v1.1.4: sandboxed code runs with a SANITIZED environment. The
	// previous full os.Environ() handed every host secret (API keys,
	// tokens) to model-generated python/node. TEMP/TMP stay sandbox-local
	// so Python's tempfile & co. litter inside the workdir.
	cmd.Env = proc.SanitizedEnvironment(map[string]string{
		"TMP":    dir,
		"TEMP":   dir,
		"TMPDIR": dir,
	})

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	if gov != nil {
		_ = gov.attach(cmd.Process)
	}

	waitErr := cmd.Wait()

	// When the context deadline/cancellation fired, expose that fact
	// instead of hiding it behind a platform-specific process error.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return buf.Bytes(), ctxErr
	}

	return buf.Bytes(), waitErr
}

// Close releases the sandbox: terminates any live job children (Windows),
// drops the script files, and removes the workdir when we created it as a
// temp dir. Files the agent's code produced under an explicit workdir are
// NEVER deleted — they may be artifacts the user wants.
func (s *CodeExecSandbox) Close() error {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()
		return nil
	}

	s.closed = true

	scripts := s.scripts
	s.scripts = nil

	gov := s.gov
	dir := s.dir
	temp := s.tempDir

	s.gov = nil

	s.mu.Unlock()

	for _, f := range scripts {
		_ = os.Remove(f)
	}

	var govErr error

	if gov != nil {
		govErr = gov.close()
	}

	if temp {
		if err := os.RemoveAll(dir); err != nil && govErr == nil {
			govErr = err
		}
	}

	return govErr
}

// pythonBin resolves the Python interpreter across platforms.
func pythonBin() (string, error) {
	return resolvePython()
}
