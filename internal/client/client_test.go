package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

// -- BaseURL ----------------------------------------------------------------

func TestBaseURL_EnvOverride(t *testing.T) {
	t.Setenv("SPRAWL_API_URL", "https://env.example")
	if got := BaseURL(); got != "https://env.example" {
		t.Fatalf("BaseURL = %q, want env value", got)
	}
}

func TestBaseURL_FallsBackToBuild(t *testing.T) {
	t.Setenv("SPRAWL_API_URL", "")
	want := strings.TrimRight(build.APIURL, "/")
	if got := BaseURL(); got != want {
		t.Fatalf("BaseURL = %q, want %q (from build.APIURL)", got, want)
	}
}

func TestBaseURL_StripsTrailingSlash(t *testing.T) {
	t.Setenv("SPRAWL_API_URL", "https://env.example/")
	if got := BaseURL(); got != "https://env.example" {
		t.Fatalf("BaseURL = %q, want trailing slash stripped", got)
	}
}

// -- Error types ------------------------------------------------------------

func TestAPIError_FormatsWithCode(t *testing.T) {
	e := &APIError{Status: 403, Code: "forbidden", Body: `{"error":"forbidden"}`}
	if got := e.Error(); got != "http 403: forbidden" {
		t.Fatalf("Error() = %q, want http 403: forbidden", got)
	}
}

func TestAPIError_FormatsWithoutCodeFallsBackToBody(t *testing.T) {
	e := &APIError{Status: 500, Body: "internal boom"}
	if got := e.Error(); got != "http 500: internal boom" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestDevicePollError_Format(t *testing.T) {
	e := &DevicePollError{Code: "authorization_pending"}
	if got := e.Error(); got != "device grant: authorization_pending" {
		t.Fatalf("Error() = %q", got)
	}
}

// -- Device flow ------------------------------------------------------------

func TestCreateDeviceGrant_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"device_code":               "dc",
			"user_code":                 "UC1",
			"verification_uri":          "https://x/auth/device",
			"verification_uri_complete": "https://x/auth/device?user_code=UC1",
			"expires_in":                600,
			"interval":                  5,
		})
	})
	c := New()
	g, err := c.CreateDeviceGrant(context.Background())
	if err != nil {
		t.Fatalf("CreateDeviceGrant: %v", err)
	}
	if g.DeviceCode != "dc" || g.UserCode != "UC1" || g.Interval != 5 || g.ExpiresIn != 600 {
		t.Fatalf("grant = %+v", g)
	}
	reqs := ts.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Method != "POST" || reqs[0].Path != "/api/auth/device" {
		t.Fatalf("request = %+v", reqs[0])
	}
	if reqs[0].Authorization != "" || reqs[0].AgentSecret != "" {
		t.Fatalf("unauth endpoint must not send auth headers: %+v", reqs[0])
	}
}

func TestPollDeviceToken_Success(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"token": "tok-abc"})
	})
	c := New()
	tok, err := c.PollDeviceToken(context.Background(), "dc")
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if tok != "tok-abc" {
		t.Fatalf("token = %q", tok)
	}
}

func TestPollDeviceToken_RFC8628Codes(t *testing.T) {
	// The server returns 400 for every non-success state; the client decodes
	// the `error` field and surfaces it as DevicePollError.
	for _, code := range []string{"authorization_pending", "access_denied", "expired_token", "invalid_grant"} {
		t.Run(code, func(t *testing.T) {
			code := code
			newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 400, map[string]string{"error": code})
			})
			c := New()
			_, err := c.PollDeviceToken(context.Background(), "dc")
			var dpe *DevicePollError
			if !errors.As(err, &dpe) {
				t.Fatalf("want *DevicePollError, got %T: %v", err, err)
			}
			if dpe.Code != code {
				t.Fatalf("DevicePollError.Code = %q, want %q", dpe.Code, code)
			}
		})
	}
}

func TestPollDeviceToken_EmptyBodyStillErrors(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{}) // no token, no error
	})
	c := New()
	_, err := c.PollDeviceToken(context.Background(), "dc")
	if err == nil {
		t.Fatal("expected error on empty success body")
	}
}

// -- Authed endpoints -------------------------------------------------------

