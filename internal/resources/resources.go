// Package resources powers SHEYTAN's Resources view (v1.0.6): a professional
// accounting of where the app's disk goes, what the engine process consumes,
// and safe one-click cleanup actions that honor the user's quotas.
package resources

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FolderUsage is one row of the storage breakdown.
type FolderUsage struct {
	Name  string // display name ("Models", "Sessions", ...)
	Path  string
	Bytes int64
}

// DirSize walks dir and returns its total size in bytes (missing dirs = 0).
func DirSize(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// Scan returns the storage breakdown of the standard folders under the app
// root (largest first), plus the root itself. Only top-level folders are
// reported by their display names; unknown folders fall under "(other)".
func Scan(root string) []FolderUsage {
	names := map[string]string{
		"models":          "Models",
		"sessions":        "Sessions",
		"logs":            "Logs",
		"charts":          "Charts",
		"workspace":       "Workspace",
		"browser-profile": "Browser profile",
		"sandbox":         "Sandbox",
		"bin":             "Engine (bin)",
		"recall":          "Memory (recall)",
	}
	known := map[string]int64{}
	var other int64
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() {
			if display, ok := names[e.Name()]; ok {
				known[display] = DirSize(p)
			} else if e.Name() != "screenshots" { // logs/screenshots is under Logs
				other += DirSize(p)
			}
			continue
		}
		// top-level files (config.json, memory.jsonl, exe, ...)
		if fi, err := e.Info(); err == nil {
			other += fi.Size()
		}
	}
	var out []FolderUsage
	for name, b := range known {
		if b > 0 {
			out = append(out, FolderUsage{Name: name, Path: filepath.Join(root, strings.ToLower(name)), Bytes: b})
		}
	}
	if other > 0 {
		out = append(out, FolderUsage{Name: "App files", Path: root, Bytes: other})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

// HumanBytes renders a byte count for the UI ("1.4 GB", "512 KB").
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// --- live process memory ------------------------------------------------------

// ProcRAM returns the resident memory (working set) of a process in bytes.
// Windows: psapi GetProcessMemoryInfo via a limited-information handle;
// other platforms: /proc/<pid>/status VmRSS. Returns an error when the
// process is gone (callers show "—").
func ProcRAM(pid int) (int64, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid %d", pid)
	}
	return procRAMImpl(pid)
}

// --- cleanup actions ------------------------------------------------------------

// TrimSessions deletes the OLDEST sessions beyond the newest `keep`. The
// `list` callback must return session IDs NEWEST-FIRST (that is the store's
// natural order); `remove` deletes one session (use the store's Delete so the
// meta-index stays consistent). Returns the number of removed sessions.
// keep <= 0 keeps everything.
func TrimSessions(keep int, list func() ([]string, error), remove func(id string) error) int {
	if keep <= 0 {
		return 0
	}
	ids, err := list()
	if err != nil || len(ids) <= keep {
		return 0
	}
	victims := ids[keep:] // everything after the newest `keep`
	removed := 0
	for _, id := range victims {
		if err := remove(id); err == nil {
			removed++
		}
	}
	return removed
}

// TrimLogs shrinks the log folder down to maxMB by rotating the tail of each
// oversized file (the newest lines survive — exactly what a debugging user
// wants to keep). Returns the number of bytes freed.
func TrimLogs(logsDir string, maxMB int64) int64 {
	if maxMB <= 0 {
		return 0
	}
	budget := maxMB << 20
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return 0
	}
	type logFile struct {
		path string
		size int64
	}
	var files []logFile
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, logFile{path: filepath.Join(logsDir, e.Name()), size: fi.Size()})
		total += fi.Size()
	}
	if total <= budget {
		return 0
	}
	// Biggest first — that's where the bytes are.
	sort.Slice(files, func(i, j int) bool { return files[i].size > files[j].size })
	var freed int64
	share := budget / int64(len(files)+1)
	for _, f := range files {
		if total <= budget {
			break
		}
		if f.size <= share {
			continue
		}
		keep := share
		if cut, err := rotateTail(f.path, keep); err == nil {
			freed += cut
			total -= cut
		}
	}
	return freed
}

// rotateTail keeps only the last `keep` bytes of a file, returning how many
// bytes were dropped.
func rotateTail(path string, keep int64) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if fi.Size() <= keep {
		return 0, nil
	}
	// Read the tail and CLOSE the source before any rename. On Windows
	// the old v1.0.10 code held `f` open across os.Rename(tmp, path):
	// Windows refuses to replace a file that still has an open handle,
	// the rename failed, TrimLogs silently ate the error and no bytes
	// were ever freed (the exact CI failure on windows-latest).
	tail, err := readTail(path, keep)
	if err != nil {
		return 0, err
	}
	tmp := path + ".rot"
	if err := os.WriteFile(tmp, tail, 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return fi.Size() - keep, nil
}

// readTail returns the last `keep` bytes of `path`. The file is fully closed
// before the function returns, so a caller may replace the file immediately.
func readTail(path string, keep int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(-keep, io.SeekEnd); err != nil {
		return nil, err
	}
	tail := make([]byte, keep)
	if _, err := io.ReadFull(f, tail); err != nil {
		return nil, err
	}
	return tail, nil
}

// ClearDir removes every child of dir (the folder itself survives). Returns
// how many top-level items were removed.
func ClearDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		var err error
		if e.IsDir() {
			err = os.RemoveAll(p)
		} else {
			err = os.Remove(p)
		}
		if err == nil {
			removed++
		}
	}
	return removed, nil
}
