package lab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// Tool exposes the Coding Lab through the orchestrator's generic tool system.
//
// The tool is deliberately a single controlled interface. The model does not
// receive direct filesystem or process primitives; all operations flow through
// the Lab session, task, workspace, policy, runner, and verifier layers.
type Tool struct {
	cfg      *config.Config
	tasks    *TaskManager
	verifier *Verifier
	sessions *SessionRegistry

	intelMu sync.Mutex
	intel   IntelRecorder
}

// SetIntel installs the project intelligence recorder. Optional: without
// it the Lab works exactly as before; with it, VERIFIED build/test commands
// (never model claims) become persistent per-project facts.
func (t *Tool) SetIntel(rec IntelRecorder) {
	if t == nil || rec == nil {
		return
	}

	t.intelMu.Lock()
	t.intel = rec
	t.intelMu.Unlock()
}

// currentIntel returns the installed recorder (nil = none).
func (t *Tool) currentIntel() IntelRecorder {
	t.intelMu.Lock()
	defer t.intelMu.Unlock()
	return t.intel
}

// IntelRecorder receives durable project facts from Lab runs (v1.1.5
// Phase 6 persistent project intelligence). Implementations must be safe
// for concurrent use; errors are non-fatal (intelligence is best-effort,
// never a gate).
type IntelRecorder interface {
	// RecordVerifiedCommand notes a command that passed objective Lab
	// verification against the project rooted at root.
	RecordVerifiedCommand(root, kind, command string) error

	// Learn appends a bounded lesson for the project rooted at root.
	Learn(root, lesson string) error
}

// NewTool creates a fully initialized Coding Lab tool.
func NewTool(cfg *config.Config) (*Tool, error) {
	if cfg == nil {
		return nil, errors.New("lab: config is nil")
	}

	if !cfg.LabEnabled {
		return nil, errors.New("lab: Coding Lab is disabled")
	}

	timeout := cfg.EffectiveLabCommandTimeout()
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	workspaceRoot := strings.TrimSpace(cfg.LabWorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = filepath.Join(cfg.LabDir(), "workspaces")
	}

	workspaces, err := NewWorkspaceManager(workspaceRoot)
	if err != nil {
		return nil, err
	}

	runner := NewRunner(
		timeout,
		2*1024*1024,
	)

	policy := DefaultPolicy()
	policy.AllowNetwork = cfg.LabAllowNetwork

	tasks, err := NewTaskManager(
		workspaces,
		runner,
		policy,
		cfg.LabKeepWorkspaces,
	)
	if err != nil {
		return nil, err
	}

	verifier, err := NewVerifier(tasks)
	if err != nil {
		return nil, err
	}

	return &Tool{
		cfg:      cfg,
		tasks:    tasks,
		verifier: verifier,
		sessions: NewSessionRegistry(),
	}, nil
}

// Name implements the agent.Tool interface.
func (t *Tool) Name() string {
	return "coding_lab"
}

// ShortDescription is the one-line UI label (v1.1.7). The full Description
// remains the model-facing spec.
func (t *Tool) ShortDescription() string {
	return "Run isolated engineering tasks in a disposable workspace."
}

// Description implements the agent.Tool interface.
func (t *Tool) Description() string {
	return `Use the isolated Coding Lab to work on a local code project.

The Lab creates a disposable workspace, executes policy-controlled commands,
and runs objective verification checks.

Use this tool when the task requires inspecting, modifying, building, testing,
debugging, or verifying a local project.

Lifecycle:
1. start_task
2. inspect: read_file (+ run ls / grep as needed)
3. modify: edit_file (safe anchored edits) or run
4. verify the project
5. export_patch and/or promote explicitly
6. finish only after successful verification and promotion
7. close when retained workspace cleanup is required

Available actions:
- start_task
- run
- read_file (bounded line-numbered file view: path, offset, limit)
- edit_file (safe anchored edit: path, oldText, newText — oldText must occur EXACTLY ONCE; stale or ambiguous anchors are refused; re-read the file first if refused)
- verify
- export_patch
- promote
- finish
- fail
- cancel
- block
- close

Important:
- Command success does not mean the project is correct.
- Use verify before declaring a coding task complete.
- Network access is disabled by default.
- Dangerous, interactive, and workspace-escaping commands are blocked by policy.
- Promotion snapshots the original source before any source mutation.
- The Lab workspace is not implicitly promoted by finish.
- export_patch never mutates the source repository.`
}

