package lab

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// Package lab contains the autonomous coding-laboratory runtime.
//
// runner.go provides the process-execution layer used by the Coding Lab.
// It deliberately does not decide whether a command is permitted; command
// policy belongs to the policy layer. The runner's responsibility is to
// execute a permitted command inside a validated workspace and return a
// bounded, structured result.

var (
	ErrCommandEmpty    = errors.New("lab: command is empty")
	ErrOutputLimit     = errors.New("lab: command output exceeded configured limit")
	ErrCommandTimedOut = errors.New("lab: command timed out")
)

// Command describes one process execution requested by the Coding Lab.
type Command struct {
	// Command is interpreted by the platform shell.
	Command string `json:"command"`

	// WorkingDir is relative to the workspace root. Empty means the workspace
	// root itself.
	WorkingDir string `json:"workingDir,omitempty"`

	// Environment contains additional environment variables in KEY=VALUE form.
	// Only non-sensitive variables are passed to the child process.
	Environment []string `json:"environment,omitempty"`

	// Timeout overrides the runner default for this command. A zero value uses
	// the runner's configured default.
	Timeout time.Duration `json:"timeout,omitempty"`

	// MaxOutputBytes limits combined stdout/stderr captured from the process.
	// A zero value uses the runner default.
	MaxOutputBytes int64 `json:"maxOutputBytes,omitempty"`
}

// CommandResult contains the complete structured result of one execution.
type CommandResult struct {
	Command     string        `json:"command"`
	WorkingDir  string        `json:"workingDir"`
	Stdout      string        `json:"stdout"`
	Stderr      string        `json:"stderr"`
	Output      string        `json:"output"`
	ExitCode    int           `json:"exitCode"`
	Duration    time.Duration `json:"duration"`
	StartedAt   time.Time     `json:"startedAt"`
	FinishedAt  time.Time     `json:"finishedAt"`
	TimedOut    bool          `json:"timedOut"`
	Canceled    bool          `json:"canceled"`
	OutputLimit bool          `json:"outputLimit"`
	Success     bool          `json:"success"`
}

// Runner executes commands inside Coding Lab workspaces.
type Runner struct {
	DefaultTimeout        time.Duration
	DefaultMaxOutputBytes int64
}

// NewRunner creates a process runner with bounded defaults.
//
// A timeout of <= 0 uses 5 minutes.
// An output limit of <= 0 uses 2 MiB of combined stdout/stderr.
func NewRunner(timeout time.Duration, maxOutputBytes int64) *Runner {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	if maxOutputBytes <= 0 {
		maxOutputBytes = 2 * 1024 * 1024
	}

	return &Runner{
		DefaultTimeout:        timeout,
		DefaultMaxOutputBytes: maxOutputBytes,
	}
}

