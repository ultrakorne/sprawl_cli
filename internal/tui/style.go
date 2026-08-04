package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/icons"
)

// styles holds the always-on lipgloss styles the TUI renders with.
//
// Colors are terminal ANSI palette indices (0-15) only — the same choices as
// internal/cli/style.go (1 red, 2 green, 3 yellow, 6 cyan) — so the UI adopts
// whatever theme the user's terminal is running rather than any hard-coded RGB.
//
// Unlike the CLI's gated `stylesEnabled` global, TUI styling is unconditional:
// bubbletea owns the terminal for the session and downgrades color to the
// terminal's real profile (and strips it entirely on a dumb terminal) at write
// time, so the model can always emit styled strings.
type styles struct {
	sel    lipgloss.Style // selection cursor + label: bold + cyan
	header lipgloss.Style // screen/section headers: bold + cyan
	accent lipgloss.Style // accent bits (rules, prompts)
	faint  lipgloss.Style // secondary / muted text
	bold   lipgloss.Style
	ok     lipgloss.Style // done / success (green)
	warn   lipgloss.Style // in-progress (yellow)
	danger lipgloss.Style // error / danger (red)
	plain  lipgloss.Style // no-op
	link   lipgloss.Style // clickable text (PR numbers): accent + underline

	icons icons.Set // item-state glyphs, shared with the CLI; see internal/icons
}

func newStyles() styles {
	ns := lipgloss.NewStyle
	return styles{
		icons:  icons.Resolve(),
		sel:    ns().Bold(true).Foreground(lipgloss.Color("6")),
		header: ns().Bold(true).Foreground(lipgloss.Color("6")),
		accent: ns().Foreground(lipgloss.Color("6")),
		faint:  ns().Faint(true),
		bold:   ns().Bold(true),
		ok:     ns().Foreground(lipgloss.Color("2")),
		warn:   ns().Foreground(lipgloss.Color("3")),
		danger: ns().Foreground(lipgloss.Color("1")),
		plain:  ns(),
		// Underlined accent is the terminal's own convention for a link. It is
		// applied ONLY where the URL actually resolves, so the styling is a
		// promise that clicking does something — a PR number on a project with no
		// repo stays faint like any other inert field.
		link: ns().Foreground(lipgloss.Color("6")).Underline(true),
	}
}

// progress colors a done/total pair like a traffic light, matching the CLI's
// progressStyle: green complete, yellow in progress, red nothing done, plain
// when there is no checklist at all.
func (s styles) progress(done, total int) lipgloss.Style {
	switch {
	case total == 0:
		return s.plain
	case done == 0:
		return s.danger
	case done >= total:
		return s.ok
	default:
		return s.warn
	}
}

// stateIcon is the glyph for an item state, or "" when it has none. One cell
// wide in both icon sets, which is what lets the state column be a constant.
func (s styles) stateIcon(state string) string { return s.icons.For(state) }

// stateBadge is icon + full name ("󱌣 In progress"), for the item header where
// there is room to spell it out. The names match the web app's pills.
func (s styles) stateBadge(state string) string {
	if state == "" {
		return ""
	}
	return s.stateIcon(state) + " " + stateName(state)
}

// state colors an item-state badge with the same vocabulary as the rest of the
// UI: ready is cyan (available to pick up), in progress yellow (running), in
// review green (work done, waiting on a human). Stateless items render plain —
// their badge is empty anyway.
func (s styles) state(state string) lipgloss.Style {
	switch state {
	case client.StateReadyToPickup:
		return s.accent
	case client.StateInProgress:
		return s.warn
	case client.StateInReview:
		return s.ok
	default:
		return s.faint
	}
}

// checkbox colors a checklist checkbox green when done, faint when open.
func (s styles) checkbox(done bool) lipgloss.Style {
	if done {
		return s.ok
	}
	return s.faint
}
