package skill

import (
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderPlain returns the View body with ANSI escape codes stripped, so
// substring asserts don't break when lipgloss colours interleave with the
// content.
func renderPlain(m tea.Model) string {
	v := m.View()
	return ansi.Strip(v.Content)
}

// drive feeds messages into a bubbletea Model one at a time and returns
// the final state. We test models directly rather than spawning a Program
// so tests stay deterministic and don't need a TTY.
func drive(t *testing.T, m tea.Model, msgs ...tea.Msg) tea.Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	return m
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	}
	// Single printable character.
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestMultiSelect_DefaultsToAllSelected(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}, {label: "B", value: "b"}}, nil)
	final := drive(t, m, key("enter")).(multiSelectModel)
	got := final.values()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("values = %v, want [a b]", got)
	}
}

func TestMultiSelect_UsesProvidedDefaults(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}, {label: "B", value: "b"}, {label: "C", value: "c"}}, []bool{true, false, true})
	final := drive(t, m, key("enter")).(multiSelectModel)
	got := final.values()
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("values = %v, want [a c]", got)
	}
}

func TestMultiSelect_SpaceTogglesAtCursor(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}, {label: "B", value: "b"}}, nil)
	// Toggle off A, leave B on, confirm.
	final := drive(t, m, key("space"), key("enter")).(multiSelectModel)
	got := final.values()
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("values = %v, want [b]", got)
	}
}

func TestMultiSelect_ArrowKeysMoveCursor(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}, {label: "B", value: "b"}, {label: "C", value: "c"}}, nil)
	final := drive(t, m, key("down"), key("space"), key("enter")).(multiSelectModel)
	// All on by default; toggling B off leaves [a, c].
	got := final.values()
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("values = %v, want [a c]", got)
	}
}

func TestMultiSelect_VimKeysMoveCursor(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}, {label: "B", value: "b"}}, nil)
	// j down → cursor on B; toggle B off; k up; toggle A off; enter must
	// refuse since selection is now empty.
	final := drive(t, m, key("j"), key("space"), key("k"), key("space"), key("enter")).(multiSelectModel)
	if len(final.values()) != 0 {
		t.Fatalf("values = %v, expected empty after toggling both off", final.values())
	}
	if final.cancel {
		t.Fatal("empty enter should not flip cancel")
	}
}

func TestMultiSelect_AKeyTogglesAll(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}, {label: "B", value: "b"}}, nil)
	// All on by default → 'a' clears all → 'a' again restores all → enter.
	final := drive(t, m, key("a"), key("a"), key("enter")).(multiSelectModel)
	if len(final.values()) != 2 {
		t.Fatalf("values = %v, want both", final.values())
	}
}

func TestMultiSelect_EscCancels(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}}, nil)
	final := drive(t, m, key("esc")).(multiSelectModel)
	if !final.cancel {
		t.Fatal("esc should set cancel")
	}
}

func TestMultiSelect_CtrlCCancels(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}}, nil)
	final := drive(t, m, key("ctrl+c")).(multiSelectModel)
	if !final.cancel {
		t.Fatal("ctrl+c should set cancel")
	}
}

func TestMultiSelect_CursorClampsAtEnds(t *testing.T) {
	m := newMultiSelect("pick", []option{{label: "A", value: "a"}, {label: "B", value: "b"}}, nil)
	final := drive(t, m, key("up"), key("up"), key("up")).(multiSelectModel)
	if final.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (clamped)", final.cursor)
	}
	final = drive(t, m, key("down"), key("down"), key("down"), key("down")).(multiSelectModel)
	if final.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (clamped at last index)", final.cursor)
	}
}

func TestMultiSelect_View_RendersCheckboxesAndCursor(t *testing.T) {
	m := newMultiSelect("Pick something", []option{{label: "Alpha", value: "a"}, {label: "Beta", value: "b"}}, nil)
	body := renderPlain(m)
	if !strings.Contains(body, "Pick something") {
		t.Errorf("title missing: %q", body)
	}
	if !strings.Contains(body, "[x] Alpha") {
		t.Errorf("Alpha row should be checked: %q", body)
	}
	if !strings.Contains(body, "› [x] Alpha") {
		t.Errorf("cursor should be on Alpha: %q", body)
	}
	if !strings.Contains(body, "esc: cancel") {
		t.Errorf("hints missing: %q", body)
	}
}

func TestMultiSelect_View_HintsWhenNothingSelected(t *testing.T) {
	m := newMultiSelect("Pick tools", []option{{label: "Claude", value: "claude"}, {label: "Codex", value: "codex"}}, []bool{false, false})
	body := renderPlain(m)
	if !strings.Contains(body, "no tools selected") {
		t.Fatalf("empty-selection hint missing: %q", body)
	}
}

func TestMultiSelect_View_EmitsAnsiWhenStyled(t *testing.T) {
	// Pin the contract that we *do* emit ANSI styling — so a future change
	// that strips colours doesn't go unnoticed.
	m := newMultiSelect("hello", []option{{label: "A", value: "a"}}, nil)
	if !strings.Contains(m.View().Content, "\x1b[") {
		t.Fatal("View should contain ANSI escape codes for colour")
	}
}

func TestSingleSelect_EnterPicksFirst(t *testing.T) {
	m := newSingleSelect("scope", []option{{label: "Global", value: "global"}, {label: "Local", value: "local"}})
	final := drive(t, m, key("enter")).(singleSelectModel)
	if final.value() != "global" {
		t.Fatalf("value = %q, want global", final.value())
	}
}

func TestSingleSelect_DownThenEnter(t *testing.T) {
	m := newSingleSelect("scope", []option{{label: "Global", value: "global"}, {label: "Local", value: "local"}})
	final := drive(t, m, key("down"), key("enter")).(singleSelectModel)
	if final.value() != "local" {
		t.Fatalf("value = %q, want local", final.value())
	}
}

func TestSingleSelect_EscCancels(t *testing.T) {
	m := newSingleSelect("scope", []option{{label: "Global", value: "global"}})
	final := drive(t, m, key("esc")).(singleSelectModel)
	if !final.cancel {
		t.Fatal("esc should cancel")
	}
}

func TestSingleSelect_View_HasCursorAndHints(t *testing.T) {
	m := newSingleSelect("Scope?", []option{{label: "Global", value: "g"}, {label: "Local — /tmp", value: "l"}})
	body := renderPlain(m)
	if !strings.Contains(body, "Scope?") {
		t.Errorf("title missing: %q", body)
	}
	if !strings.Contains(body, "› Global") {
		t.Errorf("cursor on first row: %q", body)
	}
	if !strings.Contains(body, "Local — /tmp") {
		t.Errorf("local label with cwd missing: %q", body)
	}
}

func TestInstalledToolDefaults(t *testing.T) {
	prev := lookPathFunc
	lookPathFunc = func(name string) (string, error) {
		if name == "claude" || name == "codex" {
			return "/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPathFunc = prev })

	got := installedToolDefaults([]option{
		{value: "claude", autodetectBy: "claude"},
		{value: "opencode", autodetectBy: "opencode"},
		{value: "codex", autodetectBy: "codex"},
	})
	want := []bool{true, false, true}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("defaults = %v, want %v", got, want)
		}
	}
}
