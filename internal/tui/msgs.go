package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// -- messages ---------------------------------------------------------------

// tasksLoadedMsg carries a task-list (or search) result. validated marks the
// message as the successful outcome of a credential-validating fetch, which the
// model uses to leave the credentials prompt.
type tasksLoadedMsg struct {
	tasks     []*client.Task
	validated bool
}

// searchResultMsg carries server-side search results (title + item matches).
type searchResultMsg struct {
	tasks []*client.Task
	query string
}

// taskLoadedMsg carries a single full task (checklist + notes). forCopy routes
// it to the clipboard formatter instead of the checklist screen. wantID is the
// task id that was requested, so the reducer can drop a stale out-of-order
// response that no longer matches the open task.
type taskLoadedMsg struct {
	task    *client.Task
	wantID  int64
	forCopy bool
}

// itemToggledMsg is the server's confirmation of a completion toggle. prev is
// the pre-toggle state so a late failure could still reason about it.
type itemToggledMsg struct {
	item *client.ChecklistItem
}

// toggleFailedMsg reverts an optimistic toggle: itemID identifies the item,
// prev is the completion flag to restore, and prevState the item state that was
// cleared alongside it (completing an item clears its state, so the optimistic
// update drops both and the revert has to put both back).
type toggleFailedMsg struct {
	itemID    int64
	prev      bool
	prevState string
	err       error
}

// taskMutatedMsg is the result of create/update/set-due — a fresh task.
type taskMutatedMsg struct {
	task    *client.Task
	created bool
}

type taskDeletedMsg struct{ id int64 }

// itemMutatedMsg is the result of add/edit-title on a checklist item.
type itemMutatedMsg struct {
	item    *client.ChecklistItem
	created bool
}

type itemDeletedMsg struct{ id int64 }

// itemStateSetMsg is the server's confirmation of a state / PR-number write.
// label is the footer text for the specific change ("state → review", "PR
// #412"), decided when the command is built so the reducer stays generic.
type itemStateSetMsg struct {
	item  *client.ChecklistItem
	label string
}

// itemStateFailedMsg reports a failed state / PR write. The optimistic local
// change is resynced by re-fetching the task rather than by remembering a
// previous value: completion and state interact server-side (setting a state
// un-completes an item), so the server's copy is the only trustworthy one.
// context names which of the two writes failed — they share a route but not a
// keypress, so "set state" on a failed `p` would misdirect.
type itemStateFailedMsg struct {
	err     error
	context string
}

// notesSetMsg is the result of writing an item's note.
type notesSetMsg struct {
	itemID int64
	notes  *string
}

// errMsg is a non-fatal error surfaced on the footer status line.
type errMsg struct {
	err     error
	context string
}

// authFailedMsg drops the model to the credentials prompt: any 401/403 during
// credential validation, or a mid-session secret-auth failure (see
// isSecretAuthErr — 401 or a secret-coded 403).
type authFailedMsg struct{ err error }

// clearStatusMsg clears the transient footer status when tok still matches the
// current status token (so a newer status isn't wiped by a stale timer).
type clearStatusMsg struct{ tok int }

// editorDoneMsg carries the edited buffer back from an $EDITOR session.
type editorDoneMsg struct {
	kind editKind
	id   int64
	body string
	err  error
}

// editKind identifies which field an $EDITOR session edits.
type editKind int

const (
	editTaskDesc editKind = iota
	editItemNote
)

// -- error classification ---------------------------------------------------

// isAuthErr reports whether err is an APIError with a 401/403 status.
func isAuthErr(err error) bool {
	var ae *client.APIError
	if errors.As(err, &ae) {
		return ae.Status == 401 || ae.Status == 403
	}
	return false
}

// isSecretAuthErr reports whether err means the AGENT SECRET is bad / missing /
// revoked, so a mid-session failure should bounce back to the credentials prompt.
// Per the server's error contract that's a 401, or a 403 whose code names a
// secret problem. A plain 403 "forbidden" is an ordinary permission denial —
// NOT included — so it stays a footer error instead of demanding a new secret.
func isSecretAuthErr(err error) bool {
	var ae *client.APIError
	if !errors.As(err, &ae) {
		return false
	}
	if ae.Status == 401 {
		return true
	}
	if ae.Status == 403 {
		switch ae.Code {
		case "agent_secret_required", "invalid_agent_secret", "agent_key_revoked":
			return true
		}
	}
	return false
}

// validationErrMsg turns a validating fetch's error into the right message:
// 401 OR 403 → credentials prompt (spec: both are auth failures at validation
// time), where either narrowing factor can be re-entered.
func validationErrMsg(err error) tea.Msg {
	if isAuthErr(err) {
		return authFailedMsg{err}
	}
	return errMsg{err: err, context: "validate"}
}

// opErrMsg turns a post-validation op's error into the right message: a
// secret-auth failure (401, or a secret-coded 403) bounces to the credentials
// prompt so the user can re-enter a revoked/invalid secret; everything else (incl.
// a plain 403 "forbidden" permission error) stays a footer error.
func opErrMsg(err error, ctx string) tea.Msg {
	if isSecretAuthErr(err) {
		return authFailedMsg{err}
	}
	return errMsg{err: err, context: ctx}
}

// -- command builders -------------------------------------------------------

// validateAndListCmd performs the first authed call (task list) with the given
// client. On 401/403 it routes to the credentials prompt; on success it marks the
// result validated so the model leaves the prompt.
func validateAndListCmd(ctx context.Context, c Client) tea.Cmd {
	return func() tea.Msg {
		tasks, err := c.ListTasks(ctx)
		if err != nil {
			return validationErrMsg(err)
		}
		return tasksLoadedMsg{tasks: tasks, validated: true}
	}
}

