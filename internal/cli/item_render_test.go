package cli

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/icons"
)

func ptr(s string) *string { return &s }

// colKey renders a cell to the bytes it would actually print — styled text plus
// its link target — because lipgloss.Style isn't comparable and, more to the
// point, what has to match across views is the OUTPUT, not the struct.
func colKey(c col) string {
	was := stylesEnabled
	stylesEnabled = true
	defer func() { stylesEnabled = was }()
	return sty.render(c.style, c.text) + "\x00" + c.link
}

func demoItem() *client.ChecklistItem {
	return &client.ChecklistItem{
		ID: 203, Title: "add the migration", Completed: true,
		State: client.StateInReview, PRNumber: 412, HasNotes: true,
		Notes: ptr("blocked on the backfill"),
	}
}

func demoProject() *client.Project {
	return &client.Project{
		ID: 42, Name: "sprawl_cli", Key: "sc",
		GithubURL: "https://github.com/ultrakorne/sprawl_cli",
	}
}

// -- the consolidation invariant --------------------------------------------

// TestItemRow_IdenticalCellsAcrossViews is THE test that keeps this
// consolidation from rotting. `task <id>`, `item <id>` and `queue` must produce
// byte-identical cells for the same item — the moment one view starts building
// its own row, this fails.
func TestItemRow_IdenticalCellsAcrossViews(t *testing.T) {
	it := demoItem()
	proj := demoProject()

	taskRow := itemRow(itemView{item: it, project: proj}, itemCols{checkbox: true, state: true})
	itemRowCells := itemRow(itemView{item: it, project: proj}, itemCols{checkbox: true, state: true})
	queueRow := itemRow(itemView{item: it, project: proj}, itemCols{})

	// task <id> and item <id> use the same column set, so their rows are equal
	// cell for cell.
	if len(taskRow) != len(itemRowCells) {
		t.Fatalf("task/item column counts differ: %d vs %d", len(taskRow), len(itemRowCells))
	}
	for i := range taskRow {
		if colKey(taskRow[i]) != colKey(itemRowCells[i]) {
			t.Errorf("cell %d differs between task <id> and item <id>: %q vs %q",
				i, colKey(taskRow[i]), colKey(itemRowCells[i]))
		}
	}

	// queue drops the checkbox and STATE, but the cells it DOES share must be
	// identical — same id text, same PR cell, same link,
	// same notes flag, same title.
	shared := func(header []string, row []col, name string) map[string]col {
		out := map[string]col{}
		for i, h := range header {
			switch h {
			case "ID", "PR", "NOTES", "TITLE":
				out[h] = row[i]
			}
		}
		if len(out) != 4 {
			t.Fatalf("%s: expected 4 shared columns, got %d", name, len(out))
		}
		return out
	}
	a := shared(itemTableHeader(itemCols{checkbox: true, state: true}), taskRow, "task")
	b := shared(itemTableHeader(itemCols{}), queueRow, "queue")
	for k, av := range a {
		if colKey(av) != colKey(b[k]) {
			t.Errorf("column %s differs between task <id> and queue: %q vs %q", k, colKey(av), colKey(b[k]))
		}
	}
}

func TestItemTableHeader_ColumnSets(t *testing.T) {
	for _, tc := range []struct {
		name string
		cols itemCols
		want []string
	}{
		{"task <id> / item <id>", itemCols{checkbox: true, state: true},
			[]string{"[x]", "ID", "STATE", "PR", "NOTES", "TITLE"}},
		{"queue", itemCols{},
			[]string{"ID", "PR", "NOTES", "TITLE"}},
	} {
		got := itemTableHeader(tc.cols)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s header = %v, want %v", tc.name, got, tc.want)
		}
	}
	// POS is gone from every view.
	for _, cols := range []itemCols{{checkbox: true, state: true}, {}} {
		for _, h := range itemTableHeader(cols) {
			if h == "POS" {
				t.Errorf("POS column resurfaced in %+v", cols)
			}
		}
	}
}

// The ID column is bare — the `#` belongs to PR numbers only, and carrying it on
// both is what made them easy to confuse.
func TestItemRow_IDIsBareAndPRIsHashed(t *testing.T) {
	row := itemRow(itemView{item: demoItem(), project: demoProject()}, itemCols{checkbox: true, state: true})
	header := itemTableHeader(itemCols{checkbox: true, state: true})
	cell := func(name string) string { return row[indexOf(header, name)].text }

	if got := cell("ID"); got != "203" {
		t.Errorf("ID cell = %q, want a bare %q", got, "203")
	}
	if got := cell("PR"); got != "#412" {
		t.Errorf("PR cell = %q, want %q", got, "#412")
	}
}

