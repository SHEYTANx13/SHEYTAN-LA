//go:build !headless

// view_workspace.go — the Work Environment view (areas 5 + 6): project
// card, workspace switch/reveal, the GitHub clone workflow with status
// polling + cancel, the repository index and bounded search. Git lives
// INSIDE this view per the Fyne IA (documented in the migration map: the
// dock panel is one "Workspace" destination; git clone/status/cancel and
// the repo index are its Git card).
package goguiui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-gui-org/go-gui/gui"
)

// workspaceViewState is the Workspace view's state.
type workspaceViewState struct {
	st *shellState

	mu        sync.Mutex
	summary   string
	repoIdx   string
	searchOut string
	cloneStat string
	pathIn    string
	cloneURL  string
	searchIn  string
	err       error
	loaded    bool
	cloning   bool
}

func workspaceState(s *shellState) *workspaceViewState {
	return s.view("workspace", func() any { return &workspaceViewState{st: s} }).(*workspaceViewState)
}

func buildWorkspaceView(s *shellState) gui.View {
	v := workspaceState(s)
	v.ensureLoaded()

	return gui.Column(gui.ContainerCfg{
		ID:     absID("view-workspace"),
		Sizing: gui.FillFill,
		Content: []gui.View{
			PageHeader("Workspace", "Work environment, projects and repository index"),
			v.errView(),
			gui.Column(gui.ContainerCfg{
				ID:         absID("workspace-scroll"),
				Sizing:     gui.FillFill,
				Scrollable: true,
				Spacing:    gui.SpacingPx(6),
				Content: []gui.View{
					v.projectCard(),
					v.switchCard(),
					v.cloneCard(),
					v.repoCard(),
				},
			}),
		},
	})
}

func (v *workspaceViewState) ensureLoaded() {
	v.mu.Lock()
	if v.loaded {
		v.mu.Unlock()
		return
	}
	v.loaded = true
	v.mu.Unlock()
	v.refresh()
}

func (v *workspaceViewState) errView() gui.View {
	v.mu.Lock()
	err := v.err
	v.mu.Unlock()
	if err == nil {
		return nil
	}
	return ErrorSurface("workspace", err, nil)
}

func (v *workspaceViewState) projectCard() gui.View {
	v.mu.Lock()
	summary := v.summary
	loaded := v.loaded && v.err == nil
	v.mu.Unlock()
	if !loaded {
		return card("Project", "", LoadingSurface("workspace"))
	}
	if summary == "" {
		return card("Project", "", EmptySurface("◇", "No workspace", "Switch to a directory to load the project card."))
	}
	return card("Project", "", wrappedText(summary))
}

func (v *workspaceViewState) switchCard() gui.View {
	v.mu.Lock()
	pathIn := v.pathIn
	v.mu.Unlock()
	return card("Switch workspace", "Point the agent at another directory",
		gui.Input(gui.InputCfg{
			ID:          absID("ws-path"),
			Text:        pathIn,
			Placeholder: "Workspace directory path",
			OnEnter:     func(gui.EventCtx) { v.doSwitch() },
			OnTextChanged: func(text string, _ gui.EventCtx) {
				v.mu.Lock()
				v.pathIn = text
				v.mu.Unlock()
			},
		}),
		gui.Row(gui.ContainerCfg{
			Content: []gui.View{
				gui.Button(gui.ButtonCfg{ID: absID("ws-switch"), Label: "Switch", OnClick: func(gui.EventCtx) { v.doSwitch() }}),
				gui.Button(gui.ButtonCfg{ID: absID("ws-reveal-files"), Label: "Reveal files", OnClick: func(gui.EventCtx) { v.reveal("files") }}),
				gui.Button(gui.ButtonCfg{ID: absID("ws-reveal-terminal"), Label: "Open terminal", OnClick: func(gui.EventCtx) { v.reveal("terminal") }}),
			},
		}),
	)
}

func (v *workspaceViewState) cloneCard() gui.View {
	v.mu.Lock()
	cloneURL := v.cloneURL
	stat := v.cloneStat
	cloning := v.cloning
	v.mu.Unlock()

	rows := []gui.View{
		gui.Input(gui.InputCfg{
			ID:          absID("ws-clone-url"),
			Text:        cloneURL,
			Placeholder: "https://github.com/owner/repo",
			OnEnter:     func(gui.EventCtx) { v.clone() },
			OnTextChanged: func(text string, _ gui.EventCtx) {
				v.mu.Lock()
				v.cloneURL = text
				v.mu.Unlock()
			},
		}),
	}
	actions := []gui.View{
		gui.Button(gui.ButtonCfg{ID: absID("ws-clone"), Label: "Clone", Variant: gui.ButtonPrimary, OnClick: func(gui.EventCtx) { v.clone() }}),
	}
	if cloning {
		actions = append(actions, gui.Button(gui.ButtonCfg{
			ID: absID("ws-clone-cancel"), Label: "Cancel clone",
			OnClick: func(gui.EventCtx) {
				go func() { _ = v.st.ap.Client().CancelClone(v.st.ap.Ctx()) }()
			},
		}))
	}
	if stat != "" {
		rows = append(rows, wrappedText("Clone: "+stat))
	}
	rows = append(rows, gui.Row(gui.ContainerCfg{Content: actions}))
	return card("Clone repository", "Clone from GitHub (backend gitclone is the URL authority)", rows...)
}

