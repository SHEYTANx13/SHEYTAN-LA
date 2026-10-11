package llm

// importlock.go — v1.6.2: the per-models-directory import lock.
//
// THE DEFECT (reproduced with -race in the v1.6.2 session): ImportModel
// chose its destination with
//
//      Stat(dest) → choose destination → … copy … → Rename(tmp, dest)
//
// and nothing serialized that window. Two concurrent imports of the
// same filename both observed "no existing file", both streamed into
// their own temp file, and both renamed onto the SAME destination — on
// Unix rename(2) atomically REPLACES the target, so the second import
// silently destroyed the first's completed model (verified: the final
// file's tail bytes belonged to whichever import renamed last). Two
// concurrent imports of IDENTICAL bytes reported zero duplicates and
// also collapsed onto one file.
//
// THE FIX: one exclusive per-models-directory lock, held across the
// whole destination choice AND copy, so:
//
//   - same filename, different bytes  → the second importer sees the
//     first's COMPLETED file and takes a collision-free "-1" name;
//   - same filename, same bytes       → the second importer sees the
//     completed identical file and reports ONE duplicate result;
//   - neither import can overwrite the other's completed file;
//   - a failed/canceled import still cleans its own .tmp and releases
//     the lock (defer).
//
// The lock is TWO layers, both crash-safe:
//
//   1. an in-process mutex keyed by the cleaned absolute directory path
//      (no flock semantics needed, no edge cases on exotic mounts);
//   2. a cross-process advisory file lock on <modelsDir>/.import.lock —
//      flock(2) on Unix, LockFileEx on Windows — both released by the
//      kernel when the owning process dies, so a crashed importer never
//      wedges the directory.
//
// The lock file is a dotfile without the .gguf extension: it is invisible
// to the model picker and to import's own collision scans. Import
// remains streaming and atomic — nothing about the copy changes.
import (
        "errors"
        "fmt"
        "os"
        "path/filepath"
        "sync"
        "time"
)

// importLockFileName is the advisory lock file inside the models dir.
const importLockFileName = ".import.lock"

// importLockAcquireTimeout bounds how long a second import waits for a
// concurrent one. Generous on purpose: the lock is held for the WHOLE
// streaming copy (a 20 GB model at ~200 MB/s is ~100 s, plus the
// duplicate SHA-256 pass), and the alternative — failing fast — would
// turn "import two models at once" into an error for no good reason.
// It is a var (not a const) so tests can shrink it.
var importLockAcquireTimeout = 15 * time.Minute

// importMutexes is the in-process lock table (one mutex per models dir).
var importMutexes sync.Map // string -> *sync.Mutex

// lockModelsDir acquires the exclusive per-models-directory import lock
// (in-process mutex + cross-process advisory file lock). The returned
// release function is idempotent and must be called exactly once,
// typically via defer. A lock that cannot be acquired within the
// bounded timeout fails with an actionable error — the import is
// REFUSED rather than run un-serialized (the un-serialized path is
// exactly the race being fixed).
func lockModelsDir(modelsDir string) (func(), error) {
        abs, absErr := filepath.Abs(modelsDir)
        if absErr != nil {
                return nil, fmt.Errorf("import: resolve models dir: %w", absErr)
        }
        key := filepath.Clean(abs)

        // Layer 1: in-process mutex. Held until release().
        muAny, _ := importMutexes.LoadOrStore(key, &sync.Mutex{})
        mu := muAny.(*sync.Mutex)

        if !mu.TryLock() {
                // Another goroutine in THIS process holds it. Bounded wait so a
                // wedged in-process import cannot deadlock the API forever.
                deadline := time.Now().Add(importLockAcquireTimeout)
                for !mu.TryLock() {
                        if time.Now().After(deadline) {
                                return nil, fmt.Errorf(
                                        "import: another import into %s is still in progress — retry when it completes",
                                        key,
                                )
                        }
                        time.Sleep(25 * time.Millisecond)
                }
        }

        // Layer 2: cross-process advisory file lock. The mutex guarantees a
        // single in-process contender for the file lock at any time.
        var f *os.File

        release := func() {
                if f != nil {
                        _ = f.Close() // closing the fd drops the flock / LockFileEx range
                        f = nil
                }
                mu.Unlock()
        }

        if err := os.MkdirAll(key, 0o755); err != nil {
                release()
                return nil, fmt.Errorf("import: models dir: %w", err)
        }

        lockFile, openErr := os.OpenFile(filepath.Join(key, importLockFileName), os.O_CREATE|os.O_RDWR, 0o644)
        if openErr != nil {
                release()
                return nil, fmt.Errorf("import: open import lock: %w", openErr)
        }
        f = lockFile

        if err := lockImportFile(f); err != nil {
                _ = f.Close()
                f = nil
                release()

                if errors.Is(err, errImportLockTimeout) {
                        return nil, fmt.Errorf(
                                "import: another process is importing into %s — retry when it completes (%w)",
                                key, err,
                        )
                }
                return nil, fmt.Errorf("import: acquire import lock: %w", err)
        }

        return release, nil
}
