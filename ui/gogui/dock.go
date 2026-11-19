//go:build !headless

// dock.go — the central tabbed dock workspace with draggable/resizable
// splitters and PERSISTED layout, the go-gui-native upgrade over the
// Fyne shell's fixed body stack.
//
// Structure: the nine primary views are dock panels of one tabbed group
// (the central workspace). The go-gui DockLayout provides drag-and-drop
// panel rearrangement and splitter trees over that group tree; the
// contextual inspector and the activity drawer (desktop classes, see
// shell.go) are splitters around the dock whose RATIOS persist, and the
// dock node tree itself persists too (panel groups, selected panel,
// split ratios).
//
// Persistence: one small JSON file under the app data dir
// (<data>/ui/gogui-dock-layout.json). The tree is sanitized with
// gui.DockNodeSanitize after every load (the toolkit's documented
// hardening against malformed input). Tests inject a temp store.
package goguiui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/logging"
)

// DockLayoutState is the persisted docking geometry: the dock node tree
// (panel groups + ratios) and the surrounding splitter ratios.
type DockLayoutState struct {
	Tree           *gui.DockNode `json:"tree,omitempty"`
	InspectorRatio float32       `json:"inspectorRatio"`
	DrawerRatio    float32       `json:"drawerRatio"`
	SavedAt        string        `json:"savedAt,omitempty"`
}

// defaults are the first-run geometry.
func (s DockLayoutState) defaults() DockLayoutState {
	if s.InspectorRatio <= 0 || s.InspectorRatio >= 1 {
		s.InspectorRatio = 0.72
	}
	if s.DrawerRatio <= 0 || s.DrawerRatio >= 1 {
		s.DrawerRatio = 0.82
	}
	return s
}

// setInspectorRatio records an inspector split change (called from the
// splitter's OnChange on the frame thread; the value is a plain float32
// read by the next frame — go-gui regenerates every frame, so no lock is
// needed beyond the store write below).
func (s *DockLayoutState) setInspectorRatio(r float32) {
	if r <= 0 || r >= 1 {
		return
	}
	s.InspectorRatio = r
	s.saveSoon()
}

// setDrawerRatio records a drawer split change.
func (s *DockLayoutState) setDrawerRatio(r float32) {
	if r <= 0 || r >= 1 {
		return
	}
	s.DrawerRatio = r
	s.saveSoon()
}

// dockStore persists the layout state under the app data dir. Zero-value
// path means disabled (detached tests).
type dockStore struct{ path string }

// defaultDockStore resolves <data root>/ui/gogui-dock-layout.json. A
// resolution failure returns a disabled store (layout persists in-memory
// only for the session — never a crash from an unwritable disk).
func defaultDockStore() dockStore {
	root, _, _ := config.ResolveRoot()
	if root == "" {
		return dockStore{}
	}
	return dockStore{path: filepath.Join(root, "ui", "gogui-dock-layout.json")}
}

