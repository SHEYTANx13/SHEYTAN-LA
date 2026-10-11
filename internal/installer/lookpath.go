package installer

import "os/exec"

// lookPath resolves an executable on PATH (thin wrapper kept as the single
// seam where discovery happens).
func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}
