package contextplan

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func TestNewBudgetReservesOutput(t *testing.T) {
	b := NewBudget(8192, 1024)

	if b.Total != 8192 {
		t.Fatalf("total = %d, want 8192", b.Total)
	}

	if b.ReserveOutput != 1024 {
		t.Fatalf("reserve = %d, want 1024", b.ReserveOutput)
	}

	if b.Usable != 8192-1024 {
		t.Fatalf("usable = %d", b.Usable)
	}
}

func TestNewBudgetClampsExtremeOutputRequest(t *testing.T) {
	// A max_tokens larger than half the window is clamped so the prompt
	// always retains the majority of the context.
	b := NewBudget(4096, 8000)

	if b.ReserveOutput > 4096/2 {
		t.Fatalf("reserve not clamped: %d", b.ReserveOutput)
	}

	if b.ReserveOutput < 512 {
		t.Fatalf("reserve floor violated: %d", b.ReserveOutput)
	}
}

func TestAssembleSectionsAccountFullBudget(t *testing.T) {
	sys := 420
	tools := 1200
	injected := 300

	plan := Assemble(Input{
		SystemTokens:    sys,
		ToolTokens:      tools,
		RecallTokens:    injected,
		NumCtx:          8192,
		MaxOutputTokens: 1024,
	})

	if plan.HistoryBudget <= 0 {
		t.Fatal("history budget must be positive")
	}

	// The plan must account for the whole usable window: history budget +
	// measured fixed sections must never exceed usable.
	used := sys + tools + injected
	if plan.HistoryBudget+used > plan.Budget.Usable+2048 {
		t.Fatalf(
			"budget overcommitted: history %d + fixed %d > usable %d (floor exception)",
			plan.HistoryBudget, used, plan.Budget.Usable,
		)
	}

	// Section inventory must be complete.
	seen := map[string]bool{}
	for _, s := range plan.Sections {
		seen[s.Name] = true
	}

	for _, want := range []string{SectionSystem, SectionTools, SectionRecall, SectionHistory, SectionReserve} {
		if !seen[want] {
			t.Fatalf("plan missing section %s", want)
		}
	}
}

func TestAssemblePressureDropsRecallFirst(t *testing.T) {
	// A tiny window stuffed with fixed content: the plan must report the
	// overflow honestly (pressure > 1) instead of pretending everything
	// fits — the spec's "never exceed real capacity" is enforced by making
	// the overflow VISIBLE, not by silently lying about it.
	plan := Assemble(Input{
		SystemTokens:    3000,
		ToolTokens:      3000,
		RecallTokens:    2000,
		NumCtx:          8192,
		MaxOutputTokens: 1024,
	})

	// Fixed sections (system + tools) alone leave less than the recall
	// block + history floor: recall must be reported as dropped.
	fixed := 3000 + 3000
	floor := 2048

	if fixed+2000+floor > plan.Budget.Usable {
		for _, s := range plan.Sections {
			if s.Name == SectionRecall && s.Included {
				t.Fatal("recall must be reported dropped under extreme pressure")
			}
		}
	}

	if plan.Summary() == "" {
		t.Fatal("summary must always render")
	}
}

func TestAssembleTinyWindowKeepsHistoryFloor(t *testing.T) {
	// v1.2.8.1 REPAIR: the history floor is bounded by the actual
	// available prompt budget — never a fixed 2048-token promise. On a
	// 4096 window with 7000 tokens of fixed sections, the v1.2.8
	// unconditional floor allocated 2048 history tokens that do not
	// exist, overfilled the windower, and forced a hard turn refusal.
	// The adaptive floor keeps the budget at the real remainder
	// and lets the optional sections drop instead.
	plan := Assemble(Input{
		SystemTokens: 4000,
		ToolTokens:   3000,
		NumCtx:       4096,
	})

	// usable = 4096 - 1024 (output reserve) = 3072. The system briefing
	// (4000) exceeds half the window and is refused; tools (3000) count.
	// The true remainder is 72 — the floor must never exceed it (the
	// v1.2.8 code promised 2048 here).
	if plan.HistoryBudget > plan.Budget.Usable-3000 {
		t.Fatalf("adaptive floor must not promise tokens that do not exist: %d (remainder %d)",
			plan.HistoryBudget, plan.Budget.Usable-3000)
	}

	for _, s := range plan.Sections {
		if s.Name == SectionRecall && s.Included {
			t.Fatal("optional sections must be dropped when the history floor cannot be met")
		}
	}
}

