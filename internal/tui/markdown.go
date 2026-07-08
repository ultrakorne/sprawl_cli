package tui

import (
	"fmt"
	"strings"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// The Markdown formatters below are pure and unit-tested. They produce the
// clipboard payloads copied via tea.SetClipboard (OSC 52), templated exactly as
// the interactive-tui spec's "Copy" section prescribes.

// mdCheckbox is the Markdown task-list checkbox for a completed flag.
func mdCheckbox(done bool) string {
	if done {
		return "[x]"
	}
	return "[ ]"
}

// mdDash returns s trimmed, or an em dash when blank — the spec's "|—"
// placeholder for empty status/due/project fields.
func mdDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func mdProject(p *client.Project) string {
	if p == nil || strings.TrimSpace(p.Name) == "" {
		return "—"
	}
	return p.Name
}

// taskMarkdown formats a whole task as Markdown for the clipboard. It expects a
// full task (ChecklistItems populated, each with its Notes) as fetched by
// GetTask(full=true); the checklist section is omitted when there are no items.
//
//	# Sprawl Task #<id> — <title>
//	status: <status>  due: <due|—>  project: <name|—>  progress: <done>/<total>
//
//	<description, if present>
//
//	## Checklist
//	- [x] #<itemid> <title>
//	      <note lines, indented> (if any)
//	- [ ] #<itemid> <title>
func taskMarkdown(t *client.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Sprawl Task #%d — %s\n", t.ID, t.Title)
	fmt.Fprintf(&b, "status: %s  due: %s  project: %s  progress: %d/%d\n",
		mdDash(t.Status), mdDash(t.DueDate), mdProject(t.Project),
		t.ChecklistProgress.Done, t.ChecklistProgress.Total)

	if strings.TrimSpace(t.Description) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(t.Description, "\n"))
		b.WriteString("\n")
	}

	if len(t.ChecklistItems) > 0 {
		b.WriteString("\n## Checklist\n")
		for _, it := range t.ChecklistItems {
			writeItemMarkdown(&b, it)
		}
	}
	return b.String()
}

// writeItemMarkdown writes one checklist item as a Markdown task-list line
// (`- [x] #<id> <title>`) followed by its note indented underneath. Shared by
// the whole-task and single-item copy formats so both stay identical.
func writeItemMarkdown(b *strings.Builder, it *client.ChecklistItem) {
	fmt.Fprintf(b, "- %s #%d %s\n", mdCheckbox(it.Completed), it.ID, it.Title)
	if it.Notes != nil && strings.TrimSpace(*it.Notes) != "" {
		for _, ln := range strings.Split(strings.TrimRight(*it.Notes, "\n"), "\n") {
			fmt.Fprintf(b, "      %s\n", ln)
		}
	}
}

// itemMarkdown formats a single checklist item as Markdown for the clipboard,
// led by a one-line parent-task context header so a model has the surrounding
// task. task carries the parent's id/title (may be nil defensively).
//
//	sprawl task: #<taskid> <tasktitle>
//	- [ ] #<itemid> <title>
//	      <note lines, indented> (if any)
func itemMarkdown(it *client.ChecklistItem, task *client.Task) string {
	var b strings.Builder
	var taskID int64
	var taskTitle string
	if task != nil {
		taskID = task.ID
		taskTitle = task.Title
	}
	fmt.Fprintf(&b, "sprawl task: #%d %s\n", taskID, taskTitle)
	writeItemMarkdown(&b, it)
	return b.String()
}
