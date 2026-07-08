package tui

import (
	"strings"
	"testing"
)

func TestTextInput_TypingAndEditing(t *testing.T) {
	var in textInput
	for _, k := range []string{"h", "e", "l", "l", "o"} {
		if !in.handleKey(k) {
			t.Fatalf("expected key %q consumed", k)
		}
	}
	if got := in.String(); got != "hello" {
		t.Fatalf("value = %q, want hello", got)
	}
	// space inserts a literal space
	in.handleKey("space")
	in.handleKey("w")
	if got := in.String(); got != "hello w" {
		t.Fatalf("value = %q, want %q", got, "hello w")
	}
	// backspace removes the last rune
	in.handleKey("backspace")
	in.handleKey("backspace")
	if got := in.String(); got != "hello" {
		t.Fatalf("after backspace value = %q, want hello", got)
	}
}

func TestTextInput_CursorMovementAndInsert(t *testing.T) {
	var in textInput
	in.setValue("abc")
	in.handleKey("home")
	in.handleKey("X") // insert at start
	if got := in.String(); got != "Xabc" {
		t.Fatalf("value = %q, want Xabc", got)
	}
	in.handleKey("end")
	in.handleKey("Y")
	if got := in.String(); got != "XabcY" {
		t.Fatalf("value = %q, want XabcY", got)
	}
	in.handleKey("left")
	in.handleKey("Z") // insert before Y
	if got := in.String(); got != "XabcZY" {
		t.Fatalf("value = %q, want XabcZY", got)
	}
}

func TestTextInput_IgnoresNamedKeys(t *testing.T) {
	var in textInput
	in.setValue("abc")
	for _, k := range []string{"enter", "esc", "tab", "up", "down"} {
		if in.handleKey(k) {
			t.Fatalf("named key %q should not be consumed as text", k)
		}
	}
	if got := in.String(); got != "abc" {
		t.Fatalf("value changed to %q by named keys", got)
	}
}

func TestTextInput_MaskedRender(t *testing.T) {
	var in textInput
	in.setValue("secret")
	masked := in.render(true)
	// masked render must not contain the raw secret
	if strings.Contains(masked, "secret") {
		t.Fatalf("masked render leaked the value: %q", masked)
	}
	if !strings.Contains(masked, "••••••") {
		t.Fatalf("masked render should show bullets, got %q", masked)
	}
}
