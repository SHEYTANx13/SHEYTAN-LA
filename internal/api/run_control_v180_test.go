package api

// run_control_v180_test.go — v1.8.0 PAUSE/RESUME SYNCHRONIZATION REGRESSION.
//
// Defect repaired in v1.8.0 (Actions run 36553559366, Windows x64 Go
// verification): TestPauseResumePauseAgainThenResumeCompletes reused
// waitForRunResponse AFTER a resume. That helper's condition is
// `LatestResponse != ""` — true forever once the FIRST generation has
// streamed, because the cumulative snapshot survives pause → resume. The
// helper therefore returned BEFORE the resumed generation emitted anything,
// the second pause raced the resumed run, and the run settled "done" while
// the test waited for "paused".
//
// The contract pinned here is resumedGenerationEvidence: proof of a resumed
// generation requires a strictly newer authoritative sequence AND a changed
// cumulative response/reasoning snapshot. Stale cumulative state, a bare
// sequence bump (status/task/other events), and settled runs with a
// persisted-reply swap are all rejected.
//
// Evidence classes in this file:
//
//      deterministic unit      — TestResumedGenerationEvidenceContract
//      integration (slow SSE)  — TestPauseResumeRequiresNewGenerationEvidence
//      race                    — run under -race by the Go race gate

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
)

// --- deterministic unit: the evidence contract -------------------------------

func TestResumedGenerationEvidenceContract(t *testing.T) {
	pre := runSnapshot{
		Sequence:       7,
		LatestResponse: "chunk-00 chunk-01 ",
	}

	t.Run("stale cumulative snapshot is never evidence", func(t *testing.T) {
		// The EXACT defect: after resume, the cumulative snapshot
		// still carries the pre-resume text at the same sequence.
		// waitForRunResponse accepted this as "the run streamed".
		stale := runSnapshot{
			Sequence:       pre.Sequence,
			LatestResponse: pre.LatestResponse,
		}

		if resumedGenerationEvidence(pre, stale) {
			t.Fatal("an unchanged cumulative snapshot at the same sequence must never count as resumed-generation evidence")
		}
	})

	t.Run("empty response becomes non-empty is evidence (initial generation)", func(t *testing.T) {
		freshPre := runSnapshot{Sequence: 3}
		firstDelta := runSnapshot{Sequence: 4, LatestResponse: "chunk-00 "}

		if !resumedGenerationEvidence(freshPre, firstDelta) {
			t.Fatal("a new response delta beyond an empty snapshot must count as evidence")
		}
	})

	t.Run("sequence bump without a content change is not evidence", func(t *testing.T) {
		// status/task/other events bump the sequence but are not
		// generation output.
		statusOnly := runSnapshot{
			Sequence:       pre.Sequence + 1,
			LatestResponse: pre.LatestResponse,
			LatestStatus:   "First response… · standard tier",
		}

		if resumedGenerationEvidence(pre, statusOnly) {
			t.Fatal("a sequence bump without a response/reasoning change must never count as evidence")
		}
	})

	t.Run("older or equal sequence is never evidence", func(t *testing.T) {
		older := runSnapshot{
			Sequence:       pre.Sequence - 1,
			LatestResponse: pre.LatestResponse + "chunk-02 ",
		}

		if resumedGenerationEvidence(pre, older) {
			t.Fatal("a snapshot at or below the captured sequence must never count as evidence")
		}
	})

	t.Run("changed response with newer sequence is evidence", func(t *testing.T) {
		newDelta := runSnapshot{
			Sequence:       pre.Sequence + 2,
			LatestResponse: pre.LatestResponse + "chunk-02 ",
		}

		if !resumedGenerationEvidence(pre, newDelta) {
			t.Fatal("a changed cumulative response at a newer sequence must count as evidence")
		}
	})

	t.Run("changed reasoning with newer sequence is evidence", func(t *testing.T) {
		reasoningDelta := runSnapshot{
			Sequence:        pre.Sequence + 1,
			LatestResponse:  pre.LatestResponse,
			LatestReasoning: "thinking…",
		}

		if !resumedGenerationEvidence(pre, reasoningDelta) {
			t.Fatal("a changed cumulative reasoning snapshot at a newer sequence must count as evidence")
		}
	})

	t.Run("shorter continuation caption is still evidence", func(t *testing.T) {
		// The resumed stream's cumulative caption restarts from the
		// continuation ("chunk-00 ") and can be SHORTER than the
		// pre-pause snapshot ("chunk-00 chunk-01 chunk-02 "). Any
		// CHANGE is evidence — not only growth.
		shorter := runSnapshot{
			Sequence:       pre.Sequence + 3,
			LatestResponse: "chunk-00 ",
		}

		if !resumedGenerationEvidence(pre, shorter) {
			t.Fatal("a changed (even shorter) cumulative response at a newer sequence must count as evidence")
		}
	})
}

