package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/aicontext"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// AICtx manages SHEYTAN's AI instruction file (AI-CONTEXT.md) — the complete
// operating manual prepended to the system prompt of every plugged-in model.
//
//	sheytan context              print the effective instructions + live env
//	sheytan context --path       print only the file path
//	sheytan context --reset      regenerate the canonical file (drops edits)
func AICtx(cfg *config.Config, args []string) int {
	fs := flag.NewFlagSet(
		"context",
		flag.ContinueOnError,
	)
	fs.SetOutput(os.Stderr)

	reset := fs.Bool(
		"reset",
		false,
		"regenerate the canonical AI-CONTEXT.md (user edits are lost)",
	)

	pathOnly := fs.Bool(
		"path",
		false,
		"print only the file path",
	)

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *reset {
		p := aicontext.Path(cfg.DataDir)

		if err := os.Remove(p); err != nil &&
			!os.IsNotExist(err) {
			fmt.Fprintf(
				os.Stderr,
				"remove: %v\n",
				err,
			)
			return 1
		}
	}

	path, err := aicontext.EnsureFile(
		cfg.DataDir,
	)
	if err != nil {
		fmt.Fprintf(
			os.Stderr,
			"AI context file: %v\n",
			err,
		)
		return 1
	}

	if *pathOnly {
		fmt.Println(path)
		return 0
	}

	fmt.Printf(
		"┌─ SHEYTAN™ v%s — AI context file\n",
		config.AppVersion,
	)

	fmt.Printf(
		"│ path: %s (edit freely; regenerated on instruction upgrades)\n",
		path,
	)

	fmt.Printf(
		"│ instructions below are exactly what the plugged-in model receives\n",
	)

	fmt.Printf(
		"└─ live environment block is appended per conversation\n\n",
	)

	fmt.Println(
		aicontext.SystemMessage(cfg),
	)

	return 0
}
