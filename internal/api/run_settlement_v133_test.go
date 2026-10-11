package api

// run_settlement_v133_test.go — v1.3.3 settlement regression coverage.
//
// The v1.3.2 CI failure (run 35552680611) exposed two contract gaps:
//
//   1. TEST GAP: the settlement barrier waited for the rolling summary
//      (durable step 1) as a proxy for complete settlement, while
//      production writes the summary BEFORE the agent.md handoff
//      (durable step 2) — a poll landing in that window read a missing
//      agent.md ("agent.md handoff missing"). Fixed by the
//      terminal-outcome barrier (waitForRunSettled); pinned here by
//      settling the SAME run repeatedly and verifying BOTH artifacts
//      after every barrier crossing.
//
//   2. PRODUCTION GAP: a completed Agent run whose MANDATORY agent.md
//      handoff failed to write still settled "done" — a falsely
//      successful terminal result while the next agent starts without
//      its handoff. Fixed by the v1.3.3 mandatory-handoff honesty
//      demotion in server.go; pinned here by forcing a deterministic
//      handoff write failure and asserting the terminal outcome is
//      "error", never "done".

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// newRemoteServerWithWorkspaceRoot builds the standard remote-engine test
// server with an OVERRIDDEN workspace root — the injection point for
// deterministic handoff write failures (a regular FILE where the agent.md
// directory should be: os.CreateTemp fails with ENOTDIR, no timing, no
// permissions lottery).
func newRemoteServerWithWorkspaceRoot(t *testing.T, engineURL, wsRoot string) (*Server, *httptest.Server) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = cfg.DataDir + "/models"
	cfg.SessionsDir = cfg.DataDir + "/sessions"
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "remote"
	cfg.RemoteBaseURL = engineURL + "/v1"
	cfg.RemoteAPIKey = "test-key"
	cfg.RemoteModel = "fake-remote-model"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"
	cfg.WorkspaceRoot = wsRoot

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	if err := srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}

	t.Cleanup(srv.Close)

	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	return srv, server
}

// TestSettlementBarrierWaitsForBothSummaryAndHandoff pins the v1.3.3
// barrier semantics end to end: after EVERY settlement crossing, BOTH
// required durable artifacts of an agent run are already on disk — the
// rolling summary AND the agent.md handoff. Repeated settlement (three
// sequential runs in ONE session) also pins the registry-consumption
// ordering: each barrier must observe THAT run's terminal record, not a
// stale one left by the previous run.
func TestSettlementBarrierWaitsForBothSummaryAndHandoff(t *testing.T) {
	engine := toolStreamEngine(t)
	t.Cleanup(engine.Close)

	srv, server := newRemoteServerWithHandle(t, engine.URL)

	cfg := srv.src.Load()
	wsRoot := cfg.EffectiveWorkspaceRoot()

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	messages := []string{
		"Write the first evidence file now.",
		"Write the second evidence file now.",
		"Write the third evidence file now.",
	}

	for i, msg := range messages {
		runResp, err := http.Post(
			server.URL+"/api/run",
			"application/json",
			strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":%q}`, id, msg)),
		)
		if err != nil {
			t.Fatalf("run %d: POST /api/run: %v", i+1, err)
		}

		var accepted struct {
			RunID string `json:"runId"`
		}
		if err := json.NewDecoder(runResp.Body).Decode(&accepted); err != nil {
			t.Fatalf("run %d: decode run acceptance: %v", i+1, err)
		}
		runResp.Body.Close()

		if accepted.RunID == "" {
			t.Fatalf("run %d: POST /api/run returned no runId", i+1)
		}

		// The deterministic barrier: THIS run's terminal outcome is in
		// the registry — by the durable-completion ordering every
		// required durable artifact of the run is already on disk.
		if !waitForRunSettledFor(t, srv, id, accepted.RunID) {
			t.Fatalf("run %d: never recorded a terminal outcome", i+1)
		}

		rec, ok := srv.outcomes.latest(id)
		if !ok || rec.Outcome != "done" {
			t.Fatalf("run %d: outcome = %q, want done", i+1, rec.Outcome)
		}

		// Artifact 1 — the rolling summary is settled and versioned.
		sumResp, err := http.Get(server.URL + "/api/sessions/" + id + "/summary")
		if err != nil {
			t.Fatalf("run %d: GET summary: %v", i+1, err)
		}

		var summary struct {
			Version int `json:"version"`
		}
		if err := json.NewDecoder(sumResp.Body).Decode(&summary); err != nil {
			t.Fatalf("run %d: decode summary: %v", i+1, err)
		}
		sumResp.Body.Close()

		if summary.Version < i+1 {
			t.Fatalf("run %d: summary version = %d, want >= %d", i+1, summary.Version, i+1)
		}

		// Artifact 2 — the agent.md handoff exists on disk IMMEDIATELY
		// after the barrier (no post-barrier grace, no sleeps).
		data, err := os.ReadFile(filepath.Join(wsRoot, agent.HandoffFileName))
		if err != nil {
			t.Fatalf("run %d: agent.md handoff missing right after settlement: %v", i+1, err)
		}

		if !strings.Contains(string(data), agent.HandoffBeginMarker) {
			t.Fatalf("run %d: agent.md carries no handoff section", i+1)
		}
	}
}

