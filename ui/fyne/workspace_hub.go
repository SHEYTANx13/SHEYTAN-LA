//go:build !headless

// workspace_hub.go — the UI-side owner of the Web Workspace slice.
//
// The hub is a thin client of the Go presentation contract: it holds the
// ONE bounded block store of this process, registers chat responses as
// markdown blocks after settlement, and starts/stops the on-demand
// loopback workspace server when the user asks for the rich surface.
//
// Failure posture: the browser surface is additive. If the server cannot
// start, the error surfaces in the native shell and the native rendering
// continues unchanged — content is never lost with the browser.
package fyneui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/presentation"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/workspace"
)

// wsHub owns the presentation store and the workspace server of the app.
type wsHub struct {
	store  *presentation.Store
	server *workspace.Server
}

func newWSHub() *wsHub {
	return &wsHub{store: presentation.NewStore(presentation.DefaultStoreMax)}
}

// source adapters — the workspace server reads through this interface.
func (h *wsHub) WorkspaceBlocks() []presentation.Meta { return h.store.List() }
func (h *wsHub) WorkspaceBlock(id string) (presentation.Block, bool) {
	return h.store.Get(id)
}

// chatBlockID derives the deterministic block ID of a chat response:
// content-addressed, so a re-issued identical response maps to the same
// logical artifact (one ID — one artifact), and an edited response gets
// its own block without mutating history.
func chatBlockID(sessionID, content string) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + content))
	return "chat-" + hex.EncodeToString(sum[:8])
}

// RegisterChatResponse issues the settled assistant response as a
// markdown block. Empty content registers nothing (no fabricated blocks).
// Refusals (oversized/invalid) are returned as errors and simply mean the
// native rendering remains the only surface — never a data loss.
func (h *wsHub) RegisterChatResponse(sessionID, content string) (presentation.Block, error) {
	if strings.TrimSpace(content) == "" {
		return presentation.Block{}, nil
	}
	b := presentation.Block{
		ID:        chatBlockID(sessionID, content),
		Kind:      presentation.KindMarkdown,
		Title:     "Chat response",
		SessionID: sessionID,
		Source:    "ui/chat",
		Payload:   []byte(content),
	}
	return h.store.Put(b)
}

// Blocks exposes the bounded snapshot (System Centre / tests).
func (h *wsHub) Blocks() []presentation.Meta { return h.store.List() }

// open starts the workspace server if needed and returns the launch URL
// carrying the per-launch fragment token. The URL is never logged.
func (h *wsHub) open() (string, error) {
	if h.server != nil && h.server.Running() {
		return h.server.TokenURL(), nil
	}
	if h.server == nil {
		h.server = workspace.NewServer(h)
	}
	return h.server.Start()
}

// stop shuts the server down at window close (idempotent).
func (h *wsHub) stop() {
	if h.server != nil {
		_ = h.server.Stop()
	}
}

// openWorkspace is the user-facing action: start (if needed) and hand the
// URL to the default browser. The native shell keeps rendering whatever
// it rendered — a browser failure changes nothing on this side.
func (ap *App) openWorkspace() error {
	raw, err := ap.ws.open()
	if err != nil {
		return fmt.Errorf("workspace could not start: %w", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("workspace launch URL invalid: %w", err)
	}
	var openErr error
	uiDo(func() { openErr = ap.a.OpenURL(u) })
	return openErr
}

// stopWorkspace releases the loopback listener (app teardown).
func (ap *App) stopWorkspace() { ap.ws.stop() }
