package artifacts

// taskmeta_test.go — v1.7.0 task/run artifact contract: create/read/list,
// version replacement, task ownership, path safety, invalid type bounds,
// atomic failure, cleanup, provenance.

import (
        "os"
        "path/filepath"
        "strings"
        "testing"
)

func TestCreateReadListAndProvenance(t *testing.T) {
        r := NewTaskRegistry(t.TempDir())

        meta, err := r.Create(CreateRequest{
                TaskID:   "task-1",
                RunID:    "run-11",
                Source:   "artifact_create",
                Title:    "Findings",
                Filename: "report.md",
                Content:  []byte("# Report\n\nAll systems verified."),
        })
        if err != nil {
                t.Fatal(err)
        }

        if meta.Kind != KindDoc || meta.Version != 1 || meta.Size != int64(len("# Report\n\nAll systems verified.")) {
                t.Fatalf("meta: %+v", meta)
        }
        if meta.TaskID != "task-1" || meta.RunID != "run-11" || meta.Source != "artifact_create" {
                t.Fatalf("provenance: %+v", meta)
        }
        if meta.Hash == "" {
                t.Fatal("content hash missing")
        }

        // Read round-trips the content.
        data, err := r.Read(meta.ID)
        if err != nil {
                t.Fatal(err)
        }
        if !strings.HasPrefix(string(data), "# Report") {
                t.Fatalf("content: %q", data)
        }

        // Markdown IS a first-class doc kind.
        if meta.Kind != KindDoc {
                t.Fatalf("markdown kind = %v", meta.Kind)
        }

        // List by task and by run.
        if got := r.List("task-1"); len(got) != 1 {
                t.Fatalf("list: %+v", got)
        }
        if got := r.ListRun("run-11"); len(got) != 1 {
                t.Fatalf("listRun: %+v", got)
        }
        if got := r.ListRun("run-99"); len(got) != 0 {
                t.Fatalf("cross-run leak: %+v", got)
        }
}

func TestVersionReplacementHistory(t *testing.T) {
        r := NewTaskRegistry(t.TempDir())

        v1, err := r.Create(CreateRequest{TaskID: "t", Source: "artifact_create", Filename: "notes.md", Content: []byte("v1")})
        if err != nil {
                t.Fatal(err)
        }
        v2, err := r.Create(CreateRequest{TaskID: "t", Source: "artifact_create", Filename: "notes.md", Content: []byte("v2 body")})
        if err != nil {
                t.Fatal(err)
        }

        if v1.Version != 1 || v2.Version != 2 {
                t.Fatalf("versions: %d/%d", v1.Version, v2.Version)
        }

        // Both versions remain readable (history preserved).
        d1, err := r.Read(v1.ID)
        if err != nil || string(d1) != "v1" {
                t.Fatalf("v1 read: %q err=%v", d1, err)
        }
        d2, _ := r.Read(v2.ID)
        if string(d2) != "v2 body" {
                t.Fatalf("v2 read: %q", d2)
        }

        history := r.Versions("t", "notes.md")
        if len(history) != 2 || history[0].Version != 1 || history[1].Version != 2 {
                t.Fatalf("history: %+v", history)
        }
}

func TestPathSafetyAndBounds(t *testing.T) {
        r := NewTaskRegistry(t.TempDir())

        cases := []struct {
                name     string
                filename string
                taskID   string
        }{
                {"traversal", "../escape.md", "t"},
                {"deep traversal", "sub/../../../escape.md", "t"},
                {"absolute", "/etc/passwd", "t"},
                {"unsafe chars", "bad name!.md", "t"},
                {"empty", "", "t"},
                {"empty after sanitize", "x.md", ".."},
                {"traversal task id", "x.md", "../../evil"},
        }

        for _, tc := range cases {
                if _, err := r.Create(CreateRequest{TaskID: tc.taskID, Source: "tool", Filename: tc.filename, Content: []byte("x")}); err == nil {
                        t.Errorf("%s: expected rejection", tc.name)
                }
        }

        // Empty content and oversize content.
        if _, err := r.Create(CreateRequest{TaskID: "t", Source: "tool", Filename: "a.md", Content: nil}); err == nil {
                t.Error("empty content must be rejected")
        }
        big := make([]byte, MaxArtifactBytes+1)
        if _, err := r.Create(CreateRequest{TaskID: "t", Source: "tool", Filename: "a.md", Content: big}); err == nil {
                t.Error("oversize content must be rejected")
        }

        // Missing source (provenance is mandatory).
        if _, err := r.Create(CreateRequest{TaskID: "t", Filename: "a.md", Content: []byte("x")}); err == nil {
                t.Error("missing source must be rejected")
        }
}

