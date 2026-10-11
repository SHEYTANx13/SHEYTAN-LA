package lab

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// newSafeEditTool spins a Coding Lab tool against a source tree with the
// given files, plus a started task, and returns the tool and the task ID.
func newSafeEditTool(t *testing.T, files map[string]string) (*Tool, string) {
	t.Helper()

	root := t.TempDir()
	source := filepath.Join(root, "source")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	for rel, content := range files {
		full := filepath.Join(source, rel)

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	cfg.DataDir = root
	cfg.ModelsDir = filepath.Join(root, "models")
	cfg.SessionsDir = filepath.Join(root, "sessions")
	cfg.LabEnabled = true
	cfg.LabWorkspaceRoot = filepath.Join(root, "lab", "workspaces")
	cfg.LabCommandTimeoutSec = 5
	cfg.LabKeepWorkspaces = false
	cfg.LabAllowNetwork = false

	tool, err := NewTool(cfg)
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	startOutput, err := tool.Run(
		context.Background(),
		marshalLabTestArgs(t, map[string]any{
			"action": "start_task",
			"title":  "safe edit test",
			"source": source,
		}),
	)
	if err != nil {
		t.Fatalf("start_task: %v", err)
	}

	return tool, extractTaskID(t, startOutput)
}

func TestReadFileIsBoundedAndNumbered(t *testing.T) {
	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, strings.Repeat("x", 10))
	}

	tool, taskID := newSafeEditTool(t, map[string]string{
		"big.txt": strings.Join(lines, "\n"),
	})

	out, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action": "read_file",
		"taskId": taskID,
		"path":   "big.txt",
		"offset": 10,
		"limit":  5,
	}))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}

	if !strings.Contains(out, `"startLine":10`) || !strings.Contains(out, `"endLine":14`) {
		t.Fatalf("read_file window wrong: %s", out)
	}

	if !strings.Contains(out, `"totalLines":50`) || !strings.Contains(out, `"truncated":true`) {
		t.Fatalf("read_file bounds not reported: %s", out)
	}

	// Line-numbered rendering: the model sees "   10| xxxxxxxxxx".
	if !strings.Contains(out, "  10| ") {
		t.Fatalf("line numbering missing: %s", out)
	}
}

func TestReadFileRefusesBinary(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"blob.bin": "text\x00binary",
	})

	_, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action": "read_file",
		"taskId": taskID,
		"path":   "blob.bin",
	}))

	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("read_file must refuse binary, got: %v", err)
	}
}

