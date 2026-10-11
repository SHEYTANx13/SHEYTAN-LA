package cmd

// update: run the scheduled engine updater on demand from the CLI.
//
// Usage:
//
//      sheytan-local-agent update            # check + apply if due
//      sheytan-local-agent update --force    # check + apply now, ignoring schedule
//      sheytan-local-agent update --status   # show schedule, last check, engine tag
import (
	"context"
	"flag"
	"fmt"
	"runtime"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

func goosWindows() bool { return runtime.GOOS == "windows" }

// Update runs one engine update pass (or prints status).
func Update(cfg *config.Config, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	force := fs.Bool("force", false, "check now, ignoring the schedule")
	status := fs.Bool("status", false, "show update status only")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	tag := updater.InstalledEngineTag(cfg)
	if tag == "" {
		tag = "(not recorded)"
	}
	last, _ := time.Parse(time.RFC3339, cfg.LastUpdateCheck)
	schedule := updater.NormalizeSchedule(cfg.UpdateSchedule)

	if *status {
		fmt.Printf("Engine update status:\n")
		fmt.Printf("  schedule:      %s\n", schedule)
		fmt.Printf("  last check:    %s\n", humanTimeOrNever(last))
		fmt.Printf("  engine tag:    %s\n", tag)
		fmt.Printf("  next check:    %s\n", nextCheck(schedule, last))
		fmt.Printf("  binary:        %s\n", engineBinPath(cfg))
		return 0
	}

	src := config.NewSource(cfg)

	if *force {
		msg, updated, err := updater.CheckAndApplyForced(context.Background(), cfg, llm.NewLlamaServer(src))
		_ = config.Save(cfg.ConfigPath(), cfg)
		fmt.Println(msg)
		if err != nil {
			fmt.Printf("  (error: %v)\n", err)
			return 1
		}
		if updated {
			fmt.Println("  engine upgraded and recorded.")
		}
		return 0
	}
	msg, updated, err := updater.CheckAndApply(context.Background(), cfg, llm.NewLlamaServer(src))
	_ = config.Save(cfg.ConfigPath(), cfg)
	fmt.Println(msg)
	if err != nil {
		fmt.Printf("  (error: %v)\n", err)
		return 1
	}
	if updated {
		fmt.Println("  engine upgraded and recorded.")
	}
	return 0
}

func humanTimeOrNever(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format(time.RFC3339)
}

func nextCheck(schedule string, last time.Time) string {
	if schedule == updater.ScheduleOff {
		return "never (updates off)"
	}
	if last.IsZero() {
		return "on next launch"
	}
	var interval time.Duration
	switch schedule {
	case updater.ScheduleDaily:
		interval = 24 * time.Hour
	case updater.ScheduleWeekly:
		interval = 7 * 24 * time.Hour
	case updater.ScheduleMonthly:
		interval = 30 * 24 * time.Hour
	}
	return last.Add(interval).Format(time.RFC3339)
}

func engineBinPath(cfg *config.Config) string {
	if cfg.LlamaBinPath != "" {
		return cfg.LlamaBinPath
	}
	name := "llama-server"
	if goosWindows() {
		name += ".exe"
	}
	return cfg.DataDir + "/bin/" + name
}
