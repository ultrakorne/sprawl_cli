package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// -- resolveProjectKey ------------------------------------------------------

func TestResolveProjectKey_FlagWins(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "from-env")
	if got := resolveProjectKey(&runtimeOpts{projectKey: "from-flag"}); got != "from-flag" {
		t.Fatalf("project key = %q, want from-flag", got)
	}
}

func TestResolveProjectKey_FallsBackToEnv(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "from-env")
	if got := resolveProjectKey(&runtimeOpts{}); got != "from-env" {
		t.Fatalf("project key = %q, want from-env", got)
	}
}

// The server trims and case-folds, so a value pasted with stray whitespace
// still resolves — the CLI trims locally so the header matches what the user
// sees, and leaves case alone (the server folds it).
func TestResolveProjectKey_TrimsWhitespace(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "  acme\n")
	if got := resolveProjectKey(&runtimeOpts{}); got != "acme" {
		t.Fatalf("project key = %q, want acme", got)
	}
	if got := resolveProjectKey(&runtimeOpts{projectKey: " Acme "}); got != "Acme" {
		t.Fatalf("project key = %q, want Acme (case preserved)", got)
	}
}

// A whitespace-only flag value is not a narrowing factor — it must fall
// through to the env var rather than silently sending a blank header.
func TestResolveProjectKey_BlankFlagFallsThrough(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "from-env")
	if got := resolveProjectKey(&runtimeOpts{projectKey: "   "}); got != "from-env" {
		t.Fatalf("project key = %q, want from-env", got)
	}
}

func TestResolveProjectKey_UnsetIsEmpty(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "")
	if got := resolveProjectKey(&runtimeOpts{}); got != "" {
		t.Fatalf("project key = %q, want empty", got)
	}
}

// -- newAuthedClient narrowing factors --------------------------------------

// seedToken wires a scratch config dir with a bearer token and clears both
// narrowing factors from the environment.
func seedToken(t *testing.T) {
	t.Helper()
	scratchConfigDir(t)
	if err := config.Save(build.AppName, &config.Config{Token: "tok"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	t.Setenv("SPRAWL_TOKEN", "tok")
	t.Setenv("SPRAWL_AGENT_SECRET", "")
	t.Setenv("SPRAWL_PROJECT_KEY", "")
}

// A project key alone is enough: no agent secret required, and the request
// carries the bearer + X-Project-Key.
func TestNewAuthedClient_ProjectKeyAloneSuffices(t *testing.T) {
	var gotKey, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Project-Key")
		gotSecret = r.Header.Get("X-Agent-Secret")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
	}))
	t.Cleanup(srv.Close)
	seedToken(t)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	c, err := newAuthedClient(&runtimeOpts{projectKey: "acme"})
	if err != nil {
		t.Fatalf("newAuthedClient: %v", err)
	}
	if _, err := c.ListTasks(context.Background()); err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if gotKey != "acme" {
		t.Errorf("X-Project-Key = %q, want acme", gotKey)
	}
	if gotSecret != "" {
		t.Errorf("X-Agent-Secret = %q, want empty", gotSecret)
	}
}

// Both factors configured ⇒ both headers, which the server intersects.
func TestNewAuthedClient_BothFactorsSendBothHeaders(t *testing.T) {
	var gotKey, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Project-Key")
		gotSecret = r.Header.Get("X-Agent-Secret")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
	}))
	t.Cleanup(srv.Close)
	seedToken(t)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	c, err := newAuthedClient(&runtimeOpts{projectKey: "acme", agentSecret: "K7X2M9QA"})
	if err != nil {
		t.Fatalf("newAuthedClient: %v", err)
	}
	if _, err := c.ListTasks(context.Background()); err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if gotKey != "acme" || gotSecret != "K7X2M9QA" {
		t.Fatalf("headers = key %q / secret %q, want both set", gotKey, gotSecret)
	}
}

// Neither factor ⇒ fail before the HTTP call (the server's
// second_factor_required is useless: it can't know which one the user meant),
// naming both escape hatches.
func TestNewAuthedClient_NeitherFactorFailsPreHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server was hit; expected pre-HTTP failure. Path: %s", r.URL.Path)
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	seedToken(t)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	_, err := newAuthedClient(&runtimeOpts{})
	if err == nil {
		t.Fatal("expected error when neither project key nor agent secret is set")
	}
	for _, want := range []string{"SPRAWL_PROJECT_KEY", "SPRAWL_AGENT_SECRET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %s: %v", want, err)
		}
	}
}

