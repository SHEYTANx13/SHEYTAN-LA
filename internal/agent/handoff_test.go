// handoff_test.go — v1.2.8 agent.md handoff tests.
package agent

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"path/filepath"
)

func TestWriteHandoffCreatesAndReplacesDynamicSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	h := Handoff{
		Task:         "Implement the deploy fix",
		Objective:    "Implement the deploy fix",
		CurrentState: "TEST: passed — go test ./...",
		ChangesMade:  []string{"run outcome: done", "patched scripts/build.sh"},
		FilesChanged: []string{"scripts/build.sh"},
		TestsAndVer:  []string{"tests run:", "  - go test ./...", "verification: verified — 1/1 checks passed"},
		NextAction:   "Push the tag",
		DoNotRedo:    []string{"scripts/build.sh"},
	}

	if err := WriteHandoffFile(path, h); err != nil {
		t.Fatalf("WriteHandoffFile (create): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	first := string(data)

	// The section structure is present and machine-parseable.
	for _, want := range []string{
		HandoffBeginMarker,
		"# Latest Agent Handoff",
		"## Task",
		"## Objective",
		"## Changes made",
		"## Files changed",
		"## Tests and verification",
		"## Recommended next action",
		"## Do not redo",
		HandoffEndMarker,
		"- scripts/build.sh",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("handoff section missing %q:\n%s", want, first)
		}
	}

	// Stable content BEFORE the section survives a second write.
	stable := "# SHEYTAN-Local-Agent — Agent Context\n\nStable engineering rules stay here.\n"
	if err := os.WriteFile(path, []byte(stable), 0o644); err != nil {
		t.Fatalf("seed stable content: %v", err)
	}

	h2 := h
	h2.Task = "Second run"
	h2.Objective = "Second run"
	if err := WriteHandoffFile(path, h2); err != nil {
		t.Fatalf("WriteHandoffFile (update): %v", err)
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after update: %v", err)
	}
	updated := string(data)

	if !strings.HasPrefix(updated, "# SHEYTAN-Local-Agent — Agent Context") {
		t.Fatalf("stable content lost:\n%s", updated)
	}

	if !strings.Contains(updated, "Stable engineering rules stay here.") {
		t.Fatal("stable engineering instructions were not preserved")
	}

	if strings.Count(updated, HandoffBeginMarker) != 1 {
		t.Fatalf("expected exactly ONE dynamic section, found %d", strings.Count(updated, HandoffBeginMarker))
	}

	if strings.Contains(updated, "Implement the deploy fix\n") {
		t.Fatal("the previous handoff content should be fully replaced")
	}

	if !strings.Contains(updated, "Second run") {
		t.Fatal("the new handoff content is missing")
	}
}

func TestWriteHandoffReplacesTornSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	// A dangling begin marker (crash mid-write) is treated as absent.
	torn := "# Header\n\n" + HandoffBeginMarker + "\n## Task\npartial\n"
	if err := os.WriteFile(path, []byte(torn), 0o644); err != nil {
		t.Fatalf("seed torn file: %v", err)
	}

	if err := WriteHandoffFile(path, Handoff{Task: "recovery run"}); err != nil {
		t.Fatalf("WriteHandoffFile: %v", err)
	}

	data, _ := os.ReadFile(path)
	out := string(data)

	if strings.Count(out, HandoffBeginMarker) != 1 {
		t.Fatalf("torn marker not superseded:\n%s", out)
	}
	if !strings.Contains(out, "recovery run") {
		t.Fatal("new section missing")
	}
}

func TestHandoffFileNameIsLowercase(t *testing.T) {
	// Windows filesystems may treat agent.md and Agent.md as the SAME
	// path — the constant is lowercase by contract.
	if HandoffFileName != "agent.md" {
		t.Fatalf("HandoffFileName = %q, want exactly agent.md", HandoffFileName)
	}
}

