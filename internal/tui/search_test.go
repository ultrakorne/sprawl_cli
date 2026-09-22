package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func strp(s string) *string { return &s }

// -- matching ---------------------------------------------------------------

func TestFuzzyTitle(t *testing.T) {
	cases := []struct {
		query, title string
		ok           bool
		pos          []int
	}{
		{"fb", "Foo Bar", true, []int{0, 4}},
		{"FOO", "foo bar", true, []int{0, 1, 2}},              // case-insensitive
		{"bar foo", "Foo Bar", true, []int{0, 1, 2, 4, 5, 6}}, // terms in any order
		{"xyz", "Foo Bar", false, nil},
		{"rab", "Foo Bar", false, nil}, // a subsequence keeps its order
		// Prefers the contiguous occurrence over the first scattered one.
		{"api", "a pile of api work", true, []int{10, 11, 12}},
	}
	for _, c := range cases {
		_, pos, ok := fuzzyTitle(searchTerms(c.query), c.title)
		if ok != c.ok || (ok && !reflect.DeepEqual(pos, c.pos)) {
			t.Errorf("fuzzyTitle(%q, %q) = %v %v, want %v %v", c.query, c.title, pos, ok, c.pos, c.ok)
		}
	}
}

func TestFuzzyTitle_RanksTightWordStartsHigher(t *testing.T) {
	terms := searchTerms("mig")
	tight, _, _ := fuzzyTitle(terms, "add the migration")
	loose, _, _ := fuzzyTitle(terms, "make it go")
	if tight <= loose {
		t.Fatalf("contiguous word-start hit should outrank a scattered one: %d vs %d", tight, loose)
	}
}

// Long text needs every word, not a subsequence — otherwise any short query
// would match nearly every note.
func TestWordsMatch(t *testing.T) {
	note := "Deploy blocked on the Redis upgrade; see runbook."
	if !wordsMatch(searchTerms("redis RUNBOOK"), note) {
		t.Error("every word present should match")
	}
	if wordsMatch(searchTerms("redis postgres"), note) {
		t.Error("a missing word must not match")
	}
	if wordsMatch(searchTerms("rdsupg"), note) {
		t.Error("bodies are not matched as a subsequence")
	}
}

func TestFilterTasks_RanksTitleThenDescriptionThenItems(t *testing.T) {
	tasks := []*client.Task{
		{ID: 1, Title: "unrelated", Description: ""},
		{ID: 2, Title: "misc", Description: "mentions the cache here"},
		{ID: 3, Title: "chores"},
		{ID: 4, Title: "Cache warmup"},
	}
	full := map[int64]*client.Task{
		3: {ID: 3, ChecklistItems: []*client.ChecklistItem{
			{ID: 30, Title: "nothing"},
			{ID: 31, Title: "other", Notes: strp("flush the cache first")},
		}},
	}
	got := filterTasks(searchTerms("cache"), tasks, full)
	var ids []int64
	for _, tm := range got {
		ids = append(ids, tm.task.ID)
	}
	if !reflect.DeepEqual(ids, []int64{4, 2, 3}) {
		t.Fatalf("order = %v, want title, description, items", ids)
	}
	if !got[1].desc {
		t.Error("description hit not flagged")
	}
	if len(got[2].items) != 1 || got[2].items[0].item.ID != 31 || !got[2].items[0].note {
		t.Errorf("item hit via note not reported: %+v", got[2].items)
	}
	if all := filterTasks(nil, tasks, full); len(all) != len(tasks) {
		t.Errorf("no query should pass everything, got %d", len(all))
	}
}

// -- list flow --------------------------------------------------------------

func searchFixture() (*fakeClient, []*client.Task) {
	tasks := []*client.Task{
		{ID: 1, Title: "Apple pie"},
		{ID: 2, Title: "Banana bread", Description: "needs ripe fruit"},
		{ID: 3, Title: "Grape jam"},
	}
	fc := &fakeClient{getByID: map[string]*client.Task{
		"1": {ID: 1, Title: "Apple pie", ChecklistItems: []*client.ChecklistItem{}},
		"2": {ID: 2, Title: "Banana bread", ChecklistItems: []*client.ChecklistItem{}},
		"3": {ID: 3, Title: "Grape jam", ChecklistItems: []*client.ChecklistItem{
			{ID: 30, Title: "sterilise jars", HasNotes: true, Notes: strp("boil for ten minutes")},
		}},
	}}
	return fc, tasks
}

