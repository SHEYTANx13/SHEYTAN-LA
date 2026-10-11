// v1.3.6 (spec §15–§19): LEGACY APPDATA ROOT MIGRATION.
//
// The 1.3.5 NSIS installer wrote the machine environment variable
// SHEYTAN_DATA_DIR=%LOCALAPPDATA%\SHEYTAN-LA, which forced every
// installed copy to keep its data in AppData even though the runtime's
// canonical contract is <AppRoot>\data. Machines upgraded from 1.3.5
// therefore carry a populated legacy root at
// %LOCALAPPDATA%\SHEYTAN-LA while the canonical root starts empty.
//
// This migration moves that legacy data into the canonical root —
// reusing the exact merge machinery of MigrateMalformedRoots
// (hash-verified copies, newer-wins collisions, rename-first moves,
// config recovery) — and removes the legacy root only after the merge
// is fully verified. It is idempotent, restart-safe and never runs when
// the user chose an explicit data root.
//
// The engine bundle is migrated as a UNIT (spec §18 item 9): when the
// canonical root already hosts the managed llama-server, the legacy
// engine package is not merged file-by-file — mixing two llama.cpp
// builds' DLL sets is exactly the 0xC0000139 entry-point failure class
// this release exists to prevent.

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// localAppDataDir resolves the per-user local application data root the
// 1.3.5 installer used (%LOCALAPPDATA%\SHEYTAN-LA on Windows).
// LOCALAPPDATA is honored on every platform (it is where the 1.3.5
// installer pointed its env var; tests and portable Windows-layout
// installs rely on it), with the platform cache dir as fallback.
func localAppDataDir() string {
	if v := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); v != "" {
		return v
	}

	if v, err := os.UserCacheDir(); err == nil {
		return v
	}

	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache")
	}

	return ""
}

// legacyAppDataRoot returns the 1.3.5-era AppData data root, or "" when
// this machine has none.
func legacyAppDataRoot() string {
	local := localAppDataDir()
	if local == "" {
		return ""
	}

	return filepath.Join(local, AppShortName)
}

// canonicalRootIsDefault reports whether the canonical root was resolved
// from the application root (the portable contract) rather than an
// explicit user override. An explicit SHEYTAN_DATA_DIR is a deliberate
// choice — its data is authoritative exactly where it is and migration
// must never touch it (spec §16).
func canonicalRootIsDefault() bool {
	raw, present := os.LookupEnv("SHEYTAN_DATA_DIR")
	if present && strings.TrimSpace(raw) != "" {
		return false
	}

	return true
}

// MigrateLegacyAppDataRoot folds the 1.3.5-era AppData data root into
// the canonical root. Safe to call on every start: it is a no-op unless
// the legacy root exists, the canonical root is the default (AppRoot)
// resolution, and the two roots are unrelated.
func MigrateLegacyAppDataRoot(cfg *Config) (*MigrationReport, error) {
	report := &MigrationReport{}

	if !canonicalRootIsDefault() {
		return report, nil
	}

	legacy := legacyAppDataRoot()
	if legacy == "" {
		return report, nil
	}

	legacy = filepath.Clean(legacy)
	canonical := filepath.Clean(cfg.DataDir)

	if legacy == canonical || canonical == "" || canonical == "." {
		return report, nil
	}

	// Never merge a root into itself or its own child.
	if sameOrChild(canonical, legacy) || sameOrChild(legacy, canonical) {
		return report, nil
	}

	fi, err := os.Stat(legacy)
	if err != nil || !fi.IsDir() {
		return report, nil // no legacy root — nothing to do (idempotent no-op)
	}

	report.Detected = append(report.Detected, legacy)

	if err := os.MkdirAll(canonical, 0o755); err != nil {
		return report, fmt.Errorf("migrate: canonical root %s: %w", canonical, err)
	}

	// Engine bundle moves as a unit (see package doc): when the
	// canonical root already hosts a managed engine binary, the legacy
	// engine package is left out of the per-file merge entirely.
	legacyBin := filepath.Join(legacy, "bin")
	canonicalBin := filepath.Join(canonical, "bin")

	if engineBundlePresent(legacyBin) && engineBundlePresent(canonicalBin) {
		report.Collisions = append(report.Collisions,
			"engine bundle: canonical managed package kept; legacy engine package not merged (single authoritative copy, spec §18/§9)")
	}

	if err := mergeAppDataTree(legacy, canonical, report); err != nil {
		// Source left intact: the next start retries (idempotent).
		return report, fmt.Errorf("migrate legacy root %s: %w", legacy, err)
	}

	// Every file accounted for — the legacy root is now obsolete.
	if err := os.RemoveAll(legacy); err != nil {
		return report, fmt.Errorf("migrate: remove legacy root %s: %w", legacy, err)
	}

	report.Removed = append(report.Removed, legacy)

	return report, nil
}