func TestItemRow_StateIsTheIconNotTheWord(t *testing.T) {
	t.Setenv("SPRAWL_ICONS", "plain")
	header := itemTableHeader(itemCols{checkbox: true, state: true})
	stateCell := func(it *client.ChecklistItem) string {
		return itemRow(itemView{item: it}, itemCols{checkbox: true, state: true})[indexOf(header, "STATE")].text
	}

	it := demoItem()
	it.State = client.StateInProgress
	if got := stateCell(it); got != icons.Plain.Progress {
		t.Errorf("state cell = %q, want the icon %q", got, icons.Plain.Progress)
	}
	if got := stateCell(it); got == "progress" || got == "in_progress" {
		t.Errorf("state cell spelled the word out: %q", got)
	}
	// No state renders a dash, not an empty cell — an empty one reads as a
	// rendering bug rather than as "this item has no state".
	it.State = ""
	if got := stateCell(it); got != "-" {
		t.Errorf("stateless cell = %q, want %q", got, "-")
	}
}

func TestItemRow_NotesFlagDrivenByHasNotes(t *testing.T) {
	header := itemTableHeader(itemCols{checkbox: true, state: true})
	notesCell := func(has bool) string {
		it := &client.ChecklistItem{ID: 1, HasNotes: has}
		return itemRow(itemView{item: it}, itemCols{checkbox: true, state: true})[indexOf(header, "NOTES")].text
	}
	if got := notesCell(true); got != "o" {
		t.Errorf("has_notes=true → %q, want %q", got, "o")
	}
	if got := notesCell(false); got != "-" {
		t.Errorf("has_notes=false → %q, want %q", got, "-")
	}
}

func TestItemRow_PRWithoutNumberIsDash(t *testing.T) {
	header := itemTableHeader(itemCols{checkbox: true, state: true})
	it := &client.ChecklistItem{ID: 1}
	c := itemRow(itemView{item: it, project: demoProject()}, itemCols{checkbox: true, state: true})[indexOf(header, "PR")]
	if c.text != "-" || c.link != "" {
		t.Errorf("PR cell for an item with no PR = %+v, want a plain dash", c)
	}
}

// -- OSC 8 hyperlinks -------------------------------------------------------

// The CLI counterpart of TestPRHyperlink in the TUI: a PR number is a clickable
// link when the whole chain resolves, plain text when any link in it is missing,
// and the escapes never enter the width maths.
func TestPRCol_Hyperlink(t *testing.T) {
	defer func() { stylesEnabled = false }()
	stylesEnabled = true

	linked := hyperlink(sty.render(prCol(412, demoProject()).style, "#412"), prCol(412, demoProject()).link)
	if !strings.Contains(linked, "https://github.com/ultrakorne/sprawl_cli/pull/412") {
		t.Fatalf("no link target in %q", linked)
	}
	if got := ansi.Strip(linked); got != "#412" {
		t.Errorf("visible text = %q — the URL leaked into the output", got)
	}
	if got := lipgloss.Width(linked); got != len("#412") {
		t.Errorf("width = %d, want %d — a non-zero-width escape shears every column after it", got, len("#412"))
	}

	// A project with no repo URL: the number still shows, but nothing claims to
	// be clickable. An underlined accent number that did nothing would be a
	// worse lie than no styling at all.
	noRepo := &client.Project{ID: 42, Name: "sprawl_cli"}
	if c := prCol(412, noRepo); c.link != "" {
		t.Errorf("unresolvable PR got a link: %+v", c)
	}
	// No project at all (a projectless task) is the same story, not an error.
	if c := prCol(412, nil); c.link != "" || c.text != "#412" {
		t.Errorf("projectless PR cell = %+v, want a plain #412", c)
	}
}

