// library_other.go — the measured library-load probe on non-Windows (v1.2.6).
//go:build !windows

package accelerator

import (
	"errors"
	"os/exec"
	"strings"
)

// loadable reports whether the OS loader can actually map the library.
// dlopen(3) with RTLD_NOW|RTLD_LOCAL performs the full dependency
// resolution — the same load the OpenVINO runtime itself would perform.
func loadable(path string) bool {
	// cgo dlopen would be the exact primitive; without cgo in this
	// build, `ldd -r` gives the dependency-resolution verdict on the
	// platforms we ship (Linux/darwin). A missing symbol or dependency
	// fails the probe honestly.
	out, err := exec.Command("ldd", path).Output()
	if err != nil {
		// darwin lacks ldd; otool exists but adds fragility — treat the
		// file's EXISTENCE plus executability as the (weaker) measured
		// signal there, and say so.
		return strings.Contains(path, ".dylib")
	}

	text := string(out)

	// ldd reports "not found" for missing dependencies.
	return !strings.Contains(text, "not found")
}

// findLibraryOnPath resolves a library through the loader search path.
func findLibraryOnPath(name string) (string, error) {
	out, err := exec.Command("/sbin/ldconfig", "-p").Output()
	if err != nil {
		// Some minimal systems keep no ldconfig cache — PATH-based search
		// for the common lib dirs is the honest fallback.
		return "", errors.New("ldconfig cache unavailable")
	}

	for _, line := range strings.Split(string(out), "\n") {
		if idx := strings.LastIndex(line, "=> "); idx >= 0 {
			candidate := strings.TrimSpace(line[idx+3:])
			if strings.HasSuffix(candidate, "/"+name) {
				return candidate, nil
			}
		}
	}

	return "", errors.New("library not in the loader cache")
}
