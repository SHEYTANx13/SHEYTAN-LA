// Package contextplan implements SHEYTAN's explicit long-context budget
// (v1.1.3). Every inference request assembles its prompt from named
// sections — system instructions, tool schemas, recalled memory, attachment
// chunks, recent history, and reserved output space — and every section is
// measured, prioritized, and accounted against the model's real context
// capacity BEFORE the request is sent.
//
// The plan is pure math over token estimates: no I/O, no goroutines. The
// orchestrator calls it once per turn, windowes history to the computed
// history budget, and reports the result to the UI (provenance: the UI can
// show WHERE the prompt budget went without exposing raw prompts).
package contextplan

import (
	"fmt"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// Section names, stable for the UI contract.
const (
	SectionSystem      = "system"
	SectionTools       = "tools"
	SectionSummary     = "summary"
	SectionRecall      = "recall"
	SectionHistoryRefs = "history-refs"
	SectionAttachments = "attachments"
	SectionHistory     = "history"
	SectionReserve     = "output-reserve"
)

// Priority orders sections when pressure forces cuts. Lower wins.
const (
	PriorityReserve     = 0 // output space is never spent on prompt
	PrioritySystem      = 1 // the agent briefing is indispensable
	PriorityTools       = 2 // tool schemas enable the tool loop
	PrioritySummary     = 2 // (v1.2.8) the rolling session summary
	PriorityHistory     = 3 // the current turn lives here
	PriorityRecall      = 4 // past-exchange digests
	PriorityHistoryRefs = 4 // (v1.2.8) retrieved cross-mode history blocks
	PriorityAttachments = 5 // retrieved attachment chunks
)

// v1.2.5 priority classes — the four-level view of the same ladder the UI
// and the tier system speak in. P0 is mandatory, P1 highly relevant, P2
// useful, P3 optional. The mapping is derived, never stored twice.
const (
	ClassP0 = "P0" // mandatory: output reserve, system briefing
	ClassP1 = "P1" // highly relevant: tool schemas, history/current turn
	ClassP2 = "P2" // useful: recall digests
	ClassP3 = "P3" // optional: attachment chunks
)

// PriorityClass maps a numeric priority to its P0-P3 label.
func PriorityClass(priority int) string {
	switch {
	case priority <= PrioritySystem:
		return ClassP0
	case priority <= PriorityHistory:
		return ClassP1
	case priority <= PriorityRecall:
		return ClassP2
	default:
		return ClassP3
	}
}

// Pressure levels for the v1.2.8 context-cleaning triggers. The labels
// mirror the continuum usage levels (ok/warm/high/critical) so the UI
// speaks ONE pressure language everywhere. The orchestrator uses the
// classification to decide when to proactively compact BEFORE the final
// model call fails from overflow — never silently exhaust the window.
const (
	PressureOK       = "ok"       // < 50%
	PressureWarm     = "warm"     // < 75%
	PressureHigh     = "high"     // < 90%
	PressureCritical = "critical" // >= 90% — cleanup triggers now
)

// ClassifyPressure maps a used/usable ratio (plan.Pressure()) onto the
// shared pressure vocabulary.
func ClassifyPressure(p float64) string {
	switch {
	case p >= 0.90:
		return PressureCritical
	case p >= 0.75:
		return PressureHigh
	case p >= 0.50:
		return PressureWarm
	default:
		return PressureOK
	}
}

// Budget is the explicit context budget for one request.
type Budget struct {
	// Total is the model's usable context window (num_ctx).
	Total int

	// ReserveOutput is tokens held back for the model's reply.
	ReserveOutput int

	// Usable is Total - ReserveOutput; the prompt must never exceed it.
	Usable int
}

// NewBudget computes the budget from the engine context size.
//
// v1.2.9 HONEST SMALL-WINDOW ACCOUNTING: numCtx is the EFFECTIVE window
// the engine actually serves. The previous code inflated any numCtx <
// 1024 to 1024 — fabricating context the model does not have, so the
// planner happily allocated tokens the engine would truncate or refuse.
// Now only a NON-POSITIVE numCtx (unknown/unresolved) falls back to the
// documented 1024 default assumption; a real small window is budgeted
// AS IT IS. The output-reserve floor likewise SCALES DOWN on small
// windows (never more than a quarter of the window, and always at least
// a token) instead of demanding a fixed 512 tokens a small model cannot
// spare. maxTokens still defaults to 1024 when unset and is clamped to
// at most half the window.
func NewBudget(numCtx, maxTokens int) Budget {
	if numCtx <= 0 {
		numCtx = 1024 // unknown window: documented default assumption, not a claim
	}

	if maxTokens <= 0 {
		maxTokens = 1024
	}

	if maxTokens > numCtx/2 {
		maxTokens = numCtx / 2
	}

	reserve := maxTokens
	// v1.2.9: the 512-token reserve floor is a PREFERENCE bounded by
	// the real window: on a small model it scales down to a quarter
	// of the window (a 512-token floor on a 512-token window would
	// leave the prompt ZERO usable space — the fixed-minimum-budget
	// flaw this release removes).
	floor := 512
	if q := numCtx / 4; q < floor {
		floor = q
	}
	if floor < 1 {
		floor = 1
	}
	if reserve < floor {
		reserve = floor
	}

	return Budget{
		Total:         numCtx,
		ReserveOutput: reserve,
		Usable:        numCtx - reserve,
	}
}

// OutputReserveFor returns the SAME clamped output reserve NewBudget
// applies (v1.2.6). Tier selection, context planning and telemetry MUST
// agree on ONE reserve number — v1.2.5 fed tier selection the RAW
// max_tokens while the plan clamped it, producing different outputReserve
// values between the tier decision and the context report.
func OutputReserveFor(numCtx, maxTokens int) int {
	return NewBudget(numCtx, maxTokens).ReserveOutput
}

// Section is the measured cost of one prompt section after assembly.
type Section struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Tokens   int    `json:"tokens"`
	Budget   int    `json:"budget"`
	Included bool   `json:"included"`
	Note     string `json:"note,omitempty"`
}

