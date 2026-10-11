// Package multiagent is the multi-agent orchestration layer: a planner →
// executor → critic loop with persistent memory and a final summarizer.
//
// Architecture:
//
//	user prompt
//	  ↓
//	┌────────────┐  plan JSON
//	│  Planner   │ ──────────►
//	└────────────┘
//	  ↓ steps
//	┌────────────┐  tool calls / LLM calls
//	│  Executor  │ ──────────►
//	└────────────┘
//	  ↓
//	┌────────────┐  critique JSON
//	│   Critic   │ ──────────►
//	└────────────┘
//	  ↓ (if needs_revision && iter < max)
//	┌────────────┐
//	│ Summarizer │
//	└────────────┘
//	  ↓
//	final response to user
package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/memory"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
)

// PlannerSchema / CriticSchema are the JSON shapes each role expects.
var (
	plannerSystemPrompt = `You are the PLANNER agent of SHEYTAN-Local-Agent. Your job: read the user's request, decide on a small set of executable steps. Reply with strict JSON only:

{
  "summary": "one-line description of what you're going to do",
  "steps": [
    {"id": 1, "goal": "what this step should achieve", "tool": "shell|files|codeExec|webSearch|git|memory|llm", "args": {}}
  ]
}

Rules:
- 1-5 steps max. Prefer fewer.
- Each step's tool must be one of the listed tools.
- For "llm" steps, args should contain {"prompt": "..."}.
- Reply with the JSON only. No prose, no markdown code fences.`

	criticSystemPrompt = `You are the CRITIC agent of SHEYTAN-Local-Agent. Your job: review the executor's work and decide whether the user's request was satisfied. Reply with strict JSON only:

{
  "satisfied": true | false,
  "issues": ["list of issues, if any"],
  "next_step": "what to try next, if not satisfied"
}

Rules:
- "satisfied": true is allowed only when the executor's result provides sufficient evidence that the original request was actually completed.
- The "Objective verification" line in the user message reports the system's MEASURED verdict: verified means objective checks passed; not_verified or failed means NO objective evidence backs the completion claim. For tasks that can be verified (code that builds/tests, files that should exist, commands that should succeed), "satisfied": true requires at least "partially_verified" — executor prose alone is NOT sufficient evidence.
- If the evidence is incomplete, ambiguous, malformed, or missing, set "satisfied": false.
- Reply with JSON only. No prose.`

	summarizerSystemPrompt = `You are the SUMMARIZER agent of SHEYTAN-Local-Agent. The user's request was processed. The critic marked it as satisfied (or the maximum revision iterations were reached). Write a single final reply to the user explaining what was done, in clear language. No JSON, no markdown headings — just the answer.`
)

// MultiAgent orchestrates the planner → executor → critic loop with memory.
type MultiAgent struct {
	client  *llm.Client
	orch    *agent.Orchestrator
	mem     *memory.Store
	maxIter int

	// ModelFn resolves the current model id lazily so provider/model
	// switches in the UI apply to the very next LLM call.
	ModelFn func() string

	// ModelLimitsFn (1.1.6 §5) resolves the model's GGUF context maximum
	// and the engine's verified window so every role's context policy is
	// resolved against real limits. Nil = limits unknown (policies then
	// resolve against the base effective context only).
	ModelLimitsFn func() (modelMax, engineMax int)
}

// NewMultiAgent constructs a multi-agent runner. modelFn is called before
// every LLM request to resolve the current model id.
func NewMultiAgent(
	client *llm.Client,
	orch *agent.Orchestrator,
	mem *memory.Store,
	modelFn func() string,
	maxIter int,
) *MultiAgent {
	return &MultiAgent{
		client:  client,
		orch:    orch,
		mem:     mem,
		maxIter: maxIter,
		ModelFn: modelFn,
	}
}

func (m *MultiAgent) model() string {
	if m.ModelFn != nil {
		return m.ModelFn()
	}

	return ""
}