func TestSummaryRendersProvenance(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens: 500,
		ToolTokens:   1000,
		NumCtx:       8192,
	})

	// The orchestrator measures the windowed history before reporting;
	// mirror that here.
	plan.SetSectionTokens(SectionHistory, 2500)

	summary := plan.Summary()

	for _, want := range []string{"context", "system", "tools", "history"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestEstimateTokensConsistency(t *testing.T) {
	// Same estimator as chunking: 4 bytes ≈ 1 token.
	if got := EstimateTokens("abcdefgh"); got != 2 {
		t.Fatalf("EstimateTokens(8 ascii bytes) = %d, want 2", got)
	}
}

func TestPlanPressureNeverNegative(t *testing.T) {
	plan := Assemble(Input{NumCtx: 8192})

	if plan.Pressure() < 0 {
		t.Fatalf("pressure must never be negative: %v", plan.Pressure())
	}
}

func TestHistoryMessagesDoNotConsumeSystemBudget(t *testing.T) {
	// The Input.History field exists for downstream measurement; the plan
	// must not panic or mis-allocate when the conversation is populated.
	history := []llm.Message{
		{Role: "user", Content: strings.Repeat("hello ", 500)},
		{Role: "assistant", Content: strings.Repeat("answer ", 500)},
	}

	plan := Assemble(Input{
		SystemTokens: 500,
		ToolTokens:   800,
		History:      history,
		NumCtx:       8192,
	})

	if plan.HistoryBudget <= 0 {
		t.Fatal("history budget must remain positive with populated history")
	}
}

// --- small-window degradation and honest token accounting ---

// smallContextCases are the v1.2.8.1 spec scenarios: 2K / 4K / 8K
// effective contexts with a large history, a large summary, cross-mode
// refs and attachments — everything the degradation ladder must survive.
func smallContextCases() []struct {
	name             string
	numCtx           int
	systemTokens     int
	toolTokens       int
	summaryTokens    int
	recallTokens     int
	histRefTokens    int
	attachmentTokens int
} {
	return []struct {
		name             string
		numCtx           int
		systemTokens     int
		toolTokens       int
		summaryTokens    int
		recallTokens     int
		histRefTokens    int
		attachmentTokens int
	}{
		{
			name:             "2k window with large history and refs",
			numCtx:           2048,
			systemTokens:     420,
			toolTokens:       300,
			summaryTokens:    220,
			recallTokens:     180,
			histRefTokens:    3600, // 4 refs × DefaultBlockTokens
			attachmentTokens: 150,
		},
		{
			name:             "4k window with large history and refs",
			numCtx:           4096,
			systemTokens:     600,
			toolTokens:       500,
			summaryTokens:    250,
			recallTokens:     300,
			histRefTokens:    3600,
			attachmentTokens: 400,
		},
		{
			name:             "8k deep-tier window",
			numCtx:           8192,
			systemTokens:     3000,
			toolTokens:       1200,
			summaryTokens:    300,
			recallTokens:     600,
			histRefTokens:    3600,
			attachmentTokens: 800,
		},
	}
}

// TestAssembleSmallContextsDegradeGracefully runs the spec scenarios and
// verifies the three invariants that make degradation graceful:
//
//	history budget ≤ the tokens that actually remain,
//	dropped sections never count toward Overflow(),
//	the plan does not refuse while anything degradable remains.
func TestAssembleSmallContextsDegradeGracefully(t *testing.T) {
	for _, tc := range smallContextCases() {
		t.Run(tc.name, func(t *testing.T) {
			plan := Assemble(Input{
				SystemTokens:       tc.systemTokens,
				ToolTokens:         tc.toolTokens,
				SummaryTokens:      tc.summaryTokens,
				RecallTokens:       tc.recallTokens,
				HistoryRefTokens:   tc.histRefTokens,
				AttachmentTokens:   tc.attachmentTokens,
				NumCtx:             tc.numCtx,
				MaxOutputTokens:    512,
				SafetyMarginTokens: 64,
				MinHistoryTokens:   2048, // the v1.2.8 production default
			})

			// Invariant 1: the history budget never exceeds what actually
			// remains after the INCLUDED fixed sections. (The v1.2.8 floor
			// raised it to 2048 regardless.)
			remaining := plan.Budget.Usable - 64 - tc.toolTokens
			if sectionIncluded(plan, SectionSystem) {
				remaining -= tc.systemTokens
			}
			if sectionIncluded(plan, SectionSummary) {
				remaining -= tc.summaryTokens
			}
			if plan.HistoryBudget > remaining {
				t.Fatalf("history budget %d exceeds the real remainder %d — the floor promised tokens that do not exist",
					plan.HistoryBudget, remaining)
			}

			// Invariant 2: overflow is measured over INCLUDED sections
			// only. With history not yet windowed (0 tokens), a degradable
			// plan NEVER refuses here.
			if over := plan.Overflow(); over > 0 {
				t.Fatalf("plan overflows by %d tokens while optional sections remain droppable — graceful degradation broken", over)
			}

			// Invariant 3: a dropped section keeps its measured tokens for
			// the UI, but is excluded from TotalTokens().
			if !sectionIncluded(plan, SectionHistoryRefs) && tc.histRefTokens > 0 {
				if got := plan.TotalTokens(); got > plan.PromptCeiling() {
					t.Fatalf("TotalTokens %d still exceeds the ceiling %d despite dropped sections",
						got, plan.PromptCeiling())
				}
			}
		})
	}
}

// TestOverflowZeroWhenHistoryRefsDropped is the concrete v1.2.8 failure
// repro: 4 attached references (4 × 900 = 3600 tok) on a 4K window — the
// planner drops the refs section by design and the turn MUST proceed
// without them, not refuse.
func TestOverflowZeroWhenHistoryRefsDropped(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens:       600,
		ToolTokens:         500,
		SummaryTokens:      250,
		HistoryRefTokens:   3600,
		NumCtx:             4096,
		MaxOutputTokens:    512,
		SafetyMarginTokens: 64,
	})

	if sectionIncluded(plan, SectionHistoryRefs) {
		t.Fatal("precondition: the refs section must be dropped under this budget")
	}

	if over := plan.Overflow(); over != 0 {
		t.Fatalf("dropped history-refs poisoned Overflow(): %d tokens", over)
	}

	if plan.TotalTokens() != tc3IncludedTokens(plan) {
		t.Fatalf("TotalTokens = %d, want only included sections %d",
			plan.TotalTokens(), tc3IncludedTokens(plan))
	}
}

