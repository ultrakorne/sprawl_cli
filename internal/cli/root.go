package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/updater"
)

// runtimeOpts holds invocation-wide flag state bound by the root command.
// A fresh instance is created per NewRootCmd call so tests and any in-process
// embedder can run commands concurrently without sharing mutable state.
type runtimeOpts struct {
	format      string // --format / $SPRAWL_OUTPUT, resolved by resolveFormat
	agentSecret string // --agent-secret / -s, fallback $SPRAWL_AGENT_SECRET
}

func NewRootCmd() *cobra.Command {
	opts := &runtimeOpts{}

	root := &cobra.Command{
		Use:   build.AppName,
		Short: "CLI for the sprawl API",
		Long: "sprawl — HTTP client for the sprawl API.\n\n" +
			"Output: --format=text|json|toon (default toon; override session-wide with $SPRAWL_OUTPUT).",
		SilenceUsage: true,
		// PersistentPreRunE runs the daily version check on the prod binary
		// (no-op everywhere else). Errors are swallowed inside MaybeNotify
		// so a flaky network never blocks a real command. We skip the
		// `update` subcommand to avoid printing "update available" right
		// before running the update itself.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Name() == "update" {
				return nil
			}
			_ = updater.MaybeNotify(cmd.Context(), cmd.ErrOrStderr())
			return nil
		},
	}

	root.PersistentFlags().StringVar(&opts.format, "format", "",
		"output format: text|json|toon (default: toon, or $SPRAWL_OUTPUT)")
	root.PersistentFlags().StringVarP(&opts.agentSecret, "agent-secret", "s", "",
		"agent secret value (overrides $SPRAWL_AGENT_SECRET)")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newLoginCmd())
	root.AddCommand(newWhoamiCmd(opts))
	root.AddCommand(newActivityCmd(opts))
	root.AddCommand(newThemeCmd(opts))
	root.AddCommand(newTaskCmd(opts))
	root.AddCommand(newChecklistCmd(opts))
	root.AddCommand(newNoteCmd(opts))
	root.AddCommand(newSkillCmd())
	root.AddCommand(newUpdateCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and the baked-in API URL",
		Args:  textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"%s %s\n  api:    %s\n  date:   %s\n",
				build.AppName, build.Version, build.APIURL, build.Date,
			)
			return err
		},
	}
}

// textArgs wraps a cobra.PositionalArgs validator so arg-count failures print
// a plain-text error + usage line to stderr. Commands set SilenceErrors=true
// so reportErr (from RunE) doesn't double-print; that flag also swallows
// validator errors by default, which leaves the user with a silent exit 1.
// This wrapper restores the usage message. Usage errors stay plain text even
// when --format=json|toon — the format pipeline is for API responses, not
// for telling someone they typed the command wrong.
func textArgs(check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := check(cmd, args); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\nUsage: %s\n", err, cmd.UseLine())
			return err
		}
		return nil
	}
}
