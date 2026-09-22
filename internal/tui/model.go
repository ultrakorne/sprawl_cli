package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// screen identifies a view in the back stack. esc pops one screen.
type screen int

const (
	screenCreds screen = iota
	screenNotLoggedIn
	screenList
	screenChecklist
	screenNote
)

// overlayKind is the active modal overlay, if any.
type overlayKind int

const (
	ovNone overlayKind = iota
	ovConfirm
	ovInput
	ovPicker
	ovHelp
)

type inputKind int

const (
	inNewTaskTitle inputKind = iota
	inEditTaskTitle
	inAddItem
	inEditItemTitle
	inSetPR
)

type confirmKind int

const (
	confirmDeleteTask confirmKind = iota
	confirmDeleteItem
)

type pickerKind int

const (
	pickDue pickerKind = iota
	pickProject
	pickWorkspace
)

type pickerItem struct {
	label string
	value string
}

// credField is the focused field on the credentials prompt. The server needs
// the bearer plus at least ONE of the two, so the prompt offers both and
// submits whichever the user filled in (both is legal too — the server
// intersects them).
type credField int

const (
	credSecret credField = iota
	credProjectKey
)

// Model is the bubbletea model for the interactive TUI.
type Model struct {
	ctx    context.Context
	styles styles

	newClient func(secret, projectKey, workspace string) Client
	client    Client
	secret    string
	loggedIn  bool
	validated bool
	// projectKey is the confinement this session runs under (empty = none). It
	// comes from the flag/env or from the credentials prompt, and the list
	// header shows it.
	projectKey string
	// workspace is the selected workspace id (empty = the factors' default),
	// from the flag/env or the `w` picker. wsCurrent / wsList are what whoami
	// last reported: the workspace the session runs in (the selection resolved
	// against the reachable list, else the server's default) and every
	// reachable workspace. Both are nil until the first whoami lands, and stay
	// nil on a pre-workspaces server — the header then simply omits the name.
	workspace string
	wsCurrent *client.Workspace
	wsList    []client.Workspace

	width, height int

	stack []screen

	// list data
	tasks   []*client.Task
	listSel int

	// checklist detail (full task with items + notes)
	detail  *client.Task
	itemSel int
	// pendingTaskID is the task id of the in-flight drill-in/refresh fetch, so a
	// stale out-of-order taskLoadedMsg (opened A, backed out, opened B) can be
	// discarded instead of overwriting the task the user is actually viewing.
	pendingTaskID int64

	// note screen scroll offset (lines)
	noteOff int
	// noteItemID is the item the note screen shows. It is pinned by id rather
	// than read through itemSel: under a checklist filter, editing the note can
	// drop the item out of the filtered rows, and the screen must not slide
	// onto whichever item takes its place.
	noteItemID int64

	loading bool

	// credentials prompt (agent secret / project key — either one will do)
	secretInput textInput
	keyInput    textInput
	credFocus   credField
	credErr     string
	credBusy    bool

	// `/` search on the list: searching is true while the query is being typed;
	// listFilter is the query kept after enter. Either filters the list live.
	searching   bool
	searchInput textInput
	listFilter  string

	// `/` search on the checklist, same shape as the list's. Reset whenever a
	// task is opened from the list.
	itemSearching   bool
	itemSearchInput textInput
	itemFilter      string

	// index holds full tasks (items + notes) by id, so the list search can
	// reach checklist items and notes the list payload doesn't carry. Filled
	// on `/` by indexTasksCmd and by every drill-in; dropped by refresh and a
	// workspace switch. indexGen invalidates an in-flight fill that a reset
	// has overtaken; indexFailed stops a task that won't load from being
	// re-requested until the next reset.
	index       map[int64]*client.Task
	indexFailed map[int64]bool
	indexing    bool
	indexGen    int
	indexCancel context.CancelFunc // aborts the running fill's requests

	// overlays
	overlay          overlayKind
	input            textInput
	inputKind        inputKind
	inputPrompt      string
	inputTargetID    int64
	confirmKind      confirmKind
	confirmID        int64
	confirmName      string
	pickerKind       pickerKind
	pickerItems      []pickerItem
	pickerSel        int
	pickerTitle      string
	pendingTaskTitle string

	// transient footer status
	status    string
	statusErr bool
	statusTok int

	initCmd  tea.Cmd
	quitting bool
}

