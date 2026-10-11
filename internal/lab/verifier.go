package lab

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	ErrVerificationFailed   = errors.New("lab: verification failed")
	ErrVerificationEmpty    = errors.New("lab: no verification checks configured")
	ErrVerificationCanceled = errors.New("lab: verification canceled")
	ErrVerificationTrivial  = errors.New("lab: verification command is not meaningful")
)

const (
	maxVerificationCommandLength = 16 * 1024
)

// VerificationStatus describes the outcome of one verification check.
type VerificationStatus string

const (
	VerificationPassed   VerificationStatus = "passed"
	VerificationFailed   VerificationStatus = "failed"
	VerificationCanceled VerificationStatus = "canceled"
	VerificationSkipped  VerificationStatus = "skipped"
)

// VerificationCheck describes one command that determines whether the task
// actually works.
//
// A check is intentionally separate from a normal Task command. Verification
// commands may invoke compilers, test runners, linters, or other diagnostics,
// but their final purpose is to establish objective evidence about the current
// workspace state.
type VerificationCheck struct {
	Name         string  `json:"name"`
	Command      Command `json:"command"`
	Required     bool    `json:"required"`
	AllowFailure bool    `json:"allowFailure,omitempty"`
}

// VerificationResult records the result of one verification check.
type VerificationResult struct {
	Name       string             `json:"name"`
	Status     VerificationStatus `json:"status"`
	Result     CommandResult      `json:"result"`
	Error      string             `json:"error,omitempty"`
	StartedAt  time.Time          `json:"startedAt"`
	FinishedAt time.Time          `json:"finishedAt"`
}

// VerificationSummary is the aggregate judgment for a verification run.
type VerificationSummary struct {
	Passed         bool                 `json:"passed"`
	RequiredTotal  int                  `json:"requiredTotal"`
	RequiredPassed int                  `json:"requiredPassed"`
	RequiredFailed int                  `json:"requiredFailed"`
	OptionalTotal  int                  `json:"optionalTotal"`
	OptionalPassed int                  `json:"optionalPassed"`
	OptionalFailed int                  `json:"optionalFailed"`
	Results        []VerificationResult `json:"results"`
	Duration       time.Duration        `json:"duration"`
	StartedAt      time.Time            `json:"startedAt"`
	FinishedAt     time.Time            `json:"finishedAt"`
	Error          string               `json:"error,omitempty"`
}

// Verifier executes objective checks against a running task.
type Verifier struct {
	Tasks *TaskManager
}

// NewVerifier creates a verifier backed by a TaskManager.
func NewVerifier(tasks *TaskManager) (*Verifier, error) {
	if tasks == nil {
		return nil, errors.New("lab: task manager is nil")
	}

	return &Verifier{
		Tasks: tasks,
	}, nil
}

