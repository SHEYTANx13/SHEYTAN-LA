// taskstate.go — v1.2.8 Agent task memory.
//
// PROBLEM: an Agent run previously kept its working state only in the
// conversation loop itself. A WebSocket reconnect recovered the streamed
// text (runLive snapshot) but NOT the working state — what was inspected,
// what changed, which commands and tests ran, what failed and was repaired,
// what remains. The UI could show "generating…" but not WHERE the task was.
//
// DESIGN: one bounded, structured TaskState per RunDetailed, maintained
// DETERMINISTICALLY from the run's real tool traffic (never from model
// claims). It is:
//   - published live as a `task` Activity per tool round (bounded payload),
//     so the UI renders the actual pipeline (INSPECT → ACT → TEST →
//     VERIFY → …) instead of a decorative fake;
//   - folded into the authoritative runLive snapshot (runSnapshot.task),
//     so a reconnect mid-run restores the task view — the run NEVER
//     restarts because the UI reconnected;
//   - returned on RunResult.Task, where the API layer uses it for the
//     rolling session summary and the agent.md handoff at settlement.
//
// Bounds: 8 entries per list, 200 chars per entry, dedup by normalized
// prefix. TaskState is honest by construction: only observed tool calls
// and their outcomes enter it — no speculative success claims.
package agent

import (
	"encoding/json"
	"sort"
	"strings"
)

// TaskState is the bounded structured state of ONE agent run.
type TaskState struct {
	Goal string `json:"goal,omitempty"`

	Constraints    []string `json:"constraints,omitempty"`
	CurrentStep    string   `json:"currentStep,omitempty"`
	Plan           []string `json:"plan,omitempty"`
	FilesInspected []string `json:"filesInspected,omitempty"`
	FilesChanged   []string `json:"filesChanged,omitempty"`
	ToolsUsed      []string `json:"toolsUsed,omitempty"`
	CommandsRun    []string `json:"commandsRun,omitempty"`
	TestsRun       []string `json:"testsRun,omitempty"`
	Failures       []string `json:"failures,omitempty"`
	Repairs        []string `json:"repairs,omitempty"`
	Verification   string   `json:"verification,omitempty"`
	Artifacts      []string `json:"artifacts,omitempty"`
	OpenQuestions  []string `json:"openQuestions,omitempty"`
	NextStep       string   `json:"nextStep,omitempty"`
}

// taskStateBounds mirror the session summary bounds — a task state is a
// context component, not a log.
const (
	taskMaxItems     = 8
	taskMaxItemChars = 200
	taskMaxGoal      = 300
	taskMaxStep      = 200
)

// NewTaskState seeds the state from the run's goal (the user request).
func NewTaskState(goal string) *TaskState {
	ts := &TaskState{Goal: clipTask(goal, taskMaxGoal)}
	ts.Constraints = extractConstraints(goal)
	return ts
}

// ObserveToolStart records an execution attempt (INSPECT/ACT signals).
func (t *TaskState) ObserveToolStart(name, args string) {
	if t == nil {
		return
	}
	t.ToolsUsed = appendTaskItem(t.ToolsUsed, name)
	switch name {
	case "files":
		action := taskJSONField(args, "action")
		path := taskJSONField(args, "path")
		if action == "read" && path != "" {
			t.FilesInspected = appendTaskItem(t.FilesInspected, path)
			t.CurrentStep = "INSPECT: reading " + clipTask(path, taskMaxStep)
		}
	case "shell", "git", "lab":
		cmd := taskJSONField(args, "command")
		if cmd == "" {
			cmd = clipTask(args, 80)
		}
		if isTestCommand(cmd) {
			t.CurrentStep = "TEST: " + clipTask(cmd, taskMaxStep)
		} else {
			t.CurrentStep = "ACT: " + clipTask(cmd, taskMaxStep)
		}
	}
}

// ObserveToolEnd folds one settled tool call (any exit path — refusal,
// cache hit, error or success) into the state.
func (t *TaskState) ObserveToolEnd(name, args, result string, failed bool) {
	if t == nil {
		return
	}

	switch name {
	case "files":
		action := taskJSONField(args, "action")
		path := taskJSONField(args, "path")
		if path != "" && (action == "write" || action == "append" || action == "delete" || action == "patch" || action == "edit") {
			t.FilesChanged = appendTaskItem(t.FilesChanged, path)
			t.Artifacts = appendTaskItem(t.Artifacts, path)
			t.CurrentStep = "ACT: edited " + clipTask(path, taskMaxStep)
		}
	case "shell":
		cmd := taskJSONField(args, "command")
		if cmd != "" {
			t.CommandsRun = appendTaskItem(t.CommandsRun, cmd)
			if isTestCommand(cmd) {
				t.TestsRun = appendTaskItem(t.TestsRun, cmd)
				if !failed && testOutputPassed(result) {
					t.CurrentStep = "TEST: passed — " + clipTask(cmd, taskMaxStep)
				} else if failed {
					t.CurrentStep = "TEST: failed — " + clipTask(cmd, taskMaxStep)
				}
			}
		}
	case "diff":
		if !failed {
			t.Artifacts = appendTaskItem(t.Artifacts, "diff")
		}
	}

	if failed {
		t.Failures = appendTaskItem(t.Failures, name+": "+clipTask(firstTaskLine(result), taskMaxItemChars))
	} else if len(t.Failures) > 0 && !strings.HasPrefix(result, "Error:") {
		// A success FOLLOWING a recorded failure is the repair evidence —
		// the same tool name succeeding after a failure of the same tool.
		lastFail := t.Failures[len(t.Failures)-1]
		if strings.HasPrefix(lastFail, name+":") {
			t.Repairs = appendTaskItem(t.Repairs, name+" recovered after failure")
		}
	}
}

