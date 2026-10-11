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

        // v1.3.6 (spec §23/§24): fold a pre-<AppRoot>\data portable layout
        // (application data created DIRECTLY under the executable's
        // directory) into the canonical install-local data root BEFORE any
        // other migration touches derived trees. Rename-first, verified,
        // never runs when an explicit data-root override is set.
        appRootReport, appRootErr := config.MigrateAppRootDirectData(cfg)
        if appRootErr != nil {
                logging.Default().Error("paths", "app-root data migration incomplete (will retry on next start): %v", appRootErr)
        }
        for _, line := range config.LogMigrationNotes(nil, appRootReport) {
                logging.Default().Info("paths", "%s", line)
        }

        if appRootReport != nil && appRootReport.ReloadConfig {
                if reloaded, rerr := config.Load(configPath()); rerr == nil {
                        cfg = reloaded
                        logging.Default().Info("paths", "configuration folded from the application root into the install-local data root and re-loaded")
                } else {
                        logging.Default().Warn("paths", "folded configuration could not be re-loaded: %v (defaults remain active)", rerr)
                }
        }

        // v1.3.0: fold v1.2.9 malformed runtime trees
        // (<root>\%LOCALAPPDATA%\SHEYTAN-LA, doubled SHEYTAN-LA nesting)
        // into the canonical root — models/sessions first, verified, then the
        // source tree removed. Restart-safe: an interrupted pass is retried
        // on the next boot.
        report, migrateErr := config.MigrateMalformedRoots(cfg)
        if migrateErr != nil {
                logging.Default().Error("paths", "runtime root migration incomplete (will retry on next start): %v", migrateErr)
        }
        for _, line := range config.LogMigrationNotes(nil, report) {
                logging.Default().Info("paths", "%s", line)
        }

        // A config.json recovered from a malformed tree: re-Load so the
        // recovered settings (model, sessions, research) actually drive this
        // run — with the same normalization guarantees.
        if report != nil && report.ReloadConfig {
                if reloaded, rerr := config.Load(configPath()); rerr == nil {
                        cfg = reloaded
                        for _, note := range config.TakePathNotes(cfg) {
                                logging.Default().Warn("paths", "%s", note)
                        }
                        logging.Default().Info("paths", "configuration recovered from a malformed runtime root and re-loaded")
                } else {
                        logging.Default().Warn("paths", "recovered configuration could not be re-loaded: %v (defaults remain active)", rerr)
                }
        }

        // v1.3.6 (spec §18): fold the 1.3.5-era AppData data root
        // (%LOCALAPPDATA%\SHEYTAN-LA — written by the old installer's
        // SHEYTAN_DATA_DIR env var) into the canonical <AppRoot>\data
        // root. Hash-verified, restart-safe, engine bundle as a unit,
        // legacy root removed only after full verification. Runs ONLY
        // when no explicit data-root override is set.
        appDataReport, appDataErr := config.MigrateLegacyAppDataRoot(cfg)
        if appDataErr != nil {
                logging.Default().Error("paths", "legacy AppData migration incomplete (will retry on next start): %v", appDataErr)
        }
        for _, line := range config.LogMigrationNotes(nil, appDataReport) {
                logging.Default().Info("paths", "%s", line)
        }

        if err := cfg.EnsureDirs(); err != nil {
                logging.Default().Error("boot", "ensure dirs: %v", err)
        }

        // v1.7.4: migrations are done — only NOW open the canonical sinks
        // (app.log, tools.jsonl, llm.jsonl, crashes/), with the FINAL config
        // so a recovered/reloaded configuration drives the logs dir.
        mgr, err := logging.New(cfg.LogsDir())
        if err != nil {
                // Keep the bootstrap logger installed: diagnostics stay on
                // stderr instead of vanishing into a no-op manager.
                logging.Default().Error("boot", "log catcher: %v", err)
        } else {
                logging.SetDefault(mgr)
                defer mgr.Close()
        }

        // v1.2.2: one unambiguous session separator BEFORE anything else -
        // app.log persists across versions/boots, so historical startup
        // entries (e.g. v0.8.0) previously mixed indistinguishably with the
        // current run. Everything above a banner is verifiably historical.
        logging.Default().SessionBanner(brand.FullName, config.AppVersion)

        // v1.7.4: bootstrap replay — the pre-canonical lines (path notes,
        // sampling repairs, migration decisions) land in app.log right
        // AFTER the session banner: the banner still separates this run's
        // entries from history, the replayed boot context reads naturally
        // before the "boot ... starting" line below, and the original
        // message order is preserved. If the canonical manager could not be
        // opened, the replay is skipped and the lines remain on stderr via
        // the bootstrap logger — nothing is lost silently.
        boot.ReplayInto(mgr)

        logging.Default().Info(
                "boot",
                "%s v%s starting (%s/%s, provider=%s)",
                brand.FullName,
                config.AppVersion,
                runtime.GOOS,
                runtime.GOARCH,
                cfg.ProviderKind(),
        )

        // Crash catcher: any panic in a command becomes a crash-*.log file
        // instead of a silent exit.
        exitCode := 0
        func() {
                defer func() {
                        if r := recover(); r != nil {
                                buf := make([]byte, 16384)
                                n := runtime.Stack(buf, false)
                                path := logging.Default().Crash(r, buf[:n])
                                fmt.Fprintf(
                                        os.Stderr,
                                        "panic: %v\n(crash report: %s)\n",
                                        r,
                                        path,
                                )
                                exitCode = 1
                        }
                }()
                exitCode = dispatch(cfg, defaultFn)
        }()

        logging.Default().Info("boot", "exit code %d", exitCode)
        return exitCode
}