func TestHandoffFromTaskStateFactualMapping(t *testing.T) {
	ts := NewTaskState("Ship the fix")
	ts.FilesChanged = append(ts.FilesChanged, "main.go")
	ts.CommandsRun = append(ts.CommandsRun, "go build ./...")
	ts.TestsRun = append(ts.TestsRun, "go test ./internal/...")
	ts.Failures = append(ts.Failures, "shell: compile error")
	ts.SetVerification("verified", "1/1 checks passed")

	h := HandoffFromTaskState(*ts, ts.Verification, "done")

	if h.Task != "Ship the fix" {
		t.Fatalf("task mapping wrong: %q", h.Task)
	}

	joined := strings.Join(h.TestsAndVer, "\n")
	if !strings.Contains(joined, "go test ./internal/...") ||
		!strings.Contains(joined, "verified — 1/1 checks passed") {
		t.Fatalf("tests/verification mapping wrong:\n%s", joined)
	}

	foundFile := false
	for _, item := range h.FilesChanged {
		if item == "main.go" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatal("files changed not carried into the handoff")
	}
}

// --- handoff write/read contracts ---

// TestNoChangeAgentRunWritesHonestHandoff pins the v1.2.8.1 product
// contract: an evidence-free completed run (no files, no commands, no
// tests, no repairs, no artifacts) still produces a usable, honest
// handoff — never an empty one, never a skipped write.
func TestNoChangeAgentRunWritesHonestHandoff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	// A run that only answered prose: no engineering evidence at all.
	ts := NewTaskState("Explain the deployment layout")

	h := HandoffFromTaskState(ts.snapshot(), "", "done")

	if err := WriteHandoffFile(path, h); err != nil {
		t.Fatalf("no-change handoff write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	content := string(data)

	for _, want := range []string{
		"No engineering changes were made.",
		"## Task",
		"## Current state",
		"## Changes made",
		"## Remaining work",
		"None recorded.",
		"## Recommended next action",
		HandoffBeginMarker,
		HandoffEndMarker,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("honest no-change handoff missing %q:\n%s", want, content)
		}
	}

	if strings.Contains(content, "## Files changed") {
		t.Fatal("no-change handoff must not invent a Files changed section")
	}
}

// TestHandoffBytePreservationOutsideMarkers pins byte-exact preservation:
// odd whitespace, CRLF-ish content and trailing text after the markers
// survive a handoff replacement untouched.
func TestHandoffBytePreservationOutsideMarkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	before := "# Rules\n\n  indented   line \n\n\nkeep   this   spacing\n"
	after := "\n\n\nTrailing notes: kept exactly.\n"

	seed := before + HandoffBeginMarker + "\n# Latest Agent Handoff\n\nold content\n" + HandoffEndMarker + after
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := Handoff{Task: "second run", Objective: "second run"}
	if err := WriteHandoffFile(path, h); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := os.ReadFile(path)
	out := string(data)

	if !strings.HasPrefix(out, before) {
		t.Fatalf("bytes before the markers were rewritten:\n%q", out[:min(len(out), len(before)+20)])
	}

	if !strings.HasSuffix(out, after) {
		t.Fatalf("bytes after the markers were rewritten:\n%q", out)
	}

	if strings.Count(out, HandoffBeginMarker) != 1 {
		t.Fatalf("expected exactly one marker pair:\n%s", out)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestHandoffReadErrorAborts pins the durability repair: an existing but
// unreadable agent.md must NEVER be silently replaced (the v1.2.8 code
// treated any read error as "absent" and clobbered the file).
func TestHandoffReadErrorAborts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	original := "# Precious content\n\nDo not lose this.\n"
	if err := os.WriteFile(path, []byte(original), 0o000); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteHandoffFile(path, Handoff{Task: "clobber attempt"}); err == nil {
		// Restore permissions so TempDir cleanup works, then fail.
		_ = os.Chmod(path, 0o644)
		t.Fatal("an unreadable agent.md was replaced instead of aborting")
	}

	// The failure must be honest AND the file untouched. Restore read
	// permission first (the test itself needs it to verify the bytes).
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("restore permissions: %v", err)
	}

	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("original file unreadable after failed handoff: %v", rerr)
	}

	if string(data) != original {
		t.Fatalf("original content changed:\n%s", string(data))
	}

	if strings.Contains(string(data), "clobber attempt") {
		t.Fatal("the handoff was written despite the read error")
	}
}

