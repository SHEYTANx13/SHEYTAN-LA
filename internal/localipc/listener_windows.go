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
//   - W1/W4: a fresh instance is created AHEAD of demand — so the next
//     dial never races instance creation. v1.10.10 (W-INV, run 38919925013
//     step 14): the guarantee is now STRUCTURAL, not bookkeeping order —
//     the replacement instance is created inside the SAME locked section
//     that hands out the spare (takeInstance), so at least one listening
//     instance exists at EVERY instant from Serve's return until Close.
//     The kernel marks an instance busy at the client's CreateFile, BEFORE
//     any userspace completion notice, so a refill deferred to the accept
//     completion leaves a kernel-level zero-instance window — the exact
//     shape of the W-A hosted failure ("All pipe instances are busy"). An
//     instance created by CreateNamedPipe stays listening until a client
//     connects to it, even while the accept loop holds it.
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

// v1.10.7 (run 38731387959 class, W-C root cause): the client handle MUST be
// opened with FILE_FLAG_OVERLAPPED. pipeConn.Read/Write speak the overlapped
// contract (Overlapped structs + events + GetOverlappedResult) on EVERY conn,
// server- and client-side alike; issuing overlapped IO on a handle opened
// without FILE_FLAG_OVERLAPPED is outside the Windows contract (MSDN: the
// results are unpredictable). The canonical pipe client (x/sys winio
// DialPipe) opens with FILE_FLAG_OVERLAPPED for exactly this reason. The
// synchronous open here silently broke every client-side pipe round-trip on
// Windows while the Linux transport (net.Listen) never touches this path —
// the six-run Windows-only "Go verification" red.
//
// v1.10.10 (W-CLI, run 38919925013 step 14): dialEndpoint also implements the
// documented Windows named-pipe CLIENT contract for ERROR_PIPE_BUSY — the
// documented sequence (MSDN, Named Pipe Client): "If the pipe exists but all
// of its instances are busy, CreateFile returns INVALID_HANDLE_VALUE and the
// GetLastError function returns ERROR_PIPE_BUSY. When this happens, the
// named pipe client uses the WaitNamedPipe function to wait for an instance
// of the named pipe to become available." WaitNamedPipe is the OS's own
// readiness notification — the wait is event-driven, never a sleep-based
// poll. The retry is bounded (single-digit-seconds per wait, a small attempt
// count) and exhaustion returns the real error. This serves the production
// dial path (the real UI's transport behind http.Client), not any test: a UI
// issuing rapid successive requests through the same endpoint benefits
// identically. WaitNamedPipe is not exposed by golang.org/x/sys/windows, so
// it is bound directly from kernel32.dll (WaitNamedPipeW) — the documented
// exporting module; the canonical go-winio client binds it from kernel32.dll
// identically.
//
// v1.10.11 (W-BIND, run 38984879544 step 14): the v1.10.10 binding loaded
// "winbase.dll" — a module that does NOT exist as a loadable file on
// modern Windows (WinBase functions are exported by kernel32.dll; winbase
// is a HEADER, not a DLL). LazyDLL loads on first Call, so GOOS=windows
// build and vet passed locally while the first real busy wait panicked:
// LoadLibrary("winbase.dll") fails with ERROR_MOD_NOT_FOUND — message
// exactly "The specified module could not be found." — and LazyProc.Call
// panics on load failure, killing the whole test binary (hosted evidence,
// the win-go-test-headless FAIL; in production the same panic would kill
// the real UI's process on its first contended dial). The binding now
// targets kernel32.dll (Microsoft's WaitNamedPipe requirements: "Library:
// Kernel32.lib, DLL: Kernel32.dll"), and waitNamedPipe resolves the proc
// with Find() BEFORE any Call, returning a typed error on resolution
// failure — no LazyProc.Call panic path remains anywhere in the transport:
// a library dial path fails with an error, never kills the host process.
const (
	// pipeBusyWaitMS bounds ONE WaitNamedPipe readiness wait. Finite,
	// single-digit seconds: long enough for a saturated server's next
	// accept iteration to publish a listening instance, short enough that
	// a vanished server fails fast.
	pipeBusyWaitMS = 2000
	// pipeBusyMaxRetries caps the CreateFile retries after a busy wait —
	// a small bounded count, so the total dial budget stays finite and
	// exhaustion surfaces the real error instead of looping.
	pipeBusyMaxRetries = 3
)

