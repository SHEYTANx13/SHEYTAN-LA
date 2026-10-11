package config

// v1.3.7 data-root regression: the historical <AppRoot>\logs directory
// must fold into the canonical <AppRoot>\data\logs EVEN THOUGH the boot
// order guarantees the canonical logs directory already exists — the log
// catcher (cmd/root.go) starts before MigrateAppRootDirectData and
// creates it. Under the v1.3.6 collision rule ("destination exists →
// leave the stray untouched") the historical logs were never folded and
// stayed as permanent cruft in the application root.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppRootLogsFoldDespitePreCreatedCanonicalLogs(t *testing.T) {
	appRoot := t.TempDir()
	canonical := filepath.Join(appRoot, "data")

	// The CANONICAL logs directory already exists — exactly what the
	// boot-order (log catcher first) produces — holding the LIVE log.
	mkdirT(t, filepath.Join(canonical, "logs"))
	writeT(t, filepath.Join(canonical, "logs", "app.log"), "LIVE LOG — THIS BOOT")

	// The STRAY application-root logs directory with historical files:
	// rotated logs, the crash-report subdirectory, and a same-named
	// app.log from an older boot (name collision → canonical wins).
	mkdirT(t, filepath.Join(appRoot, "logs"))
	writeT(t, filepath.Join(appRoot, "logs", "app.log"), "HISTORICAL app.log — MUST NOT REPLACE THE LIVE ONE")
	writeT(t, filepath.Join(appRoot, "logs", "app.log.2025-01-04"), "historical rotated log")
	writeT(t, filepath.Join(appRoot, "logs", "tools.jsonl"), "historical tool telemetry")
	mkdirT(t, filepath.Join(appRoot, "logs", "crashes"))
	writeT(t, filepath.Join(appRoot, "logs", "crashes", "crash-001.json"), "historical crash report")

	report := &MigrationReport{}

	if err := migrateAppRootEntries(appRoot, canonical, report); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// Historical files folded with content verified.
	for name, want := range map[string]string{
		"logs/app.log.2025-01-04":     "historical rotated log",
		"logs/tools.jsonl":            "historical tool telemetry",
		"logs/crashes/crash-001.json": "historical crash report",
	} {
		b, err := os.ReadFile(filepath.Join(canonical, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s must be folded into the canonical logs dir: %v", name, err)
		}

		if string(b) != want {
			t.Fatalf("%s folded with wrong content: %q", name, string(b))
		}
	}

	// The LIVE log is authoritative — never overwritten.
	if b, _ := os.ReadFile(filepath.Join(canonical, "logs", "app.log")); string(b) != "LIVE LOG — THIS BOOT" {
		t.Fatalf("the live canonical app.log must never be replaced: %q", string(b))
	}

	// The stray logs directory is GONE from the application root.
	if _, err := os.Stat(filepath.Join(appRoot, "logs")); !os.IsNotExist(err) {
		t.Fatalf("the stray logs directory must be removed after the verified fold (err=%v)", err)
	}

	// Idempotent: a second run is a no-op (no error, no duplicate notes).
	report2 := &MigrationReport{}

	if err := migrateAppRootEntries(appRoot, canonical, report2); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
}

// TestAppRootLogsFoldFailureLeavesStrayForNextStart pins the
// restart-safety of the merge: when a stray file cannot be copied (the
// destination path is unwritable), the stray directory STAYS and the
// next start retries.
func TestAppRootLogsFoldFailureLeavesStrayForNextStart(t *testing.T) {
	appRoot := t.TempDir()
	canonical := filepath.Join(appRoot, "data")

	mkdirT(t, filepath.Join(canonical, "logs"))
	mkdirT(t, filepath.Join(appRoot, "logs"))

	// A FILE blocks the destination of a stray DIRECTORY: the walk must
	// fail creating canonical/logs/nested (a file already owns the
	// name)…
	writeT(t, filepath.Join(canonical, "logs", "nested"), "a file owns the name")
	mkdirT(t, filepath.Join(appRoot, "logs", "nested"))
	// …so the verified fold cannot complete and the stray must survive
	// for the next start.
	writeT(t, filepath.Join(appRoot, "logs", "nested", "keep.txt"), "historical log")
	writeT(t, filepath.Join(appRoot, "logs", "app.log.2025-01-04"), "historical rotated log")

	report := &MigrationReport{}

	if err := migrateAppRootEntries(appRoot, canonical, report); err != nil {
		t.Fatalf("migration: %v", err)
	}

	if _, err := os.Stat(filepath.Join(appRoot, "logs")); err != nil {
		t.Fatalf("a failed fold must LEAVE the stray logs directory for the next start: %v", err)
	}

	// The collision note reports the incomplete merge honestly.
	joined := ""

	for _, c := range report.Collisions {
		joined += c + "\n"
	}

	if !containsFoldNote(joined) {
		t.Fatalf("the report must note the incomplete merge, got: %v", report.Collisions)
	}
}

func containsFoldNote(s string) bool {
	return len(s) > 0 && (indexOf(s, "merge incomplete") >= 0 || indexOf(s, "fold") >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}

	return -1
}
