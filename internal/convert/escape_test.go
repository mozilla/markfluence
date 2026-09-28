package convert_test

import (
	"html"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
)

// escapeCase is one text, the escaped form escapeText writes for it, and why.
type escapeCase struct{ text, want, why string }

// inlineEscapes are the inline rules, each row a whole paragraph's text. The
// rows that stay unchanged matter as much as the others: they are what keeps
// exported Markdown readable (_plans/056).
var inlineEscapes = []escapeCase{
	{`*not emphasis*`, `\*not emphasis\*`, "emphasis"},
	{`a * b * c`, `a * b * c`, "a * between spaces cannot open or close"},
	{`2*3*4`, `2\*3\*4`, "intraword * is emphasis"},
	{`snake_case_word`, `snake_case_word`, "intraword _ cannot open or close"},
	{`a _b_ c`, `a \_b\_ c`, "emphasis"},
	{`_x_`, `\_x\_`, "emphasis"},
	{`about ~5 min`, `about ~5 min`, "a tilde after a space cannot close"},
	{`~5 min to ~10 min`, `\~5 min to ~10 min`, "a tilde at the edge is unknown"},
	{`a ~b~ c`, `a ~b\~ c`, "goldmark takes a single tilde as strikethrough"},
	{`a ~~b~~ c`, `a \~\~b\~\~ c`, "strikethrough"},
	{"`x`", "\\`x\\`", "a code span"},
	{`<b>x</b>`, `\<b>x\</b>`, "raw HTML"},
	{`<!-- c -->`, `\<!-- c -->`, "a comment Confluence would strip"},
	{`a < b`, `a < b`, "not a tag"},
	{`a<b`, `a\<b`, "a tag start"},
	{`<3`, `<3`, "not a tag"},
	{`&copy;`, `\&copy;`, "an entity"},
	{`&#169; and &#xA9;`, `\&#169; and \&#xA9;`, "numeric entities"},
	{`AT&T`, `AT&T`, "not an entity"},
	{`[x]`, `\[x]`, "a link wherever a definition exists"},
	{`[x]: y`, `\[x]: y`, "a link reference definition, which vanishes"},
	{`x]`, `x]`, "a lone ] is inert outside link text"},
	{`C:\path`, `C:\path`, "a backslash before a letter is literal"},
	{`a\*b`, `a\\\*b`, "a backslash before punctuation is an escape"},
	{`a\`, `a\\`, "a backslash at the edge could meet punctuation"},
}

func TestEscapeText(t *testing.T) {
	for _, c := range inlineEscapes {
		t.Run(c.text, func(t *testing.T) {
			if got := convert.EscapeTextForTest(c.text, false); got != c.want {
				t.Errorf("escapeText = %q, want %q (%s)", got, c.want, c.why)
			}
			checkPublishesAsText(t, c.want, c.text)
		})
	}
}

// TestEscapeTextInLink: in link text a "]" ends the text, so it is escaped too.
func TestEscapeTextInLink(t *testing.T) {
	if got, want := convert.EscapeTextForTest(`a]b *c*`, true), `a\]b \*c\*`; got != want {
		t.Errorf("escapeText(inLink) = %q, want %q", got, want)
	}
}

// checkPublishesAsText fails unless md publishes as a paragraph holding
// exactly text: every escape must round-trip as the literal character.
func checkPublishesAsText(t *testing.T, md, text string) {
	t.Helper()
	got := strings.TrimSpace(publish(t, md+"\n"))
	inner, ok := strings.CutPrefix(got, "<p>")
	if ok {
		inner, ok = strings.CutSuffix(inner, "</p>")
	}
	if !ok || strings.Contains(inner, "<") || html.UnescapeString(inner) != text {
		t.Errorf("%q publishes as %q, want a paragraph holding %q", md, got, text)
	}
}

// TestMentionMarkdownEscapesTheName: a display name is plain text off the
// server, so it is escaped like any other, and user-find prints this same line.
func TestMentionMarkdownEscapesTheName(t *testing.T) {
	got := convert.MentionMarkdown("Ada *Lovelace* [she/her]", "abc")
	want := `[@Ada \*Lovelace\* \[she/her\]](https://home.atlassian.com/people/abc)`
	if got != want {
		t.Errorf("MentionMarkdown = %q, want %q", got, want)
	}
}