// Verify executes all configured checks and records the resulting verification
// state on the task.
//
// Verification commands are independently policy-checked and must be
// meaningful. A trivial command such as "echo ok", "true", or "exit 0" cannot
// provide objective evidence of the workspace state.
//
// The task is considered verified only after the complete verification run has
// finished and the resulting summary has been recorded through
// TaskManager.RecordVerification().
func (v *Verifier) Verify(
	ctx context.Context,
	task *Task,
	checks []VerificationCheck,
) (VerificationSummary, error) {
	started := time.Now().UTC()

	summary := VerificationSummary{
		Results:   make([]VerificationResult, 0, len(checks)),
		StartedAt: started,
	}

	if v == nil || v.Tasks == nil {
		return finishVerification(
			summary,
			errors.New("lab: verifier is not configured"),
		)
	}

	if task == nil {
		return finishVerification(summary, ErrTaskInvalid)
	}

	if task.Status != TaskRunning {
		err := fmt.Errorf(
			"%w: current status=%s",
			ErrTaskNotRunnable,
			task.Status,
		)

		return finishVerification(summary, err)
	}

	if len(checks) == 0 {
		return finishVerification(
			summary,
			ErrVerificationEmpty,
		)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	for index, check := range checks {
		if err := ctx.Err(); err != nil {
			summary.Error = ErrVerificationCanceled.Error()
			summary.Passed = false

			finished := time.Now().UTC()
			summary.FinishedAt = finished
			summary.Duration = finished.Sub(started)

			_ = v.Tasks.RecordVerification(task, summary)

			return summary, ErrVerificationCanceled
		}

		normalized, err := normalizeCheck(check, index)

		if err != nil {
			finished := time.Now().UTC()

			verificationResult := VerificationResult{
				Name:       check.Name,
				Status:     VerificationFailed,
				Error:      err.Error(),
				StartedAt:  finished,
				FinishedAt: finished,
			}

			summary.Results = append(
				summary.Results,
				verificationResult,
			)

			if check.Required && !check.AllowFailure {
				summary.RequiredTotal++
				summary.RequiredFailed++
			} else {
				summary.OptionalTotal++
				summary.OptionalFailed++
			}

			summary.Passed = false
			summary.Error = ErrVerificationFailed.Error()
			summary.FinishedAt = finished
			summary.Duration = finished.Sub(started)

			recordErr := v.Tasks.RecordVerification(
				task,
				summary,
			)

			if recordErr != nil {
				return summary, errors.Join(
					ErrVerificationFailed,
					err,
					recordErr,
				)
			}

			return summary, errors.Join(
				ErrVerificationFailed,
				err,
			)
		}

		if normalized.Required {
			summary.RequiredTotal++
		} else {
			summary.OptionalTotal++
		}

		checkStarted := time.Now().UTC()

		if err := v.Tasks.Policy.EvaluateForWorkspace(
			normalized.Command.Command,
			normalized.Command.WorkingDir,
		); err != nil {
			checkFinished := time.Now().UTC()

			result := VerificationResult{
				Name:       normalized.Name,
				Status:     VerificationFailed,
				Error:      err.Error(),
				StartedAt:  checkStarted,
				FinishedAt: checkFinished,
			}

			summary.Results = append(
				summary.Results,
				result,
			)

			if normalized.Required && !normalized.AllowFailure {
				summary.RequiredFailed++
			} else {
				summary.OptionalFailed++
			}

			if normalized.Required && !normalized.AllowFailure {
				summary.Passed = false
				summary.Error = ErrVerificationFailed.Error()
				summary.FinishedAt = checkFinished
				summary.Duration = checkFinished.Sub(started)

				recordErr := v.Tasks.RecordVerification(
					task,
					summary,
				)

				if recordErr != nil {
					return summary, errors.Join(
						ErrVerificationFailed,
						err,
						recordErr,
					)
				}

				return summary, errors.Join(
					ErrVerificationFailed,
					err,
				)
			}

			continue
		}

		commandResult, commandErr := v.Tasks.RunCommand(
			ctx,
			task,
			normalized.Command,
		)

		checkFinished := time.Now().UTC()

		verificationResult := VerificationResult{
			Name:       normalized.Name,
			Result:     commandResult,
			StartedAt:  checkStarted,
			FinishedAt: checkFinished,
		}

		switch {
		case commandErr == nil && commandResult.Success:
			verificationResult.Status = VerificationPassed

			if normalized.Required {
				summary.RequiredPassed++
			} else {
				summary.OptionalPassed++
			}

		case errors.Is(commandErr, context.Canceled):
			verificationResult.Status = VerificationCanceled
			verificationResult.Error = commandErrorString(commandErr)

			if normalized.Required {
				summary.RequiredFailed++
			} else {
				summary.OptionalFailed++
			}

			summary.Results = append(
				summary.Results,
				verificationResult,
			)

			summary.Passed = false
			summary.Error = ErrVerificationCanceled.Error()
			summary.FinishedAt = checkFinished
			summary.Duration = checkFinished.Sub(started)

			_ = v.Tasks.RecordVerification(task, summary)

			return summary, ErrVerificationCanceled

		default:
			verificationResult.Status = VerificationFailed
			verificationResult.Error = commandErrorString(commandErr)

			if normalized.Required && !normalized.AllowFailure {
				summary.RequiredFailed++
			} else {
				summary.OptionalFailed++
			}
		}

		summary.Results = append(
			summary.Results,
			verificationResult,
		)

		if verificationResult.Status == VerificationFailed &&
			normalized.Required &&
			!normalized.AllowFailure {
			summary.Passed = false
			summary.Error = ErrVerificationFailed.Error()
			summary.FinishedAt = checkFinished
			summary.Duration = checkFinished.Sub(started)

			recordErr := v.Tasks.RecordVerification(
				task,
				summary,
			)

			if recordErr != nil {
				return summary, errors.Join(
					ErrVerificationFailed,
					recordErr,
				)
			}

			return summary, ErrVerificationFailed
		}
	}

	finished := time.Now().UTC()

	summary.FinishedAt = finished
	summary.Duration = finished.Sub(started)

	summary.Passed =
		summary.RequiredTotal > 0 &&
			summary.RequiredFailed == 0 &&
			summary.RequiredPassed == summary.RequiredTotal

	if !summary.Passed {
		summary.Error = ErrVerificationFailed.Error()
	}

	recordErr := v.Tasks.RecordVerification(
		task,
		summary,
	)

	if recordErr != nil {
		if summary.Passed {
			summary.Passed = false
			summary.Error = ErrVerificationFailed.Error()
		}

		return summary, recordErr
	}

	if !summary.Passed {
		return summary, ErrVerificationFailed
	}

	return summary, nil
}

// VerifyStandard provides project-aware verification.
//
// Explicit build/test commands are used when supplied. When either is empty,
// the verifier discovers appropriate native checks from the workspace.
//
// This means an empty caller configuration cannot silently become a successful
// verification.
func (v *Verifier) VerifyStandard(
	ctx context.Context,
	task *Task,
	buildCommand string,
	testCommand string,
) (VerificationSummary, error) {
	if task == nil ||
		task.Workspace == nil ||
		strings.TrimSpace(task.Workspace.Path) == "" {
		return VerificationSummary{}, ErrInvalidWorkspace
	}

	checks, err := standardChecks(
		task.Workspace.Path,
		buildCommand,
		testCommand,
	)

	if err != nil {
		return VerificationSummary{}, err
	}

	return v.Verify(
		ctx,
		task,
		checks,
	)
}

// VerifyNative performs independent native project verification.
//
// It deliberately ignores caller-provided verification commands and derives
// meaningful checks from the workspace itself. This is the stronger path used
// by autonomous repair/verification.
func (v *Verifier) VerifyNative(
	ctx context.Context,
	task *Task,
) (VerificationSummary, error) {
	if task == nil ||
		task.Workspace == nil ||
		strings.TrimSpace(task.Workspace.Path) == "" {
		return VerificationSummary{}, ErrInvalidWorkspace
	}

	checks, err := discoverNativeChecks(
		task.Workspace.Path,
	)

	if err != nil {
		return VerificationSummary{}, err
	}

	return v.Verify(
		ctx,
		task,
		checks,
	)
}

// VerifyCommands verifies an arbitrary ordered list of commands.
//
// Every supplied command is treated as a required verification check.
func (v *Verifier) VerifyCommands(
	ctx context.Context,
	task *Task,
	commands []string,
) (VerificationSummary, error) {
	checks := make(
		[]VerificationCheck,
		0,
		len(commands),
	)

	for index, command := range commands {
		command = strings.TrimSpace(command)

		if command == "" {
			continue
		}

		checks = append(
			checks,
			VerificationCheck{
				Name:     fmt.Sprintf("check-%d", index+1),
				Required: true,
				Command: Command{
					Command: command,
				},
			},
		)
	}

	if len(checks) == 0 {
		return VerificationSummary{}, ErrVerificationEmpty
	}

	return v.Verify(
		ctx,
		task,
		checks,
	)
}

// standardChecks builds required checks from explicit commands and/or the
// project detected in workspaceRoot.
func standardChecks(
	workspaceRoot string,
	buildCommand string,
	testCommand string,
) ([]VerificationCheck, error) {
	buildCommand = strings.TrimSpace(buildCommand)
	testCommand = strings.TrimSpace(testCommand)

	if buildCommand != "" {
		if err := validateVerificationCommand(buildCommand); err != nil {
			return nil, err
		}
	}

	if testCommand != "" {
		if err := validateVerificationCommand(testCommand); err != nil {
			return nil, err
		}
	}

	checks := make(
		[]VerificationCheck,
		0,
		4,
	)

	if buildCommand != "" {
		checks = append(
			checks,
			VerificationCheck{
				Name:     "build",
				Required: true,
				Command: Command{
					Command: buildCommand,
				},
			},
		)
	}

	if testCommand != "" {
		checks = append(
			checks,
			VerificationCheck{
				Name:     "tests",
				Required: true,
				Command: Command{
					Command: testCommand,
				},
			},
		)
	}

	if len(checks) == 0 {
		discovered, err := discoverNativeChecks(
			workspaceRoot,
		)

		if err != nil {
			return nil, err
		}

		checks = append(
			checks,
			discovered...,
		)
	}

	if len(checks) == 0 {
		return nil, ErrVerificationEmpty
	}

	return checks, nil
}

// discoverNativeChecks identifies meaningful project verification commands
// directly from the workspace.
//
// At most one build check and one test check are generated for each detected
// project family, avoiding a combinatorial explosion while still providing
// objective evidence.
func discoverNativeChecks(
	workspaceRoot string,
) ([]VerificationCheck, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)

	if workspaceRoot == "" {
		return nil, ErrInvalidWorkspace
	}

	info, err := os.Stat(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf(
			"lab: inspect verification workspace: %w",
			err,
		)
	}

	if !info.IsDir() {
		return nil, ErrInvalidWorkspace
	}

	checks := make(
		[]VerificationCheck,
		0,
		4,
	)

	// Go project.
	if fileExists(filepath.Join(workspaceRoot, "go.mod")) {
		checks = append(
			checks,
			VerificationCheck{
				Name:     "go-build",
				Required: true,
				Command: Command{
					Command: goBuildCommand(),
				},
			},
		)

		checks = append(
			checks,
			VerificationCheck{
				Name:     "go-test",
				Required: true,
				Command: Command{
					Command: goTestCommand(),
				},
			},
		)
	}

	// Node/npm project.
	if fileExists(filepath.Join(workspaceRoot, "package.json")) {
		testCommand := ""

		if fileExists(filepath.Join(workspaceRoot, "package-lock.json")) ||
			fileExists(filepath.Join(workspaceRoot, "npm-shrinkwrap.json")) {
			testCommand = "npm test -- --runInBand"
		} else {
			testCommand = "npm test"
		}

		checks = append(
			checks,
			VerificationCheck{
				Name:     "node-test",
				Required: true,
				Command: Command{
					Command: testCommand,
				},
			},
		)

		// A package.json with a build script is independently verified by npm.
		if packageHasScript(workspaceRoot, "build") {
			checks = append(
				checks,
				VerificationCheck{
					Name:     "node-build",
					Required: true,
					Command: Command{
						Command: "npm run build",
					},
				},
			)
		}
	}

	// Python project.
	if fileExists(filepath.Join(workspaceRoot, "pyproject.toml")) ||
		fileExists(filepath.Join(workspaceRoot, "setup.py")) ||
		fileExists(filepath.Join(workspaceRoot, "pytest.ini")) {
		python := "python3"

		if runtime.GOOS == "windows" {
			python = "python"
		}

		checks = append(
			checks,
			VerificationCheck{
				Name:     "python-compile",
				Required: true,
				Command: Command{
					Command: fmt.Sprintf(
						"%s -m compileall -q .",
						python,
					),
				},
			},
		)

		if fileExists(filepath.Join(workspaceRoot, "pytest.ini")) ||
			fileExists(filepath.Join(workspaceRoot, "tests")) {
			checks = append(
				checks,
				VerificationCheck{
					Name:     "python-test",
					Required: true,
					Command: Command{
						Command: fmt.Sprintf(
							"%s -m pytest -q",
							python,
						),
					},
				},
			)
		}
	}

	// Rust project.
	if fileExists(filepath.Join(workspaceRoot, "Cargo.toml")) {
		checks = append(
			checks,
			VerificationCheck{
				Name:     "rust-build",
				Required: true,
				Command: Command{
					Command: "cargo check",
				},
			},
		)

		checks = append(
			checks,
			VerificationCheck{
				Name:     "rust-test",
				Required: true,
				Command: Command{
					Command: "cargo test --quiet",
				},
			},
		)
	}

	if len(checks) == 0 {
		return nil, fmt.Errorf(
			"%w: no recognized project build/test configuration",
			ErrVerificationEmpty,
		)
	}

	return checks, nil
}

