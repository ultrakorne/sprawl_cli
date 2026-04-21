package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newThemeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "Read or update the UI theme",
	}
	cmd.AddCommand(newThemeGetCmd())
	cmd.AddCommand(newThemeSetCmd())
	return cmd
}

func newThemeGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show the current theme (GET /api/v1/settings/theme)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runThemeGet(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newThemeSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set the active theme by name, case-insensitive (PATCH /api/v1/settings/theme)",
		Long: "Set the active theme. Name match is case-insensitive (e.g. \"Tokyo Night\", \"tokyo night\", \"TOKYO NIGHT\"). " +
			"Unknown name → 404 theme_not_found. Non-owner agent → 403 forbidden.",
		// We validate args inside RunE so that reportErr renders the error
		// in the resolved format (text/json/toon), matching every other
		// error path. cobra's default Args check would be silenced by
		// SilenceErrors below.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("theme set requires exactly one argument: the theme name"))
			}
			return runThemeSet(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0])
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runThemeGet(ctx context.Context, stdout, stderr io.Writer) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	theme, err := c.GetTheme(ctx)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderThemePayload(stdout, theme, fmt.Sprintf("%s (%s)", theme.Name, theme.ID))
}

func runThemeSet(ctx context.Context, stdout, stderr io.Writer, name string) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	theme, err := c.SetTheme(ctx, name)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderThemePayload(stdout, theme, fmt.Sprintf("Theme set to %s (%s)", theme.Name, theme.ID))
}

func renderThemePayload(out io.Writer, theme *client.Theme, textFallback string) error {
	payload := map[string]any{
		"theme": map[string]any{
			"id":   theme.ID,
			"name": theme.Name,
		},
	}
	return renderPayload(out, payload, textFallback)
}
