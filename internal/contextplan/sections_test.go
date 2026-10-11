// sections_test.go — v1.2.8 contextplan summary + history-refs sections.
package contextplan

import "testing"

func TestSummaryIsARequiredFixedSection(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens:     800,
		ToolTokens:       600,
		SummaryTokens:    300,
		NumCtx:           8192,
		MaxOutputTokens:  1024,
		MinHistoryTokens: 2048,
	})

	section := findSection(t, plan, SectionSummary)

	if !section.Included {
		t.Fatalf("summary section must be included, note=%q", section.Note)
	}

	if section.Tokens != 300 {
		t.Fatalf("summary tokens = %d, want 300", section.Tokens)
	}

	if section.Priority != PrioritySummary {
		t.Fatalf("summary priority = %d, want %d", section.Priority, PrioritySummary)
	}

	// The summary is budgeted with the FIXED sections: history receives
	// usable − system − tools − summary (never reduced by optional blocks
	// but also never double-counted).
	history := findSection(t, plan, SectionHistory)
	fixedUsed := 800 + 600 + 300
	want := plan.Budget.Usable - plan.SafetyMargin - fixedUsed
	if history.Budget < want-1 || history.Budget > want+1 {
		t.Fatalf("history budget = %d, want ~%d (summary must consume fixed budget)", history.Budget, want)
	}
}

func TestHistoryRefsOptionalDroppedUnderPressure(t *testing.T) {
	// A tiny window: fixed sections + history floor leave no room for the
	// history-refs block.
	plan := Assemble(Input{
		SystemTokens:     900,
		ToolTokens:       700,
		SummaryTokens:    200,
		HistoryRefTokens: 4000,
		NumCtx:           4096,
		MaxOutputTokens:  512,
		MinHistoryTokens: 2048,
	})

	refs := findSection(t, plan, SectionHistoryRefs)
	if refs.Included {
		t.Fatal("history-refs must be dropped when the history floor cannot be kept")
	}

	if refs.Note == "" {
		t.Fatal("a dropped section must carry its honest note")
	}

	// The summary SURVIVES the same pressure (required section).
	summary := findSection(t, plan, SectionSummary)
	if !summary.Included {
		t.Fatal("the required summary must not be dropped before optional sections")
	}
}

func TestHistoryRefsIncludedWhenRoomAllows(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens:     500,
		ToolTokens:       400,
		SummaryTokens:    200,
		HistoryRefTokens: 300,
		NumCtx:           16384,
		MaxOutputTokens:  1024,
		MinHistoryTokens: 2048,
	})

	refs := findSection(t, plan, SectionHistoryRefs)
	if !refs.Included {
		t.Fatalf("history-refs should fit here, note=%q", refs.Note)
	}

	if plan.HistoryBudget <= 0 {
		t.Fatal("history budget collapsed")
	}
}

func TestClassifyPressureLadder(t *testing.T) {
	cases := map[float64]string{
		0.0:  PressureOK,
		0.49: PressureOK,
		0.55: PressureWarm,
		0.74: PressureWarm,
		0.80: PressureHigh,
		0.89: PressureHigh,
		0.91: PressureCritical,
		1.20: PressureCritical,
	}

	for pct, want := range cases {
		if got := ClassifyPressure(pct); got != want {
			t.Fatalf("ClassifyPressure(%0.2f) = %q, want %q", pct, got, want)
		}
	}
}

func findSection(t *testing.T, plan Plan, name string) Section {
	t.Helper()
	for _, s := range plan.Sections {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("section %q missing from the plan", name)
	return Section{}
}
