package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/agent"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/llm"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/scheduler"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/skills"
)

// v1.11.1 regression (CodeQL go/useless-assignment-to-local, work order
// §6.3): taskRunner rendered the task-scoped skill block and assigned it
// to a LOCAL `prompt` that nothing ever read — the run received
// task.Prompt VERBATIM, so the documented "injected as a bounded block"
// progressive disclosure never reached a single scheduled run. The block
// is now prepended to the task copy the runner consumes.
func TestTaskRunnerInjectsTaskSkillBlockIntoRunPrompt(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	stack := &Stack{
		Src:  config.NewSource(cfg),
		Cfg:  cfg,
		Orch: agent.New(config.NewSource(cfg), llm.NewClient(config.NewSource(cfg))),
		// Task authorities (TaskTools/Artifacts) stay nil here: this test
		// isolates the skill-injection data flow, not the tool wiring.
		MarkdownSkills: skills.NewMarkdownStore(t.TempDir(), t.TempDir()),
	}

	md := &skills.MarkdownSkill{
		Meta: skills.MarkdownMeta{
			Name:        "Greeting Format",
			Description: "How the greeting must be shaped",
			Version:     1,
			Triggers:    []string{"greet"},
		},
		Scope:  "task",
		TaskID: "task-skill-inject",
		Body:   "GREETING-BODY-MARKER every greeting opens with evidence.",
	}
	if err := stack.MarkdownSkills.Create("greeting-format", md, "task"); err != nil {
		t.Fatalf("create task skill: %v", err)
	}

	var got scheduler.Task

	ran := false

	runner := taskRunner(stack, stack.Orch, func(_ context.Context, task scheduler.Task) (string, error) {
		ran = true
		got = task
		return "ok", nil
	})

	const originalPrompt = "please greet the operator"

	out, err := runner(context.Background(), scheduler.Task{
		ID:     "task-skill-inject",
		Prompt: originalPrompt,
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	if !ran {
		t.Fatal("the inner runner never ran")
	}
	if out != "ok" {
		t.Fatalf("runner output must pass through, got %q", out)
	}

	const blockHeader = "## TASK CONTEXT (scoped skills for this task — bounded)"

	if !strings.HasPrefix(got.Prompt, blockHeader) {
		t.Fatalf("the run prompt must be prefixed by the task skill block, got %q", got.Prompt)
	}

	if !strings.Contains(got.Prompt, "GREETING-BODY-MARKER") {
		t.Fatalf("the injected block must carry the matched skill body, got %q", got.Prompt)
	}

	if !strings.HasSuffix(got.Prompt, originalPrompt) {
		t.Fatalf("the original prompt must be preserved after the block, got %q", got.Prompt)
	}
}

// No match → the run prompt is the task's prompt VERBATIM (no empty
// block, no prefix churn).
func TestTaskRunnerLeavesUnmatchedPromptVerbatim(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	stack := &Stack{
		Src:            config.NewSource(cfg),
		Cfg:            cfg,
		Orch:           agent.New(config.NewSource(cfg), llm.NewClient(config.NewSource(cfg))),
		MarkdownSkills: skills.NewMarkdownStore(t.TempDir(), t.TempDir()),
	}

	var got scheduler.Task

	runner := taskRunner(stack, stack.Orch, func(_ context.Context, task scheduler.Task) (string, error) {
		got = task
		return "ok", nil
	})

	const originalPrompt = "nothing matches any skill trigger here"

	if _, err := runner(context.Background(), scheduler.Task{
		ID:     "task-no-skill",
		Prompt: originalPrompt,
	}); err != nil {
		t.Fatalf("runner: %v", err)
	}

	if got.Prompt != originalPrompt {
		t.Fatalf("an unmatched task must run its prompt verbatim, got %q", got.Prompt)
	}
}
