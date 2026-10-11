// migrate.go — v1.3.0 malformed-runtime-root migration.
//
// # THE LEGACY LAYOUT THIS FILE REPAIRS
//
// v1.2.9 could persist a literal environment token as DataDir (see
// paths.go for the origin). Because "%LOCALAPPDATA%\SHEYTAN-LA" is not
// an absolute path, every join anchored it to the process working
// directory / application root and produced real on-disk directories:
//
//      <root>\%LOCALAPPDATA%\SHEYTAN-LA\models
//      <root>\%LOCALAPPDATA%\SHEYTAN-LA\sessions\...
//      <root>\SHEYTAN-LA\SHEYTAN-LA\models          (doubled product nesting)
//
// Real models, sessions and configuration were written INTO those
// malformed trees. The migration below folds every such tree back into
// the canonical root — model-safe, session-safe, idempotent and
// restart-safe:
//
//   - every file is either RENAMED (same volume, atomic) or COPIED and
//     hash-verified (cross-volume / replace);
//   - a destination that already exists wins iff it is not older than
//     the source (never overwrite newer data);
//   - identical duplicates are detected (size + SHA-256) and the source
//     copy is dropped;
//   - the malformed source tree is removed ONLY after every entry has
//     been accounted for;
//   - an interrupted run leaves the source tree (partially) intact, and
//     the next run completes it — re-running the merge is a no-op for
//     files already migrated;
//   - a config.json recovered from a malformed tree is reported so the
//     caller re-Loads the configuration; when both trees carry one, the
//     live file keeps its place and the recovered one is preserved as
//     config.legacy.json (nothing is silently discarded).
package config

import (
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "io"
        "os"
        "path/filepath"
        "sort"
        "strings"
)

// MigrationReport records everything the malformed-root migration did,
// in a form suitable for one honest log entry per line.
type MigrationReport struct {
        // Detected lists the malformed source roots found on disk.
        Detected []string
        // Merged lists per-directory statistics ("<dir>: N files, M bytes").
        Merged []string
        // Recovered lists notable files brought back (config.json etc.).
        Recovered []string
        // Collisions lists resolved conflicts ("kept <newer>, replaced <older>").
        Collisions []string
        // Removed lists the source trees deleted after verified migration.
        Removed []string
        // ReloadConfig is true when a config.json was recovered into the
        // canonical root and the caller should re-Load the configuration.
        ReloadConfig bool
}

// HasMigrated reports whether anything at all happened.
func (r *MigrationReport) HasMigrated() bool {
        return r != nil && (len(r.Detected) > 0 || len(r.Merged) > 0)
}

// summary returns a one-line human digest for the log entry.
func (r *MigrationReport) summary() string {
        if r == nil || !r.HasMigrated() {
                return "no malformed runtime roots found"
        }
        return fmt.Sprintf(
                "detected=%d merged=%d recovered=%d collisions=%d removed=%d reloadConfig=%t",
                len(r.Detected), len(r.Merged), len(r.Recovered), len(r.Collisions), len(r.Removed), r.ReloadConfig,
        )
}

// MigrateMalformedRoots folds every malformed runtime tree into the
// canonical root. It is idempotent and restart-safe (see the package
// comment). The canonical root itself is never treated as a candidate.
//
// Scanned locations:
//   - immediate subdirectories of the canonical root whose name is a
//     literal environment token ("%LOCALAPPDATA%");
//   - the doubled product-nesting directory (canonical/SHEYTAN-LA when
//     the canonical root is itself named SHEYTAN-LA, plus the legacy
//     long-name variant);
//   - when SHEYTAN_DATA_DIR moved the root elsewhere, the application
//     root's malformed trees (where v1.2.9 actually created them).
func MigrateMalformedRoots(cfg *Config) (*MigrationReport, error) {
        report := &MigrationReport{}

        canonical := filepath.Clean(cfg.DataDir)
        if canonical == "" || canonical == "." {
                return report, nil
        }

        if err := os.MkdirAll(canonical, 0o755); err != nil {
                return report, fmt.Errorf("migrate: canonical root %s: %w", canonical, err)
        }

        candidates, tokenParents := malformedCandidates(canonical)

        // When the data root lives away from the application root, the v1.2.9
        // malformed trees were created under the APPLICATION root — scan it
        // too (deduplicated below).
        if appRoot := filepath.Clean(AppRoot()); appRoot != canonical {
                appCandidates, appTokens := malformedCandidates(appRoot)
                candidates = append(candidates, appCandidates...)
                tokenParents = append(tokenParents, appTokens...)
        }

        sort.Strings(candidates)

        seen := map[string]bool{}
        for _, candidate := range candidates {
                if seen[candidate] {
                        continue
                }
                seen[candidate] = true

                // A candidate that IS the canonical root, or CONTAINS it,
                // must never be merged into itself. (Candidates are
                // normally strict subdirectories of the canonical root —
                // that is the whole point of the merge.)
                if candidate == canonical || sameOrChild(canonical, candidate) {
                        continue
                }

                fi, err := os.Stat(candidate)
                if err != nil || !fi.IsDir() {
                        continue
                }

                report.Detected = append(report.Detected, candidate)

                if err := mergeTree(candidate, canonical, report); err != nil {
                        // Leave the source intact: the next run retries (idempotent).
                        return report, fmt.Errorf("migrate %s: %w", candidate, err)
                }

                // Every entry accounted for — remove the malformed tree.
                if err := os.RemoveAll(candidate); err != nil {
                        return report, fmt.Errorf("migrate: remove %s: %w", candidate, err)
                }
                report.Removed = append(report.Removed, candidate)
        }

        // Post-pass: drop the (now-empty) literal token directories
        // themselves. os.Remove only succeeds when they are empty — a token
        // directory still holding unexpected content stays and is treated
        // as a candidate by the next run.
        for _, tokenDir := range tokenParents {
                if tokenDir == canonical {
                        continue
                }
                if _, err := os.Stat(tokenDir); err == nil {
                        _ = os.Remove(tokenDir)
                }
        }

        return report, nil
}

