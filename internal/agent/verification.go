// verification.go — v1.1.5 Phase 6: verification as a first-class system
// at the ORCHESTRATOR level.
//
// The Coding Lab already owns OBJECTIVE verification (policy-checked
// commands, build/test evidence, VerificationSummary). What was missing
// is a contract at the agent boundary: when the model claims a task is
// complete, the run's own record must distinguish
//
//	verified / partially_verified / failed / not_verified
//
// ...because "the model said so" is not proof. This file defines that
// contract and the EvidenceCollector that implements it by watching the
// tool traffic of a run. Verification evidence arrives through two
// channels today, both of which the collector recognizes:
//
//  1. The Coding Lab verify action — its result text carries the
//     verification summary (checks passed/failed, task verdict).
//  2. Explicit build/test evidence — a run/build/test command whose
//     output contains recognizable success or failure markers.
//
// The collector never fabricates a pass: unknown tools and ambiguous
// output contribute nothing, and a final "completed" turn without any
// recorded evidence reports not_verified so the UI (and the multi-agent
// critic) can demand verification instead of celebrating.
package agent

import (
	"fmt"
	"strings"
	"sync"
)

// VerificationOutcome is the run-level verification verdict.
type VerificationOutcome string

const (
	// VerificationVerified: at least one verification check recorded and
	// every recorded check passed.
	VerificationVerified VerificationOutcome = "verified"
	// VerificationPartial: checks recorded, at least one passed, at
	// least one failed.
	VerificationPartial VerificationOutcome = "partially_verified"
	// VerificationFailed: checks recorded and ALL of them failed.
	VerificationFailed VerificationOutcome = "failed"
	// VerificationNone: no verification evidence was recorded for this
	// run — the completion claim is unproven.
	VerificationNone VerificationOutcome = "not_verified"
)

// EvidenceKind names the class of objective evidence observed.
type EvidenceKind string

const (
	EvidenceLabVerify EvidenceKind = "lab_verify"
	EvidenceBuild     EvidenceKind = "build"
	EvidenceTest      EvidenceKind = "test"
	EvidenceFileState EvidenceKind = "file_state"
	EvidenceGitState  EvidenceKind = "git_state"
	EvidenceCommand   EvidenceKind = "command"
)

// Evidence is one objective observation recorded during a run.
type Evidence struct {
	Kind   EvidenceKind `json:"kind"`
	Source string       `json:"source"`
	Passed bool         `json:"passed"`
	Detail string       `json:"detail,omitempty"`
}

// VerificationReport is the run-level verification record carried on
// RunResult and surfaced to the UI/multi-agent critic.
type VerificationReport struct {
	Outcome  VerificationOutcome `json:"outcome"`
	Evidence []Evidence          `json:"evidence,omitempty"`
}

// Summary renders a one-line human-readable verdict for the timeline.
func (r VerificationReport) Summary() string {
	switch r.Outcome {
	case VerificationVerified:
		return fmt.Sprintf("Verified — %d objective check(s) passed", len(r.Evidence))
	case VerificationPartial:
		passed := 0
		for _, e := range r.Evidence {
			if e.Passed {
				passed++
			}
		}
		return fmt.Sprintf("Partially verified — %d/%d checks passed", passed, len(r.Evidence))
	case VerificationFailed:
		return fmt.Sprintf("Verification FAILED — %d check(s) recorded, none passed", len(r.Evidence))
	default:
		return "Not verified — no objective evidence recorded for the completion claim"
	}
}

// EvidenceCollector watches tool traffic and accumulates verification
// evidence. It is deliberately conservative: only recognizable, explicit
// evidence is recorded (a passing lab verify, an explicit build/test
// success marker, an explicit test failure). It is safe for concurrent
// use.
type EvidenceCollector struct {
	mu       sync.Mutex
	evidence []Evidence
}

// NewEvidenceCollector returns an empty collector.
func NewEvidenceCollector() *EvidenceCollector {
	return &EvidenceCollector{}
}

