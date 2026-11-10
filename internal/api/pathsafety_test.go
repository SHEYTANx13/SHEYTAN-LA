package api

// pathsafety_test.go — v1.11.1 path-injection remediation (adversarial
// regression tests for the api-boundary alerts): run ids arrive from
// request bodies and are used as single path components under
// <DataDir>/runs/paused; clone destinations, workspace-switch paths and
// model paths arrive straight from HTTP requests. These tests pin the
// hostile spellings (traversal, absolute, drive/UNC, NUL, ADS colon,
// dotfiles, control characters) and prove that a hostile value is
// rejected with a clear error and leaves files OUTSIDE the intended root
// untouched.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func hostileRunIDs() []string {
	return []string{
		"",                  // empty
		".",                 // dot
		"..",                // parent
		"../escape",         // POSIX traversal
		"..\\escape",        // Windows traversal
		"a/../escape",       // nested traversal
		"/etc/passwd",       // POSIX absolute
		"\\Windows\\evil",   // Windows rooted
		"C:paused",          // drive-relative
		"C:\\Users\\evil",   // drive absolute
		"\\\\server\\share", // UNC
		"run:stream",        // Windows ADS colon
		"\x00evil",          // NUL
		" evil",             // whitespace-padded
		"ev il",             // embedded space (not in grammar)
		"evil/id",           // embedded separator
		".hidden",           // leading dot
	}
}

func TestValidRunIDGrammar(t *testing.T) {
	good := []string{
		newRunID(),                  // server-minted hex form
		"20060102T150405.000000000", // crypto-fallback timestamp form
		"0123456789abcdef01234567",  // legacy hex-ish
		"run_fallback-1",            // grammar characters
	}
	for _, id := range good {
		if !validRunID(id) {
			t.Errorf("run id %q must be accepted by the grammar", id)
		}
	}
	for _, id := range hostileRunIDs() {
		if validRunID(id) {
			t.Errorf("hostile run id %q must be rejected by the grammar", id)
		}
	}
}

func TestPausedRunPathFailsClosed(t *testing.T) {
	srv := newAdmissionTestServer(t)

	for _, id := range hostileRunIDs() {
		if p, ok := srv.pausedRunPath(id); ok {
			t.Errorf("pausedRunPath(%q) must fail closed, got path %q", id, p)
		}
	}

	if p, ok := srv.pausedRunPath(newRunID()); !ok || p == "" {
		t.Fatalf("a minted run id must resolve, got %q ok=%t", p, ok)
	}
}

func TestHostileRunIDCheckpointOperationsLeaveTreeUntouched(t *testing.T) {
	srv := newAdmissionTestServer(t)

	// A decoy OUTSIDE the paused directory that a traversal payload would
	// overwrite if containment failed.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.json")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, id := range hostileRunIDs() {
		if _, ok := srv.loadPausedRecord(id); ok {
			t.Errorf("loadPausedRecord(%q) must not find a record", id)
		}
		if err := srv.savePausedRecord(&pausedRunRecord{RunID: id}); !errors.Is(err, errInvalidRunID) {
			t.Errorf("savePausedRecord(%q) must fail with the invalid-run-id cause, got %v", id, err)
		}
		srv.deletePausedRecord(id) // must be a no-op, never a traversal remove
	}

	// The outside decoy is byte-identical and nothing new appeared outside.
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("outside file vanished: %v", err)
	}
	if string(got) != "outside-original" {
		t.Errorf("outside file content changed: %q", got)
	}
	entries, err := os.ReadDir(outsideDir)
	if err != nil {
		t.Fatalf("read outside dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("hostile run ids created files outside the paused directory: %d entries", len(entries))
	}
}

func TestEditTxJournalFailsClosed(t *testing.T) {
	srv := newAdmissionTestServer(t)

	for _, id := range hostileRunIDs() {
		if p := srv.editTxPath(id); p != "" {
			t.Errorf("editTxPath(%q) must yield no path for a hostile id, got %q", id, p)
		}
		if _, ok := srv.loadEditTx(id); ok {
			t.Errorf("loadEditTx(%q) must not find a journal", id)
		}
		rec := &editTxRecord{
			Version:     editTxSchemaVer,
			RunID:       id,
			SessionID:   "s1",
			Phase:       editTxPrepared,
			NewRevision: 2,
			OldRevision: 1,
		}
		if err := srv.saveEditTx(rec); !errors.Is(err, errInvalidRunID) {
			t.Errorf("saveEditTx(runId=%q) must fail with the invalid-run-id cause, got %v", id, err)
		}
		srv.deleteEditTx(id) // must be a no-op, never a traversal remove
	}

	// Nothing was written into the paused directory either.
	if entries, err := os.ReadDir(srv.pausedDir()); err == nil && len(entries) != 0 {
		t.Errorf("hostile journal attempts left entries in the paused dir: %v", entries)
	}
}

