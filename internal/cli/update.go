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
		Long: "Updates the sprawl binary against the latest GitHub release (verified via " +
			"checksums.txt, atomic replace). Refuses on the dev binary and on local " +
			"(non-release) builds. Pass --yes to skip the confirmation prompt.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := updater.RunUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), yes); err != nil {
				return printAndReturn(cmd.ErrOrStderr(), err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.SilenceErrors = true
	return cmd
}
