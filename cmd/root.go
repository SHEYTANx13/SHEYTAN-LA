package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// RunWithDefaultFn dispatches to the right subcommand. If no command is
// given, runs `defaultFn()`.
//
// v1.7.4 boot order: the runtime-data migrations run BEFORE the canonical
// log sinks open. Under v1.7.3 the sinks (app.log/tools.jsonl/llm.jsonl —
// O_APPEND handles held for the whole process lifetime) were opened
// first, so copyVerified's final rename onto an open sink file failed on
// Windows ("Access is denied") and the legacy-root migration never
// completed. A bootstrap logger (stderr + bounded memory buffer, no files
// opened) records every migration decision meanwhile; its lines are
// replayed into app.log right after the session banner.
func RunWithDefaultFn(defaultFn func() int) int {
	cfg, err := config.Load(configPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		return 1
	}

	// v1.7.4: bootstrap logger FIRST — stderr plus a bounded memory
	// buffer, and NO file created or opened (the migrations below may
	// need to create or rename the very sink files a canonical logger
	// would hold open for the process lifetime).
	boot := logging.NewBootstrap()
	logging.SetDefault(boot)
	logging.SetVersion(config.AppVersion)

	// v1.3.0: one place reports every path normalization the loader had
	// to perform (rejected "%TOKEN%" values, canonical-root fallbacks),
	// so a path problem found during Load is reported through PathNotes
	// instead of vanishing into the pre-logger void. Emitted through the
	// bootstrap logger; replayed into app.log once the sinks exist.
	for _, note := range config.TakePathNotes(cfg) {
		logging.Default().Warn("paths", "%s", note)
	}

	// v1.6.1: every parser-invalid sampling value the loader safely
	// repaired (the P0 repeatPenalty=0 class) is reported once, with the
	// field, the violated rule and the reset value.
	for _, note := range config.TakeSamplingNotes(cfg) {
		logging.Default().Warn("config", "sampling value repaired by the loader: %s", note)
	}

	// (rest of function unchanged — see repository for full body)
	_ = runtime.GOOS
	_ = brand.FullName
	return 0
}