// Run executes one full multi-agent turn. Every phase emits an agent.Activity.
//
// v1.1.5 Phase 6 (multi-agent efficiency): the executor runs through
// RunDetailed so the CRITIC sees the run's objective verification verdict
// (verified / partially_verified / failed / not_verified) and tool-use
// stats — not just the executor's prose. A completion claim with no
// recorded evidence is passed to the critic as such, and the critic prompt
// forbids "satisfied": true without evidence. All inter-agent hand-offs
// are bounded excerpts (low-end hardware: one small model, sequential
// phases, compact artifacts).
func (m *MultiAgent) Run(
	ctx context.Context,
	prompt string,
	onActivity func(agent.Activity),
) (string, error) {
	// v1.1.4: local default instead of mutating the shared struct —
	// concurrent Run calls previously raced this write.
	maxIter := m.maxIter
	if maxIter < 1 {
		maxIter = 3
	}

	// 1) PLANNER
	onActivity(agent.Activity{
		Type:      "thinking",
		Caption:   "Planner: drafting steps…",
		Timestamp: time.Now(),
	})

	plan, err := m.callPlanner(ctx, prompt)
	if err != nil {
		// Fall back to direct orchestrator run (no plan).
		onActivity(agent.Activity{
			Type:      "thinking",
			Caption:   "Planner failed; falling back to direct execution: " + err.Error(),
			Timestamp: time.Now(),
		})

		return m.orch.Run(
			ctx,
			[]llm.Message{
				{
					Role:    "user",
					Content: prompt,
				},
			},
			onActivity,
		)
	}

	onActivity(agent.Activity{
		Type: "plan",
		Caption: fmt.Sprintf(
			"Planner: %d steps — %s",
			len(plan.Steps),
			plan.Summary,
		),
		Detail:    plan,
		Timestamp: time.Now(),
	})

	// 2) EXECUTOR — run the orchestrator (which executes tool calls as the
	// LLM sees fit, given the plan as additional context).
	executionPrompt := buildExecutionPrompt(
		prompt,
		plan,
	)

	// Phase 7C: bounded specialist consultations for complex tasks.
	// Advisory input only — the critic still requires objective
	// evidence, and a failed consultation never blocks execution.
	if consult := m.consultSpecialists(ctx, prompt, plan, onActivity); consult != "" {
		executionPrompt += "\n\n" + consult

		onActivity(agent.Activity{
			Type:      "plan",
			Caption:   "Specialist input attached to the execution brief",
			Timestamp: time.Now(),
		})
	}

	var verification agent.VerificationReport
	var loopStats []agent.StatsSnapshot

	final, err := m.orch.RunDetailed(
		ctx,
		[]llm.Message{
			{
				Role:    "user",
				Content: executionPrompt,
			},
		},
		onActivity,
	)

	if err != nil {
		return "", err
	}

	verification = final.Verification
	loopStats = final.LoopStats

	if v := verification.Summary(); v != "" {
		onActivity(agent.Activity{
			Type:      "verification",
			Caption:   "Executor: " + v,
			Detail:    verification,
			Timestamp: time.Now(),
		})
	}

	// 3) CRITIC
	for iter := 0; iter < maxIter; iter++ {
		critique, err := m.callCritic(
			ctx,
			prompt,
			final.Text,
			verification,
			loopStats,
		)

		if err != nil {
			// A failed critic call cannot establish satisfaction.
			onActivity(agent.Activity{
				Type:      "thinking",
				Caption:   "Critic unavailable; treating result as unsatisfied: " + err.Error(),
				Timestamp: time.Now(),
			})

			critique = &critiqueJSON{
				Satisfied: false,
				Issues: []string{
					"critic unavailable",
				},
				NextStep: "Retry the critic and require explicit JSON evidence.",
			}
		}

		if critique.Satisfied {
			onActivity(agent.Activity{
				Type:      "thinking",
				Caption:   "Critic: satisfied ✓",
				Detail:    critique,
				Timestamp: time.Now(),
			})

			break
		}

		onActivity(agent.Activity{
			Type: "thinking",
			Caption: fmt.Sprintf(
				"Critic: revision needed — %v",
				critique.Issues,
			),
			Detail:    critique,
			Timestamp: time.Now(),
		})

		// Re-execute with the critic's feedback.
		executionPrompt = fmt.Sprintf(
			"%s\n\n[CRITIC FEEDBACK]\nIssues: %s\nNext step: %s\n\nPlease revise your approach.",
			executionPrompt,
			strings.Join(
				critique.Issues,
				"; ",
			),
			critique.NextStep,
		)

		revised, err := m.orch.RunDetailed(
			ctx,
			[]llm.Message{
				{
					Role:    "user",
					Content: executionPrompt,
				},
			},
			onActivity,
		)

		if err != nil {
			break
		}

		final = revised
		verification = revised.Verification
		loopStats = revised.LoopStats
	}

	// 4) SUMMARIZER
	summary, err := m.callSummarizer(
		ctx,
		prompt,
		final.Text,
	)

	if err == nil &&
		summary != "" {
		onActivity(agent.Activity{
			Type:      "done",
			Caption:   "Summarizer: final reply ready",
			Timestamp: time.Now(),
		})

		// Save to memory.
		if m.mem != nil {
			_ = m.mem.Append(
				[]string{
					"conversation",
					time.Now().Format("2006-01-02"),
				},
				summary,
				"multiagent",
			)
		}

		return summary, nil
	}

	return final.Text, nil
}

