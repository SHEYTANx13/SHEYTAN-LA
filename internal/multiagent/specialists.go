package multiagent

// specialists.go — Phase 7C: persistent specialist agents.
//
// The Phase 6 pipeline (planner → executor → critic → summarizer) is role
// prompting through ONE shared model. Specialists extend it WITHOUT
// spawning separate model processes: for complex tasks, bounded specialist
// consultations (researcher, architect, debugger, tester, security) run
// between planning and execution, each one bounded LLM call whose output is
// appended to the execution prompt as evidence.
//
// Resource rule — "one small model + better orchestration": specialists
// are selected by task complexity, capped per run, every consultation is
// time- and token-bounded, and a failed consultation is skipped (it can
// never block execution). The critic remains the only judge of completion;
// specialist prose is input, not proof.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// Role identifies a specialist.
type Role string

// Specialist roles. The executor and critic roles already exist as the
// Phase 6 pipeline stages; these are the consultable experts.
const (
	RoleResearcher Role = "researcher"
	RoleArchitect  Role = "architect"
	RoleCoder      Role = "coder"
	RoleDebugger   Role = "debugger"
	RoleTester     Role = "tester"
	RoleSecurity   Role = "security"
)

// maxSpecialistsPerRun is the resource bound: even the most complex task
// consults at most this many specialists.
const maxSpecialistsPerRun = 2

// specialistProfile describes one specialist's focus and triggers.
type specialistProfile struct {
	Role     Role
	Label    string
	System   string
	Triggers []string
}

var specialistProfiles = []specialistProfile{
	{
		Role:     RoleResearcher,
		Label:    "Researcher",
		System:   "You are the RESEARCHER specialist. Given a task, list the key unknowns and where to find them (local files, web search, git history). Be concrete: name files, commands, queries. Max 6 bullet points, no prose intro.",
		Triggers: []string{"research", "find out", "investigate", "compare", "evaluate", "latest", "how does", "what is"},
	},
	{
		Role:     RoleArchitect,
		Label:    "Architect",
		System:   "You are the ARCHITECT specialist. Given a task, outline the minimal sound structure: components, files to touch, interfaces, and one risk to watch. Max 6 bullet points, no prose intro.",
		Triggers: []string{"design", "architecture", "refactor", "structure", "module", "interface", "api", "plan", "implement"},
	},
	{
		Role:     RoleCoder,
		Label:    "Coder",
		System:   "You are the CODER specialist. Given a task, state the concrete edit strategy: exact files/functions to change, the order of edits, and what NOT to touch. Max 6 bullet points, no prose intro.",
		Triggers: []string{"implement", "fix", "write", "code", "function", "feature", "patch", "bug"},
	},
	{
		Role:     RoleDebugger,
		Label:    "Debugger",
		System:   "You are the DEBUGGER specialist. Given a task and symptoms, list the most probable causes ranked, and the single cheapest diagnostic command for each. Max 5 bullets, no prose intro.",
		Triggers: []string{"error", "fails", "failing", "crash", "hang", "broken", "wrong", "panic", "exception", "stack trace"},
	},
	{
		Role:     RoleTester,
		Label:    "Tester",
		System:   "You are the TESTER specialist. Given a task, define the objective acceptance checks: the exact commands or probes whose success proves the task is done. Max 5 bullets, commands included verbatim, no prose intro.",
		Triggers: []string{"test", "verify", "prove", "acceptance", "coverage", "benchmark", "regression"},
	},
	{
		Role:     RoleSecurity,
		Label:    "Security",
		System:   "You are the SECURITY specialist. Given a task, list the concrete risks to avoid (injection, path escape, secret exposure, unsafe defaults) and the guard to apply for each. Max 5 bullets, no prose intro.",
		Triggers: []string{"password", "secret", "token", "auth", "security", "sanitize", "escape", "injection", "permission", "unsafe"},
	},
}

// complexityScore estimates whether a task deserves specialist consultation
// (0-3). Simple tasks run the plain pipeline — specialists cost tokens.
func complexityScore(task string, planSteps int) int {
	score := 0
	if planSteps >= 3 {
		score++
	}
	if len(task) > 400 {
		score++
	}
	lower := strings.ToLower(task)
	hits := 0
	for _, p := range specialistProfiles {
		for _, kw := range p.Triggers {
			if strings.Contains(lower, kw) {
				hits++
				break
			}
		}
	}
	if hits >= 2 {
		score++
	}
	if hits >= 4 {
		score++
	}
	return score
}

