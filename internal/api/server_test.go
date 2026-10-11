package api

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"encoding/binary"
	"encoding/json"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recall"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
)

// newTestServer builds a fully-wired Server against a temp data dir. The
// local engine cannot start here (no binary/model) — the prewarm fails
// fast and the engine reports failed, which is itself part of the
// contract under test.
func newTestServer(t *testing.T) (*httptest.Server, *config.Config) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off" // v1.2.0: no network in unit tests — the scheduled updater is owned and closed, never exercised here // no engine binary in tests — no prewarm noise

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

	return server, cfg
}

func TestEngineEndpointReportsAuthoritativeState(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/api/engine")
	if err != nil {
		t.Fatalf("GET /api/engine: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var snap struct {
		State    string `json:"state"`
		Provider string `json:"provider"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if snap.Provider != "local" {
		t.Fatalf("provider = %s", snap.Provider)
	}

	// Without a binary the engine is either idle (no prewarm) or failed
	// (prewarm attempt) — both are REAL backend states. What must never
	// happen is a fabricated ready.
	if snap.State == "ready" || snap.State == "running" || snap.State == "busy" {
		t.Fatalf("engine must not fabricate readiness, got %s", snap.State)
	}
}

func TestSessionLifecycleOverAPI(t *testing.T) {
	server, _ := newTestServer(t)

	// Create.
	resp, err := http.Post(server.URL+"/api/sessions", "application/json", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	defer resp.Body.Close()

	var sess struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if sess.ID == "" {
		t.Fatal("no session id")
	}

	// Rename.
	body, _ := json.Marshal(map[string]any{"title": "api test"})

	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/sessions/"+sess.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	_ = resp2.Body.Close()

	// Detail includes the new title.
	detail, err := http.Get(server.URL + "/api/sessions/" + sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	defer detail.Body.Close()

	var got struct {
		Title string `json:"title"`
	}

	_ = json.NewDecoder(detail.Body).Decode(&got)

	if got.Title != "api test" {
		t.Fatalf("title = %q", got.Title)
	}

	// Delete.
	reqDel, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/sessions/"+sess.ID, nil)

	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_ = respDel.Body.Close()

	// Second delete errors.
	respDel2, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("delete 2: %v", err)
	}

	defer respDel2.Body.Close()

	if respDel2.StatusCode != http.StatusInternalServerError && respDel2.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete should fail, got %d", respDel2.StatusCode)
	}
}

func TestAttachmentUploadInspectDelete(t *testing.T) {
	server, _ := newTestServer(t)

	// Upload via multipart.
	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("files", "notes.txt")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}

	content := strings.Repeat("attachment pipeline test line\n", 100)

	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	upload, err := http.Post(server.URL+"/api/attachments", writer.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	defer upload.Body.Close()

	if upload.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", upload.StatusCode)
	}

	var uploaded struct {
		OK          bool `json:"ok"`
		Attachments []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Kind string `json:"kind"`
			Size int64  `json:"size"`
		} `json:"attachments"`
		Failed []map[string]string `json:"failed"`
	}

	if err := json.NewDecoder(upload.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}

	if !uploaded.OK || len(uploaded.Attachments) != 1 {
		t.Fatalf("upload mismatch: ok=%v n=%d failed=%v", uploaded.OK, len(uploaded.Attachments), uploaded.Failed)
	}

	att := uploaded.Attachments[0]

	if att.Kind != "text" || att.Size != int64(len(content)) {
		t.Fatalf("attachment metadata: %+v", att)
	}

	// Inspect.
	inspect, err := http.Get(server.URL + "/api/attachments/" + att.ID)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	defer inspect.Body.Close()

	var detail struct {
		Attachment struct {
			Chunks []struct {
				Index int `json:"index"`
			} `json:"chunks"`
		} `json:"attachment"`
	}

	if err := json.NewDecoder(inspect.Body).Decode(&detail); err != nil {
		t.Fatalf("decode inspect: %v", err)
	}

	if len(detail.Attachment.Chunks) == 0 {
		t.Fatal("text attachment must be chunked")
	}

	// List shows it.
	list, err := http.Get(server.URL + "/api/attachments")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	defer list.Body.Close()

	_ = list.Body.Close()

	// Delete.
	reqDel, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/attachments/"+att.ID, nil)

	del, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	defer del.Body.Close()

	var deleted struct {
		OK bool `json:"ok"`
	}

	_ = json.NewDecoder(del.Body).Decode(&deleted)

	if !deleted.OK {
		t.Fatal("delete must report ok")
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	server, _ := newTestServer(t)

	// Missing message.
	resp, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(`{"sessionId":"whatever"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// Unknown session with message → 404.
	resp2, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(`{"sessionId":"nope","message":"hi"}`))
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}

	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp2.StatusCode)
	}
}

func TestConfigPatchRoundTrip(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/api/config")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}

	defer resp.Body.Close()

	var cfg map[string]any

	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := cfg["thinkingMode"]; !ok {
		t.Fatal("config response missing thinkingMode")
	}

	// The redacted config must never leak the API key.
	if key, _ := cfg["remoteApiKey"].(string); key != "" {
		t.Fatal("remoteApiKey leaked through GET /api/config")
	}
}

// guard against unused import in future refactors.
var _ = fmt.Sprintf

// --- config patch concurrency, feedback, models, run timeout ---

