package cli

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/icons"
)

// This file is the SINGLE source of checklist-item rendering. `task <id>`,
// `item <id>` and `queue` all build their tables here and none of them builds a
// row of its own — that is the whole point of the consolidation, and
// TestItemRow_IdenticalCellsAcrossViews is what keeps it from rotting.
//
// Machine output goes through itemMap for the same reason.

// itemCols selects which columns a view shows. The column ORDER is fixed by
// itemTableHeader; this only turns columns on and off.
//
//	task <id>  → {checkbox: true, state: true}
//	item <id>  → {checkbox: true, state: true}
//	queue      → {}
//
// queue drops the checkbox and the state because both are constant within one
// queue — every queued item is incomplete by construction, and the state is the
// query, so it lives in the heading rather than repeating identically down a
// column. It has no TASK or PROJECT column either: its rows are grouped under
// their task, whose header names both.
type itemCols struct{ checkbox, state bool }

// itemView pairs an item with the context a row needs beyond the item itself:
// the project whose repo URL turns a PR number into a link. It may be nil — a
// task with no project — and that is not an error.
type itemView struct {
	item    *client.ChecklistItem
	project *client.Project
}

// noteLabel prefixes an expanded note under its row. Singular: an item has at
// most one note.
const noteLabel = "note: "

// noteMinWrap is the narrowest column count worth re-flowing to. Below it (a
// very narrow terminal) authored lines print verbatim rather than being chopped
// into unreadable fragments.
const noteMinWrap = 20

// noteMinIndent keeps an expanded note off the left margin even when the column
// it aligns to starts there (ID is column 0 on the queue). A note flush against
// the margin reads as a new block rather than as part of the row above it.
const noteMinIndent = 2

func itemTableHeader(c itemCols) []string {
	h := make([]string, 0, 8)
	if c.checkbox {
		h = append(h, "[x]")
	}
	// Bare ID — no `#` prefix. The `#` belongs to PR numbers only; carrying it on
	// both is what made the two ids easy to confuse.
	h = append(h, "ID")
	if c.state {
		h = append(h, "STATE")
	}
	h = append(h, "PR", "NOTES", "TITLE")
	return h
}

// itemRow builds one row matching itemTableHeader(c).
func itemRow(v itemView, c itemCols) []col {
	it := v.item
	row := make([]col, 0, 8)
	if c.checkbox {
		row = append(row, styledCol(checkbox(it.Completed), sty.checkboxStyle(it.Completed)))
	}
	row = append(row, plainCol(fmt.Sprintf("%d", it.ID)))
	if c.state {
		// The icon, not the word — the same glyph the TUI shows, so the two
		// surfaces read identically. See internal/icons.
		row = append(row, styledCol(fallback(icons.For(it.State), "-"), stateStyle(it.State)))
	}
	row = append(row, prCol(it.PRNumber, v.project))
	row = append(row, plainCol(notesFlag(it.HasNotes)))
	row = append(row, plainCol(it.Title))
	return row
}

// prCol is the PR column: `#412` hyperlinked (and link-styled) when the chain
// resolves — item has a number, task has a project, project has a repo URL —
// and a plain `#412` when it doesn't. `-` when the item has no PR at all.
// Neither break in the chain is an error; the number is still shown.
func prCol(prNumber int64, project *client.Project) col {
	if prNumber <= 0 {
		return plainCol("-")
	}
	text := fmt.Sprintf("#%d", prNumber)
	url := client.PRURL(project, prNumber)
	if url == "" {
		return plainCol(text)
	}
	return linkedCol(text, sty.link, url)
}

// itemTable renders the header, the rule and one row per item. When withNotes is
// set each item's note is expanded on the lines below its row, indented to start
// under the ID column so the body gets the width it needs. Items without a note
// contribute nothing extra.
func itemTable(views []itemView, c itemCols, withNotes bool) string {
	header := itemTableHeader(c)
	rows := make([][]col, len(views))
	for i, v := range views {
		rows[i] = itemRow(v, c)
	}
	table := renderTable(header, rows)
	if !withNotes {
		return table
	}

	// Notes hang under the ID column, not under TITLE. Aligning them with the
	// title would look tidier but squeezes the body into whatever width is left
	// after every other column — on a queue row that can be half the terminal.
	// Starting at ID buys those columns back for the text that actually needs
	// them, and the row above is still what the note reads as belonging to.
	//
	// Never flush against the margin, though: ID is column 0 on the queue, and a
	// note starting there reads as a new block rather than as part of the row.
	noteAt := max(colOffset(colWidths(header, rows), indexOf(header, "ID")), noteMinIndent)

	// renderTable emits its header lines first, then exactly one line per row, so
	// the data rows are the last len(rows) lines however many header lines there
	// are — which is what lets the notes be woven in between them.
	lines := strings.Split(table, "\n")
	head := len(lines) - len(views)

	var b strings.Builder
	b.WriteString(strings.Join(lines[:head], "\n"))
	for i, v := range views {
		b.WriteByte('\n')
		b.WriteString(lines[head+i])
		for _, nl := range itemNoteLines(v.item, noteAt) {
			b.WriteByte('\n')
			b.WriteString(nl)
		}
	}
	return b.String()
}

