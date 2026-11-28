package webshell

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The shell assets are the product surface: missing or empty assets are a
// broken build, not a degraded one.
func TestAssetsEmbedded(t *testing.T) {
	for _, name := range []string{"index.html", "app.css", "app.js"} {
		data := MustAsset(name)
		if len(data) < 200 {
			t.Fatalf("asset %q is suspiciously small (%d bytes) — the shell is incomplete", name, len(data))
		}
	}

	if len(AssetNames()) < 3 {
		t.Fatalf("AssetNames() = %v, want at least the three shell assets", AssetNames())
	}
}

// TestRegistryMirrorsNativeIA pins the web shell's workspace registry to
// the ONE canonical IA the native mirror shell derives its rail, compact
// strip and shortcut maps from (ui/gogui/shell.go primaryViews +
// subPanels). The fyne shell is RETIRED (run 45634345922 update: the web
// host is the primary window on every desktop platform); the go-gui
// mirror remains the native cross-check. The ids AND the order must
// match — order is the rail order and the Ctrl+number shortcut order.
func TestRegistryMirrorsNativeIA(t *testing.T) {
	raw, err := os.ReadFile("../../ui/gogui/shell.go")
	if err != nil {
		t.Skipf("native mirror shell source unavailable in this checkout layout: %v", err)
	}

	primaryRe := regexp.MustCompile(`\{"([a-z]+)", "[^"]+", build\w+\}`)

	var nativePrimary, nativeUtility []string
	inUtility := false

	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "subPanels = ") {
			inUtility = true
		}

		m := primaryRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		if inUtility {
			nativeUtility = append(nativeUtility, m[1])
		} else {
			nativePrimary = append(nativePrimary, m[1])
		}
	}

	if len(nativePrimary) == 0 || len(nativeUtility) == 0 {
		t.Skipf("the native registry shape changed — re-pin this contract (primary=%v utility=%v)", nativePrimary, nativeUtility)
	}

	gotPrimary := Workspaces()
	if len(gotPrimary) != len(nativePrimary) {
		t.Fatalf("primary registry: web shell has %d entries (%v), native shell has %d (%v)", len(gotPrimary), gotPrimary, len(nativePrimary), nativePrimary)
	}

	for i, w := range gotPrimary {
		if w.ID != nativePrimary[i] {
			t.Fatalf("primary registry order diverged at %d: web shell %q, native %q — ONE IA means ONE order", i, w.ID, nativePrimary[i])
		}
	}

	gotUtility := Utilities()
	if len(gotUtility) != len(nativeUtility) {
		t.Fatalf("utility registry: web shell has %d entries (%v), native shell has %d (%v)", len(gotUtility), gotUtility, len(nativeUtility), nativeUtility)
	}

	for i, w := range gotUtility {
		if w.ID != nativeUtility[i] {
			t.Fatalf("utility registry order diverged at %d: web shell %q, native %q", i, w.ID, nativeUtility[i])
		}
	}
}

// TestResolveClassBands pins the shared width classification (the same
// 600/900/1280 bands the native shells use, named per the adaptive-shell
// blueprint).
func TestResolveClassBands(t *testing.T) {
	cases := []struct {
		width int
		want  string
	}{
		{0, ClassCompact},
		{320, ClassCompact},
		{599, ClassCompact},
		{600, ClassMedium},
		{899, ClassMedium},
		{900, ClassExpanded},
		{1279, ClassExpanded},
		{1280, ClassWide},
		{2560, ClassWide},
	}

	for _, tc := range cases {
		if got := ResolveClass(tc.width); got != tc.want {
			t.Fatalf("ResolveClass(%d) = %q, want %q", tc.width, got, tc.want)
		}
	}
}

// cssElementFiles is the ONE-per-element cascade the shell ships since
// the run 46050871166 update: app.css is the ENTRY (index.html keeps the
// single /app.css reference; the file carries the ordered @import
// cascade), and every element owns its own file under /css/. The order
// mirrors the cascade contract documented in app.css itself.
var cssElementFiles = []string{
	"css/tokens.css",
	"css/base.css",
	"css/layout.css",
	"css/controls.css",
	"css/rail.css",
	"css/views.css",
	"css/chat.css",
	"css/panels.css",
	"css/loading.css",
	"css/system.css",
	"css/adaptive.css",
	"css/motion.css",
}

// cssBundle assembles the presentation language the shell actually
// serves: the entry plus every per-element file, in cascade order.
func cssBundle(t *testing.T) string {
	t.Helper()

	var b strings.Builder

	b.WriteString(string(MustAsset("app.css")))
	b.WriteString("\n")

	for _, name := range cssElementFiles {
		b.WriteString(string(MustAsset(name)))
		b.WriteString("\n")
	}

	return b.String()
}

