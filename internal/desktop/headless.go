//go:build headless

// Headless desktop shell (v1.1.3): the same runtime stack and HTTP/WebSocket
// API as the Wails window, without any GUI dependency. Selected with the
// `headless` build tag — used by CI, containers, servers, and the headless
// test suite on machines without GTK/WebKit development libraries.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// ServeHeadless boots the API server (including automatic engine prewarm)
// and blocks until SIGINT/SIGTERM. Exit codes match the desktop shell.
func ServeHeadless(cfg *config.Config) int {
	if cfg == nil {
		fmt.Fprintln(os.Stderr, "headless: missing configuration")
		return 1
	}

	srv, err := api.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "headless: initialize server:", err)
		return 1
	}

	setupComplete := false
	defer func() {
		if !setupComplete {
			srv.Close()
		}
	}()

	if err := srv.EnsureSetup(); err != nil {
		fmt.Fprintln(os.Stderr, "headless: setup:", err)
		return 1
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	fmt.Printf("%s v%s (headless)\n", config.AppName, config.AppVersion)
	fmt.Printf("  server:  http://localhost:%d/\n", cfg.Port)
	fmt.Printf("  models:  %s\n", cfg.ModelsDir)
	fmt.Printf("  data:    %s\n", cfg.DataDir)
	fmt.Println()

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

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "shutdown:", err)
			_ = httpServer.Close()
		}
	}()

	setupComplete = true

	err = httpServer.ListenAndServe()

	if err == nil || errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		srv.Close()
		return 0
	}

	fmt.Fprintln(os.Stderr, "server:", err)
	_ = httpServer.Close()
	srv.Close()

	stopSignal()
	<-shutdownDone

	return 1
}
