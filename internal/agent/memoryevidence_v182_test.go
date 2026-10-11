package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/contextplan"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// v1.8.2 MEMORY EVIDENCE: every turn publishes the `context` activity
// with a plan.MemoryEvidence record composed from the injection facts.
// The record must be present, truthful for a plain chat (no recall ran,
// nothing injected), and the record rides the SAME plan the UI already
// consumes — no second memory authority.
func TestContextActivityCarriesMemoryEvidence(t *testing.T) {
	var got *contextplan.MemoryEvidence

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		var b strings.Builder

		b.WriteString(sseChunk("Hello!"))
		b.WriteString(sseDone)

		return b.String()
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	_, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "hi"},
	}, func(a Activity) {
		if a.Type != "context" {
			return
		}

		if plan, ok := a.Detail.(contextplan.Plan); ok && plan.Memory != nil {
			got = plan.Memory
		}
	}, WithSessionSummaryBlock("The user greeted the agent earlier."))

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if got == nil {
		t.Fatal("context activity carried no MemoryEvidence record")
	}

	if !got.SummaryInjected {
		t.Fatal("summary block passed and kept — SummaryInjected must be true")
	}

	if got.SummaryTokens <= 0 {
		t.Fatalf("SummaryTokens must carry the measured estimate, got %d", got.SummaryTokens)
	}
}

func TestMemoryEvidenceOnContextPlan(t *testing.T) {
	// The evidence record rides the plan; verify the wiring end to end:
	// a RunDetailed publishes exactly one context activity whose Detail
	// carries a MemoryEvidence pointer with honest defaults for a plain
	// remote chat turn.
	var got *contextplan.MemoryEvidence

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		var b strings.Builder

		b.WriteString(sseChunk("ok"))
		b.WriteString(sseDone)

		return b.String()
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	_, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "hi"},
	}, func(a Activity) {
		if a.Type != "context" {
			return
		}

		if plan, ok := a.Detail.(contextplan.Plan); ok && plan.Memory != nil {
			got = plan.Memory
		}
	})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if got == nil {
		t.Fatal("context activity carried no MemoryEvidence record")
	}

	if got.SummaryInjected {
		t.Fatal("no summary block passed — SummaryInjected must be false")
	}

	if got.RecalledExchanges != 0 {
		t.Fatalf("no recall ran — RecalledExchanges must be 0, got %d", got.RecalledExchanges)
	}

	if got.RecallAttempted {
		t.Fatal("remote plain turn must not claim a recall attempt")
	}
}
