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
		case screenSecret:
			s = m.viewSecret()
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

// frame assembles a screen: a header (title + rule), a body region clipped to
// the available height, and a two-line footer (transient status + key hints).
// The output is exactly effHeight() lines tall for normal windows; on a very
// short window (< 5 rows) it is clamped to effHeight() so the frame never
// overflows the viewport and breaks the layout.
func (m *Model) frame(title string, body []string, hints string) string {
	w := m.effWidth()
	h := m.effHeight()
	if m.loading {
		title += "  " + m.styles.faint.Render("loading…")
	}

	lines := make([]string, 0, h)
	lines = append(lines, m.clip(m.styles.header.Render(title), w))
	lines = append(lines, m.clip(m.styles.accent.Render(strings.Repeat("─", w)), w))

	bodyH := h - 4
	if bodyH < 1 {
		bodyH = 1
	}
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
	lines = append(lines, m.clip(m.styles.faint.Render(hints), w))

	// Never emit more rows than the viewport has: header + rule + body + status +
	// hints is at least 5 lines, which would overflow a window shorter than that.
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// windowStart centers a scrolling window on sel within a list of n rows.
func windowStart(sel, height, n int) int {
	if height <= 0 || n <= height {
		return 0
	}
	start := sel - height/2
	if start < 0 {
		start = 0
	}
	if start > n-height {
		start = n - height
	}
	return start
}

// -- list screen ------------------------------------------------------------

func (m *Model) viewList() string {
	title := "sprawl · tasks"
	if m.searching {
		title = "search: " + m.searchInput.render(false)
	} else if m.searchLabel != "" {
		title = fmt.Sprintf("sprawl · search %q (%d)", m.searchLabel, len(m.tasks))
	}

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
		body = m.listRows(vis)
	}

	hints := "↑↓ move · enter open · / search · c copy · n new · e title · E desc · t due · d delete · ? help · q quit"
	return m.frame(title, body, hints)
}

func (m *Model) listRows(vis []*client.Task) []string {
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

	bodyH := m.effHeight() - 4
	if bodyH < 1 {
		bodyH = 1
	}
	start := windowStart(m.listSel, bodyH, len(vis))
	end := start + bodyH
	if end > len(vis) {
		end = len(vis)
	}

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		t := vis[i]
		id := padRight("#"+itoa(t.ID), idW)
		prog := fmt.Sprintf("%d/%d", t.ChecklistProgress.Done, t.ChecklistProgress.Total)
		project := projectLabel(t.Project)
		due := t.DueDate
		if strings.TrimSpace(due) == "" {
			due = "—"
		}

		selected := i == m.listSel
		var line string
		if selected {
			// Selected: cursor + whole row bold+cyan (spec's selection style).
			plain := fmt.Sprintf("› %s  %s  %s  %s  %s", id, padRight(prog, progW), padRight(due, 10), project, t.Title)
			line = m.styles.sel.Render(plain)
		} else {
			progStyled := m.styles.progress(t.ChecklistProgress.Done, t.ChecklistProgress.Total).Render(padRight(prog, progW))
			line = fmt.Sprintf("  %s  %s  %s  %s  %s",
				m.styles.faint.Render(id), progStyled, m.styles.faint.Render(padRight(due, 10)), project, t.Title)
		}
		// Search-result annotation: surface matched checklist-item titles.
		if m.searchLabel != "" && len(t.MatchedChecklistItems) > 0 {
			var names []string
			for _, it := range t.MatchedChecklistItems {
				names = append(names, it.Title)
			}
			line += "  " + m.styles.faint.Render("↳ "+strings.Join(names, ", "))
		}
		rows = append(rows, line)
	}
	return rows
}

// -- checklist screen -------------------------------------------------------

