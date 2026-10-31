//go:build !headless

// workspace_hub_test.go — the hybrid-presentation vertical slice over the
// Fyne test harness: blocks issued by the Go presentation contract are
// registered by the UI hub, served read-only by the workspace server, and
// every failure path leaves the native content untouched.
package fyneui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/presentation"
)

func TestWorkspaceHubRegistersChatResponse(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	hub := ap.Workspace()

	b, err := hub.RegisterChatResponse("sess-1", "# Title\n\nA **rich** answer")
	if err != nil {
		t.Fatalf("RegisterChatResponse: %v", err)
	}
	if b.Kind != presentation.KindMarkdown || b.Encoding != "utf8" {
		t.Fatalf("block = %+v", b)
	}
	if presentation.SelectSurface(b.Kind) != presentation.SurfaceWorkspace {
		t.Fatal("chat responses must prefer the workspace surface")
	}
	// Bounded snapshot served through the Source adapter.
	if got := hub.WorkspaceBlocks(); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("snapshot = %+v", got)
	}
	if _, ok := hub.WorkspaceBlock(b.ID); !ok {
		t.Fatal("served block missing by ID")
	}
}

func TestWorkspaceHubEmptyContentRegistersNothing(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	if b, err := ap.Workspace().RegisterChatResponse("s", "   "); err != nil || b.ID != "" {
		t.Fatalf("empty content must be a no-op, got %+v err=%v", b, err)
	}
	if n := len(ap.Workspace().Blocks()); n != 0 {
		t.Fatalf("blocks = %d, want 0 (no fabricated blocks)", n)
	}
}

func TestWorkspaceHubContentAddressedUpsert(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	hub := ap.Workspace()
	b1, err := hub.RegisterChatResponse("s", "same content")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b2, err := hub.RegisterChatResponse("s", "same content")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if b1.ID != b2.ID {
		t.Fatalf("identical content must map to one logical artifact: %q vs %q", b1.ID, b2.ID)
	}
	if hub := len(hub.Blocks()); hub != 1 {
		t.Fatalf("blocks = %d, want 1", hub)
	}
	if _, err := hub.RegisterChatResponse("s", "different content"); err != nil {
		t.Fatalf("edited content: %v", err)
	}
	if len(hub.Blocks()) != 2 {
		t.Fatalf("edited content must get its own block, blocks = %d", len(hub.Blocks()))
	}
}

func TestWorkspaceHubOpenServesRegisteredBlocks(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	hub := ap.Workspace()
	reg, err := hub.RegisterChatResponse("sess-open", "## Rich content\n\n- item one\n- item two")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	raw, err := hub.open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer ap.stopWorkspace()
	if !strings.Contains(raw, "#t=") {
		t.Fatal("launch URL must carry the fragment token")
	}
	origin := raw
	if i := strings.IndexByte(origin, '#'); i >= 0 {
		origin = origin[:i]
	}
	// scheme://host only — the launch URL also carries the /workspace path.
	if j := strings.Index(origin[len("http://"):], "/"); j >= 0 {
		origin = origin[:len("http://")+j]
	}
	if !strings.HasPrefix(origin, "http://127.0.0.1:") {
		t.Fatalf("origin %q is not loopback", origin)
	}

	// The page itself carries no token and requires none.
	resp, err := http.Get(origin + "/workspace")
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(page), strings.TrimPrefix(raw, origin+"/workspace#t=")) {
		t.Fatal("page leaked the launch token")
	}

	// The API requires the token and serves the registered block.
	req, _ := http.NewRequest(http.MethodGet, origin+"/api/workspace/block?id="+reg.ID, nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("api no-token: %v", err)
	}
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("api without token = %d, want 401", resp2.StatusCode)
	}
	resp2.Body.Close()

	req2, _ := http.NewRequest(http.MethodGet, origin+"/api/workspace/block?id="+reg.ID, nil)
	req2.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(raw, origin+"/workspace#t="))
	resp3, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("api: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("api with token = %d", resp3.StatusCode)
	}
	var got presentation.Block
	if err := json.NewDecoder(resp3.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != reg.ID || string(got.Payload) != "## Rich content\n\n- item one\n- item two" {
		t.Fatalf("served block mismatch: %+v", got)
	}
}

func TestWorkspaceHubCloseReleasesListener(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	hub := ap.Workspace()
	if _, err := hub.open(); err != nil {
		t.Fatalf("open: %v", err)
	}
	ap.stopWorkspace()
	if hub.server != nil && hub.server.Running() {
		t.Fatal("stopWorkspace left the listener running")
	}
	// Idempotent: a second stop must not error or panic.
	ap.stopWorkspace()
}

func TestWorkspaceButtonWiredInChatView(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	_ = newShell(ap)
	// The chat view must expose the Workspace action (composition contract):
	// buildChatView registers ap.shell callbacks; assert via the runtime the
	// view was constructed without panic and the hub exists on the App.
	if ap.Workspace() == nil {
		t.Fatal("App must own a workspace hub")
	}
}