// --- integration: the honest abort outcome -----------------------------------

// TestAbortedRunStateAgreesWithOutcome pins the v1.8.0 abort contract the
// pause/resume family depends on: after /api/abort reaches a resumed
// generation, the live state's terminal outcome, its phase AND the bounded
// outcome registry must agree on "aborted". The pre-v1.8.0 orchestrator
// published `done` on ctx cancelation, so observe() flipped the live state
// to "done" while settle() recorded "aborted" — the divergence that made
// TestAbortReachesResumedGeneration flaky (~1 in 6 on Linux, and a wrong
// terminal state in the WS snapshot for every aborted run).
func TestAbortedRunStateAgreesWithOutcome(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)
	resumeRun(t, server, sessionID, runID, snap.Revision)

	// Deterministic abort beat: prove the resumed generation is live
	// (the v1.8.0 evidence contract), THEN stop it — never a race on
	// elapsed time.
	pre := registeredRun(srv, sessionID).live.snapshot()
	resumeSnap := waitForResumedGeneration(t, srv, sessionID, pre)

	status, body := postJSON(t, server, "/api/abort", map[string]any{
		"sessionId": sessionID,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST /api/abort on a proven-live resumed run: status = %d body = %v, want 200 ok", status, body)
	}

	deadline := time.Now().Add(15 * time.Second)

	for {
		rs := registeredRun(srv, sessionID)

		if rs == nil {
			break // settled and released
		}

		s := rs.live.snapshot()

		if s.TerminalOutcome != "" {
			// THE contract: the early-visible outcome the
			// orchestrator's terminal activity folded must be the
			// outcome the caller settles. "done" here means the
			// honest abort marker regressed.
			if s.TerminalOutcome != "aborted" && s.TerminalOutcome != "error" {
				t.Fatalf("resumed run live state settled %q (phase %q), want aborted/error — the honest abort marker regressed", s.TerminalOutcome, s.Phase)
			}

			if s.Phase != s.TerminalOutcome {
				t.Fatalf("live phase %q disagrees with terminal outcome %q", s.Phase, s.TerminalOutcome)
			}

			// The resumed evidence must have been REAL generation
			// activity (seq advanced beyond the pre-abort capture).
			if resumeSnap.Sequence <= pre.Sequence {
				t.Fatalf("abort landed without proven resumed generation: seq %d <= pre %d", resumeSnap.Sequence, pre.Sequence)
			}

			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("the RESUMED run ignored /api/abort — still phase %q (the cancel never reached the fresh generation context)", s.Phase)
		}

		time.Sleep(10 * time.Millisecond)
	}

	// The bounded outcome registry must agree with the live state.
	waitForRegistryRelease(t, srv, sessionID)
}

// --- unit: the authoritative state folds the honest abort marker -------------

