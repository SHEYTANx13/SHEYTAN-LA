//go:build !headless

// desktop_test.go — focused unit tests for the launcher's UI-resolution
// contract (v1.10.2). The full desktop lifecycle (core up → IPC bound →
// UI child → clean exit) is proven end-to-end by scripts/
// qt-desktop-smoke.sh and the QtTest acceptance suite; these tests pin
// the resolution logic those harnesses depend on: the "none" sentinel,
// the explicit override, the next-to-core packaged layout, and the hard
// refusal when no UI can be located (a silent headless fallback would
// hide a broken install).
package desktop

import (
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "testing"
)

func TestUIExecutableNoneSentinel(t *testing.T) {
        t.Setenv("SHEYTAN_UI_EXECUTABLE", "none")
        path, err := uiExecutable()
        if err != nil {
                t.Fatalf("none sentinel must be accepted: %v", err)
        }
        if path != "none" {
                t.Fatalf("uiExecutable = %q, want the literal %q", path, "none")
        }
}

func TestUIExecutableExplicitOverride(t *testing.T) {
        dir := t.TempDir()
        alt := filepath.Join(dir, "custom-ui")
        // Plain bytes, not a script fixture: the resolution contract only
        // Stats the file, and the portability gate bans POSIX-only shell
        // fixtures from Go tests (they are not executables on Windows).
        if err := os.WriteFile(alt, []byte("placeholder"), 0o755); err != nil {
                t.Fatalf("write alt: %v", err)
        }

        t.Setenv("SHEYTAN_UI_EXECUTABLE", alt)
        path, err := uiExecutable()
        if err != nil {
                t.Fatalf("override must be honored: %v", err)
        }
        if path != alt {
                t.Fatalf("uiExecutable = %q, want %q", path, alt)
        }
}

// TestUIExecutableOverrideWinsOverNextToCore pins the v1.10.3 (R13)
// resolution order: an EXPLICIT override wins even when a packaged UI sits
// next to the core executable. The v1.10.2 next-to-core-first order
// silently ignored the override in exactly that situation.
func TestUIExecutableOverrideWinsOverNextToCore(t *testing.T) {
        self, err := os.Executable()
        if err != nil {
                t.Skipf("cannot resolve test binary: %v", err)
        }
        name, ok := uiExecutableNames[runtime.GOOS]
        if !ok {
                t.Skipf("no packaged UI name for %s", runtime.GOOS)
        }
        // A packaged UI next to the core (the test binary here).
        placeholder := filepath.Join(filepath.Dir(self), name)
        if err := os.WriteFile(placeholder, []byte("placeholder"), 0o755); err != nil {
                t.Skipf("test binary dir is not writable: %v", err)
        }
        t.Cleanup(func() { _ = os.Remove(placeholder) })

        // AND an explicit override pointing somewhere else: the override
        // must win — never the next-to-core copy.
        dir := t.TempDir()
        alt := filepath.Join(dir, "override-ui")
        if err := os.WriteFile(alt, []byte("placeholder"), 0o755); err != nil {
                t.Fatalf("write alt: %v", err)
        }
        t.Setenv("SHEYTAN_UI_EXECUTABLE", alt)

        path, err := uiExecutable()
        if err != nil {
                t.Fatalf("override must be honored: %v", err)
        }
        if path != alt {
                t.Fatalf("uiExecutable = %q, want the override %q (next-to-core must lose)", path, alt)
        }
}

func TestUIExecutableOverrideMissingIsRefused(t *testing.T) {
        t.Setenv("SHEYTAN_UI_EXECUTABLE", filepath.Join(t.TempDir(), "does-not-exist"))
        _, err := uiExecutable()
        if err == nil {
                t.Fatal("a missing override must be refused, not silently ignored")
        }
        if !strings.Contains(err.Error(), "does not exist") {
                t.Fatalf("refusal must name the problem, got: %v", err)
        }
}

func TestUIExecutableNextToCoreIsResolved(t *testing.T) {
        // The packaged layout: sheytan-ui sits next to the core executable.
        // In tests os.Executable() is the test binary — drop a placeholder
        // UI named for THIS platform next to it and expect resolution.
        self, err := os.Executable()
        if err != nil {
                t.Skipf("cannot resolve test binary: %v", err)
        }
        name, ok := uiExecutableNames[runtime.GOOS]
        if !ok {
                t.Skipf("no packaged UI name for %s", runtime.GOOS)
        }
        placeholder := filepath.Join(filepath.Dir(self), name)
        if err := os.WriteFile(placeholder, []byte("placeholder"), 0o755); err != nil {
                t.Skipf("test binary dir is not writable: %v", err)
        }
        t.Cleanup(func() { _ = os.Remove(placeholder) })

        // v1.10.3 (R13): an EMPTY override means "no override" — one call
        // does both the set and the restore (t.Setenv); the redundant
        // os.Unsetenv pair is gone.
        t.Setenv("SHEYTAN_UI_EXECUTABLE", "")

        path, err := uiExecutable()
        if err != nil {
                t.Fatalf("next-to-core resolution failed: %v", err)
        }
        if path != placeholder {
                t.Fatalf("uiExecutable = %q, want %q", path, placeholder)
        }
}

func TestUIExecutableMissingEverywhereIsAHardRefusal(t *testing.T) {
        // No packaged UI next to the test binary, no override: the launch
        // path must FAIL LOUDLY (the launcher exits non-zero) instead of
        // starting a UI-less desktop silently.
        t.Setenv("SHEYTAN_UI_EXECUTABLE", "")

        _, err := uiExecutable()
        if err == nil {
                t.Fatal("missing UI must be a hard refusal")
        }
        if !strings.Contains(err.Error(), "SHEYTAN_UI_EXECUTABLE") {
                t.Fatalf("refusal must point at the override, got: %v", err)
        }
}
