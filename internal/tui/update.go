package tui

import (
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// errEmptyTask is surfaced when the server returns a 2xx whose body decodes to a
// nil task (e.g. `{"task": null}`), so the copy/drill-in paths report it instead
// of dereferencing nil and crashing the TUI.
var errEmptyTask = errors.New("empty task response")

// errBadPRNumber is the local refusal for a PR field that isn't a positive
// integer — the server's own rule, applied before the round-trip.
var errBadPRNumber = errors.New("a PR number must be a positive integer (empty clears it)")

// Update is the bubbletea reducer. It never blocks — every network call is a
// tea.Cmd, and every error arrives as a message routed to the footer, so the
// TUI can't crash on an API failure.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		// In v2 bracketed-paste arrives here, not as KeyPressMsg. Route it into
		// the active text field so pasting an agent secret / search / title works.
		m.handlePaste(msg.Content)
		return m, nil

	case tasksLoadedMsg:
		if m.stale(msg.from) {
			return m, nil
		}
		m.loading = false
		var cmd tea.Cmd
		if msg.validated {
			m.validated = true
			m.credBusy = false
			m.stack = []screen{screenList}
			// Credentials are good: learn which workspace this is, silently,
			// so the header can name it. Errors here are dropped.
			cmd = whoamiCmd(m.ctx, m.client, false)
		}
		m.tasks = msg.tasks
		m.searchLabel = ""
		m.clampListSel()
		return m, cmd

	case whoamiLoadedMsg:
		if m.stale(msg.from) {
			return m, nil
		}
		m.applyWhoami(msg.who)
		// The picker was asked for on the list; if the user has since drilled
		// into a task, don't drop an overlay on the wrong screen (and leave
		// `loading` to the fetch that owns it now).
		if msg.openPicker && m.current() == screenList {
			m.loading = false
			return m.openWorkspacePicker()
		}
		return m, nil

	case searchResultMsg:
		if m.stale(msg.from) {
			return m, nil
		}
		m.loading = false
		m.tasks = msg.tasks
		m.searchLabel = msg.query
		m.listSel = 0
		return m, nil

	case taskLoadedMsg:
		if msg.forCopy {
			m.loading = false
			if msg.task == nil {
				m.setError("copy task", errEmptyTask)
				return m, nil
			}
			md := taskMarkdown(msg.task)
			return m, tea.Batch(tea.SetClipboard(md), m.setTransient("✓ copied task #"+itoa(msg.task.ID)))
		}
		// Drop a stale out-of-order response (opened A, backed out, opened B):
		// leave `loading` set for the request that's still in flight.
		if msg.wantID != m.pendingTaskID {
			return m, nil
		}
		m.loading = false
		if msg.task == nil {
			m.setError("open task", errEmptyTask)
			if m.current() == screenChecklist || m.current() == screenNote {
				m.popTo(screenList)
			}
			m.detail = nil
			return m, nil
		}
		m.detail = msg.task
		m.clampItemSel()
		return m, nil

	case itemToggledMsg:
		// Take the server's whole item, not just `completed`: checking an item
		// CLEARS its state server-side, and copying two fields by hand left the
		// stale state on screen until something else refetched the task.
		m.replaceItem(msg.item)
		return m, nil

	case toggleFailedMsg:
		if m.detail != nil {
			for _, it := range m.detail.ChecklistItems {
				if it.ID == msg.itemID {
					it.Completed = msg.prev
					it.State = msg.prevState
					break
				}
			}
			m.detail.ChecklistProgress = recomputeProgress(m.detail.ChecklistItems)
			m.syncListProgress(m.detail.ID, m.detail.ChecklistProgress)
		}
		// A toggle that failed on a bad/revoked secret bounces to the
		// credentials prompt (the optimistic state was just reverted above).
		if isSecretAuthErr(msg.err) {
			return m.Update(authFailedMsg{err: msg.err})
		}
		m.setError("toggle", msg.err)
		return m, nil

	case taskMutatedMsg:
		return m.onTaskMutated(msg)

	case taskDeletedMsg:
		m.removeTask(msg.id)
		if m.detail != nil && m.detail.ID == msg.id {
			m.popTo(screenList)
			m.detail = nil
		}
		m.clampListSel()
		return m, m.setTransient("✓ deleted task #" + itoa(msg.id))

	case itemMutatedMsg:
		return m.onItemMutated(msg)

	case itemStateSetMsg:
		// The response is authoritative about `completed` too: setting a state on
		// a completed item un-completes it server-side, so progress has to be
		// recomputed from the item the server handed back.
		m.replaceItem(msg.item)
		return m, m.setTransient("✓ " + msg.label)

	case itemStateFailedMsg:
		if isSecretAuthErr(msg.err) {
			return m.Update(authFailedMsg{err: msg.err})
		}
		m.setError(msg.context, msg.err)
		// Resync: the optimistic change never reached the server, and state
		// interacts with completion, so re-read rather than guess.
		if m.detail != nil {
			m.pendingTaskID = m.detail.ID
			return m, getTaskCmd(m.ctx, m.client, m.detail.ID, false)
		}
		return m, nil

	case itemDeletedMsg:
		if m.detail != nil {
			m.detail.ChecklistItems = removeItemByID(m.detail.ChecklistItems, msg.id)
			m.detail.ChecklistProgress = recomputeProgress(m.detail.ChecklistItems)
			m.syncListProgress(m.detail.ID, m.detail.ChecklistProgress)
			m.clampItemSel()
		}
		return m, m.setTransient("✓ deleted item #" + itoa(msg.id))

	case notesSetMsg:
		if m.detail != nil {
			for _, it := range m.detail.ChecklistItems {
				if it.ID == msg.itemID {
					it.Notes = msg.notes
					it.HasNotes = msg.notes != nil
					break
				}
			}
		}
		return m, m.setTransient("✓ saved note")

	case editorDoneMsg:
		if msg.err != nil {
			m.setError("editor", msg.err)
			return m, nil
		}
		body := strings.TrimRight(msg.body, "\n")
		switch msg.kind {
		case editTaskDesc:
			m.loading = true
			return m, updateTaskCmd(m.ctx, m.client, msg.id, map[string]any{"description": body})
		case editItemNote:
			return m, setNotesCmd(m.ctx, m.client, msg.id, body)
		}
		return m, nil

	case errMsg:
		m.loading = false
		// On the credentials prompt a non-auth validation failure (500, timeout,
		// VPN blip) must re-enable the prompt — otherwise credBusy stays set and
		// every key but ctrl+c is dead. That screen shows credErr, not the
		// footer status, so surface it there.
		if m.current() == screenCreds {
			m.credBusy = false
			m.credErr = "couldn't validate: " + msg.err.Error()
			return m, nil
		}
		m.setError(msg.context, msg.err)
		// A failed drill-in leaves an empty detail screen on the stack; pop back.
		if (m.current() == screenChecklist || m.current() == screenNote) && m.detail == nil {
			m.popTo(screenList)
		}
		return m, nil

	case authFailedMsg:
		// Both narrowing factors are re-typeable at the prompt, so every auth
		// failure routes there. The rejected secret is dropped (it has to be
		// re-entered); the project key is kept in its field so a typo'd key can
		// be corrected rather than retyped from scratch.
		m.loading = false
		m.validated = false
		m.credBusy = false
		m.focusCreds()
		m.secret = ""
		m.client = nil
		m.detail = nil
		m.secretInput.reset()
		m.keyInput.setValue(m.projectKey)
		m.credErr = "credentials rejected — try again"
		m.stack = []screen{screenCreds}
		return m, nil

	case urlOpenedMsg:
		return m, m.setTransient("✓ opened " + msg.url)

	case urlOpenFailedMsg:
		// No browser to hand it to — the normal case over SSH. OSC 52 still
		// reaches the user's own machine, so the link isn't lost. One message,
		// not an error plus a message: the fallback worked.
		return m, tea.Batch(
			tea.SetClipboard(msg.url),
			m.setTransient("no browser here — copied "+msg.url),
		)

	case clearStatusMsg:
		if msg.tok == m.statusTok {
			m.status = ""
			m.statusErr = false
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) onTaskMutated(msg taskMutatedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.created {
		// Reload the list so ordering matches the server and the new task shows.
		m.loading = true
		return m, tea.Batch(
			listTasksCmd(m.ctx, m.client),
			m.setTransient("✓ created task #"+itoa(msg.task.ID)),
		)
	}
	m.upsertTask(msg.task)
	if m.detail != nil && m.detail.ID == msg.task.ID {
		m.detail.Title = msg.task.Title
		m.detail.Description = msg.task.Description
		m.detail.DueDate = msg.task.DueDate
		m.detail.Status = msg.task.Status
		m.detail.Project = msg.task.Project
	}
	return m, m.setTransient("✓ updated task #" + itoa(msg.task.ID))
}

func (m *Model) onItemMutated(msg itemMutatedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if m.detail == nil {
		return m, nil
	}
	if msg.created {
		m.detail.ChecklistItems = append(m.detail.ChecklistItems, msg.item)
		m.detail.ChecklistProgress = recomputeProgress(m.detail.ChecklistItems)
		m.syncListProgress(m.detail.ID, m.detail.ChecklistProgress)
	} else {
		m.replaceItem(msg.item)
	}
	m.clampItemSel()
	if msg.created {
		return m, m.setTransient("✓ added item #" + itoa(msg.item.ID))
	}
	return m, m.setTransient("✓ updated item #" + itoa(msg.item.ID))
}

// -- key routing ------------------------------------------------------------

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ctrl+c always quits, even inside inputs/overlays.
	if key == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	if m.overlay != ovNone {
		return m.handleOverlayKey(key)
	}

	switch m.current() {
	case screenCreds:
		return m.handleCredsKey(key)
	case screenNotLoggedIn:
		if key == "q" || key == "esc" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	case screenList:
		if m.searching {
			return m.handleSearchKey(key)
		}
		return m.handleBaseKey(key)
	default:
		return m.handleBaseKey(key)
	}
}

