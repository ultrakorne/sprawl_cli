package tui

import "charm.land/lipgloss/v2"

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
}

func newStyles() styles {
	ns := lipgloss.NewStyle
	return styles{
		sel:    ns().Bold(true).Foreground(lipgloss.Color("6")),
		header: ns().Bold(true).Foreground(lipgloss.Color("6")),
		accent: ns().Foreground(lipgloss.Color("6")),
		faint:  ns().Faint(true),
		bold:   ns().Bold(true),
		ok:     ns().Foreground(lipgloss.Color("2")),
		warn:   ns().Foreground(lipgloss.Color("3")),
		danger: ns().Foreground(lipgloss.Color("1")),
		plain:  ns(),
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

// checkbox colors a checklist checkbox green when done, faint when open.
func (s styles) checkbox(done bool) lipgloss.Style {
	if done {
		return s.ok
	}
	return s.faint
}
