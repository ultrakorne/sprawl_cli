package tui

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// errNotBrowsable guards the one place this package hands a string to the
// platform's URL handler. Every URL we open is built by client.PRURL from a
// server-validated `https://github.com/<owner>/<repo>`, so this can only trip
// if that contract breaks — but the opener is the wrong place to find out.
var errNotBrowsable = errors.New("refusing to open a non-https URL")

// urlOpenedMsg / urlOpenFailedMsg report the outcome of an open attempt. A
// failure is not fatal and not even unusual: over SSH there is no browser to
// hand the URL to, which is why the reducer falls back to the clipboard.
type urlOpenedMsg struct{ url string }

type urlOpenFailedMsg struct {
	url string
	err error
}

// openURLCmd hands a URL to the platform's default handler, detached: the
// process is started and reaped in the background rather than waited on, so a
// browser that takes two seconds to appear doesn't freeze the TUI. Unlike the
// $EDITOR path this deliberately does NOT use tea.ExecProcess — there is
// nothing to suspend for, the browser owns its own window.
func openURLCmd(u string) tea.Cmd {
	return func() tea.Msg {
		if err := openURL(u); err != nil {
			return urlOpenFailedMsg{url: u, err: err}
		}
		return urlOpenedMsg{url: u}
	}
}

func openURL(u string) error {
	if !strings.HasPrefix(u, "https://") {
		return errNotBrowsable
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	// Leave stdio detached: xdg-open's chatter on stderr would land in the middle
	// of the alt-screen and corrupt the frame.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap; nobody is waiting on the exit code
	return nil
}
