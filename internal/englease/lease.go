// Package englease implements the CROSS-PROCESS engine ownership and
// provisioning locks (v1.3.6, spec §16).
//
// Executable-path equality is NOT ownership: two SHEYTAN instances can
// execute the same binary. One engine installation therefore has exactly
// one managing owner at a time, proven through a lease file whose holder
// record is verified against the live OS before it is trusted:
//
//	holder PID alive (signal 0 / OpenProcess)
//	+ holder executable path (QueryFullProcessImageName / /proc/<pid>/exe)
//	+ holder process start time (GetProcessTimes / proc stat starttime)
//
// Rules implemented here (spec §16):
//
//   - one installation cannot have two independent engine owners;
//   - stale ownership (dead holder, or a recycled PID now backing a
//     different executable) is recovered ONLY after that verification
//     fails;
//   - a LIVE foreign holder is never killed, only reported;
//   - re-entrant acquisition inside the owning process is supported via
//     a refcount (lifecycle ops in one process already serialize on the
//     lifecycle lock).
//
// The lease is advisory against malicious tampering — it protects
// against accidental double management (two app instances, CLI updater
// racing a live app), which is the failure class that produced mixed
// engine packages and 0xC0000139 loader states.
package englease

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// Well-known lease names. Both live under <DataDir>/run/.
const (
	OwnerLease   = "engine-owner.json" // the ONE manager of the installation
	InstallLease = "install.lock"      // cross-process provisioning mutex
)

// ErrHeldByLiveOwner is returned when the lease is held by a verified,
// still-running foreign process. Callers must NOT kill that process.
var ErrHeldByLiveOwner = errors.New("engine lease held by another live process")

// Record is the persisted holder identity.
type Record struct {
	PID        int    `json:"pid"`
	Exe        string `json:"exe"`
	StartTime  string `json:"startTime"` // RFC3339Nano, empty when unprovable
	Hostname   string `json:"hostname"`  // guard against shared-dir races
	Purpose    string `json:"purpose"`   // "engine-owner" | "engine-install"
	Token      string `json:"token"`     // per-acquisition reentrancy token
	AcquiredAt string `json:"acquiredAt"`
}

// handle is one held refcount unit of a lease inside this process.
type handle struct {
	path string
}

var (
	mu       sync.Mutex
	refcount = map[string]int{} // lease path → units held by this process
)

// Acquire takes the named lease inside dir, creating dir when needed.
//
//   - re-entrant for the same process (refcounted);
//   - refuses with ErrHeldByLiveOwner while a verified live foreign
//     holder exists;
//   - steals the file only when verification proves the holder dead or
//     the PID recycled for a different executable.
func Acquire(dir, name, purpose string) (*handle, error) {
	path := filepath.Join(dir, name)

	mu.Lock()
	defer mu.Unlock()

	if refcount[path] > 0 {
		refcount[path]++
		return &handle{path: path}, nil
	}

	if err := acquireOnDisk(path, name, purpose); err != nil {
		return nil, err
	}

	refcount[path]++

	return &handle{path: path}, nil
}

// acquireOnDisk does the create-or-verify-or-steal file transaction.
func acquireOnDisk(path, name, purpose string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	for attempt := 0; attempt < 3; attempt++ {
		rec := newRecord(purpose)

		data, merr := json.MarshalIndent(rec, "", "  ")
		if merr != nil {
			return merr
		}

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, werr := f.Write(data)
			cerr := f.Close()

			if werr != nil || cerr != nil {
				_ = os.Remove(path)
				return fmt.Errorf("write lease %s: %w", name, errors.Join(werr, cerr))
			}

			return nil
		}

		if !errors.Is(err, os.ErrExist) {
			return err
		}

		// The file exists — verify the recorded holder.
		holder, rerr := readRecord(path)
		if rerr != nil {
			// Unreadable/corrupt lease: the holder identity is
			// unprovable either way — recover the file.
			_ = os.Remove(path)
			continue
		}

		if verr := verifyHolder(holder); verr == nil {
			return fmt.Errorf(
				"%w: %s is owned by live process pid %d (%s) — close that instance first; it will not be killed automatically",
				ErrHeldByLiveOwner, name, holder.PID, holder.Purpose,
			)
		}

		// Verification failed — the lease is stale; recover it.
		_ = os.Remove(path)
	}

	return fmt.Errorf("lease %s could not be acquired after repeated contention", name)
}

