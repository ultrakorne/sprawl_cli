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

// -- pure text-format helpers ----------------------------------------------

func TestActivityText_Empty(t *testing.T) {
	got := activityText(&client.ActivityLog{Date: "2026-04-29"})
	if !strings.Contains(got, "(no activity)") {
		t.Fatalf("empty activity log = %q", got)
	}
	if !strings.Contains(got, "2026-04-29") {
		t.Fatalf("missing date in: %q", got)
	}
}

func TestActivityText_TasksAndItems(t *testing.T) {
	got := activityText(&client.ActivityLog{
		Date: "2026-04-29",
		CompletedTasks: []*client.Task{
			{
				ID: 42, Title: "Ship Q2 plan", Status: "done",
				DueDate:           "2026-04-29",
				Project:           &client.Project{ID: 7, Name: "Roadmap"},
				ChecklistProgress: client.ChecklistProgress{Done: 3, Total: 3},
			},
		},
		CompletedItems: []*client.ActivityChecklistItem{
			{
				ID: 101, Title: "Draft proposal", Completed: true,
				CompletedAt: "2026-04-29T16:42:11Z",
				Task: client.ActivityItemTask{
					ID: 42, Title: "Ship Q2 plan",
					Project: &client.Project{ID: 7, Name: "Roadmap"},
				},
			},
		},
	})
	for _, want := range []string{
		"activity for 2026-04-29",
		"completed tasks (1)",
		"Ship Q2 plan",
		"3/3",
		"completed items (1)",
		"Draft proposal",
		"#42 Ship Q2 plan",
		"2026-04-29T16:42:11Z",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestActivityText_TasksOnly(t *testing.T) {
	got := activityText(&client.ActivityLog{
		Date: "2026-04-28",
		CompletedTasks: []*client.Task{
			{ID: 1, Title: "Solo", Status: "done"},
		},
	})
	if !strings.Contains(got, "completed tasks (1)") {
		t.Fatalf("missing tasks header: %q", got)
	}
	if strings.Contains(got, "completed items") {
		t.Fatalf("expected no items section, got: %q", got)
	}
}

func TestActivityText_ItemsOnly(t *testing.T) {
	got := activityText(&client.ActivityLog{
		Date: "2026-04-27",
		CompletedItems: []*client.ActivityChecklistItem{
			{ID: 9, Title: "tiny step", Task: client.ActivityItemTask{ID: 3, Title: "Parent"}},
		},
	})
	if !strings.Contains(got, "completed items (1)") {
		t.Fatalf("missing items header: %q", got)
	}
	if strings.Contains(got, "completed tasks") {
		t.Fatalf("expected no tasks section, got: %q", got)
	}
}

// -- pre-flight validation -------------------------------------------------

func TestValidateActivityParams(t *testing.T) {
	cases := []struct {
		name, date, daysAgo, wantSubstr string
	}{
		{"both set", "2026-04-29", "1", "not both"},
		{"non-integer days_ago", "", "abc", "non-negative integer"},
		{"negative days_ago", "", "-1", "non-negative integer"},
		{"malformed date", "29-04-2026", "", "YYYY-MM-DD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateActivityParams(tc.date, tc.daysAgo)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("err = %q, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestValidateActivityParams_Valid(t *testing.T) {
	for _, tc := range []struct{ date, daysAgo string }{
		{"", ""},
		{"2026-04-29", ""},
		{"", "0"},
		{"", "365"},
	} {
		if err := validateActivityParams(tc.date, tc.daysAgo); err != nil {
			t.Errorf("validateActivityParams(%q, %q) = %v, want nil", tc.date, tc.daysAgo, err)
		}
	}
}

// -- runActivity local-error paths skip the HTTP call ----------------------

func TestRunActivity_BothFlagsErrorsLocally(t *testing.T) {
	// Server fails the test if it's hit — pre-flight should short-circuit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP call to %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SPRAWL_API_URL", srv.URL)
	t.Setenv("SPRAWL_TOKEN", "the-token")
	t.Setenv("SPRAWL_AGENT_SECRET", "")

	opts := &runtimeOpts{format: "json", agentSecret: "the-secret"}
	var stdout, stderr bytes.Buffer
	err := runActivity(context.Background(), &stdout, &stderr, "2026-04-29", "1", opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(stdout.String(), "not both") {
		t.Fatalf("expected 'not both' in stdout envelope:\n%s", stdout.String())
	}
}

// -- end-to-end through runActivity ----------------------------------------

func TestRunActivity_DefaultNoQueryParams(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/activity_log" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("expected empty query string, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"date":            "2026-04-30",
			"completed_tasks": []any{},
			"completed_items": []any{},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runActivity(context.Background(), &stdout, &stderr, "", "", fx.Opts); err != nil {
		t.Fatalf("runActivity: %v", err)
	}
	for _, want := range []string{`"status":"ok"`, `"date":"2026-04-30"`, `"completed_tasks":[]`, `"completed_items":[]`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q in:\n%s", want, stdout.String())
		}
	}
}

func TestRunActivity_DateQueryParam(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("date"); got != "2026-04-29" {
			t.Errorf("date = %q", got)
		}
		if got := r.URL.Query().Get("days_ago"); got != "" {
			t.Errorf("unexpected days_ago = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"date":            "2026-04-29",
			"completed_tasks": []any{},
			"completed_items": []any{},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runActivity(context.Background(), &stdout, &stderr, "2026-04-29", "", fx.Opts); err != nil {
		t.Fatalf("runActivity: %v", err)
	}
}

func TestRunActivity_DaysAgoQueryParam(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("days_ago"); got != "1" {
			t.Errorf("days_ago = %q", got)
		}
		if got := r.URL.Query().Get("date"); got != "" {
			t.Errorf("unexpected date = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"date":            "2026-04-29",
			"completed_tasks": []any{},
			"completed_items": []any{},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runActivity(context.Background(), &stdout, &stderr, "", "1", fx.Opts); err != nil {
		t.Fatalf("runActivity: %v", err)
	}
}

func TestRunActivity_PassesPayloadThroughJSON(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"date": "2026-04-29",
			"completed_tasks": []any{
				map[string]any{
					"id": 42, "title": "Ship Q2 plan", "description": nil,
					"status": "done", "due_date": "2026-04-29",
					"project":            map[string]any{"id": 7, "name": "Roadmap", "color": "#3366ff"},
					"checklist_progress": map[string]any{"done": 3, "total": 3},
					"created_by":         nil,
					"last_actor":         map[string]any{"type": "user", "id": 1},
				},
			},
			"completed_items": []any{
				map[string]any{
					"id": 101, "title": "Draft proposal", "completed": true,
					"completed_at": "2026-04-29T16:42:11Z",
					"position":     0, "has_notes": false,
					"last_actor": map[string]any{"type": "agent", "id": 4, "emoji": "🦊"},
					"task": map[string]any{
						"id": 42, "title": "Ship Q2 plan",
						"project": map[string]any{"id": 7, "name": "Roadmap", "color": "#3366ff"},
					},
				},
			},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runActivity(context.Background(), &stdout, &stderr, "", "", fx.Opts); err != nil {
		t.Fatalf("runActivity: %v", err)
	}
	for _, want := range []string{
		`"date":"2026-04-29"`,
		`"id":42`,
		`"title":"Ship Q2 plan"`,
		`"completed_at":"2026-04-29T16:42:11Z"`,
		`"emoji":"🦊"`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q in:\n%s", want, stdout.String())
		}
	}
}

func TestRunActivity_422InvalidDaysAgoSurfaced(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_days_ago"})
	})
	var stdout, stderr bytes.Buffer
	// Use a value that passes our local check but the server treats as out
	// of range (we only enforce ≥ 0; the 365 cap is server-side).
	err := runActivity(context.Background(), &stdout, &stderr, "", "999", fx.Opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(stdout.String(), `"http_status":422`) || !strings.Contains(stdout.String(), `"invalid_days_ago"`) {
		t.Fatalf("error envelope missing fields:\n%s", stdout.String())
	}
}

func TestRunActivity_403InvalidAgentSecretSurfaced(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_agent_secret"})
	})
	var stdout, stderr bytes.Buffer
	err := runActivity(context.Background(), &stdout, &stderr, "", "", fx.Opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(stdout.String(), `"http_status":403`) || !strings.Contains(stdout.String(), `"invalid_agent_secret"`) {
		t.Fatalf("error envelope missing fields:\n%s", stdout.String())
	}
}