func TestHealth_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	c := NewAuthed("tok", "sec")
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	reqs := ts.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Authorization != "Bearer tok" {
		t.Fatalf("Authorization = %q", reqs[0].Authorization)
	}
	if reqs[0].AgentSecret != "sec" {
		t.Fatalf("X-Agent-Secret = %q", reqs[0].AgentSecret)
	}
}

func TestHealth_401MissingBearer(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 401, "missing_bearer")
	})
	c := NewAuthed("tok", "sec")
	err := c.Health(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T: %v", err, err)
	}
	if ae.Status != 401 || ae.Code != "missing_bearer" {
		t.Fatalf("APIError = %+v", ae)
	}
}

func TestHealth_403InvalidAgentSecret(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 403, "invalid_agent_secret")
	})
	c := NewAuthed("tok", "sec")
	err := c.Health(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T", err)
	}
	if ae.Status != 403 || ae.Code != "invalid_agent_secret" {
		t.Fatalf("APIError = %+v", ae)
	}
}

// -- Theme ------------------------------------------------------------------

func TestGetTheme_Success(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"theme": "tokyo-night"})
	})
	c := NewAuthed("tok", "sec")
	id, err := c.GetTheme(context.Background())
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if id != "tokyo-night" {
		t.Fatalf("theme id = %q", id)
	}
}

func TestSetTheme_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"theme": "kanagawa"})
	})
	c := NewAuthed("tok", "sec")
	id, err := c.SetTheme(context.Background(), "kanagawa")
	if err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if id != "kanagawa" {
		t.Fatalf("theme id = %q", id)
	}
	reqs := ts.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request")
	}
	r := reqs[0]
	if r.Method != "PATCH" || r.Path != "/api/v1/settings/theme" {
		t.Fatalf("request = %+v", r)
	}
	if r.ContentType != "application/json" {
		t.Fatalf("Content-Type = %q", r.ContentType)
	}
	var body struct {
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// Client passes the arg through verbatim — no normalization.
	if body.Theme != "kanagawa" {
		t.Fatalf("body.theme = %q", body.Theme)
	}
}

// TestSetTheme_PassesArgVerbatim guards the "no client-side normalization"
// rule: an id with uppercase / spaces reaches the server exactly as typed
// so the server is the only place id validation lives.
func TestSetTheme_PassesArgVerbatim(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 404, "theme_not_found")
	})
	c := NewAuthed("tok", "sec")
	_, _ = c.SetTheme(context.Background(), "Tokyo Night")
	var body struct {
		Theme string `json:"theme"`
	}
	_ = json.Unmarshal(ts.Requests()[0].Body, &body)
	if body.Theme != "Tokyo Night" {
		t.Fatalf("body.theme = %q, want verbatim 'Tokyo Night'", body.Theme)
	}
}

func TestSetTheme_ErrorMatrix(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		code     string
		wantCode string
	}{
		{"theme_not_found", 404, "theme_not_found", "theme_not_found"},
		{"forbidden (non-owner)", 403, "forbidden", "forbidden"},
		{"invalid_agent_secret", 403, "invalid_agent_secret", "invalid_agent_secret"},
		{"missing_bearer", 401, "missing_bearer", "missing_bearer"},
		{"theme_required", 422, "theme_required", "theme_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeError(w, tc.status, tc.code)
			})
			c := NewAuthed("tok", "sec")
			_, err := c.SetTheme(context.Background(), "anything")
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("want APIError, got %T: %v", err, err)
			}
			if ae.Status != tc.status || ae.Code != tc.wantCode {
				t.Fatalf("APIError = %+v, want status %d code %q", ae, tc.status, tc.wantCode)
			}
		})
	}
}

// -- Phase 4: read endpoints -----------------------------------------------

