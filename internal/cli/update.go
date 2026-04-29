package cli

import (
	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/updater"
)

func newUpdateCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download and install the latest sprawl release",
		Long: "Fetches the latest GitHub release, verifies its SHA256 against checksums.txt, " +
			"and atomically replaces the running binary. Refuses on the dev binary " +
			"and on local (non-release) builds. Skip the confirmation prompt with --yes.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updater.RunUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.SilenceErrors = true
	return cmd
}
