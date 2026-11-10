package llm

// importmodel.go — v1.6.1: importing a local GGUF is a first-class path.
//
// The v1.6.0 story was "place a GGUF in the models folder yourself, then
// refresh" — the empty picker literally told the user to go find the
// folder. This file gives the import the same engineering standard as
// every other model operation:
//
//   - the GGUF header is VALIDATED before anything is copied (magic,
//     version, sane metadata — the same ReadModelCard authority the
//     picker and the recommendation engine use);
//   - the copy is STREAMING and file-based (io.CopyN with a bounded
//     buffer) — never a whole-model RAM load, so a 20 GB file costs the
//     same memory as a 20 MB one;
//   - the final placement is ATOMIC: the bytes land in a hidden
//     ".import-*.tmp" file inside the models directory and are renamed
//     into place only after the copied size is verified — a half-written
//     model can never appear in the picker;
//   - duplicates are SAFE: an existing file with the same size and
//     SHA-256 is reported as a duplicate (nothing overwritten); a
//     different file with the same name gets a fresh "-1"/"-2" name;
//   - the source is COPIED, never moved — arbitrary external model
//     paths (a Downloads folder, an external drive) stay exactly where
//     the user put them, and importing from a read-only location works;
//   - every import is one honest log line, and a cancelled copy leaves
//     no partial artifact behind.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
)

// ImportResult describes one completed import.
type ImportResult struct {
	// Name is the final file name inside the managed models directory.
	Name string `json:"name"`
	// Path is the absolute path of the imported copy.
	Path string `json:"path"`
	// SizeBytes is the verified size of the imported file.
	SizeBytes int64 `json:"sizeBytes"`
	// Duplicate is true when an identical file (same size + SHA-256)
	// already existed and the copy was skipped.
	Duplicate bool `json:"duplicate"`
	// RenamedFrom is the original file name when a collision forced a
	// fresh name ("" when the original name was kept).
	RenamedFrom string `json:"renamedFrom,omitempty"`
	// Card is the parsed GGUF metadata of the imported model (nil never
	// happens here — import requires a parseable header).
	Card *ModelCard `json:"-"`
}

// importCopyChunk is the streaming copy buffer size (1 MiB).
const importCopyChunk = 1 << 20

// --- path-injection guards (v1.11.1 remediation) -------------------------
//
// ImportModel mixes two distinct path classes and each gets its own rule:
//
//   - the USER-SELECTED source path is an explicit external file the user
//     asked to import (a Downloads folder, an external drive). That
//     capability is KEPT: the path is validated (non-empty, NUL-free,
//     absolute once resolved) and then used VERBATIM — no untrusted
//     component is ever appended to it and it is never written to.
//   - APP-MANAGED storage under modelsDir. A name that originates from
//     the outside world (the imported file's base name) must pass the
//     strict single-component grammar validModelFileName and is joined
//     ONLY through managedModelPath — the fail-closed chokepoint for the
//     models root (the same shape as sessions.artifactPath). A hostile
//     name produces an error, never a path outside modelsDir.

// sanitizeNULDiagnostics renders a tainted path safely for an error
// message: every NUL byte becomes an escaped "\\x00" so the diagnostic
// itself can never smuggle a raw control byte into a log file or a UI
// field. Only the diagnostics change — the canonical value is untouched.
func sanitizeNULDiagnostics(s string) string {
	if !strings.ContainsRune(s, '\x00') {
		return s
	}
	return strings.ReplaceAll(s, "\x00", "\\x00")
}

// validImportSource validates a user-selected external model source path
// at the point of use: it must be non-empty, NUL-free and absolute.
func validImportSource(src string) error {
	if src == "" {
		return fmt.Errorf("import: source path is empty")
	}
	if strings.ContainsRune(src, '\x00') {
		return fmt.Errorf("import: source path contains NUL")
	}
	if !filepath.IsAbs(src) {
		return fmt.Errorf("import: source path %q must be absolute", src)
	}
	// v1.11.1 sink-level rooting: the rooted-shape gate is the lexical
	// half of the selected-host-path contract, enforced right here where
	// the source enters the import pipeline (NUL-free, rooted, non-empty).
	if !hostPathShape.MatchString(src) {
		return fmt.Errorf("import: source path %q must be a NUL-free absolute host path", src)
	}
	return nil
}

