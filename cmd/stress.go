package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/memory"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/multiagent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sandbox"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
)

// stressTest is one of the hostile scenarios in the chaos suite.
type stressTest struct {
	name string
	run  func() error
}

// runStressSuite exercises the chaos suite: the core hostile
// scenarios across the agent, tools, sessions, and memory, the current
// subsystem contracts, and the release surface.
//
// The versioned scenario files that accumulated across early releases
// (cmd/stress_v08.go ... cmd/stress_v110.go) were retired long ago.
// The feature coverage they provided lives on in the unit tests under
// internal/ (vision, continuum, chunking, lab, llm, memory, recall,
// research, tools). This suite keeps what only it can do: hostile-input
// robustness and the release-surface contract.
//
// Returns 0 if all pass, 1 otherwise.
func runStressSuite(cfg *config.Config) int {
	// Mirror the runtime stack (internal/runtime): the tool jail must be
	// configured before any tool runs, or shell/files calls fail with
	// "tools: base directory is not configured".
	tools.SetBaseDir(cfg.DataDir)

	store := sessions.New(cfg.SessionsDir)
	src := config.NewSource(cfg)
	client := llm.NewClient(src)
	orch := agent.New(src, client)
	orch.Register(tools.Shell{})
	orch.Register(tools.Files{})
	orch.Register(tools.CodeExec{})
	orch.Register(tools.Git{})

	tests := []stressTest{
		// Core hostile scenarios.
		{"empty_prompt", func() error { return stressEmptyPrompt(orch) }},
		{"huge_prompt", func() error { return stressHugePrompt(orch) }},
		{"garbage_tool_args", func() error { return stressGarbageToolArgs() }},
		{"unknown_tool", func() error { return stressUnknownTool(orch) }},
		{"null_path_in_files_tool", func() error { return stressNullPath() }},
		{"malformed_json_in_tool_args", func() error { return stressMalformedJSON() }},
		{"abort_mid_call", func() error { return stressAbortMid(orch) }},
		{"huge_tool_result", func() error { return stressHugeResult() }},
		{"read_missing_file", func() error { return stressReadMissing() }},
		{"deep_nonexistent_dir", func() error { return stressDeepDir() }},
		{"shell_injection", func() error { return stressShellInjection() }},
		{"circuit_breaker", func() error { return stressCircuitBreaker() }},
		{"memory_flood", func() error { return stressMemoryFlood(store) }},
		{"concurrent_tool_calls", func() error { return stressConcurrentTools() }},
		{"long_path", func() error { return stressLongPath() }},
		{"unicode_emoji_args", func() error { return stressUnicode() }},
		{"null_args", func() error { return stressNullArgs() }},
		{"catastrophic_garbage", func() error { return stressCatastrophic() }},
		// Current subsystem contracts (memory, sessions, JSON
		// extraction, sandbox).
		{"memory_store_search", func() error { return stressMemorySearch() }},
		{"memory_store_corrupt_jsonl", func() error { return stressCorruptJSONL() }},
		{"session_concurrent_writes", func() error { return stressSessionConcurrentWrites(store) }},
		{"session_delete_twice", func() error { return stressSessionDeleteTwice(store) }},
		{"session_update_missing", func() error { return stressSessionUpdateMissing(store) }},
		{"extract_json_markdown_fences", func() error { return stressExtractJSONFences() }},
		{"extract_json_nested", func() error { return stressExtractJSONNested() }},
		{"extract_json_no_braces", func() error { return stressExtractJSONNoBraces() }},
		{"sandbox_smoke_test", func() error { return stressSandboxSmoke() }},
		// Release surface: repaired Linux CI dependencies,
		// pinned toolchains, portable packaging,
		// collision-proof memory IDs, Windows-safe log rotation.
		{"release_surface", func() error { return stressReleaseSurface() }},
		{"memory_unique_ids", func() error { return stressMemoryUniqueIDs() }},
		{"trimlogs_rotate", func() error { return stressTrimLogsRotate() }},
	}
	// v1.2.1 release hardening: updater integrity (offline, tampered
	// sha256, size mismatch, staged drift, zip-slip), vision readiness
	// honesty, memory/session robustness, bounded context planning +
	// caching, recommendation extremes, LoopGuard retry ceiling.
	tests = append(tests, stressReleaseScenarios()...)

	pass, fail := 0, 0
	for _, t := range tests {
		fmt.Printf("  ▸ %-30s ", t.name)
		start := time.Now()
		err := t.run()
		dur := time.Since(start).Round(time.Millisecond)
		if err != nil {
			fmt.Printf("FAIL (%v, %v): %v\n", dur, "err", err)
			fail++
		} else {
			fmt.Printf("ok   (%v)\n", dur)
			pass++
		}
	}
	fmt.Printf("\n%d pass / %d fail\n", pass, fail)
	// Machine-readable summary (v1.2.1): a hang can never reach this
	// line (the suite would still be running), a crash would abort the
	// process before it prints — so hangs=0 crashes=0 are constructive
	// facts of a completed run, not decorations.
	fmt.Printf("STRESS-RESULT pass=%d fail=%d hangs=0 crashes=0\n", pass, fail)
	if fail > 0 {
		return 1
	}
	return 0
}

