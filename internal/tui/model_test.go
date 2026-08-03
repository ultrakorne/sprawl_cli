package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// -- fake client ------------------------------------------------------------

type fakeClient struct {
	listResp   []*client.Task
	listErr    error
	getResp    *client.Task
	getErr     error
	searchResp []*client.Task
	searchErr  error
	toggleResp *client.ChecklistItem
	toggleErr  error
	createResp *client.Task
	createErr  error
	updateResp *client.Task
	updateErr  error
	dueResp    *client.Task
	dueErr     error
	deleteErr  error
	itemResp   *client.ChecklistItem
	itemErr    error
	notesResp  *string
	notesErr   error
	stateResp  *client.ChecklistItem
	stateErr   error

	createAttrs     map[string]any
	updateTaskAttrs map[string]any
	updateItemAttrs map[string]any
	createItemAttrs map[string]any
	stateAttrs      map[string]any
	dueArg          *string
	dueArgSet       bool
	notesArg        string
	calls           []string
}

func (f *fakeClient) SetChecklistItemState(_ context.Context, id string, attrs map[string]any) (*client.ChecklistItem, error) {
	f.calls = append(f.calls, "SetState:"+id)
	f.stateAttrs = attrs
	return f.stateResp, f.stateErr
}

func (f *fakeClient) ListTasks(context.Context) ([]*client.Task, error) {
	f.calls = append(f.calls, "ListTasks")
	return f.listResp, f.listErr
}
func (f *fakeClient) GetTask(_ context.Context, id string, _ bool) (*client.Task, error) {
	f.calls = append(f.calls, "GetTask:"+id)
	return f.getResp, f.getErr
}
func (f *fakeClient) SearchTasks(_ context.Context, q string) ([]*client.Task, error) {
	f.calls = append(f.calls, "SearchTasks:"+q)
	return f.searchResp, f.searchErr
}
func (f *fakeClient) SetChecklistItemCompleted(_ context.Context, id string, _ bool) (*client.ChecklistItem, error) {
	f.calls = append(f.calls, "SetCompleted:"+id)
	return f.toggleResp, f.toggleErr
}
func (f *fakeClient) CreateTask(_ context.Context, attrs map[string]any) (*client.Task, error) {
	f.calls = append(f.calls, "CreateTask")
	f.createAttrs = attrs
	return f.createResp, f.createErr
}
func (f *fakeClient) UpdateTask(_ context.Context, id string, attrs map[string]any) (*client.Task, error) {
	f.calls = append(f.calls, "UpdateTask:"+id)
	f.updateTaskAttrs = attrs
	return f.updateResp, f.updateErr
}
func (f *fakeClient) SetTaskDueDate(_ context.Context, id string, due *string) (*client.Task, error) {
	f.calls = append(f.calls, "SetDue:"+id)
	f.dueArg = due
	f.dueArgSet = true
	return f.dueResp, f.dueErr
}
func (f *fakeClient) DeleteTask(_ context.Context, id string) error {
	f.calls = append(f.calls, "DeleteTask:"+id)
	return f.deleteErr
}
func (f *fakeClient) CreateChecklistItem(_ context.Context, taskID string, attrs map[string]any) (*client.ChecklistItem, error) {
	f.calls = append(f.calls, "CreateItem:"+taskID)
	f.createItemAttrs = attrs
	return f.itemResp, f.itemErr
}
func (f *fakeClient) UpdateChecklistItem(_ context.Context, id string, attrs map[string]any) (*client.ChecklistItem, error) {
	f.calls = append(f.calls, "UpdateItem:"+id)
	f.updateItemAttrs = attrs
	return f.itemResp, f.itemErr
}
func (f *fakeClient) SetNotes(_ context.Context, id, notes string) (*string, error) {
	f.calls = append(f.calls, "SetNotes:"+id)
	f.notesArg = notes
	return f.notesResp, f.notesErr
}
func (f *fakeClient) DeleteChecklistItem(_ context.Context, id string) error {
	f.calls = append(f.calls, "DeleteItem:"+id)
	return f.deleteErr
}

