package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// resolveToken returns the bearer token from SPRAWL_TOKEN or config.toml,
// in that order. Empty → "not logged in" error.
func resolveToken() (string, error) {
	if t := os.Getenv("SPRAWL_TOKEN"); t != "" {
		return t, nil
	}
	cfg, err := config.Load(build.AppName)
	if err != nil {
		return "", err
	}
	if cfg.Token == "" {
		return "", fmt.Errorf("not logged in, run `%s login`", build.AppName)
	}
	return cfg.Token, nil
}

// resolveAgentSecret returns the agent secret from --agent-secret or
// $SPRAWL_AGENT_SECRET, in that order. Empty → error.
func resolveAgentSecret(opts *runtimeOpts) (string, error) {
	if opts.agentSecret != "" {
		return opts.agentSecret, nil
	}
	if v := os.Getenv("SPRAWL_AGENT_SECRET"); v != "" {
		return v, nil
	}
	return "", errors.New("agent secret not set — export SPRAWL_AGENT_SECRET or pass --agent-secret")
}

// resolveProjectKey returns the project key from --project-key or
// $SPRAWL_PROJECT_KEY, in that order. Empty is a valid answer — it means "no
// project confinement" — so this returns no error. The value is trimmed
// because the server trims and case-folds it anyway; length / charset rules
// stay server-side (surfacing as 403 invalid_project_key) so the CLI never
// disagrees with the source of truth. Like the agent secret, it is never
// persisted by sprawl: per-repo confinement is the shell's job (direnv, an
// .envrc, a wrapper), not a config file.
func resolveProjectKey(opts *runtimeOpts) string {
	if k := strings.TrimSpace(opts.projectKey); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv("SPRAWL_PROJECT_KEY"))
}

// resolveWorkspace returns the workspace selector from --workspace or
// $SPRAWL_WORKSPACE, in that order. Empty is the normal answer — it means
// "the workspace these factors resolve to by default" — so it is not an
// error. A non-empty value has to be a positive integer id: the server
// answers anything else with a bare 404 that would read as "task not found",
// so the shape check happens here, before any request. Like the project key,
// the selection is never persisted by sprawl — a workspace is a resource on
// the wire (a path prefix), and which one a shell works in is the shell's job.
func resolveWorkspace(opts *runtimeOpts) (string, error) {
	v := strings.TrimSpace(opts.workspace)
	if v == "" {
		v = strings.TrimSpace(os.Getenv("SPRAWL_WORKSPACE"))
	}
	if v == "" {
		return "", nil
	}
	if !isPositiveInt(v) {
		return "", fmt.Errorf("invalid workspace %q: want a numeric id (run `%s workspace list` to find it)", v, build.AppName)
	}
	return v, nil
}

// isPositiveInt reports whether s is a base-10 integer > 0 with no sign,
// spaces or leading zeros — the only spelling the server's id cast accepts.
func isPositiveInt(s string) bool {
	if s == "" || s[0] == '0' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// errNoNarrowingFactor is the local stand-in for the server's 403
// second_factor_required. The server can't know which factor the user meant,
// so we fail before the HTTP call with both options spelled out.
var errNoNarrowingFactor = errors.New(
	"no project key or agent secret set — export SPRAWL_PROJECT_KEY (or pass --project-key) " +
		"to work inside one project, or export SPRAWL_AGENT_SECRET (or pass --agent-secret) " +
		"to act as an agent key")

// newAuthedClient resolves the bearer plus at least one narrowing factor and
// returns a client ready for /api/v1/* calls. The server accepts a project
// key, an agent secret, or both (intersected — a project key can only narrow,
// never widen); it rejects neither. Missing both fails pre-HTTP.
func newAuthedClient(opts *runtimeOpts) (*client.Client, error) {
	token, err := resolveToken()
	if err != nil {
		return nil, err
	}
	projectKey := resolveProjectKey(opts)
	secret, secretErr := resolveAgentSecret(opts)
	if projectKey == "" && secretErr != nil {
		return nil, errNoNarrowingFactor
	}
	// The workspace is a selector, not a factor: it never satisfies the
	// narrowing rule above, it only picks which workspace the factors work in.
	workspace, err := resolveWorkspace(opts)
	if err != nil {
		return nil, err
	}
	return client.NewAuthed(token, secret,
		client.WithProjectKey(projectKey), client.WithWorkspace(workspace)), nil
}

// newUserScopedClient builds a client for user-level (not project-level)
// endpoints — today just the theme write. The server rejects those outright
// when a project key is present, so this path demands an agent secret and
// deliberately omits the key: `sprawl theme set` keeps working from a repo
// that exports SPRAWL_PROJECT_KEY, as long as a secret is also configured.
// `what` names the setting so the missing-secret error explains itself.
func newUserScopedClient(opts *runtimeOpts, what string) (*client.Client, error) {
	token, err := resolveToken()
	if err != nil {
		return nil, err
	}
	secret, err := resolveAgentSecret(opts)
	if err != nil {
		return nil, fmt.Errorf("%s is a user-level setting, not project-level: %w", what, err)
	}
	return client.NewAuthed(token, secret), nil
}
