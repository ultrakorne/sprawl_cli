package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// authedFixture stitches together the preconditions every `run*` test needs:
// a scratch XDG config dir with a token, SPRAWL_API_URL pointing at the mock
// server, an httptest.Server, and a runtimeOpts carrying an agent secret +
// format. Each caller's handler shapes the response; the fixture owns auth.
type authedFixture struct {
	Server *httptest.Server
	Opts   *runtimeOpts
}

func newAuthedFixture(t *testing.T, format string, handler http.HandlerFunc) *authedFixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := config.Save(build.AppName, &config.Config{Token: "the-token"}); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	t.Setenv("SPRAWL_TOKEN", "the-token")
	t.Setenv("SPRAWL_AGENT_SECRET", "")

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	return &authedFixture{
		Server: srv,
		Opts:   &runtimeOpts{format: format, agentSecret: "the-secret"},
	}
}

// -- pure text-format helpers ----------------------------------------------

func TestTaskListText_Empty(t *testing.T) {
	got := taskListText(nil)
	if got != "(no tasks)" {
		t.Fatalf("empty list = %q", got)
	}
}

func TestTaskListText_FormatsRows(t *testing.T) {
	tasks := []*client.Task{
		{
			ID: 1, Title: "First", Status: "not_started",
			ChecklistProgress: client.ChecklistProgress{Done: 0, Total: 0},
		},
		{
			ID: 2, Title: "Second", Status: "in_progress", DueDate: "2026-04-25",
			Project:           &client.Project{ID: 7, Name: "Engineering"},
			ChecklistProgress: client.ChecklistProgress{Done: 1, Total: 3},
		},
	}
	got := taskListText(tasks)
	for _, want := range []string{"ID", "STATUS", "First", "Second", "Engineering", "1/3"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Empty due date should render as `-`, not an empty column.
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got %d lines:\n%s", len(lines), got)
	}
}

func TestTaskDetailText_IncludesDescription(t *testing.T) {
	task := &client.Task{
		ID: 17, Title: "hello", Status: "done", Description: "body copy",
		ChecklistProgress: client.ChecklistProgress{Done: 2, Total: 3},
		Project:           &client.Project{ID: 1, Name: "P"},
		CreatedBy:         &client.Actor{Type: "user", ID: 5},
	}
	got := taskDetailText(task)
	for _, want := range []string{"#17 hello", "status:   done", "progress: 2 / 3", "user#5", "body copy"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestTaskDetailText_OmitsBlankDescription(t *testing.T) {
	task := &client.Task{ID: 1, Title: "x", Status: "done"}
	got := taskDetailText(task)
	// Ends with the created_by line, not with the description body.
	if strings.Contains(got, "\n\n") {
		t.Fatalf("blank description should not leave a trailing blank block:\n%s", got)
	}
}

// -- runTaskList ------------------------------------------------------------

func TestRunTaskList_JSONEnvelope(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": []any{
				map[string]any{
					"id": 1, "title": "First", "description": "", "status": "not_started",
					"due_date": nil, "project": nil,
					"checklist_progress": map[string]any{"done": 0, "total": 0},
					"created_by":         nil, "last_actor": nil,
				},
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskList(context.Background(), &stdout, &stderr, fx.Opts); err != nil {
		t.Fatalf("runTaskList: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	tasks, ok := out["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("tasks shape = %+v", out["tasks"])
	}
	first := tasks[0].(map[string]any)
	if first["title"] != "First" {
		t.Fatalf("title = %v", first["title"])
	}
}

func TestRunTaskList_TextFallbackGoesToStdout(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskList(context.Background(), &stdout, &stderr, fx.Opts); err != nil {
		t.Fatalf("runTaskList: %v", err)
	}
	if !strings.Contains(stdout.String(), "(no tasks)") {
		t.Fatalf("stdout = %q, want (no tasks)", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty on success, got %q", stderr.String())
	}
}

func TestRunTaskList_APIErrorGoesToStdout(t *testing.T) {
	// With --format=json, API errors render as a structured payload on stdout
	// so agents parsing stdout don't have to read stderr separately.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})

	var stdout, stderr bytes.Buffer
	err := runTaskList(context.Background(), &stdout, &stderr, fx.Opts)
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty in json mode, got %q", stderr.String())
	}
	var out map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &out); jerr != nil {
		t.Fatalf("not JSON: %v (%q)", jerr, stdout.String())
	}
	if out["error"] != "forbidden" || out["status"] != "error" {
		t.Fatalf("payload = %+v", out)
	}
}

// -- runTaskShow ------------------------------------------------------------

func TestRunTaskShow_JSONEnvelope(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/42" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 42, "title": "hello", "description": "d", "status": "done",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskShow(context.Background(), &stdout, &stderr, "42", fx.Opts); err != nil {
		t.Fatalf("runTaskShow: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	task, ok := out["task"].(map[string]any)
	if !ok || task["id"] == nil || task["title"] != "hello" {
		t.Fatalf("task = %+v", out["task"])
	}
}

// -- runTaskCreate ----------------------------------------------------------

func TestRunTaskCreate_WiresEnvelope(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 17, "title": "hello", "description": "", "status": "not_started",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})

	var stdout, stderr bytes.Buffer
	attrs := map[string]any{"title": "hello"}
	if err := runTaskCreate(context.Background(), &stdout, &stderr, attrs, fx.Opts); err != nil {
		t.Fatalf("runTaskCreate: %v", err)
	}
	var sent struct {
		Task map[string]any `json:"task"`
	}
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.Task["title"] != "hello" {
		t.Fatalf("sent title = %v, want hello", sent.Task["title"])
	}
}