// -- test helpers -----------------------------------------------------------

// kp builds a KeyPressMsg whose String() matches s, for driving Update.
func kp(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	if len(r) == 1 {
		return tea.KeyPressMsg{Code: r[0], Text: s}
	}
	return tea.KeyPressMsg{Text: s}
}

func (m *Model) send(msg tea.Msg) tea.Cmd {
	_, cmd := m.Update(msg)
	return cmd
}

func (m *Model) press(s string) tea.Cmd { return m.send(kp(s)) }

// runCmd executes a Cmd (recursively flattening batches) into its messages.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// clipboardPayload flattens cmd and returns the payload of the first
// tea.SetClipboard command, ignoring the transient-status tea.Tick that rides in
// the same batch (its Cmd blocks for ~2.5s). setClipboardMsg is an unexported
// string type in bubbletea, so it is detected by its reflect kind (string)
// rather than a type assertion — clearStatusMsg (a struct) is skipped.
func clipboardPayload(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a clipboard command, got nil")
	}
	msgs := make(chan tea.Msg, 8)
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		m := c()
		if b, ok := m.(tea.BatchMsg); ok {
			for _, sub := range b {
				go run(sub) // concurrent so a blocking Tick can't stall the probe
			}
			return
		}
		msgs <- m
	}
	go run(cmd)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case m := <-msgs:
			if v := reflect.ValueOf(m); v.Kind() == reflect.String {
				return v.String()
			}
		case <-deadline:
			t.Fatal("no clipboard payload observed")
			return ""
		}
	}
}

func newListModel(fc *fakeClient, tasks []*client.Task) *Model {
	m := newModel(context.Background(), Deps{
		LoggedIn: true, Secret: "sek", SecretProvided: true,
		NewClient: func(string, string) Client { return fc },
	})
	m.width, m.height = 100, 30
	m.Update(tasksLoadedMsg{tasks: tasks, validated: true})
	return m
}

// -- pure helpers -----------------------------------------------------------

func TestBackStack_PushPopCurrent(t *testing.T) {
	m := &Model{stack: []screen{screenList}}
	if m.current() != screenList {
		t.Fatal("current should be list")
	}
	m.push(screenChecklist)
	m.push(screenNote)
	if m.current() != screenNote {
		t.Fatal("current should be note")
	}
	m.pop()
	if m.current() != screenChecklist {
		t.Fatal("pop should return to checklist")
	}
	m.popTo(screenList)
	if m.current() != screenList {
		t.Fatal("popTo should return to list")
	}
	// popping the root is a no-op (never empties the stack)
	m.pop()
	if len(m.stack) != 1 || m.current() != screenList {
		t.Fatalf("root pop must not empty the stack: %v", m.stack)
	}
}

func TestFilterTasksByTitle(t *testing.T) {
	tasks := []*client.Task{
		{ID: 1, Title: "Apple pie"},
		{ID: 2, Title: "Banana bread"},
		{ID: 3, Title: "Grape jam"},
	}
	if got := filterTasksByTitle(tasks, ""); len(got) != 3 {
		t.Fatalf("empty query should pass all through, got %d", len(got))
	}
	got := filterTasksByTitle(tasks, "AN") // case-insensitive
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("query AN should match Banana only, got %+v", got)
	}
	if got := filterTasksByTitle(tasks, "zzz"); len(got) != 0 {
		t.Fatalf("no match expected, got %d", len(got))
	}
}

func TestRecomputeProgress(t *testing.T) {
	items := []*client.ChecklistItem{
		{Completed: true}, {Completed: false}, {Completed: true},
	}
	p := recomputeProgress(items)
	if p.Done != 2 || p.Total != 3 {
		t.Fatalf("progress = %d/%d, want 2/3", p.Done, p.Total)
	}
}

// -- credentials prompt state machine ---------------------------------------

