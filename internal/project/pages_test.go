package project

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// discoverPages loads a project file and returns its entries.
func discoverPages(t *testing.T, body string) map[string]Entry {
	t.Helper()
	root, err := Discover(write(t, body))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return root.Config.Pages
}

func TestReadPagesReadsEntries(t *testing.T) {
	pages := discoverPages(t, `space: ENG
pages:
  docs/engineering-docs.md:
    title: Engineering Docs
    page_id: 12345
    labels: [howto]

  docs/deploy-runbook.md:
    title: Deploy Runbook
    parent: docs/engineering-docs.md
    page_id: 12346
    page_width: wide
`)
	if len(pages) != 2 {
		t.Fatalf("pages = %#v, want two", pages)
	}
	got := pages["docs/deploy-runbook.md"]
	want := map[string]string{
		"title":      "Deploy Runbook",
		"parent":     "docs/engineering-docs.md",
		"page_id":    "12346",
		"page_width": "wide",
	}
	if !reflect.DeepEqual(got.Fields, want) {
		t.Errorf("fields = %#v, want %#v", got.Fields, want)
	}
	if len(got.Lists) != 0 {
		t.Errorf("lists = %#v, want none", got.Lists)
	}
	if l := pages["docs/engineering-docs.md"].Lists["labels"]; len(l) != 1 || l[0] != "howto" {
		t.Errorf("labels = %#v, want [howto]", l)
	}
}

// An entry is shaped exactly like a parsed frontmatter block, which is what
// lets internal/labels and internal/pagewidth validate one unchanged -- this
// package cannot import either of them (both reach internal/client, which
// holds a *Cache), so the two maps are the whole mechanism.
func TestEntryIsShapedLikeAFrontmatterBlock(t *testing.T) {
	pages := discoverPages(t, "pages:\n  a.md:\n    title: A\n    labels: [x, y]\n")
	e := pages["a.md"]
	if e.Fields == nil || e.Lists == nil {
		t.Fatalf("entry = %#v, want both maps non-nil", e)
	}
	// The shapes MarkdownFile.Frontmatter and .Lists have -- asserted by
	// handing them to functions typed for those, which is what every consumer
	// does and what would break if Entry were ever re-typed.
	takesFrontmatter(e.Fields)
	takesLists(e.Lists)
}

// A project with no pages: key has not chosen the manifest; one with an empty
// pages: has, and has registered nothing. Nil vs empty is how a command tells
// them apart, and D9 turns on exactly that.
func TestPagesNilWhenAbsentAndEmptyWhenDeclared(t *testing.T) {
	if pages := discoverPages(t, "space: ENG\n"); pages != nil {
		t.Errorf("pages = %#v, want nil when there is no pages: key", pages)
	}
	pages := discoverPages(t, "pages: {}\n")
	if pages == nil {
		t.Fatal("pages = nil for `pages: {}`, want an empty non-nil map")
	}
	if len(pages) != 0 {
		t.Errorf("pages = %#v, want empty", pages)
	}
}

func TestReadPagesRefusals(t *testing.T) {
	tests := map[string]struct{ body, want string }{
		"entry is a scalar": {
			"pages:\n  a.md: 123\n", `page "a.md" must be a mapping of fields`},
		"unknown field": {
			"pages:\n  a.md:\n    titel: A\n",
			`page "a.md" has an unknown field "titel"`},
		"unknown field names the known ones": {
			"pages:\n  a.md:\n    titel: A\n",
			"known: labels, page_id, page_width, parent, space, title"},
		"unknown field suggests a newer markfluence": {
			"pages:\n  a.md:\n    titel: A\n", "needs a newer markfluence"},
		"scalar field given a list": {
			"pages:\n  a.md:\n    title: [A, B]\n",
			`page "a.md" field "title" must be a single value, not a list`},
		"list field given a scalar": {
			"pages:\n  a.md:\n    labels: one\n",
			`page "a.md" field "labels" must be a list`},
		"field given a mapping": {
			"pages:\n  a.md:\n    title:\n      deeper: x\n",
			`setting "title" must be a single scalar value, found Mapping`},
		"key escaping the root": {
			"pages:\n  ../elsewhere.md:\n    title: A\n",
			`page "../elsewhere.md" is outside the project root`},
		"absolute key": {
			"pages:\n  /etc/passwd.md:\n    title: A\n",
			`must be relative to the project root, not absolute`},
		"two keys normalizing to one": {
			"pages:\n  docs/a.md:\n    title: A\n  ./docs/a.md:\n    title: B\n",
			`both name "docs/a.md"`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Discover(write(t, tc.body))
			if err == nil {
				t.Fatalf("Discover accepted %q, want an error", tc.body)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Errorf("error is %T, want it to unwrap to *ConfigError", err)
			}
		})
	}
}

