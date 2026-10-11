package agent

// userflows_test.go — v1.2.5 Phase 15 user-flow acceptance tests that are
// not already covered by orchestrator_tiers_test.go: old-conversation
// retrieval (recall gating by tier), research-shaped work, and vision
// payloads — each verified against what the fake engine actually received.

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/taskclassify"
)

// --- Old conversation reference: targeted retrieval, not full transcript.

func TestUserFlowOldConversationRetrieval(t *testing.T) {
	seen := &engineSeen{}
	recallComposed := 0

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("we discussed the parser rewrite.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	cfg.RecallEnabled = true

	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	orch.SetRecaller(recallerFunc(func(query string, k, maxTokens int) string {
		recallComposed++
		return "user asked: rewrite the parser\nassistant answered: use a two-pass design"
	}))

	// A long conversation whose latest turn references the past.
	messages := make([]llm.Message, 0, 41)
	messages = append(messages, llm.Message{Role: "user", Content: "let's start a project"})
	messages = append(messages, llm.Message{Role: "assistant", Content: "sure"})

	for i := 0; i < 38; i++ {
		messages = append(messages, llm.Message{Role: "user", Content: "step"})
		messages = append(messages, llm.Message{Role: "assistant", Content: "ok"})
	}

	messages = append(messages, llm.Message{Role: "user", Content: "what did we discuss earlier?"})

	res, err := orch.RunDetailed(context.Background(), messages, func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if res.Text != "we discussed the parser rewrite." {
		t.Fatalf("text = %q", res.Text)
	}

	// The long-history reference escalates above FAST and composes recall.
	if taskclassify.TierRank(res.Tier) < taskclassify.TierRank(taskclassify.TierStandard) {
		t.Fatalf("old-conversation tier = %q, want >= STANDARD", res.Tier)
	}

	if recallComposed == 0 {
		t.Fatalf("recall never composed for an old-conversation reference")
	}

	if !strings.Contains(seen.allContent(), "user asked:") {
		t.Fatalf("recall digest missing from the prompt")
	}

	if res.Recalled == 0 {
		t.Fatalf("Recalled = %d, want > 0", res.Recalled)
	}
}

// --- Simple chat never pays recall retrieval (the v1.2.4 behaviour).

func TestUserFlowSimpleChatSkipsRecallRetrieval(t *testing.T) {
	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		return sseChunk("hey!") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	cfg.RecallEnabled = true

	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	recallComposed := 0

	orch.SetRecaller(recallerFunc(func(query string, k, maxTokens int) string {
		recallComposed++
		return "user asked: x"
	}))

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "hi"}},
		func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if res.Tier != taskclassify.TierFast {
		t.Fatalf("tier = %q, want FAST", res.Tier)
	}

	if recallComposed != 0 {
		t.Fatalf("recall composed %d times on simple chat; want 0 (FAST skips the I/O)", recallComposed)
	}
}

// --- Research-shaped work reaches at least STANDARD (recall + research tools).

func TestUserFlowResearchTaskTier(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("researched.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "search the web for the latest news about llama.cpp and research the release notes"}},
		func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if taskclassify.TierRank(res.Tier) < taskclassify.TierRank(taskclassify.TierStandard) {
		t.Fatalf("research tier = %q, want >= STANDARD", res.Tier)
	}
}

// --- Vision payloads never run FAST and carry the images.

func TestUserFlowVisionTaskFloor(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("i see a diagram.") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	// A real (tiny) image file — the multimodal client reads it from disk
	// and embeds it as an image_url part.
	png, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	imgPath := filepath.Join(t.TempDir(), "vision.png")

	if writeErr := os.WriteFile(imgPath, png, 0o600); writeErr != nil {
		t.Fatalf("write vision fixture: %v", writeErr)
	}

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{
			Role:    "user",
			Content: "what do you see?",
			Images:  []string{imgPath},
		}},
		func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if taskclassify.TierRank(res.Tier) < taskclassify.TierRank(taskclassify.TierStandard) {
		t.Fatalf("vision tier = %q, want >= STANDARD (vision never runs FAST)", res.Tier)
	}

	// The multimodal wire form carries the image part.
	wire := seen.allContent()
	if !strings.Contains(wire, "image_url") {
		t.Fatalf("vision request missing the image_url part: %.200s", wire)
	}
}
