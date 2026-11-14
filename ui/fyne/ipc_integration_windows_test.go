//go:build windows && !headless

// ipc_integration_windows_test.go — v1.11.1 Windows IPC repair: the
// PRODUCTION CoreClient (the exact client every Fyne view shares)
// against the REAL core (internal/api.Server) over the REAL
// localipc.Serve WINDOWS NAMED PIPE.
//
// This is the hosted gate that closes the run-43455711025 gap: the
// Windows lane proved a visible window and headless core tests, never an
// authenticated production-client request through the named pipe. Runs
// natively on Windows (the hosted Windows x64 lane); locally it is
// compile-verified with GOOS=windows go vet / go build. The Unix twin
// (ipc_integration_test.go) proves the same wire contract on Linux.
//
// The hand-rolled-listener fixtures (mismatched protocol, missing
// endpoint) are Unix-only here; the Windows missing-pipe and busy-pipe
// dial behaviors are covered by internal/localipc/dial_windows_test.go
// against the real pipe API.
package fyneui_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/api"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/localipc"
	fyneui "github.com/SHEYTANx13/SHEYTAN-LA/ui/fyne"
)

// newIsolatedCoreWindows builds a REAL api.Server over an isolated
// temporary data root — the IPC layer must be proven against the real
// core handlers, never a stub.
func newIsolatedCoreWindows(t *testing.T) (*api.Server, *config.Config) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"

	srv, err := api.New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	if err := srv.EnsureSetup(); err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}
	t.Cleanup(srv.Close)

	return srv, cfg
}

// TestCoreClientEndToEndOverWindowsNamedPipe: authenticated versioned
// handshake, representative read-only APIs from the HTTP-backed views
// (Config, Sessions, Logs, Models/Selection, SysInfo, Tools, Workspace),
// and the WebSocket activity stream with a normal close — all through
// one production CoreClient over the Windows named pipe. No GPU, no
// model, no outbound network dependency.
func TestCoreClientEndToEndOverWindowsNamedPipe(t *testing.T) {
	srv, cfg := newIsolatedCoreWindows(t)
	key, err := localipc.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	ipc, err := localipc.Serve(srv, cfg, key)
	if err != nil {
		t.Fatalf("localipc.Serve: %v", err)
	}
	t.Cleanup(ipc.Stop)

	client := fyneui.NewCoreClient(ipc.Endpoint(), key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The endpoint must be a named pipe on Windows — the transport this
	// repair exists to restore.
	if !strings.HasPrefix(strings.ToLower(ipc.Endpoint()), `\\.\pipe\`) {
		t.Fatalf("endpoint %q is not a Windows named pipe", ipc.Endpoint())
	}

	// 1. Authenticated versioned handshake over the pipe.
	if err := client.Handshake(ctx); err != nil {
		t.Fatalf("production CoreClient handshake over the Windows named pipe failed: %v", err)
	}
	if client.Version() != config.AppVersion {
		t.Fatalf("handshake coreVersion = %q, want %q", client.Version(), config.AppVersion)
	}

	// 2. Representative real read-only APIs across the views.
	if _, err := client.Config(ctx); err != nil {
		t.Fatalf("Config: %v", err)
	}
	if _, err := client.Sessions(ctx, "chat"); err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if _, _, err := client.Logs(ctx, 200); err != nil {
		t.Fatalf("Logs: %v — the Logs panel's fetch path is broken", err)
	}
	if _, err := client.Models(ctx); err != nil {
		t.Fatalf("Models: %v", err)
	}
	if _, err := client.Selection(ctx); err != nil {
		t.Fatalf("Selection: %v", err)
	}
	if _, err := client.SysInfo(ctx); err != nil {
		t.Fatalf("SysInfo: %v", err)
	}
	if _, err := client.Tools(ctx); err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if _, err := client.Workspace(ctx); err != nil {
		t.Fatalf("Workspace: %v", err)
	}

	// 3. The connection stays alive across many requests.
	for i := 0; i < 5; i++ {
		if _, err := client.Sessions(ctx, "chat"); err != nil {
			t.Fatalf("Sessions follow-up %d: %v", i, err)
		}
	}

	// 4. The WebSocket streaming path over the named pipe.
	sess, err := client.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rt := fyneui.NewRuntime(client)
	defer rt.Shutdown()

	streamSeen := make(chan struct{}, 1)
	rt.SetCallbacks(
		func() {
			select {
			case streamSeen <- struct{}{}:
			default:
			}
		},
		func() {},
		func() {},
	)

	attachDone := make(chan error, 1)
	actx, acancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer acancel()
	go func() { attachDone <- rt.AttachActivity(actx, sess.ID) }()

	select {
	case <-streamSeen:
		// The attached frame arrived: the WebSocket dialed through the
		// shared named-pipe transport and the core answered.
	case <-time.After(15 * time.Second):
		t.Fatal("no WebSocket attach frame within 15s — the streaming transport is broken over the named pipe")
	}

	rt.DetachActivity()
	select {
	case err := <-attachDone:
		if err != nil && strings.Contains(err.Error(), "dial") {
			t.Fatalf("attach ended with a DIAL error — the WebSocket transport never connected: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("attach pump did not end within 5s of detach")
	}
}

// TestCoreClientRejectsWrongKeyWindows: a client holding the wrong key is
// refused at the named-pipe handshake — typed error, no fallback.
func TestCoreClientRejectsWrongKeyWindows(t *testing.T) {
	srv, cfg := newIsolatedCoreWindows(t)
	key, err := localipc.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	ipc, err := localipc.Serve(srv, cfg, key)
	if err != nil {
		t.Fatalf("localipc.Serve: %v", err)
	}
	t.Cleanup(ipc.Stop)

	bad := fyneui.NewCoreClient(ipc.Endpoint(), "definitely-the-wrong-key")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = bad.Handshake(ctx)
	if err == nil {
		t.Fatal("wrong key must be refused at the handshake")
	}
	if strings.Contains(err.Error(), "definitely-the-wrong-key") {
		t.Fatal("the refusal must never echo the rejected key material")
	}
}
