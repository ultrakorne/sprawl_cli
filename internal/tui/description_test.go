package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// A task with a description is flagged on its list row; whitespace alone isn't
// a description.
func TestListRows_FlagTaskWithDescription(t *testing.T) {
	m := newListModel(&fakeClient{}, []*client.Task{
		{ID: 1, Title: "described", Description: "some context"},
		{ID: 2, Title: "blank", Description: "  \n "},
		{ID: 3, Title: "bare"},
	})
	m.width, m.height = 100, 20

	rows := m.listRows(m.visibleTasks(), m.bodyHeight(hintsFor(screenList)))
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	for i, want := range []bool{true, false, false} {
		if got := strings.Contains(ansi.Strip(rows[i]), "🗒"); got != want {
			t.Errorf("row %d flag = %v, want %v: %q", i, got, want, ansi.Strip(rows[i]))
		}
	}
}

// The checklist screen shows the description above the items.
func TestViewChecklist_ShowsDescription(t *testing.T) {
	_, m, detail := checklistFixture()
	detail.Description = "why this task exists"
	m.width, m.height = 80, 24

	out := ansi.Strip(m.viewChecklist())
	desc := strings.Index(out, "why this task exists")
	item := strings.Index(out, "#10")
	if desc < 0 {
		t.Fatalf("description missing from the checklist screen:\n%s", out)
	}
	if item < 0 || desc > item {
		t.Fatalf("description should sit above the items:\n%s", out)
	}
}

// No description, no block: the items start right under the header.
func TestDescriptionBlock_EmptyWhenNoDescription(t *testing.T) {
	_, m, _ := checklistFixture()
	for _, d := range []string{"", "   \n\t"} {
		if got := m.descriptionBlock(d, 20); got != nil {
			t.Errorf("description %q should render nothing, got %q", d, got)
		}
	}
}

// A long description is cut to the preview cap, says how much is hidden, and
// leaves the checklist its room.
func TestDescriptionBlock_CapsLongDescription(t *testing.T) {
	_, m, detail := checklistFixture()
	var lines []string
	for i := range 10 {
		lines = append(lines, "line "+itoa(int64(i)))
	}
	detail.Description = strings.Join(lines, "\n")
	m.width, m.height = 80, 24

	block := m.descriptionBlock(detail.Description, m.bodyHeight(hintsFor(screenChecklist)))
	if len(block) != descPreviewMax+1 { // preview + blank separator
		t.Fatalf("want %d lines, got %d: %q", descPreviewMax+1, len(block), block)
	}
	last := ansi.Strip(block[descPreviewMax-1])
	if !strings.Contains(last, "+8 more lines") || !strings.Contains(last, "E to open") {
		t.Fatalf("a cut preview should say what's hidden and how to see it, got %q", last)
	}
	out := ansi.Strip(m.viewChecklist())
	if !strings.Contains(out, "#10") || !strings.Contains(out, "#11") {
		t.Fatalf("items must still show under a long description:\n%s", out)
	}
}

// On a short window the description always leaves the checklist at least half
// the body: the checklist is what the screen is for.
func TestDescriptionBlock_YieldsToChecklistOnShortWindow(t *testing.T) {
	_, m, _ := checklistFixture()
	long := strings.Repeat("a line\n", 10)
	for bodyH := 1; bodyH <= 12; bodyH++ {
		block := m.descriptionBlock(long, bodyH)
		if left := bodyH - len(block); left < max(1, bodyH/2) {
			t.Errorf("bodyH %d: description took %d lines, leaving the checklist %d", bodyH, len(block), left)
		}
	}
	// One line of room shows description text, not just the hint.
	for _, bodyH := range []int{3, 4} {
		block := m.descriptionBlock(long, bodyH)
		if first := ansi.Strip(block[0]); !strings.Contains(first, "a line") {
			t.Errorf("bodyH %d: first line should be description text, got %q", bodyH, first)
		}
	}
	if got := m.descriptionBlock(long, 2); got != nil {
		t.Errorf("no room for a description on a 2-line body, got %q", got)
	}
}

// E targets the open task on the checklist, even when the list highlight has
// moved elsewhere, and the highlighted row on the list.
func TestDescriptionTask_FollowsScreen(t *testing.T) {
	_, m, detail := checklistFixture()
	other := &client.Task{ID: 2, Title: "other"}
	m.tasks = append(m.tasks, other)
	m.listSel = 1

	if got := m.descriptionTask(); got != detail {
		t.Fatalf("checklist: E should edit the open task #%d, got %+v", detail.ID, got)
	}
	m.pop()
	if got := m.descriptionTask(); got == nil || got.ID != other.ID {
		t.Fatalf("list: E should edit the highlighted task #%d, got %+v", other.ID, got)
	}
}
