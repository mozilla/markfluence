package convert

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// calloutAttr is the node attribute set on a blockquote recognized as a
// GitHub-style callout; its value is the Confluence macro name to emit.
const calloutAttr = "mfCallout"

// calloutMacro maps a GitHub alert type to its Confluence macro. Confluence has
// no separate "caution", so it reuses "warning".
var calloutMacro = map[string]string{
	"note":      "info",
	"tip":       "tip",
	"important": "note",
	"warning":   "warning",
	"caution":   "warning",
}

// calloutMarkerRE matches a callout marker line such as "[!NOTE]".
var calloutMarkerRE = regexp.MustCompile(`(?i)^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]$`)

// calloutTransformer rewrites GitHub-style alert blockquotes
//
//	> [!NOTE]
//	> body...
//
// into blockquotes tagged with the target macro, with the marker line stripped
// from the leading paragraph. A blockquote renderer turns the tag into an
// info/tip/note/warning macro.
type calloutTransformer struct{}

func (calloutTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		bq, ok := n.(*ast.Blockquote)
		if !ok {
			return ast.WalkContinue, nil
		}
		para, ok := bq.FirstChild().(*ast.Paragraph)
		if !ok {
			return ast.WalkContinue, nil
		}
		macro, marker, ok := calloutMarker(para, source)
		if !ok {
			return ast.WalkContinue, nil
		}
		// Strip the marker line (leading inline nodes up to and including the
		// node that carries the line break) from the paragraph.
		var strip []ast.Node
		for c := para.FirstChild(); c != nil; c = c.NextSibling() {
			strip = append(strip, c)
			if c == marker {
				break
			}
		}
		for _, c := range strip {
			para.RemoveChild(para, c)
		}
		if para.FirstChild() == nil {
			bq.RemoveChild(bq, para)
		}
		bq.SetAttributeString(calloutAttr, macro)
		return ast.WalkSkipChildren, nil
	})
}

// calloutMarker inspects a blockquote's leading paragraph. If its first line is a
// callout marker, it returns the target macro and the inline node that ends the
// marker line (the node bearing the line break, or the last node when the marker
// is the paragraph's only line).
func calloutMarker(para *ast.Paragraph, source []byte) (macro string, marker ast.Node, ok bool) {
	var line strings.Builder
	var end ast.Node
	for c := para.FirstChild(); c != nil; c = c.NextSibling() {
		t, isText := c.(*ast.Text)
		if !isText {
			return "", nil, false // non-text on the first line: not a plain marker
		}
		line.Write(t.Segment.Value(source))
		end = c
		if t.SoftLineBreak() || t.HardLineBreak() {
			break
		}
	}
	m := calloutMarkerRE.FindStringSubmatch(strings.TrimSpace(line.String()))
	if m == nil {
		return "", nil, false
	}
	return calloutMacro[strings.ToLower(m[1])], end, true
}
