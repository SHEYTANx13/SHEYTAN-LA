//go:build !headless

// artifact_card.go — UI/UX Phase 2 (Artifact / Workspace Integration):
// the native-first settled-artifact affordance.
//
// The card renders the LATEST SETTLED rich block after the transcript
// settles (markdown/code/diff/table/chart per the presentation surface
// contract) and NEVER references the provisional live bubble: only the
// settle path calls setBlock, and the card starts hidden with no
// actionable entry for empty or unsupported content.
//
// It is deliberately a SMALL native card — kind + title, one short safe
// plain-text excerpt, the truthful Workspace status, and the contextual
// Open Workspace action — not a second renderer: the native transcript
// above stays authoritative, and the rich document surface remains the
// on-demand loopback Workspace (opened in the browser; no webview ever
// enters the shell).
//
// Stale-reference safety is by construction: the card stores only the
// bounded Meta projection, re-resolves the block in the store at open
// time, and degrades visibly (status + disabled action) when the bounded
// presentation store has already evicted it.
package fyneui

import (
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/presentation"
)

// excerptRunes bounds the safe plain-text excerpt of the settled block.
const excerptRunes = 140

// Typed card states (errors.Is-able) — the open action refuses to fabricate
// a Workspace session for content it cannot resolve.
var (
	errNoArtifact      = errors.New("artifact card: no settled artifact")
	errArtifactEvicted = errors.New("artifact card: artifact no longer in the presentation store")
)

// artifactCard is the native preview/affordance card of Phase 2. It is NOT
// a state authority: the presentation store stays the only registry, the
// card renders one bounded Meta of it and re-checks that store on open.
type artifactCard struct {
	ap       *App
	card     *widget.Card
	kindLab  *widget.Label
	excerpt  *widget.Label
	status   *widget.Label
	openBtn  *widget.Button
	meta     *presentation.Meta
	lastOpen string // excerpt/summary of the content the meta came from
}

func newArtifactCard(ap *App) *artifactCard {
	c := &artifactCard{ap: ap}
	c.kindLab = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	c.excerpt = widget.NewLabel("")
	c.excerpt.Wrapping = fyne.TextWrapWord
	c.excerpt.TextStyle = fyne.TextStyle{Monospace: true}
	c.status = widget.NewLabel("")
	c.status.Wrapping = fyne.TextWrapWord
	c.status.Importance = widget.LowImportance
	// v1.11.1 button-fit policy: the short label ("Open workspace") keeps
	// the action readable at card widths; the card's kind and status
	// lines carry the context.
	c.openBtn = widget.NewButtonWithIcon("Open workspace", theme.ComputerIcon(), c.open)
	body := container.NewVBox(c.excerpt, c.status, c.openBtn)
	c.card = widget.NewCard("Artifact", "", container.NewVBox(c.kindLab, body))
	c.card.Hide()
	return c
}

// canvas returns the card widget for view composition.
func (c *artifactCard) canvas() fyne.CanvasObject { return c.card }

// setBlock renders a settled rich block. content is the settled payload
// text the meta was issued from (used only for the short safe excerpt —
// the card never renders the full payload; the transcript and the
// Workspace own the content).
func (c *artifactCard) setBlock(m presentation.Meta, content string) {
	c.meta = &m
	c.lastOpen = content
	c.kindLab.SetText(kindTitleLine(m))
	c.excerpt.SetText(plainExcerpt(content, excerptRunes))
	c.openBtn.Enable()
	c.syncStatus()
	c.card.Show()
}

// clear hides the card and drops the reference (no fabricated affordance).
func (c *artifactCard) clear() {
	c.meta = nil
	c.lastOpen = ""
	c.card.Hide()
}

// hasBlock reports whether the card currently references a settled block.
func (c *artifactCard) hasBlock() bool { return c.meta != nil }

// syncStatus renders the truthful Workspace state: closed / open, and the
// visible degradation when the referenced block is no longer resolvable.
// The native shell remains the owner of navigation and state — the status
// says exactly that instead of pretending the browser is part of the app.
func (c *artifactCard) syncStatus() {
	if c.meta == nil {
		return
	}
	if _, ok := c.ap.ws.WorkspaceBlock(c.meta.ID); !ok {
		c.status.SetText("Evicted from the bounded presentation store — the settled transcript above remains the record.")
		c.openBtn.Disable()
		return
	}
	if c.ap.ws.Running() {
		c.status.SetText("Workspace open in the browser (loopback, token-gated). This native view stays your session and navigation.")
	} else {
		c.status.SetText("Open starts the on-demand loopback Workspace (token-gated, read-only). The native shell stays authoritative.")
	}
}

// openTarget resolves the launch URL for the CURRENT card reference at
// open time. A stale reference is a typed error, never a blank surface.
func (c *artifactCard) openTarget() (string, error) {
	if c.meta == nil {
		return "", errNoArtifact
	}
	if _, ok := c.ap.ws.WorkspaceBlock(c.meta.ID); !ok {
		return "", fmt.Errorf("%w: %q", errArtifactEvicted, c.meta.ID)
	}
	return c.ap.ws.open()
}

// open is the user-facing contextual action: resolve, start the server if
// needed, hand the URL to the browser, refresh the truthful status. A
// stale or missing reference degrades visibly instead of opening a blank
// Workspace.
func (c *artifactCard) open() {
	raw, err := c.openTarget()
	if err != nil {
		c.syncStatus()
		return
	}
	if err := c.ap.openWorkspaceURL(raw); err != nil {
		c.syncStatus()
		return
	}
	c.syncStatus()
}

// kindTitleLine renders the bounded "kind · title" identification line.
func kindTitleLine(m presentation.Meta) string {
	title := m.Title
	if title == "" {
		title = "settled block"
	}
	return fmt.Sprintf("%s · %s", string(m.Kind), title)
}

// plainExcerpt collapses content to one bounded single-line plain-text
// excerpt. Labels render text, never markup, so the excerpt is safe by
// construction; the collapse keeps it a recognizable, non-flickering
// summary rather than a second rendering surface.
func plainExcerpt(s string, maxRunes int) string {
	fields := strings.Fields(s)
	line := strings.Join(fields, " ")
	r := []rune(line)
	if len(r) > maxRunes {
		return string(r[:maxRunes]) + "…"
	}
	if line == "" {
		return "(no text content)"
	}
	return line
}

// syncWorkspaceAffordance is the one truthful state sync for the general
// Workspace entry of a view: the entry is actionable only when at least
// one settled block exists (an empty Workspace surface is never a click
// target). It runs on the settle path only — no polling, no worker.
func syncWorkspaceAffordance(btn *widget.Button, ap *App) {
	if btn == nil || ap == nil {
		return
	}
	if len(ap.ws.Blocks()) > 0 {
		btn.Enable()
		return
	}
	btn.Disable()
}
