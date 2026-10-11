package computer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionHappyLoop(t *testing.T) {
	s := NewSession()

	plan := []Action{
		{Kind: KindObserve, Name: "extract", Risk: RiskReadOnly, Run: func(ctx context.Context) (Observation, error) {
			return Observation{Text: "page content: hello world"}, nil
		}},
		{Kind: KindAct, Name: "click", Risk: RiskInteractive, Run: func(ctx context.Context) (Observation, error) {
			return Observation{URL: "http://example.com/next"}, nil
		}},
		{Kind: KindVerify, Name: "verify-url", Risk: RiskReadOnly, Run: func(ctx context.Context) (Observation, error) {
			return Observation{URL: "http://example.com/next"}, nil
		}, Verify: VerifyURLPrefix("http://example.com/next")},
	}

	obs, err := s.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if len(obs) != 3 || !obs[0].OK || !obs[2].OK {
		t.Fatalf("observations: %+v", obs)
	}
}

func TestSessionDeniesDestructive(t *testing.T) {
	s := NewSession()
	called := false

	_, err := s.Run(context.Background(), []Action{
		{Kind: KindAct, Name: "delete-everything", Risk: RiskDestructive, Run: func(ctx context.Context) (Observation, error) {
			called = true
			return Observation{}, nil
		}},
	})
	if err == nil {
		t.Fatal("destructive action must be denied by default")
	}
	if called {
		t.Fatal("denied action must never execute")
	}
	if !contains(err.Error(), "risk policy") {
		t.Fatalf("denial should name the risk policy: %v", err)
	}
}

func TestSessionPermissionGateOverride(t *testing.T) {
	s := NewSession()
	s.Gate = allowAll{}

	_, err := s.Run(context.Background(), []Action{
		{Kind: KindAct, Name: "special", Risk: RiskDestructive, Run: func(ctx context.Context) (Observation, error) {
			return Observation{Text: "done"}, nil
		}},
	})
	if err != nil {
		t.Fatalf("explicit permission must allow: %v", err)
	}
}

type allowAll struct{}

func (allowAll) Allow(Risk, string) bool { return true }

func TestSessionVerifyFailureStops(t *testing.T) {
	s := NewSession()

	obs, err := s.Run(context.Background(), []Action{
		{Kind: KindAct, Name: "click", Risk: RiskInteractive, Run: func(ctx context.Context) (Observation, error) {
			return Observation{URL: "http://wrong.example.com"}, nil
		}},
		{Kind: KindVerify, Name: "verify-url", Risk: RiskReadOnly, Run: func(ctx context.Context) (Observation, error) {
			return Observation{URL: "http://wrong.example.com"}, nil
		}, Verify: VerifyURLPrefix("http://expected.example.com")},
		{Kind: KindAct, Name: "never", Risk: RiskInteractive, Run: func(ctx context.Context) (Observation, error) {
			t.Fatal("post-verification-failure step must not run")
			return Observation{}, nil
		}},
	})

	if err == nil {
		t.Fatal("verification failure must surface")
	}
	if len(obs) != 2 {
		t.Fatalf("plan must stop after verification failure, got %d observations", len(obs))
	}
}

func TestSessionStepTimeout(t *testing.T) {
	s := NewSession()
	s.DefaultTimeout = 100 * time.Millisecond

	_, err := s.Run(context.Background(), []Action{
		{Kind: KindObserve, Name: "slow", Risk: RiskReadOnly, Run: func(ctx context.Context) (Observation, error) {
			<-ctx.Done()
			return Observation{}, ctx.Err()
		}},
	})
	if err == nil {
		t.Fatal("a step exceeding its timeout must fail")
	}
}

func TestSessionCancellation(t *testing.T) {
	s := NewSession()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Run(ctx, []Action{
		{Kind: KindObserve, Name: "x", Risk: RiskReadOnly, Run: func(ctx context.Context) (Observation, error) {
			return Observation{}, nil
		}},
	})
	if err == nil || !contains(err.Error(), "canceled") {
		t.Fatalf("cancellation must be honored: %v", err)
	}
}

func TestSessionMaxStepsBound(t *testing.T) {
	s := NewSession()
	s.MaxSteps = 2

	obs, err := s.Run(context.Background(), []Action{
		{Kind: KindObserve, Name: "1", Risk: RiskReadOnly, Run: func(ctx context.Context) (Observation, error) { return Observation{}, nil }},
		{Kind: KindObserve, Name: "2", Risk: RiskReadOnly, Run: func(ctx context.Context) (Observation, error) { return Observation{}, nil }},
		{Kind: KindObserve, Name: "3", Risk: RiskReadOnly, Run: func(ctx context.Context) (Observation, error) {
			return Observation{}, errors.New("beyond the bound")
		}},
	})
	if err != nil {
		t.Fatalf("bound must truncate silently: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("steps executed = %d, want 2 (the bound)", len(obs))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
