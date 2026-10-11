// tasktool_e2e_test.go — v1.7.0: a task-scoped custom tool flows through
// the REAL agent loop: the fake model requests the task tool by name,
// the orchestrator executes it through the SAME registry path, the result
// returns to the model, and the final answer carries it. Unapproved or
// unregistered task tools are never offered.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/customtools"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func TestTaskScopedToolFlowsThroughTheRealAgentLoop(t *testing.T) {
	var (
		mu                sync.Mutex
		turns             int
		sawTaskToolResult bool
		unregisteredAfter bool
	)

	var orchProbeOrch *Orchestrator

	registryProbe := func() []string {
		// The orchestrator toolset snapshot at probe time.
		names := make([]string, 0)
		for name := range orchProbeOrch.Tools() {
			names = append(names, name)
		}
		return names
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "text/event-stream")

		offered := false
		if tools, ok := body["tools"].([]any); ok {
			for _, tl := range tools {
				tm, _ := tl.(map[string]any)
				fn, _ := tm["function"].(map[string]any)
				if fn != nil && fn["name"] == "taskecho" {
					offered = true
				}
			}
		}

		mu.Lock()
		turns++
		turn := turns
		mu.Unlock()

		if turn == 1 {
			if !offered {
				_, _ = w.Write([]byte(sseChunk("ERROR: taskecho was not offered")))
				_, _ = w.Write([]byte(sseDone))
				return
			}
			_, _ = w.Write([]byte(sseToolCall("call-task-1", "taskecho", `{"text":"task-scoped"}`)))
			_, _ = w.Write([]byte(sseDone))
			return
		}

		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, _ := m.(map[string]any)
			if msg["role"] == "tool" && strings.Contains(fmt.Sprint(msg["content"]), "task-says task-scoped") {
				mu.Lock()
				sawTaskToolResult = true
				mu.Unlock()
			}
		}

		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n"))
		_, _ = w.Write([]byte(sseDone))
	}))
	t.Cleanup(server.Close)

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)
	orchProbeOrch = orch

	// The task tool lifecycle through the REAL TaskStore:
	// CREATE → VALIDATE → APPROVE → REGISTER → EXECUTE → UNREGISTER.
	store, err := customtools.NewTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	exe := "echo"
	args := []string{"task-says", "$text"}
	if runtime.GOOS == "windows" {
		exe = "cmd"
		args = []string{"/C", "echo", "task-says", "$text"}
	}

	td, err := store.CreateTaskTool("task-loop-1", "run-1", &customtools.Definition{
		Name:        "taskecho",
		ShortDesc:   "Echoes text for the task",
		Description: "Echoes the given text using a local command.",
		Params: []customtools.Param{
			{Name: "text", Type: "string", Required: true, Description: "the text"},
		},
		ExecType:    customtools.ExecCommand,
		Command:     &customtools.CommandExec{Executable: exe, Args: args},
		Permission:  customtools.PermLocal,
		TimeoutSec:  10,
		OutputLimit: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Unapproved: RegisterInto offers NOTHING.
	unreg0, _ := store.RegisterInto(customtools.RegistrarFuncs{
		Register:   func(t2 *customtools.Tool) { orch.Register(t2) },
		Unregister: orch.Unregister,
	}, "task-loop-1")
	if names := registryProbe(); len(names) > 0 {
		for _, n := range names {
			if n == "taskecho" {
				t.Fatal("an unapproved task tool must never be registered")
			}
		}
	}
	unreg0()

	if err := store.Approve(td.ID); err != nil {
		t.Fatal(err)
	}

	unregister, registered := store.RegisterInto(customtools.RegistrarFuncs{
		Register:   func(t2 *customtools.Tool) { orch.Register(t2) },
		Unregister: orch.Unregister,
	}, "task-loop-1")

	if len(registered) != 1 || registered[0] != "taskecho" {
		t.Fatalf("registered task tools: %v", registered)
	}
	defer unregister()

	result, err := orch.RunDetailed(context.Background(), []llm.Message{
		{Role: "user", Content: "use the taskecho tool with text task-scoped"},
	}, func(_ Activity) {})
	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	mu.Lock()
	saw := sawTaskToolResult
	mu.Unlock()

	if !saw {
		t.Fatal("the follow-up turn never carried the task tool's result — the loop never executed it")
	}

	if len(result.ToolsUsed) != 1 || result.ToolsUsed[0] != "taskecho" {
		t.Fatalf("tools used: %v", result.ToolsUsed)
	}

	// Teardown: the task tool leaves the ONE registry.
	unregister()
	for name := range orch.Tools() {
		if name == "taskecho" {
			unregisteredAfter = false
			t.Fatal("the task tool survived the run teardown")
		}
	}
	unregisteredAfter = true
	_ = unregisteredAfter

	// CleanupTask removes the scoped store.
	if err := store.CleanupTask("task-loop-1"); err != nil {
		t.Fatal(err)
	}
	if got := store.ListForTask("task-loop-1"); len(got) != 0 {
		t.Fatalf("cleanup left definitions: %v", got)
	}
}