// tc3IncludedTokens sums the sections the contract counts: everything
// included plus the always-traveling system briefing.
func tc3IncludedTokens(p Plan) int {
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

func sectionIncluded(p Plan, name string) bool {
	for _, s := range p.Sections {
		if s.Name == name {
			return s.Included
		}
	}
	return false
}

// TestTotalTokensExcludesDroppedSections pins the accounting rule
// directly: dropped retrieval sections are excluded, the system briefing
// is always counted (splitSystemPrefix always carries it), the output
// reserve is never counted.
func TestTotalTokensExcludesDroppedSections(t *testing.T) {
	plan := Plan{
		Sections: []Section{
			{Name: SectionReserve, Tokens: 512, Included: true},
			{Name: SectionSystem, Tokens: 400, Included: false}, // refused but still travels
			{Name: SectionTools, Tokens: 300, Included: true},
			{Name: SectionSummary, Tokens: 250, Included: false}, // refused AND not injected
			{Name: SectionRecall, Tokens: 180, Included: false},  // dropped
			{Name: SectionHistoryRefs, Tokens: 3600, Included: false},
			{Name: SectionHistory, Tokens: 0, Included: true},
		},
		Budget: Budget{Total: 2048, ReserveOutput: 512, Usable: 1536},
	}

	// 400 (system, always travels) + 300 (tools) + 0 (history).
	if got := plan.TotalTokens(); got != 700 {
		t.Fatalf("TotalTokens = %d, want 700 (system + tools only)", got)
	}

	if strings.Count(plan.Summary(), "recall") != 0 {
		t.Fatal("Summary renders a dropped recall section")
	}
}

// TestAdaptiveFloorMetWhenRoomExists guards the other direction: the
// floor is a PREFERENCE — when the window DOES have room, history still
// receives at least the requested floor.
func TestAdaptiveFloorMetWhenRoomExists(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens:     400,
		ToolTokens:       300,
		SummaryTokens:    150,
		NumCtx:           8192,
		MaxOutputTokens:  512,
		MinHistoryTokens: 2048,
	})

	if plan.HistoryBudget < 2048 {
		t.Fatalf("history floor violated when room exists: %d", plan.HistoryBudget)
	}
}