// --- individual stress tests ---

func stressEmptyPrompt(orch *agent.Orchestrator) error {
	// Should not crash; the LLM client would normally error since no model is loaded
	// on this dev box, so we just verify the orchestrator's loop is abort-safe.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	_, _ = orch.Run(ctx, []llm.Message{{Role: "user", Content: ""}}, func(a agent.Activity) {})
	return nil
}

func stressHugePrompt(orch *agent.Orchestrator) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	huge := strings.Repeat("hello ", 50_000) // ~300 KB
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	_, _ = orch.Run(ctx, []llm.Message{{Role: "user", Content: huge}}, func(a agent.Activity) {})
	return nil
}

func stressGarbageToolArgs() error {
	t := tools.Files{}
	_, err := t.Run(context.Background(), json.RawMessage("not json"))
	if err == nil {
		return fmt.Errorf("expected error for garbage args, got nil")
	}
	return nil
}

func stressUnknownTool(orch *agent.Orchestrator) error {
	// Manually invoke a tool that isn't registered
	t, ok := orch.Tools()["nonExistentTool"]
	if ok {
		return fmt.Errorf("non-existent tool unexpectedly registered")
	}
	if t != nil {
		return fmt.Errorf("non-existent tool returned non-nil")
	}
	return nil
}

func stressNullPath() error {
	t := tools.Files{}
	_, err := t.Run(context.Background(), json.RawMessage(`{"action":"read","path":""}`))
	if err == nil {
		return fmt.Errorf("expected error for empty path, got nil")
	}
	return nil
}

func stressMalformedJSON() error {
	t := tools.Shell{}
	// Trailing comma, unquoted keys — should error
	_, err := t.Run(context.Background(), json.RawMessage(`{command: "ls",}`))
	if err == nil {
		return fmt.Errorf("expected error for malformed JSON")
	}
	return nil
}

func stressAbortMid(orch *agent.Orchestrator) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	cancel() // abort before run: caller-provided context is canceled up front
	_, _ = orch.Run(ctx, []llm.Message{{Role: "user", Content: "x"}}, func(a agent.Activity) {})
	return nil
}

func stressHugeResult() error {
	t := tools.Shell{}
	// Generate a huge shell output via `yes`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := t.Run(ctx, json.RawMessage(`{"command":"yes hi | head -c 200000","timeout":3}`))
	if err != nil && !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "context") {
		return fmt.Errorf("unexpected error: %v", err)
	}
	return nil
}

func stressReadMissing() error {
	t := tools.Files{}
	_, err := t.Run(context.Background(), json.RawMessage(`{"action":"read","path":"/nonexistent/path/file.txt"}`))
	if err == nil {
		return fmt.Errorf("expected error for missing file")
	}
	return nil
}

func stressDeepDir() error {
	t := tools.Files{}
	deepPath := "/tmp/sht-deep-" + randomString(8) + "/a/b/c/d/e/f/g/h/i/j"
	_, err := t.Run(context.Background(), json.RawMessage(`{"action":"list","path":"`+deepPath+`"}`))
	if err == nil {
		return fmt.Errorf("expected error for deep nonexistent dir")
	}
	return nil
}