// validReadablePath is the minimal validation every file path handed to a
// read/compare helper must pass (sameContent, fileSHA256): non-empty and
// NUL-free. It fails closed on a path that could never name a real file.
func validReadablePath(p string) error {
	if p == "" {
		return fmt.Errorf("import: empty file path")
	}
	if strings.ContainsRune(p, '\x00') {
		return fmt.Errorf("import: file path contains NUL")
	}
	return nil
}

// validModelFileName is the ONE grammar for a file name placed into the
// managed models directory. It accepts ordinary model file names and
// rejects: empty/oversized names, path separators (POSIX and Windows),
// NUL and the Windows volume/ADS colon, dot-prefixed names (they collide
// with the import lock and staging files), names that are only dots or
// spaces, trailing dot/space (stripped by Windows), control characters
// and Windows reserved device names (with or without extension).
func validModelFileName(name string) bool {
	if name == "" || len(name) > 255 {
		return false
	}
	if strings.ContainsAny(name, "/\\") || strings.ContainsAny(name, "\x00:") {
		return false
	}
	if name == "." || name == ".." {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	trimmed := strings.TrimRight(name, " .")
	if trimmed == "" || trimmed != name {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	if windowsReservedDeviceName(trimmed) {
		return false
	}
	return true
}

// windowsReservedDeviceName reports whether the stem (the part before the
// first dot) is a Windows reserved device name — unusable as a managed
// model name on Windows drives with or without an extension.
func windowsReservedDeviceName(stem string) bool {
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// hostPathShape is the lexical half of the user-selected-external-path
// contract (v1.11.1, work order §4.4 class "selected host path"): a
// NUL-free ABSOLUTE host path in either platform spelling — POSIX root,
// Windows drive prefix or Windows rooted form. It is enforced alongside
// filepath.IsAbs at every point of use, so a hostile or relative value
// can never reach a filesystem sink through this package.
var hostPathShape = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|[\\/])[^\x00]*$`)

// managedModelPath builds the on-disk path for one managed model file and
// FAILS CLOSED on a name that is not a safe single path component: the
// result is always filepath.Join(modelsDir, name) and can never leave the
// models root.
func managedModelPath(modelsDir, name string) (string, error) {
	if !validModelFileName(name) {
		return "", fmt.Errorf("import: %q is not a safe model file name", name)
	}
	return filepath.Join(modelsDir, name), nil
}

// modelNameOf derives the models-root-relative name for a
// managedModelPath result and gates it with filepath.IsLocal — the
// explicit containment guard for the rooted operations below.
func modelNameOf(dest string) (string, error) {
	rel := filepath.Base(dest)
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("import: %q is not a safe model file name", rel)
	}
	return rel, nil
}

// modelsRoot opens the managed models-root containment root. Every write,
// rename and stat on a managed model file runs THROUGH this root, so a
// hostile name can neither leave the models root nor redirect through a
// planted symlink.
func modelsRoot(modelsDir string) (*os.Root, error) {
	return os.OpenRoot(modelsDir)
}

// ImportModel validates and copies one GGUF file into the managed models
// directory. The source path may be anywhere on disk (or an external
// volume) — it is never modified. onProgress, when non-nil, receives the
// running copied/total byte counts.
//
// v1.6.2 CONCURRENCY: the whole destination-choice + copy window runs
// under the exclusive per-models-directory import lock (importlock.go)
// — two simultaneous imports can never overwrite or steal each other's
// destination. The copy itself is unchanged: streaming, file-based,
// atomically placed. Validation still happens BEFORE the lock (a bad
// source fails fast without serializing behind a legitimate import).
// errImportSourceNUL is the typed, deterministic refusal for a source
// path carrying a NUL byte: it is returned BEFORE any filesystem call
// (filepath.Abs, EvalSymlinks, os.Stat, os.Open), so a NUL-tainted
// source can never surface an incidental OS "invalid argument" as the
// primary diagnostic and can never touch a single file.
var errImportSourceNUL = errors.New("import: source path contains a NUL byte (invalid path bytes)")

// sourceNULOffset returns the byte offset of the first NUL in s, or -1.
func sourceNULOffset(s string) int {
	return strings.IndexByte(s, '\x00')
}

func ImportModel(modelsDir, src string, onProgress func(copied, total int64)) (*ImportResult, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("import: source path is empty")
	}

	// v1.11.1: NUL is detected on the RAW source FIRST — before
	// filepath.Abs, cleaning, EvalSymlinks, stat or open. A NUL byte is
	// never a real filename byte: it is refused with the typed defect
	// (errors.Is-able) naming the byte offset, and nothing on disk is
	// created, changed or touched.
	if off := sourceNULOffset(src); off >= 0 {
		return nil, fmt.Errorf("%w at byte offset %d in %q", errImportSourceNUL, off, sanitizeNULDiagnostics(src))
	}

	abs, err := filepath.Abs(src)
	if err != nil {
		return nil, fmt.Errorf("import: resolve %s: %w", src, err)
	}

	// v1.11.1 path-injection: the user-selected external source is
	// validated at the point of first use (non-empty, NUL-free, absolute)
	// and then used verbatim — never reassembled, never written to.
	if err := validImportSource(abs); err != nil {
		return nil, err
	}

	fi, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("import: source not reachable: %w", err)
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("import: %s is a directory, not a GGUF file", abs)
	}
	if !strings.EqualFold(filepath.Ext(abs), ".gguf") {
		return nil, fmt.Errorf("import: %s is not a .gguf file", filepath.Base(abs))
	}

	// --- validate the GGUF header BEFORE any copy -----------------------
	// ReadModelCard is the same authority the picker uses: magic, version
	// bounds, sanity-checked metadata. A non-GGUF file is rejected here,
	// before gigabytes start moving.
	card, err := ReadModelCard(abs)
	if err != nil {
		return nil, fmt.Errorf(
			"import: %s failed GGUF validation: %w (the file may be corrupt, incomplete, or not a GGUF model)",
			filepath.Base(abs), err,
		)
	}

	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return nil, fmt.Errorf("import: models dir: %w", err)
	}

	// v1.6.2: serialize the Stat → choose destination → copy → Rename
	// window across processes AND goroutines (importlock.go). Held
	// until return via defer — a failed or cancelled import releases
	// it with its cleaned-up .tmp.
	releaseLock, lockErr := lockModelsDir(modelsDir)
	if lockErr != nil {
		return nil, lockErr
	}
	defer releaseLock()

	// --- duplicate-safe destination naming ------------------------------
	// The imported file's base name is an outside-world name: it reaches
	// the models root ONLY through the managedModelPath chokepoint.
	base := filepath.Base(abs)
	dest, err := managedModelPath(modelsDir, base)
	if err != nil {
		return nil, err
	}
	renamedFrom := ""

	root, rootErr := modelsRoot(modelsDir)
	if rootErr != nil {
		return nil, rootErr
	}
	defer root.Close()

	origRel, origRelErr := modelNameOf(dest)
	if origRelErr != nil {
		return nil, origRelErr
	}

	if _, statErr := root.Stat(origRel); statErr == nil {
		dup, dupErr := sameContent(root, origRel, abs)
		if dupErr == nil && dup {
			logging.Default().Info("models",
				"import skipped: %s already exists in the models directory (identical size + SHA-256)",
				base)

			return &ImportResult{
				Name:        base,
				Path:        dest,
				SizeBytes:   fi.Size(),
				Duplicate:   true,
				RenamedFrom: renamedFrom,
				Card:        card,
			}, nil
		}

		// Different content under the same name: a fresh, collision-free
		// name — never an overwrite of the user's existing model.
		// v1.11.1 (CodeQL go/useless-assignment-to-local #236): `base` is
		// never read past this point — only the collision-free destination
		// path is consumed below.
		_, candidatePath, nfErr := nextFreeModelName(modelsDir, base)
		if nfErr != nil {
			return nil, nfErr
		}
		dest = candidatePath
		renamedFrom = filepath.Base(abs)
	}

	// --- streaming copy into a hidden temp file -------------------------
	tmp, err := os.CreateTemp(modelsDir, ".import-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("import: staging file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	in, err := os.Open(abs)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("import: open source: %w", err)
	}
	defer in.Close()

	var copied int64
	buf := make([]byte, importCopyChunk)

	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				cleanup()
				return nil, fmt.Errorf("import: write staging file: %w", werr)
			}
			copied += int64(n)
			if onProgress != nil {
				onProgress(copied, fi.Size())
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			cleanup()
			return nil, fmt.Errorf("import: read source: %w", rerr)
		}
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil, fmt.Errorf("import: close staging file: %w", err)
	}

	// Size verification before the atomic placement.
	tfi, err := os.Stat(tmpName)
	if err != nil || tfi.Size() != fi.Size() {
		_ = os.Remove(tmpName)
		return nil, fmt.Errorf(
			"import: staged copy is incomplete (got %d of %d bytes) — nothing was placed in the models directory",
			tfi.Size(), fi.Size(),
		)
	}

	// --- atomic final placement -----------------------------------------
	// destRel is derived AFTER the collision rename above finalized dest:
	// the staged copy moves to the FINAL managed name, never the original
	// spelling it was first joined under.
	destRel, destRelErr := modelNameOf(dest)
	if destRelErr != nil {
		_ = os.Remove(tmpName)
		return nil, destRelErr
	}
	if err := root.Rename(filepath.Base(tmpName), destRel); err != nil {
		_ = os.Remove(tmpName)
		return nil, fmt.Errorf("import: place model: %w", err)
	}

	// Drop any foreign write bits (a read-only source must not produce a
	// read-only managed copy the updater cannot manage).
	_ = root.Chmod(destRel, 0o644)

	renameNote := ""
	if renamedFrom != "" {
		renameNote = fmt.Sprintf(" (renamed from %s — a different file already used that name)", renamedFrom)
	}

	logging.Default().Info("models",
		"imported %s (%s) into the managed models directory%s",
		filepath.Base(dest), FormatBytes(fi.Size()), renameNote)

	return &ImportResult{
		Name:        filepath.Base(dest),
		Path:        dest,
		SizeBytes:   fi.Size(),
		Duplicate:   false,
		RenamedFrom: renamedFrom,
		Card:        card,
	}, nil
}

// nextFreeModelName returns a collision-free (name, path) pair by suffixing
// -1, -2, … before the .gguf extension. Every candidate is joined through
// the managedModelPath chokepoint, so the search can never leave the
// models root even when the incoming base name is hostile.
func nextFreeModelName(modelsDir, base string) (string, string, error) {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	root, err := modelsRoot(modelsDir)
	if err != nil {
		return "", "", err
	}
	defer root.Close()

	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		candidatePath, err := managedModelPath(modelsDir, candidate)
		if err != nil {
			continue
		}
		rel, relErr := modelNameOf(candidatePath)
		if relErr != nil {
			continue
		}
		if _, err := root.Stat(rel); err != nil {
			return candidate, candidatePath, nil
		}
	}

	// Practically unreachable; fall back to a unique random suffix.
	for i := 0; i < 64; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, rand.Int63n(1<<30), ext)
		candidatePath, err := managedModelPath(modelsDir, candidate)
		if err != nil {
			continue
		}
		rel, relErr := modelNameOf(candidatePath)
		if relErr != nil {
			continue
		}
		if _, err := root.Stat(rel); err != nil {
			return candidate, candidatePath, nil
		}
	}

	return "", "", fmt.Errorf("import: no collision-free name found for %q", base)
}

// sameContent reports whether the managed destination and the external
// source are byte-identical (size pre-check, then full SHA-256 — the same
// standard the data-root migration uses). v1.11.1: the MANAGED side is
// read through the models root; the external side is the shape-gated,
// user-selected source path.
func sameContent(root *os.Root, destRel, src string) (bool, error) {
	if err := validReadablePath(src); err != nil {
		return false, err
	}

	fa, err := root.Stat(destRel)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	if fa.Size() != fb.Size() {
		return false, nil
	}

	ha, err := rootFileSHA256(root, destRel)
	if err != nil {
		return false, err
	}
	hb, err := fileSHA256(src)
	if err != nil {
		return false, err
	}
	return ha == hb, nil
}

// rootFileSHA256 hashes a managed model file through the models root.
func rootFileSHA256(root *os.Root, nameRel string) (string, error) {
	f, err := root.Open(nameRel)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileSHA256(path string) (string, error) {
	if err := validReadablePath(path); err != nil {
		return "", err
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