func TestCredsPrompt_StateMachine(t *testing.T) {
	fc := &fakeClient{}
	m := newModel(context.Background(), Deps{
		LoggedIn: true, SecretProvided: false,
		NewClient: func(string, string) Client { return fc },
	})
	m.width, m.height = 80, 24
	if m.current() != screenCreds {
		t.Fatalf("start screen = %d, want credentials", m.current())
	}

	// enter with both fields empty → error, stays on the prompt
	m.press("enter")
	if m.credErr == "" || m.current() != screenCreds {
		t.Fatalf("empty submit should error and stay: err=%q screen=%d", m.credErr, m.current())
	}

	// type a secret; it is echoed in the clear so a typo can be spotted and
	// fixed (it still never leaves memory)
	for _, k := range []string{"h", "u", "n", "t", "e", "r"} {
		m.press(k)
	}
	if got := m.secretInput.String(); got != "hunter" {
		t.Fatalf("secret buffer = %q, want hunter", got)
	}
	if !strings.Contains(ansi.Strip(m.viewCreds()), "hunter") {
		t.Fatalf("prompt should echo the typed secret:\n%s", m.viewCreds())
	}

	// submit → busy + client built
	m.press("enter")
	if !m.credBusy {
		t.Fatal("submit should set credBusy")
	}
	if m.client == nil {
		t.Fatal("submit should build the client from the secret")
	}

	// server rejects → back to prompt with inline error, buffer cleared
	m.send(authFailedMsg{err: &client.APIError{Status: 403}})
	if m.current() != screenCreds || m.credErr == "" {
		t.Fatalf("rejection should return to prompt with error: screen=%d err=%q", m.current(), m.credErr)
	}
	if m.credBusy || m.secretInput.String() != "" {
		t.Fatal("rejection should clear busy + buffer")
	}

	// retry and succeed
	for _, k := range []string{"o", "k"} {
		m.press(k)
	}
	m.press("enter")
	m.send(tasksLoadedMsg{tasks: []*client.Task{{ID: 1, Title: "T"}}, validated: true})
	if m.current() != screenList || !m.validated {
		t.Fatalf("successful validation should land on list: screen=%d validated=%v", m.current(), m.validated)
	}
}

// -- optimistic toggle ------------------------------------------------------

func checklistFixture() (*fakeClient, *Model, *client.Task) {
	detail := &client.Task{
		ID: 1, Title: "T",
		ChecklistProgress: client.ChecklistProgress{Done: 0, Total: 2},
		ChecklistItems: []*client.ChecklistItem{
			{ID: 10, Title: "a", Completed: false},
			{ID: 11, Title: "b", Completed: false},
		},
	}
	listTask := &client.Task{ID: 1, Title: "T", ChecklistProgress: client.ChecklistProgress{Done: 0, Total: 2}}
	fc := &fakeClient{}
	m := newListModel(fc, []*client.Task{listTask})
	m.push(screenChecklist)
	m.detail = detail
	m.itemSel = 0
	return fc, m, detail
}

func TestOptimisticToggle_FlipsAndSyncsList(t *testing.T) {
	fc, m, detail := checklistFixture()
	fc.toggleResp = &client.ChecklistItem{ID: 10, Title: "a", Completed: true}

	cmd := m.press("space")
	// optimistic: item + progress flip immediately, before the server responds
	if !detail.ChecklistItems[0].Completed {
		t.Fatal("toggle should flip the item optimistically")
	}
	if detail.ChecklistProgress != (client.ChecklistProgress{Done: 1, Total: 2}) {
		t.Fatalf("detail progress = %+v, want 1/2", detail.ChecklistProgress)
	}
	if m.tasks[0].ChecklistProgress != (client.ChecklistProgress{Done: 1, Total: 2}) {
		t.Fatalf("list progress not synced: %+v", m.tasks[0].ChecklistProgress)
	}
	// server confirms — reconcile leaves it consistent
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if !detail.ChecklistItems[0].Completed {
		t.Fatal("server confirm should keep it completed")
	}
}