// SetVerification records the run's objective verdict (from the evidence
// collector, never from the model's own claims).
func (t *TaskState) SetVerification(outcome, summary string) {
	if t == nil {
		return
	}
	t.Verification = clipTask(outcome+" — "+summary, taskMaxItemChars)
}

// SetPlan records the working plan (extracted from the model's own
// declared plan when the run surfaces one; bounded).
func (t *TaskState) SetPlan(steps []string) {
	if t == nil {
		return
	}
	for _, s := range steps {
		t.Plan = appendTaskItem(t.Plan, s)
	}
}

// SetNextStep records the declared next action after a tool round.
func (t *TaskState) SetNextStep(step string) {
	if t == nil {
		return
	}
	t.NextStep = clipTask(step, taskMaxStep)
}

// snapshot returns a read-isolated, bounded copy for publication.
func (t *TaskState) snapshot() TaskState {
	if t == nil {
		return TaskState{}
	}
	out := *t
	out.Goal = clipTask(t.Goal, taskMaxGoal)
	out.CurrentStep = clipTask(t.CurrentStep, taskMaxStep)
	out.NextStep = clipTask(t.NextStep, taskMaxStep)
	out.Verification = clipTask(t.Verification, taskMaxItemChars)
	out.Constraints = boundTaskList(t.Constraints)
	out.Plan = boundTaskList(t.Plan)
	out.FilesInspected = boundTaskList(t.FilesInspected)
	out.FilesChanged = boundTaskList(t.FilesChanged)
	out.ToolsUsed = boundTaskList(t.ToolsUsed)
	out.CommandsRun = boundTaskList(t.CommandsRun)
	out.TestsRun = boundTaskList(t.TestsRun)
	out.Failures = boundTaskList(t.Failures)
	out.Repairs = boundTaskList(t.Repairs)
	out.Artifacts = boundTaskList(t.Artifacts)
	out.OpenQuestions = boundTaskList(t.OpenQuestions)
	return out
}

// HasEngineeringEvidence reports whether the run actually touched
// engineering surfaces (files, commands or tests) — the gate for writing
// the agent.md handoff at settlement.
func (t *TaskState) HasEngineeringEvidence() bool {
	if t == nil {
		return false
	}
	return len(t.FilesChanged) > 0 || len(t.CommandsRun) > 0 || len(t.TestsRun) > 0
}

func boundTaskList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, item := range in {
		key := dedupTaskKey(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, clipTask(item, taskMaxItemChars))
		if len(out) >= taskMaxItems {
			break
		}
	}
	return out
}

func appendTaskItem(list []string, item string) []string {
	item = strings.TrimSpace(item)
	if item == "" {
		return list
	}
	key := dedupTaskKey(item)
	for _, existing := range list {
		if dedupTaskKey(existing) == key {
			return list
		}
	}
	list = append(list, clipTask(item, taskMaxItemChars))
	if len(list) > taskMaxItems {
		list = list[len(list)-taskMaxItems:]
	}
	return list
}

func dedupTaskKey(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func clipTask(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

func firstTaskLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		return s[:i]
	}
	return s
}

// taskJSONField extracts one top-level string field from a tool-args JSON
// blob without importing an unmarshal path into the hot loop (tools parse
// their own args; this is state bookkeeping only).
func taskJSONField(args, field string) string {
	if args == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	v, ok := m[field].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// isTestCommand recognizes the explicit build/test invocations the
// verification loop cares about (objective evidence, substring-precise).
func isTestCommand(cmd string) bool {
	cmd = strings.ToLower(cmd)
	markers := []string{
		"go test", "go build", "go vet", "gofmt",
		"npm test", "npm run test", "npm run build", "npx tsc",
		"pytest", "cargo test", "cargo build", "make test", "make check",
		"mvn test", "gradle test", "dotnet test", "tsc --", "tsc -",
	}
	for _, m := range markers {
		if strings.Contains(cmd, m) {
			return true
		}
	}
	return false
}

// testOutputPassed scans bounded test output for explicit pass markers
// (never "the model said it passed").
func testOutputPassed(out string) bool {
	out = strings.ToLower(out)
	markers := []string{
		"ok  ", "all tests passed", "tests passed", "build succeeded",
		"0 failures", "success:", "compiled successfully",
	}
	for _, m := range markers {
		if strings.Contains(out, m) {
			return true
		}
	}
	return false
}

// sortedCopy is used by tests to compare list contents order-insensitively.
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// constraintMarkers — the vocabulary for user constraints (kept local so
// the agent package stays decoupled from the sessions summary layer).
var taskConstraintMarkers = []string{
	"must ", "mustn't", "must not", "only use", "never ", "do not ",
	"don't ", "always ", "require", "constraint", "has to ", "have to ",
	"make sure", "no more than", "at most", "at least", "do not touch",
	"without changing", "keep the",
}

// extractConstraints pulls the user's stated constraints out of the
// request text — bounded, deterministic, and only from what the user
// ACTUALLY wrote (never inferred rules).
func extractConstraints(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []string
	for _, sentence := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '\n'
	}) {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		lower := strings.ToLower(sentence)
		for _, m := range taskConstraintMarkers {
			if strings.Contains(lower, m) {
				out = appendTaskItem(out, sentence)
				break
			}
		}
	}
	return out
}
