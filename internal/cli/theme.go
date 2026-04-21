package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
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
		Short: "Show the current theme id (GET /api/v1/settings/theme)",
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
		Use:   "set <id>",
		Short: "Set the active theme by id (PATCH /api/v1/settings/theme)",
		Long: "Set the active theme by id. Theme ids are lowercase kebab-case " +
			"(e.g. `tokyo-night`, `catppuccin-latte`, `gruvbox`). No client-side " +
			"normalization happens — an unknown or mis-cased id is rejected by " +
			"the server as 404 theme_not_found. Non-owner agent → 403 forbidden.",
		// We validate args inside RunE so that reportErr renders the error
		// in the resolved format (text/json/toon), matching every other
		// error path. cobra's default Args check would be silenced by
		// SilenceErrors below.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("theme set requires exactly one argument: the theme id"))
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
	id, err := c.GetTheme(ctx)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderThemePayload(stdout, id, id)
}

func runThemeSet(ctx context.Context, stdout, stderr io.Writer, id string) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	applied, err := c.SetTheme(ctx, id)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderThemePayload(stdout, applied, fmt.Sprintf("Theme set to %s", applied))
}

// renderThemePayload mirrors the server envelope on the wire: flat
// `{"theme": "<id>"}`. The text fallback is passed in by the caller so `get`
// and `set` can give distinct human-friendly lines while json/toon output
// stays identical.
func renderThemePayload(out io.Writer, id, textFallback string) error {
	payload := map[string]any{"theme": id}
	return renderPayload(out, payload, textFallback)
}
