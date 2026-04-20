package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

var jsonOutput bool

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   build.AppName,
		Short: "CLI for the task_manager API",
		Long: "sprawl — JSON-over-HTTP client for the task_manager API.\n\n" +
			"The base URL is baked into the binary; the `sprawl_dev` binary targets a local server.\n" +
			"Auth: token in ~/.config/" + build.AppName + "/config.toml, agent secret in $SPRAWL_AGENT_SECRET.",
		SilenceUsage: true,
	}

	root.PersistentFlags().BoolVar(&jsonOutput, "json", false, "emit machine-readable JSON output")

	root.AddCommand(newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and the baked-in API URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"%s %s\n  api:    %s\n  commit: %s\n  date:   %s\n",
				build.AppName, build.Version, build.APIURL, build.Commit, build.Date,
			)
			return err
		},
	}
}