// verifyHolder proves the recorded holder is still the same live process.
// nil means "verified live owner" (acquire must be refused); non-nil
// means "stale, recoverable".
func verifyHolder(rec Record) error {
	if rec.PID <= 0 {
		return errors.New("no holder pid recorded")
	}

	if rec.Hostname != "" {
		if self, err := os.Hostname(); err == nil && !strings.EqualFold(self, rec.Hostname) {
			return fmt.Errorf("holder ran on another host (%s)", rec.Hostname)
		}
	}

	if rec.PID == os.Getpid() {
		// Our own PID backs the record. Either this process lost its
		// in-memory refcount (not supported — refuse to double-manage
		// within one process), or the PID was recycled after a previous
		// process with the same pid exited. Distinguish via start time.
		if rec.StartTime != "" {
			if t, err := time.Parse(time.RFC3339Nano, rec.StartTime); err == nil {
				if proc.SameStartIdentity(rec.PID, t) {
					return errors.New("self-pid lease without in-process refcount")
				}
			}
		}

		return errors.New("stale self-pid lease (pid recycled)")
	}

	if !proc.ProcessAlive(rec.PID) {
		return fmt.Errorf("holder pid %d is not alive", rec.PID)
	}

	// The PID answers — bind it to the recorded identity before trusting.
	if rec.Exe != "" {
		exe, err := proc.ProcessExePath(rec.PID)
		if err != nil {
			return fmt.Errorf("holder pid %d identity unprovable: %w", rec.PID, err)
		}

		if !proc.SameExecutable(exe, rec.Exe) {
			return fmt.Errorf("pid %d was recycled: now %s, lease recorded %s", rec.PID, exe, rec.Exe)
		}
	}

	if rec.StartTime != "" {
		if t, err := time.Parse(time.RFC3339Nano, rec.StartTime); err == nil {
			if !proc.SameStartIdentity(rec.PID, t) {
				return errors.New("pid start time mismatch — pid recycled")
			}
		}
	}

	// Verified: a live process with the recorded identity holds the lease.
	return nil
}

func newRecord(purpose string) Record {
	exe, _ := os.Executable()
	host, _ := os.Hostname()
	start := ""

	if t, err := proc.ProcessStartTime(os.Getpid()); err == nil {
		start = t.Format(time.RFC3339Nano)
	}

	return Record{
		PID:        os.Getpid(),
		Exe:        exe,
		StartTime:  start,
		Hostname:   host,
		Purpose:    purpose,
		Token:      randomToken(),
		AcquiredAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func randomToken() string {
	b := make([]byte, 12)

	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}

	return fmt.Sprintf("%x", b)
}

func readRecord(path string) (Record, error) {
	var rec Record

	data, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}

	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, err
	}

	return rec, nil
}

// Release drops one refcount; the file is removed on the last release.
func (h *handle) Release() {
	if h == nil {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	if refcount[h.path] <= 0 {
		return
	}

	refcount[h.path]--

	if refcount[h.path] == 0 {
		_ = os.Remove(h.path)
	}
}

// OwnedHere reports whether THIS process currently holds the named
// lease inside dir.
func OwnedHere(dir, name string) bool {
	mu.Lock()
	defer mu.Unlock()

	return refcount[filepath.Join(dir, name)] > 0
}

// ForeignHolder describes a live foreign lease holder, when one exists.
// Stale files are not reported (they are recoverable, not live owners).
func ForeignHolder(dir, name string) (Record, bool) {
	rec, err := readRecord(filepath.Join(dir, name))
	if err != nil {
		return Record{}, false
	}

	mu.Lock()
	local := refcount[filepath.Join(dir, name)] > 0
	mu.Unlock()

	if local {
		return Record{}, false
	}

	if verifyHolder(rec) != nil {
		return Record{}, false
	}

	return rec, true
}
