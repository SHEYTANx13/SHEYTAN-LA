package studio

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fixedSource returns a valid topology — the schema the api adapter must
// produce. Deliberately minimal but complete.
func fixedSource() Topology {
	return Topology{
		GeneratedAt: "2026-10-26T00:00:00Z",
		AppVersion:  "1.11.0",
		Source:      "test",
		Nodes: []Node{
			{ID: "host", Kind: "host", Label: "Host", Status: "ok"},
			{ID: "cpu", Kind: "cpu", Label: "CPU", Metrics: map[string]string{"cores": "8"}},
			{ID: "engine", Kind: "engine", Label: "Engine", Status: "offline"},
		},
		Edges: []Edge{
			{From: "host", To: "cpu", Relation: "contains"},
			{From: "host", To: "engine", Relation: "contains"},
		},
	}
}

type srcOK struct{}

func (srcOK) StudioTopology(context.Context) (Topology, error) { return fixedSource(), nil }

type srcFail struct{}

func (srcFail) StudioTopology(context.Context) (Topology, error) {
	return Topology{}, context.DeadlineExceeded
}

type srcInvalid struct{}

func (srcInvalid) StudioTopology(context.Context) (Topology, error) {
	// Duplicate node ids — a real schema violation the gate must catch.
	return Topology{Nodes: []Node{
		{ID: "a", Kind: "cpu", Label: "A"},
		{ID: "a", Kind: "cpu", Label: "A2"},
	}}, nil
}

func startTestViewer(t *testing.T, src ViewSource) (*Viewer, string) {
	t.Helper()
	v := NewViewer(src)
	url, err := v.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })
	return v, url
}

// baseURL strips the fragment AND the /view path: scheme://host:port.
func baseURL(launchURL string) string {
	rest := strings.SplitN(launchURL, "#", 2)[0]
	return strings.TrimSuffix(rest, "/view")
}

// fragToken extracts the per-launch token from the launch URL fragment.
func fragToken(launchURL string) string {
	frag := strings.SplitN(launchURL, "#", 2)[1]
	return strings.TrimPrefix(frag, "t=")
}

// authedGet performs the GET the real viewer page performs.
func authedGet(t *testing.T, launchURL, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL(launchURL)+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+fragToken(launchURL))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return res
}

func TestValidateAcceptsWellFormed(t *testing.T) {
	if err := fixedSource().Validate(); err != nil {
		t.Fatalf("valid topology rejected: %v", err)
	}
}

func TestValidateRejectsBrokenShapes(t *testing.T) {
	cases := map[string]Topology{
		"empty":         {},
		"no label":      {Nodes: []Node{{ID: "x", Kind: "cpu"}}},
		"no id":         {Nodes: []Node{{Kind: "cpu", Label: "X"}}},
		"dup id":        {Nodes: []Node{{ID: "x", Kind: "cpu", Label: "1"}, {ID: "x", Kind: "cpu", Label: "2"}}},
		"dangling edge": {Nodes: []Node{{ID: "x", Kind: "cpu", Label: "1"}}, Edges: []Edge{{From: "x", To: "ghost", Relation: "contains"}}},
	}
	for name, topo := range cases {
		if err := topo.Validate(); err == nil {
			t.Fatalf("%s: malformed topology accepted", name)
		}
	}
}

// Security: the topology endpoint is the ONLY data door and it is shut
// without the exact bearer token.
func TestTopologyRequiresToken(t *testing.T) {
	_, url := startTestViewer(t, srcOK{})

	res, err := http.Get(baseURL(url) + "/api/view")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, baseURL(url)+"/api/view", nil)
	req.Header.Set("Authorization", "Bearer deadbeef")
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("wrong-token: %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", res2.StatusCode)
	}
}

func TestTopologyServesWithToken(t *testing.T) {
	_, url := startTestViewer(t, srcOK{})
	res := authedGet(t, url, "/api/view")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("authorized status = %d, body %s", res.StatusCode, body)
	}
	var topo Topology
	if err := json.NewDecoder(res.Body).Decode(&topo); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(topo.Nodes) == 0 || topo.AppVersion != "1.11.0" {
		t.Fatalf("round-tripped topology lost data: %+v", topo)
	}
	// CSP must forbid every remote origin on data responses too.
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatal("CSP header missing on the topology response")
	}
}

