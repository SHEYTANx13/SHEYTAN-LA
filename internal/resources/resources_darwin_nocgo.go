//go:build darwin && !cgo

package resources

// procRAMImpl (darwin, no cgo): the Mach task probe is unavailable in
// this build configuration. The typed unsupported result is the honest
// answer — callers must render "unknown", never a zero value.
func procRAMImpl(pid int) (int64, error) {
	return 0, ErrProcRAMUnsupported
}