// Parameters implements the agent.Tool interface.
func (t *Tool) Parameters() any {
	return codingLabParameters{
		Type: "object",
		Properties: map[string]labToolSchema{
			"action": {
				Type:        "string",
				Description: "Coding Lab lifecycle operation to perform.",
				Enum: []string{
					"start_task",
					"run",
					"read_file",
					"edit_file",
					"verify",
					"export_patch",
					"promote",
					"finish",
					"fail",
					"cancel",
					"block",
					"close",
				},
			},
			"taskId": {
				Type:        "string",
				Description: "Task ID returned by start_task.",
			},
			"title": {
				Type:        "string",
				Description: "Human-readable task title used by start_task.",
			},
			"description": {
				Type:        "string",
				Description: "Detailed coding objective used by start_task.",
			},
			"source": {
				Type:        "string",
				Description: "Absolute or relative local project directory copied into the isolated workspace.",
			},
			"command": {
				Type:        "string",
				Description: "Shell command executed inside the task workspace.",
			},
			"path": {
				Type:        "string",
				Description: "Workspace-relative file path used by read_file and edit_file.",
			},
			"offset": {
				Type:        "integer",
				Description: "1-based first line for read_file (default 1).",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of lines for read_file (default 2000).",
			},
			"oldText": {
				Type:        "string",
				Description: "Exact existing text to replace (edit_file). Must occur exactly once in the file — include surrounding lines until unique.",
			},
			"newText": {
				Type:        "string",
				Description: "Replacement text for edit_file.",
			},
			"workingDir": {
				Type:        "string",
				Description: "Workspace-relative directory used as the command working directory.",
			},
			"environment": {
				Type:        "array",
				Description: "Additional KEY=VALUE environment variables for the command.",
				Items: &labToolSchema{
					Type: "string",
				},
			},
			"timeoutSec": {
				Type:        "integer",
				Description: "Optional command timeout in seconds.",
			},
			"maxOutputMB": {
				Type:        "number",
				Description: "Optional maximum command output retained in megabytes.",
			},
			"buildCommand": {
				Type:        "string",
				Description: "Optional build command used by standard verification.",
			},
			"testCommand": {
				Type:        "string",
				Description: "Optional test command used by standard verification.",
			},
			"checks": {
				Type:        "array",
				Description: "Explicit verification checks.",
				Items: &labToolSchema{
					Type: "object",
				},
			},
			"error": {
				Type:        "string",
				Description: "Failure explanation used by the fail action.",
			},
			"reason": {
				Type:        "string",
				Description: "Reason used by the block action.",
			},
		},
		Required: []string{
			"action",
		},
	}
}

