// modelpaths.go — v1.6.1: model-path repair across runtime-root migrations.
//
// When a data root is migrated (the 1.3.5-era %LOCALAPPDATA%\SHEYTAN-LA
// AppData root, the legacy ~/.sheytan home directory, or a malformed
// v1.2.9 token tree), the FILES move — but config.json fields that name
// model files keep pointing at the OLD root:
//
//      "model":        "C:\\Users\\X\\AppData\\Local\\SHEYTAN-LA\\models\\foo.gguf"
//      "draftModel":   "...\\SHEYTAN-LA\\models\\foo-draft.gguf"
//      "visionMmproj": "...\\SHEYTAN-LA\\models\\foo-mmproj.gguf"
//
// The engine then refuses to start ("model not found") even though the
// file sits in the managed models directory. The repair below re-anchors
// those fields onto the canonical data root, PRESERVING the relative
// layout the migrations themselves preserve.
//
// The rule is deliberately narrow:
//
//   - ONLY Model, DraftModel and VisionMMProj are touched;
//   - ONLY paths that point INSIDE a retired SHEYTAN runtime root are
//     repaired (prefix rewrite);
//   - an arbitrary external path (a custom models folder on another
//     drive, a Downloads directory) is NEVER rewritten — external model
//     paths are a supported posture and stay the user's choice;
//   - a path that merely carries an unresolved environment token is
//     expanded once, exactly like every other path field.
package config

import (
        "fmt"
        "os"
        "path/filepath"
        "runtime"
        "strings"
)

// retiredModelPathFields is the exact repair surface.
var retiredModelPathFields = []struct {
        get  func(*Config) string
        set  func(*Config, string)
        name string
}{
        {
                name: "model",
                get:  func(c *Config) string { return c.Model },
                set:  func(c *Config, v string) { c.Model = v },
        },
        {
                name: "draftModel",
                get:  func(c *Config) string { return c.DraftModel },
                set:  func(c *Config, v string) { c.DraftModel = v },
        },
        {
                name: "visionMmproj",
                get:  func(c *Config) string { return c.VisionMMProj },
                set:  func(c *Config, v string) { c.VisionMMProj = v },
        },
}

// RepairRetiredModelPaths re-anchors model-file fields that still point
// inside a retired SHEYTAN runtime root onto the canonical data root,
// preserving the relative path (the migrations preserve layout). Returns
// one report line per repair. Idempotent: an already-canonical path never
// matches a retired root.
func RepairRetiredModelPaths(cfg *Config) []string {
        if cfg == nil || strings.TrimSpace(cfg.DataDir) == "" {
                return nil
        }

        roots := retiredRootsFor(cfg.DataDir)
        if len(roots) == 0 {
                return nil
        }

        var notes []string

        for _, field := range retiredModelPathFields {
                raw := strings.TrimSpace(field.get(cfg))
                if raw == "" {
                        continue
                }

                // An unresolved environment token gets the standard one-shot
                // expansion; if the expansion lands inside a retired root the
                // re-anchoring below still applies.
                if HasEnvToken(raw) {
                        expanded := ExpandEnvPath(raw)
                        if HasEnvToken(expanded) {
                                notes = append(notes, fmt.Sprintf(
                                        "%s=%q contains unresolved environment references; the value is kept verbatim for manual repair",
                                        field.name, raw,
                                ))
                                continue
                        }
                        raw = expanded
                }

                // Relative model names (plain "foo.gguf") are already portable —
                // ResolveModelPath resolves them against ModelsDir.
                if !filepath.IsAbs(raw) {
                        continue
                }

                for _, root := range roots {
                        rel, inside := pathInsideRoot(raw, root)
                        if !inside {
                                continue
                        }

                        repaired := filepath.Join(cfg.DataDir, rel)

                        // A DIRECT models-dir citizen collapses to its base name —
                        // ResolveModelPath resolves names against ModelsDir, and a
                        // plain name survives further data-root moves. Nested layouts
                        // (models/subdir/x.gguf) keep the full re-anchored path so the
                        // subdirectory is not lost.
                        if rel == filepath.Join("models", filepath.Base(rel)) {
                                repaired = filepath.Base(rel)
                        }

                        if repaired == field.get(cfg) {
                                continue
                        }

                        field.set(cfg, repaired)
                        notes = append(notes, fmt.Sprintf(
                                "%s pointed inside the retired runtime root %s; re-anchored to %q",
                                field.name, root, repaired,
                        ))
                        break
                }
        }

        return notes
}

// retiredRootsFor lists the retired runtime roots relevant to this
// machine (canonical root excluded; empty when none apply).
func retiredRootsFor(canonical string) []string {
        var roots []string

        if legacy := legacyAppDataRoot(); legacy != "" {
                roots = append(roots, filepath.Clean(legacy))
        }

        if home, err := os.UserHomeDir(); err == nil && home != "" {
                legacy := filepath.Join(home, ".sheytan")
                if legacy != "" {
                        roots = append(roots, filepath.Clean(legacy))
                }
        }

        // The canonical root itself is never "retired".
        out := roots[:0]
        for _, r := range roots {
                if !samePath(r, canonical) {
                        out = append(out, r)
                }
        }
        return out
}

// pathInsideRoot reports whether abs is inside root (strictly), returning
// the relative path when it is. Windows path comparison is
// case-insensitive, matching the platform's filesystem semantics.
func pathInsideRoot(abs, root string) (string, bool) {
        abs = filepath.Clean(abs)
        root = filepath.Clean(root)

        var rel string
        var err error

        if runtime.GOOS == "windows" {
                rel, err = filepath.Rel(root, abs)
                if err != nil {
                        return "", false
                }
                // Case-insensitive prefix validation: Rel only failed on different
                // drives; same-drive case differences produce a valid relative path
                // but the prefix check must not depend on case.
                rootLen := len(root)
                if len(abs) > rootLen && strings.EqualFold(abs[:rootLen], root) {
                        // fall through to the escape check below
                } else if !(rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)) {
                        return "", false
                }
        } else {
                rel, err = filepath.Rel(root, abs)
                if err != nil {
                        return "", false
                }
        }

        if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
                return "", false
        }

        return rel, true
}

// samePath compares two cleaned paths with platform-appropriate casing.
func samePath(a, b string) bool {
        a = filepath.Clean(a)
        b = filepath.Clean(b)

        if runtime.GOOS == "windows" {
                return strings.EqualFold(a, b)
        }
        return a == b
}