func TestListTasks_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"tasks": []any{
				map[string]any{
					"id":                 1,
					"title":              "First",
					"description":        "",
					"status":             "not_started",
					"due_date":           nil,
					"project":            nil,
					"checklist_progress": map[string]any{"done": 0, "total": 0},
					"created_by":         nil,
					"last_actor":         nil,
				},
				map[string]any{
					"id":          2,
					"title":       "Second",
					"description": "deets",
					"status":      "in_progress",
					"due_date":    "2026-04-25",
					"project": map[string]any{
						"id": 7, "name": "Engineering", "color": "#112233",
					},
					"checklist_progress": map[string]any{"done": 1, "total": 3},
					"created_by":         map[string]any{"type": "user", "id": 42},
					"last_actor":         map[string]any{"type": "agent", "id": 9},
				},
			},
		})
	})
	c := NewAuthed("tok", "sec")
	tasks, err := c.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	if tasks[0].ID != 1 || tasks[0].Title != "First" || tasks[0].Project != nil || tasks[0].LastActor != nil {
		t.Fatalf("tasks[0] = %+v", tasks[0])
	}
	if tasks[1].Project == nil || tasks[1].Project.Name != "Engineering" {
		t.Fatalf("tasks[1].Project = %+v", tasks[1].Project)
	}
	if tasks[1].ChecklistProgress.Done != 1 || tasks[1].ChecklistProgress.Total != 3 {
		t.Fatalf("tasks[1].ChecklistProgress = %+v", tasks[1].ChecklistProgress)
	}
	if tasks[1].LastActor == nil || tasks[1].LastActor.Type != "agent" || tasks[1].LastActor.ID != 9 {
		t.Fatalf("tasks[1].LastActor = %+v", tasks[1].LastActor)
	}
	reqs := ts.Requests()
	if len(reqs) != 1 || reqs[0].Method != "GET" || reqs[0].Path != "/api/v1/tasks" {
		t.Fatalf("request = %+v", reqs[0])
	}
}

func TestGetTask_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"task": map[string]any{
				"id": 42, "title": "one", "description": "", "status": "done",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})
	c := NewAuthed("tok", "sec")
	task, err := c.GetTask(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.ID != 42 || task.Status != "done" {
		t.Fatalf("task = %+v", task)
	}
	if ts.Requests()[0].Path != "/api/v1/tasks/42" {
		t.Fatalf("path = %q", ts.Requests()[0].Path)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 404, "not_found")
	})
	c := NewAuthed("tok", "sec")
	_, err := c.GetTask(context.Background(), "999")
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T", err)
	}
	if ae.Status != 404 || ae.Code != "not_found" {
		t.Fatalf("APIError = %+v", ae)
	}
}

func TestGetTask_Forbidden(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 403, "forbidden")
	})
	c := NewAuthed("tok", "sec")
	_, err := c.GetTask(context.Background(), "1")
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T", err)
	}
	if ae.Status != 403 || ae.Code != "forbidden" {
		t.Fatalf("APIError = %+v", ae)
	}
}

func TestSearchTasks_EncodesQueryString(t *testing.T) {
	// Regression guard: the query must reach the server as a real query string,
	// not as a path-escaped segment. url.JoinPath would mangle this.
	var gotQuery, gotPath string
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		writeJSON(w, 200, map[string]any{"tasks": []any{}})
	})
	c := NewAuthed("tok", "sec")
	if _, err := c.SearchTasks(context.Background(), "hello world & friends"); err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if gotPath != "/api/v1/tasks/search" {
		t.Fatalf("path = %q", gotPath)
	}
	if got := url.QueryEscape("hello world & friends"); gotQuery != "q="+got {
		t.Fatalf("raw query = %q, want q=%s", gotQuery, got)
	}
}

func TestSearchTasks_EmptyQuery422(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Server echoes the documented 422 for blank q. The client doesn't
		// pre-validate; it surfaces the server's response unchanged.
		if r.URL.Query().Get("q") != "" {
			t.Errorf("expected empty q, got %q", r.URL.Query().Get("q"))
		}
		writeError(w, 422, "query_required")
	})
	c := NewAuthed("tok", "sec")
	_, err := c.SearchTasks(context.Background(), "")
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T", err)
	}
	if ae.Status != 422 || ae.Code != "query_required" {
		t.Fatalf("APIError = %+v", ae)
	}
}

