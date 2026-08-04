package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// TestResolveFormat_HumanFlag locks in `-h` (--human) as the text alias and the
// precedence rule: an explicit --format always beats -h.
func TestResolveFormat_HumanFlag(t *testing.T) {
	if f, err := resolveFormat(&runtimeOpts{human: true}); err != nil || f != FormatText {
		t.Fatalf("-h should resolve to text, got %q (err %v)", f, err)
	}
	if f, _ := resolveFormat(&runtimeOpts{format: "json", human: true}); f != FormatJSON {
		t.Fatalf("explicit --format=json must beat -h, got %q", f)
	}
}

// TestStyling_PreservesPlainLayout is the load-bearing test for the whole
// feature: enabling styling must not change a single visible character once the
// ANSI is stripped (as the colorprofile.Writer does on any non-terminal). This
// guards the table alignment (colored cells are measured from their plain text
// so they can't shear columns) and the hand-spaced note indentation.
func TestStyling_PreservesPlainLayout(t *testing.T) {
	defer func() { stylesEnabled = false }()

	tasks := []*client.Task{
		{ID: 1, Status: "done", Title: "short",
			ChecklistProgress: client.ChecklistProgress{Done: 1, Total: 1}},
		{ID: 200, Status: "in_progress", Title: "a longer title",
			ChecklistProgress: client.ChecklistProgress{Done: 0, Total: 2}},
	}

	doneNotes := "did it"
	// A task read carries its items. Structural characters (padding, the header
	// rule, the note indentation) are emitted unconditionally and only color and
	// the OSC 8 link are gated, so stripping ANSI must reproduce the plain
	// rendering exactly.
	shown := &client.Task{ID: 77, Status: "in_progress", Title: "Agentic",
		Description:       "a description\nover two lines",
		ChecklistProgress: client.ChecklistProgress{Done: 1, Total: 2},
		Project:           &client.Project{ID: 3, Name: "Sprawl", GithubURL: "https://github.com/ultrakorne/sprawl"},
		CreatedBy:         &client.Actor{Type: "user", ID: 1},
		ChecklistItems: []*client.ChecklistItem{
			{ID: 5, Title: "done step", Completed: true, HasNotes: true, Notes: &doneNotes,
				State: client.StateInReview, PRNumber: 412},
			{ID: 6, Title: "todo step", Completed: false},
		}}

	queue := []*client.ItemDetail{{
		ChecklistItem: client.ChecklistItem{ID: 7, Title: "add the migration",
			State: client.StateInReview, PRNumber: 412, HasNotes: true, Notes: &doneNotes},
		Task: client.ItemTask{ID: 3, Title: "Ship the API",
			Project: &client.Project{ID: 1, Name: "Sprawl", GithubURL: "https://github.com/ultrakorne/sprawl"}},
	}}

	for _, tc := range []struct {
		name  string
		build func() string
	}{
		{"task list", func() string { return taskListText(tasks) }},
		{"task show", func() string { return taskShowText(shown, false) }},
		{"task show --full", func() string { return taskShowText(shown, true) }},
		{"queue", func() string { return queueText(queue, client.StateInReview, false) }},
		{"queue --full", func() string { return queueText(queue, client.StateInReview, true) }},
	} {
		stylesEnabled = false
		plain := tc.build()

		stylesEnabled = true
		styled := tc.build()

		if !strings.Contains(styled, "\x1b[") {
			t.Errorf("%s: expected ANSI when styling on, got none:\n%q", tc.name, styled)
		}
		if got := stripANSI(styled); got != plain {
			t.Errorf("%s: stripped styled output differs from plain:\nplain:  %q\nstyled: %q", tc.name, plain, got)
		}
	}
}

// stripANSI runs s through the same colorprofile writer output.go uses, on a
// non-terminal buffer, which removes every escape sequence.
func stripANSI(s string) string {
	var buf bytes.Buffer
	_, _ = colorprofile.NewWriter(&buf, []string{}).WriteString(s)
	return buf.String()
}
