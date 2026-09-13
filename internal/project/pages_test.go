package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// Separators are normalized before absoluteness is judged. Judging the raw
// string let these through as keys that normalize to absolute paths KeyFor can
// never produce -- a silently unreachable entry, which is worse than an error
// because nothing says so.
func TestNormalizePageKeyRefusesWindowsAbsoluteForms(t *testing.T) {
	for _, in := range []string{`\foo.md`, `C:\foo.md`, `c:/foo.md`, `\\server\share\a.md`} {
		t.Run(in, func(t *testing.T) {
			if got, err := NormalizePageKey(in); err == nil {
				t.Errorf("NormalizePageKey(%q) = %q, want an error", in, got)
			}
		})
	}
}

// A `pages:` key with nothing after it is a mapping-shaped setting given no
// mapping, which is refused rather than read as an empty block -- unlike a
// scalar setting, where blank means unset. An author who typed the key and
// stopped gets told, instead of silently having a project with no entries.
func TestPagesWithNoValueIsRefused(t *testing.T) {
	_, err := Discover(write(t, "pages:\n"))
	if err == nil {
		t.Fatal("Discover accepted a valueless pages:, want an error")
	}
	if !strings.Contains(err.Error(), `setting "pages" must be a mapping`) {
		t.Errorf("error = %q, want the must-be-a-mapping message", err)
	}
}

// IsPageField is entryFields[name] != 0, which is only sound because kind
// starts at iota + 1. A zero-valued kind would make every unknown name look
// known, so this pins the invariant rather than the lookup.
func TestKindZeroValueIsNotAValidKind(t *testing.T) {
	if kindScalar == 0 || kindList == 0 || kindMapping == 0 {
		t.Fatal("a kind is zero; IsPageField and the settings lookups both read 0 as absent")
	}
	if IsPageField("titel") || IsPageField("") {
		t.Error("an unknown name reported as a page field")
	}
	for _, name := range []string{"title", "space", "parent", "page_id", "page_width", "labels"} {
		if !IsPageField(name) {
			t.Errorf("%s is not reported as a page field", name)
		}
	}
}

// --- SetPageEntry -------------------------------------------------------------

// rootAt discovers a root in a directory holding the given project file.
func rootAt(t *testing.T, body string) *Root {
	t.Helper()
	root, err := Discover(write(t, body))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return root
}

func TestSetPageEntryWritesAnEntry(t *testing.T) {
	root := rootAt(t, "space: ENG\n")
	err := root.SetPageEntry("docs/a.md", Entry{
		Fields: map[string]string{"title": "A", "page_id": "123"},
		Lists:  map[string][]string{"labels": {"runbook"}},
	})
	if err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	body, err := os.ReadFile(root.File)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"space: ENG", "pages:", "  docs/a.md:", "    title: A",
		"    page_id: 123", "    labels: [runbook]"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	// And the in-memory Config is updated, so a caller that goes on to resolve
	// another file in the same run sees the entry it just wrote.
	if _, ok := root.Config.Pages["docs/a.md"]; !ok {
		t.Error("root.Config was not refreshed")
	}
}

// The marker that ships is a comment and nothing else, which is the file most
// first entries will be written into.
func TestSetPageEntryIntoTheShippedMarker(t *testing.T) {
	root := rootAt(t, "# Marks the root of a markfluence project. Image and link paths are recorded\n"+
		"# relative to this directory. https://github.com/mozilla/markfluence\n")
	if err := root.SetPageEntry("a.md", Entry{
		Fields: map[string]string{"page_id": "1"},
	}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	body, err := os.ReadFile(root.File)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Marks the root of a markfluence project") {
		t.Errorf("the comment was dropped:\n%s", body)
	}
	// It reads back through the loader, which is the contract.
	again, err := Discover(filepath.Dir(root.File))
	if err != nil {
		t.Fatalf("the written file does not load: %v\n%s", err, body)
	}
	defer func() { _ = again.FS.Close() }()
	if again.Config.Pages["a.md"].Fields["page_id"] != "1" {
		t.Errorf("entry did not read back:\n%s", body)
	}
}

