// clone_api_test.go — v1.3.0 regression tests for the GitHub clone HTTP
// surface. The fake git executable is injected through the
// SHEYTAN_GIT_BINARY seam (the same seam gitclone.DefaultGitBinary
// reads).
//
// v1.3.5: the fake git is the TEST BINARY ITSELF re-executed through
// the shared internal/testfakes helper — a real executable on Windows
// and Unix — replacing the POSIX shell scripts that could not run on
// the Windows release pipeline.
package api

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

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/testfakes"
)

// TestMain arms the re-exec helper: when the clone machinery spawns
// this test binary as the fake git, the helper runs the scripted
// behavior instead of the test suite.
//
// v1.8.1: the SAME TestMain also dispatches the fake llama.cpp engine
// (localchat_v181_test.go) — the run-level E2E tests spawn the test
// binary as the local engine subprocess, exactly like the llm package's
// fake engine.
func TestMain(m *testing.M) {
        if testfakes.RunFakeGit(os.Args[1:]) {
                return // the helper ran and ended the process
        }
        if runFakeLlamaEngineV181() {
                return // the fake engine served and ended the process
        }
        os.Exit(m.Run())
}

// fakeGitBinary returns the fake git executable path for the scripted
// mode: the test binary itself plus the mode variable (inherited by the
// spawned clone process through the parent environment, restored
// automatically by t.Setenv).
func fakeGitBinary(t *testing.T, mode string) string {
        t.Helper()

        exe, err := os.Executable()
        if err != nil {
                t.Fatalf("test binary path: %v", err)
        }

        t.Setenv(testfakes.GitModeEnv, mode)
        return exe
}

func fakeAPISuccessGit(t *testing.T) string {
        return fakeGitBinary(t, "success")
}

func postClone(t *testing.T, srv *httptest.Server, body string) (int, map[string]any) {
        t.Helper()

        resp, err := http.Post(srv.URL+"/api/workspace/clone", "application/json", strings.NewReader(body))
        if err != nil {
                t.Fatalf("POST clone: %v", err)
        }
        defer resp.Body.Close()

        var out map[string]any
        if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
                t.Fatalf("decode clone response: %v", err)
        }
        return resp.StatusCode, out
}

func getCloneStatus(t *testing.T, srv *httptest.Server) (int, map[string]any) {
        t.Helper()

        resp, err := http.Get(srv.URL + "/api/workspace/clone/status")
        if err != nil {
                t.Fatalf("GET clone status: %v", err)
        }
        defer resp.Body.Close()

        var out map[string]any
        if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
                t.Fatalf("decode clone status: %v", err)
        }
        return resp.StatusCode, out
}

// waitForCloneTerminal polls the status endpoint until the state leaves
// "running" (or the test times out).
func waitForCloneTerminal(t *testing.T, srv *httptest.Server) map[string]any {
        t.Helper()

        deadline := time.Now().Add(20 * time.Second)
        for {
                _, status := getCloneStatus(t, srv)
                state, _ := status["state"].(string)
                if state != "" && state != "running" && state != "idle" {
                        return status
                }
                if time.Now().After(deadline) {
                        t.Fatalf("clone never reached a terminal state: %v", status)
                }
                time.Sleep(50 * time.Millisecond)
        }
}

