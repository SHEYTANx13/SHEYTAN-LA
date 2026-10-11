package engine

// engine_test.go — native engine lifecycle tests (v1.1.5 Phase 1).
//
// The fake host uses the SAME test-binary re-exec pattern as the llama
// tests (internal/llm/llama_test.go): the test executable re-runs itself
// as a native host speaking the real protocol implementation from
// protocol.go, so framing/round-trip correctness is exercised in both
// directions on every run.
//
// Coverage required by the Phase 1 plan:
//   - native lifecycle (start → ready, stop → stopped)
//   - native failure (death → bounded restart → terminal failure)
//   - malformed native requests (garbage host output; connection loss)
//   - cancellation (cancel op round-trip; op timeout)
//   - concurrent lifecycle access (race detector target)
//   - hardware profile (native probe merged with sysinfo)
//   - metrics (only measured values)

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// TestMain implements the re-exec fake host.
func TestMain(m *testing.M) {
	if os.Getenv("SHEYTAN_FAKE_NATIVE_HOST") == "1" {
		runFakeNativeHost()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// runFakeNativeHost speaks the REAL protocol implementation over
// stdin/stdout. Modes (via SHEYTAN_FAKE_NATIVE_MODE):
//
//	""         well-behaved host (model ops included)
//	"crash"    dies 150 ms after becoming ready (watchdog target)
//	"garbage" writes non-protocol bytes, then idles (broken host)
//	"slow"     delays every response by 3 s (timeout target)
//	"badping" answers ping with the wrong protocol version
//	"loadfail" load_model always fails with a parse-style error
//	"loadslow" load_model delays 3 s (bounded load timeout target)
//	"hang"     reads frames but never answers (bounded-boot target)
func runFakeNativeHost() {
	mode := os.Getenv("SHEYTAN_FAKE_NATIVE_MODE")

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	in := bufio.NewReader(os.Stdin)

	// Model concern state of this fake host process (fresh per spawn,
	// exactly like the real C++ engine).
	var modelLoaded bool
	var modelPath string
	var modelError string

	// Fake generation registry (cancel addressing).
	var fakeActiveGen string
	var fakeGenCancel bool

	ready := func() {
		if mode == "crash" {
			go func() {
				time.Sleep(150 * time.Millisecond)
				os.Exit(1)
			}()
		}
	}

	for {
		payload, err := ReadFrame(in)
		if err != nil {
			return // EOF or malformed frame: exit
		}

		if mode == "garbage" {
			// Deliberately write non-protocol bytes once.
			_, _ = out.WriteString("this is not a frame at all\n")
			_ = out.Flush()

			// Then idle: the connection is dead but the process lives.
			select {}
		}

		var req Request
		if err := json.Unmarshal(payload, &req); err != nil || req.Op == "" {
			_ = EncodeResponse(out, &Response{
				ID:    0,
				OK:    false,
				Error: "malformed request",
			})
			_ = out.Flush()
			continue
		}

		if mode == "slow" {
			time.Sleep(3 * time.Second)
		}

		if mode == "hang" {
			// Handshake-hang target: the process lives and reads
			// frames but never answers. The supervisor's bounded
			// boot (caller context / handshake timeout) must
			// tear it down deterministically — no orphan.
			select {}
		}

		resp := &Response{ID: req.ID, OK: true}

		switch req.Op {
		case OpPing:
			proto := ProtocolVersion
			if mode == "badping" {
				proto = ProtocolVersion + 99
			}

			result, _ := json.Marshal(PingResult{
				ProtocolVersion: proto,
				ABIVersion:      ABIVersionExpected,
				Engine:          "fake-native-host",
			})
			resp.Result = result

		case OpHealth:
			ready()

			result, _ := json.Marshal(HealthResult{
				Healthy: true,
				State:   "ready",
				Detail:  "fake host",
			})
			resp.Result = result

		case OpHardware:
			setFakeHW(&resp.Result)

		case OpMetrics:
			result, _ := json.Marshal(MetricsResult{
				EngineState:     "ready",
				UptimeSeconds:   1.5,
				ProcessRSSBytes: 4096,
			})
			resp.Result = result

		case OpCancel:
			var cp CancelPayload
			_ = json.Unmarshal(req.Payload, &cp)

			if cp.RequestID != "" && fakeActiveGen == cp.RequestID {
				fakeGenCancel = true
				result, _ := json.Marshal(CancelResult{Cancelled: true})
				resp.Result = result
			} else {
				result, _ := json.Marshal(CancelResult{
					Cancelled: false,
					Reason:    "no active generation request with this id (fake host)",
				})
				resp.Result = result
			}

		case OpLoadModel:
			if mode == "loadslow" {
				time.Sleep(3 * time.Second)
			}

			var payload LoadModelPayload
			if len(req.Payload) == 0 || json.Unmarshal(req.Payload, &payload) != nil || payload.Path == "" {
				resp.OK = false
				resp.Error = "malformed load_model payload"
				break
			}

			if mode == "loadfail" {
				modelLoaded = false
				modelPath = payload.Path
				modelError = "gguf: bad magic (not a GGUF file)"
				resp.OK = false
				resp.Error = modelError
				break
			}

			// The fake host cannot parse GGUF; it reports a canned
			// metadata card for the requested path (bounded, honest
			// about being fake in the engine name only).
			modelLoaded = true
			modelPath = payload.Path
			modelError = ""

			info := NativeModelInfo{
				Path:              modelPath,
				Architecture:      "llama",
				Name:              "fake-model",
				Quantization:      "Q4_K_M",
				State:             ModelStateLoaded,
				FileSizeBytes:     4096,
				ParameterCount:    123000,
				ContextLength:     256,
				VocabularySize:    96,
				EmbeddingLength:   64,
				LayerCount:        2,
				TensorCount:       3,
				GGUFVersion:       3,
				FileType:          15,
				HasFileType:       true,
				GenerationCapable: true,
				KVCacheBytes:      2 * 2 * 256 * 64 * 2,
				WorkspaceBytes:    256 * 96 * 4,
				TotalPlanBytes:    4096 + 2*2*256*64*2 + 256*96*4 + 64<<20,
			}
			plan := NativeMemoryPlan{
				ModelFileBytes:       4096,
				MappedBytes:          4096,
				WeightsBytes:         2048,
				WorkspaceBytes:       256 * 96 * 4,
				KVCacheBytes:         2 * 2 * 256 * 64 * 2,
				RuntimeOverheadBytes: 64 << 20,
				TotalBytes:           4096 + 2048 + 256*96*4 + 2*2*256*64*2 + 64<<20,
				AvailableRAMBytes:    8 << 30,
				FitsInRAM:            1,
			}

			result, _ := json.Marshal(ModelOpResult{
				Loaded: true,
				State:  ModelStateLoaded,
				Model:  &info,
				Memory: &plan,
			})
			resp.Result = result

		case OpUnloadModel:
			modelLoaded = false
			modelPath = ""
			modelError = ""

			result, _ := json.Marshal(ModelOpResult{
				Loaded: false,
				State:  ModelStateUnloaded,
			})
			resp.Result = result

		case OpModelInfo:
			result := ModelOpResult{
				Loaded: modelLoaded,
				State:  ModelStateUnloaded,
			}

			if modelLoaded {
				result.State = ModelStateLoaded
				result.Model = &NativeModelInfo{
					Path:              modelPath,
					Architecture:      "llama",
					Name:              "fake-model",
					Quantization:      "Q4_K_M",
					State:             ModelStateLoaded,
					FileSizeBytes:     4096,
					ParameterCount:    123000,
					ContextLength:     256,
					VocabularySize:    96,
					EmbeddingLength:   64,
					LayerCount:        2,
					TensorCount:       3,
					GGUFVersion:       3,
					FileType:          15,
					HasFileType:       true,
					GenerationCapable: true,
				}
				result.Memory = &NativeMemoryPlan{
					ModelFileBytes:       4096,
					MappedBytes:          4096,
					WeightsBytes:         2048,
					WorkspaceBytes:       256 * 96 * 4,
					KVCacheBytes:         2 * 2 * 256 * 64 * 2,
					RuntimeOverheadBytes: 64 << 20,
					TotalBytes:           4096 + 2048 + 256*96*4 + 2*2*256*64*2 + 64<<20,
					AvailableRAMBytes:    8 << 30,
					FitsInRAM:            1,
				}
			} else if modelError != "" {
				result.State = ModelStateFailed
				result.Model = &NativeModelInfo{
					Path:  modelPath,
					State: ModelStateFailed,
					Error: modelError,
				}
			}

			data, _ := json.Marshal(result)
			resp.Result = data

		case OpGenerate:
			var gp GeneratePayload
			if len(req.Payload) == 0 || json.Unmarshal(req.Payload, &gp) != nil || gp.Prompt == "" || gp.MaxTokens == 0 {
				resp.OK = false
				resp.Error = "generate requires a prompt and maxTokens (fake host)"
				break
			}

			if !modelLoaded {
				resp.OK = false
				resp.Error = "generation: no model loaded"
				break
			}

			if mode == "genfail" {
				resp.OK = false
				resp.Error = "generation: model is not natively executable - unsupported tensor type (fake host)"
				break
			}

			// Stream event chunks, then the final frame (multiple frames
			// for one id — the exact streaming shape the real host
			// produces).
			fakeActiveGen = gp.RequestID
			fakeGenCancel = false

			words := []string{"hello ", "world ", "from ", "the ", "fake ", "host"}

			n := int(gp.MaxTokens)
			if n > len(words) {
				n = len(words)
			}

			if mode == "genstreamslow" {
				time.Sleep(50 * time.Millisecond)
			}

			for i := 0; i < n; i++ {
				if fakeGenCancel {
					break
				}
				chunkResult, _ := json.Marshal(GenerationEventResult{
					RequestID: gp.RequestID,
					Text:      words[i],
					Token:     uint32(i + 1),
				})
				ev := &Response{
					ID:     req.ID,
					OK:     true,
					Event:  "chunk",
					Result: chunkResult,
				}
				_ = EncodeResponse(out, ev)
				_ = out.Flush()
			}

			finish := FinishLength
			generated := uint32(n)
			if fakeGenCancel {
				finish = FinishCancelled
				generated = 0
			}

			final := GenerationFinalResult{
				RequestID:       gp.RequestID,
				FinishReason:    finish,
				PromptTokens:    5,
				GeneratedTokens: generated,
				Metrics: GenerationMetricsWire{
					PromptSeconds:         0.001,
					TTFTSeconds:           0.002,
					DecodeSeconds:         0.003,
					TotalSeconds:          0.004,
					TokensPerSecond:       1000,
					PromptTokensPerSecond: 5000,
					KVPositionsUsed:       uint64(5 + generated),
				},
			}
			finalData, _ := json.Marshal(final)
			resp.Result = finalData
			fakeActiveGen = ""

		case OpShutdown:
			_ = EncodeResponse(out, &Response{ID: req.ID, OK: true, Result: json.RawMessage("{}")})
			_ = out.Flush()
			return

		default:
			resp.OK = false
			resp.Error = fmt.Sprintf("unknown op %q", req.Op)
		}

		_ = EncodeResponse(out, resp)
		_ = out.Flush()
	}
}

// setFakeHW replaces the hardware result payload with the fake values.
func setFakeHW(dst *json.RawMessage) {
	type fakeCPU struct {
		Name          string `json:"name"`
		PhysicalCores int    `json:"physicalCores"`
		LogicalCores  int    `json:"logicalCores"`
	}

	type fakeRAM struct {
		TotalBytes     uint64 `json:"totalBytes"`
		AvailableBytes uint64 `json:"availableBytes"`
	}

	payload := struct {
		Architecture string  `json:"architecture"`
		CPU          fakeCPU `json:"cpu"`
		RAM          fakeRAM `json:"ram"`
	}{
		Architecture: "x86_64",
		CPU:          fakeCPU{Name: "Fake CPU", PhysicalCores: 4, LogicalCores: 8},
		RAM:          fakeRAM{TotalBytes: 16 << 30, AvailableBytes: 8 << 30},
	}

	data, _ := json.Marshal(payload)
	*dst = data
}

// fakeHostPath returns the re-exec path (this test binary) with the fake
// host environment prepared. The returned cleanup restores the env.
func fakeHostPath(t *testing.T, mode string) (string, func()) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary path: %v", err)
	}

	dir := t.TempDir()

	// The host binary must live somewhere; the Engine spawns the path
	// directly, so the test binary itself is fine.
	if mode != "" {
		t.Setenv("SHEYTAN_FAKE_NATIVE_MODE", mode)
	} else {
		t.Setenv("SHEYTAN_FAKE_NATIVE_MODE", "")
	}

	script := exe

	_ = dir

	return script, func() {}
}

