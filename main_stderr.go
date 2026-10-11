package main

import "os"

// stderrWriter is the destination for headless bootstrap errors; a var so
// tests can silence it if ever needed. Available in all build variants.
var stderrWriter = os.Stderr