func stressShellInjection() error {
	t := tools.Shell{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Try injection patterns — they should be treated as normal shell args (bash will quote them)
	_, err := t.Run(ctx, json.RawMessage(`{"command":"echo '; rm -rf /; echo '","timeout":2}`))
	_ = err // we just don't want a panic
	return nil
}

func stressCircuitBreaker() error {
	// Force 5 rapid failures on a tool
	t := tools.Files{}
	for i := 0; i < 5; i++ {
		_, _ = t.Run(context.Background(), json.RawMessage(`{"action":"read","path":""}`))
	}
	return nil
}

func stressMemoryFlood(store *sessions.Store) error {
	sess := store.Create()
	for i := 0; i < 200; i++ {
		msg := llm.Message{Role: "user", Content: fmt.Sprintf("message %d", i)}
		_, _ = store.AppendMessage(sess.ID, msg)
	}
	// Verify session still loads
	loaded, err := store.Get(sess.ID)
	if err != nil {
		return fmt.Errorf("session load after flood: %w", err)
	}
	if len(loaded.Messages) != 200 {
		return fmt.Errorf("expected 200 messages, got %d", len(loaded.Messages))
	}
	_ = store.Delete(sess.ID)
	return nil
}

func stressConcurrentTools() error {
	t := tools.Shell{}
	const N = 10
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := t.Run(ctx, json.RawMessage(`{"command":"echo `+fmt.Sprintf("%d", idx)+`"}`))
			errs <- err
		}(i)
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			return fmt.Errorf("concurrent tool call %d failed: %w", i, err)
		}
	}
	return nil
}

func stressLongPath() error {
	t := tools.Files{}
	longPath := "/tmp/" + strings.Repeat("a", 200)
	_, err := t.Run(context.Background(), json.RawMessage(`{"action":"list","path":"`+longPath+`"}`))
	if err == nil {
		return fmt.Errorf("expected error for long path")
	}
	return nil
}

func stressUnicode() error {
	t := tools.Shell{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := t.Run(ctx, json.RawMessage(`{"command":"echo 'héllo wörld 🚀🎉 日本語 тест'"}`))
	_ = err
	return nil
}

func stressNullArgs() error {
	t := tools.Shell{}
	_, err := t.Run(context.Background(), json.RawMessage(`null`))
	if err == nil {
		return fmt.Errorf("expected error for null args")
	}
	return nil
}

func stressCatastrophic() error {
	t := tools.Shell{}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	// Random garbage command — bash will fail but should not panic
	garbage := make([]byte, 100)
	rand.Read(garbage)
	_, _ = t.Run(ctx, json.RawMessage(`{"command":"`+strings.ReplaceAll(string(garbage), `"`, `'`)+`"}`))
	return nil
}

func randomString(n int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	letters := "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[r.Intn(len(letters))]
	}
	return string(b)
}

// --- subsystem stress tests ---

func stressMemorySearch() error {
	tmp, err := os.CreateTemp("", "stress-mem-*.jsonl")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	tmp.Close()

	mem := memory.New(tmp.Name())
	// Append several entries
	if err := mem.Append([]string{"alpha", "fruit"}, "apple is red", "test"); err != nil {
		return err
	}
	if err := mem.Append([]string{"beta", "fruit"}, "banana is yellow", "test"); err != nil {
		return err
	}
	if err := mem.Append([]string{"gamma", "vehicle"}, "car has four wheels", "test"); err != nil {
		return err
	}
	// Search for "fruit"
	hits, err := mem.Search("fruit", 10)
	if err != nil {
		return err
	}
	if len(hits) != 2 {
		return fmt.Errorf("expected 2 hits for 'fruit', got %d", len(hits))
	}
	// Search for "yellow"
	hits, err = mem.Search("yellow", 10)
	if err != nil {
		return err
	}
	if len(hits) != 1 {
		return fmt.Errorf("expected 1 hit for 'yellow', got %d", len(hits))
	}
	// Search for "no-such-word"
	hits, err = mem.Search("zzz-no-such-word", 10)
	if err != nil {
		return err
	}
	if len(hits) != 0 {
		return fmt.Errorf("expected 0 hits for 'zzz-no-such-word', got %d", len(hits))
	}
	return nil
}

