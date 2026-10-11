// snapshot.go — the complete logical work state of one exhausted run.
//
// v1.7.1 §3.3: the recovery snapshot preserves the COMPLETE logical state,
// not the last few messages. Every field maps to a real authority:
//
//	Mission / Constraints / Facts / Decisions / OpenThreads
//	    ← the user's request + continuum.Distill over the run messages
//	FilesInspected / FilesChanged / ToolCalls / TestsAndBuilds /
//	Failures / Repairs / Artifacts / CurrentState / NextActions
//	    ← the agent.TaskState of the run (observed tool traffic)
//	Artifacts (registered) ← the artifacts.TaskRegistry provenance
//	TaskTools / LinkedSkills ← the scheduler Task definition
//	SourceBackend / SourceModel / Configuration ← the run config snapshot
//
// Bounds: every list and string is bounded so the snapshot — even for a
// huge exhausted context — stays small, JSON-serializable and
// prompt-injectable. The FULL conversation lives on in the durable
// session files; the snapshot carries the compact structured state plus
// the neutral message list for summarization.
package recovery

import (
	"time"
)

// ToolRecord is one observed tool execution with its outcome.
type ToolRecord struct {
	Name    string `json:"name"`
	Args    string `json:"args,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	Failed  bool   `json:"failed,omitempty"`
}

// Message is the neutral message representation used by the snapshot and
// the summarizer. Deliberately decoupled from llm.Message so the llm
// package can depend on recovery without an import cycle.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Snapshot is the complete frozen logical state of one exhausted run.
type Snapshot struct {
	// Identity: which logical work is being recovered.
	SessionID string `json:"sessionId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	TaskID    string `json:"taskId,omitempty"` // scheduler task (task runs)
	RunID     string `json:"runId,omitempty"`  // API run identity

	// Objective and state.
	Mission      string   `json:"mission,omitempty"`
	CurrentState string   `json:"currentState,omitempty"`
	Constraints  []string `json:"constraints,omitempty"`

	// Distilled knowledge (continuum authority over the run messages).
	Facts       []string `json:"facts,omitempty"`
	Decisions   []string `json:"decisions,omitempty"`
	OpenThreads []string `json:"openThreads,omitempty"`
	Preferences []string `json:"preferences,omitempty"`

	// Observed execution state (TaskState authority).
	FilesInspected []string     `json:"filesInspected,omitempty"`
	FilesChanged   []string     `json:"filesChanged,omitempty"`
	ToolCalls      []ToolRecord `json:"toolCalls,omitempty"`
	TestsAndBuilds []string     `json:"testsAndBuilds,omitempty"`
	Failures       []string     `json:"failures,omitempty"`
	Repairs        []string     `json:"repairs,omitempty"`
	Artifacts      []string     `json:"artifacts,omitempty"`
	NextActions    []string     `json:"nextActions,omitempty"`

	// Task scoping (scheduler task metadata).
	TaskTools    []string `json:"taskTools,omitempty"`
	LinkedSkills []string `json:"linkedSkills,omitempty"`

	// Environment of the exhausted attempt.
	SourceBackend string            `json:"sourceBackend,omitempty"`
	SourceModel   string            `json:"sourceModel,omitempty"`
	Configuration map[string]string `json:"configuration,omitempty"`

	// The complete logical conversation of the attempt (neutral form).
	// This is what the hierarchical summarizer reads; it is NOT injected
	// back into the continuation prompt.
	Messages []Message `json:"messages,omitempty"`

	// Detection evidence.
	Detected Exhaustion `json:"detected"`
	// Attempt is the 1-based attempt number that exhausted.
	Attempt int `json:"attempt"`
	// CreatedAt stamps the freeze.
	CreatedAt time.Time `json:"createdAt"`

	// EstimatedTokens is the token estimate of Messages, filled by Bound.
	// It records how large the exhausted logical context was.
	EstimatedTokens int `json:"estimatedTokens,omitempty"`
}

// Snapshot bounds — a snapshot is a context component, not a log.
const (
	maxSnapItems     = 12
	maxSnapItemChars = 200
	maxMissionChars  = 300
	maxStateChars    = 300
)

