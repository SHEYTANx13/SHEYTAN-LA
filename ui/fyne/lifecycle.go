//go:build !headless

// lifecycle.go — the ONE idempotent application shutdown coordinator
// (work order for run 43008361835 §F3/§5.2).
//
// Every supported exit path funnels through RequestClose:
//
//   - the user closes the window (X)  → Window.SetCloseIntercept → coordinator
//   - SIGINT/SIGTERM                  → internal/desktop signal handler →
//     App.Close() → coordinator
//   - programmatic close (tests,
//     diagnostic modes, future Quit)  → App.Close() → coordinator
//
// States: running → closing → closed. The running→closing transition is a
// single atomic CAS, so repeated/racing close requests share ONE teardown
// instead of starting it twice. Teardown runs on a coordinator WORKER
// goroutine — never the Fyne UI/event goroutine — and is bounded by a
// deadline. After bounded teardown the window is closed for real: Fyne
// v2.8.2's programmatic Window.Close() does NOT re-enter the close
// interceptor (only the user's window-close event does, via processClosed),
// so the coordinator cannot deadlock on its own intercept.
//
// Fyne contract notes (verified against the locked v2.8.2 sources):
//   - SetCloseIntercept replaces the X-close behavior; the interceptor must
//     not block the UI loop — here it only spawns/signals the coordinator.
//   - SetOnClosed runs inside Window.Close() while the window still exists;
//     it is used for final bookkeeping ONLY, never as the teardown owner.
//   - A teardown that exceeds its deadline is recorded as incomplete —
//     never as graceful — and the original failures are preserved with
//     errors.Join.
package fyneui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/startup"
)

// shutdownDeadline bounds the graceful teardown window (UI workers, activity
// pumps, workspace listener). The core/IPC servers close after Run() returns
// through internal/desktop's deferred, ordered shutdown — those have their
// own bounded implementations.
const shutdownDeadline = 5 * time.Second

type lifecycleState int32

const (
	lifecycleRunning lifecycleState = iota
	lifecycleClosing
	lifecycleClosed
)

func (s lifecycleState) String() string {
	switch s {
	case lifecycleRunning:
		return "running"
	case lifecycleClosing:
		return "closing"
	default:
		return "closed"
	}
}

// shutdownResult is the structured outcome of one teardown.
type shutdownResult struct {
	Graceful   bool
	Incomplete bool
	Err        error
	Duration   time.Duration
}

// shutdownCoordinator owns the running → closing → closed transition.
type shutdownCoordinator struct {
	app      *App
	deadline time.Duration
	state    atomic.Int32
	done     chan struct{}

	mu     sync.Mutex
	result shutdownResult
}

func newShutdownCoordinator(ap *App, deadline time.Duration) *shutdownCoordinator {
	return &shutdownCoordinator{app: ap, deadline: deadline, done: make(chan struct{})}
}

// currentState returns the current lifecycle state.
func (c *shutdownCoordinator) currentState() lifecycleState {
	return lifecycleState(c.state.Load())
}

// snapshotResult returns a copy of the structured teardown outcome.
func (c *shutdownCoordinator) snapshotResult() shutdownResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result
}

// closing reports whether a close has been requested (closing or closed).
func (c *shutdownCoordinator) closing() bool {
	return c.currentState() != lifecycleRunning
}

// requestClose is the single close entry point, safe from ANY goroutine
// (UI interceptor, signal handler, tests). Exactly one teardown executes;
// repeat and racing requests share its result.
func (c *shutdownCoordinator) requestClose() {
	if !c.state.CompareAndSwap(int32(lifecycleRunning), int32(lifecycleClosing)) {
		return // teardown already in flight or complete
	}
	go c.teardown()
}

// wait exposes the teardown-completion channel (tests, diagnostics).
func (c *shutdownCoordinator) wait() <-chan struct{} { return c.done }

