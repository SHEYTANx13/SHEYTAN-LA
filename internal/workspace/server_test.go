package workspace

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/presentation"
)

// fakeSource is a read-only in-memory Source.
type fakeSource struct{ store *presentation.Store }

func (f fakeSource) WorkspaceBlocks() []presentation.Meta { return f.store.List() }
func (f fakeSource) WorkspaceBlock(id string) (presentation.Block, bool) {
	return f.store.Get(id)
}

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	st := presentation.NewStore(16)
	_, err := st.Put(presentation.Block{
		ID: "b1", Kind: presentation.KindMarkdown, Title: "Answer", RunID: "r1", SessionID: "s1",
		Payload: []byte("# Hi\n\n**bold** and `code` with <script>alert(1)</script>"),
	})
	if err != nil {
		t.Fatalf("seed block: %v", err)
	}
	_, err = st.Put(presentation.Block{ID: "b2", Kind: presentation.Kind("hologram"), Payload: []byte("raw")})
	if err != nil {
		t.Fatalf("seed degrade block: %v", err)
	}
	s := NewServer(fakeSource{store: st})
	url, err := s.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s, url
}

// base strips path AND fragment: the launch URL carries both, and test
// paths must target the bare origin.
func base(url string) string {
	if i := strings.IndexByte(url, '#'); i >= 0 {
		url = url[:i]
	}
	if i := strings.Index(url, "://"); i >= 0 {
		rest := url[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return url[:i+3] + rest[:j]
		}
	}
	return url
}

// get issues a request against the running server.
func get(t *testing.T, url, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base(url)+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func fragToken(url string) string {
	if i := strings.LastIndex(url, "#t="); i >= 0 {
		return url[i+3:]
	}
	return ""
}

func TestStartBindsLoopbackEphemeral(t *testing.T) {
	s, url := newTestServer(t)
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("URL %q is not loopback", url)
	}
	st := s.State()
	if !st.Running || st.Port == 0 || st.URL == "" {
		t.Fatalf("State = %+v", st)
	}
	if st.URL != url[:len(url)-len("#t="+fragToken(url))] {
		t.Errorf("State.URL must not carry the token fragment: %q", st.URL)
	}
	if fragToken(st.URL) != "" {
		t.Errorf("State leaked the token: %q", st.URL)
	}
}

func TestPageServedWithoutTokenAndContainsNoToken(t *testing.T) {
	_, url := newTestServer(t)
	tok := fragToken(url)
	resp := get(t, url, "/workspace", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page status = %d", resp.StatusCode)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %q", want, csp)
		}
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" ||
		resp.Header.Get("Referrer-Policy") != "no-referrer" ||
		resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("security headers incomplete on the page response")
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), tok) {
		t.Fatal("the page HTML leaked the launch token")
	}
}

func TestAPIRequiresBearer(t *testing.T) {
	_, url := newTestServer(t)
	for _, path := range []string{"/api/workspace/blocks", "/api/workspace/block?id=b1"} {
		resp := get(t, url, path, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without token: status = %d, want 401", path, resp.StatusCode)
		}
		resp2 := get(t, url, path, "wrong-token-value-wrong-token-value")
		if resp2.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s with wrong token: status = %d, want 401", path, resp2.StatusCode)
		}
	}
}

