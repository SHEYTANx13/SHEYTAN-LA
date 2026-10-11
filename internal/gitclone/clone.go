// clone.go — v1.3.0 first-class GitHub clone execution.
//
// DESIGN CONTRACT (release contract §9)
//
//   - REUSE of the validated execution path: every process runs through
//     internal/proc (hidden console on Windows, context cancellation that
//     kills the WHOLE process tree) — the same seam the agent's Git tool,
//     the Coding Lab runner and the updater use;
//   - NO SHELL: the git executable is invoked with EXPLICIT argument
//     vectors — user input is never interpolated into a command string
//     and `cmd /c` is never used;
//   - git runs NON-INTERACTIVE (GIT_TERMINAL_PROMPT=0): an authentication
//     prompt fails fast instead of hanging the job;
//   - bounded: a hard timeout, bounded captured output, bounded progress
//     fan-out;
//   - verified: a successful clone is validated (git rev-parse inside
//     the work tree) before the job reports success;
//   - classified: failures map to stable error kinds with actionable
//     messages — raw git noise is attached as diagnostic detail, never
//     served as the only explanation.
package gitclone

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// --- classified errors ----------------------------------------------------

// ErrorKind is a stable machine-readable failure classification.
type ErrorKind string

const (
	ErrGitUnavailable ErrorKind = "git-unavailable"
	ErrInvalidURL     ErrorKind = "invalid-url"
	ErrInvalidDest    ErrorKind = "invalid-destination"
	ErrDestExists     ErrorKind = "destination-exists"
	ErrAuth           ErrorKind = "authentication"
	ErrNotFound       ErrorKind = "repository-not-found"
	ErrNetwork        ErrorKind = "network"
	ErrPermission     ErrorKind = "permission"
	ErrCanceled       ErrorKind = "canceled"
	ErrTimeout        ErrorKind = "timeout"
	ErrVerification   ErrorKind = "verification"
	ErrUnknown        ErrorKind = "unknown"
)

// Error is a classified clone failure.
type Error struct {
	Kind    ErrorKind
	Message string
}

func (e *Error) Error() string { return string(e.Kind) + ": " + e.Message }

// ClassifyError maps an error to its ErrorKind (unknown errors classify
// as ErrUnknown).
func ClassifyError(err error) ErrorKind {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Kind
	}
	return ErrUnknown
}

// --- job status -----------------------------------------------------------

// State is the job lifecycle state.
type State string

