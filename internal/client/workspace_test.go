package client

import (
	"context"
	"net/http"
	"testing"
)

// A selected workspace becomes a path prefix on every workspace-bound route —
// and only those. User-level routes stay flat: the server doesn't mount them
// under the selector, so prefixing them would be a 404.
func TestWithWorkspace_PrefixesWorkspaceBoundRoutes(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"tasks": []any{}, "task": map[string]any{"id": 1}, "checklist_items": []any{},
			"checklist_item": map[string]any{"id": 2}, "theme": "x", "status": "ok",
			"date": "2026-01-01", "completed_tasks": []any{}, "completed_items": []any{},
		})
	})
	c := NewAuthed("tok", "sek", WithWorkspace("42"))
	ctx := context.Background()

	calls := []struct {
		name string
		do   func() error
		want string
	}{
		{"ListTasks", func() error { _, err := c.ListTasks(ctx); return err }, "/api/v1/workspaces/42/tasks"},
		{"SearchTasks", func() error { _, err := c.SearchTasks(ctx, "q"); return err }, "/api/v1/workspaces/42/tasks/search"},
		{"GetTask", func() error { _, err := c.GetTask(ctx, "7", false); return err }, "/api/v1/workspaces/42/tasks/7"},
		{"CreateTask", func() error { _, err := c.CreateTask(ctx, map[string]any{"title": "t"}); return err }, "/api/v1/workspaces/42/tasks"},
		{"UpdateTask", func() error { _, err := c.UpdateTask(ctx, "7", map[string]any{"title": "t"}); return err }, "/api/v1/workspaces/42/tasks/7"},
		{"SetTaskDueDate", func() error { _, err := c.SetTaskDueDate(ctx, "7", nil); return err }, "/api/v1/workspaces/42/tasks/7/due_date"},
		{"DeleteTask", func() error { return c.DeleteTask(ctx, "7") }, "/api/v1/workspaces/42/tasks/7"},
		{"CreateChecklistItem", func() error { _, err := c.CreateChecklistItem(ctx, "7", map[string]any{"title": "i"}); return err }, "/api/v1/workspaces/42/tasks/7/checklist"},
		{"GetChecklistItem", func() error { _, err := c.GetChecklistItem(ctx, "9"); return err }, "/api/v1/workspaces/42/checklist_items/9"},
		{"SetChecklistItemCompleted", func() error { _, err := c.SetChecklistItemCompleted(ctx, "9", true); return err }, "/api/v1/workspaces/42/checklist_items/9/completed"},
		{"SetChecklistItemState", func() error { _, err := c.SetChecklistItemState(ctx, "9", map[string]any{"state": nil}); return err }, "/api/v1/workspaces/42/checklist_items/9/state"},
		{"UpdateChecklistItem", func() error { _, err := c.UpdateChecklistItem(ctx, "9", map[string]any{"title": "i"}); return err }, "/api/v1/workspaces/42/checklist_items/9"},
		{"DeleteChecklistItem", func() error { return c.DeleteChecklistItem(ctx, "9") }, "/api/v1/workspaces/42/checklist_items/9"},
		{"ListChecklistItemsByState", func() error { _, err := c.ListChecklistItemsByState(ctx, "ready", false); return err }, "/api/v1/workspaces/42/checklist_items"},
		{"GetActivityLog", func() error { _, err := c.GetActivityLog(ctx, "", ""); return err }, "/api/v1/workspaces/42/activity_log"},
		// User-level: never prefixed.
		{"Whoami", func() error { _, err := c.Whoami(ctx); return err }, "/api/v1/whoami"},
		{"GetTheme", func() error { _, err := c.GetTheme(ctx); return err }, "/api/v1/settings/theme"},
	}
	for i, tc := range calls {
		if err := tc.do(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		reqs := ts.Requests()
		if got := reqs[i].Path; got != tc.want {
			t.Errorf("%s: path = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Empty means "the factors' default": no prefix, exactly the old paths.
func TestWithWorkspace_EmptyIsNoOp(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"tasks": []any{}})
	})
	c := NewAuthed("tok", "sek", WithWorkspace(""))
	if _, err := c.ListTasks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ts.Requests()[0].Path; got != "/api/v1/tasks" {
		t.Fatalf("path = %q, want the flat route", got)
	}
	if c.Workspace() != "" {
		t.Fatalf("Workspace() = %q, want empty", c.Workspace())
	}
}

// The selector never replaces a factor: both narrowing headers still go out.
func TestWithWorkspace_KeepsNarrowingHeaders(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"tasks": []any{}})
	})
	c := NewAuthed("tok", "sek", WithProjectKey("acme"), WithWorkspace("3"))
	if _, err := c.ListTasks(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := ts.Requests()[0]
	if req.AgentSecret != "sek" || req.ProjectKey != "acme" || req.Path != "/api/v1/workspaces/3/tasks" {
		t.Fatalf("request = %+v", req)
	}
}

// whoami decodes the workspace picture and tolerates a server without one.
func TestWhoami_DecodesWorkspaces(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status":    "ok",
			"agent":     map[string]any{"id": 1, "name": "owner", "is_owner": true},
			"workspace": map[string]any{"id": 1, "name": "Default", "role": "owner", "level": "write_create"},
			"workspaces": []any{
				map[string]any{"id": 1, "name": "Default", "role": "owner", "level": "write_create"},
				map[string]any{"id": 3, "name": "Work", "role": "owner", "level": "none"},
			},
			"project": nil,
		})
	})
	w, err := NewAuthed("tok", "sek").Whoami(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if w.Workspace == nil || w.Workspace.ID != 1 || w.Workspace.Name != "Default" {
		t.Fatalf("workspace = %+v", w.Workspace)
	}
	if len(w.Workspaces) != 2 || w.Workspaces[1].Level != "none" {
		t.Fatalf("workspaces = %+v", w.Workspaces)
	}
	if w.ProjectPermissions != nil {
		t.Fatalf("project_permissions should be nil when absent, got %+v", w.ProjectPermissions)
	}
}

func TestWhoami_PreWorkspacesServer(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status": "ok", "agent": map[string]any{"id": 1},
			"project_permissions": []any{},
		})
	})
	w, err := NewAuthed("tok", "sek").Whoami(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if w.Workspace != nil || w.Workspaces != nil {
		t.Fatalf("expected no workspace picture, got %+v / %+v", w.Workspace, w.Workspaces)
	}
	if w.ProjectPermissions == nil || len(w.ProjectPermissions) != 0 {
		t.Fatalf("an explicit [] should decode as empty non-nil, got %#v", w.ProjectPermissions)
	}
}