// Plan is the full accounting of one assembled request.
type Plan struct {
	Budget   Budget    `json:"budget"`
	Sections []Section `json:"sections"`

	// HistoryBudget is the token budget handed to the history windower.
	HistoryBudget int `json:"historyBudget"`

	// Elided counts history messages compacted by the windower.
	Elided int `json:"elided,omitempty"`

	// Recalled counts injected recall digests.
	Recalled int `json:"recalled,omitempty"`

	// Attachments counts attachments represented in the prompt.
	Attachments int `json:"attachments,omitempty"`

	// PromptBytes is the MEASURED size of the final prompt content in
	// bytes (sum of message content + tool-call arguments). Phase 3
	// instrumentation: a real measurement, set after assembly — never an
	// estimate.
	PromptBytes int64 `json:"promptBytes,omitempty"`

	// SafetyMargin is the Phase 7 extra headroom held back from the
	// prompt on top of the output reserve (token-estimator error,
	// template expansion). The prompt ceiling is
	// Budget.Usable - SafetyMargin.
	SafetyMargin int `json:"safetyMargin,omitempty"`

	// Adjustments records every automatic repair applied while fitting
	// the request into the budget (toolset reduction, briefing
	// compaction, dropped blocks), in order — the honest trail of what
	// changed and why.
	Adjustments []string `json:"adjustments,omitempty"`

	// ModelSummary optionally carries the model capability summary line
	// that produced the budget (small-model transparency for the UI).
	ModelSummary string `json:"modelSummary,omitempty"`

	// Tier (v1.2.5) is the adaptive context tier this plan was composed
	// under: FAST | STANDARD | DEEP | MAX. Empty for plans built outside
	// the tier system (compat).
	Tier string `json:"tier,omitempty"`

	// Estimator (v1.2.9) labels the token-accounting tier this plan was
	// measured with: "exact" (engine tokenizer), "family:<name>"
	// (family-tuned estimate) or "heuristic" (conservative fallback).
	// A plan on any estimate tier is CONSERVATIVE, never mathematically
	// exact — consumers must not claim exact context safety when this
	// label is not "exact".
	Estimator string `json:"estimator,omitempty"`

	// Memory (v1.8.2) is the MEASURED memory-evidence record for this
	// turn: which memory systems exist, which were actually injected and
	// how much they carried. The UI renders exactly this — no fabricated
	// "memory used" claims are possible because the record is composed
	// at the injection site from the injection facts themselves. Nil for
	// plans built outside the orchestrator (compat).
	Memory *MemoryEvidence `json:"memory,omitempty"`
}

