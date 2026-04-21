package cli

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Call /api/v1/health to verify the full auth pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealth(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	// We render errors ourselves (plain/json/toon) so cobra doesn't double-print.
	cmd.SilenceErrors = true
	return cmd
}

func runHealth(ctx context.Context, stdout, stderr io.Writer) error {
	token, err := resolveToken()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	secret, err := resolveAgentSecret()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}

	c := client.NewAuthed(token, secret)
	if err := c.Health(ctx); err != nil {
		return reportErr(stdout, stderr, err)
	}

	return renderPayload(stdout, map[string]any{"status": "ok"}, "200 ok")
}
