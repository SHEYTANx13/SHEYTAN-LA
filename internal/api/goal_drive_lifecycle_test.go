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

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/aisystem"
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

// chunkedParkEngine is the P2 probe's engine fixture: the same wire shape
// as scripts/fake-remote-engine.py (the Qt acceptance's engine) — chunked
// delta content, flushed per chunk, "data: [DONE]" terminator. The chunk
// pacing keeps the runs genuinely streamed, the way the hosted acceptance
// drives them.
func chunkedParkEngine(t *testing.T, reply string, delay time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []string{"Hello", " — ", "fixture ", "answer."} {
			payload := map[string]any{
				"id": "chatcmpl-park",
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"role": "assistant", "content": chunk + reply},
					"finish_reason": nil,
				}},
			}
			enc, _ := json.Marshal(payload)
			if _, err := w.Write([]byte("data: " + string(enc) + "\n\n")); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(delay)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server
}

// dumpParkDiagnostics is the probe's discriminating record: when the park
// never lands, the durable documents name the failing layer (status, phase,
// plan, pending approval, the evidence tail) instead of a bare timeout.
func dumpParkDiagnostics(t *testing.T, srv *Server, ids ...string) {
	t.Helper()
	for _, id := range ids {
		g, err := srv.goalStore().Get(id)
		if err != nil {
			t.Logf("goal %s → Get failed: %v", id, err)
			continue
		}
		t.Logf("goal %s → status=%s phase=%s turns=%d replans=%d pending=%+v",
			id, g.Status, g.Phase, g.TurnsUsed, g.Replans, g.PendingApproval)
		for i, st := range g.Plan {
			t.Logf("goal %s → plan[%d] index=%d status=%s objective=%q", id, i, st.Index, st.Status, st.Objective)
		}
		tail := g.Evidence
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		for _, e := range tail {
			t.Logf("goal %s → evidence: phase=%s action=%q status=%s detail=%q", id, e.Phase, e.Action, e.Status, e.Detail)
		}
	}
}

