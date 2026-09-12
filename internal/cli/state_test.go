package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/icons"
)

// -- pure parsing -----------------------------------------------------------

func TestParseState_AliasesAndWireValues(t *testing.T) {
	cases := map[string]string{
		"ready":           client.StateReadyToPickup,
		"progress":        client.StateInProgress,
		"review":          client.StateInReview,
		"READY":           client.StateReadyToPickup,
		"  review  ":      client.StateInReview,
		"ready_to_pickup": client.StateReadyToPickup,
		"in_progress":     client.StateInProgress,
		"in_review":       client.StateInReview,
		"none":            "",
		"clear":           "",
		"null":            "",
	}
	for in, want := range cases {
		got, err := parseState(in)
		if err != nil {
			t.Errorf("parseState(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseState_RejectsUnknown(t *testing.T) {
	// The task-level `status` vocabulary must not be accepted here: `done` and
	// `not_started` are a different field on a different object.
	for _, in := range []string{"done", "not_started", "reviewing", ""} {
		if _, err := parseState(in); err == nil {
			t.Errorf("parseState(%q) accepted", in)
		}
	}
	_, err := parseState("nope")
	if err == nil || !strings.Contains(err.Error(), "ready|progress|review|none") {
		t.Fatalf("error should name the accepted set, got %v", err)
	}
}

func TestParsePRNumber(t *testing.T) {
	n, err := parsePRNumber("412")
	if err != nil || n == nil || *n != 412 {
		t.Fatalf("412 → %v, %v", n, err)
	}
	for _, in := range []string{"none", "clear", "NULL"} {
		n, err := parsePRNumber(in)
		if err != nil || n != nil {
			t.Fatalf("%q → %v, %v", in, n, err)
		}
	}
	// The server rejects <= 0; catching it locally keeps the message specific.
	for _, in := range []string{"0", "-1", "abc", "4.5", ""} {
		if _, err := parsePRNumber(in); err == nil {
			t.Errorf("parsePRNumber(%q) accepted", in)
		}
	}
}

// -- state / pr commands ----------------------------------------------------

func TestRunItemState_SetsState(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeItemJSON(w, map[string]any{
			"id": 7, "title": "add the migration", "completed": false, "position": 1,
			"state": "in_review", "pr_number": 412, "has_notes": false, "last_actor": nil,
		})
	})
	var out, errOut bytes.Buffer

	if err := runItemState(context.Background(), &out, &errOut, "7",
		map[string]any{"state": "in_review"}, itemStateSummary, fx.Opts); err != nil {
		t.Fatalf("runItemState: %v", err)
	}

	if gotMethod != http.MethodPatch || gotPath != "/api/v1/checklist_items/7/state" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody["state"] != "in_review" {
		t.Fatalf("body = %v", gotBody)
	}
	var payload struct {
		Item struct {
			State    string `json:"state"`
			PRNumber int64  `json:"pr_number"`
		} `json:"checklist_item"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode output: %v — %s", err, out.String())
	}
	// Short form on the way out, wire value on the way in.
	if payload.Item.State != "review" || payload.Item.PRNumber != 412 {
		t.Fatalf("payload = %+v", payload.Item)
	}
}

// The one-line write summaries, which replaced the old item rendering.
func TestItemStateAndPRSummaries(t *testing.T) {
	for _, tc := range []struct {
		item *client.ChecklistItem
		want string
	}{
		{&client.ChecklistItem{ID: 7, State: client.StateInReview}, "✓ item 7 → review"},
		{&client.ChecklistItem{ID: 7}, "✓ item 7 → no state"},
	} {
		if got := itemStateSummary(tc.item); got != tc.want {
			t.Errorf("itemStateSummary = %q, want %q", got, tc.want)
		}
	}
	for _, tc := range []struct {
		item *client.ChecklistItem
		want string
	}{
		{&client.ChecklistItem{ID: 7, PRNumber: 412}, "✓ item 7 → PR #412"},
		{&client.ChecklistItem{ID: 7}, "✓ item 7 → no PR"},
	} {
		if got := itemPRSummary(tc.item); got != tc.want {
			t.Errorf("itemPRSummary = %q, want %q", got, tc.want)
		}
	}
}

func TestItemStateCmd_ClearSendsNull(t *testing.T) {
	var gotBody map[string]any
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeItemJSON(w, map[string]any{"id": 7, "title": "x", "state": nil, "pr_number": nil})
	})

	cmd := newItemStateCmd(fx.Opts)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"7", "none"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	v, present := gotBody["state"]
	if !present || v != nil {
		t.Fatalf("clear must send a present null state, got %v", gotBody)
	}
	// Clearing state must not touch the PR number.
	if _, present := gotBody["pr_number"]; present {
		t.Fatalf("pr_number must be absent, got %v", gotBody)
	}
}

func TestItemStateCmd_InvalidValueFailsLocally(t *testing.T) {
	called := false
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeItemJSON(w, map[string]any{"id": 7})
	})

	var out, errOut bytes.Buffer
	cmd := newItemStateCmd(fx.Opts)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"7", "done"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want an error for an invalid state")
	}
	if called {
		t.Fatal("no request should go out for an invalid state")
	}
	if !strings.Contains(out.String(), "invalid state") {
		t.Fatalf("structured error should name the problem: %s", out.String())
	}
}

func TestItemPRCmd_SetAndClear(t *testing.T) {
	var bodies []map[string]any
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		writeItemJSON(w, map[string]any{"id": 7, "title": "x", "state": nil, "pr_number": 412})
	})

	for _, args := range [][]string{{"7", "412"}, {"7", "none"}} {
		cmd := newItemPRCmd(fx.Opts)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute %v: %v", args, err)
		}
	}

	if len(bodies) != 2 {
		t.Fatalf("requests = %d", len(bodies))
	}
	if bodies[0]["pr_number"] != float64(412) {
		t.Fatalf("set body = %v", bodies[0])
	}
	v, present := bodies[1]["pr_number"]
	if !present || v != nil {
		t.Fatalf("clear body = %v", bodies[1])
	}
	// Neither call may carry a state key — the PR number is independent, and
	// sending state:null here would silently wipe a state the user still wants.
	for i, b := range bodies {
		if _, present := b["state"]; present {
			t.Fatalf("body %d leaked a state key: %v", i, b)
		}
	}
}

// -- queue ------------------------------------------------------------------

func TestRunQueue_JSONPayloadAndPRURL(t *testing.T) {
	var gotState string
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		gotState = r.URL.Query().Get("state")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{
			map[string]any{
				"id": 3, "title": "Ship the API", "description": "the whole thing", "due_date": "2026-09-20",
				"project": map[string]any{
					"id": 1, "name": "Sprawl", "key": "sprawl", "color": "",
					"github_url": "https://github.com/ultrakorne/sprawl",
				},
				"checklist_items": []any{map[string]any{
					"id": 7, "title": "add the migration", "completed": false, "position": 1,
					"state": "in_review", "pr_number": 412, "has_notes": true, "last_actor": nil,
				}},
			},
			map[string]any{
				"id": 4, "title": "Loose", "description": "", "due_date": nil, "project": nil,
				"checklist_items": []any{map[string]any{
					"id": 9, "title": "unlinked", "completed": false, "position": 2,
					"state": "in_review", "pr_number": 77, "has_notes": false, "last_actor": nil,
				}},
			},
		}})
	})
	var out, errOut bytes.Buffer

	if err := runQueue(context.Background(), &out, &errOut, client.StateInReview, false, fx.Opts); err != nil {
		t.Fatalf("runQueue: %v", err)
	}
	if gotState != client.StateInReview {
		t.Fatalf("state query = %q", gotState)
	}

	type item struct {
		ID       int64   `json:"id"`
		State    string  `json:"state"`
		PRNumber int64   `json:"pr_number"`
		PRURL    *string `json:"pr_url"`
	}
	var payload struct {
		Tasks []struct {
			ID          int64   `json:"id"`
			Title       string  `json:"title"`
			Description string  `json:"description"`
			DueDate     *string `json:"due_date"`
			Project     *struct {
				Key       string  `json:"key"`
				GithubURL *string `json:"github_url"`
			} `json:"project"`
			Items []item `json:"checklist_items"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v — %s", err, out.String())
	}
	// The top-level key is `tasks`, never `checklist_items` — items live only
	// inside their group, so a consumer on the old flat shape fails loudly
	// instead of reading an empty queue.
	var top map[string]any
	if err := json.Unmarshal(out.Bytes(), &top); err != nil {
		t.Fatalf("decode top: %v", err)
	}
	if _, ok := top["tasks"]; !ok {
		t.Fatalf("payload has no tasks key: %s", out.String())
	}
	if _, ok := top["checklist_items"]; ok {
		t.Fatalf("payload still carries a top-level checklist_items: %s", out.String())
	}
	if len(payload.Tasks) != 2 {
		t.Fatalf("tasks = %d", len(payload.Tasks))
	}
	first := payload.Tasks[0]
	if first.ID != 3 || first.Title != "Ship the API" || first.Description != "the whole thing" {
		t.Fatalf("first group = %+v", first)
	}
	if first.DueDate == nil || *first.DueDate != "2026-09-20" {
		t.Fatalf("due_date = %v", first.DueDate)
	}
	if first.Project == nil || first.Project.Key != "sprawl" {
		t.Fatalf("task project = %+v", first.Project)
	}
	if len(first.Items) != 1 {
		t.Fatalf("first items = %d", len(first.Items))
	}
	if first.Items[0].State != "review" {
		t.Fatalf("queue state = %q, want the short form", first.Items[0].State)
	}
	if first.Items[0].PRURL == nil || *first.Items[0].PRURL != "https://github.com/ultrakorne/sprawl/pull/412" {
		t.Fatalf("pr_url = %v", first.Items[0].PRURL)
	}
	// A task with no project can't resolve a link — the number stays, the URL
	// is null. Neither is an error. A null due_date stays null.
	second := payload.Tasks[1]
	if second.Project != nil || second.DueDate != nil {
		t.Fatalf("second group = %+v", second)
	}
	if len(second.Items) != 1 || second.Items[0].PRURL != nil {
		t.Fatalf("unresolvable pr_url = %+v", second.Items)
	}
	if second.Items[0].PRNumber != 77 {
		t.Fatalf("pr number dropped: %+v", second.Items[0])
	}
}

// `queue --full` has to send ?full=true and surface the notes it buys, in both
// the payload and the rendering. queueText alone can't catch a dropped param.
func TestRunQueue_FullSendsParamAndEmitsNotes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		full      bool
		wantParam string
	}{
		{"non-full", false, ""},
		{"full", true, "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotFull string
			fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
				gotFull = r.URL.Query().Get("full")
				w.Header().Set("Content-Type", "application/json")
				item := map[string]any{
					"id": 7, "title": "add the migration", "completed": false, "position": 1,
					"state": "in_review", "pr_number": 412, "has_notes": true,
				}
				// Mirror the server: notes ride only on the full read.
				if tc.full {
					item["notes"] = "blocked on the backfill"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{map[string]any{
					"id": 3, "title": "Ship the API", "description": "", "due_date": nil, "project": nil,
					"checklist_items": []any{item},
				}}})
			})

			var out, errOut bytes.Buffer
			if err := runQueue(context.Background(), &out, &errOut, client.StateInReview, tc.full, fx.Opts); err != nil {
				t.Fatalf("runQueue: %v", err)
			}
			if gotFull != tc.wantParam {
				t.Fatalf("full query = %q, want %q", gotFull, tc.wantParam)
			}

			var payload struct {
				Tasks []struct {
					Items []map[string]any `json:"checklist_items"`
				} `json:"tasks"`
			}
			if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
				t.Fatalf("decode: %v — %s", err, out.String())
			}
			if len(payload.Tasks) != 1 || len(payload.Tasks[0].Items) != 1 {
				t.Fatalf("payload = %s", out.String())
			}
			got := payload.Tasks[0].Items[0]
			notes, present := got["notes"]
			if tc.full {
				if !present || notes != "blocked on the backfill" {
					t.Fatalf("notes = %v (present=%v), want the body", notes, present)
				}
			} else if present {
				t.Fatalf("notes must be absent when they weren't fetched: %+v", got)
			}
			// has_notes rides either way — it's what drives the NOTES column.
			if got["has_notes"] != true {
				t.Fatalf("has_notes = %v", got["has_notes"])
			}
		})
	}
}

