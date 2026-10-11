// paths.go — v1.3.0 canonical runtime path resolution.
//
// # THE DEFECT THIS FILE ELIMINATES
//
// The v1.2.9 NSIS installer declared the user data location as a
// machine-level REG_EXPAND_SZ value:
//
//	WriteRegExpandStr HKLM ...\Environment "SHEYTAN_DATA_DIR" "%LOCALAPPDATA%\SHEYTAN-LA"
//
// LOCALAPPDATA is a PER-USER profile variable; machine environment values
// are expanded before user variables exist, so Windows hands the process
// the LITERAL string "%LOCALAPPDATA%\SHEYTAN-LA" through os.Getenv.
// config.Default()/applyEnv then accepted the raw string as DataDir. A
// "%..." string is not an absolute path, so every filepath.Join produced
// the observed malformed tree:
//
//	<install-root>\%LOCALAPPDATA%\SHEYTAN-LA\models
//	<install-root>\SHEYTAN-LA\%LOCALAPPDATA%\SHEYTAN-LA\models
//
// THE CONTRACT (v1.3.0, amended v1.3.6 spec §23 — INSTALL-LOCAL ROOT)
//
// Exactly ONE authoritative resolution mechanism, in this package:
//
//  1. the application root is the executable's directory (AppRoot);
//     the CANONICAL DATA ROOT is <AppRoot>\data — a dedicated subtree
//     of the installed application root (models/sessions/logs/bin/
//     workspace live under it). The v1.3.0 rule used AppRoot itself;
//     v1.3.6 nests the data one level so an installed app (Program
//     Files or portable) keeps its writable data tree separate from
//     the binaries, matching the NSIS installer's <AppRoot>\data
//     directory contract. A one-time migration folds a pre-existing
//     AppRoot-level layout into <AppRoot>\data (see
//     MigrateAppRootDirectData);
//  2. environment-variable references are expanded ONCE, here — never
//     persisted and never passed through unresolved;
//  3. a value that still contains an unresolved %TOKEN% is REJECTED and
//     the canonical root is used instead (the failure is reported to the
//     caller so it can be logged with context);
//  4. every derived path (models, sessions, logs, workspace, …) is
//     joined from the canonical root and normalized (absolute, cleaned);
//  5. relative overrides resolve against the application root — never
//     the current working directory — so startup is CWD-independent.
//
// The SHEYTAN_DATA_DIR override remains a legitimate engineering seam
// (tests, portable layouts, CI); it must simply resolve to a real
// absolute path before use.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// envTokenPattern matches one %-delimited environment token as it appears
// in raw Windows registry values ("%LOCALAPPDATA%", "%PROGRAMDATA%").
var envTokenPattern = regexp.MustCompile(`%[A-Za-z_][A-Za-z0-9_()\-]*%`)

