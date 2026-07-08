package tui

import (
	"slices"
	"strings"
	"testing"
)

func TestResolveEditorFields(t *testing.T) {
	cases := []struct {
		name   string
		visual string
		editor string
		want   []string
	}{
		{"terminal editor untouched", "", "nvim", []string{"nvim"}},
		{"gui code gets --wait", "", "code", []string{"code", "--wait"}},
		{"code --wait not doubled", "", "code --wait", []string{"code", "--wait"}},
		{"absolute path base detected", "", "/usr/bin/code", []string{"/usr/bin/code", "--wait"}},
		{"visual wins over editor", "code", "nvim", []string{"code", "--wait"}},
		{"sublime wait", "", "subl", []string{"subl", "--wait"}},
		{"textmate uses -w", "", "mate", []string{"mate", "-w"}},
		{"empty falls back to vi", "", "", []string{"vi"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("VISUAL", c.visual)
			t.Setenv("EDITOR", c.editor)
			got := resolveEditorFields()
			if !slices.Equal(got, c.want) {
				t.Fatalf("resolveEditorFields() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestEditNoteSaveFlow covers the full $EDITOR note round-trip the review flagged
// as untested: an editorDoneMsg carrying an edited buffer must PUT the note and
// reflect it in the note view. (The reported bug was environmental — a non-
// blocking $EDITOR handing back unedited content — but this locks the in-app
// path so a regression in the wiring is caught.)
func TestEditNoteSaveFlow(t *testing.T) {
	fc, m, detail := checklistFixture()
	m.push(screenNote)
	m.itemSel = 0 // item #10
	fc.notesResp = strptr("hello from editor")

	_, cmd := m.Update(editorDoneMsg{kind: editItemNote, id: 10, body: "hello from editor\n"})
	if cmd == nil {
		t.Fatal("editorDoneMsg produced no save command")
	}
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if !slices.Contains(fc.calls, "SetNotes:10") {
		t.Fatalf("expected SetNotes:10 to be called, calls=%v", fc.calls)
	}
	if fc.notesArg != "hello from editor" {
		t.Fatalf("note body sent = %q, want %q (trailing newline should be trimmed)", fc.notesArg, "hello from editor")
	}
	got := detail.ChecklistItems[0].Notes
	if got == nil || *got != "hello from editor" {
		t.Fatalf("item.Notes = %v, want \"hello from editor\"", got)
	}
	if !strings.Contains(m.viewNote(), "hello from editor") {
		t.Fatalf("note view does not show the saved note:\n%s", m.viewNote())
	}
}
