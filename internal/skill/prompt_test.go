package skill

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func reader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

func TestPromptMultiSelect_BlankPicksAll(t *testing.T) {
	in := reader("\n")
	var out bytes.Buffer
	got, err := promptMultiSelect(in, &out, "header", []option{
		{label: "A", value: "a"}, {label: "B", value: "b"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestPromptMultiSelect_CommaSeparated(t *testing.T) {
	in := reader("2,1\n")
	var out bytes.Buffer
	got, err := promptMultiSelect(in, &out, "header", []option{
		{label: "A", value: "a"}, {label: "B", value: "b"}, {label: "C", value: "c"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Returned in input order; duplicates collapsed.
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("got %v", got)
	}
}

func TestPromptMultiSelect_RetriesOnInvalidInput(t *testing.T) {
	in := reader("99\n1\n")
	var out bytes.Buffer
	got, err := promptMultiSelect(in, &out, "header", []option{
		{label: "A", value: "a"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(out.String(), "Invalid selection") {
		t.Fatalf("expected error message, got %q", out.String())
	}
}

func TestPromptSingleSelect(t *testing.T) {
	in := reader("2\n")
	var out bytes.Buffer
	got, err := promptSingleSelect(in, &out, "scope", []option{
		{label: "Global", value: "global"}, {label: "Local", value: "local"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "local" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptYesNo_Defaults(t *testing.T) {
	cases := []struct {
		in         string
		defaultYes bool
		want       bool
	}{
		{"\n", true, true},
		{"\n", false, false},
		{"y\n", false, true},
		{"YES\n", false, true},
		{"n\n", true, false},
		{"nope\n", true, false},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		got := promptYesNo(reader(tc.in), &out, "ok? ", tc.defaultYes)
		if got != tc.want {
			t.Fatalf("input %q default=%v: got %v want %v", tc.in, tc.defaultYes, got, tc.want)
		}
	}
}

func TestPromptChoice_FullPath(t *testing.T) {
	// Pick both skill+agent (blank), pick claude only (1), pick local (2).
	in := reader("\n1\n2\n")
	var out bytes.Buffer
	got, err := promptChoice(in, &out, "/here")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.What) != 2 {
		t.Fatalf("What = %v", got.What)
	}
	if len(got.Tools) != 1 || got.Tools[0] != "claude" {
		t.Fatalf("Tools = %v", got.Tools)
	}
	if got.Scope != "local" {
		t.Fatalf("Scope = %q", got.Scope)
	}
	if !strings.Contains(out.String(), "current folder: /here") {
		t.Fatalf("cwd not shown in label: %q", out.String())
	}
}

func TestParseIndexes(t *testing.T) {
	got, ok := parseIndexes("1,3,2", 3)
	if !ok || len(got) != 3 || got[0] != 0 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("parseIndexes = %v ok=%v", got, ok)
	}
	if _, ok := parseIndexes("1,abc", 3); ok {
		t.Fatal("expected !ok on bad token")
	}
	if _, ok := parseIndexes("1,4", 3); ok {
		t.Fatal("expected !ok on out-of-range")
	}
	if _, ok := parseIndexes("", 3); ok {
		t.Fatal("expected !ok on empty token")
	}
}
