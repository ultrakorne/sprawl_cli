package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// guiEditorWaitFlag maps GUI editors — which fork and return immediately — to
// the flag that makes them block until the file is closed. Without it,
// ExecProcess reads the temp file back the instant the launcher process exits
// (before the user has typed anything), silently "saving" the unchanged seed
// and deleting the temp file. Keyed by the command's base name so an absolute
// path or a "code --wait" that already carries the flag both resolve correctly.
var guiEditorWaitFlag = map[string]string{
	"code":          "--wait",
	"code-insiders": "--wait",
	"codium":        "--wait",
	"vscodium":      "--wait",
	"cursor":        "--wait",
	"zed":           "--wait",
	"subl":          "--wait",
	"sublime_text":  "--wait",
	"atom":          "--wait",
	"mate":          "-w",
}

// resolveEditorFields returns the editor command and its args (without the file
// path). It honors $VISUAL over $EDITOR (the conventional precedence for
// full-screen editors) and falls back to vi. For known GUI editors it injects
// the blocking "wait" flag when absent so the editor blocks until the file is
// closed — otherwise the read-back races the user's edit (see the map above).
func resolveEditorFields() []string {
	ed := strings.TrimSpace(os.Getenv("VISUAL"))
	if ed == "" {
		ed = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if ed == "" {
		ed = "vi"
	}
	fields := strings.Fields(ed)
	if len(fields) == 0 {
		return []string{"vi"}
	}
	base := filepath.Base(fields[0])
	if wait, ok := guiEditorWaitFlag[base]; ok && !slices.Contains(fields, wait) {
		fields = append(fields, wait)
	}
	return fields
}

// editInEditorCmd suspends the TUI, opens $EDITOR on a temp file seeded with
// `initial`, and — once the editor exits — reads the buffer back as an
// editorDoneMsg. Multi-line fields (task description, item note) are edited this
// way per the spec, via tea.ExecProcess so the terminal is handed cleanly to the
// child process and restored on return.
func editInEditorCmd(kind editKind, id int64, initial string) tea.Cmd {
	f, err := os.CreateTemp("", "sprawl-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{kind: kind, id: id, err: err} }
	}
	path := f.Name()
	if _, err := f.WriteString(initial); err != nil {
		f.Close()
		os.Remove(path)
		return func() tea.Msg { return editorDoneMsg{kind: kind, id: id, err: err} }
	}
	// A Close error can surface a deferred flush failure (e.g. disk full); if the
	// seed didn't land, don't hand $EDITOR a truncated buffer — fail cleanly.
	if err := f.Close(); err != nil {
		os.Remove(path)
		return func() tea.Msg { return editorDoneMsg{kind: kind, id: id, err: err} }
	}

	fields := resolveEditorFields()
	args := make([]string, 0, len(fields))
	args = append(args, fields[1:]...)
	args = append(args, path)
	c := exec.Command(fields[0], args...) // #nosec G204 — editor is the user's own $EDITOR

	return tea.ExecProcess(c, func(runErr error) tea.Msg {
		defer os.Remove(path)
		if runErr != nil {
			return editorDoneMsg{kind: kind, id: id, err: runErr}
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return editorDoneMsg{kind: kind, id: id, err: readErr}
		}
		return editorDoneMsg{kind: kind, id: id, body: string(b)}
	})
}
