package api

// goal_drive_lifecycle_test.go — v1.10.13 regression tests for the drive
// registry repair. Deterministic: every cross-goroutine wait is a channel
// or a bounded condition poll — no blind sleeps. Each test names the
// invariant it pins (quoted from the register; these survive the cycle):
//
//      I1 — Atomic single-flight: admission and reservation happen under
//           one lock; no check-then-insert gap exists anywhere a registry
//           is used.
//      I2 — Generation ownership: cleanup removes only the record it
//           owns; no old actor can delete a newer actor's registration.
//      I3 — No discarded outcomes: any goroutine whose result matters has
//           its result and error observed and settled.
//      I4 — Approvals are exact, one-use, and durable: bound to the
//           canonical action, consumed atomically at execution.
//      I6 — Status and drive always agree: an active goal always has a
//           live or finishing drive; the invariant is tested, not assumed.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/goal"
)

// blockingDriveEngine is an OpenAI-compatible SSE engine whose
// chat/completions handler BLOCKS until release is closed — the barrier
// that holds a drive generation mid-model-call deterministically. The
// handler ignores client cancellation (a cancelled drive generation stays
// parked in the call until release: exactly the observation the
// reservation-retention test needs).
func blockingDriveEngine(t *testing.T, reply string, entered chan struct{}) (*httptest.Server, func()) {
	t.Helper()
	// The release func closes EXACTLY once — explicitly or via cleanup
	// (a test that Fatals mid-way must never leave the handler parked, or
	// the harness's server.Close cleanup would wait for it forever).
	release := make(chan struct{})
	var once sync.Once
	releaseFn := func() { once.Do(func() { close(release) }) }
	t.Cleanup(releaseFn)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if entered != nil {
			entered <- struct{}{}
		}
		<-release
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := map[string]any{
			"id": "chatcmpl-block",
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{"role": "assistant", "content": reply},
				"finish_reason": nil,
			}},
		}
		enc, _ := json.Marshal(chunk)
		_, _ = w.Write([]byte("data: " + string(enc) + "\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server, releaseFn
}

func postGoalJSON(t *testing.T, serverURL, path, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(serverURL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m
}

func deleteGoal(t *testing.T, serverURL, id string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, serverURL+"/api/goals/"+id, nil)
	if err != nil {
		t.Fatalf("DELETE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func pollFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition never held within %s: %s", timeout, what)
}

// TestGoalDriveAtomicAdmission pins I1: N concurrent starts admit EXACTLY
// one drive generation; every loser gets the typed conflict
// (ErrDriveConflict — an honest 409), never a parallel loop.
func TestGoalDriveAtomicAdmission(t *testing.T) {
	entered := make(chan struct{}, 64)
	engine, releaseDrive := blockingDriveEngine(t, "fixture admission answer", entered)
	srv, _ := newRemoteServerWithHandle(t, engine.URL)

	g, err := srv.goalStore().Create("atomic admission fixture", "sess", "", 0, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const n = 16
	type result struct {
		err error
	}
	results := make(chan result, n)
	for i := 0; i < n; i++ {
		go func() {
			results <- result{err: srv.startGoalDrive(g.GoalID)}
		}()
	}

	admitted, conflicted, other := 0, 0, 0
	for i := 0; i < n; i++ {
		r := <-results
		switch {
		case r.err == nil:
			admitted++
		case errors.Is(r.err, goal.ErrDriveConflict):
			conflicted++
		default:
			other++
			t.Logf("unexpected error: %v", r.err)
		}
	}
	if admitted != 1 {
		t.Fatalf("admitted = %d, want exactly 1 (I1: atomic single-flight)", admitted)
	}
	if conflicted != n-1 {
		t.Fatalf("conflicted = %d, want %d", conflicted, n-1)
	}
	if other != 0 {
		t.Fatalf("unexpected errors = %d (see log)", other)
	}

	// Cleanup: release the barrier; the drive unwinds; the reservation is
	// released by the goroutine's own generation-safe cleanup (I2).
	<-entered // prove the winner is mid-model-call (the barrier held)
	releaseDrive()
	if !goalDriveWaitStop(g.GoalID, 30*time.Second) {
		t.Fatal("the admitted generation never unwound after release")
	}
	if goalDriveLookup(g.GoalID) != nil {
		t.Fatal("registry entry survived the goroutine's own cleanup (I2 violated)")
	}
}

// TestGoalResumeSerializesWithOldGeneration pins the RESUME-500 repair:
// resume while the old generation is alive → a TYPED 409 naming the
// state, the goal stays paused (never a generic 500, never
// active-without-drive); after the old generation unwinds, resume
// succeeds and re-drives.
func TestGoalResumeSerializesWithOldGeneration(t *testing.T) {
	entered := make(chan struct{}, 8)
	engine, releaseDrive := blockingDriveEngine(t, "fixture resume answer", entered)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	g, err := srv.goalStore().Create("resume serialization fixture", "sess", "", 0, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := srv.startGoalDrive(g.GoalID); err != nil {
		t.Fatalf("startGoalDrive: %v", err)
	}
	<-entered // the old generation is now parked inside the model call

	// Pause (the durable control surface — the drive is mid-call).
	if _, err := srv.goalStore().Pause(g.GoalID); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	// Resume while the old generation is ALIVE: a short bound forces the
	// serialization wait to expire — the answer must be a typed 409 and
	// the status must stay paused (no 500, no active-without-drive).
	oldWait := goalDriveStopWait
	goalDriveStopWait = 300 * time.Millisecond
	defer func() { goalDriveStopWait = oldWait }()

	code, _ := postGoalJSON(t, server.URL, "/api/goals/"+g.GoalID+"/resume", `{}`)
	if code != http.StatusConflict {
		t.Fatalf("resume while old generation alive = %d, want 409 (typed conflict, never a generic 500)", code)
	}
	if got, _ := srv.goalStore().Get(g.GoalID); got.Status != goal.StatusPaused {
		t.Fatalf("status after conflicted resume = %q, want paused (I6: never active without a drive)", got.Status)
	}

	// Release: the old generation parks on PauseCheck, unwinds, releases.
	releaseDrive()
	if !goalDriveWaitStop(g.GoalID, 30*time.Second) {
		t.Fatal("the old generation never unwound after release")
	}

	// Now resume succeeds and re-drives from the checkpoint.
	code, _ = postGoalJSON(t, server.URL, "/api/goals/"+g.GoalID+"/resume", `{}`)
	if code != http.StatusOK {
		t.Fatalf("resume after unwind = %d, want 200", code)
	}
	pollFor(t, 15*time.Second, "the re-driven goal to leave paused", func() bool {
		got, err := srv.goalStore().Get(g.GoalID)
		return err == nil && (got.Status == goal.StatusActive || got.IsTerminal())
	})

	// Cleanup: terminal cancel + bounded unwind.
	if _, err := srv.goalStore().Cancel(g.GoalID, "test cleanup"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	goalDriveCancel(g.GoalID)
	goalDriveWaitStop(g.GoalID, 30*time.Second)
}

// TestGoalCancelRetainsReservationUntilUnwind pins I2's cancel half, in
// the deterministic order the invariant is about: while a generation is
// mid-call its reservation blocks any replacement (a second start gets
// the typed conflict); the cancel keeps the reservation with the OWNER
// (the goroutine releases it itself, after the unwind — never the
// caller); and a cancelled goal's late drive error never resurrects the
// terminal status.
func TestGoalCancelRetainsReservationUntilUnwind(t *testing.T) {
	entered := make(chan struct{}, 8)
	engine, releaseDrive := blockingDriveEngine(t, "fixture cancel answer", entered)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	g, err := srv.goalStore().Create("cancel reservation fixture", "sess", "", 0, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := srv.startGoalDrive(g.GoalID); err != nil {
		t.Fatalf("startGoalDrive: %v", err)
	}
	<-entered // generation 1 is now mid-model-call, reservation live

	// A replacement CANNOT overlap the live generation (I1/I2): typed
	// conflict, no second admission.
	if err := srv.startGoalDrive(g.GoalID); !errors.Is(err, goal.ErrDriveConflict) {
		t.Fatalf("second start during a live generation = %v, want ErrDriveConflict", err)
	}

	// Cancel through the REAL surface. goalDriveCancel only cancels the
	// context — the reservation belongs to the goroutine, which releases
	// it after its own unwind.
	if code := deleteGoal(t, server.URL, g.GoalID); code != http.StatusOK {
		t.Fatalf("DELETE /api/goals/{id} = %d, want 200", code)
	}
	if got, _ := srv.goalStore().Get(g.GoalID); got.Status != goal.StatusCancelled {
		t.Fatalf("status after cancel = %q, want cancelled", got.Status)
	}

	// The cancelled generation unwinds (its engine call was cancelled),
	// settles its own outcome (control class — evidence only) and
	// releases the reservation ITSELF.
	if !goalDriveWaitStop(g.GoalID, 30*time.Second) {
		t.Fatal("the cancelled generation never unwound")
	}
	if goalDriveLookup(g.GoalID) != nil {
		t.Fatal("registry entry survived the cancelled generation's unwind")
	}
	releaseDrive() // free the parked handler before the server.Close cleanup
	got, _ := srv.goalStore().Get(g.GoalID)
	if got.Status != goal.StatusCancelled {
		t.Fatalf("status after unwind = %q, want cancelled (no resurrection from a late drive error)", got.Status)
	}
}

// TestGoalFailingDriveSettlesBlocked pins I3/I6 end-to-end: a drive whose
// model call FAILS is observed; the goal settles to blocked with durable
// evidence, the registry empties, and the goal is never left
// active-without-drive (the run #24 park-timeout class had ZERO
// diagnostics — this test is the regression gate for that silence).
func TestGoalFailingDriveSettlesBlocked(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"fixture engine failure"}}`, http.StatusInternalServerError)
	}))
	t.Cleanup(engine.Close)
	srv, _ := newRemoteServerWithHandle(t, engine.URL)

	g, err := srv.goalStore().Create("failing drive settlement fixture", "sess", "", 0, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := srv.startGoalDrive(g.GoalID); err != nil {
		t.Fatalf("startGoalDrive: %v", err)
	}

	// The drive fails → the goroutine settles the goal deliberately.
	pollFor(t, 30*time.Second, "the failed drive to settle the goal blocked", func() bool {
		got, err := srv.goalStore().Get(g.GoalID)
		return err == nil && got.Status == goal.StatusBlocked
	})
	pollFor(t, 10*time.Second, "the failed generation to release the registry", func() bool {
		return goalDriveLookup(g.GoalID) == nil
	})

	got, _ := srv.goalStore().Get(g.GoalID)
	if got.Status == goal.StatusActive {
		t.Fatal("goal left active after a failed drive (I6 violated)")
	}
	found := false
	for _, e := range got.Evidence {
		if e.Action == "drive failed" && e.Status == "error" && strings.Contains(e.Detail, "category=run") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no 'drive failed' evidence with category/phase detail — the settlement is not diagnosable (evidence tail: %+v)", got.Evidence[max(0, len(got.Evidence)-3):])
	}
}

// TestGoalApproveConsumesGrantExactlyOnce pins I4 end-to-end over the
// REAL surface: an ask-all goal parks; the human decision arms the
// ONE-USE grant; the restarted drive consumes it at the step boundary and
// EXECUTES the step — the same approved step is never re-parked (the
// APPROVAL-ONE-USE design gap), the goal settles terminal with exactly
// one approval decision in evidence.
func TestGoalApproveConsumesGrantExactlyOnce(t *testing.T) {
	engine := remoteFakeEngine(t, "fixture acting answer — step executed")
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	// The ask-all system (the same fixture the Qt acceptance seeds).
	code, sys := postGoalJSON(t, server.URL, "/api/systems",
		`{"name":"lifecycle ask-all","instructions":"fixture","approvalPolicy":"ask-all"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/systems = %d", code)
	}
	systemID, _ := sys["systemId"].(string)
	if systemID == "" {
		t.Fatalf("no systemId in response: %+v", sys)
	}

	// A goal bound to it, started at creation.
	code, created := postGoalJSON(t, server.URL, "/api/goals",
		`{"goal":"approve consumes grant fixture","systemId":"`+systemID+`","start":true}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/goals = %d: %+v", code, created)
	}
	goalID, _ := created["goalId"].(string)
	if goalID == "" {
		t.Fatalf("no goalId in response: %+v", created)
	}

	// The drive parks BEFORE step 0 (ask-all) — waiting_for_approval.
	pollFor(t, 60*time.Second, "the goal to park waiting_for_approval", func() bool {
		got, err := srv.goalStore().Get(goalID)
		return err == nil && got.Status == goal.StatusWaitingApproval
	})
	parked, _ := srv.goalStore().Get(goalID)
	if parked.PendingApproval == nil || parked.PendingApproval.ApprovalID == "" {
		t.Fatalf("parked goal carries no durable pending approval: %+v", parked.PendingApproval)
	}
	approvalID := parked.PendingApproval.ApprovalID

	// The human decision: approve. The drive restarts, consumes the grant
	// for the EXACT step and executes it — never re-parking that step.
	code, _ = postGoalJSON(t, server.URL, "/api/goals/"+goalID+"/approve",
		`{"approvalId":"`+approvalID+`"}`)
	if code != http.StatusOK {
		t.Fatalf("POST approve = %d", code)
	}

	pollFor(t, 60*time.Second, "the approved drive to execute step 0 and settle terminally (no re-park)", func() bool {
		got, err := srv.goalStore().Get(goalID)
		return err == nil && got.IsTerminal()
	})
	got, _ := srv.goalStore().Get(goalID)

	decisions := 0
	stepCompleted := false
	for _, e := range got.Evidence {
		if e.Action == "approval decision" && e.Status == "approved" {
			decisions++
		}
	}
	for _, st := range got.Plan {
		if st.Index == 0 && st.Status == goal.StepCompleted {
			stepCompleted = true
		}
	}
	if decisions != 1 {
		t.Fatalf("approval-decision evidence records = %d, want exactly 1 (a re-park would demand a second decision)", decisions)
	}
	if !stepCompleted {
		t.Fatalf("step 0 never executed after approval (plan: %+v status: %s) — the grant was not consumed at the boundary", got.Plan, got.Status)
	}
	if got.ApprovedAction != nil {
		t.Fatalf("ONE-USE grant survived execution: %+v", got.ApprovedAction)
	}
	if got.PendingApproval != nil {
		t.Fatalf("pending approval survived execution: %+v", got.PendingApproval)
	}
}