func newFakeEngine(t *testing.T, mode string) *Engine {
	t.Helper()

	path, cleanup := fakeHostPath(t, mode)
	t.Cleanup(cleanup)

	t.Setenv("SHEYTAN_FAKE_NATIVE_HOST", "1")

	return New(path)
}

// --- protocol tests ---------------------------------------------------------

func TestProtocolFramingRoundTrip(t *testing.T) {
	var sink strings.Builder

	req := &Request{ID: 42, Op: OpHealth}

	if err := EncodeRequest(&sink, req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	payload, err := ReadFrame(strings.NewReader(sink.String()))
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	decoded, err := DecodeRequest(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.ID != 42 || decoded.Op != OpHealth {
		t.Fatalf("round trip mismatch: %+v", decoded)
	}
}

func TestProtocolRejectsOversizedFrame(t *testing.T) {
	header := []byte{0xFF, 0xFF, 0xFF, 0xFF} // > 1 MiB

	if _, err := ReadFrame(strings.NewReader(string(header))); err != ErrFrameTooLarge {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}

func TestProtocolRejectsTruncatedFrame(t *testing.T) {
	frame := []byte{10, 0, 0, 0, 's', 'h'} // header says 10 bytes, body 2

	if _, err := ReadFrame(strings.NewReader(string(frame))); err == nil {
		t.Fatal("expected truncated frame error")
	}
}

func TestDecodeRequestValidation(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		errPart string
	}{
		{"malformed json", "{not json", "malformed"},
		{"missing op", `{"id":1}`, "missing op"},
		{"unknown op", `{"id":1,"op":"explode"}`, "unknown op"},
		{"empty", "", "malformed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeRequest([]byte(tc.payload))
			if err == nil {
				t.Fatalf("expected error for %q", tc.payload)
			}

			if !strings.Contains(err.Error(), tc.errPart) {
				t.Fatalf("error %q does not mention %q", err, tc.errPart)
			}
		})
	}
}

func TestDecodeResultRejectsErrorResponses(t *testing.T) {
	resp := &Response{ID: 1, OK: false, Error: "engine exploded"}

	var out PingResult

	if err := DecodeResult(resp, &out); err == nil {
		t.Fatal("expected error for failed response")
	}

	respOK := &Response{ID: 1, OK: true, Result: json.RawMessage(`{"healthy":true}`)}

	var health HealthResult
	if err := DecodeResult(respOK, &health); err != nil {
		t.Fatalf("healthy result: %v", err)
	}

	if !health.Healthy {
		t.Fatal("healthy flag lost")
	}
}

// --- lifecycle tests ---------------------------------------------------------

func TestEngineStartReachesReady(t *testing.T) {
	e := newFakeEngine(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer e.Stop(context.Background())

	if e.State() != llm.StateReady {
		t.Fatalf("state = %q, want ready", e.State())
	}

	if !e.IsAlive() {
		t.Fatal("engine not alive after start")
	}

	if e.Pid() == 0 {
		t.Fatal("pid not reported")
	}

	if e.Restarts() != 0 {
		t.Fatalf("restarts = %d, want 0", e.Restarts())
	}
}

func TestEngineStartFailsWithMissingBinary(t *testing.T) {
	e := New(filepath.Join(t.TempDir(), "does-not-exist"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := e.Start(ctx)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}

	if e.State() != llm.StateFailed {
		t.Fatalf("state = %q, want failed", e.State())
	}
}

func TestEngineHealthRoundTrip(t *testing.T) {
	e := newFakeEngine(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer e.Stop(context.Background())

	report, err := e.Health(ctx)
	if err != nil {
		t.Fatalf("health: %v", err)
	}

	if !report.Alive || report.State != llm.StateReady {
		t.Fatalf("health report: %+v", report)
	}
}

func TestEngineHealthWhenDown(t *testing.T) {
	e := newFakeEngine(t, "")

	if _, err := e.Health(context.Background()); err == nil {
		t.Fatal("expected error when engine is down")
	}
}

func TestEngineStopWalksStoppingToStopped(t *testing.T) {
	e := newFakeEngine(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	events, unsubscribe := e.SubscribeEvents()
	defer unsubscribe()

	if err := e.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if e.State() != llm.StateStopped {
		t.Fatalf("state = %q, want stopped", e.State())
	}

	// The event stream must contain the stopping → stopped walk.
	var sawStopping, sawStopped bool

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				goto done
			}

			if ev.State == llm.StateStopping {
				sawStopping = true
			}

			if ev.State == llm.StateStopped {
				sawStopped = true
			}

		case <-time.After(2 * time.Second):
			goto done
		}
	}

done:
	if !sawStopping || !sawStopped {
		t.Fatalf("event walk incomplete: stopping=%t stopped=%t", sawStopping, sawStopped)
	}
}

func TestEngineEventsArePublished(t *testing.T) {
	e := newFakeEngine(t, "")

	events, unsubscribe := e.SubscribeEvents()
	defer unsubscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer e.Stop(context.Background())

	var states []string

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				goto done2
			}

			states = append(states, ev.State)

			if ev.State == llm.StateReady {
				goto done2
			}

		case <-time.After(5 * time.Second):
			goto done2
		}
	}