func TestCloneEndpointSuccessfulCloneAndAutoSwitch(t *testing.T) {
        t.Setenv("SHEYTAN_GIT_BINARY", fakeAPISuccessGit(t))

        server, cfg := newTestServer(t)

        destParent := cfg.WorkspaceDir()
        dest := filepath.Join(destParent, "repository")

        body := fmt.Sprintf(`{"url":"https://github.com/owner/repository.git","destination":%q}`, dest)
        code, out := postClone(t, server, body)
        if code != http.StatusAccepted {
                t.Fatalf("POST clone = %d: %v", code, out)
        }
        if out["state"] != "running" {
                t.Fatalf("initial state = %v", out["state"])
        }

        final := waitForCloneTerminal(t, server)
        if final["state"] != "succeeded" {
                t.Fatalf("final state = %v (%v)", final["state"], final)
        }

        // The clone landed on disk.
        if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
                t.Fatalf("cloned file missing: %v", err)
        }

        // The workspace switched automatically.
        if final["switched"] != true {
                t.Fatalf("workspace not auto-switched: %v", final)
        }

        // The ACTIVE workspace is now the cloned repository.
        resp, err := http.Get(server.URL + "/api/workspace")
        if err != nil {
                t.Fatal(err)
        }
        defer resp.Body.Close()
        var summary struct {
                Root      string   `json:"root"`
                Recents   []string `json:"recentWorkspaces"`
                IsDefault bool     `json:"isDefault"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
                t.Fatal(err)
        }
        if summary.Root != dest {
                t.Fatalf("workspace root = %q, want %q", summary.Root, dest)
        }
        if summary.IsDefault {
                t.Fatal("workspace still marked default after clone switch")
        }
        if len(summary.Recents) == 0 || summary.Recents[0] != dest {
                t.Fatalf("recents not updated: %v", summary.Recents)
        }
}

func TestCloneEndpointRejectsInvalidURL(t *testing.T) {
        t.Setenv("SHEYTAN_GIT_BINARY", fakeAPISuccessGit(t))

        server, _ := newTestServer(t)

        code, out := postClone(t, server, `{"url":"https://gitlab.com/owner/repository"}`)
        if code != http.StatusBadRequest {
                t.Fatalf("invalid URL = %d, want 400", code)
        }
        if out["errorKind"] != "invalid-url" {
                t.Fatalf("errorKind = %v", out["errorKind"])
        }
        if msg, _ := out["error"].(string); !strings.Contains(msg, "github.com") {
                t.Fatalf("error not actionable: %v", out["error"])
        }
}

func TestCloneEndpointRejectsMissingGit(t *testing.T) {
        t.Setenv("SHEYTAN_GIT_BINARY", "/nonexistent/git-binary-for-tests")

        server, _ := newTestServer(t)

        code, out := postClone(t, server, `{"url":"https://github.com/owner/repository"}`)
        if code != http.StatusBadRequest {
                t.Fatalf("missing git = %d, want 400", code)
        }
        if out["errorKind"] != "git-unavailable" {
                t.Fatalf("errorKind = %v", out["errorKind"])
        }
}

func TestCloneEndpointDestinationCollision(t *testing.T) {
        t.Setenv("SHEYTAN_GIT_BINARY", fakeAPISuccessGit(t))

        server, cfg := newTestServer(t)

        dest := filepath.Join(cfg.WorkspaceDir(), "occupied")
        if err := os.MkdirAll(dest, 0o755); err != nil {
                t.Fatal(err)
        }
        if err := os.WriteFile(filepath.Join(dest, "precious.txt"), []byte("keep me"), 0o644); err != nil {
                t.Fatal(err)
        }

        body := fmt.Sprintf(`{"url":"https://github.com/owner/repository","destination":%q}`, dest)
        code, out := postClone(t, server, body)
        if code != http.StatusConflict {
                t.Fatalf("destination collision = %d, want 409: %v", code, out)
        }
        if out["errorKind"] != "destination-exists" {
                t.Fatalf("errorKind = %v", out["errorKind"])
        }

        // Unrelated content untouched.
        if _, err := os.Stat(filepath.Join(dest, "precious.txt")); err != nil {
                t.Fatalf("existing content disturbed: %v", err)
        }
}

func TestCloneEndpointFailedCloneReportsClassifiedError(t *testing.T) {
        t.Setenv("SHEYTAN_GIT_BINARY", fakeGitBinary(t, "not-found"))

        server, cfg := newTestServer(t)

        dest := filepath.Join(cfg.WorkspaceDir(), "repository")
        code, out := postClone(t, server, fmt.Sprintf(`{"url":"https://github.com/owner/repository","destination":%q}`, dest))
        if code != http.StatusAccepted {
                t.Fatalf("POST clone = %d: %v", code, out)
        }

        final := waitForCloneTerminal(t, server)
        if final["state"] != "failed" {
                t.Fatalf("final state = %v", final["state"])
        }
        if final["errorKind"] != "repository-not-found" {
                t.Fatalf("errorKind = %v, want repository-not-found", final["errorKind"])
        }
        if final["switched"] == true {
                t.Fatal("failed clone must never switch the workspace")
        }
}

func TestCloneEndpointCancel(t *testing.T) {
        // A fake git that genuinely stays alive mid-clone until the real
        // cancellation path terminates the process tree.
        t.Setenv("SHEYTAN_GIT_BINARY", fakeGitBinary(t, "hang"))

        server, cfg := newTestServer(t)

        dest := filepath.Join(cfg.WorkspaceDir(), "repository")
        code, _ := postClone(t, server, fmt.Sprintf(`{"url":"https://github.com/owner/repository","destination":%q}`, dest))
        if code != http.StatusAccepted {
                t.Fatalf("POST clone = %d", code)
        }

        resp, err := http.Post(server.URL+"/api/workspace/clone/cancel", "application/json", nil)
        if err != nil {
                t.Fatalf("POST cancel: %v", err)
        }
        resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                t.Fatalf("cancel = %d", resp.StatusCode)
        }

        final := waitForCloneTerminal(t, server)
        if final["state"] != "canceled" {
                t.Fatalf("final state = %v, want canceled", final["state"])
        }

        // A second cancel with nothing running is a clean 409.
        resp2, err := http.Post(server.URL+"/api/workspace/clone/cancel", "application/json", nil)
        if err != nil {
                t.Fatal(err)
        }
        resp2.Body.Close()
        if resp2.StatusCode != http.StatusConflict {
                t.Fatalf("idle cancel = %d, want 409", resp2.StatusCode)
        }
}

func TestCloneStatusIdleOnFreshServer(t *testing.T) {
        t.Setenv("SHEYTAN_GIT_BINARY", fakeAPISuccessGit(t))

        server, _ := newTestServer(t)

        code, status := getCloneStatus(t, server)
        if code != http.StatusOK {
                t.Fatalf("status = %d", code)
        }
        if status["state"] != "idle" {
                t.Fatalf("fresh state = %v, want idle", status["state"])
        }
}
