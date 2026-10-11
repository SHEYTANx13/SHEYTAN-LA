package api

// localchat_v181_test.go — v1.8.1 P0 END-TO-END REGRESSION: the real
// Windows `hi` failure class, driven against the LOCAL engine path.
//
// THE DEFECT (v1.8.0 Windows runtime log): local provider + small
// Gemma-class GGUF + one-word chat → "recovered panic: runtime error:
// invalid memory address or nil pointer dereference" between `task
// classified` and `tier selected`, run settled as error in ~57 ms, no
// request ever reaching the engine. Root cause: the Gemma-class
// tokenizer block exceeded the GGUF parser's read bound → caps nil →
// the orchestrator's estimator block dereferenced nil.
//
// This file provides the local-engine seam the api package tests never
// had (all existing run tests use the REMOTE provider): the test binary
// re-executes as a stand-in llama-server (the same re-exec pattern as
// internal/llm's TestMain fake), serving the full engine contract —
// /health, /v1/models, /props, /v1/chat/completions SSE — so the whole
// POST /api/run → gate → engine start → generation → persistence →
// settlement path runs for REAL against a local backend.

import (
        "encoding/json"
        "fmt"
        "net"
        "net/http"
        "os"
        "strconv"
        "testing"
)

// ---------------------------------------------------------------------------
// The fake llama.cpp engine (re-exec dispatch, called from TestMain).
// ---------------------------------------------------------------------------

// v181FakeEngineHelp mirrors the canonical --help contract the
// capability probe parses (same shape as internal/llm's fake engine).
const v181FakeEngineHelp = `usage: llama-server [options]

  -h, --help            show this help message and exit
  --device DEVICE       select the device for inference
  --flash-attn [on|off|auto]  enable/disable Flash Attention
  --cache-reuse N       minimize KV-cache re-computation
  --jinja               use the jinja template engine for chat
  --no-webui            disable the built-in web UI
  --ubatch-size N       logical maximum batch size
  --threads-batch N     number of threads used during batch generation
`

// runFakeLlamaEngineV181 answers the engine subprocess roles. Returns
// true when THIS process acted as the engine (the caller must exit).
//
//   - argv probes ( --version / --help / --list-devices ) are
//     ARGV-triggered, like internal/llm's TestMain: the production
//     --help probe sanitizes the child environment, so only argv works
//     there;
//   - the serving role is armed by SHEYTAN_FAKE_LLAMA=1 (inherited by
//     the spawned engine subprocess through the parent environment).
func runFakeLlamaEngineV181() bool {
        if len(os.Args) > 1 {
                switch os.Args[1] {
                case "--version":
                        fmt.Println("version: 4818 (abcdef12)")
                        return true
                case "--help":
                        fmt.Print(v181FakeEngineHelp)
                        return true
                case "--list-devices":
                        fmt.Print("Available devices:\n" +
                                "  Vulkan0: Fake Local Graphics (2048 MiB)\n")
                        return true
                }
        }

        if os.Getenv("SHEYTAN_FAKE_LLAMA") == "1" {
                runFakeLlamaServerV181()
                return true
        }

        return false
}

// runFakeLlamaServerV181 serves the full local-engine HTTP contract
// until the parent kills the subprocess.
func runFakeLlamaServerV181() {
        port := 0

        for i, a := range os.Args {
                if a == "--port" && i+1 < len(os.Args) {
                        port, _ = strconv.Atoi(os.Args[i+1])
                        break
                }
        }

        if port == 0 {
                os.Exit(2)
        }

        // The chat-request evidence recorder: the E2E test asserts the run
        // REALLY reached the generation backend by reading this file.
        requestLog := os.Getenv("SHEYTAN_V181_REQUEST_LOG")

        mux := http.NewServeMux()

        mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
                w.WriteHeader(http.StatusOK)
                _, _ = w.Write([]byte(`{"status":"ok"}`))
        })

        mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gemma-4-E2B-it-Q4_K_M.gguf"}]}`))
        })

        mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"default_generation_settings":{"n_ctx":16384}}`))
        })

        mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
                // Record the request evidence: the body's message count and the
                // model field — proof the run reached the generation backend.
                var body struct {
                        Model    string `json:"model"`
                        Messages []struct {
                                Role    string `json:"role"`
                                Content string `json:"content"`
                        } `json:"messages"`
                        Stream bool `json:"stream"`
                }
                _ = json.NewDecoder(r.Body).Decode(&body)

                if requestLog != "" {
                        entry := fmt.Sprintf("chat model=%s messages=%d stream=%v\n",
                                body.Model, len(body.Messages), body.Stream)
                        f, err := os.OpenFile(requestLog,
                                os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
                        if err == nil {
                                _, _ = f.WriteString(entry)
                                _ = f.Close()
                        }
                }

                if !body.Stream {
                        http.Error(w, "fake engine serves stream=true only", http.StatusBadRequest)
                        return
                }

                // A genuine multi-chunk SSE generation: the cumulative snapshots
                // the orchestrator emits prove streaming BEFORE completion.
                chunks := []string{"Hello", "!", " How", " can", " I", " help", " you", " today", "?"}

                w.Header().Set("Content-Type", "text/event-stream")
                w.Header().Set("Cache-Control", "no-cache")

                flusher, canFlush := w.(http.Flusher)

                for _, c := range chunks {
                        payload, _ := json.Marshal(map[string]any{
                                "id":      "chatcmpl-v181",
                                "object":  "chat.completion.chunk",
                                "choices": []map[string]any{{
                                        "index":         0,
                                        "delta":         map[string]any{"content": c},
                                        "finish_reason": nil,
                                }},
                        })
                        _, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
                        if canFlush {
                                flusher.Flush()
                        }
                }

                donePayload, _ := json.Marshal(map[string]any{
                        "id":      "chatcmpl-v181",
                        "object":  "chat.completion.chunk",
                        "choices": []map[string]any{{
                                "index":         0,
                                "delta":         map[string]any{},
                                "finish_reason": "stop",
                        }},
                        "usage": map[string]int{
                                "prompt_tokens":     9,
                                "completion_tokens": len(chunks),
                                "total_tokens":      9 + len(chunks),
                        },
                })
                _, _ = w.Write([]byte("data: " + string(donePayload) + "\n\n"))
                _, _ = w.Write([]byte("data: [DONE]\n\n"))
                if canFlush {
                        flusher.Flush()
                }
        })

        server := &http.Server{
                Addr:    fmt.Sprintf("127.0.0.1:%d", port),
                Handler: mux,
        }

        _ = server.ListenAndServe()
        os.Exit(0)
}

// v181FreePort reserves an unused local TCP port.
func v181FreePort(t *testing.T) int {
        t.Helper()

        l, err := net.Listen("tcp", "127.0.0.1:0")
        if err != nil {
                t.Fatalf("free port: %v", err)
        }
        defer l.Close()

        return l.Addr().(*net.TCPAddr).Port
}
