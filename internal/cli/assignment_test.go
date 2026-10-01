package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func TestAssignmentWrites(t *testing.T) {
	for _, tc := range []struct {
		name, workspace, input, method, path, attrs string
		args                                        []string
	}{
		{"assign user", "", "", "PATCH", "/api/v1/checklist_items/203", `{"assignee":{"type":"user","id":3}}`, []string{"assign", "203", "user:3"}},
		{"reassign key", "42", "", "PATCH", "/api/v1/workspaces/42/checklist_items/203", `{"assignee":{"type":"agent_key","id":7}}`, []string{"assign", "203", "agent_key:7"}},
		{"clear", "", "", "PATCH", "/api/v1/checklist_items/203", `{"assignee":null}`, []string{"unassign", "203"}},
		{"create assigned", "42", "", "POST", "/api/v1/workspaces/42/tasks/119/checklist", `{"title":"docs","assignee":{"type":"agent_key","id":7}}`, []string{"add", "119", "--title", "docs", "--assignee", "agent_key:7"}},
		{"combined update", "", "", "PATCH", "/api/v1/checklist_items/203", `{"title":"docs","assignee":{"type":"user","id":3}}`, []string{"update", "203", "--title", "docs", "--assignee", "user:3"}},
		{"JSON pair", "", `{"assignee":{"type":"user","id":"3"}}`, "PATCH", "/api/v1/checklist_items/203", `{"assignee":{"type":"user","id":3}}`, []string{"update", "203", "--from-json", "-"}},
		{"JSON integer pair", "", `{"assignee":{"type":"user","id":3}}`, "PATCH", "/api/v1/checklist_items/203", `{"assignee":{"type":"user","id":3}}`, []string{"update", "203", "--from-json", "-"}},
		{"JSON maximum ID", "", `{"assignee":{"type":"user","id":2147483647}}`, "PATCH", "/api/v1/checklist_items/203", `{"assignee":{"type":"user","id":2147483647}}`, []string{"update", "203", "--from-json", "-"}},
		{"JSON null", "", `{"assignee":null}`, "PATCH", "/api/v1/checklist_items/203", `{"assignee":null}`, []string{"update", "203", "--from-json", "-"}},
		{"omitted", "", "", "PATCH", "/api/v1/checklist_items/203", `{"title":"docs"}`, []string{"update", "203", "--title", "docs"}},
		{"flags override JSON", "", `{"title":"docs","assignee":"invalid"}`, "PATCH", "/api/v1/checklist_items/203", `{"title":"docs","assignee":{"type":"user","id":3}}`, []string{"update", "203", "--from-json", "-", "--assignee", "user:3"}},
		{"clear overrides JSON", "", `{"assignee":{"type":"user","id":3}}`, "PATCH", "/api/v1/checklist_items/203", `{"assignee":null}`, []string{"update", "203", "--from-json", "-", "--unassign"}},
		{"stdin note and assignment", "", "context\nmore", "PATCH", "/api/v1/checklist_items/203", `{"notes":"context\nmore","assignee":{"type":"user","id":3}}`, []string{"update", "203", "--notes", "-", "--assignee", "user:3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer the-token" || r.Header.Get("X-Agent-Secret") != "the-secret" || r.Header.Get("X-Project-Key") != "acme" {
					t.Error("credentials changed")
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				var attrs map[string]any
				_ = json.Unmarshal([]byte(tc.attrs), &attrs)
				if !reflect.DeepEqual(got, map[string]any{"checklist_item": attrs}) {
					t.Errorf("body = %#v, want %s", got, tc.attrs)
				}
				pair := attrs["assignee"]
				if _, present := attrs["assignee"]; !present {
					pair = map[string]any{"type": "agent_key", "id": 99}
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.method == "POST" {
					w.WriteHeader(http.StatusCreated)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"checklist_item": map[string]any{
					"id": 203, "title": "docs", "assignee": pair, "state": "in_review", "pr_number": 412,
					"completed": true, "notes": "saved", "last_actor": map[string]any{"type": "agent", "id": 7},
				}})
			})
			fx.Opts.workspace, fx.Opts.projectKey = tc.workspace, "acme"
			cmd := newItemCmd(fx.Opts)
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader(tc.input))
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v\n%s", err, stderr.String())
			}
			if calls != 1 {
				t.Fatalf("got %d calls, want one write and no roster request", calls)
			}
			var payload struct {
				Item map[string]any `json:"checklist_item"`
			}
			if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if _, ok := payload.Item["assignee"]; !ok || payload.Item["state"] != "review" || payload.Item["completed"] != true || payload.Item["pr_number"] != float64(412) {
				t.Fatalf("fields lost: %s", out.String())
			}
		})
	}
}

