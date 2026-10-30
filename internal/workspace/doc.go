// Package workspace documentation lives in server.go. This file carries
// the package-level lifecycle error shared by the server surface.
package workspace

import "errors"

// errAlreadyRunning is returned by Start when the server is running.
// The UI layer treats it as a no-op re-open, never as a failure.
var errAlreadyRunning = errors.New("the Web Workspace server is already running")
