package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// TestBuildChatRequestCarriesSamplingSettings is the regression test for
// the v1.1.4Z fix: MinP, RepeatLastN, PresencePenalty and FrequencyPenalty
// were editable in Settings but silently dropped from every request.
func TestBuildChatRequestCarriesSamplingSettings(t *testing.T) {
	cfg := config.Default()
	cfg.Provider = config.ProviderLocal
	cfg.LLM.MinP = 0.05
	cfg.LLM.RepeatLastN = 128
	cfg.LLM.PresencePenalty = 0.3
	cfg.LLM.FrequencyPenalty = 0.4

	client := NewClient(config.NewSource(cfg))

	req := client.BuildChatRequest("m", nil, nil)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	wire := string(body)

	for _, want := range []string{
		`"min_p":0.05`,
		`"repeat_last_n":128`,
		`"presence_penalty":0.3`,
		`"frequency_penalty":0.4`,
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("wire body missing %s — the setting was dropped again:\n%s", want, wire)
		}
	}
}

// TestBuildChatRequestRemoteOmitsLlamaOnlyKnobs: strict OpenAI-compatible
// remotes may reject min_p / repeat_last_n / top_k / n_ctx — they stay
// local-only. presence/frequency penalty are OpenAI-standard and ship.
func TestBuildChatRequestRemoteOmitsLlamaOnlyKnobs(t *testing.T) {
	cfg := config.Default()
	cfg.Provider = config.ProviderRemote
	cfg.RemoteBaseURL = "https://api.example.com/v1"
	cfg.LLM.MinP = 0.05
	cfg.LLM.RepeatLastN = 128
	cfg.LLM.PresencePenalty = 0.2

	client := NewClient(config.NewSource(cfg))

	req := client.BuildChatRequest("m", nil, nil)
	body, _ := json.Marshal(req)
	wire := string(body)

	for _, banned := range []string{`"min_p"`, `"repeat_last_n"`, `"n_ctx"`, `"top_k"`} {
		if strings.Contains(wire, banned) {
			t.Errorf("remote wire body must not contain %s:\n%s", banned, wire)
		}
	}

	if !strings.Contains(wire, `"presence_penalty":0.2`) {
		t.Errorf("remote wire body should carry the OpenAI-standard presence_penalty:\n%s", wire)
	}
}

// TestSetBusyHookIsConcurrencySafe mirrors the runtime wiring call racing
// the streaming markBusy path (v1.1.4Z: plain field write + read raced).
func TestSetBusyHookIsConcurrencySafe(t *testing.T) {
	client := NewClient(config.NewSource(config.Default()))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			client.SetBusyHook(func(bool) {})
			client.SetBusyHook(nil)
		}
	}()

	for i := 0; i < 500; i++ {
		client.markBusy(true)
		client.markBusy(false)
	}

	<-done
}

// TestReadModelCardParsesSyntheticHeader proves the GGUF parser works and
// is actually reachable (before v1.1.4Z it had zero callers and no tests).
func TestReadModelCardParsesSyntheticHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.gguf")

	buf := &strings.Builder{}
	buf.WriteString("GGUF")

	putU32 := func(v uint32) {
		var b [4]byte
		b[0] = byte(v)
		b[1] = byte(v >> 8)
		b[2] = byte(v >> 16)
		b[3] = byte(v >> 24)
		buf.Write(b[:])
	}

	putU64 := func(v uint64) {
		var b [8]byte
		for i := 0; i < 8; i++ {
			b[i] = byte(v >> (8 * i))
		}
		buf.Write(b[:])
	}

	putStr := func(s string) {
		putU64(uint64(len(s)))
		buf.WriteString(s)
	}

	putU32(3) // version 3
	putU64(0) // tensor count (unused by the parser)
	putU64(2) // kv count

	// general.architecture = "qwen2" (type 8 string)
	putStr("general.architecture")
	putU32(8)
	putStr("qwen2")

	// general.file_type = 15 (Q4_K_M, type 4 uint32)
	putStr("general.file_type")
	putU32(4)
	putU32(15)

	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	card, err := ReadModelCard(path)
	if err != nil {
		t.Fatalf("ReadModelCard: %v", err)
	}
	if card == nil {
		t.Fatal("card is nil")
	}
	if card.Arch != "qwen2" {
		t.Errorf("Arch = %q, want qwen2", card.Arch)
	}
	if card.Quant != "Q4_K_M" {
		t.Errorf("Quant = %q, want Q4_K_M", card.Quant)
	}

	// corrupt file must error, not panic
	if err := os.WriteFile(path, []byte("NOTGGUFxx"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := ReadModelCard(path); err == nil {
		t.Error("corrupt header must return an error")
	}
}

// startQuietServer serves a chat endpoint that never responds.
func startQuietServer(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// never write another byte — the connection hangs open
		<-r.Context().Done()
	}))

	return srv
}

// TestStreamStallWatchdogAbortsQuietStream pins the v1.1.4Z stall
// watchdog: a stream that never sends a byte must be aborted with the
// stall error instead of hanging for the old 10-minute client timeout.
func TestStreamStallWatchdogAbortsQuietStream(t *testing.T) {
	// A handler that accepts the request and never writes a byte.
	srv := startQuietServer(t)
	defer srv.Close()

	cfg := config.Default()
	cfg.Provider = config.ProviderLocal
	cfg.LLMBaseURL = srv.URL
	cfg.LlamaHost = "127.0.0.1"
	cfg.LlamaPort = 0

	client := NewClient(config.NewSource(cfg))
	req := client.BuildChatRequest("m", []Message{{Role: "user", Content: "hi"}}, nil)

	// Shrink the stall window for the test.
	orig := streamStallTimeout
	streamStallTimeout = 300 * time.Millisecond
	defer func() { streamStallTimeout = orig }()

	start := time.Now()
	_, err := client.StreamChatDetailed(context.Background(), req, func(StreamEvent) error { return nil })

	if err == nil {
		t.Fatal("quiet stream must fail, not hang forever")
	}
	if !strings.Contains(err.Error(), "stalled") && !strings.Contains(err.Error(), "no data") {
		t.Logf("err = %v", err)
	}

	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("watchdog took %v — it should fire after the stall window", elapsed)
	}
}