func TestOptimisticToggle_RevertsOnError(t *testing.T) {
	fc, m, detail := checklistFixture()
	fc.toggleErr = &client.APIError{Status: 500, Body: "boom"}

	cmd := m.press("space")
	if !detail.ChecklistItems[0].Completed {
		t.Fatal("optimistic flip expected")
	}
	// server fails → revert
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if detail.ChecklistItems[0].Completed {
		t.Fatal("failed toggle should revert the item")
	}
	if detail.ChecklistProgress != (client.ChecklistProgress{Done: 0, Total: 2}) {
		t.Fatalf("progress should revert to 0/2, got %+v", detail.ChecklistProgress)
	}
	if m.tasks[0].ChecklistProgress != (client.ChecklistProgress{Done: 0, Total: 2}) {
		t.Fatalf("list progress should revert, got %+v", m.tasks[0].ChecklistProgress)
	}
	if !m.statusErr || m.status == "" {
		t.Fatal("failed toggle should surface a footer error")
	}
}

// -- drill-in ---------------------------------------------------------------

func TestOpenTask_LoadsDetail(t *testing.T) {
	fc := &fakeClient{
		getResp: &client.Task{ID: 7, Title: "Deep", ChecklistItems: []*client.ChecklistItem{{ID: 1, Title: "x"}}},
	}
	m := newListModel(fc, []*client.Task{{ID: 7, Title: "Deep"}})
	cmd := m.press("enter")
	if m.current() != screenChecklist {
		t.Fatalf("enter should push checklist, got screen %d", m.current())
	}
	if !m.loading {
		t.Fatal("drill-in should show loading until the fetch returns")
	}
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if m.detail == nil || m.detail.ID != 7 {
		t.Fatalf("detail not loaded: %+v", m.detail)
	}
	if m.loading {
		t.Fatal("loading should clear after detail arrives")
	}
}

// -- search -----------------------------------------------------------------

func TestSearch_LiveFilterThenServer(t *testing.T) {
	fc := &fakeClient{
		searchResp: []*client.Task{{ID: 2, Title: "Banana", MatchedChecklistItems: []client.MatchedChecklistItem{{ID: 9, Title: "peel"}}}},
	}
	tasks := []*client.Task{
		{ID: 1, Title: "Apple"}, {ID: 2, Title: "Banana"}, {ID: 3, Title: "Grape"},
	}
	m := newListModel(fc, tasks)

	m.press("/")
	if !m.searching {
		t.Fatal("/ should enter search mode")
	}
	// live client-side filter as you type
	m.press("a")
	m.press("n")
	if got := m.searchInput.String(); got != "an" {
		t.Fatalf("search buffer = %q, want an", got)
	}
	if vis := m.visibleTasks(); len(vis) != 1 || vis[0].ID != 2 {
		t.Fatalf("live filter should show Banana only, got %+v", vis)
	}

	// Enter runs the server search
	cmd := m.press("enter")
	if m.searching {
		t.Fatal("enter should exit typing mode")
	}
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if m.searchLabel != "an" || len(m.tasks) != 1 {
		t.Fatalf("server search results not applied: label=%q tasks=%d", m.searchLabel, len(m.tasks))
	}
	// esc clears the server search and reloads the full list
	fc.listResp = tasks
	cmd = m.press("esc")
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if m.searchLabel != "" || len(m.tasks) != 3 {
		t.Fatalf("esc should clear search and restore full list: label=%q tasks=%d", m.searchLabel, len(m.tasks))
	}
}

// -- delete + create --------------------------------------------------------

func TestDeleteTask_ConfirmFlow(t *testing.T) {
	fc := &fakeClient{}
	m := newListModel(fc, []*client.Task{{ID: 5, Title: "Doomed"}})
	m.press("d")
	if m.overlay != ovConfirm || m.confirmID != 5 {
		t.Fatalf("d should open a delete confirm for #5, got overlay=%d id=%d", m.overlay, m.confirmID)
	}
	// 'n' cancels
	m.press("n")
	if m.overlay != ovNone || len(m.tasks) != 1 {
		t.Fatal("n should cancel the delete")
	}
	// 'y' confirms
	m.press("d")
	cmd := m.press("y")
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if len(m.tasks) != 0 {
		t.Fatalf("confirmed delete should remove the task, %d left", len(m.tasks))
	}
}

