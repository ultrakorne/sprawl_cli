package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
)

type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// resolveFormat picks the output format in this order:
// --format flag → -h/--human → SPRAWL_OUTPUT env → json.
// An explicit --format wins over -h so `--format=json -h` stays json.
func resolveFormat(opts *runtimeOpts) (Format, error) {
	v := strings.ToLower(strings.TrimSpace(opts.format))
	if v == "" && opts.human {
		v = string(FormatText)
	}
	if v == "" {
		v = strings.ToLower(strings.TrimSpace(os.Getenv("SPRAWL_OUTPUT")))
	}
	if v == "" {
		return FormatJSON, nil
	}
	switch Format(v) {
	case FormatText, FormatJSON:
		return Format(v), nil
	default:
		return "", fmt.Errorf("invalid format %q (want: text|json)", v)
	}
}

// writeHuman emits text-format (human / -h) output through a colorprofile
// writer bound to the destination. On a TTY this keeps the lipgloss styling;
// on a pipe, a file, a test buffer, or under $NO_COLOR it strips every escape
// sequence so the output is plain text — identical to what it was before
// styling existed. This is the single choke point that guarantees styling
// never reaches non-human (json) output or a non-terminal.
func writeHuman(w io.Writer, s string) error {
	_, err := io.WriteString(colorprofile.NewWriter(w, os.Environ()), s)
	return err
}

// renderPayload writes structured success data in the resolved format. For
// `text`, the caller supplies a pre-formatted human line via textFallback.
func renderPayload(out io.Writer, payload map[string]any, textFallback string, opts *runtimeOpts) error {
	f, err := resolveFormat(opts)
	if err != nil {
		return err
	}
	if f == FormatText {
		return writeHuman(out, textFallback+"\n")
	}
	// Not a switch with a bare fall-through: an unhandled format would render
	// nothing and still exit 0. json is the only other format, and the only
	// other thing resolveFormat can return.
	return json.NewEncoder(out).Encode(payload)
}

// parseErrorsDetails extracts the shared changeset fallback body:
// `{"errors": {...}}`. Returns the raw errors value (usually a field→messages
// map) so JSON can render it directly. Returns false when the body
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

// apiGuidance is the human-facing reading of a documented server error code.
// `headline` replaces the raw "http 403: invalid_project_key" as the error
// line in text output; `remedy` is the follow-up telling the user what to do
// about it. json keeps the machine-readable code and gets both joined
// into an additive `hint` key.
type apiGuidance struct{ headline, remedy string }

func (g apiGuidance) hint() string { return g.headline + " — " + g.remedy }

// explainAPIError maps the server's auth / scope error codes to guidance.
// These are the failures a user can actually fix from the shell: a wrong key,
// a missing factor, a scope boundary. Everything else (404s, changeset
// failures) already says what it means and is left alone.
func explainAPIError(err error, opts *runtimeOpts) (apiGuidance, bool) {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return apiGuidance{}, false
	}
	key := resolveProjectKey(opts)
	switch apiErr.Code {
	case "unauthenticated":
		return apiGuidance{
			headline: "bearer token rejected",
			remedy:   fmt.Sprintf("it's missing, expired, or revoked — run `%s login` to pair this device again", build.AppName),
		}, true
	case "second_factor_required":
		return apiGuidance{
			headline: "no project key or agent secret reached the server",
			remedy: "export SPRAWL_PROJECT_KEY (or pass --project-key) to work inside one project, " +
				"or export SPRAWL_AGENT_SECRET (or pass --agent-secret) to act as an agent key",
		}, true
	case "invalid_project_key":
		headline := "the project key sent doesn't match any of your projects"
		if key != "" {
			headline = fmt.Sprintf("project key %q doesn't match any of your projects", key)
		}
		return apiGuidance{
			headline: headline,
			remedy:   "check the Project key field on the project's side panel in /tasks (matched case-insensitively, whitespace trimmed)",
		}, true
	case "invalid_agent_secret":
		return apiGuidance{
			headline: "agent secret rejected",
			remedy:   "the secret is wrong, revoked, or issued by another user or another server; re-copy the 8-character value from /auth-settings",
		}, true
	case "forbidden":
		if key != "" {
			return apiGuidance{
				headline: "not allowed on this resource",
				remedy: fmt.Sprintf("project key %q confines this session to one project — the target is outside it, "+
					"or this agent has no access there (check `%s whoami`)", key, build.AppName),
			}, true
		}
		return apiGuidance{
			headline: "not allowed on this resource",
			remedy:   fmt.Sprintf("this agent key lacks permission here — check `%s whoami` for its scope", build.AppName),
		}, true
	}
	// A 401 with no code (or an unrecognised one) is still the bearer.
	if apiErr.Status == 401 {
		return apiGuidance{
			headline: "bearer token rejected",
			remedy:   fmt.Sprintf("run `%s login` to pair this device again", build.AppName),
		}, true
	}
	return apiGuidance{}, false
}

// printAndReturn prints err to stderr and returns it, so SilenceErrors=true
// commands still tell the user what failed before cobra triggers exit 1.
// Used by the text-only commands (skill install, update) that don't go
// through the format-aware reportErr pipeline.
func printAndReturn(stderr io.Writer, err error) error {
	fmt.Fprintf(stderr, "error: %v\n", err)
	return err
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
	guidance, explained := explainAPIError(err, opts)

	if f == FormatText {
		// Known auth / scope failures speak plainly ("project key "x" doesn't
		// match…") instead of echoing "http 403: invalid_project_key", with the
		// fix on an indented second line. Everything else is unchanged.
		msg := err.Error()
		if explained {
			msg = guidance.headline
		}
		out := fmt.Sprintf("%s %v\n", sty.errTag.Render("error:"), msg)
		if explained {
			out += sty.render(sty.faint, "  "+guidance.remedy) + "\n"
		}
		_ = writeHuman(stderr, out)
		return err
	}

	payload := map[string]any{"status": "error"}
	if explained {
		// Additive: `status` / `error` / `http_status` keep their shape, so
		// anything parsing the envelope today is unaffected.
		payload["hint"] = guidance.hint()
	}
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

	_ = json.NewEncoder(stdout).Encode(payload)
	return err
}
