package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// -- resolution -------------------------------------------------------------

func TestResolveWorkspace_FlagBeatsEnvAndTrims(t *testing.T) {
	t.Setenv("SPRAWL_WORKSPACE", "3")
	got, err := resolveWorkspace(&runtimeOpts{workspace: " 7 "})
	if err != nil || got != "7" {
		t.Fatalf("got %q, %v; want 7", got, err)
	}
	got, err = resolveWorkspace(&runtimeOpts{})
	if err != nil || got != "3" {
		t.Fatalf("env fallback: got %q, %v; want 3", got, err)
	}
	t.Setenv("SPRAWL_WORKSPACE", "")
	got, err = resolveWorkspace(&runtimeOpts{})
	if err != nil || got != "" {
		t.Fatalf("unset: got %q, %v; want empty", got, err)
	}
}

// Anything but a positive integer is refused locally: the server's answer
// would be a bare 404 that reads as "task not found".
func TestResolveWorkspace_RejectsNonIds(t *testing.T) {
	t.Setenv("SPRAWL_WORKSPACE", "")
	for _, bad := range []string{"abc", "0", "-1", "3.5", "01", "3 4"} {
		if _, err := resolveWorkspace(&runtimeOpts{workspace: bad}); err == nil {
			t.Errorf("%q should be rejected", bad)
		} else if !strings.Contains(err.Error(), "workspace list") {
			t.Errorf("%q: error should point at `workspace list`, got %v", bad, err)
		}
	}
}

// A malformed selector fails in PersistentPreRunE, before any request.
func TestRootCmd_InvalidWorkspaceIsPreHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("request escaped before workspace validation: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SPRAWL_API_URL", srv.URL)
	t.Setenv("SPRAWL_TOKEN", "the-token")
	t.Setenv("SPRAWL_AGENT_SECRET", "the-secret")
	t.Setenv("SPRAWL_WORKSPACE", "")
	t.Setenv("SPRAWL_NO_UPDATE_CHECK", "1")

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"task", "list", "-w", "main"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected a non-zero exit for a non-numeric workspace")
	}
	if !strings.Contains(stderr.String(), `invalid workspace "main"`) {
		t.Fatalf("stderr should explain the bad selector, got %q", stderr.String())
	}
}

// -- the selector reaches the wire ------------------------------------------

func TestTaskList_WorkspaceFlagPrefixesPath(t *testing.T) {
	var gotPath string
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tasks":[]}`))
	})
	fix.Opts.workspace = "3"
	var stdout, stderr bytes.Buffer
	if err := runTaskList(context.Background(), &stdout, &stderr, fix.Opts); err != nil {
		t.Fatalf("runTaskList: %v\n%s", err, stderr.String())
	}
	if gotPath != "/api/v1/workspaces/3/tasks" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestTaskList_WorkspaceEnvPrefixesPath(t *testing.T) {
	var gotPath string
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tasks":[]}`))
	})
	t.Setenv("SPRAWL_WORKSPACE", "5")
	var stdout, stderr bytes.Buffer
	if err := runTaskList(context.Background(), &stdout, &stderr, fix.Opts); err != nil {
		t.Fatalf("runTaskList: %v", err)
	}
	if gotPath != "/api/v1/workspaces/5/tasks" {
		t.Fatalf("path = %q", gotPath)
	}
}

// The selector is not a factor: with neither key nor secret the call still
// fails locally, exactly as before.
func TestWorkspace_IsNotANarrowingFactor(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("request escaped: %s %s", r.Method, r.URL.Path)
	})
	fix.Opts.agentSecret = ""
	fix.Opts.workspace = "3"
	_, err := newAuthedClient(fix.Opts)
	if err != errNoNarrowingFactor {
		t.Fatalf("err = %v, want errNoNarrowingFactor", err)
	}
}

// -- workspace list ---------------------------------------------------------