// malformedCandidates enumerates the malformed DATA-ROOT candidates under
// one parent (deterministic order), plus the literal token directories
// that own them (the post-pass removes those once empty).
//
// For a token directory "%LOCALAPPDATA%" the v1.2.9 DataDir was usually
// "%LOCALAPPDATA%\SHEYTAN-LA" — so the data tree sits INSIDE the token
// directory, one level down. Resolving the candidate to that inner root
// (instead of the token directory itself) is what makes merged files land
// at <canonical>/models rather than <canonical>/SHEYTAN-LA/models.
func malformedCandidates(parent string) (candidates []string, tokenParents []string) {
        entries, err := os.ReadDir(parent)
        if err != nil {
                return nil, nil
        }

        base := filepath.Base(filepath.Clean(parent))
        productNested := strings.EqualFold(base, AppShortName) || strings.EqualFold(base, AppName)

        for _, entry := range entries {
                if !entry.IsDir() {
                        continue
                }
                name := entry.Name()

                // Literal environment-token directory ("%LOCALAPPDATA%").
                if envTokenPattern.MatchString(name) {
                        tokenDir := filepath.Join(parent, name)
                        tokenParents = append(tokenParents, tokenDir)

                        // The product-named data root inside the token directory.
                        inner := ""
                        for _, prod := range []string{AppShortName, AppName} {
                                if fi, err := os.Stat(filepath.Join(tokenDir, prod)); err == nil && fi.IsDir() {
                                        inner = filepath.Join(tokenDir, prod)
                                        break
                                }
                        }

                        if inner != "" {
                                candidates = append(candidates, inner)
                        } else {
                                // DataDir was the token itself — its content IS the data
                                // root (models/ directly under it).
                                candidates = append(candidates, tokenDir)
                        }
                        continue
                }

                // Doubled product nesting under a product-named root
                // (.../SHEYTAN-LA/SHEYTAN-LA).
                if productNested &&
                        (strings.EqualFold(name, AppShortName) || strings.EqualFold(name, AppName)) {
                        candidates = append(candidates, filepath.Join(parent, name))
                }
        }

        sort.Strings(candidates)
        sort.Strings(tokenParents)
        return candidates, tokenParents
}

// mergeTree folds srcRoot into dstRoot. Files are renamed when the
// destination is free (same volume: atomic), or hash-verified copies
// otherwise. Collisions keep the newer file. Errors abort the merge
// leaving the source tree recoverable.
func mergeTree(srcRoot, dstRoot string, report *MigrationReport) error {
        var (
                files   int
                bytes   int64
                failErr error
        )

        err := filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }

                rel, relErr := filepath.Rel(srcRoot, path)
                if relErr != nil {
                        return relErr
                }
                if rel == "." {
                        return nil
                }

                dst := filepath.Join(dstRoot, rel)

                if info.IsDir() {
                        return os.MkdirAll(dst, 0o755)
                }
                if !info.Mode().IsRegular() {
                        // Symlinks/special nodes are not migrated — recorded, not
                        // silently dropped.
                        report.Collisions = append(report.Collisions,
                                fmt.Sprintf("skipped non-regular file %s", rel))
                        return nil
                }

                if rel == "config.json" {
                        return mergeRecoveredConfig(path, dst, report)
                }

                moved, werr := migrateFile(path, dst, rel, report)
                if werr != nil {
                        failErr = werr
                        return werr
                }
                if moved {
                        files++
                        bytes += info.Size()
                }
                return nil
        })
        if err != nil && failErr == nil {
                failErr = err
        }
        if failErr != nil {
                return failErr
        }

        report.Merged = append(report.Merged, fmt.Sprintf("%s: %d files, %d bytes", srcRoot, files, bytes))
        return nil
}

