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
// busy condition); an unresolved WaitNamedPipe proc or an unexpected
// wait failure (waiting is impossible; the loop never spins on a broken
// wait facility) surfaces the ORIGINAL busy error. A wait that TIMES OUT
// with every instance still busy is NOT an early exit (v1.11.1, run
// 43773885283): one per-wait cap is not the busy-dial budget, so the
// documented wait/retry contract continues — each wait clamped to the
// remaining budget — until the budget or the tighter caller deadline is
// spent. On exhaustion the terminal identity is attributed
// deterministically (v1.11.1, run 43862893851 repair): an observed — or
// arithmetically passed — caller deadline/cancellation returns the context
// error (errors.Is(err, context.DeadlineExceeded/Canceled) stays true);
// only a context that is still live when the limit lands returns the typed
// ErrBusyDialBudgetExhausted naming the budget and the elapsed time.
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
			// v1.11.1 (run 43862893851 repair): attribute the exhaustion
			// honestly BEFORE returning. The two hosted Windows failures
			// (TestDialContextCallerDeadlineBoundsBusyDial,
			// TestDialContextBusyDialCrossesWaitCap) both landed here with
			// the caller's deadline as the binding limit — 300ms and 2.75s,
			// each far under the 10s budget — yet received
			// ErrBusyDialBudgetExhausted, because this branch never
			// re-examined the caller context. Reaching it with the context
			// unobserved-as-done is a scheduling race, not a state decision:
			// the wait clamp truncates to whole milliseconds, so the final
			// sub-millisecond of a caller-deadline dial spends its last stretch
			// between the top-of-loop check and this branch while the runtime's
			// timer for the caller deadline is still being processed. The
			// identity of the terminal error is now decided deterministically:
			//
			//   1. an observed context error (cancellation takes precedence,
			//      then deadline) returns the context error — errors.Is(err,
			//      context.Canceled/DeadlineExceeded) stays true;
			//   2. a caller deadline that has passed arithmetically — even one
			//      whose timer has not yet published it — returns the context
			//      deadline error, because the binding limit was the CALLER's,
			//      never the app budget;
			//   3. only a context still live (no deadline, or one in the future)
			//      returns the typed ErrBusyDialBudgetExhausted: the app's OWN
			//      budget was the limit that expired.
			if cerr := ctx.Err(); cerr != nil {
				return nil, fmt.Errorf("localipc: dial %s: %w (busy %v): %w",
					name, cerr, time.Since(start).Round(time.Millisecond), err)
			}
			if d, ok := ctx.Deadline(); ok && !d.After(time.Now()) {
				return nil, fmt.Errorf("localipc: dial %s: %w (busy %v): %w",
					name, context.DeadlineExceeded, time.Since(start).Round(time.Millisecond), err)
			}
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
		// The wait did NOT report an instance. Distinguish the two honest
		// outcomes (v1.11.1, run 43773885283):
		if errors.Is(werr, windows.ERROR_SEM_TIMEOUT) {
			// The bounded readiness wait expired with every instance
			// still busy. ONE per-wait cap is not the busy-dial budget
			// (the defect this closes: a single wait timeout used to
			// return the generic "no listening pipe instance" error
			// after 2000ms of a 10s budget, leaving the typed
			// ErrBusyDialBudgetExhausted unreachable and
			// TestDialContextBusyBudgetExhaustsTyped red on the hosted
			// Windows runner). The documented client contract keeps
			// waiting — each wait clamped to the remaining budget —
			// until the budget, or the tighter caller deadline, is
			// spent; the top-of-loop deadline check then returns the
			// typed error. A timeout consumed waitMS of wall clock, so
			// the loop can never spin: every iteration advances toward
			// the deadline.
			continue
		}
		// The wait facility itself failed (unresolved proc) or returned
		// an unexpected error: waiting is impossible, so the budget
		// cannot be honored — surface the ORIGINAL busy error honestly
		// (never spin on a broken wait facility).
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