// handlePaste routes bracketed-paste text into whichever text field is focused.
// Ignored when no field is active (confirm/picker/help overlays, or a browse
// screen). The credentials prompt is included, so pasting a secret or a project
// key works.
func (m *Model) handlePaste(s string) {
	switch {
	case m.overlay == ovInput:
		m.input.insertString(s)
	case m.overlay != ovNone:
		// confirm / picker / help overlays have no text field
	case m.current() == screenCreds:
		if !m.credBusy {
			m.credInput().insertString(s)
		}
	case m.current() == screenList && m.searching:
		m.searchInput.insertString(s)
		m.clampListSel()
	}
}

// handleCredsKey drives the credentials prompt. The two fields are switched
// with tab / ↑↓; enter submits whatever is filled in. The server needs at least
// one narrowing factor, so an empty submit is refused here rather than sent.
func (m *Model) handleCredsKey(key string) (tea.Model, tea.Cmd) {
	if m.credBusy {
		return m, nil
	}
	switch key {
	case "esc":
		m.quitting = true
		return m, tea.Quit
	case "tab", "shift+tab", "up", "down":
		if m.credFocus == credSecret {
			m.credFocus = credProjectKey
		} else {
			m.credFocus = credSecret
		}
		return m, nil
	case "enter":
		secret := strings.TrimSpace(m.secretInput.String())
		// The server trims and case-folds the key anyway (see the CLI's
		// resolveProjectKey), so trim here too and don't second-guess the rest.
		projectKey := strings.TrimSpace(m.keyInput.String())
		if secret == "" && projectKey == "" {
			m.credErr = "enter an agent secret or a project key"
			return m, nil
		}
		m.secret = secret
		m.projectKey = projectKey
		m.client = m.newClient(secret, projectKey, m.workspace)
		m.credBusy = true
		m.credErr = ""
		return m, validateAndListCmd(m.ctx, m.client)
	default:
		m.credInput().handleKey(key)
		return m, nil
	}
}