// Escapes must stay balanced inside a rendered row, or the link bleeds into
// every column after it.
func TestPRCol_EscapesBalancedInRow(t *testing.T) {
	defer func() { stylesEnabled = false }()
	stylesEnabled = true

	table := itemTable([]itemView{{item: demoItem(), project: demoProject()}},
		itemCols{checkbox: true, state: true}, false)
	if opens := strings.Count(table, "\x1b]8;;"); opens != 2 {
		t.Errorf("%d OSC 8 markers, want 2 (one open, one close):\n%q", opens, table)
	}
	// The visible table is exactly what it would have been unstyled.
	stylesEnabled = false
	plain := itemTable([]itemView{{item: demoItem(), project: demoProject()}},
		itemCols{checkbox: true, state: true}, false)
	stylesEnabled = true
	styled := itemTable([]itemView{{item: demoItem(), project: demoProject()}},
		itemCols{checkbox: true, state: true}, false)
	if got := stripANSI(styled); got != plain {
		t.Errorf("stripped styled table differs from plain:\nplain:  %q\nstyled: %q", plain, got)
	}
}

// Hyperlinks follow the same rule as colour: text output to a terminal only.
// A pipe or a redirect gets the characters and nothing else.
func TestPRCol_NoLinkWhenStylingOff(t *testing.T) {
	stylesEnabled = false
	table := itemTable([]itemView{{item: demoItem(), project: demoProject()}},
		itemCols{checkbox: true, state: true}, false)
	if strings.Contains(table, "\x1b]8;;") {
		t.Errorf("OSC 8 escape leaked into non-terminal output:\n%q", table)
	}
}

// -- state icons ------------------------------------------------------------

// The CLI counterpart of the TUI's TestStateIconsAreOneCell: the STATE column is
// one cell wide, so a two-cell glyph would shear every row carrying it.
func TestStateIconsAreOneCellInTheTable(t *testing.T) {
	for _, set := range []icons.Set{icons.Nerd, icons.Plain} {
		for _, glyph := range []string{set.Ready, set.Progress, set.Review, icons.Unknown} {
			if w := lipgloss.Width(glyph); w != icons.Width {
				t.Errorf("glyph %q width = %d, want %d", glyph, w, icons.Width)
			}
		}
	}
}

// The CLI honours the same SPRAWL_ICONS=plain opt-out as the TUI, so a terminal
// without a patched font isn't stuck rendering tofu in one surface and glyphs in
// the other.
func TestItemRow_HonoursIconOptOut(t *testing.T) {
	header := itemTableHeader(itemCols{checkbox: true, state: true})
	stateCell := func() string {
		it := &client.ChecklistItem{ID: 1, State: client.StateInReview}
		return itemRow(itemView{item: it}, itemCols{checkbox: true, state: true})[indexOf(header, "STATE")].text
	}
	t.Setenv("SPRAWL_ICONS", "plain")
	if got := stateCell(); got != icons.Plain.Review {
		t.Errorf("with the opt-out, state cell = %q, want %q", got, icons.Plain.Review)
	}
	t.Setenv("SPRAWL_ICONS", "")
	if got := stateCell(); got != icons.Nerd.Review {
		t.Errorf("by default, state cell = %q, want %q", got, icons.Nerd.Review)
	}
}

// -- notes ------------------------------------------------------------------

func TestItemNoteLines_LabelIsSingularAndBodyHangs(t *testing.T) {
	it := &client.ChecklistItem{ID: 1, Notes: ptr("blocked on the backfill\nretry after 2026-08-05")}
	got := itemNoteLines(it, 10)
	if len(got) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(got), got)
	}
	if !strings.HasPrefix(got[0], strings.Repeat(" ", 10)+"note: ") {
		t.Errorf("first line = %q, want it indented 10 and labelled %q", got[0], "note: ")
	}
	if strings.Contains(got[0], "notes:") {
		t.Errorf("label is singular — an item has at most one note: %q", got[0])
	}
	// Continuation lines align under the BODY, not under the label, so a
	// multi-line note reads as one block.
	wantIndent := 10 + len("note: ")
	if !strings.HasPrefix(got[1], strings.Repeat(" ", wantIndent)+"retry") {
		t.Errorf("continuation = %q, want it indented %d", got[1], wantIndent)
	}
}

// An item with no note gets NOTHING — no "(no notes)" placeholder. That string
// is gone from the CLI entirely.
func TestItemNoteLines_EmptyYieldsNothing(t *testing.T) {
	for name, it := range map[string]*client.ChecklistItem{
		"nil":        {ID: 1},
		"empty":      {ID: 1, Notes: ptr("")},
		"whitespace": {ID: 1, Notes: ptr("   \n  ")},
	} {
		if got := itemNoteLines(it, 4); got != nil {
			t.Errorf("%s notes → %q, want nothing at all", name, got)
		}
	}
	table := itemTable([]itemView{{item: &client.ChecklistItem{ID: 1, Title: "x"}}},
		itemCols{checkbox: true, state: true}, true)
	if strings.Contains(table, "no notes") {
		t.Errorf("the (no notes) placeholder is back:\n%s", table)
	}
}

