package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSetChecklistItemState_SendsFlatBody(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"checklist_item": map[string]any{
			"id": 7, "title": "add the migration", "completed": false, "position": 1,
			"state": "in_review", "pr_number": 412, "has_notes": true,
		}})
	})
	c := NewAuthed("tok", "sec")

	item, err := c.SetChecklistItemState(context.Background(), "7", map[string]any{
		"state": "in_review", "pr_number": int64(412),
	})
	if err != nil {
		t.Fatalf("SetChecklistItemState: %v", err)
	}
	if item.State != "in_review" || item.PRNumber != 412 {
		t.Fatalf("decoded item = %+v", item)
	}

	req := ts.Requests()[0]
	if req.Method != http.MethodPatch || req.Path != "/api/v1/checklist_items/7/state" {
		t.Fatalf("request = %s %s", req.Method, req.Path)
	}
	// The body is flat — NOT wrapped in a `checklist_item` envelope like the
	// generic item PATCH. Sending the envelope would be silently ignored.
	var body map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, wrapped := body["checklist_item"]; wrapped {
		t.Fatalf("body must not be enveloped: %s", req.Body)
	}
	if body["state"] != "in_review" || body["pr_number"] != float64(412) {
		t.Fatalf("body = %s", req.Body)
	}
}

// A cleared field is a present key with a null value; an absent key means
// "leave unchanged". Conflating the two would silently wipe the other field.
func TestSetChecklistItemState_ClearSendsExplicitNull(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"checklist_item": map[string]any{
			"id": 7, "title": "x", "state": nil, "pr_number": 412,
		}})
	})
	c := NewAuthed("tok", "sec")

	item, err := c.SetChecklistItemState(context.Background(), "7", map[string]any{"state": nil})
	if err != nil {
		t.Fatalf("SetChecklistItemState: %v", err)
	}
	// A null state decodes to the empty string; the PR number is untouched.
	if item.State != "" || item.PRNumber != 412 {
		t.Fatalf("item = %+v", item)
	}

	var body map[string]any
	if err := json.Unmarshal(ts.Requests()[0].Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	v, present := body["state"]
	if !present || v != nil {
		t.Fatalf("want present-and-null state key, got %s", ts.Requests()[0].Body)
	}
	if _, present := body["pr_number"]; present {
		t.Fatalf("pr_number must be absent when unchanged: %s", ts.Requests()[0].Body)
	}
}

func TestSetChecklistItemState_InvalidStateIsAPIError(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 422, "invalid_state")
	})
	c := NewAuthed("tok", "sec")

	_, err := c.SetChecklistItemState(context.Background(), "7", map[string]any{"state": "nope"})
	var apiErr *APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.Status != 422 || apiErr.Code != "invalid_state" {
		t.Fatalf("err = %v", err)
	}
}

func TestListChecklistItemsByState(t *testing.T) {
	var gotQuery string
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("state")
		writeJSON(w, 200, map[string]any{"checklist_items": []any{
			map[string]any{
				"id": 7, "title": "add the migration", "completed": false, "position": 1,
				"state": "ready_to_pickup", "pr_number": nil, "has_notes": true,
				"task": map[string]any{
					"id": 3, "title": "Ship the API",
					"project": map[string]any{
						"id": 1, "name": "Sprawl", "key": "sprawl", "color": "#3B82F6",
						"github_url": "https://github.com/ultrakorne/sprawl",
					},
				},
			},
		}})
	})
	c := NewAuthed("tok", "sec")

	items, err := c.ListChecklistItemsByState(context.Background(), StateReadyToPickup)
	if err != nil {
		t.Fatalf("ListChecklistItemsByState: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d", len(items))
	}
	it := items[0]
	// Embedded item fields are promoted, and the task stub carries the project
	// that resolves PR links.
	if it.ID != 7 || it.State != StateReadyToPickup || it.PRNumber != 0 {
		t.Fatalf("item = %+v", it.ChecklistItem)
	}
	if it.Task.ID != 3 || it.Task.Project == nil || it.Task.Project.Key != "sprawl" {
		t.Fatalf("task stub = %+v", it.Task)
	}
	if it.Task.Project.GithubURL != "https://github.com/ultrakorne/sprawl" {
		t.Fatalf("github_url = %q", it.Task.Project.GithubURL)
	}

	req := ts.Requests()[0]
	if req.Method != http.MethodGet {
		t.Fatalf("method = %s", req.Method)
	}
	if req.Path != "/api/v1/checklist_items" {
		t.Fatalf("path = %q", req.Path)
	}
	if gotQuery != StateReadyToPickup {
		t.Fatalf("state query = %q", gotQuery)
	}
}

func TestPRURL(t *testing.T) {
	repo := &Project{ID: 1, Name: "Sprawl", GithubURL: "https://github.com/ultrakorne/sprawl"}
	if got := PRURL(repo, 412); got != "https://github.com/ultrakorne/sprawl/pull/412" {
		t.Fatalf("resolved = %q", got)
	}
	// Every way the chain can break yields "" — the caller renders the bare
	// number, which is not an error condition.
	if got := PRURL(nil, 412); got != "" {
		t.Fatalf("no project = %q", got)
	}
	if got := PRURL(&Project{ID: 1, Name: "Sprawl"}, 412); got != "" {
		t.Fatalf("no github_url = %q", got)
	}
	if got := PRURL(repo, 0); got != "" {
		t.Fatalf("no pr number = %q", got)
	}
}

func TestValidState(t *testing.T) {
	for _, s := range States {
		if !ValidState(s) {
			t.Errorf("ValidState(%q) = false", s)
		}
	}
	for _, s := range []string{"", "ready", "done", "in-review"} {
		if ValidState(s) {
			t.Errorf("ValidState(%q) = true", s)
		}
	}
}

// asAPIError is errors.As specialised, kept local so this file doesn't need the
// errors import for one call site.
func asAPIError(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}
