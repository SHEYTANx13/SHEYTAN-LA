package logging

import (
	"os"
	"strings"
	"sync/atomic"
)

// developerDebug gates DEBUG-level app.log lines. Default OFF so production
// and normal users are not flooded; enable via Settings → Advanced →
// "Developer debug logging" or env SHEYTAN_DEVELOPER_DEBUG=1|true|yes.
var developerDebug atomic.Bool

func init() {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("SHEYTAN_DEVELOPER_DEBUG")))
	switch v {
	case "1", "true", "yes", "on":
		developerDebug.Store(true)
	}
}

// SetDeveloperDebug turns DEBUG sink writes on or off at runtime.
func SetDeveloperDebug(on bool) {
	developerDebug.Store(on)
}

// DeveloperDebug reports whether DEBUG lines are currently enabled.
func DeveloperDebug() bool {
	return developerDebug.Load()
}

// Debug logs to app.log only when developer debug is enabled (default OFF).
func (m *Manager) Debug(category, format string, args ...interface{}) {
	if !DeveloperDebug() {
		return
	}
	m.log("DEBUG", category, format, args...)
}