func (m *Model) handleSearchKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.searching = false
		m.searchInput.reset()
		if m.searchLabel != "" {
			// A server search had replaced the list — restore the full list.
			m.searchLabel = ""
			m.loading = true
			return m, listTasksCmd(m.ctx, m.client)
		}
		m.clampListSel()
		return m, nil
	case "enter":
		q := strings.TrimSpace(m.searchInput.String())
		m.searching = false
		m.searchInput.reset()
		if q == "" {
			m.clampListSel()
			return m, nil
		}
		m.loading = true
		return m, searchTasksCmd(m.ctx, m.client, q)
	case "up":
		m.moveSel(-1)
		return m, nil
	case "down":
		m.moveSel(1)
		return m, nil
	default:
		m.searchInput.handleKey(key)
		m.clampListSel()
		return m, nil
	}
}

// handleBaseKey dispatches a key on the list / checklist / note screens.
func (m *Model) handleBaseKey(key string) (tea.Model, tea.Cmd) {
	switch dispatch(m.current(), key) {
	case actQuitHard, actQuit:
		m.quitting = true
		return m, tea.Quit
	case actBack:
		// esc on the list clears an active server-search (reloads the full list)
		// before it would pop — matching the spec's "esc clears" for search.
		if m.current() == screenList && m.searchLabel != "" {
			m.searchLabel = ""
			m.loading = true
			return m, listTasksCmd(m.ctx, m.client)
		}
		m.pop()
		return m, nil
	case actHelp:
		m.overlay = ovHelp
		return m, nil
	case actRefresh:
		return m.refresh()
	case actUp:
		m.moveSel(-1)
		return m, nil
	case actDown:
		m.moveSel(1)
		return m, nil
	case actTop:
		m.jumpSel(true)
		return m, nil
	case actBottom:
		m.jumpSel(false)
		return m, nil
	case actOpen:
		return m.open()
	case actToggle:
		return m.toggle()
	case actSearch:
		m.searching = true
		m.searchInput.reset()
		return m, nil
	case actCopy:
		return m.copy()
	case actNewTask:
		m.openInput(inNewTaskTitle, "New task title", "")
		return m, nil
	case actEditTitle:
		return m.editTitle()
	case actEditDesc:
		return m.editDescription()
	case actSetDue:
		return m.openDuePicker()
	case actDelete:
		return m.openDelete()
	case actAddItem:
		m.openInput(inAddItem, "New item title", "")
		return m, nil
	case actEditNote:
		return m.editNote()
	case actCycleState:
		return m.cycleState()
	case actSetPR:
		return m.openPRInput()
	case actOpenPR:
		return m.openPRLink()
	case actWorkspace:
		return m.pickWorkspace()
	}
	return m, nil
}

