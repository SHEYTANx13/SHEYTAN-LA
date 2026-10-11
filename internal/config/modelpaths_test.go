package config

// modelpaths_test.go — v1.6.1: model-file path repair across retired
// runtime roots (narrow rule: only retired-root insiders are re-anchored,
// external paths are never touched).

import (
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "testing"
)

// withRetiredRoot simulates the 1.3.5-era AppData root for this machine.
// The legacy root is <LOCALAPPDATA>/SHEYTAN-LA (see legacyAppDataRoot).
func withRetiredRoot(t *testing.T, parent string) string {
        t.Helper()

        t.Setenv("LOCALAPPDATA", parent)

        if runtime.GOOS != "windows" {
                // localAppDataDir() falls back to UserCacheDir/home on non-Windows;
                // point XDG_CACHE_HOME at the parent so the same root is derived.
                t.Setenv("XDG_CACHE_HOME", parent)
        }

        return filepath.Join(parent, AppShortName)
}

func TestRepairRetiredModelPathsReanchors(t *testing.T) {
        dir := t.TempDir()
        t.Setenv("SHEYTAN_DATA_DIR", dir)

        retired := withRetiredRoot(t, dir)

        cfg := Default()
        cfg.DataDir = dir
        cfg.ModelsDir = filepath.Join(dir, "models")
        cfg.Model = filepath.Join(retired, "models", "foo.gguf")
        cfg.DraftModel = filepath.Join(retired, "models", "foo-draft.gguf")
        cfg.VisionMMProj = filepath.Join(retired, "models", "foo-mmproj.f16.gguf")

        notes := RepairRetiredModelPaths(cfg)

        // Model-file fields under the models subtree collapse to base names
        // (portable across further data-root moves).
        if cfg.Model != "foo.gguf" {
                t.Fatalf("model must re-anchor to the base name, got %q", cfg.Model)
        }
        if cfg.DraftModel != "foo-draft.gguf" {
                t.Fatalf("draftModel must re-anchor, got %q", cfg.DraftModel)
        }
        if cfg.VisionMMProj != "foo-mmproj.f16.gguf" {
                t.Fatalf("visionMmproj must re-anchor, got %q", cfg.VisionMMProj)
        }

        if len(notes) != 3 {
                t.Fatalf("one note per repaired field, got %v", notes)
        }
        for _, note := range notes {
                if !strings.Contains(note, "retired runtime root") {
                        t.Fatalf("note must say what happened: %q", note)
                }
        }

        // Idempotent: a second pass changes nothing and reports nothing.
        if again := RepairRetiredModelPaths(cfg); len(again) != 0 {
                t.Fatalf("repair must be idempotent, got %v", again)
        }
}

func TestRepairRetiredModelPathsKeepsSubdirectoryLayout(t *testing.T) {
        dir := t.TempDir()
        t.Setenv("SHEYTAN_DATA_DIR", dir)

        retired := withRetiredRoot(t, dir)

        cfg := Default()
        cfg.DataDir = dir
        cfg.Model = filepath.Join(retired, "models", "subdir", "bar.gguf")

        RepairRetiredModelPaths(cfg)

        // A nested models layout keeps its relative path anchored at DataDir.
        want := filepath.Join(dir, "models", "subdir", "bar.gguf")
        if cfg.Model != want {
                t.Fatalf("nested path must preserve layout: want %q got %q", want, cfg.Model)
        }
}

func TestRepairRetiredModelPathsNeverTouchesExternalPaths(t *testing.T) {
        dir := t.TempDir()
        t.Setenv("SHEYTAN_DATA_DIR", dir)

        _ = withRetiredRoot(t, dir)

        external := "/mnt/bigdrive/models/external.gguf"
        if runtime.GOOS == "windows" {
                external = `D:\models\external.gguf`
        }

        cfg := Default()
        cfg.DataDir = dir
        cfg.Model = external
        cfg.DraftModel = ""
        cfg.VisionMMProj = ""

        if notes := RepairRetiredModelPaths(cfg); len(notes) != 0 {
                t.Fatalf("external paths must never be touched, got %v", notes)
        }
        if cfg.Model != external {
                t.Fatalf("external model path must survive verbatim, got %q", cfg.Model)
        }
}

func TestRepairRetiredModelPathsLeavesPlainNames(t *testing.T) {
        dir := t.TempDir()
        t.Setenv("SHEYTAN_DATA_DIR", dir)

        cfg := Default()
        cfg.DataDir = dir
        cfg.Model = "plain-name.gguf"

        if notes := RepairRetiredModelPaths(cfg); len(notes) != 0 {
                t.Fatalf("plain names are already portable, got %v", notes)
        }
        if cfg.Model != "plain-name.gguf" {
                t.Fatalf("plain name must survive, got %q", cfg.Model)
        }
}

func TestLoadRepairsRetiredModelPaths(t *testing.T) {
        dir := t.TempDir()
        t.Setenv("SHEYTAN_DATA_DIR", dir)

        retired := withRetiredRoot(t, dir)

        // The on-disk config still points inside the retired root.
        path := filepath.Join(dir, "config.json")
        broken := `{
                "model": "` + strings.ReplaceAll(filepath.Join(retired, "models", "legacy.gguf"), `\`, `\\`) + `"
        }`
        if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
                t.Fatal(err)
        }

        cfg, err := Load(path)
        if err != nil {
                t.Fatal(err)
        }

        if cfg.Model != "legacy.gguf" {
                t.Fatalf("Load must repair the retired-root model path to the base name, got %q", cfg.Model)
        }

        found := false
        for _, note := range cfg.PathNotes {
                if strings.Contains(note, "retired runtime root") {
                        found = true
                }
        }
        if !found {
                t.Fatalf("the repair must be reported through PathNotes, got %v", cfg.PathNotes)
        }
}