// --- types ---

type planJSON struct {
	Summary string     `json:"summary"`
	Steps   []planStep `json:"steps"`
}

type planStep struct {
	ID   int            `json:"id"`
	Goal string         `json:"goal"`
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

type critiqueJSON struct {
	Satisfied bool     `json:"satisfied"`
	Issues    []string `json:"issues"`
	NextStep  string   `json:"next_step"`
}

// withEnv appends the offline environment note (if any) to a specialist
// system prompt so planner/critic/summarizer also avoid web-only plans.
func withEnv(sys string) string {
	if note := netcheck.Note(); note != "" {
		return sys + "\n\n" + note
	}

	return sys
}

// callPlanner invokes the LLM with the planner system prompt and parses
// the JSON plan.
func (m *MultiAgent) callPlanner(
	ctx context.Context,
	prompt string,
) (*planJSON, error) {
	policy := PolicyForRole("planner")
	effectiveCtx := m.resolveRoleContext("planner", policy, 0)
	req := &llm.ChatRequest{
		Model: m.model(),
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: withEnv(plannerSystemPrompt),
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.3,
		MaxTokens:   512,
		NumCtx:      effectiveCtx,
	}

	resp, err := m.client.Chat(
		ctx,
		req,
	)

	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf(
			"planner returned no choices",
		)
	}

	raw := resp.Choices[0].Message.Content
	jsonStr := extractJSON(raw)

	var plan planJSON

	if err := json.Unmarshal(
		[]byte(jsonStr),
		&plan,
	); err != nil {
		// Planner failure is handled separately from critic satisfaction.
		// Preserve the existing graceful planner fallback.
		plan = planJSON{
			Summary: "direct execution (planner produced non-JSON)",
			Steps: []planStep{
				{
					ID:   1,
					Goal: prompt,
					Tool: "llm",
					Args: map[string]any{
						"prompt": prompt,
					},
				},
			},
		}
	}

	return &plan, nil
}

func (m *MultiAgent) callCritic(
	ctx context.Context,
	prompt string,
	result string,
	verification agent.VerificationReport,
	loopStats []agent.StatsSnapshot,
) (*critiqueJSON, error) {
	policy := PolicyForRole("critic")
	effectiveCtx := m.resolveRoleContext("critic", policy, 0)
	req := &llm.ChatRequest{
		Model: m.model(),
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: withEnv(criticSystemPrompt),
			},
			{
				Role: "user",
				Content: fmt.Sprintf(
					"Original request:\n%s\n\nExecutor's result (excerpt):\n%s\n\nObjective verification: %s",
					prompt,
					boundedExcerpt(result, criticResultBudget),
					verification.Summary(),
				),
			},
		},
		Temperature: 0.2,
		MaxTokens:   256,
		NumCtx:      effectiveCtx,
	}

	resp, err := m.client.Chat(
		ctx,
		req,
	)

	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf(
			"critic returned no choices",
		)
	}

	jsonStr := extractJSON(
		resp.Choices[0].Message.Content,
	)

	var c critiqueJSON

	if err := json.Unmarshal(
		[]byte(jsonStr),
		&c,
	); err != nil {
		// Critical safety invariant:
		// malformed critic output MUST NOT become "satisfied".
		return &critiqueJSON{
			Satisfied: false,
			Issues: []string{
				"critic response was not valid JSON",
			},
			NextStep: "Retry the critic and require strict JSON with explicit evidence.",
		}, nil
	}

	return &c, nil
}