func TestItemNoteLines_WrapsLongLinesKeepsAuthoredBreaks(t *testing.T) {
	defer func() { outputWidth = 0 }()
	outputWidth = 60

	long := strings.Repeat("word ", 30)
	it := &client.ChecklistItem{ID: 1, Notes: ptr(long + "\nsecond paragraph")}
	got := itemNoteLines(it, 10)
	if len(got) < 3 {
		t.Fatalf("a 150-column note at width 60 should wrap, got %d lines", len(got))
	}
	for i, ln := range got {
		if w := lipgloss.Width(ln); w > outputWidth {
			t.Errorf("line %d is %d columns wide, over the %d limit: %q", i, w, outputWidth, ln)
		}
	}
	// The authored break survives: the last line is the second paragraph, not a
	// fragment of the first.
	if !strings.HasSuffix(got[len(got)-1], "second paragraph") {
		t.Errorf("authored line break lost; last line = %q", got[len(got)-1])
	}
}

// A pipe reports no width (outputWidth 0), which must mean "don't wrap" rather
// than "wrap to nothing".
func TestItemNoteLines_UnknownWidthDoesNotWrap(t *testing.T) {
	outputWidth = 0
	long := strings.Repeat("word ", 40)
	got := itemNoteLines(&client.ChecklistItem{ID: 1, Notes: ptr(long)}, 10)
	if len(got) != 1 {
		t.Fatalf("unknown width should print verbatim, got %d lines", len(got))
	}
}

// Notes hang under the TITLE column, which is what makes them read as belonging
// to their row rather than to the table.
func TestItemTable_NotesAlignUnderID(t *testing.T) {
	views := []itemView{
		{item: &client.ChecklistItem{ID: 203, Title: "add the migration", HasNotes: true, Notes: ptr("the note")}},
		{item: &client.ChecklistItem{ID: 4, Title: "write the docs"}},
	}
	out := itemTable(views, itemCols{checkbox: true, state: true}, true)

	var rowLine, noteLine string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "add the migration") {
			rowLine = ln
		}
		if strings.Contains(ln, "note: the note") {
			noteLine = ln
		}
	}
	if rowLine == "" || noteLine == "" {
		t.Fatalf("missing row or note line in:\n%s", out)
	}
	idAt := lipgloss.Width(rowLine[:strings.Index(rowLine, "203")])
	titleAt := lipgloss.Width(rowLine[:strings.Index(rowLine, "add the migration")])
	noteAt := lipgloss.Width(noteLine[:strings.Index(noteLine, "note:")])

	if noteAt != idAt {
		t.Errorf("note starts at column %d, want the ID column %d:\n%s", noteAt, idAt, out)
	}
	// The point of moving it off TITLE: the body gets those columns back.
	if noteAt >= titleAt {
		t.Errorf("note at %d is no wider than aligning to TITLE at %d — the change bought nothing:\n%s",
			noteAt, titleAt, out)
	}
	// The second item has no note, so nothing follows its row.
	if strings.Contains(out, "write the docs\n ") {
		t.Errorf("an item without a note got a trailing line:\n%s", out)
	}
}

// The queue puts ID at column 0, where a note would sit flush against the
// margin and read as a new block rather than as part of the row above it.
func TestItemTable_NotesNeverFlushLeft(t *testing.T) {
	views := []itemView{{
		item: &client.ChecklistItem{ID: 7, Title: "add the migration", HasNotes: true, Notes: ptr("the note")},
	}}
	out := itemTable(views, itemCols{}, true)
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "note: the note") {
			if !strings.HasPrefix(ln, strings.Repeat(" ", noteMinIndent)) {
				t.Errorf("note line is flush against the margin: %q\n%s", ln, out)
			}
			return
		}
	}
	t.Fatalf("no note line in:\n%s", out)
}

// -- taskHeader -------------------------------------------------------------