func TestListChecklistItems_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"checklist_items": []any{
				map[string]any{
					"id": 5, "title": "a", "completed": false, "position": 0,
					"has_notes": false, "last_actor": nil,
				},
				map[string]any{
					"id": 6, "title": "b", "completed": true, "position": 1,
					"has_notes":  true,
					"last_actor": map[string]any{"type": "user", "id": 1},
				},
			},
		})
	})
	c := NewAuthed("tok", "sec")
	items, err := c.ListChecklistItems(context.Background(), "77")
	if err != nil {
		t.Fatalf("ListChecklistItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d", len(items))
	}
	if items[1].ID != 6 || !items[1].Completed || !items[1].HasNotes {
		t.Fatalf("items[1] = %+v", items[1])
	}
	if items[1].LastActor == nil || items[1].LastActor.Type != "user" {
		t.Fatalf("items[1].LastActor = %+v", items[1].LastActor)
	}
	if ts.Requests()[0].Path != "/api/v1/tasks/77/checklist" {
		t.Fatalf("path = %q", ts.Requests()[0].Path)
	}
}

func TestListChecklistItems_Forbidden(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 403, "forbidden")
	})
	c := NewAuthed("tok", "sec")
	_, err := c.ListChecklistItems(context.Background(), "3")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 403 || ae.Code != "forbidden" {
		t.Fatalf("APIError = %+v err %v", ae, err)
	}
}

func TestGetNotes_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"notes": "hello\nworld"})
	})
	c := NewAuthed("tok", "sec")
	notes, err := c.GetNotes(context.Background(), "9")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if notes != "hello\nworld" {
		t.Fatalf("notes = %q", notes)
	}
	if ts.Requests()[0].Path != "/api/v1/checklist_items/9/notes" {
		t.Fatalf("path = %q", ts.Requests()[0].Path)
	}
}

func TestGetNotes_EmptyStringIsValid(t *testing.T) {
	// A checklist item with no notes returns {"notes": ""} — that's success,
	// not an error.
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"notes": ""})
	})
	c := NewAuthed("tok", "sec")
	notes, err := c.GetNotes(context.Background(), "9")
	if err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if notes != "" {
		t.Fatalf("notes = %q, want empty", notes)
	}
}

func TestGetNotes_NotFound(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 404, "not_found")
	})
	c := NewAuthed("tok", "sec")
	_, err := c.GetNotes(context.Background(), "999")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 404 || ae.Code != "not_found" {
		t.Fatalf("APIError = %+v err %v", ae, err)
	}
}

// -- Phase 5: write endpoints ----------------------------------------------

func TestCreateTask_SendsTaskEnvelope(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		writeJSON(w, 201, map[string]any{
			"task": map[string]any{
				"id": 17, "title": "hello", "description": "d", "status": "not_started",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})
	c := NewAuthed("tok", "sec")
	task, err := c.CreateTask(context.Background(), map[string]any{
		"title":       "hello",
		"description": "d",
		"project_id":  5,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID != 17 || task.Title != "hello" {
		t.Fatalf("task = %+v", task)
	}
	reqs := ts.Requests()
	if len(reqs) != 1 || reqs[0].Method != "POST" || reqs[0].Path != "/api/v1/tasks" {
		t.Fatalf("request = %+v", reqs[0])
	}
	// Body must be wrapped in the `task` envelope — agents piping attrs must
	// not accidentally skip the wrapper.
	var sent struct {
		Task map[string]any `json:"task"`
	}
	if err := json.Unmarshal(reqs[0].Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if sent.Task["title"] != "hello" || sent.Task["description"] != "d" {
		t.Fatalf("task body = %+v", sent.Task)
	}
	// json numbers decode as float64.
	if got, _ := sent.Task["project_id"].(float64); int(got) != 5 {
		t.Fatalf("project_id = %v", sent.Task["project_id"])
	}
}

func TestCreateTask_ErrorMatrix(t *testing.T) {
	cases := []struct {
		name, code string
		status     int
	}{
		{"missing_bearer", "missing_bearer", 401},
		{"forbidden (non-owner / no create perm)", "forbidden", 403},
		{"project_not_found", "not_found", 404},
		{"title required", "title", 422},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeError(w, tc.status, tc.code)
			})
			c := NewAuthed("tok", "sec")
			_, err := c.CreateTask(context.Background(), map[string]any{"title": ""})
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("want APIError, got %T", err)
			}
			if ae.Status != tc.status || ae.Code != tc.code {
				t.Fatalf("APIError = %+v, want %d %q", ae, tc.status, tc.code)
			}
		})
	}
}

