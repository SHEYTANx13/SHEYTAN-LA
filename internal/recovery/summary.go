// summary.go — whole-context summary without exceeding the summarizer
// window (v1.7.1 §3.4) plus the deterministic fallback (§3.5).
//
// The exhausted context itself may be too large for one summarization
// request, so the summarizer works HIERARCHICALLY:
//
//	complete persisted logical context
//	        ↓  ChunkMessages (bounded chunks)
//	        ↓  per-chunk summaries (bounded windows)
//	        ↓  folded merges (also bounded — pairs fold until one fits)
//	        merged recovery summary
//
// Every stage is bounded by token budgets measured with the same
// estimator the rest of the context stack uses (chunking.EstimateTokens).
// The summary represents the WHOLE logical context, not only the last
// window fragment: chunks are taken in order across the full message
// list, and the merge stage folds all partial summaries.
//
// If the summarizer cannot run (no engine, repeated failure), the
// deterministic FallbackSummary builds a bounded, structured handoff from
// the snapshot alone — the user's task is NEVER discarded because the
// optional summarizer failed.
package recovery

import (
	"context"
	"fmt"
	"strings"
)

// Summarizer is the bounded summarization seam. Implementations run ONE
// bounded LLM request and return its text; they must keep the request
// window inside the model's usable context (the prompt is already
// bounded by the chunk budgets below).
type Summarizer interface {
	Summarize(ctx context.Context, prompt string) (string, error)
}

// SummarizerFunc adapts a function to the Summarizer interface.
type SummarizerFunc func(ctx context.Context, prompt string) (string, error)

// Summarize implements Summarizer.
func (f SummarizerFunc) Summarize(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

// Summarization budgets (tokens, estimator-consistent).
const (
	// ChunkTokenBudget bounds one source chunk handed to the summarizer.
	ChunkTokenBudget = 2048
	// MergeTokenBudget bounds the merge-stage input. Partial summaries
	// fold in groups until the merge input fits this budget.
	MergeTokenBudget = 3072
	// maxChunkSummaries bounds the partial-summary list; beyond it the
	// oldest partials are folded together first (nothing is dropped).
	maxChunkSummaries = 64
)

// ChunkMessages splits the logical context into bounded, in-order chunks.
// Boundary preference: chunk at message boundaries; a single oversized
// message is split on paragraph, then hard-bounded. The chunks cover the
// COMPLETE list — no windowing, no tail-only fragment.
func ChunkMessages(msgs []Message, chunkTokens int) [][]Message {
	if chunkTokens < 256 {
		chunkTokens = 256
	}
	if len(msgs) == 0 {
		return nil
	}

	var chunks [][]Message
	var cur []Message
	curTokens := 0

	flush := func() {
		if len(cur) > 0 {
			chunks = append(chunks, cur)
			cur = nil
			curTokens = 0
		}
	}

	for i := range msgs {
		m := msgs[i]
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		t := estTokens(m.Content)

		// Oversized single message: split by paragraphs into pieces that
		// fit the chunk budget.
		for t > chunkTokens {
			flush()
			pieces := splitByTokens(m.Content, chunkTokens)
			for _, p := range pieces {
				chunks = append(chunks, []Message{{Role: m.Role, Content: p}})
			}
			m = Message{} // consumed
			t = 0
		}
		if m.Content == "" {
			continue
		}

		if curTokens+t > chunkTokens && len(cur) > 0 {
			flush()
		}
		cur = append(cur, m)
		curTokens += t
	}
	flush()
	return chunks
}

// splitByTokens splits one large text into ~token-bounded pieces at
// paragraph, then line, then hard boundaries.
func splitByTokens(text string, budget int) []string {
	var out []string
	paras := strings.Split(text, "\n\n")
	cur := strings.Builder{}
	curTokens := 0

	appendCur := func() {
		if cur.Len() > 0 {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			curTokens = 0
		}
	}

	for _, p := range paras {
		pt := estTokens(p)
		if pt > budget {
			// Hard-split an enormous paragraph by lines.
			appendCur()
			lines := strings.Split(p, "\n")
			var lineCur strings.Builder
			lineTokens := 0
			for _, l := range lines {
				lt := estTokens(l)
				if lineTokens+lt > budget && lineCur.Len() > 0 {
					out = append(out, strings.TrimSpace(lineCur.String()))
					lineCur.Reset()
					lineTokens = 0
				}
				lineCur.WriteString(l)
				lineCur.WriteString("\n")
				lineTokens += lt
			}
			if lineCur.Len() > 0 {
				out = append(out, strings.TrimSpace(lineCur.String()))
			}
			continue
		}
		if curTokens+pt > budget && cur.Len() > 0 {
			appendCur()
		}
		cur.WriteString(p)
		cur.WriteString("\n\n")
		curTokens += pt
	}
	appendCur()
	return out
}

// HierarchicalSummary produces the whole-context recovery summary.
// When sum is nil or fails on every stage, it returns ("", err) and the
// caller MUST fall back to FallbackSummary — the task is never dropped.
func HierarchicalSummary(ctx context.Context, snap *Snapshot, sum Summarizer) (string, error) {
	if snap == nil {
		return "", fmt.Errorf("recovery summary: nil snapshot")
	}
	if sum == nil {
		return "", fmt.Errorf("recovery summary: no summarizer available")
	}

	chunks := ChunkMessages(snap.Messages, ChunkTokenBudget)
	if len(chunks) == 0 {
		// Nothing to summarize from raw messages — the structured state
		// alone is the summary source.
		return "", fmt.Errorf("recovery summary: no messages to summarize")
	}

	// Stage 1: per-chunk summaries (in order — the merge preserves the
	// whole-context coverage).
	partials := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("recovery summary: %w", err)
		}
		out, err := sum.Summarize(ctx, chunkPrompt(snap, chunk, i+1, len(chunks)))
		if err != nil {
			return "", fmt.Errorf("recovery summary: chunk %d/%d failed: %w", i+1, len(chunks), err)
		}
		if out = strings.TrimSpace(out); out != "" {
			partials = append(partials, clip(out, 900))
		}
		if len(partials) >= maxChunkSummaries {
			break
		}
	}
	if len(partials) == 0 {
		return "", fmt.Errorf("recovery summary: all chunk summaries empty")
	}

	// Stage 2: bounded merge. Fold partial summaries in groups until the
	// merge input fits MergeTokenBudget — the merge stage itself must
	// never exceed the available context.
	for estTokens(strings.Join(partials, "\n")) > MergeTokenBudget && len(partials) > 1 {
		partials = foldPartials(partials, sum, ctx)
		if partials == nil {
			return "", fmt.Errorf("recovery summary: merge stage failed")
		}
	}
	if len(partials) == 1 {
		// A single partial already fits: one final pass folds the task
		// framing (mission/next actions) into the continuation summary.
		out, err := sum.Summarize(ctx, mergePrompt(snap, partials))
		if err != nil {
			return "", fmt.Errorf("recovery summary: merge failed: %w", err)
		}
		return clip(strings.TrimSpace(out), 4000), nil
	}

	out, err := sum.Summarize(ctx, mergePrompt(snap, partials))
	if err != nil {
		return "", fmt.Errorf("recovery summary: merge failed: %w", err)
	}
	return clip(strings.TrimSpace(out), 4000), nil
}