// pickWorkspace is the `w` key: refetch the workspace picture (it's one cheap
// call and the list may have changed since startup), then open the picker on
// the fresh data. Under a project key the workspace is pinned by the key —
// the server refuses any other with workspace_mismatch — so say that instead
// of offering a choice that can only fail.
func (m *Model) pickWorkspace() (tea.Model, tea.Cmd) {
	if m.projectKey != "" {
		return m, m.setTransient("project key " + m.projectKey + " pins the workspace — unset it to switch")
	}
	m.loading = true
	return m, whoamiCmd(m.ctx, m.client, true)
}

// openWorkspacePicker shows the reachable workspaces, cursor on the current
// one. A server that reports none (pre-workspaces) gets a footer line.
func (m *Model) openWorkspacePicker() (tea.Model, tea.Cmd) {
	if len(m.wsList) == 0 {
		return m, m.setTransient("this server doesn't report workspaces")
	}
	m.overlay = ovPicker
	m.pickerKind = pickWorkspace
	m.pickerTitle = "Switch workspace"
	m.pickerItems = workspacePickerItems(m.wsList, m.wsCurrent)
	m.pickerSel = 0
	for i := range m.wsList {
		if m.wsCurrent != nil && m.wsList[i].ID == m.wsCurrent.ID {
			m.pickerSel = i
		}
	}
	return m, nil
}

