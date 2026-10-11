// runcontrol_regress_v174_test.go — v1.7.4 P0 crash-window regressions.
//
// The v1.7.2 fix made Tools() an immutable snapshot, but the supplied
// v1.7.3 Windows runtime still died inside the SAME planning window
// (between `task classified` and `tier selected`). This file pins the
// v1.7.4 hardening of the remaining shared-mutable-state class:
//
//   - customtools.Tool used to alias the store's Definition internals
//     (HTTP headers map, Params slice). Any in-place mutation of a
//     definition while the planning pass introspected
//     Name/Description/Parameters is a fatal, unrecoverable
//     `concurrent map read and map write` on Windows. NewTool now takes
//     a DEEP copy — the registry-facing tool object is immutable for the
//     whole run (TestCustomToolDefinitionIsDeepCopied).
//
//   - the crash-window ordering (task classified → … → tier selected →
//     generation) must survive concurrent custom-tool register/unregister,
//     concurrent schema introspection and spec-cache invalidation with
//     REAL custom tool objects on the registry
//     (TestCrashWindowRealCustomToolSurface).
//
// Run with -race: every mutation/introspection interleaving here is a
// DATA RACE on the defective shape and clean on the fixed one.

package agent

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

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/customtools"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// slowCustomEngine keeps the run inside the planning/generation window
// long enough for the mutator goroutines to interleave.
func slowCustomEngine(t *testing.T) (*httptest.Server, *int) {
        return newFakeEngine(t, func(turn int, _ map[string]any) string {
                time.Sleep(5 * time.Millisecond)
                return sseChunk(fmt.Sprintf("reply-%d.", turn)) + sseDone
        })
}

// httpCustomDef builds a valid, enabled HTTP custom-tool definition — the
// shape whose HTTP.Headers map is the fatal-race hazard.
func httpCustomDef(name string) *customtools.Definition {
        return &customtools.Definition{
                ID:          "ct-" + name,
                Name:        name,
                ShortDesc:   "stress tool " + name,
                Description: "Stress tool used by the v1.7.4 crash-window regression.",
                Params: []customtools.Param{
                        {
                                Name:        "target",
                                Type:        "string",
                                Required:    true,
                                Description: "target selector",
                                Enum:        []string{"a", "b", "c"},
                        },
                },
                ExecType:   customtools.ExecHTTP,
                Permission: customtools.PermNetwork,
                HTTP: &customtools.HTTPExec{
                        Method: "GET",
                        URL:    "https://example.invalid/endpoint",
                        Headers: map[string]string{
                                "X-Stress-One": "value-one",
                                "X-Stress-Two": "value-two",
                        },
                },
                TimeoutSec:  customtools.DefaultTimeoutSeconds,
                OutputLimit: customtools.DefaultOutputLimit,
                Enabled:     true,
        }
}

// TestCustomToolDefinitionIsDeepCopied pins the v1.7.4 aliasing removal:
// mutating the CALLER'S definition (including the headers map) after
// NewTool must never change the registry-facing tool.
func TestCustomToolDefinitionIsDeepCopied(t *testing.T) {
        def := httpCustomDef("aliascheck")
        def.CreatedAt = time.Now().UTC()

        tool := customtools.NewTool(def)

        before := tool.Description()

        // Mutate every aliased structure the OLD shallow share exposed.
        def.HTTP.Headers["X-Stress-One"] = "MUTATED"
        def.HTTP.Headers["X-Injected"] = "injected"
        def.Params[0].Enum[0] = "MUTATED"
        def.Params[0].Description = "MUTATED"
        def.Description = "MUTATED"
        def.Enabled = false

        if after := tool.Description(); after != before {
                t.Fatalf("tool description observed a caller-side mutation:\nbefore: %q\nafter:  %q", before, after)
        }

        // Header NAMES are model-visible (by design); header VALUES never are.
        // The tool's copy must still show the original names and none of the
        // caller's mutations.
        if !strings.Contains(before, "X-Stress-One") ||
                strings.Contains(before, "MUTATED") ||
                strings.Contains(before, "injected") {
                t.Fatalf("schema surface corrupted: %q", before)
        }

        schema, ok := tool.Parameters().(map[string]any)
        if !ok {
                t.Fatalf("parameters must be a schema map, got %T", tool.Parameters())
        }

        props, _ := schema["properties"].(map[string]any)
        prop, _ := props["target"].(map[string]any)
        enum, _ := prop["enum"].([]string)

        if len(enum) != 3 || enum[0] != "a" {
                t.Fatalf("enum observed the caller-side mutation: %v", enum)
        }

        // The executor still honors the tool's own (copied) enabled flag.
        if _, runErr := tool.Run(context.Background(), json.RawMessage(`{"target":"a"}`)); runErr != nil &&
                strings.Contains(runErr.Error(), "disabled") {
                t.Fatalf("tool must not inherit the caller's Enabled=false mutation: %v", runErr)
        }
}

