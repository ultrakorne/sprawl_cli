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

// Theme is the shape returned by /api/v1/settings/theme (both GET and PATCH).
type Theme struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type themeEnvelope struct {
	Theme Theme `json:"theme"`
}

func (c *Client) GetTheme(ctx context.Context) (*Theme, error) {
	var env themeEnvelope
	if err := c.do(ctx, http.MethodGet, "/api/v1/settings/theme", nil, &env); err != nil {
		return nil, err
	}
	return &env.Theme, nil
}

// SetTheme PATCHes the active theme by name (server match is case-insensitive).
// Unknown name → APIError with Status 404 and Code "theme_not_found".
// Non-owner agent → APIError with Status 403 and Code "forbidden".
func (c *Client) SetTheme(ctx context.Context, name string) (*Theme, error) {
	body := map[string]string{"theme": name}
	var env themeEnvelope
	if err := c.do(ctx, http.MethodPatch, "/api/v1/settings/theme", body, &env); err != nil {
		return nil, err
	}
	return &env.Theme, nil
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
	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return fmt.Errorf("join url: %w", err)
	}
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
