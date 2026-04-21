package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

var (
	formatFlag      string
	agentSecretFlag string
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   build.AppName,
		Short: "CLI for the sprawl API",
		Long: "sprawl — HTTP client for the sprawl API.\n\n" +
			"Output: --format=text|json|toon (default toon; override session-wide with $SPRAWL_OUTPUT).",
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&formatFlag, "format", "",
		"output format: text|json|toon (default: toon, or $SPRAWL_OUTPUT)")
	root.PersistentFlags().StringVarP(&agentSecretFlag, "agent-secret", "s", "",
		"agent secret value (overrides $SPRAWL_AGENT_SECRET)")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newLoginCmd())
	root.AddCommand(newHealthCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and the baked-in API URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"%s %s\n  api:    %s\n  date:   %s\n",
				build.AppName, build.Version, build.APIURL, build.Date,
			)
			return err
		},
	}
}
