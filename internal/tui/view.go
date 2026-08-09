package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// View renders the current screen (or active overlay) into a full-window view.
// AltScreen is set here — it's a View field in bubbletea v2, not a program
// option.
func (m *Model) View() tea.View {
	var s string
	if m.overlay != ovNone {
		s = m.viewOverlay()
	} else {
		switch m.current() {
		case screenCreds:
			s = m.viewCreds()
		case screenNotLoggedIn:
			s = m.viewNotLoggedIn()
		case screenList:
			s = m.viewList()
		case screenChecklist:
			s = m.viewChecklist()
		case screenNote:
			s = m.viewNote()
		default:
			s = m.viewList()
		}
	}
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

// heading styles a screen title for frame. Every frame call goes through it,
// except the item header, which appends its own link-styled PR afterwards.
func (m *Model) heading(s string) string { return m.styles.header.Render(s) }

func (m *Model) effWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

func (m *Model) effHeight() int {
	if m.height <= 0 {
		return 24
	}
	return m.height
}

// clip truncates a (possibly styled) line to the terminal width, ANSI-aware, so
// nothing ever overflows horizontally and breaks the layout.
func (m *Model) clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

const (
	// maxHintRows caps how far the key hints may grow. Two is enough for the
	// busiest screen; a third would eat the body for no gain.
	maxHintRows = 2
	// fixedChrome is what every frame spends regardless: title, rule, status.
	// The hints add 1 or 2 more, decided per screen by hintLines.
	fixedChrome = 3
	// hintSep joins hint segments and is where hintLines is allowed to break.
	hintSep = " · "
)

// hintsFor is the key-hint text for a base screen, as one string. Screens
// declare it in one place because both the renderer and the scroll-clamp maths
// need to know how tall the footer will be.
func hintsFor(s screen) string {
	switch s {
	case screenList:
		return "↑↓ move · enter open · / search · c copy · n new · e title · E desc · t due · d delete · r refresh · ? help · q quit"
	case screenChecklist:
		return "↑↓ move · space/x toggle · s state · p pr · o open pr · enter note · c copy · n add · e title · d delete · esc back · ? help"
	case screenNote:
		return "e edit · c copy · o open pr · ↑↓ scroll · esc back · ? help"
	}
	return ""
}

// hintLines packs the hints into as few rows as fit: one line whenever the
// whole string fits the width, spilling onto a second only when it doesn't.
//
// The BOTTOM row is the one that gets filled. Packing walks the segments
// backwards, taking as many trailing bindings as fit on the last line, and
// whatever is left rises to the line above — so the dense row sits against the
// bottom edge and the overflow grows upward, rather than a full top line with a
// stub hanging under it. Segment order is untouched, so it still reads left to
// right, top to bottom. Breaks land on the ` · ` separators, never mid-binding.
func hintLines(hints string, w int) []string {
	if hints == "" {
		return nil
	}
	if w <= 0 || lipgloss.Width(hints) <= w {
		return []string{hints}
	}
	segs := strings.Split(hints, hintSep)
	bottom, i := "", len(segs)-1
	for ; i >= 0; i-- {
		cand := segs[i]
		if bottom != "" {
			cand += hintSep + bottom
		}
		if lipgloss.Width(cand) > w {
			break
		}
		bottom = cand
	}
	// A single binding wider than the window: nothing to split on, so hand the
	// whole string back as one row and let frame clip it.
	if bottom == "" {
		return []string{hints}
	}
	// More than two rows' worth: the leftovers stay on the top row and get
	// clipped there. Losing the middle of the list beats losing its tail, which
	// is where `? help` lives.
	return []string{strings.Join(segs[:i+1], hintSep), bottom}
}

// bodyHeight is the number of rows a screen's body may occupy once the chrome
// above and below it is accounted for. Renderers that window their content take
// it as an argument so they can't drift out of step with frame().
func (m *Model) bodyHeight(hints string) int {
	return maxInt(1, m.effHeight()-fixedChrome-len(hintLines(hints, m.effWidth())))
}

// frame assembles a screen: a header (title + rule), a body region clipped to
// the available height, a transient status line, and the key hints on as few
// rows as they fit. The output is exactly effHeight() lines tall for normal
// windows; on a very short window it is clamped to effHeight() so the frame
// never overflows the viewport and breaks the layout.
//
// `title` arrives ALREADY STYLED — see heading(). frame can't style it here
// because the item header embeds an OSC 8 hyperlink, and rendering a string
// that contains one prints the URL instead of linking it.
func (m *Model) frame(title string, body []string, hints string) string {
	w := m.effWidth()
	h := m.effHeight()
	if m.loading {
		title += "  " + m.styles.faint.Render("loading…")
	}

	lines := make([]string, 0, h)
	lines = append(lines, m.clip(title, w))
	lines = append(lines, m.clip(m.styles.accent.Render(strings.Repeat("─", w)), w))

	bodyH := m.bodyHeight(hints)
	shown := body
	if len(shown) > bodyH {
		shown = shown[:bodyH]
	}
	for _, ln := range shown {
		lines = append(lines, m.clip(ln, w))
	}
	for i := len(shown); i < bodyH; i++ {
		lines = append(lines, "")
	}

	status := m.status
	statusStyle := m.styles.faint
	if m.statusErr {
		statusStyle = m.styles.danger
	}
	if status == "" {
		status = " "
	}
	lines = append(lines, m.clip(statusStyle.Render(status), w))
	for _, row := range hintLines(hints, w) {
		lines = append(lines, m.clip(m.styles.faint.Render(row), w))
	}

	// Never emit more rows than the viewport has: the chrome alone is several
	// lines, which would overflow a shorter window.
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// rowLines renders one logical row as one or more display lines: the fixed
// prefix on the first line, then `title` word-wrapped into the remaining width
// with continuation lines hanging-indented under the title column (so a long
// title flows onto a second line instead of being truncated with an ellipsis).
// prefixW is the prefix's PLAIN display width.
//
// The prefix is emitted VERBATIM and is the caller's job to style; `style`,
// when non-nil, is applied to the title text only. That split exists because a
// prefix cell may carry an OSC 8 hyperlink, and lipgloss re-encodes any ESC it
// finds in its input — re-rendering such a prefix would spell the URL out on
// screen. Styling per cell instead of per line is visually identical here: the
// selection style is bold + foreground with no background, so the gaps between
// cells have nothing to show. On a terminal too narrow to wrap sensibly the
// title is left on the first line for frame() to clip.
func rowLines(prefix string, prefixW, width int, title string, style func(...string) string) []string {
	apply := func(s string) string {
		if style == nil {
			return s
		}
		return style(s)
	}
	segs := []string{title}
	if avail := width - prefixW; avail >= 8 {
		if wrapped := wrapLines(title, avail); len(wrapped) > 0 {
			segs = wrapped
		}
	}
	indent := strings.Repeat(" ", prefixW)
	out := make([]string, 0, len(segs))
	for i, seg := range segs {
		if i == 0 {
			out = append(out, prefix+apply(seg))
		} else {
			out = append(out, indent+apply(seg))
		}
	}
	return out
}

// wrapAndWindow flattens per-item display-line groups (each 1+ wrapped lines)
// into a scrolling window exactly bodyH lines tall, keeping the selected item's
// lines visible — preferring its top when the item alone is taller than the
// viewport. Windowing over display lines (not logical items) keeps wrapped rows
// from being sheared at the window edge.
func wrapAndWindow(groups [][]string, sel, bodyH int) []string {
	var all []string
	starts := make([]int, len(groups))
	for i, g := range groups {
		starts[i] = len(all)
		all = append(all, g...)
	}
	if bodyH <= 0 || len(all) <= bodyH {
		return all
	}
	selStart, selEnd := 0, len(all)
	if sel >= 0 && sel < len(groups) {
		selStart = starts[sel]
		selEnd = selStart + len(groups[sel])
	}
	start := selStart - (bodyH-(selEnd-selStart))/2
	lo := selEnd - bodyH // keep the selected item's bottom in view
	hi := selStart       // keep its top in view
	switch {
	case lo > hi: // taller than the viewport — pin to its top
		start = hi
	case start < lo:
		start = lo
	case start > hi:
		start = hi
	}
	if start < 0 {
		start = 0
	}
	if start > len(all)-bodyH {
		start = len(all) - bodyH
	}
	return all[start : start+bodyH]
}

// -- list screen ------------------------------------------------------------

func (m *Model) viewList() string {
	title := "sprawl · tasks"
	// Under a project key the list is server-filtered to one project, which is
	// invisible otherwise — name it so an empty list reads as "this project has
	// no tasks" rather than "sprawl is broken".
	if m.projectKey != "" {
		title = "sprawl · tasks · " + m.projectKey
	}
	if m.searching {
		title = "search: " + m.searchInput.render()
	} else if m.searchLabel != "" {
		title = fmt.Sprintf("sprawl · search %q (%d)", m.searchLabel, len(m.tasks))
	}

	hints := hintsFor(screenList)
	vis := m.visibleTasks()
	var body []string
	switch {
	case m.loading && len(vis) == 0:
		body = []string{m.styles.faint.Render("loading…")}
	case len(vis) == 0:
		if m.searching || m.searchLabel != "" {
			body = []string{m.styles.faint.Render("(no matches)")}
		} else {
			body = []string{m.styles.faint.Render("(no tasks) — n to create")}
		}
	default:
		body = m.listRows(vis, m.bodyHeight(hints))
	}

	return m.frame(m.heading(title), body, hints)
}

func (m *Model) listRows(vis []*client.Task, bodyH int) []string {
	// Column widths from the visible set for stable alignment.
	idW, progW := 2, 3
	for _, t := range vis {
		if l := len("#" + itoa(t.ID)); l > idW {
			idW = l
		}
		if l := len(fmt.Sprintf("%d/%d", t.ChecklistProgress.Done, t.ChecklistProgress.Total)); l > progW {
			progW = l
		}
	}

	w := m.effWidth()

	groups := make([][]string, len(vis))
	for i, t := range vis {
		id := padRight("#"+itoa(t.ID), idW)
		prog := fmt.Sprintf("%d/%d", t.ChecklistProgress.Done, t.ChecklistProgress.Total)
		project := projectLabel(t.Project)
		due := t.DueDate
		if strings.TrimSpace(due) == "" {
			due = "—"
		}
		plainPrefix := fmt.Sprintf("  %s  %s  %s  %s  ", id, padRight(prog, progW), padRight(due, 10), project)
		prefixW := lipgloss.Width(plainPrefix)

		var group []string
		if i == m.listSel {
			// Selected: cursor + whole row bold+cyan (spec's selection style).
			// Styled here rather than by rowLines, which now only styles the title.
			cursorPrefix := m.styles.sel.Render(
				fmt.Sprintf("› %s  %s  %s  %s  ", id, padRight(prog, progW), padRight(due, 10), project))
			group = rowLines(cursorPrefix, prefixW, w, t.Title, m.styles.sel.Render)
		} else {
			progStyled := m.styles.progress(t.ChecklistProgress.Done, t.ChecklistProgress.Total).Render(padRight(prog, progW))
			displayPrefix := fmt.Sprintf("  %s  %s  %s  %s  ",
				m.styles.faint.Render(id), progStyled, m.styles.faint.Render(padRight(due, 10)), project)
			group = rowLines(displayPrefix, prefixW, w, t.Title, nil)
		}
		// Search-result annotation: surface matched checklist-item titles on their
		// own indented line under the task.
		if m.searchLabel != "" && len(t.MatchedChecklistItems) > 0 {
			var names []string
			for _, it := range t.MatchedChecklistItems {
				names = append(names, it.Title)
			}
			group = append(group, "      "+m.styles.faint.Render("↳ "+strings.Join(names, ", ")))
		}
		groups[i] = group
	}
	return wrapAndWindow(groups, m.listSel, bodyH)
}

// -- checklist screen -------------------------------------------------------

func (m *Model) viewChecklist() string {
	if m.detail == nil {
		return m.frame(m.heading("sprawl · task"), []string{m.styles.faint.Render("loading…")}, "esc back")
	}
	t := m.detail
	due := t.DueDate
	if strings.TrimSpace(due) == "" {
		due = "—"
	}
	prog := fmt.Sprintf("%d/%d", t.ChecklistProgress.Done, t.ChecklistProgress.Total)
	title := fmt.Sprintf("#%d %s  ·  %s  ·  due %s  ·  %s",
		t.ID, t.Title, m.styles.progress(t.ChecklistProgress.Done, t.ChecklistProgress.Total).Render(prog),
		due, projectLabel(t.Project))

	hints := hintsFor(screenChecklist)
	var body []string
	if len(t.ChecklistItems) == 0 {
		body = []string{m.styles.faint.Render("(no checklist items) — n to add")}
	} else {
		body = m.checklistRows(t.ChecklistItems, m.bodyHeight(hints))
	}
	return m.frame(m.heading(title), body, hints)
}

func (m *Model) checklistRows(items []*client.ChecklistItem, bodyH int) []string {
	idW := 2
	prW := prColMinW
	for _, it := range items {
		if l := len("#" + itoa(it.ID)); l > idW {
			idW = l
		}
		prW = maxInt(prW, lipgloss.Width(prCell(it.PRNumber)))
	}
	w := m.effWidth()

	groups := make([][]string, len(items))
	for i, it := range items {
		box := mdCheckbox(it.Completed)
		id := padRight("#"+itoa(it.ID), idW)
		// State and PR are their own fixed columns between the id and the title,
		// blank when unset. Padding is by display width, not rune count — the
		// glyphs aren't ASCII.
		icon := padVis(m.styles.stateIcon(it.State), stateColW)
		// Styled and linked BEFORE padding, so only the number is clickable and
		// only the number is underlined — not the blank cells aligning the column.
		selPR := padVis(m.prField(prCell(it.PRNumber), it.PRNumber, true), prW)
		rowPR := padVis(m.prField(prCell(it.PRNumber), it.PRNumber, false), prW)
		// The note flag rides at the end of the title so it wraps with it; the 🗒
		// emoji renders in its own color, so no separate faint styling is needed.
		title := it.Title
		if it.HasNotes {
			title += " 🗒"
		}
		// Measured from plain text: the styled cells carry escapes, and the PR
		// cell a hyperlink, none of which occupy columns.
		prefixW := lipgloss.Width(fmt.Sprintf("  %s %s  %s  %s  ",
			box, id, icon, padVis(prCell(it.PRNumber), prW)))
		if i == m.itemSel {
			// Styled cell by cell rather than as a whole line, so the PR's
			// hyperlink stays outside anything lipgloss renders.
			sel := m.styles.sel.Render
			cursorPrefix := sel(fmt.Sprintf("› %s %s  %s  ", box, id, icon)) + selPR + sel("  ")
			groups[i] = rowLines(cursorPrefix, prefixW, w, title, sel)
		} else {
			displayPrefix := fmt.Sprintf("  %s %s  %s  %s  ",
				m.styles.checkbox(it.Completed).Render(box),
				m.styles.faint.Render(id),
				m.styles.state(it.State).Render(icon),
				rowPR)
			groups[i] = rowLines(displayPrefix, prefixW, w, title, nil)
		}
	}
	return wrapAndWindow(groups, m.itemSel, bodyH)
}

// Checklist-row column widths. Both are CONSTANT (a floor, in the PR column's
// case) rather than measured from whatever the visible items happen to carry: a
// checklist where nothing has a state still reserves the column, so toggling one
// item from none to a state never reflows the rows around it. Stable columns are
// worth a few dead cells on boards that don't use the fields.
const (
	stateColW = 1 // every glyph in both icon sets is one cell
	prColMinW = 5 // "#1234"; grows only if a longer number is on screen
)

// stateName is the full label the web app shows on its pills — used where there
// is room to spell it out (the item header), never in a row.
func stateName(state string) string {
	switch state {
	case "":
		return ""
	case client.StateReadyToPickup:
		return "Ready to pick up"
	case client.StateInProgress:
		return "In progress"
	case client.StateInReview:
		return "In review"
	default:
		return state
	}
}

// stateLabel is the bare short word for a state, used in footer confirmations
// where it echoes the vocabulary `checklist state` accepts.
func stateLabel(state string) string {
	switch state {
	case client.StateReadyToPickup:
		return "ready"
	case client.StateInProgress:
		return "progress"
	case client.StateInReview:
		return "review"
	default:
		return state
	}
}

// prCell is the PR column's contents: `#412`, or blank when the item has none.
// A dash would add noise to every row on a board that doesn't use PRs, and the
// column is reserved either way.
func prCell(prNumber int64) string {
	if prNumber <= 0 {
		return ""
	}
	return "#" + itoa(prNumber)
}

// linkPR turns a rendered PR number into an OSC 8 hyperlink so a ctrl/cmd-click
// opens it, leaving the visible text untouched. The escape sequences are
// zero-width and ansi.Truncate keeps them balanced when a line is clipped, so
// this can't shear a column or leak the link into the rest of the screen.
//
// Terminals that don't understand OSC 8 (and older tmux, which won't forward
// it) simply show the plain text — which is why `o` exists as the binding that
// always works, rather than this being the only way to reach a PR.
func (m *Model) linkPR(text string, prNumber int64) string {
	url := m.prURL(prNumber)
	if text == "" || url == "" {
		return text
	}
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}

// prURL is the loaded task's link for a PR number, or "" when the chain doesn't
// resolve (no task loaded, no project, no repo URL).
func (m *Model) prURL(prNumber int64) string {
	if m.detail == nil {
		return ""
	}
	return client.PRURL(m.detail.Project, prNumber)
}

// prField renders a PR number for display: hyperlinked AND link-coloured when
// the URL resolves, faint and inert when it doesn't. Tying the colour to the
// same condition as the link keeps it honest — an underlined accent number that
// did nothing on click would be a worse lie than no colour at all.
//
// `text` is the visible form: a bare `#412` in the row's column, `PR #412` in
// the item header where there is no column to name it. `selected` swaps the
// colour for the row-selection one but KEEPS the underline: a link that stops
// looking like a link the moment the cursor lands on it reads as a glitch.
func (m *Model) prField(text string, prNumber int64, selected bool) string {
	if text == "" {
		return ""
	}
	linked, inert := m.styles.link, m.styles.faint
	if selected {
		linked, inert = m.styles.sel.Underline(true), m.styles.sel
	}
	if m.prURL(prNumber) == "" {
		return inert.Render(text)
	}
	// Style first, THEN wrap in the hyperlink. The other order feeds an OSC 8
	// escape to lipgloss, which renders it as literal text — the URL ends up
	// printed on screen instead of attached to the number.
	return m.linkPR(linked.Render(text), prNumber)
}

// padVis pads s with spaces to a display width of w (ANSI- and wide-rune-aware,
// unlike padRight's rune count — the state glyphs aren't one cell each on every
// terminal). Never truncates.
func padVis(s string, w int) string {
	if pad := w - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// -- note screen ------------------------------------------------------------

func (m *Model) viewNote() string {
	it := m.selectedItem()
	if it == nil {
		return m.frame(m.heading("sprawl · note"), []string{m.styles.faint.Render("(item gone)")}, "esc back")
	}
	// The item view is where the state gets spelled out: the row it came from
	// only had room for the icon. The PR is labelled here too — there is no
	// column header to tell you what a bare `#412` is.
	head := fmt.Sprintf("note · item #%d %s", it.ID, it.Title)
	if badge := m.styles.stateBadge(it.State); badge != "" {
		head += "  ·  " + badge
	}
	title := m.heading(head)
	// The PR is appended AFTER the heading is styled and stays last: it carries
	// its own link colour plus an OSC 8 hyperlink, and re-rendering a string that
	// contains one would print the URL instead of linking it.
	if it.PRNumber > 0 {
		title += m.heading("  ·  ") + m.prField("PR "+prCell(it.PRNumber), it.PRNumber, false)
	}

	var body []string
	note := ""
	if it.Notes != nil {
		note = *it.Notes
	}
	if strings.TrimSpace(note) == "" {
		body = []string{m.styles.faint.Render("(no notes) — e to add")}
	} else {
		// frame() clips to the available height, so the note body is only offset
		// by the scroll position here; no explicit height windowing is needed.
		lines := wrapLines(note, m.effWidth())
		off := m.noteOff
		if off > maxInt(0, len(lines)-1) {
			off = maxInt(0, len(lines)-1)
		}
		if off < len(lines) {
			lines = lines[off:]
		} else {
			lines = nil
		}
		body = lines
	}
	return m.frame(title, body, hintsFor(screenNote))
}

// wrapLines splits text on newlines, then soft-wraps each line to width.
func wrapLines(text string, width int) []string {
	if width <= 0 {
		width = 80
	}
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		if lipgloss.Width(ln) <= width {
			out = append(out, ln)
			continue
		}
		wrapped := ansi.Wrap(ln, width, "")
		out = append(out, strings.Split(wrapped, "\n")...)
	}
	return out
}

// noteMaxOff is the largest valid scroll offset for the currently selected
// item's note, so the last screenful still fills the body. The reducer clamps
// m.noteOff to this (instead of a sentinel), keeping scroll-up responsive after
// G / over-scrolling. 0 when there's no note or it fits on one screen.
func (m *Model) noteMaxOff() int {
	it := m.selectedItem()
	if it == nil || it.Notes == nil {
		return 0
	}
	note := *it.Notes
	if strings.TrimSpace(note) == "" {
		return 0
	}
	return maxInt(0, len(wrapLines(note, m.effWidth()))-m.bodyHeight(hintsFor(screenNote)))
}

// -- credentials / not-logged-in screens ------------------------------------

// viewCreds renders the two-field credentials prompt. Both values are shown in
// the clear: either is fixable only if you can see what you typed, and neither
// is written to disk, logged, or set as a flag default.
func (m *Model) viewCreds() string {
	body := []string{
		"",
		m.styles.bold.Render("Enter an agent secret or a project key"),
		"",
		m.credRow("agent secret", &m.secretInput, credSecret),
		m.credRow("project key", &m.keyInput, credProjectKey),
	}
	if m.credBusy {
		body = append(body, "", m.styles.faint.Render("validating…"))
	} else if m.credErr != "" {
		body = append(body, "", m.styles.danger.Render(m.credErr))
	}
	return m.frame(m.heading("sprawl · credentials"), body, "tab switch field · enter submit · esc quit")
}

// credRow renders one field of the credentials prompt; the focused one carries
// the cursor and the `›` marker.
func (m *Model) credRow(label string, in *textInput, field credField) string {
	focused := m.credFocus == field && !m.credBusy
	marker := "  "
	if focused {
		marker = m.styles.accent.Render("› ")
	}
	value := in.String()
	if focused {
		value = in.render() // cursor only on the focused field
	}
	return marker + m.styles.accent.Render(padRight(label+":", 13)+" ") + value
}

func (m *Model) viewNotLoggedIn() string {
	body := []string{
		"",
		m.styles.bold.Render("Not logged in."),
		"",
		"Run " + m.styles.accent.Render("sprawl login") + " to authenticate, then relaunch.",
	}
	return m.frame(m.heading("sprawl"), body, "q quit")
}

// -- overlays ---------------------------------------------------------------

func (m *Model) viewOverlay() string {
	switch m.overlay {
	case ovHelp:
		return m.frame(m.heading("Help"), helpLines(m.styles), "any key to close")

	case ovConfirm:
		kind := "task"
		if m.confirmKind == confirmDeleteItem {
			kind = "checklist item"
		}
		body := []string{
			"",
			fmt.Sprintf("Delete %s #%d?", kind, m.confirmID),
			m.styles.faint.Render("  " + m.confirmName),
			"",
			m.styles.danger.Render("y") + " confirm · " + m.styles.accent.Render("n") + " cancel",
		}
		return m.frame(m.heading("Confirm delete"), body, "y confirm · n cancel")

	case ovInput:
		body := []string{
			"",
			m.styles.bold.Render(m.inputPrompt),
			"",
			"  " + m.styles.accent.Render("› ") + m.input.render(),
		}
		return m.frame(m.heading("Input"), body, "enter submit · esc cancel")

	case ovPicker:
		body := []string{""}
		for i, it := range m.pickerItems {
			if i == m.pickerSel {
				body = append(body, m.styles.sel.Render("› "+it.label))
			} else {
				body = append(body, "  "+it.label)
			}
		}
		return m.frame(m.heading(m.pickerTitle), body, "↑↓ move · enter select · esc cancel")
	}
	return m.frame(m.heading(""), nil, "")
}

func helpLines(s styles) []string {
	rows := [][2]string{
		{"Global", "? help · r refresh · esc back · ctrl+c quit"},
		{"List", "↑↓/jk move · g/G top/bottom · enter open · / search · q quit"},
		{"", "c copy task · n new · e title · E description · t due · d delete"},
		{"Checklist", "↑↓/jk move · g/G top/bottom · space/x toggle · enter note"},
		{"", "c copy item · n add · e title · d delete · esc back"},
		{"", "s cycle state (ready → progress → review → none) · p set PR number"},
		{"", "o open the PR in a browser (copies the link if it can't)"},
		{"", "PR numbers are clickable in terminals that support hyperlinks"},
		{"", "setting a state un-completes the item — the two are exclusive"},
		{"Note", "e edit (in $EDITOR) · c copy item · o open PR · ↑↓ scroll · esc back"},
	}
	out := []string{""}
	for _, r := range rows {
		if r[0] == "" {
			out = append(out, "            "+r[1])
			continue
		}
		out = append(out, s.header.Render(padRight(r[0], 10))+"  "+r[1])
	}
	return out
}

// -- shared render helpers --------------------------------------------------

func projectLabel(p *client.Project) string {
	if p == nil || strings.TrimSpace(p.Name) == "" {
		return "—"
	}
	return p.Name
}

// padRight pads s with spaces to width w (rune count). Never truncates.
func padRight(s string, w int) string {
	n := len([]rune(s))
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}
