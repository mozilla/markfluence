package pageref

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveNumericID(t *testing.T) {
	got, err := Resolve("123456")
	if err != nil || got != "123456" {
		t.Fatalf("Resolve = %q, %v; want 123456", got, err)
	}
}

func TestResolveURL(t *testing.T) {
	cases := []struct {
		arg, want string
	}{
		{"https://org.atlassian.net/wiki/spaces/ENG/pages/123456/Some+Title", "123456"},
		{"https://org.atlassian.net/wiki/spaces/ENG/pages/123456", "123456"},
		{"https://org.atlassian.net/wiki/spaces/ENG/pages/123456/", "123456"},
		{"https://org.atlassian.net/wiki/pages/viewpage.action?pageId=987", "987"},
		// A folder URL, which is what the browser hands you for a folder. children
		// takes one; the id is all Resolve reports either way.
		{"https://org.atlassian.net/wiki/spaces/~60c/folder/2972975121", "2972975121"},
		{"https://org.atlassian.net/wiki/spaces/ENG/folder/2972975121/", "2972975121"},
		// A query id wins over a path that has none.
		{"https://org.atlassian.net/wiki/x?pageId=42", "42"},
	}
	for _, c := range cases {
		got, err := Resolve(c.arg)
		if err != nil || got != c.want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", c.arg, got, err, c.want)
		}
	}
}

func TestResolveMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.md")
	if err := os.WriteFile(path, []byte("---\ntitle: T\npage_id: 555\n---\n\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(path)
	if err != nil || got != "555" {
		t.Fatalf("Resolve = %q, %v; want 555", got, err)
	}
}

// TestResolvePrefersFileOverID pins the precedence: a file named "123.md" is a
// file. Statting first is what makes that unambiguous.
func TestResolvePrefersFileOverID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "123.md")
	if err := os.WriteFile(path, []byte("---\npage_id: 999\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(path)
	if err != nil || got != "999" {
		t.Fatalf("Resolve = %q, %v; want the frontmatter id 999", got, err)
	}
}

func TestResolveRejects(t *testing.T) {
	dir := t.TempDir()
	noID := filepath.Join(dir, "noid.md")
	if err := os.WriteFile(noID, []byte("---\ntitle: T\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, arg string }{
		{"empty", ""},
		{"not a number or url", "banana"},
		{"a directory", dir},
		{"file without page_id", noID},
		{"url with no page id", "https://org.atlassian.net/wiki/spaces/ENG"},
		{"negative", "-5"},
		{"id with whitespace", "12 34"},
	}
	for _, c := range cases {
		if got, err := Resolve(c.arg); err == nil {
			t.Errorf("%s: Resolve(%q) = %q, nil; want an error", c.name, c.arg, got)
		}
	}
}

func TestIsDigits(t *testing.T) {
	for _, s := range []string{"0", "123456"} {
		if !IsDigits(s) {
			t.Errorf("IsDigits(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "12a", "-1", " 1", "1 "} {
		if IsDigits(s) {
			t.Errorf("IsDigits(%q) = true, want false", s)
		}
	}
}