done2:
	joined := strings.Join(states, ",")

	if !strings.Contains(joined, llm.StateStarting) {
		t.Fatalf("missing starting transition in %q", joined)
	}

	if !strings.Contains(joined, llm.StateReady) {
		t.Fatalf("missing ready transition in %q", joined)
	}
}

func TestEngineDeathTriggersBoundedRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("bounded-restart ladder takes ~8 s")
	}

	// Crash mode: every host instance dies 150 ms after ready.
	e := newFakeEngine(t, "crash")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The watchdog must restart (bounded) and eventually give up with a
	// visible failed state. Backoff 1s+2s+4s + ~150ms lifetime per host.
	deadline := time.Now().Add(30 * time.Second)

	for e.State() != llm.StateFailed {
		if time.Now().After(deadline) {
			t.Fatalf("engine never reached failed (state %q, restarts %d)", e.State(), e.Restarts())
		}

		time.Sleep(100 * time.Millisecond)
	}

	if e.Restarts() < 3 {
		t.Fatalf("restarts = %d, want >= 3 before giving up", e.Restarts())
	}

	if e.Detail() == "" {
		t.Fatal("expected a visible failure detail")
	}
}

func TestEngineMalformedHostOutputDetected(t *testing.T) {
	// Garbage mode: the host writes non-protocol bytes and idles.
	e := newFakeEngine(t, "garbage")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start fails during the handshake because the connection breaks.
	err := e.Start(ctx)
	if err == nil {
		defer e.Stop(context.Background())

		// If the handshake raced the garbage write, the next health
		// probe must fail instead.
		if _, herr := e.Health(ctx); herr == nil {
			t.Fatal("expected health failure against a garbage host")
		}

		return
	}

	if e.State() != llm.StateFailed {
		t.Fatalf("state = %q, want failed after garbage host", e.State())
	}
}