func TestQueueCmd_DefaultsToReadyToPickup(t *testing.T) {
	var gotState string
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		gotState = r.URL.Query().Get("state")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
	})

	cmd := newQueueCmd(fx.Opts)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotState != client.StateReadyToPickup {
		t.Fatalf("default state = %q", gotState)
	}
}

func TestQueueCmd_RejectsNoneState(t *testing.T) {
	called := false
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	var out bytes.Buffer
	cmd := newQueueCmd(fx.Opts)
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--state", "none"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want an error for --state none")
	}
	if called {
		t.Fatal("no request should go out")
	}
}

func TestQueueText_EmptyAndRows(t *testing.T) {
	if got := queueText(nil, client.StateReadyToPickup, false); !strings.Contains(got, "no items in ready") {
		t.Fatalf("empty = %q", got)
	}
	// A group whose task has no items in the state contributes nothing, and
	// counts for nothing — the server never sends one, but an empty queue must
	// not depend on that.
	if got := queueText([]*client.QueueTask{{ID: 1, Title: "hollow"}}, client.StateReadyToPickup, false); !strings.Contains(got, "no items in ready") {
		t.Fatalf("hollow group = %q", got)
	}
	items := []*client.QueueTask{{
		ID: 3, Title: "Ship the API", Description: "the whole thing\nover two lines", DueDate: "2026-09-20",
		Project: &client.Project{ID: 1, Name: "Sprawl"},
		ChecklistItems: []*client.ChecklistItem{
			{ID: 7, Title: "add the migration", State: client.StateInReview, PRNumber: 412, HasNotes: true, Notes: ptr("the note")},
		},
	}, {
		ID: 4, Title: "Loose",
		ChecklistItems: []*client.ChecklistItem{
			{ID: 9, Title: "unlinked", State: client.StateInReview},
		},
	}}
	got := queueText(items, client.StateInReview, false)
	// The task id is bare here too — the `#` belongs to PR numbers only. The
	// group header carries the task's title, description, project and due
	// date, so none of them needs a column.
	for _, want := range []string{"review", "(2)", "#412", "add the migration", "3 Ship the API", "Sprawl", "due 2026-09-20",
		"the whole thing\nover two lines", "4 Loose", "unlinked"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The checkbox and STATE columns are dropped: every queued item is
	// incomplete by construction and the state is the query, so both would be
	// constant down the column. TASK and PROJECT are gone too — the group
	// header names both.
	for _, unwanted := range []string{"[x]", "STATE", "TASK", "PROJECT"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("queue should not render %q:\n%s", unwanted, got)
		}
	}
	// Each group is its own header + table; the second group's header comes
	// after the first group's rows.
	if strings.Index(got, "4 Loose") < strings.Index(got, "add the migration") {
		t.Errorf("groups out of order:\n%s", got)
	}
	// --full expands the note under the row; without it the NOTES column alone
	// says there is one.
	if strings.Contains(got, "note: the note") {
		t.Errorf("non-full queue must not expand notes:\n%s", got)
	}
	if full := queueText(items, client.StateInReview, true); !strings.Contains(full, "note: the note") {
		t.Errorf("--full should expand the note:\n%s", full)
	}
}