// Updating an existing entry replaces values without disturbing its neighbours.
func TestSetPageEntryUpdatesInPlace(t *testing.T) {
	root := rootAt(t, "pages:\n  a.md:\n    page_id: 1\n    title: Old\n  b.md:\n    page_id: 2\n")
	if err := root.SetPageEntry("a.md", Entry{
		Fields: map[string]string{"title": "New", "page_id": "1"},
	}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	body, _ := os.ReadFile(root.File)
	if !strings.Contains(string(body), "title: New") || strings.Contains(string(body), "title: Old") {
		t.Errorf("title not replaced:\n%s", body)
	}
	if !strings.Contains(string(body), "  b.md:\n    page_id: 2") {
		t.Errorf("the neighbouring entry was disturbed:\n%s", body)
	}
}

// An empty labels list is a declaration ("remove them all"); an absent one is
// not the same thing, and the two must not collapse.
func TestSetPageEntryEmptyListIsWritten(t *testing.T) {
	root := rootAt(t, "# marker\n")
	if err := root.SetPageEntry("a.md", Entry{
		Fields: map[string]string{"page_id": "1"},
		Lists:  map[string][]string{"labels": {}},
	}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	body, _ := os.ReadFile(root.File)
	if !strings.Contains(string(body), "labels: []") {
		t.Errorf("an empty declared list was not written:\n%s", body)
	}
}

// A value the YAML dialect has to quote must survive the round trip -- the
// write-then-re-read rule, at the entry level.
func TestSetPageEntryQuotesWhatNeedsIt(t *testing.T) {
	root := rootAt(t, "# marker\n")
	if err := root.SetPageEntry("a.md", Entry{
		Fields: map[string]string{"title": "Deploy: Part 2", "page_id": "1"},
	}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	again, err := Discover(filepath.Dir(root.File))
	if err != nil {
		t.Fatalf("the written file does not load: %v", err)
	}
	defer func() { _ = again.FS.Close() }()
	if got := again.Config.Pages["a.md"].Fields["title"]; got != "Deploy: Part 2" {
		t.Errorf("title read back as %q", got)
	}
}

// Nothing is written when the result would not load. The guard matters because
// the alternative is a tool that corrupts the file it was recording success in.
//
// The file is corrupted *after* discovery, which is the only way to reach this:
// a project file that cannot be loaded cannot be discovered either. It is also
// the realistic shape -- something else edited the file while a run was in
// flight.
func TestSetPageEntryWritesNothingWhenTheResultWouldNotLoad(t *testing.T) {
	root := rootAt(t, "space: ENG\n")
	corrupt := "pages: nope\n"
	if err := os.WriteFile(root.File, []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := root.SetPageEntry("a.md", Entry{Fields: map[string]string{"page_id": "1"}}); err == nil {
		t.Fatal("SetPageEntry succeeded against a scalar pages:, want an error")
	}
	after, _ := os.ReadFile(root.File)
	if string(after) != corrupt {
		t.Errorf("the file was modified:\n%s", after)
	}
}

// The same guard against a file that turned unreadable under us.
func TestSetPageEntryReportsAnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 file")
	}
	root := rootAt(t, "space: ENG\n")
	if err := os.Chmod(root.File, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root.File, 0o644) })
	if err := root.SetPageEntry("a.md", Entry{Fields: map[string]string{"page_id": "1"}}); err == nil {
		t.Fatal("SetPageEntry succeeded on an unreadable file, want an error")
	}
}

// A root with no project file has nowhere to write, and says so rather than
// creating one: whether markfluence may create a project file is a separate
// decision (#5), not something a create should make silently.
func TestSetPageEntryRefusesWithNoProjectFile(t *testing.T) {
	dir := t.TempDir()
	root, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.FS.Close() }()
	if err := root.SetPageEntry("a.md", Entry{}); err == nil {
		t.Fatal("SetPageEntry succeeded with no project file, want an error")
	}
	if _, err := os.Stat(filepath.Join(dir, Filename)); err == nil {
		t.Error("a project file was created")
	}
}

