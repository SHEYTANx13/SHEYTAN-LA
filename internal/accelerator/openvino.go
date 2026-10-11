// openvino.go — measured OpenVINO runtime availability (v1.2.6).
//
// Detection is by RUNTIME LOAD, not by name: on Windows we attempt to
// LoadLibrary the OpenVINO runtime DLL from the engine directory (where a
// bundled runtime would live) and from the PATH; elsewhere we look for the
// shared library next to the engine binary. A "Detected" result means the
// OS loader could actually map the library — the strongest cheap signal
// available without importing the OpenVINO Go bindings (which the project
// does not vendor).
//
// This is deliberately conservative: absence is decisive (the NPU gate
// fails closed), presence is necessary-but-not-sufficient (the accelerator
// resolver still requires the measured benchmark gate before NPU wins).
package accelerator

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// openvinoLibraryName is the runtime DLL name per platform.
func openvinoLibraryName() string {
	switch runtime.GOOS {
	case "windows":
		return "openvino.dll"
	case "darwin":
		return "libopenvino.dylib"
	default:
		return "libopenvino.so"
	}
}

// DetectOpenVINO measures whether the OpenVINO runtime is loadable from
// the engine directory or the system search path. engineDir may be empty
// (PATH-only probe).
func DetectOpenVINO(engineDir string) OpenVINOStatus {
	lib := openvinoLibraryName()

	candidates := []string{}

	if dir := strings.TrimSpace(engineDir); dir != "" {
		candidates = append(candidates, filepath.Join(dir, lib))
	}

	// PATH probe: find the library via the OS search semantics.
	if path, err := findLibraryOnPath(lib); err == nil {
		candidates = append(candidates, path)
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}

		// The file exists — try the actual load. This is the measured gate.
		if loadable(candidate) {
			return OpenVINOStatus{
				RuntimePresent: true,
				Detail:         fmt.Sprintf("runtime loadable at %s", candidate),
			}
		}

		return OpenVINOStatus{
			RuntimePresent: false,
			Detail:         fmt.Sprintf("runtime present at %s but not loadable (architecture mismatch?)", candidate),
		}
	}

	return OpenVINOStatus{
		RuntimePresent: false,
		Detail:         "OpenVINO runtime not found beside the engine or on the library path",
	}
}