// -- item / project rendering ----------------------------------------------

func TestItemMap_EmitsNullForClearedFields(t *testing.T) {
	m := itemMap(&client.ChecklistItem{ID: 7, Title: "x"}, nil, false)
	// The keys are always present — a consumer shouldn't have to distinguish
	// "absent" from "null" to answer "does this item have a state?".
	for _, key := range []string{"state", "pr_number"} {
		v, present := m[key]
		if !present {
			t.Errorf("%s key missing", key)
		}
		if v != nil {
			t.Errorf("%s = %v, want nil", key, v)
		}
	}

	m = itemMap(&client.ChecklistItem{ID: 7, Title: "x", State: client.StateInReview, PRNumber: 412}, nil, false)
	if m["state"] != "review" || m["pr_number"] != int64(412) {
		t.Fatalf("set item = %v", m)
	}
}

func TestProjectMap_CarriesKeyAndGithubURL(t *testing.T) {
	m := projectMap(&client.Project{ID: 1, Name: "Sprawl", Key: "sprawl", Color: "#3B82F6",
		GithubURL: "https://github.com/ultrakorne/sprawl"}).(map[string]any)
	if m["key"] != "sprawl" || m["github_url"] != "https://github.com/ultrakorne/sprawl" {
		t.Fatalf("project = %v", m)
	}
	m = projectMap(&client.Project{ID: 2, Name: "No repo"}).(map[string]any)
	if m["github_url"] != nil {
		t.Fatalf("unset github_url = %v", m["github_url"])
	}
	if projectMap(nil) != nil {
		t.Fatal("nil project must stay null")
	}
}