// Fields land in frontmatter's canonical order, so an entry reads like a
// frontmatter block -- which is the whole premise of the shape.
func TestSetPageEntryUsesCanonicalOrder(t *testing.T) {
	root := rootAt(t, "# marker\n")
	if err := root.SetPageEntry("a.md", Entry{
		Fields: map[string]string{
			"page_width": "wide", "title": "A", "page_id": "1", "space": "ENG", "parent": "null",
		},
		Lists: map[string][]string{"labels": {"x"}},
	}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	body, _ := os.ReadFile(root.File)
	at := -1
	for _, key := range []string{"title", "space", "parent", "page_id", "labels", "page_width"} {
		i := strings.Index(string(body), key+":")
		if i < 0 {
			t.Fatalf("%s missing:\n%s", key, body)
		}
		if i < at {
			t.Errorf("%s out of canonical order:\n%s", key, body)
		}
		at = i
	}
}

// parseConfig strips a BOM deliberately, so a BOM-prefixed project file is a
// shape markfluence accepts. Leaving it in made the first key parse as
// "\ufeffpages", so the writer created a second pages: block beside it and the
// reload refused the file -- page created, nothing recorded.
func TestSetPageEntryHandlesABOM(t *testing.T) {
	for name, body := range map[string]string{
		"bom then a setting": "\ufeffspace: ENG\n",
		"bom then pages":     "\ufeffpages: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := rootAt(t, body)
			if err := root.SetPageEntry("a.md", Entry{
				Fields: map[string]string{"page_id": "1"},
			}); err != nil {
				t.Fatalf("SetPageEntry: %v", err)
			}
			written := mustReadFile(t, root.File)
			if !strings.HasPrefix(written, "\ufeff") {
				t.Errorf("the BOM was dropped:\n%q", written)
			}
			again, err := Discover(filepath.Dir(root.File))
			if err != nil {
				t.Fatalf("the written file does not load: %v\n%q", err, written)
			}
			defer func() { _ = again.FS.Close() }()
			if again.Config.Pages["a.md"].Fields["page_id"] != "1" {
				t.Errorf("entry did not read back:\n%q", written)
			}
		})
	}
}

// Keys are compared after normalization, so an entry written as "./b.md" *is*
// the entry for "b.md". Appending a normalized second spelling made the file
// hold two keys naming one path, which the loader then refused.
func TestSetPageEntryUpdatesAnUnnormalizedKey(t *testing.T) {
	for _, spelling := range []string{"./b.md", "docs/../b.md"} {
		t.Run(spelling, func(t *testing.T) {
			root := rootAt(t, "pages:\n  "+spelling+":\n    title: B\n")
			if err := root.SetPageEntry("b.md", Entry{
				Fields: map[string]string{"page_id": "2"},
			}); err != nil {
				t.Fatalf("SetPageEntry: %v", err)
			}
			written := mustReadFile(t, root.File)
			if strings.Count(written, "title: B") != 1 {
				t.Errorf("the entry was duplicated:\n%s", written)
			}
			again, err := Discover(filepath.Dir(root.File))
			if err != nil {
				t.Fatalf("the written file does not load: %v\n%s", err, written)
			}
			defer func() { _ = again.FS.Close() }()
			if again.Config.Pages["b.md"].Fields["page_id"] != "2" {
				t.Errorf("entry did not read back:\n%s", written)
			}
		})
	}
}

