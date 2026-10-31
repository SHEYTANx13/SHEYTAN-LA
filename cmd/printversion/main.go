// printversion — the CI/packaging version resolver.
//
// ONE authority: internal/config.AppVersion. This command exists so the
// build-desktop workflow can resolve the product version WITHOUT compiling
// the Fyne desktop UI: run #54 (40394318382) failed its Linux
// version-resolution step and its audit stress gate for the same root
// cause — `go run ./cmd/sheytan-local-agent version` pulls in ui/fyne via
// the desktop entrypoints, so the resolution step needed the full CGO
// graphics development stack (X11/OpenGL/Wayland headers) on a fresh
// runner that had not installed it yet.
//
// This command imports ONLY internal/config (pure Go, zero CGO), so it
// resolves in about one second on any host, with or without a graphics
// toolchain, before the Fyne prerequisites are installed.
//
// Output contracts (both deterministic, documented, fail-closed):
//
//	printversion --plain   → "1.11.0" exactly (no prefix, no padding)
//	printversion --json    → {"app":"SHEYTAN-Local-Agent","version":"1.11.0"}
//
// Any malformed version (not MAJOR.MINOR.PATCH with numeric components)
// exits 2 — the caller must never fall back to a hardcoded version, and
// this helper must never print a guessable default.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

// semverRe pins the exact release shape the workflow and the packaging
// metadata accept: exactly three numeric dot-separated components. No
// pre-release/build suffixes: the product has never shipped one, and a
// permissive pattern here would let "1.11.0-rc1" reach artifact names.
var semverRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// validateVersion is the fail-closed gate over the resolved authority
// value. Exposed for tests: the shipped AppVersion is a const, so the
// malformed paths are exercised through this seam.
func validateVersion(v string) error {
	if !semverRe.MatchString(v) {
		return fmt.Errorf(
			"internal/config.AppVersion %q is not a valid MAJOR.MINOR.PATCH version — refusing to resolve (fail closed)",
			v,
		)
	}
	return nil
}

// run executes one resolution against the given authority value.
// Returns the process exit code; never panics, never prints a fallback.
func run(version string, args []string, out, errW io.Writer) int {
	fs := flag.NewFlagSet("printversion", flag.ContinueOnError)
	plain := fs.Bool("plain", false, "print the bare semantic version (no prefix)")
	js := fs.Bool("json", false, "print a machine-readable JSON object")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(errW, "printversion: bad flags")
		return 2
	}

	v := strings.TrimSpace(version)
	if err := validateVersion(v); err != nil {
		fmt.Fprintf(errW, "printversion: %v\n", err)
		return 2
	}

	switch {
	case *plain && *js:
		fmt.Fprintln(errW, "printversion: --plain and --json are mutually exclusive")
		return 2
	case *plain:
		fmt.Fprintln(out, v)
	case *js:
		encoded, err := json.Marshal(map[string]string{
			"app":     config.AppName,
			"version": v,
		})
		if err != nil {
			fmt.Fprintf(errW, "printversion: encode JSON: %v\n", err)
			return 2
		}
		fmt.Fprintln(out, string(encoded))
	default:
		fmt.Fprintln(errW, "printversion: pass --plain or --json")
		return 2
	}
	return 0
}

func main() {
	os.Exit(run(config.AppVersion, os.Args[1:], os.Stdout, os.Stderr))
}
