package cli

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	link   lipgloss.Style // clickable text (PR numbers): accent + underline
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

// outputWidth is the terminal's column count, captured once per Execute next to
// stylesEnabled. It's 0 when the destination isn't a terminal (pipe / file /
// test buffer) or its size can't be read. Renderers that wrap text to fit
// (e.g. the full-task notes) use it; 0 means "don't wrap". Like stylesEnabled
// it's process-level, so a single Execute decides it before any render — which
// also keeps it constant across a render, so wrapping can't shear the
// stripANSI==plain invariant (wrapping runs on plain text either way).
var outputWidth int

// enableStylingFor decides whether human output to w should be styled: only
// when w is a real terminal. A strict TTY check (not colorprofile.Detect) keeps
// it deterministic across test/CI environments — CLICOLOR_FORCE can't flip a
// buffer into "styled". It also records the terminal width for wrap-aware
// renderers.
func enableStylingFor(w io.Writer) {
	stylesEnabled = isTerminal(w)
	outputWidth = terminalWidth(w)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(f.Fd())
}

// terminalWidth returns w's column count, or 0 when w isn't a terminal or its
// size is unavailable (so callers treat 0 as "unknown / don't wrap").
func terminalWidth(w io.Writer) int {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(f.Fd())
	if err != nil {
		return 0
	}
	return cols
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
		// Underlined accent is the terminal's own convention for a link, and it
		// matches the TUI's. It is applied ONLY where the URL actually resolves,
		// so the styling is a promise that clicking does something — a PR number
		// on a project with no repo stays plain like any other inert field.
		link: ns().Foreground(lipgloss.Color("6")).Underline(true),
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

// hyperlink wraps already-rendered text in an OSC 8 escape so a ctrl/cmd-click
// opens url, leaving the visible characters untouched. An empty url (or styling
// off) returns text unchanged, so an unresolvable link is plain text rather than
// a dangling escape.
//
// Order matters and is the reason this is a separate step rather than part of a
// lipgloss style: the escape must go OUTSIDE the styling. Feeding an OSC 8
// sequence *into* lipgloss makes it render the URL as literal text — the link
// ends up printed on screen instead of attached to the number. Same constraint
// the TUI documents on Model.prField.
func hyperlink(text, url string) string {
	if url == "" || text == "" || !stylesEnabled {
		return text
	}
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}

// col is one table cell: text is the plain value (used to measure column
// width) and style is applied to it when styling is enabled. Keeping the plain
// text separate is what lets colored tables stay aligned — Go's tabwriter
// counts a colored cell's escape bytes toward its width, so it can't align
// colored columns; we measure from the plain text and lay the table out
// ourselves.
//
// link, when set, makes the cell an OSC 8 hyperlink to that URL. The escapes are
// zero-width, so they never enter the width maths — but they are only emitted
// when styling is on, for the same reason color is: a pipe or a redirect gets
// the plain characters and nothing else (see stylesEnabled).
type col struct {
	text  string
	style lipgloss.Style
	link  string
}

func plainCol(text string) col                    { return col{text: text, style: sty.plain} }
func styledCol(text string, s lipgloss.Style) col { return col{text: text, style: s} }

// linkedCol is a styled cell that is also a hyperlink. url == "" degrades to a
// plain styled cell, so callers can pass a resolved-but-possibly-empty URL
// unconditionally.
func linkedCol(text string, s lipgloss.Style, url string) col {
	return col{text: text, style: s, link: url}
}

// renderTable lays out header + rows as a left-aligned table with a two-space
// gap, padding every column except the last to its widest plain value. The
// header is rendered as a whole bold line after layout (bolding before would
// not affect width, but doing it once keeps it simple). Widths are rune counts
// of the plain text, matching the previous tabwriter behavior for the ASCII
// columns it pads.
func renderTable(header []string, rows [][]col) string {
	width := colWidths(header, rows)

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

// colWidths is the layout renderTable lays a table out on: each column is as
// wide as its widest plain value, and never narrower than its own header. It's
// exposed separately from renderTable so a renderer that has to align text
// *under* a column (the item table's notes) can ask where that column starts
// without re-deriving the layout — one source of truth for both.
func colWidths(header []string, rows [][]col) []int {
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
	return width
}

// colOffset is the display column that column i starts at: every preceding
// column's width plus the two-space gap after each. colOffset(w, 0) is 0.
func colOffset(width []int, i int) int {
	off := 0
	for _, w := range width[:i] {
		off += w + 2
	}
	return off
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
		b.WriteString(hyperlink(sty.render(c.style, c.text), c.link))
		if i == last {
			break
		}
		pad := width[i] - len([]rune(c.text)) + 2
		b.WriteString(strings.Repeat(" ", pad))
	}
	return b.String()
}