// newModel wires the initial model + first command from resolved deps.
func newModel(ctx context.Context, deps Deps) *Model {
	m := &Model{
		ctx:        ctx,
		styles:     newStyles(),
		newClient:  deps.NewClient,
		loggedIn:   deps.LoggedIn,
		projectKey: deps.ProjectKey,
		workspace:  deps.Workspace,
	}
	switch {
	case !deps.LoggedIn:
		m.stack = []screen{screenNotLoggedIn}
	case (deps.SecretProvided && deps.Secret != "") || deps.ProjectKey != "":
		// A narrowing factor is already configured — a supplied secret, a
		// project key, or both. Validate straight away by fetching the list; a
		// 401/403 drops to the credentials prompt, where either factor can be
		// (re-)entered.
		m.secret = deps.Secret
		m.client = m.newClient(deps.Secret, deps.ProjectKey, deps.Workspace)
		m.stack = []screen{screenList}
		m.loading = true
		m.initCmd = validateAndListCmd(ctx, m.client)
	default:
		// Neither factor configured: ask for one or the other.
		m.stack = []screen{screenCreds}
	}
	return m
}

// focusCreds points the credentials prompt at the field the user is most likely
// to want: the project key when that is the only factor in play, the agent
// secret otherwise (it's the usual answer, and the default on a cold start).
func (m *Model) focusCreds() {
	if m.projectKey != "" && m.secret == "" {
		m.credFocus = credProjectKey
		return
	}
	m.credFocus = credSecret
}

// credInput returns the focused field of the credentials prompt.
func (m *Model) credInput() *textInput {
	if m.credFocus == credProjectKey {
		return &m.keyInput
	}
	return &m.secretInput
}

func (m *Model) Init() tea.Cmd { return m.initCmd }

// -- back stack -------------------------------------------------------------

func (m *Model) current() screen { return m.stack[len(m.stack)-1] }

func (m *Model) push(s screen) { m.stack = append(m.stack, s) }