// -- theme is user-level ----------------------------------------------------

// `theme set` never sends the project key: the server rejects a user-level
// write that carries one, so the CLI drops it and uses the agent secret.
func TestRunThemeSet_OmitsProjectKey(t *testing.T) {
	var gotKey, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Project-Key")
		gotSecret = r.Header.Get("X-Agent-Secret")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"theme": "gruvbox"})
	}))
	t.Cleanup(srv.Close)
	seedToken(t)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	opts := &runtimeOpts{format: "json", projectKey: "acme", agentSecret: "K7X2M9QA"}
	var stdout, stderr bytes.Buffer
	if err := runThemeSet(context.Background(), &stdout, &stderr, "gruvbox", opts); err != nil {
		t.Fatalf("runThemeSet: %v", err)
	}
	if gotKey != "" {
		t.Errorf("X-Project-Key = %q, want none on a user-level write", gotKey)
	}
	if gotSecret != "K7X2M9QA" {
		t.Errorf("X-Agent-Secret = %q, want the secret", gotSecret)
	}
}

// A project-confined session with no agent secret can't change the theme.
// Fail locally with a message that says why, instead of a bare 403.
func TestRunThemeSet_ProjectKeyOnlyFailsPreHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server was hit; expected pre-HTTP failure. Path: %s", r.URL.Path)
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	seedToken(t)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	opts := &runtimeOpts{format: "text", projectKey: "acme"}
	var stdout, stderr bytes.Buffer
	err := runThemeSet(context.Background(), &stdout, &stderr, "gruvbox", opts)
	if err == nil {
		t.Fatal("expected error when no agent secret is configured")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "user-level") || !strings.Contains(msg, "SPRAWL_AGENT_SECRET") {
		t.Fatalf("error should explain the user-level requirement:\n%s", msg)
	}
}

// `theme get` is readable under either factor, so it stays on the normal path.
func TestRunThemeGet_WorksWithProjectKeyOnly(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Project-Key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"theme": "gruvbox"})
	}))
	t.Cleanup(srv.Close)
	seedToken(t)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	opts := &runtimeOpts{format: "json", projectKey: "acme"}
	var stdout, stderr bytes.Buffer
	if err := runThemeGet(context.Background(), &stdout, &stderr, opts); err != nil {
		t.Fatalf("runThemeGet: %v", err)
	}
	if gotKey != "acme" {
		t.Errorf("X-Project-Key = %q, want acme", gotKey)
	}
}

// -- delete under confinement -----------------------------------------------

// The server answers 404 for anything outside the confined project, so the
// unconfined "already gone (no change)" line would claim a delete that never
// happened to a task living in another project. Verified against a live
// backend: `task delete <id-in-another-project>` returns 404, not 403.
func TestRunTaskDelete_ConfinedMissingTaskSaysWhy(t *testing.T) {
	fix := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found"})
	})
	fix.Opts.projectKey = "acme"

	var stdout, stderr bytes.Buffer
	if err := runTaskDelete(context.Background(), &stdout, &stderr, "3654", fix.Opts); err != nil {
		t.Fatalf("runTaskDelete: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `No task #3654 in project "acme"`) {
		t.Errorf("expected the confinement to be named:\n%s", got)
	}
	if strings.Contains(got, "already gone") {
		t.Errorf("must not claim the task was already deleted:\n%s", got)
	}
}

// Unconfined, the wording is unchanged: a 404 there really does mean gone.
func TestRunTaskDelete_UnconfinedKeepsAlreadyGone(t *testing.T) {
	fix := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found"})
	})
	var stdout, stderr bytes.Buffer
	if err := runTaskDelete(context.Background(), &stdout, &stderr, "999", fix.Opts); err != nil {
		t.Fatalf("runTaskDelete: %v", err)
	}
	if !strings.Contains(stdout.String(), "Task #999 already gone") {
		t.Fatalf("unconfined wording changed:\n%s", stdout.String())
	}
}

// -- error guidance ---------------------------------------------------------

