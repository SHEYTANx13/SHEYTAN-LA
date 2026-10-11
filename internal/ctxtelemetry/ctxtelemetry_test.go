package ctxtelemetry

import (
	"testing"
)

func TestRecordAndSummarize(t *testing.T) {
	store := NewStore(t.TempDir())

	store.Record(TurnRecord{
		SessionID:          "s1",
		BudgetTokens:       8192,
		UsedTokens:         4096,
		Pressure:           0.5,
		TokensAdded:        600,
		TokensRemoved:      2400,
		CompressionRatio:   0.8,
		RetrievalLatencyMs: 12,
		RetrievalHits:      3,
		ToolCalls:          4,
		ToolSuccesses:      3,
	})
	store.Record(TurnRecord{
		SessionID:     "s1",
		BudgetTokens:  8192,
		UsedTokens:    2048,
		Pressure:      0.25,
		ToolCalls:     2,
		ToolSuccesses: 2,
	})

	sum := store.Summarize()
	if sum.Turns != 2 {
		t.Fatalf("turns = %d", sum.Turns)
	}
	if sum.AvgPressure < 0.36 || sum.AvgPressure > 0.38 {
		t.Fatalf("avg pressure = %f", sum.AvgPressure)
	}
	if sum.AvgRetrievalMs != 6 {
		t.Fatalf("avg retrieval ms = %f", sum.AvgRetrievalMs)
	}
	if sum.RetrievedTurnRate != 0.5 {
		t.Fatalf("retrieved turn rate = %f", sum.RetrievedTurnRate)
	}
	if sum.ToolSuccessRate < 0.874 || sum.ToolSuccessRate > 0.876 {
		t.Fatalf("tool success rate = %f", sum.ToolSuccessRate)
	}
}

func TestRecentAndBound(t *testing.T) {
	store := NewStore(t.TempDir())
	for i := 0; i < 20; i++ {
		store.Record(TurnRecord{SessionID: "s"})
	}
	recent := store.Recent(5)
	if len(recent) != 5 {
		t.Fatalf("recent = %d", len(recent))
	}

	// Bound test: pre-populate a store beyond maxRecs, record once, and
	// verify the compaction halves the store instead of growing forever.
	over := NewStore(t.TempDir())
	for i := 0; i < 4200; i++ {
		over.Record(TurnRecord{SessionID: "s", Turn: i})
	}
	after := len(over.Recent(0))
	if after >= 4200 {
		t.Fatalf("store unbounded: %d records", after)
	}
	if after < 2000 {
		t.Fatalf("compaction dropped too much: %d", after)
	}
}

func TestEmptySummary(t *testing.T) {
	store := NewStore(t.TempDir())
	sum := store.Summarize()
	if sum.Turns != 0 {
		t.Fatal("empty store must summarize to zero turns")
	}
}