// Run implements the agent.Tool interface.
func (t *Tool) Run(
	ctx context.Context,
	args json.RawMessage,
) (string, error) {
	if t == nil ||
		t.tasks == nil ||
		t.verifier == nil ||
		t.sessions == nil {
		return "", errors.New("lab: tool is not initialized")
	}

	if len(args) == 0 {
		return "", errors.New("lab: tool arguments are empty")
	}

	var request codingLabRequest

	if err := json.Unmarshal(args, &request); err != nil {
		return "", fmt.Errorf("lab: invalid tool arguments: %w", err)
	}

	request.Action = strings.ToLower(strings.TrimSpace(request.Action))

	// A task session is serialized across all lifecycle operations after
	// start_task. This prevents concurrent model calls from racing on the
	// same Task, Workspace, verification state, or promotion state.
	if request.Action != "start_task" {
		session, err := t.sessions.Get(request.TaskID)
		if err != nil {
			return "", err
		}

		session.Lock()
		defer session.Unlock()
	}

	switch request.Action {
	case "start_task":
		return t.startTask(ctx, request)

	case "run":
		return t.runCommand(ctx, request)

	case "read_file":
		return t.readFileTask(request)

	case "edit_file":
		return t.editFileTask(request)

	case "verify":
		return t.verifyTask(ctx, request)

	case "export_patch":
		return t.exportPatchTask(ctx, request)

	case "promote":
		return t.promoteTask(ctx, request)

	case "finish":
		return t.finishTask(request)

	case "fail":
		return t.failTask(request)

	case "cancel":
		return t.cancelTask(request)

	case "block":
		return t.blockTask(request)

	case "close":
		return t.closeTask(request)

	default:
		return "", fmt.Errorf(
			"lab: unknown action %q; expected start_task, run, read_file, edit_file, verify, export_patch, promote, finish, fail, cancel, block, or close",
			request.Action,
		)
	}
}

type codingLabRequest struct {
	Action string `json:"action"`

	TaskID string `json:"taskId,omitempty"`

	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`

	Command     string   `json:"command,omitempty"`
	WorkingDir  string   `json:"workingDir,omitempty"`
	Environment []string `json:"environment,omitempty"`

	Path    string `json:"path,omitempty"`
	Offset  int    `json:"offset,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	OldText string `json:"oldText,omitempty"`
	NewText string `json:"newText,omitempty"`

	TimeoutSec  int     `json:"timeoutSec,omitempty"`
	MaxOutputMB float64 `json:"maxOutputMB,omitempty"`

	BuildCommand string `json:"buildCommand,omitempty"`
	TestCommand  string `json:"testCommand,omitempty"`

	Checks []codingLabCheck `json:"checks,omitempty"`

	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type codingLabCheck struct {
	Name         string `json:"name,omitempty"`
	Command      string `json:"command"`
	WorkingDir   string `json:"workingDir,omitempty"`
	Required     bool   `json:"required"`
	AllowFailure bool   `json:"allowFailure,omitempty"`
	TimeoutSec   int    `json:"timeoutSec,omitempty"`
}

type codingLabParameters struct {
	Type       string                   `json:"type"`
	Properties map[string]labToolSchema `json:"properties"`
	Required   []string                 `json:"required"`
}

type labToolSchema struct {
	Type        string         `json:"type,omitempty"`
	Description string         `json:"description,omitempty"`
	Enum        []string       `json:"enum,omitempty"`
	Items       *labToolSchema `json:"items,omitempty"`
}

func (t *Tool) startTask(
	ctx context.Context,
	request codingLabRequest,
) (string, error) {
	title := strings.TrimSpace(request.Title)
	description := strings.TrimSpace(request.Description)
	source := strings.TrimSpace(request.Source)

	if source == "" {
		return "", ErrInvalidSource
	}

	task := t.tasks.NewTask(title, description)

	if err := t.tasks.Start(ctx, task, source); err != nil {
		payload, encodeErr := encodeLabResponse(
			codingLabResponse{
				OK:     false,
				Action: "start_task",
				Task:   task,
				Error:  err.Error(),
			},
		)
		if encodeErr != nil {
			err = errors.Join(err, encodeErr)
		}

		return payload, err
	}

	if _, err := t.sessions.Create(task); err != nil {
		// The task has successfully created a workspace, but the runtime
		// session could not be registered. Clean it up rather than leaving
		// an orphaned workspace behind.
		cleanupErr := t.tasks.Fail(task, err)

		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}

		payload, encodeErr := encodeLabResponse(
			codingLabResponse{
				OK:     false,
				Action: "start_task",
				Task:   task,
				Error:  err.Error(),
			},
		)
		if encodeErr != nil {
			err = errors.Join(err, encodeErr)
		}

		return payload, err
	}

	return encodeLabResponse(
		codingLabResponse{
			OK:      true,
			Action:  "start_task",
			Task:    task,
			Message: "Coding Lab task started in an isolated workspace.",
		},
	)
}

