// Package browser manages a persistent, human-like Chromium session for the
// agent's browser-automation tool. It discovers Chrome/Edge on Windows (Edge
// ships with Windows 10/11, so this always works) or Chromium on Linux,
// boots it once via chromedp (Chrome DevTools Protocol), keeps it alive
// across tool calls, and restarts it automatically if it crashes.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	cdp "github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/proc"
)

// userAgent returns a realistic Chrome UA matching the host platform so the
// session looks like a human's browser, not an automation harness.
func userAgent() string {
	if runtime.GOOS == "windows" {
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"
	}
	if runtime.GOOS == "darwin" {
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"
	}
	return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"
}

// stealthJS is injected into every new document to keep the session
// human-like: navigator.webdriver is removed (CDP sets it to true), and
// common automation tells are smoothed over.
const stealthJS = `
Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
window.chrome = window.chrome || { runtime: {} };
Object.defineProperty(navigator, 'languages', {get: () => ['en-US', 'en']});
Object.defineProperty(navigator, 'plugins', {get: () => [1, 2, 3, 4, 5]});
`

// FindChrome returns the best available Chromium-based browser executable.
// override (from config) wins; otherwise well-known install paths are probed.
func FindChrome(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
		return "", fmt.Errorf("configured browser executable not found: %s", override)
	}

	var candidates []string
	home, _ := os.UserHomeDir()

	if runtime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles")
		pf86 := os.Getenv("ProgramFiles(x86)")
		lad := os.Getenv("LocalAppData")
		candidates = []string{
			filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(lad, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf86, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(lad, `Microsoft\Edge\Application\msedge.exe`),
		}
	} else {
		candidates = []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		}
		// Playwright-bundled chromium (dev machines / CI) — pick newest.
		var hits []string
		glob := filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux*", "chrome")
		if paths, err := filepath.Glob(glob); err == nil {
			hits = append(hits, paths...)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(hits)))
		candidates = append(candidates, hits...)
	}

	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("no Chromium browser found (tried %d known paths). "+
		"Install Google Chrome or Microsoft Edge, or set browserExecutablePath in the config", len(candidates))
}

// Session is a persistent Chromium session. Zero value is not usable —
// call NewSession.
type Session struct {
	execPath   string
	headless   bool
	slowMo     time.Duration
	profileDir string

	mu          sync.Mutex
	allocCtx    context.Context
	allocCancel context.CancelFunc
	ctx         context.Context
	cancel      context.CancelFunc
	startedAt   time.Time
	navigations int
}

// NewSession boots a browser session with a persistent profile (kept inside
// the app folder). The browser process is started lazily on first use and
// kept alive until Close. The ctx bounds startup so a hung Chrome fails
// fast instead of blocking the agent forever.
func NewSession(ctx context.Context, execPath, profileDir string, headless bool, slowMo time.Duration) (*Session, error) {
	s := &Session{
		execPath:   execPath,
		headless:   headless,
		slowMo:     slowMo,
		profileDir: profileDir,
	}
	if err := s.start(ctx); err != nil {
		return nil, err
	}
	logging.Default().Info("browser", "session started (headless=%v, exe=%s, profile=%s)", headless, execPath, profileDir)
	return s, nil
}

// start boots the allocator + browser. Caller must hold s.mu (or be single-
// threaded during construction).
func (s *Session) start(ctx context.Context) error {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(s.execPath),
		// override the harness-y defaults for human-like behavior:
		chromedp.Flag("headless", s.headless),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-infobars", true),
		chromedp.WindowSize(1366, 900),
		chromedp.UserAgent(userAgent()),
	)
	if s.profileDir != "" {
		// Persistent profile inside the app folder: cookies, logins and
		// history survive restarts and the whole app stays portable.
		if err := os.MkdirAll(s.profileDir, 0o755); err == nil {
			opts = append(opts, chromedp.UserDataDir(s.profileDir))
		}
	}
	if runtime.GOOS != "windows" {
		// Playwright chromium in containers needs --no-sandbox.
		opts = append(opts, chromedp.NoSandbox)
	}
	// v1.0.4: Chrome is a console-subsystem binary on Windows — without
	// this hook EVERY browser tool call flashes (or holds, in headed
	// mode) a console window. ModifyCmdFunc replaces chromedp's (empty)
	// non-Linux default with our hidden-console attributes.
	opts = append(opts, chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
		proc.Hide(cmd)
	}))
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	if ctx.Err() != nil { // startup already timed out / cancelled
		cancel()
		allocCancel()
		return fmt.Errorf("browser startup cancelled: %w", ctx.Err())
	}

	// Boot the browser + first blank tab (chromedp v0.23 API: Run takes
	// exactly one action — an empty Steps() sequence allocates and boots
	// without touching the page).
	if _, err := chromedp.Run(ctx, chromedp.Steps()); err != nil {
		cancel()
		allocCancel()
		return fmt.Errorf("start browser: %w", err)
	}
	// Inject the stealth script into every future document (chromedp
	// v0.23 / cdproto v0.160 API: protocol commands execute through
	// cdp.Call against the session target inside a plain action func).
	if _, err := chromedp.Run(ctx, func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		_, err := cdp.Call(ctx, t, page.AddScriptToEvaluateOnNewDocument,
			page.AddScriptToEvaluateOnNewDocumentParams{Source: stealthJS})
		return chromedp.Void{}, err
	}); err != nil {
		logging.Default().Warn("browser", "stealth script injection failed: %v", err)
	}
	s.allocCtx, s.allocCancel = allocCtx, allocCancel
	s.ctx, s.cancel = ctx, cancel
	s.startedAt = time.Now()
	return nil
}

