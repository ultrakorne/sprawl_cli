package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadJSONFromSource_Stdin(t *testing.T) {
	got, err := loadJSONFromSource("-", strings.NewReader(`{"title":"hi","description":"x"}`))
	if err != nil {
		t.Fatalf("loadJSONFromSource: %v", err)
	}
	if got["title"] != "hi" || got["description"] != "x" {
		t.Fatalf("got = %+v", got)
	}
}

func TestLoadJSONFromSource_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "attrs.json")
	if err := os.WriteFile(path, []byte(`{"title":"from-file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadJSONFromSource(path, strings.NewReader(""))
	if err != nil {
		t.Fatalf("loadJSONFromSource: %v", err)
	}
	if got["title"] != "from-file" {
		t.Fatalf("got = %+v", got)
	}
}

func TestLoadJSONFromSource_RejectsArrayTopLevel(t *testing.T) {
	_, err := loadJSONFromSource("-", strings.NewReader(`[1,2,3]`))
	if err == nil {
		t.Fatal("expected error for array top-level JSON")
	}
}

func TestLoadJSONFromSource_MissingFile(t *testing.T) {
	_, err := loadJSONFromSource(filepath.Join(t.TempDir(), "nope.json"), strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestMergeProjectID(t *testing.T) {
	attrs := map[string]any{}
	if err := mergeProjectID(attrs, ""); err != nil {
		t.Fatalf("empty flag should no-op, got %v", err)
	}
	if _, ok := attrs["project_id"]; ok {
		t.Fatal("empty flag should leave attrs untouched")
	}
	if err := mergeProjectID(attrs, "42"); err != nil {
		t.Fatalf("mergeProjectID: %v", err)
	}
	if attrs["project_id"] != 42 {
		t.Fatalf("project_id = %v", attrs["project_id"])
	}
	if err := mergeProjectID(attrs, "not-an-int"); err == nil {
		t.Fatal("expected error for non-integer project id")
	}
}

func TestRequireAttrs(t *testing.T) {
	if err := requireAttrs(map[string]any{}, "task update"); err == nil {
		t.Fatal("expected error on empty attrs")
	}
	if err := requireAttrs(map[string]any{"title": "x"}, "task update"); err != nil {
		t.Fatalf("non-empty attrs should pass, got %v", err)
	}
}

func TestMergeStringFlag(t *testing.T) {
	attrs := map[string]any{}
	mergeStringFlag(attrs, "title", "")
	if _, ok := attrs["title"]; ok {
		t.Fatal("empty value should skip merge")
	}
	mergeStringFlag(attrs, "title", "hello")
	if attrs["title"] != "hello" {
		t.Fatalf("title = %v", attrs["title"])
	}
}
