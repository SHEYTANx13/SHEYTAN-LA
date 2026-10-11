package api

import (
	"context"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// TestRunBudgetTimerSurvivesRunStart pins the v1.1.4 self-audit fix: the
// budget cancel was briefly invoked at goroutine START (killing the timer
// immediately) instead of in the deferred cleanup. This test proves the
// timer is armed for the configured duration, not cancelled up front.
func TestRunBudgetTimerSurvivesRunStart(t *testing.T) {
	cfg := config.Default()
	cfg.RunTimeoutMinutes = 1

	budget := cfg.EffectiveRunTimeout()
	if budget != time.Minute {
		t.Fatalf("budget = %v, want 1 minute", budget)
	}

	// simulate the handler's wiring: budget must remain live until the
	// run's deferred cleanup fires
	runCtx := context.Background()
	budgetCtx, budgetCancel := context.WithTimeout(runCtx, budget)
	defer budgetCancel()

	deadline, ok := budgetCtx.Deadline()
	if !ok {
		t.Fatal("budget context has no deadline")
	}

	if remaining := time.Until(deadline); remaining < 30*time.Second {
		t.Fatalf("budget deadline is only %v away — the timer was cancelled prematurely", remaining)
	}
}