// teardown is the coordinator worker: bounded, ordered, never on the UI
// goroutine.
func (c *shutdownCoordinator) teardown() {
	ap := c.app
	started := time.Now()
	var errs []error
	incomplete := false

	// 1. Cancel the app-scoped root context: in-flight loads, streams and
	//    background refreshes observe cancellation through their request
	//    contexts (stale results can never overwrite UI state afterwards).
	if ap.cancel != nil {
		ap.cancel()
	}

	// 2. Stop UI notifications and the activity subscription (no further
	//    view updates will be dispatched after this point).
	ap.rt.Shutdown()

	// 3. Bounded wait for the app-owned activity pump goroutines to observe
	//    cancellation. A deadline here is recorded as an INCOMPLETE
	//    shutdown — never graceful — and never blocks the window close.
	waitCtx, cancelWait := context.WithTimeout(context.Background(), c.deadline)
	if err := ap.rt.ShutdownWait(waitCtx); err != nil {
		errs = append(errs, fmt.Errorf("activity pumps: %w", err))
		incomplete = true
	}
	cancelWait()

	// 4. Release the workspace loopback listener (idempotent).
	ap.stopWorkspace()

	result := shutdownResult{
		Graceful:   !incomplete && len(errs) == 0,
		Incomplete: incomplete,
		Duration:   time.Since(started),
	}
	if len(errs) > 0 {
		result.Err = errors.Join(errs...)
	}
	c.mu.Lock()
	c.result = result
	c.mu.Unlock()

	// Record + log before the window closes so the evidence exists even if
	// the close itself fails. The UI-side teardown duration is recorded as
	// shutdown_ui; the FULL process shutdown (UI close → IPC stop → core
	// close) is recorded as `shutdown` by internal/desktop's final deferred.
	su := startup.Default()
	if result.Graceful {
		logging.Default().Info("lifecycle", "shutdown complete (graceful) in %s", result.Duration)
		su.RecordPhase(startup.PhaseShutdownUI, result.Duration, nil)
	} else {
		logging.Default().Error("lifecycle", "shutdown INCOMPLETE after %s: %v", result.Duration, result.Err)
		su.RecordPhase(startup.PhaseShutdownUI, result.Duration, context.DeadlineExceeded)
	}
	close(c.done)

	// 5. Close the window for real. Programmatic Close() bypasses the
	//    interceptor (Fyne v2.8.2 semantics) and is main-thread-safe; a
	//    racing second user X-close in flight is a no-op in the coordinator.
	if ap.w != nil {
		uiDo(ap.w.Close)
	}
}

// markClosed is the post-close bookkeeping transition (SetOnClosed). It is
// NOT the teardown owner and must not be relied on as the only shutdown
// signal — the coordinator may already have completed the teardown before
// the window actually closes.
func (c *shutdownCoordinator) markClosed() {
	c.state.CompareAndSwap(int32(lifecycleRunning), int32(lifecycleClosed))
	c.state.CompareAndSwap(int32(lifecycleClosing), int32(lifecycleClosed))
}

// RequestClose requests application shutdown from any goroutine. Before
// Run() arms the coordinator it degrades to a direct (marshalled) window
// close so early exits still work.
func (ap *App) RequestClose() {
	if ap.coord != nil {
		ap.coord.requestClose()
		return
	}
	if ap.w != nil {
		uiDo(ap.w.Close)
	}
}

// Close is the programmatic shutdown path (kept as the historical name used
// by internal/desktop's signal handler and tests). All closes funnel through
// the ONE coordinator.
func (ap *App) Close() { ap.RequestClose() }

// LifecycleState reports the coordinator state (running/closing/closed);
// "running" before Run() arms the coordinator.
func (ap *App) LifecycleState() string {
	if ap.coord == nil {
		return lifecycleRunning.String()
	}
	return ap.coord.currentState().String()
}

// resizeSmokeTarget is one matrix entry. clampExpected marks sizes the
// window system or the product minimum will legitimately NOT settle at
// exactly: sub-minimum sizes (the shell declares a 360x320 logical product
// minimum, plus canvas padding), degenerate sizes (0x0/1x1) and sizes beyond
// the display. For those the contract is: settle STABLY at the clamped size,
// never below the product minimum, no panic.
type resizeSmokeTarget struct {
	want          fyne.Size
	clampExpected bool
}

// resizeSmokeMatrix is the bounded logical-size matrix exercised by the
// controlled SHEYTAN_UI_RESIZE_SMOKE=1 mode: phones portrait/landscape,
// tablets, the three breakpoints' boundary pairs, laptops, desktop, wide,
// plus the degenerate/clamp probes.
var resizeSmokeMatrix = []resizeSmokeTarget{
	{want: fyne.NewSize(390, 844)},                        // phone portrait
	{want: fyne.NewSize(599, 800)},                        // one below the compact breakpoint
	{want: fyne.NewSize(600, 800)},                        // AT the compact breakpoint
	{want: fyne.NewSize(899, 600)},                        // top of the medium band
	{want: fyne.NewSize(900, 600)},                        // AT the desktop breakpoint
	{want: fyne.NewSize(1279, 720)},                       // top of the desktop band
	{want: fyne.NewSize(1280, 720)},                       // AT the wide breakpoint
	{want: fyne.NewSize(1366, 768)},                       // laptop
	{want: fyne.NewSize(768, 1024)},                       // tablet portrait
	{want: fyne.NewSize(844, 390)},                        // phone landscape (medium band)
	{want: fyne.NewSize(2560, 1440)},                      // wide
	{want: fyne.NewSize(3840, 2160), clampExpected: true}, // beyond a 2560x1440 display: display clamp allowed
	{want: fyne.NewSize(320, 568), clampExpected: true},   // below the 360x320 product minimum: minimum clamp expected
	{want: fyne.NewSize(320, 240), clampExpected: true},   // below minimum, degenerate height
	{want: fyne.NewSize(1, 1), clampExpected: true},       // degenerate
}