func whoamiFixtureBody() map[string]any {
	return map[string]any{
		"status":    "ok",
		"agent":     map[string]any{"id": 1, "name": "owner", "emoji": "🐧", "is_owner": true, "default_permission": "write_create"},
		"workspace": map[string]any{"id": 1, "name": "Default", "role": "owner", "level": "write_create"},
		"workspaces": []any{
			map[string]any{"id": 1, "name": "Default", "role": "owner", "level": "write_create"},
			map[string]any{"id": 3, "name": "Work_2", "role": "owner", "level": "none"},
			map[string]any{"id": 9, "name": "Shared", "role": "read", "level": "read"},
		},
		"project": nil,
	}
}

func TestRunWorkspaceList_JSONMarksServerDefaultAsCurrent(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/whoami" {
			t.Errorf("path = %q, want the flat whoami route", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(whoamiFixtureBody())
	})
	var stdout, stderr bytes.Buffer
	if err := runWorkspaceList(context.Background(), &stdout, &stderr, fix.Opts); err != nil {
		t.Fatalf("runWorkspaceList: %v", err)
	}
	var got struct {
		Status     string           `json:"status"`
		Current    client.Workspace `json:"current"`
		Workspaces []client.Workspace
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout.String())
	}
	if got.Status != "ok" || got.Current.ID != 1 || len(got.Workspaces) != 3 {
		t.Fatalf("payload = %+v", got)
	}
}

func TestRunWorkspaceList_SelectionIsCurrent(t *testing.T) {
	fix := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(whoamiFixtureBody())
	})
	fix.Opts.workspace = "9"
	var stdout, stderr bytes.Buffer
	if err := runWorkspaceList(context.Background(), &stdout, &stderr, fix.Opts); err != nil {
		t.Fatalf("runWorkspaceList: %v", err)
	}
	out := stdout.String()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "›") && !strings.Contains(line, "Shared") {
			t.Fatalf("marker on the wrong row: %q\n%s", line, out)
		}
	}
	if !strings.Contains(out, "›  9   Shared") {
		t.Fatalf("selected workspace should carry the marker:\n%s", out)
	}
	if !strings.Contains(out, "LEVEL") || !strings.Contains(out, "none") {
		t.Fatalf("level column missing:\n%s", out)
	}
}

func TestRunWorkspaceList_UnreachableSelectionFails(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(whoamiFixtureBody())
	})
	fix.Opts.workspace = "77"
	var stdout, stderr bytes.Buffer
	err := runWorkspaceList(context.Background(), &stdout, &stderr, fix.Opts)
	if err == nil || !strings.Contains(err.Error(), "workspace 77 is not one you can reach") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stdout.String(), `"status":"error"`) {
		t.Fatalf("json error envelope expected:\n%s", stdout.String())
	}
}

func TestRunWorkspaceList_PreWorkspacesServer(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","agent":{"id":1},"project_permissions":[]}`))
	})
	var stdout, stderr bytes.Buffer
	err := runWorkspaceList(context.Background(), &stdout, &stderr, fix.Opts)
	if err == nil || !strings.Contains(err.Error(), "doesn't report workspaces") {
		t.Fatalf("err = %v", err)
	}
}

// Under a project key the level column is dropped: confinement makes every
// workspace read `none`, which is not "no access".
func TestWorkspaceListText_ConfinedDropsLevel(t *testing.T) {
	list := []client.Workspace{{ID: 1, Name: "Default", Role: "owner", Level: "none"}}
	got := workspaceListText(list, &list[0], true)
	if strings.Contains(got, "LEVEL") || strings.Contains(got, "none") {
		t.Fatalf("confined table should not show levels:\n%s", got)
	}
	got = workspaceListText(list, &list[0], false)
	if !strings.Contains(got, "LEVEL") || !strings.Contains(got, "none") {
		t.Fatalf("unconfined table should show levels:\n%s", got)
	}
}

// -- whoami under a selection -----------------------------------------------

func TestRunWhoami_SelectionReplacesDefaultWorkspace(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(whoamiFixtureBody())
	})
	fix.Opts.workspace = "3"
	var stdout, stderr bytes.Buffer
	if err := runWhoami(context.Background(), &stdout, &stderr, fix.Opts); err != nil {
		t.Fatalf("runWhoami: %v", err)
	}
	var got struct {
		Workspace  client.Workspace   `json:"workspace"`
		Workspaces []client.Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Workspace.ID != 3 || got.Workspace.Name != "Work_2" {
		t.Fatalf("workspace = %+v, want the selected one", got.Workspace)
	}
	if len(got.Workspaces) != 3 {
		t.Fatalf("workspaces = %+v", got.Workspaces)
	}
	if strings.Contains(stdout.String(), "project_permissions") {
		t.Fatalf("a workspaces server sends no project_permissions; the CLI must not invent it:\n%s", stdout.String())
	}
}

