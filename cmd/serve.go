package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// Serve starts the HTTP server and (optionally) opens the browser.
func Serve(cfg *config.Config, args []string) int {
	openBrowserEnabled := true

	// Parse simple flags.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 < len(args) {
				var p int

				if _, err := fmt.Sscanf(args[i+1], "%d", &p); err == nil && p > 0 {
					cfg.Port = p
				}

				i++
			}

		case "--host":
			if i+1 < len(args) {
				if args[i+1] != "" {
					cfg.Host = args[i+1]
				}

				i++
			}

		case "--no-browser":
			openBrowserEnabled = false
		}
	}

	srv, err := api.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "init server:", err)
		return 1
	}

	// Any failure after api.New owns a live runtime stack, so make sure the
	// stack is released before returning.
	setupComplete := false
	defer func() {
		if !setupComplete {
			srv.Close()
		}
	}()

	if err := srv.EnsureSetup(); err != nil {
		fmt.Fprintln(os.Stderr, "setup:", err)
		return 1
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	url := fmt.Sprintf("http://%s/", addr)
	switch cfg.Host {
	case "0.0.0.0", "127.0.0.1", "localhost":
		url = fmt.Sprintf("http://localhost:%d/", cfg.Port)
	}

	fmt.Printf("%s v%s\n", config.AppName, config.AppVersion)
	fmt.Printf("  server: %s\n", url)
	fmt.Printf("  models: %s\n", cfg.ModelsDir)
	fmt.Printf("  sessions: %s\n", cfg.SessionsDir)
	fmt.Printf("  data:    %s\n", cfg.DataDir)
	fmt.Println()
	fmt.Println("Open the URL above in your browser. Ctrl-C to stop.")
	fmt.Println()

	if openBrowserEnabled {
		go openBrowser(url)
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownCtx, stopSignal := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopSignal()

	shutdownDone := make(chan struct{})

	go func() {
		defer close(shutdownDone)

		<-shutdownCtx.Done()

		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "shutdown:", err)

			// Force-close the listening socket if graceful shutdown could
			// not complete within the deadline.
			_ = httpServer.Close()
		}
	}()

	setupComplete = true

	err = httpServer.ListenAndServe()

	// A normal shutdown is reported by net/http as ErrServerClosed.
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		srv.Close()
		return 0
	}

	// Unexpected server failure: close the server immediately rather than
	// waiting for an external signal that may never arrive.
	fmt.Fprintln(os.Stderr, "server:", err)

	_ = httpServer.Close()
	srv.Close()

	// Stop the signal context so the shutdown goroutine can exit even though
	// no OS signal caused this shutdown.
	stopSignal()
	<-shutdownDone

	return 1
}

func openBrowser(url string) {
	// Hidden-console launch via internal/proc.
	switch runtime.GOOS {
	case "darwin":
		_ = proc.Command("open", url).Start()

	case "windows":
		_ = proc.Command(
			"rundll32",
			"url.dll,FileProtocolHandler",
			url,
		).Start()

	default:
		_ = proc.Command("xdg-open", url).Start()
	}
}