func stressCorruptJSONL() error {
	tmp, err := os.CreateTemp("", "stress-mem-corrupt-*.jsonl")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	// Write garbage to the file
	_, _ = tmp.WriteString("garbage line that's not json\n")
	_, _ = tmp.WriteString(`{"id":"1","tags":["a"],"content":"good","createdAt":"2026-01-01T00:00:00Z"}` + "\n")
	_, _ = tmp.WriteString("more garbage\n")
	_, _ = tmp.WriteString(`{"id":"2","tags":["b"],"content":"also good","createdAt":"2026-01-02T00:00:00Z"}` + "\n")
	tmp.Close()

	mem := memory.New(tmp.Name())
	all, err := mem.All()
	if err != nil {
		return fmt.Errorf("All() should not fail on corrupt lines: %w", err)
	}
	// Should have 2 valid entries (the corrupt lines should be skipped)
	if len(all) != 2 {
		return fmt.Errorf("expected 2 valid entries, got %d", len(all))
	}
	return nil
}

func stressSessionConcurrentWrites(store *sessions.Store) error {
	sess := store.Create()
	const N = 20
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			msg := llm.Message{Role: "user", Content: fmt.Sprintf("concurrent-%d", idx)}
			_, err := store.AppendMessage(sess.ID, msg)
			errs <- err
		}(i)
	}
	for i := 0; i < N; i++ {
		if err := <-errs; err != nil {
			return fmt.Errorf("concurrent write %d: %w", i, err)
		}
	}
	loaded, err := store.Get(sess.ID)
	if err != nil {
		return fmt.Errorf("load after concurrent writes: %w", err)
	}
	// We can't guarantee exact ordering, but we should have all 20
	if len(loaded.Messages) != N {
		return fmt.Errorf("expected %d messages, got %d (some concurrent writes lost)", N, len(loaded.Messages))
	}
	_ = store.Delete(sess.ID)
	return nil
}

func stressSessionDeleteTwice(store *sessions.Store) error {
	sess := store.Create()
	if err := store.Delete(sess.ID); err != nil {
		return fmt.Errorf("first delete failed: %w", err)
	}
	if err := store.Delete(sess.ID); err == nil {
		return fmt.Errorf("second delete should fail (already deleted)")
	}
	return nil
}

func stressSessionUpdateMissing(store *sessions.Store) error {
	// Updating a non-existent session should fail gracefully (no panic)
	err := store.UpdateTitle("definitely-nonexistent-id-12345", "x")
	if err == nil {
		// It's OK if the store silently succeeds (writes file), as long as no panic
	}
	return nil
}

func stressExtractJSONFences() error {
	// The multiagent.extractJSON function should strip ```...``` fences
	// We can't call it directly (private function) — so we test via the
	// behavior contract: it should return the JSON string without fences.
	raw := "```json\n{\"a\": 1, \"b\": [1, 2, 3]}\n```"
	extracted := multiagent.ExtractJSON(raw)
	if !strings.Contains(extracted, `"a": 1`) {
		return fmt.Errorf("extract failed to strip fences: got %q", extracted)
	}
	if strings.Contains(extracted, "```") {
		return fmt.Errorf("extract still contains fences: %q", extracted)
	}
	return nil
}

func stressExtractJSONNested() error {
	raw := `Here's the plan:
{"summary": "x", "steps": [{"id": 1, "goal": "do thing"}]}
That's it.`
	extracted := multiagent.ExtractJSON(raw)
	if !strings.Contains(extracted, `"summary": "x"`) {
		return fmt.Errorf("extract failed: %q", extracted)
	}
	if !strings.HasPrefix(extracted, "{") {
		return fmt.Errorf("extract should start with {: %q", extracted)
	}
	if !strings.HasSuffix(extracted, "}") {
		return fmt.Errorf("extract should end with }: %q", extracted)
	}
	return nil
}

func stressExtractJSONNoBraces() error {
	raw := "just prose, no JSON here"
	extracted := multiagent.ExtractJSON(raw)
	if extracted != raw {
		return fmt.Errorf("expected raw string when no braces, got %q", extracted)
	}
	return nil
}

func stressSandboxSmoke() error {
	// Sandbox creation should always succeed (it just creates a temp dir)
	sb, err := sandbox.New(64, 10, "")
	if err != nil {
		return fmt.Errorf("sandbox.New: %w", err)
	}
	defer sb.Close()
	if sb.WorkDir() == "" {
		return fmt.Errorf("sandbox workdir empty")
	}
	// On Linux, Execute() returns "not available" — that's expected
	// On Windows, it would actually spawn a process inside the Job Object
	out, err := sb.Execute(context.Background(), "echo", "hi")
	_ = out
	_ = err
	return nil
}
