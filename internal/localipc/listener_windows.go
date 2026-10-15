//go:build windows

// Windows transport: named pipe with a current-user-only DACL, plus a
// post-connect client-process-SID verification. Implemented directly on
// golang.org/x/sys/windows (overlapped IO) — no third-party dependency.
//
// v1.10.6 TRANSPORT CONTRACT (the W1–W4 repair, run 38714950052 class):
//
//   - W1: listenEndpoint creates the FIRST pipe instance synchronously —
//     Serve returns only when the endpoint exists, mirroring Unix bind
//     semantics (listener_unix.go). An immediately dialing client can
//     never hit ERROR_FILE_NOT_FOUND.
//   - W2: Accept NEVER returns a connect timeout. The overlapped
//     ConnectNamedPipe is waited on indefinitely; the endpoint survives
//     arbitrarily long idle periods (the former 5000 ms fatal timeout
//     killed http.Serve — and with it the whole IPC endpoint — after 5
//     idle seconds).
//   - W3: Close cancels a pending overlapped accept via CancelIoEx, so a
//     clean Stop never hangs on the accept.
//   - W1/W4: a fresh instance is created AHEAD of demand — immediately
//     after a client connects — so the next dial never races instance
//     creation.
//   - W4: the identity decision (sidAllowed) is a pure, unit-tested
//     comparison against the current user's token SID; it fails closed on
//     an empty client SID.
package localipc