func (t *Tool) runCommand(
	ctx context.Context,
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	command := Command{
		Command:     strings.TrimSpace(request.Command),
		WorkingDir:  strings.TrimSpace(request.WorkingDir),
		Environment: append([]string(nil), request.Environment...),
	}

	if request.TimeoutSec > 0 {
		command.Timeout = time.Duration(request.TimeoutSec) * time.Second
	}

	if request.MaxOutputMB > 0 {
		command.MaxOutputBytes = int64(request.MaxOutputMB * 1024 * 1024)
	}

	result, err := t.tasks.RunCommand(ctx, task, command)

	_ = t.sessions.Touch(task.ID)

	response := codingLabResponse{
		OK:     err == nil && result.Success,
		Action: "run",
		Task:   task,
		Result: &result,
	}

	if err != nil {
		response.Error = err.Error()
	}

	encoded, encodeErr := encodeLabResponse(response)
	if encodeErr != nil {
		return "", encodeErr
	}

	return encoded, err
}

// readFileTask serves the read_file action: a bounded, line-numbered view
// of one workspace file — the INSPECT step of the coding loop that does not
// depend on shell quoting or unbounded cat output.
func (t *Tool) readFileTask(request codingLabRequest) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(request.Path) == "" {
		return "", errors.New("lab: read_file: path is required")
	}

	file, err := t.ReadFile(task, request.Path, request.Offset, request.Limit)

	_ = t.sessions.Touch(task.ID)

	response := codingLabResponse{
		OK:     err == nil,
		Action: "read_file",
		Task:   task,
		File:   &file,
	}

	if err != nil {
		response.Error = err.Error()
		response.File = nil
	}

	encoded, encodeErr := encodeLabResponse(response)
	if encodeErr != nil {
		return "", encodeErr
	}

	if err != nil {
		return encoded, err
	}

	return encoded, nil
}

// editFileTask serves the edit_file action: the anchored, verified, atomic
// safe edit (see safeedit.go). Refusals (stale/ambiguous anchors) are
// returned as errors so the orchestrator's failure classification can
// diagnose them and the model re-plans.
func (t *Tool) editFileTask(request codingLabRequest) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(request.Path) == "" {
		return "", errors.New("lab: edit_file: path is required")
	}

	edit, err := t.EditFile(task, request.Path, request.OldText, request.NewText)

	_ = t.sessions.Touch(task.ID)

	response := codingLabResponse{
		OK:     err == nil,
		Action: "edit_file",
		Task:   task,
		Edit:   &edit,
	}

	if err != nil {
		response.Error = err.Error()
		response.Edit = nil
	} else {
		response.Message = fmt.Sprintf(
			"Edited %s at line %d (%d bytes replaced by %d). The edit is applied but NOT verified — run verify before claiming completion.",
			edit.Path, edit.Line, edit.ReplacedLen, edit.InsertedLen,
		)
	}

	encoded, encodeErr := encodeLabResponse(response)
	if encodeErr != nil {
		return "", encodeErr
	}

	if err != nil {
		return encoded, err
	}

	return encoded, nil
}