func TestItemTable_ShowsStateAndPR(t *testing.T) {
	t.Setenv("SPRAWL_ICONS", "plain")
	views := []itemView{
		{item: &client.ChecklistItem{ID: 5, Title: "plain"}},
		{item: &client.ChecklistItem{ID: 6, Title: "in review", State: client.StateInReview, PRNumber: 412}},
	}
	got := itemTable(views, itemCols{checkbox: true, state: true}, false)
	for _, want := range []string{"STATE", "PR", icons.Plain.Review, "#412"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The stateless item shows a placeholder, not an empty cell.
	if !strings.Contains(got, "-") {
		t.Errorf("stateless row should render a placeholder:\n%s", got)
	}
	// The row carries the ICON, never the word.
	if strings.Contains(got, "review ") || strings.Contains(got, "in_review") {
		t.Errorf("rows must not spell the state out:\n%s", got)
	}
}

func TestStateLabel_PassesUnknownThrough(t *testing.T) {
	if stateLabel("") != "" {
		t.Fatal("empty state must render empty")
	}
	// A state added server-side after this build should be visible, not hidden.
	if got := stateLabel("blocked"); got != "blocked" {
		t.Fatalf("unknown state = %q", got)
	}
}

// writeItemJSON writes a `{"checklist_item": …}` envelope response.
func writeItemJSON(w http.ResponseWriter, item map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"checklist_item": item})
}
