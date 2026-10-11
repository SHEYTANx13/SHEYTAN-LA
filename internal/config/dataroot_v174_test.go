package config

// v1.7.4 (P0 #3, H3): installed.json belongs to the app-root fold.
//
// The pre-<AppRoot>\data fold (MigrateAppRootDirectData) moves bin/ — with
// the engine-install.json manifest inside it — but used to LEAVE
// installed.json behind, splitting the engine identity across roots: the
// canonical root carried the binary, the stale root carried the tag. The
// entry list now includes installed.json as a regular data file.

import (
        "os"
        "path/filepath"
        "strings"
        "testing"
)

func TestMigrateAppRootDirectDataFoldsInstalledJSON(t *testing.T) {
        appRoot := t.TempDir()
        canonical := filepath.Join(appRoot, "data")

        if err := os.MkdirAll(canonical, 0o755); err != nil {
                t.Fatal(err)
        }

        writeT(t, filepath.Join(appRoot, "installed.json"),
                `{"appVersion":"1.3.6","components":{"llamaServer":{"status":"installed","meta":{"engineTag":"b11223"}}}}`)

        report := &MigrationReport{}

        if err := migrateAppRootEntries(appRoot, canonical, report); err != nil {
                t.Fatalf("migration: %v", err)
        }

        data, err := os.ReadFile(filepath.Join(canonical, "installed.json"))
        if err != nil {
                t.Fatalf("installed.json must fold into the canonical root: %v", err)
        }

        if !strings.Contains(string(data), "b11223") {
                t.Fatalf("folded installed.json must keep its content, got %s", data)
        }

        if _, err := os.Stat(filepath.Join(appRoot, "installed.json")); !os.IsNotExist(err) {
                t.Fatalf("the stray installed.json must be gone from the app root (err=%v)", err)
        }
}

func TestMigrateAppRootDirectDataKeepsCanonicalInstalledJSONAuthoritative(t *testing.T) {
        appRoot := t.TempDir()
        canonical := filepath.Join(appRoot, "data")

        if err := os.MkdirAll(canonical, 0o755); err != nil {
                t.Fatal(err)
        }

        // The canonical identity is NEWER — it stays authoritative and the
        // stray is left untouched (the standard collision rule, unchanged).
        canonicalState := `{"appVersion":"1.7.4","components":{"llamaServer":{"status":"installed","meta":{"engineTag":"b10642"}}}}`
        writeT(t, filepath.Join(canonical, "installed.json"), canonicalState)

        writeT(t, filepath.Join(appRoot, "installed.json"),
                `{"appVersion":"1.3.6","components":{"llamaServer":{"status":"installed","meta":{"engineTag":"b10642"}}}}`)

        report := &MigrationReport{}

        if err := migrateAppRootEntries(appRoot, canonical, report); err != nil {
                t.Fatalf("migration: %v", err)
        }

        if len(report.Collisions) == 0 {
                t.Fatal("the canonical-vs-stray installed.json collision must be reported")
        }

        data, err := os.ReadFile(filepath.Join(canonical, "installed.json"))
        if err != nil {
                t.Fatal(err)
        }

        if string(data) != canonicalState {
                t.Fatal("the canonical installed.json must remain untouched on collision")
        }

        if _, err := os.Stat(filepath.Join(appRoot, "installed.json")); err != nil {
                t.Fatalf("the stray must be LEFT IN PLACE on collision: %v", err)
        }
}
