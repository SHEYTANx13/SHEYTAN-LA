package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	agentruntime "github.com/Parsaetak/SHEYTAN-local-agent/internal/runtime"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

// Ask runs one headless agent turn. It powers CLI/automation use and the
// automated test cycle (provider=remote + a GLM-backed proxy server).
//
//	sheytan ask "list the files in /tmp"
//	sheytan ask --multi "research X and write a summary"
//	sheytan ask --new --no-llm-start "quick question"
func Ask(cfg *config.Config, args []string) int {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	multi := fs.Bool(
		"multi",
		false,
		"use the planner→executor→critic multi-agent pipeline",
	)

	newSess := fs.Bool(
		"new",
		false,
		"start a fresh session instead of reusing the latest",
	)

	noStart := fs.Bool(
		"no-llm-start",
		false,
		"do not auto-start the local llama.cpp server",
	)

	sessID := fs.String(
		"session",
		"",
		"continue a specific session id",
	)

	think := fs.Bool(
		"think",
		false,
		"enable thinking mode for this turn (step-by-step reasoning)",
	)

	toolSel := fs.String(
		"tools",
		"",
		"comma-separated tool allow-list (e.g. files,shell,dataAnalysis); empty = all",
	)

	attach := fs.String(
		"attach",
		"",
		"comma-separated file paths to attach to the message (text files inline, binaries as metadata)",
	)

	if err := fs.Parse(args); err != nil {
		return 2
	}

	prompt := strings.Join(fs.Args(), " ")

	if prompt == "" && *attach == "" {
		fmt.Fprintln(
			os.Stderr,
			`usage: sheytan ask [--multi] [--new] [--no-llm-start] [--session ID] [--think] [--tools a,b] [--attach f1.txt,f2.md] "your prompt"`,
		)
		return 2
	}

	// v1.0.2 per-run overrides: thinking mode + tool selection.
	if *think {
		cfg.ThinkingMode = true
	}

	if *toolSel != "" {
		var list []string

		for _, tool := range strings.Split(*toolSel, ",") {
			if tool = strings.TrimSpace(tool); tool != "" {
				list = append(list, tool)
			}
		}

		cfg.EnabledTools = list
	}

	// v1.0.2 attachments: compose the message with inlined file contents.
	// v1.0.6: images ride the message's Images field (multimodal wire).
	var attached []string

	for _, p := range strings.Split(*attach, ",") {
		if p = strings.TrimSpace(p); p != "" {
			attached = append(attached, p)
		}
	}

	composed, images := chunking.ComposeWithImages(
		prompt,
		attached,
		cfg.AttachmentsBudgetBytes(),
	)

	var attachNames []string

	for _, p := range attached {
		attachNames = append(
			attachNames,
			filepath.Base(p),
		)
	}

	stack := agentruntime.NewStack(cfg)
	defer stack.Close()

	if !*noStart {
		if err := stack.EnsureLLM(); err != nil {
			fmt.Fprintf(
				os.Stderr,
				"LLM backend: %v\n",
				err,
			)
			return 1
		}
	}

	// Session handling.
	store := sessions.New(cfg.SessionsDir)

	var sess *sessions.Session

	if *sessID != "" {
		var err error

		sess, err = store.Get(*sessID)
		if err != nil {
			fmt.Fprintf(
				os.Stderr,
				"session %s: %v\n",
				*sessID,
				err,
			)
			return 1
		}
	} else if *newSess {
		sess = store.Create()
	} else {
		// v1.0.2: the session list is meta-index stubs — load the full
		// history only for the one we continue.
		list, _ := store.List()

		if len(list) > 0 {
			if full, err := store.Get(list[0].ID); err == nil {
				sess = full
			} else {
				sess = list[0]
			}
		} else {
			sess = store.Create()
		}
	}

	stack.Orch.SetSessionID(sess.ID)

	// Build the message list: optional system prompt + history.
	var msgs []llm.Message

	if sess.Context.SystemPrompt != "" {
		msgs = append(
			msgs,
			llm.Message{
				Role:    "system",
				Content: sess.Context.SystemPrompt,
			},
		)
	}

	msgs = append(
		msgs,
		sess.Messages...,
	)

	now := time.Now()

	msgs = append(
		msgs,
		llm.Message{
			Role:        "user",
			Content:     composed,
			Attachments: attachNames,
			Images:      images,
			At:          now,
		},
	)

	sess.Messages = append(
		sess.Messages,
		llm.Message{
			Role:        "user",
			Content:     composed,
			Attachments: attachNames,
			Images:      images,
			At:          now,
		},
	)

	if len(sess.Messages) == 1 {
		title := prompt

		if title == "" {
			title = strings.Join(
				attachNames,
				", ",
			)
		}

		if len(title) > 60 {
			title = title[:60] + "…"
		}

		sess.Title = title
		_ = store.UpdateTitle(
			sess.ID,
			title,
		)
	}

	_ = store.Save(sess)

	fmt.Printf(
		"┌─ SHEYTAN™ v%s — ask (provider=%s, model=%s)\n",
		config.AppVersion,
		cfg.ProviderKind(),
		cfg.EffectiveModel(),
	)

	fmt.Printf(
		"│ session: %s\n",
		sess.ID,
	)

	if len(attachNames) > 0 {
		fmt.Printf(
			"│ attach: %s\n",
			strings.Join(
				attachNames,
				", ",
			),
		)
	}

	fmt.Printf(
		"└─ prompt: %.100s\n\n",
		prompt,
	)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	printActivity := func(a agent.Activity) {
		switch a.Type {
		case "tool_start":
			fmt.Printf(
				"  ▶ %s\n",
				a.Caption,
			)

		case "tool_end":
			fmt.Printf(
				"  ✔ %s\n",
				a.Caption,
			)

		case "error":
			fmt.Printf(
				"  ✖ %s\n",
				a.Caption,
			)

		case "plan":
			fmt.Printf(
				"  📋 %s\n",
				a.Caption,
			)

		case "done":
			fmt.Printf(
				"  ● %s\n",
				a.Caption,
			)

		case "thinking":
			fmt.Printf(
				"  · %s\n",
				a.Caption,
			)

		case "reasoning":
			// v1.0.2: live thinking trace — first line only, dimmed.
			line := a.Caption

			if i := strings.IndexByte(line, '\n'); i >= 0 {
				line = line[:i]
			}

			if len(line) > 100 {
				line = line[:100] + "…"
			}

			fmt.Printf(
				"  ~ %s\n",
				line,
			)
		}
	}

	var final string
	var reasoningTrace string
	var runErr error

	func() {
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 8192)
				n := runtime.Stack(
					buf,
					false,
				)

				logging.Default().Crash(
					r,
					buf[:n],
				)

				runErr = fmt.Errorf(
					"panic: %v",
					r,
				)
			}
		}()

		var err error

		if *multi {
			final, err = stack.Multi.Run(
				ctx,
				prompt,
				printActivity,
			)
		} else {
			var res agent.RunResult

			res, err = stack.Orch.RunDetailed(
				ctx,
				msgs,
				printActivity,
			)

			final = res.Text
			reasoningTrace = res.Reasoning
		}

		runErr = err
	}()

	if runErr != nil {
		fmt.Fprintf(
			os.Stderr,
			"\nerror: %v\n",
			runErr,
		)
		return 1
	}

	if final != "" {
		sess.Messages = append(
			sess.Messages,
			llm.Message{
				Role:      "assistant",
				Content:   final,
				Reasoning: reasoningTrace,
			},
		)

		_ = store.Save(sess)

		if reasoningTrace != "" {
			fmt.Printf(
				"\n%s\n  (thinking trace: %d chars — see the session file)\n",
				strings.Repeat("─", 60),
				len(reasoningTrace),
			)
		}

		fmt.Printf(
			"\n%s\n",
			strings.Repeat("─", 60),
		)

		fmt.Println(final)
	}

	logging.Default().Info(
		"ask",
		"turn complete (session=%s, reply=%d chars)",
		sess.ID,
		len(final),
	)

	// v1.0.2: index the completed exchange into persistent recall.
	if stack.Recall != nil && final != "" {
		_ = stack.Recall.IndexTurn(
			sess.ID,
			sess.Title,
			prompt,
			final,
			nil,
		)
	}

	return 0
}
