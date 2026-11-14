package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- v1.11.1 structured-sink (JSONL) output tests ---
//
// The work order §5 requires proof that tools.jsonl / llm.jsonl really
// receive records, that the JSON lines are valid, that clipping applies
// to oversized payloads, that the nil-sink path preserves the record as
// emergency evidence, and that JSONL rotation works. app.log paths were
// already covered by sinkhealth_test.go / faultinjection_test.go; these
// tests close the structured-sink gap.

// TestJSONLOutputWritesValidRecords drives ToolCall and LLMCall and
// asserts the exact bytes on disk: one JSON object per line with the
// documented fields, no array wrapper, newline-terminated.
func TestJSONLOutputWritesValidRecords(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove log dir (descriptor leak?): %v", err)
		}
	})

	m.ToolCall(ToolCallRecord{
		TS:         time.Now(),
		Tool:       "read_file",
		Args:       `{"path":"/tmp/x.txt"}`,
		Result:     "contents",
		DurationMs: 42,
		Session:    "s1",
	})
	m.LLMCall(LLMCallRecord{
		TS:              time.Now(),
		Provider:        "llama.cpp",
		Model:           "test-model",
		PromptMsgs:      2,
		PromptChars:     100,
		CompletionChars: 300,
		FinishReason:    "stop",
		DurationMs:      900,
	})

	for _, name := range []string{"tools.jsonl", "llm.jsonl"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v — the structured sink must exist after a recorded call", name, err)
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if len(lines) != 1 {
			t.Fatalf("%s carries %d lines, want exactly 1 record", name, len(lines))
		}
		// Every line must be a VALID JSON OBJECT (the structured-sink
		// contract the diagnostics consumers rely on).
		var obj map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &obj); err != nil {
			t.Fatalf("%s line is not a valid JSON object: %v", name, err)
		}
		switch name {
		case "tools.jsonl":
			if obj["tool"] != "read_file" {
				t.Fatalf("tools.jsonl tool field = %v", obj["tool"])
			}
			if obj["durationMs"].(float64) != 42 {
				t.Fatalf("tools.jsonl durationMs = %v", obj["durationMs"])
			}
		case "llm.jsonl":
			if obj["provider"] != "llama.cpp" || obj["model"] != "test-model" {
				t.Fatalf("llm.jsonl provider/model = %v/%v", obj["provider"], obj["model"])
			}
			if obj["finishReason"] != "stop" {
				t.Fatalf("llm.jsonl finishReason = %v", obj["finishReason"])
			}
		}
	}

	// Both structured sinks report ready in the observable health state.
	if h := m.Health(); !h.SinksReady {
		t.Fatalf("Health().SinksReady = false after successful JSONL writes: %+v", h)
	}
}

// TestJSONLPayloadsClipped proves the bounded-payload contract: an
// oversized tool result is clipped on disk (the record never grows
// unbounded), while normal fields survive.
func TestJSONLPayloadsClipped(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	huge := strings.Repeat("x", 100_000)
	m.ToolCall(ToolCallRecord{Tool: "t", Result: huge, Error: strings.Repeat("e", 5000)})

	data, err := os.ReadFile(filepath.Join(dir, "tools.jsonl"))
	if err != nil {
		t.Fatalf("read tools.jsonl: %v", err)
	}
	var rec ToolCallRecord
	line := strings.TrimRight(string(data), "\n")
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("tools.jsonl invalid: %v", err)
	}
	if len(rec.Result) >= len(huge) {
		t.Fatalf("result field not clipped on disk (%d chars)", len(rec.Result))
	}
	// The error field clips at 1024 per the writer contract.
	if len(rec.Error) > 1024+64 {
		t.Fatalf("error field not clipped (%d chars)", len(rec.Error))
	}
	if !strings.Contains(rec.Result, "clipped") && len(rec.Result) == 0 {
		t.Fatal("clipped result lost entirely — clipping must preserve a bounded prefix")
	}
}

// TestJSONLNilSinkPreservesRecordAsEmergency proves the structured-sink
// failure path: with tools.jsonl unavailable, the record is NOT silently
// dropped — a bounded emergency summary (no payloads) is preserved and
// the health state reports the sink failure.
func TestJSONLNilSinkPreservesRecordAsEmergency(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close after nil JSONL sink: %v", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove log dir (descriptor leak?): %v", err)
		}
	})

	m.mu.Lock()
	old := m.toolF
	m.toolF = nil
	m.mu.Unlock()
	// Mirror the production rotation path: release the replaced handle so
	// the temporary directory stays removable on Windows.
	_ = old.Close()

	m.ToolCall(ToolCallRecord{Tool: "secret_tool", Args: `{"password":"hunter2"}`, DurationMs: 7})

	h := m.Health()
	if h.SinksReady {
		t.Fatal("Health().SinksReady must be false while the structured sink is broken")
	}
	if !strings.Contains(h.LastErr, "tools.jsonl sink unavailable") {
		t.Fatalf("Health().LastErr = %q, want the structured sink failure", h.LastErr)
	}
	emergency := m.Emergency(10)
	if len(emergency) == 0 {
		t.Fatal("the lost record must be preserved as emergency evidence, not discarded")
	}
	joined := strings.Join(emergency, "\n")
	// The summary names the tool but NEVER carries the payloads.
	if !strings.Contains(joined, "tool=secret_tool") {
		t.Fatalf("emergency evidence missing the record identity: %q", joined)
	}
	if strings.Contains(joined, "hunter2") {
		t.Fatal("emergency evidence leaked record payloads — summaries must stay payload-free")
	}
}

// TestJSONLRotationShiftsAndReopens forces a tools.jsonl rotation with a
// tiny threshold override (via direct bookkeeping, the same deterministic
// shape the app.log rotation tests use) and proves the shift + reopen:
// the pre-rotation record survives in the shifted generation and the sink
// stays writable afterwards.
func TestJSONLRotationShiftsAndReopens(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Errorf("Close after JSONL rotation: %v", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove log dir (descriptor leak?): %v", err)
		}
	})

	m.ToolCall(ToolCallRecord{Tool: "before_rotation", Result: "kept"})

	// The next record crosses the rotation threshold.
	m.mu.Lock()
	m.toolN = jsonlMaxBytes - 1
	m.mu.Unlock()
	m.ToolCall(ToolCallRecord{Tool: "triggers_rotation"})

	// Pre-rotation bytes survive in the shifted generation.
	shifted, err := os.ReadFile(filepath.Join(dir, "tools.jsonl.1"))
	if err != nil {
		t.Fatalf("read shifted tools.jsonl.1: %v — rotation did not shift the first generation", err)
	}
	if !strings.Contains(string(shifted), "before_rotation") {
		t.Fatalf("shifted generation missing the pre-rotation record: %q", shifted)
	}
	// The sink reopened: a fresh record lands in the new tools.jsonl.
	m.ToolCall(ToolCallRecord{Tool: "after_rotation"})
	fresh, err := os.ReadFile(filepath.Join(dir, "tools.jsonl"))
	if err != nil {
		t.Fatalf("read fresh tools.jsonl: %v", err)
	}
	if !strings.Contains(string(fresh), "after_rotation") {
		t.Fatalf("fresh tools.jsonl missing the post-rotation record: %q", fresh)
	}
	if strings.Contains(string(fresh), "before_rotation") {
		t.Fatal("fresh tools.jsonl must only carry post-rotation records")
	}
	if h := m.Health(); !h.SinksReady {
		t.Fatalf("Health().SinksReady = false after a successful JSONL rotation: %+v", h)
	}
}
