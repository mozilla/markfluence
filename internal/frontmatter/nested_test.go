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

// `pages: {}` is a flow mapping, and it is exactly the shape a project that
// has chosen the manifest but registered nothing has. A block entry appended
// to a flow mapping emits "pages: {\n  a.md:" and does not parse.
func TestSetNestedConvertsAFlowMappingToBlock(t *testing.T) {
	for name, src := range map[string]string{
		"empty flow":     "space: ENG\npages: {}\n",
		"populated flow": "pages: {a.md: {page_id: 1}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := SetNested(src, []string{"pages", "b.md"},
				[]Field{{Key: "page_id", Value: "2"}})
			if err != nil {
				t.Fatalf("SetNested: %v", err)
			}
			if strings.Contains(got, "{") {
				t.Errorf("still flow style:\n%s", got)
			}
			if !strings.Contains(got, "  b.md:\n    page_id: 2") {
				t.Errorf("entry not written as a block:\n%s", got)
			}
			// And whatever was already there survives.
			if name == "populated flow" && !strings.Contains(got, "a.md:") {
				t.Errorf("the existing entry was lost:\n%s", got)
			}
		})
	}
}

// A project file indented some other way must still be writable. A constant
// indent refused any pages: block not indented by exactly two spaces -- a
// four-space file, which the loader accepts, could never be recorded into, so
// create made the page and reported a parse failure.
func TestSetNestedFollowsTheExistingIndent(t *testing.T) {
	for name, src := range map[string]string{
		"four spaces": "pages:\n    a.md:\n        page_id: 1\n",
		"three":       "pages:\n   a.md:\n      page_id: 1\n",
		"one":         "pages:\n a.md:\n  page_id: 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := SetNested(src, []string{"pages", "b.md"},
				[]Field{{Key: "page_id", Value: "2"}})
			if err != nil {
				t.Fatalf("SetNested: %v", err)
			}
			// Both entries must be siblings under pages:, which is what the
			// one-space case got wrong by nesting b.md inside a.md.
			items, err := (Dialect{Doc: "d", Item: "k", MaxDepth: 2}).ReadMapping(got)
			if err != nil {
				t.Fatalf("does not read back: %v\n%s", err, got)
			}
			if len(items) != 1 || items[0].Map == nil || len(items[0].Map) != 2 {
				t.Fatalf("want two sibling entries, got:\n%s", got)
			}
		})
	}
}

// A page key is a path, and a path may hold any of YAML's indicators. Values
// have had the readsBackAs quoting fallback since #130; keys had nothing, so a
// legitimate filename was refused rather than quoted.
func TestSetNestedQuotesAKeyThatNeedsIt(t *testing.T) {
	keys := []string{
		"#x.md", "- a.md", "a: b.md", "a #b.md", "!a.md", "&a.md", "*a.md",
		"@a.md", "|a.md", ">a.md", "a\tb.md", " a.md", "a.md ", "{a}.md",
		"[a].md", "?a.md", "%a.md", "docs/ünïcode.md", "true", "123",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			got, err := SetNested("# marker\n", []string{"pages", key},
				[]Field{{Key: "page_id", Value: "1"}})
			if err != nil {
				t.Fatalf("SetNested(%q): %v", key, err)
			}
			items, err := (Dialect{Doc: "d", Item: "k", MaxDepth: 2}).ReadMapping(got)
			if err != nil {
				t.Fatalf("does not read back: %v\n%s", err, got)
			}
			if len(items) != 1 || items[0].Map == nil || len(items[0].Map) != 1 {
				t.Fatalf("unexpected shape:\n%s", got)
			}
			if items[0].Map[0].Key != key {
				t.Errorf("key read back as %q, wrote %q:\n%s", items[0].Map[0].Key, key, got)
			}
		})
	}
}

// An existing quoted key is matched and updated rather than duplicated.
func TestSetNestedUpdatesAnExistingQuotedKey(t *testing.T) {
	got, err := SetNested("pages:\n  \"a: b.md\":\n    page_id: 1\n",
		[]string{"pages", "a: b.md"}, []Field{{Key: "page_id", Value: "2"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	if strings.Count(got, "page_id") != 1 {
		t.Errorf("the entry was duplicated:\n%s", got)
	}
	if !strings.Contains(got, "page_id: 2") {
		t.Errorf("not updated:\n%s", got)
	}
}

// A document whose root mapping is itself indented is legal YAML and the loader
// accepts it. Computing a child's column from an absolute depth emitted the new
// entry at pages:' own column, which is the same bug childColumn fixed on the
// non-flow path -- still present on the flow one.
func TestSetNestedHandlesAnIndentedRootMapping(t *testing.T) {
	for name, src := range map[string]string{
		"indented flow":           "  pages: {}\n",
		"indented flow populated": "  space: ENG\n  pages: {a.md: {page_id: 1}}\n",
		"indented block":          "  pages:\n    a.md:\n      page_id: 1\n",
		"four-space root":         "    pages:\n      a.md:\n        page_id: 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := SetNested(src, []string{"pages", "b.md"},
				[]Field{{Key: "page_id", Value: "2"}})
			if err != nil {
				t.Fatalf("SetNested: %v\nfrom:\n%s", err, src)
			}
			items, err := (Dialect{Doc: "d", Item: "k", MaxDepth: 2}).ReadMapping(got)
			if err != nil {
				t.Fatalf("does not read back: %v\n%s", err, got)
			}
			var pages []Item
			for _, it := range items {
				if it.Key == "pages" {
					pages = it.Map
				}
			}
			if pages == nil {
				t.Fatalf("pages lost:\n%s", got)
			}
			found := false
			for _, e := range pages {
				if e.Key == "b.md" {
					found = true
				}
			}
			if !found {
				t.Errorf("b.md is not a sibling under pages:\n%s", got)
			}
		})
	}
}

// SetNested is exported and takes []Field, so a Comment must be honoured the
// way Render and setField honour it rather than dropped in silence.
func TestSetNestedHonoursAFieldComment(t *testing.T) {
	got, err := SetNested("# marker\n", []string{"pages", "a.md"},
		[]Field{{Key: "parent", Value: "123", Comment: "index.md"}})
	if err != nil {
		t.Fatalf("SetNested: %v", err)
	}
	if !strings.Contains(got, "index.md") {
		t.Errorf("the comment was dropped:\n%s", got)
	}
}
