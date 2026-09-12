package frontmatter

import (
	"reflect"
	"strings"
	"testing"
)

// testDialect is the wording internal/project uses, so these tests pin the
// nouns a project-file diagnostic will actually carry.
var testDialect = Dialect{Doc: "a project file", Item: "setting"}

func TestReadMappingReadsScalarsAndSequences(t *testing.T) {
	items, err := testDialect.ReadMapping("space: ENG\nlabels: [a, b]\nempty: []\n")
	if err != nil {
		t.Fatalf("ReadMapping: %v", err)
	}
	want := []Item{
		{Key: "space", Line: 1, Value: "ENG"},
		{Key: "labels", Line: 2, List: []string{"a", "b"}},
		{Key: "empty", Line: 3, List: []string{}},
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("items = %#v, want %#v", items, want)
	}
}

// A nil List is what distinguishes a scalar from a sequence, and an empty but
// non-nil one is what makes "labels: []" expressible. A caller that checked
// len(List) instead would read the two the same way.
func TestReadMappingDistinguishesEmptyListFromScalar(t *testing.T) {
	items, err := testDialect.ReadMapping("a: []\nb: \"\"\n")
	if err != nil {
		t.Fatalf("ReadMapping: %v", err)
	}
	if items[0].List == nil {
		t.Error("a: [] read with a nil List; an empty sequence must stay a sequence")
	}
	if items[1].List != nil {
		t.Errorf(`b: "" read with List = %#v, want nil`, items[1].List)
	}
}

// The project file that ships today is a comment and nothing else, and export
// plants exactly that. Both it and an empty file must load as no settings
// rather than as a malformed document.
func TestReadMappingAcceptsEmptyAndCommentOnly(t *testing.T) {
	for name, text := range map[string]string{
		"empty":        "",
		"blank lines":  "\n\n",
		"comment only": "# Marks the root of a markfluence project.\n# https://example.com\n",
	} {
		t.Run(name, func(t *testing.T) {
			items, err := testDialect.ReadMapping(text)
			if err != nil {
				t.Fatalf("ReadMapping: %v", err)
			}
			if len(items) != 0 {
				t.Errorf("items = %#v, want none", items)
			}
		})
	}
}

func TestReadMappingLineNumbersAreDocumentRelative(t *testing.T) {
	// No "---" opener to account for, unlike a fenced block: line 1 of the
	// text is line 1 of the file.
	items, err := testDialect.ReadMapping("# comment\n\nspace: ENG\n")
	if err != nil {
		t.Fatalf("ReadMapping: %v", err)
	}
	if len(items) != 1 || items[0].Line != 3 {
		t.Errorf("items = %#v, want space on line 3", items)
	}
}

func TestReadMappingEveryNullSpellingIsEmpty(t *testing.T) {
	items, err := testDialect.ReadMapping("a: null\nb: Null\nc: ~\nd:\n")
	if err != nil {
		t.Fatalf("ReadMapping: %v", err)
	}
	for _, it := range items {
		if it.Value != "" {
			t.Errorf("%s = %q, want empty", it.Key, it.Value)
		}
	}
}

func TestReadMappingRefusals(t *testing.T) {
	tests := map[string]struct {
		text string
		want string
	}{
		"nested mapping":      {"space:\n  key: ENG\n", `setting "space" must be a single scalar value`},
		"literal block":       {"space: |\n  ENG\n", `setting "space" must be a single scalar value`},
		"folded block":        {"space: >\n  ENG\n", `setting "space" must be a single scalar value`},
		"anchor":              {"space: &anchor ENG\n", `setting "space" must be a single scalar value`},
		"tag":                 {"space: !!str ENG\n", `setting "space" must be a single scalar value`},
		"block element block": {"labels:\n  - |-\n    a\n", `setting "labels[0]" must be a single scalar value`},
		"continued scalar":    {"space: ENG\n  OPS\n", `must be a single-line scalar`},
		"top-level sequence":  {"- ENG\n", "a project file must be a flat mapping of key: value pairs"},
		"bare scalar":         {"ENG\n", "a project file must be a flat mapping of key: value pairs"},
		"second document":     {"space: ENG\n...\nspace: OPS\n", "a project file must be a single document"},
		"duplicate key":       {"space: ENG\nspace: OPS\n", "already defined"},
		"tab indent":          {"labels:\n\t- a\n", "cannot start any token"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := testDialect.ReadMapping(tc.text)
			if err == nil {
				t.Fatalf("ReadMapping(%q) succeeded, want an error", tc.text)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A parse error must stay one line wherever it comes from: these strings land
// verbatim in check --json and in a project-file load failure.
func TestReadMappingErrorsAreOneLine(t *testing.T) {
	_, err := testDialect.ReadMapping("space: ENG\nspace: OPS\n")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("error spans lines:\n%s", err)
	}
}

// The whole point of sharing the reader: a document read as a bare mapping and
// the same text read as a fenced block must agree about every value. A second
// copy of the dialect is how they start to differ.
func TestReadMappingAgreesWithFencedBlock(t *testing.T) {
	const body = "title: \"Deploy Runbook: Part 2\"\npage_id: 123\nparent: ~\nlabels: [runbook, ci/cd]\n"

	items, err := testDialect.ReadMapping(body)
	if err != nil {
		t.Fatalf("ReadMapping: %v", err)
	}
	scalars := map[string]string{}
	lists := map[string][]string{}
	for _, it := range items {
		if it.List != nil {
			lists[it.Key] = it.List
			continue
		}
		scalars[it.Key] = it.Value
	}

	mf, err := Parse("page.md", "---\n"+body+"---\nbody\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(scalars, mf.Frontmatter) {
		t.Errorf("scalars = %#v, frontmatter = %#v", scalars, mf.Frontmatter)
	}
	if !reflect.DeepEqual(lists, mf.Lists) {
		t.Errorf("lists = %#v, frontmatter lists = %#v", lists, mf.Lists)
	}
}

// scalarOnly is frontmatter's own rule, not the dialect's: a project file has
// no "parent" field to protect, and #139's pages: entries will want a mapping
// where frontmatter allows none.
func TestReadMappingDoesNotApplyFrontmattersScalarOnlyKeys(t *testing.T) {
	items, err := testDialect.ReadMapping("parent: [a, b]\n")
	if err != nil {
		t.Fatalf("ReadMapping: %v", err)
	}
	if len(items) != 1 || items[0].List == nil {
		t.Fatalf("items = %#v, want parent as a list", items)
	}
	if _, err := Parse("page.md", "---\nparent: [a, b]\n---\n"); err == nil {
		t.Error("frontmatter accepted a list-valued parent; scalarFields must still apply there")
	}
}
