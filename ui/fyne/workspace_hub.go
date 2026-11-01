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
	"sync"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/presentation"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/workspace"
)

// wsHub owns the presentation store and the workspace server of the app.
// Phase 2 addition: it also tracks the LATEST SETTLED rich block — the one
// contextual affordance the native artifact card renders. The tracker is
// a projection of the store (never a second registry): the authoritative
// content always lives in the bounded store, and the card re-resolves its
// block at open time so an evicted reference degrades visibly.
type wsHub struct {
	store  *presentation.Store
	server *workspace.Server

	lastMu   sync.RWMutex
	lastMeta *presentation.Meta
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

// registerConversation is the shared settle-path registration for the
// conversation surfaces (chat + agent). Empty content registers nothing
// (no fabricated blocks). A successful non-empty Put records the block as
// the latest settled artifact (content-addressed upsert: re-issuing the
// same response refreshes the same block — one ID, one artifact).
func (h *wsHub) registerConversation(sessionID, content, source, title string) (presentation.Block, error) {
	if strings.TrimSpace(content) == "" {
		return presentation.Block{}, nil
	}
	b := presentation.Block{
		ID:        chatBlockID(sessionID, content),
		Kind:      presentation.KindMarkdown,
		Title:     title,
		SessionID: sessionID,
		Source:    source,
		Payload:   []byte(content),
	}
	out, err := h.store.Put(b)
	if err != nil || out.ID == "" {
		return out, err
	}
	meta := presentation.MetaOf(out)
	h.lastMu.Lock()
	h.lastMeta = &meta
	h.lastMu.Unlock()
	return out, nil
}

// RegisterChatResponse issues the settled CHAT assistant response as a
// markdown block. Refusals (oversized/invalid) are returned as errors and
// simply mean the native rendering remains the only surface — never a
// data loss.
func (h *wsHub) RegisterChatResponse(sessionID, content string) (presentation.Block, error) {
	return h.registerConversation(sessionID, content, "ui/chat", "Chat response")
}

// RegisterAgentResponse issues the settled AGENT assistant response as a
// markdown block (Phase 2: the agent surface gets the same artifact
// affordance; chat and agent stay separate conversation spaces — the
// source field records which one produced the block).
func (h *wsHub) RegisterAgentResponse(sessionID, content string) (presentation.Block, error) {
	return h.registerConversation(sessionID, content, "ui/agent", "Agent response")
}

// Latest returns the latest settled rich block, if any. It is a read-only
// projection: callers must re-resolve the block in the store before
// acting on it (the store is bounded and may have evicted it).
func (h *wsHub) Latest() (presentation.Meta, bool) {
	h.lastMu.RLock()
	defer h.lastMu.RUnlock()
	if h.lastMeta == nil {
		return presentation.Meta{}, false
	}
	return *h.lastMeta, true
}

// Running reports truthfully whether the on-demand loopback workspace
// server is up (the status the native artifact card renders).
func (h *wsHub) Running() bool {
	return h.server != nil && h.server.Running()
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

// openWorkspaceURL hands an already-resolved launch URL to the default
// browser. The native shell keeps rendering whatever it rendered — a
// browser failure changes nothing on this side.
func (ap *App) openWorkspaceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("workspace launch URL invalid: %w", err)
	}
	var openErr error
	uiDo(func() { openErr = ap.a.OpenURL(u) })
	return openErr
}

// openWorkspace is the user-facing action: start (if needed) and hand the
// URL to the default browser.
func (ap *App) openWorkspace() error {
	raw, err := ap.ws.open()
	if err != nil {
		return fmt.Errorf("workspace could not start: %w", err)
	}
	return ap.openWorkspaceURL(raw)
}

// stopWorkspace releases the loopback listener (app teardown).
func (ap *App) stopWorkspace() { ap.ws.stop() }
