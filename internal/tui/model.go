package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// screen identifies a view in the back stack. esc pops one screen.
type screen int

const (
	screenSecret screen = iota
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
)

type pickerItem struct {
	label string
	value string
}

// Model is the bubbletea model for the interactive TUI.
type Model struct {
	ctx    context.Context
	styles styles

	newClient func(secret string) Client
	client    Client
	secret    string
	loggedIn  bool
	validated bool

	width, height int

	stack []screen

	// list data
	tasks       []*client.Task
	listSel     int
	searchLabel string // non-empty when the current list is a server-search result

	// checklist detail (full task with items + notes)
	detail  *client.Task
	itemSel int

	// note screen scroll offset (lines)
	noteOff int

	loading bool

	// secret prompt
	secretInput textInput
	secretErr   string
	secretBusy  bool

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
		ctx:       ctx,
		styles:    newStyles(),
		newClient: deps.NewClient,
		loggedIn:  deps.LoggedIn,
	}
	switch {
	case !deps.LoggedIn:
		m.stack = []screen{screenNotLoggedIn}
	case deps.SecretProvided && deps.Secret != "":
		// Secret supplied via flag/env: validate straight away by fetching the
		// list; a 401/403 drops to the masked prompt.
		m.secret = deps.Secret
		m.client = m.newClient(deps.Secret)
		m.stack = []screen{screenList}
		m.loading = true
		m.initCmd = validateAndListCmd(ctx, m.client)
	default:
		m.stack = []screen{screenSecret}
	}
	return m
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

// setTransient sets a footer status that auto-clears after ~2.5s. The token
// guards against a stale timer wiping a newer status.
func (m *Model) setTransient(text string) tea.Cmd {
	m.status = text
	m.statusErr = false
	m.statusTok++
	return clearStatusAfter(m.statusTok, 2500*time.Millisecond)
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