func TestEditFileAppliesAnchoredEdit(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"main.go": "package main\n\nfunc main() {\n\tprintln(\"old\")\n}\n",
	})

	out, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "edit_file",
		"taskId":  taskID,
		"path":    "main.go",
		"oldText": "println(\"old\")",
		"newText": "println(\"new\")",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v (%s)", err, out)
	}

	if !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"line":4`) {
		t.Fatalf("edit result wrong: %s", out)
	}

	// The message must tell the model the edit is not yet verified.
	if !strings.Contains(out, "NOT verified") {
		t.Fatalf("edit message must demand verification: %s", out)
	}

	// Confirm on disk through read_file (tool output is JSON, so quotes
	// arrive escaped — assert on the decoded content).
	var readBack struct {
		File struct {
			Content string `json:"content"`
		} `json:"file"`
	}

	out2, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action": "read_file",
		"taskId": taskID,
		"path":   "main.go",
	}))
	if err != nil {
		t.Fatalf("read_file after edit: %v", err)
	}

	if err := json.Unmarshal([]byte(out2), &readBack); err != nil {
		t.Fatalf("decode read_file: %v", err)
	}

	if !strings.Contains(readBack.File.Content, `println("new")`) {
		t.Fatalf("edit did not land: %s", readBack.File.Content)
	}
}

func TestEditFileRefusesStaleAnchor(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"a.txt": "current content\n",
	})

	out, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "edit_file",
		"taskId":  taskID,
		"path":    "a.txt",
		"oldText": "content that is not there",
		"newText": "replacement",
	}))
	if err == nil {
		t.Fatal("stale anchor must fail")
	}

	if !errors.Is(err, ErrEditStale) {
		t.Fatalf("want ErrEditStale, got: %v", err)
	}

	if !strings.Contains(out, "re-read") {
		t.Fatalf("refusal must instruct a re-read: %s", out)
	}

	// File untouched.
	out2, _ := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action": "read_file",
		"taskId": taskID,
		"path":   "a.txt",
	}))
	if !strings.Contains(out2, "current content") {
		t.Fatalf("stale refusal must not mutate: %s", out2)
	}
}

func TestEditFileRefusesAmbiguousAnchor(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"a.txt": "same\nsame\nsame\n",
	})

	_, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "edit_file",
		"taskId":  taskID,
		"path":    "a.txt",
		"oldText": "same",
		"newText": "different",
	}))

	if !errors.Is(err, ErrEditAmbiguous) {
		t.Fatalf("want ErrEditAmbiguous, got: %v", err)
	}
}

func TestEditFileRefusesNoopAndEmptyAnchor(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"a.txt": "hello\n",
	})

	_, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "edit_file",
		"taskId":  taskID,
		"path":    "a.txt",
		"oldText": "hello",
		"newText": "hello",
	}))
	if !errors.Is(err, ErrEditNoop) {
		t.Fatalf("want ErrEditNoop, got: %v", err)
	}

	_, err = tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "edit_file",
		"taskId":  taskID,
		"path":    "a.txt",
		"oldText": "   ",
		"newText": "x",
	}))
	if !errors.Is(err, ErrEditEmptyAnchor) {
		t.Fatalf("want ErrEditEmptyAnchor, got: %v", err)
	}
}

func TestEditFilePathJail(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"a.txt": "inside\n",
	})

	for _, escape := range []string{"../a.txt", "a/../../a.txt"} {
		_, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
			"action":  "edit_file",
			"taskId":  taskID,
			"path":    escape,
			"oldText": "inside",
			"newText": "escaped",
		}))
		if err == nil || !strings.Contains(err.Error(), "workspace") {
			t.Fatalf("path escape %q must be refused (got %v)", escape, err)
		}
	}
}

func TestEditFileInvalidatesVerification(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"a.txt": "value: 1\n",
	})

	// Verify first (check passes).
	_, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action": "verify",
		"taskId": taskID,
		"checks": []map[string]any{
			{
				"name":     "content",
				"command":  verificationContentCheck("a.txt", "1"),
				"required": true,
			},
		},
	}))
	if err != nil {
		t.Fatalf("verify before edit: %v", err)
	}

	// Edit mutates the workspace.
	_, err = tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "edit_file",
		"taskId":  taskID,
		"path":    "a.txt",
		"oldText": "value: 1",
		"newText": "value: 2",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}

	// Task verification state must be invalidated: finish must refuse.
	out, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action": "finish",
		"taskId": taskID,
	}))
	if err == nil {
		t.Fatalf("finish after unverified edit must fail: %s", out)
	}
}

func TestEditFileCRLFNormalization(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"win.txt": "line one\r\nline two\r\n",
	})

	// The model supplies LF-normalized anchors; the edit must still land.
	_, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "edit_file",
		"taskId":  taskID,
		"path":    "win.txt",
		"oldText": "line two",
		"newText": "line 2",
	}))
	if err != nil {
		t.Fatalf("edit_file CRLF: %v", err)
	}

	out, _ := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action": "read_file",
		"taskId": taskID,
		"path":   "win.txt",
	}))
	if !strings.Contains(out, "line 2") {
		t.Fatalf("CRLF edit did not land: %s", out)
	}
}

func TestEditFileCreatesNoTempResidue(t *testing.T) {
	tool, taskID := newSafeEditTool(t, map[string]string{
		"a.txt": "keep\n",
	})

	_, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "edit_file",
		"taskId":  taskID,
		"path":    "a.txt",
		"oldText": "keep",
		"newText": "kept",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}

	out, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "run",
		"taskId":  taskID,
		"command": "ls -a",
	}))
	if err != nil {
		t.Fatalf("ls: %v", err)
	}

	if strings.Contains(out, ".sheytan-edit-") {
		t.Fatalf("temp file leaked into workspace: %s", out)
	}
}