// MemoryEvidence is the v1.8.2 backend-truth record of what the memory
// authorities actually contributed to THIS turn's prompt. Every field is
// set from the injection facts at the moment of injection — counts and
// presence only, never content, never guesses.
type MemoryEvidence struct {
	// SummaryInjected is true when the rolling session summary block was
	// actually carried into the prompt (the plan kept it).
	SummaryInjected bool `json:"summaryInjected"`

	// SummaryTokens is the measured token estimate of the injected
	// summary block (0 when absent).
	SummaryTokens int `json:"summaryTokens,omitempty"`

	// RecalledExchanges counts the past-exchange digests the targeted
	// recall engine actually injected this turn (0 when none matched or
	// recall did not run).
	RecalledExchanges int `json:"recalledExchanges,omitempty"`

	// HistoryRefs counts the attached cross-mode history-reference
	// blocks actually carried into the prompt.
	HistoryRefs int `json:"historyRefs,omitempty"`

	// RecallAttempted distinguishes "recall ran and found nothing"
	// (true) from "recall did not apply to this turn" (false) — the UI
	// must never conflate a clean miss with an untried tool.
	RecallAttempted bool `json:"recallAttempted,omitempty"`
}

// PromptCeiling is the authoritative maximum for the assembled prompt:
// usable window minus the safety margin. The request must never
// intentionally exceed it.
func (p Plan) PromptCeiling() int {
	return p.Budget.Usable - p.SafetyMargin
}

// Overflow returns how many tokens the measured sections exceed the
// prompt ceiling (0 = the plan fits).
func (p Plan) Overflow() int {
	over := p.TotalTokens() - p.PromptCeiling()
	if over < 0 {
		return 0
	}
	return over
}

// SetSectionTokens overrides the measured token count of one section
// (used by the orchestrator after history windowing measures the actual
// windowed messages).
func (p *Plan) SetSectionTokens(name string, tokens int) {
	for i := range p.Sections {
		if p.Sections[i].Name == name {
			p.Sections[i].Tokens = tokens
			return
		}
	}
}

// SetPromptBytes records the measured final prompt size (Phase 3
// instrumentation; measured values only).
func (p *Plan) SetPromptBytes(b int64) {
	p.PromptBytes = b
}

// TotalTokens sums the measured PROMPT sections (Phase 7: the output
// reserve is accounted separately — it is not prompt content, so it must
// not be double-counted against the prompt ceiling).
//
// v1.2.8.1: sections the planner DROPPED (Included=false) are no longer
// counted. A dropped section is not part of the prompt — every injection
// point gates on Included — so counting its measured tokens poisoned
// Overflow() and converted designed graceful drops (history-refs and the
// summary under pressure) into hard turn refusals. The system briefing is
// the one exception: splitSystemPrefix always carries it, even when the
// planner marks it refused, so its tokens still count.
func (p Plan) TotalTokens() int {
	total := 0
	for _, s := range p.Sections {
		if s.Name == SectionReserve {
			continue
		}
		if !s.Included && s.Name != SectionSystem {
			continue
		}
		total += s.Tokens
	}
	return total
}

// Pressure returns used/usable as 0..1+.
func (p Plan) Pressure() float64 {
	if p.Budget.Usable <= 0 {
		return 0
	}
	return float64(p.TotalTokens()) / float64(p.Budget.Usable)
}