// Run executes one command inside the given workspace.
func (r *Runner) Run(
	ctx context.Context,
	workspace *Workspace,
	command Command,
) (CommandResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if workspace == nil || workspace.Path == "" {
		return CommandResult{}, ErrInvalidWorkspace
	}

	command.Command = strings.TrimSpace(command.Command)

	if command.Command == "" {
		return CommandResult{}, ErrCommandEmpty
	}

	workingDir, err := workspace.PathFor(command.WorkingDir)
	if err != nil {
		return CommandResult{}, fmt.Errorf(
			"lab: resolve command working directory: %w",
			err,
		)
	}

	if info, statErr := os.Stat(workingDir); statErr != nil {
		return CommandResult{}, fmt.Errorf(
			"lab: stat command working directory: %w",
			statErr,
		)
	} else if !info.IsDir() {
		return CommandResult{}, fmt.Errorf(
			"lab: command working directory is not a directory: %s",
			workingDir,
		)
	}

	timeout := command.Timeout

	if timeout <= 0 {
		timeout = r.defaultTimeout()
	}

	maxOutput := command.MaxOutputBytes

	if maxOutput <= 0 {
		maxOutput = r.defaultMaxOutputBytes()
	}

	if maxOutput <= 0 {
		maxOutput = 2 * 1024 * 1024
	}

	runCtx := ctx
	cancel := func() {}

	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}

	defer cancel()

	cmd := buildShellCommand(runCtx, command.Command)
	cmd.Dir = workingDir

	// Coding Lab processes must not inherit the full host environment.
	// This removes API keys, tokens, passwords, credentials, cookies, and
	// other host secrets while retaining normal compiler/runtime variables.
	// v1.1.4: HOME and USERPROFILE are pinned to the workspace. A lexical
	// policy check cannot see through shell expansion, so $HOME/~/ tokens
	// previously escaped the workspace jail while still resolving to the
	// real user profile (reads AND writes outside the sandbox).
	sanitized := proc.SanitizedEnvironment(homeOverrides(workingDir))
	cmd.Env = mergeEnvironment(sanitized, command.Environment)

	// stdin remains disconnected: autonomous runs must never block waiting
	// for terminal input.
	cmd.Stdin = nil

	// stdout and stderr share ONE output budget. This makes MaxOutputBytes a
	// true combined output limit rather than an independent limit per stream.
	budget := newOutputBudget(maxOutput)

	stdoutBuffer := &boundedBuffer{
		budget: budget,
	}

	stderrBuffer := &boundedBuffer{
		budget: budget,
	}

	cmd.Stdout = stdoutBuffer
	cmd.Stderr = stderrBuffer

	started := time.Now().UTC()

	startErr := cmd.Start()

	if startErr != nil {
		finished := time.Now().UTC()

		result := buildCommandResult(
			command,
			workingDir,
			stdoutBuffer.String(),
			stderrBuffer.String(),
			budget.Exceeded(),
			started,
			finished,
			startErr,
			false,
			false,
		)

		return result, fmt.Errorf(
			"lab: start command: %w",
			startErr,
		)
	}

	waitErr := cmd.Wait()
	finished := time.Now().UTC()

	timedOut := errors.Is(
		runCtx.Err(),
		context.DeadlineExceeded,
	)

	canceled := !timedOut && errors.Is(
		runCtx.Err(),
		context.Canceled,
	)

	outputLimited := budget.Exceeded()

	result := buildCommandResult(
		command,
		workingDir,
		stdoutBuffer.String(),
		stderrBuffer.String(),
		outputLimited,
		started,
		finished,
		waitErr,
		timedOut,
		canceled,
	)

	if timedOut {
		if outputLimited {
			return result, errors.Join(
				ErrCommandTimedOut,
				ErrOutputLimit,
			)
		}

		return result, ErrCommandTimedOut
	}

	if canceled {
		if outputLimited {
			return result, errors.Join(
				context.Canceled,
				ErrOutputLimit,
			)
		}

		return result, context.Canceled
	}

	if outputLimited {
		if waitErr != nil {
			return result, errors.Join(
				waitErr,
				ErrOutputLimit,
			)
		}

		return result, ErrOutputLimit
	}

	if waitErr != nil {
		return result, fmt.Errorf(
			"lab: command failed: %w",
			waitErr,
		)
	}

	return result, nil
}

func buildShellCommand(
	ctx context.Context,
	command string,
) *exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		// cmd.exe is deliberately used instead of PowerShell so command syntax
		// stays predictable for common Windows build/test tooling.
		return proc.CommandContext(
			ctx,
			"cmd.exe",
			"/d",
			"/s",
			"/c",
			command,
		)

	default:
		// Do NOT use "-l": a login shell can read host startup configuration
		// and unexpectedly reintroduce host state into the Coding Lab.
		return proc.CommandContext(
			ctx,
			"/bin/sh",
			"-c",
			command,
		)
	}
}

// sanitizedEnvironment keeps normal build/runtime variables while removing
// variables that commonly contain credentials or other host secrets.
func mergeEnvironment(
	base,
	extra []string,
) []string {
	index := make(map[string]int, len(base))

	for i, item := range base {
		key := envKey(item)

		if key != "" {
			index[key] = i
		}
	}

	result := append([]string(nil), base...)

	for _, item := range extra {
		key := envKey(item)

		if key == "" || proc.IsSensitiveEnvKey(key) {
			continue
		}

		if i, exists := index[key]; exists {
			result[i] = item
			continue
		}

		index[key] = len(result)
		result = append(result, item)
	}

	return result
}

func envKey(value string) string {
	index := strings.IndexByte(value, '=')

	if index <= 0 {
		return ""
	}

	return strings.ToUpper(value[:index])
}

