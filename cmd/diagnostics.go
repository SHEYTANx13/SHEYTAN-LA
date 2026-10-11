package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
)

// v1.1.4: Diagnostics now lives where its name says (it was buried in
// license.go next to the Logs helper).

// Diagnostics exports the full diagnostics zip (logs + stats + redacted
// config + sysinfo) for bug reports / update analysis.
func Diagnostics(cfg *config.Config, args []string) int {
	m := logging.Default()
	if !m.Enabled() {
		rm, err := logging.New(cfg.LogsDir())
		if err != nil {
			fmt.Fprintln(os.Stderr, "logs:", err)
			return 1
		}
		m = rm
	}
	out := filepath.Join(cfg.DataDir, "diagnostics", "sheytan-diagnostics.zip")
	if len(args) > 0 && args[0] != "" {
		out = args[0]
	}
	// sysinfo snapshot
	extra := map[string]string{}
	if data, err := json.MarshalIndent(sysinfo.Probe(), "", "  "); err == nil {
		tmp := filepath.Join(os.TempDir(), "sheytan-sysinfo.json")
		if os.WriteFile(tmp, data, 0o644) == nil {
			extra["sysinfo.json"] = tmp
		}
	}
	path, err := m.Diagnostics(out, cfg.ConfigPath(), extra)
	if err != nil {
		fmt.Fprintln(os.Stderr, "diagnostics:", err)
		return 1
	}
	fmt.Println("Diagnostics bundle written to:", path)
	fmt.Println("  (logs, stats, crashes, sysinfo, redacted config)")
	return 0
}
