package api

// admission_v195_test.go — v1.9.5 (P1): the run-admission side of the
// engine-transaction coordination, pinned deterministically:
//
//   1. BeginEngineMutation and run REGISTRATION are linearized: while a
//      claim is held, POST /api/run is refused with the deterministic
//      503 and the persisted transcript is untouched (no dangling user
//      message);
//   2. the claim's `live` verdict sees runs registered BEFORE the claim
//      (the transaction-refusal path — the llm package pins that side);
//   3. EndEngineMutation re-opens admission;
//   4. the claim is not taken when a run is already active.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func newAdmissionTestServer(t *testing.T) *Server {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(srv.Close)

	return srv
}

func TestAdmissionRefusesRunWhileMutationClaimed(t *testing.T) {
	srv := newAdmissionTestServer(t)

	store := httptest.NewServer(srv.Handler())
	t.Cleanup(store.Close)

	// A session to run against.
	resp, err := http.Post(store.URL+"/api/sessions", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	resp.Body.Close()

	sessID, _ := created["id"].(string)
	if sessID == "" {
		t.Fatalf("created session has no id: %v", created)
	}

	post := func() *http.Response {
		body := map[string]any{"sessionId": sessID, "message": "acceptance race probe"}
		raw, _ := json.Marshal(body)

		req, _ := http.NewRequest(http.MethodPost, store.URL+"/api/run", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /api/run: %v", err)
		}

		return res
	}

	// Baseline: admission open. The run is accepted into the pipeline
	// (the offline test env answers with the engine-gate error path —
	// anything except the admission refusal).
	res := post()
	if res.StatusCode == http.StatusServiceUnavailable {
		res.Body.Close()
		t.Fatal("baseline admission unexpectedly refused")
	}
	res.Body.Close()

	// Bounded readiness wait: the baseline run settles asynchronously and
	// unregisters itself. Admission coordination requires the registry to
	// actually be empty before the claim — wait for the drain instead of
	// sleeping an arbitrary number.
	drainEnd := time.Now().Add(10 * time.Second)
	for srv.anyRunActive() {
		if time.Now().After(drainEnd) {
			t.Fatal("the baseline run never unregistered from the run registry")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The baseline (accepted) run legitimately persisted its user message;
	// capture that as the floor the refusal must not change.
	messageCount := func(t *testing.T) int {
		t.Helper()

		sessions, err := http.Get(store.URL + "/api/sessions/" + sessID)
		if err != nil {
			t.Fatalf("GET session: %v", err)
		}
		defer sessions.Body.Close()

		var sess struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(sessions.Body).Decode(&sess); err != nil {
			t.Fatalf("decode session: %v", err)
		}

		return len(sess.Messages)
	}

	beforeRefusal := messageCount(t)

	// Hold the claim exactly like the engine transaction does.
	if live := srv.BeginEngineMutation(); live {
		t.Fatal("BeginEngineMutation reported live for an empty run registry")
	}

	if !srv.engineMutationActive() {
		t.Fatal("claim not mirrored as active")
	}

	// While the claim is held the deterministic refusal applies.
	res = post()
	defer res.Body.Close()

	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/run while mutation claimed = %d, want 503", res.StatusCode)
	}

	// The refusal leaves NO dangling user message: the persisted
	// transcript is exactly what it was before the refused request.
	if got := messageCount(t); got != beforeRefusal {
		t.Fatalf("refused run changed the persisted transcript: %d -> %d messages (want unchanged — no dangling user message)", beforeRefusal, got)
	}

	// Release: admission re-opens.
	srv.EndEngineMutation()

	if srv.engineMutationActive() {
		t.Fatal("claim still mirrored after release")
	}

	res2 := post()
	if res2.StatusCode == http.StatusServiceUnavailable {
		res2.Body.Close()
		t.Fatal("admission still refused after EndEngineMutation")
	}
	res2.Body.Close()
}

func TestAdmissionClaimNotTakenWhenRunActive(t *testing.T) {
	srv := newAdmissionTestServer(t)

	// A run registered before the claim: the claim reports live and does
	// NOT block admission (the transaction itself refuses instead).
	srv.runsMu.Lock()
	srv.runs["admission-live-proof"] = &runState{}
	srv.runsMu.Unlock()

	if live := srv.BeginEngineMutation(); !live {
		t.Fatal("BeginEngineMutation must report live when a run is already registered")
	}

	if srv.engineMutationActive() {
		t.Fatal("no claim may be held when the verdict was live (the transaction refuses instead)")
	}

	// Ownership contract: the caller does not release a claim it never
	// took — EndEngineMutation is still safe and clears nothing that was
	// never set.
	srv.EndEngineMutation()
}