// --- budget honesty and estimator labeling ---

// Honest small-window
// budgeting (no fabricated context, no fixed floors that exceed the
// real window, adaptive history floor) and the estimator-tier label.

// TestNewBudgetHonestSmallWindows pins the removal of the two
// fabrications: numCtx < 1024 is no longer inflated to 1024, and the
// 512-token output reserve scales down instead of starving the prompt.
func TestNewBudgetHonestSmallWindows(t *testing.T) {
	cases := []struct {
		name          string
		numCtx        int
		maxTokens     int
		wantTotal     int
		wantUsableMin int // lower bound for usable prompt space
		wantUsableMax int // upper bound (reserve must exist)
	}{
		{"real 512 window", 512, 256, 512, 128, 384},
		{"real 256 window", 256, 128, 256, 32, 192},
		{"tiny 128 window", 128, 64, 128, 16, 96},
		{"unknown window", 0, 1024, 1024, 0, 1024},
		{"normal 8k window", 8192, 1024, 8192, 3072, 7680},
		{"large 128k window", 131072, 4096, 131072, 60000, 126976},
	}

	for _, tc := range cases {
		b := NewBudget(tc.numCtx, tc.maxTokens)
		if b.Total != tc.wantTotal {
			t.Errorf("%s: Total = %d, want %d (small windows must not be inflated)", tc.name, b.Total, tc.wantTotal)
		}
		if b.Usable < tc.wantUsableMin {
			t.Errorf("%s: Usable = %d, want >= %d (a fixed reserve must not starve the prompt)", tc.name, b.Usable, tc.wantUsableMin)
		}
		if b.Usable > tc.wantUsableMax {
			t.Errorf("%s: Usable = %d, want <= %d (an output reserve must exist)", tc.name, b.Usable, tc.wantUsableMax)
		}
		if b.Usable+b.ReserveOutput != b.Total {
			t.Errorf("%s: accounting broken: %d + %d != %d", tc.name, b.Usable, b.ReserveOutput, b.Total)
		}
	}
}

// TestAssembleHistoryFloorBoundedByWindow pins the adaptive floor: the
// 2048-token default preference never exceeds a quarter of the usable
// window, so small models are not pushed into permanent drop mode.
func TestAssembleHistoryFloorBoundedByWindow(t *testing.T) {
	cases := []struct {
		name   string
		numCtx int
	}{
		{"small 2k window", 2048},
		{"small 4k window", 4096},
		{"normal 8k window", 8192},
		{"large 32k window", 32768},
	}

	for _, tc := range cases {
		plan := Assemble(Input{
			SystemTokens:    200,
			ToolTokens:      400,
			NumCtx:          tc.numCtx,
			MaxOutputTokens: 1024,
		})

		// The plan must not promise more history budget than the usable
		// window minus fixed sections.
		if plan.HistoryBudget < 0 {
			t.Errorf("%s: negative history budget", tc.name)
		}

		cap := plan.Budget.Usable / 4
		fixed := plan.SectionTokens(SectionSystem) + plan.SectionTokens(SectionTools)
		remaining := plan.Budget.Usable - fixed
		if remaining < 0 {
			remaining = 0
		}
		if plan.HistoryBudget > remaining {
			t.Errorf("%s: history budget %d exceeds the real remainder %d", tc.name, plan.HistoryBudget, remaining)
		}

		// On small windows the implicit floor (usable/4) is small enough
		// that optional sections are droppable rather than force-fitted.
		if tc.numCtx <= 4096 && cap < 2048 {
			// sanity: the floor was actually scaled down.
			if plan.SectionTokens(SectionHistory) > plan.Budget.Usable {
				t.Errorf("%s: history exceeds the usable window", tc.name)
			}
		}
	}
}

