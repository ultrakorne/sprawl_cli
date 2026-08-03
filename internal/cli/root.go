package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/tui"
	"github.com/ultrakorne/sprawl_cli/internal/updater"
)

// runtimeOpts holds invocation-wide flag state bound by the root command.
// A fresh instance is created per NewRootCmd call so tests and any in-process
// embedder can run commands concurrently without sharing mutable state.
type runtimeOpts struct {
	format      string // --format / $SPRAWL_OUTPUT, resolved by resolveFormat
	human       bool   // -h / --human: shorthand for --format=text
	agentSecret string // --agent-secret / -s, fallback $SPRAWL_AGENT_SECRET
	projectKey  string // --project-key / -p, fallback $SPRAWL_PROJECT_KEY
}

func NewRootCmd() *cobra.Command {
	opts := &runtimeOpts{}

	root := &cobra.Command{
		Use:   build.AppName,
		Short: "CLI for the sprawl API",
		Long: "sprawl — HTTP client for the sprawl API.\n\n" +
			"Output: --format=text|json|toon (default toon; override session-wide with $SPRAWL_OUTPUT).\n\n" +
			"Scope: every call needs the bearer from `login` plus at least one narrowing factor —\n" +
			"a project key (--project-key / $SPRAWL_PROJECT_KEY) to work inside a single project,\n" +
			"an agent secret (--agent-secret / $SPRAWL_AGENT_SECRET) to act as an agent key, or both.",
		SilenceUsage: true,
		// PersistentPreRunE runs the daily version check on the prod binary
		// (no-op everywhere else). Errors are swallowed inside MaybeNotify
		// so a flaky network never blocks a real command. We skip the
		// `update` subcommand to avoid printing "update available" right
		// before running the update itself.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Styling lights up only when human (text) output is headed to a
			// real terminal; everything else (json/toon, pipes, files) stays
			// plain. Decided once here, before any command renders.
			if f, err := resolveFormat(opts); err == nil && f == FormatText {
				enableStylingFor(cmd.OutOrStdout())
			}
			if cmd.Name() == "update" {
				return nil
			}
			_ = updater.MaybeNotify(cmd.Context(), cmd.ErrOrStderr())
			return nil
		},
		// Bare `sprawl` launches the interactive TUI when stdin+stdout are a
		// terminal; otherwise it prints help (today's behavior for pipes /
		// redirects / CI). `--help` still short-circuits to help before RunE
		// runs (cobra reads the help flag first), and unknown subcommands still
		// error before reaching here.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && tui.IsTTY() {
				return launchTUI(cmd.Context(), cmd.ErrOrStderr(), opts)
			}
			return cmd.Help()
		},
	}

	root.PersistentFlags().StringVar(&opts.format, "format", "",
		"output format: text|json|toon (default: toon, or $SPRAWL_OUTPUT)")
	root.PersistentFlags().BoolVarP(&opts.human, "human", "h", false,
		"shorthand for --format=text: human-readable, color-styled output")
	root.PersistentFlags().StringVarP(&opts.agentSecret, "agent-secret", "s", "",
		"agent secret value (overrides $SPRAWL_AGENT_SECRET)")
	root.PersistentFlags().StringVarP(&opts.projectKey, "project-key", "p", "",
		"confine this request to one project by its key (overrides $SPRAWL_PROJECT_KEY)")

	// Reclaim -h for --human. cobra normally auto-registers --help with a -h
	// shorthand; defining our own --help flag (long form only) makes cobra skip
	// that, freeing -h while keeping --help working everywhere. Persistent so
	// every subcommand inherits it and likewise skips adding its own -h.
	root.PersistentFlags().Bool("help", false, "show help")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newLoginCmd())
	root.AddCommand(newWhoamiCmd(opts))
	root.AddCommand(newActivityCmd(opts))
	root.AddCommand(newThemeCmd(opts))
	root.AddCommand(newTaskCmd(opts))
	root.AddCommand(newChecklistCmd(opts))
	root.AddCommand(newQueueCmd(opts))
	root.AddCommand(newNoteCmd(opts))
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newTUICmd(opts))
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and the API URL this binary talks to",
		Args:  textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The EFFECTIVE url, not the baked-in one: $SPRAWL_API_URL points a
			// dev binary at whichever backend a branch / worktree is serving, and
			// "which server am I actually hitting?" is the question this line
			// exists to answer. The override is named when it's in play.
			api := client.BaseURL()
			if api != build.APIURL {
				api += fmt.Sprintf("  (SPRAWL_API_URL override; built with %s)", build.APIURL)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"%s %s\n  api:    %s\n  date:   %s\n",
				build.AppName, build.Version, api, build.Date,
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
