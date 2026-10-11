// injection.go — the continuation prompt block (v1.7.1 §3.7 step 6).
//
// RenderInjection renders the recovery handoff as a single bounded system
// block. The continuation attempt's message list becomes:
//
//	[system…] + [RECOVERY HANDOFF block] + [the ORIGINAL last user message]
//
// Guarantees (§3.8):
//   - the user message is NOT duplicated: the injection never re-quotes
//     the full original turn (the anchor message is appended separately
//     exactly once by the orchestrator);
//   - completed work is listed as "do not redo" so the model never
//     re-executes an irreversible completed action merely because it
//     forgot it;
//   - the block is token-bounded (InjectionTokenBudget) — it must fit
//     the same window that just exhausted.
package recovery

import (
	"fmt"
	"strings"
)

// InjectionHeader prefixes every rendered recovery injection so the UI,
// telemetry and distillation can recognize recovery continuations.
const InjectionHeader = "[SHEYTAN RECOVERY HANDOFF"

// InjectionTokenBudget bounds the rendered block.
const InjectionTokenBudget = 1200

// InjectionTokenBudget is enforced by trimming the summary section first,
// then list sections — mission and next actions survive deepest cuts.

// RenderInjection renders the bounded continuation block. An empty
// handoff renders an empty string (nothing to inject).
func RenderInjection(h *Handoff) string {
	if h == nil || (h.Summary == "" && h.Mission == "" && len(h.NextActions) == 0) {
		return ""
	}

	used := 0
	var b strings.Builder

	write := func(s string) {
		b.WriteString(s)
		used += estTokens(s)
	}

	write(fmt.Sprintf("%s — context limit reached; the task continues here]\n", InjectionHeader))
	write("Your previous context window was exhausted. The state below is your complete recovered memory of this task. Continue the SAME task seamlessly: never re-ask what is answered here, never redo work listed as already completed.\n\n")

	if h.Mission != "" {
		write("Objective: " + h.Mission + "\n")
	}
	if h.Summary != "" {
		write("\nWhat happened so far:\n" + boundSection(h.Summary, &used, 800) + "\n")
	}

	section := func(title string, items []string, perItem int) {
		if len(items) == 0 {
			return
		}
		head := "\n" + title + "\n"
		cost := estTokens(head)
		lines := make([]string, 0, len(items))
		for _, it := range items {
			l := "- " + clip(it, perItem) + "\n"
			c := estTokens(l)
			if used+cost+c > InjectionTokenBudget {
				break
			}
			cost += c
			lines = append(lines, l)
		}
		if len(lines) == 0 {
			return
		}
		write(head)
		for _, l := range lines {
			write(l)
		}
	}

	section("Do NOT redo (already completed):", h.CompletedWork, 140)
	section("Key facts:", h.Facts, 140)
	section("Decisions already made:", h.Decisions, 140)
	section("Open threads:", h.OpenThreads, 140)
	section("Next actions (continue here):", h.NextActions, 160)

	return strings.TrimRight(b.String(), "\n")
}

// boundSection writes s trimmed to the remaining budget.
func boundSection(s string, used *int, budgetTokens int) string {
	if *used >= InjectionTokenBudget {
		return ""
	}
	allow := InjectionTokenBudget - *used
	if budgetTokens < allow {
		allow = budgetTokens
	}
	out := clipToTokens(s, allow)
	*used += estTokens(out)
	return out
}

// clipToTokens clips s to ~budget tokens at a line boundary.
func clipToTokens(s string, budget int) string {
	if estTokens(s) <= budget {
		return s
	}
	lines := strings.Split(s, "\n")
	var b strings.Builder
	used := 0
	for _, l := range lines {
		lt := estTokens(l + "\n")
		if used+lt > budget && b.Len() > 0 {
			break
		}
		b.WriteString(l)
		b.WriteString("\n")
		used += lt
	}
	return strings.TrimRight(b.String(), "\n") + "\n… (summary bounded)"
}
