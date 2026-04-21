package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate via device flow and save the token",
		Long: "Runs the RFC 8628 device flow: prints a URL to visit, polls for approval, " +
			"and writes the resulting token to the config file (mode 0600). " +
			"The agent secret is a separate value and must be set in SPRAWL_AGENT_SECRET before running authed commands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func runLogin(ctx context.Context, out io.Writer) error {
	c := client.New()

	grant, err := c.CreateDeviceGrant(ctx)
	if err != nil {
		return fmt.Errorf("create device grant: %w", err)
	}

	// RFC 8628 defaults in case the server sends zeros.
	interval := grant.Interval
	if interval <= 0 {
		interval = 5
	}
	expires := grant.ExpiresIn
	if expires <= 0 {
		expires = 600
	}

	fmt.Fprintln(out, "To authorise this device:")
	fmt.Fprintf(out, "  1. Open %s\n", grant.VerificationURIComplete)
	fmt.Fprintf(out, "     (or visit %s and enter code %s)\n", grant.VerificationURI, grant.UserCode)
	fmt.Fprintln(out, "  2. Log in to sprawl and approve.")
	fmt.Fprintf(out, "\nWaiting for approval (expires in %ds, polling every %ds)…\n", expires, interval)

	pollCtx, cancel := context.WithTimeout(ctx, time.Duration(expires)*time.Second)
	defer cancel()

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			if errors.Is(pollCtx.Err(), context.DeadlineExceeded) {
				return errors.New("login timed out before approval — run login again")
			}
			return pollCtx.Err() // context cancelled (ctrl+C)
		case <-ticker.C:
			token, err := c.PollDeviceToken(pollCtx, grant.DeviceCode)
			if err == nil {
				return onApproved(out, token)
			}
			var dpe *client.DevicePollError
			if errors.As(err, &dpe) {
				switch dpe.Code {
				case "authorization_pending":
					continue // keep polling
				case "access_denied":
					return errors.New("login denied in the browser")
				case "expired_token":
					return errors.New("device code expired before approval — run login again")
				case "invalid_grant":
					return errors.New("device code rejected by server (invalid_grant)")
				default:
					return fmt.Errorf("device grant error: %s", dpe.Code)
				}
			}
			return fmt.Errorf("poll device token: %w", err)
		}
	}
}

func onApproved(out io.Writer, token string) error {
	if err := config.Save(build.AppName, &config.Config{Token: token}); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	path, _ := config.Path(build.AppName)
	fmt.Fprintf(out, "\nLogged in. Token saved to %s (mode 0600).\n", path)
	fmt.Fprintln(out, "\nNext: export your agent secret in this shell so authed requests work:")
	fmt.Fprintln(out, "  export SPRAWL_AGENT_SECRET=<your owner key secret>")
	fmt.Fprintln(out, "\nThe agent secret is never stored on disk by sprawl. Get it from the settings page (phase 6) or the server's ensure_owner_key logs.")
	return nil
}
