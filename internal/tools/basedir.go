package tools

import (
	"errors"
	"fmt"
	"io/fs"
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

	// errInvalidArtifactName is returned (wrapped) for artifact names
	// that fail validArtifactName.
	errInvalidArtifactName = errors.New("invalid artifact name")
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

// toolPath is the ONE fail-closed chokepoint for tool-argument paths
// (v1.11.1 path-injection remediation, work order §7 — class (a):
// app-managed tool paths). Every untrusted path an agent hands to a tool
// resolves through here: the returned absolute path is guaranteed to stay
// inside BaseDir(), including through symlinks, and a hostile or malformed
// path produces a wrapped error — never a filesystem operation outside the
// base.
func toolPath(p string) (string, error) {
	resolved, err := ResolvePathChecked(p)
	if err != nil {
		return "", fmt.Errorf("tools: refusing path %q: %w", p, err)
	}

	return resolved, nil
}

// --- sink-level rooting (v1.11.1, work order §4.4) ------------------------
//
// Every filesystem operation on a tool path runs THROUGH the BaseDir
// os.Root with the base-relative name: the root refuses traversal, absolute
// spellings and symlink escapes at the OS layer, on every call, regardless
// of what any upstream check saw. toolRel derives the base-relative form
// from a toolPath-resolved absolute path and gates it with
// filepath.IsLocal (an explicit, recognized containment guard).

// toolRel converts a toolPath-resolved absolute path into the
// BaseDir-relative name the os.Root helpers accept.
func toolRel(resolved string) (string, error) {
	base := BaseDir()
	if base == "" {
		return "", ErrBaseDirUnset
	}

	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	baseAbs = filepath.Clean(baseAbs)

	rel, err := filepath.Rel(baseAbs, filepath.Clean(resolved))
	if err != nil {
		return "", err
	}
	if !filepath.IsLocal(rel) {
		return "", ErrPathOutsideBase
	}

	return rel, nil
}

// toolRoot opens the BaseDir containment root.
func toolRoot() (*os.Root, error) {
	base := BaseDir()
	if base == "" {
		return nil, ErrBaseDirUnset
	}
	return os.OpenRoot(base)
}

// toolOpen opens (read-only) a tool-resolved path through the BaseDir root.
func toolOpen(resolved string) (*os.File, error) {
	rel, err := toolRel(resolved)
	if err != nil {
		return nil, err
	}
	root, err := toolRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(rel)
}

// toolCreate creates/truncates a tool-resolved path through the BaseDir root.
func toolCreate(resolved string) (*os.File, error) {
	return toolOpenFileFlags(resolved, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
}

// toolOpenFileFlags opens a tool-resolved path with explicit flags through
// the BaseDir root.
func toolOpenFileFlags(resolved string, flags int, perm os.FileMode) (*os.File, error) {
	rel, err := toolRel(resolved)
	if err != nil {
		return nil, err
	}
	root, err := toolRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(rel, flags, perm)
}

// toolMkdirAll creates a directory tree at a tool-resolved path through the
// BaseDir root.
func toolMkdirAll(resolved string, perm os.FileMode) error {
	rel, err := toolRel(resolved)
	if err != nil {
		return err
	}
	root, err := toolRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(rel, perm)
}

// toolStats stats a tool-resolved path through the BaseDir root.
func toolStat(resolved string) (os.FileInfo, error) {
	rel, err := toolRel(resolved)
	if err != nil {
		return nil, err
	}
	root, err := toolRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Stat(rel)
}

// toolReadDir lists a tool-resolved directory through the BaseDir root
// (via the root's fs.FS view — os.Root has no direct ReadDir).
func toolReadDir(resolved string) ([]os.DirEntry, error) {
	rel, err := toolRel(resolved)
	if err != nil {
		return nil, err
	}
	root, err := toolRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return fs.ReadDir(root.FS(), rel)
}

// toolWriteFile writes a file at a tool-resolved path through the BaseDir
// root.
func toolWriteFile(resolved string, data []byte, perm os.FileMode) error {
	rel, err := toolRel(resolved)
	if err != nil {
		return err
	}
	root, err := toolRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteFile(rel, data, perm)
}

// toolReadFile reads a file at a tool-resolved path through the BaseDir root.
func toolReadFile(resolved string) ([]byte, error) {
	rel, err := toolRel(resolved)
	if err != nil {
		return nil, err
	}
	root, err := toolRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(rel)
}

// toolRemove deletes a tool-resolved path through the BaseDir root.
func toolRemove(resolved string) error {
	rel, err := toolRel(resolved)
	if err != nil {
		return err
	}
	root, err := toolRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(rel)
}

// toolRemoveAll deletes a tool-resolved tree through the BaseDir root.
func toolRemoveAll(resolved string) error {
	rel, err := toolRel(resolved)
	if err != nil {
		return err
	}
	root, err := toolRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(rel)
}

// toolRename moves within the BaseDir root: both operands must be
// tool-resolved paths.
func toolRename(oldResolved, newResolved string) error {
	oldRel, err := toolRel(oldResolved)
	if err != nil {
		return err
	}
	newRel, err := toolRel(newResolved)
	if err != nil {
		return err
	}
	root, err := toolRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Rename(oldRel, newRel)
}

// toolCreateTempIn creates a fresh exclusive temporary file inside the
// directory of a tool-resolved path, through the BaseDir root (the os.Root
// API has no CreateTemp — the exclusive O_EXCL create loop reproduces its
// semantics inside the root).
func toolCreateTempIn(resolvedDir string, prefix string) (*os.File, error) {
	dirRel, err := toolRel(resolvedDir)
	if err != nil {
		return nil, err
	}
	root, err := toolRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	var next uint64
	for attempt := 0; attempt < 10000; attempt++ {
		next++
		name := fmt.Sprintf("%s%d", prefix, next)
		f, err := root.OpenFile(filepath.Join(dirRel, name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return nil, fs.ErrExist
}

// validArtifactName is the ONE grammar for a caller-supplied artifact name
// used as a single filesystem path component (v1.11.1 path-injection
// remediation, work order §7 — class (b): single-component identifier).
// Names arrive from tool arguments (untrusted) and from dataset basenames;
// every write built from one must land as a FLAT component beside its
// dataset. Mirrors the sessions.validSessionID grammar and rejects: empty
// and oversized names, path separators (POSIX and Windows), NUL,
// volume/ADS colons, leading dots ("." and ".." included), trailing
// dots/spaces (Windows reserved shapes), whitespace-padded names, "~" and
// non-grammar characters.
func validArtifactName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if strings.ContainsAny(name, "\x00:") {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	if strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ' ':
		default:
			return false
		}
	}
	return true
}

// ResolvePathChecked resolves p and guarantees that the resulting path stays
// inside BaseDir(), including through existing symlinks.
//
// v1.11.1 sink-level rooting (work order §4.4/§7): the containment of the
// EXISTING portion of the candidate is verified through an os.Root opened
// on BaseDir — the root refuses traversal, separators and symlink escapes
// at the OS layer for every component it resolves, so no validated path can
// redirect a later operation outside the base even between validation and
// use. The returned value remains the lexical absolute path (unchanged
// contract); the actual filesystem operations of every tool run through the
// tool* Root helpers below.
func ResolvePathChecked(p string) (string, error) {
	p = strings.TrimSpace(p)

	if p == "" {
		return "", errors.New("tools: path is empty")
	}

	// v1.11.1 path-injection remediation (work order §7): explicit
	// fail-closed NUL rejection. A NUL-bearing path is never a valid tool
	// path; without this guard it dies with an opaque syscall EINVAL at
	// the sink (or is silently truncated by a C-host runtime).
	if strings.ContainsRune(p, 0) {
		return "", errors.New("tools: path contains a NUL byte")
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

	// Lexical containment passed: verify the candidate through the BaseDir
	// root. Root.Stat refuses, at the OS layer, any candidate whose
	// existing portion escapes the base (including intermediate symlink
	// redirection); a not-yet-existing candidate is fine (create flows).
	if err := verifyUnderToolRoot(candidateAbs); err != nil {
		return "", err
	}

	return candidateAbs, nil
}

// verifyUnderToolRoot proves the existing portion of a BaseDir-contained
// lexical path resolves INSIDE the base through an os.Root. A missing
// candidate is acceptable (create flows); an escaping symlink, a traversal
// component or an inaccessible component is not.
func verifyUnderToolRoot(candidateAbs string) error {
	base := BaseDir()
	if base == "" {
		return ErrBaseDirUnset
	}

	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	baseAbs = filepath.Clean(baseAbs)

	rel, err := filepath.Rel(baseAbs, filepath.Clean(candidateAbs))
	if err != nil {
		return err
	}

	// filepath.IsLocal refuses absolute forms, separators-escaping shapes
	// and anything that is not a plain relative path — a recognized,
	// explicit containment gate in addition to the root itself.
	if !filepath.IsLocal(rel) {
		return ErrPathOutsideBase
	}

	root, err := os.OpenRoot(baseAbs)
	if err != nil {
		return err
	}
	defer root.Close()

	if _, err := root.Stat(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}

// ResolvePathWithinBase resolves a caller-supplied path beneath an explicit
// base directory. This is useful for isolated components such as Coding Lab
// workspaces where BaseDir() is intentionally not the relevant root.
//
// v1.11.1 sink-level rooting: containment of the existing portion is
// verified through an os.Root on the explicit base, exactly like
// ResolvePathChecked does for BaseDir.
func ResolvePathWithinBase(base, p string) (string, error) {
	base = strings.TrimSpace(base)
	p = strings.TrimSpace(p)

	if base == "" {
		return "", ErrBaseDirUnset
	}

	if p == "" {
		return "", errors.New("tools: path is empty")
	}

	// v1.11.1 path-injection remediation (work order §7): the same
	// explicit NUL rejection as ResolvePathChecked.
	if strings.ContainsRune(p, 0) {
		return "", errors.New("tools: path contains a NUL byte")
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

	// Root-verify against the EXPLICIT base (not BaseDir): open the base,
	// and refuse any candidate whose existing portion escapes it.
	rel, err := filepath.Rel(baseAbs, candidateAbs)
	if err != nil {
		return "", err
	}

	if !filepath.IsLocal(rel) {
		return "", ErrPathOutsideBase
	}

	root, err := os.OpenRoot(baseAbs)
	if err != nil {
		return "", err
	}
	defer root.Close()

	if _, err := root.Stat(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
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
//
// v1.11.1 path-injection remediation (work order §7): every removal is
// ROOTED — the directory is opened as an os.Root and entries are removed
// by their listing names through that handle. os.Root rejects separators,
// ".." and symlink redirection at the OS layer, so no pruned name can ever
// address anything outside dir. Failures stay ignored, exactly like the
// previous os.Remove loop.
func pruneScreenshots(dir string, keep int) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return
	}
	defer root.Close()

	entries, err := fs.ReadDir(root.FS(), ".")
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
		_ = root.Remove(name)
	}
}