var (
	// v1.10.11 (W-BIND): kernel32.dll is the documented exporting module
	// for WaitNamedPipeW. "winbase" is a header family (winbase.h), not a
	// loadable DLL — the former NewLazySystemDLL("winbase.dll") could
	// never load on modern Windows.
	modkernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procWaitNamedPipeW = modkernel32.NewProc("WaitNamedPipeW")
)

// ErrWaitNamedPipeUnresolved marks a failed WaitNamedPipeW proc resolution
// (a wrong module name or wrong proc name in a future binding regression,
// or the module genuinely absent). A transport dial must fail with an
// error — never kill the host process the way LazyProc.Call's load-failure
// panic does. When waitNamedPipe returns this error, the dial loop's
// bounded retries exhaust and surface the ORIGINAL busy error honestly.
var ErrWaitNamedPipeUnresolved = errors.New("localipc: WaitNamedPipeW proc unresolved")

// waitNamedPipe waits for a listening instance of the named pipe to become
// available (the OS's readiness notification — no sleeping, no polling).
// Returns nil when an instance became available within the timeout; the
// GetLastError value otherwise (ERROR_SEM_TIMEOUT on timeout,
// ERROR_FILE_NOT_FOUND when no pipe instance exists at all). The proc is
// resolved with Find() BEFORE any Call — a resolution failure returns
// ErrWaitNamedPipeUnresolved (a real error, never a panic), so the dial
// path stays non-panicking on every branch.
func waitNamedPipe(name string, timeoutMS uint32) error {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	if err := procWaitNamedPipeW.Find(); err != nil {
		return fmt.Errorf("%w: %v", ErrWaitNamedPipeUnresolved, err)
	}
	r1, _, e1 := procWaitNamedPipeW.Call(uintptr(unsafe.Pointer(p)), uintptr(timeoutMS))
	if r1 == 0 {
		if e1 != nil && e1 != windows.ERROR_SUCCESS {
			return e1
		}
		return windows.ERROR_SEM_TIMEOUT
	}
	return nil
}

func dialEndpoint(endpoint string) (net.Conn, error) {
	name := pipeName(endpoint)
	var lastBusy error
	for attempt := 0; ; attempt++ {
		handle, err := windows.CreateFile(
			windows.StringToUTF16Ptr(name),
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,   // exclusive
			nil, // no security attributes on the client side
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_OVERLAPPED,
			0,
		)
		if err == nil {
			return newPipeConn(handle)
		}
		if err != windows.ERROR_PIPE_BUSY {
			return nil, err
		}
		lastBusy = err
		if attempt >= pipeBusyMaxRetries {
			break
		}
		// The documented client contract: wait for an instance to become
		// available, then retry the open. A vanished endpoint
		// (ERROR_FILE_NOT_FOUND from the wait) cannot be retried into
		// existence — stop and surface the real error.
		if werr := waitNamedPipe(name, pipeBusyWaitMS); werr != nil && errors.Is(werr, windows.ERROR_FILE_NOT_FOUND) {
			break
		}
	}
	return nil, fmt.Errorf("localipc: dial %s: no listening pipe instance after %d busy waits: %w", name, pipeBusyMaxRetries, lastBusy)
}

