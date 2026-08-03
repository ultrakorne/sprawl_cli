package tui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// iconSet is the glyph vocabulary for item states. Two sets exist because the
// good one needs a font the user may not have.
type iconSet struct{ ready, progress, review string }

// Nerd Font Material Design Icons — the same icon family the web app's state
// pills use: a raised hand, a crossed hammer + wrench, an eye. They are
// monochrome glyphs in the Private Use Area, so unlike emoji they carry no
// colour of their own and inherit the terminal's foreground exactly like a
// letter does — which is what lets `state` below tint them from the ANSI
// palette. Each measures one cell.
var nerdIcons = iconSet{
	ready:    "\U000F0E47", // md-hand_back_right
	progress: "\U000F1323", // md-hammer_wrench
	review:   "\U000F0208", // md-eye
}

// plainIcons is the fallback for terminals whose font has no Nerd Font glyphs,
// where the set above renders as tofu boxes. Opt in with SPRAWL_ICONS=plain.
// Geometric shapes, also one cell each, also uncoloured.
var plainIcons = iconSet{ready: "▸", progress: "◐", review: "◉"}

// resolveIcons picks the icon set. Nerd Font glyphs are the default — this is a
// developer's terminal tool and a patched font is the norm — with an env escape
// hatch rather than a config field, matching how every other one-off override in
// this CLI works.
func resolveIcons() iconSet {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPRAWL_ICONS")), "plain") {
		return plainIcons
	}
	return nerdIcons
}

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

	icons iconSet // item-state glyphs; see resolveIcons
}

func newStyles() styles {
	ns := lipgloss.NewStyle
	return styles{
		icons:  resolveIcons(),
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
func (s styles) stateIcon(state string) string {
	switch state {
	case "":
		return ""
	case client.StateReadyToPickup:
		return s.icons.ready
	case client.StateInProgress:
		return s.icons.progress
	case client.StateInReview:
		return s.icons.review
	default:
		// A state added server-side after this build: show a marker rather than
		// nothing, so the row doesn't lie about being stateless.
		return "•"
	}
}

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