func TestReloadSurvivesRestart(t *testing.T) {
        dir := t.TempDir()

        r1 := NewTaskRegistry(dir)
        meta, err := r1.Create(CreateRequest{TaskID: "t", RunID: "r1", Source: "artifact_create", Filename: "keep.md", Content: []byte("persist me")})
        if err != nil {
                t.Fatal(err)
        }

        // A fresh process reloads the metadata.
        r2 := NewTaskRegistry(dir)
        if err := r2.Load(); err != nil {
                t.Fatal(err)
        }

        got, ok := r2.Get(meta.ID)
        if !ok || got.RelPath != "keep.md" || got.RunID != "r1" {
                t.Fatalf("registry did not survive restart: %+v ok=%v", got, ok)
        }

        // Sequence numbers continue (no ID collisions after reload).
        m2, err := r2.Create(CreateRequest{TaskID: "t", Source: "artifact_create", Filename: "second.md", Content: []byte("2")})
        if err != nil {
                t.Fatal(err)
        }
        if m2.ID == meta.ID {
                t.Fatal("ID collision after reload")
        }
}

func TestAtomicFailureLeavesNoTempFile(t *testing.T) {
        dir := t.TempDir()
        r := NewTaskRegistry(dir)

        // A valid create is atomic: no .tmp leftovers.
        if _, err := r.Create(CreateRequest{TaskID: "t", Source: "tool", Filename: "ok.md", Content: []byte("ok")}); err != nil {
                t.Fatal(err)
        }

        entries, err := os.ReadDir(filepath.Join(dir, "task-artifacts", "t"))
        if err != nil {
                t.Fatal(err)
        }
        for _, e := range entries {
                if strings.HasSuffix(e.Name(), ".tmp") {
                        t.Fatalf("temp file leaked: %s", e.Name())
                }
        }
}

func TestCleanupTaskAndDelete(t *testing.T) {
        dir := t.TempDir()
        r := NewTaskRegistry(dir)

        a, _ := r.Create(CreateRequest{TaskID: "gone", Source: "tool", Filename: "a.md", Content: []byte("a")})
        b, _ := r.Create(CreateRequest{TaskID: "stays", Source: "tool", Filename: "b.md", Content: []byte("b")})

        if _, err := os.Stat(a.Path); err != nil {
                t.Fatal("artifact file missing")
        }

        if err := r.CleanupTask("gone"); err != nil {
                t.Fatal(err)
        }
        if _, err := os.Stat(a.Path); !os.IsNotExist(err) {
                t.Fatal("cleaned artifact file survived")
        }
        if len(r.List("gone")) != 0 {
                t.Fatal("cleaned metadata survived")
        }
        if len(r.List("stays")) != 1 {
                t.Fatal("unrelated task artifacts must survive")
        }

        if !r.Delete(b.ID) {
                t.Fatal("delete failed")
        }
        if _, err := os.Stat(b.Path); !os.IsNotExist(err) {
                t.Fatal("deleted file survived")
        }
}

func TestKindCoverage(t *testing.T) {
        r := NewTaskRegistry(t.TempDir())

        kinds := map[string]Kind{
                "doc.md":      KindDoc,
                "notes.txt":   KindDoc,
                "main.go":     KindCode,
                "data.json":   KindData,
                "table.csv":   KindData,
                "index.html":  KindCode,
                "chart.svg":   KindChart,
                "photo.png":   KindImage,
                "bundle.zip":  KindArchive,
                "summary.md":  KindDoc,
        }

        for file, want := range kinds {
                m, err := r.Create(CreateRequest{TaskID: "kinds", Source: "tool", Filename: file, Content: []byte("x")})
                if err != nil {
                        t.Fatalf("%s: %v", file, err)
                }
                if m.Kind != want {
                        t.Errorf("%s kind = %v, want %v", file, m.Kind, want)
                }
        }
}
