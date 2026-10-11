// E2E custom-tool loop test (v1.6.0, spec §9): a REAL custom tool flows
// through the REAL agent loop — the fake model requests the tool by
// name, the orchestrator executes it through the SAME registry path as
// built-in tools, the result returns to the model, and the final answer
// carries it.
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

func customEchoDef() *customtools.Definition {
        exe := "echo"
        args := []string{"tool-says", "$text"}
        if runtime.GOOS == "windows" {
                exe = "cmd"
                args = []string{"/C", "echo", "tool-says", "$text"}
        }

        return &customtools.Definition{
                Name:        "myecho",
                ShortDesc:   "Echoes text through a local command",
                Description: "Echoes the given text using a local command and returns the output.",
                Params: []customtools.Param{
                        {Name: "text", Type: "string", Required: true, Description: "the text to echo"},
                },
                ExecType:    customtools.ExecCommand,
                Command:     &customtools.CommandExec{Executable: exe, Args: args},
                Permission:  customtools.PermLocal,
                TimeoutSec:  10,
                OutputLimit: 4096,
                Enabled:     true,
        }
}

func TestCustomToolFlowsThroughTheRealAgentLoop(t *testing.T) {
        var gotToolResultInHistory bool

        var mu sync.Mutex
        turns := 0

        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
                        http.Error(w, "not found", http.StatusNotFound)
                        return
                }

                var body map[string]any
                _ = json.NewDecoder(r.Body).Decode(&body)

                w.Header().Set("Content-Type", "text/event-stream")

                // The request MUST offer the custom tool (toolset selection: a
                // task that NAMES the tool gets it — custom tools included).
                offered := false

                if tools, ok := body["tools"].([]any); ok {
                        for _, t := range tools {
                                tm, _ := t.(map[string]any)
                                fn, _ := tm["function"].(map[string]any)
                                if fn != nil && fn["name"] == "myecho" {
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
                                _, _ = w.Write([]byte(sseChunk("ERROR: myecho was not offered")))
                                _, _ = w.Write([]byte(sseDone))
                                return
                        }

                        _, _ = w.Write([]byte(sseToolCall("call-ct-1", "myecho", `{"text":"sheytan-custom"}`)))
                        _, _ = w.Write([]byte(sseDone))
                        return
                }

                // Second turn: the request must carry the custom tool's result.
                msgs, _ := body["messages"].([]any)

                for _, m := range msgs {
                        msg, _ := m.(map[string]any)

                        if msg["role"] == "tool" {
                                if strings.Contains(fmt.Sprint(msg["content"]), "tool-says sheytan-custom") {
                                        gotToolResultInHistory = true
                                }
                        }
                }

                _, _ = w.Write([]byte(sseChunk("The custom tool returned: tool-says sheytan-custom")))
                _, _ = w.Write([]byte(sseDone))
        }))
        t.Cleanup(server.Close)

        cfg := remoteConfig(t, server.URL)
        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        // The custom tool is registered EXACTLY like a built-in: one
        // registry, no special casing.
        orch.Register(customtools.NewTool(customEchoDef()))

        result, err := orch.RunDetailed(context.Background(), []llm.Message{
                {Role: "user", Content: "use the myecho tool with text sheytan-custom"},
        }, func(_ Activity) {})

        if err != nil {
                t.Fatalf("RunDetailed: %v", err)
        }

        if !gotToolResultInHistory {
                t.Fatal("the follow-up turn did not carry the custom tool's result — the loop never executed it")
        }

        if len(result.ToolsUsed) != 1 || result.ToolsUsed[0] != "myecho" {
                t.Fatalf("tools used mismatch: %v", result.ToolsUsed)
        }

        if !strings.Contains(result.Text, "tool-says sheytan-custom") {
                t.Fatalf("final answer must reference the custom tool result: %q", result.Text)
        }
}

// TestCustomToolDisabledIsRejectedByTheLoop: a DISABLED custom tool is
// never offered — and the executor refuses it if reached anyway.
func TestCustomToolDisabledIsRejectedByTheLoop(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
                        http.Error(w, "not found", http.StatusNotFound)
                        return
                }

                var body map[string]any
                _ = json.NewDecoder(r.Body).Decode(&body)

                w.Header().Set("Content-Type", "text/event-stream")

                // The tool must NOT be offered to the model.
                if tools, ok := body["tools"].([]any); ok {
                        for _, t := range tools {
                                tm, _ := t.(map[string]any)
                                fn, _ := tm["function"].(map[string]any)
                                if fn != nil && fn["name"] == "myecho" {
                                        _, _ = w.Write([]byte(sseChunk("ERROR: disabled tool was offered")))
                                        _, _ = w.Write([]byte(sseDone))
                                        return
                                }
                        }
                }

                _, _ = w.Write([]byte(sseChunk("no tools offered")))
                _, _ = w.Write([]byte(sseDone))
        }))
        t.Cleanup(server.Close)

        cfg := remoteConfig(t, server.URL)
        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        // A DISABLED tool is simply NOT REGISTERED (the registry is the
        // authority the loop offers from).
        def := customEchoDef()
        def.Enabled = false

        // Direct executor check: Run refuses it.
        _, err := customtools.NewTool(def).Run(context.Background(), json.RawMessage(`{"text":"x"}`))
        if err == nil || !strings.Contains(err.Error(), "disabled") {
                t.Fatalf("disabled tool must be refused by the executor, got %v", err)
        }

        // And the loop never sees it (not registered).
        if _, ok := orch.Tools()["myecho"]; ok {
                t.Fatal("disabled tool must not be in the registry")
        }

        result, err := orch.RunDetailed(context.Background(), []llm.Message{
                {Role: "user", Content: "use the myecho tool"},
        }, func(_ Activity) {})

        if err != nil {
                t.Fatalf("RunDetailed: %v", err)
        }

        if strings.Contains(result.Text, "ERROR") {
                t.Fatalf("disabled tool leaked into the offered toolset: %q", result.Text)
        }
}
