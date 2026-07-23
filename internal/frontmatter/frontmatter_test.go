package frontmatter_test

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/frontmatter"
)

// value extracts frontmatter and returns the value for key.
func value(t *testing.T, body, key string) string {
	t.Helper()
	fm, _ := frontmatter.Extract(body)
	v, ok := fm[key]
	if !ok {
		t.Fatalf("key %q not found in frontmatter of:\n%s", key, body)
	}
	return v
}

// --- read: quotes suppress inline-comment stripping --------------------------

func TestReadValues(t *testing.T) {
	tests := []struct {
		name string
		body string
		key  string
		want string
	}{
		{"double-quoted keeps hash", "---\ntitle: \"Detect # Verify\"\n---\nx\n", "title", "Detect # Verify"},
		{"single-quoted keeps hash", "---\ntitle: 'Detect # Verify'\n---\nx\n", "title", "Detect # Verify"},
		{"unquoted strips inline comment", "---\ntitle: Detect # Verify\n---\nx\n", "title", "Detect"},
		{"parent comment form reads value only", "---\nparent: 4  # foo.md\n---\nx\n", "parent", "4"},
		{"single-quote escape", "---\ntitle: 'it''s here'\n---\nx\n", "title", "it's here"},
		{"double-quote escapes", "---\ntitle: \"say \\\"hi\\\"\"\n---\nx\n", "title", `say "hi"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := value(t, tc.body, tc.key); got != tc.want {
				t.Errorf("value = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseValueUnterminatedQuoteFallsBackToLiteral(t *testing.T) {
	if got := frontmatter.ParseValue(` "oops`); got != `"oops` {
		t.Errorf("ParseValue = %q, want %q", got, `"oops`)
	}
}

func TestPlainValuesUnaffected(t *testing.T) {
	fm, _ := frontmatter.Extract("---\npage_id: 5\nspace: ENG\n---\nx\n")
	if fm["page_id"] != "5" || fm["space"] != "ENG" || len(fm) != 2 {
		t.Errorf("frontmatter = %v, want {page_id:5, space:ENG}", fm)
	}
}

// --- write: auto-quote only when needed --------------------------------------

// fieldLine writes key/value into a fixed doc and returns the "key: ..." line.
func fieldLine(t *testing.T, key, value, comment string) string {
	t.Helper()
	md := frontmatter.UpdateField("---\nk: x\n---\nbody\n", key, value, comment)
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, key+":") {
			return line
		}
	}
	t.Fatalf("no line starting with %q in:\n%s", key+":", md)
	return ""
}

func TestWriteRendering(t *testing.T) {
	tests := []struct {
		name, key, value, comment, want string
	}{
		{"safe value bare", "title", "Hello World", "", "title: Hello World"},
		{"numeric bare", "page_id", "12345", "", "page_id: 12345"},
		{"colon value bare", "title", "a: b", "", "title: a: b"},
		{"inline-comment marker quoted", "title", "Detect # Verify", "", "title: 'Detect # Verify'"},
		{"leading whitespace quoted", "title", "  x", "", "title: '  x'"},
		{"comment separate from value", "parent", "4", "foo.md", "parent: 4  # foo.md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fieldLine(t, tc.key, tc.value, tc.comment); got != tc.want {
				t.Errorf("line = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	values := []string{"Detect # Verify", "it's here", `say "hi"`, "  pad  ", "#lead", "plain", "a: b"}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			md := frontmatter.UpdateField("---\nk: x\n---\nb\n", "k", v, "")
			fm, _ := frontmatter.Extract(md)
			if fm["k"] != v {
				t.Errorf("round-trip of %q = %q", v, fm["k"])
			}
		})
	}
}

func TestParentValueRoundTripsWithoutTheComment(t *testing.T) {
	md := frontmatter.UpdateField("---\nk: x\n---\nb\n", "parent", "4", "foo.md")
	fm, _ := frontmatter.Extract(md)
	if fm["parent"] != "4" {
		t.Errorf("parent = %q, want %q", fm["parent"], "4")
	}
}

// --- write: canonical field order --------------------------------------------

func TestUpdateFieldCanonicalOrder(t *testing.T) {
	// Fields present in a jumbled order plus an extra key; updating any field
	// rewrites the whole block as title, space, parent, page_id, then the rest
	// alphabetically.
	in := "---\npage_width: max\npage_id: 9\ncustom: z\nparent: 4\nspace: ENG\ntitle: T\n---\nbody\n"
	got := frontmatter.UpdateField(in, "page_id", "10", "")
	want := "---\ntitle: T\nspace: ENG\nparent: 4\npage_id: 10\ncustom: z\npage_width: max\n---\nbody\n"
	if got != want {
		t.Errorf("UpdateField reorder =\n%q\nwant\n%q", got, want)
	}
}

func TestUpdateFieldPreservesCommentsDropsBlanks(t *testing.T) {
	in := "---\n# a note\ntitle: T\n\npage_id: 9\n---\nbody\n"
	got := frontmatter.UpdateField(in, "space", "ENG", "")
	want := "---\n# a note\ntitle: T\nspace: ENG\npage_id: 9\n---\nbody\n"
	if got != want {
		t.Errorf("UpdateField comments/blanks =\n%q\nwant\n%q", got, want)
	}
}

// --- MarkdownFile accessors --------------------------------------------------

func TestMarkdownFileAccessors(t *testing.T) {
	md := frontmatter.Parse("doc.md",
		"---\ntitle: My Page\npage_id: 123\nspace: ENG\nparent: 456\n---\nbody\n")
	if md.Title() != "My Page" || md.PageID() != "123" || md.Space() != "ENG" || md.Parent() != "456" {
		t.Errorf("accessors = %q/%q/%q/%q", md.Title(), md.PageID(), md.Space(), md.Parent())
	}
	if md.Body != "body\n" {
		t.Errorf("Body = %q, want %q", md.Body, "body\n")
	}
}

func TestCoordinateSentinelsAreUnset(t *testing.T) {
	// Missing, blank, and literal "null" all read as "" for coordinate fields.
	for _, doc := range []string{
		"---\ntitle: X\n---\nb\n",                // page_id missing
		"---\ntitle: X\npage_id:\n---\nb\n",      // blank
		"---\ntitle: X\npage_id: null\n---\nb\n", // literal null
	} {
		md := frontmatter.Parse("doc.md", doc)
		if md.PageID() != "" {
			t.Errorf("PageID() = %q for %q, want empty", md.PageID(), doc)
		}
	}
}

func TestTitleKeepsLiteralNullButBlankIsEmpty(t *testing.T) {
	if md := frontmatter.Parse("d.md", "---\ntitle: null\n---\nb\n"); md.Title() != "null" {
		t.Errorf("Title() = %q, want %q (a title is free text)", md.Title(), "null")
	}
	if md := frontmatter.Parse("d.md", "---\ntitle:\n---\nb\n"); md.Title() != "" {
		t.Errorf("Title() = %q, want empty for blank", md.Title())
	}
}