func TestSearch_ListFiltersFuzzyAcrossTitleDescriptionItemsAndNotes(t *testing.T) {
	shortTransients(t)
	fc, tasks := searchFixture()
	m := newListModel(fc, tasks)

	cmd := m.press("/")
	if !m.searching {
		t.Fatal("/ should open the query")
	}
	for _, r := range "apie" {
		m.press(string(r))
	}
	if vis := m.visibleTasks(); len(vis) != 1 || vis[0].ID != 1 {
		t.Fatalf("fuzzy title filter should keep Apple pie only, got %+v", vis)
	}
	m.press("ctrl+u")
	for _, r := range "ripe" {
		m.press(string(r))
	}
	if vis := m.visibleTasks(); len(vis) != 1 || vis[0].ID != 2 {
		t.Fatalf("description words should match Banana bread, got %+v", vis)
	}

	// Items and notes only become searchable once the index fill lands.
	m.press("ctrl+u")
	for _, r := range "boil" {
		m.press(string(r))
	}
	if vis := m.visibleTasks(); len(vis) != 0 {
		t.Fatalf("nothing indexed yet, got %+v", vis)
	}
	drain(m, cmd)
	if len(fc.calls) == 0 || m.indexing {
		t.Fatalf("index fill didn't run: calls=%v indexing=%v", fc.calls, m.indexing)
	}
	ms := m.listMatches()
	if len(ms) != 1 || ms[0].task.ID != 3 || len(ms[0].items) != 1 || !ms[0].items[0].note {
		t.Fatalf("note hit should surface Grape jam via item #30, got %+v", ms)
	}
	view := ansi.Strip(m.viewList())
	if !strings.Contains(view, "↳ sterilise jars 🗒") {
		t.Errorf("matched item not listed under its task:\n%s", view)
	}

	// enter keeps the filter; esc then clears it and the cursor stays put.
	m.press("enter")
	if m.searching || m.listFilter != "boil" {
		t.Fatalf("enter should keep the filter: searching=%v filter=%q", m.searching, m.listFilter)
	}
	if !strings.Contains(ansi.Strip(m.viewList()), "/boil (1 of 3)") {
		t.Errorf("kept filter not shown in the header")
	}
	m.press("esc")
	if m.listFilter != "" || len(m.visibleTasks()) != 3 {
		t.Fatalf("esc should clear the filter: %q", m.listFilter)
	}
	if sel := m.selectedTask(); sel == nil || sel.ID != 3 {
		t.Fatalf("cursor should stay on the found task, got %+v", sel)
	}
	if m.current() != screenList {
		t.Fatal("esc on a filter must clear it, not pop")
	}
}

// The index is fetched once and reused; refresh drops it so the next search
// sees the server's current checklists.
func TestSearch_IndexReusedUntilRefresh(t *testing.T) {
	shortTransients(t)
	fc, tasks := searchFixture()
	fc.listResp = tasks
	m := newListModel(fc, tasks)
	drain(m, m.press("/"))
	m.press("esc")
	n := len(fc.calls)
	if cmd := m.press("/"); cmd != nil {
		t.Fatal("a fully indexed list should not refetch")
	}
	m.press("esc")
	drain(m, m.press("r"))
	if m.index != nil {
		t.Fatal("refresh should drop the index")
	}
	drain(m, m.press("/"))
	if len(fc.calls) <= n+1 {
		t.Fatalf("expected a fresh fill after refresh, calls=%v", fc.calls[n:])
	}
}

