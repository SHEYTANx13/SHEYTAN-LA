//go:build !headless

// widgets.go — the shared product kit ported from ui/fyne/widgets.go:
// page headers, honest loading/empty/error surfaces, status badges, JSON
// evidence cards. Every view composes these so hierarchy, spacing and
// states stay coherent. Nothing fakes data: an empty surface never renders
// while loading, an error never renders as empty.
//
// go-gui mapping: go-gui has no retained widget objects — these helpers
// return gui.View builders that read the caller's state at generation
// time, so the same call sites work in every frame.
package goguiui

import (
	"fmt"
	"strings"

	"github.com/go-gui-org/go-gui/gui"
)

// uiIDPrefix scopes every addressable widget id this package mints. The
// IDSep ":" makes the leaf ABSOLUTE (own whole identity), so tests can
// address the composer/send/badge controls no matter how the dock or
// splitter panes scope their children (the POC's lesson, kept).
const uiIDPrefix = "ui:"

// absID mints an absolute widget id from parts.
func absID(parts ...string) string {
	return uiIDPrefix + strings.Join(parts, "-")
}

// TextStyle helpers over the active theme's semantic roles.
func styleTitle(t gui.Theme) gui.TextStyle   { return t.TextStyleTitle }
func styleBody(t gui.Theme) gui.TextStyle    { return t.TextStyleBody }
func styleSmall(t gui.Theme) gui.TextStyle   { return t.TextStyleBodySmall }
func styleCaption(t gui.Theme) gui.TextStyle { return t.TextStyleCaption }
func styleCode(t gui.Theme) gui.TextStyle    { return t.TextStyleCode }

// pageTitle builds the header title text (title role).
func pageTitle(text string) gui.View {
	return gui.Text(gui.TextCfg{Text: text, TextStyle: gui.CurrentTheme().TextStyleTitle})
}

// subtitleText builds a wrapped secondary line (caption role).
func subtitleText(text string) gui.View {
	if text == "" {
		return nil
	}
	return gui.Text(gui.TextCfg{Text: text, TextStyle: gui.CurrentTheme().TextStyleCaption, Mode: gui.TextModeWrap})
}

// PageHeader is the standard view header: title, optional subtitle, right
// slot for view actions — the same shape ui/fyne/widgets.go ships.
func PageHeader(title, subtitle string, actions ...gui.View) gui.View {
	content := []gui.View{pageTitle(title)}
	if s := subtitleText(subtitle); s != nil {
		content = append(content, s)
	}
	head := gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFill,
		Content: content,
	})
	row := gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Content: []gui.View{head, gui.Row(gui.ContainerCfg{Sizing: gui.FitFit, Content: actions})},
	})
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Content: []gui.View{row, gui.Separator(gui.SeparatorCfg{})},
		Spacing: gui.SpacingPx(4),
		Padding: gui.PadAll(8),
	})
}

// wrappedText builds a word-wrapping body text (no fixed geometry) — the
// go-gui analogue of ui/fyne's wrappedLabel. FillFit + TextWrap lets the
// box take the available width and grow vertically with the lines.
func wrappedText(s string) gui.View {
	return gui.Text(gui.TextCfg{Text: s, TextStyle: gui.CurrentTheme().TextStyleBody, Mode: gui.TextModeWrap, Sizing: gui.FillFit})
}

// codeText builds monospace read-only text (evidence, logs, JSON).
func codeText(s string) gui.View {
	return gui.Text(gui.TextCfg{Text: s, TextStyle: gui.CurrentTheme().TextStyleCode, Mode: gui.TextModeWrap, Sizing: gui.FillFit})
}

// card builds a titled surface card: the go-gui analogue of widget.NewCard
// (panel surface, padding, title + caption + content). The caption WRAPS
// (the v1.11.1 fit rule) and the content fills the width.
func card(title, caption string, content ...gui.View) gui.View {
	head := []gui.View{}
	if title != "" {
		head = append(head, gui.Text(gui.TextCfg{Text: title, TextStyle: gui.CurrentTheme().TextStyleBody}))
	}
	if caption != "" {
		head = append(head, subtitleText(caption))
	}
	items := append(head, content...)
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingPx(4),
		Padding: gui.PadAll(8),
		Color:   gui.CurrentTheme().ColorPanel,
		Radius:  gui.RadiusPx(8),
		Content: items,
	})
}

// LoadingSurface is the visible in-flight state (indefinite progress bar).
func LoadingSurface(what string) gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingPx(6),
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: "Loading " + what + "…", TextStyle: gui.CurrentTheme().TextStyleBody}),
			gui.ProgressBar(gui.ProgressBarCfg{Indefinite: true, Sizing: gui.FillFit}),
		},
	})
}

// EmptySurface is the honest nothing-here state — distinct from loading
// and error (the contract the retired EmptyState.qml shipped).
func EmptySurface(icon, title, detail string) gui.View {
	return gui.Column(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SpacingPx(6),
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: icon, TextStyle: gui.CurrentTheme().TextStyleTitle}),
			gui.Text(gui.TextCfg{Text: title, TextStyle: gui.CurrentTheme().TextStyleBody}),
			wrappedText(detail),
		},
	})
}

// ErrorSurface renders a typed failure with its reason — never a silent
// blank panel — plus an optional retry action.
func ErrorSurface(what string, err error, retry func()) gui.View {
	msg := "unknown failure"
	if err != nil {
		msg = err.Error()
	}
	content := []gui.View{
		// The failure marker is a themed error badge — the resolved
		// semantic colors live in the toolkit's style mirrors, not on
		// the public Theme struct (v0.97.0 verified).
		gui.Badge(gui.BadgeCfg{Label: "⚠ " + what + " failed", Variant: gui.BadgeError}),
		wrappedText(msg),
	}
	if retry != nil {
		content = append(content, gui.Button(gui.ButtonCfg{ID: absID("err-retry"), Label: "Retry", OnClick: func(gui.EventCtx) { retry() }}))
	}
	return gui.Column(gui.ContainerCfg{Sizing: gui.FillFit, Spacing: gui.SpacingPx(6), Content: content})
}

// StatusBadge is a small colored state chip. Semantic choices map to the
// toolkit's badge variants (success/warning/error/info/neutral) — the
// theme resolves the colors, so the badge can never drift from the theme.
func StatusBadge(id, text string, v gui.BadgeVariant) gui.View {
	return gui.Badge(gui.BadgeCfg{Label: text, Variant: v})
}

// Badge variants — the semantic choices call sites use.
const (
	BadgeNeutral = gui.BadgeInfo
	BadgeOK      = gui.BadgeSuccess
	BadgeWarn    = gui.BadgeWarning
	BadgeFail    = gui.BadgeError
	BadgeInfo    = gui.BadgeInfo
)

// EvidenceCard renders read-only JSON evidence (bounded), the pattern the
// System/Diagnostics views use for honest backend facts.
func EvidenceCard(title string, raw []byte) gui.View {
	return card(title, "", codeText(jsonPretty(raw, 4000)))
}

// kvRow helper for label/value lines.
func kvRow(k, v string) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Content: []gui.View{gui.Text(gui.TextCfg{Text: k + ":", TextStyle: gui.CurrentTheme().TextStyleBody}), wrappedText(v)},
	})
}

// infoLine returns "key: value" wrapped line.
func infoLine(k, v string) string {
	if v == "" {
		v = "—"
	}
	return fmt.Sprintf("%s: %s", k, v)
}