// pipeListener accepts one instance at a time; http.Serve serializes
// Accept anyway. The instance inventory is governed by mu, and the W-INV
// invariant (v1.10.10) holds UNDER that lock: whenever an instance is
// handed out, the replacement is created in the same locked section, so at
// least one listening instance exists at every instant from Serve's return
// until Close — the kernel's connect-time busy marking can never expose a
// zero-instance window to a dialing client (W1/W4).
type pipeListener struct {
	name      string
	sa        windows.SecurityAttributes
	dial      func() (net.Conn, error)
	mu        sync.Mutex
	handle    windows.Handle // pre-created instance awaiting a client
	hasHandle bool
	handleErr error // deferred instance-creation failure (surfaced by the next Accept)
	conns     map[*pipeConn]struct{}
	closed    chan struct{}
	once      sync.Once

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

// preCreate ensures one listening instance exists (W1 — the synchronous
// first instance created by listenEndpoint before Serve ever returns).
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

// takeInstance hands out a listening instance AND creates the replacement
// in the SAME locked section (v1.10.10 W-INV, run 38919925013 step 14).
// The kernel marks the handed-out instance busy at the FIRST client's
// CreateFile — before this process observes any completion — so the
// replacement must already exist at hand-out time or a dialing client can
// hit ERROR_PIPE_BUSY during the server goroutine's wakeup latency (the
// exact hosted failure: "health after garbage: All pipe instances are
// busy"). An instance created by CreateNamedPipe stays listening until a
// client connects to it, even while the accept loop holds the handed-out
// one, so the invariant "at least one listening instance at every instant"
// holds by construction. A refill failure defers to the next Accept
// (handleErr) so the CURRENT connection is still handed to http.Serve —
// the endpoint does not lose a live client because the NEXT instance could
// not be made. Nothing is created after Close (the endpoint must vanish
// with the last handle).
func (l *pipeListener) takeInstance() (windows.Handle, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	select {
	case <-l.closed:
		return 0, net.ErrClosed
	default:
	}

	if l.handleErr != nil {
		err := l.handleErr
		l.handleErr = nil
		return 0, err
	}

	handle := l.handle
	has := l.hasHandle
	l.handle, l.hasHandle = 0, false
	if !has {
		// No spare exists (only possible on the very first Accept after a
		// listenEndpoint failure path or a consumed deferred error): create
		// the instance to hand out first.
		var err error
		if handle, err = l.createInstanceLocked(); err != nil {
			return 0, err
		}
	}

	// W-INV: the refill happens HERE, inside the lock that governs the
	// instance inventory — never after the connect completion. From this
	// instant, one instance is listening for the next client no matter when
	// the current one's kernel-level connect lands.
	if h, err := l.createInstanceLocked(); err != nil {
		l.handleErr = err // deferred to the next Accept; the hand-out proceeds
	} else {
		l.handle = h
		l.hasHandle = true
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
			// v1.10.9 (W-A, run 38871731277 step 14): the WHOLE vanished-peer
			// completion family is transient here. A client that connects and
			// dies before the overlapped ConnectNamedPipe resolves can surface
			// ERROR_NO_DATA (client handle closed), ERROR_BROKEN_PIPE, or
			// ERROR_PIPE_NOT_CONNECTED from the GetOverlappedResult completion
			// — v1.10.8 treated only ERROR_NO_DATA as transient, so this
			// family killed Accept, http.Serve, and the whole endpoint (the
			// malformed-garbage client is the only test client that closes
			// WITHOUT reading; its health redial then failed instantly,
			// matching the 0.06s fast-fail). The endpoint must outlive rude
			// clients: recreate the instance and keep serving. A genuine
			// endpoint fault stays fatal — see isVanishedPeer.
			if isVanishedPeer(err) {
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

		// W-INV (v1.10.10, run 38919925013 step 14): the NEXT listening
		// instance was already created inside takeInstance's locked
		// hand-out — there is no post-completion refill anymore. The
		// kernel marked the handed-out instance busy at the client's
		// CreateFile, BEFORE this completion notice; a refill HERE would
		// leave the zero-instance window that the hosted immediate redial
		// hit under full-suite load ("All pipe instances are busy").

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

// isVanishedPeer reports whether err belongs to the client-gone completion
// family on the accept path: the peer connected and vanished before the
// overlapped connect resolved, so the completion surfaces ERROR_NO_DATA
// (the client closed its handle), ERROR_BROKEN_PIPE, or
// ERROR_PIPE_NOT_CONNECTED (the peer of a completed connect is already
// gone). All three are TRANSIENT: the endpoint recreates the instance and
// keeps serving (the malformed-garbage contract — garbage must not kill
// the endpoint). ERROR_OPERATION_ABORTED deliberately stays fatal: it
// means OUR IO was cancelled (the W3 close path is already filtered by
// the l.closed select before this check), which is an endpoint fault, not
// an idle event.
func isVanishedPeer(err error) bool {
	return errors.Is(err, windows.ERROR_NO_DATA) ||
		errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED)
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr(l.name) }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

// --- pipeConn: net.Conn over an overlapped named-pipe handle ---
//
// Deadline support follows the CancelIoEx convention: SetReadDeadline with
// a past-due time cancels a pending read immediately; a FUTURE deadline arms
// a bounded timer that cancels the in-flight read when it passes; timed-out
// waits translate to os.ErrDeadlineExceeded (v1.10.7: deadlines are now
// enforced while a read is pending, not only before it starts).

type pipeConn struct {
	handle       windows.Handle
	onClose      func()
	once         sync.Once
	muDeadline   sync.RWMutex
	readDeadline time.Time
	// v1.10.7: a FUTURE read deadline must fire while a ReadFile is
	// pending. CancelIoEx is the only mechanism that unblocks an
	// in-flight overlapped read, so the deadline arms a bounded timer
	// that cancels the IO when the deadline passes (the pre-v1.10.7
	// code only cancelled when the deadline was ALREADY expired at
	// set time, which made http.Server's ReadHeaderTimeout dead code
	// on the Windows transport).
	readTimer *time.Timer
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
	case errors.Is(err, windows.ERROR_OPERATION_ABORTED):
		// v1.10.7: the read was cancelled. A passed deadline is the
		// canonical net.Conn timeout answer; an abort WITHOUT a passed
		// deadline (pipeConn.Close) is a real error the server must
		// see as such.
		c.muDeadline.RLock()
		expired := !c.readDeadline.IsZero() && time.Now().After(c.readDeadline)
		c.muDeadline.RUnlock()
		if expired {
			return 0, os.ErrDeadlineExceeded
		}
		return 0, fmt.Errorf("localipc: read aborted: %w", err)
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
		// v1.10.9 (W-B, latent, fixed regardless of W-A's outcome):
		// stop the deadline timer FIRST, under the deadline mutex, and
		// clear the deadline the callback re-checks. SetReadDeadline
		// arms time.AfterFunc(..., CancelIoEx(handle, nil)) for future
		// deadlines; a timer that outlives this connection fires on a
		// RECYCLED handle value seconds later and aborts an unrelated
		// live connection's in-flight IO. Stop() is best-effort (the
		// callback may already be running), so clearing readDeadline
		// under the same lock also disarms an in-flight callback's
		// armed re-check.
		c.muDeadline.Lock()
		if c.readTimer != nil {
			c.readTimer.Stop()
			c.readTimer = nil
		}
		c.readDeadline = time.Time{}
		c.muDeadline.Unlock()
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
	if c.readTimer != nil {
		c.readTimer.Stop()
		c.readTimer = nil
	}
	now := time.Now()
	switch {
	case t.IsZero():
		// Deadline cleared: leave any pending read alone.
		c.muDeadline.Unlock()
	case now.After(t):
		// Already expired: unblock any pending read immediately.
		c.muDeadline.Unlock()
		_ = windows.CancelIoEx(c.handle, nil)
	default:
		// Future deadline: arm the bounded cancel that unblocks an
		// in-flight overlapped read exactly when the deadline passes.
		handle := c.handle
		c.readTimer = time.AfterFunc(t.Sub(now), func() {
			c.muDeadline.RLock()
			armed := c.readDeadline.Equal(t) && time.Now().After(t)
			c.muDeadline.RUnlock()
			if armed {
				_ = windows.CancelIoEx(handle, nil)
			}
		})
		c.muDeadline.Unlock()
	}
	return nil
}