func TestAssignmentInvalidInputFailsBeforeHTTP(t *testing.T) {
	for _, value := range []string{"", "agent:7", "user:0", "user:-1", "user:+3", "user:2147483648", "user:3.5", "user:1e2", "user: 3", "user:3:4", "user", "USER:3"} {
		t.Run(value, func(t *testing.T) {
			fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected HTTP request") })
			for _, args := range [][]string{{"assign", "203", value}, {"update", "203", "--assignee", value}, {"add", "119", "--title", "docs", "--assignee", value}} {
				cmd := newItemCmd(fx.Opts)
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				cmd.SetArgs(args)
				if err := cmd.Execute(); err == nil {
					t.Errorf("accepted %v", args)
				}
			}
		})
	}
	for _, tc := range []struct {
		input string
		args  []string
	}{
		{"", []string{"update", "203", "--assignee", "user:3", "--unassign"}},
		{"{}", []string{"update", "203", "--notes", "-", "--from-json", "-", "--assignee", "user:3"}},
		{`{"assignee":{"type":"user","id":3.5}}`, []string{"update", "203", "--from-json", "-"}},
		{`{"assignee":{"type":"user","id":3.0000000000000001}}`, []string{"update", "203", "--from-json", "-"}},
		{`{"assignee":{"type":"user","id":2147483647.0000001}}`, []string{"update", "203", "--from-json", "-"}},
		{`{"assignee":{"type":"user","id":3.0}}`, []string{"update", "203", "--from-json", "-"}},
		{`{"assignee":{"type":"user","id":1e2}}`, []string{"update", "203", "--from-json", "-"}},
		{`{"assignee":{"type":"agent","id":7}}`, []string{"update", "203", "--from-json", "-"}},
		{`{"assignee":{"type":"user","id":2147483648}}`, []string{"update", "203", "--from-json", "-"}},
		{`{"assignee":{}}`, []string{"update", "203", "--from-json", "-"}},
		{`{"assignee":true}`, []string{"update", "203", "--from-json", "-"}},
	} {
		fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected HTTP request") })
		cmd := newItemCmd(fx.Opts)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetIn(strings.NewReader(tc.input))
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("accepted %v with %s", tc.args, tc.input)
		}
	}
}

func TestAssignmentRefusalsPreserveError(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		for _, tc := range []struct {
			status int
			code   string
		}{{401, "unauthenticated"}, {403, "forbidden"}, {403, "workspace_mismatch"}, {404, "not_found"}, {422, "invalid_assignee"}, {422, "invalid_body"}} {
			t.Run(fmt.Sprintf("%s/%s", format, tc.code), func(t *testing.T) {
				fx := newAuthedFixture(t, format, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": tc.code})
				})
				var out, stderr bytes.Buffer
				err := runItemUpdate(context.Background(), &out, &stderr, "203", map[string]any{"assignee": &client.Assignee{Type: "user", ID: 3}}, fx.Opts)
				var apiErr *client.APIError
				if !errors.As(err, &apiErr) || apiErr.Status != tc.status || apiErr.Code != tc.code {
					t.Fatalf("error lost: %v", err)
				}
				if strings.Contains(out.String(), "✓") || strings.Contains(out.String(), `"checklist_item"`) {
					t.Fatalf("reported success: %s", out.String())
				}
				if format == "json" && !strings.Contains(out.String(), `"error":"`+tc.code+`"`) {
					t.Fatalf("error code lost: %s", out.String())
				}
			})
		}
	}
}

func TestAssignmentOutputAcrossViews(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		for _, assignment := range []any{nil, map[string]any{"type": "agent_key", "id": 999}} {
			for _, view := range []string{"item", "task", "queue", "write"} {
				t.Run(fmt.Sprintf("%s/%v/%s", format, assignment, view), func(t *testing.T) {
					calls := 0
					fx := newAuthedFixture(t, format, func(w http.ResponseWriter, r *http.Request) {
						calls++
						if strings.Contains(r.URL.Path, "actors") {
							t.Error("read-only view requested write-only roster")
						}
						item := map[string]any{"id": 203, "title": "docs", "assignee": assignment, "state": "in_review", "pr_number": 412, "completed": false}
						task := map[string]any{"id": 119, "title": "CLI", "checklist_items": []any{item}}
						var body any
						switch view {
						case "task":
							body = map[string]any{"task": task}
						case "queue":
							body = map[string]any{"tasks": []any{task}}
						default:
							body = map[string]any{"checklist_item": item}
						}
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(body)
					})
					var out, stderr bytes.Buffer
					ctx := context.Background()
					var err error
					switch view {
					case "task":
						err = runTaskShow(ctx, &out, &stderr, "119", true, fx.Opts)
					case "queue":
						err = runQueue(ctx, &out, &stderr, client.StateInReview, true, fx.Opts)
					case "item":
						err = runItemShow(ctx, &out, &stderr, "203", fx.Opts)
					case "write":
						err = runItemUpdate(ctx, &out, &stderr, "203", map[string]any{"title": "docs"}, fx.Opts)
					}
					if err != nil {
						t.Fatalf("view: %v", err)
					}
					if calls != 1 {
						t.Fatalf("calls = %d", calls)
					}
					if format == "json" {
						var raw map[string]any
						if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
							t.Fatal(err)
						}
						var item map[string]any
						switch view {
						case "task":
							item = raw["task"].(map[string]any)["checklist_items"].([]any)[0].(map[string]any)
						case "queue":
							item = raw["tasks"].([]any)[0].(map[string]any)["checklist_items"].([]any)[0].(map[string]any)
						default:
							item = raw["checklist_item"].(map[string]any)
						}
						stored, present := item["assignee"]
						want, _ := json.Marshal(assignment)
						got, _ := json.Marshal(stored)
						if !present || string(got) != string(want) {
							t.Fatalf("assignee lost: %s", out.String())
						}
					} else if assignment != nil && !strings.Contains(out.String(), "agent_key:999") {
						t.Fatalf("unknown stored pair hidden: %s", out.String())
					}
				})
			}
		}
	}
}