// TestMandatoryHandoffFailureCannotMasqueradeAsDone pins the v1.3.3
// production honesty contract: a completed Agent run whose MANDATORY
// agent.md handoff fails to write must settle "error" — never "done".
//
// The failure is injected deterministically: the workspace root points at
// a regular FILE, so the handoff's os.CreateTemp fails with ENOTDIR on
// every platform (no chmod 000 lottery, no timing window). The run
// itself completes normally — the engine answers, the reply persists —
// which is exactly the masquerade shape the v1.3.2 code allowed.
func TestMandatoryHandoffFailureCannotMasqueradeAsDone(t *testing.T) {
	engine := remoteFakeEngine(t, "The work is recorded.")
	t.Cleanup(engine.Close)

	// The blocker: a regular file where the workspace root should be a
	// directory. agent.md writes fail with ENOTDIR — deterministically.
	blocker := filepath.Join(t.TempDir(), "ws-blocker")
	if err := os.WriteFile(blocker, []byte("this is a file, not a workspace"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	srv, server := newRemoteServerWithWorkspaceRoot(t, engine.URL, blocker)

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"Do the work and record it."}`, id)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	// The terminal outcome must be "error" — a mandatory-handoff
	// failure is not a successful completion. waitForRunOutcome returns
	// false immediately on a contradicting terminal record, so a
	// regression to "done" fails fast instead of spinning 30 s.
	if !waitForRunOutcome(t, srv, id, "error") {
		rec, _ := srv.outcomes.latest(id)
		t.Fatalf("mandatory handoff failure settled %q (caption %q) — want error", rec.Outcome, rec.Caption)
	}

	rec, ok := srv.outcomes.latest(id)
	if !ok {
		t.Fatal("no outcome recorded")
	}

	if !strings.Contains(rec.Caption, "agent.md handoff failed") {
		t.Fatalf("outcome caption = %q, want the concrete handoff failure cause", rec.Caption)
	}

	// The honest error/activity behavior is PRESERVED: the reply itself
	// was generated and persisted (the run's content is not lost — only
	// its terminal status is honest).
	if !rec.Persisted {
		t.Fatal("reply was not persisted — the honesty demotion must not lose the run's content")
	}

	// The durable step 1 (rolling summary) still ran BEFORE the failed
	// step 2 — the summary sidecar must exist.
	sumResp, err := http.Get(server.URL + "/api/sessions/" + id + "/summary")
	if err != nil {
		t.Fatalf("GET summary: %v", err)
	}
	defer sumResp.Body.Close()

	var summary struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(sumResp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Version < 1 {
		t.Fatalf("summary version = %d, want >= 1 (durable step 1 precedes the failed handoff)", summary.Version)
	}

	// And the handoff genuinely does NOT exist — the UI was never lied to.
	if _, err := os.Stat(filepath.Join(blocker, agent.HandoffFileName)); err == nil {
		t.Fatal("agent.md exists under a file-as-directory workspace root — impossible")
	}
}

// TestHandoffFailureSurfacesErrorActivity pins the honest-activity half of
// the contract: the run's LIVE activity stream carries the concrete
// handoff write failure as an error activity a connected client actually
// receives (the visible degradation), while the session keeps the
// assistant reply. The error activity is a live-stream settlement event
// (same convention as the reply-persistence error) — the durable record
// of the failure is the terminal outcome caption, pinned by the
// masquerade test above.
func TestHandoffFailureSurfacesErrorActivity(t *testing.T) {
	engine := remoteFakeEngine(t, "Recorded work summary.")
	t.Cleanup(engine.Close)

	blocker := filepath.Join(t.TempDir(), "ws-blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	srv, server := newRemoteServerWithWorkspaceRoot(t, engine.URL, blocker)

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	// Attach the activity socket BEFORE the run so the whole live
	// stream — including the post-terminal handoff error — is observed.
	conn := dialActivityWS(t, server, id)
	defer conn.Close()

	// The deterministic attach acknowledgement.
	_ = readFrameWithDeadline(t, conn)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"Do the work."}`, id)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForRunOutcome(t, srv, id, "error") {
		t.Fatal("run did not settle error")
	}

	// The live stream must carry the concrete handoff failure.
	sawHandoffError := false
	var captions []string

	for i := 0; i < 64 && !sawHandoffError; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("activity stream closed before the handoff error arrived (saw: %v)", captions)
		}

		if typ, _ := frame["type"].(string); typ == "error" {
			if caption, _ := frame["caption"].(string); strings.Contains(caption, "agent.md handoff failed") {
				sawHandoffError = true
			}
		}

		if caption, ok := frame["caption"].(string); ok {
			captions = append(captions, caption)
		}
	}

	if !sawHandoffError {
		t.Fatalf("no live error activity for the handoff failure (saw: %v)", captions)
	}

	// The session keeps the assistant reply — the honesty demotion must
	// not swallow the run's content.
	sess, err := srv.store.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}

	sawAssistantReply := false
	for _, m := range sess.Messages {
		if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
			sawAssistantReply = true
		}
	}

	if !sawAssistantReply {
		t.Fatal("the assistant reply was lost — honesty demotion must not swallow run content")
	}
}
