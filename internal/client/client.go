// Package client is the HTTP client for the sprawl backend.
//
// It uses only stdlib net/http + encoding/json per the plan. The base URL
// is resolved from SPRAWL_API_URL (one-off override) or the compiled-in
// build.APIURL. Authenticated calls inject `Authorization: Bearer <token>`
// plus whichever narrowing headers are configured — `X-Agent-Secret: <secret>`
// (act as that agent key) and/or `X-Project-Key: <key>` (confine the request
// to one project). The server requires at least one of the two; callers are
// expected to enforce that before building a client so the failure is local.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

const (
	defaultTimeout = 15 * time.Second
	maxBodyBytes   = 1 << 20 // 1 MiB — API bodies are tiny; cap defensively.
)

type Client struct {
	baseURL    string
	token      string // empty for device-flow calls
	secret     string // empty for device-flow calls
	projectKey string // empty ⇒ no project confinement
	http       *http.Client
}

// Option customises an authed client. Options exist for the narrowing factors
// that are optional on the wire, so NewAuthed keeps its two-argument shape for
// the common (agent-secret) case.
type Option func(*Client)

// WithProjectKey confines every request the client makes to a single project
// by sending `X-Project-Key`. Reads come back pre-filtered, creates default to
// that project, and anything outside it is 403. The empty string is a no-op,
// so callers can pass a resolved-but-possibly-empty value unconditionally.
func WithProjectKey(key string) Option {
	return func(c *Client) { c.projectKey = key }
}

// BaseURL returns the effective API URL: SPRAWL_API_URL env override, then
// the ldflag-baked build.APIURL. Trailing slashes are stripped so path joins
// don't produce double slashes.
func BaseURL() string {
	if u := strings.TrimRight(os.Getenv("SPRAWL_API_URL"), "/"); u != "" {
		return u
	}
	return strings.TrimRight(build.APIURL, "/")
}

