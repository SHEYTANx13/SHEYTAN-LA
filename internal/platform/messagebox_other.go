//go:build !windows

package platform

// MessageBox is a Windows-only concept (user32!MessageBoxW); other
// platforms report the truth: unsupported — the same honest no-op rule
// every platform operation in this package follows (identity_other.go).
// The named-step ERROR log line remains the boot-failure catcher on
// every platform; the message box is an additional surface, never a
// replacement.
func MessageBox(title, text string) error {
	_ = title
	_ = text
	return ErrUnsupportedPlatform
}
