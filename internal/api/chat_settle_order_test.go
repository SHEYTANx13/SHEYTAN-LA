package api

// chat_settle_order_test.go — v1.10.13 Task 30 (run #33 R2) regression pin.
//
// The pinned defect: the orchestrator's terminal `done`/`complete`
// activities were broadcast INSIDE RunDetailed, while the assistant
// reply's AppendMessage runs in finishSuccessfulRun — after RunDetailed
// returns. A client that reloads the transcript when the settle frame
// arrives (the Qt client's handleActivityFrame reloads exactly once on
// done/complete) races the append; on a fast local pipe the reload wins
// and reads a transcript without the reply, and a plain chat publishes
// nothing after the append — the stale one-message model then persists
// past every bound (the Windows run-#26/#33 chat-persist
// 'firstPersisted' FALSE class).
//
// The pinned contract (v1.2.9's own ordering rule, enforced at the
// broadcast boundary): the client-visible terminal frame never precedes
// the durable transcript write it announces. Asserted deterministically:
// the moment the subscribed socket receives the terminal frame, a
// GET /api/sessions/{id} ALREADY carries the assistant reply.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDoneFrameFollowsTranscriptCommit(t *testing.T) {
	engine := remoteFakeEngine(t, "Hello — I am here and working fine.")
	srv, server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	conn := dialActivityWS(t, server, sessionID)

	first := readFrameWithDeadline(t, conn)
	if first["type"] != "attached" {
		t.Fatalf("first frame type = %v, want attached", first["type"])
	}

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"hi"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	defer runResp.Body.Close()

	var runBody struct {
		OK    bool   `json:"ok"`
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(runResp.Body).Decode(&runBody); err != nil {
		t.Fatalf("decode run response: %v", err)
	}
	if !runBody.OK || runBody.RunID == "" {
		t.Fatalf("run response = %+v — runId missing", runBody)
	}

	// Read frames until the terminal done arrives. The held frames keep
	// their relative order, so the first terminal frame seen here is the
	// orchestrator's `done` — the exact frame a settling client reloads on.
	deadline := time.Now().Add(30 * time.Second)
	var terminalType string
	for time.Now().Before(deadline) {
		frame := readFrameWithDeadline(t, conn)
		switch frame["type"] {
		case "done":
			terminalType = "done"
		case "complete":
			if terminalType == "" {
				t.Fatalf("complete arrived before done — the held frames lost their relative order: %v", frame)
			}
		default:
			continue
		}
		if terminalType == "done" {
			break
		}
	}
	if terminalType != "done" {
		t.Fatal("the terminal done frame never arrived within 30s")
	}

	// THE CONTRACT: at the moment the client-visible terminal frame is
	// delivered, the transcript it announces is ALREADY durable. A client
	// reloading on the settle frame — the acceptance slot's exact
	// behavior — must never read a one-message transcript here.
	resp, err := http.Get(server.URL + "/api/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("GET session at settle: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET session body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET session = %d: %s", resp.StatusCode, string(raw))
	}
	var sess struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &sess); err != nil {
		t.Fatalf("session body is not a session document: %v", err)
	}
	replyPersisted := false
	for _, m := range sess.Messages {
		if m.Role == "assistant" && strings.Contains(m.Content, "working fine") {
			replyPersisted = true
		}
	}
	if !replyPersisted {
		t.Fatalf("ORDERING VIOLATION: the terminal done frame reached the socket while the transcript still lacks the assistant reply (messages=%+v) — a settle-triggered reload races the append (the run-#33 Windows chat-persist class)", sess.Messages)
	}

	// Settle tail: the run fully unwinds before the TempDir cleanup (the
	// same cleanup safety every run-transport test observes).
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the fired run never settled — cleanup safety violated")
	}
	if !waitForRunSettled(t, srv, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}
}
