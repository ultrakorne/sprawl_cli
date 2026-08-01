package tui

import "unicode/utf8"

// textInput is a hand-rolled single-line input. It intentionally avoids the
// bubbles textinput dependency (the spec asks to hand-roll unless genuinely
// unworkable) — a one-line editor is a few dozen lines and keeps the dependency
// surface small. Keys are matched by their bubbletea String() form so the type
// is pure and unit-testable without a running Program.
type textInput struct {
	value  []rune
	cursor int // rune index in [0, len(value)]
}

func (t *textInput) reset() {
	t.value = t.value[:0]
	t.cursor = 0
}

func (t *textInput) setValue(s string) {
	t.value = []rune(s)
	t.cursor = len(t.value)
}

func (t *textInput) String() string { return string(t.value) }

func (t *textInput) empty() bool { return len(t.value) == 0 }

// insertRune places r at the cursor and advances it.
func (t *textInput) insertRune(r rune) {
	next := make([]rune, 0, len(t.value)+1)
	next = append(next, t.value[:t.cursor]...)
	next = append(next, r)
	next = append(next, t.value[t.cursor:]...)
	t.value = next
	t.cursor++
}

// insertString inserts s at the cursor, dropping control characters (newlines,
// tabs) so a bracketed-paste of a secret/title with a trailing newline can't
// corrupt this single-line field. Used by the paste handler.
func (t *textInput) insertString(s string) {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		t.insertRune(r)
	}
}

// handleKey applies a key (in bubbletea String() form) to the input. It returns
// true when the key was consumed as editing input. Navigation/submit keys the
// caller handles first (enter/esc/tab) fall through as not-consumed if passed.
func (t *textInput) handleKey(key string) bool {
	switch key {
	case "backspace", "ctrl+h":
		if t.cursor > 0 {
			t.value = append(t.value[:t.cursor-1], t.value[t.cursor:]...)
			t.cursor--
		}
		return true
	case "delete", "ctrl+d":
		if t.cursor < len(t.value) {
			t.value = append(t.value[:t.cursor], t.value[t.cursor+1:]...)
		}
		return true
	case "left", "ctrl+b":
		if t.cursor > 0 {
			t.cursor--
		}
		return true
	case "right", "ctrl+f":
		if t.cursor < len(t.value) {
			t.cursor++
		}
		return true
	case "home", "ctrl+a":
		t.cursor = 0
		return true
	case "end", "ctrl+e":
		t.cursor = len(t.value)
		return true
	case "ctrl+u": // kill to start of line
		t.value = append([]rune{}, t.value[t.cursor:]...)
		t.cursor = 0
		return true
	case "ctrl+k": // kill to end of line
		t.value = t.value[:t.cursor]
		return true
	case "space":
		t.insertRune(' ')
		return true
	}
	// Any single printable rune is inserted verbatim. Named keys (enter, tab,
	// esc, up, …) are multi-byte strings and fall through here as not-consumed.
	if r, size := utf8.DecodeRuneInString(key); size == len(key) && size > 0 && r >= 0x20 && r != utf8.RuneError {
		t.insertRune(r)
		return true
	}
	return false
}

// render draws the input value with a block cursor. Values are always echoed in
// the clear — including on the credentials prompt: a mistyped secret or project
// key is only fixable if you can see it, and neither ever leaves memory.
func (t *textInput) render() string {
	display := make([]rune, len(t.value))
	copy(display, t.value)
	// Cursor as a trailing block; when mid-string, underline the char by wrapping
	// with reverse-ish brackets is overkill — a simple caret suffices for a
	// one-line field.
	if t.cursor >= len(display) {
		return string(display) + "█"
	}
	return string(display[:t.cursor]) + "█" + string(display[t.cursor:])
}