func TestCreateTask_NoProjects(t *testing.T) {
	fc := &fakeClient{createResp: &client.Task{ID: 9, Title: "Fresh"}, listResp: []*client.Task{{ID: 9, Title: "Fresh"}}}
	m := newListModel(fc, []*client.Task{{ID: 1, Title: "old"}}) // no projects present
	m.press("n")
	if m.overlay != ovInput {
		t.Fatalf("n should open the title input, got overlay %d", m.overlay)
	}
	for _, k := range []string{"F", "r", "e", "s", "h"} {
		m.press(k)
	}
	cmd := m.press("enter")
	if m.overlay != ovNone {
		t.Fatal("enter should close the input")
	}
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if fc.createAttrs["title"] != "Fresh" {
		t.Fatalf("create attrs title = %v, want Fresh", fc.createAttrs["title"])
	}
	if !strings.Contains(m.status, "created") {
		t.Fatalf("status should confirm creation, got %q", m.status)
	}
}

// -- copy -------------------------------------------------------------------

func TestCopyItem_SetsStatus(t *testing.T) {
	_, m, detail := checklistFixture()
	cmd := m.press("c")
	if !strings.Contains(m.status, "copied item") {
		t.Fatalf("copy item should set a transient status, got %q", m.status)
	}
	if cmd == nil {
		t.Fatal("copy should return a clipboard command")
	}
	// The clipboard payload must be the selected item's Markdown, wired to the
	// right item (#10) and its parent task (#1) — not just any status text.
	got := clipboardPayload(t, cmd)
	want := itemMarkdown(detail.ChecklistItems[0], detail)
	if got != want {
		t.Fatalf("clipboard payload mismatch:\n got=%q\nwant=%q", got, want)
	}
	if !strings.Contains(got, "- [ ] #10 a") || !strings.Contains(got, "sprawl task: #1 T") {
		t.Fatalf("clipboard should carry item #10 under task #1, got:\n%s", got)
	}
}

func TestCopyTask_FromList(t *testing.T) {
	full := &client.Task{
		ID: 3, Title: "Whole", Status: "open",
		ChecklistItems: []*client.ChecklistItem{{ID: 4, Title: "step", Notes: strptr("do it")}},
	}
	fc := &fakeClient{getResp: full}
	m := newListModel(fc, []*client.Task{{ID: 3, Title: "Whole"}})
	// press c → getTaskCmd(forCopy); running it yields taskLoadedMsg{forCopy}.
	loadMsgs := runCmd(m.press("c"))
	var clip tea.Cmd
	for _, msg := range loadMsgs {
		_, cmd := m.Update(msg)
		if cmd != nil {
			clip = cmd
		}
	}
	if !strings.Contains(m.status, "copied task #3") {
		t.Fatalf("copy task should confirm via status, got %q", m.status)
	}
	got := clipboardPayload(t, clip)
	want := taskMarkdown(full)
	if got != want {
		t.Fatalf("clipboard payload should equal taskMarkdown(full task):\n got=%q\nwant=%q", got, want)
	}
	if !strings.Contains(got, "# Sprawl Task #3 — Whole") || !strings.Contains(got, "do it") {
		t.Fatalf("clipboard should carry the full task incl. item notes, got:\n%s", got)
	}
}

// -- picker flows -----------------------------------------------------------

func TestSetDue_PickerClearSendsNil(t *testing.T) {
	fc := &fakeClient{dueResp: &client.Task{ID: 5, Title: "D"}}
	m := newListModel(fc, []*client.Task{{ID: 5, Title: "D"}})
	m.press("t")
	if m.overlay != ovPicker || m.pickerKind != pickDue {
		t.Fatalf("t should open the due picker, got overlay=%d kind=%d", m.overlay, m.pickerKind)
	}
	// items: yesterday, today, this week, clear (none) — move to the last.
	m.press("down")
	m.press("down")
	m.press("down")
	cmd := m.press("enter")
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if !fc.dueArgSet {
		t.Fatal("SetTaskDueDate should have been called")
	}
	if fc.dueArg != nil {
		t.Fatalf("clear should send a nil due date, got %v", *fc.dueArg)
	}
}