// A project file indented some other way must be writable: the loader accepts
// it, so create has to be able to record into it.
func TestSetPageEntryFollowsTheExistingIndent(t *testing.T) {
	root := rootAt(t, "pages:\n    a.md:\n        page_id: 1\n")
	if err := root.SetPageEntry("b.md", Entry{Fields: map[string]string{"page_id": "2"}}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	again, err := Discover(filepath.Dir(root.File))
	if err != nil {
		t.Fatalf("the written file does not load: %v\n%s", err, mustReadFile(t, root.File))
	}
	defer func() { _ = again.FS.Close() }()
	if len(again.Config.Pages) != 2 {
		t.Errorf("want two entries, got %#v:\n%s", again.Config.Pages, mustReadFile(t, root.File))
	}
}

// A page key is a path, and a path may hold YAML indicators.
func TestSetPageEntryQuotesAKeyThatNeedsIt(t *testing.T) {
	for _, key := range []string{"docs/a: b.md", "#a.md", "- a.md", "docs/ünïcode.md"} {
		t.Run(key, func(t *testing.T) {
			root := rootAt(t, "# marker\n")
			if err := root.SetPageEntry(key, Entry{
				Fields: map[string]string{"page_id": "1"},
			}); err != nil {
				t.Fatalf("SetPageEntry(%q): %v", key, err)
			}
			again, err := Discover(filepath.Dir(root.File))
			if err != nil {
				t.Fatalf("does not load: %v\n%s", err, mustReadFile(t, root.File))
			}
			defer func() { _ = again.FS.Close() }()
			if _, ok := again.Config.Pages[key]; !ok {
				t.Errorf("no entry for %q:\n%s", key, mustReadFile(t, root.File))
			}
		})
	}
}

// The write goes through a temporary file and a rename, so an interrupted write
// cannot truncate the one file holding every entry in the project.
func TestSetPageEntryLeavesNoTemporaryFile(t *testing.T) {
	root := rootAt(t, "# marker\n")
	if err := root.SetPageEntry("a.md", Entry{Fields: map[string]string{"page_id": "1"}}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(root.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != Filename {
			t.Errorf("left %q behind", e.Name())
		}
	}
}

// An existing file's mode is preserved across the replace.
func TestSetPageEntryPreservesTheFileMode(t *testing.T) {
	root := rootAt(t, "# marker\n")
	if err := os.Chmod(root.File, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.SetPageEntry("a.md", Entry{Fields: map[string]string{"page_id": "1"}}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	info, err := os.Stat(root.File)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %v, want 0600", got)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Before the manifest, each page's metadata went into its own file, so two
// concurrent creates could not collide. They share one file now, which
// reintroduces the lost update: A reads, B reads, A writes, B writes, and A's
// entry is gone while A's page exists -- "page created, entry not recorded"
// again, arriving from concurrency rather than from a refusal.
//
// The two tests below drive the collision through the beforeReplace hook, which
// is the only way to land a competing write *inside* the read-modify-write
// window. An earlier version of this test wrote the competing entry before
// calling SetPageEntry at all, so the initial read already saw it and no
// collision occurred -- it passed with the detection removed, which is how it
// was caught.

// The give-up path: a file that changes before *every* attempt is reported
// rather than silently clobbered. Driven by the beforeReplace hook, since
// racing a real writer would make a slow and flaky test of a branch that
// exists precisely so a collision is never resolved by guessing.
func TestSetPageEntryGivesUpIfTheFileKeepsChanging(t *testing.T) {
	root := rootAt(t, "pages:\n  a.md:\n    page_id: 1\n")

	n := 0
	beforeReplace = func() {
		n++
		_ = os.WriteFile(root.File,
			[]byte(fmt.Sprintf("pages:\n  a.md:\n    page_id: %d\n", n+1)), 0o644)
	}
	t.Cleanup(func() { beforeReplace = nil })

	err := root.SetPageEntry("c.md", Entry{Fields: map[string]string{"page_id": "3"}})
	if err == nil {
		t.Fatal("SetPageEntry reported success while the file changed before every attempt")
	}
	if !strings.Contains(err.Error(), "changed while the entry") {
		t.Errorf("error = %q, want the concurrent-change message", err)
	}
	if n != 2 {
		t.Errorf("attempted %d times, want 2", n)
	}
	// And nothing of ours was written over the other writer's file.
	if strings.Contains(mustReadFile(t, root.File), "c.md") {
		t.Errorf("our entry was written despite the collision:\n%s", mustReadFile(t, root.File))
	}
}

// One collision is absorbed: the second attempt merges into the winner's file.
func TestSetPageEntryRetriesOnceAndSucceeds(t *testing.T) {
	root := rootAt(t, "pages:\n  a.md:\n    page_id: 1\n")

	once := false
	beforeReplace = func() {
		if once {
			return
		}
		once = true
		_ = os.WriteFile(root.File,
			[]byte("pages:\n  a.md:\n    page_id: 1\n  b.md:\n    page_id: 2\n"), 0o644)
	}
	t.Cleanup(func() { beforeReplace = nil })

	if err := root.SetPageEntry("c.md", Entry{Fields: map[string]string{"page_id": "3"}}); err != nil {
		t.Fatalf("SetPageEntry: %v", err)
	}
	again, err := Discover(filepath.Dir(root.File))
	if err != nil {
		t.Fatalf("does not load: %v", err)
	}
	defer func() { _ = again.FS.Close() }()
	for _, key := range []string{"a.md", "b.md", "c.md"} {
		if _, ok := again.Config.Pages[key]; !ok {
			t.Errorf("%s is missing:\n%s", key, mustReadFile(t, root.File))
		}
	}
}