// A bad *value* is deliberately not checked here: #139 requires it to be
// reported only when that entry's file is one of the arguments, so one bad
// entry cannot block every invocation in the repo. Structure is this package's
// job; vocabulary is the command's.
func TestReadPagesDoesNotValidateValues(t *testing.T) {
	pages := discoverPages(t, `pages:
  a.md:
    page_id: not-a-number
    page_width: huge
    labels: [Has Space]
`)
	e := pages["a.md"]
	if e.Fields["page_id"] != "not-a-number" || e.Fields["page_width"] != "huge" {
		t.Errorf("fields = %#v, want the raw values preserved for the caller to judge", e.Fields)
	}
	if l := e.Lists["labels"]; len(l) != 1 || l[0] != "Has Space" {
		t.Errorf("labels = %#v, want the raw value", l)
	}
}

func TestNormalizePageKey(t *testing.T) {
	ok := map[string]string{
		"docs/a.md":        "docs/a.md",
		"./docs/a.md":      "docs/a.md",
		"docs//a.md":       "docs/a.md",
		"docs/./a.md":      "docs/a.md",
		"docs/sub/../a.md": "docs/a.md",
		"a.md":             "a.md",
		// Backslashes are normalized so a key written on Windows matches the
		// slash form every other path in markfluence uses.
		`docs\a.md`: "docs/a.md",
	}
	for in, want := range ok {
		t.Run(in, func(t *testing.T) {
			got, err := NormalizePageKey(in)
			if err != nil {
				t.Fatalf("NormalizePageKey(%q): %v", in, err)
			}
			if got != want {
				t.Errorf("= %q, want %q", got, want)
			}
		})
	}

	bad := []string{"", "..", "../x.md", "docs/../../x.md", "/abs.md", "."}
	for _, in := range bad {
		t.Run("refuses "+in, func(t *testing.T) {
			if got, err := NormalizePageKey(in); err == nil {
				t.Errorf("NormalizePageKey(%q) = %q, want an error", in, got)
			}
		})
	}
}

// Lexical only, no symlink resolution: L2 forbids a key whose meaning depends
// on the checkout's layout, and a symlinked docs/ is legitimate.
func TestNormalizePageKeyIsLexical(t *testing.T) {
	// A path that would escape only after resolving a symlink is still just a
	// path here, and one that escapes lexically is refused without touching
	// the filesystem -- NormalizePageKey never stats anything.
	if _, err := NormalizePageKey("docs/../../etc/passwd.md"); err == nil {
		t.Error("want a refusal for a lexically escaping key")
	}
	got, err := NormalizePageKey("docs/link/a.md")
	if err != nil || got != "docs/link/a.md" {
		t.Errorf("= %q/%v, want the path unchanged and no stat", got, err)
	}
}

// A page key is compared byte-for-byte after normalization, with no case
// folding. Documented limit: on a case-insensitive filesystem the file opens
// and the key does not match, so the file reads as unmanaged.
func TestNormalizePageKeyDoesNotFoldCase(t *testing.T) {
	upper, _ := NormalizePageKey("Docs/A.md")
	lower, _ := NormalizePageKey("docs/a.md")
	if upper == lower {
		t.Error("keys were case-folded; the documented behavior is that they are not")
	}
}

// The duplicate-key error names both spellings, since naming only the survivor
// would leave the author hunting for the other one.
func TestDuplicatePageKeyNamesBothSpellings(t *testing.T) {
	_, err := Discover(write(t,
		"pages:\n  docs/a.md:\n    title: A\n  docs/./a.md:\n    title: B\n"))
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"docs/a.md", "docs/./a.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

// Both load-time errors are structural, so they are not scoped per file: an
// escaping or duplicate key means the manifest is wrong, not that one entry is.
func TestStructuralPageErrorsAreNotPerFile(t *testing.T) {
	dir := write(t, "pages:\n  ../out.md:\n    title: A\n  fine.md:\n    title: B\n")
	if _, err := Discover(dir); err == nil {
		t.Fatal("Discover succeeded; one escaping key must fail the load outright")
	}
}

func takesFrontmatter(map[string]string) {}
func takesLists(map[string][]string)     {}