func TestPageServesWithSecurityHeaders(t *testing.T) {
	_, url := startTestViewer(t, srcOK{})
	res, err := http.Get(baseURL(url) + "/view")
	if err != nil {
		t.Fatalf("get page: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("page status = %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "SHEYTAN-LA 3D Studio") {
		t.Fatal("page content mismatch")
	}
	csp := res.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("page CSP incomplete: %s", csp)
	}

	// Static assets resolve too (the page's importmap contract).
	for _, asset := range []string{"/view/studio.js", "/view/studio.css", "/view/assets/three.module.js", "/view/assets/three.core.js", "/view/assets/OrbitControls.js"} {
		ar, err := http.Get(baseURL(url) + asset)
		if err != nil {
			t.Fatalf("asset %s: %v", asset, err)
		}
		body, _ := io.ReadAll(ar.Body)
		ar.Body.Close()
		if ar.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("asset %s: status %d, %d bytes", asset, ar.StatusCode, len(body))
		}
	}

	// Token-free static asset named like the data endpoint must NOT exist.
	nf, err := http.Get(baseURL(url) + "/view/assets/../../../secrets")
	if err != nil {
		t.Fatalf("traversal: %v", err)
	}
	nf.Body.Close()
	if nf.StatusCode != http.StatusNotFound {
		t.Fatalf("traversal probe status = %d, want 404", nf.StatusCode)
	}
}

func TestFailingSourceFailsClosed(t *testing.T) {
	_, url := startTestViewer(t, srcFail{})
	res := authedGet(t, url, "/api/view")
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("source failure status = %d, want 503", res.StatusCode)
	}
}

func TestInvalidSourceFailsClosed(t *testing.T) {
	_, url := startTestViewer(t, srcInvalid{})
	res := authedGet(t, url, "/api/view")
	defer res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("invalid source status = %d, want 500", res.StatusCode)
	}
}

func TestUnknownPathIs404NoListing(t *testing.T) {
	_, url := startTestViewer(t, srcOK{})
	res, err := http.Get(baseURL(url) + "/view/../../etc/passwd")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("traversal probe status = %d, want 404", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if strings.Contains(string(body), "root:") {
		t.Fatal("traversal probe leaked file content")
	}
}

// Lifecycle: double start is refused; stop is idempotent; state is honest.
func TestViewerLifecycle(t *testing.T) {
	v, url := startTestViewer(t, srcOK{})
	if !v.Running() {
		t.Fatal("viewer must report running after Start")
	}
	if _, err := v.Start(); err == nil {
		t.Fatal("double Start must fail")
	}
	st := v.State()
	if !st.Running || st.Port == 0 || st.URL == "" {
		t.Fatalf("state while running must be complete: %+v", st)
	}
	if strings.Contains(st.URL, "t=") {
		t.Fatal("State().URL must never carry the token")
	}
	if v.TokenURL() == "" || !strings.Contains(v.TokenURL(), "#t=") {
		t.Fatal("TokenURL must carry the fragment token")
	}
	_ = url
	if err := v.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if v.Running() {
		t.Fatal("viewer must report stopped after Stop")
	}
	if err := v.Stop(); err != nil {
		t.Fatal("Stop must be idempotent")
	}
	if st := v.State(); st.Running {
		t.Fatal("State after Stop must be stopped")
	}
	if v.TokenURL() != "" {
		t.Fatal("TokenURL after Stop must be empty (token cleared)")
	}
}

// The viewer must be bound to loopback only — assert the listen address.
func TestViewerBindsLoopbackOnly(t *testing.T) {
	v, url := startTestViewer(t, srcOK{})
	base := baseURL(url)
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("viewer URL must be 127.0.0.1-bound, got %s", base)
	}
	st := v.State()
	if st.Port == 0 {
		t.Fatal("port must be reported")
	}
}

func TestStartStopStartWorks(t *testing.T) {
	v := NewViewer(srcOK{})
	url1, err := v.Start()
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := v.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	url2, err := v.Start()
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if url1 == url2 {
		t.Fatal("ephemeral ports: two launches must differ (or at least re-bind)")
	}
	_ = v.Stop()
}