// selectSpecialists picks the specialists for a task: profile triggers are
// scored against the task text, capped at maxSpecialistsPerRun.
// Deterministic (registry order, stable).
func selectSpecialists(task string) []specialistProfile {
	lower := strings.ToLower(task)
	var selected []specialistProfile
	for _, p := range specialistProfiles {
		for _, kw := range p.Triggers {
			if strings.Contains(lower, kw) {
				selected = append(selected, p)
				break
			}
		}
		if len(selected) >= maxSpecialistsPerRun {
			break
		}
	}
	return selected
}

// consultSpecialists runs the bounded specialist consultations for a task.
// Returns the composed block for the execution prompt ("" when the task is
// simple or every consultation failed — a consultation never blocks).
func (m *MultiAgent) consultSpecialists(
	ctx context.Context,
	task string,
	plan *planJSON,
	onActivity func(agent.Activity),
) string {
	if complexityScore(task, len(plan.Steps)) < 2 {
		return "" // simple task: plain pipeline, no specialist cost
	}

	selected := selectSpecialists(task)
	if len(selected) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("[SPECIALIST INPUT — advisory, the critic still requires objective evidence]\n")

	for _, sp := range selected {
		if err := ctx.Err(); err != nil {
			break
		}

		// 1.1.6 §17: the activity shows the agent, its resolved
		// context policy and status — without noisy internals.
		policy := PolicyForRole(string(sp.Role))
		resolvedCtx := m.resolveRoleContext(string(sp.Role), policy, 0)

		onActivity(agent.Activity{
			Type:      "thinking",
			Caption:   fmt.Sprintf("Specialist %s: consulting… (%s context)", sp.Label, llm.ContextLabel(resolvedCtx)),
			Timestamp: time.Now(),
		})

		out := m.consultOne(ctx, sp, task, boundedExcerpt(planSummary(plan), 2000))
		if out == "" {
			onActivity(agent.Activity{
				Type:      "thinking",
				Caption:   fmt.Sprintf("Specialist %s: unavailable — skipped", sp.Label),
				Timestamp: time.Now(),
			})
			continue
		}

		fmt.Fprintf(&b, "\n%s:\n%s\n", sp.Label, boundedExcerpt(out, 1200))
	}

	// Only the header and nothing else means no specialist produced
	// usable input.
	if b.Len() <= len("[SPECIALIST INPUT — advisory, the critic still requires objective evidence]\n") {
		return ""
	}
	return b.String()
}

// consultOne performs ONE bounded specialist call (45s, policy-bounded
// output). The specialist's context policy (1.1.6 §5) is resolved and
// logged before the call so no agent silently runs with the largest
// available window.
func (m *MultiAgent) consultOne(
	ctx context.Context,
	sp specialistProfile,
	task string,
	planExcerpt string,
) string {
	callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	policy := PolicyForRole(string(sp.Role))
	effectiveCtx := m.resolveRoleContext(string(sp.Role), policy, 0)

	userContent := fmt.Sprintf("Task: %s\n\nPlan summary: %s",
		boundedExcerpt(task, 2000), planExcerpt)

	resp, err := m.client.Chat(callCtx, &llm.ChatRequest{
		Model: m.model(),
		Messages: []llm.Message{
			{Role: "system", Content: sp.System},
			{Role: "user", Content: userContent},
		},
		MaxTokens:   policy.OutputReserve,
		Temperature: 0.3,
		NumCtx:      effectiveCtx,
	})
	if err != nil {
		return ""
	}
	if len(resp.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content)
}

// planSummary renders the plan's steps compactly for specialist prompts.
func planSummary(plan *planJSON) string {
	if plan == nil {
		return ""
	}
	var parts []string
	for _, s := range plan.Steps {
		parts = append(parts, fmt.Sprintf("%d. %s", s.ID, s.Goal))
	}
	return strings.Join(parts, " | ")
}
