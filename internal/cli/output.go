package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alpkeskin/gotoon"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
	FormatTOON Format = "toon"
)

// resolveFormat picks the output format in this order:
// --format flag → SPRAWL_OUTPUT env → toon.
func resolveFormat(opts *runtimeOpts) (Format, error) {
	v := strings.ToLower(strings.TrimSpace(opts.format))
	if v == "" {
		v = strings.ToLower(strings.TrimSpace(os.Getenv("SPRAWL_OUTPUT")))
	}
	if v == "" {
		return FormatTOON, nil
	}
	switch Format(v) {
	case FormatText, FormatJSON, FormatTOON:
		return Format(v), nil
	default:
		return "", fmt.Errorf("invalid format %q (want: text|json|toon)", v)
	}
}

// renderPayload writes structured success data in the resolved format. For
// `text`, the caller supplies a pre-formatted human line via textFallback.
func renderPayload(out io.Writer, payload map[string]any, textFallback string, opts *runtimeOpts) error {
	f, err := resolveFormat(opts)
	if err != nil {
		return err
	}
	switch f {
	case FormatText:
		_, err := fmt.Fprintln(out, textFallback)
		return err
	case FormatJSON:
		return json.NewEncoder(out).Encode(payload)
	case FormatTOON:
		s, err := gotoon.Encode(payload)
		if err != nil {
			return fmt.Errorf("encode toon: %w", err)
		}
		_, err = fmt.Fprintln(out, s)
		return err
	}
	return nil
}

// parseErrorsDetails extracts the shared changeset fallback body:
// `{"errors": {...}}`. Returns the raw errors value (usually a field→messages
// map) so JSON / TOON can render it directly. Returns false when the body
// isn't a JSON object or doesn't carry an `errors` field.
func parseErrorsDetails(body string) (any, bool) {
	var parsed struct {
		Errors any `json:"errors"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil || parsed.Errors == nil {
		return nil, false
	}
	return parsed.Errors, true
}

// isNotFoundAPIError reports whether err is a *client.APIError with HTTP
// 404 and the server's "not_found" code. Used by delete commands to make
// the CLI idempotent: a second DELETE on a missing resource is success.
// Other 404s (e.g. "theme_not_found") still surface as errors.
func isNotFoundAPIError(err error) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == 404 && apiErr.Code == "not_found"
}

// reportErr renders err in the resolved format. Structured errors go to
// stdout (agents parse stdout); human text goes to stderr. Returns the
// original error so cobra's RunE exits non-zero.
func reportErr(stdout, stderr io.Writer, err error, opts *runtimeOpts) error {
	f, ferr := resolveFormat(opts)
	if ferr != nil {
		// Invalid --format value itself — surface that to stderr plainly and
		// return the caller's original error.
		fmt.Fprintf(stderr, "error: %v\n", ferr)
		return err
	}
	if f == FormatText {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return err
	}

	payload := map[string]any{"status": "error"}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		payload["http_status"] = apiErr.Status
		switch {
		case apiErr.Code != "":
			payload["error"] = apiErr.Code
		default:
			// Shared changeset fallback shape: {"errors": {...}}. When present,
			// tag the error as "invalid" and surface the structured field
			// errors so agents don't have to re-parse a JSON-in-string blob.
			if details, ok := parseErrorsDetails(apiErr.Body); ok {
				payload["error"] = "invalid"
				payload["details"] = details
			} else {
				payload["error"] = apiErr.Body
			}
		}
	} else {
		payload["error"] = err.Error()
	}

	switch f {
	case FormatJSON:
		_ = json.NewEncoder(stdout).Encode(payload)
	case FormatTOON:
		if s, encErr := gotoon.Encode(payload); encErr == nil {
			fmt.Fprintln(stdout, s)
		} else {
			// Fall back to stderr plain if TOON encoding itself fails.
			fmt.Fprintf(stderr, "error: %v\n", err)
		}
	}
	return err
}