func TestExplainAPIError_Codes(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "")
	cases := []struct {
		name         string
		err          error
		opts         *runtimeOpts
		wantHeadline string
		wantRemedy   string
	}{
		{
			name:         "invalid project key names the key",
			err:          &client.APIError{Status: 403, Code: "invalid_project_key"},
			opts:         &runtimeOpts{projectKey: "acmee"},
			wantHeadline: `"acmee"`,
			wantRemedy:   "Project key",
		},
		{
			name:         "second factor required lists both options",
			err:          &client.APIError{Status: 403, Code: "second_factor_required"},
			opts:         &runtimeOpts{},
			wantHeadline: "no project key or agent secret",
			wantRemedy:   "SPRAWL_PROJECT_KEY",
		},
		{
			name:         "forbidden under a key blames the confinement",
			err:          &client.APIError{Status: 403, Code: "forbidden"},
			opts:         &runtimeOpts{projectKey: "acme"},
			wantHeadline: "not allowed",
			wantRemedy:   `"acme" confines`,
		},
		{
			name:         "forbidden without a key blames the agent key",
			err:          &client.APIError{Status: 403, Code: "forbidden"},
			opts:         &runtimeOpts{},
			wantHeadline: "not allowed",
			wantRemedy:   "agent key lacks permission",
		},
		{
			name:         "invalid agent secret points at auth settings",
			err:          &client.APIError{Status: 403, Code: "invalid_agent_secret"},
			opts:         &runtimeOpts{},
			wantHeadline: "agent secret rejected",
			wantRemedy:   "/auth-settings",
		},
		{
			name:         "unauthenticated points at login",
			err:          &client.APIError{Status: 401, Code: "unauthenticated"},
			opts:         &runtimeOpts{},
			wantHeadline: "bearer token rejected",
			wantRemedy:   "login",
		},
		{
			name:         "bare 401 still points at login",
			err:          &client.APIError{Status: 401},
			opts:         &runtimeOpts{},
			wantHeadline: "bearer token rejected",
			wantRemedy:   "login",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, ok := explainAPIError(c.err, c.opts)
			if !ok {
				t.Fatalf("explainAPIError returned no guidance for %v", c.err)
			}
			if !strings.Contains(g.headline, c.wantHeadline) {
				t.Errorf("headline %q missing %q", g.headline, c.wantHeadline)
			}
			if !strings.Contains(g.remedy, c.wantRemedy) {
				t.Errorf("remedy %q missing %q", g.remedy, c.wantRemedy)
			}
		})
	}
}

// Errors the user can't act on from the shell (404s, changeset failures,
// transport errors) keep their existing rendering — no invented advice.
func TestExplainAPIError_UnrelatedErrorsGetNoGuidance(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "")
	for _, err := range []error{
		&client.APIError{Status: 404, Code: "not_found"},
		&client.APIError{Status: 422, Code: "invalid_due"},
		errNoNarrowingFactor,
	} {
		if _, ok := explainAPIError(err, &runtimeOpts{}); ok {
			t.Errorf("expected no guidance for %v", err)
		}
	}
}

func TestReportErr_TextPrintsHeadlineAndRemedy(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "")
	var stdout, stderr bytes.Buffer
	err := &client.APIError{Status: 403, Code: "invalid_project_key"}
	_ = reportErr(&stdout, &stderr, err, &runtimeOpts{format: "text", projectKey: "acmee"})

	got := stderr.String()
	if !strings.Contains(got, `project key "acmee" doesn't match`) {
		t.Errorf("text output missing the plain-language headline:\n%s", got)
	}
	if !strings.Contains(got, "side panel") {
		t.Errorf("text output missing the remedy line:\n%s", got)
	}
	if strings.Contains(got, "http 403") {
		t.Errorf("text output should replace the raw status line:\n%s", got)
	}
	if stdout.Len() != 0 {
		t.Errorf("text errors belong on stderr, got stdout:\n%s", stdout.String())
	}
}

