// providers.go — the typed platform-capability adapters for the UI
// migration (go-gui host). The package keeps its house rules: NO ui
// imports, honest no-ops (a capability without a real implementation
// reports ErrUnsupportedPlatform, never fake success), and typed errors
// the UI can branch on.
//
// The go-gui window installs real implementations where the toolkit
// exposes them (desktop clipboard via Window.SetClipboardFn hooks are
// wired by the shell package, which CONSTRUCTS these providers with real
// function pointers — the package itself stays toolkit-free).
package platform

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Typed capability errors. Callers branch with errors.Is, never string
// matching.
var (
	// ErrUnsupported is the typed failure for a capability without a
	// real implementation on this platform/backend.
	ErrUnsupported = errors.New("platform capability not supported here")
	// ErrPermissionDenied reports an OS-level refusal.
	ErrPermissionDenied = errors.New("platform permission denied")
	// ErrBusy reports a single-flight adapter asked twice.
	ErrBusy = errors.New("platform adapter busy")
)

// DocumentProvider opens/exports user documents (file pickers). The
// desktop real implementation forwards to the toolkit's native dialogs;
// mobile gets the OS picker through its own binding.
type DocumentProvider interface {
	// OpenFile asks the user for one existing file path.
	OpenFile(title string, extensions []string) (path string, err error)
	// SaveFile asks the user for a save target.
	SaveFile(title, defaultName, defaultExt string) (path string, err error)
	// RevealPath reveals path in the OS file manager (best effort).
	RevealPath(path string) error
}

// ClipboardProvider reads/writes the system clipboard. The go-gui window
// exposes SetClipboard/GetClipboard; the shell wires them into a real
// provider. Everything else is typed-unsupported.
type ClipboardProvider interface {
	SetText(text string) error
	Text() (string, error)
}

// PermissionProvider queries/grants runtime permissions (mobile-first;
// desktop reports granted-or-unsupported honestly).
type PermissionProvider interface {
	// Check reports the grant state of one permission.
	Check(permission string) (granted bool, err error)
	// Request asks the OS to grant one permission.
	Request(permission string) (granted bool, err error)
}

// NotificationProvider posts OS-level notifications.
type NotificationProvider interface {
	Notify(title, body string) error
}

// AccessibilityProvider exposes assistive-tech hooks.
type AccessibilityProvider interface {
	// Announce pushes a string to the screen reader (best effort).
	Announce(text string) error
	// Enabled reports whether an assistive layer is active.
	Enabled() bool
}

// PowerProvider keeps the display/device awake during long operations —
// the typed mirror of go-gui v0.97.0's nativeKeepAwake requirement:
//
//	SetKeepAwake(on bool, app, reason string) error
//
// (gui counts tokens across windows and calls the platform only on
// aggregate state changes; app and reason are non-empty when on is true.)
type PowerProvider interface {
	// SetKeepAwake holds or releases a display/idle wake assertion.
	SetKeepAwake(on bool, app, reason string) error
}

// LifecycleProvider surfaces app lifecycle transitions for mobile hosts
// (suspend/resume) and desktop equivalents.
type LifecycleProvider interface {
	// OnSuspend/OnResume register callbacks; the adapters invoke them on
	// the platform's events. Registering nil removes the callback.
	OnSuspend(fn func())
	OnResume(fn func())
}

// WebWorkspaceProvider opens the loopback Web Workspace surface in the
// default browser (the typed seam ui/gogui's workspace hub calls).
type WebWorkspaceProvider interface {
	// OpenURL hands a URL to the OS default handler.
	OpenURL(raw string) error
}

// --- Default "unsupported" implementations ------------------------------
//
// One struct per capability, all returning typed errors (or false) —
// the honest default every platform starts from. The ui layer swaps in
// real implementations by constructing the interface over the toolkit's
// functions where they exist.

// UnsupportedDocumentProvider is the default DocumentProvider.
type UnsupportedDocumentProvider struct{}

func (UnsupportedDocumentProvider) OpenFile(string, []string) (string, error) {
	return "", fmt.Errorf("OpenFile: %w", ErrUnsupported)
}
func (UnsupportedDocumentProvider) SaveFile(string, string, string) (string, error) {
	return "", fmt.Errorf("SaveFile: %w", ErrUnsupported)
}
func (UnsupportedDocumentProvider) RevealPath(string) error {
	return fmt.Errorf("RevealPath: %w", ErrUnsupported)
}

// UnsupportedClipboardProvider is the default ClipboardProvider.
type UnsupportedClipboardProvider struct{}

func (UnsupportedClipboardProvider) SetText(string) error {
	return fmt.Errorf("clipboard SetText: %w", ErrUnsupported)
}
func (UnsupportedClipboardProvider) Text() (string, error) {
	return "", fmt.Errorf("clipboard Text: %w", ErrUnsupported)
}

// UnsupportedPermissionProvider is the default PermissionProvider.
type UnsupportedPermissionProvider struct{}

