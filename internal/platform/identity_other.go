//go:build !windows

package platform

// setAppUserModelID is a Windows-only concept; other platforms report the
// truth: unsupported.
func setAppUserModelID(id string) error {
	return ErrUnsupportedPlatform
}
