// Package webshell is the ONE adaptive web shell of the product UI: the
// web-shell-first presentation layer defined by the adaptive web UI
// platform blueprint (2026-11-25) and served BOTH through the typed web
// UI runtime's asset server (the primary desktop window on every
// platform, internal/webworkspace host) and over the secured loopback
// HTTP surface (browser/web mode).
//
// Structure (blueprint §03):
//   - ONE shell, many compositions: the shell owns navigation, status,
//     notifications, the command palette and the adaptive layout; each
//     workspace owns its own composition.
//   - Workspace registry: the canonical 9-destination IA plus the 4
//     utility surfaces. THIS package is the CANONICAL registry since the
//     fyne retirement (run 45634345922 update); the surviving native
//     mirror shell (ui/gogui) derives its rail, compact strip and
//     shortcut maps from the same ids and order, and the contract test
//     pins the two sides together.
//   - Screen classes compact / medium / expanded / wide resolved from the
//     shared 600 / 900 / 1280 CSS-px bands the native shells already use
//     (the blueprint's shared width classification as the starting point;
//     container queries refine per component).
//   - Design tokens: the CSS custom properties in assets/app.css mirror
//     the product token authority (internal/theme) — same roles, radii,
//     spacing and touch floors; a workspace may change density and
//     arrangement, never the token language.
//   - Work is asynchronous and visible: long operations (engine update
//     download) surface phase, byte progress and speed through the
//     existing /api/engine/update/check route — file size and status are
//     rendered in the shell wherever downloading happens.
//
// The package is build-tag free on purpose: the shell assets, the
// registry contract and the security policy of the serving path are
// testable on EVERY platform and in the headless suite — the typed web
// UI host (internal/webworkspace, webworkspace build tag) embeds THIS
// handler as its asset server, so the native window and the browser mode
// render the exact same bytes.
package webshell

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed assets
var assetsFS embed.FS

// root is the embedded prefix stripped from request paths.
const root = "assets"

// UpdateCheckPath is the ONE route the shell polls for long-running
// download progress (engine updates): phase, BytesDone, BytesTotal,
// BytesPerSec and the composed human message (internal/downloader).
const UpdateCheckPath = "/api/engine/update/check"

// Screen classes (blueprint §3.2/§3.3) resolved from the shared bands:
// compact < 600, medium 600–899, expanded 900–1279, wide >= 1280 CSS px.
const (
	ClassCompact  = "compact"
	ClassMedium   = "medium"
	ClassExpanded = "expanded"
	ClassWide     = "wide"
)

// Band boundaries shared with the native mirror shell (ui/gogui/shell.go):
// ONE width classification across every surface.
const (
	BandMedium   = 600
	BandExpanded = 900
	BandWide     = 1280
)

// ResolveClass maps an available width in CSS px to the shell screen
// class. Pure function — deterministic and unit-testable (blueprint §3.4).
func ResolveClass(widthCSS int) string {
	switch {
	case widthCSS < BandMedium:
		return ClassCompact
	case widthCSS < BandExpanded:
		return ClassMedium
	case widthCSS < BandWide:
		return ClassExpanded
	default:
		return ClassWide
	}
}

// Workspace is one registry entry: the id, its display title and its kind
// (primary destination vs utility surface). The ids are the product IA —
// the native shells, the shortcut map and this shell all derive from the
// same ordered list.
type Workspace struct {
	ID    string
	Title string
	Kind  string
}

// Kinds of registry entries.
const (
	KindPrimary  = "primary"
	KindUtility  = "utility"
	KindDownload = "download"
)

// primaryIDs is the ordered 9-destination IA (mirrored by ui/gogui/shell.go
// primaryViews). Order IS the rail order and the Ctrl+number shortcut
// order.
var primaryIDs = []Workspace{
	{"chat", "Chat", KindPrimary},
	{"agent", "Multi-Agent", KindPrimary},
	{"workspace", "Workspace", KindPrimary},
	{"lab", "Coding Lab", KindPrimary},
	{"automation", "Automation", KindPrimary},
	{"system", "System", KindPrimary},
	{"settings", "Settings", KindPrimary},
	{"studio", "3D Studio", KindPrimary},
	{"engines", "Engines", KindPrimary},
}

