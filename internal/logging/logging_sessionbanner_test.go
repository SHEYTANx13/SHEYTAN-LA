package logging

// logging_sessionbanner_test.go — v1.2.2: every process start must write
// ONE unambiguous session separator into app.log and the viewer ring, so
// historical entries (e.g. a stale v0.8.0 startup) are clearly separated
// from the CURRENT runtime session.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionBannerMarksCurrentSession(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Open handles must not outlive the test (Windows RemoveAll blocks
	// on open file handles — see TestRecentParsedRedactsSecrets).
	t.Cleanup(func() { _ = m.Close() })

	m.SessionBanner("SHEYTAN-LA", "1.2.2")
	m.Info("boot", "starting")

	entries := m.RecentParsed(10)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	banner := entries[0]
	if banner.Category != "session" {
		t.Fatalf("banner category = %q, want session", banner.Category)
	}
	if !strings.Contains(banner.Message, "SHEYTAN-LA") ||
		!strings.Contains(banner.Message, "v1.2.2") ||
		!strings.Contains(banner.Message, "session start") {
		t.Fatalf("banner message missing markers: %q", banner.Message)
	}
	if banner.Time == "" {
		t.Fatalf("banner line missing its timestamp: %+v", banner)
	}

	// The banner survives the tolerant parser intact (raw preserved).
	if !strings.Contains(banner.Raw, "====") {
		t.Fatalf("banner separator lost in raw line: %q", banner.Raw)
	}
}

// The banner must be a no-op (never panic) on the disabled manager —
// processes that fail to open their log dir still boot.
func TestSessionBannerSafeOnNoopManager(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SessionBanner panicked on noop manager: %v", r)
		}
	}()

	noop.SessionBanner("SHEYTAN-LA", "1.2.2")
}