// TestShellAssetContract pins the structure the blueprint demands: the
// assets reference each other, the JS carries the whole registry and the
// download-progress polling, the CSS bundle carries the four screen
// classes, the touch floor, momentum scrolling and the reduced-motion
// contract — and NOTHING references a remote origin (the CSP is
// same-origin only).
func TestShellAssetContract(t *testing.T) {
	html := string(MustAsset("index.html"))
	entry := string(MustAsset("app.css"))
	css := cssBundle(t)
	js := string(MustAsset("app.js"))

	if !strings.Contains(html, `href="/app.css"`) || !strings.Contains(html, `src="/app.js"`) {
		t.Fatalf("index.html must reference the shell assets self-origin only")
	}

	// The per-element contract: ONE entry, ONE file per element, no
	// element rules left in the entry itself.
	for _, name := range cssElementFiles {
		if len(MustAsset(name)) < 60 {
			t.Fatalf("per-element stylesheet %q is suspiciously small — the element cascade is incomplete", name)
		}

		if !strings.Contains(entry, "@import url(\""+name+"\")") { // run 46050871166: double-quoted needle (byte-identical to the served bytes)
			t.Fatalf("app.css does not import the per-element file %q in its cascade", name)
		}
	}

	for _, w := range append(Workspaces(), Utilities()...) {
		if !strings.Contains(html, `data-view="`+w.ID+`"`) {
			t.Fatalf("index.html has no %q workspace section", w.ID)
		}

		if !strings.Contains(js, `id: "`+w.ID+`"`) {
			t.Fatalf("app.js registry is missing %q — the JS copy must mirror the ONE registry", w.ID)
		}
	}

	for _, want := range []string{UpdateCheckPath, "visibilitychange", "humanBytes", "startPolling", "stopPolling"} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js is missing the %q contract", want)
		}
	}

	if !strings.Contains(html, "aria-valuemax") {
		t.Fatalf("index.html download bar is missing the progressbar ARIA contract")
	}

	for _, want := range []string{
		"max-width: 599.98px", "600px", "899.98px", "900px", "1279.98px", "1280px",
		"--touch-floor: 44px", "-webkit-overflow-scrolling: touch",
		"overscroll-behavior: contain", "prefers-reduced-motion",
		"env(safe-area-inset-top)",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("the css bundle (entry + per-element files) is missing the %q contract (adaptive classes, touch floor, scroll or motion)", want)
		}
	}

	// The entry must stay an entry: rules for elements live in the
	// per-element files, so the entry carries only comments and imports.
	// The scan tracks /* ... */ comment state so multi-line comment text
	// never trips the rule check.
	inComment := false

	for _, line := range strings.Split(entry, "\n") {
		trimmed := strings.TrimSpace(line)

		if inComment {
			if strings.Contains(trimmed, "*/") {
				inComment = false
			}

			continue
		}

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "/*") {
			if !strings.Contains(trimmed, "*/") {
				inComment = true
			}

			continue
		}

		if strings.HasPrefix(trimmed, "@import") {
			continue
		}

		t.Fatalf("app.css carries a rule outside the @import cascade (%q) — element rules belong in css/", trimmed)
	}

	// CSP conformance: no remote origin anywhere in the shell assets.
	for name, body := range map[string]string{"index.html": html, "app.css": entry, "app.js": js} {
		for _, remote := range []string{"http://", "https://", "//cdn.", "integrity="} {
			if strings.Contains(body, remote) {
				t.Fatalf("%s references %q — the shell CSP is same-origin only", name, remote)
			}
		}
	}

	for _, name := range cssElementFiles {
		body := string(MustAsset(name))

		for _, remote := range []string{"http://", "https://", "//cdn.", "integrity="} {
			if strings.Contains(body, remote) {
				t.Fatalf("%s references %q — the shell CSP is same-origin only", name, remote)
			}
		}
	}
}

// TestHandlerServesAndSecures exercises the serving contract: API
// passthrough, shell assets with the security policy, the SPA fallback
// and the traversal-proof path resolution.
func TestHandlerServesAndSecures(t *testing.T) {
	var apiHits int

	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiHits++
		w.WriteHeader(http.StatusTeapot)
	})

	h := Handler(api)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		return rec
	}

	if rec := get("/api/engine/update/check"); rec.Code != http.StatusTeapot || apiHits != 1 {
		t.Fatalf("/api/* must pass through untouched, got %d after %d hits", rec.Code, apiHits)
	}

	if rec := get("/healthz"); rec.Code != http.StatusTeapot || apiHits != 2 {
		t.Fatalf("/healthz must pass through untouched, got %d", rec.Code)
	}

	rec := get("/")
	if rec.Code != http.StatusOK {
		t.Fatalf("/ must serve the shell, got %d", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("the shell must be text/html, got %q", ct)
	}

	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(header) == "" {
			t.Fatalf("the shell answer is missing the %q security header", header)
		}
	}

	if !strings.Contains(rec.Body.String(), "SHEYTAN") {
		t.Fatalf("the served shell is not the product shell")
	}

	if rec := get("/engines/deep/link"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SHEYTAN") {
		t.Fatalf("a deep route must fall back to the shell (SPA hash routing), got %d", rec.Code)
	}

	rec = get("/app.js")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("app.js must serve as javascript, got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// Traversal-proof: the embedded FS has no files outside the shell, and
	// a hostile path must never escape the root.
	for _, hostile := range []string{"/../go.mod", "/../../internal/proc/identity.go", "/app.js/../../../go.sum"} {
		rec := get(hostile)
		if strings.Contains(rec.Body.String(), "module github.com/") {
			t.Fatalf("path %q escaped the shell asset root", hostile)
		}
	}

	// Without an API handler the shell still serves (browser/web mode
	// without a mounted core must not crash).
	if rec := (Handler(nil)).ServeHTTP; false {
		_ = rec
	}

	rec2 := httptest.NewRecorder()
	Handler(nil).ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("nil-API mode must still serve the shell, got %d", rec2.Code)
	}
}