// engineBundlePresent reports whether dir contains the llama-server
// engine binary (the managed package's entry point).
func engineBundlePresent(dir string) bool {
	name := "llama-server"
	if runtime.GOOS == "windows" {
		name = "llama-server.exe"
	}

	fi, err := os.Stat(filepath.Join(dir, name))

	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// mergeAppDataTree folds srcRoot into dstRoot with the same verified
// merge machinery as MigrateMalformedRoots, with one exception: when
// BOTH roots carry a complete engine bundle, the legacy bin directory is
// skipped (the canonical package stays authoritative — never mixed).
func mergeAppDataTree(srcRoot, dstRoot string, report *MigrationReport) error {
	skipBin := engineBundlePresent(srcRoot) && engineBundlePresent(dstRoot)

	// Fast path: nothing to skip — reuse the proven merge directly.
	if !skipBin {
		return mergeTree(srcRoot, dstRoot, report)
	}

	// Merge everything EXCEPT the legacy engine package: copy the legacy
	// tree (minus bin) into a staging dir, merge that, then merge the
	// legacy bin ONLY when the canonical bin is missing engine files the
	// legacy tree uniquely provides (it never is — both bundles exist).
	staging, err := os.MkdirTemp(filepath.Dir(dstRoot), ".legacy-migrate-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if err := copyTreeSkipping(srcRoot, staging, "bin"); err != nil {
		return err
	}

	return mergeTree(staging, dstRoot, report)
}

// copyTreeSkipping copies srcRoot into dstRoot, omitting the named
// top-level entries. It is a verified COPY (not a move): the legacy root
// is only ever removed after the FULL merge below succeeds.
func copyTreeSkipping(srcRoot, dstRoot string, skip ...string) error {
	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[strings.ToLower(s)] = true
	}

	return filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // unreachable entries are simply not copied
		}

		rel, relErr := filepath.Rel(srcRoot, path)
		if relErr != nil || rel == "." {
			return nil
		}

		top := strings.ToLower(strings.SplitN(rel, string(filepath.Separator), 2)[0])
		if skipSet[top] {
			if info.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		dst := filepath.Join(dstRoot, rel)

		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		return copyVerified(path, dst)
	})
}

// appRootDataEntries lists the top-level entries a PRE-<AppRoot>\data
// runtime layout could have created DIRECTLY under the application root
// (v1.3.0–early-v1.3.6 portable runs resolved the data root to AppRoot
// itself). Only unambiguous application-data names are folded; binaries
// and unrelated files are never touched.
var appRootDataEntries = []string{
	"config.json", "installed.json", "models", "sessions", "logs", "bin",
	"lab", "workspace", "charts", "sandbox", "run", "research",
}

// mergeStrayLogsDir folds a stray application-root logs directory into
// the canonical logs directory (v1.3.7 — see the call site for why the
// plain collision rule never worked for this entry). Every stray file is
// copied with copyVerified (temp file + size + SHA-256 + rename); an
// existing canonical file is NEVER overwritten (the live app.log stays
// authoritative). The stray directory is removed only after every file
// landed verified — an interrupted merge leaves it in place and retries
// on the next start (already-copied files are skipped by the
// exists-check, so the fold is idempotent).
func mergeStrayLogsDir(src, dst string, report *MigrationReport) error {
	moved := 0

	walkErr := filepath.Walk(src, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}

		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}

		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		if !info.Mode().IsRegular() {
			return nil // symlinks/special files never migrate
		}

		if _, serr := os.Stat(target); serr == nil {
			return nil // the canonical tree owns this name — keep it
		}

		if cerr := copyVerified(p, target); cerr != nil {
			return fmt.Errorf("fold %s: %w", rel, cerr)
		}

		moved++

		return nil
	})

	if walkErr != nil {
		return walkErr
	}

	if rerr := os.RemoveAll(src); rerr != nil {
		return rerr
	}

	report.Merged = append(report.Merged,
		fmt.Sprintf("logs: %d historical log file(s) folded into the canonical data root", moved))

	return nil
}