// TestGoalAskAllTwoGoalsParkConcurrently is the run #26 park-timeout
// reproduction probe (register 3.1): the Qt acceptance
// goalsApproveRejectParkedGoal parks TWO goals bound to the SAME ask-all
// AI System CONCURRENTLY (both started at creation, back-to-back), and
// fails its 60s bound on three consecutive hosted runs — while the
// single-goal ask-all park (TestGoalApproveConsumesGrantExactlyOnce) is
// green in the hosted audit. This probe reproduces the two-goal shape
// through the real server stack and the chunked SSE fixture engine, and
// on any timeout dumps BOTH goals' durable documents so the failing
// layer is named, not guessed. Honest outcomes: REPRODUCED (the dump
// pins the layer → fix there) or NOT REPRODUCED locally (the probe
// stays as the regression pin and the diagnosis belongs to the hosted
// QWARN diagnostics).
func TestGoalAskAllTwoGoalsParkConcurrently(t *testing.T) {
	engine := chunkedParkEngine(t, "park probe", 15*time.Millisecond)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	// The ask-all system through the REAL surface — the exact fixture the
	// Qt acceptance seeds.
	code, sys := postGoalJSON(t, server.URL, "/api/systems",
		`{"name":"acceptance ask-all","instructions":"fixture policy: park before every acting step","approvalPolicy":"ask-all"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/systems = %d", code)
	}
	systemID, _ := sys["systemId"].(string)
	if systemID == "" {
		t.Fatalf("no systemId in response: %+v", sys)
	}

	// TWO goals, both started at creation, back-to-back — the Qt shape.
	createParked := func(text string) string {
		code, created := postGoalJSON(t, server.URL, "/api/goals",
			`{"goal":"`+text+`","systemId":"`+systemID+`","start":true}`)
		if code != http.StatusOK {
			t.Fatalf("POST /api/goals = %d: %+v", code, created)
		}
		id, _ := created["goalId"].(string)
		if id == "" {
			t.Fatalf("no goalId in response: %+v", created)
		}
		return id
	}
	approveID := createParked("acceptance approval fixture")
	rejectID := createParked("acceptance rejection fixture")

	// Bounded poll — BOTH goals must sit in waiting_for_approval (the same
	// condition, through the durable store the Qt test observes via the
	// client's re-reads). Deadline-based; no fixed sleeps beyond the poll
	// cadence.
	deadline := time.Now().Add(60 * time.Second)
	parked := false
	for time.Now().Before(deadline) {
		a, aerr := srv.goalStore().Get(approveID)
		r, rerr := srv.goalStore().Get(rejectID)
		if aerr == nil && rerr == nil &&
			a.Status == goal.StatusWaitingApproval && r.Status == goal.StatusWaitingApproval {
			parked = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !parked {
		dumpParkDiagnostics(t, srv, approveID, rejectID)
		t.Fatalf("the two-goal ask-all park never landed within 60s (approve=%s reject=%s) — REPRODUCED: the durable dumps above name the failing layer",
			func() string {
				g, e := srv.goalStore().Get(approveID)
				if e != nil {
					return "get-error"
				}
				return g.Status
			}(),
			func() string {
				g, e := srv.goalStore().Get(rejectID)
				if e != nil {
					return "get-error"
				}
				return g.Status
			}())
	}

	// The park carries the durable pending call with a stable approvalId —
	// the same contract the Qt test asserts next.
	for _, id := range []string{approveID, rejectID} {
		g, err := srv.goalStore().Get(id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if g.PendingApproval == nil || g.PendingApproval.ApprovalID == "" {
			t.Fatalf("parked goal %s carries no durable pending approval", id)
		}
		if g.PendingApproval.ToolName != "goal.step" {
			t.Fatalf("parked goal %s pending tool = %q, want goal.step", id, g.PendingApproval.ToolName)
		}
	}
}

// TestGoalSystemRevisionFrozenContract pins the v1.10.13 SYSTEM-REVISION
// probe: a goal created against revision 1 keeps that frozen revision
// across phases, and once the system is revised the run boundary fails
// TYPED (goal.ErrInvalid) — it never silently runs against different
// instructions, and the durable goal document is never rewritten to the
// store's current revision.
func TestGoalSystemRevisionFrozenContract(t *testing.T) {
	srv, _ := newRemoteServerWithHandle(t, "http://127.0.0.1:1") // the engine is never reached

	sys, err := srv.systemsStore().Create(aisystem.System{
		Name:         "revision fixture",
		Instructions: "frozen instructions",
	})
	if err != nil {
		t.Fatalf("systems Create: %v", err)
	}

	g, err := srv.goalStore().Create("revision contract fixture", "sess", sys.SystemID, 1, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if g.SystemID != sys.SystemID || g.SystemRevision != 1 {
		t.Fatalf("goal froze systemId=%q revision=%d, want %q revision 1", g.SystemID, g.SystemRevision, sys.SystemID)
	}

	// While the store still holds revision 1, the snapshot resolves.
	if _, err := srv.resolveGoalSystemSnapshot(g.SystemID, g.SystemRevision); err != nil {
		t.Fatalf("matching revision must resolve: %v", err)
	}

	// Revise the system → revision 2. The durable goal KEEPS revision 1.
	if _, err := srv.systemsStore().Update(sys.SystemID, func(u *aisystem.System) error {
		u.Instructions = "REVISED instructions"
		return nil
	}); err != nil {
		t.Fatalf("systems Update: %v", err)
	}
	got, err := srv.goalStore().Get(g.GoalID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SystemRevision != 1 {
		t.Fatalf("the goal must keep its frozen revision across phases: got %d, want 1", got.SystemRevision)
	}

	// The run boundary refuses the revised system TYPED — the frozen
	// revision is never silently substituted with the current one.
	_, err = srv.resolveGoalSystemSnapshot(g.SystemID, got.SystemRevision)
	if err == nil || !errors.Is(err, goal.ErrInvalid) {
		t.Fatalf("revised system = %v; want typed goal.ErrInvalid (frozen-revision contract)", err)
	}
}

// ---------------------------------------------------------------------------
// Task ID 28 (v1.10.13 cycle) — §12 probes and the deterministic I6
// regression. The fixture engine mirrors scripts/fake-remote-engine.py
// EXACTLY (the script stays read-only; the contract lives here): 9 SSE
// chunks, ~150ms pacing per chunk, OpenAI chat.completions delta framing,
// "data: [DONE]" terminator, the SAME answer for every request. The fast
// chunkedParkEngine fixture above is retained unchanged.
// ---------------------------------------------------------------------------

// exactFixtureEngine is the PACKAGED-PROFILE engine (register V9): the
// exact wire shape and pacing of scripts/fake-remote-engine.py, which the
// Qt acceptance's core really drives (initTestCase pins it on port 18652).
// The fast fixture (15ms, 4 chunks) never exercised this timing class.
func exactFixtureEngine(t *testing.T) *httptest.Server {
	t.Helper()
	chunks := []string{"Hello", " — ", "I ", "am ", "here ", "and ", "working", " ", "fine."}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			payload := map[string]any{
				"id": "chatcmpl-fixture",
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"role": "assistant", "content": c},
					"finish_reason": nil,
				}},
			}
			enc, _ := json.Marshal(payload)
			if _, err := w.Write(append(append([]byte("data: "), enc...), []byte("\n\n")...)); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(150 * time.Millisecond)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server
}

// dumpProbeGoalRecord dumps ONE goal's FULL durable record (status, phase,
// plan, every evidence record) — the §12 requirement that a reproduction
// names the exact mechanism through the durable documents, never a bare
// timeout.
func dumpProbeGoalRecord(t *testing.T, srv *Server, id string) {
	t.Helper()
	g, err := srv.goalStore().Get(id)
	if err != nil {
		t.Logf("goal %s → Get failed: %v", id, err)
		return
	}
	t.Logf("goal %s → status=%s phase=%s turns=%d replans=%d lastAction=%q pending=%+v",
		id, g.Status, g.Phase, g.TurnsUsed, g.Replans, g.LastAction, g.PendingApproval)
	for i, st := range g.Plan {
		t.Logf("goal %s → plan[%d] index=%d status=%s objective=%q result=%q", id, i, st.Index, st.Status, st.Objective, st.Result)
	}
	for _, e := range g.Evidence {
		t.Logf("goal %s → evidence: %s phase=%s action=%q status=%s detail=%q",
			id, e.Timestamp.Format("15:04:05.000"), e.Phase, e.Action, e.Status, e.Detail)
	}
}

// TestGoalRoundTripCancelReplayPackagedProfile is PROBE A (§12): the
// goalsStartPauseResumeCancelRoundTrip replay — start → phase leaves
// understanding → pause → paused → resume → active → cancel → poll
// "cancelled" under the suite's 15s bound class — through the REAL server
// stack with the EXACT packaged engine profile (9 chunks × 150ms). The
// goal is created WITHOUT systemId: a fresh acceptance data dir binds no
// active system, so the acceptance goal runs UNBOUND (the probe asserts
// that precondition explicitly — V5's gate-live assumption is checked,
// not assumed). On failure the full durable record is dumped and the
// exact status transition chain names the mechanism.
func TestGoalRoundTripCancelReplayPackagedProfile(t *testing.T) {
	engine := exactFixtureEngine(t)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	code, created := postGoalJSON(t, server.URL, "/api/goals", `{"goal":"acceptance round trip fixture"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/goals = %d: %+v", code, created)
	}
	goalID, _ := created["goalId"].(string)
	if goalID == "" {
		t.Fatalf("no goalId in response: %+v", created)
	}
	if g, err := srv.goalStore().Get(goalID); err != nil {
		t.Fatalf("fixture precondition failed: Get: %v", err)
	} else if g.SystemID != aisystem.ReservedID {
		// Register correction verified here (Task ID 28): a no-systemId
		// goal binds the ALWAYS-PRESENT ACTIVE default system
		// (aisystem.ReservedID) — V5's gate-live assumption HOLDS, the
		// acceptance round-trip goal is BOUND. The default system's
		// approval policy is not ask-all (the slot's pause/resume legs
		// passed on run #28), so no park fires — the gate is wired but
		// the fixture emits no ask-class tool calls.
		t.Fatalf("fixture precondition failed: the no-systemId goal binds %q, want the reserved active default %q", g.SystemID, aisystem.ReservedID)
	}

	// Start: the bounded drive begins; the phase leaves understanding
	// (45s bound class — the slot's own bound).
	if code, _ := postGoalJSON(t, server.URL, "/api/goals/"+goalID+"/start", `{}`); code != http.StatusOK {
		t.Fatalf("POST start = %d", code)
	}
	leftUnderstanding := false
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if g, err := srv.goalStore().Get(goalID); err == nil && g.Phase != goal.PhaseUnderstanding {
			leftUnderstanding = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !leftUnderstanding {
		dumpProbeGoalRecord(t, srv, goalID)
		t.Fatalf("the phase never left understanding within 45s")
	}

	// Pause → paused (15s bound class).
	if code, _ := postGoalJSON(t, server.URL, "/api/goals/"+goalID+"/pause", `{}`); code != http.StatusOK {
		t.Fatalf("POST pause = %d", code)
	}
	pauseOK := false
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g, err := srv.goalStore().Get(goalID); err == nil && g.Status == goal.StatusPaused {
			pauseOK = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !pauseOK {
		dumpProbeGoalRecord(t, srv, goalID)
		t.Fatalf("the goal never reached paused within 15s")
	}

	// Resume → active (15s bound class).
	if code, _ := postGoalJSON(t, server.URL, "/api/goals/"+goalID+"/resume", `{}`); code != http.StatusOK {
		t.Fatalf("POST resume = %d", code)
	}
	resumeOK := false
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g, err := srv.goalStore().Get(goalID); err == nil && g.Status == goal.StatusActive {
			resumeOK = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !resumeOK {
		dumpProbeGoalRecord(t, srv, goalID)
		t.Fatalf("the goal never reached active within 15s after resume")
	}

	// Cancel: the SAME immediate cancel the Qt client fires once the
	// active status is observed — mid-acting-run under the packaged
	// pacing. Poll "cancelled" under the slot's 15s bound.
	if code, _ := postGoalJSON(t, server.URL, "/api/goals/"+goalID+"/cancel", `{}`); code != http.StatusOK {
		t.Fatalf("POST cancel = %d", code)
	}
	cancelled := false
	var finalStatus string
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g, err := srv.goalStore().Get(goalID); err == nil {
			finalStatus = g.Status
			if g.Status == goal.StatusCancelled {
				cancelled = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !cancelled {
		dumpProbeGoalRecord(t, srv, goalID)
		t.Fatalf("REPRODUCED-class outcome: the goal never read 'cancelled' within 15s after cancel — final durable status %q (the full record above names the exact transition chain)", finalStatus)
	}

	// The terminal status must ALSO survive the drive's full unwind —
	// the poll may observe cancelled while a late settlement is still in
	// flight (the run-#28 class): re-read after the generation unwinds.
	if !goalDriveWaitStop(goalID, 30*time.Second) {
		dumpProbeGoalRecord(t, srv, goalID)
		t.Fatalf("the cancelled drive generation never unwound")
	}
	if g, err := srv.goalStore().Get(goalID); err != nil || g.Status != goal.StatusCancelled {
		dumpProbeGoalRecord(t, srv, goalID)
		t.Fatalf("REPRODUCED-class outcome: the cancelled status was OVERWRITTEN during the drive unwind (status=%q err=%v) — a late settlement resurrected a terminal goal", g.Status, err)
	}
}

// TestGoalAskAllTwoGoalsParkPackagedProfile is PROBE B (§12): the
// goalsApproveRejectParkedGoal replay at the EXACT fixture timing — an
// ask-all system via POST /api/systems, TWO goals with explicit systemId
// and start:true, both polled to waiting_for_approval under the suite's
// 60s park bound through the packaged engine profile. NOT-REPRODUCED here
// exonerates the Go park machinery at this timing and moves the residual
// delta to the packaged core/IPC/Qt-client layer (V8's silent downgrade
// stays the next discriminating action — never fixed blind).
func TestGoalAskAllTwoGoalsParkPackagedProfile(t *testing.T) {
	engine := exactFixtureEngine(t)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	code, sys := postGoalJSON(t, server.URL, "/api/systems",
		`{"name":"acceptance ask-all","instructions":"fixture policy: park before every acting step","approvalPolicy":"ask-all"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/systems = %d", code)
	}
	systemID, _ := sys["systemId"].(string)
	if systemID == "" {
		t.Fatalf("no systemId in response: %+v", sys)
	}

	createParked := func(text string) string {
		code, created := postGoalJSON(t, server.URL, "/api/goals",
			`{"goal":"`+text+`","systemId":"`+systemID+`","start":true}`)
		if code != http.StatusOK {
			t.Fatalf("POST /api/goals = %d: %+v", code, created)
		}
		id, _ := created["goalId"].(string)
		if id == "" {
			t.Fatalf("no goalId in response: %+v", created)
		}
		return id
	}
	approveID := createParked("acceptance approval fixture")
	rejectID := createParked("acceptance rejection fixture")

	parked := false
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		a, aerr := srv.goalStore().Get(approveID)
		r, rerr := srv.goalStore().Get(rejectID)
		if aerr == nil && rerr == nil &&
			a.Status == goal.StatusWaitingApproval && r.Status == goal.StatusWaitingApproval {
			parked = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !parked {
		dumpParkDiagnostics(t, srv, approveID, rejectID)
		t.Fatalf("the two-goal ask-all park never landed within 60s at the packaged profile — the durable dumps above name the failing layer")
	}
	for _, id := range []string{approveID, rejectID} {
		g, err := srv.goalStore().Get(id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if g.PendingApproval == nil || g.PendingApproval.ApprovalID == "" {
			t.Fatalf("parked goal %s carries no durable pending approval", id)
		}
		if g.PendingApproval.ToolName != "goal.step" {
			t.Fatalf("parked goal %s pending tool = %q, want goal.step", id, g.PendingApproval.ToolName)
		}
	}
}

// TestGoalParkedStateListMatchesSingleGoalEnvelope is the R1 byte-compare
// probe (Task ID 30): the acceptance slot goalsApproveRejectParkedGoal
// waits for the parked state through the client's LIST view
// (GET /api/goals items) while the run #33 failure dump read the
// SINGLE-GOAL view (GET /api/goals/{id}) — the two views were never
// byte-compared at the parked state, so a serializer divergence at that
// state was invisible (I6 at the list boundary). This probe drives TWO
// ask-all goals to waiting_for_approval through the packaged fallback
// route (the exact fixture engine, no new seam), then fetches BOTH views
// through the real HTTP stack and byte-compares every wire field the
// acceptance reads from a list item (status, phase, pendingApproval with
// its approvalId and toolName, and the evidence records the approve leg
// scans) against the single-goal document. A divergence FAILS here with
// the verbatim bytes — the regression test that fails on the diverging
// serializer; full equality exonerates the server projection and moves
// the residual delta to the client's list parsing (V3b). While parked the
// durable documents are static (no drive writes), so the two reads are
// compared at a stable byte state.
func TestGoalParkedStateListMatchesSingleGoalEnvelope(t *testing.T) {
	engine := exactFixtureEngine(t)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	code, sys := postGoalJSON(t, server.URL, "/api/systems",
		`{"name":"acceptance ask-all","instructions":"fixture policy: park before every acting step","approvalPolicy":"ask-all"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/systems = %d", code)
	}
	systemID, _ := sys["systemId"].(string)
	if systemID == "" {
		t.Fatalf("no systemId in response: %+v", sys)
	}

	createParked := func(text string) string {
		code, created := postGoalJSON(t, server.URL, "/api/goals",
			`{"goal":"`+text+`","systemId":"`+systemID+`","start":true}`)
		if code != http.StatusOK {
			t.Fatalf("POST /api/goals = %d: %+v", code, created)
		}
		id, _ := created["goalId"].(string)
		if id == "" {
			t.Fatalf("no goalId in response: %+v", created)
		}
		return id
	}
	approveID := createParked("acceptance approval fixture")
	rejectID := createParked("acceptance rejection fixture")

	parked := false
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		a, aerr := srv.goalStore().Get(approveID)
		r, rerr := srv.goalStore().Get(rejectID)
		if aerr == nil && rerr == nil &&
			a.Status == goal.StatusWaitingApproval && r.Status == goal.StatusWaitingApproval {
			parked = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !parked {
		dumpParkDiagnostics(t, srv, approveID, rejectID)
		t.Fatalf("the two-goal ask-all park never landed within 60s at the packaged profile — the byte-compare precondition is unreachable")
	}

	// getJSON fetches one view through the REAL server stack and returns
	// the raw body — the comparison is defined at the byte level.
	getJSON := func(path string) []byte {
		t.Helper()
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("GET %s body: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, resp.StatusCode, string(raw))
		}
		return raw
	}

	var listEnv struct {
		Goals []json.RawMessage `json:"goals"`
	}
	listRaw := getJSON("/api/goals")
	if err := json.Unmarshal(listRaw, &listEnv); err != nil {
		t.Fatalf("list body is not a goals envelope: %v", err)
	}
	byID := map[string]json.RawMessage{}
	for _, item := range listEnv.Goals {
		var id struct {
			GoalID string `json:"goalId"`
		}
		if err := json.Unmarshal(item, &id); err != nil {
			t.Fatalf("list item without goalId: %s", string(item))
		}
		byID[id.GoalID] = item
	}

	// formatOrAbsent renders a verbatim field (or its absence) for the
	// failure record — never a normalized re-marshaling.
	formatOrAbsent := func(raw json.RawMessage, ok bool) string {
		if !ok {
			return "<ABSENT>"
		}
		return string(raw)
	}

	for _, id := range []string{approveID, rejectID} {
		item, ok := byID[id]
		if !ok {
			t.Fatalf("I6 LIST-BOUNDARY DIVERGENCE: parked goal %s is MISSING from GET /api/goals (items=%d) while GET /api/goals/%s serves it — the list projection dropped a parked goal",
				id, len(listEnv.Goals), id)
		}

		singleRaw := getJSON("/api/goals/" + id)
		var singleEnv struct {
			Goal    json.RawMessage `json:"goal"`
			Driving json.RawMessage `json:"driving"`
		}
		if err := json.Unmarshal(singleRaw, &singleEnv); err != nil {
			t.Fatalf("single-goal body is not a goal envelope: %v", err)
		}

		// fieldBytes extracts the raw JSON bytes of one field.
		fieldBytes := func(doc json.RawMessage, field string) (json.RawMessage, bool) {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(doc, &m); err != nil {
				return nil, false
			}
			v, present := m[field]
			return v, present
		}

		// compareField fails with the VERBATIM bytes on any divergence.
		compareField := func(field string) {
			t.Helper()
			itemVal, itemOK := fieldBytes(item, field)
			singleVal, singleOK := fieldBytes(singleEnv.Goal, field)
			if !itemOK && !singleOK {
				t.Logf("goal %s → %s identical: <ABSENT in both views>", id, field)
				return
			}
			if itemOK != singleOK || string(itemVal) != string(singleVal) {
				t.Fatalf("I6 LIST-BOUNDARY DIVERGENCE on goal %s field %q at the parked state:\n  list item bytes:    %s\n  single-goal bytes:  %s",
					id, field, formatOrAbsent(itemVal, itemOK), formatOrAbsent(singleVal, singleOK))
			}
			t.Logf("goal %s → %s identical: %s", id, field, string(singleVal))
		}

		t.Logf("goal %s → list item bytes:    %s", id, string(item))
		t.Logf("goal %s → single-goal bytes:  %s", id, string(singleEnv.Goal))
		// Every wire field the acceptance slot reads from a list item
		// at the parked state (status/phase/pendingApproval/approvalId/
		// toolName ride inside the compared objects; evidence is the
		// approve leg's scan target).
		compareField("status")
		compareField("phase")
		compareField("pendingApproval")
		compareField("evidence")

		// pendingApproval's identity fields, byte-exact, both views.
		for _, doc := range []struct {
			name string
			raw  json.RawMessage
		}{{"list", item}, {"single", singleEnv.Goal}} {
			paRaw, paOK := fieldBytes(doc.raw, "pendingApproval")
			if !paOK || len(paRaw) == 0 {
				t.Fatalf("I6 LIST-BOUNDARY DIVERGENCE: parked goal %s carries no pendingApproval in the %s view (the slot's QCOMPARE needs approvalId and toolName there)", id, doc.name)
			}
			approvalID, aOK := fieldBytes(paRaw, "approvalId")
			toolName, tOK := fieldBytes(paRaw, "toolName")
			if !aOK || !tOK || len(approvalID) < 2 || string(toolName) != `"goal.step"` {
				t.Fatalf("I6 LIST-BOUNDARY DIVERGENCE: parked goal %s pendingApproval in the %s view carries approvalId=%s toolName=%s, want a stable approvalId and \"goal.step\"",
					id, doc.name, formatOrAbsent(approvalID, aOK), formatOrAbsent(toolName, tOK))
			}
		}

		// The envelope's driving flag stays a single-goal-only wire
		// field by design — recorded, not compared against the item.
		t.Logf("goal %s → envelope driving bytes: %s (list items carry no driving field by design — the client never reads it from the list)", id, string(singleEnv.Driving))
	}
}

// phaseGateEngine answers understanding/planning/acting immediately and
// BLOCKS the verifying call until released — the deterministic barrier
// that parks the drive mid-verify so the cancel race is constructed, not
// waited for. Planning answers with a real JSON plan (parsePlan path).
func phaseGateEngine(t *testing.T) (*httptest.Server, chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	calls := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// SSE Content-Type is load-bearing: without it the client takes
		// the non-SSE plain-JSON fallback path and chokes on "data:".
		w.Header().Set("Content-Type", "text/event-stream")
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		var text string
		switch {
		case n == 2:
			text = `["acceptance step one"]`
		case n >= 4:
			// The verifying call: hold until the test releases it.
			<-release
			text = "verification aborted by the race fixture"
		default:
			text = "classification prose for the phase gate"
		}
		payload := map[string]any{
			"id": "chatcmpl-gate",
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{"role": "assistant", "content": text},
				"finish_reason": nil,
			}},
		}
		enc, _ := json.Marshal(payload)
		_, _ = w.Write(append(append([]byte("data: "), enc...), []byte("\n\n")...))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server, release
}

// TestGoalCancelDuringVerifyKeepsCancelledStatus pins I6 deterministically:
// a cancel that lands while the drive is parked in the VERIFYING model
// call must survive the late settlement — the verify run's failure path
// writes verification evidence, never a new terminal status over a
// cancelled goal. This test FAILS on the unguarded SetVerification path
// (the run-#28 round-trip mechanism): the goal would flip cancelled →
// failed during the unwind.
func TestGoalCancelDuringVerifyKeepsCancelledStatus(t *testing.T) {
	engine, release := phaseGateEngine(t)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	code, created := postGoalJSON(t, server.URL, "/api/goals", `{"goal":"cancel-during-verify fixture"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/goals = %d", code)
	}
	goalID, _ := created["goalId"].(string)

	if code, _ := postGoalJSON(t, server.URL, "/api/goals/"+goalID+"/start", `{}`); code != http.StatusOK {
		t.Fatalf("POST start = %d", code)
	}
	pollFor(t, 30*time.Second, "the drive must reach the verifying phase", func() bool {
		g, err := srv.goalStore().Get(goalID)
		return err == nil && g.Phase == goal.PhaseVerifying
	})

	if code, _ := postGoalJSON(t, server.URL, "/api/goals/"+goalID+"/cancel", `{}`); code != http.StatusOK {
		t.Fatalf("POST cancel = %d", code)
	}
	if g, err := srv.goalStore().Get(goalID); err != nil || g.Status != goal.StatusCancelled {
		t.Fatalf("status right after cancel = %q (err=%v), want cancelled", g.Status, err)
	}

	// Free the parked verify call: the drive's late settlement runs now.
	close(release)
	if !goalDriveWaitStop(goalID, 30*time.Second) {
		dumpProbeGoalRecord(t, srv, goalID)
		t.Fatal("the cancelled drive generation never unwound")
	}
	g, err := srv.goalStore().Get(goalID)
	if err != nil {
		t.Fatalf("Get after unwind: %v", err)
	}
	if g.Status != goal.StatusCancelled {
		dumpProbeGoalRecord(t, srv, goalID)
		t.Fatalf("I6 REGRESSION: the cancelled status was overwritten during the verify unwind (status=%q) — a late settlement resurrected a terminal goal", g.Status)
	}
}