func TestVerifyEditTxRefusesHostileRunID(t *testing.T) {
	// A syntactically complete journal whose runId escapes the grammar is
	// untrustworthy: verifyEditTx must fail it closed so recovery
	// quarantines the record under its FILE name, never under this id.
	body := `{"version":1,"runId":"../escape","sessionId":"s1","phase":"prepared",
                  "oldRevision":1,"newRevision":2,"integrity":"nope"}`
	if _, err := verifyEditTx([]byte(body)); err == nil {
		t.Fatal("a journal with a hostile runId must fail verification")
	} else if !strings.Contains(err.Error(), "run id") {
		t.Errorf("rejection must name the run-id cause, got: %v", err)
	}
}

func TestModelFilePathBoundary(t *testing.T) {
	good := []string{
		"/tmp/models/model.gguf",
		"/models/a..b.gguf",    // dots INSIDE a component are a legal name
		"/models/x_y-z 1.gguf", // spaces are legal in filenames
	}
	bad := []string{
		"",
		"model.gguf",            // relative
		"models/model.gguf",     // relative
		"../model.gguf",         // traversal
		"/models/../model.gguf", // surviving traversal after clean
		"a\x00b.gguf",           // NUL
		"/models/a\nb.gguf",     // newline (control character)
		strings.Repeat("a", 4097),
	}
	for _, p := range good {
		if !validModelFilePath(p) {
			t.Errorf("model path %q must be accepted", p)
		}
	}
	for _, p := range bad {
		if validModelFilePath(p) {
			t.Errorf("hostile model path %q must be rejected", p)
		}
	}
}

func TestModelCardForRejectsHostilePaths(t *testing.T) {
	srv := newAdmissionTestServer(t)
	cfg := srv.src.Load()

	for _, p := range []string{
		"",
		"model.gguf",
		"../model.gguf",
		"/models/../model.gguf",
		"a\x00b.gguf",
		"../../etc/passwd",
	} {
		if card, caps := modelCardFor(p, cfg); card != nil || caps != nil {
			t.Errorf("modelCardFor(%q) must fail soft with no card, got card=%v caps=%v", p, card, caps)
		}
	}
}

func TestWorkspaceSwitchRejectsHostileRequestPaths(t *testing.T) {
	server, _ := newTestServer(t)
	srv := server.Config.Handler.(http.Handler)

	// The workspace root before the hostile attempts.
	root := workspaceRootOf(t, server)

	for _, body := range []string{
		`{"path":"../escape"}`,
		`{"path":".."}`,
		`{"path":"a\u0000evil"}`,  // NUL smuggled through JSON
		`{"path":"a\u000aevil"}`,  // newline (control character)
		`{"path":"relative/dir"}`, // CWD-relative resolution is not a feature
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/workspace/switch", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("switch with %s = %d, want 400: %s", body, rec.Code, rec.Body.String())
		}
	}

	// No hostile attempt landed: the workspace root is unchanged.
	if got := workspaceRootOf(t, server); got != root {
		t.Errorf("workspace root changed by a hostile switch: %q -> %q", root, got)
	}
}

