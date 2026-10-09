package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func eventBody(id int64, cursor string) map[string]any {
	m := map[string]any{
		"id": id, "change": "assigned", "assignment_revision": 4, "workspace_id": 7,
		"task_id": 1001, "checklist_item_id": 5555, "actor": map[string]any{"type": "user", "id": 3},
		"occurred_at": "2026-10-09T12:00:00.123456Z",
	}
	if cursor != "" {
		m["cursor"] = cursor
	}
	return m
}

// No cursor asks for the head: no `after`, no `wait`, just the cursor for now.
func TestListAssignmentEvents_HeadSendsNoParams(t *testing.T) {
	var query url.Values
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(w, 200, map[string]any{"events": []any{}, "next_cursor": "head", "has_more": false})
	})
	page, err := NewAuthed("tok", "sek").ListAssignmentEvents(context.Background(), "", 25)
	if err != nil {
		t.Fatalf("ListAssignmentEvents: %v", err)
	}
	if len(query) != 0 {
		t.Fatalf("head request sent params %v", query)
	}
	if page.NextCursor != "head" || len(page.Events) != 0 {
		t.Fatalf("page = %+v", page)
	}
}

func TestListAssignmentEvents_AfterAndWaitDecodePage(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"events":      []any{eventBody(123, "c123"), eventBody(124, "c124")},
			"next_cursor": "c130", "has_more": true,
		})
	})
	page, err := NewAuthed("tok", "sek").ListAssignmentEvents(context.Background(), "c100", 25)
	if err != nil {
		t.Fatalf("ListAssignmentEvents: %v", err)
	}
	req := ts.Requests()[0]
	if req.Path != "/api/v1/assignment_events" || req.Authorization != "Bearer tok" || req.AgentSecret != "sek" {
		t.Fatalf("request = %+v", req)
	}
	if !page.HasMore || page.NextCursor != "c130" || len(page.Events) != 2 {
		t.Fatalf("page = %+v", page)
	}
	ev := page.Events[0]
	if ev.ID != 123 || ev.Change != ChangeAssigned || ev.AssignmentRevision != 4 || ev.WorkspaceID != 7 ||
		ev.TaskID != 1001 || ev.ChecklistItemID != 5555 || ev.Cursor != "c123" ||
		ev.Actor == nil || ev.Actor.Type != "user" || ev.Actor.ID != 3 ||
		ev.OccurredAt != "2026-10-09T12:00:00.123456Z" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestListAssignmentEvents_QueryParams(t *testing.T) {
	var query url.Values
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeJSON(w, 200, map[string]any{"events": []any{}, "next_cursor": "c"})
	})
	if _, err := NewAuthed("tok", "sek").ListAssignmentEvents(context.Background(), "abc_-", 7); err != nil {
		t.Fatal(err)
	}
	if query.Get("after") != "abc_-" || query.Get("wait") != "7" || query.Has("limit") {
		t.Fatalf("query = %v", query)
	}
}

func TestListAssignmentEvents_ExpiredCursorIsAPIError410(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 410, "cursor_expired")
	})
	_, err := NewAuthed("tok", "sek").ListAssignmentEvents(context.Background(), "old", 25)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 410 || apiErr.Code != "cursor_expired" {
		t.Fatalf("err = %v", err)
	}
}

// The single-event read is decoded whichever way the server wraps it, and the
// MCP id form `ae_<id>` addresses the same event.
func TestGetAssignmentEvent_Shapes(t *testing.T) {
	for _, tc := range []struct {
		name, id, wantPath string
		body               map[string]any
	}{
		{"assignment_event envelope", "123", "/api/v1/assignment_events/123", map[string]any{"assignment_event": eventBody(123, "")}},
		{"event envelope", "123", "/api/v1/assignment_events/123", map[string]any{"event": eventBody(123, "")}},
		{"bare", "123", "/api/v1/assignment_events/123", eventBody(123, "")},
		{"mcp id", "ae_123", "/api/v1/assignment_events/123", map[string]any{"event": eventBody(123, "")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, tc.body) })
			ev, err := NewAuthed("tok", "sek").GetAssignmentEvent(context.Background(), tc.id)
			if err != nil {
				t.Fatalf("GetAssignmentEvent: %v", err)
			}
			if got := ts.Requests()[0].Path; got != tc.wantPath {
				t.Fatalf("path = %q", got)
			}
			if ev.ID != 123 || ev.ChecklistItemID != 5555 || ev.Change != ChangeAssigned {
				t.Fatalf("event = %+v", ev)
			}
		})
	}
}

func TestGetAssignmentEvent_EmptyAnswerAndNotFound(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{}) })
	if _, err := NewAuthed("tok", "sek").GetAssignmentEvent(context.Background(), "1"); !errors.Is(err, errNoEvent) {
		t.Fatalf("empty answer: err = %v", err)
	}

	newTestServer(t, func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "not_found") })
	_, err := NewAuthed("tok", "sek").GetAssignmentEvent(context.Background(), "1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("404: err = %v", err)
	}
}

func TestListQueue_AssigneeParams(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    QueueFilter
		want url.Values
	}{
		{"assignee only", QueueFilter{Assignee: AssigneeMe}, url.Values{"assignee": {"me"}}},
		{"assignee and state", QueueFilter{Assignee: AssigneeMe, State: StateInReview, Full: true},
			url.Values{"assignee": {"me"}, "state": {"in_review"}, "full": {"true"}}},
		{"state only", QueueFilter{State: StateReadyToPickup}, url.Values{"state": {"ready_to_pickup"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got url.Values
			newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query()
				writeJSON(w, 200, map[string]any{"tasks": []any{}})
			})
			if _, err := NewAuthed("tok", "sek").ListQueue(context.Background(), tc.f); err != nil {
				t.Fatal(err)
			}
			if got.Encode() != tc.want.Encode() {
				t.Fatalf("query = %v, want %v", got, tc.want)
			}
		})
	}
}
