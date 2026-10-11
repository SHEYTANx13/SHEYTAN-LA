package api

// repoindex_api_test.go — v1.3.4 regression contract for the
// repository-intelligence API surface: status (walk-free), bounded
// refresh, hybrid search, workspace-switch re-indexing and the
// method/error contract.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRepoFixture(t *testing.T, root string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "internal", "util"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":                     "module example.com/fixture\n\ngo 1.26\n",
		"main.go":                    "package main\n\nimport (\n\t\"example.com/fixture/internal/util\"\n)\n\nfunc main() { _ = util.Helper() }\n",
		"internal/util/util.go":      "package util\n\nfunc Helper() string { return \"h\" }\n",
		"internal/util/util_test.go": "package util\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) { _ = Helper() }\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepoIndexAPIContract(t *testing.T) {
	server, cfg := newTestServer(t)

	// Switch to a real project fixture (this exercises the bounded
	// re-index hook on the switch path too).
	proj := t.TempDir()
	writeRepoFixture(t, proj)

	body := `{"path":"` + strings.ReplaceAll(proj, `\`, `\\`) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspace/switch", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Config.Handler.(http.Handler).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("switch = %d: %s", rec.Code, rec.Body.String())
	}

	// Status after the switch: the index for the new root exists and
	// carries the fixture's measured numbers.
	resp, err := http.Get(server.URL + "/api/repo/index")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/repo/index = %d", resp.StatusCode)
	}
	var status struct {
		Root      string         `json:"root"`
		State     string         `json:"state"`
		Files     int            `json:"files"`
		Symbols   int            `json:"symbols"`
		DepEdges  int            `json:"depEdges"`
		TestLinks int            `json:"testLinks"`
		Languages map[string]int `json:"languages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Root != filepath.Clean(proj) {
		t.Errorf("status root = %q, want %q", status.Root, filepath.Clean(proj))
	}
	if status.Files != 4 {
		t.Errorf("files = %d, want 4", status.Files)
	}
	if status.Symbols == 0 || status.DepEdges == 0 || status.TestLinks == 0 {
		t.Errorf("index incomplete: %+v", status)
	}
	if status.Languages["go"] != 3 {
		t.Errorf("go files = %d, want 3", status.Languages["go"])
	}

	// Refresh: POST, bounded, returns report + fresh status.
	req = httptest.NewRequest(http.MethodPost, "/api/repo/index/refresh", nil)
	rec = httptest.NewRecorder()
	server.Config.Handler.(http.Handler).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST refresh = %d: %s", rec.Code, rec.Body.String())
	}
	var refresh struct {
		Report struct {
			Indexed   int  `json:"indexed"`
			Reindexed int  `json:"reindexed"`
			Partial   bool `json:"partial"`
		} `json:"report"`
		Status struct {
			Files int `json:"files"`
		} `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&refresh); err != nil {
		t.Fatal(err)
	}
	if refresh.Report.Indexed != 4 {
		t.Errorf("refresh indexed %d, want 4", refresh.Report.Indexed)
	}
	if refresh.Report.Reindexed != 0 {
		t.Errorf("refresh re-parsed %d files on an unchanged tree, want 0 (incremental contract)", refresh.Report.Reindexed)
	}

	// Search: symbol dimension finds Helper in util.go.
	req = httptest.NewRequest(http.MethodPost, "/api/repo/search", strings.NewReader(`{"symbol":"Helper"}`))
	rec = httptest.NewRecorder()
	server.Config.Handler.(http.Handler).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST search = %d: %s", rec.Code, rec.Body.String())
	}
	var search struct {
		TotalHits int `json:"totalHits"`
		Results   []struct {
			Path     string   `json:"path"`
			Evidence string   `json:"evidence"`
			Symbols  []string `json:"symbols"`
		} `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}
	if search.TotalHits == 0 || search.Results[0].Path != "internal/util/util.go" {
		t.Errorf("symbol search failed: %+v", search)
	}

	// Search: dependency dimension resolves the import edge.
	req = httptest.NewRequest(http.MethodPost, "/api/repo/search", strings.NewReader(`{"depsOf":"main.go"}`))
	rec = httptest.NewRecorder()
	server.Config.Handler.(http.Handler).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST depsOf search = %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}
	if search.TotalHits == 0 || search.Results[0].Path != "internal/util/util.go" {
		t.Errorf("depsOf search failed: %+v", search)
	}

	// Empty query is a 400 with a structured error.
	req = httptest.NewRequest(http.MethodPost, "/api/repo/search", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	server.Config.Handler.(http.Handler).ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("empty search = %d, want 400", rec.Code)
	}

	// GET on a POST-only endpoint is 405.
	req = httptest.NewRequest(http.MethodGet, "/api/repo/search", nil)
	rec = httptest.NewRecorder()
	server.Config.Handler.(http.Handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/repo/search = %d, want 405", rec.Code)
	}

	_ = cfg
}