func TestEngineSlowHostHitsOpTimeout(t *testing.T) {
	e := newFakeEngine(t, "slow")

	// The slow host delays every response by 3 s; the caller-bounded
	// boot must give up well before that and tear down deterministically.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := e.Start(ctx)

	if err == nil {
		defer e.Stop(context.Background())
		t.Fatal("expected slow host to fail the bounded start")
	}

	if e.State() != llm.StateFailed {
		t.Fatalf("state = %q, want failed after slow host timeout", e.State())
	}
}

func TestEngineConcurrentLifecycle(t *testing.T) {
	e := newFakeEngine(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_ = e.Start(ctx)

			_, _ = e.Health(ctx)

			_, _ = e.MetricsSnapshot(ctx)
		}()
	}

	wg.Wait()

	if e.State() != llm.StateReady {
		t.Fatalf("state = %q, want ready after concurrent starts", e.State())
	}

	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestEngineCancelReturnsReason(t *testing.T) {
	e := newFakeEngine(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer e.Stop(context.Background())

	// Phase 1: cancel is a real round-trip but there are no generation
	// requests, so the engine honestly reports the miss.
	err := e.Cancel(ctx, "req-1")
	if err == nil {
		t.Fatal("expected cancel miss (no active requests in Phase 1)")
	}

	if !strings.Contains(err.Error(), "no active") {
		t.Fatalf("unexpected cancel error: %v", err)
	}
}

func TestEngineMetricsMeasuredValues(t *testing.T) {
	e := newFakeEngine(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer e.Stop(context.Background())

	m, err := e.MetricsSnapshot(ctx)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}

	if m.Backend != "native" {
		t.Fatalf("backend = %q", m.Backend)
	}

	if m.EngineState != llm.StateReady {
		t.Fatalf("engineState = %q", m.EngineState)
	}

	// The fake host reports measured values.
	if m.UptimeSeconds <= 0 {
		t.Fatal("uptime not measured")
	}

	if m.ProcessRSSBytes == 0 {
		t.Fatal("RSS not measured")
	}

	if m.Pid == 0 {
		t.Fatal("pid not measured")
	}

	// No generation exists: decode metrics must stay absent, not zero.
	if m.DecodeTokensPerSecond != 0 {
		t.Fatal("decode speed must not be invented")
	}
}

func TestEngineHardwareMergesSysinfo(t *testing.T) {
	e := newFakeEngine(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer e.Stop(context.Background())

	hw, err := e.Hardware(ctx)
	if err != nil {
		t.Fatalf("hardware: %v", err)
	}

	if hw.Backend != "native" {
		t.Fatalf("backend = %q", hw.Backend)
	}

	// Fake host contributes the native-side values.
	if hw.CPU.LogicalCores != 8 {
		t.Fatalf("logical cores = %d, want 8 (fake host value)", hw.CPU.LogicalCores)
	}

	if hw.RAM.TotalBytes != 16<<30 {
		t.Fatalf("ram total = %d, want fake value", hw.RAM.TotalBytes)
	}

	// sysinfo merge supplies real values.
	found := false

	for _, src := range hw.DetectedBy {
		if src == "sysinfo" {
			found = true
		}
	}

	if !found {
		t.Fatalf("sysinfo not merged: %v", hw.DetectedBy)
	}

	if len(hw.GPUs) == 0 && hw.DetectedBy[len(hw.DetectedBy)-1] != "sysinfo" {
		t.Fatal("unexpected detectedBy state")
	}
}

// --- backend contract tests ---------------------------------------------------

func TestBackendImplementsContract(t *testing.T) {
	var _ llm.Backend = NewBackend(nil)
}

// TestBackendGenerationLifecycleGuard pins the honest lifecycle guard of
// the native generation surface (Phase 5 semantics): without a running
// engine generation fails with the lifecycle error (never a fake success),
// and request shapes the native path cannot serve return the explicit
// ErrNotImplemented fallback signal the router acts on.
func TestBackendGenerationLifecycleGuard(t *testing.T) {
	b := NewBackend(New("/nonexistent"))

	if b.Name() != "native" {
		t.Fatalf("name = %q", b.Name())
	}

	if b.GenerationCapable() {
		t.Fatal("native backend without a running engine must not claim generation capability")
	}

	// Phase 5 semantics: generation is REAL now. With no running engine
	// the error is the honest lifecycle one (the router checks
	// GenerationCapable before ever calling); the ErrNotImplemented
	// FALLBACK SIGNAL is reserved for request shapes the native path
	// cannot serve (tools / images — see TestBackendGenerationContract).
	if _, err := b.Generate(context.Background(), &llm.ChatRequest{}); err == nil {
		t.Fatal("Generate must fail without a running engine")
	} else if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("Generate error must name the lifecycle state, got %v", err)
	}

	if _, err := b.StreamGenerate(context.Background(), &llm.ChatRequest{}, nil); err == nil {
		t.Fatal("StreamGenerate must fail without a running engine")
	}

	// The explicit fallback signal for unsupported request shapes:
	dummy := llm.ToolSpec{Type: "function"}
	dummy.Function.Name = "x"
	req := &llm.ChatRequest{Tools: []llm.ToolSpec{dummy}}
	if _, err := b.Generate(context.Background(), req); err == nil ||
		!strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("tool-carrying request must return the ErrNotImplemented fallback signal, got %v", err)
	}
}