import (
	"errors"
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
	pipeOpenMode   = windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED
	pipeMode       = windows.PIPE_TYPE_BYTE | windows.PIPE_READMODE_BYTE | windows.PIPE_WAIT
	pipeInstances  = windows.PIPE_UNLIMITED_INSTANCES
	pipeInBufSize  = 1 << 20 // 1 MiB OS buffer; HTTP framing stays bounded by http.Server
	pipeOutBufSize = 1 << 20
	// W2: no connect timeout exists on the accept path. The former
	// connectTimeoutMS = 5000 const and its fatal error return are gone:
	// a connect wait ends only when a client arrives or the listener is
	// closed. Idle time is not an error condition on a listener.
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

// sidAllowed is the pure Windows identity decision — the named-pipe
// counterpart of the Unix credAllowed. It compares a client process SID
// string with the current user's token SID and is separated from the
// kernel-backed extraction (GetNamedPipeClientProcessId / OpenProcessToken)
// so the deny path is unit-testable without forging process tokens
// (documented seam: the kernel extraction stays production-owned; the
// DECISION is what the tests pin). An empty or whitespace client SID fails
// closed — the transport must never accept an unidentifiable peer.
func sidAllowed(clientSID string) (bool, error) {
	if strings.TrimSpace(clientSID) == "" {
		return false, nil
	}

	own, err := currentSID()
	if err != nil {
		return false, err
	}

	return clientSID == own, nil
}

// listenEndpoint creates the per-user named pipe server. The first pipe
// instance is created synchronously (W1): on return, the endpoint exists
// and a client dialing immediately succeeds — the same contract a Unix
// bind gives the Unix transport.
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
		name:   name,
		sa:     sa,
		dial:   func() (net.Conn, error) { return dialEndpoint(endpoint) },
		closed: make(chan struct{}),
		conns:  map[*pipeConn]struct{}{},
	}

	// W1: the synchronous first instance. Serve returns only when the
	// pipe exists.
	if err := ln.preCreate(); err != nil {
		return nil, err
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
// Accept anyway. A pre-created instance always sits between Accepts so a
// dialing client never races instance creation (W1/W4).
type pipeListener struct {
	name    string
	sa      windows.SecurityAttributes
	dial    func() (net.Conn, error)
	mu      sync.Mutex
	handle  windows.Handle // pre-created instance awaiting a client
	hasHandle bool
	handleErr error          // deferred instance-creation failure (surfaced by the next Accept)
	conns   map[*pipeConn]struct{}
	closed  chan struct{}
	once    sync.Once

	// The pending accept's overlapped state, published so Close can
	// CancelIoEx it (W3).
	pendingMu     sync.Mutex
	pendingHandle windows.Handle
	pendingOL     *windows.Overlapped
}

func (l *pipeListener) createInstanceLocked() (windows.Handle, error) {
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

// preCreate ensures one listening instance exists (W1/W4).
func (l *pipeListener) preCreate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hasHandle {
		return nil
	}
	handle, err := l.createInstanceLocked()
	if err != nil {
		return err
	}
	l.handle = handle
	l.hasHandle = true
	return nil
}

// setHandleErr defers an instance-creation failure to the next Accept so
// the CURRENT connection is still handed to http.Serve (the endpoint does
// not lose a live client because the NEXT instance could not be made).
func (l *pipeListener) setHandleErr(err error) {
	l.mu.Lock()
	l.handleErr = err
	l.mu.Unlock()
}

// takeInstance hands out the pre-created instance or creates one.
func (l *pipeListener) takeInstance() (windows.Handle, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.handleErr != nil {
		err := l.handleErr
		l.handleErr = nil
		return 0, err
	}
	if l.hasHandle {
		l.hasHandle = false
		handle := l.handle
		l.handle = 0
		return handle, nil
	}
	return l.createInstanceLocked()
}

func (l *pipeListener) Accept() (net.Conn, error) {
	for {
		select {
		case <-l.closed:
			return nil, net.ErrClosed
		default:
		}

		handle, err := l.takeInstance()
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

		// W2: wait for the client WITHOUT a fatal timeout. The wait ends
		// when a client connects or the listener is closed (W3: Close
		// aborts the pending accept via CancelIoEx). A connect error that
		// is NOT a client-gone race is fatal — that is an endpoint fault,
		// not an idle condition.
		if err := l.connectUntilClientOrClose(conn); err != nil {
			_ = conn.Close()
			select {
			case <-l.closed:
				return nil, net.ErrClosed
			default:
			}
			// ERROR_NO_DATA: the client connected and vanished before the
			// server side of the race resolved — transient, recreate the
			// instance and keep serving (the endpoint must outlive rude
			// clients; the former code killed the whole endpoint here).
			if errors.Is(err, windows.ERROR_NO_DATA) {
				continue
			}
			return nil, err
		}

		// Race guard: a client may connect exactly as Close fires.
		select {
		case <-l.closed:
			_ = conn.Close()
			return nil, net.ErrClosed
		default:
		}

		// W1/W4: create the NEXT listening instance immediately, before
		// the identity check and before this connection is handed to
		// http.Serve — the very next dial finds an instance waiting, so
		// the re-dial sequences in the malformed/oversized tests (and the
		// real UI's rapid successive requests) never race instance
		// creation.
		if perr := l.preCreate(); perr != nil {
			l.setHandleErr(perr)
		}

		// Post-connect identity check: the DACL already restricts the
		// namespace to the current user; this closes the remaining hole of
		// an elevated or service process in the same session. Fail-closed:
		// a rejected peer is closed and the accept loop continues.
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

// connectUntilClientOrClose waits for a client on the (already pre-created)
// pipe instance. The overlapped ConnectNamedPipe is waited on INDEFINITELY
// (W2 — the former 5s timeout was fatal to the whole endpoint); Close
// aborts the wait via CancelIoEx (W3).
func (l *pipeListener) connectUntilClientOrClose(c *pipeConn) error {
	evt, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("localipc: event: %w", err)
	}
	defer windows.CloseHandle(evt)

	var ol windows.Overlapped
	ol.HEvent = evt

	// Publish the pending state so Close can CancelIoEx it (W3).
	l.pendingMu.Lock()
	l.pendingHandle = c.handle
	l.pendingOL = &ol
	l.pendingMu.Unlock()
	defer func() {
		l.pendingMu.Lock()
		l.pendingHandle = 0
		l.pendingOL = nil
		l.pendingMu.Unlock()
	}()

	err = windows.ConnectNamedPipe(c.handle, &ol)
	if err == windows.ERROR_PIPE_CONNECTED {
		return nil // client already waiting on the pre-created instance
	}
	if err != nil && err != windows.ERROR_IO_PENDING {
		return fmt.Errorf("localipc: ConnectNamedPipe: %w", err)
	}

	type connectResult struct{ err error }
	done := make(chan connectResult, 1)
	go func() {
		s, werr := windows.WaitForSingleObject(evt, windows.INFINITE)
		if werr != nil {
			done <- connectResult{err: fmt.Errorf("localipc: wait connect: %w", werr)}
			return
		}
		if s != windows.WAIT_OBJECT_0 {
			done <- connectResult{err: fmt.Errorf("localipc: wait connect: unexpected state %d", s)}
			return
		}
		// The event fired: the connect completed, failed, or was aborted
		// (CancelIoEx sets the event and completes with ERROR_OPERATION_
		// ABORTED). GetOverlappedResult disambiguates.
		var n uint32
		if gerr := windows.GetOverlappedResult(c.handle, &ol, &n, false); gerr != nil {
			done <- connectResult{err: gerr}
			return
		}
		done <- connectResult{}
	}()

	select {
	case r := <-done:
		return r.err
	case <-l.closed:
		// W3: abort the pending accept so a clean Stop never hangs on it.
		_ = windows.CancelIoEx(c.handle, &ol)
		<-done // join the waiter — no goroutine leak
		return net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() {
		close(l.closed)

		// W3: cancel a pending overlapped accept. The closed channel makes
		// the Accept loop return net.ErrClosed; CancelIoEx unblocks the
		// OS-side wait so nothing hangs behind it.
		l.pendingMu.Lock()
		ph, pol := l.pendingHandle, l.pendingOL
		l.pendingMu.Unlock()
		if ph != 0 {
			_ = windows.CancelIoEx(ph, pol)
		}

		// Drop the pre-created, never-connected instance: the endpoint
		// must vanish with the last handle (the pipe-namespace lifecycle).
		l.mu.Lock()
		handle, has := l.handle, l.hasHandle
		l.handle, l.hasHandle, l.handleErr = 0, false, nil
		l.mu.Unlock()
		if has && handle != 0 {
			_ = windows.CloseHandle(handle)
		}

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
	handle       windows.Handle
	onClose      func()
	once         sync.Once
	muDeadline   sync.RWMutex
	readDeadline time.Time
}

func newPipeConn(handle windows.Handle) (*pipeConn, error) {
	return &pipeConn{handle: handle}, nil
}

// clientSID extracts the SID string of the client process behind the pipe.
// Kernel-backed by design; the DECISION on the extracted value lives in
// sidAllowed (the documented seam — see its comment).
func (c *pipeConn) clientSID() (string, error) {
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(c.handle, &pid); err != nil {
		return "", fmt.Errorf("localipc: client pid: %w", err)
	}

	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", fmt.Errorf("localipc: open client process: %w", err)
	}
	defer windows.CloseHandle(proc)

	var tok windows.Token
	if err := windows.OpenProcessToken(proc, windows.TOKEN_QUERY, &tok); err != nil {
		return "", fmt.Errorf("localipc: open client token: %w", err)
	}
	defer tok.Close()

	tu, err := tok.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("localipc: client token user: %w", err)
	}

	return tu.User.Sid.String(), nil
}

// verifyClientIsCurrentUser opens the client process behind the pipe,
// extracts its token user SID and delegates the fail-closed comparison to
// sidAllowed.
func (c *pipeConn) verifyClientIsCurrentUser() error {
	clientSID, err := c.clientSID()
	if err != nil {
		return err
	}

	ok, err := sidAllowed(clientSID)
	if err != nil {
		return err
	}
	if !ok {
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