// switchWorkspace re-pins the session on workspace `id`: a new client with
// the same factors and the new selector, then a clean list — every loaded
// task and item belongs to the old workspace and its ids mean nothing in the
// new one, so nothing carries over. The choice lives in memory only.
func (m *Model) switchWorkspace(id string) (tea.Model, tea.Cmd) {
	var target *client.Workspace
	for i := range m.wsList {
		if itoa(m.wsList[i].ID) == id {
			target = &m.wsList[i]
		}
	}
	if target == nil || (m.wsCurrent != nil && target.ID == m.wsCurrent.ID) {
		return m, nil
	}
	m.workspace = id
	m.wsCurrent = target
	m.client = m.newClient(m.secret, m.projectKey, id)
	m.tasks = nil
	m.detail = nil
	m.pendingTaskID = 0
	m.listSel = 0
	m.searching = false
	m.searchLabel = ""
	m.stack = []screen{screenList}
	m.loading = true
	status := "✓ switched to " + wsName(target)
	if target.Level == "" || target.Level == "none" {
		// The switch itself succeeds server-side; the policy just caps this
		// key at none there, so the list will be empty. Say why up front.
		status = "switched to " + wsName(target) + " — this agent key has no access there, lists will be empty"
	}
	return m, tea.Batch(listTasksCmd(m.ctx, m.client), m.setTransient(status))
}

// -- navigation -------------------------------------------------------------

func (m *Model) moveSel(delta int) {
	switch m.current() {
	case screenList:
		n := len(m.visibleTasks())
		if n > 0 {
			m.listSel = clamp(m.listSel+delta, 0, n-1)
		}
	case screenChecklist:
		n := m.itemCount()
		if n > 0 {
			m.itemSel = clamp(m.itemSel+delta, 0, n-1)
		}
	case screenNote:
		m.noteOff = clamp(m.noteOff+delta, 0, m.noteMaxOff())
	}
}

func (m *Model) jumpSel(top bool) {
	switch m.current() {
	case screenList:
		if top {
			m.listSel = 0
		} else {
			m.listSel = maxInt(0, len(m.visibleTasks())-1)
		}
	case screenChecklist:
		if top {
			m.itemSel = 0
		} else {
			m.itemSel = maxInt(0, m.itemCount()-1)
		}
	case screenNote:
		if top {
			m.noteOff = 0
		} else {
			m.noteOff = m.noteMaxOff()
		}
	}
}

// -- actions ----------------------------------------------------------------

func (m *Model) refresh() (tea.Model, tea.Cmd) {
	switch m.current() {
	case screenList:
		m.loading = true
		if m.searchLabel != "" {
			return m, searchTasksCmd(m.ctx, m.client, m.searchLabel)
		}
		return m, listTasksCmd(m.ctx, m.client)
	case screenChecklist, screenNote:
		if m.detail == nil {
			return m, nil
		}
		m.loading = true
		m.pendingTaskID = m.detail.ID
		return m, getTaskCmd(m.ctx, m.client, m.detail.ID, false)
	}
	return m, nil
}