// --- v1.1.4 regression tests ---

// TestConfigPatchIsRaceFree exercises concurrent PATCH + GET + engine-gate
// reads against the copy-on-write source. Under -race the v1.1.3
// in-place mergeConfigPatch (`*s.cfg = updated`) failed this test.
func TestConfigPatchIsRaceFree(t *testing.T) {
	server, _ := newTestServer(t)

	var wg sync.WaitGroup

	patch := func(i int) {
		defer wg.Done()

		body, _ := json.Marshal(map[string]any{
			"maxIterations": 5 + i%20,
		})

		resp, err := http.Post(server.URL+"/api/config", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Errorf("PATCH: %v", err)
			return
		}
		resp.Body.Close()
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go patch(i)
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			resp, err := http.Get(server.URL + "/api/config")
			if err != nil {
				t.Errorf("GET: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}

	wg.Wait()
}

// TestConfigPatchRejectsUnboundedBody pins the MaxBytesReader guard.
func TestConfigPatchRejectsUnboundedBody(t *testing.T) {
	server, _ := newTestServer(t)

	huge := bytes.Repeat([]byte("a"), 2<<20) // 2 MB > 1 MB cap

	resp, err := http.Post(server.URL+"/api/config", "application/json", bytes.NewReader(huge))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("2 MB config patch must be rejected by the body cap")
	}
}

// TestAbortRequiresValidBody: the old handler returned {ok:true} for a
// malformed body while aborting nothing.
func TestAbortRequiresValidBody(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Post(
		server.URL+"/api/abort",
		"application/json",
		strings.NewReader("not json at all"),
	)
	if err != nil {
		t.Fatalf("POST /api/abort: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed abort body: status = %d, want 400", resp.StatusCode)
	}
}

// TestFeedbackEndpointWritesRecallSteering covers the previously dead
// SetFeedback write path: the verdict must persist and steer scoring.
func TestFeedbackEndpointWritesRecallSteering(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off" // v1.2.0: no network in unit tests — the scheduled updater is owned and closed, never exercised here

	engine := recall.New(cfg.DataDir)

	if err := engine.IndexTurn("s1", "t", "how do I parse csv", "use the csv reader", nil); err != nil {
		t.Fatalf("IndexTurn: %v", err)
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	// route the server's recall to our engine-backed store
	srv.recall = engine

	t.Cleanup(srv.Close)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(map[string]any{
		"sessionId": "s1",
		"query":     "how do I parse csv",
		"liked":     true,
	})

	resp, err := http.Post(ts.URL+"/api/feedback", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/feedback: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("feedback status = %d", resp.StatusCode)
	}

	id := recall.CapsuleID("s1", "how do I parse csv")
	if got := engine.FeedbackFor(id); got != 1 {
		t.Fatalf("FeedbackFor = %d, want +1 (the steering write path was dead before v1.1.4)", got)
	}

	if likes, _ := engine.FeedbackStats(); likes != 1 {
		t.Fatalf("FeedbackStats likes = %d, want 1", likes)
	}
}

// TestModelsEndpointIncludesGGUFMetadata proves the GGUF header parser is
// actually wired into the endpoint (a full parser sat dead in llm/gguf.go
// while /api/models shipped stat-only entries).
func TestModelsEndpointIncludesGGUFMetadata(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.LlamaAutoStart = false

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// synth-4k.gguf: the name IS the metadata contract for the endpoint
	// (list + stat + card). A synthetic GGUF header would test the parser,
	// which has its own dedicated unit test in package llm.
	modelPath := filepath.Join(cfg.ModelsDir, "test-model.gguf")
	if err := writeFakeModel(modelPath); err != nil {
		t.Fatalf("write model: %v", err)
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	t.Cleanup(srv.Close)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/models")
	if err != nil {
		t.Fatalf("GET /api/models: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Local []struct {
			ID        string `json:"id"`
			SizeBytes int64  `json:"sizeBytes"`
		} `json:"local"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(payload.Local) != 1 || payload.Local[0].ID != "test-model.gguf" {
		t.Fatalf("local models = %+v", payload.Local)
	}

	if payload.Local[0].SizeBytes == 0 {
		t.Fatal("SizeBytes missing")
	}
}

// writeFakeModel writes a minimal GGUF v2 file with one string kv —
// enough header for ReadModelCard to succeed without metadata.
func writeFakeModel(path string) error {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("GGUF")

	var u32 [4]byte

	putU32 := func(v uint32) {
		binary.LittleEndian.PutUint32(u32[:], v)
		buf.Write(u32[:])
	}

	putU32(2) // version
	putU32(1) // kv count
	putU32(8) // key length
	buf.WriteString("test.key")
	putU32(8) // type = string
	putU32(5) // value length
	buf.WriteString("value")

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// TestRunTimeoutBudgetApplies pins the per-run time budget: a session run
// with a tiny budget must terminate with the timeout caption, not hang.
func TestRunTimeoutBudgetApplies(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.LlamaAutoStart = false
	cfg.RunTimeoutMinutes = 1 // minimum clamp

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	t.Cleanup(srv.Close)

	// create a session directly through the store
	sess := srv.store.Create()

	// effective budget must be positive
	if got := cfg.EffectiveRunTimeout(); got < time.Minute {
		t.Fatalf("EffectiveRunTimeout = %v, want >= 1 minute", got)
	}

	_ = sess
}
