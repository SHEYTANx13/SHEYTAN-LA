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
        "fmt"
        "io"
        "math/rand"
        "os"
        "path/filepath"
        "strings"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
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
func ImportModel(modelsDir, src string, onProgress func(copied, total int64)) (*ImportResult, error) {
        src = strings.TrimSpace(src)
        if src == "" {
                return nil, fmt.Errorf("import: source path is empty")
        }

        abs, err := filepath.Abs(src)
        if err != nil {
                return nil, fmt.Errorf("import: resolve %s: %w", src, err)
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
        base := filepath.Base(abs)
        dest := filepath.Join(modelsDir, base)
        renamedFrom := ""

        if _, statErr := os.Stat(dest); statErr == nil {
                dup, dupErr := sameContent(dest, abs)
                if dupErr == nil && dup {
                        logging.Default().Info("models",
                                "import skipped: %s already exists in the models directory (identical size + SHA-256)",
                                base)

                        return &ImportResult{
                                Name:       base,
                                Path:       dest,
                                SizeBytes:  fi.Size(),
                                Duplicate:  true,
                                RenamedFrom: renamedFrom,
                                Card:       card,
                        }, nil
                }

                // Different content under the same name: a fresh, collision-free
                // name — never an overwrite of the user's existing model.
                base, dest = nextFreeModelName(modelsDir, base)
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
        if err := os.Rename(tmpName, dest); err != nil {
                _ = os.Remove(tmpName)
                return nil, fmt.Errorf("import: place model: %w", err)
        }

        // Drop any foreign write bits (a read-only source must not produce a
        // read-only managed copy the updater cannot manage).
        _ = os.Chmod(dest, 0o644)

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
// -1, -2, … before the .gguf extension.
func nextFreeModelName(modelsDir, base string) (string, string) {
        ext := filepath.Ext(base)
        stem := strings.TrimSuffix(base, ext)

        for i := 1; i < 1000; i++ {
                candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
                if _, err := os.Stat(filepath.Join(modelsDir, candidate)); err != nil {
                        return candidate, filepath.Join(modelsDir, candidate)
                }
        }

        // Practically unreachable; fall back to a unique random suffix.
        candidate := fmt.Sprintf("%s-%d%s", stem, rand.Int63n(1<<30), ext)
        return candidate, filepath.Join(modelsDir, candidate)
}

// sameContent reports whether two existing files are byte-identical
// (size pre-check, then full SHA-256 — the same standard the data-root
// migration uses).
func sameContent(a, b string) (bool, error) {
        fa, err := os.Stat(a)
        if err != nil {
                return false, err
        }
        fb, err := os.Stat(b)
        if err != nil {
                return false, err
        }
        if fa.Size() != fb.Size() {
                return false, nil
        }

        ha, err := fileSHA256(a)
        if err != nil {
                return false, err
        }
        hb, err := fileSHA256(b)
        if err != nil {
                return false, err
        }
        return ha == hb, nil
}

func fileSHA256(path string) (string, error) {
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
