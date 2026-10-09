package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AssignmentEvent is one row of the server's assignment outbox: an agent key
// was given an item (`assigned`) or lost one (`removed`). Identifiers only —
// no titles, notes or the other side of a reassignment — so a consumer reads
// the item itself (`item <checklist_item_id>`) for content.
//
// AssignmentRevision is the item's revision after this transition; comparing it
// with the item's current `assignment_revision` says whether the event is still
// the latest word on that item. Cursor is set on feed entries only (the
// position just after this event) and empty on a single-event read.
type AssignmentEvent struct {
	ID                 int64  `json:"id"`
	Change             string `json:"change"`
	AssignmentRevision int64  `json:"assignment_revision"`
	WorkspaceID        int64  `json:"workspace_id"`
	TaskID             int64  `json:"task_id"`
	ChecklistItemID    int64  `json:"checklist_item_id"`
	Actor              *Actor `json:"actor"`
	OccurredAt         string `json:"occurred_at"`
	Cursor             string `json:"cursor,omitempty"`
}

// The two values of AssignmentEvent.Change.
const (
	ChangeAssigned = "assigned"
	ChangeRemoved  = "removed"
)

// AssignmentEventPage is one answer from the feed: the events after the
// requested cursor, oldest first, and the cursor to ask from next. NextCursor
// can move past events the caller never sees (filtered by workspace or project
// confinement), so it — not the last event's cursor — is where to resume.
// HasMore means the page was full and more are ready without waiting.
type AssignmentEventPage struct {
	Events     []*AssignmentEvent `json:"events"`
	NextCursor string             `json:"next_cursor"`
	HasMore    bool               `json:"has_more"`
}

// MaxEventWait is the longest the feed holds a request open, in seconds.
const MaxEventWait = 30

// eventPollSlack is how much longer than the requested wait the HTTP client
// gives a feed request before timing out: the server answers at `wait` at the
// latest, so anything past that is a stalled connection, not a slow answer.
const eventPollSlack = 15 * time.Second

// ListAssignmentEvents reads the caller's assignment feed:
// GET /api/v1/assignment_events?after=<cursor>&wait=<s>.
//
// An empty `after` asks for the head: no events and no wait, just the cursor
// that marks "now". Otherwise the server answers as soon as anything after the
// cursor exists, or after `wait` seconds with an empty page. The cursor is
// opaque and per agent key. Expiry surfaces as APIError 410 `cursor_expired`, a
// malformed or foreign cursor as 422 `invalid_cursor`.
//
// The request outlives the client's normal 15 s timeout by design, so this call
// runs on its own http.Client sized to the wait.
func (c *Client) ListAssignmentEvents(ctx context.Context, after string, wait int) (*AssignmentEventPage, error) {
	params := url.Values{}
	if after != "" {
		params.Set("after", after)
		params.Set("wait", strconv.Itoa(wait))
	}
	path := c.scoped("/assignment_events")
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	lp := *c
	lp.http = &http.Client{
		Timeout:   time.Duration(wait)*time.Second + eventPollSlack,
		Transport: c.http.Transport,
	}
	var page AssignmentEventPage
	if err := lp.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// assignmentEventEnvelope decodes the single-event read whether the server
// wraps it (`{"assignment_event": …}` or `{"event": …}`) or answers with the
// bare object — the embedded fields catch the bare form.
type assignmentEventEnvelope struct {
	Wrapped *AssignmentEvent `json:"assignment_event"`
	Event   *AssignmentEvent `json:"event"`
	AssignmentEvent
}

// errNoEvent is returned when a 2xx single-event read carries no event in any
// shape — a server older or newer than this CLI, never a missing event (that
// is a 404).
var errNoEvent = errors.New("the server's answer carried no assignment event")

// GetAssignmentEvent fetches one event by id: GET /api/v1/assignment_events/:id.
// It answers only for the caller's own events in the selected workspace (and
// confined project); anything else is a 404, never a 403, so the read can't be
// used to probe other keys' events. An `ae_` prefix (the MCP event id form) is
// accepted and stripped.
func (c *Client) GetAssignmentEvent(ctx context.Context, id string) (*AssignmentEvent, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "ae_")
	var env assignmentEventEnvelope
	if err := c.do(ctx, http.MethodGet, c.scoped("/assignment_events/"+url.PathEscape(id)), nil, &env); err != nil {
		return nil, err
	}
	switch {
	case env.Wrapped != nil:
		return env.Wrapped, nil
	case env.Event != nil:
		return env.Event, nil
	case env.ID != 0:
		return &env.AssignmentEvent, nil
	}
	return nil, errNoEvent
}
