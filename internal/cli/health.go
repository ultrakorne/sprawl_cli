package cli

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

func newHealthCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Call /api/v1/health to verify the full auth pipeline",
		Args:  textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealth(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
		},
	}
	// We render errors ourselves (plain/json/toon) so cobra doesn't double-print.
	cmd.SilenceErrors = true
	return cmd
}

func runHealth(ctx context.Context, stdout, stderr io.Writer, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	if err := c.Health(ctx); err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderPayload(stdout, map[string]any{"status": "ok"}, "200 ok", opts)
}
