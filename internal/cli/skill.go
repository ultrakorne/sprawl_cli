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
	return cmd
}

func newSkillInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Interactively install the sprawl skill and/or sprawl-bookkeeper agent",
		Long: "Walks you through picking what to install (skill, agent, or both), " +
			"for which AI tools (Claude Code, OpenCode, or both), and where (your " +
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