// dollarTokenPattern matches unix-style references ($VAR, ${VAR}) so the
// same rejection rule covers hand-edited configurations on any platform.
var dollarTokenPattern = regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_]*\}?`)

// HasEnvToken reports whether the path still contains a literal
// environment-variable reference ("%LOCALAPPDATA%", "$HOME", "${X}").
// A persisted runtime path must NEVER satisfy this after normalization.
func HasEnvToken(path string) bool {
	if envTokenPattern.MatchString(path) {
		return true
	}
	return dollarTokenPattern.MatchString(path)
}

// ExpandEnvPath expands every %TOKEN% / $TOKEN / ${TOKEN} reference using
// the current process environment and returns the expanded value.
//
// Unresolvable tokens are left in place; the caller decides whether the
// partial expansion is usable. Empty input expands to empty. The
// double-% escape form Windows uses for literal percent signs
// ("%%") is preserved verbatim (it is not a variable reference).
func ExpandEnvPath(raw string) string {
	if raw == "" {
		return ""
	}

	// Windows-style %TOKEN%: expand every resolvable token, leave
	// unresolvable ones for the caller to detect and reject.
	expanded := envTokenPattern.ReplaceAllStringFunc(raw, func(token string) string {
		name := strings.Trim(token, "%")
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		return token
	})

	// Unix-style $TOKEN / ${TOKEN}.
	expanded = os.Expand(expanded, func(name string) string {
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		// os.Expand's contract: returning "" removes the token. We need
		// unresolvable references to stay VISIBLE so HasEnvToken can
		// reject the result; reinsert a detectable marker.
		return "$" + name
	})

	return expanded
}

// defaultDataRoot is the CANONICAL data root when no override is set:
// a dedicated `data` subtree under the application root (v1.3.6 spec
// §23 — the installed runtime's one canonical root, shared verbatim
// with the NSIS installer).
func defaultDataRoot() string {
	return filepath.Join(AppRoot(), "data")
}

// ResolveRoot resolves the canonical application-data root.
//
// Resolution order:
//  1. SHEYTAN_DATA_DIR (expanded; must resolve to a real absolute or
//     app-root-relative path with NO surviving env tokens);
//  2. the canonical install-local root <AppRoot>\data.
//
// A path that is relative resolves against the application root, never
// the current working directory. The returned path is absolute and
// cleaned. When the environment override is unusable, fallback=true is
// returned together with the reason so the caller can log it with
// actionable context (a silent fallback would hide the misconfiguration).
func ResolveRoot() (root string, fallback bool, reason string) {
	raw, present := os.LookupEnv("SHEYTAN_DATA_DIR")
	if !present || strings.TrimSpace(raw) == "" {
		return defaultDataRoot(), false, ""
	}

	expanded := ExpandEnvPath(strings.TrimSpace(raw))

	if HasEnvToken(expanded) {
		return defaultDataRoot(), true, fmt.Sprintf(
			"SHEYTAN_DATA_DIR=%q contains an unresolved environment-variable reference (%s); using the canonical install-local data root",
			raw, expanded,
		)
	}

	// A literal "%"-only escape ("%%") is pathological; reject it too.
	if strings.Contains(expanded, "%") {
		return defaultDataRoot(), true, fmt.Sprintf(
			"SHEYTAN_DATA_DIR=%q expanded to %q which still contains a raw '%%' — refusing; using the canonical install-local data root",
			raw, expanded,
		)
	}

	resolved := AbsAgainstAppRoot(expanded)

	// Defense in depth: the resolved root must never nest a token-shaped
	// directory name ("%LOCALAPPDATA%") — the v1.2.9 malformed layout.
	if isMalformedRootPath(resolved) {
		return defaultDataRoot(), true, fmt.Sprintf(
			"SHEYTAN_DATA_DIR=%q resolved to the malformed path %q; using the canonical install-local data root",
			raw, resolved,
		)
	}

	return resolved, false, ""
}

// AbsAgainstAppRoot makes path absolute against the application root
// (not the CWD) and cleans it. Empty input returns the application root.
func AbsAgainstAppRoot(path string) string {
	if strings.TrimSpace(path) == "" {
		return AppRoot()
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(AppRoot(), path)
}

// isMalformedRootPath detects the v1.2.9 defect signature: any path
// component that is a literal environment token ("%LOCALAPPDATA%") or a
// doubled product-name nesting ("...\SHEYTAN-LA\SHEYTAN-LA\...") that
// appears when a token-relative DataDir was joined onto an install root
// already named SHEYTAN-LA.
func isMalformedRootPath(path string) bool {
	if path == "" {
		return false
	}
	for _, part := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' }) {
		if part == "" || part == "." {
			continue
		}
		if envTokenPattern.MatchString(part) {
			return true
		}
	}
	// Doubled product nesting: the install root's base name repeated as
	// the first derived component (e.g. C:\...\SHEYTAN-LA\SHEYTAN-LA).
	base := filepath.Base(filepath.Clean(path))
	if strings.EqualFold(base, AppShortName) || strings.EqualFold(base, AppName) {
		parent := filepath.Base(filepath.Dir(filepath.Clean(path)))
		if strings.EqualFold(parent, AppShortName) || strings.EqualFold(parent, AppName) {
			return true
		}
	}
	return false
}

// normalizeRuntimePaths canonicalizes every persisted path field of the
// configuration IN PLACE:
//
//   - env references are expanded once;
//   - a field whose expansion leaves an unresolved token, or that
//     resolves to a malformed nested root, falls back to the canonical
//     derivation from DataDir and the fallback is reported;
//   - every field ends absolute and cleaned.
//
// It runs at the end of Load (after applyEnv) so no subsystem ever
// observes a raw "%..." path. Reports are returned (not logged here —
// config.Load runs before the log catcher boots in some entrypoints;
// cmd.Root publishes them once logging is up).
func normalizeRuntimePaths(cfg *Config) []string {
	var reports []string

	// --- DataDir ---------------------------------------------------------
	if HasEnvToken(cfg.DataDir) {
		expanded := ExpandEnvPath(cfg.DataDir)
		if HasEnvToken(expanded) {
			reports = append(reports, fmt.Sprintf(
				"dataDir=%q contains unresolved environment references; using the canonical install-local data root",
				cfg.DataDir,
			))
			cfg.DataDir = defaultDataRoot()
		} else {
			cfg.DataDir = AbsAgainstAppRoot(expanded)
		}
	} else if cfg.DataDir != "" {
		cfg.DataDir = AbsAgainstAppRoot(cfg.DataDir)
	}

	if isMalformedRootPath(cfg.DataDir) {
		reports = append(reports, fmt.Sprintf(
			"dataDir=%q is a malformed nested root; using the canonical install-local data root",
			cfg.DataDir,
		))
		cfg.DataDir = defaultDataRoot()
	}

	// --- derived roots ---------------------------------------------------
	if HasEnvToken(cfg.ModelsDir) {
		cfg.ModelsDir, _ = resolveDerived(cfg.DataDir, cfg.ModelsDir, "models", "modelsDir", &reports)
	} else {
		cfg.ModelsDir = deriveIfEmpty(cfg.ModelsDir, cfg.DataDir, "models")
	}

	if HasEnvToken(cfg.SessionsDir) {
		cfg.SessionsDir, _ = resolveDerived(cfg.DataDir, cfg.SessionsDir, "sessions", "sessionsDir", &reports)
	} else {
		cfg.SessionsDir = deriveIfEmpty(cfg.SessionsDir, cfg.DataDir, "sessions")
	}

	if HasEnvToken(cfg.LabWorkspaceRoot) {
		cfg.LabWorkspaceRoot, _ = resolveDerived(cfg.DataDir, cfg.LabWorkspaceRoot, filepath.Join("lab", "workspaces"), "labWorkspaceRoot", &reports)
	} else {
		cfg.LabWorkspaceRoot = deriveIfEmpty(cfg.LabWorkspaceRoot, cfg.DataDir, filepath.Join("lab", "workspaces"))
	}

	// --- user project workspace -----------------------------------------
	if strings.TrimSpace(cfg.WorkspaceRoot) != "" {
		resolved := resolveWorkspaceField(cfg.WorkspaceRoot)
		if resolved == "" {
			reports = append(reports, fmt.Sprintf(
				"workspaceRoot=%q contains unresolved environment references; keeping the default workspace",
				cfg.WorkspaceRoot,
			))
			cfg.WorkspaceRoot = ""
		} else {
			cfg.WorkspaceRoot = resolved
		}
	}

	return reports
}

// resolveDerived resolves one derived-directory field: expand once; if
// the expansion still holds a token, fall back to the canonical
// derivation under dataDir. The bool reports whether the fallback fired.
func resolveDerived(dataDir, raw, sub, field string, reports *[]string) (string, bool) {
	expanded := ExpandEnvPath(raw)
	if HasEnvToken(expanded) {
		*reports = append(*reports, fmt.Sprintf(
			"%s=%q contains unresolved environment references; deriving %s from the canonical root",
			field, raw, sub,
		))
		return filepath.Join(dataDir, sub), true
	}
	return AbsAgainstAppRoot(expanded), false
}

// resolveWorkspaceField expands and absolutizes the user's project
// workspace root. Returns "" when the value is unusable (unresolved
// token) — callers keep the default workspace in that case.
func resolveWorkspaceField(raw string) string {
	expanded := ExpandEnvPath(strings.TrimSpace(raw))
	if expanded == "" || HasEnvToken(expanded) {
		return ""
	}
	return AbsAgainstAppRoot(expanded)
}

// deriveIfEmpty keeps an explicitly-set absolute value (cleaned) and
// derives from dataDir otherwise.
func deriveIfEmpty(value, dataDir, sub string) string {
	if strings.TrimSpace(value) != "" {
		return AbsAgainstAppRoot(value)
	}
	return filepath.Join(dataDir, sub)
}

// EnvDataDirRaw returns the raw SHEYTAN_DATA_DIR value (for diagnostics
// only — never use it as a path).
func EnvDataDirRaw() string {
	return os.Getenv("SHEYTAN_DATA_DIR")
}