// Save writes the state atomically (tmp + rename).
func (st dockStore) Save(state DockLayoutState) error {
	if st.path == "" {
		return nil
	}
	state.SavedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// Load reads the persisted state; ok=false on any failure (first run,
// corrupt file, disabled store — callers keep defaults).
func (st dockStore) Load() (state DockLayoutState, ok bool) {
	if st.path == "" {
		return DockLayoutState{}, false
	}
	raw, err := os.ReadFile(st.path)
	if err != nil {
		return DockLayoutState{}, false
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		logging.Default().Error("ui", "dock layout file unreadable, keeping defaults: %v", err)
		return DockLayoutState{}, false
	}
	// The toolkit's documented hardening for deserialized trees.
	if state.Tree != nil {
		gui.DockNodeSanitize(state.Tree)
	}
	return state.defaults(), true
}

// saveSoon persists asynchronously (splitter drags fire on every frame;
// the write is debounced to one per quiet second by the worker below —
// the last state always wins).
var (
	saveMu      chan struct{}
	savePending = make(chan struct{}, 1)
	saveStarted bool
)

func (s *DockLayoutState) saveSoon() {
	if saveMu == nil {
		return // detached probe
	}
	select {
	case savePending <- struct{}{}:
	default:
	}
}

// startDockPersist runs the debounced persistence worker (App.Run).
func startDockPersist(ap *App) {
	if saveStarted {
		return
	}
	saveStarted = true
	saveMu = make(chan struct{})
	close(saveMu)
	go func() {
		for range savePending {
			time.Sleep(time.Second) // debounce: coalesce drag storms
			if ap.w == nil {
				continue
			}
			s := ap.shell
			s.mu.Lock()
			state := s.dock
			s.mu.Unlock()
			if err := ap.dockStore.Save(state); err != nil {
				logging.Default().Error("ui", "dock layout persist failed: %v", err)
			}
		}
	}()
}

// dockView builds the central dock workspace. All nine primary views are
// registered panels (draggable); the SELECTED panel of each group
// materializes content, so an unvisited panel costs one cfg struct, and
// panel selection IS navigation (OnPanelSelect → shell.show).
func (s *shellState) dockView(w *gui.Window) gui.View {
	s.mu.Lock()
	tree := s.dock.Tree
	if tree == nil {
		tree = defaultDockTree(s.active)
	}
	active := s.active
	s.mu.Unlock()

	panels := make([]gui.DockPanelDef, 0, len(primaryViews)+len(subPanels))
	for _, v := range primaryViews {
		vv := v
		content := []gui.View{}
		if vv.id == active {
			content = []gui.View{vv.build(s)}
		}
		panels = append(panels, gui.DockPanelDef{ID: vv.id, Label: vv.label, Content: content})
	}
	// The utility surfaces are dock panels too (reachable by drag) —
	// the grouped rail entry points stay the primary route.
	for _, p := range subPanels {
		pp := p
		content := []gui.View{}
		if pp.id == active {
			content = []gui.View{pp.build(s)}
		}
		panels = append(panels, gui.DockPanelDef{ID: pp.id, Label: pp.label, Content: content})
	}
	// Migration-added surfaces (real registry panels, not rail entries):
	// Downloads (area 4), Approvals (area 12) and the first-run wizard
	// (area 11). The rail IA keeps the Fyne 9+4 shape.
	extra := []struct {
		id    string
		label string
		build func(*shellState) gui.View
	}{
		{"downloads", "Downloads", buildDownloadsView},
		{"approvals", "Approvals", buildApprovalsView},
		{"onboarding", "Get started", buildOnboardingView},
	}
	for _, x := range extra {
		xx := x
		content := []gui.View{}
		if xx.id == active {
			content = []gui.View{xx.build(s)}
		}
		panels = append(panels, gui.DockPanelDef{ID: xx.id, Label: xx.label, Content: content})
	}

	return gui.DockLayout(gui.DockLayoutCfg{
		ID:     absID("dock"),
		Root:   tree,
		Panels: panels,
		OnPanelSelect: func(groupID, panelID string, _ gui.EventCtx) {
			s.show(panelID) // one navigation path: dock selection == show
		},
		OnLayoutChange: func(node *gui.DockNode, _ gui.EventCtx) {
			s.mu.Lock()
			s.dock.Tree = node
			s.mu.Unlock()
			s.dock.saveSoon()
		},
		OnPanelClose: func(panelID string, _ gui.EventCtx) {
			// A closed panel falls back to the first primary view — the
			// workspace is never blank (v1.11.1 contract, kept).
			s.show(primaryViews[0].id)
		},
	})
}

// defaultDockTree is the first-run tree: one tabbed group holding every
// primary view, the active id selected.
func defaultDockTree(selected string) *gui.DockNode {
	ids := make([]string, 0, len(primaryViews))
	for _, v := range primaryViews {
		ids = append(ids, v.id)
	}
	if selected == "" {
		selected = ids[0]
	}
	return gui.DockPanelGroup("dock-main", ids, selected)
}