func (m *Model) viewChecklist() string {
	if m.detail == nil {
		return m.frame("sprawl · task", []string{m.styles.faint.Render("loading…")}, "esc back")
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

	var body []string
	if len(t.ChecklistItems) == 0 {
		body = []string{m.styles.faint.Render("(no checklist items) — a to add")}
	} else {
		body = m.checklistRows(t.ChecklistItems)
	}
	hints := "↑↓ move · space/x toggle · enter note · c copy · a add · e title · d delete · esc back · ? help"
	return m.frame(title, body, hints)
}

func (m *Model) checklistRows(items []*client.ChecklistItem) []string {
	idW := 2
	for _, it := range items {
		if l := len("#" + itoa(it.ID)); l > idW {
			idW = l
		}
	}
	bodyH := m.effHeight() - 4
	if bodyH < 1 {
		bodyH = 1
	}
	start := windowStart(m.itemSel, bodyH, len(items))
	end := start + bodyH
	if end > len(items) {
		end = len(items)
	}

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		it := items[i]
		box := mdCheckbox(it.Completed)
		id := padRight("#"+itoa(it.ID), idW)
		notes := ""
		if it.HasNotes {
			notes = " 🗒"
		}
		selected := i == m.itemSel
		if selected {
			plain := fmt.Sprintf("› %s %s %s%s", box, id, it.Title, notes)
			rows = append(rows, m.styles.sel.Render(plain))
		} else {
			line := fmt.Sprintf("  %s %s %s%s",
				m.styles.checkbox(it.Completed).Render(box), m.styles.faint.Render(id), it.Title,
				m.styles.faint.Render(notes))
			rows = append(rows, line)
		}
	}
	return rows
}

// -- note screen ------------------------------------------------------------

func (m *Model) viewNote() string {
	it := m.selectedItem()
	if it == nil {
		return m.frame("sprawl · note", []string{m.styles.faint.Render("(item gone)")}, "esc back")
	}
	title := fmt.Sprintf("note · item #%d %s", it.ID, it.Title)

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
	hints := "e edit · c copy · ↑↓ scroll · esc back"
	return m.frame(title, body, hints)
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

// -- secret / not-logged-in screens -----------------------------------------

func (m *Model) viewSecret() string {
	body := []string{
		"",
		m.styles.bold.Render("Enter your agent secret"),
		m.styles.faint.Render("(input is masked and kept in memory only — never written to disk)"),
		"",
		"  " + m.styles.accent.Render("secret: ") + m.secretInput.render(true),
	}
	if m.secretBusy {
		body = append(body, "", m.styles.faint.Render("validating…"))
	} else if m.secretErr != "" {
		body = append(body, "", m.styles.danger.Render(m.secretErr))
	}
	return m.frame("sprawl · agent secret", body, "enter submit · esc quit")
}

func (m *Model) viewNotLoggedIn() string {
	body := []string{
		"",
		m.styles.bold.Render("Not logged in."),
		"",
		"Run " + m.styles.accent.Render("sprawl login") + " to authenticate, then relaunch.",
	}
	return m.frame("sprawl", body, "q quit")
}

// -- overlays ---------------------------------------------------------------

func (m *Model) viewOverlay() string {
	switch m.overlay {
	case ovHelp:
		return m.frame("Help", helpLines(m.styles), "any key to close")

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
		return m.frame("Confirm delete", body, "y confirm · n cancel")

	case ovInput:
		body := []string{
			"",
			m.styles.bold.Render(m.inputPrompt),
			"",
			"  " + m.styles.accent.Render("› ") + m.input.render(false),
		}
		return m.frame("Input", body, "enter submit · esc cancel")

	case ovPicker:
		body := []string{""}
		for i, it := range m.pickerItems {
			if i == m.pickerSel {
				body = append(body, m.styles.sel.Render("› "+it.label))
			} else {
				body = append(body, "  "+it.label)
			}
		}
		return m.frame(m.pickerTitle, body, "↑↓ move · enter select · esc cancel")
	}
	return m.frame("", nil, "")
}

func helpLines(s styles) []string {
	rows := [][2]string{
		{"Global", "? help · r refresh · esc back · ctrl+c quit"},
		{"List", "↑↓/jk move · g/G top/bottom · enter open · / search · q quit"},
		{"", "c copy task · n new · e title · E description · t due · d delete"},
		{"Checklist", "↑↓/jk move · g/G top/bottom · space/x toggle · enter note"},
		{"", "c copy item · a add · e title · d delete · esc back"},
		{"Note", "e edit (in $EDITOR) · c copy item · ↑↓ scroll · esc back"},
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
