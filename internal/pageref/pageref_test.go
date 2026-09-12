package pageref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/project"
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

// --- a file whose page_id lives in markfluence.yaml ---------------------------

// The seam an audit found after review: seven commands take a page argument
// through Resolve, and all of them read mf.PageID() alone -- so a file that
// update could publish from its pages: entry could not be named to info, read,
// children, export or the three attachment verbs.
func TestResolveReadsThePageIDFromTheManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename),
		[]byte("pages:\n  docs/a.md:\n    title: A\n    page_id: 12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pristine: no frontmatter at all.
	path := filepath.Join(dir, "docs", "a.md")
	if err := os.WriteFile(path, []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "12345" {
		t.Errorf("= %q, want 12345", got)
	}
}

// Frontmatter still wins where it speaks, and a file with neither still says
// so -- naming both places the id could go.
func TestResolveFrontmatterStillWinsAndNeitherIsReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename),
		[]byte("pages:\n  inline.md:\n    page_id: 999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Agreement, so no disagreement error: the entry and the file say the same.
	inline := filepath.Join(dir, "inline.md")
	if err := os.WriteFile(inline, []byte("---\npage_id: 999\n---\n# I\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve(inline); err != nil || got != "999" {
		t.Errorf("= %q/%v, want 999", got, err)
	}

	bare := filepath.Join(dir, "bare.md")
	if err := os.WriteFile(bare, []byte("# B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(bare)
	if err == nil {
		t.Fatal("want an error for a file with no page_id anywhere")
	}
	for _, want := range []string{"no page_id", project.Filename} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// The two locations naming different pages is the question being asked, so it
// is an error rather than a guess.
func TestResolveRefusesADisagreement(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename),
		[]byte("pages:\n  a.md:\n    page_id: 111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("---\npage_id: 222\n---\n# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(path); err == nil {
		t.Fatal("want an error when the two locations name different pages")
	}
}

// A malformed project file must not stop the frontmatter from answering. This
// function answers "which page does this argument name", and a project file it
// never consults should not fail that -- the commands that bound reads by the
// root report the problem themselves.
func TestResolveIgnoresAMalformedProjectFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename),
		[]byte("spce: ENG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("---\npage_id: 777\n---\n# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "777" {
		t.Errorf("= %q, want 777", got)
	}
}

// A file with no project file above it resolves exactly as it did before.
func TestResolveWithNoProjectFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("---\npage_id: 42\n---\n# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve(path); err != nil || got != "42" {
		t.Errorf("= %q/%v, want 42", got, err)
	}
}