// itemNoteLines is an item's note expanded under its row: a faint `note:` label
// then the body, every line indented to `indent` display columns. Continuation
// lines align under the body rather than under the label, so a multi-line note
// reads as one block. Returns nil when the item has no note — an item without
// one gets nothing at all, never a placeholder line.
//
// Authored line breaks survive; only over-long lines are re-flowed, and only
// when the terminal width is known (outputWidth is 0 on a pipe) and leaves
// enough room to be worth it.
func itemNoteLines(it *client.ChecklistItem, indent int) []string {
	notes := ""
	if it.Notes != nil {
		notes = *it.Notes
	}
	if strings.TrimSpace(notes) == "" {
		return nil
	}
	pad := strings.Repeat(" ", indent)
	bodyPad := pad + strings.Repeat(" ", len(noteLabel))

	wrapped := wrapLines(notes, outputWidth-indent-len(noteLabel))
	out := make([]string, 0, len(wrapped))
	for i, ln := range wrapped {
		if i == 0 {
			out = append(out, pad+sty.render(sty.faint, noteLabel)+ln)
			continue
		}
		out = append(out, bodyPad+ln)
	}
	return out
}

// taskHeader is the block above the table on `task <id>`: the bold title, then
// the description on the following lines. `item <id>` and `queue` render no task
// header at all.
//
// The id is deliberately absent — you typed it to get here. Project, due date
// and progress are absent too; `task list` carries all three per task, and
// --format=json carries them here. An empty description contributes no lines, so
// the title-only case falls out rather than being special-cased.
func taskHeader(t *client.Task) string {
	lines := []string{sty.render(sty.bold, t.Title)}
	if strings.TrimSpace(t.Description) != "" {
		lines = append(lines, wrapLines(t.Description, outputWidth)...)
	}
	return strings.Join(lines, "\n")
}

// wrapLines splits s on its authored line breaks and re-flows each one to width,
// returning the physical lines to print. Authored breaks (paragraphs) always
// survive; only over-long lines are broken. A width below noteMinWrap — which
// includes the width <= 0 that means "not a terminal, size unknown" — disables
// re-flowing entirely.
//
// Wrapping runs on plain text, before any styling, so a styled render and a
// plain one break in exactly the same places (the stripANSI == plain invariant).
func wrapLines(s string, width int) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if width >= noteMinWrap {
			ln = ansi.Wrap(ln, width, "")
		}
		out = append(out, strings.Split(ln, "\n")...)
	}
	return out
}

// itemMap is the machine (json) shape of one item — the single builder
// behind `task <id>`, `item <id>` and `queue`.
//
// Three things differ from what the server sends, deliberately:
//   - `state` is the SHORT form (`ready` / `progress` / `review` / null), never
//     the server's `ready_to_pickup` etc. One vocabulary for reading and writing;
//     it is also what keeps an item's state from colliding with a task's status.
//   - `pr_url` is resolved here and emitted everywhere an item is. Null when the
//     chain can't resolve. Every consumer would otherwise concatenate it by hand
//     and the "render it unlinked" rule is easy to get wrong.
//   - `position` is absent. It drove only the POS column, which is gone; array
//     order already carries ordering, since the server returns items in position
//     order.
//
// withNotes gates the `notes` key: absent on the non-full reads, present (null
// when empty) when the caller has the bodies. `has_notes` is present either way
// — it is what drives the NOTES column.
func itemMap(it *client.ChecklistItem, project *client.Project, withNotes bool) map[string]any {
	m := map[string]any{
		"id":         it.ID,
		"title":      it.Title,
		"completed":  it.Completed,
		"state":      nilIfEmpty(stateLabel(it.State)),
		"pr_number":  nilIfZero(it.PRNumber),
		"pr_url":     nilIfEmpty(client.PRURL(project, it.PRNumber)),
		"has_notes":  it.HasNotes,
		"last_actor": actorMap(it.LastActor),
	}
	if withNotes {
		// Collapse empty ("" from a pre-rollout server, or null from a current
		// one) to a literal null, for a uniform "empty ⇒ null" contract.
		if it.Notes != nil && *it.Notes != "" {
			m["notes"] = *it.Notes
		} else {
			m["notes"] = nil
		}
	}
	return m
}

// itemTaskMap is the parent-task stub carried in `item <id>`
// payloads: id, title, project. It is the only thing tying an item id back to
// its task, and the project inside it is what resolved pr_url.
func itemTaskMap(t *client.ItemTask) map[string]any {
	return map[string]any{
		"id":      t.ID,
		"title":   t.Title,
		"project": projectMap(t.Project),
	}
}

func checkbox(done bool) string {
	if done {
		return "[x]"
	}
	return "[ ]"
}

// notesFlag drives the NOTES column: whether there is a note to go and read,
// which is the question the column answers with or without --full. A marker
// rather than a word — the header already says NOTES, so spelling "yes" down
// the column just widens it to no purpose.
func notesFlag(has bool) string {
	if has {
		return "o"
	}
	return "-"
}

// stateLabel is the short human word for a state — the same vocabulary
// `item state` accepts, so what you read is what you type. An unknown value (a
// state added server-side after this build) passes through verbatim rather than
// being hidden.
func stateLabel(state string) string {
	switch state {
	case client.StateReadyToPickup:
		return "ready"
	case client.StateInProgress:
		return "progress"
	case client.StateInReview:
		return "review"
	case "":
		return ""
	default:
		return state
	}
}

// stateStyle colors a state like the rest of the CLI's status vocabulary:
// ready is cyan (available), in-progress yellow (running), in review green
// (work is done, awaiting a human).
func stateStyle(state string) lipgloss.Style {
	switch state {
	case client.StateReadyToPickup:
		return sty.accent
	case client.StateInProgress:
		return sty.warn
	case client.StateInReview:
		return sty.ok
	default:
		return sty.plain
	}
}

// nilIfEmpty / nilIfZero map a zero value back to a literal JSON null, which is
// what the server sends for a cleared state / PR number. Keeping the key present
// (rather than omitting it) means a payload always answers "is there a state?"
// without the consumer having to distinguish absent from null.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return 0
}
