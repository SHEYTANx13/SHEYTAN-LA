//go:build !webworkspace || mobile

package webworkspace

import "time"

// DefaultProvider is the build-time selected provider outside the tagged
// desktop build: typed unsupported, reason surfaced, nothing pretended.
func DefaultProvider() Provider {
	return UnsupportedProvider{Reason: ErrUnsupported.Error()}
}

func timeNow() time.Time { return time.Now() }