func TestBackendLoadModelValidatesFirst(t *testing.T) {
	// The engine under it has no running host: validation errors must
	// fire before any attempt to reach the subprocess, and a valid file
	// must fail with a lifecycle error (engine not running), never with
	// a fake success.
	dir := t.TempDir()
	valid := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(valid, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := NewBackend(New("/nonexistent"))

	// Empty path: validation error.
	err := b.LoadModel(context.Background(), llm.ModelSpec{})
	if err == nil || strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected validation error for empty path, got %v", err)
	}

	// Missing file: validation error.
	err = b.LoadModel(context.Background(), llm.ModelSpec{Path: filepath.Join(dir, "nope.gguf")})
	if err == nil || strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected file validation error, got %v", err)
	}

	// Valid file but engine down: honest lifecycle error.
	err = b.LoadModel(context.Background(), llm.ModelSpec{Path: valid})
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected engine-not-running error, got %v", err)
	}

	// Unload with no engine: honest idempotent no-op.
	if err := b.UnloadModel(context.Background()); err != nil {
		t.Fatalf("unload with engine down must be an idempotent no-op, got %v", err)
	}

	// ModelInfo with engine down: unloaded snapshot, no error.
	info, err := b.ModelInfo(context.Background())
	if err != nil {
		t.Fatalf("ModelInfo with engine down: %v", err)
	}
	if info.Backend != "native" || info.Loaded {
		t.Fatalf("unexpected ModelInfo with engine down: %+v", info)
	}
}

