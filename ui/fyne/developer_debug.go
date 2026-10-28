//go:build !headless

package fyneui

import "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"

func setDeveloperDebugLive(on bool) {
	logging.SetDeveloperDebug(on)
	if on {
		logging.Default().Info("logging", "developer debug logging enabled from Settings")
	} else {
		logging.Default().Info("logging", "developer debug logging disabled from Settings")
	}
}