func (m *Model) pop() {
	if len(m.stack) > 1 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

func (m *Model) popTo(s screen) {
	for len(m.stack) > 1 && m.current() != s {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// -- selection helpers ------------------------------------------------------

// listQuery is the query filtering the list: the one being typed, else the
// one kept after enter.
func (m *Model) listQuery() string {
	if m.searching {
		return m.searchInput.String()
	}
	return m.listFilter
}

// itemQuery is listQuery for the checklist.
func (m *Model) itemQuery() string {
	if m.itemSearching {
		return m.itemSearchInput.String()
	}
	return m.itemFilter
}

// listMatches is the list as the screen shows it: filtered and ranked by the
// query, with what matched in each row.
func (m *Model) listMatches() []taskMatch {
	return filterTasks(searchTerms(m.listQuery()), m.tasks, m.index)
}

func (m *Model) visibleTasks() []*client.Task {
	ms := m.listMatches()
	out := make([]*client.Task, len(ms))
	for i, tm := range ms {
		out[i] = tm.task
	}
	return out
}

// itemMatches is the open task's checklist as the screen shows it: filtered by
// the query, in checklist order.
func (m *Model) itemMatches() []itemMatch {
	if m.detail == nil {
		return nil
	}
	return filterItems(searchTerms(m.itemQuery()), m.detail.ChecklistItems)
}

// visibleItems is the checklist rows on screen; itemSel indexes into it, so
// every item action works on the filtered rows.
func (m *Model) visibleItems() []*client.ChecklistItem {
	ms := m.itemMatches()
	out := make([]*client.ChecklistItem, len(ms))
	for i, im := range ms {
		out[i] = im.item
	}
	return out
}

func (m *Model) itemCount() int { return len(m.visibleItems()) }

func (m *Model) selectedTask() *client.Task {
	vis := m.visibleTasks()
	if len(vis) == 0 || m.listSel < 0 || m.listSel >= len(vis) {
		return nil
	}
	return vis[m.listSel]
}

// descriptionTask is the task E edits: the open one on the checklist, the
// highlighted row on the list.
func (m *Model) descriptionTask() *client.Task {
	if m.current() == screenChecklist {
		return m.detail
	}
	return m.selectedTask()
}

// selectedItem is the item actions apply to: the highlighted row on the
// checklist, the pinned item on the note screen (ids are positive, so an unset
// pin falls back to the highlighted row).
func (m *Model) selectedItem() *client.ChecklistItem {
	if m.current() == screenNote && m.noteItemID != 0 {
		if m.detail == nil {
			return nil
		}
		for _, it := range m.detail.ChecklistItems {
			if it.ID == m.noteItemID {
				return it
			}
		}
		return nil
	}
	vis := m.visibleItems()
	if m.itemSel < 0 || m.itemSel >= len(vis) {
		return nil
	}
	return vis[m.itemSel]
}

func (m *Model) clampListSel() {
	n := len(m.visibleTasks())
	m.listSel = clamp(m.listSel, 0, maxInt(0, n-1))
}

func (m *Model) clampItemSel() {
	n := m.itemCount()
	m.itemSel = clamp(m.itemSel, 0, maxInt(0, n-1))
}

// selectTask puts the list cursor on task id if it is visible, else clamps —
// so clearing a filter leaves the cursor on the task that was found.
func (m *Model) selectTask(id int64) {
	for i, t := range m.visibleTasks() {
		if t.ID == id {
			m.listSel = i
			return
		}
	}
	m.clampListSel()
}

// selectItem is selectTask for the checklist.
func (m *Model) selectItem(id int64) {
	for i, it := range m.visibleItems() {
		if it.ID == id {
			m.itemSel = i
			return
		}
	}
	m.clampItemSel()
}

// -- search index -----------------------------------------------------------

// ensureIndex fetches, in the background, the full copy of every listed task
// the index doesn't hold yet. nil when there is nothing to fetch or a fill is
// already running (its arrival calls back in here for anything it missed).
func (m *Model) ensureIndex() tea.Cmd {
	if m.indexing || m.client == nil {
		return nil
	}
	var ids []int64
	for _, t := range m.tasks {
		if m.index[t.ID] == nil && !m.indexFailed[t.ID] {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	m.indexing = true
	ctx, cancel := context.WithCancel(m.ctx)
	m.indexCancel = cancel
	return indexTasksCmd(ctx, m.client, ids, m.indexGen)
}

// indexTask records a full task. While a task is open it is kept as the very
// pointer on screen, so edits made on the checklist (toggles, titles, notes,
// new items) are what the list search sees without a refetch. m.detail
// outlives the screen, so once back on the list a fetched copy wins over it.
func (m *Model) indexTask(t *client.Task) {
	if t == nil || t.ChecklistItems == nil {
		return
	}
	onTask := m.current() == screenChecklist || m.current() == screenNote
	if onTask && m.detail != nil && m.detail.ID == t.ID {
		t = m.detail
	}
	if m.index == nil {
		m.index = map[int64]*client.Task{}
	}
	m.index[t.ID] = t
}

// resetIndex forgets every full task — on refresh, so the search sees what the
// server has now, and on a workspace switch, where the ids mean other tasks.
func (m *Model) resetIndex() {
	m.stopIndexing()
	m.index = nil
	m.indexFailed = nil
	m.indexGen++
}

// stopIndexing ends the running fill, if any, cancelling its requests.
func (m *Model) stopIndexing() {
	if m.indexCancel != nil {
		m.indexCancel()
		m.indexCancel = nil
	}
	m.indexing = false
}

// -- status -----------------------------------------------------------------

// transientTTL is how long a transient footer status stays up. A variable so
// tests that drain every command can shorten the timer instead of sleeping.
var transientTTL = 2500 * time.Millisecond

// setTransient sets a footer status that auto-clears after transientTTL. The
// token guards against a stale timer wiping a newer status.
func (m *Model) setTransient(text string) tea.Cmd {
	m.status = text
	m.statusErr = false
	m.statusTok++
	return clearStatusAfter(m.statusTok, transientTTL)
}

// setError sets a persistent footer error (cleared by the next status). The
// token bump invalidates any pending transient-clear timer.
func (m *Model) setError(ctxLabel string, err error) {
	m.status = ctxLabel + ": " + err.Error()
	m.statusErr = true
	m.statusTok++
}

// -- data reconciliation ----------------------------------------------------

// recomputeProgress derives a checklist's done/total from its items. Pure.
func recomputeProgress(items []*client.ChecklistItem) client.ChecklistProgress {
	p := client.ChecklistProgress{Total: len(items)}
	for _, it := range items {
		if it.Completed {
			p.Done++
		}
	}
	return p
}

// syncListProgress mirrors a toggled task's progress onto the list entry so the
// list stays in sync when the checklist screen is popped.
func (m *Model) syncListProgress(taskID int64, p client.ChecklistProgress) {
	for _, t := range m.tasks {
		if t.ID == taskID {
			t.ChecklistProgress = p
			return
		}
	}
}

func (m *Model) upsertTask(t *client.Task) {
	for i, existing := range m.tasks {
		if existing.ID == t.ID {
			m.tasks[i] = t
			return
		}
	}
	m.tasks = append(m.tasks, t)
}

func (m *Model) removeTask(id int64) {
	out := m.tasks[:0]
	for _, t := range m.tasks {
		if t.ID != id {
			out = append(out, t)
		}
	}
	m.tasks = out
}

// replaceItem swaps the loaded copy of an item for a fresher one from the
// server and recomputes everything derived from it. The incoming item keeps the
// loaded notes body: single-item write responses (title, state, PR) never carry
// notes, so taking the response verbatim would blank the note screen.
func (m *Model) replaceItem(item *client.ChecklistItem) {
	if m.detail == nil || item == nil {
		return
	}
	for i, it := range m.detail.ChecklistItems {
		if it.ID == item.ID {
			item.Notes = it.Notes
			m.detail.ChecklistItems[i] = item
			break
		}
	}
	m.detail.ChecklistProgress = recomputeProgress(m.detail.ChecklistItems)
	m.syncListProgress(m.detail.ID, m.detail.ChecklistProgress)
}

// nextState is the `s` key's cycle: none → ready → progress → review → none.
// An unrecognised value (a state this build doesn't know) restarts the cycle
// rather than sticking. Pure and unit-tested.
func nextState(cur string) string {
	switch cur {
	case "":
		return client.StateReadyToPickup
	case client.StateReadyToPickup:
		return client.StateInProgress
	case client.StateInProgress:
		return client.StateInReview
	case client.StateInReview:
		return ""
	default:
		return client.StateReadyToPickup
	}
}

// removeItemByID returns items with the given id dropped (order preserved).
func removeItemByID(items []*client.ChecklistItem, id int64) []*client.ChecklistItem {
	out := make([]*client.ChecklistItem, 0, len(items))
	for _, it := range items {
		if it.ID != id {
			out = append(out, it)
		}
	}
	return out
}

// distinctProjects returns the unique projects present in the loaded task list,
// most-recently-seen order preserved, for the create-time project picker (there
// is no ListProjects endpoint — see the spec's "known gap").
func (m *Model) distinctProjects() []pickerItem {
	seen := map[int64]bool{}
	var out []pickerItem
	for _, t := range m.tasks {
		if t.Project == nil || seen[t.Project.ID] {
			continue
		}
		seen[t.Project.ID] = true
		out = append(out, pickerItem{label: t.Project.Name, value: strconv.FormatInt(t.Project.ID, 10)})
	}
	return out
}

// -- workspaces -------------------------------------------------------------

// stale reports whether a response came from a client this session no longer
// uses — the workspace switch and the credentials prompt both replace
// m.client, and a list or whoami answer issued before that would otherwise
// land under the new workspace's header. Messages from before the client
// existed (from == nil) are never stale.
func (m *Model) stale(from Client) bool {
	return from != nil && from != m.client
}

// wsName is the workspace's display name, with the same fallback the CLI's
// tables use so a nameless workspace never renders as an empty gap.
func wsName(ws *client.Workspace) string {
	if ws == nil || strings.TrimSpace(ws.Name) == "" {
		return "(unnamed)"
	}
	return ws.Name
}

// applyWhoami records what the server said about workspaces. The current one
// is the selection resolved against the reachable list — whoami is user-level
// and doesn't take the selector, so its `workspace` is only the default. A
// selection that isn't in the list leaves wsCurrent nil: the header shows
// nothing rather than the wrong name, and the list fetch's 404 says the rest.
func (m *Model) applyWhoami(w *client.Whoami) {
	if w == nil {
		return
	}
	m.wsList = w.Workspaces
	m.wsCurrent = resolveWorkspace(w, m.workspace)
}

// resolveWorkspace is applyWhoami's pure core: the reachable workspace whose
// id matches `selected`, or the server's default when nothing is selected.
func resolveWorkspace(w *client.Whoami, selected string) *client.Workspace {
	if selected == "" {
		return w.Workspace
	}
	for i := range w.Workspaces {
		if itoa(w.Workspaces[i].ID) == selected {
			return &w.Workspaces[i]
		}
	}
	return nil
}

// workspacePickerItems lists the reachable workspaces for the `w` picker:
// name, then the user's role and the key's level there, so a workspace the
// key can't reach (level none — every list there would be empty) is visible
// before it's chosen. The current one is marked; selecting it is a no-op.
func workspacePickerItems(list []client.Workspace, current *client.Workspace) []pickerItem {
	out := make([]pickerItem, 0, len(list))
	for i := range list {
		ws := &list[i]
		label := fmt.Sprintf("%s  #%d · %s", wsName(ws), ws.ID, ws.Role)
		if ws.Level != "" && ws.Level != ws.Role {
			label += " · " + ws.Level
		}
		if current != nil && ws.ID == current.ID {
			label += "  (current)"
		}
		out = append(out, pickerItem{label: label, value: itoa(ws.ID)})
	}
	return out
}

// -- small helpers ----------------------------------------------------------

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
