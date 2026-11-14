//go:build windows

package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/sys/windows"
)

// dialContext is the Windows half of the canonical client dial
// (v1.11.1 Windows IPC repair). It opens the named pipe with
// FILE_FLAG_OVERLAPPED and implements the documented Windows named-pipe
// CLIENT contract for ERROR_PIPE_BUSY (Microsoft, Named Pipe Client):
// attempt CreateFile; on ERROR_PIPE_BUSY call WaitNamedPipe and — while
// the wait reports an instance becoming available — retry CreateFile
// immediately. There is NO attempt count (v1.10.12, W-BUDGET): the only
// bounds are the caller's context deadline and pipeBusyDialBudget,
// whichever is TIGHTER (v1.11.1: the caller deadline is the tighter
// limit); each individual wait stays capped by pipeBusyWaitMS and is
// clamped to the remaining budget, so the stated total is honest.
//
// Honest early exits: a CreateFile error other than ERROR_PIPE_BUSY
// returns as-is (ERROR_FILE_NOT_FOUND — the pipe does not exist — is
// never retried into existence); a wait that reports ERROR_FILE_NOT_FOUND
// returns that error (the pipe name vanished — the server is gone; not a
// busy condition); a wait that times out with no instance (the server
// stopped publishing — a real failure) or an unresolved WaitNamedPipe
// proc (waiting is impossible; the loop never spins on a broken wait
// facility) surfaces the ORIGINAL busy error. On deadline exhaustion the
// typed ErrBusyDialBudgetExhausted names the budget and the elapsed time;
// if the binding limit was the caller's context, the context error is
// returned (errors.Is(err, context.DeadlineExceeded) stays true).
//
// Context cancellation is checked before the first attempt, before every
// readiness wait, and after a successful CreateFile (a connection the
// caller already abandoned is closed, matching net.Dialer semantics). A
// cancellation that lands while one bounded readiness wait is outstanding
// terminates the dial when that wait returns — the wait is the OS's
// readiness notification, never a sleep-based poll, and is clamped to the
// remaining budget so the total stays within the stated deadline.
func dialContext(ctx context.Context, endpoint string) (net.Conn, error) {
	name := pipeName(endpoint)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("localipc: dial %s: %w", name, err)
	}

	// The tighter of the caller deadline and the documented busy-dial
	// budget bounds the whole loop.
	deadline := time.Now().Add(pipeBusyDialBudget)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	start := time.Now()
	for {
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
			// The caller's context may have fired while the connect was
			// completing; honor net.Dialer semantics and hand back the
			// cancellation instead of a connection the caller abandoned.
			if cerr := ctx.Err(); cerr != nil {
				_ = windows.CloseHandle(handle)
				return nil, fmt.Errorf("localipc: dial %s: %w", name, cerr)
			}
			return newPipeConn(handle)
		}
		if err != windows.ERROR_PIPE_BUSY {
			return nil, fmt.Errorf("localipc: dial %s: %w", name, err)
		}

		// A canceled or deadlined caller never enters another wait.
		if cerr := ctx.Err(); cerr != nil {
			return nil, fmt.Errorf("localipc: dial %s: %w (busy %v): %w",
				name, cerr, time.Since(start).Round(time.Millisecond), err)
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("localipc: dial %s: %w (%v budget, %v elapsed): %w",
				name, ErrBusyDialBudgetExhausted, pipeBusyDialBudget,
				time.Since(start).Round(time.Millisecond), err)
		}

		// The documented client contract: wait for an instance to become
		// available, then retry the open. The wait is the OS's readiness
		// notification — event-driven, never a sleep-based poll — and is
		// clamped to the remaining budget so the total stays within the
		// stated deadline.
		waitMS := uint32(pipeBusyWaitMS)
		if remMS := uint32(remaining / time.Millisecond); remMS < waitMS {
			waitMS = remMS
		}
		if waitMS == 0 {
			// Sub-millisecond budget left: one more open attempt, then
			// the deadline check above exits.
			continue
		}
		werr := waitNamedPipe(name, waitMS)
		if werr == nil {
			// An instance became available: retry CreateFile NOW — no
			// attempt counter, the deadline is the only bound.
			continue
		}
		// The wait was clamped to the remaining budget, so a timeout here
		// means the budget — and any caller deadline behind it — has
		// passed: surface the context error first when one exists.
		if cerr := ctx.Err(); cerr != nil {
			return nil, fmt.Errorf("localipc: dial %s: %w (busy %v): %w",
				name, cerr, time.Since(start).Round(time.Millisecond), err)
		}
		if errors.Is(werr, windows.ERROR_FILE_NOT_FOUND) {
			// The pipe name vanished — the server is gone. That error
			// is returned honestly; it is not a busy condition.
			return nil, fmt.Errorf("localipc: dial %s: pipe vanished while busy-waiting: %w", name, werr)
		}
		// The wait did NOT report an instance: a timeout (the server
		// stopped publishing — a real failure) or an unresolved proc
		// (waiting is impossible). Surface the ORIGINAL busy error
		// honestly.
		return nil, fmt.Errorf("localipc: dial %s: no listening pipe instance after a %dms busy wait: %w", name, waitMS, err)
	}
}

// dialEndpoint opens a connection to the endpoint with no caller context
// (background deadline semantics: the full documented busy-dial budget).
// It remains the seam for the listener's self-dial probe paths;
// production clients call the public DialContext.
func dialEndpoint(endpoint string) (net.Conn, error) {
	return dialContext(context.Background(), endpoint)
}
