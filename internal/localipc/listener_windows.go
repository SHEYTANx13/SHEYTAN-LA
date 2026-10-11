//go:build windows

// Windows transport: named pipe with a current-user-only DACL, plus a
// post-connect client-process-SID verification. Implemented directly on
// golang.org/x/sys/windows (overlapped IO) — no third-party dependency.
package localipc

import (
        "fmt"
        "io"
        "net"
        "os"
        "strings"
        "sync"
        "time"
        "unsafe"

        "golang.org/x/sys/windows"
)

const (
        pipeOpenMode     = windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED
        pipeMode         = windows.PIPE_TYPE_BYTE | windows.PIPE_READMODE_BYTE | windows.PIPE_WAIT
        pipeInstances    = windows.PIPE_UNLIMITED_INSTANCES
        pipeInBufSize    = 1 << 20 // 1 MiB OS buffer; HTTP framing stays bounded by http.Server
        pipeOutBufSize   = 1 << 20
        connectTimeoutMS = 5000
)

// currentSID returns the SDDL string of the current user's SID.
func currentSID() (string, error) {
        var tok windows.Token
        if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &tok); err != nil {
                return "", fmt.Errorf("localipc: open process token: %w", err)
        }
        defer tok.Close()

        tu, err := tok.GetTokenUser()
        if err != nil {
                return "", fmt.Errorf("localipc: token user: %w", err)
        }

        return tu.User.Sid.String(), nil
}

// listenEndpoint creates the per-user named pipe server.
func listenEndpoint(endpoint string) (net.Listener, error) {
        sid, err := currentSID()
        if err != nil {
                return nil, err
        }

        // SDDL: protected DACL, grant GENERIC_ALL to the current user only.
        sddl := "D:P(A;;GA;;;" + sid + ")"
        sd, err := windows.SecurityDescriptorFromString(sddl)
        if err != nil {
                return nil, fmt.Errorf("localipc: security descriptor: %w", err)
        }

        var sa windows.SecurityAttributes
        sa.Length = uint32(unsafe.Sizeof(sa))
        sa.InheritHandle = 0
        sa.SecurityDescriptor = sd

        name := pipeName(endpoint)

        ln := &pipeListener{
                name:    name,
                sa:      sa,
                dial:    func() (net.Conn, error) { return dialEndpoint(endpoint) },
                closed:  make(chan struct{}),
                conns:   map[*pipeConn]struct{}{},
        }
        return ln, nil
}