func (t *Tool) verifyTask(
	ctx context.Context,
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	var summary VerificationSummary

	switch {
	case len(request.Checks) > 0:
		checks := make([]VerificationCheck, 0, len(request.Checks))

		for index, check := range request.Checks {
			command := strings.TrimSpace(check.Command)

			if command == "" {
				return "", fmt.Errorf(
					"lab: verification check %d has an empty command",
					index+1,
				)
			}

			verificationCommand := Command{
				Command:    command,
				WorkingDir: strings.TrimSpace(check.WorkingDir),
			}

			if check.TimeoutSec > 0 {
				verificationCommand.Timeout =
					time.Duration(check.TimeoutSec) * time.Second
			}

			checks = append(checks, VerificationCheck{
				Name:         strings.TrimSpace(check.Name),
				Command:      verificationCommand,
				Required:     true,
				AllowFailure: check.AllowFailure,
			})
		}

		summary, err = t.verifier.Verify(
			ctx,
			task,
			checks,
		)

	default:
		// VerifyStandard auto-discovers native project checks when the caller
		// does not provide explicit build/test commands.
		summary, err = t.verifier.VerifyStandard(
			ctx,
			task,
			request.BuildCommand,
			request.TestCommand,
		)
	}

	_ = t.sessions.Touch(task.ID)

	// v1.1.5 Phase 6: verified commands become persistent project facts.
	// Only checks that PASSED objective verification are recorded, and only
	// against the task's SOURCE root (the project the workspace was cloned
	// from) — a lab workspace is disposable, the project is not.
	if summary.Passed && t.currentIntel() != nil && task.Workspace != nil && task.Workspace.Source != "" {
		for _, res := range summary.Results {
			if res.Status != VerificationPassed || res.Result.Command == "" {
				continue
			}

			kind := ""
			switch res.Name {
			case "build":
				kind = "build"
			case "tests", "test":
				kind = "test"
			default:
				continue
			}

			if err := t.currentIntel().RecordVerifiedCommand(
				task.Workspace.Source, kind, res.Result.Command,
			); err != nil {
				// Best-effort: never fail a successful verification over
				// intelligence recording.
				continue
			}
		}
	}

	response := codingLabResponse{
		OK:           err == nil && summary.Passed,
		Action:       "verify",
		Task:         task,
		Verification: &summary,
	}

	if err != nil {
		response.Error = err.Error()
	}

	encoded, encodeErr := encodeLabResponse(response)
	if encodeErr != nil {
		return "", encodeErr
	}

	return encoded, err
}

// exportPatchTask produces a unified binary-capable Git patch between the
// original source tree and the modified Lab workspace.
//
// The source repository is copied to a temporary comparison tree with .git
// excluded. The workspace is copied to a second temporary tree. git diff
// therefore sees only project content and cannot export repository metadata.
//
// This operation never mutates the source repository or the Lab workspace.
func (t *Tool) exportPatchTask(
	ctx context.Context,
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	if task.Workspace == nil {
		return "", ErrInvalidWorkspace
	}

	if task.Status != TaskRunning {
		return "", fmt.Errorf(
			"%w: current status=%s",
			ErrTaskNotRunnable,
			task.Status,
		)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	root := t.tasks.Workspaces.Root

	tempRoot, err := os.MkdirTemp(
		root,
		"export-patch-*",
	)
	if err != nil {
		return "", fmt.Errorf(
			"lab: create patch workspace: %w",
			err,
		)
	}
	defer os.RemoveAll(tempRoot)

	sourceCopy := filepath.Join(tempRoot, "source")
	workspaceCopy := filepath.Join(tempRoot, "workspace")

	if err := os.MkdirAll(sourceCopy, 0o755); err != nil {
		return "", fmt.Errorf(
			"lab: create source comparison tree: %w",
			err,
		)
	}

	if err := os.MkdirAll(workspaceCopy, 0o755); err != nil {
		return "", fmt.Errorf(
			"lab: create workspace comparison tree: %w",
			err,
		)
	}

	if err := copyTree(
		ctx,
		task.Workspace.Source,
		sourceCopy,
	); err != nil {
		return "", fmt.Errorf(
			"lab: copy source for patch: %w",
			err,
		)
	}

	if err := copyTreeContents(
		ctx,
		task.Workspace.Path,
		workspaceCopy,
	); err != nil {
		return "", fmt.Errorf(
			"lab: copy workspace for patch: %w",
			err,
		)
	}

	patchDir := filepath.Join(root, "patches")

	if err := os.MkdirAll(patchDir, 0o755); err != nil {
		return "", fmt.Errorf(
			"lab: create patch directory: %w",
			err,
		)
	}

	patchPath := filepath.Join(
		patchDir,
		task.ID+".patch",
	)

	output, err := runGitNoIndexDiff(
		ctx,
		tempRoot,
		"source",
		"workspace",
	)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(
		patchPath,
		[]byte(output),
		0o600,
	); err != nil {
		return "", fmt.Errorf(
			"lab: write patch %q: %w",
			patchPath,
			err,
		)
	}

	task.Metadata["patchPath"] = patchPath

	_ = t.sessions.Touch(task.ID)

	return encodeLabResponse(
		codingLabResponse{
			OK:           true,
			Action:       "export_patch",
			Task:         task,
			ArtifactPath: patchPath,
			Message:      "Patch exported without modifying the source repository.",
		},
	)
}

func runGitNoIndexDiff(
	ctx context.Context,
	workDir string,
	sourcePath string,
	workspacePath string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.CommandContext(
		ctx,
		"git",
		"diff",
		"--no-index",
		"--binary",
		"--src-prefix=a/",
		"--dst-prefix=b/",
		"--",
		sourcePath,
		workspacePath,
	)
	cmd.Dir = workDir

	env := append(
		[]string(nil),
		os.Environ()...,
	)

	env = append(
		env,
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_NOSYSTEM=1",
	)

	cmd.Env = env

	output, err := cmd.CombinedOutput()

	if err == nil {
		return string(output), nil
	}

	var exitErr *exec.ExitError

	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			// git diff --no-index returns 1 when differences exist.
			return string(output), nil

		default:
			return "", fmt.Errorf(
				"lab: export patch git diff failed with exit code %d: %s",
				exitErr.ExitCode(),
				strings.TrimSpace(string(output)),
			)
		}
	}

	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}

	return "", fmt.Errorf(
		"lab: execute git diff for patch: %w",
		err,
	)
}

