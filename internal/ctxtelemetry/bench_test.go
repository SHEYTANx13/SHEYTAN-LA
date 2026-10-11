package ctxtelemetry

import (
	"path/filepath"
	"testing"
)

// BenchmarkRecordSteady measures the per-turn record path once the store has
// accumulated ~2000 records. Baseline v1.2.3 re-reads + re-marshals + rewrites
// the whole JSONL file on every Record call.
func BenchmarkRecordSteady(b *testing.B) {
	st := NewStore(b.TempDir())
	// Seed 2000 records so the file is at a realistic steady state.
	for i := 0; i < 2000; i++ {
		st.Record(TurnRecord{
			SessionID:     "seed",
			UsedTokens:    12000,
			BudgetTokens:  16384,
			TokensHistory: 9000,
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st.Record(TurnRecord{
			SessionID:     "bench",
			UsedTokens:    12000,
			BudgetTokens:  16384,
			TokensHistory: 9500,
		})
	}
}

// BenchmarkRecentSteady measures the read path used by the UI summary.
func BenchmarkRecentSteady(b *testing.B) {
	st := NewStore(filepath.Join(b.TempDir(), "recent"))
	for i := 0; i < 2000; i++ {
		st.Record(TurnRecord{SessionID: "seed", UsedTokens: 12000})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st.Recent(50)
	}
}
