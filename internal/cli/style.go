package cli

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

// styler holds the lipgloss styles used to dress up `--format=text` (a.k.a.
// `-h`, human) output. It is intentionally tiny and immutable: every field is
// a value-type lipgloss.Style, so the package-level `sty` instance is safe to
// share across concurrently-running commands.
//
// Colors are the terminal's own ANSI palette (indices 0–15) rather than fixed
// RGB values, so the output adopts whatever theme the user's terminal is
// running — `lipgloss.Color("2")` is "the terminal's green", not a specific
// shade we picked.
//
// Styling is ONLY ever emitted on the text path. JSON and TOON are machine
// formats and never touch these styles (see output.go: renderPayload/reportErr
// only style the FormatText branch). On a non-terminal — a pipe, a file, a
// test buffer, or when $NO_COLOR is set — the colorprofile.Writer in output.go
// strips every escape sequence, so styled output degrades to the exact plain
// text it always was.
type styler struct {
	bold   lipgloss.Style // free-flowing titles & section labels (detail, whoami, activity)
	header lipgloss.Style // table column-header row (bold + accent color)
	accent lipgloss.Style // table header rule, and other accent bits
	faint  lipgloss.Style // detail-view keys, placeholders, muted values
	ok     lipgloss.Style // done / completed
	warn   lipgloss.Style // in-progress
	danger lipgloss.Style // blocked / errors
	plain  lipgloss.Style // no-op style (renders the string unchanged)
	errTag lipgloss.Style // the leading "error:" on stderr
}

// sty is the shared, read-only style set. lipgloss.Style values are immutable
// (every builder method returns a copy), so a package global is safe here.
var sty = newStyler()

// stylesEnabled gates whether the text builders emit any styling at all. It is
// a process-level switch (terminal color is a property of the process's stdout,
// not of an individual command) set once per Execute by enableStylingFor. It
// stays false unless we're writing text-format output to an actual terminal, so:
//   - json/toon output is never styled (resolveFormat won't be text),
//   - piped / redirected / file output stays plain,
//   - unit tests that call the text builders directly (no Execute) see plain
//     strings and their substring assertions keep matching.
//
// $NO_COLOR is honored separately, at write time, by the colorprofile.Writer in
// output.go — so even on a TTY, NO_COLOR strips the escapes this flag produced.
var stylesEnabled bool

// enableStylingFor decides whether human output to w should be styled: only
// when w is a real terminal. A strict TTY check (not colorprofile.Detect) keeps
// it deterministic across test/CI environments — CLICOLOR_FORCE can't flip a
// buffer into "styled".
func enableStylingFor(w io.Writer) {
	stylesEnabled = isTerminal(w)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(f.Fd())
}

func newStyler() *styler {
	ns := lipgloss.NewStyle
	// ANSI palette indices, rendered with the user's terminal theme rather than
	// hard-coded colors: 1 red, 2 green, 3 yellow, 6 cyan. Cyan is the table
	// header color — distinct from the status traffic-light (red/green/yellow)
	// so headers never collide with the data they sit above.
	return &styler{
		bold:   ns().Bold(true),
		header: ns().Bold(true).Foreground(lipgloss.Color("6")),
		accent: ns().Foreground(lipgloss.Color("6")),
		faint:  ns().Faint(true),
		ok:     ns().Foreground(lipgloss.Color("2")),
		warn:   ns().Foreground(lipgloss.Color("3")),
		danger: ns().Foreground(lipgloss.Color("1")),
		plain:  ns(),
		errTag: ns().Bold(true).Foreground(lipgloss.Color("1")),
	}
}

// progressStyle colors a done/total checklist pair like a traffic light:
// green when complete, yellow while in progress, red when nothing's done yet.
// A task with no checklist (total 0) renders plain — there's no progress to
// signal, so it isn't flagged.
func (s *styler) progressStyle(done, total int) lipgloss.Style {
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

// checkboxStyle colors a checklist checkbox: green when done, faint when still
// open.
func (s *styler) checkboxStyle(done bool) lipgloss.Style {
	if done {
		return s.ok
	}
	return s.faint
}

// render styles text for free-flowing (non-tabular) output.
func (s *styler) render(st lipgloss.Style, text string) string {
	if !stylesEnabled {
		return text
	}
	return st.Render(text)
}

// col is one table cell: text is the plain value (used to measure column
// width) and style is applied to it when styling is enabled. Keeping the plain
// text separate is what lets colored tables stay aligned — Go's tabwriter
// counts a colored cell's escape bytes toward its width, so it can't align
// colored columns; we measure from the plain text and lay the table out
// ourselves.
type col struct {
	text  string
	style lipgloss.Style
}

func plainCol(text string) col                    { return col{text, sty.plain} }
func styledCol(text string, s lipgloss.Style) col { return col{text, s} }

// renderTable lays out header + rows as a left-aligned table with a two-space
// gap, padding every column except the last to its widest plain value. The
// header is rendered as a whole bold line after layout (bolding before would
// not affect width, but doing it once keeps it simple). Widths are rune counts
// of the plain text, matching the previous tabwriter behavior for the ASCII
// columns it pads.
func renderTable(header []string, rows [][]col) string {
	width := make([]int, len(header))
	for i, h := range header {
		width[i] = len([]rune(h))
	}
	for _, row := range rows {
		for i, c := range row {
			if w := len([]rune(c.text)); w > width[i] {
				width[i] = w
			}
		}
	}

	hdrCols := make([]col, len(header))
	for i, h := range header {
		hdrCols[i] = plainCol(h)
	}

	var b strings.Builder
	// Leading blank line: a bit of breathing room so the header isn't jammed
	// against the command line / preceding output.
	b.WriteByte('\n')
	b.WriteString(sty.render(sty.header, layoutRow(hdrCols, width)))
	b.WriteByte('\n')
	b.WriteString(sty.render(sty.accent, tableRule(width)))
	for _, row := range rows {
		b.WriteByte('\n')
		b.WriteString(layoutRow(row, width))
	}
	return b.String()
}

// tableRule is the horizontal bar drawn under the header: one ─ line spanning
// the table's full width (every column plus the two-space gaps). It separates
// the header from the rows without drawing column dividers or a full grid. It's
// part of the layout, so it's present whether or not color is on; on a terminal
// it renders in the accent color, matching the header above it.
func tableRule(width []int) string {
	total := 2 * (len(width) - 1)
	for _, w := range width {
		total += w
	}
	return strings.Repeat("─", total)
}

// layoutRow renders one row: each non-final cell is styled then padded with
// spaces to its column width (+2 gap) based on the plain text length; the final
// cell is emitted unpadded so trailing columns (titles) don't get ragged
// whitespace.
func layoutRow(cells []col, width []int) string {
	var b strings.Builder
	last := len(cells) - 1
	for i, c := range cells {
		b.WriteString(sty.render(c.style, c.text))
		if i == last {
			break
		}
		pad := width[i] - len([]rune(c.text)) + 2
		b.WriteString(strings.Repeat(" ", pad))
	}
	return b.String()
}