func TestSetDue_PickerValueSendsPointer(t *testing.T) {
	fc := &fakeClient{dueResp: &client.Task{ID: 5, Title: "D"}}
	m := newListModel(fc, []*client.Task{{ID: 5, Title: "D"}})
	m.press("t")
	m.press("down") // -> "today"
	cmd := m.press("enter")
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if fc.dueArg == nil || *fc.dueArg != "today" {
		t.Fatalf("selecting 'today' should send &\"today\", got %v", fc.dueArg)
	}
}

func TestCreateTask_WithProjectPicker(t *testing.T) {
	fc := &fakeClient{
		createResp: &client.Task{ID: 9, Title: "Fresh"},
		listResp:   []*client.Task{{ID: 9, Title: "Fresh"}},
	}
	// A task with a project makes distinctProjects() non-empty → project picker.
	m := newListModel(fc, []*client.Task{{ID: 1, Title: "old", Project: &client.Project{ID: 42, Name: "Alpha"}}})
	if len(m.distinctProjects()) != 1 {
		t.Fatalf("distinctProjects should surface the one project, got %d", len(m.distinctProjects()))
	}
	m.press("n")
	for _, k := range []string{"F", "r", "e", "s", "h"} {
		m.press(k)
	}
	m.press("enter") // no create yet — should open the project picker
	if m.overlay != ovPicker || m.pickerKind != pickProject {
		t.Fatalf("enter with projects present should open the project picker, got overlay=%d kind=%d", m.overlay, m.pickerKind)
	}
	// items: (none), Alpha — pick Alpha.
	m.press("down")
	cmd := m.press("enter")
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if fc.createAttrs["title"] != "Fresh" {
		t.Fatalf("create title = %v, want Fresh", fc.createAttrs["title"])
	}
	if got, ok := fc.createAttrs["project_id"].(int64); !ok || got != 42 {
		t.Fatalf("project_id should be threaded as int64(42), got %v (ok=%v)", fc.createAttrs["project_id"], ok)
	}
}

func TestCreateTask_ProjectPickerNoneOmitsProjectID(t *testing.T) {
	fc := &fakeClient{
		createResp: &client.Task{ID: 9, Title: "Fresh"},
		listResp:   []*client.Task{{ID: 9, Title: "Fresh"}},
	}
	m := newListModel(fc, []*client.Task{{ID: 1, Title: "old", Project: &client.Project{ID: 42, Name: "Alpha"}}})
	m.press("n")
	for _, k := range []string{"F", "r", "e", "s", "h"} {
		m.press(k)
	}
	m.press("enter")        // opens project picker, defaults to "(none)"
	cmd := m.press("enter") // select "(none)"
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if _, ok := fc.createAttrs["project_id"]; ok {
		t.Fatalf("(none) should omit project_id, got %v", fc.createAttrs["project_id"])
	}
}

// -- editor round-trips -----------------------------------------------------

func TestEditorDone_TaskDescUpdatesTask(t *testing.T) {
	fc := &fakeClient{updateResp: &client.Task{ID: 1, Title: "T", Description: "new"}}
	m := newListModel(fc, []*client.Task{{ID: 1, Title: "T"}})
	cmd := m.send(editorDoneMsg{kind: editTaskDesc, id: 1, body: "new desc\n"})
	if !m.loading {
		t.Fatal("editTaskDesc should set loading while the update is in flight")
	}
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if fc.updateTaskAttrs["description"] != "new desc" {
		t.Fatalf("description should be trimmed and sent, got %v", fc.updateTaskAttrs["description"])
	}
}

func TestEditorDone_ItemNoteSavesNote(t *testing.T) {
	fc, m, detail := checklistFixture()
	m.push(screenNote)
	fc.notesResp = strptr("new note")
	cmd := m.send(editorDoneMsg{kind: editItemNote, id: 10, body: "new note\n"})
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if fc.notesArg != "new note" {
		t.Fatalf("note body should be trimmed and sent, got %q", fc.notesArg)
	}
	// notesSetMsg should have written the note back onto the item.
	if detail.ChecklistItems[0].Notes == nil || *detail.ChecklistItems[0].Notes != "new note" {
		t.Fatalf("item note not applied: %v", detail.ChecklistItems[0].Notes)
	}
	if !detail.ChecklistItems[0].HasNotes {
		t.Fatal("saving a note should set HasNotes")
	}
	if !strings.Contains(m.status, "saved note") {
		t.Fatalf("status should confirm the save, got %q", m.status)
	}
}

