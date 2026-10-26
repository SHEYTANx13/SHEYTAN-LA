// Package localipc provides the ONE local transport between the native
// Qt/QML desktop shell and the Go core (v1.10.1 Qt migration).
//
// DESIGN CONTRACT (R&D authority §6-§7, Qt Phase 2):
//
//   - The Go core remains the ONLY high-level authority. The Qt host is a
//     CLIENT: every run, session, tool, approval, memory and policy decision
//     stays inside the existing api.Server handlers. This package therefore
//     does not re-expose business logic — it TUNNELS the existing
//     HTTP/WebSocket API surface over a secure local channel, so the Qt
//     client and the (headless/browser) API mode observe byte-identical
//     semantics.
//   - Transport: a per-user Unix domain socket (Linux/macOS) or a per-user
//     named pipe (Windows). No TCP listener is ever opened by the desktop
//     path; the channel is bound to the current user by construction
//     (socket directory mode 0700 + SO_PEERCRED uid verification on Unix,
//     a current-user-only DACL plus a post-connect client-SID check on
//     Windows) and additionally guarded by an ephemeral one-time key the
//     launcher hands to the UI process through the environment.
//   - Versioned handshake: the first request the UI makes is a typed
//     GET /api/ipc/handshake carrying the key; the core answers with the
//     protocol name/version triple. A UI built against a different
//     protocol refuses to talk instead of misbehaving silently.
//   - Bounded resources: HTTP framing rules apply (bounded headers via
//     http.Server, bounded bodies via the existing per-handler
//     MaxBytesReader limits), the handshake is size-capped, and the
//     listener never spawns goroutines per queued connection beyond what
//     http.Serve already does.
package localipc

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ProtocolName / ProtocolVersion are the typed handshake identity. Bump
// ProtocolVersion whenever the wire contract changes shape; the Qt host
// refuses a mismatched core instead of guessing.
const (
	ProtocolName    = "sheytan-ipc"
	ProtocolVersion = 1
)

// Environment variables passed by the desktop launcher to the UI process.
const (
	// EnvEndpoint is the full endpoint string (socket path or pipe name).
	EnvEndpoint = "SHEYTAN_IPC_ENDPOINT"
	// EnvKey is the ephemeral handshake key. Never logged, never persisted.
	EnvKey = "SHEYTAN_IPC_KEY"
	// HeaderKey is the HTTP header carrying the ephemeral key. Enforced on
	// the handshake; every core request from the one UI sends it for
	// parity (v1.11.0 one-Go-UI migration — one name, one source).
	HeaderKey = "X-SHEYTAN-IPC-Key"
)

// ErrUnauthorized is returned when a peer fails the transport-level
// identity check (foreign uid on Unix, foreign SID on Windows).
var ErrUnauthorized = errors.New("localipc: peer identity rejected")

// credAllowed is the pure Unix identity decision, separated so the deny
// path is unit-testable without forging kernel credentials. It lives in
// the platform-neutral file (not listener_unix.go) so the contract tests
// compile on every platform — on Windows the named-pipe DACL plus the
// post-connect client-SID check own the identity boundary and this
// helper is simply unused by the transport.
func credAllowed(uid int) bool {
	return uid == os.Getuid()
}

// NewKey returns a fresh 128-bit hex handshake key.
func NewKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("localipc: handshake key: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Endpoint returns the per-user local endpoint for the desktop channel and
// the directory that must exist (Unix only; empty on Windows).
//
// The endpoint embeds the effective uid (Unix) or the user name (Windows)
// so two accounts on one machine can never collide. The socket directory is
// created with mode 0700: only the current user can even see the path.
func Endpoint() (endpoint string, dir string, err error) {
	if runtime.GOOS == "windows" {
		user := strings.TrimSpace(os.Getenv("USERNAME"))
		if user == "" {
			user = "user"
		}
		// Pipe names are case-insensitive and flat; keep the character set
		// conservative (letters, digits, dash).
		var b strings.Builder
		for _, r := range strings.ToLower(user) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
				b.WriteRune(r)
			}
		}
		name := b.String()
		if name == "" {
			name = "user"
		}
		return fmt.Sprintf(`\\.\pipe\sheytan-la-%s-core`, name), "", nil
	}

	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return "", "", fmt.Errorf("localipc: unsupported platform %q", runtime.GOOS)
	}

	if runtime0700 := os.Getenv("XDG_RUNTIME_DIR"); runtime0700 != "" {
		dir = filepath.Join(runtime0700, "sheytan-la")
	} else {
		// Fall back to the system temp dir; the directory itself is created
		// 0700 owned by the current user, which preserves the boundary.
		base := os.TempDir()
		dir = filepath.Join(base, fmt.Sprintf("sheytan-la-%d", os.Getuid()))
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("localipc: endpoint directory: %w", err)
	}

	// Defensive: if the directory already existed with a wider mode, tighten
	// it. A world-visible socket directory would leak the endpoint path and
	// invite pre-emption (stale-endpoint squatting).
	if st, statErr := os.Stat(dir); statErr == nil {
		if st.Mode().Perm() != 0o700 {
			if chErr := os.Chmod(dir, 0o700); chErr != nil {
				return "", "", fmt.Errorf("localipc: tighten endpoint directory mode: %w", chErr)
			}
		}
	}

	return filepath.Join(dir, "core.sock"), dir, nil
}

// cleanStaleEndpoint removes a leftover socket file that no live core is
// bound to. The probe is a DIAL: if something answers, the endpoint is LIVE
// and must not be touched (returns false, no removal); if the path does not
// exist there is nothing to do (true); if connecting fails with a
// connection error the file is stale and is removed (true).
func cleanStaleEndpoint(endpoint string) (removed bool, err error) {
	if runtime.GOOS == "windows" {
		// Named pipes vanish with their last handle; nothing to clean.
		return false, nil
	}

	conn, dialErr := dialEndpoint(endpoint)
	if dialErr == nil {
		_ = conn.Close()
		return false, nil // live core — never touch
	}

	if os.IsNotExist(dialErr) {
		return false, nil
	}

	if rmErr := os.Remove(endpoint); rmErr != nil && !os.IsNotExist(rmErr) {
		return false, fmt.Errorf("localipc: remove stale endpoint: %w", rmErr)
	}
	return true, nil
}
