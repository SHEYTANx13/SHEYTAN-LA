package lab

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func TestWorkspaceIsolation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source")
	workspaceRoot := filepath.Join(root, "workspaces")

	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	original := filepath.Join(source, "main.txt")
	if err := os.WriteFile(original, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewWorkspaceManager(workspaceRoot)
	if err != nil {
		t.Fatalf("NewWorkspaceManager: %v", err)
	}

	taskWorkspace, err := manager.Create(
		context.Background(),
		source,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if taskWorkspace.Path == source {
		t.Fatal("workspace must not equal source")
	}

	copied := filepath.Join(taskWorkspace.Path, "main.txt")

	data, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}

	if string(data) != "original" {
		t.Fatalf("copied file = %q, want original", string(data))
	}

	if _, err := os.Stat(filepath.Join(taskWorkspace.Path, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".git should not be copied, stat error = %v", err)
	}

	if err := os.WriteFile(copied, []byte("modified"), 0o644); err != nil {
		t.Fatalf("modify workspace file: %v", err)
	}

	originalData, err := os.ReadFile(original)
	if err != nil {
		t.Fatalf("read original file: %v", err)
	}

	if string(originalData) != "original" {
		t.Fatalf("source repository was modified: %q", string(originalData))
	}

	if _, err := taskWorkspace.PathFor(filepath.Join("..", "escape")); !errors.Is(err, ErrInvalidWorkspace) {
		t.Fatalf("expected workspace escape to be rejected, got %v", err)
	}

	if _, err := taskWorkspace.PathFor(os.TempDir()); !errors.Is(err, ErrInvalidWorkspace) {
		t.Fatalf("expected absolute path to be rejected, got %v", err)
	}

	if err := manager.Remove(taskWorkspace); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(taskWorkspace.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace still exists after Remove: %v", err)
	}
}

// TestWorkspacePathForCanonicalSpelling is the v1.2.0 Windows regression:
// the temp root on CI is reached through an 8.3 short name (RUNNER~1), so
// the workspace path and its EvalSymlinks-resolved form are two DIFFERENT
// spellings of the same directory. PathFor compared them as raw strings
// and rejected every valid workspace file ("lab: invalid workspace path").
//
// On Unix the same mismatch class is reproduced with a symlink: the
// workspace spelling goes through the link, the canonical form does not.
// The security contract is pinned at the same time — .., absolute and
// outside-root paths must still be refused across spellings.
func TestWorkspacePathForCanonicalSpelling(t *testing.T) {
	t.Parallel()

	real := t.TempDir()

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink privileges unavailable on this Windows host")
		}
		t.Fatal(err)
	}

	manager, err := NewWorkspaceManager(link)
	if err != nil {
		t.Fatalf("NewWorkspaceManager: %v", err)
	}

	// The manager root must be canonicalized to the real path so every
	// derived workspace shares one representation.
	//
	// v1.3.5: the EXPECTED side is canonicalized too. On Windows CI the
	// temp root arrives through an 8.3 short name (RUNNER~1); comparing
	// the canonical manager root against the raw t.TempDir() spelling
	// compared two representations of the SAME directory and failed
	// ("manager root = C:\Users\runneradmin\..., want canonical =
	// C:\Users\RUNNER~1\..."). Canonical-to-canonical keeps the
	// contract and drops the spelling accident.
	wantRoot, wantRootErr := canonicalPath(real)
	if wantRootErr != nil {
		t.Fatalf("canonicalize expected root: %v", wantRootErr)
	}
	if manager.Root != wantRoot {
		t.Fatalf("manager root = %q, want canonical %q", manager.Root, wantRoot)
	}

	workspace := &Workspace{
		ID:     "canonical-test",
		Path:   filepath.Join(link, "canonical-test"), // non-canonical spelling
		Source: real,
	}

	if err := os.MkdirAll(workspace.Path, 0o755); err != nil {
		t.Fatal(err)
	}

	// A nested, not-yet-existing file inside a non-canonical workspace
	// spelling must resolve AND stay inside the jail.
	got, err := workspace.PathFor("nested/file.txt")
	if err != nil {
		t.Fatalf("PathFor with non-canonical workspace spelling: %v", err)
	}

	// Canonical-to-canonical comparison: the expected side resolves
	// through the same nearest-existing-ancestor rule the production
	// path checks use, so an 8.3 / symlink spelling of the temp root
	// cannot make two equal paths compare unequal.
	want, wantErr := canonicalPath(filepath.Join(real, "canonical-test", "nested", "file.txt"))
	if wantErr != nil {
		t.Fatal(wantErr)
	}

	if got != want {
		t.Fatalf("PathFor = %q, want canonical %q", got, want)
	}

	// The jail still holds across spellings.
	for _, escape := range []string{
		filepath.Join("..", "escape"),
		"..\\escape",
		real,
	} {
		if _, err := workspace.PathFor(escape); !errors.Is(err, ErrInvalidWorkspace) {
			t.Fatalf("escape %q must be refused, got %v", escape, err)
		}
	}
}

func TestRunnerSuccessAndFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspaces")
	source := filepath.Join(root, "source")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	manager, err := NewWorkspaceManager(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := manager.Create(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(
		5*time.Second,
		64*1024,
	)

	successResult, err := runner.Run(
		context.Background(),
		workspace,
		Command{
			Command: "echo lab-success",
		},
	)
	if err != nil {
		t.Fatalf("success command error: %v", err)
	}

	if !successResult.Success {
		t.Fatal("successful command returned Success=false")
	}

	if !strings.Contains(successResult.Output, "lab-success") {
		t.Fatalf("unexpected output: %q", successResult.Output)
	}

	failureCommand := "false"
	if runtime.GOOS == "windows" {
		failureCommand = "exit /b 1"
	}

	failureResult, err := runner.Run(
		context.Background(),
		workspace,
		Command{
			Command: failureCommand,
		},
	)
	if err == nil {
		t.Fatal("expected failing command to return an error")
	}

	if failureResult.Success {
		t.Fatal("failing command returned Success=true")
	}

	if failureResult.ExitCode == 0 {
		t.Fatalf("failing command returned exit code %d", failureResult.ExitCode)
	}

	timeoutResult, err := runner.Run(
		context.Background(),
		workspace,
		Command{
			Command:        sleepCommand(),
			Timeout:        100 * time.Millisecond,
			MaxOutputBytes: 64 * 1024,
		},
	)
	if !errors.Is(err, ErrCommandTimedOut) {
		t.Fatalf("expected timeout error, got %v", err)
	}

	if !timeoutResult.TimedOut {
		t.Fatal("timeout result did not set TimedOut")
	}
}

func TestPolicyBlocksDangerousAndNetworkCommands(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy()

	workspace := filepath.Join(t.TempDir(), "workspace")

	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "disk format",
			command: "format C:",
		},
		{
			name:    "git reset hard",
			command: "git reset --hard HEAD",
		},
		{
			name:    "git push",
			command: "git push origin main",
		},
		{
			name:    "curl",
			command: "curl https://example.com",
		},
		{
			name:    "powershell download",
			command: "powershell Invoke-WebRequest https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := policy.EvaluateForWorkspace(
				tt.command,
				workspace,
			); err == nil {
				t.Fatalf(
					"expected command to be blocked: %q",
					tt.command,
				)
			}
		})
	}

	policy.AllowNetwork = true

	if err := policy.EvaluateForWorkspace(
		"curl https://example.com",
		workspace,
	); err != nil {
		t.Fatalf(
			"network-enabled policy should allow curl: %v",
			err,
		)
	}
}