// --- model concern validation ---------------------------------------------------

func TestValidateModelSpecPathJail(t *testing.T) {
	root := t.TempDir()

	inside := filepath.Join(root, "model.gguf")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ValidateModelSpec(root, inside); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}

	if _, err := ValidateModelSpec(root, ""); err == nil {
		t.Fatal("empty path accepted")
	}

	if _, err := ValidateModelSpec(root, filepath.Join(root, "..", "escape.gguf")); err == nil {
		t.Fatal("path escape accepted")
	}

	if _, err := ValidateModelSpec(root, root); err == nil {
		t.Fatal("directory accepted as model")
	}
}

// --- Phase 5: generation streaming (fake host) --------------------------------

// TestEngineStreamGenerationFakeHost exercises the full Go streaming path
// (ipc streamCall + event dispatch + final frame) against the fake host:
// chunks arrive in order, the final result carries finish reason and
// measured metrics, and the engine returns to ready.
func TestEngineStreamGenerationFakeHost(t *testing.T) {
	e := newFakeEngine(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start fake host: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = e.Stop(stopCtx)
	})

	// Load the fake model first (capability + loaded state). The engine
	// validates the file exists (the fake host supplies the metadata).
	fakeModel := filepath.Join(t.TempDir(), "fake.gguf")
	if err := os.WriteFile(fakeModel, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write placeholder model: %v", err)
	}
	if err := e.LoadModel(ctx, ModelSpec{Path: fakeModel}); err != nil {
		t.Fatalf("load model: %v", err)
	}

	if !e.NativeGenerationCapable() {
		t.Fatal("loaded fake model should report generation capable")
	}

	var mu sync.Mutex
	var got strings.Builder
	var chunks int

	result, err := e.StreamGeneration(ctx, GenerationRequest{
		RequestID: "test-gen-1",
		Prompt:    "hello",
		MaxTokens: 4,
	}, func(chunk GenerationChunk) error {
		mu.Lock()
		defer mu.Unlock()
		chunks++
		got.WriteString(chunk.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("stream generation: %v", err)
	}

	if result.FinishReason != FinishLength {
		t.Fatalf("finish reason = %q, want length", result.FinishReason)
	}
	if result.GeneratedTokens != 4 {
		t.Fatalf("generated tokens = %d, want 4", result.GeneratedTokens)
	}
	if result.PromptTokens != 5 {
		t.Fatalf("prompt tokens = %d, want 5", result.PromptTokens)
	}
	if result.Metrics.TokensPerSecond != 1000 {
		t.Fatalf("tokens/sec = %f, want 1000", result.Metrics.TokensPerSecond)
	}
	if result.Metrics.KVPositionsUsed != 9 {
		t.Fatalf("kv positions = %d, want 9", result.Metrics.KVPositionsUsed)
	}
	if chunks != 4 {
		t.Fatalf("chunks = %d, want 4", chunks)
	}
	if got.String() != "hello world from the " {
		t.Fatalf("streamed text = %q", got.String())
	}

	// Engine state returned to ready after generation.
	if e.State() != llm.StateReady {
		t.Fatalf("state = %q, want ready", e.State())
	}

	// The engine is reusable for a second request.
	result2, err := e.StreamGeneration(ctx, GenerationRequest{
		RequestID: "test-gen-2",
		Prompt:    "again",
		MaxTokens: 2,
	}, func(chunk GenerationChunk) error { return nil })
	if err != nil {
		t.Fatalf("second generation: %v", err)
	}
	if result2.GeneratedTokens != 2 {
		t.Fatalf("second generation tokens = %d, want 2", result2.GeneratedTokens)
	}
}

// TestEngineStreamGenerationNoModel verifies the no-model error path.
func TestEngineStreamGenerationNoModel(t *testing.T) {
	e := newFakeEngine(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start fake host: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = e.Stop(stopCtx)
	})

	_, err := e.StreamGeneration(ctx, GenerationRequest{
		Prompt:    "hello",
		MaxTokens: 4,
	}, func(chunk GenerationChunk) error { return nil })
	if err == nil {
		t.Fatal("expected error without a loaded model")
	}
	if !strings.Contains(err.Error(), "no model") {
		t.Fatalf("error = %v, want no-model", err)
	}

	// Capability is false with no model loaded.
	if e.NativeGenerationCapable() {
		t.Fatal("no model loaded should NOT be generation capable")
	}
}