func (m *Model) open() (tea.Model, tea.Cmd) {
	switch m.current() {
	case screenList:
		t := m.selectedTask()
		if t == nil {
			return m, nil
		}
		m.detail = nil
		m.itemSel = 0
		m.loading = true
		m.pendingTaskID = t.ID
		m.push(screenChecklist)
		return m, getTaskCmd(m.ctx, m.client, t.ID, false)
	case screenChecklist:
		if m.selectedItem() == nil {
			return m, nil
		}
		m.noteOff = 0
		m.push(screenNote)
		return m, nil
	}
	return m, nil
}

func (m *Model) toggle() (tea.Model, tea.Cmd) {
	it := m.selectedItem()
	if it == nil {
		return m, nil
	}
	prev, prevState := it.Completed, it.State
	want := !prev
	it.Completed = want // optimistic
	if want {
		// Completing clears the state server-side, so drop it here too rather
		// than leaving a stale icon on the row until the response lands.
		// Unchecking does NOT bring it back — the server doesn't remember it.
		it.State = ""
	}
	m.detail.ChecklistProgress = recomputeProgress(m.detail.ChecklistItems)
	m.syncListProgress(m.detail.ID, m.detail.ChecklistProgress)
	return m, toggleItemCmd(m.ctx, m.client, it.ID, want, prev, prevState)
}

// cycleState advances the selected item's state one step and writes it. The
// local copy is updated immediately so repeated presses cycle (rather than
// re-sending the same next-state from a stale value); a failure resyncs from
// the server. Setting a state also un-completes the item server-side, which the
// optimistic copy mirrors so progress doesn't visibly jump on the response.
func (m *Model) cycleState() (tea.Model, tea.Cmd) {
	it := m.selectedItem()
	if it == nil {
		return m, nil
	}
	next := nextState(it.State)
	it.State = next
	label := "state cleared on #" + itoa(it.ID)
	if next != "" {
		it.Completed = false
		label = "#" + itoa(it.ID) + " → " + stateLabel(next)
	}
	m.detail.ChecklistProgress = recomputeProgress(m.detail.ChecklistItems)
	m.syncListProgress(m.detail.ID, m.detail.ChecklistProgress)

	// A cleared state must still be sent as an explicit null: an absent key
	// means "leave unchanged".
	var val any
	if next != "" {
		val = next
	}
	return m, setItemStateCmd(m.ctx, m.client, it.ID, map[string]any{"state": val}, label, "set state")
}

// openPRInput prompts for the selected item's PR number, prefilled with the
// current one. Submitting an empty value clears it.
func (m *Model) openPRInput() (tea.Model, tea.Cmd) {
	it := m.selectedItem()
	if it == nil {
		return m, nil
	}
	prefill := ""
	if it.PRNumber > 0 {
		prefill = itoa(it.PRNumber)
	}
	m.inputTargetID = it.ID
	m.openInput(inSetPR, "PR number for #"+itoa(it.ID)+" (empty to clear)", prefill)
	return m, nil
}

// openPRLink opens the selected item's pull request in a browser. Both ways the
// link can fail to resolve are ordinary states, not errors, so each gets a
// specific line rather than a stack of "something went wrong".
func (m *Model) openPRLink() (tea.Model, tea.Cmd) {
	it := m.selectedItem()
	if it == nil {
		return m, nil
	}
	if it.PRNumber <= 0 {
		return m, m.setTransient("#" + itoa(it.ID) + " has no PR number — press p to set one")
	}
	url := ""
	if m.detail != nil {
		url = client.PRURL(m.detail.Project, it.PRNumber)
	}
	if url == "" {
		return m, m.setTransient("PR #" + itoa(it.PRNumber) + " — this task's project has no GitHub URL to open it against")
	}
	return m, openURLCmd(url)
}