func TestBlocksEndpointShapeAndBound(t *testing.T) {
	_, url := newTestServer(t)
	tok := fragToken(url)
	resp := get(t, url, "/api/workspace/blocks", tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Error("blocks response must be no-store")
	}
	var got struct {
		V      int                 `json:"v"`
		Blocks []presentation.Meta `json:"blocks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.V != presentation.Version {
		t.Errorf("envelope v = %d, want %d", got.V, presentation.Version)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(got.Blocks))
	}
	// Metadata only: no payload bytes may appear in the index.
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "payload") {
		t.Error("index response leaked payload bytes")
	}
}

func TestBlockEndpointServesCanonicalEnvelope(t *testing.T) {
	_, url := newTestServer(t)
	tok := fragToken(url)
	resp := get(t, url, "/api/workspace/block?id=b1", tok)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var b presentation.Block
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.ID != "b1" || b.Kind != presentation.KindMarkdown || b.V != presentation.Version {
		t.Fatalf("block = %+v", b)
	}
	if !strings.Contains(string(b.Payload), "<script>") {
		t.Error("payload must be served verbatim as data (rendering neutralizes it)")
	}
	// Degrade block: unknown kind accepted, flagged, served.
	resp2 := get(t, url, "/api/workspace/block?id=b2", tok)
	var d presentation.Block
	if err := json.NewDecoder(resp2.Body).Decode(&d); err != nil {
		t.Fatalf("decode degrade: %v", err)
	}
	if d.Degrade == nil || d.Degrade.Reason != presentation.DegradeUnknownKind {
		t.Fatalf("degrade block = %+v", d)
	}
	// Unknown ID: 404, never a panic or a listing.
	resp3 := get(t, url, "/api/workspace/block?id=missing", tok)
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("missing block: status = %d, want 404", resp3.StatusCode)
	}
	// Path-ish IDs are refused before the lookup.
	for _, bad := range []string{"../../etc/passwd", "a/b", "a\\b", strings.Repeat("x", presentation.MaxIDLen+1)} {
		r := get(t, url, "/api/workspace/block?id="+strings.ReplaceAll(bad, "/", "%2F"), tok)
		if r.StatusCode != http.StatusBadRequest && r.StatusCode != http.StatusNotFound {
			t.Errorf("bad id %q: status = %d, want 400/404", bad, r.StatusCode)
		}
	}
}

func TestUnknownPath404AndNoDirectoryListing(t *testing.T) {
	_, url := newTestServer(t)
	for _, path := range []string{"/", "/view/", "/workspace/", "/workspace/../server.go", "/api/workspace/", "/api"} {
		resp := get(t, url, path, "")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("path %q: status = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	_, url := newTestServer(t)
	tok := fragToken(url)
	req, err := http.NewRequest(http.MethodPost, base(url)+"/api/workspace/blocks", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", resp.StatusCode)
	}
}

func TestAssetsServedWithNoStore(t *testing.T) {
	_, url := newTestServer(t)
	for _, path := range []string{"/workspace.js", "/workspace.css"} {
		resp := get(t, url, path, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", path, resp.StatusCode)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s missing no-store", path)
		}
	}
}

func TestStopClearsStateAndRotation(t *testing.T) {
	st := presentation.NewStore(4)
	_, _ = st.Put(presentation.Block{ID: "b", Kind: presentation.KindText, Payload: []byte("x")})
	s := NewServer(fakeSource{store: st})
	u1, err := s.Start()
	if err != nil {
		t.Fatalf("start 1: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop 1: %v", err)
	}
	if s.TokenURL() != "" || s.State().Running {
		t.Fatal("stop did not clear the lifecycle state")
	}
	u2, err := s.Start()
	if err != nil {
		t.Fatalf("start 2: %v", err)
	}
	if fragToken(u2) == "" || fragToken(u1) == fragToken(u2) {
		t.Fatal("token must rotate per launch")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop 2: %v", err)
	}
	if err := s.Stop(); err != nil { // idempotent
		t.Fatalf("second stop: %v", err)
	}
}

func TestStartTwiceFailsTyped(t *testing.T) {
	s, _ := newTestServer(t)
	if _, err := s.Start(); err == nil {
		t.Fatal("second Start must fail")
	}
}

func TestJSAssetContract(t *testing.T) {
	// The renderer must not contain any markup-injection primitive and must
	// read the token from the fragment only. This is the DOM-side half of
	// the "model output is data" invariant.
	js := pageJS
	for _, banned := range []string{"innerHTML", "insertAdjacentHTML", "document.write", "eval("} {
		if strings.Contains(js, banned) {
			t.Errorf("workspace.js contains banned primitive %q", banned)
		}
	}
	for _, required := range []string{"textContent", "location.hash", "Bearer "} {
		if !strings.Contains(js, required) {
			t.Errorf("workspace.js missing required primitive %q", required)
		}
	}
}

func TestSourceNilRefused(t *testing.T) {
	s := NewServer(nil)
	if _, err := s.Start(); err == nil {
		t.Fatal("Start without a source must fail")
	}
}