// TestCrashWindowRealCustomToolSurface reproduces the FULL v1.7.3 crash
// window with the REAL registered custom-tool surface: user chats through
// RunDetailed while custom tools are registered/unregistered concurrently
// and their model-visible schema is introspected from other goroutines —
// the exact interleaving the desktop stack produces (custom-tools HTTP
// handlers + task-scoped teardown + a live chat run).
func TestCrashWindowRealCustomToolSurface(t *testing.T) {
        engine, turns := slowCustomEngine(t)

        cfg := remoteConfig(t, engine.URL)
        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        // Seed REAL custom tools (headers map + params slice + enum).
        for i := 0; i < 6; i++ {
                orch.Register(customtools.NewTool(httpCustomDef(fmt.Sprintf("stress-%d", i))))
        }

        var wg sync.WaitGroup

        stop := make(chan struct{})

        // Mutator A — custom-tools HTTP handler pattern: create/update/delete
        // cycles replace registry members (each Save produces a NEW tool).
        wg.Add(1)
        go func() {
                defer wg.Done()
                for i := 0; i < 300; i++ {
                        name := fmt.Sprintf("stress-%d", i%6)
                        orch.Register(customtools.NewTool(httpCustomDef(name)))
                        select {
                        case <-stop:
                                return
                        default:
                        }
                }
        }()

        // Mutator B — task-scoped teardown pattern.
        wg.Add(1)
        go func() {
                defer wg.Done()
                for i := 0; i < 30; i++ {
                        name := fmt.Sprintf("taskscope-%d", i%3)
                        orch.Register(customtools.NewTool(httpCustomDef(name)))
                        orch.Unregister(name)
                        select {
                        case <-stop:
                                return
                        default:
                        }
                }
        }()

        // Introspector — concurrent Name/Description/Parameters/Spec calls on
        // the snapshot tools (what BuildSpecs does on cache misses).
        wg.Add(1)
        go func() {
                defer wg.Done()
                for i := 0; i < 600; i++ {
                        snap := orch.Tools()

                        var wgIn sync.WaitGroup

                        for _, tool := range snap {
                                wgIn.Add(1)

                                go func(tool Tool) {
                                        defer wgIn.Done()
                                        _ = tool.Name()
                                        _ = tool.Description()
                                        _ = tool.Parameters()
                                }(tool)
                        }

                        wgIn.Wait()

                        select {
                        case <-stop:
                                return
                        default:
                        }
                }
        }()

        // The user's one-word chat through the REAL RunDetailed window.
        res, err := orch.RunDetailed(context.Background(),
                []llm.Message{{Role: "user", Content: "hi"}},
                func(Activity) {},
        )

        close(stop)
        wg.Wait()

        if err != nil {
                t.Fatalf("RunDetailed must survive concurrent custom-tool mutation: %v", err)
        }
        if res.Text == "" {
                t.Fatal("assistant reply must be non-empty")
        }
        if *turns == 0 {
                t.Fatal("the engine must have been reached at least once")
        }
}

