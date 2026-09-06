package frontmatter_test

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/frontmatter"
)

// value parses content and returns one frontmatter field.
func value(t *testing.T, content, key string) string {
	t.Helper()
	mf, err := frontmatter.Parse("doc.md", content)
	if err != nil {
		t.Fatalf("Parse(%q) = %v", content, err)
	}
	return mf.Frontmatter[key]
}

func update(t *testing.T, content, key, val, comment string) string {
	t.Helper()
	got, err := frontmatter.UpdateField(content, key, val, comment)
	if err != nil {
		t.Fatalf("UpdateField(%q, %q, %q) = %v", content, key, val, err)
	}
	return got
}

// --- read ---------------------------------------------------------------------

func TestReadValues(t *testing.T) {
	tests := []struct{ name, content, key, want string }{
		{"plain", "---\ntitle: Hello\n---\nx\n", "title", "Hello"},
		{"double-quoted keeps hash", "---\ntitle: \"Detect # Verify\"\n---\nx\n", "title", "Detect # Verify"},
		{"single-quoted keeps hash", "---\ntitle: 'Detect # Verify'\n---\nx\n", "title", "Detect # Verify"},
		{"single-quote escape", "---\ntitle: 'it''s here'\n---\nx\n", "title", "it's here"},
		{"inline comment stripped", "---\ntitle: Hello # note\n---\nx\n", "title", "Hello"},
		{"colon inside quotes", "---\ntitle: \"a: b\"\n---\nx\n", "title", "a: b"},
		{"integer stays digits", "---\npage_id: 123\n---\nx\n", "page_id", "123"},
		{"tilde space key is a string", "---\nspace: ~abc\n---\nx\n", "space", "~abc"},
		{"unicode escape", "---\ntitle: \"caf\\u00e9\"\n---\nx\n", "title", "café"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := value(t, tt.content, tt.key); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// TestEveryNullSpellingIsUnset pins that a null is identified by node type, not
// by matching the literal text "null". The old parser matched only "null", so
// `parent: ~` read as though it were a page id.
func TestEveryNullSpellingIsUnset(t *testing.T) {
	for _, spelling := range []string{"", " ", " null", " Null", " NULL", " ~"} {
		content := "---\nparent:" + spelling + "\npage_id:" + spelling + "\n---\nx\n"
		mf, err := frontmatter.Parse("doc.md", content)
		if err != nil {
			t.Fatalf("Parse(parent:%q) = %v", spelling, err)
		}
		if got := mf.Parent(); got != "" {
			t.Errorf("Parent() for %q = %q, want empty", spelling, got)
		}
		if got := mf.PageID(); got != "" {
			t.Errorf("PageID() for %q = %q, want empty", spelling, got)
		}
	}
}

func TestNoFrontmatterIsWholeBody(t *testing.T) {
	mf, err := frontmatter.Parse("doc.md", "# Heading\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(mf.Frontmatter) != 0 || mf.Body != "# Heading\n" {
		t.Errorf("fm = %v, body = %q", mf.Frontmatter, mf.Body)
	}
}

func TestEmptyBlockIsEmptyMapping(t *testing.T) {
	for _, content := range []string{"---\n\n---\nbody\n", "---\n# just a note\n---\nbody\n"} {
		mf, err := frontmatter.Parse("doc.md", content)
		if err != nil {
			t.Fatalf("Parse(%q) = %v, want no error", content, err)
		}
		if len(mf.Frontmatter) != 0 {
			t.Errorf("Parse(%q) fm = %v, want empty", content, mf.Frontmatter)
		}
	}
}

func TestUnterminatedFrontmatter(t *testing.T) {
	_, err := frontmatter.Parse("doc.md", "---\ntitle: X\nbody with no closing fence\n")
	if err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("err = %v, want ErrUnterminatedFrontmatter", err)
	}
}

// TestRejectedShapes covers everything the flat-scalar contract refuses. Four of
// these were accepted-and-mangled by the hand-rolled parser rather than
// reported; the anchor, alias, tag and literal cases matter because each reports
// its indicator character as its token value, so a whitelist is the only safe
// way to read a scalar.
func TestRejectedShapes(t *testing.T) {
	tests := []struct{ name, content, wantSubstr string }{
		{"colon unquoted", "---\ntitle: a: b\n---\nx\n", "mapping value"},
		{"nested list", "---\ntitle:\n  - a\n  - b\n---\nx\n", "scalar"},
		{"nested map", "---\ntitle:\n  a: b\n---\nx\n", "scalar"},
		{"literal block", "---\ntitle: |\n  lit\n---\nx\n", "scalar"},
		{"anchor", "---\ntitle: &a foo\n---\nx\n", "scalar"},
		{"tag", "---\ntitle: !!str 12\n---\nx\n", "scalar"},
		{"duplicate key", "---\ntitle: A\ntitle: B\n---\nx\n", "already defined"},
		{"tab indent", "---\ntitle: T\n\tpage_id: 9\n---\nx\n", "cannot start any token"},
		{"space before colon", "---\ntitle :v\n---\nx\n", "flat mapping"},
		{"top-level scalar", "---\njust text\n---\nx\n", "flat mapping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := frontmatter.Parse("doc.md", tt.content)
			if err == nil {
				t.Fatalf("Parse(%q) = nil error, want one", tt.content)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("err = %q, want it to mention %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestParseErrorIsOneLineAtTheFilePosition pins both halves of the error
// treatment: goccy's multi-line source art is reduced to a line, and the
// position counts the "---" opener the block text does not include.
func TestParseErrorIsOneLineAtTheFilePosition(t *testing.T) {
	_, err := frontmatter.Parse("doc.md", "---\ntitle: a: b\n---\nx\n")
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("err spans lines:\n%s", err)
	}
	if !strings.Contains(err.Error(), "[2:") {
		t.Errorf("err = %q, want the file line 2, not the block line 1", err)
	}
}

// --- write --------------------------------------------------------------------

func TestWriteQuotesWhatYAMLNeeds(t *testing.T) {
	tests := []struct{ name, key, value, want string }{
		{"safe value bare", "title", "Hello World", "title: Hello World"},
		{"apostrophe stays bare", "title", "Bob's Runbook", "title: Bob's Runbook"},
		{"colon quoted", "title", "a: b", `title: "a: b"`},
		{"hash quoted", "title", "Detect # Verify", `title: "Detect # Verify"`},
		{"leading space quoted", "title", "  x", `title: "  x"`},
		{"boolean-looking quoted", "title", "true", `title: "true"`},
		{"number-looking quoted", "title", "123", `title: "123"`},
		{"null-looking quoted", "title", "null", `title: "null"`},
		{"flow sequence quoted", "title", "[draft] Foo", `title: "[draft] Foo"`},
		{"indicator quoted", "title", "@home", `title: "@home"`},
		{"page_id is an integer", "page_id", "123", "page_id: 123"},
		{"parent null is a null", "parent", "null", "parent: null"},
		{"parent path is a string", "parent", "../index.md", "parent: ../index.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := update(t, "---\nk: v\n---\nbody\n", tt.key, tt.value, "")
			if !strings.Contains(got, tt.want+"\n") {
				t.Errorf("UpdateField wrote:\n%s\nwant a line %q", got, tt.want)
			}
		})
	}
}

// hazards are values that have to survive a write-then-read unchanged. The last
// four are the ones goccy's own default emission gets wrong: it drops a tab and
// writes "? q" as a document it then refuses to parse. They are here because the
// writer verifies its own output and falls back to a double-quoted scalar, not
// because goccy handles them.
//
// .inf and .nan are in that group too, but this test cannot see the difference:
// scalarValue flattens every scalar kind to its token text, so they read back as
// themselves either way. TestInfinityAndNaNAreQuoted is what pins those.
var hazards = []string{
	"Plain Title", "Bob's Runbook", "a: b", "Detect # Verify", "  lead", "trail  ",
	"true", "false", "no", "123", "1.5", "null", "~", "[draft] Foo", "{a}",
	"@home", "*star", "&anchor", "!bang", "|pipe", ">gt", "%pct", "- dash",
	"a,b", "0x1f", "12:30", "2026-09-06", "", "café", `say "hi"`, `back\slash`,
	"a\tb", "? q", ".inf", ".nan",
}

// TestWriteThenReadRoundTrips is the guard behind C2. It is not testing goccy:
// it pins that every write still goes through the serializer, so a later fast
// path that concatenates a frontmatter line by hand fails here instead of
// shipping the bug this package was rewritten to remove.
func TestWriteThenReadRoundTrips(t *testing.T) {
	for _, v := range hazards {
		t.Run(v, func(t *testing.T) {
			got := value(t, update(t, "---\nk: v\n---\nbody\n", "title", v, ""), "title")
			if got != v {
				t.Errorf("round-trip of %q = %q", v, got)
			}
		})
	}
}

// FuzzUpdateFieldRoundTrips is the only thing that can find a value the
// double-quote fallback also fails on. Under plain `go test` it runs the seed
// corpus only, so it costs what a table test costs.
func FuzzUpdateFieldRoundTrips(f *testing.F) {
	for _, v := range hazards {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if !strings.ContainsRune(v, 0) && strings.ToValidUTF8(v, "") != v {
			t.Skip("not valid UTF-8")
		}
		out, err := frontmatter.UpdateField("---\nk: v\n---\nbody\n", "title", v, "")
		if err != nil {
			t.Skipf("UpdateField refused %q: %v", v, err)
		}
		mf, err := frontmatter.Parse("doc.md", out)
		if err != nil {
			t.Fatalf("wrote frontmatter it cannot read back for %q:\n%s\n%v", v, out, err)
		}
		if got := mf.Frontmatter["title"]; got != v {
			t.Fatalf("round-trip of %q = %q\nwrote:\n%s", v, got, out)
		}
	})
}

func TestRenderAndUpdateFieldAgree(t *testing.T) {
	fields := []frontmatter.Field{
		{Key: "title", Value: "a: b"},
		{Key: "space", Value: "ENG"},
		{Key: "parent", Value: "42", Comment: "original.md"},
		{Key: "page_id", Value: "123"},
		{Key: "page_width", Value: "max"},
	}
	built := frontmatter.Render(fields)

	chained := ""
	for _, f := range fields {
		chained = update(t, chained, f.Key, f.Value, f.Comment)
	}
	if built != chained {
		t.Errorf("Render:\n%s\nUpdateField chain:\n%s", built, chained)
	}
}

func TestParentValueRoundTripsWithoutTheComment(t *testing.T) {
	md := update(t, "---\nk: x\n---\nb\n", "parent", "4", "foo.md")
	if !strings.Contains(md, "parent: 4 # foo.md") {
		t.Errorf("wrote:\n%s\nwant an inline comment", md)
	}
	if got := value(t, md, "parent"); got != "4" {
		t.Errorf("parent = %q, want %q", got, "4")
	}
}

// --- surgical edits -------------------------------------------------------------

func TestUpdateFieldKeepsExistingPositions(t *testing.T) {
	in := "---\npage_id: 9\ntitle: T\n---\nbody\n"
	got := update(t, in, "page_id", "10", "")
	want := "---\npage_id: 10\ntitle: T\n---\nbody\n"
	if got != want {
		t.Errorf("UpdateField =\n%q\nwant\n%q", got, want)
	}
}

func TestUpdateFieldInsertsCanonically(t *testing.T) {
	in := "---\ntitle: T\npage_id: 9\n---\nbody\n"
	got := update(t, in, "space", "ENG", "")
	want := "---\ntitle: T\nspace: ENG\npage_id: 9\n---\nbody\n"
	if got != want {
		t.Errorf("UpdateField =\n%q\nwant\n%q", got, want)
	}
}

func TestUpdateFieldCreatesBlockWhenAbsent(t *testing.T) {
	got := update(t, "# Heading\n", "title", "T", "")
	want := "---\ntitle: T\n---\n# Heading\n"
	if got != want {
		t.Errorf("UpdateField =\n%q\nwant\n%q", got, want)
	}
}

func TestUpdateFieldKeepsCommentsAndBlanks(t *testing.T) {
	in := "---\n# a note\ntitle: T\n\npage_id: 9\n---\nbody\n"
	got := update(t, in, "page_id", "10", "")
	for _, want := range []string{"# a note", "title: T", "page_id: 10"} {
		if !strings.Contains(got, want) {
			t.Errorf("UpdateField =\n%q\nwant it to keep %q", got, want)
		}
	}
	if !strings.Contains(got, "\n\n") {
		t.Errorf("UpdateField =\n%q\nwant the blank line kept", got)
	}
}

// TestUpdateFieldKeepsACommentOnlyBlocksNote pins that a block holding nothing
// but a comment does not lose it on the first write. The comment has no key to
// attach to at parse time, so it has to be carried onto the first one inserted.
func TestUpdateFieldKeepsACommentOnlyBlocksNote(t *testing.T) {
	got := update(t, "---\n# keep me\n---\nbody\n", "title", "T", "")
	if !strings.Contains(got, "keep me") {
		t.Errorf("UpdateField =\n%q\nwant the note kept", got)
	}
}

// --- MarkdownFile accessors -----------------------------------------------------

func TestMarkdownFileAccessors(t *testing.T) {
	md, err := frontmatter.Parse("doc.md",
		"---\ntitle: My Page\npage_id: 123\nspace: ENG\nparent: 456\n---\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, got, want string }{
		{"Title", md.Title(), "My Page"},
		{"PageID", md.PageID(), "123"},
		{"Space", md.Space(), "ENG"},
		{"Parent", md.Parent(), "456"},
		{"Body", md.Body, "body\n"},
		{"Filename", md.Filename, "doc.md"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

// TestTitleFieldSeparatesAbsentFromEmpty is what update and check need: an
// absent title means "this file does not manage the title", a present-but-empty
// one is a half-finished edit.
func TestTitleFieldSeparatesAbsentFromEmpty(t *testing.T) {
	tests := []struct {
		name, content string
		wantPresent   bool
	}{
		{"absent", "---\npage_id: 1\n---\nx\n", false},
		{"present and empty", "---\ntitle:\npage_id: 1\n---\nx\n", true},
		{"present and null", "---\ntitle: null\npage_id: 1\n---\nx\n", true},
		{"present with a value", "---\ntitle: T\n---\nx\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mf, err := frontmatter.Parse("doc.md", tt.content)
			if err != nil {
				t.Fatal(err)
			}
			if _, present := mf.TitleField(); present != tt.wantPresent {
				t.Errorf("present = %v, want %v", present, tt.wantPresent)
			}
		})
	}
}

// TestInfinityAndNaNAreQuoted is the regression for a verify-and-retry loop that
// compared only text. scalarValue flattens every scalar kind to its token, so
// ".inf" appeared to round-trip while being written bare -- and `title: .inf` is
// a float to any conforming reader, which is #130 again. The check requires the
// re-parsed node to be a string, not merely to spell the same.
func TestInfinityAndNaNAreQuoted(t *testing.T) {
	for _, v := range []string{".inf", "-.inf", ".nan", ".NaN", ".Inf"} {
		t.Run(v, func(t *testing.T) {
			got := update(t, "---\nk: v\n---\nbody\n", "title", v, "")
			if !strings.Contains(got, `title: "`+v+`"`) {
				t.Errorf("wrote:\n%s\nwant %q quoted", got, v)
			}
		})
	}
}

// TestMultiLineScalarIsRejected pins the flat contract at read time. It has to
// be enforced rather than trusted, because an untouched key is re-emitted from
// the node the parser produced and goccy's re-emission is not identity: a
// continued plain scalar comes back as a "|-" block that Parse then refuses, so
// UpdateField would write a file it cannot read -- and in create, only after the
// page had been made.
func TestMultiLineScalarIsRejected(t *testing.T) {
	tests := []struct{ name, content string }{
		{"plain continuation", "---\ntitle: plain\n  continued\npage_id: 9\n---\nx\n"},
		{"plain across blank", "---\ntitle: plain\n\n  continued\npage_id: 9\n---\nx\n"},
		{"single-quoted", "---\ntitle: 'sq\n  line'\npage_id: 9\n---\nx\n"},
		{"double-quoted", "---\ntitle: \"dq\n  line\"\npage_id: 9\n---\nx\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := frontmatter.Parse("doc.md", tt.content)
			if err == nil {
				t.Fatalf("Parse(%q) = nil error, want one", tt.content)
			}
			if !strings.Contains(err.Error(), "single-line") {
				t.Errorf("err = %q, want it to mention single-line", err)
			}
		})
	}
}

// TestNewlineValueWeWroteStillRoundTrips is the other side of the line: our own
// escaped newline is one physical line, so the single-line rule must not reject
// it.
func TestNewlineValueWeWroteStillRoundTrips(t *testing.T) {
	for _, v := range []string{"a\nb", "a\tb"} {
		got := update(t, "---\nk: v\n---\nbody\n", "title", v, "")
		// The block is exactly two field lines: the escaped newline must not
		// have become a real one.
		block := strings.SplitN(strings.TrimPrefix(got, "---\n"), "\n---\n", 2)[0]
		if lines := strings.Split(block, "\n"); len(lines) != 2 {
			t.Errorf("wrote %d lines, want 2:\n%q", len(lines), got)
		}
		if back := value(t, got, "title"); back != v {
			t.Errorf("round-trip of %q = %q", v, back)
		}
	}
}

// TestSecondDocumentIsRejected covers a "..." line, which starts a new YAML
// document. Reading only the first would drop every key after it in silence,
// and update would report "no page id" about a file that visibly has one.
func TestSecondDocumentIsRejected(t *testing.T) {
	_, err := frontmatter.Parse("doc.md", "---\ntitle: T\n...\npage_id: 9\n---\nx\n")
	if err == nil || !strings.Contains(err.Error(), "single document") {
		t.Errorf("err = %v, want a single-document error", err)
	}
}

func TestRenderLastDuplicateWins(t *testing.T) {
	got := frontmatter.Render([]frontmatter.Field{
		{Key: "title", Value: "first"}, {Key: "title", Value: "second"},
	})
	if got != "---\ntitle: second\n---\n" {
		t.Errorf("Render = %q, want the last value and no duplicate key", got)
	}
	if _, err := frontmatter.Parse("doc.md", got); err != nil {
		t.Errorf("Render emitted something Parse rejects: %v", err)
	}
}

// TestNullPageWidthIsUnsetNotInvalid records a consequence of nulls being unset:
// page_width: null used to reach pagewidth.Declared as the string "null" and be
// rejected as an invalid width. It now means "not set", so the default applies
// and check no longer reports it.
func TestNullPageWidthIsUnset(t *testing.T) {
	for _, spelling := range []string{"", " null", " ~"} {
		mf, err := frontmatter.Parse("doc.md", "---\npage_width:"+spelling+"\n---\nx\n")
		if err != nil {
			t.Fatalf("Parse(page_width:%q) = %v", spelling, err)
		}
		if got := mf.Frontmatter["page_width"]; got != "" {
			t.Errorf("page_width for %q = %q, want empty", spelling, got)
		}
	}
}
