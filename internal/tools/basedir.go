package tools

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	baseMu  sync.RWMutex
	baseDir string
)

var (
	// ErrPathOutsideBase is returned when an agent path would escape the
	// canonical application/tool directory.
	ErrPathOutsideBase = errors.New("tools: path escapes base directory")

	// ErrBaseDirUnset is returned when a safe path cannot be resolved because
	// the canonical base directory has not been configured.
	ErrBaseDirUnset = errors.New("tools: base directory is not configured")
)

// SetBaseDir sets the canonical base directory that relative paths in ALL
// tools resolve against. Call once during runtime stack construction.
func SetBaseDir(dir string) {
	dir = strings.TrimSpace(dir)

	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = filepath.Clean(abs)
		}
	}

	baseMu.Lock()
	baseDir = dir
	baseMu.Unlock()
}

// BaseDir returns the current canonical base directory.
func BaseDir() string {
	baseMu.RLock()
	defer baseMu.RUnlock()

	return baseDir
}

// ResolvePath resolves p against the canonical base directory.
//
// This compatibility helper preserves the historical string-only API.
// Invalid paths resolve to an empty string. New security-sensitive code
// should use ResolvePathChecked so the caller receives the exact error.
func ResolvePath(p string) string {
	resolved, err := ResolvePathChecked(p)
	if err != nil {
		return ""
	}

	return resolved
}

// ResolvePathChecked resolves p and guarantees that the resulting path stays
// inside BaseDir(), including through existing symlinks.
func ResolvePathChecked(p string) (string, error) {
	p = strings.TrimSpace(p)

	if p == "" {
		return "", errors.New("tools: path is empty")
	}

	base := BaseDir()
	if base == "" {
		return "", ErrBaseDirUnset
	}

	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}

	baseAbs = filepath.Clean(baseAbs)

	normalized := normalizePathSeparators(p)

	// Reject tilde expansion rather than silently reaching outside the base.
	if normalized == "~" ||
		strings.HasPrefix(normalized, "~"+string(filepath.Separator)) {
		return "", ErrPathOutsideBase
	}

	candidate := normalized

	if !filepath.IsAbs(normalized) {
		candidate = filepath.Join(baseAbs, normalized)
	}

	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}

	candidateAbs = filepath.Clean(candidateAbs)

	if !pathWithinBase(candidateAbs, baseAbs) {
		return "", ErrPathOutsideBase
	}

	// Resolve the real base where possible. This prevents a symlinked
	// BaseDir itself from becoming an escape hatch.
	realBase, err := existingRealPath(baseAbs)
	if err != nil {
		return "", err
	}

	// Validate the complete existing portion of the candidate path. This
	// catches:
	//
	//   base/link/file
	//
	// when "link" points outside BaseDir, even if "file" does not exist yet.
	realExistingPrefix, err := resolveExistingPrefix(candidateAbs)
	if err != nil {
		return "", err
	}

	if !pathWithinBase(realExistingPrefix, realBase) {
		return "", ErrPathOutsideBase
	}

	// When the final path already exists, resolve it completely as a
	// defense-in-depth check for a final symlink target.
	if _, statErr := os.Lstat(candidateAbs); statErr == nil {
		realCandidate, evalErr := filepath.EvalSymlinks(candidateAbs)
		if evalErr != nil {
			return "", evalErr
		}

		realCandidateAbs, absErr := filepath.Abs(realCandidate)
		if absErr != nil {
			return "", absErr
		}

		if !pathWithinBase(
			filepath.Clean(realCandidateAbs),
			realBase,
		) {
			return "", ErrPathOutsideBase
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}

	return candidateAbs, nil
}