func (t *Tool) promoteTask(
	ctx context.Context,
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	if task.Workspace == nil {
		return "", ErrInvalidWorkspace
	}

	if !task.IsVerified() {
		return "", ErrTaskNotVerified
	}

	snapshotPath, err := t.tasks.Workspaces.Promote(
		ctx,
		task.Workspace,
	)
	if err != nil {
		payload, encodeErr := encodeLabResponse(
			codingLabResponse{
				OK:     false,
				Action: "promote",
				Task:   task,
				Error:  err.Error(),
			},
		)
		if encodeErr != nil {
			err = errors.Join(err, encodeErr)
		}

		return payload, err
	}

	if task.Metadata == nil {
		task.Metadata = make(map[string]string)
	}

	verifiedAt := ""
	if task.VerifiedAt != nil {
		verifiedAt = task.VerifiedAt.UTC().Format(time.RFC3339Nano)
	}

	task.Metadata["promotionSnapshot"] = snapshotPath
	task.Metadata["promotionVerifiedAt"] = verifiedAt

	_ = t.sessions.Touch(task.ID)

	return encodeLabResponse(
		codingLabResponse{
			OK:           true,
			Action:       "promote",
			Task:         task,
			SnapshotPath: snapshotPath,
			Message:      "Verified Lab workspace promoted to the source repository after creating a recovery snapshot.",
		},
	)
}

func (t *Tool) finishTask(
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	if !task.IsVerified() {
		return "", ErrTaskVerificationStale
	}

	currentVerification := ""
	if task.VerifiedAt != nil {
		currentVerification = task.VerifiedAt.UTC().Format(time.RFC3339Nano)
	}

	promotedVerification := ""
	if task.Metadata != nil {
		promotedVerification = task.Metadata["promotionVerifiedAt"]
	}

	if currentVerification == "" ||
		promotedVerification == "" ||
		currentVerification != promotedVerification {
		return "", errors.New(
			"lab: task must be promoted after its current verification before finish",
		)
	}

	if err := t.tasks.Finish(task); err != nil {
		return "", err
	}

	response, encodeErr := encodeLabResponse(
		codingLabResponse{
			OK:      true,
			Action:  "finish",
			Task:    task,
			Message: "Coding Lab task marked successful.",
		},
	)

	removeErr := t.sessions.RemoveCompleted(task.ID)

	if encodeErr != nil {
		return "", encodeErr
	}

	if removeErr != nil {
		return response, removeErr
	}

	return response, nil
}

