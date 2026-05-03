package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/skill"
)

func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage the sprawl skill and sprawl-bookkeeper agent install",
	}
	cmd.AddCommand(newSkillInstallCmd())
	cmd.AddCommand(newSkillUninstallCmd())
	return cmd
}

func newSkillInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Interactively install the sprawl skill and/or sprawl-bookkeeper agent",
		Long: "Walks you through picking what to install (skill, agent, or both), " +
			"for which AI tools, and where (your " +
			"home directory or the current folder). Source is the repo's master " +
			"branch on GitHub. Each install is recorded in config.toml so " +
			"`sprawl update` can refresh them when a new version ships.",
		Args:         textArgs(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return printAndReturn(cmd.ErrOrStderr(), fmt.Errorf("getwd: %w", err))
			}
			if err := skill.Install(cmd.Context(), cwd, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
				return printAndReturn(cmd.ErrOrStderr(), err)
			}
			return nil
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newSkillUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove every recorded sprawl skill / agent install",
		Long: "Lists every skill / agent install recorded in config.toml, asks for " +
			"confirmation, then deletes each path and clears its config row. " +
			"There is no per-target selection — the command always clears every " +
			"recorded install at once.",
		Args:         textArgs(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := skill.Uninstall(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
				return printAndReturn(cmd.ErrOrStderr(), err)
			}
			return nil
		},
	}
	cmd.SilenceErrors = true
	return cmd
}
