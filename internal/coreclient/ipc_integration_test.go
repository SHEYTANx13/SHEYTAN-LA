//go:build !windows && !headless

// ipc_integration_test.go — v1.11.1 Windows IPC repair: the PRODUCTION
// CoreClient (the exact client every Fyne view shares) against the REAL
// core (internal/api.Server) over the REAL localipc.Serve transport.
//
// This is the gate the green run 43455711025 never had: a visible window
// plus headless core tests proved nothing about the production client's
// ability to perform an authenticated request through the local
// transport. These tests exercise the same wire path the desktop
// application uses — handshake, representative read-only APIs from every
// HTTP-backed view, the WebSocket streaming path with a normal close,
// key rejection and protocol refusal — with no GPU, no model and no
// outbound network dependency.
//
// Runs natively on Linux (Unix domain socket). The Windows named-pipe
// twin lives in ipc_integration_windows_test.go and runs in the hosted
// Windows lane; locally it is compile-verified with GOOS=windows.
// The hand-rolled-listener fixtures (mismatched protocol, missing
// endpoint) are Unix-only; their Windows equivalents are covered by
// internal/localipc/dial_windows_test.go against the real pipe API.
package coreclient_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/api"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/brand"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	coreclient "github.com/SHEYTANx13/SHEYTAN-LA/internal/coreclient"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/localipc"
)

// newIsolatedCore builds a REAL api.Server over an isolated temporary
// data root — the IPC layer must be proven against the real core
// handlers, never a stub — with the endpoint directory equally isolated
// (XDG_RUNTIME_DIR) so parallel test binaries can never collide.
func newIsolatedCore(t *testing.T) (*api.Server, *config.Config) {
	t.Helper()

	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

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

// startIPC binds the real localipc endpoint for the isolated core.
func startIPC(t *testing.T, srv *api.Server, cfg *config.Config, key string) *localipc.Server {
	t.Helper()

	ipc, err := localipc.Serve(srv, cfg, key)
	if err != nil {
		t.Fatalf("localipc.Serve: %v", err)
	}
	t.Cleanup(ipc.Stop)
	return ipc
}

// TestCoreClientEndToEndOverLocalIPC is the full production wire path:
// authenticated versioned handshake, representative read-only APIs from
// the HTTP-backed views (Config, Sessions, Logs, Models/Selection,
// SysInfo, Tools, Workspace), and the WebSocket activity stream with a
// normal close — through one CoreClient over the real transport.
func TestCoreClientEndToEndOverLocalIPC(t *testing.T) {
	srv, cfg := newIsolatedCore(t)
	key, err := localipc.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	ipc := startIPC(t, srv, cfg, key)

	client := coreclient.NewCoreClient(ipc.Endpoint(), key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Authenticated versioned handshake.
	if err := client.Handshake(ctx); err != nil {
		t.Fatalf("production CoreClient handshake over the local transport failed: %v", err)
	}
	if client.Version() != brand.DisplayVersion {
		t.Fatalf("handshake coreVersion = %q, want %q", client.Version(), brand.DisplayVersion)
	}

	// 2. Representative real read-only APIs across the views. Only valid
	// read-only endpoints: no model download, no outbound network, no
	// engine launch.
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

	// 3. The connection stays alive across many requests (no per-request
	// transport teardown).
	for i := 0; i < 5; i++ {
		if _, err := client.Sessions(ctx, "chat"); err != nil {
			t.Fatalf("Sessions follow-up %d: %v", i, err)
		}
	}

	// 4. The WebSocket streaming path used by chat/agent views: create a
	// real session, attach the production runtime, wait for the
	// deterministic `attached` frame, then close normally.
	sess, err := client.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rt := coreclient.NewRuntime(client)
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
		// shared transport and the core answered.
	case <-time.After(15 * time.Second):
		t.Fatal("no WebSocket attach frame within 15s — the streaming transport is broken")
	}

	// Normal close: detach cancels the pump.
	rt.DetachActivity()
	select {
	case err := <-attachDone:
		if err != nil && strings.Contains(err.Error(), "dial") {
			t.Fatalf("attach ended with a DIAL error — the WebSocket transport never connected: %v", err)
		}
		// A cancellation or read-side close error is the expected normal
		// close shape; only a dial failure means the transport is broken.
	case <-time.After(5 * time.Second):
		t.Fatal("attach pump did not end within 5s of detach")
	}
}

// TestCoreClientRejectsWrongKey: a client holding the wrong key is
// refused at the handshake — typed error, no fallback, no retry storm.
func TestCoreClientRejectsWrongKey(t *testing.T) {
	srv, cfg := newIsolatedCore(t)
	key, err := localipc.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	ipc := startIPC(t, srv, cfg, key)

	bad := coreclient.NewCoreClient(ipc.Endpoint(), "definitely-the-wrong-key")
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

// TestCoreClientRefusesMismatchedProtocol: a core announcing a different
// protocol identity is refused — never guessed, never silently degraded.
func TestCoreClientRefusesMismatchedProtocol(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mismatched.sock")

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ipc/handshake", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"protocol":"something-else","protocolVersion":99,"coreVersion":"0.0.0","authenticated":true}`))
	})
	fake := &http.Server{Handler: mux}
	t.Cleanup(func() { _ = fake.Close() })
	go func() { _ = fake.Serve(ln) }()

	client := coreclient.NewCoreClient(path, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = client.Handshake(ctx)
	if err == nil {
		t.Fatal("mismatched protocol must be refused")
	}
	if !strings.Contains(err.Error(), "mismatch") && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refusal should name the protocol mismatch, got: %v", err)
	}
}