func (t *Tool) failTask(
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	reason := strings.TrimSpace(request.Error)

	var cause error

	if reason != "" {
		cause = errors.New(reason)
	} else {
		cause = errors.New("task failed by agent request")
	}

	if err := t.tasks.Fail(task, cause); err != nil {
		return "", err
	}

	response, encodeErr := encodeLabResponse(
		codingLabResponse{
			OK:      true,
			Action:  "fail",
			Task:    task,
			Message: "Coding Lab task marked failed.",
		},
	)

	removeErr := t.sessions.RemoveCompleted(task.ID)

	if encodeErr != nil {
		return "", encodeErr
	}

	if removeErr != nil {
		return response, removeErr
	}

	return response, nil
}

func (t *Tool) cancelTask(
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	if err := t.tasks.Cancel(task); err != nil {
		return "", err
	}

	response, encodeErr := encodeLabResponse(
		codingLabResponse{
			OK:      true,
			Action:  "cancel",
			Task:    task,
			Message: "Coding Lab task canceled.",
		},
	)

	removeErr := t.sessions.RemoveCompleted(task.ID)

	if encodeErr != nil {
		return "", encodeErr
	}

	if removeErr != nil {
		return response, removeErr
	}

	return response, nil
}

func (t *Tool) blockTask(
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	reason := strings.TrimSpace(request.Reason)

	if reason == "" {
		reason = "task blocked by agent"
	}

	if err := t.tasks.Block(task, reason); err != nil {
		return "", err
	}

	response, encodeErr := encodeLabResponse(
		codingLabResponse{
			OK:      true,
			Action:  "block",
			Task:    task,
			Message: "Coding Lab task blocked.",
		},
	)

	removeErr := t.sessions.RemoveCompleted(task.ID)

	if encodeErr != nil {
		return "", encodeErr
	}

	if removeErr != nil {
		return response, removeErr
	}

	return response, nil
}

func (t *Tool) closeTask(
	request codingLabRequest,
) (string, error) {
	task, err := t.lookupTask(request.TaskID)
	if err != nil {
		return "", err
	}

	if err := t.tasks.Close(task); err != nil {
		return "", err
	}

	if err := t.sessions.Delete(task.ID); err != nil {
		return "", err
	}

	return encodeLabResponse(
		codingLabResponse{
			OK:      true,
			Action:  "close",
			Task:    task,
			Message: "Coding Lab workspace released.",
		},
	)
}

func (t *Tool) lookupTask(id string) (*Task, error) {
	id = strings.TrimSpace(id)

	if id == "" {
		return nil, errors.New("lab: taskId is required")
	}

	return t.sessions.GetTask(id)
}

type codingLabResponse struct {
	OK           bool                 `json:"ok"`
	Action       string               `json:"action"`
	Task         *Task                `json:"task,omitempty"`
	Result       *CommandResult       `json:"result,omitempty"`
	Verification *VerificationSummary `json:"verification,omitempty"`
	File         *FileReadResult      `json:"file,omitempty"`
	Edit         *EditResult          `json:"edit,omitempty"`
	Message      string               `json:"message,omitempty"`
	Error        string               `json:"error,omitempty"`
	ArtifactPath string               `json:"artifactPath,omitempty"`
	SnapshotPath string               `json:"snapshotPath,omitempty"`
}

func encodeLabResponse(value codingLabResponse) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("lab: encode response: %w", err)
	}

	return string(data), nil
}
