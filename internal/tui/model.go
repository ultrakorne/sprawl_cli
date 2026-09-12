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
	tasks       []*client.Task
	listSel     int
	searchLabel string // non-empty when the current list is a server-search result

	// checklist detail (full task with items + notes)
	detail  *client.Task
	itemSel int
	// pendingTaskID is the task id of the in-flight drill-in/refresh fetch, so a
	// stale out-of-order taskLoadedMsg (opened A, backed out, opened B) can be
	// discarded instead of overwriting the task the user is actually viewing.
	pendingTaskID int64

	// note screen scroll offset (lines)
	noteOff int

	loading bool

	// credentials prompt (agent secret / project key — either one will do)
	secretInput textInput
	keyInput    textInput
	credFocus   credField
	credErr     string
	credBusy    bool

	// live search (list screen)
	searching   bool
	searchInput textInput

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

func (m *Model) visibleTasks() []*client.Task {
	if m.searching {
		return filterTasksByTitle(m.tasks, m.searchInput.String())
	}
	return m.tasks
}

func (m *Model) itemCount() int {
	if m.detail == nil {
		return 0
	}
	return len(m.detail.ChecklistItems)
}

func (m *Model) selectedTask() *client.Task {
	vis := m.visibleTasks()
	if len(vis) == 0 || m.listSel < 0 || m.listSel >= len(vis) {
		return nil
	}
	return vis[m.listSel]
}

func (m *Model) selectedItem() *client.ChecklistItem {
	if m.detail == nil || m.itemSel < 0 || m.itemSel >= len(m.detail.ChecklistItems) {
		return nil
	}
	return m.detail.ChecklistItems[m.itemSel]
}

func (m *Model) clampListSel() {
	n := len(m.visibleTasks())
	m.listSel = clamp(m.listSel, 0, maxInt(0, n-1))
}

func (m *Model) clampItemSel() {
	n := m.itemCount()
	m.itemSel = clamp(m.itemSel, 0, maxInt(0, n-1))
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

// -- pure search filter -----------------------------------------------------

// filterTasksByTitle keeps tasks whose title contains query (case-insensitive).
// An empty/blank query returns the slice unchanged. Pure and unit-tested.
func filterTasksByTitle(tasks []*client.Task, query string) []*client.Task {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return tasks
	}
	out := make([]*client.Task, 0, len(tasks))
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.Title), q) {
			out = append(out, t)
		}
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