// workspaceRootOf reads the served workspace summary root.
func workspaceRootOf(t *testing.T, server *httptest.Server) string {
	t.Helper()

	resp, err := http.Get(server.URL + "/api/workspace")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var sum struct {
		Root string `json:"root"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sum); err != nil {
		t.Fatal(err)
	}
	if sum.Root == "" {
		t.Fatal("empty workspace root in summary")
	}
	return sum.Root
}

func TestCloneDestinationConstrainedToWorkspaceRoot(t *testing.T) {
	t.Setenv("SHEYTAN_GIT_BINARY", fakeAPISuccessGit(t))

	server, cfg := newTestServer(t)

	root := cfg.WorkspaceDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// Every hostile destination is rejected with 400 invalid-destination
	// BEFORE any clone job exists.
	for _, body := range []string{
		fmtDestination(t.TempDir()),                         // absolute OUTSIDE the root
		fmtDestination(filepath.Join(root, "..", "escape")), // traversal out of the root
		`{"url":"https://github.com/owner/repository.git","destination":"repo/../../../escape"}`,
		`{"url":"https://github.com/owner/repository.git","destination":"C:\\Users\\evil"}`,
		`{"url":"https://github.com/owner/repository.git","destination":"\\\\server\\share\\repo"}`,
		`{"url":"https://github.com/owner/repository.git","destination":"a\u0000evil"}`,
	} {
		code, out := postClone(t, server, body)
		if code != http.StatusBadRequest {
			t.Errorf("clone with %s = %d, want 400: %v", body, code, out)
			continue
		}
		if out["errorKind"] != "invalid-destination" {
			t.Errorf("clone with %s: errorKind = %v, want invalid-destination", body, out["errorKind"])
		}
	}

	// No job was started by the hostile attempts.
	if code, status := getCloneStatus(t, server); code != http.StatusOK || status["state"] != "idle" {
		t.Errorf("status after hostile attempts = %d/%v, want 200/idle", code, status["state"])
	}

	// The positive control: a destination INSIDE the workspaces root is
	// accepted and lands exactly there (the constrained contract).
	dest := filepath.Join(root, "inside")
	code, out := postClone(t, server, fmtDestination(dest))
	if code != http.StatusAccepted {
		t.Fatalf("clone into the workspaces root = %d: %v", code, out)
	}
	final := waitForCloneTerminal(t, server)
	if final["state"] != "succeeded" {
		t.Fatalf("constrained clone state = %v (%v)", final["state"], final)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("cloned file missing inside the workspaces root: %v", err)
	}
}

// fmtDestination builds a clone request body with an absolute destination.
func fmtDestination(dest string) string {
	return `{"url":"https://github.com/owner/repository.git","destination":` +
		strconv.Quote(dest) + `}`
}

func TestCloneRelativeDestinationStaysInsideWorkspacesRoot(t *testing.T) {
	t.Setenv("SHEYTAN_GIT_BINARY", fakeAPISuccessGit(t))

	server, cfg := newTestServer(t)
	root := cfg.WorkspaceDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// A plain relative destination resolves INSIDE the workspaces root —
	// never against the server's working directory.
	code, out := postClone(t, server, `{"url":"https://github.com/owner/repository.git","destination":"repo"}`)
	if code != http.StatusAccepted {
		t.Fatalf("relative clone = %d: %v", code, out)
	}
	final := waitForCloneTerminal(t, server)
	if final["state"] != "succeeded" {
		t.Fatalf("relative clone state = %v (%v)", final["state"], final)
	}
	if _, err := os.Stat(filepath.Join(root, "repo", "README.md")); err != nil {
		t.Fatalf("relative destination did not land inside the workspaces root: %v", err)
	}
}

// TestModelFilePathBoundaryWindowsShapes pins the selected-host-path
// contract of validModelFilePath across host path syntaxes (v1.11.1
// Windows repair): a POSIX-spelled absolute selection must be accepted
// on Windows exactly where IsAbs refuses it, dots INSIDE a component
// ("a..b.gguf") are an ordinary name, and genuine ".." components,
// relatives, control bytes and volume tricks are refused on every host.
func TestModelFilePathBoundaryWindowsShapes(t *testing.T) {
	good := []string{
		"/tmp/models/model.gguf", // POSIX-spelled absolute (rooted on Windows)
		"/models/a..b.gguf",      // dots inside a component are a legal name
		"/models/x_y-z 1.gguf",   // spaces are legal in filenames
	}
	if runtime.GOOS == "windows" {
		good = append(good,
			`C:\models\model.gguf`, // drive-absolute selection
			`C:/models/model.gguf`, // drive-absolute, forward-slash spelling
			`\models\model.gguf`,   // volume-rooted selection
		)
	}
	for _, p := range good {
		if !validModelFilePath(p) {
			t.Errorf("model path %q must be accepted on %s", p, runtime.GOOS)
		}
	}

	bad := []string{
		"",                      // empty
		"model.gguf",            // relative
		"models/model.gguf",     // relative
		"../model.gguf",         // traversal
		"/models/../model.gguf", // surviving parent component
		"a\x00b.gguf",           // NUL
		"/models/a\nb.gguf",     // control character
		strings.Repeat("a", 4097),
	}
	if runtime.GOOS == "windows" {
		bad = append(bad,
			`C:models\model.gguf`, // drive-relative
			`C:models/model.gguf`, // drive-relative, forward-slash spelling
		)
	} else {
		// On POSIX hosts a drive-prefixed string is not a host path at
		// all: the selected-file contract keeps it refused.
		bad = append(bad, `C:\models\model.gguf`)
	}
	for _, p := range bad {
		if validModelFilePath(p) {
			t.Errorf("hostile model path %q must be rejected on %s", p, runtime.GOOS)
		}
	}
}
