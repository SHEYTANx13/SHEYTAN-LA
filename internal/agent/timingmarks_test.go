package agent

// timingmarks_test.go — v1.2.6 continuation regression tests for the
// MEASURED timing semantics:
//
//   1. request_sent / response_headers / first_byte are emitted by the
//      HTTP streaming client at the moments they happen (not derived from
//      content deltas);
//   2. first_token stays first-content semantics and is DISTINCT from
//      first_byte;
//   3. the derived durations are sane and ordered;
//   4. raw stage timestamps ride the Timing payload (provenance);
//   5. tool_start/tool_end are two different moments (no reuse);
//   6. unmarked stages report 0 / stay absent — never invented.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// delayedHeadersEngine delays the response headers, then the first body
// byte, so the transport ladder is distinguishable end-to-end.
func delayedHeadersEngine(t *testing.T, headerDelay, bodyDelay time.Duration, reply string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(headerDelay)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		time.Sleep(bodyDelay)

		chunk := map[string]any{
			"id": "chatcmpl-timing",
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

	return server
}

// newTimingOrchestrator builds an orchestrator pointed at a remote engine.
func newTimingOrchestrator(t *testing.T, engineURL string) *Orchestrator {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "remote"
	cfg.RemoteBaseURL = engineURL + "/v1"
	cfg.RemoteAPIKey = "test-key"
	cfg.RemoteModel = "fake-remote-model"

	src := config.NewSource(cfg)
	client := llm.NewClient(src)

	return New(src, client)
}

// TestTransportTimingMarksMeasuredAtTransport pins the transport ladder:
// request_sent fires at hand-off, response_headers when the server
// answers, first_byte when the first body byte is READ — each observable
// as a distinct, ordered moment on a server with real delays.
func TestTransportTimingMarksMeasuredAtTransport(t *testing.T) {
	logging.Default()

	engine := delayedHeadersEngine(t, 150*time.Millisecond, 250*time.Millisecond, "timing answer")
	orch := newTimingOrchestrator(t, engine.URL)

	// Stream directly through the client and capture the marks at the
	// moments they arrive — the observable contract the orchestrator
	// folds into its RunClock.
	req := orch.client.BuildChatRequest(
		"fake-remote-model",
		[]llm.Message{{Role: "user", Content: "again"}},
		nil,
	)

	var order []string
	at := map[string]time.Time{}

	_, streamErr := orch.client.StreamChatDetailed(t.Context(), req, func(ev llm.StreamEvent) error {
		if ev.TimingMark != "" {
			if _, seen := at[ev.TimingMark]; !seen {
				at[ev.TimingMark] = time.Now()
				order = append(order, ev.TimingMark)
			}
		}
		return nil
	})
	if streamErr != nil {
		t.Fatalf("StreamChatDetailed: %v", streamErr)
	}

	wantOrder := []string{
		llm.TimingMarkRequestSent,
		llm.TimingMarkResponseHeader,
		llm.TimingMarkFirstByte,
	}

	if len(order) < len(wantOrder) {
		t.Fatalf("marks = %v, want at least %v", order, wantOrder)
	}

	got := order[:len(wantOrder)]

	for i, want := range wantOrder {
		if got[i] != want {
			t.Fatalf("mark order = %v, want %v (the transport ladder must be ordered)", got, wantOrder)
		}
	}

	// The delays are real: headers measurably lag the send, the body
	// measurably lags the headers.
	if d := at[llm.TimingMarkResponseHeader].Sub(at[llm.TimingMarkRequestSent]); d < 50*time.Millisecond {
		t.Fatalf("response_headers lagged request_sent by only %v — the mark is not measuring the transport", d)
	}

	if d := at[llm.TimingMarkFirstByte].Sub(at[llm.TimingMarkResponseHeader]); d < 100*time.Millisecond {
		t.Fatalf("first_byte lagged response_headers by only %v — the mark is not measuring the body read", d)
	}
}

// TestRunTimingCarriesTransportLadder pins the orchestrator→result
// integration: the turn's Timing carries request_sent, response_headers,
// first_byte AND first_token as DISTINCT measured stages — first_byte is
// no longer the first content delta, and TTFT is measured from the REAL
// request hand-off.
func TestRunTimingCarriesTransportLadder(t *testing.T) {
	logging.Default()

	engine := delayedHeadersEngine(t, 60*time.Millisecond, 120*time.Millisecond, "folded answer")
	orch := newTimingOrchestrator(t, engine.URL)

	res, err := orch.RunDetailed(
		t.Context(),
		[]llm.Message{{Role: "user", Content: "fold"}},
		func(a Activity) {},
	)
	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	if res.Text == "" {
		t.Fatal("no reply — the timing test needs a real stream")
	}

	snap := res.Timing

	for _, stage := range []string{
		StageRequestSent, StageResponseHeaders,
		StageFirstByte, StageFirstToken,
	} {
		if ts, ok := snap.Stages[stage]; !ok || ts == 0 {
			t.Fatalf("stage %s missing from the measured ladder: %+v", stage, snap.Stages)
		}
	}

	// first_byte precedes (or equals) first_token: the body byte arrives
	// before the content delta is decoded from it.
	fb := snap.Stages[StageFirstByte]
	ft := snap.Stages[StageFirstToken]

	if ft < fb {
		t.Fatalf("first_token (%d) precedes first_byte (%d) — first_byte was mislabelled as a content delta", ft, fb)
	}

	// Provenance + measured duration sanity.
	if snap.HeadersMs < 30 {
		t.Fatalf("headersMs = %d — the 60ms header delay was not measured", snap.HeadersMs)
	}

	if snap.TTFTMs < snap.HeadersMs {
		t.Fatalf("ttftMs (%d) < headersMs (%d) — TTFT is no longer measured from the real request hand-off", snap.TTFTMs, snap.HeadersMs)
	}
}

// measuredTool is a Tool whose execution duration is measurable.
type measuredTool struct {
	name  string
	delay time.Duration
}

func (m *measuredTool) Name() string        { return m.name }
func (m *measuredTool) Description() string { return "measured test tool" }
func (m *measuredTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (m *measuredTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	time.Sleep(m.delay)
	return "tool output", nil
}

// TestToolStagesAreTwoDistinctMoments pins the clock contract the tool
// loop relies on: tool_start fires BEFORE execution, tool_end AFTER it,
// and the two timestamps are never the same moment (the v1.2.6 defect
// marked tool_start after the tool had already returned).
func TestToolStagesAreTwoDistinctMoments(t *testing.T) {
	clock := NewRunClock()

	if _, ok := clock.LastStageTime(StageToolStart); ok {
		t.Fatal("fresh clock already carries tool_start")
	}

	tool := &measuredTool{name: "measured", delay: 40 * time.Millisecond}

	execStart := time.Now()

	clock.Mark(StageToolStart)

	if ts, _ := clock.LastStageTime(StageToolStart); ts.Before(execStart) {
		t.Fatal("tool_start fired before the tool began — the inversion is back")
	}

	_, err := tool.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("tool.Run: %v", err)
	}

	clock.AddToolMs(time.Since(execStart).Milliseconds())
	clock.Mark(StageToolEnd)

	ts, okS := clock.LastStageTime(StageToolStart)
	te, okE := clock.LastStageTime(StageToolEnd)

	if !okS || !okE {
		t.Fatal("tool_start/tool_end marks missing after the executed path")
	}

	if !te.After(ts) {
		t.Fatal("tool_end is not strictly after tool_start — one timestamp was reused for both stages")
	}

	snap := clock.Snapshot()

	if snap.ToolMs < 30 {
		t.Fatalf("toolMs = %d — the measured 40ms execution did not accumulate", snap.ToolMs)
	}

	if _, ok := snap.Stages[StageToolStart]; !ok {
		t.Fatal("tool_start missing from the provenance map")
	}

	if _, ok := snap.Stages[StageToolEnd]; !ok {
		t.Fatal("tool_end missing from the provenance map")
	}
}

// TestTimingStagesCarryProvenance pins the raw-timestamp contract: every
// FIRED stage appears in Timing.Stages with its unix-milli timestamp, and
// stages that never fired stay absent — never invented.
func TestTimingStagesCarryProvenance(t *testing.T) {
	clock := NewRunClock()

	clock.SetReceived(time.Now())
	clock.Mark(StageClassified)
	clock.Mark(StageRequestSent)
	clock.Mark(StageResponseHeaders)
	clock.Mark(StageFirstByte)
	clock.Mark(StageFirstToken)
	clock.Mark(StageGenerationEnd)
	clock.Mark(StageDone)

	snap := clock.Snapshot()

	for _, stage := range []string{
		StageReceived, StageClassified, StageRequestSent,
		StageResponseHeaders, StageFirstByte, StageFirstToken,
		StageGenerationEnd, StageDone,
	} {
		if ts, ok := snap.Stages[stage]; !ok || ts == 0 {
			t.Fatalf("stage %s missing from the provenance map: %+v", stage, snap.Stages)
		}
	}

	// Unmarked stages stay absent.
	if _, ok := snap.Stages[StageToolStart]; ok {
		t.Fatal("tool_start appears in provenance although it never fired")
	}

	if _, ok := snap.Stages[StageToolEnd]; ok {
		t.Fatal("tool_end appears in provenance although it never fired")
	}
}

// TestStageResponseHeadersWireName pins the stage names (the diagnostics
// wire contract).
func TestStageResponseHeadersWireName(t *testing.T) {
	if StageResponseHeaders != "response_headers" {
		t.Fatalf("StageResponseHeaders = %q, want response_headers", StageResponseHeaders)
	}

	if StageFirstByte != "first_byte" || StageFirstToken != "first_token" {
		t.Fatal("first_byte/first_token stage names changed — wire contract broken")
	}
}
