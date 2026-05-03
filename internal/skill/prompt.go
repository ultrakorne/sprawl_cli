package skill

import (
	"errors"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Palette for the install prompts. ANSI256 codes so colours degrade
// reasonably on 8-colour terminals; lipgloss handles the SGR codes.
var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")) // violet
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))           // pink
	checkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))            // green
	rowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))           // pink (cursored row)
	hintStyle   = lipgloss.NewStyle().Faint(true)
)

// errPromptCancelled means the user hit ctrl+c / esc before confirming.
// install.go translates it into a friendly "Cancelled." message.
var errPromptCancelled = errors.New("prompt cancelled")

// option pairs a human-readable label with the value emitted on selection.
type option struct {
	label string
	value string
}

// promptChoiceFunc and promptConfirmFunc are seams so install_test can
// substitute deterministic choices instead of driving a bubbletea program
// over a fake TTY. Production code routes through the real Run* helpers.
var (
	promptChoiceFunc  = runPromptChoice
	promptConfirmFunc = runPromptConfirm
)

// runPromptChoice walks the user through what / tools / scope as three
// successive bubbletea programs. Cancelling any one aborts the whole
// flow with errPromptCancelled.
func runPromptChoice(in io.Reader, out io.Writer, cwd string) (Choice, error) {
	what, err := runMultiSelect(in, out, "What to install?", []option{
		{label: "sprawl skill", value: "skill"},
		{label: "sprawl-bookkeeper agent", value: "agent"},
	})
	if err != nil {
		return Choice{}, err
	}
	tools, err := runMultiSelect(in, out, "For which AI tools?", []option{
		{label: "Claude Code", value: "claude"},
		{label: "OpenCode", value: "opencode"},
	})
	if err != nil {
		return Choice{}, err
	}
	scope, err := runSingleSelect(in, out, "Scope?", []option{
		{label: "Global (your home directory)", value: "global"},
		{label: "Local — " + cwd, value: "local"},
	})
	if err != nil {
		return Choice{}, err
	}
	return Choice{What: what, Tools: tools, Scope: scope}, nil
}

// runPromptConfirm reuses the single-select model for a clean Yes/No
// picker. Returning ("yes" → true) keeps the call sites readable.
func runPromptConfirm(in io.Reader, out io.Writer, title string) (bool, error) {
	v, err := runSingleSelect(in, out, title, []option{
		{label: "Yes, proceed", value: "yes"},
		{label: "No, abort", value: "no"},
	})
	if err != nil {
		return false, err
	}
	return v == "yes", nil
}

func runMultiSelect(in io.Reader, out io.Writer, title string, items []option) ([]string, error) {
	p := tea.NewProgram(newMultiSelect(title, items),
		tea.WithInput(in), tea.WithOutput(out))
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	m := final.(multiSelectModel)
	if m.cancel {
		return nil, errPromptCancelled
	}
	return m.values(), nil
}

func runSingleSelect(in io.Reader, out io.Writer, title string, items []option) (string, error) {
	p := tea.NewProgram(newSingleSelect(title, items),
		tea.WithInput(in), tea.WithOutput(out))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	m := final.(singleSelectModel)
	if m.cancel {
		return "", errPromptCancelled
	}
	return m.value(), nil
}

// multiSelectModel is a checklist with cursor + per-row toggle + an "all"
// shortcut. All items default to selected so an enter-only run picks
// everything (matching the prior comma-separated UX where blank meant all).
type multiSelectModel struct {
	title    string
	items    []option
	cursor   int
	selected []bool
	cancel   bool
}

func newMultiSelect(title string, items []option) multiSelectModel {
	sel := make([]bool, len(items))
	for i := range sel {
		sel[i] = true
	}
	return multiSelectModel{title: title, items: items, selected: sel}
}

func (m multiSelectModel) Init() tea.Cmd { return nil }

func (m multiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "ctrl+c", "esc":
		m.cancel = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "space", " ":
		m.selected[m.cursor] = !m.selected[m.cursor]
	case "a":
		// Toggle the whole list — flip to "all on" unless already all on,
		// in which case clear it. Lets the user start over without
		// hunting cells.
		allOn := true
		for _, v := range m.selected {
			if !v {
				allOn = false
				break
			}
		}
		for i := range m.selected {
			m.selected[i] = !allOn
		}
	case "enter":
		// Refuse to confirm an empty selection — multi-select with zero
		// picks is almost certainly a mis-press; let the user toggle or
		// hit esc to cancel.
		for _, v := range m.selected {
			if v {
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m multiSelectModel) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title) + "\n\n")
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("› ")
		}
		check := "[ ]"
		if m.selected[i] {
			check = checkStyle.Render("[x]")
		}
		label := item.label
		if i == m.cursor {
			label = rowStyle.Render(label)
		}
		fmt.Fprintf(&b, "%s%s %s\n", cursor, check, label)
	}
	b.WriteString("\n" + hintStyle.Render("space: toggle • a: all/none • enter: confirm • esc: cancel") + "\n")
	return tea.NewView(b.String())
}

func (m multiSelectModel) values() []string {
	var out []string
	for i, v := range m.selected {
		if v {
			out = append(out, m.items[i].value)
		}
	}
	return out
}

// singleSelectModel is a one-of-N picker — used for scope and for the
// Yes/No confirmation. Cursor starts at the first item, which is
// conventionally the safe default ("global", "yes").
type singleSelectModel struct {
	title  string
	items  []option
	cursor int
	cancel bool
}

func newSingleSelect(title string, items []option) singleSelectModel {
	return singleSelectModel{title: title, items: items}
}

func (m singleSelectModel) Init() tea.Cmd { return nil }

func (m singleSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "ctrl+c", "esc":
		m.cancel = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter":
		return m, tea.Quit
	}
	return m, nil
}

func (m singleSelectModel) View() tea.View {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title) + "\n\n")
	for i, item := range m.items {
		cursor := "  "
		label := item.label
		if i == m.cursor {
			cursor = cursorStyle.Render("› ")
			label = rowStyle.Render(label)
		}
		b.WriteString(cursor + label + "\n")
	}
	b.WriteString("\n" + hintStyle.Render("enter: confirm • esc: cancel") + "\n")
	return tea.NewView(b.String())
}

func (m singleSelectModel) value() string {
	return m.items[m.cursor].value
}