func TestTaskHeader_TitleOnlyWhenNoDescription(t *testing.T) {
	got := taskHeader(&client.Task{ID: 119, Title: "Ship the CLI consolidation"})
	if got != "Ship the CLI consolidation" {
		t.Errorf("header = %q, want the title alone", got)
	}
	// The id is deliberately absent — you typed it to get here.
	if strings.Contains(got, "119") {
		t.Errorf("header repeats the id you just typed: %q", got)
	}
}

func TestTaskHeader_DescriptionOnFollowingLines(t *testing.T) {
	got := taskHeader(&client.Task{
		ID: 119, Title: "Ship it",
		Description: "outline the v2 API\nand land it behind a flag",
	})
	want := "Ship it\noutline the v2 API\nand land it behind a flag"
	if got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
}

func TestTaskHeader_WrapsLongDescription(t *testing.T) {
	defer func() { outputWidth = 0 }()
	outputWidth = 40
	got := taskHeader(&client.Task{ID: 1, Title: "T", Description: strings.Repeat("word ", 30)})
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("a 150-column description at width 40 should wrap, got %d lines", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > outputWidth {
			t.Errorf("line %d is %d wide, over %d: %q", i, w, outputWidth, ln)
		}
	}
}

// A whitespace-only description contributes nothing — the empty case falls out
// rather than being special-cased into a blank line.
func TestTaskHeader_BlankDescriptionAddsNoLines(t *testing.T) {
	got := taskHeader(&client.Task{ID: 1, Title: "T", Description: "   \n  "})
	if got != "T" {
		t.Errorf("header = %q, want %q", got, "T")
	}
}

// -- itemMap ----------------------------------------------------------------

func TestItemMap_StateIsShortForm(t *testing.T) {
	for wire, want := range map[string]any{
		client.StateReadyToPickup: "ready",
		client.StateInProgress:    "progress",
		client.StateInReview:      "review",
		"":                        nil,
	} {
		m := itemMap(&client.ChecklistItem{ID: 1, State: wire}, nil, false)
		if m["state"] != want {
			t.Errorf("state %q → %v, want %v", wire, m["state"], want)
		}
	}
	// A state this build doesn't know passes through verbatim rather than being
	// hidden or mangled.
	m := itemMap(&client.ChecklistItem{ID: 1, State: "blocked"}, nil, false)
	if m["state"] != "blocked" {
		t.Errorf("unknown state → %v, want it passed through", m["state"])
	}
}

func TestItemMap_PRURLResolvedAndNullable(t *testing.T) {
	it := &client.ChecklistItem{ID: 1, PRNumber: 412}
	if got := itemMap(it, demoProject(), false)["pr_url"]; got != "https://github.com/ultrakorne/sprawl_cli/pull/412" {
		t.Errorf("pr_url = %v", got)
	}
	// Each break in the chain is null, never an error, and the key is always
	// present so a consumer never has to distinguish absent from null.
	for name, m := range map[string]map[string]any{
		"no project": itemMap(it, nil, false),
		"no repo":    itemMap(it, &client.Project{ID: 1, Name: "p"}, false),
		"no PR":      itemMap(&client.ChecklistItem{ID: 1}, demoProject(), false),
	} {
		v, present := m["pr_url"]
		if !present || v != nil {
			t.Errorf("%s: pr_url = %v (present=%v), want a present null", name, v, present)
		}
	}
}

func TestItemMap_PositionIsGone(t *testing.T) {
	m := itemMap(&client.ChecklistItem{ID: 1, Position: 7}, nil, true)
	if _, present := m["position"]; present {
		t.Errorf("position resurfaced in the payload: %+v", m)
	}
}

func TestItemMap_NotesGatedHasNotesAlways(t *testing.T) {
	it := &client.ChecklistItem{ID: 1, HasNotes: true, Notes: ptr("the body")}

	off := itemMap(it, nil, false)
	if _, present := off["notes"]; present {
		t.Errorf("notes must be absent when they weren't fetched: %+v", off)
	}
	if off["has_notes"] != true {
		t.Errorf("has_notes must be present either way — it drives the NOTES column: %+v", off)
	}

	on := itemMap(it, nil, true)
	if on["notes"] != "the body" {
		t.Errorf("notes = %v", on["notes"])
	}
	// Empty collapses to a literal null, not "" and not an absent key.
	empty := itemMap(&client.ChecklistItem{ID: 1, Notes: ptr("")}, nil, true)
	if v, present := empty["notes"]; !present || v != nil {
		t.Errorf("empty notes = %v (present=%v), want a present null", v, present)
	}
}