func TestRunWhoami_UnreachableSelectionFails(t *testing.T) {
	fix := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(whoamiFixtureBody())
	})
	fix.Opts.workspace = "77"
	var stdout, stderr bytes.Buffer
	if err := runWhoami(context.Background(), &stdout, &stderr, fix.Opts); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr.String(), "workspace 77 is not one you can reach") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestWhoamiText_WorkspaceLines(t *testing.T) {
	w := &client.Whoami{
		Agent:     client.Agent{ID: 1, Name: "owner", IsOwner: true},
		Workspace: &client.Workspace{ID: 1, Name: "Default", Role: "owner", Level: "write_create"},
	}
	got := whoamiText(w, w.Workspace, false)
	for _, want := range []string{"workspace: Default #1", "role:    owner", "access:  write_create"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "(selected)") || strings.Contains(got, "elevated project permissions") {
		t.Fatalf("unexpected lines:\n%s", got)
	}

	sel := &client.Workspace{ID: 3, Name: "Work_2", Role: "owner", Level: "none"}
	got = whoamiText(w, sel, true)
	if !strings.Contains(got, "workspace (selected): Work_2 #3") {
		t.Fatalf("selected label missing:\n%s", got)
	}
	if !strings.Contains(got, "can't reach that workspace") {
		t.Fatalf("level none should be explained:\n%s", got)
	}
}

// A project key pins its workspace: selecting another is diagnosed here, and
// the key's `none` level on every workspace is not shown as "no access".
func TestWhoamiText_ProjectKeyAndForeignSelectionWarns(t *testing.T) {
	w := &client.Whoami{
		Agent:     client.Agent{ID: 1, Name: "owner", IsOwner: true},
		Workspace: &client.Workspace{ID: 1, Name: "Default", Role: "owner", Level: "none"},
		Project:   &client.WhoamiProject{ID: 8, Name: "bla", Key: "bla", Level: "write_create"},
	}
	got := whoamiText(w, w.Workspace, false)
	if strings.Contains(got, "access:  none") || strings.Contains(got, "warning:") {
		t.Fatalf("confined default should be quiet:\n%s", got)
	}
	sel := &client.Workspace{ID: 3, Name: "Work_2", Role: "owner", Level: "none"}
	got = whoamiText(w, sel, true)
	if !strings.Contains(got, "workspace_mismatch") || !strings.Contains(got, `project key "bla" lives in workspace #1`) {
		t.Fatalf("expected a mismatch warning:\n%s", got)
	}
}

// -- error guidance ---------------------------------------------------------

func TestExplainAPIError_WorkspaceCodes(t *testing.T) {
	t.Setenv("SPRAWL_WORKSPACE", "")
	opts := &runtimeOpts{projectKey: "bla", workspace: "3"}

	g, ok := explainAPIError(&client.APIError{Status: 403, Code: "workspace_mismatch"}, opts)
	if !ok || !strings.Contains(g.headline, `"bla"`) || !strings.Contains(g.headline, "--workspace 3") {
		t.Fatalf("mismatch guidance = %+v, %v", g, ok)
	}
	g, ok = explainAPIError(&client.APIError{Status: 403, Code: "workspace_required"}, opts)
	if !ok || !strings.Contains(g.remedy, "--workspace <id>") {
		t.Fatalf("required guidance = %+v, %v", g, ok)
	}
	// A 404 under a selector names both possibilities…
	g, ok = explainAPIError(&client.APIError{Status: 404, Code: "not_found"}, opts)
	if !ok || !strings.Contains(g.remedy, "workspace 3 isn't one you can reach") {
		t.Fatalf("404 guidance = %+v, %v", g, ok)
	}
	// …and without one it stays the plain, unexplained 404 it always was.
	if _, ok := explainAPIError(&client.APIError{Status: 404, Code: "not_found"}, &runtimeOpts{}); ok {
		t.Fatal("a 404 without a selector should not be explained")
	}
}

