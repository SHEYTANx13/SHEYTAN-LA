package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// v1.8.2 CAPABILITY SELF-MODEL: a capability question ("what tools do you
// have?") must inject the runtime self-model block — composed from the
// live registry — into the request's system prefix, and must NOT trigger
// any web research. An ordinary chat ("hi") must NOT carry the block.
func TestCapabilityIntentInjectsSelfModel(t *testing.T) {
	var seenSelfModelInRequest bool
	var turnsSeen int

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		turnsSeen = turn

		msgs, _ := body["messages"].([]any)

		for _, m := range msgs {
			msg, _ := m.(map[string]any)

			if msg["role"] != "system" {
				continue
			}

			if content, _ := msg["content"].(string); strings.Contains(content, "RUNTIME SELF-MODEL") {
				seenSelfModelInRequest = true
			}
		}

		var b strings.Builder

		b.WriteString(sseChunk("I have shell, files and more — see my tool list."))
		b.WriteString(sseDone)

		return b.String()
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	// Register one real tool so the catalog has an entry to describe.
	orch.Register(&fakeSelfModelTool{})

	_, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "what tools do you have and what capabilities do you have?"},
	}, func(a Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if turnsSeen != 1 {
		t.Fatalf("capability question must be ONE cheap engine turn, got %d", turnsSeen)
	}

	if !seenSelfModelInRequest {
		t.Fatal("capability question did not inject the runtime self-model block")
	}
}

func TestOrdinaryChatDoesNotInjectSelfModel(t *testing.T) {
	var sawSelfModel bool

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		msgs, _ := body["messages"].([]any)

		for _, m := range msgs {
			msg, _ := m.(map[string]any)

			if msg["role"] != "system" {
				continue
			}

			if content, _ := msg["content"].(string); strings.Contains(content, "RUNTIME SELF-MODEL") {
				sawSelfModel = true
			}
		}

		var b strings.Builder

		b.WriteString(sseChunk("Hello!"))
		b.WriteString(sseDone)

		return b.String()
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)
	orch.Register(&fakeSelfModelTool{})

	_, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "hi"},
	}, func(a Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if sawSelfModel {
		t.Fatal("ordinary chat must not pay the self-model overhead")
	}
}

// fakeSelfModelTool is a minimal registry citizen for the self-model tests.
type fakeSelfModelTool struct{}

func (t *fakeSelfModelTool) Name() string { return "selfcheck" }

func (t *fakeSelfModelTool) Description() string {
	return "Check the runtime. Long operational prose with JSON action syntax."
}

func (t *fakeSelfModelTool) ShortDescription() string { return "Check the runtime." }

func (t *fakeSelfModelTool) Parameters() any {
	return struct{}{}
}

func (t *fakeSelfModelTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return "ok", nil
}

// BuildSelfModel unit coverage: bounded, catalog lines with the short
// description, and the registered/enabled/offered distinction.
func TestBuildSelfModelCatalog(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	tool := &fakeSelfModelTool{}

	block := BuildSelfModel(SelfModelInput{
		Cfg:          cfg,
		ToolSnapshot: map[string]Tool{"selfcheck": tool, "ghost": tool},
		EnabledNames: []string{"selfcheck"},
		OfferedNames: []string{"selfcheck"},
	})

	if !strings.Contains(block, "selfcheck — Check the runtime.") {
		t.Fatalf("catalog line with ShortDescription missing:\n%s", block)
	}

	if !strings.Contains(block, "Registered but currently DISABLED in settings (NOT callable): ghost") {
		t.Fatalf("disabled tool must be named as not callable:\n%s", block)
	}

	if len(block) > 6*1024 {
		t.Fatalf("self-model block unbounded: %d bytes", len(block))
	}

	if strings.Contains(block, "json.RawMessage") || strings.Contains(block, "Long operational prose") {
		t.Fatal("full operational description must never leak into the concise catalog")
	}
}
