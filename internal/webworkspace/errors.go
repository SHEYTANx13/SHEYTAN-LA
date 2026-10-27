package webworkspace

import "errors"

// errAlreadyRunning — a second Start on a live host is a typed state
// conflict, never a silent double-listener.
var errAlreadyRunning = errors.New("web workspace host is already running")
