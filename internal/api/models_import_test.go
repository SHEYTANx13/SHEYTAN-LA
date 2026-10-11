package api

// models_import_test.go — v1.6.1: the first-class import surface through
// the HTTP API, proven with the REAL in-repo GGUF fixture: import → list
// → select through the EXISTING selection flow → honest selection state.

import (
        "bytes"
        "encoding/json"
        "net/http"
        "os"
        "path/filepath"
        "strings"
        "testing"
)

// realGGUFFixturePath returns the in-repo real GGUF fixture for API tests.
func realGGUFFixturePath(t *testing.T) string {
        t.Helper()

        candidates := []string{
                filepath.Join("..", "..", "native", "engine", "tests", "fixtures", "tiny-llama-f32.gguf"),
        }

        for _, c := range candidates {
                if _, err := os.Stat(c); err == nil {
                        abs, _ := filepath.Abs(c)
                        return abs
                }
        }

        t.Skip("real GGUF fixture not present in this checkout")
        return ""
}

// TestModelsImportImportsAndLists drives the full import surface: POST
// the real fixture's path, find the imported model in GET /api/models
// with its parsed metadata, and verify a duplicate import is reported.
func TestModelsImportImportsAndLists(t *testing.T) {
        server, _ := newTestServer(t)
        defer server.Close()

        fixture := realGGUFFixturePath(t)

        // Import through an "external" directory (the user's own location —
        // must stay untouched).
        external := t.TempDir()
        src := filepath.Join(external, "tiny-llama-f32.gguf")
        if data, err := os.ReadFile(fixture); err != nil {
                t.Fatal(err)
        } else if err := os.WriteFile(src, data, 0o644); err != nil {
                t.Fatal(err)
        }

        body, _ := json.Marshal(map[string]string{"path": src})
        resp, err := http.Post(server.URL+"/api/models/import", "application/json", bytes.NewReader(body))
        if err != nil {
                t.Fatal(err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                t.Fatalf("import = %d", resp.StatusCode)
        }

        var result modelsImportResponse
        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
                t.Fatal(err)
        }

        if !result.OK || result.Name != "tiny-llama-f32.gguf" {
                t.Fatalf("import result: %+v", result)
        }
        if result.Architecture != "llama" || result.Quantization != "F32" {
                t.Fatalf("imported metadata must come from the REAL GGUF header: %+v", result)
        }

        // The source survived verbatim.
        if _, err := os.Stat(src); err != nil {
                t.Fatalf("external source must never be removed: %v", err)
        }

        // The imported model appears in the listing with parsed metadata.
        list, err := http.Get(server.URL + "/api/models")
        if err != nil {
                t.Fatal(err)
        }
        defer list.Body.Close()

        var models struct {
                Local []struct {
                        ID           string `json:"id"`
                        Architecture string `json:"architecture"`
                        Quantization string `json:"quantization"`
                } `json:"local"`
        }
        if err := json.NewDecoder(list.Body).Decode(&models); err != nil {
                t.Fatal(err)
        }

        found := false
        for _, m := range models.Local {
                if m.ID == "tiny-llama-f32.gguf" {
                        found = true
                        if m.Architecture != "llama" || m.Quantization != "F32" {
                                t.Fatalf("listed metadata: %+v", m)
                        }
                }
        }
        if !found {
                t.Fatalf("imported model must appear in GET /api/models: %+v", models.Local)
        }

        // Re-import: duplicate, no error.
        resp2, err := http.Post(server.URL+"/api/models/import", "application/json", bytes.NewReader(body))
        if err != nil {
                t.Fatal(err)
        }
        defer resp2.Body.Close()

        if resp2.StatusCode != http.StatusOK {
                t.Fatalf("duplicate import = %d", resp2.StatusCode)
        }

        var dup modelsImportResponse
        if err := json.NewDecoder(resp2.Body).Decode(&dup); err != nil {
                t.Fatal(err)
        }
        if !dup.Duplicate {
                t.Fatalf("identical re-import must report duplicate: %+v", dup)
        }
}

// TestModelsImportThenSelect: the imported model id immediately drives the
// EXISTING selection state machine — config applied, model resolved, the
// honest "engine starts on first use" posture (auto-start off in tests).
func TestModelsImportThenSelect(t *testing.T) {
        server, cfg := newTestServer(t)
        defer server.Close()

        fixture := realGGUFFixturePath(t)

        body, _ := json.Marshal(map[string]string{"path": fixture})
        resp, err := http.Post(server.URL+"/api/models/import", "application/json", bytes.NewReader(body))
        if err != nil {
                t.Fatal(err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                t.Fatalf("import = %d", resp.StatusCode)
        }

        var result modelsImportResponse
        if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
                t.Fatal(err)
        }

        // The EXISTING selection API, with the imported id.
        selBody, _ := json.Marshal(map[string]string{"model": result.Name})
        selResp, err := http.Post(server.URL+"/api/models/select", "application/json", bytes.NewReader(selBody))
        if err != nil {
                t.Fatal(err)
        }
        defer selResp.Body.Close()

        if selResp.StatusCode != http.StatusOK {
                t.Fatalf("select after import = %d", selResp.StatusCode)
        }

        var sel struct {
                Phase string `json:"phase"`
                Model string `json:"model"`
                Error string `json:"error"`
        }
        if err := json.NewDecoder(selResp.Body).Decode(&sel); err != nil {
                t.Fatal(err)
        }

        if sel.Phase != "ready" {
                t.Fatalf("selection must reach ready (engine starts on first use with auto-start off), got phase=%q error=%q", sel.Phase, sel.Error)
        }
        if sel.Model != "tiny-llama-f32.gguf" {
                t.Fatalf("selected model = %q", sel.Model)
        }

        // The live configuration names the imported model (through the same
        // copy-on-write source the Settings panel sees).
        cfgResp, err := http.Get(server.URL + "/api/config")
        if err != nil {
                t.Fatal(err)
        }
        defer cfgResp.Body.Close()

        var live struct {
                Model string `json:"model"`
        }
        if err := json.NewDecoder(cfgResp.Body).Decode(&live); err != nil {
                t.Fatal(err)
        }
        if filepath.Base(live.Model) != "tiny-llama-f32.gguf" {
                t.Fatalf("live config.Model must name the imported model, got %q", live.Model)
        }

        _ = cfg
}

// TestModelsImportRejectsInvalid: corrupt payloads get an actionable 400.
func TestModelsImportRejectsInvalid(t *testing.T) {
        server, _ := newTestServer(t)
        defer server.Close()

        dir := t.TempDir()
        fake := filepath.Join(dir, "fake.gguf")
        _ = os.WriteFile(fake, []byte("not a gguf at all"), 0o644)

        body, _ := json.Marshal(map[string]string{"path": fake})
        resp, err := http.Post(server.URL+"/api/models/import", "application/json", bytes.NewReader(body))
        if err != nil {
                t.Fatal(err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusBadRequest {
                t.Fatalf("corrupt gguf import must be 400, got %d", resp.StatusCode)
        }

        var out map[string]any
        if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
                t.Fatal(err)
        }
        if msg, _ := out["error"].(string); msg == "" || !strings.Contains(msg, "GGUF validation") {
                t.Fatalf("rejection must be actionable, got %v", out)
        }
}