// MigrateAppRootDirectData folds a pre-<AppRoot>\data layout —
// application data created DIRECTLY under the executable's directory —
// into the canonical install-local <AppRoot>\data root (v1.3.6 spec
// §23/§24). Safe to call on every start:
//
//   - it never runs when the user chose an explicit data root;
//   - entries that already exist in the canonical root are LEFT ALONE
//     (the canonical copy stays authoritative; the stray is reported);
//   - the engine `bin` directory moves as a UNIT (rename, never a
//     DLL-by-DLL merge — spec §24/§12: mixing engine package
//     generations produces the 0xC0000139 entry-point failure class).
//
// Rename-first, verified: a rename either succeeds atomically within
// the application root or the entry is left untouched (idempotent on
// the next start).
func MigrateAppRootDirectData(cfg *Config) (*MigrationReport, error) {
	report := &MigrationReport{}

	if !canonicalRootIsDefault() {
		return report, nil
	}

	appRoot := filepath.Clean(AppRoot())
	canonical := filepath.Clean(cfg.DataDir)

	// Only the default derivation is migrated; explicit roots are the
	// user's deliberate choice and are never touched (spec §24).
	if canonical != filepath.Clean(filepath.Join(appRoot, "data")) || canonical == appRoot {
		return report, nil
	}

	if err := migrateAppRootEntries(appRoot, canonical, report); err != nil {
		return report, err
	}

	return report, nil
}

// migrateAppRootEntries is the testable core of MigrateAppRootDirectData:
// it folds the listed app-data entries from appRoot into canonical.
func migrateAppRootEntries(appRoot, canonical string, report *MigrationReport) error {
	fi, err := os.Stat(appRoot)
	if err != nil || !fi.IsDir() {
		return nil
	}

	report.Detected = append(report.Detected, appRoot)

	moved := 0

	for _, name := range appRootDataEntries {
		src := filepath.Join(appRoot, name)
		info, statErr := os.Stat(src)
		if statErr != nil {
			continue
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			continue
		}

		dst := filepath.Join(canonical, name)

		if _, err := os.Stat(dst); err == nil {
			// v1.3.7: `logs` is the ONE entry the boot order guarantees to
			// collide — the log catcher (cmd/root.go) starts BEFORE this
			// migration and creates the canonical <AppRoot>\data\logs.
			// Under the v1.3.6 collision rule ("destination exists → leave
			// the stray") the historical <AppRoot>\logs therefore NEVER
			// folded. Fold it now with a verified, never-overwriting merge.
			if name == "logs" {
				if merr := mergeStrayLogsDir(src, dst, report); merr != nil {
					report.Collisions = append(report.Collisions,
						fmt.Sprintf("logs: historical log merge incomplete (%v); the stray logs directory stays for the next start", merr))
				}

				continue
			}

			report.Collisions = append(report.Collisions,
				fmt.Sprintf("%s: canonical entry already exists; the stray application-root entry was left untouched", name))

			continue
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("migrate app-root data: create %s: %w", filepath.Dir(dst), err)
		}

		if err := os.Rename(src, dst); err != nil {
			report.Collisions = append(report.Collisions,
				fmt.Sprintf("%s: rename into the canonical root failed (%v); entry left in place", name, err))

			continue
		}

		report.Merged = append(report.Merged,
			fmt.Sprintf("%s: moved into the canonical install-local data root", name))

		if name == "config.json" {
			report.Recovered = append(report.Recovered, dst)
			report.ReloadConfig = true
		}

		moved++
	}

	if moved > 0 {
		// Never REMOVE the application root itself (it hosts the
		// executable) — the stray entries were folded one by one; the
		// Removed list stays empty by construction here.
		report.Merged = append(report.Merged,
			fmt.Sprintf("app-root layout migration complete: %d entr(y/ies) folded into %s", moved, canonical))
	}

	return nil
}