// resizeSmokeInterval is the bounded per-size settle wait used ONLY by the
// diagnostic resize-smoke mode (a readiness poll, not application logic).
const resizeSmokeInterval = 25 * time.Millisecond

// runResizeSmoke walks the logical-size matrix on a worker goroutine,
// resizes the real window through the UI loop, waits (bounded) for the
// canvas to reach each size and prints one stable evidence line per size:
//
//	UI-RESIZE-SMOKE target=390x844 canvas=390x844 zone=navTopStrip ok=1
//
// fyne.Do is ASYNCHRONOUS: canvas reads are marshalled through the UI loop
// and returned over a channel so the poll observes the APPLIED size (a
// fire-and-forget read would race the queue and always see the pre-resize
// value). A settled size is confirmed by two consecutive equal reads, so a
// transient canvas value can never count as a pass. clampExpected targets
// (sub-minimum/degenerate/beyond-display) pass when the canvas settles
// STABLY at the clamped size, never below the 360x320 product minimum.
// Exact targets get ONE bounded re-issue of the resize (a window system can
// transiently drop a single resize request under a WM-less X server).
//
// The mode ends with a summary line and a GRACEFUL close through the same
// coordinator users exercise. It must not change normal application
// behavior: with the environment switch unset none of this code runs.
func (ap *App) runResizeSmoke() {
	readCanvas := func() (fyne.Size, bool) {
		ch := make(chan fyne.Size, 1)
		uiDo(func() { ch <- ap.w.Canvas().Size() })
		select {
		case sz := <-ch:
			return sz, true
		case <-time.After(500 * time.Millisecond):
			return fyne.Size{}, false // UI loop stalled — reported honestly
		}
	}
	stable := func(sz fyne.Size) bool {
		time.Sleep(100 * time.Millisecond)
		a, okA := readCanvas()
		if !okA || a != sz {
			return false
		}
		time.Sleep(100 * time.Millisecond)
		b, okB := readCanvas()
		return okB && b == sz
	}
	const productMinW, productMinH = float32(360), float32(320)
	pass := 0
	for _, target := range resizeSmokeMatrix {
		want := target.want
		uiDo(func() { ap.w.Resize(want) })
		ok := false
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			got, served := readCanvas()
			if served && got == want && stable(got) {
				ok = true
				break
			}
			time.Sleep(resizeSmokeInterval)
		}
		if !ok && !target.clampExpected {
			// One bounded re-issue: a WM-less X server can transiently drop
			// a single resize request.
			uiDo(func() { ap.w.Resize(want) })
			deadline = time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				got, served := readCanvas()
				if served && got == want && stable(got) {
					ok = true
					break
				}
				time.Sleep(resizeSmokeInterval)
			}
		}
		got, _ := readCanvas()
		if !ok && target.clampExpected {
			// The clamp contract: settled stably at the clamped size, never
			// below the product minimum, no panic.
			ok = stable(got) && got.Width >= productMinW && got.Height >= productMinH
		}
		zone := navPolicy(got.Width)
		if ok {
			pass++
		}
		fmt.Printf("UI-RESIZE-SMOKE target=%dx%d canvas=%dx%d zone=%s ok=%t\n",
			int(want.Width), int(want.Height), int(got.Width), int(got.Height), zone, ok)
	}
	fmt.Printf("UI-RESIZE-SMOKE complete targets=%d pass=%d\n", len(resizeSmokeMatrix), pass)
	ap.RequestClose()
}

// resizeSmokeEnabled is the controlled diagnostic-mode switch (CI resize
// smoke; never set by normal application flow).
func resizeSmokeEnabled() bool { return os.Getenv("SHEYTAN_UI_RESIZE_SMOKE") == "1" }