// The structured envelope keeps its shape (status / error / http_status) and
// gains `hint` — additive, so existing parsers are unaffected.
func TestReportErr_JSONAddsHintKeepsCode(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "")
	var stdout, stderr bytes.Buffer
	err := &client.APIError{Status: 403, Code: "invalid_project_key"}
	_ = reportErr(&stdout, &stderr, err, &runtimeOpts{format: "json", projectKey: "acmee"})

	var payload map[string]any
	if uerr := json.Unmarshal(stdout.Bytes(), &payload); uerr != nil {
		t.Fatalf("decode payload: %v (%s)", uerr, stdout.String())
	}
	if payload["error"] != "invalid_project_key" {
		t.Errorf("error = %v, want the machine-readable code", payload["error"])
	}
	if payload["status"] != "error" || payload["http_status"] != float64(403) {
		t.Errorf("envelope shape changed: %v", payload)
	}
	hint, _ := payload["hint"].(string)
	if !strings.Contains(hint, "acmee") || !strings.Contains(hint, "side panel") {
		t.Errorf("hint = %q, want headline + remedy", hint)
	}
}

// No guidance ⇒ no hint key, so nothing new shows up on ordinary failures.
func TestReportErr_JSONOmitsHintWhenUnexplained(t *testing.T) {
	t.Setenv("SPRAWL_PROJECT_KEY", "")
	var stdout, stderr bytes.Buffer
	err := &client.APIError{Status: 404, Code: "not_found"}
	_ = reportErr(&stdout, &stderr, err, &runtimeOpts{format: "json"})

	if strings.Contains(stdout.String(), "hint") {
		t.Fatalf("unexpected hint on an unexplained error:\n%s", stdout.String())
	}
}

// -- whoami.project ---------------------------------------------------------

func TestWhoamiPayload_ProjectBlock(t *testing.T) {
	payload := whoamiPayloadDefault(&client.Whoami{
		Agent:   client.Agent{ID: 1, Name: "owner", IsOwner: true},
		Project: &client.WhoamiProject{ID: 7, Name: "Acme Corp", Key: "acme", Level: "write_create"},
	})
	proj, ok := payload["project"].(map[string]any)
	if !ok {
		t.Fatalf("project = %#v, want a map", payload["project"])
	}
	if proj["key"] != "acme" || proj["level"] != "write_create" || proj["name"] != "Acme Corp" {
		t.Fatalf("project block = %#v", proj)
	}
}

func TestWhoamiPayload_ProjectNullWhenUnconfined(t *testing.T) {
	payload := whoamiPayloadDefault(&client.Whoami{Agent: client.Agent{ID: 1}})
	v, present := payload["project"]
	if !present {
		t.Fatal("project key should always be present (null when unconfined)")
	}
	if v != nil {
		t.Fatalf("project = %#v, want nil", v)
	}
}

func TestWhoamiText_ShowsConfinedProject(t *testing.T) {
	got := whoamiTextDefault(&client.Whoami{
		Agent:   client.Agent{ID: 1, Name: "owner", Emoji: "🦊", IsOwner: true},
		Project: &client.WhoamiProject{ID: 7, Name: "Acme Corp", Key: "acme", Level: "write_create"},
	})
	for _, want := range []string{"working in: Acme Corp (acme)", "access:   write_create"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// level "none" is a valid key naming a project this agent can't reach — say so
// rather than leaving the user to wonder why every list is empty.
func TestWhoamiText_LevelNoneIsExplained(t *testing.T) {
	got := whoamiTextDefault(&client.Whoami{
		Agent:   client.Agent{ID: 2, Name: "scout", DefaultPermission: "read"},
		Project: &client.WhoamiProject{ID: 7, Name: "Acme Corp", Key: "acme", Level: "none"},
	})
	if !strings.Contains(got, "no access to that project") {
		t.Fatalf("expected an explanation of level none:\n%s", got)
	}
}

// An unconfined whoami reads exactly as it did before project keys existed.
func TestWhoamiText_NoProjectLineWhenUnconfined(t *testing.T) {
	got := whoamiTextDefault(&client.Whoami{
		Agent: client.Agent{ID: 1, Name: "owner", IsOwner: true},
	})
	if strings.Contains(got, "working in:") {
		t.Fatalf("unconfined whoami should not print a project line:\n%s", got)
	}
}

func TestRunWhoami_JSONCarriesProject(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Project-Key"); got != "acme" {
			t.Errorf("X-Project-Key = %q, want acme", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
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
	fix.Opts.projectKey = "acme"

	var stdout, stderr bytes.Buffer
	if err := runWhoami(context.Background(), &stdout, &stderr, fix.Opts); err != nil {
		t.Fatalf("runWhoami: %v", err)
	}
	for _, want := range []string{`"project":`, `"key":"acme"`, `"level":"write_create"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q in:\n%s", want, stdout.String())
		}
	}
}