// TestEngineStreamGenerationCtxCancel verifies that a caller context
// abort cancels the generation through the cancel op and returns a
// context error (the engine stays usable).
func TestEngineStreamGenerationCtxCancel(t *testing.T) {
	e := newFakeEngine(t, "genstreamslow")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start fake host: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = e.Stop(stopCtx)
	})

	fakeModel := filepath.Join(t.TempDir(), "fake.gguf")
	if err := os.WriteFile(fakeModel, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write placeholder model: %v", err)
	}
	if err := e.LoadModel(ctx, ModelSpec{Path: fakeModel}); err != nil {
		t.Fatalf("load model: %v", err)
	}

	genCtx, genCancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer genCancel()

	_, err := e.StreamGeneration(genCtx, GenerationRequest{
		RequestID: "cancel-target",
		Prompt:    "hello",
		MaxTokens: 6,
	}, func(chunk GenerationChunk) error { return nil })
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if ctxErr := genCtx.Err(); ctxErr == nil {
		t.Fatalf("expected genCtx to be cancelled, err = %v", err)
	}

	// The engine is reusable after the cancellation.
	result, err := e.StreamGeneration(ctx, GenerationRequest{
		RequestID: "after-cancel",
		Prompt:    "hello",
		MaxTokens: 2,
	}, func(chunk GenerationChunk) error { return nil })
	if err != nil {
		t.Fatalf("generation after cancellation: %v", err)
	}
	if result.GeneratedTokens != 2 {
		t.Fatalf("tokens = %d, want 2", result.GeneratedTokens)
	}
}

