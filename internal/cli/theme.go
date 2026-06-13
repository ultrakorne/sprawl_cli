package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newThemeCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "Read or update the UI theme",
	}
	cmd.AddCommand(newThemeGetCmd(opts))
	cmd.AddCommand(newThemeSetCmd(opts))
	return cmd
}

func newThemeGetCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show the current theme id (GET /api/v1/settings/theme)",
		Args:  textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runThemeGet(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newThemeSetCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <id>",
		Short: "Set the active theme by id (PATCH /api/v1/settings/theme)",
		Long: "Set the active theme by id. Theme ids are lowercase kebab-case " +
			"(e.g. `tokyo-night`, `catppuccin-latte`, `gruvbox`). No client-side " +
			"normalization happens — an unknown or mis-cased id is rejected by " +
			"the server as 404 theme_not_found. Non-owner agent → 403 forbidden.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runThemeSet(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runThemeGet(ctx context.Context, stdout, stderr io.Writer, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	id, err := c.GetTheme(ctx)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderThemePayload(stdout, id, id, opts)
}

func runThemeSet(ctx context.Context, stdout, stderr io.Writer, id string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	applied, err := c.SetTheme(ctx, id)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderThemePayload(stdout, applied, sty.render(sty.ok, fmt.Sprintf("Theme set to %s", applied)), opts)
}

// renderThemePayload mirrors the server envelope on the wire: flat
// `{"theme": "<id>"}`. The text fallback is passed in by the caller so `get`
// and `set` can give distinct human-friendly lines while json/toon output
// stays identical.
func renderThemePayload(out io.Writer, id, textFallback string, opts *runtimeOpts) error {
	payload := map[string]any{"theme": id}
	return renderPayload(out, payload, textFallback, opts)
}