// restart tears the browser down and boots a fresh one.
func (s *Session) restart() {
	s.stopLocked()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.start(ctx); err != nil {
		logging.Default().Error("browser", "restart failed: %v", err)
	}
}

func (s *Session) stopLocked() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}
	s.ctx, s.cancel = nil, nil
	s.allocCtx, s.allocCancel = nil, nil
}

// target returns the live session context, restarting a dead browser
// once. Callers use the returned context for their chromedp calls.
func (s *Session) target() (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil {
		s.restart()
		if s.ctx == nil {
			return nil, fmt.Errorf("browser session unavailable")
		}
	}
	return s.ctx, nil
}

// Do runs chromedp VOID actions in the live session. If the browser has
// died, it is restarted once and the actions retried (chromedp v0.23 API:
// value-returning work goes through the package-level Run helper, which
// shares this restart-once discipline).
func (s *Session) Do(parent context.Context, steps ...chromedp.Action[chromedp.Void]) error {
	ctx, err := s.target()
	if err != nil {
		return err
	}
	if err := chromedp.Do(ctx, steps...); err != nil && isDeadSession(ctx, err) {
		logging.Default().Warn("browser", "browser died mid-action (%v); restarting and retrying once", err)
		ctx2, terr := s.target()
		if terr != nil {
			return terr
		}
		return chromedp.Do(ctx2, steps...)
	} else {
		return err
	}
}

// Run executes ONE value-returning chromedp action in the live session
// with the same restart-once discipline as Do. Go methods cannot be
// generic, so this is a package-level helper that receives the session
// explicitly (chromedp v0.23 API: Run(ctx, action) returns the action's
// value).
func Run[T any](s *Session, parent context.Context, a chromedp.Action[T]) (T, error) {
	ctx, err := s.target()
	if err != nil {
		var zero T
		return zero, err
	}
	v, err := chromedp.Run(ctx, a)
	if err != nil && isDeadSession(ctx, err) {
		logging.Default().Warn("browser", "browser died mid-action (%v); restarting and retrying once", err)
		ctx2, terr := s.target()
		if terr != nil {
			var zero T
			return zero, terr
		}
		return chromedp.Run(ctx2, a)
	}
	return v, err
}

func isDeadSession(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "target closed") ||
		strings.Contains(msg, "websocket") ||
		strings.Contains(msg, "browser has exited") ||
		strings.Contains(msg, "context canceled")
}

// Alive reports whether the session currently holds a live browser.
func (s *Session) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx != nil && s.ctx.Err() == nil
}

// Info returns session metadata for the UI.
func (s *Session) Info() (exe string, headless bool, since time.Time, navs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.execPath, s.headless, s.startedAt, s.navigations
}

// CountNav increments the navigation counter (called by the tool on navigate).
func (s *Session) CountNav() {
	s.mu.Lock()
	s.navigations++
	s.mu.Unlock()
}

// Close terminates the browser process and frees everything.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	logging.Default().Info("browser", "session closed")
}

// HoverJS builds an action that dispatches realistic mouse events
// (mousemove → mouseover → mouseenter) on the first element matching `sel`.
// CSS selectors, XPath, and "text=…" selectors are all accepted.
// (chromedp v0.23 API: an action is a plain func against the session
// target; value-returning actions are called directly as funcs.)
func HoverJS(sel string) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		selJSON, err := json.Marshal(sel)
		if err != nil {
			return chromedp.Void{}, err
		}
		js := `(sel => {
                        const resolve = s => {
                                if (s.startsWith('text=')) {
                                        const needle = s.slice(5).toLowerCase();
                                        return [...document.querySelectorAll('a, button, [role=button], input, select, textarea, *')]
                                                .find(el => (el.innerText || '').trim().toLowerCase().includes(needle));
                                }
                                if (s.startsWith('//') || s.startsWith('./') || s.startsWith('(')) {
                                        return document.evaluate(s, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue;
                                }
                                return document.querySelector(s);
                        };
                        const el = resolve(sel);
                        if (!el) return false;
                        const r = el.getBoundingClientRect();
                        const opts = {bubbles: true, cancelable: true, clientX: r.x + r.width/2, clientY: r.y + r.height/2};
                        el.dispatchEvent(new MouseEvent('mousemove', opts));
                        el.dispatchEvent(new MouseEvent('mouseover', opts));
                        el.dispatchEvent(new MouseEvent('mouseenter', {...opts, bubbles: false}));
                        return true;
                })(` + string(selJSON) + `)`
		if err := func() error {
			matched, err := chromedp.Evaluate[bool](js)(ctx, t)
			if err != nil {
				return err
			}
			if !matched {
				return fmt.Errorf("hover: no element matches %q", sel)
			}
			return nil
		}(); err != nil {
			return chromedp.Void{}, err
		}
		return chromedp.Void{}, nil
	}
}
