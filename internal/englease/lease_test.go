package englease

// Regression coverage for spec §16 (engine ownership must be exclusive)
// and §31 (duplicate engine ownership prevented; stale ownership
// recoverable only after process verification).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// TestMain doubles as the helper-process host used to obtain a REAL
// live foreign process for the ownership tests.
func TestMain(m *testing.M) {
	switch os.Getenv("SHEYTAN_LEASE_HELPER") {
	case "sleep":
		time.Sleep(120 * time.Second)
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// spawnSleeper starts a real child process (the test binary itself in
// helper mode) and returns its PID plus a cleanup func.
func spawnSleeper(t *testing.T) (int, func()) {
	t.Helper()

	cmd := proc.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "SHEYTAN_LEASE_HELPER=sleep")
	if cmd.Start() != nil {
		t.Skip("cannot spawn helper process on this platform")
	}

	kill := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}

	t.Cleanup(kill)

	return cmd.Process.Pid, kill
}

func writeRecord(t *testing.T, dir, name string, rec Record) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	data, _ := json.MarshalIndent(rec, "", "  ")

	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireReleaseRoundTrip(t *testing.T) {
	dir := t.TempDir()

	h, err := Acquire(dir, OwnerLease, "engine-owner")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Re-entrant for the same process.
	h2, err := Acquire(dir, OwnerLease, "engine-owner")
	if err != nil {
		t.Fatalf("reentrant acquire: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, OwnerLease)); err != nil {
		t.Fatalf("lease file missing: %v", err)
	}

	h2.Release()
	h.Release()

	if _, err := os.Stat(filepath.Join(dir, OwnerLease)); !os.IsNotExist(err) {
		t.Fatalf("lease file must be removed on final release (err=%v)", err)
	}
}

func TestAcquireRefusesVerifiedLiveForeignHolder(t *testing.T) {
	dir := t.TempDir()

	pid, kill := spawnSleeper(t)

	start := ""
	if st, err := proc.ProcessStartTime(pid); err == nil {
		start = st.Format(time.RFC3339Nano)
	}

	exe, _ := os.Executable()

	writeRecord(t, dir, OwnerLease, Record{
		PID:       pid,
		Exe:       exe,
		StartTime: start,
		Hostname:  mustHostname(),
		Purpose:   "engine-owner",
	})

	if _, err := Acquire(dir, OwnerLease, "engine-owner"); err == nil {
		kill()
		t.Fatal("acquire must refuse while a verified live foreign holder exists")
	}

	// The foreign process must NOT have been killed by the refusal.
	if !proc.ProcessAlive(pid) {
		t.Fatal("refused lease must never kill the live foreign holder (spec §16)")
	}

	// Once the holder is gone the lease becomes recoverable.
	kill()

	deadline := time.Now().Add(5 * time.Second)
	for proc.ProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	h, err := Acquire(dir, OwnerLease, "engine-owner")
	if err != nil {
		t.Fatalf("acquire after holder death must succeed (recovery): %v", err)
	}

	h.Release()
}

func TestAcquireRecoversDeadHolder(t *testing.T) {
	dir := t.TempDir()

	pid, kill := spawnSleeper(t)
	kill()

	deadline := time.Now().Add(5 * time.Second)
	for proc.ProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	writeRecord(t, dir, OwnerLease, Record{
		PID:      pid,
		Exe:      mustSelfExe(),
		Hostname: mustHostname(),
		Purpose:  "engine-owner",
	})

	h, err := Acquire(dir, OwnerLease, "engine-owner")
	if err != nil {
		t.Fatalf("dead holder must be recoverable after verification: %v", err)
	}

	h.Release()
}

func TestAcquireRecyclesPIDForDifferentExecutable(t *testing.T) {
	dir := t.TempDir()

	pid, kill := spawnSleeper(t)
	start := ""

	if st, err := proc.ProcessStartTime(pid); err == nil {
		start = st.Format(time.RFC3339Nano)
	}

	kill()

	deadline := time.Now().Add(5 * time.Second)
	for proc.ProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	// Simulate a recycled PID: same (now dead) pid recorded, but a
	// different executable and start time. Recovery must not require
	// the executable to match because the holder is not alive.
	writeRecord(t, dir, OwnerLease, Record{
		PID:       pid,
		Exe:       `C:\definitely\not-sheytan.exe`,
		StartTime: start,
		Hostname:  mustHostname(),
		Purpose:   "engine-owner",
	})

	h, err := Acquire(dir, OwnerLease, "engine-owner")
	if err != nil {
		t.Fatalf("recycled/dead pid must be recoverable: %v", err)
	}

	h.Release()
}

func TestAcquireSelfPIDLeaseIsRecoverable(t *testing.T) {
	dir := t.TempDir()

	start := ""
	if st, err := proc.ProcessStartTime(os.Getpid()); err == nil {
		start = st.Format(time.RFC3339Nano)
	}

	// A file left by an earlier incarnation of this very PID (or a
	// refcount that no longer exists) logically belongs to THIS live
	// process — re-acquiring it must succeed. Double management within
	// one process is prevented by the refcount map + the lifecycle
	// lock, not by refusing self-owned files.
	writeRecord(t, dir, OwnerLease, Record{
		PID:       os.Getpid(),
		Exe:       mustSelfExe(),
		StartTime: start,
		Hostname:  mustHostname(),
		Purpose:   "engine-owner",
	})

	h, err := Acquire(dir, OwnerLease, "engine-owner")
	if err != nil {
		t.Fatalf("self-owned lease file must be re-acquirable: %v", err)
	}

	h.Release()
}

func TestVerifyHolderStalePIDRecycledForForeignExe(t *testing.T) {
	// Unit-level: a LIVE pid (this test process) recorded with a
	// foreign executable path. The PID is provably recycled — the
	// recorded holder cannot be the live process behind the PID.
	rec := Record{PID: os.Getpid(), Exe: `C:\foreign\other.exe`}

	if err := verifyHolder(rec); err == nil {
		t.Fatal("live pid with foreign exe identity must verify as stale/recycled")
	}
}

func TestForeignHolderIgnoresStaleFiles(t *testing.T) {
	dir := t.TempDir()

	writeRecord(t, dir, OwnerLease, Record{PID: 999999999})

	if _, ok := ForeignHolder(dir, OwnerLease); ok {
		t.Fatal("stale lease must not be reported as a live foreign holder")
	}
}

func mustHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}

	return h
}

func mustSelfExe() string {
	exe, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}

	return exe
}