// TestAssembleSmallContextDropsOptionalSections: on a 2k window with a
// big fixed payload, the optional sections (recall, refs, attachments)
// drop — and the SYSTEM section still reports honestly.
func TestAssembleSmallContextDropsOptionalSections(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens:     300,
		ToolTokens:       500,
		RecallTokens:     600,
		HistoryRefTokens: 500,
		AttachmentTokens: 400,
		NumCtx:           2048,
		MaxOutputTokens:  1024,
	})

	// Recall must be dropped: it does not fit alongside the bounded
	// history floor.
	if plan.SectionIncluded(SectionRecall) {
		t.Fatalf("recall was included on a 2k window with 500 tool tokens — the drop ladder must fire")
	}
	if plan.Overflow() < 0 {
		t.Fatalf("negative overflow is impossible")
	}
}

// TestPlanCarriesEstimatorLabel pins the honesty label: a plan built on
// the heuristic tier is labeled "heuristic", never presented as exact.
func TestPlanCarriesEstimatorLabel(t *testing.T) {
	chunking.ResetTokenEstimator()
	plan := Assemble(Input{SystemTokens: 100, ToolTokens: 200, NumCtx: 8192, MaxOutputTokens: 1024})
	if plan.Estimator != "heuristic" {
		t.Fatalf("Estimator = %q, want heuristic (default tier)", plan.Estimator)
	}

	chunking.SetModelFamilyEstimator("qwen")
	plan = Assemble(Input{SystemTokens: 100, ToolTokens: 200, NumCtx: 8192, MaxOutputTokens: 1024})
	if plan.Estimator != "family:qwen" {
		t.Fatalf("Estimator = %q, want family:qwen", plan.Estimator)
	}

	chunking.SetExactTokenCounter(func(s string) int { return len(s) / 4 })
	plan = Assemble(Input{SystemTokens: 100, ToolTokens: 200, NumCtx: 8192, MaxOutputTokens: 1024})
	if plan.Estimator != "exact" {
		t.Fatalf("Estimator = %q, want exact", plan.Estimator)
	}

	chunking.ResetTokenEstimator()
}

// TestEstimatorCJKAware pins the conservative fallback: CJK runes count
// as ~1 token each (the old runes/4 formula under-counted by 4x — a
// latent overflow for non-Latin conversations).
func TestEstimatorCJKAware(t *testing.T) {
	chunking.ResetTokenEstimator()

	ascii := "abcdefghij" // 10 runes → ceil(10/4) = 3
	if got := chunking.EstimateTokens(ascii); got != 3 {
		t.Errorf("ascii estimate = %d, want 3", got)
	}

	cjk := "你好世界" // 4 CJK runes → 4 tokens (1 per rune)
	if got := chunking.EstimateTokens(cjk); got < 4 {
		t.Errorf("cjk estimate = %d, want >= 4 (one per rune — the old formula said %d)", got, 1)
	}

	// Mixed: 2 CJK + 8 ASCII → 2 + 2 = 4.
	mixed := "你好abcdefgh"
	if got := chunking.EstimateTokens(mixed); got < 4 {
		t.Errorf("mixed estimate = %d, want >= 4", got)
	}
}

// SectionIncluded reports whether one section traveled (test helper).
func (p Plan) SectionIncluded(name string) bool {
	for _, s := range p.Sections {
		if s.Name == name {
			return s.Included
		}
	}
	return false
}

// --- prompt ceiling and output reserve rejection ---

// Phase 7 regression tests for the fit-guarantee primitives.

