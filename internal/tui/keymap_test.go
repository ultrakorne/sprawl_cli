package tui

import "testing"

func TestDispatch_GlobalBindings(t *testing.T) {
	for _, scr := range []screen{screenList, screenChecklist, screenNote} {
		if got := dispatch(scr, "ctrl+c"); got != actQuitHard {
			t.Errorf("scr %d ctrl+c = %v, want actQuitHard", scr, got)
		}
		if got := dispatch(scr, "?"); got != actHelp {
			t.Errorf("scr %d ? = %v, want actHelp", scr, got)
		}
		if got := dispatch(scr, "r"); got != actRefresh {
			t.Errorf("scr %d r = %v, want actRefresh", scr, got)
		}
	}
}

func TestDispatch_ListScreen(t *testing.T) {
	cases := map[string]action{
		"up": actUp, "k": actUp, "down": actDown, "j": actDown,
		"g": actTop, "G": actBottom, "enter": actOpen, "/": actSearch,
		"q": actQuit, "esc": actBack, "c": actCopy, "n": actNewTask,
		"e": actEditTitle, "E": actEditDesc, "t": actSetDue, "d": actDelete,
		"z": actNone,
	}
	for key, want := range cases {
		if got := dispatch(screenList, key); got != want {
			t.Errorf("list %q = %v, want %v", key, got, want)
		}
	}
}

func TestDispatch_ChecklistScreen(t *testing.T) {
	cases := map[string]action{
		"space": actToggle, "x": actToggle, "enter": actOpen,
		"a": actAddItem, "e": actEditTitle, "d": actDelete,
		"c": actCopy, "g": actTop, "G": actBottom, "esc": actBack,
		// list-only keys must not leak onto the checklist
		"n": actNone, "E": actNone, "t": actNone, "/": actNone,
	}
	for key, want := range cases {
		if got := dispatch(screenChecklist, key); got != want {
			t.Errorf("checklist %q = %v, want %v", key, got, want)
		}
	}
}

func TestDispatch_NoteScreen(t *testing.T) {
	cases := map[string]action{
		"e": actEditNote, "c": actCopy, "esc": actBack,
		"up": actUp, "down": actDown, "g": actTop, "G": actBottom,
		// note screen edits with lowercase e only (matches the help overlay)
		"E": actNone, "space": actNone, "a": actNone,
	}
	for key, want := range cases {
		if got := dispatch(screenNote, key); got != want {
			t.Errorf("note %q = %v, want %v", key, got, want)
		}
	}
}
