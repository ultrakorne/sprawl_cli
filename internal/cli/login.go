package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
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

	settingsURL := client.BaseURL() + "/auth-settings"
	fmt.Fprintln(out, "Before you approve this device, copy your owner agent secret from:")
	fmt.Fprintf(out, "  %s\n", settingsURL)
	fmt.Fprintln(out, "You'll export it as SPRAWL_AGENT_SECRET after login.")
	fmt.Fprintln(out)

	grant, err := c.CreateDeviceGrant(ctx, deviceTokenName())
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

// deviceTokenName builds the display label sent with POST /api/auth/device.
// Format: "<system> · <hostname>" (e.g. "linux · home-laptop"). Falls back to
// just the system tag if the hostname is unavailable. Returns "" if even the
// system tag is empty — empty string tells the server to use its default.
func deviceTokenName() string {
	system := sanitizeNamePart(runtime.GOOS)
	host, _ := os.Hostname()
	host = sanitizeNamePart(host)

	var name string
	switch {
	case system != "" && host != "":
		name = system + " · " + host
	case system != "":
		name = system
	case host != "":
		name = host
	default:
		return ""
	}

	name = strings.TrimSpace(name)
	return truncateRunes(name, 64)
}

// sanitizeNamePart strips control characters and trims whitespace from a
// single component of the device-token name.
func sanitizeNamePart(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// truncateRunes returns s truncated to at most n runes — rune-safe so we
// never split a multibyte codepoint.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

func onApproved(out io.Writer, token string) error {
	cfg, err := config.Load(build.AppName)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Token = token
	if err := config.Save(build.AppName, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	path, _ := config.Path(build.AppName)
	fmt.Fprintf(out, "\nLogged in. Token saved to %s (mode 0600).\n", path)
	fmt.Fprintln(out, "\nNext: export your agent secret in this shell so authed requests work:")
	fmt.Fprintln(out, "  export SPRAWL_AGENT_SECRET=<your owner key secret>")
	fmt.Fprintf(out, "\nIf you don't have it yet, retrieve it from %s/auth-settings. The agent secret is never stored on disk by sprawl.\n", client.BaseURL())
	fmt.Fprintln(out, "\nTip: install the sprawl skill into your AI tool with:")
	fmt.Fprintln(out, "  gh skill install ultrakorne/sprawl_cli sprawl")
	return nil
}