// A task that fails to load is reported once and not re-requested on every
// keystroke; a fill overtaken by a reset is dropped.
func TestSearch_IndexFailuresAndStaleFills(t *testing.T) {
	shortTransients(t)
	fc, tasks := searchFixture()
	delete(fc.getByID, "2")
	m := newListModel(fc, tasks)
	drain(m, m.press("/"))
	if !m.statusErr || !strings.Contains(m.status, "1 task(s) not searchable") {
		t.Fatalf("failure not reported: %q", m.status)
	}
	if cmd := m.ensureIndex(); cmd != nil {
		t.Fatal("a failed task must not be re-requested before a reset")
	}

	gen := m.indexGen
	m.resetIndex()
	m.send(indexLoadedMsg{tasks: []*client.Task{{ID: 1, ChecklistItems: []*client.ChecklistItem{}}}, gen: gen, from: m.client})
	if m.index[1] != nil {
		t.Fatal("a fill from before the reset must be dropped")
	}
}

// -- checklist flow ---------------------------------------------------------

func TestSearch_ChecklistFiltersItemsAndActsOnFilteredRows(t *testing.T) {
	task := demoTask()
	task.ChecklistItems = append(task.ChecklistItems,
		&client.ChecklistItem{ID: 9, Title: "deploy", HasNotes: true, Notes: strp("run the migration twice")})
	m := newChecklistModel(&fakeClient{toggleResp: &client.ChecklistItem{ID: 8, Title: "write the tests", Completed: true}}, task)

	m.press("/")
	for _, r := range "wt" {
		m.press(string(r))
	}
	if vis := m.visibleItems(); len(vis) != 1 || vis[0].ID != 8 {
		t.Fatalf("fuzzy item filter should keep 'write the tests', got %+v", vis)
	}
	m.press("enter")
	if m.itemFilter != "wt" {
		t.Fatalf("filter not kept: %q", m.itemFilter)
	}
	// Actions target the filtered row, not the item at the same index unfiltered.
	m.press("x")
	if !task.ChecklistItems[1].Completed || task.ChecklistItems[0].Completed {
		t.Fatal("toggle hit the wrong item under a filter")
	}

	// A note hit keeps the item and marks the flag, not the title.
	m.press("/")
	m.press("ctrl+u")
	for _, r := range "migration" {
		m.press(string(r))
	}
	ms := m.itemMatches()
	if len(ms) != 2 || ms[0].item.ID != 7 || ms[1].item.ID != 9 || !ms[1].note || ms[1].title != nil {
		t.Fatalf("want title hit #7 and note hit #9, got %+v", ms)
	}
	m.press("down")
	m.press("esc")
	if m.itemFilter != "" || len(m.visibleItems()) != 3 {
		t.Fatal("esc while typing should drop the filter")
	}
	if it := m.selectedItem(); it == nil || it.ID != 9 {
		t.Fatalf("cursor should stay on the found item, got %+v", it)
	}
	if m.current() != screenChecklist {
		t.Fatal("esc in the query must not pop the checklist")
	}
}

// Opening another task starts its checklist unfiltered.
func TestSearch_ChecklistFilterResetsOnOpen(t *testing.T) {
	task := demoTask()
	m := newChecklistModel(&fakeClient{getResp: task}, task)
	m.press("/")
	m.press("z")
	m.press("enter")
	m.press("esc") // clears the filter
	m.press("/")
	m.press("z")
	m.press("enter")
	m.stack = []screen{screenList}
	m.open()
	if m.itemFilter != "" || m.itemSearching {
		t.Fatalf("filter leaked into the next task: %q", m.itemFilter)
	}
}

// -- rendering --------------------------------------------------------------

func TestSearch_HighlightsMatchesAndNoteFlag(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[1].HasNotes = true
	task.ChecklistItems[1].Notes = strp("remember the migration")
	m := newChecklistModel(&fakeClient{}, task)
	m.itemFilter = "migration"

	rows := m.checklistRows(m.itemMatches(), m.bodyHeight(hintsFor(screenChecklist)))
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, m.styles.match.Render("migration")) {
		t.Errorf("title hit not highlighted:\n%q", joined)
	}
	if !strings.Contains(joined, m.styles.hitBg.Render("🗒")) {
		t.Errorf("note hit should mark the flag:\n%q", joined)
	}
	plain := ansi.Strip(joined)
	if !strings.Contains(plain, "add the migration") || !strings.Contains(plain, "write the tests 🗒") {
		t.Errorf("highlighting changed the text:\n%s", plain)
	}
}