// homeOverrides pins the shell-visible home directory into the workspace so
// $HOME/~/ expansion cannot escape the jail (v1.1.4).
func homeOverrides(workingDir string) map[string]string {
	return map[string]string{
		"HOME":          workingDir,
		"USERPROFILE":   workingDir,
		"TMPDIR":        workingDir,
		"SHELLYTAN_JOB": "1",
	}
}

func buildCommandResult(
	command Command,
	workingDir string,
	stdout string,
	stderr string,
	outputLimited bool,
	started time.Time,
	finished time.Time,
	runErr error,
	timedOut bool,
	canceled bool,
) CommandResult {
	exitCode := 0

	success :=
		runErr == nil &&
			!timedOut &&
			!canceled &&
			!outputLimited

	if runErr != nil {
		exitCode = exitCodeFromError(runErr)
	}

	output := combineOutput(stdout, stderr)

	if outputLimited {
		output += "\n\n[LAB OUTPUT TRUNCATED]\n"
	}

	return CommandResult{
		Command:     command.Command,
		WorkingDir:  workingDir,
		Stdout:      stdout,
		Stderr:      stderr,
		Output:      output,
		ExitCode:    exitCode,
		Duration:    finished.Sub(started),
		StartedAt:   started,
		FinishedAt:  finished,
		TimedOut:    timedOut,
		Canceled:    canceled,
		OutputLimit: outputLimited,
		Success:     success,
	}
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError

	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return -1
}

func combineOutput(stdout, stderr string) string {
	switch {
	case stdout == "":
		return stderr

	case stderr == "":
		return stdout

	default:
		var b strings.Builder

		b.Grow(len(stdout) + len(stderr) + 16)
		b.WriteString("[stdout]\n")
		b.WriteString(stdout)
		b.WriteString("\n[stderr]\n")
		b.WriteString(stderr)

		return b.String()
	}
}

// outputBudget provides one thread-safe byte budget shared by stdout and
// stderr. cmd.Wait may receive output from both pipes concurrently.
type outputBudget struct {
	mu        sync.Mutex
	remaining int64
	exceeded  bool
}

func newOutputBudget(limit int64) *outputBudget {
	if limit <= 0 {
		limit = 2 * 1024 * 1024
	}

	return &outputBudget{
		remaining: limit,
	}
}

// reserve grants up to len(p) bytes from the shared budget.
func (b *outputBudget) reserve(length int) int {
	if b == nil || length <= 0 {
		return 0
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.remaining <= 0 {
		b.exceeded = true
		return 0
	}

	allowed := int64(length)

	if allowed > b.remaining {
		allowed = b.remaining
		b.exceeded = true
	}

	b.remaining -= allowed

	return int(allowed)
}

func (b *outputBudget) Exceeded() bool {
	if b == nil {
		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	return b.exceeded
}

// boundedBuffer captures bytes from one output stream while using the shared
// process output budget.
type boundedBuffer struct {
	budget    *outputBudget
	Written   int64
	Truncated bool
	buffer    bytes.Buffer
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}

	if len(p) == 0 {
		return 0, nil
	}

	allowed := b.budget.reserve(len(p))

	if allowed <= 0 {
		b.Truncated = true
		return len(p), nil
	}

	n, err := b.buffer.Write(p[:allowed])

	b.Written += int64(n)

	if allowed < len(p) {
		b.Truncated = true
	}

	if err != nil {
		return n, err
	}

	// Returning len(p) tells os/exec the writer consumed the complete input
	// even though only the bounded prefix was retained.
	return len(p), nil
}

// String returns only the captured bytes. The truncation marker is added
// to the combined CommandResult.Output by buildCommandResult so that the
// marker never inflates Stdout/Stderr beyond the shared output budget.
func (b *boundedBuffer) String() string {
	if b == nil {
		return ""
	}

	return b.buffer.String()
}

func (r *Runner) defaultTimeout() time.Duration {
	if r == nil || r.DefaultTimeout <= 0 {
		return 5 * time.Minute
	}

	return r.DefaultTimeout
}

func (r *Runner) defaultMaxOutputBytes() int64 {
	if r == nil || r.DefaultMaxOutputBytes <= 0 {
		return 2 * 1024 * 1024
	}

	return r.DefaultMaxOutputBytes
}