func goBuildCommand() string {
	return "go build ./..."
}

func goTestCommand() string {
	return "go test ./..."
}

func validateVerificationCommand(command string) error {
	command = strings.TrimSpace(command)

	if command == "" {
		return ErrCommandEmpty
	}

	if len(command) > maxVerificationCommandLength {
		return ErrCommandTooLong
	}

	tokens, err := tokenizeCommand(command)
	if err != nil {
		return fmt.Errorf(
			"%w: %v",
			ErrVerificationTrivial,
			err,
		)
	}

	words := commandWords(tokens)

	if len(words) == 0 {
		return ErrVerificationTrivial
	}

	meaningful := false

	for _, commandWords := range words {
		if len(commandWords) == 0 {
			continue
		}

		name := filepath.Base(
			normalizedToken(commandWords[0]),
		)

		switch name {
		case "echo",
			"printf",
			"true",
			"false",
			"exit":
			// These are not objective project verification commands.
			continue
		}

		if name == "cmd.exe" &&
			len(commandWords) >= 3 &&
			normalizedToken(commandWords[1]) == "/c" &&
			isTrivialCommandWords(commandWords[2:]) {
			continue
		}

		if name == "sh" ||
			name == "bash" {
			inner := commandWords[1:]

			if len(inner) > 0 &&
				normalizedToken(inner[0]) == "-c" &&
				len(inner) > 1 {
				innerCommand := strings.Join(
					inner[1:],
					" ",
				)

				if isTrivialVerificationString(
					innerCommand,
				) {
					continue
				}
			}
		}

		meaningful = true
	}

	if !meaningful {
		return fmt.Errorf(
			"%w: %q",
			ErrVerificationTrivial,
			command,
		)
	}

	return nil
}