func (UnsupportedPermissionProvider) Check(string) (bool, error) {
	return false, fmt.Errorf("permission Check: %w", ErrUnsupported)
}
func (UnsupportedPermissionProvider) Request(string) (bool, error) {
	return false, fmt.Errorf("permission Request: %w", ErrUnsupported)
}

// UnsupportedNotificationProvider is the default NotificationProvider.
type UnsupportedNotificationProvider struct{}

func (UnsupportedNotificationProvider) Notify(string, string) error {
	return fmt.Errorf("notification: %w", ErrUnsupported)
}

// UnsupportedAccessibilityProvider is the default AccessibilityProvider.
type UnsupportedAccessibilityProvider struct{}

func (UnsupportedAccessibilityProvider) Announce(string) error {
	return fmt.Errorf("a11y Announce: %w", ErrUnsupported)
}
func (UnsupportedAccessibilityProvider) Enabled() bool { return false }

// UnsupportedPowerProvider is the default PowerProvider. Holding a wake
// lock without a real backend would LIE about power behavior — the
// default refuses loudly.
type UnsupportedPowerProvider struct{}

func (UnsupportedPowerProvider) SetKeepAwake(on bool, app, reason string) error {
	return fmt.Errorf("SetKeepAwake(%v, %q, %q): %w", on, app, reason, ErrUnsupported)
}

// UnsupportedLifecycleProvider is the default LifecycleProvider: it
// stores the callbacks and never fires them (suspend/resume do not
// happen where no platform reports them).
type UnsupportedLifecycleProvider struct {
	mu      sync.Mutex
	suspend func()
	resume  func()
}

func (p *UnsupportedLifecycleProvider) OnSuspend(fn func()) {
	p.mu.Lock()
	p.suspend = fn
	p.mu.Unlock()
}

func (p *UnsupportedLifecycleProvider) OnResume(fn func()) {
	p.mu.Lock()
	p.resume = fn
	p.mu.Unlock()
}

// UnsupportedWebWorkspaceProvider is the default WebWorkspaceProvider.
type UnsupportedWebWorkspaceProvider struct{}

func (UnsupportedWebWorkspaceProvider) OpenURL(raw string) error {
	return fmt.Errorf("OpenURL(%q): %w", raw, ErrUnsupported)
}

// DefaultProviders returns the honest all-unsupported set (mobile and
// desktop hosts override per capability at construction).
func DefaultProviders() Providers {
	return Providers{
		Document:      UnsupportedDocumentProvider{},
		Clipboard:     UnsupportedClipboardProvider{},
		Permission:    UnsupportedPermissionProvider{},
		Notification:  UnsupportedNotificationProvider{},
		Accessibility: UnsupportedAccessibilityProvider{},
		Power:         UnsupportedPowerProvider{},
		Lifecycle:     &UnsupportedLifecycleProvider{},
		WebWorkspace:  UnsupportedWebWorkspaceProvider{},
	}
}

// Providers bundles the typed adapters (one bundle per app host). Nil
// members are replaced by the unsupported defaults by WithDefaults.
type Providers struct {
	Document      DocumentProvider
	Clipboard     ClipboardProvider
	Permission    PermissionProvider
	Notification  NotificationProvider
	Accessibility AccessibilityProvider
	Power         PowerProvider
	Lifecycle     LifecycleProvider
	WebWorkspace  WebWorkspaceProvider
}

// WithDefaults fills nil members with the unsupported defaults so a host
// can never hold a nil capability (a nil adapter would panic at the
// worst possible moment; a typed error is always better).
func (p Providers) WithDefaults() Providers {
	if p.Document == nil {
		p.Document = UnsupportedDocumentProvider{}
	}
	if p.Clipboard == nil {
		p.Clipboard = UnsupportedClipboardProvider{}
	}
	if p.Permission == nil {
		p.Permission = UnsupportedPermissionProvider{}
	}
	if p.Notification == nil {
		p.Notification = UnsupportedNotificationProvider{}
	}
	if p.Accessibility == nil {
		p.Accessibility = UnsupportedAccessibilityProvider{}
	}
	if p.Power == nil {
		p.Power = UnsupportedPowerProvider{}
	}
	if p.Lifecycle == nil {
		p.Lifecycle = &UnsupportedLifecycleProvider{}
	}
	if p.WebWorkspace == nil {
		p.WebWorkspace = UnsupportedWebWorkspaceProvider{}
	}
	return p
}

// KeepAwakeDuring runs fn while a wake lock is held (best effort: the
// error of the RELEASE is never allowed to mask fn's result). The
// convenience exists because the two long operations that need it
// (engine update install, model import) are exactly the ones whose
// failure must not be polluted.
func KeepAwakeDuring(p PowerProvider, app, reason string, fn func() error) error {
	_ = p.SetKeepAwake(true, app, reason)
	defer func() { _ = p.SetKeepAwake(false, app, reason) }()
	return fn()
}

// Deadline is a small helper the mobile lifecycle uses to bound teardown
// waits (documented here so the adapters file owns the time vocabulary).
func Deadline(d time.Duration) time.Time { return time.Now().Add(d) }