func TestUpdateTask_SendsTaskEnvelope(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"task": map[string]any{
				"id": 3, "title": "renamed", "description": "", "status": "in_progress",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 1, "total": 2},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})
	c := NewAuthed("tok", "sec")
	task, err := c.UpdateTask(context.Background(), "3", map[string]any{"title": "renamed"})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if task.ID != 3 || task.Title != "renamed" {
		t.Fatalf("task = %+v", task)
	}
	r := ts.Requests()[0]
	if r.Method != "PATCH" || r.Path != "/api/v1/tasks/3" {
		t.Fatalf("request = %+v", r)
	}
	var sent struct {
		Task map[string]any `json:"task"`
	}
	_ = json.Unmarshal(r.Body, &sent)
	if sent.Task["title"] != "renamed" {
		t.Fatalf("body = %+v", sent.Task)
	}
}

func TestUpdateTask_NotFound(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 404, "not_found")
	})
	c := NewAuthed("tok", "sec")
	_, err := c.UpdateTask(context.Background(), "999", map[string]any{"title": "x"})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("APIError = %+v", ae)
	}
}

func TestCreateChecklistItem_SendsItemEnvelope(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, map[string]any{
			"checklist_item": map[string]any{
				"id": 8, "title": "step", "completed": false, "position": 2,
				"has_notes": false, "last_actor": nil,
			},
		})
	})
	c := NewAuthed("tok", "sec")
	item, err := c.CreateChecklistItem(context.Background(), "4", map[string]any{"title": "step"})
	if err != nil {
		t.Fatalf("CreateChecklistItem: %v", err)
	}
	if item.ID != 8 || item.Title != "step" {
		t.Fatalf("item = %+v", item)
	}
	r := ts.Requests()[0]
	if r.Method != "POST" || r.Path != "/api/v1/tasks/4/checklist" {
		t.Fatalf("request = %+v", r)
	}
	var sent struct {
		Item map[string]any `json:"checklist_item"`
	}
	_ = json.Unmarshal(r.Body, &sent)
	if sent.Item["title"] != "step" {
		t.Fatalf("body = %+v", sent.Item)
	}
}

func TestSetChecklistItemCompleted_Check(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"checklist_item": map[string]any{
				"id": 8, "title": "step", "completed": true, "position": 2,
				"has_notes": false, "last_actor": nil,
			},
		})
	})
	c := NewAuthed("tok", "sec")
	item, err := c.SetChecklistItemCompleted(context.Background(), "8", true)
	if err != nil {
		t.Fatalf("SetChecklistItemCompleted: %v", err)
	}
	if !item.Completed {
		t.Fatalf("expected completed, got %+v", item)
	}
	r := ts.Requests()[0]
	if r.Method != "PATCH" || r.Path != "/api/v1/checklist_items/8/completed" {
		t.Fatalf("request = %+v", r)
	}
	var sent struct {
		Completed bool `json:"completed"`
	}
	if err := json.Unmarshal(r.Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !sent.Completed {
		t.Fatalf("body.completed = %v, want true", sent.Completed)
	}
}

func TestSetChecklistItemCompleted_Uncheck(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"checklist_item": map[string]any{
				"id": 8, "title": "step", "completed": false, "position": 2,
				"has_notes": false, "last_actor": nil,
			},
		})
	})
	c := NewAuthed("tok", "sec")
	item, err := c.SetChecklistItemCompleted(context.Background(), "8", false)
	if err != nil {
		t.Fatalf("SetChecklistItemCompleted: %v", err)
	}
	if item.Completed {
		t.Fatalf("expected not completed, got %+v", item)
	}
	r := ts.Requests()[0]
	if r.Method != "PATCH" || r.Path != "/api/v1/checklist_items/8/completed" {
		t.Fatalf("request = %+v", r)
	}
	var sent struct {
		// Pointer so we can distinguish "omitted" from "present and false".
		Completed *bool `json:"completed"`
	}
	if err := json.Unmarshal(r.Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if sent.Completed == nil || *sent.Completed {
		t.Fatalf("body.completed = %v, want explicit false", sent.Completed)
	}
}