func dispatch(cfg *config.Config, defaultFn func() int) int {
        args := os.Args[1:]

        if len(args) == 0 {
                if defaultFn != nil {
                        return defaultFn()
                }
                return Sysinfo(cfg)
        }

        switch args[0] {
        case "ask", "a":
                return Ask(cfg, args[1:])

        case "serve", "s", "web", "ui":
                return Serve(cfg, args[1:])

        case "gui", "desktop":
                if defaultFn != nil {
                        return defaultFn()
                }
                return 0

        case "version", "-v", "--version":
                fmt.Printf("%s v%s\n", config.AppName, config.AppVersion)
                fmt.Printf("  %s\n", brand.Notice())
                fmt.Printf("  go: %s\n", goVersion())
                fmt.Printf("  os: %s/%s\n", osName(), osArch())
                return 0

        case "doctor":
                return Doctor(cfg)

        case "install":
                return Install(cfg)

        case "sysinfo":
                return Sysinfo(cfg)

        case "setup":
                return Setup(cfg)

        case "stress":
                return Stress(cfg)

        case "logs":
                return Logs(cfg, args[1:])

        case "update":
                return Update(cfg, args[1:])

        case "diagnostics":
                return Diagnostics(cfg, args[1:])

        case "license":
                return License(cfg)

        case "context", "ai-context":
                return AICtx(cfg, args[1:])

        case "help", "-h", "--help":
                printHelp()
                return 0

        default:
                fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
                printHelp()
                return 2
        }
}

func configPath() string {
        // Portable mode: config.json lives next to the executable, inside the
        // SHEYTAN-Local-Agent folder (SHEYTAN_DATA_DIR can still override).
        return config.DefaultPath()
}

func printHelp() {
        fmt.Println(
                brand.Trademark + " " + config.AppName + " v" + config.AppVersion,
        )
        fmt.Println(brand.Notice())
        fmt.Println()

        fmt.Println("Usage: sheytan-local-agent <command> [options]")
        fmt.Println()

        fmt.Println("Commands:")
        fmt.Println("  (no args)    Launch the native Windows desktop GUI (default, Windows-only)")
        fmt.Println("  ask          Headless agent turn:  ask \"do anything\"")
        fmt.Println("  serve        Start the HTTP server only (no GUI)")
        fmt.Println("  setup        Run the first-run setup wizard (CLI)")
        fmt.Println("  install      Ensure all components are installed and check for updates")
        fmt.Println("  doctor       Run a full health check")
        fmt.Println("  sysinfo      Print system capabilities + recommended knobs")
        fmt.Println("  logs         Show recent logs + aggregated tool/LLM stats")
        fmt.Println("  update       Check for a llama.cpp engine update (scheduled: daily/weekly/monthly/off)")
        fmt.Println("  diagnostics  Export a diagnostics zip (logs, stats, sysinfo, config)")
        fmt.Println("  license      Print the SHEYTAN™ trademark + license")
        fmt.Println("  context      Show / regenerate the AI instruction file (AI-CONTEXT.md)")
        fmt.Println("  stress       Run the stress test suite")
        fmt.Println("  version      Print version info")
        fmt.Println("  help         Show this help")

        fmt.Println()
        fmt.Println("Options:")
        fmt.Println("  --port N            Override HTTP port (default 8765)")
        fmt.Println("  --host ADDR         Override bind host (default 127.0.0.1)")
        fmt.Println("  --no-update-check   Skip the per-launch component diff")
        fmt.Println("  --base-url URL      Override llama.cpp base URL")

        fmt.Println()
        fmt.Println("Environment:")
        fmt.Println("  SHEYTAN_PROVIDER=local|remote        LLM backend selector")
        fmt.Println("  SHEYTAN_REMOTE_BASE_URL=...          OpenAI-compatible endpoint")
        fmt.Println("  SHEYTAN_REMOTE_API_KEY=...           API key for the endpoint")
        fmt.Println("  SHEYTAN_REMOTE_MODEL=...             Model name on the endpoint")
        fmt.Println("  SHEYTAN_BROWSER_PATH=...             Chrome/Edge executable override")
}

func goVersion() string {
        return runtime.Version()
}

func osName() string {
        return runtime.GOOS
}

func osArch() string {
        return runtime.GOARCH
}