func (m *MultiAgent) callSummarizer(
	ctx context.Context,
	prompt string,
	result string,
) (string, error) {
	policy := PolicyForRole("summarizer")
	effectiveCtx := m.resolveRoleContext("summarizer", policy, 0)
	req := &llm.ChatRequest{
		Model: m.model(),
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: withEnv(summarizerSystemPrompt),
			},
			{
				Role: "user",
				Content: fmt.Sprintf(
					"Original request:\n%s\n\nWhat was done:\n%s",
					prompt,
					boundedExcerpt(result, criticResultBudget),
				),
			},
		},
		Temperature: 0.5,
		MaxTokens:   1024,
		NumCtx:      effectiveCtx,
	}

	resp, err := m.client.Chat(
		ctx,
		req,
	)

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf(
			"summarizer returned no choices",
		)
	}

	return resp.Choices[0].Message.Content, nil
}

func buildExecutionPrompt(
	userPrompt string,
	plan *planJSON,
) string {
	var b strings.Builder

	fmt.Fprintf(
		&b,
		"User's original request:\n%s\n\n",
		userPrompt,
	)

	fmt.Fprintf(
		&b,
		"Plan:\n",
	)

	fmt.Fprintf(
		&b,
		"Summary: %s\n",
		plan.Summary,
	)

	for _, s := range plan.Steps {
		fmt.Fprintf(
			&b,
			"  %d. [%s] %s\n",
			s.ID,
			s.Tool,
			s.Goal,
		)
	}

	fmt.Fprintf(
		&b,
		"\nExecute the plan. Use tools as needed. Return a clear final answer.",
	)

	return b.String()
}

// criticResultBudget bounds the executor-result excerpt handed to the
// critic and summarizer. Small local models on low-end hardware need the
// judgment to fit; the excerpt keeps the head (what was done) and marks
// the cut so the critic knows it saw an excerpt, not the whole.
const criticResultBudget = 6000

// boundedExcerpt clips s to at most n bytes on a rune boundary, marking
// the cut so the critic knows it saw an excerpt, not the whole.
func boundedExcerpt(s string, n int) string {
	if len(s) <= n {
		return s
	}

	cut := s[:n]

	// Back off to a rune boundary (the clip must not split a rune).
	for len(cut) > 0 && !utf8.ValidString(cut[len(cut)-4:]) {
		cut = cut[:len(cut)-1]
	}

	return cut + "\n…[excerpt — full text available in the executor's stream]"
}

// extractJSON finds the first { ... } block in s.
//
// LLMs sometimes wrap JSON in markdown fences or add prose around it.
// ExtractJSON extracts the first brace-balanced JSON object from a model
// reply (fences and prose tolerated). Exported (v1.1.4) so the release
// stress suite tests the REAL parser instead of a hand-copied duplicate.
func ExtractJSON(s string) string {
	return extractJSON(s)
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(
		s,
		"```",
	) {
		lines := strings.Split(
			s,
			"\n",
		)

		var out []string

		for _, l := range lines {
			if strings.HasPrefix(
				l,
				"```",
			) {
				continue
			}

			out = append(
				out,
				l,
			)
		}

		s = strings.Join(
			out,
			"\n",
		)
	}

	start := strings.Index(
		s,
		"{",
	)

	if start < 0 {
		return s
	}

	depth := 0

	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++

		case '}':
			depth--

			if depth == 0 {
				return s[start : i+1]
			}
		}
	}

	return s[start:]
}