const (
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

// Phase is the fine-grained progress phase while running.
type Phase string

const (
	PhaseValidating Phase = "validating"
	PhaseCloning    Phase = "cloning"
	PhaseVerifying  Phase = "verifying"
)

// Status is an immutable progress snapshot (JSON-ready for the API).
type Status struct {
	State        State    `json:"state"`
	Phase        Phase    `json:"phase,omitempty"`
	URL          string   `json:"url,omitempty"`
	RepoName     string   `json:"repoName,omitempty"`
	Owner        string   `json:"owner,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	SSH          bool     `json:"ssh,omitempty"`
	Destination  string   `json:"destination,omitempty"`
	Percent      int      `json:"percent,omitempty"`
	Message      string   `json:"message,omitempty"`
	StartedAt    string   `json:"startedAt,omitempty"`
	FinishedAt   string   `json:"finishedAt,omitempty"`
	ExitCode     int      `json:"exitCode,omitempty"`
	Head         string   `json:"head,omitempty"`
	OutputTail   []string `json:"outputTail,omitempty"`
	ErrorKind    string   `json:"errorKind,omitempty"`
	ErrorMessage string   `json:"errorMessage,omitempty"`
}

// --- options ---------------------------------------------------------------

// Options configures one job. All fields have production defaults; tests
// inject a fake git binary through GitBinary.
type Options struct {
	// GitBinary is the git executable to invoke (default: "git" resolved
	// through PATH; override for tests via the SHEYTAN_GIT_BINARY seam).
	GitBinary string
	// Timeout bounds the whole clone (default 30 minutes).
	Timeout time.Duration
	// MaxTailLines bounds the captured diagnostic output (default 64).
	MaxTailLines int
}

func (o Options) withDefaults() Options {
	if o.GitBinary == "" {
		o.GitBinary = DefaultGitBinary()
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Minute
	}
	if o.MaxTailLines <= 0 {
		o.MaxTailLines = 64
	}
	return o
}

// DefaultGitBinary resolves the git executable: the SHEYTAN_GIT_BINARY
// override first (tests, exotic layouts), then plain "git".
func DefaultGitBinary() string {
	if v := strings.TrimSpace(os.Getenv("SHEYTAN_GIT_BINARY")); v != "" {
		return v
	}
	return "git"
}

// --- job --------------------------------------------------------------------

// Job is one running (or finished) clone.
type Job struct {
	target Target
	dest   string
	branch string
	opts   Options

	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	status Status
	tail   []string
}

// Start launches the clone for target into dest and returns immediately.
// The caller polls Status(); Cancel() aborts (process tree included).
func Start(target Target, dest string, branch string, opts Options) *Job {
	if !validBranchName(branch) {
		branch = ""
	}

	ctx, cancel := context.WithCancel(context.Background())

	j := &Job{
		target: target,
		dest:   dest,
		branch: branch,
		opts:   opts.withDefaults(),
		cancel: cancel,
		done:   make(chan struct{}),
		status: Status{
			State:       StateRunning,
			Phase:       PhaseValidating,
			URL:         target.Raw,
			RepoName:    target.Repo,
			Owner:       target.Owner,
			Branch:      branch,
			SSH:         target.SSH,
			Destination: dest,
			StartedAt:   time.Now().UTC().Format(time.RFC3339),
			Message:     "Validating repository URL…",
		},
	}

	go j.run(ctx)

	return j
}

// Status returns the current immutable snapshot.
func (j *Job) Status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.status
	out.OutputTail = append([]string(nil), j.tail...)
	return out
}

// Cancel aborts the job (no-op when already finished).
func (j *Job) Cancel() {
	j.cancel()
}

// Done is closed exactly once when the job reaches a terminal state.
func (j *Job) Done() <-chan struct{} {
	return j.done
}

// Destination returns the configured destination (used by the API layer
// for the post-clone workspace switch).
func (j *Job) Destination() string { return j.dest }

// Target returns the validated target.
func (j *Job) Target() Target { return j.target }

func (j *Job) run(ctx context.Context) {
	defer close(j.done)

	// Re-check the destination right before spawning (Start's caller
	// validated too, but time passes).
	if err := ValidateDestination(j.dest); err != nil {
		j.finish(Status{
			State:        StateFailed,
			ErrorKind:    string(ClassifyError(err)),
			ErrorMessage: err.Error(),
		})
		return
	}

	j.setPhase(PhaseCloning, "Cloning "+j.target.Name+"…", 0)

	exitCode, runErr := j.runClone(ctx)
	if runErr != nil {
		j.fail(ctx, exitCode, runErr)
		return
	}

	j.setPhase(PhaseVerifying, "Verifying repository…", 100)

	head, verifyErr := j.verify(ctx)
	if verifyErr != nil {
		j.finish(Status{
			State:        StateFailed,
			Phase:        PhaseVerifying,
			ErrorKind:    string(ErrVerification),
			ErrorMessage: "clone finished but the repository did not verify: " + verifyErr.Error(),
			ExitCode:     exitCode,
		})
		return
	}

	j.finish(Status{
		State:    StateSucceeded,
		Phase:    PhaseVerifying,
		Head:     head,
		ExitCode: exitCode,
		Message:  fmt.Sprintf("Cloned %s (%s) into %s", j.target.Name, shortHead(head), j.dest),
	})
}

// runClone spawns `git clone --progress [--branch b] -- url dest` as a
// structured, tree-killable, bounded, non-interactive process.
func (j *Job) runClone(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, j.opts.Timeout)
	defer cancel()

	args := []string{"clone", "--progress"}
	if j.branch != "" {
		args = append(args, "--branch", j.branch)
	}
	args = append(args, "--", j.target.NormalizedURL, j.dest)

	cmd := proc.CommandContext(ctx, j.opts.GitBinary, args...)
	cmd.Env = gitEnv()
	cmd.Dir = filepath.Dir(j.dest)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, &Error{Kind: ErrUnknown, Message: "capture stdout: " + err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, &Error{Kind: ErrUnknown, Message: "capture stderr: " + err.Error()}
	}

	if err := cmd.Start(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return -1, &Error{Kind: ErrCanceled, Message: "canceled before git started"}
		}
		if isNotFound(err) {
			return -1, &Error{
				Kind:    ErrGitUnavailable,
				Message: "Git was not found on this machine — install it from https://git-scm.com/download and try again",
			}
		}
		return -1, &Error{Kind: ErrUnknown, Message: "start git: " + err.Error()}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		j.scan(ctx, stdout, false)
	}()
	go func() {
		defer wg.Done()
		j.scan(ctx, stderr, true)
	}()

	// Drain both pipes to EOF BEFORE reaping the process: os/exec's
	// Wait closes the parent pipe descriptors once the process exits,
	// and a scanner that has not drained the buffered output yet can
	// lose tail lines to the closed descriptor — the stderr evidence
	// the error classification depends on (the StdoutPipe docs are
	// explicit that calling Wait before all reads complete is
	// incorrect). The scanners see EOF when the child exits and its
	// write ends close; reaping an already-exited process returns
	// immediately, so this order cannot deadlock. (Cancellation is
	// tree-kill: the whole process group's pipes close, the scanners
	// unblock, then the reap happens.)
	wg.Wait()
	waitErr := cmd.Wait()

	exitCode := exitCodeOf(waitErr)
	if waitErr != nil {
		kind := classifyFailure(ctx, exitCode, j.snapshotTail())
		return exitCode, &Error{
			Kind:    kind,
			Message: failureMessage(ctx, exitCode, j.snapshotTail(), kind),
		}
	}
	return exitCode, nil
}

// verify proves the destination is a real git work tree and reads HEAD.
func (j *Job) verify(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := proc.CommandContext(ctx, j.opts.GitBinary, "-C", j.dest, "rev-parse", "HEAD")
	cmd.Env = gitEnv()

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	head := strings.TrimSpace(out.String())
	if head == "" {
		return "", fmt.Errorf("git rev-parse HEAD produced no commit")
	}
	if len(head) != 40 || strings.Trim(head, "0123456789abcdefABCDEF") != "" {
		return "", fmt.Errorf("git rev-parse HEAD returned an unexpected value %q", head)
	}
	return head, nil
}

// scan reads one output stream, feeding the progress parser and the
// bounded tail ring. Progress lines are also kept in the tail (they are
// the natural "what happened" evidence).
func (j *Job) scan(ctx context.Context, r io.Reader, isStderr bool) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		j.appendTail(line)

		if isStderr {
			if ok, pct := parseProgressLine(line); ok {
				j.setPercent(pct, line)
			}
		}
	}
}

func (j *Job) appendTail(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.tail = append(j.tail, line)
	if len(j.tail) > j.opts.MaxTailLines {
		j.tail = j.tail[len(j.tail)-j.opts.MaxTailLines:]
	}
}

func (j *Job) snapshotTail() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.tail...)
}

func (j *Job) setPhase(phase Phase, message string, percent int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status.Phase = phase
	j.status.Message = message
	if percent > j.status.Percent {
		j.status.Percent = percent
	}
}

func (j *Job) setPercent(percent int, evidence string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if percent > j.status.Percent {
		j.status.Percent = percent
	}
	j.status.Message = evidence
}

func (j *Job) finish(patch Status) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status.State != StateRunning {
		return
	}
	patch.StartedAt = j.status.StartedAt
	patch.URL = j.status.URL
	patch.RepoName = j.status.RepoName
	patch.Owner = j.status.Owner
	patch.Branch = j.status.Branch
	patch.SSH = j.status.SSH
	patch.Destination = j.status.Destination
	if patch.Percent == 0 {
		patch.Percent = j.status.Percent
	}
	patch.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if patch.OutputTail == nil {
		patch.OutputTail = append([]string(nil), j.tail...)
	}
	j.status = patch
}

func (j *Job) fail(ctx context.Context, exitCode int, runErr error) {
	kind := ClassifyError(runErr)
	message := runErr.Error()

	// classified errors carry their own actionable message already
	if kind == ErrUnknown {
		kind = classifyFailure(ctx, exitCode, j.snapshotTail())
		message = failureMessage(ctx, exitCode, j.snapshotTail(), kind)
	}

	state := StateFailed
	if errors.Is(ctx.Err(), context.Canceled) || kind == ErrCanceled {
		state = StateCanceled
	}

	j.finish(Status{
		State:        state,
		Phase:        PhaseCloning,
		ExitCode:     exitCode,
		ErrorKind:    string(kind),
		ErrorMessage: message,
	})
}

// --- progress parsing -----------------------------------------------------

// progressPattern matches git's --progress stderr lines:
//
//	Receiving objects:  45% (12/26), 2.31 MiB | 1.20 MiB/s
//	Resolving deltas:  30% (3/10)
//	Updating files:  50% (5/10)
var progressPattern = regexp.MustCompile(`(Enumerating|Counting|Compressing|Receiving|Resolving|Updating)[a-z ]*:? *(?:objects|deltas|files)?:?\s+(\d+)%`)

// parseProgressLine maps one git stderr line to (progressRelevant, percent).
func parseProgressLine(line string) (bool, int) {
	m := progressPattern.FindStringSubmatch(line)
	if m == nil {
		return false, 0
	}
	stage := m[1]
	pct := 0
	for _, ch := range m[2] {
		if ch < '0' || ch > '9' {
			return false, 0
		}
		pct = pct*10 + int(ch-'0')
		if pct > 100 {
			return false, 0
		}
	}

	switch stage {
	case "Receiving":
		return true, 5 + pct*85/100 // 5..90: the download is the long pole
	case "Resolving", "Updating":
		if pct >= 100 {
			return true, 95
		}
		return false, 0
	default:
		return true, 2 // preparing
	}
}

// classifyFailure maps a failed clone to a kind using the exit code and
// the captured tail.
func classifyFailure(ctx context.Context, exitCode int, tail []string) ErrorKind {
	combined := strings.ToLower(strings.Join(tail, "\n"))

	if errors.Is(ctx.Err(), context.Canceled) {
		return ErrCanceled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}

	switch {
	case strings.Contains(combined, "not found") &&
		(strings.Contains(combined, "repository") || strings.Contains(combined, "repo") || strings.Contains(combined, "remote branch")):
		return ErrNotFound
	case strings.Contains(combined, "authentication failed") ||
		strings.Contains(combined, "permission denied") ||
		strings.Contains(combined, "could not read username") ||
		strings.Contains(combined, "terminal prompts disabled") ||
		strings.Contains(combined, "permission denied (publickey"):
		return ErrAuth
	case strings.Contains(combined, "could not resolve host") ||
		strings.Contains(combined, "connection timed out") ||
		strings.Contains(combined, "network is unreachable") ||
		strings.Contains(combined, "unable to access") ||
		strings.Contains(combined, "connection was reset") ||
		strings.Contains(combined, "gnutls") || strings.Contains(combined, "ssl"):
		return ErrNetwork
	case strings.Contains(combined, "destination path") && strings.Contains(combined, "already exists"):
		return ErrDestExists
	case strings.Contains(combined, "access is denied") || strings.Contains(combined, "permission"):
		return ErrPermission
	}

	return ErrUnknown
}

// failureMessage renders the actionable human explanation for a failed
// clone — never raw shell noise alone (the tail rides along separately
// as diagnostics).
func failureMessage(ctx context.Context, exitCode int, tail []string, kind ErrorKind) string {
	switch kind {
	case ErrCanceled:
		return "clone canceled — nothing was overwritten; delete the partial folder or clone again"
	case ErrTimeout:
		return "the clone exceeded its time budget (30 minutes) — try again on a faster connection"
	case ErrNotFound:
		return "repository not found — check the URL, and that the repository is public (private repositories need your credentials)"
	case ErrAuth:
		return "authentication failed — for private repositories over HTTPS, sign in with your Git credential manager; over SSH, make sure your key is loaded (ssh-add)"
	case ErrNetwork:
		return "network problem while cloning — check the internet connection and try again"
	case ErrDestExists:
		return "destination directory already exists and is not empty — choose another location or remove it first"
	case ErrPermission:
		return "insufficient permissions writing to the destination folder — choose a writable location"
	}
	return fmt.Sprintf("git exited with code %d — see the output below for git's own diagnostics", exitCode)
}

// gitEnv builds the non-interactive git environment (house style from
// the Coding Lab's patch exporter): the user's own environment (PATH,
// HOME/USERPROFILE, credential managers) plus prompt-killing overrides.
func gitEnv() []string {
	env := os.Environ()
	overrides := []string{
		"GIT_TERMINAL_PROMPT=0", // never hang waiting for a password
		"GIT_PAGER=cat",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_ASKPASS=", // no GUI prompters either
	}
	dropped := map[string]bool{
		"GIT_TERMINAL_PROMPT": true,
		"GIT_PAGER":           true,
		"GIT_OPTIONAL_LOCKS":  true,
		"GIT_ASKPASS":         true,
	}
	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}
		if dropped[key] {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, overrides...)
	return out
}

// validBranchName rejects obviously invalid branch names (git has its
// full own check-ref rules — this is the cheap pre-filter that keeps
// flags out of the argument vector).
func validBranchName(branch string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" || len(branch) > 255 {
		return false
	}
	if strings.HasPrefix(branch, "-") || strings.ContainsAny(branch, " \t\n\x00~^:?*[\\") ||
		strings.Contains(branch, "..") || strings.HasSuffix(branch, ".lock") ||
		strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") {
		return false
	}
	return true
}

// ValidateDestination checks the destination BEFORE spawning git:
// parent must exist and be a directory; the destination itself must not
// exist, or be an EMPTY directory. Cloning over an unrelated non-empty
// directory is refused here — never silently.
func ValidateDestination(dest string) error {
	if strings.TrimSpace(dest) == "" {
		return &Error{Kind: ErrInvalidDest, Message: "choose a destination folder for the clone"}
	}
	if !filepath.IsAbs(dest) {
		return &Error{Kind: ErrInvalidDest, Message: "the destination must be an absolute path"}
	}

	parent := filepath.Dir(dest)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return &Error{Kind: ErrInvalidDest, Message: "the parent folder does not exist: " + parent}
	}
	if !parentInfo.IsDir() {
		return &Error{Kind: ErrInvalidDest, Message: "the parent path is not a folder: " + parent}
	}

	info, err := os.Stat(dest)
	if os.IsNotExist(err) {
		return nil // clean destination — git creates it
	}
	if err != nil {
		return &Error{Kind: ErrInvalidDest, Message: "the destination cannot be inspected: " + err.Error()}
	}
	if !info.IsDir() {
		return &Error{Kind: ErrDestExists, Message: "the destination exists and is a file: " + dest}
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		return &Error{Kind: ErrPermission, Message: "the destination folder cannot be read: " + err.Error()}
	}
	if len(entries) > 0 {
		return &Error{Kind: ErrDestExists, Message: "the destination folder is not empty — choose a different location or empty it first"}
	}
	return nil
}

// Available reports whether the git binary can be executed, with an
// actionable error when it cannot.
func Available() error {
	bin := DefaultGitBinary()
	if _, err := exec.LookPath(bin); err != nil {
		return &Error{Kind: ErrGitUnavailable, Message: "Git was not found on this machine — install it from https://git-scm.com/download and try again"}
	}
	return nil
}

func shortHead(head string) string {
	if len(head) > 12 {
		return head[:12]
	}
	return head
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.Is(execErr.Err, exec.ErrNotFound)
	}
	return strings.Contains(err.Error(), exec.ErrNotFound.Error())
}

func exitCodeOf(waitErr error) int {
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