func TestTaskVerificationGateAndInvalidation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source")
	workspaceRoot := filepath.Join(root, "workspaces")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(source, "main.txt"),
		[]byte("hello"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	workspaces, err := NewWorkspaceManager(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := NewTaskManager(
		workspaces,
		NewRunner(5*time.Second, 64*1024),
		DefaultPolicy(),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	verifier, err := NewVerifier(manager)
	if err != nil {
		t.Fatal(err)
	}

	task := manager.NewTask(
		"verification gate",
		"test task lifecycle",
	)

	if err := manager.Start(
		context.Background(),
		task,
		source,
	); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := manager.Finish(task); !errors.Is(err, ErrTaskNotVerified) {
		t.Fatalf(
			"expected Finish to require verification, got %v",
			err,
		)
	}

	successSummary, err := verifier.VerifyCommands(
		context.Background(),
		task,
		[]string{verificationContentCheck("main.txt", "hello")},
	)
	if err != nil {
		t.Fatalf("VerifyCommands success: %v", err)
	}

	if !successSummary.Passed {
		t.Fatal("verification summary should be passed")
	}

	if !task.IsVerified() {
		t.Fatal("task should be verified after successful verification")
	}

	if err := manager.Finish(task); err != nil {
		t.Fatalf("Finish after verification: %v", err)
	}

	if task.Status != TaskSucceeded {
		t.Fatalf(
			"task status = %s, want %s",
			task.Status,
			TaskSucceeded,
		)
	}
}

func TestTaskVerificationInvalidatesAfterCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source")
	workspaceRoot := filepath.Join(root, "workspaces")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(source, "main.txt"),
		[]byte("hello"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	workspaces, err := NewWorkspaceManager(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := NewTaskManager(
		workspaces,
		NewRunner(5*time.Second, 64*1024),
		DefaultPolicy(),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	verifier, err := NewVerifier(manager)
	if err != nil {
		t.Fatal(err)
	}

	task := manager.NewTask(
		"invalidation",
		"verification must become stale after mutation",
	)

	if err := manager.Start(
		context.Background(),
		task,
		source,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := verifier.VerifyCommands(
		context.Background(),
		task,
		[]string{verificationContentCheck("main.txt", "hello")},
	); err != nil {
		t.Fatalf("VerifyCommands: %v", err)
	}

	if !task.IsVerified() {
		t.Fatal("task should initially be verified")
	}

	if _, err := manager.RunCommand(
		context.Background(),
		task,
		Command{
			Command: "echo mutation",
		},
	); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}

	if task.IsVerified() {
		t.Fatal("new command must invalidate previous verification")
	}

	if err := manager.Finish(task); !errors.Is(err, ErrTaskVerificationStale) {
		t.Fatalf(
			"expected stale verification error, got %v",
			err,
		)
	}
}

func TestVerifierRecordsFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source")
	workspaceRoot := filepath.Join(root, "workspaces")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	workspaces, err := NewWorkspaceManager(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := NewTaskManager(
		workspaces,
		NewRunner(5*time.Second, 64*1024),
		DefaultPolicy(),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	verifier, err := NewVerifier(manager)
	if err != nil {
		t.Fatal(err)
	}

	task := manager.NewTask(
		"failed verification",
		"required verification should fail",
	)

	if err := manager.Start(
		context.Background(),
		task,
		source,
	); err != nil {
		t.Fatal(err)
	}

	command := "false"
	if runtime.GOOS == "windows" {
		command = "exit /b 1"
	}

	summary, err := verifier.VerifyCommands(
		context.Background(),
		task,
		[]string{command},
	)
	if !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf(
			"expected ErrVerificationFailed, got %v",
			err,
		)
	}

	if summary.Passed {
		t.Fatal("failed verification cannot report Passed=true")
	}

	if task.IsVerified() {
		t.Fatal("failed verification must not mark task verified")
	}

	if task.LastVerification == nil {
		t.Fatal("failed verification should still be recorded")
	}

	if task.LastVerification.Passed {
		t.Fatal("recorded failed verification cannot be marked passed")
	}

	if err := manager.Finish(task); !errors.Is(err, ErrTaskVerificationStale) {
		t.Fatalf(
			"expected Finish to reject failed/stale verification, got %v",
			err,
		)
	}
}

func TestSessionRegistryLifecycle(t *testing.T) {
	t.Parallel()

	registry := NewSessionRegistry()

	task := &Task{
		ID:     "task-test-session",
		Status: TaskRunning,
	}

	session, err := registry.Create(task)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if session.ID == "" {
		t.Fatal("session ID must not be empty")
	}

	got, err := registry.Get(session.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Task != task {
		t.Fatal("registry returned a different task pointer")
	}

	gotTask, err := registry.GetTask(session.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if gotTask != task {
		t.Fatal("GetTask returned a different task pointer")
	}

	if registry.Count() != 1 {
		t.Fatalf("Count = %d, want 1", registry.Count())
	}

	task.Status = TaskSucceeded

	if err := registry.RemoveCompleted(session.ID); err != nil {
		t.Fatalf("RemoveCompleted: %v", err)
	}

	if registry.Count() != 0 {
		t.Fatalf("Count after removal = %d, want 0", registry.Count())
	}
}

func TestCodingLabToolEndToEnd(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(source, "app.txt"),
		[]byte("release"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()

	cfg.DataDir = root
	cfg.ModelsDir = filepath.Join(root, "models")
	cfg.SessionsDir = filepath.Join(root, "sessions")
	cfg.LabEnabled = true
	cfg.LabWorkspaceRoot = filepath.Join(root, "lab", "workspaces")
	cfg.LabCommandTimeoutSec = 5
	cfg.LabKeepWorkspaces = false
	cfg.LabAllowNetwork = false

	tool, err := NewTool(cfg)
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	startArgs := marshalLabTestArgs(t, map[string]any{
		"action":      "start_task",
		"title":       "tool integration",
		"description": "verify the full Coding Lab tool lifecycle",
		"source":      source,
	})

	startOutput, err := tool.Run(
		context.Background(),
		startArgs,
	)
	if err != nil {
		t.Fatalf("start_task: %v", err)
	}

	taskID := extractTaskID(t, startOutput)

	runArgs := marshalLabTestArgs(t, map[string]any{
		"action":  "run",
		"taskId":  taskID,
		"command": "echo tool-ok",
	})

	runOutput, err := tool.Run(
		context.Background(),
		runArgs,
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.Contains(runOutput, "tool-ok") {
		t.Fatalf(
			"run output does not contain expected text: %s",
			runOutput,
		)
	}

	verifyArgs := marshalLabTestArgs(t, map[string]any{
		"action": "verify",
		"taskId": taskID,
		"checks": []map[string]any{
			{
				"name":     "content",
				"command":  verificationContentCheck("app.txt", "release"),
				"required": true,
			},
		},
	})

	verifyOutput, err := tool.Run(
		context.Background(),
		verifyArgs,
	)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if !strings.Contains(verifyOutput, `"passed":true`) {
		t.Fatalf(
			"verification response was not successful: %s",
			verifyOutput,
		)
	}

	promoteArgs := marshalLabTestArgs(t, map[string]any{
		"action": "promote",
		"taskId": taskID,
	})

	promoteOutput, err := tool.Run(
		context.Background(),
		promoteArgs,
	)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}

	if !strings.Contains(promoteOutput, `"ok":true`) {
		t.Fatalf(
			"promotion response was not successful: %s",
			promoteOutput,
		)
	}

	finishArgs := marshalLabTestArgs(t, map[string]any{
		"action": "finish",
		"taskId": taskID,
	})

	finishOutput, err := tool.Run(
		context.Background(),
		finishArgs,
	)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}

	if !strings.Contains(
		finishOutput,
		`"status":"succeeded"`,
	) {
		t.Fatalf(
			"finish response did not report success: %s",
			finishOutput,
		)
	}
}

func marshalLabTestArgs(
	t *testing.T,
	value map[string]any,
) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	return data
}

func extractTaskID(
	t *testing.T,
	output string,
) string {
	t.Helper()

	var response struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}

	if err := json.Unmarshal(
		[]byte(output),
		&response,
	); err != nil {
		t.Fatalf(
			"decode task response: %v\noutput=%s",
			err,
			output,
		)
	}

	if response.Task.ID == "" {
		t.Fatalf("task ID missing from response: %s", output)
	}

	return response.Task.ID
}

func sleepCommand() string {
	if runtime.GOOS == "windows" {
		return "ping 127.0.0.1 -n 6 >nul"
	}

	return "sleep 5"
}