// Marks survive wrapping: the text of a wrapped, highlighted row is exactly
// the text of the same row unhighlighted.
func TestRowLinesMarked_WrapKeepsText(t *testing.T) {
	title := "one two three four five six seven eight nine ten eleven twelve"
	_, pos, _ := fuzzyTitle(searchTerms("twelve"), title)
	marked, marks := markedTitle(title, pos, true, true)
	m := &Model{styles: newStyles()}
	got := rowLinesMarked("> ", 2, 20, marked, marks, m.markRenderer(nil))
	want := rowLines("> ", 2, 20, marked, nil)
	if len(got) != len(want) {
		t.Fatalf("line count %d vs %d", len(got), len(want))
	}
	for i := range got {
		if ansi.Strip(got[i]) != want[i] {
			t.Errorf("line %d: %q vs %q", i, ansi.Strip(got[i]), want[i])
		}
	}
	if !strings.Contains(got[len(got)-1], m.styles.match.Render("twelve")) {
		t.Errorf("hit on the wrapped line not highlighted: %q", got[len(got)-1])
	}
}

// A note opened under a filter scrolls to its first hit.
func TestSearch_NoteOpensAtFirstHit(t *testing.T) {
	task := demoTask()
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "filler"
	}
	lines[40] = "the needle is here"
	task.ChecklistItems[0].HasNotes = true
	task.ChecklistItems[0].Notes = strp(strings.Join(lines, "\n"))
	m := newChecklistModel(&fakeClient{}, task)
	m.itemFilter = "needle"
	m.open()
	if m.current() != screenNote || m.noteOff != 39 {
		t.Fatalf("note should open just above the hit, off=%d", m.noteOff)
	}
	if !strings.Contains(m.viewNote(), m.styles.match.Render("needle")) {
		t.Error("hit not highlighted in the note")
	}
}

// -- review regressions -----------------------------------------------------

// Editing a note so its item leaves the checklist filter must not slide the
// note screen onto another item.
func TestSearch_NoteScreenPinnedToItem(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].HasNotes = true
	task.ChecklistItems[0].Notes = strp("foo")
	task.ChecklistItems[1].Title = "foo tests"
	m := newChecklistModel(&fakeClient{}, task)
	m.itemFilter = "foo"
	m.open()
	m.send(notesSetMsg{itemID: 7, notes: strp("bar")})
	if it := m.selectedItem(); it == nil || it.ID != 7 {
		t.Fatalf("note screen moved off #7: %+v", it)
	}
	m.press("esc")
	if m.current() != screenChecklist || m.selectedItem() == nil {
		t.Fatalf("checklist cursor invalid after the item left the filter")
	}
}

// A 2xx without a task is a failure: it must not be re-requested forever.
func TestSearch_EmptyTaskResponseIsNotRefetched(t *testing.T) {
	shortTransients(t)
	fc, tasks := searchFixture()
	fc.getByID["2"] = nil
	m := newListModel(fc, tasks)
	drain(m, m.press("/"))
	if !m.indexFailed[2] || m.indexing {
		t.Fatalf("empty task not marked failed: failed=%v indexing=%v", m.indexFailed, m.indexing)
	}
	if n := strings.Count(strings.Join(fc.calls, ","), "GetTask:2"); n != 1 {
		t.Fatalf("task #2 fetched %d times", n)
	}
}

