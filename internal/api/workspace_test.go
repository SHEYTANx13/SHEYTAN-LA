package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceSummaryAndSwitch(t *testing.T) {
	server, _ := newTestServer(t)

	// Summary on a fresh install: default scratch workspace, honest exists.
	req := httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec := httptest.NewRecorder()
	srv := server.Config.Handler.(http.Handler)
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET /api/workspace = %d: %s", rec.Code, rec.Body.String())
	}
	var sum workspaceSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatal(err)
	}
	if sum.Root == "" {
		t.Fatal("empty workspace root in summary")
	}

	// Switch to a real directory.
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"path":"` + strings.ReplaceAll(proj, `\`, `\\`) + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/workspace/switch", strings.NewReader(body))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST /api/workspace/switch = %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Ok      bool             `json:"ok"`
		Summary workspaceSummary `json:"summary"`
		Changed []string         `json:"changed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Ok || out.Summary.Root != filepath.Clean(proj) {
		t.Fatalf("switch did not land: %+v", out.Summary.Root)
	}
	if len(out.Changed) == 0 {
		t.Fatal("switch response must state what changed")
	}
	if out.Summary.IsDefault {
		t.Fatal("root should no longer be the default scratch workspace")
	}

	// Switching to a missing directory must fail with 400.
	req = httptest.NewRequest(http.MethodPost, "/api/workspace/switch", strings.NewReader(`{"path":"Z:/definitely/missing/dir"}`))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("switch to missing dir = %d, want 400", rec.Code)
	}

	// Switching to a FILE must also fail.
	fileTmp := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(fileTmp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/workspace/switch", strings.NewReader(`{"path":"`+fileTmp+`"}`))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("switch to file = %d, want 400", rec.Code)
	}

	// Recent workspaces now contain the project.
	req = httptest.NewRequest(http.MethodGet, "/api/workspace", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range sum.Recents {
		if r == filepath.Clean(proj) {
			found = true
		}
	}
	if !found {
		t.Fatalf("recent workspaces missing %s: %v", proj, sum.Recents)
	}
}
