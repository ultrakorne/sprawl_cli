package client

import (
	"context"
	"net/http"
	"testing"
)

// -- X-Project-Key header ---------------------------------------------------

func TestWithProjectKey_SendsHeader(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"tasks": []any{}})
	})
	c := NewAuthed("tok", "sec", WithProjectKey("acme"))
	if _, err := c.ListTasks(context.Background()); err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	req := ts.Requests()[0]
	if req.ProjectKey != "acme" {
		t.Errorf("X-Project-Key = %q, want acme", req.ProjectKey)
	}
	if req.AgentSecret != "sec" {
		t.Errorf("X-Agent-Secret = %q, want sec (both factors intersect)", req.AgentSecret)
	}
}

// A project key alone is a complete narrowing factor: no agent secret header
// goes out, and the server is expected to accept the request on the bearer +
// key alone.
func TestWithProjectKey_AloneOmitsAgentSecret(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"tasks": []any{}})
	})
	c := NewAuthed("tok", "", WithProjectKey("acme"))
	if _, err := c.ListTasks(context.Background()); err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	req := ts.Requests()[0]
	if req.ProjectKey != "acme" {
		t.Errorf("X-Project-Key = %q, want acme", req.ProjectKey)
	}
	if req.AgentSecret != "" {
		t.Errorf("X-Agent-Secret = %q, want empty", req.AgentSecret)
	}
}

// No option (or an empty key) must leave the wire byte-identical to what the
// pre-project-keys client sent.
func TestNoProjectKey_OmitsHeader(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"tasks": []any{}})
	})
	for _, c := range []*Client{
		NewAuthed("tok", "sec"),
		NewAuthed("tok", "sec", WithProjectKey("")),
	} {
		if _, err := c.ListTasks(context.Background()); err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
	}
	for i, req := range ts.Requests() {
		if req.ProjectKey != "" {
			t.Errorf("request %d sent X-Project-Key = %q, want none", i, req.ProjectKey)
		}
	}
}

func TestProjectKey_Accessor(t *testing.T) {
	if got := NewAuthed("tok", "sec", WithProjectKey("acme")).ProjectKey(); got != "acme" {
		t.Errorf("ProjectKey() = %q, want acme", got)
	}
	if got := NewAuthed("tok", "sec").ProjectKey(); got != "" {
		t.Errorf("ProjectKey() = %q, want empty", got)
	}
}

// -- whoami.project ---------------------------------------------------------

func TestWhoami_DecodesProjectBlock(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status": "ok",
			"agent": map[string]any{
				"id": 1, "name": "owner", "emoji": "🦊",
				"is_owner": true, "default_permission": "write_create",
			},
			"project": map[string]any{
				"id": 7, "name": "Acme Corp", "key": "acme", "level": "write_create",
			},
			"project_permissions": []any{},
		})
	})
	w, err := NewAuthed("tok", "", WithProjectKey("acme")).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if w.Project == nil {
		t.Fatal("Project = nil, want the confined project")
	}
	if w.Project.ID != 7 || w.Project.Name != "Acme Corp" ||
		w.Project.Key != "acme" || w.Project.Level != "write_create" {
		t.Fatalf("Project = %+v", *w.Project)
	}
}

// A null `project` (no key sent) and a server that predates the field both
// decode to nil — clients branch on one condition, not two.
func TestWhoami_ProjectNilWhenAbsentOrNull(t *testing.T) {
	for _, body := range []map[string]any{
		{"status": "ok", "agent": map[string]any{"id": 1}, "project": nil, "project_permissions": []any{}},
		{"status": "ok", "agent": map[string]any{"id": 1}, "project_permissions": []any{}},
	} {
		func() {
			newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, body)
			})
			w, err := NewAuthed("tok", "sec").Whoami(context.Background())
			if err != nil {
				t.Fatalf("Whoami: %v", err)
			}
			if w.Project != nil {
				t.Fatalf("Project = %+v, want nil", *w.Project)
			}
		}()
	}
}
