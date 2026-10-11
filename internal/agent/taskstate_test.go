// taskstate_test.go — v1.2.8 agent task memory tests.
package agent

import (
	"strings"
	"testing"
)

func TestTaskStateObservesFilesAndCommands(t *testing.T) {
	ts := NewTaskState("Fix the deploy bug in the release pipeline. Must not touch the API.")

	ts.ObserveToolStart("files", `{"action":"read","path":"scripts/build.sh"}`)
	ts.ObserveToolEnd("files", `{"action":"read","path":"scripts/build.sh"}`, "file contents…", false)

	ts.ObserveToolStart("files", `{"action":"write","path":"scripts/build.sh"}`)
	ts.ObserveToolEnd("files", `{"action":"write","path":"scripts/build.sh"}`, "ok", false)

	ts.ObserveToolStart("shell", `{"command":"go test ./..."}`)
	ts.ObserveToolEnd("shell", `{"command":"go test ./..."}`, "ok   github.com/x 0.4s", false)

	snap := ts.snapshot()

	if len(snap.FilesInspected) != 1 || snap.FilesInspected[0] != "scripts/build.sh" {
		t.Fatalf("filesInspected wrong: %+v", snap.FilesInspected)
	}

	if len(snap.FilesChanged) != 1 || snap.FilesChanged[0] != "scripts/build.sh" {
		t.Fatalf("filesChanged wrong: %+v", snap.FilesChanged)
	}

	if len(snap.CommandsRun) != 1 || snap.CommandsRun[0] != "go test ./..." {
		t.Fatalf("commandsRun wrong: %+v", snap.CommandsRun)
	}

	if len(snap.TestsRun) != 1 {
		t.Fatalf("testsRun wrong: %+v", snap.TestsRun)
	}

	if !strings.Contains(snap.CurrentStep, "TEST") {
		t.Fatalf("current step should reflect the test run: %q", snap.CurrentStep)
	}

	if !snap.HasEngineeringEvidence() {
		t.Fatal("engineering evidence missing (files + commands + tests recorded)")
	}

	// Constraints come from the user's own words.
	if len(snap.Constraints) == 0 {
		t.Fatal("constraints not extracted from the goal")
	}
}

func TestTaskStateRecordsFailureThenRepair(t *testing.T) {
	ts := NewTaskState("make the build green")

	ts.ObserveToolEnd("shell", `{"command":"go build ./..."}`, "compile error: undefined: X", true)
	ts.ObserveToolEnd("shell", `{"command":"go build ./..."}`, "build succeeded", false)

	snap := ts.snapshot()

	if len(snap.Failures) != 1 {
		t.Fatalf("failures wrong: %+v", snap.Failures)
	}

	if len(snap.Repairs) == 0 {
		t.Fatalf("repair evidence missing after recovery: %+v", snap)
	}
}

func TestTaskStateBounded(t *testing.T) {
	ts := NewTaskState("stress")

	for i := 0; i < 30; i++ {
		ts.ObserveToolStart("files", `{"action":"write","path":"file`+string(rune('a'+i%26))+`.go"}`)
		ts.ObserveToolEnd("files", `{"action":"write","path":"file`+string(rune('a'+i%26))+`.go"}`, "ok", false)
	}

	snap := ts.snapshot()

	if len(snap.FilesChanged) > taskMaxItems {
		t.Fatalf("filesChanged grew to %d, cap %d", len(snap.FilesChanged), taskMaxItems)
	}

	for _, item := range snap.FilesChanged {
		if len(item) > taskMaxItemChars+3 {
			t.Fatalf("item exceeds clip: %q", item)
		}
	}
}

func TestTaskStateNilSafety(t *testing.T) {
	var ts *TaskState

	ts.ObserveToolStart("files", "{}")
	ts.ObserveToolEnd("files", "{}", "ok", false)
	ts.SetVerification("verified", "ok")

	snap := ts.snapshot()
	if snap.Verification != "" || snap.HasEngineeringEvidence() {
		t.Fatal("nil task state must stay empty")
	}
}

func TestIsTestCommand(t *testing.T) {
	cases := map[string]bool{
		"go test ./internal/...": true,
		"npm run build":          true,
		"cargo test --release":   true,
		"make test":              true,
		"ls -la":                 false,
		"cat main.go":            false,
		"echo go test later":     true, // substring-precise by design
	}

	for cmd, want := range cases {
		if got := isTestCommand(cmd); got != want {
			t.Fatalf("isTestCommand(%q) = %v, want %v", cmd, got, want)
		}
	}
}