// Summary renders the compact provenance line for the UI, e.g.
// "context 3.2k/8.0k tok — system 0.4k · tools 1.1k · history 1.2k · recall 0.3k".
func (p Plan) Summary() string {
	var b strings.Builder

	fmt.Fprintf(&b, "context %s/%s tok",
		formatK(p.TotalTokens()), formatK(p.Budget.Usable))

	first := true

	for _, s := range p.Sections {
		if !s.Included || s.Tokens <= 0 || s.Name == SectionReserve {
			continue
		}

		if first {
			b.WriteString(" — ")
			first = false
		} else {
			b.WriteString(" · ")
		}

		fmt.Fprintf(&b, "%s %s", s.Name, formatK(s.Tokens))

		if s.Name == SectionHistory && p.Elided > 0 {
			fmt.Fprintf(&b, " (%d compacted)", p.Elided)
		}
	}

	return b.String()
}

// TierSummary renders the v1.2.5 compact tier-aware line for the live
// status chip, e.g. "STANDARD · 7.2k tok" (measured plan tokens only).
func (p Plan) TierSummary() string {
	if p.Tier == "" {
		return p.Summary()
	}

	return fmt.Sprintf("%s · %s tok", p.Tier, formatK(p.TotalTokens()))
}

func formatK(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// Input carries the measured pieces the orchestrator already has.
type Input struct {
	SystemTokens     int // AI-context briefing (+ thinking nudge)
	ToolTokens       int // serialized tool schemas
	RecallTokens     int // recall block already composed
	AttachmentTokens int // attachment representation already composed
	History          []llm.Message
	NumCtx           int
	MaxOutputTokens  int
	MinHistoryTokens int // floor so a huge current turn still gets room

	// SummaryTokens (v1.2.8) is the measured cost of the rolling session
	// summary block. It is a REQUIRED section (priority 2): the summary
	// is what makes elided history survivable, so it is budgeted with the
	// fixed sections, not with the optional ones.
	SummaryTokens int

	// HistoryRefTokens (v1.2.8) is the measured cost of the retrieved
	// cross-mode history blocks attached to this turn. OPTIONAL (priority
	// 4, with recall): under pressure they are dropped first — the user
	// can re-attach them, while the current turn cannot be re-derived.
	HistoryRefTokens int

	// SafetyMarginTokens is Phase 7 headroom held back from the prompt
	// on top of the output reserve (estimator error, chat-template
	// expansion). 0 keeps the v1.1.3 behavior.
	SafetyMarginTokens int

	// MaxHistoryShare (v1.2.5) optionally caps the history budget to a
	// fraction of the usable window (0..1; <=0 = no cap). The adaptive
	// tier sets it so smaller tiers leave room for escalation instead of
	// filling the window with transcript on the first try.
	MaxHistoryShare float64
}

// Assemble computes the plan: allocates budgets per section with priority
// fallbacks, and returns the history budget to window the conversation
// with.
//
// v1.2.8.1: the history floor (MinHistoryTokens, default 2048) is a
// PREFERENCE bounded by what actually remains — not a fixed promise. The
// v1.2.8 unconditional raise (`historyBudget = max(remaining, 2048)`)
// allocated tokens that do not exist on small effective contexts (2K/4K):
// the windower filled the fictional budget, and the final fit gate then
// refused the turn with a misleading reason. Now, when the fixed sections
// leave less room than the floor, history gets what is left (possibly only
// the current turn — the windower never drops the fresh user request), the
// optional sections (recall / history-refs / attachments) are dropped
// first, and only a genuinely un-fittable request is refused, with the
// exact measured reason.
func Assemble(in Input) Plan {
	budget := NewBudget(in.NumCtx, in.MaxOutputTokens)

	minHistory := in.MinHistoryTokens
	if minHistory <= 0 {
		minHistory = 2048
	}

	// v1.2.9: the history floor is BOUNDED BY THE REAL WINDOW. A
	// fixed 2048-token floor on a small effective context (2K/4K
	// windows, or a narrowed per-session policy) exceeded the budget
	// the model actually has and pushed every optional-section
	// decision into permanent drop mode. The floor is now capped at a
	// quarter of the usable window — on large windows the preference
	// stands unchanged; on small windows history gets its honest
	// share. (This complements the v1.2.8.1 adaptive-floor rule, which
	// already stopped RAISING the budget to the floor; the floor
	// itself is no longer larger than the window.)
	usable := budget.Usable
	if cap := usable / 4; minHistory > cap {
		minHistory = cap
	}

	// Phase 7: the safety margin trims the usable window; it can never
	// push the usable space below the output reserve itself.
	safety := in.SafetyMarginTokens
	if safety < 0 {
		safety = 0
	}
	if usable-safety < budget.ReserveOutput {
		safety = usable - budget.ReserveOutput
		if safety < 0 {
			safety = 0
		}
	}
	usable -= safety

	used := 0

	sections := make([]Section, 0, 6)

	// 1. output reserve (always accounted, never spent).
	sections = append(sections, Section{
		Name:     SectionReserve,
		Priority: PriorityReserve,
		Tokens:   budget.ReserveOutput,
		Budget:   budget.ReserveOutput,
		Included: true,
		Note:     "reserved for the model's reply",
	})

	// 2. system instructions — always included (clamped if pathological).
	sysOK := true
	sysNote := ""
	if in.SystemTokens > usable/2 {
		sysOK = false
		sysNote = "system briefing exceeds half the window — engine context too small"
	} else {
		used += in.SystemTokens
	}

	sections = append(sections, Section{
		Name:     SectionSystem,
		Priority: PrioritySystem,
		Tokens:   in.SystemTokens,
		Budget:   usable,
		Included: sysOK,
		Note:     sysNote,
	})

	// 3. tool schemas — always included (they enable the tool loop).
	used += in.ToolTokens

	sections = append(sections, Section{
		Name:     SectionTools,
		Priority: PriorityTools,
		Tokens:   in.ToolTokens,
		Budget:   usable,
		Included: true,
	})

	// 3.5 (v1.2.8) the rolling session summary — a REQUIRED section:
	// it is what makes history elision survivable, so it is budgeted
	// with the fixed sections rather than the optional ones. Only a
	// pathological summary (larger than half the window — impossible
	// under the sidecar caps) is refused.
	summaryOK := true
	summaryNote := ""
	if in.SummaryTokens > usable/2 {
		summaryOK = false
		summaryNote = "session summary exceeds half the window — omitted this turn"
	} else {
		used += in.SummaryTokens
	}

	sections = append(sections, Section{
		Name:     SectionSummary,
		Priority: PrioritySummary,
		Tokens:   in.SummaryTokens,
		Budget:   usable,
		Included: summaryOK,
		Note:     summaryNote,
	})

	// 4. history: whatever remains after fixed sections, bounded below by
	// the floor and (v1.2.5) above by the tier's share of the usable
	// window. Optional blocks (recall, attachments) do NOT inflate this
	// floor — under pressure they are dropped, not force-fitted.
	historyBudget := usable - used
	if historyBudget < 0 {
		historyBudget = 0
	}

	if in.MaxHistoryShare > 0 && in.MaxHistoryShare < 1 {
		shareCap := int(float64(usable) * in.MaxHistoryShare)
		if historyBudget > shareCap {
			historyBudget = shareCap
		}
	}

	// v1.2.8.1 adaptive floor: never promise history more tokens than
	// the remaining budget can honor. When the floor cannot be met,
	// historyBudget stays at the real remainder (>= 0); the optional
	// sections below see avail = historyBudget - minHistory <= 0 and
	// drop themselves in priority order.

	sections = append(sections, Section{
		Name:     SectionHistory,
		Priority: PriorityHistory,
		Budget:   historyBudget,
		Included: true,
	})

	// 5. recall — included when composed and it fits alongside the history
	// floor. Under pressure it is dropped first (its facts remain available
	// through the memory tool).
	recallOK := false
	recallNote := ""

	if in.RecallTokens > 0 {
		if in.RecallTokens <= historyBudget-minHistory {
			recallOK = true
		} else {
			recallNote = "dropped under context pressure — history keeps the budget"
		}
	}

	sections = append(sections, Section{
		Name:     SectionRecall,
		Priority: PriorityRecall,
		Tokens:   in.RecallTokens,
		Budget:   historyBudget,
		Included: recallOK,
		Note:     recallNote,
	})

	// 6. (v1.2.8) retrieved cross-mode history blocks — optional,
	// same treatment as recall: under pressure they are dropped FIRST
	// among the retrieval sections (the user can re-attach them; the
	// current turn cannot be re-derived).
	histRefOK := false
	histRefNote := ""

	if in.HistoryRefTokens > 0 {
		avail := historyBudget - minHistory
		if recallOK {
			avail -= in.RecallTokens
		}

		if in.HistoryRefTokens <= avail {
			histRefOK = true
		} else {
			histRefNote = "dropped under context pressure — re-attach the history reference"
		}
	}

	sections = append(sections, Section{
		Name:     SectionHistoryRefs,
		Priority: PriorityHistoryRefs,
		Tokens:   in.HistoryRefTokens,
		Budget:   historyBudget,
		Included: histRefOK,
		Note:     histRefNote,
	})

	// 7. attachments — same treatment as recall.
	attOK := false
	attNote := ""

	if in.AttachmentTokens > 0 {
		avail := historyBudget - minHistory
		if recallOK {
			avail -= in.RecallTokens
		}
		if histRefOK {
			avail -= in.HistoryRefTokens
		}

		if in.AttachmentTokens <= avail {
			attOK = true
		} else {
			attNote = "dropped under context pressure — retrieve fewer chunks"
		}
	}

	sections = append(sections, Section{
		Name:     SectionAttachments,
		Priority: PriorityAttachments,
		Tokens:   in.AttachmentTokens,
		Budget:   historyBudget,
		Included: attOK,
		Note:     attNote,
	})

	plan := Plan{
		Budget:        budget,
		Sections:      sections,
		HistoryBudget: historyBudget,
		SafetyMargin:  safety,
		Estimator:     chunking.EstimatorLabel(),
	}

	if recallOK || attOK || histRefOK {
		// Fixed sections + injected blocks must leave the floor for history;
		// the windower works within what actually remains.
		remaining := usable - in.SystemTokens - in.ToolTokens
		if summaryOK {
			remaining -= in.SummaryTokens
		}
		if recallOK {
			remaining -= in.RecallTokens
		}
		if histRefOK {
			remaining -= in.HistoryRefTokens
		}
		if attOK {
			remaining -= in.AttachmentTokens
		}
		if remaining < 0 {
			remaining = 0
		}
		// v1.2.8.1: no artificial floor raise here either — same
		// adaptive-floor rule as the primary allocation above. (When
		// any optional section was included, the avail chain already
		// guarantees remaining >= minHistory.)

		// v1.2.5: the tier share cap binds the recomputed budget too.
		if in.MaxHistoryShare > 0 && in.MaxHistoryShare < 1 {
			if shareCap := int(float64(usable) * in.MaxHistoryShare); remaining > shareCap && shareCap >= minHistory {
				remaining = shareCap
			}
		}

		plan.HistoryBudget = remaining
	}

	return plan
}

// AddAdjustment records one automatic context repair on the plan.
func (p *Plan) AddAdjustment(note string) {
	p.Adjustments = append(p.Adjustments, note)
}

// SectionTokens returns the measured token count of one section (0 when
// the section is absent — e.g. before assembly assigns it).
func (p Plan) SectionTokens(name string) int {
	for _, s := range p.Sections {
		if s.Name == name {
			return s.Tokens
		}
	}
	return 0
}

// EstimateTokens delegates to the shared estimator (single source of
// truth for token math).
func EstimateTokens(s string) int {
	return chunking.EstimateTokens(s)
}
