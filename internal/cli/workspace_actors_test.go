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

func TestWorkspaceActorsResolutionAndOutput(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		for _, tc := range []struct {
			name, explicit, selected, env string
			paths                         []string
		}{
			{"default", "", "", "", []string{"/api/v1/whoami", "/api/v1/workspaces/42/actors"}},
			{"explicit", "42", "", "", []string{"/api/v1/workspaces/42/actors"}},
			{"selected", "", "42", "", []string{"/api/v1/workspaces/42/actors"}},
			{"matching", "42", "42", "", []string{"/api/v1/workspaces/42/actors"}},
			{"environment", "", "", "42", []string{"/api/v1/workspaces/42/actors"}},
			{"flag overrides environment", "", "42", "9", []string{"/api/v1/workspaces/42/actors"}},
		} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				var paths []string
				fx := newAuthedFixture(t, format, func(w http.ResponseWriter, r *http.Request) {
					paths = append(paths, r.URL.Path)
					if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer the-token" || r.Header.Get("X-Agent-Secret") != "the-secret" || r.Header.Get("X-Project-Key") != "acme" {
						t.Error("method or credentials changed")
					}
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path == "/api/v1/whoami" {
						_, _ = io.WriteString(w, `{"workspace":{"id":42,"name":"Shared","role":"write","level":"write"}}`)
					} else {
						_, _ = io.WriteString(w, `{"actors":[{"type":"user","id":3,"label":"Alex","marker":"🦊"},{"type":"agent_key","id":3,"label":"builder","marker":"🤖"}]}`)
					}
				})
				fx.Opts.workspace, fx.Opts.projectKey = tc.selected, "acme"
				t.Setenv("SPRAWL_WORKSPACE", tc.env)
				var out, stderr bytes.Buffer
				if err := runWorkspaceActors(context.Background(), &out, &stderr, tc.explicit, fx.Opts); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(paths, tc.paths) {
					t.Fatalf("paths = %v, want %v", paths, tc.paths)
				}
				if format == "json" {
					var got struct {
						Actors []client.WorkspaceActor `json:"actors"`
					}
					if err := json.Unmarshal(out.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					if len(got.Actors) != 2 || got.Actors[0].Type != "user" || got.Actors[1].Type != "agent_key" || got.Actors[1].Marker != "🤖" || got.Actors[0].Label != "Alex" {
						t.Fatalf("actors lost: %s", out.String())
					}
				} else {
					for _, want := range []string{"user:3", "agent_key:3", "Alex", "builder", "🦊", "🤖"} {
						if !strings.Contains(out.String(), want) {
							t.Errorf("missing %q in %s", want, out.String())
						}
					}
				}
			})
		}
	}
}

func TestWorkspaceActorsLocalErrors(t *testing.T) {
	for _, tc := range []struct{ id, selected string }{{"42", "9"}, {"-1", ""}, {"0", ""}, {"abc", ""}, {"", "abc"}} {
		fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
		fx.Opts.workspace = tc.selected
		if err := runWorkspaceActors(context.Background(), io.Discard, io.Discard, tc.id, fx.Opts); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
}

func TestWorkspaceActorsMissingDefaultAndEmptyRoster(t *testing.T) {
	for _, body := range []string{`{}`, `{"workspace":null}`, `{"workspace":{"id":0}}`} {
		fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/whoami" {
				t.Error("unexpected actor request")
			}
			_, _ = io.WriteString(w, body)
		})
		if err := runWorkspaceActors(context.Background(), io.Discard, io.Discard, "", fx.Opts); !errors.Is(err, errServerNoWorkspaces) {
			t.Fatalf("err = %v", err)
		}
	}
	for _, format := range []string{"json", "text"} {
		fx := newAuthedFixture(t, format, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"actors":[]}`) })
		var out bytes.Buffer
		if err := runWorkspaceActors(context.Background(), &out, io.Discard, "42", fx.Opts); err != nil {
			t.Fatal(err)
		}
		want := `{"actors":[]}`
		if format == "text" {
			want = "(no eligible assignees)"
		}
		if strings.TrimSpace(out.String()) != want {
			t.Fatalf("output = %q", out.String())
		}
	}
}

func TestWorkspaceActorsRefusal(t *testing.T) {
	for _, status := range []int{401, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"forbidden"}`)
			})
			var out bytes.Buffer
			err := runWorkspaceActors(context.Background(), &out, io.Discard, "42", fx.Opts)
			var apiErr *client.APIError
			if !errors.As(err, &apiErr) || apiErr.Status != status || !strings.Contains(out.String(), `"status":"error"`) {
				t.Fatalf("error lost: %v / %s", err, out.String())
			}
		})
	}
}

func TestDiscoveryDoesNotReserveAssignment(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = io.WriteString(w, `{"actors":[{"type":"user","id":3,"label":"Alex","marker":"🦊"}]}`)
		} else {
			w.WriteHeader(422)
			_, _ = io.WriteString(w, `{"error":"invalid_assignee"}`)
		}
	})
	if err := runWorkspaceActors(context.Background(), io.Discard, io.Discard, "42", fx.Opts); err != nil {
		t.Fatal(err)
	}
	fx.Opts.workspace = "42"
	var out bytes.Buffer
	err := runItemUpdate(context.Background(), &out, io.Discard, "203", map[string]any{"assignee": &client.Assignee{Type: "user", ID: 3}}, fx.Opts)
	if err == nil || !strings.Contains(out.String(), `"error":"invalid_assignee"`) || strings.Contains(out.String(), `"checklist_item"`) {
		t.Fatalf("refusal = %v / %s", err, out.String())
	}
}
