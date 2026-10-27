package webworkspace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/presentation"
)

type fakeSource struct{ blocks []presentation.Block }

func (f *fakeSource) Blocks(ctx context.Context, limit int) ([]presentation.Block, error) {
	if limit < len(f.blocks) {
		return f.blocks[:limit], nil
	}
	return f.blocks, nil
}

func startTestHost(t *testing.T, src Source) (*Host, string) {
	t.Helper()
	h := NewHost(src)
	url, err := h.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = h.Stop() })
	return h, url
}

// originOf returns scheme://host from a launch URL (strips path and
// fragment) — the tests must never build a request by string-slicing
// the launch URL's path.
func originOf(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil {
		panic("test launch url unparseable: " + raw)
	}
	return u.Scheme + "://" + u.Host
}

func TestStartBindsLoopbackEphemeral(t *testing.T) {
	h, url := startTestHost(t, &fakeSource{})
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("bind url = %q — must be loopback with an ephemeral port", url)
	}
	if !strings.Contains(url, "#t=") {
		t.Fatal("launch URL must carry the token in the fragment")
	}
	st := h.State()
	if !st.Running || st.URL == "" {
		t.Fatalf("state = %+v", st)
	}
	if strings.Contains(st.URL, "#t=") || strings.Contains(st.URL, "t=") {
		t.Fatalf("tokenless state URL leaked a token: %q", st.URL)
	}
}

func TestStartTwiceIsTypedError(t *testing.T) {
	h, _ := startTestHost(t, &fakeSource{})
	if _, err := h.Start(); err == nil {
		t.Fatal("second start must be a typed conflict, never a double listener")
	}
}

func TestStopIsIdempotentAndClearsToken(t *testing.T) {
	h, _ := startTestHost(t, &fakeSource{})
	if err := h.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := h.Stop(); err != nil {
		t.Fatal("second stop must be a clean no-op")
	}
	if h.TokenURL() != "" {
		t.Fatal("stopped host must clear the tokened URL")
	}
	if h.Running() {
		t.Fatal("Running() true after Stop")
	}
}

func TestPageServedWithStrictSecurityHeaders(t *testing.T) {
	_, url := startTestHost(t, &fakeSource{})
	base := originOf(url)
	resp, err := http.Get(base + "/workspace")
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("page status = %d", resp.StatusCode)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "connect-src 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP missing %q: %q", want, csp)
		}
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" ||
		resp.Header.Get("Referrer-Policy") != "no-referrer" ||
		resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("security headers incomplete: %v", resp.Header)
	}
	if strings.Contains(string(body), "src=\"http") || strings.Contains(string(body), "href=\"http") {
		t.Fatal("page references a remote origin — assets must be embedded/offline")
	}
}

func TestAssetsAreAllowListedOnly(t *testing.T) {
	_, url := startTestHost(t, &fakeSource{})
	base := originOf(url)
	// Allow-listed assets serve with the right content types.
	for name, ctype := range map[string]string{
		"/workspace/app.js":  "text/javascript",
		"/workspace/app.css": "text/css",
	} {
		resp, err := http.Get(base + name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), ctype) {
			t.Fatalf("%s: status=%d type=%q", name, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if len(body) == 0 {
			t.Fatalf("%s served empty", name)
		}
	}
	// Traversal probes and unknown paths: 404, never a listing.
	for _, probe := range []string{
		"/workspace/../host.go", "/workspace/..%2Fhost.go", "/workspace/",
		"/workspace/secret.txt", "/", "/anything",
	} {
		resp, err := http.Get(base + probe)
		if err != nil {
			t.Fatalf("probe %s: %v", probe, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("probe %q status = %d, want 404", probe, resp.StatusCode)
		}
		if strings.Contains(string(body), "package webworkspace") {
			t.Fatalf("probe %q leaked source", probe)
		}
	}
}

func TestBlocksRequireBearerToken(t *testing.T) {
	_, url := startTestHost(t, &fakeSource{blocks: []presentation.Block{
		{ID: "b1", Version: 1, Kind: "markdown", Payload: "hi", Source: "t"},
	}})
	base := originOf(url)
	// Missing token → 401, never a silent success.
	resp, err := http.Get(base + "/api/blocks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", resp.StatusCode)
	}
	// Wrong token → 401.
	req, _ := http.NewRequest("GET", base+"/api/blocks", nil)
	req.Header.Set("Authorization", "Bearer "+"0000000000000000000000000000000000000000000000000000000000000000")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", resp.StatusCode)
	}
	// The launch token (from the fragment) authorizes.
	token := strings.TrimPrefix(url, base+"/workspace#t=")
	req2, _ := http.NewRequest("GET", base+"/api/blocks", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("tokened status = %d", resp2.StatusCode)
	}
	var env blocksEnvelope
	if err := json.NewDecoder(resp2.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Version != presentation.EnvelopeVersion || len(env.Blocks) != 1 || env.Blocks[0].ID != "b1" {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestBlocksRejectsNonGet(t *testing.T) {
	_, url := startTestHost(t, &fakeSource{})
	base := originOf(url)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(method, base+"/api/blocks", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s /api/blocks = %d, want 405", method, resp.StatusCode)
		}
	}
	// Static surface is GET-only too.
	req, _ := http.NewRequest(http.MethodPost, base+"/workspace", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /workspace = %d, want 405", resp.StatusCode)
	}
}

func TestEmptySourceServesEnvelopeWithNoBlocks(t *testing.T) {
	_, url := startTestHost(t, &fakeSource{})
	base := originOf(url)
	token := strings.TrimPrefix(url, base+"/workspace#t=")
	req, _ := http.NewRequest("GET", base+"/api/blocks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env blocksEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("empty source must still serve a valid envelope: %v", err)
	}
	if len(env.Blocks) != 0 {
		t.Fatalf("empty source served blocks: %+v", env.Blocks)
	}
}

// The renderer must never contain the forbidden DOM sinks — this is the
// XSS-by-construction gate over the shipped asset itself.
func TestRendererHasNoHTMLSinks(t *testing.T) {
	for _, sink := range []string{"innerHTML", "outerHTML", "document.write", "insertAdjacentHTML"} {
		if strings.Contains(appJS, sink) {
			t.Fatalf("app.js contains forbidden DOM sink %q", sink)
		}
	}
	if !strings.Contains(appJS, "createTextNode") {
		t.Fatal("app.js must build text via createTextNode")
	}
	// No remote references anywhere in the shipped assets.
	for _, asset := range []string{pageHTML, appJS, appCSS} {
		if strings.Contains(asset, "https://") && !strings.Contains(asset, "http://www.w3.org") {
			t.Fatalf("asset references a remote origin: %.80s", asset)
		}
	}
}
