package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/skill"
	"github.com/ultrakorne/sprawl_cli/internal/updater"
)

func newUpdateCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download and install the latest sprawl release and update installed skills",
		Long: "Updates the sprawl binary against the latest GitHub release (verified via " +
			"checksums.txt, atomic replace) and refreshes any skill / agent installs " +
			"recorded in config.toml against master. Binary update refuses on the dev " +
			"binary and on local (non-release) builds; skill update runs regardless. " +
			"Skip the binary confirmation prompt with --yes.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			binErr := updater.RunUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), yes)
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Checking installed skills/agents…")
			skillErr := skill.Update(cmd.Context(), cmd.OutOrStdout())
			if err := errors.Join(binErr, skillErr); err != nil {
				return printAndReturn(cmd.ErrOrStderr(), err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.SilenceErrors = true
	return cmd
}