func TestUpdateChecklistItem_SendsItemEnvelope(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"checklist_item": map[string]any{
				"id": 8, "title": "renamed", "completed": false, "position": 2,
				"has_notes": true, "last_actor": nil,
			},
		})
	})
	c := NewAuthed("tok", "sec")
	item, err := c.UpdateChecklistItem(context.Background(), "8", map[string]any{"title": "renamed"})
	if err != nil {
		t.Fatalf("UpdateChecklistItem: %v", err)
	}
	if item.Title != "renamed" {
		t.Fatalf("item = %+v", item)
	}
	r := ts.Requests()[0]
	if r.Method != "PATCH" || r.Path != "/api/v1/checklist_items/8" {
		t.Fatalf("request = %+v", r)
	}
	var sent struct {
		Item map[string]any `json:"checklist_item"`
	}
	_ = json.Unmarshal(r.Body, &sent)
	if sent.Item["title"] != "renamed" {
		t.Fatalf("body = %+v", sent.Item)
	}
}

func TestSetNotes_RoundTrip(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"notes": "line one\nline two"})
	})
	c := NewAuthed("tok", "sec")
	got, err := c.SetNotes(context.Background(), "9", "line one\nline two")
	if err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
	if got != "line one\nline two" {
		t.Fatalf("notes = %q", got)
	}
	r := ts.Requests()[0]
	if r.Method != "PUT" || r.Path != "/api/v1/checklist_items/9/notes" {
		t.Fatalf("request = %+v", r)
	}
	var sent struct {
		Notes string `json:"notes"`
	}
	_ = json.Unmarshal(r.Body, &sent)
	if sent.Notes != "line one\nline two" {
		t.Fatalf("body.notes = %q", sent.Notes)
	}
}

func TestSetNotes_EmptyStringAccepted(t *testing.T) {
	// Clearing notes is a valid operation — round-trip an empty string.
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"notes": ""})
	})
	c := NewAuthed("tok", "sec")
	if _, err := c.SetNotes(context.Background(), "9", ""); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}
	var sent struct {
		Notes string `json:"notes"`
	}
	_ = json.Unmarshal(ts.Requests()[0].Body, &sent)
	if sent.Notes != "" {
		t.Fatalf("body.notes = %q, want empty", sent.Notes)
	}
}

// -- Header invariants ------------------------------------------------------