// Lab verification result markers (internal/lab tool result text).
const (
	labVerifyTaskPassed = "task verified"
	labVerifyPassed     = "passed"
	labVerifyFailed     = "failed"
)

// ObserveToolResult inspects one completed tool result for verification
// evidence. Only the Coding Lab verify action and explicit build/test
// commands contribute; everything else is ignored.
func (c *EvidenceCollector) ObserveToolResult(toolName, args, result string, failed bool) {
	if c == nil {
		return
	}

	lower := strings.ToLower(result)
	kind, pass, ok := classifyToolEvidence(toolName, args, lower, failed)
	if !ok {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.evidence = append(c.evidence, Evidence{
		Kind:   kind,
		Source: toolName,
		Passed: pass,
		Detail: firstLine(result),
	})
}

// classifyToolEvidence maps one tool result to evidence, if any. The
// lab tool's verify action is authoritative; shell-ish tools contribute
// only when their output is an UNAMBIGUOUS build/test signal.
func classifyToolEvidence(tool, args, lowerResult string, failed bool) (EvidenceKind, bool, bool) {
	// The Coding Lab verify action: its result carries the verification
	// summary. A failed call of the verify action itself is failed
	// evidence (the verification could not even run counts as failed,
	// never as a pass).
	if tool == "coding_lab" {
		if strings.Contains(strings.ToLower(args), `"verify"`) ||
			strings.Contains(args, "verify") {
			if strings.Contains(lowerResult, labVerifyTaskPassed) ||
				(strings.Contains(lowerResult, "verification") &&
					!strings.Contains(lowerResult, labVerifyFailed)) {
				return EvidenceLabVerify, true, true
			}
			return EvidenceLabVerify, false, true
		}
		return "", false, false
	}

	// Shell-like tools with explicit build/test commands.
	switch tool {
	case "shell", "code_exec", "terminal", "git":
	default:
		return "", false, false
	}

	lowerArgs := strings.ToLower(args)

	isBuild := containsAny(lowerArgs, "go build", "make ", "cmake --build",
		"npm run build", "cargo build", "gcc ", "g++ ", "dotnet build")
	isTest := containsAny(lowerArgs, "go test", "npm test", "pytest",
		"cargo test", "ctest", "dotnet test", "make test")
	isGitState := tool == "git" && !isBuild && !isTest

	if !isBuild && !isTest && !isGitState {
		return "", false, false
	}

	// Success markers must be explicit. A bare "exit 0" with no test/build
	// output is not evidence (echo ok tricks nothing here).
	pass := !failed &&
		(containsAny(lowerResult, "ok ", "pass", "succeed", "no issues",
			"build succeeded", "built target", "up to date", "0 vulnerabilities",
			"tests pass") ||
			!containsAny(lowerResult, "fail", "error", "fatal", "panic"))

	if containsAny(lowerResult, "fail", "error", "fatal", "panic",
		"exit status 1", "exit status 2") {
		pass = false
	}

	switch {
	case isTest:
		return EvidenceTest, pass, true
	case isBuild:
		return EvidenceBuild, pass, true
	default:
		return EvidenceGitState, pass, true
	}
}

// firstLine extracts the first non-empty line of a result for the detail
// field, clipped to a bounded length.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 160 {
				return line[:160] + "…"
			}
			return line
		}
	}
	return ""
}

// Report computes the final verification verdict from the collected
// evidence. No evidence → not_verified (the honest default).
func (c *EvidenceCollector) Report() VerificationReport {
	if c == nil {
		return VerificationReport{Outcome: VerificationNone}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.evidence) == 0 {
		return VerificationReport{Outcome: VerificationNone}
	}

	passed := 0
	for _, e := range c.evidence {
		if e.Passed {
			passed++
		}
	}

	out := VerificationReport{Outcome: VerificationVerified, Evidence: c.evidence}
	switch {
	case passed == 0:
		out.Outcome = VerificationFailed
	case passed < len(c.evidence):
		out.Outcome = VerificationPartial
	}
	return out
}
