package multiagent

// subtasks.go — v1.9.0 BOUNDED EXECUTABLE DELEGATION (spec §5).
//
// The existing specialists (researcher, architect, coder, debugger,
// tester, security) remain ADVISORY consultations. This file adds real,
// bounded, executable delegation ALONGSIDE them — same package, same
// orchestrator underneath, no second agent runtime:
//
//   - every subtask carries an explicit budget (context, deadline,
//     tool surface, read/write capability) — the executor enforces it
//     through the ONE orchestrator's existing seams (ToolPolicy
//     constrain, tier selection);
//   - resource policy: at most 2 simultaneous READ-ONLY subtasks (when
//     the concurrency seam permits), mutating work strictly serialized,
//     NO nested spawning (the executor is structurally a leaf: one
//     RunDetailed call — it cannot delegate further);
//   - results merge DETERMINISTICALLY in subtaskId order, never in
//     worker completion order;
//   - failed subtasks stay failed/unresolved — no fake success
//     synthesis;
//   - the parent receives structured evidence and concise results, not
//     raw sub-agent transcripts (context economy).

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Subtask bounds (spec §5 defaults; the Governor owns the resource
// policy through the concurrency seam).
const (
	MaxSimultaneousReadOnly = 2
	MaxSubtasksPerGoal      = 8
	MaxResultTextLen        = 4 * 1024
	DefaultSubtaskDeadline  = 120 * time.Second
)

// Subtask statuses (honest vocabulary — no fake success).
const (
	SubtaskCompleted = "completed"
	SubtaskFailed    = "failed"
	SubtaskBlocked   = "blocked"
)

// SubtaskSpec is one bounded, executable delegation.
type SubtaskSpec struct {
	SubtaskID   string   `json:"subtaskId"`   // deterministic merge key
	Role        Role     `json:"role"`        // researcher/architect/... (advisory profile reused)
	Objective   string   `json:"objective"`
	AllowedTools []string `json:"allowedTools,omitempty"`
	// ReadOnly restricts the subtask to read-only tools (the executor
	// constrains the orchestrator surface accordingly).
	ReadOnly  bool          `json:"readOnly,omitempty"`
	WorkspaceScope string   `json:"workspaceScope,omitempty"` // advisory scope hint
	MaxContextTokens int   `json:"maxContextTokens,omitempty"`
	Deadline  time.Duration `json:"deadline,omitempty"`
	EvidenceRequirement string `json:"evidenceRequirement,omitempty"`
}

// SubtaskEvidence is one compact structured record (spec §17).
type SubtaskEvidence struct {
	Action  string `json:"action"`
	Target  string `json:"target,omitempty"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
}

// SubtaskResult is the structured outcome of one subtask.
type SubtaskResult struct {
	SubtaskID    string            `json:"subtaskId"`
	Role         string            `json:"role"`
	Status       string            `json:"status"`
	Result       string            `json:"result,omitempty"`
	Evidence     []SubtaskEvidence `json:"evidence,omitempty"`
	Artifacts    []string          `json:"artifacts,omitempty"`
	ChangedFiles []string          `json:"changedFiles,omitempty"`
	Unresolved   []string          `json:"unresolved,omitempty"`
	DurationMs   int64             `json:"durationMs,omitempty"`
}

// SubtaskExecutor executes ONE leaf subtask through the orchestrator.
// It is structurally incapable of spawning further sub-agents (one
// RunDetailed call, no recursion) — the executor implementation owns
// that guarantee.
type SubtaskExecutor func(ctx context.Context, spec SubtaskSpec) SubtaskResult

// RunSubtasks executes the bounded fan-out and merges deterministically.
//
// Contract:
//   - results are returned in subtaskId order (the caller's order), never
//     in completion order;
//   - at most maxReadOnly (default 2, or fewer when the concurrency seam
//     reports tighter limits) read-only subtasks run simultaneously;
//   - mutating (non-read-only) subtasks run ONE AT A TIME, strictly
//     serialized, and never concurrently with each other;
//   - a parent-level cancellation stops scheduling NEW subtasks; already
//     running ones finish or hit their deadline;
//   - every subtask runs under its deadline (a deadline hit is an honest
//     blocked result, not a silent truncation).
func RunSubtasks(
	ctx context.Context,
	specs []SubtaskSpec,
	executor SubtaskExecutor,
	concurrencyPermit func() int,
) []SubtaskResult {
	if len(specs) == 0 || executor == nil {
		return nil
	}
	if len(specs) > MaxSubtasksPerGoal {
		specs = specs[:MaxSubtasksPerGoal]
	}

	// Normalize ids: a missing id gets its positional id (deterministic).
	for i := range specs {
		if strings.TrimSpace(specs[i].SubtaskID) == "" {
			specs[i].SubtaskID = fmt.Sprintf("st-%d", i+1)
		}
		if specs[i].Deadline <= 0 {
			specs[i].Deadline = DefaultSubtaskDeadline
		}
	}

	// The concurrency seam (Governor) may tighten — never widen — the
	// read-only parallelism.
	readOnlyLimit := MaxSimultaneousReadOnly
	if concurrencyPermit != nil {
		if p := concurrencyPermit(); p >= 1 && p < readOnlyLimit {
			readOnlyLimit = p
		}
	}

	results := make([]SubtaskResult, len(specs))
	byID := map[string]int{}
	for i, sp := range specs {
		byID[sp.SubtaskID] = i
	}

	var mu sync.Mutex
	readOnlySem := make(chan struct{}, readOnlyLimit)
	mutatingSem := make(chan struct{}, 1) // mutating work: strictly serialized

	var wg sync.WaitGroup
	for i, spec := range specs {
		wg.Add(1)
		go func(i int, spec SubtaskSpec) {
			defer wg.Done()

			// Parent cancellation: no NEW subtask starts.
			if ctx.Err() != nil {
				mu.Lock()
				results[i] = SubtaskResult{
					SubtaskID:  spec.SubtaskID,
					Role:       string(spec.Role),
					Status:     SubtaskBlocked,
					Unresolved: []string{"parent goal cancelled before this subtask started"},
				}
				mu.Unlock()
				return
			}

			var sem chan struct{}
			if spec.ReadOnly {
				sem = readOnlySem
			} else {
				sem = mutatingSem
			}

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				results[i] = SubtaskResult{
					SubtaskID:  spec.SubtaskID,
					Role:       string(spec.Role),
					Status:     SubtaskBlocked,
					Unresolved: []string{"parent goal cancelled while waiting for a execution slot"},
				}
				mu.Unlock()
				return
			}

			runCtx, cancel := context.WithTimeout(ctx, spec.Deadline)
			defer cancel()

			start := time.Now()
			res := executor(runCtx, spec)
			res.DurationMs = time.Since(start).Milliseconds()
			if res.SubtaskID == "" {
				res.SubtaskID = spec.SubtaskID
			}
			if res.Role == "" {
				res.Role = string(spec.Role)
			}
			if res.Status == "" {
				res.Status = SubtaskCompleted
			}

			mu.Lock()
			results[i] = res
			mu.Unlock()
		}(i, spec)
	}
	wg.Wait()

	// Deterministic merge in subtaskId order.
	sort.SliceStable(results, func(a, b int) bool {
		return posOf(byID, results[a].SubtaskID) < posOf(byID, results[b].SubtaskID)
	})
	return results
}

func posOf(byID map[string]int, id string) int {
	if p, ok := byID[id]; ok {
		return p
	}
	return 1 << 30
}

// MergeSubtaskResults composes the parent-facing summary: concise,
// subtaskId-ordered, failures explicit — never a raw transcript dump.
func MergeSubtaskResults(results []SubtaskResult) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	failed := 0
	for _, r := range results {
		if r.Status != SubtaskCompleted {
			failed++
		}
	}
	fmt.Fprintf(&b, "Delegated subtasks: %d (%d failed/blocked, kept unresolved)\n", len(results), failed)
	for _, r := range results {
		fmt.Fprintf(&b, "\n[%s] role=%s status=%s\n", r.SubtaskID, r.Role, r.Status)
		if r.Result != "" {
			fmt.Fprintf(&b, "result: %s\n", truncateForMerge(r.Result))
		}
		if len(r.Evidence) > 0 {
			b.WriteString("evidence:\n")
			for _, ev := range r.Evidence {
				fmt.Fprintf(&b, "  - %s %s (%s)\n", ev.Action, ev.Target, ev.Status)
			}
		}
		if len(r.ChangedFiles) > 0 {
			fmt.Fprintf(&b, "changed files: %s\n", strings.Join(r.ChangedFiles, ", "))
		}
		if len(r.Unresolved) > 0 {
			fmt.Fprintf(&b, "unresolved: %s\n", strings.Join(r.Unresolved, "; "))
		}
	}
	return b.String()
}

func truncateForMerge(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > MaxResultTextLen {
		return s[:MaxResultTextLen]
	}
	return s
}