// TestHeaderInvariants asserts the security-adjacent contract: every
// /api/v1/* call sends both Authorization and X-Agent-Secret; device-flow
// calls send neither; Content-Type is only set on requests with a body.
func TestHeaderInvariants(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/api/auth/device":
			writeJSON(w, 200, map[string]any{
				"device_code": "dc", "user_code": "UC", "verification_uri": "u",
				"verification_uri_complete": "u", "expires_in": 600, "interval": 5,
			})
		case p == "/api/auth/device/token":
			writeJSON(w, 200, map[string]string{"token": "tok"})
		case p == "/api/v1/health":
			writeJSON(w, 200, map[string]string{"status": "ok"})
		case p == "/api/v1/settings/theme":
			writeJSON(w, 200, map[string]string{"theme": "tokyo-night"})
		case p == "/api/v1/tasks":
			writeJSON(w, 200, map[string]any{"tasks": []any{}})
		case p == "/api/v1/tasks/search":
			writeJSON(w, 200, map[string]any{"tasks": []any{}})
		case strings.HasPrefix(p, "/api/v1/tasks/") && strings.HasSuffix(p, "/checklist"):
			// GET list or POST create — both return an items-shaped payload in
			// the appropriate envelope; reading the method here keeps the
			// fixture faithful to each endpoint's contract.
			if r.Method == http.MethodPost {
				writeJSON(w, 201, map[string]any{"checklist_item": map[string]any{
					"id": 1, "title": "x", "completed": false, "position": 0,
					"has_notes": false, "last_actor": nil,
				}})
			} else {
				writeJSON(w, 200, map[string]any{"checklist_items": []any{}})
			}
		case strings.HasPrefix(p, "/api/v1/tasks/"):
			writeJSON(w, 200, map[string]any{"task": map[string]any{
				"id": 1, "title": "t", "description": "", "status": "done",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			}})
		case strings.HasPrefix(p, "/api/v1/checklist_items/") && strings.HasSuffix(p, "/notes"):
			writeJSON(w, 200, map[string]any{"notes": ""})
		case strings.HasPrefix(p, "/api/v1/checklist_items/"):
			// set-completed or plain update — both return the item envelope.
			writeJSON(w, 200, map[string]any{"checklist_item": map[string]any{
				"id": 1, "title": "x", "completed": false, "position": 0,
				"has_notes": false, "last_actor": nil,
			}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(500)
		}
	})

	unauth := New()
	if _, err := unauth.CreateDeviceGrant(context.Background()); err != nil {
		t.Fatalf("CreateDeviceGrant: %v", err)
	}
	if _, err := unauth.PollDeviceToken(context.Background(), "dc"); err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}

	authed := NewAuthed("the-token", "the-secret")
	if err := authed.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if _, err := authed.GetTheme(context.Background()); err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if _, err := authed.SetTheme(context.Background(), "X"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if _, err := authed.ListTasks(context.Background()); err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if _, err := authed.SearchTasks(context.Background(), "x"); err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if _, err := authed.GetTask(context.Background(), "1"); err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := authed.ListChecklistItems(context.Background(), "1"); err != nil {
		t.Fatalf("ListChecklistItems: %v", err)
	}
	if _, err := authed.GetNotes(context.Background(), "9"); err != nil {
		t.Fatalf("GetNotes: %v", err)
	}
	if _, err := authed.CreateTask(context.Background(), map[string]any{"title": "t"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := authed.UpdateTask(context.Background(), "1", map[string]any{"title": "t"}); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if _, err := authed.CreateChecklistItem(context.Background(), "1", map[string]any{"title": "x"}); err != nil {
		t.Fatalf("CreateChecklistItem: %v", err)
	}
	if _, err := authed.SetChecklistItemCompleted(context.Background(), "1", true); err != nil {
		t.Fatalf("SetChecklistItemCompleted: %v", err)
	}
	if _, err := authed.UpdateChecklistItem(context.Background(), "1", map[string]any{"title": "x"}); err != nil {
		t.Fatalf("UpdateChecklistItem: %v", err)
	}
	if _, err := authed.SetNotes(context.Background(), "9", "x"); err != nil {
		t.Fatalf("SetNotes: %v", err)
	}

	for _, r := range ts.Requests() {
		if r.Accept != "application/json" {
			t.Errorf("%s %s: Accept = %q, want application/json", r.Method, r.Path, r.Accept)
		}
		switch {
		case strings.HasPrefix(r.Path, "/api/auth/"):
			if r.Authorization != "" {
				t.Errorf("%s %s: unexpected Authorization header", r.Method, r.Path)
			}
			if r.AgentSecret != "" {
				t.Errorf("%s %s: unexpected X-Agent-Secret header", r.Method, r.Path)
			}
		case strings.HasPrefix(r.Path, "/api/v1/"):
			if r.Authorization != "Bearer the-token" {
				t.Errorf("%s %s: Authorization = %q", r.Method, r.Path, r.Authorization)
			}
			if r.AgentSecret != "the-secret" {
				t.Errorf("%s %s: X-Agent-Secret = %q", r.Method, r.Path, r.AgentSecret)
			}
		default:
			t.Errorf("unexpected path bucket: %q", r.Path)
		}
		// Content-Type should only appear on requests with a body.
		hasBody := len(r.Body) > 0
		switch {
		case hasBody && r.ContentType != "application/json":
			t.Errorf("%s %s: body present but Content-Type = %q", r.Method, r.Path, r.ContentType)
		case !hasBody && r.ContentType != "":
			t.Errorf("%s %s: no body but Content-Type = %q", r.Method, r.Path, r.ContentType)
		}
	}
}

// -- Network / transport errors --------------------------------------------

func TestServerClosed_ReturnsNetworkError(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ts.Server.Close() // hang up before the request
	c := NewAuthed("tok", "sec")
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
	// Should not be surfaced as an APIError — there was no HTTP response.
	var ae *APIError
	if errors.As(err, &ae) {
		t.Fatalf("unexpected APIError: %+v", ae)
	}
}

func TestServer5xx_ReturnsAPIError(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("boom"))
	})
	c := NewAuthed("tok", "sec")
	err := c.Health(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T", err)
	}
	if ae.Status != 500 {
		t.Fatalf("status = %d", ae.Status)
	}
}