// mergeRecoveredConfig handles the config.json found inside a malformed
// tree. When the canonical root has none, the recovered file is renamed
// into place and ReloadConfig is flagged. When both exist, the LIVE file
// stays and the recovered one is preserved as config.legacy.json —
// nothing is discarded, the choice is deterministic.
func mergeRecoveredConfig(src, dst string, report *MigrationReport) error {
        if _, err := os.Stat(dst); os.IsNotExist(err) {
                if err := os.Rename(src, dst); err != nil {
                        return copyVerified(src, dst)
                }
                report.Recovered = append(report.Recovered, dst)
                report.ReloadConfig = true
                return nil
        }

        legacy := filepath.Join(filepath.Dir(dst), "config.legacy.json")
        if _, err := os.Stat(legacy); os.IsNotExist(err) {
                if err := os.Rename(src, legacy); err != nil {
                        if err := copyVerified(src, legacy); err != nil {
                                return err
                        }
                }
                report.Recovered = append(report.Recovered, legacy)
                return nil
        }

        // A legacy copy already exists from a previous interrupted run —
        // keep the newer of the two, deterministically.
        if newer(src, legacy) {
                _ = os.Remove(legacy)
                if err := os.Rename(src, legacy); err != nil {
                        return copyVerified(src, legacy)
                }
        } else {
                _ = os.Remove(src)
        }
        report.Collisions = append(report.Collisions, "config.json: kept live file, preserved recovered copy")
        return nil
}

// migrateFile moves ONE file into the canonical tree.
//
//   - destination free → atomic rename (fallback: verified copy);
//   - destination exists, identical (size + SHA-256) → drop the source;
//   - destination exists, differs → the NEWER file wins (the older one is
//     replaced only after the newer copy is verified on disk).
func migrateFile(src, dst, rel string, report *MigrationReport) (bool, error) {
        dstInfo, dstErr := os.Stat(dst)
        if os.IsNotExist(dstErr) {
                if err := os.Rename(src, dst); err == nil {
                        return true, nil
                }
                if err := copyVerified(src, dst); err != nil {
                        return false, fmt.Errorf("migrate file %s: %w", rel, err)
                }
                return true, nil
        }
        if dstErr != nil {
                return false, dstErr
        }

        srcInfo, err := os.Stat(src)
        if err != nil {
                return false, err
        }

        if srcInfo.Size() == dstInfo.Size() {
                same, hashErr := sameContent(src, dst)
                if hashErr == nil && same {
                        // Identical duplicate (previous interrupted run) — the
                        // canonical copy stands, the source is dropped.
                        _ = os.Remove(src)
                        return false, nil
                }
        }

        if newer(src, dst) {
                // The source is the newer data — it must survive. Replace the
                // older destination, verified, then drop the source.
                if err := copyVerified(src, dst); err != nil {
                        return false, fmt.Errorf("migrate newer %s: %w", rel, err)
                }
                _ = os.Remove(src)
                report.Collisions = append(report.Collisions,
                        fmt.Sprintf("%s: kept newer copy (%s)", rel, dst))
                return true, nil
        }

        // Destination is newer — keep it, drop the older source.
        _ = os.Remove(src)
        report.Collisions = append(report.Collisions,
                fmt.Sprintf("%s: kept newer existing copy", rel))
        return false, nil
}

// renameForTest is the commit seam of copyVerified (v1.7.4): tests
// substitute it to simulate platform-specific rename failures — Windows
// denies renaming onto a path another process holds open ("Access is
// denied") — without adding retries or sleeps to production code.
var renameForTest = os.Rename

