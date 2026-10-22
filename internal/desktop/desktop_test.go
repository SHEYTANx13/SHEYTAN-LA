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
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
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

// TestUILogWriterReachesAppLog pins the v1.10.14 warning catcher: the UI
// child's stdout/stderr flow through the adapter into the REAL log
// manager (stdout → Info, stderr → Warn, tagged "ui"), line-buffered,
// CRLF-tolerant, with the final unterminated partial line flushed at
// child exit. Run #36's wiring pointed these streams at the core's
// os.Stdout/os.Stderr — lost entirely under -H=windowsgui, which is how
// every Qt host warning vanished from the packaged runtime log.
func TestUILogWriterReachesAppLog(t *testing.T) {
	dir := t.TempDir()
	mgr, err := logging.New(dir)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	defer mgr.Close()
	logging.SetDefault(mgr)
	t.Cleanup(func() { logging.SetDefault(nil) })

	stdout := newUILogWriter(false)
	stderr := newUILogWriter(true)

	n, err := stdout.Write([]byte("QML Main.qml:42: expected token\r\npartial-line-no-newline"))
	if err != nil || n == 0 {
		t.Fatalf("stdout write: n=%d err=%v", n, err)
	}
	if _, err := stderr.Write([]byte("QWARN : qmlLoadsCleanly() something")); err != nil {
		t.Fatalf("stderr write: %v", err)
	}
	stdout.Flush()
	stderr.Flush()

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("app.log missing: %v", err)
	}
	log := string(data)
	for _, want := range []string{"[ui] ", "QML Main.qml:42", "partial-line-no-newline", "QWARN"} {
		if !strings.Contains(log, want) {
			t.Fatalf("app.log missing %q after flush; log:\n%s", want, log)
		}
	}
	if !strings.Contains(log, "WARN") || !strings.Contains(log, "INFO") {
		t.Fatalf("stream severity lost: stdout must land INFO, stderr WARN; log:\n%s", log)
	}
}

// ---------------------------------------------------------------------------
// v1.10.14-fixed run-phase catcher (D8): a UI child that dies MID-SESSION
// becomes a named ERROR line in app.log and a user-visible box — never an
// invisible exit. At the run-#37 tree the childDone arm recorded the exit
// code and returned silently; these tests fail to COMPILE against that
// tree (undefined reportUIExit / uiExitReason), which is their RED.
// ---------------------------------------------------------------------------

// uiExitTestChild runs a real child process that exits with the given
// non-zero code and returns the resulting error — the exact shape the
// childDone arm receives from cmd.Wait when the Qt host dies mid-session.
// Windows and POSIX each get a native one-liner (the portability gate
// bans POSIX-only shell fixtures from Go tests).
func uiExitTestChild(t *testing.T, code int) error {
	t.Helper()
	var err error
	if runtime.GOOS == "windows" {
		err = exec.Command("cmd", "/c", "exit", itoa(code)).Run()
	} else {
		err = exec.Command("sh", "-c", "exit "+itoa(code)).Run()
	}
	if err == nil {
		t.Fatalf("fixture child must exit non-zero (%d)", code)
	}
	return err
}

// itoa avoids strconv for one digit-range need with zero formatting
// ambiguity across the two fixture shells.
func itoa(n int) string {
	if n < 0 || n > 9 {
		panic("uiExitTestChild supports single-digit codes only")
	}
	return string(rune('0' + n))
}

// TestUIExitReasonCarriesTheCode pins the reason naming: an ExitError
// becomes "exit code N" (the operator's grep target on the box and in the
// log line); any other wait failure carries its own text. Pure — runs on
// every platform.
func TestUIExitReasonCarriesTheCode(t *testing.T) {
	exitErr := uiExitTestChild(t, 3)
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) {
		t.Fatalf("fixture must yield *exec.ExitError, got %T", exitErr)
	}
	if got := uiExitReason(exitErr); got != "exit code 3" {
		t.Fatalf("uiExitReason = %q, want %q", got, "exit code 3")
	}
	if got := uiExitReason(errors.New("boom")); got != "boom" {
		t.Fatalf("uiExitReason(non-ExitError) = %q, want %q", got, "boom")
	}
}

// TestReportUIExitAbnormalCloseIsVisible pins the catcher's emission: a
// named ERROR line ("ui exit") carrying the child's code lands in app.log.
// Windows skips: platform.MessageBox is a BLOCKING modal there — a test
// invocation would park the hosted runner until someone clicks OK. The
// emission path is identical on every platform (only the box is
// platform-conditional); the box's physical appearance is the operator's
// retest.
func TestReportUIExitAbnormalCloseIsVisible(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("platform.MessageBox is a blocking modal on Windows — the box is the operator's physical retest; the line format is pinned by TestUIExitReasonCarriesTheCode and this test's non-Windows run")
	}
	dir := t.TempDir()
	mgr, err := logging.New(dir)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	defer mgr.Close()
	logging.SetDefault(mgr)
	t.Cleanup(func() { logging.SetDefault(nil) })

	exitErr := uiExitTestChild(t, 7)
	reportUIExit(exitErr)

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("app.log missing: %v", err)
	}
	log := string(data)
	for _, want := range []string{"ERROR", "ui exit", "exit code 7"} {
		if !strings.Contains(log, want) {
			t.Fatalf("app.log missing %q after an abnormal UI exit — the run-phase catcher is blind; log:\n%s", want, log)
		}
	}
}

// TestReportUIExitCleanCloseIsQuiet pins the other half of the catcher: a
// normal user close (exit 0) logs ONE Info line and raises NO error and
// NO box — visibility must not cry wolf on the healthy path. The clean
// path never reaches the platform box, so this runs on every platform.
func TestReportUIExitCleanCloseIsQuiet(t *testing.T) {
	dir := t.TempDir()
	mgr, err := logging.New(dir)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	defer mgr.Close()
	logging.SetDefault(mgr)
	t.Cleanup(func() { logging.SetDefault(nil) })

	reportUIExit(nil)

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("app.log missing: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "ui exit: clean close") || !strings.Contains(log, "INFO") {
		t.Fatalf("a clean UI close must log the ONE Info line; log:\n%s", log)
	}
	if strings.Contains(log, "ERROR") {
		t.Fatalf("a clean UI close must NOT log an ERROR line; log:\n%s", log)
	}
}
