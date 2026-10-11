// WARN/ERROR records are
// never blank. The v1.2.9 runtime log carried lines like
// "WARN  [updater]" with nothing after them.
package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlankWarnIsUpgradedToActionable(t *testing.T) {
	dir := t.TempDir()
	mgr, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	previous := Default()
	SetDefault(mgr)
	t.Cleanup(func() { SetDefault(previous) })

	// The defect shape: an empty format with no args.
	mgr.Warn("updater", "")
	mgr.Error("engine", "%s", "")

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("read app.log: %v", err)
	}
	log := string(data)

	for _, line := range strings.Split(log, "\n") {
		if !strings.Contains(line, "WARN") && !strings.Contains(line, "ERROR") {
			continue
		}
		idx := strings.LastIndex(line, "]")
		if idx < 0 {
			continue
		}
		if strings.TrimSpace(line[idx+1:]) == "" {
			t.Fatalf("blank WARN/ERROR survived: %q", line)
		}
		if !strings.Contains(line, "no detail provided") {
			t.Fatalf("blank warning was not upgraded with actionable context: %q", line)
		}
	}

	if !strings.Contains(log, "WARN") || !strings.Contains(log, "ERROR") {
		t.Fatalf("expected one WARN and one ERROR line:\n%s", log)
	}
}

func TestNormalWarnPassesThrough(t *testing.T) {
	dir := t.TempDir()
	mgr, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	mgr.Warn("updater", "check failed: network unreachable")

	data, _ := os.ReadFile(filepath.Join(dir, "app.log"))
	log := string(data)

	if !strings.Contains(log, "check failed: network unreachable") {
		t.Fatalf("normal warning lost:\n%s", log)
	}
	if strings.Contains(log, "no detail provided") {
		t.Fatalf("non-empty warning was rewritten:\n%s", log)
	}
}