// copyVerified copies src to dst and verifies the copy by size AND
// SHA-256 digest. The destination is written to a temp file and renamed
// into place, so an interrupted copy never leaves a half-written target.
func copyVerified(src, dst string) error {
        srcHash, size, err := hashAndSize(src)
        if err != nil {
                return err
        }

        if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
                return err
        }

        tmp := dst + ".migrating"

        out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
        if err != nil {
                return err
        }

        in, err := os.Open(src)
        if err != nil {
                out.Close()
                _ = os.Remove(tmp)
                return err
        }

        written, copyErr := io.Copy(out, in)
        closeErr := out.Close()
        _ = in.Close()

        if copyErr != nil {
                _ = os.Remove(tmp)
                return copyErr
        }
        if closeErr != nil {
                _ = os.Remove(tmp)
                return closeErr
        }
        if written != size {
                _ = os.Remove(tmp)
                return fmt.Errorf("copy %s: wrote %d of %d bytes", dst, written, size)
        }

        dstHash, dstSize, err := hashAndSize(tmp)
        if err != nil {
                _ = os.Remove(tmp)
                return err
        }
        if dstHash != srcHash || dstSize != size {
                _ = os.Remove(tmp)
                return fmt.Errorf("copy %s: verification failed (sha256 %s != %s)", dst, dstHash, srcHash)
        }

        if err := renameForTest(tmp, dst); err != nil { // commit seam (v1.7.4, see renameForTest)
                _ = os.Remove(tmp)

                // v1.7.4: classify the conflict honestly. The temp file is
                // gone and the destination is untouched — on Windows this is
                // the "destination held open by another process" shape (the
                // canonical log sinks are O_APPEND handles held for the
                // process lifetime), and the fold simply retries on the next
                // start. Never delete or truncate the destination here.
                return fmt.Errorf("commit %s: rename: %w (destination may be held open — will retry on next start)", dst, err)
        }
        return nil
}

// hashAndSize returns the SHA-256 hex digest and size of a regular file.
func hashAndSize(path string) (string, int64, error) {
        f, err := os.Open(path)
        if err != nil {
                return "", 0, err
        }
        defer f.Close()

        h := sha256.New()
        size, err := io.Copy(h, f)
        if err != nil {
                return "", 0, err
        }
        return hex.EncodeToString(h.Sum(nil)), size, nil
}

// sameContent reports whether two files are byte-identical.
func sameContent(a, b string) (bool, error) {
        ha, _, err := hashAndSize(a)
        if err != nil {
                return false, err
        }
        hb, _, err := hashAndSize(b)
        if err != nil {
                return false, err
        }
        return ha == hb, nil
}

// newer reports whether path a's modification time is strictly after b's.
// Equal mtimes resolve to "a is not newer" (deterministic).
func newer(a, b string) bool {
        ia, err := os.Stat(a)
        if err != nil {
                return false
        }
        ib, err := os.Stat(b)
        if err != nil {
                return true
        }
        return ia.ModTime().After(ib.ModTime())
}

// sameOrChild reports whether candidate == parent or lives under it.
func sameOrChild(candidate, parent string) bool {
        c := filepath.Clean(candidate)
        p := filepath.Clean(parent)
        if c == p {
                return true
        }
        rel, err := filepath.Rel(p, c)
        if err != nil {
                return false
        }
        return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
                !filepath.IsAbs(rel)
}

// TakePathNotes drains Load's normalization reports (the command layer
// calls this once logging is live and logs each line with context).
// Token-shaped values are rendered in the safe [TOKEN] form so the
// literal never reaches the log.
func TakePathNotes(cfg *Config) []string {
        if cfg == nil || len(cfg.PathNotes) == 0 {
                return nil
        }
        notes := cfg.PathNotes
        cfg.PathNotes = nil
        for i, note := range notes {
                notes[i] = logSafePath(note)
        }
        return notes
}

// TakeSamplingNotes drains Load's sampling-repair reports (v1.6.1): one
// line per parser-invalid value that was safely reset to its documented
// default. Drained by the command layer once logging is live so the
// repair is visible instead of silent.
func TakeSamplingNotes(cfg *Config) []string {
        if cfg == nil || len(cfg.SamplingNotes) == 0 {
                return nil
        }
        notes := cfg.SamplingNotes
        cfg.SamplingNotes = nil
        return notes
}

// LogMigration emits the migration summary (called by cmd once logging
// is live; kept here so the report shape stays close to its producer).
func LogMigrationNotes(notes []string, report *MigrationReport) []string {
        lines := make([]string, 0, len(notes)+4)
        lines = append(lines, notes...)
        if report != nil && report.HasMigrated() {
                lines = append(lines, "malformed runtime root migration: "+report.summary())
                for _, det := range report.Detected {
                        lines = append(lines, "  detected: "+logSafePath(det))
                }
                for _, rec := range report.Recovered {
                        lines = append(lines, "  recovered: "+logSafePath(rec))
                }
                for _, col := range report.Collisions {
                        lines = append(lines, "  "+logSafePath(col))
                }
                for _, rem := range report.Removed {
                        lines = append(lines, "  removed: "+logSafePath(rem))
                }
        }
        return lines
}

// logSafePath renders a string for the LOG without any literal "%TOKEN%"
// sequence: a malformed directory named after an environment token is
// reported as [TOKEN], so the diagnostic evidence survives while the
// literal token never appears anywhere in runtime output (release
// contract §15: no literal %LOCALAPPDATA% in paths, config, logs or
// workspace metadata).
func logSafePath(s string) string {
        return envTokenPattern.ReplaceAllStringFunc(s, func(token string) string {
                return "[" + strings.Trim(token, "%") + "]"
        })
}
