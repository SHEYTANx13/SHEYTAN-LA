//go:build !headless

// view_workspace.go — the Work Environment view: project card, workspace
// switch/reveal, recent files, the GitHub clone workflow and the
// repository index with bounded search (restores the React-only features
// the QML surface lacked).
package fyneui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type workspaceView struct {
	ap          *App
	summary     *widget.Label
	pathInput   *widget.Entry
	switchBtn   *widget.Button
	revealF     *widget.Button
	revealT     *widget.Button
	cloneURL    *widget.Entry
	cloneBtn    *widget.Button
	cloneCancel *widget.Button
	cloneStat   *widget.Label
	repoIdx     *widget.Label
	repoRefresh *widget.Button
	searchIn    *widget.Entry
	searchBtn   *widget.Button
	searchOut   *widget.Label
	errLabel    *widget.Label
}

func buildWorkspaceView(ap *App) fyne.CanvasObject {
	v := &workspaceView{ap: ap}
	v.summary = wrappedLabel("Loading workspace…")
	v.pathInput = widget.NewEntry()
	v.pathInput.SetPlaceHolder("Absolute path to a workspace directory")
	v.switchBtn = widget.NewButton("Switch", func() { v.doSwitch() })
	v.revealF = widget.NewButton("Reveal files", func() { v.reveal("files") })
	v.revealT = widget.NewButton("Open terminal", func() { v.reveal("terminal") })

	v.cloneURL = widget.NewEntry()
	v.cloneURL.SetPlaceHolder("https://github.com/owner/repository")
	v.cloneBtn = widget.NewButtonWithIcon("Clone", theme.ContentCopyIcon(), func() { v.clone() })
	v.cloneCancel = widget.NewButton("Cancel clone", func() {
		go func() { _ = ap.Client().CancelClone(ap.Ctx()) }()
	})
	v.cloneStat = wrappedLabel("")

	v.repoIdx = wrappedLabel("Repository index status loads on open.")
	v.repoRefresh = widget.NewButton("Re-index", func() { v.reindex() })
	v.searchIn = widget.NewEntry()
	v.searchIn.SetPlaceHolder("Search the repository index…")
	v.searchBtn = widget.NewButton("Search", func() { v.repoSearch() })
	v.searchOut = wrappedLabel("")

	v.errLabel = widget.NewLabel("")
	v.errLabel.Importance = widget.DangerImportance
	v.errLabel.Wrapping = fyne.TextWrapWord
	v.errLabel.Hide()

	projectCard := widget.NewCard("Project", "", v.summary)
	switchCard := widget.NewCard("Switch workspace", "Point the agent at another directory",
		container.NewVBox(container.NewHBox(v.pathInput, v.switchBtn), container.NewHBox(v.revealF, v.revealT)))
	cloneCard := widget.NewCard("Clone repository", "Clone from GitHub (backend gitclone is the URL authority)",
		container.NewVBox(container.NewHBox(v.cloneURL, v.cloneBtn, v.cloneCancel), v.cloneStat))
	repoCard := widget.NewCard("Repository index", "", container.NewVBox(
		container.NewHBox(v.repoRefresh, v.searchBtn, v.searchIn), v.repoIdx, v.searchOut))

	root := container.NewBorder(
		PageHeader("Workspace", "Work environment, projects and repository index"),
		v.errLabel, nil, nil,
		container.NewScroll(container.NewVBox(projectCard, switchCard, cloneCard, repoCard)),
	)
	v.refresh()
	return root
}

func (v *workspaceView) refresh() {
	go func() {
		ws, err := v.ap.Client().Workspace(v.ap.Ctx())
		if err != nil {
			uiDo(func() { v.showErr("workspace", err) })
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
		repoRaw, repoErr := v.ap.Client().RepoIndex(v.ap.Ctx())
		uiDo(func() {
			v.summary.SetText(txt)
			if repoErr == nil {
				v.repoIdx.SetText(jsonPretty(repoRaw, 3000))
			}
			v.errLabel.Hide()
		})
	}()
}

func (v *workspaceView) doSwitch() {
	path := strings.TrimSpace(v.pathInput.Text)
	if path == "" {
		return
	}
	ap := v.ap
	go func() {
		if err := ap.Client().SwitchWorkspace(ap.Ctx(), path); err != nil {
			uiDo(func() { v.showErr("switch workspace", err) })
			return
		}
		uiDo(func() { v.pathInput.SetText(""); v.errLabel.Hide() })
		v.refresh()
	}()
}

func (v *workspaceView) reveal(target string) {
	go func() {
		if err := v.ap.Client().RevealWorkspace(v.ap.Ctx(), target); err != nil {
			uiDo(func() { v.showErr("reveal", err) })
		}
	}()
}

func (v *workspaceView) clone() {
	url_ := strings.TrimSpace(v.cloneURL.Text)
	if url_ == "" {
		return
	}
	ap := v.ap
	v.cloneBtn.Disable()
	go func() {
		defer uiDo(func() { v.cloneBtn.Enable() })
		if _, err := ap.Client().CloneRepository(ap.Ctx(), url_, "", false); err != nil {
			uiDo(func() { v.showErr("clone", err); v.cloneStat.SetText("") })
			return
		}
		// Bounded status poll (1.2s cadence, max ~2 min) — mirrors the
		// retired WorkspacePanel contract; the backend stays the authority.
		for i := 0; i < 100; i++ {
			select {
			case <-ap.Ctx().Done():
				return
			case <-time.After(1200 * time.Millisecond):
			}
			st, err := ap.Client().CloneStatus(ap.Ctx())
			if err != nil {
				uiDo(func() { v.showErr("clone status", err) })
				return
			}
			var s struct {
				State    string `json:"state"`
				Done     bool   `json:"done"`
				Error    string `json:"error"`
				Switched bool   `json:"switched"`
			}
			_ = json.Unmarshal(st, &s)
			uiDo(func() { v.cloneStat.SetText("Clone: " + s.State) })
			if s.Done || s.Error != "" {
				if s.Error != "" {
					uiDo(func() { v.showErr("clone", fmt.Errorf("%s", s.Error)) })
				}
				v.refresh()
				return
			}
		}
	}()
}

func (v *workspaceView) reindex() {
	go func() {
		raw, err := v.ap.Client().RefreshRepoIndex(v.ap.Ctx())
		if err != nil {
			uiDo(func() { v.showErr("re-index", err) })
			return
		}
		uiDo(func() { v.repoIdx.SetText(jsonPretty(raw, 3000)); v.errLabel.Hide() })
	}()
}

func (v *workspaceView) repoSearch() {
	q := strings.TrimSpace(v.searchIn.Text)
	if q == "" {
		return
	}
	go func() {
		raw, err := v.ap.Client().RepoSearch(v.ap.Ctx(), map[string]any{"query": q, "limit": 20})
		if err != nil {
			uiDo(func() { v.showErr("repo search", err) })
			return
		}
		uiDo(func() { v.searchOut.SetText(jsonPretty(raw, 3000)); v.errLabel.Hide() })
	}()
}

func (v *workspaceView) showErr(what string, err error) {
	v.errLabel.SetText(what + " failed: " + err.Error())
	v.errLabel.Show()
}