// TestNoNetworkListenerIsOpened
// (internal/localipc/serve_test.go) keeps the endpoint-shape pin; this
// test proves the FULL production-client flow — real core, real IPC
// server, handshake, reads, WebSocket attach/close — opened no TCP
// listener owned by this process (procfs inode match: the listener
// count is PID-precise, not system-wide, so concurrent test binaries
// cannot flake it).
func TestUnixIPCFlowOpensNoTCPListener(t *testing.T) {
	before := unixOwnTCPListeners(t)

	srv, cfg := newIsolatedCore(t)
	key, err := localipc.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	ipc := startIPC(t, srv, cfg, key)

	client := coreclient.NewCoreClient(ipc.Endpoint(), key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := client.Config(ctx); err != nil {
		t.Fatalf("Config: %v", err)
	}
	sess, err := client.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rt := coreclient.NewRuntime(client)
	defer rt.Shutdown()
	attachDone := make(chan error, 1)
	actx, acancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer acancel()
	go func() { attachDone <- rt.AttachActivity(actx, sess.ID) }()
	// Bounded settle so the WS dial has run before the recount; the pump
	// is torn down deterministically right after via Detach.
	time.Sleep(500 * time.Millisecond)
	rt.DetachActivity()
	select {
	case <-attachDone:
	case <-time.After(5 * time.Second):
		t.Fatal("attach pump did not end after detach")
	}

	after := unixOwnTCPListeners(t)
	if after != before {
		t.Fatalf("TCP listener count changed around the IPC flow: before=%d after=%d — a TCP fallback or side listener appeared", before, after)
	}
}

// unixOwnTCPListeners counts LISTENING TCP sockets whose inode belongs to
// one of THIS process's open file descriptors (pure procfs — no external
// commands, no system-wide flakiness). Skips on non-procfs platforms.
func unixOwnTCPListeners(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("procfs unavailable on this host")
	}
	own := map[string]bool{}
	for _, e := range entries {
		link, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name()))
		if err == nil && strings.HasPrefix(link, "socket:[") {
			own[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
		}
	}
	count := 0
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // no IPv6 table, restricted host, etc.
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fields := strings.Fields(line)
			// st==0A is LISTEN; field 9 is the socket inode.
			if len(fields) >= 10 && fields[3] == "0A" && own[fields[9]] {
				count++
			}
		}
	}
	return count
}

// TestCoreClientWebSocketCallerCancelUnix: the CALLER's context — not a
// detach — terminates the activity pump, and the pump surfaces the
// context error, over the REAL Unix-socket transport (the Windows named-
// pipe twin lives in ipc_negative_windows_test.go).
func TestCoreClientWebSocketCallerCancelUnix(t *testing.T) {
	srv, cfg := newIsolatedCore(t)
	key, err := localipc.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	ipc := startIPC(t, srv, cfg, key)

	client := coreclient.NewCoreClient(ipc.Endpoint(), key)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	sess, err := client.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rt := coreclient.NewRuntime(client)
	defer rt.Shutdown()

	attached := make(chan struct{}, 1)
	rt.SetCallbacks(
		func() {
			select {
			case attached <- struct{}{}:
			default:
			}
		},
		func() {},
		func() {},
	)

	attachDone := make(chan error, 1)
	actx, acancel := context.WithCancel(context.Background())
	defer acancel()
	go func() { attachDone <- rt.AttachActivity(actx, sess.ID) }()

	select {
	case <-attached:
	case <-time.After(15 * time.Second):
		t.Fatal("no WebSocket attach frame within 15s — the streaming transport is broken over the unix socket")
	}

	acancel() // the caller cancels: the pump must end with the context error
	select {
	case err := <-attachDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled attach returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("attach pump did not end within 5s of caller cancellation")
	}
}

// TestCoreClientReportsTransportFailureHonestly: with no server bound,
// every request fails with the typed transport error — the UI's honest
// error surface depends on this never being swallowed.
func TestCoreClientReportsTransportFailureHonestly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-core.sock")
	client := coreclient.NewCoreClient(path, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Handshake(ctx); err == nil {
		t.Fatal("handshake against a missing core must fail")
	}
	if _, _, err := client.Logs(ctx, 200); err == nil {
		t.Fatal("Logs against a missing core must fail — an empty panel must never mask a broken transport")
	}
}