func (m *Model) copy() (tea.Model, tea.Cmd) {
	switch m.current() {
	case screenList:
		t := m.selectedTask()
		if t == nil {
			return m, nil
		}
		// The list never carries checklist items; fetch the full task, then copy.
		return m, getTaskCmd(m.ctx, m.client, t.ID, true)
	case screenChecklist, screenNote:
		it := m.selectedItem()
		if it == nil {
			return m, nil
		}
		md := itemMarkdown(it, m.detail)
		return m, tea.Batch(tea.SetClipboard(md), m.setTransient("✓ copied item #"+itoa(it.ID)))
	}
	return m, nil
}

func (m *Model) editTitle() (tea.Model, tea.Cmd) {
	switch m.current() {
	case screenList:
		t := m.selectedTask()
		if t == nil {
			return m, nil
		}
		m.inputTargetID = t.ID
		m.openInput(inEditTaskTitle, "Edit task title", t.Title)
	case screenChecklist:
		it := m.selectedItem()
		if it == nil {
			return m, nil
		}
		m.inputTargetID = it.ID
		m.openInput(inEditItemTitle, "Edit item title", it.Title)
	}
	return m, nil
}

func (m *Model) editDescription() (tea.Model, tea.Cmd) {
	t := m.selectedTask()
	if t == nil {
		return m, nil
	}
	return m, editInEditorCmd(editTaskDesc, t.ID, t.Description)
}

func (m *Model) editNote() (tea.Model, tea.Cmd) {
	it := m.selectedItem()
	if it == nil {
		return m, nil
	}
	body := ""
	if it.Notes != nil {
		body = *it.Notes
	}
	return m, editInEditorCmd(editItemNote, it.ID, body)
}

func (m *Model) openDuePicker() (tea.Model, tea.Cmd) {
	t := m.selectedTask()
	if t == nil {
		return m, nil
	}
	m.inputTargetID = t.ID
	m.overlay = ovPicker
	m.pickerKind = pickDue
	m.pickerTitle = "Set due date for #" + itoa(t.ID)
	m.pickerItems = []pickerItem{
		{label: "yesterday", value: "yesterday"},
		{label: "today", value: "today"},
		{label: "this week", value: "week"},
		{label: "clear (none)", value: "none"},
	}
	m.pickerSel = 0
	return m, nil
}

func (m *Model) openDelete() (tea.Model, tea.Cmd) {
	switch m.current() {
	case screenList:
		t := m.selectedTask()
		if t == nil {
			return m, nil
		}
		m.overlay = ovConfirm
		m.confirmKind = confirmDeleteTask
		m.confirmID = t.ID
		m.confirmName = t.Title
	case screenChecklist:
		it := m.selectedItem()
		if it == nil {
			return m, nil
		}
		m.overlay = ovConfirm
		m.confirmKind = confirmDeleteItem
		m.confirmID = it.ID
		m.confirmName = it.Title
	}
	return m, nil
}

func (m *Model) openInput(kind inputKind, prompt, prefill string) {
	m.overlay = ovInput
	m.inputKind = kind
	m.inputPrompt = prompt
	m.input.reset()
	if prefill != "" {
		m.input.setValue(prefill)
	}
}

// -- overlay key routing ----------------------------------------------------

func (m *Model) handleOverlayKey(key string) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case ovHelp:
		m.overlay = ovNone
		return m, nil

	case ovConfirm:
		switch key {
		case "y", "Y", "enter":
			return m.confirmYes()
		case "n", "N", "esc":
			m.overlay = ovNone
		}
		return m, nil

	case ovInput:
		switch key {
		case "esc":
			m.overlay = ovNone
			m.input.reset()
			return m, nil
		case "enter":
			return m.submitInput()
		default:
			m.input.handleKey(key)
			return m, nil
		}

	case ovPicker:
		switch key {
		case "esc":
			m.overlay = ovNone
			return m, nil
		case "up", "k":
			if m.pickerSel > 0 {
				m.pickerSel--
			}
			return m, nil
		case "down", "j":
			if m.pickerSel < len(m.pickerItems)-1 {
				m.pickerSel++
			}
			return m, nil
		case "enter":
			return m.submitPicker()
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) confirmYes() (tea.Model, tea.Cmd) {
	kind := m.confirmKind
	id := m.confirmID
	m.overlay = ovNone
	switch kind {
	case confirmDeleteTask:
		return m, deleteTaskCmd(m.ctx, m.client, id)
	case confirmDeleteItem:
		return m, deleteItemCmd(m.ctx, m.client, id)
	}
	return m, nil
}