// TestHandoffReadBackVerification errors when the write cannot be
// verified — a handoff is only "completed" after the file on disk is
// proven to carry the section.
func TestHandoffReadBackVerification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	h := Handoff{Task: "verified write", Objective: "verified write"}

	if err := WriteHandoffFile(path, h); err != nil {
		t.Fatalf("write: %v", err)
	}

	// verifyHandoffContent accepts the real file…
	data, _ := os.ReadFile(path)
	if !verifyHandoffContent(string(data), RenderHandoff(h)) {
		t.Fatal("read-back verification rejected a correct file")
	}

	// …and rejects a file whose section was tampered with.
	tampered := strings.Replace(string(data), "verified write", "tampered!", 1)
	if verifyHandoffContent(tampered, RenderHandoff(h)) {
		t.Fatal("read-back verification accepted tampered content")
	}
}

// --- concurrent handoff writes ---

// Concurrent agent.md writes
// are SERIALIZED per workspace path, so two runs (or a run racing the
// CLI) can never silently drop each other's handoff section.

// TestConcurrentHandoffWritesAllSurvive pins the serialization: N
// concurrent writers each perform a read-splice-rename cycle against the
// SAME agent.md. Before the per-path lock, a writer's base could be
// read before another writer's rename, silently erasing the earlier
// section. After the fix, the file converges to a valid single-section
// state and — when writers extend different stable parts — no run's
// content is silently lost mid-sequence.
func TestConcurrentHandoffWritesAllSurvive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	const writers = 16

	// Pre-existing stable content OUTSIDE the section (must survive
	// byte-for-byte, whatever the interleaving).
	stable := "# Agent Handoff\n\nStable engineering instructions — never rewritten.\n\n"
	if err := os.WriteFile(path, []byte(stable), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, writers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			h := Handoff{
				Task:      fmt.Sprintf("task %d", n),
				Objective: fmt.Sprintf("objective %d", n),
			}
			if err := WriteHandoffFile(path, h); err != nil {
				errs <- fmt.Errorf("writer %d: %w", n, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)

	// 1. Exactly ONE marker pair (no interleaved duplication).
	if got := strings.Count(content, HandoffBeginMarker); got != 1 {
		t.Fatalf("begin markers = %d, want 1 (interleaved writes corrupted the section):\n%s", got, content)
	}
	if got := strings.Count(content, HandoffEndMarker); got != 1 {
		t.Fatalf("end markers = %d, want 1", got)
	}

	// 2. The stable content survived byte-for-byte.
	if !strings.HasPrefix(content, stable) {
		t.Fatalf("stable prefix was rewritten by a concurrent handoff write:\n%q", content)
	}

	// 3. The section content is internally consistent: the rendered
	// section parses (Task and Objective from the SAME writer — a
	// mixed splice would pair task N with objective M).
	begin := strings.Index(content, HandoffBeginMarker)
	end := strings.Index(content, HandoffEndMarker)
	section := content[begin:end]

	// Consistency check: extract "task N" and "objective N" and require
	// a matching N (a mixed splice would pair task N with objective M).
	var taskN, objN = -1, -1
	for i := 0; i < writers; i++ {
		if strings.Contains(section, fmt.Sprintf("task %d\n", i)) || strings.HasSuffix(section, fmt.Sprintf("task %d", i)) {
			taskN = i
		}
		if strings.Contains(section, fmt.Sprintf("objective %d\n", i)) || strings.HasSuffix(section, fmt.Sprintf("objective %d", i)) {
			objN = i
		}
	}
	if taskN != objN {
		t.Fatalf("mixed splice: section pairs task %d with objective %d — the read-splice-rename was NOT serialized:\n%s", taskN, objN, section)
	}
}

// TestConcurrentHandoffDifferentPathsNoDeadlock ensures the keyed locks
// do not serialize UNRELATED files (and never deadlock).
func TestConcurrentHandoffDifferentPathsNoDeadlock(t *testing.T) {
	dir := t.TempDir()

	var wg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			path := filepath.Join(dir, fmt.Sprintf("ws-%d", n), HandoffFileName)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Errorf("mkdir: %v", err)
				return
			}
			if err := WriteHandoffFile(path, Handoff{Task: fmt.Sprintf("t%d", n)}); err != nil {
				t.Errorf("write %s: %v", path, err)
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// all writes completed — no cross-path serialization or deadlock
	case <-time.After(10 * time.Second):
		t.Fatal("independent-path handoff writes deadlocked — the per-path locks must not interact")
	}
}