// TestBackendGenerationContract exercises the llm.Backend adapter's
// generation surface (Generate + StreamGenerate + GenerationCapable)
// against the fake host.
func TestBackendGenerationContract(t *testing.T) {
	e := newFakeEngine(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start fake host: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = e.Stop(stopCtx)
	})

	b := NewBackend(e)

	if b.GenerationCapable() {
		t.Fatal("fresh engine must not report generation capable")
	}

	fakeModel := filepath.Join(t.TempDir(), "fake.gguf")
	if err := os.WriteFile(fakeModel, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write placeholder model: %v", err)
	}
	if err := b.LoadModel(ctx, llm.ModelSpec{Path: fakeModel}); err != nil {
		t.Fatalf("load: %v", err)
	}

	if !b.GenerationCapable() {
		t.Fatal("loaded fake model must report generation capable")
	}

	req := &llm.ChatRequest{
		Model:     "fake",
		MaxTokens: 3,
		Messages:  []llm.Message{{Role: "user", Content: "hi there"}},
	}

	// Generate (non-streaming).
	resp, err := b.Generate(ctx, req)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "hello world from " {
		t.Fatalf("content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "length" {
		t.Fatalf("finish = %q", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 3 {
		t.Fatalf("usage = %+v", resp.Usage)
	}

	// StreamGenerate (streaming).
	var events []llm.StreamEvent
	perf, err := b.StreamGenerate(ctx, req, func(ev llm.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream generate: %v", err)
	}
	if len(events) != 4 { // 3 content + 1 terminal
		t.Fatalf("events = %d, want 4", len(events))
	}
	if events[0].Content != "hello " {
		t.Fatalf("first event content = %q", events[0].Content)
	}
	if events[3].FinishReason != "length" {
		t.Fatalf("terminal finish = %q", events[3].FinishReason)
	}
	if events[3].Usage == nil || events[3].Usage.CompletionTokens != 3 {
		t.Fatalf("terminal usage = %+v", events[3].Usage)
	}
	if perf.Tokens != 3 {
		t.Fatalf("perf tokens = %d, want 3", perf.Tokens)
	}
	if perf.WallMs < 0 {
		t.Fatalf("perf wall = %d", perf.WallMs)
	}

	// Tool-carrying requests: explicit not-implemented fallback signal.
	var dummyTool llm.ToolSpec
	dummyTool.Type = "function"
	dummyTool.Function.Name = "dummy"

	toolReq := &llm.ChatRequest{
		Model:     "fake",
		MaxTokens: 3,
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
		Tools:     []llm.ToolSpec{dummyTool},
	}
	if _, err := b.Generate(ctx, toolReq); err == nil ||
		!strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("tool request error = %v, want ErrNotImplemented signal", err)
	}
	if _, err := b.StreamGenerate(ctx, toolReq, func(llm.StreamEvent) error { return nil }); err == nil ||
		!strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("tool stream request error = %v, want ErrNotImplemented signal", err)
	}

	// Image-carrying requests: same signal.
	imgReq := &llm.ChatRequest{
		Model:     "fake",
		MaxTokens: 3,
		Messages:  []llm.Message{{Role: "user", Content: "hi", Images: []string{"/tmp/x.png"}}},
	}
	if _, err := b.Generate(ctx, imgReq); err == nil ||
		!strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("image request error = %v, want ErrNotImplemented signal", err)
	}
}

// TestBackendGenerationFailureFallback verifies the unsupported-model
// error path (genfail mode) surfaces an inspectable error.
func TestBackendGenerationFailureFallback(t *testing.T) {
	e := newFakeEngine(t, "genfail")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start fake host: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = e.Stop(stopCtx)
	})

	b := NewBackend(e)

	fakeModel := filepath.Join(t.TempDir(), "fake.gguf")
	if err := os.WriteFile(fakeModel, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write placeholder model: %v", err)
	}
	if err := b.LoadModel(ctx, llm.ModelSpec{Path: fakeModel}); err != nil {
		t.Fatalf("load: %v", err)
	}

	// The fake host reports generationCapable on load, then generate
	// fails — exactly the mid-flight failure the router handles by
	// falling back before the first token.
	_, err := b.Generate(ctx, &llm.ChatRequest{
		MaxTokens: 4,
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected generation failure")
	}
	if !strings.Contains(err.Error(), "natively executable") {
		t.Fatalf("error = %v, want unsupported-model detail", err)
	}
}