// pipeName normalizes the endpoint string to the Win32 pipe namespace.
func pipeName(endpoint string) string {
        if strings.HasPrefix(strings.ToLower(endpoint), `\\.\pipe\`) {
                return endpoint
        }
        return `\\.\pipe\` + strings.TrimPrefix(endpoint, `\`)
}

func dialEndpoint(endpoint string) (net.Conn, error) {
        handle, err := windows.CreateFile(
                windows.StringToUTF16Ptr(pipeName(endpoint)),
                windows.GENERIC_READ|windows.GENERIC_WRITE,
                0,   // exclusive
                nil, // no security attributes on the client side
                windows.OPEN_EXISTING,
                0,
                0,
        )
        if err != nil {
                return nil, err
        }
        return newPipeConn(handle)
}

// pipeListener accepts one instance at a time; http.Serve serializes
// Accept anyway, and each accepted instance immediately becomes a live
// stream connection while the next instance is created.
type pipeListener struct {
        name    string
        sa      windows.SecurityAttributes
        dial    func() (net.Conn, error)
        mu      sync.Mutex
        handle  windows.Handle
        created bool
        conns   map[*pipeConn]struct{}
        closed  chan struct{}
        once    sync.Once
}

func (l *pipeListener) createInstance() (windows.Handle, error) {
        handle, err := windows.CreateNamedPipe(
                windows.StringToUTF16Ptr(l.name),
                pipeOpenMode,
                pipeMode,
                pipeInstances,
                pipeInBufSize,
                pipeOutBufSize,
                0,
                &l.sa,
        )
        if err != nil {
                return 0, fmt.Errorf("localipc: CreateNamedPipe: %w", err)
        }
        return handle, nil
}

func (l *pipeListener) Accept() (net.Conn, error) {
        for {
                select {
                case <-l.closed:
                        return nil, net.ErrClosed
                default:
                }

                handle, err := l.createInstance()
                if err != nil {
                        select {
                        case <-l.closed:
                                return nil, net.ErrClosed
                        default:
                                return nil, err
                        }
                }

                conn, err := newPipeConn(handle)
                if err != nil {
                        windows.CloseHandle(handle)
                        return nil, err
                }

                if err := conn.connectOverlapped(); err != nil {
                        _ = conn.Close()
                        select {
                        case <-l.closed:
                                return nil, net.ErrClosed
                        default:
                                return nil, err
                        }
                }

                // Post-connect identity check: the DACL already restricts the
                // namespace to the current user; this closes the remaining hole of
                // an elevated or service process in the same session.
                if err := conn.verifyClientIsCurrentUser(); err != nil {
                        _ = conn.Close()
                        continue
                }

                l.mu.Lock()
                l.conns[conn] = struct{}{}
                l.mu.Unlock()

                conn.onClose = func() {
                        l.mu.Lock()
                        delete(l.conns, conn)
                        l.mu.Unlock()
                }

                return conn, nil
        }
}

func (l *pipeListener) Close() error {
        l.once.Do(func() {
                close(l.closed)

                l.mu.Lock()
                conns := make([]*pipeConn, 0, len(l.conns))
                for c := range l.conns {
                        conns = append(conns, c)
                }
                l.mu.Unlock()

                for _, c := range conns {
                        _ = c.Close()
                }
        })
        return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr(l.name) }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

// --- pipeConn: net.Conn over an overlapped named-pipe handle ---
//
// Deadline support follows the CancelIoEx convention: SetReadDeadline with
// a past/zero-due time cancels a pending read; timed-out waits translate to
// os.ErrDeadlineExceeded.

type pipeConn struct {
        handle      windows.Handle
        onClose     func()
        once        sync.Once
        muDeadline  sync.RWMutex
        readDeadline time.Time
}

func newPipeConn(handle windows.Handle) (*pipeConn, error) {
        return &pipeConn{handle: handle}, nil
}

// connectOverlapped starts (and bounded-waits for) the client connection.
func (c *pipeConn) connectOverlapped() error {
        evt, err := windows.CreateEvent(nil, 1, 0, nil)
        if err != nil {
                return fmt.Errorf("localipc: event: %w", err)
        }
        defer windows.CloseHandle(evt)

        var ol windows.Overlapped
        ol.HEvent = evt

        err = windows.ConnectNamedPipe(c.handle, &ol)
        if err == windows.ERROR_PIPE_CONNECTED {
                return nil // client already waiting
        }
        if err != nil && err != windows.ERROR_IO_PENDING {
                return fmt.Errorf("localipc: ConnectNamedPipe: %w", err)
        }

        s, err := windows.WaitForSingleObject(evt, connectTimeoutMS)
        if err != nil {
                return fmt.Errorf("localipc: wait connect: %w", err)
        }
        if s != windows.WAIT_OBJECT_0 {
                _ = windows.CancelIoEx(c.handle, &ol)
                return fmt.Errorf("localipc: connect timeout")
        }
        return nil
}

// verifyClientIsCurrentUser opens the client process behind the pipe and
// compares its token user SID with our own.
func (c *pipeConn) verifyClientIsCurrentUser() error {
        var pid uint32
        if err := windows.GetNamedPipeClientProcessId(c.handle, &pid); err != nil {
                return fmt.Errorf("localipc: client pid: %w", err)
        }

        proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
        if err != nil {
                return fmt.Errorf("localipc: open client process: %w", err)
        }
        defer windows.CloseHandle(proc)

        var tok windows.Token
        if err := windows.OpenProcessToken(proc, windows.TOKEN_QUERY, &tok); err != nil {
                return fmt.Errorf("localipc: open client token: %w", err)
        }
        defer tok.Close()

        tu, err := tok.GetTokenUser()
        if err != nil {
                return err
        }

        myTok, err := currentSID()
        if err != nil {
                return err
        }
        if myTok != tu.User.Sid.String() {
                return ErrUnauthorized
        }
        return nil
}

// ioEOF is the canonical pipe end-of-stream error.
var ioEOF = io.EOF

func (c *pipeConn) ioEvent() (windows.Handle, func()) {
        evt, err := windows.CreateEvent(nil, 1, 0, nil)
        if err != nil {
                return 0, nil
        }
        return evt, func() { _ = windows.CloseHandle(evt) }
}

func (c *pipeConn) Read(p []byte) (int, error) {
        if len(p) == 0 {
                return 0, nil
        }

        c.muDeadline.RLock()
        expired := !c.readDeadline.IsZero() && time.Now().After(c.readDeadline)
        c.muDeadline.RUnlock()
        if expired {
                return 0, os.ErrDeadlineExceeded
        }

        evt, done := c.ioEvent()
        if done == nil {
                return 0, fmt.Errorf("localipc: read event: %w", windows.ERROR_NOT_ENOUGH_MEMORY)
        }
        defer done()

        var ol windows.Overlapped
        ol.HEvent = evt

        var n uint32
        err := windows.ReadFile(c.handle, p, &n, &ol)
        if err == windows.ERROR_IO_PENDING {
                s, werr := windows.WaitForSingleObject(evt, windows.INFINITE)
                if werr != nil {
                        return 0, werr
                }
                if s != windows.WAIT_OBJECT_0 {
                        _ = windows.CancelIoEx(c.handle, &ol)
                        return 0, os.ErrDeadlineExceeded
                }
                err = windows.GetOverlappedResult(c.handle, &ol, &n, false)
        }

        switch {
        case err == nil:
                if n == 0 {
                        return 0, ioEOF
                }
                return int(n), nil
        case err == windows.ERROR_BROKEN_PIPE || err == windows.ERROR_NO_DATA:
                return 0, ioEOF
        default:
                return 0, fmt.Errorf("localipc: read: %w", err)
        }
}

func (c *pipeConn) Write(p []byte) (int, error) {
        if len(p) == 0 {
                return 0, nil
        }

        evt, done := c.ioEvent()
        if done == nil {
                return 0, fmt.Errorf("localipc: write event: %w", windows.ERROR_NOT_ENOUGH_MEMORY)
        }
        defer done()

        var ol windows.Overlapped
        ol.HEvent = evt

        total := 0
        for total < len(p) {
                var n uint32
                err := windows.WriteFile(c.handle, p[total:], &n, &ol)
                if err == windows.ERROR_IO_PENDING {
                        s, werr := windows.WaitForSingleObject(evt, windows.INFINITE)
                        if werr != nil {
                                return total, werr
                        }
                        if s != windows.WAIT_OBJECT_0 {
                                _ = windows.CancelIoEx(c.handle, &ol)
                                return total, os.ErrDeadlineExceeded
                        }
                        err = windows.GetOverlappedResult(c.handle, &ol, &n, false)
                }
                if err == windows.ERROR_BROKEN_PIPE || err == windows.ERROR_NO_DATA {
                        return total, ioEOF
                }
                if err != nil {
                        return total, fmt.Errorf("localipc: write: %w", err)
                }
                if n == 0 {
                        return total, ioEOF
                }
                total += int(n)
        }
        return total, nil
}

func (c *pipeConn) Close() error {
        c.once.Do(func() {
                _ = windows.CancelIoEx(c.handle, nil)
                _ = windows.CloseHandle(c.handle)
                if c.onClose != nil {
                        c.onClose()
                }
        })
        return nil
}

func (c *pipeConn) LocalAddr() net.Addr  { return nil }
func (c *pipeConn) RemoteAddr() net.Addr { return nil }

func (c *pipeConn) SetDeadline(t time.Time) error {
        if err := c.SetReadDeadline(t); err != nil {
                return err
        }
        return c.SetWriteDeadline(t)
}

func (c *pipeConn) SetWriteDeadline(t time.Time) error { return nil }

func (c *pipeConn) SetReadDeadline(t time.Time) error {
        c.muDeadline.Lock()
        c.readDeadline = t
        c.muDeadline.Unlock()

        if !t.IsZero() && time.Now().After(t) {
                // Already expired: unblock any pending read.
                _ = windows.CancelIoEx(c.handle, nil)
        }
        return nil
}