func (v *workspaceViewState) repoCard() gui.View {
	v.mu.Lock()
	repoIdx := v.repoIdx
	searchOut := v.searchOut
	searchIn := v.searchIn
	v.mu.Unlock()

	rows := []gui.View{
		gui.Input(gui.InputCfg{
			ID:          absID("ws-repo-search"),
			Text:        searchIn,
			Placeholder: "Search repo index…",
			OnEnter:     func(gui.EventCtx) { v.repoSearch() },
			OnTextChanged: func(text string, _ gui.EventCtx) {
				v.mu.Lock()
				v.searchIn = text
				v.mu.Unlock()
			},
		}),
		gui.Row(gui.ContainerCfg{
			Content: []gui.View{
				gui.Button(gui.ButtonCfg{ID: absID("ws-reindex"), Label: "Re-index", OnClick: func(gui.EventCtx) { v.reindex() }}),
				gui.Button(gui.ButtonCfg{ID: absID("ws-search"), Label: "Search", OnClick: func(gui.EventCtx) { v.repoSearch() }}),
			},
		}),
	}
	if repoIdx != "" {
		rows = append(rows, codeText(repoIdx))
	}
	if searchOut != "" {
		rows = append(rows, codeText(searchOut))
	}
	return card("Repository index", "Bounded search over the core's repo index", rows...)
}

// --- actions ---

func (v *workspaceViewState) fail(what string, err error) {
	uiDo(func() {
		v.mu.Lock()
		v.err = fmt.Errorf("%s: %w", what, err)
		v.mu.Unlock()
	})
}

func (v *workspaceViewState) refresh() {
	ap := v.st.ap
	go func() {
		ws, err := ap.Client().Workspace(ap.Ctx())
		if err != nil {
			v.fail("workspace", err)
			return
		}
		txt := fmt.Sprintf("Root: %s\nExists: %v\nDefault: %v", ws.Root, ws.Exists, ws.IsDefault)
		var card struct {
			Summary string `json:"summary"`
		}
		if len(ws.ProjectCard) > 0 {
			_ = json.Unmarshal(ws.ProjectCard, &card)
			if card.Summary != "" {
				txt += "\n\n" + card.Summary
			}
		}
		repoRaw, repoErr := ap.Client().RepoIndex(ap.Ctx())
		uiDo(func() {
			v.mu.Lock()
			v.summary = txt
			if repoErr == nil {
				v.repoIdx = jsonPretty(repoRaw, 3000)
			}
			v.err = nil
			v.mu.Unlock()
		})
	}()
}

func (v *workspaceViewState) doSwitch() {
	v.mu.Lock()
	path := strings.TrimSpace(v.pathIn)
	v.mu.Unlock()
	if path == "" {
		return
	}
	ap := v.st.ap
	go func() {
		if err := ap.Client().SwitchWorkspace(ap.Ctx(), path); err != nil {
			v.fail("switch workspace", err)
			return
		}
		uiDo(func() {
			v.mu.Lock()
			v.pathIn = ""
			v.err = nil
			v.mu.Unlock()
		})
		v.refresh()
	}()
}

func (v *workspaceViewState) reveal(target string) {
	go func() {
		if err := v.st.ap.Client().RevealWorkspace(v.st.ap.Ctx(), target); err != nil {
			v.fail("reveal", err)
		}
	}()
}

func (v *workspaceViewState) clone() {
	v.mu.Lock()
	url_ := strings.TrimSpace(v.cloneURL)
	v.mu.Unlock()
	if url_ == "" {
		return
	}
	ap := v.st.ap
	uiDo(func() {
		v.mu.Lock()
		v.cloning = true
		v.mu.Unlock()
	})
	go func() {
		defer uiDo(func() {
			v.mu.Lock()
			v.cloning = false
			v.mu.Unlock()
		})
		if _, err := ap.Client().CloneRepository(ap.Ctx(), url_, "", false); err != nil {
			v.fail("clone", err)
			return
		}
		// Bounded status poll (1.2s cadence, max ~2 min) — the same
		// contract the Fyne surface shipped; the backend stays the
		// authority.
		for i := 0; i < 100; i++ {
			select {
			case <-ap.Ctx().Done():
				return
			case <-time.After(1200 * time.Millisecond):
			}
			st, err := ap.Client().CloneStatus(ap.Ctx())
			if err != nil {
				v.fail("clone status", err)
				return
			}
			var s struct {
				State string `json:"state"`
				Done  bool   `json:"done"`
				Error string `json:"error"`
			}
			_ = json.Unmarshal(st, &s)
			uiDo(func() {
				v.mu.Lock()
				v.cloneStat = s.State
				v.mu.Unlock()
			})
			if s.Done || s.Error != "" {
				if s.Error != "" {
					v.fail("clone", fmt.Errorf("%s", s.Error))
				}
				v.refresh()
				return
			}
		}
	}()
}

func (v *workspaceViewState) reindex() {
	go func() {
		raw, err := v.st.ap.Client().RefreshRepoIndex(v.st.ap.Ctx())
		if err != nil {
			v.fail("re-index", err)
			return
		}
		uiDo(func() {
			v.mu.Lock()
			v.repoIdx = jsonPretty(raw, 3000)
			v.err = nil
			v.mu.Unlock()
		})
	}()
}

func (v *workspaceViewState) repoSearch() {
	v.mu.Lock()
	q := strings.TrimSpace(v.searchIn)
	v.mu.Unlock()
	if q == "" {
		return
	}
	go func() {
		raw, err := v.st.ap.Client().RepoSearch(v.st.ap.Ctx(), map[string]any{"query": q, "limit": 20})
		if err != nil {
			v.fail("repo search", err)
			return
		}
		uiDo(func() {
			v.mu.Lock()
			v.searchOut = jsonPretty(raw, 3000)
			v.err = nil
			v.mu.Unlock()
		})
	}()
}