func TestEditorDone_ErrorSurfacesToFooter(t *testing.T) {
	_, m, _ := checklistFixture()
	m.send(editorDoneMsg{kind: editItemNote, id: 10, err: context.Canceled})
	if !m.statusErr || m.status == "" {
		t.Fatalf("an editor error should surface on the footer, got err=%v status=%q", m.statusErr, m.status)
	}
}

// -- item mutations ---------------------------------------------------------

func TestItemMutated_AddAppends(t *testing.T) {
	_, m, detail := checklistFixture()
	m.send(itemMutatedMsg{item: &client.ChecklistItem{ID: 12, Title: "c"}, created: true})
	if len(detail.ChecklistItems) != 3 || detail.ChecklistItems[2].ID != 12 {
		t.Fatalf("add should append the new item, got %+v", detail.ChecklistItems)
	}
	if detail.ChecklistProgress.Total != 3 {
		t.Fatalf("progress total should recompute to 3, got %d", detail.ChecklistProgress.Total)
	}
	if !strings.Contains(m.status, "added item #12") {
		t.Fatalf("status should confirm the add, got %q", m.status)
	}
}

func TestItemMutated_EditTitlePreservesNotes(t *testing.T) {
	_, m, detail := checklistFixture()
	detail.ChecklistItems[0].Notes = strptr("keep me")
	detail.ChecklistItems[0].HasNotes = true
	// A title-edit response carries no notes; the loaded body must survive.
	m.send(itemMutatedMsg{item: &client.ChecklistItem{ID: 10, Title: "renamed", Notes: nil}, created: false})
	got := detail.ChecklistItems[0]
	if got.Title != "renamed" {
		t.Fatalf("title should update, got %q", got.Title)
	}
	if got.Notes == nil || *got.Notes != "keep me" {
		t.Fatalf("edit-title must preserve the loaded note, got %v", got.Notes)
	}
	if !strings.Contains(m.status, "updated item #10") {
		t.Fatalf("status should confirm the update, got %q", m.status)
	}
}

func TestItemDeleted_RemovesAndResyncs(t *testing.T) {
	_, m, detail := checklistFixture()
	detail.ChecklistItems[0].Completed = true
	detail.ChecklistProgress = recomputeProgress(detail.ChecklistItems)
	m.send(itemDeletedMsg{id: 10})
	if len(detail.ChecklistItems) != 1 || detail.ChecklistItems[0].ID != 11 {
		t.Fatalf("delete should drop item #10, got %+v", detail.ChecklistItems)
	}
	if detail.ChecklistProgress.Total != 1 {
		t.Fatalf("progress should recompute to total 1, got %+v", detail.ChecklistProgress)
	}
	if m.tasks[0].ChecklistProgress.Total != 1 {
		t.Fatalf("list progress should resync after delete, got %+v", m.tasks[0].ChecklistProgress)
	}
	if !strings.Contains(m.status, "deleted item #10") {
		t.Fatalf("status should confirm the delete, got %q", m.status)
	}
}

// -- task update reconciliation ---------------------------------------------

func TestTaskMutated_UpdateUpsertsAndCopiesDetail(t *testing.T) {
	_, m, detail := checklistFixture()
	m.send(taskMutatedMsg{
		task: &client.Task{
			ID: 1, Title: "renamed", DueDate: "2026-02-02", Status: "done",
			Project: &client.Project{ID: 7, Name: "P"},
		},
		created: false,
	})
	if m.tasks[0].Title != "renamed" {
		t.Fatalf("update should upsert the list entry, got %q", m.tasks[0].Title)
	}
	if detail.Title != "renamed" || detail.DueDate != "2026-02-02" || detail.Status != "done" {
		t.Fatalf("open detail should absorb the mutated fields, got %+v", detail)
	}
	if detail.Project == nil || detail.Project.Name != "P" {
		t.Fatalf("detail project should update, got %+v", detail.Project)
	}
	if !strings.Contains(m.status, "updated task #1") {
		t.Fatalf("status should confirm the update, got %q", m.status)
	}
}