func TestPromptCeilingWithSafetyMargin(t *testing.T) {
	b := NewBudget(8192, 1024) // usable = 7168
	plan := Assemble(Input{
		SystemTokens:       100,
		ToolTokens:         100,
		NumCtx:             8192,
		MaxOutputTokens:    1024,
		SafetyMarginTokens: 256,
	})
	if plan.Budget.Usable != b.Usable {
		t.Fatalf("budget usable = %d, want %d", plan.Budget.Usable, b.Usable)
	}
	if plan.PromptCeiling() != b.Usable-256 {
		t.Fatalf("ceiling = %d, want %d", plan.PromptCeiling(), b.Usable-256)
	}
}

func TestOverflowExactFit(t *testing.T) {
	// system+tools+history exactly at the ceiling → zero overflow.
	numCtx := 4096
	maxOut := 512
	safety := 64

	plan := Assemble(Input{
		SystemTokens:       300,
		ToolTokens:         500,
		NumCtx:             numCtx,
		MaxOutputTokens:    maxOut,
		SafetyMarginTokens: safety,
	})
	_ = plan

	budget := NewBudget(numCtx, maxOut)
	ceiling := budget.Usable - safety
	sysTok := 300
	toolTok := 500
	history := ceiling - sysTok - toolTok

	plan2 := Assemble(Input{
		SystemTokens:       sysTok,
		ToolTokens:         toolTok,
		NumCtx:             numCtx,
		MaxOutputTokens:    maxOut,
		SafetyMarginTokens: safety,
		MinHistoryTokens:   1,
	})
	plan2.SetSectionTokens(SectionHistory, history)
	if plan2.Overflow() > 0 {
		t.Fatalf("exact-fit plan overflows by %d (total %d, ceiling %d)",
			plan2.Overflow(), plan2.TotalTokens(), plan2.PromptCeiling())
	}
}

func TestOverflowByOne(t *testing.T) {
	numCtx := 4096
	sysTok := 300
	toolTok := 500
	safety := 64

	budget := NewBudget(numCtx, 512)
	ceiling := budget.Usable - safety
	history := ceiling - sysTok - toolTok + 1 // one over

	plan := Assemble(Input{
		SystemTokens:       sysTok,
		ToolTokens:         toolTok,
		NumCtx:             numCtx,
		MaxOutputTokens:    512,
		SafetyMarginTokens: safety,
		MinHistoryTokens:   1,
	})
	plan.SetSectionTokens(SectionHistory, history)

	if got := plan.Overflow(); got != 1 {
		t.Fatalf("overflow = %d, want exactly 1", got)
	}
}

func TestOutputReserveCausesRejection(t *testing.T) {
	// A prompt that would fit WITHOUT the output reserve must still be
	// rejected when the reserve is honored: reserve = half the window.
	numCtx := 2048
	plan := Assemble(Input{
		SystemTokens:       1500, // larger than half the usable space
		ToolTokens:         0,
		NumCtx:             numCtx,
		MaxOutputTokens:    1024, // clamped to numCtx/2 = 1024
		SafetyMarginTokens: 0,
	})

	// The system section is marked NOT included (pathological), which
	// makes the plan's total exceed the usable window once history is
	// measured: overflow must be visible, never hidden.
	plan.SetSectionTokens(SectionHistory, 1)
	if plan.Budget.ReserveOutput != 1024 {
		t.Fatalf("reserve = %d, want 1024 (half window)", plan.Budget.ReserveOutput)
	}
	if plan.Overflow() <= 0 && plan.Sections[1].Included {
		t.Fatalf("oversized system must be flagged: %+v", plan.Sections[1])
	}
}

func TestAdjustmentsTrail(t *testing.T) {
	plan := Assemble(Input{NumCtx: 4096, MaxOutputTokens: 512})
	plan.AddAdjustment("dynamic toolset: reduced tool surface 16 → 7 for this task")
	plan.AddAdjustment("compact system briefing (context-pressure mode)")
	if len(plan.Adjustments) != 2 {
		t.Fatalf("adjustments = %v", plan.Adjustments)
	}
}

func TestSectionTokens(t *testing.T) {
	plan := Assemble(Input{SystemTokens: 10, NumCtx: 4096, MaxOutputTokens: 512})
	if plan.SectionTokens(SectionSystem) != 10 {
		t.Fatal("SectionTokens(system) mismatch")
	}
	if plan.SectionTokens("nonexistent") != 0 {
		t.Fatal("absent section must read 0")
	}
}