func TestRunLiveObserveAbortedMarker(t *testing.T) {
	live := newRunLive("run-aborted-1", "session-aborted-1", time.Now())

	live.observe(agent.Activity{
		Type:      "aborted",
		Caption:   "Aborted by user",
		Timestamp: time.Now(),
	})

	snap := live.snapshot()

	if snap.TerminalOutcome != "aborted" {
		t.Fatalf("observe(aborted) outcome = %q, want aborted", snap.TerminalOutcome)
	}

	if snap.Phase != "aborted" {
		t.Fatalf("observe(aborted) phase = %q, want aborted", snap.Phase)
	}

	if snap.Running {
		t.Fatal("observe(aborted) must clear running")
	}

	// A later settle of the SAME outcome is a no-op, never a rewrite.
	live.settleTerminal("aborted", "Aborted by user", false, "", "")

	if snap2 := live.snapshot(); snap2.TerminalOutcome != "aborted" || snap2.Phase != "aborted" {
		t.Fatalf("settleTerminal(aborted) after the fold moved the state: %+v", snap2)
	}

	// And a DIFFERENT outcome can never overwrite the honest fold.
	live2 := newRunLive("run-aborted-2", "session-aborted-2", time.Now())
	live2.observe(agent.Activity{Type: "aborted", Caption: "Aborted by user", Timestamp: time.Now()})
	live2.settleTerminal("done", "Completed", false, "", "")

	if snap3 := live2.snapshot(); snap3.TerminalOutcome != "aborted" {
		t.Fatalf("a late settle(done) overrode the honest aborted fold: %q", snap3.TerminalOutcome)
	}
}

// TestPauseResumeRequiresNewGenerationEvidence replays the exact CI
// scenario end-to-end and pins the v1.8.0 rule at the surface where the
// defect lived: after a resume, the test must OBSERVE new authoritative
// activity of the resumed generation before issuing the second pause. The
// fixture engine streams the SAME chunk sequence on every request, so the
// resumed stream's first emitted caption can reproduce the stale pre-pause
// cumulative text — precisely the input that fooled waitForRunResponse.
func TestPauseResumeRequiresNewGenerationEvidence(t *testing.T) {
	engine := remoteFakeEngineSlow(t, pauseStreamChunks(pauseChunkCount), pauseChunkDelay)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)
	runID := postRunMessage(t, server, sessionID, pausePrompt)

	snap := pauseRunMidStream(t, srv, server, sessionID, runID)

	// Capture the authoritative state BEFORE resuming — the contract's
	// required pre-resume beat.
	pre := registeredRun(srv, sessionID).live.snapshot()

	if pre.LatestResponse == "" {
		t.Fatal("precondition: the pre-resume snapshot must carry the stale cumulative response this regression defends against")
	}

	resumeRun(t, server, sessionID, runID, snap.Revision)

	after := waitForResumedGeneration(t, srv, sessionID, pre)

	// The returned snapshot must itself satisfy the contract — the
	// assertions the defective helper never made.
	if after.Sequence <= pre.Sequence {
		t.Fatalf("resumed evidence sequence = %d, want > %d", after.Sequence, pre.Sequence)
	}

	if after.LatestResponse == pre.LatestResponse && after.LatestReasoning == pre.LatestReasoning {
		t.Fatal("resumed evidence returned an unchanged cumulative snapshot — stale-state reuse regression")
	}

	// The second pause now lands on a PROVEN live generation (the same
	// deterministic posture as the first pause: ~1s of stream remains).
	status, body := postJSON(t, server, "/api/run/pause", map[string]any{
		"sessionId": sessionID,
		"runId":     runID,
	})

	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("second pause after proven evidence: status = %d body = %v, want 200 ok", status, body)
	}

	snap2 := waitForRunPhase(t, srv, sessionID, "paused")

	if snap2.PausedDraft == "" {
		t.Fatal("the second paused snapshot must carry the accepted draft")
	}

	// The draft must be a continuation-prefix of the SAME stream — the
	// accepted draft only ever grows from the first checkpoint's family.
	if !strings.HasPrefix(strings.Join(pauseStreamChunks(pauseChunkCount), ""), firstChunk(snap2.PausedDraft)) {
		t.Fatalf("second draft %q does not belong to the authoritative chunk stream", snap2.PausedDraft)
	}

	// Cleanup: the twice-paused run must not leak — replace it with a
	// fresh ordinary run (the one-run-per-session authority) and let it
	// settle before the temp-dir teardown.
	newRunID := postRunMessage(t, server, sessionID, "A brand new ordinary run.")

	if !waitForRunSettledFor(t, srv, sessionID, newRunID) {
		t.Fatal("the replacing run never settled")
	}
}
