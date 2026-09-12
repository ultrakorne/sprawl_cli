// Package tui implements sprawl's interactive terminal UI, built on
// bubbletea v2. It is launched by `sprawl` (no args, on a TTY) and by the
// explicit `sprawl tui` subcommand. Colors come exclusively from the terminal's
// ANSI palette (indices 0-15) so the UI adopts the user's terminal theme, the
// same philosophy as the CLI's text output.
//
// The agent secret and project key, when prompted for, are held in memory only
// — neither is written to disk or logged (AGENTS.md invariants #2/#3).
package tui

import (
	"context"
	"errors"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

// Deps carries the resolved dependencies the TUI needs. Credentials are resolved
// by the caller (internal/cli) so the TUI reuses the CLI's credential logic
// rather than duplicating it.
type Deps struct {
	// LoggedIn is false when no bearer token is available; the TUI then shows the
	// not-logged-in screen instead of fetching anything.
	LoggedIn bool
	// Secret is the agent secret resolved from --agent-secret / $SPRAWL_AGENT_SECRET.
	// Empty means "prompt for it" (SecretProvided false) — unless ProjectKey is
	// set, which is a narrowing factor on its own.
	Secret         string
	SecretProvided bool
	// ProjectKey is the project key resolved from --project-key /
	// $SPRAWL_PROJECT_KEY. When set, the session is confined to that project
	// (pre-filtered lists, creates landing there) and the agent secret becomes
	// optional, so the credentials prompt is skipped. Shown in the header so
	// it's obvious which project the UI is looking at.
	ProjectKey string
	// Workspace is the workspace id resolved from --workspace / $SPRAWL_WORKSPACE,
	// empty for the factors' default. It is a selector, not a factor: it never
	// stands in for a secret or a key. The `w` key on the list re-selects it
	// for the rest of the session (in memory only — nothing is persisted).
	Workspace string
	// NewClient builds an authed client for a candidate secret, project key and
	// workspace. Only the bearer token is captured by the closure; the factors
	// and the selector are supplied per call so the credentials prompt can
	// retry with either factor, and the workspace picker can re-pin the
	// session, without leaking anything anywhere.
	NewClient func(secret, projectKey, workspace string) Client
}

// ErrNotATTY is returned when the TUI is asked to run without a terminal.
var ErrNotATTY = errors.New("interactive mode requires a terminal")

// IsTTY reports whether both stdin and stdout are terminals — the condition for
// launching the interactive UI.
func IsTTY() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

// Run builds the model from deps and runs the bubbletea program to completion.
// A clean interrupt (ctrl+c / kill) is treated as a normal exit.
func Run(ctx context.Context, deps Deps) error {
	if !IsTTY() {
		return ErrNotATTY
	}
	p := tea.NewProgram(newModel(ctx, deps), tea.WithContext(ctx))
	_, err := p.Run()
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}