// -- mid-session auth failures ----------------------------------------------

func TestMidSession401_BouncesToSecretPrompt(t *testing.T) {
	fc := &fakeClient{}
	m := newListModel(fc, []*client.Task{{ID: 1, Title: "T"}})
	// opErrMsg classifies a 401 as an auth failure that returns to the prompt.
	m.send(opErrMsg(&client.APIError{Status: 401}, "load tasks"))
	if m.current() != screenCreds {
		t.Fatalf("a mid-session 401 should bounce to the creds prompt, got screen %d", m.current())
	}
	if m.validated || m.secret != "" || m.client != nil || m.credErr == "" {
		t.Fatalf("bounce should clear creds and show an inline error: validated=%v secret=%q client=%v err=%q",
			m.validated, m.secret, m.client, m.credErr)
	}
}

func TestMidSession403_StaysFooterError(t *testing.T) {
	fc := &fakeClient{}
	m := newListModel(fc, []*client.Task{{ID: 1, Title: "T"}})
	m.send(opErrMsg(&client.APIError{Status: 403}, "delete task"))
	if m.current() != screenList {
		t.Fatalf("a 403 permission error must stay on the list, got screen %d", m.current())
	}
	if !m.statusErr || m.status == "" {
		t.Fatalf("a 403 should surface as a footer error, got err=%v status=%q", m.statusErr, m.status)
	}
}

// -- window size / rendering ------------------------------------------------

func TestWindowSize_And_ViewRendersEveryScreen(t *testing.T) {
	fc := &fakeClient{}
	m := newListModel(fc, []*client.Task{{ID: 1, Title: "T", ChecklistProgress: client.ChecklistProgress{Done: 1, Total: 2}}})
	m.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Fatalf("window size not stored: %dx%d", m.width, m.height)
	}
	// list renders without panic and fills the height
	if lines := strings.Count(m.View().Content, "\n"); lines != 39 {
		t.Fatalf("expected 40 lines (39 newlines), got %d", lines)
	}
	// checklist + note + overlays render too
	m.detail = &client.Task{ID: 1, Title: "T", ChecklistItems: []*client.ChecklistItem{{ID: 2, Title: "i", Notes: strptr("body")}}}
	m.push(screenChecklist)
	_ = m.View()
	m.push(screenNote)
	_ = m.View()
	m.overlay = ovHelp
	if !strings.Contains(m.View().Content, "Help") {
		t.Fatal("help overlay should render")
	}
}

func TestFrame_ClampsToTinyWindow(t *testing.T) {
	fc := &fakeClient{}
	m := newListModel(fc, []*client.Task{{ID: 1, Title: "T"}})
	// A window shorter than the frame's minimum (header+rule+body+status+hints)
	// must never emit more rows than the viewport has.
	for _, h := range []int{1, 2, 3, 4, 5} {
		m.send(tea.WindowSizeMsg{Width: 40, Height: h})
		lines := strings.Count(m.View().Content, "\n") + 1
		if lines > h {
			t.Fatalf("height %d: frame emitted %d lines, must not exceed the viewport", h, lines)
		}
	}
}

func TestNotLoggedIn_QuitKey(t *testing.T) {
	m := newModel(context.Background(), Deps{LoggedIn: false, NewClient: func(string, string) Client { return &fakeClient{} }})
	if m.current() != screenNotLoggedIn {
		t.Fatalf("no token should land on not-logged-in, got %d", m.current())
	}
	cmd := m.press("q")
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q should return a QuitMsg")
	}
}

func TestCtrlC_QuitsFromAnyScreen(t *testing.T) {
	fc := &fakeClient{}
	m := newListModel(fc, nil)
	cmd := m.press("ctrl+c")
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c should quit")
	}
}