// ResolvePathWithinBase resolves a caller-supplied path beneath an explicit
// base directory. This is useful for isolated components such as Coding Lab
// workspaces where BaseDir() is intentionally not the relevant root.
func ResolvePathWithinBase(base, p string) (string, error) {
	base = strings.TrimSpace(base)
	p = strings.TrimSpace(p)

	if base == "" {
		return "", ErrBaseDirUnset
	}

	if p == "" {
		return "", errors.New("tools: path is empty")
	}

	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}

	baseAbs = filepath.Clean(baseAbs)

	normalized := normalizePathSeparators(p)

	if normalized == "~" ||
		strings.HasPrefix(normalized, "~"+string(filepath.Separator)) {
		return "", ErrPathOutsideBase
	}

	var candidate string

	if filepath.IsAbs(normalized) {
		candidate = normalized
	} else {
		candidate = filepath.Join(baseAbs, normalized)
	}

	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}

	candidateAbs = filepath.Clean(candidateAbs)

	if !pathWithinBase(candidateAbs, baseAbs) {
		return "", ErrPathOutsideBase
	}

	realBase, err := existingRealPath(baseAbs)
	if err != nil {
		return "", err
	}

	realExistingPrefix, err := resolveExistingPrefix(candidateAbs)
	if err != nil {
		return "", err
	}

	if !pathWithinBase(realExistingPrefix, realBase) {
		return "", ErrPathOutsideBase
	}

	if _, statErr := os.Lstat(candidateAbs); statErr == nil {
		realCandidate, evalErr := filepath.EvalSymlinks(candidateAbs)
		if evalErr != nil {
			return "", evalErr
		}

		realCandidateAbs, absErr := filepath.Abs(realCandidate)
		if absErr != nil {
			return "", absErr
		}

		if !pathWithinBase(
			filepath.Clean(realCandidateAbs),
			realBase,
		) {
			return "", ErrPathOutsideBase
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}

	return candidateAbs, nil
}

// pathWithinBase returns true when path is base itself or a descendant of it.
func pathWithinBase(path, base string) bool {
	path = filepath.Clean(path)
	base = filepath.Clean(base)

	if path == base {
		return true
	}

	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	return rel != ".." &&
		!strings.HasPrefix(
			rel,
			".."+string(filepath.Separator),
		)
}

// normalizePathSeparators converts Windows-style separators before path
// validation. This makes traversal attempts behave consistently across
// platforms.
func normalizePathSeparators(p string) string {
	return strings.ReplaceAll(
		p,
		"\\",
		string(filepath.Separator),
	)
}

// existingRealPath resolves a path through symlinks when it exists.
// The unresolved lexical path is retained only when the path itself does
// not exist yet.
func existingRealPath(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		abs, absErr := filepath.Abs(real)
		if absErr != nil {
			return "", absErr
		}

		return filepath.Clean(abs), nil
	}

	if errors.Is(err, os.ErrNotExist) {
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return "", absErr
		}

		return filepath.Clean(abs), nil
	}

	return "", err
}

// resolveExistingPrefix finds the nearest existing ancestor of path and
// resolves all symlinks in that ancestor. The remaining non-existent suffix
// is intentionally ignored for containment purposes.
//
// Example:
//
//	base/link/new.txt
//
// If base/link is a symlink to /outside, the returned prefix is /outside and
// containment fails before anything can be created there.
func resolveExistingPrefix(path string) (string, error) {
	current := filepath.Clean(path)

	for {
		_, err := os.Lstat(current)

		if err == nil {
			real, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return "", evalErr
			}

			abs, absErr := filepath.Abs(real)
			if absErr != nil {
				return "", absErr
			}

			return filepath.Clean(abs), nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(current)

		if parent == current {
			return "", ErrPathOutsideBase
		}

		current = parent
	}
}

// SafeJoin is a convenience wrapper for code that needs to construct a path
// from several user-controlled relative components while retaining the same
// jail guarantee.
func SafeJoin(parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", errors.New("tools: no path supplied")
	}

	base := BaseDir()
	if base == "" {
		return "", ErrBaseDirUnset
	}

	joined := filepath.Join(parts...)
	return ResolvePathWithinBase(base, joined)
}

// SafeExistingPath resolves a path and verifies that the filesystem object
// remains inside the canonical base, including intermediate symlinks.
func SafeExistingPath(p string) (string, error) {
	return ResolvePathChecked(p)
}

// UnsafeAbsolutePath reports whether p is an absolute filesystem path outside
// the canonical tool base.
func UnsafeAbsolutePath(p string) bool {
	p = strings.TrimSpace(p)

	if p == "" || !filepath.IsAbs(p) {
		return false
	}

	base := BaseDir()
	if base == "" {
		return true
	}

	resolved, err := ResolvePathChecked(p)
	return err != nil || resolved == ""
}

// pruneScreenshots keeps only the newest `keep` shot-*/screen-* PNGs in
// dir (v1.1.4: screenshots previously accumulated without bound). Names
// are timestamp-prefixed, so lexicographic descending order is recency
// order.
func pruneScreenshots(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keep {
		return
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") {
			continue
		}
		names = append(names, e.Name())
	}

	if len(names) <= keep {
		return
	}

	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	for _, name := range names[keep:] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}
