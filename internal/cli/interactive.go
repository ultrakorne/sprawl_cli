package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/tui"
)

// newTUICmd is the explicit `sprawl tui` entry point. Unlike a bare `sprawl`
// (which falls back to help off a TTY), `sprawl tui` on a non-terminal is a
// clear error — the user asked for interactive mode explicitly.
func newTUICmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal UI",
		Long: "Launch the interactive terminal UI: browse tasks, drill into a task's " +
			"checklist, toggle items, view/edit notes, do full task/item CRUD, search, " +
			"and copy task/item context as Markdown for pasting into an LLM. Requires a " +
			"terminal (stdin and stdout must both be TTYs).",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return launchTUI(cmd.Context(), cmd.ErrOrStderr(), opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

// launchTUI resolves credentials (reusing the CLI's resolvers) and starts the
// TUI. A missing token is not fatal here — the TUI renders a not-logged-in
// screen. Missing narrowing factors are also fine — the TUI prompts for an
// agent secret *or* a project key, whichever the user has; with either one
// already resolved it starts straight on the list. The token is never printed.
func launchTUI(ctx context.Context, stderr io.Writer, opts *runtimeOpts) error {
	token, tokenErr := resolveToken()
	loggedIn := tokenErr == nil && token != ""

	// Ignore a missing-secret error: the TUI prompts for it. A present secret is
	// validated by the TUI's first list fetch.
	secret, _ := resolveAgentSecret(opts)
	projectKey := resolveProjectKey(opts)
	// Already validated by the root PersistentPreRunE; a bare `sprawl` runs
	// through the same hook, so this can't fail here.
	workspace, err := resolveWorkspace(opts)
	if err != nil {
		return err
	}

	deps := tui.Deps{
		LoggedIn:       loggedIn,
		Secret:         secret,
		SecretProvided: secret != "",
		ProjectKey:     projectKey,
		Workspace:      workspace,
		NewClient: func(secret, key, workspace string) tui.Client {
			return client.NewAuthed(token, secret,
				client.WithProjectKey(key), client.WithWorkspace(workspace))
		},
	}

	err = tui.Run(ctx, deps)
	if errors.Is(err, tui.ErrNotATTY) {
		fmt.Fprintln(stderr, "error: interactive mode requires a terminal")
	}
	return err
}