func listTasksCmd(ctx context.Context, c Client) tea.Cmd {
	return func() tea.Msg {
		tasks, err := c.ListTasks(ctx)
		if err != nil {
			return opErrMsg(err, "load tasks")
		}
		return tasksLoadedMsg{tasks: tasks}
	}
}

func searchTasksCmd(ctx context.Context, c Client, query string) tea.Cmd {
	return func() tea.Msg {
		tasks, err := c.SearchTasks(ctx, query)
		if err != nil {
			return opErrMsg(err, "search")
		}
		return searchResultMsg{tasks: tasks, query: query}
	}
}

func getTaskCmd(ctx context.Context, c Client, id int64, forCopy bool) tea.Cmd {
	return func() tea.Msg {
		task, err := c.GetTask(ctx, itoa(id), true)
		if err != nil {
			return opErrMsg(err, "open task")
		}
		return taskLoadedMsg{task: task, wantID: id, forCopy: forCopy}
	}
}

func toggleItemCmd(ctx context.Context, c Client, itemID int64, want, prev bool, prevState string) tea.Cmd {
	return func() tea.Msg {
		item, err := c.SetChecklistItemCompleted(ctx, itoa(itemID), want)
		if err != nil {
			return toggleFailedMsg{itemID: itemID, prev: prev, prevState: prevState, err: err}
		}
		return itemToggledMsg{item: item}
	}
}

func createTaskCmd(ctx context.Context, c Client, attrs map[string]any) tea.Cmd {
	return func() tea.Msg {
		task, err := c.CreateTask(ctx, attrs)
		if err != nil {
			return opErrMsg(err, "create task")
		}
		return taskMutatedMsg{task: task, created: true}
	}
}

func updateTaskCmd(ctx context.Context, c Client, id int64, attrs map[string]any) tea.Cmd {
	return func() tea.Msg {
		task, err := c.UpdateTask(ctx, itoa(id), attrs)
		if err != nil {
			return opErrMsg(err, "update task")
		}
		return taskMutatedMsg{task: task}
	}
}

func setDueCmd(ctx context.Context, c Client, id int64, due *string) tea.Cmd {
	return func() tea.Msg {
		task, err := c.SetTaskDueDate(ctx, itoa(id), due)
		if err != nil {
			return opErrMsg(err, "set due")
		}
		return taskMutatedMsg{task: task}
	}
}

func deleteTaskCmd(ctx context.Context, c Client, id int64) tea.Cmd {
	return func() tea.Msg {
		if err := c.DeleteTask(ctx, itoa(id)); err != nil {
			if !isNotFound(err) {
				return opErrMsg(err, "delete task")
			}
		}
		return taskDeletedMsg{id: id}
	}
}

func addItemCmd(ctx context.Context, c Client, taskID int64, title string) tea.Cmd {
	return func() tea.Msg {
		item, err := c.CreateChecklistItem(ctx, itoa(taskID), map[string]any{"title": title})
		if err != nil {
			return opErrMsg(err, "add item")
		}
		return itemMutatedMsg{item: item, created: true}
	}
}

func updateItemCmd(ctx context.Context, c Client, itemID int64, title string) tea.Cmd {
	return func() tea.Msg {
		item, err := c.UpdateChecklistItem(ctx, itoa(itemID), map[string]any{"title": title})
		if err != nil {
			return opErrMsg(err, "edit item")
		}
		return itemMutatedMsg{item: item}
	}
}

// setItemStateCmd writes state and/or pr_number through the dedicated route.
// attrs is the flat body: a key present with nil clears that field, an absent
// key leaves it alone. label is the footer text on success, errCtx the prefix
// on failure.
func setItemStateCmd(ctx context.Context, c Client, itemID int64, attrs map[string]any, label, errCtx string) tea.Cmd {
	return func() tea.Msg {
		item, err := c.SetChecklistItemState(ctx, itoa(itemID), attrs)
		if err != nil {
			return itemStateFailedMsg{err: err, context: errCtx}
		}
		return itemStateSetMsg{item: item, label: label}
	}
}

// setNotesCmd writes an item's note through the generic item PATCH — the same
// route `item update --notes` uses. There is no notes-specific endpoint any
// more: a note is a field of an item, not an object of its own, so one route
// writes it.
//
// The response is the full item, which carries the saved note back (null once
// cleared) — that echo is what makes the write verifiable rather than assumed.
func setNotesCmd(ctx context.Context, c Client, itemID int64, notes string) tea.Cmd {
	return func() tea.Msg {
		item, err := c.UpdateChecklistItem(ctx, itoa(itemID), map[string]any{"notes": notes})
		if err != nil {
			return opErrMsg(err, "save note")
		}
		// Collapse an empty note to nil so the model sees one "empty ⇒ nil"
		// contract regardless of whether the server echoed null or "".
		saved := item.Notes
		if saved != nil && *saved == "" {
			saved = nil
		}
		return notesSetMsg{itemID: itemID, notes: saved}
	}
}

func deleteItemCmd(ctx context.Context, c Client, itemID int64) tea.Cmd {
	return func() tea.Msg {
		if err := c.DeleteChecklistItem(ctx, itoa(itemID)); err != nil {
			if !isNotFound(err) {
				return opErrMsg(err, "delete item")
			}
		}
		return itemDeletedMsg{id: itemID}
	}
}

// isNotFound mirrors the CLI's idempotent-delete rule: a 404 not_found is a
// successful no-op.
func isNotFound(err error) bool {
	var ae *client.APIError
	if errors.As(err, &ae) {
		return ae.Status == 404 && ae.Code == "not_found"
	}
	return false
}

// clearStatusAfter schedules a footer clear for the given status token.
func clearStatusAfter(tok int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearStatusMsg{tok: tok} })
}
