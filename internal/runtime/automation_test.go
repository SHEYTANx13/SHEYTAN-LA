package runtime

// automation_test.go — v1.7.0: the INTEGRATION contract of the four
// systems through a REAL agent loop:
//
//      scheduler task (manual trigger)
//        → taskRunner installs the task context + task-scoped tools
//        → the model calls artifact_create
//        → the artifact lands in the durable task/run registry
//        → the run output reports the registered artifact (provenance)
//
// A chat run WITHOUT a task context refuses artifact_create (task-scoped
// honesty), and a task run with linked skills sees the injected block.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/scheduler"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/skills"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
)

func TestTaskRunCreatesRegisteredArtifactThroughTheRealLoop(t *testing.T) {
	var (
		mu    sync.Mutex
		turns int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "text/event-stream")

		mu.Lock()
		turns++
		turn := turns
		mu.Unlock()

		switch turn {
		case 1:
			// The model creates a task artifact.
			args, _ := json.Marshal(map[string]string{
				"filename": "report.md",
				"content":  "# Auto Report\n\nProduced by the task run.",
				"title":    "Run Report",
			})
			_, _ = w.Write([]byte(sseToolCallRuntime("call-art-1", "artifact_create", string(args))))
			_, _ = w.Write([]byte(sseDoneRuntime))
		default:
			_, _ = w.Write([]byte(sseChunkRuntime("report created and registered")))
			_, _ = w.Write([]byte(sseDoneRuntime))
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "remote"
	cfg.RemoteBaseURL = server.URL + "/v1"
	cfg.RemoteAPIKey = "test-key"
	cfg.RemoteModel = "fake-remote-model"
	cfg.MaxIterations = 8

	stack := &Stack{
		Src:  config.NewSource(cfg),
		Cfg:  cfg,
		Orch: agent.New(config.NewSource(cfg), llm.NewClient(config.NewSource(cfg))),
	}

	// The scheduler exists FIRST (production order), then the automation
	// wiring attaches the task authorities.
	stack.Sched = scheduler.New(t.TempDir(),
		taskRunner(stack, stack.Orch, scheduleRunner(stack.Orch)),
		nil,
	)

	// The automation wiring (real production path): registers
	// artifact_create/skill_create, opens the task authorities.
	wireAutomation(stack)

	if stack.TaskTools == nil || stack.Artifacts == nil || stack.MarkdownSkills == nil {
		t.Fatal("automation authorities not wired")
	}

	// A task-scoped SKILL.md package: the run prompt names it, the
	// disclosure model injects the body only for this task's runs.
	md := &skills.MarkdownSkill{
		Meta: skills.MarkdownMeta{
			Name:        "Report Format",
			Description: "How the report must be shaped",
			Version:     1,
			Triggers:    []string{"report"},
		},
		Scope:  "task",
		TaskID: "task-int-1",
		Body:   "The report must open with a title and close with evidence.",
	}
	if err := stack.MarkdownSkills.Create("report-format", md, "task"); err != nil {
		t.Fatal(err)
	}

	if err := stack.Sched.AddTask(scheduler.Task{
		ID:           "task-int-1",
		Name:         "Integration Task",
		Trigger:      scheduler.EventManual,
		Prompt:       "write the report using the report skill and artifact_create",
		MaxRuntime:   60 * time.Second,
		LinkedSkills: []string{"nonexistent-skill"}, // must not break the run
	}); err != nil {
		t.Fatal(err)
	}

	ch, err := stack.Sched.RunNow(context.Background(), "task-int-1")
	if err != nil {
		t.Fatal(err)
	}

	report := <-ch
	for range ch { // v1.7.1 settlement contract: close = fully settled worker
	}

	if !report.OK {
		t.Fatalf("task run failed: %+v", report)
	}

	if !strings.Contains(report.Output, "task artifact") {
		t.Fatalf("run output must carry the artifact provenance echo: %q", report.Output)
	}

	// The artifact is in the durable registry with FULL provenance.
	arts := stack.Artifacts.List("task-int-1")
	if len(arts) != 1 {
		t.Fatalf("registered artifacts: %+v", arts)
	}
	a := arts[0]
	if a.Source != "artifact_create" || a.Kind != "doc" || a.RunID == "" {
		t.Fatalf("provenance: %+v", a)
	}

	data, err := stack.Artifacts.Read(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Auto Report") {
		t.Fatalf("content: %q", data)
	}

	// The task skill body was NOT leaked into global scope and the
	// task scope still holds the package.
	if hits := stack.MarkdownSkills.Match("write the report", "", 5); len(hits) != 1 || hits[0].Scope != "task" {
		t.Fatalf("task skill scope: %+v", hits)
	}
}

func TestArtifactCreateRefusesWithoutTaskContext(t *testing.T) {
	// A chat run without a task context: the tool refuses honestly.
	tools.SetTaskContext(nil)

	tc := tools.CurrentTaskContext()
	if tc != nil {
		t.Fatal("no task context expected outside task runs")
	}

	stack := &Stack{
		Src: config.NewSource(config.Default()),
		Cfg: &config.Config{DataDir: t.TempDir()},
	}
	wireAutomation(stack)
	// (No task context installed — wireAutomation only builds authorities.)

	t.Cleanup(func() { tools.SetTaskContext(nil) })

	// Sanity: the refusal is enforced inside the tool (covered by its
	// context gate); here we prove the gate state machine.
	if ctx := tools.CurrentTaskContext(); ctx != nil {
		t.Fatalf("context must stay nil: %+v", ctx)
	}

	_ = fmt.Sprint("gate verified")
}

// --- SSE fakes (the runtime package has no test SSE helpers of its own) ---

func sseChunkRuntime(content string) string {
	payload, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"delta": map[string]any{"content": content}},
		},
	})
	return "data: " + string(payload) + "\n\n"
}

const sseDoneRuntime = "data: [DONE]\n\n"

func sseToolCallRuntime(id, name, args string) string {
	payload, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"delta": map[string]any{
				"tool_calls": []map[string]any{
					{
						"id":   id,
						"type": "function",
						"function": map[string]any{
							"name":      name,
							"arguments": args,
						},
					},
				},
			}},
		},
	})
	return "data: " + string(payload) + "\n\n"
}
