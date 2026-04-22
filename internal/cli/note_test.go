package cli

import (
	"strings"
	"testing"
)

func TestResolveNoteBody_FromPositional(t *testing.T) {
	got, err := resolveNoteBody(strings.NewReader("ignored"), []string{"hi"}, false)
	if err != nil {
		t.Fatalf("resolveNoteBody: %v", err)
	}
	if got != "hi" {
		t.Fatalf("got = %q", got)
	}
}

func TestResolveNoteBody_FromStdin(t *testing.T) {
	got, err := resolveNoteBody(strings.NewReader("piped notes\nwith newline"), nil, true)
	if err != nil {
		t.Fatalf("resolveNoteBody: %v", err)
	}
	if got != "piped notes\nwith newline" {
		t.Fatalf("got = %q", got)
	}
}

func TestResolveNoteBody_StdinAndPositionalIsError(t *testing.T) {
	_, err := resolveNoteBody(strings.NewReader("x"), []string{"y"}, true)
	if err == nil {
		t.Fatal("expected error when both --stdin and positional arg are given")
	}
}

func TestResolveNoteBody_MissingInputIsError(t *testing.T) {
	_, err := resolveNoteBody(strings.NewReader(""), nil, false)
	if err == nil {
		t.Fatal("expected error when no notes source is given")
	}
}

func TestResolveNoteBody_MultiplePositionalsIsError(t *testing.T) {
	_, err := resolveNoteBody(strings.NewReader(""), []string{"a", "b"}, false)
	if err == nil {
		t.Fatal("expected error when extra positionals are given")
	}
}

func TestResolveNoteBody_EmptyPositionalClearsNotes(t *testing.T) {
	// Explicit empty string is a valid payload (clears notes) — not the same
	// as "no argument at all".
	got, err := resolveNoteBody(strings.NewReader("ignored"), []string{""}, false)
	if err != nil {
		t.Fatalf("resolveNoteBody: %v", err)
	}
	if got != "" {
		t.Fatalf("got = %q, want empty", got)
	}
}