// utilityIDs is the ordered 4-surface utilities group (mirrors the
// subPanels registry of the native mirror shell).
var utilityIDs = []Workspace{
	{"history", "History", KindUtility},
	{"models", "Models", KindUtility},
	{"tools", "My Tools", KindUtility},
	{"logs", "Logs", KindUtility},
}

// Workspaces returns the ordered primary registry (a copy — callers
// cannot mutate the ONE registry).
func Workspaces() []Workspace {
	out := make([]Workspace, len(primaryIDs))
	copy(out, primaryIDs)

	return out
}

// Utilities returns the ordered utility registry (a copy).
func Utilities() []Workspace {
	out := make([]Workspace, len(utilityIDs))
	copy(out, utilityIDs)

	return out
}

// contentType maps the served extensions to exact MIME types (the shell
// never relies on sniffing: X-Content-Type-Options is set on every
// answer).
func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	default:
		return "application/octet-stream"
	}
}

// Handler serves the embedded shell with the shell security policy, and
// passes the API surface through untouched so the SAME loopback handler
// serves both halves. Missing routes fall back to index.html — the shell
// owns hash routing, so a deep link must always boot the shell.
//
// Security contract (mirrors internal/workspace's viewer policy):
//   - apiHandler (when non-nil) receives every /api/ and /healthz request;
//   - everything else is served ONLY from the embedded assets — no
//     directory listing, no path escape, no sniffing, no framing, no
//     referrer, a strict same-origin CSP (no inline script/style, no
//     remote origin).
func Handler(apiHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		// Run 45716559459 update (secure-connection hardening):
		// the shell talks to EXACTLY ONE origin — its own. These
		// headers pin that down beyond CSP: no ambient authority
		// is granted to any powerful platform feature (the shell
		// needs none), the window is isolated from any opener
		// context, and embedded subresources may only come from
		// this same origin.
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), serial=(), bluetooth=(), interest-cohort=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")

		if apiHandler != nil {
			p := r.URL.Path

			if p == "/healthz" || strings.HasPrefix(p, "/api/") {
				apiHandler.ServeHTTP(w, r)

				return
			}
		}

		name := path.Clean("/" + r.URL.Path) // "/x/y" form, no traversal escape
		rel := strings.TrimPrefix(name, "/")
		data, err := assetsFS.ReadFile(root + "/" + rel)

		if err != nil {
			// SPA fallback: any non-asset route boots the shell first.
			data, err = assetsFS.ReadFile(root + "/index.html")
			if err != nil {
				http.Error(w, "shell assets unavailable", http.StatusInternalServerError)

				return
			}

			rel = "index.html"
		}

		h.Set("Content-Type", contentType(rel))
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)

		if _, werr := w.Write(data); werr != nil {
			// The client went away mid-write; nothing recoverable.
			return
		}
	})
}

// MustAsset returns one embedded asset's bytes; it is a test/bootstrap
// helper and panics on a missing asset (a build that ships without the
// shell is a broken build, not a degraded one).
func MustAsset(name string) []byte {
	data, err := assetsFS.ReadFile(root + "/" + name)
	if err != nil {
		panic(fmt.Sprintf("webshell: asset %q missing: %v", name, err))
	}

	return data
}

// AssetNames lists the embedded shell assets (contract tests iterate it).
func AssetNames() []string {
	out := make([]string, 0, 16)

	// Walk the whole embedded tree — the shell's per-element CSS lives
	// under assets/css/ (run 46050871166 update: one file per element,
	// never one big stylesheet), and the contract suite must see it.
	err := fs.WalkDir(assetsFS, root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}

		if d.IsDir() {
			return nil
		}

		out = append(out, strings.TrimPrefix(p, root+"/"))

		return nil
	})
	if err != nil {
		return nil
	}

	return out
}
