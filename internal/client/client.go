// Package client is the HTTP client for the sprawl backend.
//
// It uses only stdlib net/http + encoding/json per the plan. The base URL
// is resolved from SPRAWL_API_URL (one-off override) or the compiled-in
// build.APIURL. Authenticated calls inject `Authorization: Bearer <token>`
// and `X-Agent-Secret: <secret>` headers.
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
	"strings"
	"time"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

const (
	defaultTimeout = 15 * time.Second
	maxBodyBytes   = 1 << 20 // 1 MiB — API bodies are tiny; cap defensively.
)

type Client struct {
	baseURL string
	token   string // empty for device-flow calls
	secret  string // empty for device-flow calls
	http    *http.Client
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

func NewAuthed(token, agentSecret string) *Client {
	c := New()
	c.token = token
	c.secret = agentSecret
	return c
}

// DeviceGrant is the response from POST /api/auth/device.
type DeviceGrant struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func (c *Client) CreateDeviceGrant(ctx context.Context) (*DeviceGrant, error) {
	var g DeviceGrant
	if err := c.do(ctx, http.MethodPost, "/api/auth/device", nil, &g); err != nil {
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

func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/api/v1/health", nil, nil)
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
// Non-owner agent → APIError Status 403 Code "forbidden".
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
type Actor struct {
	Type string `json:"type"`
	ID   int64  `json:"id"`
}

// Project is the nested project shape returned on a Task. Static colour only;
// dynamic (theme-indexed) colours serialise as empty by the server.
type Project struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ChecklistProgress summarises a task's checklist without paginating items.
type ChecklistProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Task matches the shape documented in task_json.ex. Nullable fields are
// pointers so encoders can emit `null` rather than a zero value.
type Task struct {
	ID                int64             `json:"id"`
	Title             string            `json:"title"`
	Description       string            `json:"description"`
	Status            string            `json:"status"`
	DueDate           string            `json:"due_date"`
	Project           *Project          `json:"project"`
	ChecklistProgress ChecklistProgress `json:"checklist_progress"`
	CreatedBy         *Actor            `json:"created_by"`
	LastActor         *Actor            `json:"last_actor"`
}

// ChecklistItem mirrors checklist_item_json.ex.
type ChecklistItem struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
	Position  int    `json:"position"`
	HasNotes  bool   `json:"has_notes"`
	LastActor *Actor `json:"last_actor"`
}

type tasksEnvelope struct {
	Tasks []*Task `json:"tasks"`
}

type taskEnvelope struct {
	Task *Task `json:"task"`
}

type checklistEnvelope struct {
	Items []*ChecklistItem `json:"checklist_items"`
}

type notesEnvelope struct {
	Notes string `json:"notes"`
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
func (c *Client) GetTask(ctx context.Context, id string) (*Task, error) {
	var env taskEnvelope
	if err := c.do(ctx, http.MethodGet, "/api/v1/tasks/"+url.PathEscape(id), nil, &env); err != nil {
		return nil, err
	}
	return env.Task, nil
}

func (c *Client) ListChecklistItems(ctx context.Context, taskID string) ([]*ChecklistItem, error) {
	var env checklistEnvelope
	path := "/api/v1/tasks/" + url.PathEscape(taskID) + "/checklist"
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return nil, err
	}
	return env.Items, nil
}

// GetNotes returns the raw notes blob for a checklist item. An empty string
// is a legitimate result (item exists, no notes). 404 / 403 surface as
// APIError.
func (c *Client) GetNotes(ctx context.Context, itemID string) (string, error) {
	var env notesEnvelope
	path := "/api/v1/checklist_items/" + url.PathEscape(itemID) + "/notes"
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		return "", err
	}
	return env.Notes, nil
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

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("%s %s: read body: %w", method, path, err)
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
