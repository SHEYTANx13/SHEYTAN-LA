//go:build webworkspace && !mobile

package webworkspace

import "time"

// DefaultProvider under the tagged desktop build: the Wails adapter.
func DefaultProvider() Provider {
	return NewWailsProvider()
}

func timeNow() time.Time { return time.Now() }
