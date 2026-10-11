package lab

import (
        "context"
        "errors"
        "io"
        "os"
        "path/filepath"
        "runtime"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/testfakes"
)

// TestMain arms the re-exec helper: when the production runner spawns
// a staged copy of this test binary as a scripted lab subprocess, the
// helper runs the scripted behavior instead of the test suite. This is
// the same cross-platform mechanism used by the gitclone/api suites —
// a real executable on every platform, no shell fixture.
func TestMain(m *testing.M) {
        if testfakes.RunLabHelper(os.Args[1:]) {
                return // the helper ran and ended the process
        }
        os.Exit(m.Run())
}

type scriptedRepairAgent struct {
        decisions []RepairDecision
        calls     int
}

func (a *scriptedRepairAgent) Repair(
        _ context.Context,
        _ *Task,
        _ VerificationSummary,
        _ []RepairIteration,
) (RepairDecision, error) {
        a.calls++

        if len(a.decisions) == 0 {
                return RepairDecision{}, ErrRepairNoAction
        }

        index := a.calls - 1
        if index >= len(a.decisions) {
                index = len(a.decisions) - 1
        }

        return a.decisions[index], nil
}

func TestRepairLoopStopsAtMaxIterations(t *testing.T) {
        manager, task, verifier := newRepairTestFixture(t)

        controller, err := NewRepairController(
                manager,
                verifier,
                3,
        )
        if err != nil {
                t.Fatal(err)
        }

        agent := &scriptedRepairAgent{
                decisions: []RepairDecision{
                        {
                                Command: repairFailCommand(t, task),
                        },
                },
        }

        summary, err := controller.Run(
                context.Background(),
                task,
                agent,
        )
        if err == nil {
                t.Fatal("expected repair loop to stop with an error")
        }

        if !errors.Is(err, ErrRepairMaxIterations) {
                t.Fatalf(
                        "expected ErrRepairMaxIterations, got %v",
                        err,
                )
        }

        if summary.Iterations != 3 {
                t.Fatalf(
                        "iterations = %d, want 3",
                        summary.Iterations,
                )
        }

        if agent.calls != 3 {
                t.Fatalf(
                        "agent calls = %d, want 3",
                        agent.calls,
                )
        }
}

func TestRepairLoopDoesNotRetryIdenticalCommandForever(t *testing.T) {
        manager, task, verifier := newRepairTestFixture(t)

        controller, err := NewRepairController(
                manager,
                verifier,
                100,
        )
        if err != nil {
                t.Fatal(err)
        }

        agent := &scriptedRepairAgent{
                decisions: []RepairDecision{
                        {
                                Command: repairFailCommand(t, task),
                        },
                },
        }

        summary, err := controller.Run(
                context.Background(),
                task,
                agent,
        )
        if err == nil {
                t.Fatal("expected repeated-command protection to stop the loop")
        }

        if !errors.Is(err, ErrRepairRepeatedCommand) {
                t.Fatalf(
                        "expected ErrRepairRepeatedCommand, got %v",
                        err,
                )
        }

        if summary.Iterations != 3 {
                t.Fatalf(
                        "iterations = %d, want 3",
                        summary.Iterations,
                )
        }

        if agent.calls != 3 {
                t.Fatalf(
                        "agent calls = %d, want 3",
                        agent.calls,
                )
        }

        if len(summary.History) != 3 {
                t.Fatalf(
                        "history length = %d, want 3",
                        len(summary.History),
                )
        }
}