func (m *Model) submitInput() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.input.String())
	kind := m.inputKind
	target := m.inputTargetID
	m.overlay = ovNone
	m.input.reset()

	switch kind {
	case inNewTaskTitle:
		if val == "" {
			return m, nil
		}
		if projs := m.distinctProjects(); len(projs) > 0 {
			m.pendingTaskTitle = val
			m.overlay = ovPicker
			m.pickerKind = pickProject
			m.pickerTitle = "Project for new task"
			m.pickerItems = append([]pickerItem{{label: "(none)", value: ""}}, projs...)
			m.pickerSel = 0
			return m, nil
		}
		m.loading = true
		return m, createTaskCmd(m.ctx, m.client, map[string]any{"title": val})
	case inEditTaskTitle:
		if val == "" {
			return m, nil
		}
		return m, updateTaskCmd(m.ctx, m.client, target, map[string]any{"title": val})
	case inAddItem:
		if val == "" || m.detail == nil {
			return m, nil
		}
		return m, addItemCmd(m.ctx, m.client, m.detail.ID, val)
	case inEditItemTitle:
		if val == "" {
			return m, nil
		}
		return m, updateItemCmd(m.ctx, m.client, target, val)
	case inSetPR:
		return m.submitPR(target, val)
	}
	return m, nil
}

// submitPR validates the typed PR number and writes it. Empty (or `none`)
// clears; anything that isn't a positive integer is refused locally with a
// footer error rather than sent for the server to 422.
func (m *Model) submitPR(itemID int64, val string) (tea.Model, tea.Cmd) {
	var body any
	label := "PR cleared on #" + itoa(itemID)
	if val != "" && !strings.EqualFold(val, "none") {
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil || n <= 0 {
			m.setError("set PR", errBadPRNumber)
			return m, nil
		}
		body = n
		label = "#" + itoa(itemID) + " → PR #" + itoa(n)
		// The task's project resolves the link; surfacing it confirms the number
		// landed somewhere real without another screen. No project or no
		// github_url just means no link — the number still stands.
		if m.detail != nil {
			if u := client.PRURL(m.detail.Project, n); u != "" {
				label += " · " + u
			}
		}
	}
	// The local copy is updated on the response (itemStateSetMsg) — unlike the
	// state cycle there's nothing to keep in sync between rapid keypresses.
	return m, setItemStateCmd(m.ctx, m.client, itemID, map[string]any{"pr_number": body}, label, "set PR")
}

func (m *Model) submitPicker() (tea.Model, tea.Cmd) {
	if m.pickerSel < 0 || m.pickerSel >= len(m.pickerItems) {
		m.overlay = ovNone
		return m, nil
	}
	sel := m.pickerItems[m.pickerSel]
	kind := m.pickerKind
	m.overlay = ovNone

	switch kind {
	case pickDue:
		var due *string
		if sel.value != "none" {
			v := sel.value
			due = &v
		}
		return m, setDueCmd(m.ctx, m.client, m.inputTargetID, due)
	case pickWorkspace:
		return m.switchWorkspace(sel.value)
	case pickProject:
		attrs := map[string]any{"title": m.pendingTaskTitle}
		m.pendingTaskTitle = ""
		if sel.value != "" {
			if id, err := strconv.ParseInt(sel.value, 10, 64); err == nil {
				attrs["project_id"] = id
			}
		}
		m.loading = true
		return m, createTaskCmd(m.ctx, m.client, attrs)
	}
	return m, nil
}
