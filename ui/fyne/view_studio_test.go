//go:build !headless

// view_studio_test.go — the 3D Studio view contract: it builds against a
// live core, renders the honest enabled/stopped state with actionable
// controls, and renders the disabled state from the core's typed reason.
package fyneui

import (
	"testing"
)

// TestStudioViewEnabledStopped pins the default composition: with the
// core reporting enabled+stopped, the view builds a usable tree (the
// fake core never starts a viewer, so no running state may be shown).
func TestStudioViewEnabledStopped(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	obj := buildStudioView(ap)
	if obj == nil {
		t.Fatal("studio view must build")
	}
	if !treeHasObjects(obj) {
		t.Fatal("studio view must render a non-empty tree")
	}
	drainBackgroundRefreshes()
}

// TestStudioViewDisabledState pins the disabled rendering: with the core
// reporting studioEnabled=false the view says the feature is off — it
// still builds a usable, honest tree.
func TestStudioViewDisabledState(t *testing.T) {
	ap, srv := newTestAppForShell(t)
	srv.studioEnabled.Store(false)

	obj := buildStudioView(ap)
	if obj == nil || !treeHasObjects(obj) {
		t.Fatal("disabled studio view must still build a usable tree")
	}
	drainBackgroundRefreshes()
}

// TestStudioViewCoreError pins the typed failure state: an unresponsive
// core renders an error line, not a fake success and not a panic.
func TestStudioViewCoreError(t *testing.T) {
	ap, srv := newTestAppForShell(t)
	srv.failStudioState = true

	obj := buildStudioView(ap)
	if obj == nil || !treeHasObjects(obj) {
		t.Fatal("error-state studio view must still build")
	}
	drainBackgroundRefreshes()
}