// Back on the list, a fetched copy wins over the last-opened m.detail; and a
// fill never overwrites an entry that arrived after it started.
func TestSearch_IndexPrefersFresherCopies(t *testing.T) {
	shortTransients(t)
	fc, tasks := searchFixture()
	m := newListModel(fc, tasks)
	old := &client.Task{ID: 3, ChecklistItems: []*client.ChecklistItem{{ID: 30, Title: "old"}}}
	m.detail = old // left over from an earlier drill-in
	drain(m, m.press("/"))
	if m.index[3] == old {
		t.Fatal("stale m.detail replaced the fetched copy")
	}

	newer := &client.Task{ID: 1, ChecklistItems: []*client.ChecklistItem{{ID: 10, Title: "edited"}}}
	m.index[1] = newer
	m.send(indexLoadedMsg{tasks: []*client.Task{{ID: 1, ChecklistItems: []*client.ChecklistItem{}}}, gen: m.indexGen, from: m.client})
	if m.index[1] != newer {
		t.Fatal("a fill overwrote a newer entry")
	}
}

// Renaming the selected task out of a kept filter leaves a valid cursor.
func TestSearch_EditOutOfFilterKeepsValidCursor(t *testing.T) {
	shortTransients(t)
	tasks := []*client.Task{{ID: 1, Title: "apple one"}, {ID: 2, Title: "apple two"}}
	m := newListModel(&fakeClient{}, tasks)
	m.listFilter = "apple"
	m.listSel = 1
	m.send(taskMutatedMsg{task: &client.Task{ID: 2, Title: "pear"}})
	if m.selectedTask() == nil {
		t.Fatalf("no selected row: sel=%d visible=%d", m.listSel, len(m.visibleTasks()))
	}
}

// A revoked secret during the fill goes to the credentials prompt.
func TestSearch_IndexAuthFailureBouncesToCreds(t *testing.T) {
	shortTransients(t)
	fc, tasks := searchFixture()
	fc.getByID = map[string]*client.Task{}
	m := newListModel(fc, tasks)
	fc.getByID = nil
	fc.getErr = &client.APIError{Status: 401, Code: "unauthorized"}
	drain(m, m.press("/"))
	if m.current() != screenCreds {
		t.Fatalf("want the credentials prompt, got %v (status %q)", m.current(), m.status)
	}
}

// Item lines appear only under tasks found through their items, and those
// rank by how well the item matched.
func TestSearch_ItemLinesOnlyForItemOnlyHits(t *testing.T) {
	tasks := []*client.Task{{ID: 1, Title: "jam"}, {ID: 2, Title: "misc"}, {ID: 3, Title: "other"}}
	full := map[int64]*client.Task{
		1: {ID: 1, ChecklistItems: []*client.ChecklistItem{{ID: 10, Title: "jars"}}},
		2: {ID: 2, ChecklistItems: []*client.ChecklistItem{{ID: 20, Title: "j a m"}}},
		3: {ID: 3, ChecklistItems: []*client.ChecklistItem{{ID: 30, Title: "jam jars"}}},
	}
	got := filterTasks(searchTerms("jam"), tasks, full)
	var ids []int64
	for _, tm := range got {
		ids = append(ids, tm.task.ID)
	}
	if !reflect.DeepEqual(ids, []int64{1, 3, 2}) {
		t.Fatalf("order = %v, want title hit, then the tighter item hit", ids)
	}
	m := &Model{styles: newStyles(), width: 100, height: 30}
	rows := ansi.Strip(strings.Join(m.listRows(got, 20), "\n"))
	if strings.Contains(rows, "↳ jars") || !strings.Contains(rows, "↳ jam jars") {
		t.Fatalf("item lines should show for item-only hits only:\n%s", rows)
	}
}

// `/` before the task has loaded does nothing; moving the text cursor in the
// query leaves the selection alone.
func TestSearch_QueryEdgeKeys(t *testing.T) {
	task := demoTask()
	m := newChecklistModel(&fakeClient{}, task)
	m.detail = nil
	m.press("/")
	if m.itemSearching {
		t.Fatal("/ opened a query on a checklist still loading")
	}
	m.detail = task
	m.press("/")
	m.press("t")
	m.press("down")
	m.press("left")
	if m.itemSel != 1 {
		t.Fatalf("cursor-only key reset the selection: %d", m.itemSel)
	}
}
