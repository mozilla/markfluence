package frontmatter

import (
	"strings"
	"testing"
)

func TestSetNestedAddsAnEntryToAnExistingBlock(t *testing.T) {
	got, err := SetNested("pages:\n  docs/a.md:\n    page_id: 1\n",
		[]string{"pages", "docs/b.md"},
		[]Field{{Key: "title", Value: "B"}, {Key: "page_id", Value: "2"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	want := "pages:\n  docs/a.md:\n    page_id: 1\n  docs/b.md:\n    title: B\n    page_id: 2\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The trap indentColumn exists for: a node built at the default position is
// emitted at column 1 whatever depth it was appended to, which turns a field
// inside an entry into a top-level setting. Valid YAML, entirely different
// meaning, and nothing downstream would refuse it.
func TestSetNestedDoesNotEscapeToTheTopLevel(t *testing.T) {
	got, err := SetNested("pages:\n  docs/a.md:\n    page_id: 1\n",
		[]string{"pages", "docs/a.md"}, []Field{{Key: "space", Value: "ENG"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	// Re-read and confirm "space" is *not* a top-level key.
	items, err := (Dialect{Doc: "d", Item: "k", MaxDepth: 2}).ReadMapping(got)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	for _, it := range items {
		if it.Key == "space" {
			t.Fatalf("space escaped to the top level:\n%s", got)
		}
	}
	if !strings.Contains(got, "    space: ENG") {
		t.Errorf("space is not indented inside the entry:\n%s", got)
	}
}

func TestSetNestedCreatesTheWholePath(t *testing.T) {
	got, err := SetNested("space: ENG\n", []string{"pages", "docs/a.md"},
		[]Field{{Key: "page_id", Value: "7"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	want := "space: ENG\npages:\n  docs/a.md:\n    page_id: 7\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The marker file ships as a comment and nothing else, so writing the first
// entry into one must not drop it.
func TestSetNestedKeepsACommentOnlyFilesComment(t *testing.T) {
	const marker = "# Marks the root of a markfluence project. Image and link paths are recorded\n" +
		"# relative to this directory. https://github.com/mozilla/markfluence\n"
	got, err := SetNested(marker, []string{"pages", "a.md"}, []Field{{Key: "page_id", Value: "1"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	if !strings.Contains(got, "Marks the root of a markfluence project") {
		t.Errorf("the comment was dropped:\n%s", got)
	}
	if !strings.Contains(got, "    page_id: 1") {
		t.Errorf("the entry is missing or misindented:\n%s", got)
	}
}

// An existing key keeps its own key node, which is where a preceding blank line
// and any comment live.
func TestSetNestedPreservesSurroundingContent(t *testing.T) {
	src := "# top comment\nspace: ENG\n\n# about the pages\npages:\n  docs/a.md:\n    title: A\n"
	got, err := SetNested(src, []string{"pages", "docs/a.md"}, []Field{{Key: "title", Value: "Changed"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	for _, want := range []string{"# top comment", "space: ENG", "# about the pages", "title: Changed"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "title: A") {
		t.Errorf("the old value survived:\n%s", got)
	}
}

func TestSetNestedFields(t *testing.T) {
	tests := map[string]struct {
		fields []Field
		want   string
	}{
		"a list": {
			[]Field{{Key: "labels", List: []string{"runbook", "ci/cd"}}},
			"    labels: [runbook, ci/cd]",
		},
		"an empty list is a declaration": {
			[]Field{{Key: "labels", List: []string{}}},
			"    labels: []",
		},
		"page_id is an integer": {
			[]Field{{Key: "page_id", Value: "123"}},
			"    page_id: 123",
		},
		"a null parent": {
			[]Field{{Key: "parent", Value: "null"}},
			"    parent: null",
		},
		"a colon needs quoting": {
			[]Field{{Key: "title", Value: "Deploy: Part 2"}},
			`title: "Deploy: Part 2"`,
		},
		"a value that looks like a float stays a string": {
			[]Field{{Key: "title", Value: ".inf"}},
			`title: ".inf"`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := SetNested("pages:\n  a.md:\n    page_id: 1\n",
				[]string{"pages", "a.md"}, tc.fields)
			if err != nil {
				t.Fatalf("SetNested: %v", err)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("want %q in:\n%s", tc.want, got)
			}
		})
	}
}

// A path level holding something that is not a mapping is refused rather than
// overwritten: `pages: nope` is somebody's mistake, not an empty block.
func TestSetNestedRefusesANonMappingLevel(t *testing.T) {
	_, err := SetNested("pages: nope\n", []string{"pages", "a.md"},
		[]Field{{Key: "page_id", Value: "1"}})
	if err == nil {
		t.Fatal("SetNested overwrote a scalar pages:, want an error")
	}
	if !strings.Contains(err.Error(), "not a mapping") {
		t.Errorf("error = %q", err)
	}
}

// `pages:` with nothing after it is a key somebody typed and stopped at; the
// caller asking to write into it meant an empty block.
func TestSetNestedFillsAValuelessLevel(t *testing.T) {
	got, err := SetNested("pages:\n", []string{"pages", "a.md"},
		[]Field{{Key: "page_id", Value: "1"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	if !strings.Contains(got, "  a.md:\n    page_id: 1") {
		t.Errorf("got:\n%s", got)
	}
}

// goccy renders a one-pair mapping as a MappingValueNode, so a single-field
// entry arrives in a different shape than a multi-field one.
func TestSetNestedHandlesASingleFieldEntry(t *testing.T) {
	got, err := SetNested("pages:\n  a.md:\n    page_id: 1\n",
		[]string{"pages", "a.md"}, []Field{{Key: "title", Value: "A"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	if !strings.Contains(got, "    title: A") || !strings.Contains(got, "    page_id: 1") {
		t.Errorf("got:\n%s", got)
	}
}

// Fields are inserted in the same canonical order frontmatter writes.
func TestSetNestedUsesCanonicalFieldOrder(t *testing.T) {
	got, err := SetNested("pages:\n  a.md: {}\n", []string{"pages", "a.md"}, []Field{
		{Key: "page_id", Value: "1"}, {Key: "labels", List: []string{"x"}},
		{Key: "title", Value: "A"}, {Key: "space", Value: "ENG"},
	})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	order := []string{"title", "space", "page_id", "labels"}
	at := -1
	for _, key := range order {
		i := strings.Index(got, key+":")
		if i < 0 {
			t.Fatalf("%s missing:\n%s", key, got)
		}
		if i < at {
			t.Errorf("%s is out of canonical order:\n%s", key, got)
		}
		at = i
	}
}

// Whatever SetNested returns must read back through the same dialect the loader
// uses -- that is the contract, and the reason the write verifies itself.
func TestSetNestedOutputReadsBack(t *testing.T) {
	got, err := SetNested("# marker\n", []string{"pages", "docs/a.md"}, []Field{
		{Key: "title", Value: "A: B"}, {Key: "page_id", Value: "9"},
		{Key: "labels", List: []string{"a", "b"}},
	})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	items, err := (Dialect{Doc: "d", Item: "k", MaxDepth: 2}).ReadMapping(got)
	if err != nil {
		t.Fatalf("the written file does not read back: %v\n%s", err, got)
	}
	if len(items) != 1 || items[0].Key != "pages" || items[0].Map == nil {
		t.Fatalf("items = %#v", items)
	}
	entry := items[0].Map[0]
	if entry.Key != "docs/a.md" || entry.Map == nil {
		t.Fatalf("entry = %#v", entry)
	}
	got2 := map[string]string{}
	for _, f := range entry.Map {
		if f.List == nil {
			got2[f.Key] = f.Value
		}
	}
	if got2["title"] != "A: B" || got2["page_id"] != "9" {
		t.Errorf("fields = %#v", got2)
	}
}
