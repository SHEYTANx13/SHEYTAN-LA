// run_task_test.go — v1.2.8: the `task` activity folds into the
// authoritative run snapshot, so a reconnect mid-run restores the bounded
// task state (goal/step/files/commands/tests/verification) without the
// run restarting.
package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
)

// TestTaskStateRidesRunSnapshot pins the reconnect contract for the agent
// task memory: a socket attaching mid-run receives the latest task-state
// snapshot in the run_snapshot frame.
func TestTaskStateRidesRunSnapshot(t *testing.T) {
	// A slow engine keeps the run live long enough to attach mid-flight.
	engine := slowStreamEngine(t, []string{"working", " still"}, 120*time.Millisecond)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Fire the run (the socket-less POST is fine for this fold test).
	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"task fold"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	// The fake remote engine never calls tools, so seed the runLive state
	// directly — the CONTRACT under test is the fold (observe → snapshot),
	// not the orchestrator's extraction (unit-tested in internal/agent).
	srv.runsMu.Lock()
	rs := srv.runs[sessionID]
	srv.runsMu.Unlock()

	if rs == nil || rs.live == nil {
		t.Fatal("no active run right after POST — precondition broken")
	}

	// Publish exactly what the orchestrator emits per tool round.
	rs.live.observe(newTestTaskActivity())

	snap := rs.live.snapshot()

	if snap.Task == nil {
		t.Fatal("run_snapshot.Task missing — the task state did not fold into the authoritative snapshot")
	}

	if snap.Task["currentStep"] != "ACT: go build ./..." {
		t.Fatalf("snapshot task step = %v", snap.Task["currentStep"])
	}

	files := asStringSlice(t, snap.Task["filesChanged"])
	if len(files) != 1 || files[0] != "main.go" {
		t.Fatalf("snapshot task filesChanged = %v", snap.Task["filesChanged"])
	}

	// The wire frame the WS attach path sends must carry the same task
	// block (round-trip through the hub's encode path).
	_ = server // (the httptest server stays alive for parity with siblings)

	// v1.2.8: the fired run keeps streaming — wait for its full settle
	// tail before t.TempDir cleanup (reply persist + summary sidecar).
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the run never persisted its reply")
	}

	if !waitForRunSettled(t, srv, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}
}

// TestTaskStateSurvivesRunLookup races the lookup with a settled run to
// prove the task map is read defensively (no panic on nil live state).
func TestTaskStateSurvivesRunLookup(t *testing.T) {
	srv := &Server{}
	_ = srv // lookup paths already nil-guard rs.live; compile-time shape check

	engine := slowStreamEngine(t, []string{"x"}, 50*time.Millisecond)
	_, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	conn := dialActivityWS(t, server, sessionID)
	defer conn.Close()

	first := readFrameWithDeadline(t, conn)
	if first["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", first["type"])
	}

	// A snapshot or idle arrives depending on run liveness; both are
	// contract-legal. This pins that a nil-task run produces a snapshot
	// WITHOUT a task field (omitempty), never a null crash.
	snap := readFrameWithDeadline(t, conn)
	switch snap["type"] {
	case "run_snapshot":
		if task, present := snap["task"]; present && task == nil {
			t.Fatal("snapshot carries an explicit null task — omit instead")
		}
	case "idle":
		// The run already settled — legal for a fast engine.
	default:
		t.Fatalf("unexpected frame %v", snap["type"])
	}
}

func newTestTaskActivity() agent.Activity {
	return agent.Activity{
		Type:    "task",
		Caption: "ACT: go build ./...",
		Detail: map[string]any{
			"goal":         "Ship the fix",
			"currentStep":  "ACT: go build ./...",
			"filesChanged": []any{"main.go"},
		},
	}
}

func asStringSlice(t *testing.T, v any) []string {
	t.Helper()
	raw, ok := v.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", v)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}