// NewSnapshot freezes a snapshot at time.Now with the detection evidence.
func NewSnapshot(sessionID, threadID, taskID, runID string, attempt int, detected Exhaustion) *Snapshot {
	if attempt < 1 {
		attempt = 1
	}
	return &Snapshot{
		SessionID: sessionID,
		ThreadID:  threadID,
		TaskID:    taskID,
		RunID:     runID,
		Attempt:   attempt,
		Detected:  detected,
		CreatedAt: time.Now().UTC(),
	}
}

// Bound applies the snapshot bounds in place: lists are deduped and
// capped, strings are clipped. Idempotent.
func (s *Snapshot) Bound() {
	if s == nil {
		return
	}
	s.Mission = clip(s.Mission, maxMissionChars)
	s.CurrentState = clip(s.CurrentState, maxStateChars)
	s.Constraints = boundList(s.Constraints)
	s.Facts = boundList(s.Facts)
	s.Decisions = boundList(s.Decisions)
	s.OpenThreads = boundList(s.OpenThreads)
	s.Preferences = boundList(s.Preferences)
	s.FilesInspected = boundList(s.FilesInspected)
	s.FilesChanged = boundList(s.FilesChanged)
	s.TestsAndBuilds = boundList(s.TestsAndBuilds)
	s.Failures = boundList(s.Failures)
	s.Repairs = boundList(s.Repairs)
	s.Artifacts = boundList(s.Artifacts)
	s.NextActions = boundList(s.NextActions)
	s.TaskTools = boundList(s.TaskTools)
	s.LinkedSkills = boundList(s.LinkedSkills)

	if len(s.ToolCalls) > maxSnapItems {
		s.ToolCalls = s.ToolCalls[len(s.ToolCalls)-maxSnapItems:]
	}
	for i := range s.ToolCalls {
		s.ToolCalls[i].Name = clip(s.ToolCalls[i].Name, 64)
		s.ToolCalls[i].Args = clip(s.ToolCalls[i].Args, maxSnapItemChars)
		s.ToolCalls[i].Outcome = clip(s.ToolCalls[i].Outcome, maxSnapItemChars)
	}

	for k := range s.Configuration {
		s.Configuration[k] = clip(s.Configuration[k], maxSnapItemChars)
	}

	// The message list is NOT truncated here — the summarizer consumes it
	// in bounded chunks. Its total size is observable:
	s.EstimatedTokens = 0
	for i := range s.Messages {
		s.EstimatedTokens += estTokens(s.Messages[i].Content)
	}
}

// TokenCount returns the estimated token size of the frozen messages.
func (s *Snapshot) TokenCount() int {
	if s == nil {
		return 0
	}
	return s.EstimatedTokens
}

// CompletedWork renders the "do not redo" list from the observed state —
// the durable answer to §3.8 (prevent duplicate work): files already
// changed, commands/tests already run, artifacts already registered.
func (s *Snapshot) CompletedWork() []string {
	if s == nil {
		return nil
	}
	var out []string
	for _, f := range s.FilesChanged {
		out = append(out, "file changed: "+f)
	}
	for _, t := range s.TestsAndBuilds {
		out = append(out, "command run: "+t)
	}
	for _, a := range s.Artifacts {
		out = append(out, "artifact: "+a)
	}
	for _, tc := range s.ToolCalls {
		if tc.Failed {
			continue
		}
		out = append(out, "tool executed: "+tc.Name)
	}
	return dedupe(out)
}

// boundList dedupes (normalized) and caps a snapshot list.
func boundList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, item := range in {
		item = clip(item, maxSnapItemChars)
		k := normKey(item)
		if k != "" && seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, item)
		if len(out) >= maxSnapItems {
			break
		}
	}
	return out
}

// dedupe removes exact-normalized duplicates, preserving order.
func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		k := normKey(s)
		if k != "" && seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

// normKey normalizes a string for dedup comparison.
func normKey(s string) string {
	var b []rune
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b = append(b, r+32)
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			b = append(b, ' ')
		default:
			b = append(b, r)
		}
	}
	return string(b)
}