func New() *Client {
	return &Client{
		baseURL: BaseURL(),
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

func NewAuthed(token, agentSecret string, opts ...Option) *Client {
	c := New()
	c.token = token
	c.secret = agentSecret
	for _, o := range opts {
		o(c)
	}
	return c
}

// ProjectKey returns the project key this client confines requests to, or ""
// when it sends none.
func (c *Client) ProjectKey() string { return c.projectKey }

// DeviceGrant is the response from POST /api/auth/device.
type DeviceGrant struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// CreateDeviceGrant kicks off the device flow. `name` is an optional display
// label the server stores on the resulting bearer token (visible in
// /auth-settings); empty string == omitted, server defaults to "default".
func (c *Client) CreateDeviceGrant(ctx context.Context, name string) (*DeviceGrant, error) {
	var body any
	if name != "" {
		body = map[string]string{"name": name}
	}
	var g DeviceGrant
	if err := c.do(ctx, http.MethodPost, "/api/auth/device", body, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// DevicePollError carries the RFC 8628 error code from the server.
//
// Valid codes per the phase-2 plan: authorization_pending, expired_token,
// access_denied, invalid_grant.
type DevicePollError struct {
	Code string
}

func (e *DevicePollError) Error() string { return "device grant: " + e.Code }

// PollDeviceToken calls POST /api/auth/device/token. On approval returns the
// bearer token. On any of the documented error codes returns *DevicePollError.
func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (string, error) {
	body := map[string]string{"device_code": deviceCode}
	var resp struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	// The server returns 400 for all four non-success states; accept it so we
	// can read the error code from the body.
	accept := func(s int) bool { return s == http.StatusOK || s == http.StatusBadRequest }
	if err := c.doWithStatus(ctx, http.MethodPost, "/api/auth/device/token", body, &resp, accept); err != nil {
		return "", err
	}
	if resp.Token != "" {
		return resp.Token, nil
	}
	if resp.Error != "" {
		return "", &DevicePollError{Code: resp.Error}
	}
	return "", errors.New("device/token: empty response")
}

// Agent is the caller's own identity as seen by the server. `default_permission`
// is the key's baseline scope (`none` / `read` / `write` / `write_create`); per-
// project overrides are listed separately in Whoami.ProjectPermissions.
type Agent struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	Emoji             string `json:"emoji"`
	IsOwner           bool   `json:"is_owner"`
	DefaultPermission string `json:"default_permission"`
}

// ProjectPermission is a per-project scope override that ranks strictly
// higher than the agent's default. Owners and `write_create` defaults always
// produce an empty override list server-side.
type ProjectPermission struct {
	ProjectID int64  `json:"project_id"`
	Name      string `json:"name"`
	Level     string `json:"level"`
}

// WhoamiProject is the project a request was confined to by `X-Project-Key`.
// Level is what the caller actually resolves to there and can legitimately be
// "none" — a valid key naming a project this agent key cannot reach. That is
// deliberately not an auth error, so clients can say "your key is valid but
// this agent has no access" instead of showing an empty task list.
type WhoamiProject struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Key   string `json:"key"`
	Level string `json:"level"`
	// GithubURL is the project's repo, empty when unset (or on a pre-rollout
	// server). Only reachable here under a project key — an unconfined whoami
	// has no project block, so the URL then only arrives nested in tasks.
	GithubURL string `json:"github_url"`
}

// Whoami mirrors GET /api/v1/whoami. The wire payload also carries
// `"status":"ok"`; we drop it on decode since it adds no information beyond
// the 200. Project is nil when the request carried no project key (and on a
// pre-project-keys server, which omits the field entirely).
type Whoami struct {
	Agent              Agent               `json:"agent"`
	Project            *WhoamiProject      `json:"project"`
	ProjectPermissions []ProjectPermission `json:"project_permissions"`
}

func (c *Client) Whoami(ctx context.Context) (*Whoami, error) {
	var w Whoami
	if err := c.do(ctx, http.MethodGet, "/api/v1/whoami", nil, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// themeEnvelope matches the flat wire shape — `{"theme": "<id>"}` — used by
// both GET and PATCH. IDs are lowercase kebab-case (e.g. `tokyo-night`).
type themeEnvelope struct {
	Theme string `json:"theme"`
}

// GetTheme returns the current theme id (e.g. `tokyo-night`).
func (c *Client) GetTheme(ctx context.Context) (string, error) {
	var env themeEnvelope
	if err := c.do(ctx, http.MethodGet, "/api/v1/settings/theme", nil, &env); err != nil {
		return "", err
	}
	return env.Theme, nil
}

// SetTheme PATCHes the active theme by id. The id must already be in
// canonical kebab-case — the server does no normalization, so an unknown or
// mis-cased id surfaces as APIError Status 404 Code "theme_not_found".
// Non-owner agent → APIError Status 403 Code "forbidden". Changeset
// validation failures (e.g. missing / blank `theme` key) use the shared
// fallback shape `{"errors": {...}}` — APIError.Code is empty in that case
// and reportErr surfaces the details map.
func (c *Client) SetTheme(ctx context.Context, id string) (string, error) {
	body := map[string]string{"theme": id}
	var env themeEnvelope
	if err := c.do(ctx, http.MethodPatch, "/api/v1/settings/theme", body, &env); err != nil {
		return "", err
	}
	return env.Theme, nil
}

// -- Phase 4: read endpoints -----------------------------------------------

// Actor identifies the user or agent that created a record or was the last
// to touch it. Null on any record created before phase 5 backfills started.
// Emoji is populated only on agent actors where the server has it; user
// actors and pre-phase-5 records leave it empty (omitempty hides the field).
type Actor struct {
	Type  string `json:"type"`
	ID    int64  `json:"id"`
	Emoji string `json:"emoji,omitempty"`
}

// Project is the nested project shape returned on a Task. Static colour only;
// dynamic (theme-indexed) colours serialise as empty by the server.
//
// Key and GithubURL arrive on every task payload. GithubURL is nullable and is
// validated server-side to be exactly `https://github.com/<owner>/<repo>` — no
// trailing slash, no `.git` — so PRURL can concatenate without normalising.
// Empty means "no URL" (JSON null, or a pre-rollout server that omits the key).
type Project struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Key       string `json:"key"`
	GithubURL string `json:"github_url"`
}

// PRURL builds the GitHub pull-request link for an item's PR number, or "" when
// the chain can't resolve — no project on the task, no github_url on the
// project, or no PR number on the item. Callers render the bare `#<n>` in that
// case; neither break is an error (see the API contract, "Building the PR
// link"). An item's link resolves through its task's *current* project, so
// repainting a card into another project re-points its PR links.
func PRURL(p *Project, prNumber int64) string {
	if p == nil || p.GithubURL == "" || prNumber <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/pull/%d", p.GithubURL, prNumber)
}

// MatchedChecklistItem is the (id, title) of an item whose title matched a
// /tasks/search query. Two fields is the whole shape, deliberately: search is a
// LOOKUP, not a view. It answers "which items mention this?" and hands back the
// ids you then fetch with `item <id>` or `task <id>`.
//
// It is a distinct type from ChecklistItem rather than a sparsely-populated one
// so the narrowness is visible at the call site. Decoding a search hit into a
// full item would leave `completed` false and `state` empty for items that are
// neither, and nothing downstream could tell those zero values from real ones.
type MatchedChecklistItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// ChecklistProgress summarises a task's checklist without paginating items.
type ChecklistProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Task matches the shape documented in task_json.ex. Nullable fields are
// pointers so encoders can emit `null` rather than a zero value.
//
// MatchedChecklistItems is search-only: present (possibly `[]`) on
// /api/v1/tasks/search responses, absent on every other endpoint. Decoding
// keeps that distinction — `nil` slice means "field not in payload",
// non-nil-empty means "search returned no item matches (title-only hit)".
// taskMap relies on that to suppress emission off the search path.
type Task struct {
	ID                    int64                  `json:"id"`
	Title                 string                 `json:"title"`
	Description           string                 `json:"description"`
	Status                string                 `json:"status"`
	DueDate               string                 `json:"due_date"`
	Project               *Project               `json:"project"`
	ChecklistProgress     ChecklistProgress      `json:"checklist_progress"`
	CreatedBy             *Actor                 `json:"created_by"`
	LastActor             *Actor                 `json:"last_actor"`
	MatchedChecklistItems []MatchedChecklistItem `json:"matched_checklist_items,omitempty"`
	// ChecklistItems rides only on GET /tasks/:id?full=true. nil ⇒ the field
	// was absent on the wire (non-full fetch) so renderers suppress it; a
	// non-nil (possibly empty) slice means the server embedded the full
	// checklist. Each item carries its notes inline (ChecklistItem.Notes).
	ChecklistItems []*ChecklistItem `json:"checklist_items,omitempty"`
}

// ChecklistItem mirrors checklist_item_json.ex.
//
// Notes is populated only on the ?full=true read paths (GET /tasks/:id and
// GET /tasks/:id/checklist). The server omits the field on non-full fetches
// and single-item write responses, and serializes empty notes as JSON null on
// the full path — so a nil pointer means either "field absent" or "present but
// empty", indistinguishable from the wire alone. Renderers use the `full` flag
// they already carry to decide whether to emit the key (see cli.itemMap).
// State and PRNumber ride on every item payload. Both are nullable on the wire
// and both use their zero value for null: "" for State, 0 for PRNumber (the
// server rejects a pr_number <= 0, so 0 can't be a real value). That keeps
// callers off pointer juggling; the renderers map the zero value back to a
// literal null. State is the hand-set item state — do NOT conflate it with
// Task.Status, which is derived from checked counts and happens to share the
// string "in_progress".
type ChecklistItem struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Completed bool    `json:"completed"`
	Position  int     `json:"position"`
	State     string  `json:"state"`
	PRNumber  int64   `json:"pr_number"`
	HasNotes  bool    `json:"has_notes"`
	Notes     *string `json:"notes,omitempty"`
	LastActor *Actor  `json:"last_actor"`
}

// The three item states the server accepts. Anything else is a 422; clearing is
// a JSON null, represented here (and in ChecklistItem.State) by "".
const (
	StateReadyToPickup = "ready_to_pickup"
	StateInProgress    = "in_progress"
	StateInReview      = "in_review"
)

// States lists the three states in lifecycle order — the order the TUI cycles
// through and the CLI documents.
var States = []string{StateReadyToPickup, StateInProgress, StateInReview}

// ValidState reports whether s is one of the three server-accepted states.
func ValidState(s string) bool { return slices.Contains(States, s) }

type tasksEnvelope struct {
	Tasks []*Task `json:"tasks"`
}

type taskEnvelope struct {
	Task *Task `json:"task"`
}

type checklistItemEnvelope struct {
	Item *ChecklistItem `json:"checklist_item"`
}

func (c *Client) ListTasks(ctx context.Context) ([]*Task, error) {
	var env tasksEnvelope
	if err := c.do(ctx, http.MethodGet, "/api/v1/tasks", nil, &env); err != nil {
		return nil, err
	}
	return env.Tasks, nil
}

// SearchTasks issues GET /api/v1/tasks/search?q=<query>. Empty/whitespace
// queries surface as an APIError with Status 422 Code "query_required" from
// the server — the CLI doesn't pre-validate so the server stays the single
// source of truth for input rules.
func (c *Client) SearchTasks(ctx context.Context, query string) ([]*Task, error) {
	path := "/api/v1/tasks/search?" + url.Values{"q": []string{query}}.Encode()
	var env tasksEnvelope
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return env.Tasks, nil
}

// GetTask fetches a single task by ID. Server-side IDs are integers; we pass
// the raw string through so the server produces the canonical 404 on a
// malformed ID rather than duplicating the rule here.
//
// When full is true the CLI requests ?full=true, which makes the server embed
// the task's checklist items (each with its notes) under task.checklist_items.
// Without it the response shape is unchanged (checklist_progress counts only).
func (c *Client) GetTask(ctx context.Context, id string, full bool) (*Task, error) {
	var env taskEnvelope
	path := "/api/v1/tasks/" + url.PathEscape(id)
	if full {
		path += "?full=true"
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return env.Task, nil
}

// ActivityLog mirrors GET /api/v1/activity_log: completed tasks + completed
// checklist items for a single day, scoped by the caller's permission cascade.
// The response is not enveloped — top-level keys come straight in.
type ActivityLog struct {
	Date           string                   `json:"date"`
	CompletedTasks []*Task                  `json:"completed_tasks"`
	CompletedItems []*ActivityChecklistItem `json:"completed_items"`
}

// ActivityChecklistItem is a checklist item inside the activity log. It
// extends ChecklistItem with `completed_at` (the timestamp the day grouping
// is built on) and a minimal parent-task summary so clients can group items
// without a follow-up fetch.
type ActivityChecklistItem struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Completed   bool     `json:"completed"`
	CompletedAt string   `json:"completed_at"`
	Position    int      `json:"position"`
	HasNotes    bool     `json:"has_notes"`
	LastActor   *Actor   `json:"last_actor"`
	Task        ItemTask `json:"task"`
}

// ItemTask is the trimmed parent-task view nested under an item — id, title,
// and project only, never a full TaskJSON. Shared by the activity log and the
// by-state queue, which return the identical stub.
type ItemTask struct {
	ID      int64    `json:"id"`
	Title   string   `json:"title"`
	Project *Project `json:"project"`
}

// GetActivityLog issues GET /api/v1/activity_log. `date` (YYYY-MM-DD) and
// `daysAgo` are mutually exclusive — the caller is responsible for enforcing
// that before invoking; the server also rejects the combo with 422
// invalid_date_params, but the CLI catches it earlier so the message is
// crisper. Empty strings for both ⇒ today in the user's timezone.
func (c *Client) GetActivityLog(ctx context.Context, date, daysAgo string) (*ActivityLog, error) {
	path := "/api/v1/activity_log"
	params := url.Values{}
	if date != "" {
		params.Set("date", date)
	}
	if daysAgo != "" {
		params.Set("days_ago", daysAgo)
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var out ActivityLog
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// -- Phase 5: write endpoints ----------------------------------------------

// CreateTask POSTs a new task. `attrs` is the inner task object — the wrapper
// `{"task": {...}}` envelope is added here. The server accepts `title`,
// `description`, and a top-level `project_id` key inside the inner object.
//
// Under a project key `project_id` is optional and usually omitted: the task
// lands in the confined project by default. Passing it is still honoured when
// it names that same project; naming any other project is 403 forbidden, and
// there is no way to create a projectless task while confined.
//
// Validation happens server-side and surfaces as APIError:
//   - malformed `project_id` (non-integer) → 422 Code "invalid_project_id"
//   - unknown / unowned `project_id` → 404 Code "not_found" (runs before
//     permission checks, so callers without create rights on a real project
//     still see 404 when the project is not visible)
//   - non-object nested `task` body → 422 Code "invalid_body"
//   - changeset failures (e.g. missing title) → shared `{"errors": {...}}`
//     fallback shape, surfaced by reportErr as error=invalid + details.
func (c *Client) CreateTask(ctx context.Context, attrs map[string]any) (*Task, error) {
	body := map[string]any{"task": attrs}
	var env taskEnvelope
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks", body, &env); err != nil {
		return nil, err
	}
	return env.Task, nil
}

// UpdateTask PATCHes an existing task. Same envelope as CreateTask. The
// server currently accepts `title` / `description`; other keys are ignored by
// the changeset.
func (c *Client) UpdateTask(ctx context.Context, id string, attrs map[string]any) (*Task, error) {
	body := map[string]any{"task": attrs}
	var env taskEnvelope
	path := "/api/v1/tasks/" + url.PathEscape(id)
	if err := c.do(ctx, http.MethodPatch, path, body, &env); err != nil {
		return nil, err
	}
	return env.Task, nil
}

// SetTaskDueDate PATCHes a preset due date. due=nil clears the due date.
// Server accepts "yesterday" | "today" | "week" | nil and resolves the
// preset against the user's timezone and week_end_day setting; anything
// else surfaces as APIError Status 422 Code "invalid_due". 404 / 403 / 401
// use the standard envelope. Response body is the same task envelope as
// GET /api/v1/tasks/:id, so the resolved ISO date lands in Task.DueDate.
func (c *Client) SetTaskDueDate(ctx context.Context, id string, due *string) (*Task, error) {
	// nil pointer marshals as JSON null, which is the wire signal to clear.
	body := map[string]any{"due": due}
	var env taskEnvelope
	path := "/api/v1/tasks/" + url.PathEscape(id) + "/due_date"
	if err := c.do(ctx, http.MethodPatch, path, body, &env); err != nil {
		return nil, err
	}
	return env.Task, nil
}

// CreateChecklistItem POSTs a new item under a task. Body envelope is
// `{"checklist_item": {...}}`; response is the same envelope.
func (c *Client) CreateChecklistItem(ctx context.Context, taskID string, attrs map[string]any) (*ChecklistItem, error) {
	body := map[string]any{"checklist_item": attrs}
	var env checklistItemEnvelope
	path := "/api/v1/tasks/" + url.PathEscape(taskID) + "/checklist"
	if err := c.do(ctx, http.MethodPost, path, body, &env); err != nil {
		return nil, err
	}
	return env.Item, nil
}

// SetChecklistItemCompleted sets the item's completion state explicitly. The
// server is idempotent (no-ops when the state already matches) and returns the
// updated item.
func (c *Client) SetChecklistItemCompleted(ctx context.Context, itemID string, completed bool) (*ChecklistItem, error) {
	body := map[string]bool{"completed": completed}
	var env checklistItemEnvelope
	path := "/api/v1/checklist_items/" + url.PathEscape(itemID) + "/completed"
	if err := c.do(ctx, http.MethodPatch, path, body, &env); err != nil {
		return nil, err
	}
	return env.Item, nil
}

// SetChecklistItemState PATCHes an item's state and/or PR number through the
// dedicated route. `attrs` is sent as the body verbatim — NOT wrapped in a
// `checklist_item` envelope, unlike UpdateChecklistItem — with at most the two
// keys `state` and `pr_number`. An absent key means "leave unchanged"; a key
// present with a nil value clears the field.
//
// Two behaviours callers must expect:
//   - Setting a state on a COMPLETED item un-completes it (state and completed
//     are mutually exclusive server-side). The returned item carries the new
//     completed=false, so refresh any progress derived from it.
//   - PR number is independent: it survives completion and state changes, and
//     is only cleared by explicitly sending `pr_number: nil`.
//
// The generic PATCH /checklist_items/:id silently ignores these two keys, which
// is why they get their own route. Errors surface as APIError 422
// (`invalid_state`, `invalid_pr_number`, or neither key present) / 404 / 403.
func (c *Client) SetChecklistItemState(ctx context.Context, itemID string, attrs map[string]any) (*ChecklistItem, error) {
	var env checklistItemEnvelope
	path := "/api/v1/checklist_items/" + url.PathEscape(itemID) + "/state"
	if err := c.do(ctx, http.MethodPatch, path, attrs, &env); err != nil {
		return nil, err
	}
	return env.Item, nil
}

// ItemDetail is a checklist item plus the trimmed parent task (with its
// project). It is the shape of BOTH single-item reads that carry context: the
// by-state queue's elements and GET /checklist_items/:id. The task stub is what
// carries the project, and the project's github_url is what turns pr_number into
// a link — drop it and PR links silently stop resolving.
//
// Notes ride on the detail read (always, it has no ?full variant) and not on the
// queue, which is exactly the ChecklistItem.Notes contract.
type ItemDetail struct {
	ChecklistItem
	Task ItemTask `json:"task"`
}

type queueEnvelope struct {
	Items []*ItemDetail `json:"checklist_items"`
}

type itemDetailEnvelope struct {
	Item *ItemDetail `json:"checklist_item"`
}

// GetChecklistItem fetches one checklist item by id, with its notes and its
// parent-task stub. This is the only single-item READ — every other single-item
// route is a write — so it is what makes a bare item id inspectable rather than
// merely writable.
//
// Permission inherits from the parent task, like every other /checklist_items/*
// route: 403 when the caller can't read it (which is what a project-key-confined
// caller gets for an item outside the confined project), 404 when it doesn't
// exist. Both surface as APIError.
func (c *Client) GetChecklistItem(ctx context.Context, itemID string) (*ItemDetail, error) {
	var env itemDetailEnvelope
	path := "/api/v1/checklist_items/" + url.PathEscape(itemID)
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return env.Item, nil
}

// ListChecklistItemsByState issues GET /api/v1/checklist_items?state=<state>,
// returning matching items across every task the scope can read. state is
// required and must be one of the three server states — anything else is a 422
// invalid_state. Results honour project confinement and the same readability
// cascade as every other list endpoint.
//
// No `completed` filter is needed: completing an item clears its state, so any
// item carrying a state is by construction incomplete.
//
// full asks the server to include each item's notes inline, the same ?full=true
// this CLI sends on the task read. A server that doesn't implement it simply
// ignores the param and returns items without notes, which renders as "no note
// to show" rather than as an error.
func (c *Client) ListChecklistItemsByState(ctx context.Context, state string, full bool) ([]*ItemDetail, error) {
	params := url.Values{"state": []string{state}}
	if full {
		params.Set("full", "true")
	}
	path := "/api/v1/checklist_items?" + params.Encode()
	var env queueEnvelope
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return env.Items, nil
}

// UpdateChecklistItem PATCHes item fields. The server accepts `title` and
// `notes` via the item changeset. Completion is only mutated through
// SetChecklistItemCompleted; state and pr_number only through
// SetChecklistItemState (this route ignores both keys).
func (c *Client) UpdateChecklistItem(ctx context.Context, itemID string, attrs map[string]any) (*ChecklistItem, error) {
	body := map[string]any{"checklist_item": attrs}
	var env checklistItemEnvelope
	path := "/api/v1/checklist_items/" + url.PathEscape(itemID)
	if err := c.do(ctx, http.MethodPatch, path, body, &env); err != nil {
		return nil, err
	}
	return env.Item, nil
}

// DeleteTask soft-deletes a task. The server returns 204 No Content on
// success; the row stays in the DB with hidden=true and deleted_at set, and
// neighbor cards are reflowed atomically. 404 / 403 / 401 surface as
// APIError; the CLI layer is responsible for translating 404 "not_found"
// into idempotent success when desired.
func (c *Client) DeleteTask(ctx context.Context, id string) error {
	path := "/api/v1/tasks/" + url.PathEscape(id)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// DeleteChecklistItem hard-deletes a checklist item. The server returns 204
// No Content on success and recomputes the parent task's completed_at.
// Same 204/404 contract as DeleteTask.
func (c *Client) DeleteChecklistItem(ctx context.Context, itemID string) error {
	path := "/api/v1/checklist_items/" + url.PathEscape(itemID)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// APIError represents a non-2xx response from the server.
type APIError struct {
	Status int
	Code   string // value of `error` field in the JSON body, if present
	Body   string // raw body, truncated
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("http %d: %s", e.Status, e.Code)
	}
	snippet := e.Body
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	return fmt.Sprintf("http %d: %s", e.Status, snippet)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.doWithStatus(ctx, method, path, body, out, func(s int) bool {
		return s >= 200 && s < 300
	})
}

func (c *Client) doWithStatus(
	ctx context.Context, method, path string,
	body, out any, accept func(int) bool,
) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	// Plain concat: baseURL has trailing slashes trimmed by BaseURL(), and
	// callers always pass a path that starts with '/'. url.JoinPath is the
	// wrong tool here — it percent-escapes '?' in the path, which breaks the
	// search endpoint's query string.
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.secret != "" {
		req.Header.Set("X-Agent-Secret", c.secret)
	}
	// Both narrowing headers are optional individually; sending both is legal
	// and intersects them (effective permission is the min across factors).
	if c.projectKey != "" {
		req.Header.Set("X-Project-Key", c.projectKey)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	// Read one byte beyond the cap so truncation surfaces as an explicit
	// error instead of a downstream JSON decode failure on a chopped body.
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("%s %s: read body: %w", method, path, err)
	}
	if len(raw) > maxBodyBytes {
		return fmt.Errorf("%s %s: response body exceeds %d bytes (status %d)",
			method, path, maxBodyBytes, res.StatusCode)
	}
	if !accept(res.StatusCode) {
		return apiErrorFromResponse(res.StatusCode, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s %s: decode response: %w", method, path, err)
		}
	}
	return nil
}

func apiErrorFromResponse(status int, raw []byte) *APIError {
	var parsed struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed) // best-effort
	body := string(raw)
	if len(body) > 1024 {
		body = body[:1024]
	}
	return &APIError{Status: status, Code: parsed.Error, Body: body}
}