func isTrivialCommandWords(words []string) bool {
	if len(words) == 0 {
		return true
	}

	return isTrivialVerificationString(
		strings.Join(words, " "),
	)
}

func isTrivialVerificationString(command string) bool {
	tokens, err := tokenizeCommand(
		strings.TrimSpace(command),
	)

	if err != nil {
		return false
	}

	words := commandWords(tokens)

	if len(words) != 1 ||
		len(words[0]) == 0 {
		return false
	}

	name := filepath.Base(
		normalizedToken(words[0][0]),
	)

	switch name {
	case "true",
		"false",
		"echo",
		"printf",
		"exit":
		return true
	}

	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info != nil
}

// packageHasScript checks package.json without importing a full JSON schema.
// Verification discovery only needs the existence of a scripts.<name> entry.
func packageHasScript(
	workspaceRoot string,
	script string,
) bool {
	path := filepath.Join(
		workspaceRoot,
		"package.json",
	)

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	needle := fmt.Sprintf(
		`"%s"`,
		script,
	)

	text := string(data)

	// Keep this deliberately conservative. A false negative merely omits an
	// optional native build check; a false positive could make verification
	// fail on projects without that script.
	scriptsIndex := strings.Index(
		text,
		`"scripts"`,
	)

	if scriptsIndex < 0 {
		return false
	}

	rest := text[scriptsIndex:]

	return strings.Contains(
		rest,
		needle,
	)
}

func normalizeCheck(
	check VerificationCheck,
	index int,
) (VerificationCheck, error) {
	name := strings.TrimSpace(check.Name)

	if name == "" {
		name = fmt.Sprintf(
			"check-%d",
			index+1,
		)
	}

	command := strings.TrimSpace(
		check.Command.Command,
	)

	if command == "" {
		return VerificationCheck{}, ErrCommandEmpty
	}

	if err := validateVerificationCommand(
		command,
	); err != nil {
		return VerificationCheck{}, err
	}

	check.Name = name
	check.Command.Command = command

	return check, nil
}

func commandErrorString(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

func finishVerification(
	summary VerificationSummary,
	err error,
) (VerificationSummary, error) {
	finished := time.Now().UTC()

	summary.FinishedAt = finished
	summary.Duration = finished.Sub(
		summary.StartedAt,
	)
	summary.Passed = false

	if err != nil {
		summary.Error = err.Error()
	}

	return summary, err
}