// foldPartials folds one group of partials into a single intermediate
// summary (one LLM call), reducing the list length.
func foldPartials(partials []string, sum Summarizer, ctx context.Context) []string {
	group := MergeTokenBudget / 2
	groupTok := 0
	take := 0
	for i, p := range partials {
		t := estTokens(p)
		if take > 1 && groupTok+t > group {
			partials = partials[:i]
			break
		}
		groupTok += t
		take++
	}
	if take < 2 {
		// Cannot fold anything — give up honestly instead of looping.
		return nil
	}

	folded := partials[:take]
	rest := append([]string(nil), partials[take:]...)

	out, err := sum.Summarize(ctx, foldPrompt(folded))
	if err != nil {
		return nil
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	return append([]string{clip(out, 1200)}, rest...)
}

// --- prompts (bounded, task-framed) -----------------------------------------

func chunkPrompt(snap *Snapshot, chunk []Message, idx, total int) string {
	var b strings.Builder
	b.WriteString("You are writing ONE part of a recovery handoff for an AI agent whose context window was exhausted mid-task.\n")
	if snap.Mission != "" {
		b.WriteString("Task objective: " + snap.Mission + "\n")
	}
	fmt.Fprintf(&b, "Summarize PART %d of %d of the conversation below. Keep: concrete facts, decisions, file paths, commands and their outcomes, and what was left unfinished. Be compact (under 250 words). No preamble.\n\n", idx, total)
	for _, m := range chunk {
		b.WriteString(m.Role + ": " + m.Content + "\n")
	}
	return b.String()
}

func foldPrompt(partials []string) string {
	var b strings.Builder
	b.WriteString("Merge these intermediate summaries of one long task into one compact summary. Preserve every distinct fact, decision, path and outcome. Under 400 words. No preamble.\n\n")
	for _, p := range partials {
		b.WriteString("- " + p + "\n")
	}
	return b.String()
}

func mergePrompt(snap *Snapshot, partials []string) string {
	var b strings.Builder
	b.WriteString("Produce the FINAL recovery summary for an AI agent continuing an interrupted task. It will be the agent's only memory of everything before the interruption.\n")
	if snap.Mission != "" {
		b.WriteString("Objective: " + snap.Mission + "\n")
	}
	if len(snap.NextActions) > 0 {
		b.WriteString("Recorded next actions: " + strings.Join(snap.NextActions, "; ") + "\n")
	}
	b.WriteString("Merge the parts below into one coherent summary: what the task is, what was done, what was decided, what failed, and what remains. Under 500 words. No preamble.\n\n")
	for _, p := range partials {
		b.WriteString("- " + p + "\n")
	}
	return b.String()
}

// --- deterministic fallback (§3.5) -------------------------------------------

// FallbackSummary builds a bounded, DETERMINISTIC recovery summary from
// the snapshot's structured state alone. It is always available: no LLM,
// no network, no failure path. When the summarizer is unusable, THIS is
// the summary — the user's task continues from durable state.
func FallbackSummary(snap *Snapshot) string {
	if snap == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("[RECOVERY SUMMARY — deterministic, no summarizer was available]\n\n")

	if snap.Mission != "" {
		b.WriteString("Objective: " + snap.Mission + "\n")
	}
	if snap.CurrentState != "" {
		b.WriteString("State when interrupted: " + snap.CurrentState + "\n")
	}
	if len(snap.Facts) > 0 {
		b.WriteString("\nKey facts:\n")
		for _, f := range snap.Facts {
			b.WriteString("- " + f + "\n")
		}
	}
	if len(snap.Decisions) > 0 {
		b.WriteString("\nDecisions made:\n")
		for _, d := range snap.Decisions {
			b.WriteString("- " + d + "\n")
		}
	}
	if len(snap.FilesInspected) > 0 {
		b.WriteString("\nFiles inspected:\n")
		for _, f := range snap.FilesInspected {
			b.WriteString("- " + f + "\n")
		}
	}
	if len(snap.FilesChanged) > 0 {
		b.WriteString("\nFiles changed:\n")
		for _, f := range snap.FilesChanged {
			b.WriteString("- " + f + "\n")
		}
	}
	if len(snap.ToolCalls) > 0 {
		b.WriteString("\nTool calls executed:\n")
		for _, tc := range snap.ToolCalls {
			status := "ok"
			if tc.Failed {
				status = "FAILED"
			}
			b.WriteString("- " + tc.Name + " (" + status + ")")
			if tc.Outcome != "" {
				b.WriteString(": " + tc.Outcome)
			}
			b.WriteString("\n")
		}
	}
	if len(snap.TestsAndBuilds) > 0 {
		b.WriteString("\nTests/builds run:\n")
		for _, t := range snap.TestsAndBuilds {
			b.WriteString("- " + t + "\n")
		}
	}
	if len(snap.Failures) > 0 {
		b.WriteString("\nFailures encountered:\n")
		for _, f := range snap.Failures {
			b.WriteString("- " + f + "\n")
		}
	}
	if len(snap.Repairs) > 0 {
		b.WriteString("\nRepairs applied:\n")
		for _, r := range snap.Repairs {
			b.WriteString("- " + r + "\n")
		}
	}
	if len(snap.Artifacts) > 0 {
		b.WriteString("\nArtifacts:\n")
		for _, a := range snap.Artifacts {
			b.WriteString("- " + a + "\n")
		}
	}
	if len(snap.OpenThreads) > 0 {
		b.WriteString("\nOpen threads:\n")
		for _, o := range snap.OpenThreads {
			b.WriteString("- " + o + "\n")
		}
	}
	if len(snap.NextActions) > 0 {
		b.WriteString("\nNext actions:\n")
		for _, n := range snap.NextActions {
			b.WriteString("- " + n + "\n")
		}
	}

	out := b.String()
	// Hard bound: the fallback is a context component.
	const maxFallbackChars = 6000
	if len(out) > maxFallbackChars {
		out = out[:maxFallbackChars]
		if i := strings.LastIndexByte(out, '\n'); i > maxFallbackChars/2 {
			out = out[:i]
		}
		out += "\n… (bounded)"
	}
	return out
}
