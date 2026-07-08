package tui

import (
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func strptr(s string) *string { return &s }

func TestTaskMarkdown_FullTemplate(t *testing.T) {
	task := &client.Task{
		ID:                1,
		Title:             "Ship it",
		Status:            "todo",
		DueDate:           "2026-07-10",
		Project:           &client.Project{ID: 3, Name: "Core"},
		ChecklistProgress: client.ChecklistProgress{Done: 1, Total: 2},
		Description:       "line one\nline two",
		ChecklistItems: []*client.ChecklistItem{
			{ID: 10, Title: "done thing", Completed: true, Notes: strptr("a note\nsecond line")},
			{ID: 11, Title: "todo thing", Completed: false},
		},
	}
	got := taskMarkdown(task)
	want := "# Task #1 — Ship it\n" +
		"status: todo  due: 2026-07-10  project: Core  progress: 1/2\n" +
		"\n" +
		"line one\nline two\n" +
		"\n" +
		"## Checklist\n" +
		"- [x] #10 done thing\n" +
		"      a note\n" +
		"      second line\n" +
		"- [ ] #11 todo thing\n"
	if got != want {
		t.Fatalf("taskMarkdown mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

func TestTaskMarkdown_EmptyFieldsUseDash(t *testing.T) {
	task := &client.Task{ID: 5, Title: "bare"}
	got := taskMarkdown(task)
	if !strings.Contains(got, "status: —  due: —  project: —  progress: 0/0") {
		t.Fatalf("missing dash placeholders:\n%s", got)
	}
	if strings.Contains(got, "## Checklist") {
		t.Fatalf("no-item task should omit the checklist section:\n%s", got)
	}
}

func TestItemMarkdown_WithAndWithoutNote(t *testing.T) {
	task := &client.Task{ID: 2, Title: "Parent"}
	withNote := &client.ChecklistItem{ID: 20, Title: "has note", Completed: true, Notes: strptr("hello")}
	got := itemMarkdown(withNote, task)
	want := "## Item #20 — has note  [x]\n" +
		"task: #2 Parent\n" +
		"note:\nhello\n"
	if got != want {
		t.Fatalf("itemMarkdown mismatch:\n got: %q\nwant: %q", got, want)
	}

	noNote := &client.ChecklistItem{ID: 21, Title: "no note", Completed: false}
	got2 := itemMarkdown(noNote, task)
	if !strings.Contains(got2, "## Item #21 — no note  [ ]") {
		t.Fatalf("missing header/checkbox:\n%s", got2)
	}
	if !strings.Contains(got2, "note:\n(none)\n") {
		t.Fatalf("empty note should render (none):\n%s", got2)
	}
}