// TestRunControlPauseBoundaries pins the orchestrator pause contract at
// the unit level: a pause requested before the first token must stop the
// run at a stream boundary with the received prefix preserved, without an
// error outcome and without fabricating a completion. The engine flushes
// its chunks with real delays so the pause lands BETWEEN deltas — a burst
// body has no boundary to pause at.
func TestRunControlPauseBoundaries(t *testing.T) {
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/event-stream")
                flusher := w.(http.Flusher)

                for i := 0; i < 8; i++ {
                        fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"piece-%d \"}}]}\n\n", i)
                        flusher.Flush()
                        time.Sleep(25 * time.Millisecond)
                }

                fmt.Fprint(w, "data: [DONE]\n\n")
                flusher.Flush()
        }))
        t.Cleanup(srv.Close)

        cfg := remoteConfig(t, srv.URL)
        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        ctrl := NewRunControl()
        ctrl.RequestPause() // requested before the run starts

        res, err := orch.RunDetailed(context.Background(),
                []llm.Message{{Role: "user", Content: "hi"}},
                func(Activity) {},
                WithRunControl(ctrl),
        )

        if err != nil {
                t.Fatalf("a pause must never surface as an error: %v", err)
        }

        if !res.Paused {
                t.Fatalf("run must report Paused at the stream boundary (got text=%q)", res.Text)
        }

        if res.PauseToolState != ToolStateNone {
                t.Fatalf("a pure generation pause reports ToolStateNone, got %q", res.PauseToolState)
        }

        // The prefix received before the pause is preserved, and the run never
        // fabricated the remaining pieces.
        if strings.Contains(res.Text, "piece-7") {
                t.Fatalf("a paused run must not keep consuming the stream after the boundary: %q", res.Text)
        }
}

// TestResumeContinuationRidesTheRequest pins the semantic-continuation
// contract: WithResumeContinuation injects the accepted draft as the
// model's own partial answer and the continuation instruction — the wire
// request the engine receives must contain the draft verbatim.
func TestResumeContinuationRidesTheRequest(t *testing.T) {
        var seenBody map[string]any

        engine, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
                seenBody = body
                return sseChunk("continued.") + sseDone
        })

        cfg := remoteConfig(t, engine.URL)
        client := llm.NewClient(config.NewSource(cfg))
        orch := New(config.NewSource(cfg), client)

        res, err := orch.RunDetailed(context.Background(),
                []llm.Message{{Role: "user", Content: "write me a poem"}},
                func(Activity) {},
                WithResumeContinuation("roses are red, violets are", ""),
        )

        if err != nil {
                t.Fatalf("resume run failed: %v", err)
        }

        if res.Text != "continued." {
                t.Fatalf("continuation text = %q, want %q", res.Text, "continued.")
        }

        // The engine request must carry the draft so the model continues it.
        messages := extractRequestMessages(t, seenBody)

        found := false

        for _, m := range messages {
                if m["role"] == "assistant" && strings.Contains(fmt.Sprint(m["content"]), "roses are red, violets are") {
                        found = true
                        break
                }
        }

        if !found {
                t.Fatalf("the accepted draft must ride the continuation request verbatim; got %v", messages)
        }
}

// extractRequestMessages pulls the messages array out of a captured chat
// completion request body (the fake engine's body map).
func extractRequestMessages(t *testing.T, body map[string]any) []map[string]any {
        t.Helper()

        if body == nil {
                t.Fatal("the engine received no request body")
        }

        raw, ok := body["messages"]
        if !ok {
                t.Fatalf("request body has no messages: %v", body)
        }

        list, ok := raw.([]any)
        if !ok {
                t.Fatalf("messages is not an array: %T", raw)
        }

        out := make([]map[string]any, 0, len(list))

        for _, e := range list {
                if m, ok := e.(map[string]any); ok {
                        out = append(out, m)
                }
        }

        return out
}