func newRepairTestFixture(
        t *testing.T,
) (*TaskManager, *Task, *Verifier) {
        t.Helper()

        root := t.TempDir()

        source := filepath.Join(root, "source")
        workspaces := filepath.Join(root, "workspaces")

        if err := os.MkdirAll(source, 0o755); err != nil {
                t.Fatal(err)
        }

        // Ensure native Go discovery has a meaningful project to verify.
        goMod := []byte("module example.com/repair-test\n\ngo 1.22\n")
        goFile := []byte("package main\n\nfunc main() {}\n")

        if err := os.WriteFile(
                filepath.Join(source, "go.mod"),
                goMod,
                0o644,
        ); err != nil {
                t.Fatal(err)
        }

        if err := os.WriteFile(
                filepath.Join(source, "main.go"),
                goFile,
                0o644,
        ); err != nil {
                t.Fatal(err)
        }

        workspaceManager, err := NewWorkspaceManager(workspaces)
        if err != nil {
                t.Fatal(err)
        }

        runner := NewRunner(
                5*time.Second,
                2*1024*1024,
        )

        tasks, err := NewTaskManager(
                workspaceManager,
                runner,
                DefaultPolicy(),
                true,
        )
        if err != nil {
                t.Fatal(err)
        }

        verifier, err := NewVerifier(tasks)
        if err != nil {
                t.Fatal(err)
        }

        task := tasks.NewTask(
                "repair test",
                "exercise bounded repair",
        )

        if err := tasks.Start(
                context.Background(),
                task,
                source,
        ); err != nil {
                t.Fatal(err)
        }

        return tasks, task, verifier
}

// verificationContentCheck returns a meaningful, hermetic verification
// command that proves the workspace contains the expected project content.
// Trivial proof commands (echo/true/exit) are rejected by the verifier, so
// verification tests must assert real workspace state.
func verificationContentCheck(file, text string) string {
        if os.PathSeparator == '\\' {
                return "findstr " + text + " " + file
        }

        return "grep -q " + text + " " + file
}

// stageLabFailHelper stages a copy of the test binary INSIDE the task
// workspace and returns the repair Command whose execution FAILS
// DETERMINISTICALLY through the REAL production boundary.
//
// v1.4.0: this replaces the previous shell-string fixture (a POSIX
// shell "exit" invocation / a cmd.exe "exit" invocation, spelled
// through the platform shell prefix). The lab runner already wraps
// every command in the platform shell, so the fixture invokes the
// staged helper copy by RELATIVE path (the lab policy denies absolute
// and parent-escaping paths — the helper therefore must live inside
// the workspace), and the scripted-failure mode travels through the
// production Command.Environment merge via internal/testfakes.
func stageLabFailHelper(t *testing.T, task *Task) Command {
        t.Helper()

        workspaceRoot := ""
        if task.Workspace != nil {
                workspaceRoot = task.Workspace.Path
        }

        if workspaceRoot == "" {
                t.Fatal("repair fixture requires a started task workspace")
        }

        exe, err := os.Executable()
        if err != nil {
                t.Fatalf("test binary path: %v", err)
        }

        helperName := "repair-fail-helper"
        if runtime.GOOS == "windows" {
                helperName += ".exe"
        }

        dst := filepath.Join(workspaceRoot, helperName)

        in, err := os.Open(exe)
        if err != nil {
                t.Fatalf("open test binary: %v", err)
        }
        defer in.Close()

        out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
        if err != nil {
                t.Fatalf("stage helper: %v", err)
        }
        defer out.Close()

        if _, err := io.Copy(out, in); err != nil {
                t.Fatalf("copy helper: %v", err)
        }

        // Relative invocation: POSIX "./repair-fail-helper", Windows
        // ".\\repair-fail-helper.exe" — both resolve inside the workspace
        // (cmd.Dir) and both pass the workspace-jail policy.
        rel := "./" + helperName
        if runtime.GOOS == "windows" {
                rel = `.\` + helperName
        }

        return Command{
                Command: rel + " " + testfakes.LabFailArg,
                Environment: []string{
                        testfakes.LabModeEnv + "=fail",
                },
                // Keep every execution bounded: a hung helper fails fast
                // instead of stalling the suite.
                Timeout:         10 * time.Second,
                MaxOutputBytes: 64 * 1024,
        }
}

// repairFailCommand builds the deterministic-failure repair command for
// the given started task (see stageLabFailHelper).
func repairFailCommand(t *testing.T, task *Task) Command {
        t.Helper()
        return stageLabFailHelper(t, task)
}