// -- deletes under a selector -----------------------------------------------

// A 404 under a selector can't say whether the id or the workspace was the
// miss, so the idempotent-success line must not claim the record was gone.
func TestRunTaskDelete_UnderWorkspaceSaysWhy(t *testing.T) {
	fix := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspaces/3/tasks/17" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})
	fix.Opts.workspace = "3"
	var stdout, stderr bytes.Buffer
	if err := runTaskDelete(context.Background(), &stdout, &stderr, "17", fix.Opts); err != nil {
		t.Fatalf("a 404 delete stays idempotent success: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "No task #17 in workspace 3") || strings.Contains(out, "already gone") {
		t.Fatalf("delete under a selector should name the workspace:\n%s", out)
	}
}

func TestRunItemDelete_UnderWorkspaceSaysWhy(t *testing.T) {
	fix := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})
	t.Setenv("SPRAWL_WORKSPACE", "3")
	var stdout, stderr bytes.Buffer
	if err := runItemDelete(context.Background(), &stdout, &stderr, "9", fix.Opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "No item #9 in workspace 3") {
		t.Fatalf("got:\n%s", stdout.String())
	}
}

func TestDeletedText_ProjectKeyWinsOverWorkspace(t *testing.T) {
	got := deletedText("task", "1", false, "acme", "3")
	if !strings.Contains(got, `project "acme"`) || strings.Contains(got, "workspace") {
		t.Fatalf("project key wording should win: %q", got)
	}
	if got := deletedText("task", "1", false, "", ""); !strings.Contains(got, "already gone") {
		t.Fatalf("unscoped wording changed: %q", got)
	}
}

// -- whoami and workspace list agree when the server sends no list ----------

func TestRunWhoami_SelectionWithoutListFails(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// `workspace` but no `workspaces`: nothing to confirm a selection against.
		_, _ = w.Write([]byte(`{"status":"ok","agent":{"id":1},"workspace":{"id":1,"name":"Default","role":"owner","level":"write_create"}}`))
	})
	fix.Opts.workspace = "3"
	var stdout, stderr bytes.Buffer
	err := runWhoami(context.Background(), &stdout, &stderr, fix.Opts)
	if err == nil || !strings.Contains(err.Error(), "doesn't report workspaces") {
		t.Fatalf("err = %v", err)
	}
	// The same call without a selector still renders the default.
	fix.Opts.workspace = ""
	stdout.Reset()
	if err := runWhoami(context.Background(), &stdout, &stderr, fix.Opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"workspace":{"id":1`) {
		t.Fatalf("default workspace missing:\n%s", stdout.String())
	}
}

func TestRunWorkspaceList_NoListFailsEvenWithDefault(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","agent":{"id":1},"workspace":{"id":1,"name":"Default","role":"owner","level":"write_create"}}`))
	})
	var stdout, stderr bytes.Buffer
	err := runWorkspaceList(context.Background(), &stdout, &stderr, fix.Opts)
	if err == nil || !strings.Contains(err.Error(), "doesn't report workspaces") {
		t.Fatalf("err = %v", err)
	}
}

func TestExplainAPIError_WorkspaceMismatchWithoutKey(t *testing.T) {
	t.Setenv("SPRAWL_WORKSPACE", "")
	t.Setenv("SPRAWL_PROJECT_KEY", "")
	g, ok := explainAPIError(&client.APIError{Status: 403, Code: "workspace_mismatch"}, &runtimeOpts{workspace: "3"})
	if !ok || strings.Contains(g.headline, `""`) || !strings.Contains(g.headline, "--workspace 3") {
		t.Fatalf("headline = %q", g.headline)
	}
	g, _ = explainAPIError(&client.APIError{Status: 403, Code: "workspace_mismatch"}, &runtimeOpts{})
	if strings.Contains(g.headline, `""`) || strings.Contains(g.headline, "--workspace ") {
		t.Fatalf("headline = %q", g.headline)
	}
}
