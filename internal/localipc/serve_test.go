package localipc

import (
        "bytes"
        "context"
        "encoding/json"
        "io"
        "net"
        "net/http"
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// newCore builds a REAL api.Server over a temp data dir — the IPC layer
// must be proven against the real core handler, never a stub.
func newCore(t *testing.T) (*api.Server, *config.Config) {
        t.Helper()

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.ModelsDir = cfg.DataDir + "/models"
        cfg.SessionsDir = cfg.DataDir + "/sessions"
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

// ipcClient builds an http.Client that dials the IPC endpoint instead of
// TCP — exactly how the Qt host consumes the channel.
func ipcClient(endpoint string) *http.Client {
        return &http.Client{
                Transport: &http.Transport{
                        DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
                                return dialEndpoint(endpoint)
                        },
                },
                Timeout: 15 * time.Second,
        }
}

func startIPC(t *testing.T, srv *api.Server, cfg *config.Config, key string) *Server {
        t.Helper()

        ipc, err := Serve(srv, cfg, key)
        if err != nil {
                t.Fatalf("localipc.Serve: %v", err)
        }
        t.Cleanup(ipc.Stop)
        return ipc
}

func TestEndpointIsUserScopedAndDirectoryIsPrivate(t *testing.T) {
        endpoint, dir, err := Endpoint()
        if err != nil {
                t.Fatalf("Endpoint: %v", err)
        }

        if strings.TrimSpace(endpoint) == "" {
                t.Fatal("endpoint must not be empty")
        }

        if dir != "" {
                st, err := os.Stat(dir)
                if err != nil {
                        t.Fatalf("stat dir: %v", err)
                }
                if got := st.Mode().Perm(); got != 0o700 {
                        t.Fatalf("endpoint dir mode = %v, want 0700 (current-user-only visibility)", got)
                }
        }

        if strings.Contains(endpoint, "0.0.0.0") {
                t.Fatalf("endpoint %q must never be a TCP-style address", endpoint)
        }
}

func TestCredAllowedDeniesForeignUid(t *testing.T) {
        if os.PathSeparator == '\\' {
                // W4 (v1.10.6): on Windows os.Getuid() == -1, which degenerated
                // this test into "4241 != -1" — vacuous, passing without testing
                // anything. The Windows transport identity is SID-based, not
                // uid-based: the equivalent fail-closed contract (own SID allowed,
                // foreign SID denied, empty SID denied) is pinned by
                // TestSIDAllowed* in listener_windows_test.go, and the end-to-end
                // accept path (DACl + post-connect SID check) is exercised by
                // every serve test below. The uid model is genuinely Unix-only
                // semantics, so the Unix assertion runs only where it means
                // something.
                t.Skip("windows: uid credential model not applicable — SID identity covered by TestSIDAllowed* (listener_windows_test.go)")
        }
        if !credAllowed(os.Getuid()) {
                t.Fatal("own uid must be allowed")
        }
        if os.Getuid() == 0 {
                t.Skip("running as root: every uid is self")
        }
        if credAllowed(os.Getuid() + 4242) {
                t.Fatal("foreign uid must be denied")
        }
}

func TestStaleEndpointRemovedLiveEndpointKept(t *testing.T) {
        if os.PathSeparator == '\\' {
                t.Skip("named pipes have no stale file lifecycle")
        }

        dir := t.TempDir()
        live := filepath.Join(dir, "live.sock")
        dead := filepath.Join(dir, "dead.sock")

        // LIVE: a real bound socket must never be removed by the probe.
        ln, err := net.Listen("unix", live)
        if err != nil {
                t.Fatalf("bind live: %v", err)
        }
        defer ln.Close()

        removed, err := cleanStaleEndpoint(live)
        if err != nil {
                t.Fatalf("cleanStaleEndpoint(live): %v", err)
        }
        if removed {
                t.Fatal("a live endpoint must never be removed")
        }
        if _, err := os.Stat(live); err != nil {
                t.Fatal("live endpoint vanished during probe")
        }

        // DEAD: a leftover file with no listener must be removed.
        if err := os.WriteFile(dead, []byte("stale"), 0o600); err != nil {
                t.Fatalf("create stale: %v", err)
        }
        removed, err = cleanStaleEndpoint(dead)
        if err != nil {
                t.Fatalf("cleanStaleEndpoint(dead): %v", err)
        }
        if !removed {
                t.Fatal("stale endpoint must be removed")
        }
        if _, err := os.Stat(dead); !os.IsNotExist(err) {
                t.Fatal("stale endpoint still present after cleanup")
        }
}

func TestServeHandshakeHappyPathAndKeyMismatch(t *testing.T) {
        srv, cfg := newCore(t)
        key, err := NewKey()
        if err != nil {
                t.Fatalf("NewKey: %v", err)
        }

        ipc := startIPC(t, srv, cfg, key)
        client := ipcClient(ipc.Endpoint())

        // Happy path: correct key → typed versioned handshake.
        req, _ := http.NewRequest(http.MethodGet, "http://localipc/api/ipc/handshake", nil)
        req.Header.Set("X-SHEYTAN-IPC-Key", key)

        resp, err := client.Do(req)
        if err != nil {
                t.Fatalf("handshake: %v", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                body, _ := io.ReadAll(resp.Body)
                t.Fatalf("handshake status = %d body = %s", resp.StatusCode, body)
        }

        var hs struct {
                Protocol        string `json:"protocol"`
                ProtocolVersion int    `json:"protocolVersion"`
                CoreVersion     string `json:"coreVersion"`
                Authenticated   bool   `json:"authenticated"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&hs); err != nil {
                t.Fatalf("decode handshake: %v", err)
        }
        if hs.Protocol != ProtocolName || hs.ProtocolVersion != ProtocolVersion {
                t.Fatalf("handshake identity = %s/%d, want %s/%d", hs.Protocol, hs.ProtocolVersion, ProtocolName, ProtocolVersion)
        }
        if !hs.Authenticated {
                t.Fatal("handshake must report authenticated=true on the correct key")
        }
        if hs.CoreVersion == "" {
                t.Fatal("handshake must carry the core version")
        }

        // Wrong key → 403, structured error, no echo of the attempted key.
        req2, _ := http.NewRequest(http.MethodGet, "http://localipc/api/ipc/handshake", nil)
        req2.Header.Set("X-SHEYTAN-IPC-Key", "wrong-key-value")

        resp2, err := client.Do(req2)
        if err != nil {
                t.Fatalf("handshake(wrong key): %v", err)
        }
        defer resp2.Body.Close()

        if resp2.StatusCode != http.StatusForbidden {
                t.Fatalf("wrong key status = %d, want 403", resp2.StatusCode)
        }

        // Missing key → 401.
        resp3, err := client.Get("http://localipc/api/ipc/handshake")
        if err != nil {
                t.Fatalf("handshake(no key): %v", err)
        }
        defer resp3.Body.Close()
        if resp3.StatusCode != http.StatusUnauthorized {
                t.Fatalf("missing key status = %d, want 401", resp3.StatusCode)
        }
}

func TestServeTunnelsRealCoreAPI(t *testing.T) {
        srv, cfg := newCore(t)
        ipc := startIPC(t, srv, cfg, "")
        client := ipcClient(ipc.Endpoint())

        // The REAL core API rides the channel: create a session through the
        // same handler the desktop UI will use.
        resp, err := client.Post("http://localipc/api/sessions",
                "application/json", strings.NewReader(`{"name":"ipc-tunnel"}`))
        if err != nil {
                t.Fatalf("create session over IPC: %v", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                body, _ := io.ReadAll(resp.Body)
                t.Fatalf("create session status = %d body = %s", resp.StatusCode, body)
        }

        var sess struct {
                ID string `json:"id"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil || sess.ID == "" {
                t.Fatalf("decode session: %v (id=%q)", err, sess.ID)
        }

        // Health check through the SAME channel.
        resp2, err := client.Get("http://localipc/api/health")
        if err != nil {
                t.Fatalf("health over IPC: %v", err)
        }
        defer resp2.Body.Close()
        if resp2.StatusCode != http.StatusOK {
                t.Fatalf("health status = %d", resp2.StatusCode)
        }
}

func TestServeMalformedRequestIsRejectedNotCrashing(t *testing.T) {
        srv, cfg := newCore(t)
        ipc := startIPC(t, srv, cfg, "")

        // Malformed HTTP: garbage bytes on the wire must be answered (or
        // dropped) by the server WITHOUT killing the endpoint.
        conn, err := dialEndpoint(ipc.Endpoint())
        if err != nil {
                t.Fatalf("dial: %v", err)
        }
        _ = conn.SetDeadline(time.Now().Add(5 * time.Second))
        _, _ = conn.Write([]byte("NOT-HTTP AT ALL\r\n\r\n"))
        _ = conn.Close()

        // The channel must keep serving valid clients afterwards.
        client := ipcClient(ipc.Endpoint())
        resp, err := client.Get("http://localipc/api/health")
        if err != nil {
                t.Fatalf("health after garbage: %v", err)
        }
        resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                t.Fatalf("health after garbage status = %d", resp.StatusCode)
        }
}

func TestServeOversizedRequestHeadersRejected(t *testing.T) {
        srv, cfg := newCore(t)
        ipc := startIPC(t, srv, cfg, "")

        // A header beyond MaxHeaderBytes must be rejected by http.Server —
        // bounded framing is part of the resource-exhaustion contract.
        conn, err := dialEndpoint(ipc.Endpoint())
        if err != nil {
                t.Fatalf("dial: %v", err)
        }
        _ = conn.SetDeadline(time.Now().Add(5 * time.Second))

        big := bytes.Repeat([]byte("x"), 2<<20) // 2 MiB header line > 1 MiB cap
        req := append([]byte("GET /api/health HTTP/1.1\r\nBig: "), big...)
        req = append(req, []byte("\r\n\r\n")...)
        _, _ = conn.Write(req)

        buf := make([]byte, 512)
        n, readErr := conn.Read(buf)
        _ = conn.Close()

        if readErr == nil && n > 0 && !bytes.Contains(buf[:n], []byte("431")) && !bytes.Contains(buf[:n], []byte("400")) {
                t.Fatalf("oversized header answered %q, want 431/400", buf[:min(n, 64)])
        }

        // Endpoint still alive.
        client := ipcClient(ipc.Endpoint())
        resp, err := client.Get("http://localipc/api/health")
        if err != nil {
                t.Fatalf("health after oversized: %v", err)
        }
        resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                t.Fatalf("health after oversized status = %d", resp.StatusCode)
        }
}

func TestStopIsCleanAndIdempotent(t *testing.T) {
        srv, cfg := newCore(t)

        ipc, err := Serve(srv, cfg, "")
        if err != nil {
                t.Fatalf("localipc.Serve: %v", err)
        }

        ipc.Stop()
        ipc.Stop() // second Stop must be a no-op, not a panic

        // The endpoint must be gone (Unix: file unlinked; Windows: pipe closed).
        if os.PathSeparator != '\\' {
                if _, err := os.Stat(ipc.Endpoint()); !os.IsNotExist(err) {
                        t.Fatalf("endpoint file still present after Stop: %v", err)
                }
        }
}

func TestNoNetworkListenerIsOpened(t *testing.T) {
        endpoint, _, err := Endpoint()
        if err != nil {
                t.Fatalf("Endpoint: %v", err)
        }

        if strings.HasPrefix(endpoint, "tcp://") || strings.Contains(endpoint, "0.0.0.0") {
                t.Fatalf("endpoint %q must never be a TCP address", endpoint)
        }
}

func min(a, b int) int {
        if a < b {
                return a
        }
        return b
}
