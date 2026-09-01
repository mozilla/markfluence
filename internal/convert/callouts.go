package convert

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// calloutAttr is the node attribute set on a blockquote recognized as a
// GitHub-style callout; its value is the lowercase alert type, which
// calloutTargets turns into the element to emit.
const calloutAttr = "mfCallout"

// calloutTarget is what a GitHub alert publishes as. Exactly one field is set:
// Confluence has a macro for four of the five colours GitHub draws alerts in,
// and purple exists only as an ADF panel, which has no macro and so is written
// as an <ac:adf-extension> (docs/confluence/storage-format.md).
type calloutTarget struct {
	macro     string // ac:name on an ac:structured-macro
	panelType string // panel-type on an ac:adf-extension
}

// calloutTargets maps a GitHub alert to the Confluence construct that renders in
// the same colour GitHub uses: NOTE blue, TIP green, IMPORTANT purple, WARNING
// orange, CAUTION red.
//
// The mapping is bijective -- every alert has its own target and every target
// its own alert -- which is what lets calloutMacroInverse recover the alert
// exactly. It used to be many-to-one, with CAUTION folded into "warning" and
// unrecoverable, because purple was assumed unreachable.
//
// Beware the vocabularies: a macro named "note" is yellow, where an ADF panel
// typed "note" is purple, and a macro named "warning" is red where an ADF panel
// typed "warning" is yellow. Only "info" means the same thing in both.
var calloutTargets = map[string]calloutTarget{
	"note":      {macro: "info"},     // blue
	"tip":       {macro: "tip"},      // green
	"important": {panelType: "note"}, // purple; no macro exists
	"warning":   {macro: "note"},     // orange
	"caution":   {macro: "warning"},  // red
}

// calloutMarkerRE matches a callout marker line such as "[!NOTE]".
var calloutMarkerRE = regexp.MustCompile(`(?i)^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]$`)

// calloutTransformer rewrites GitHub-style alert blockquotes
//
//	> [!NOTE]
//	> body...
//
// into blockquotes tagged with the alert type, with the marker line stripped
// from the leading paragraph. A blockquote renderer turns the tag into the
// callout construct that matches GitHub's own colour for it.
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
		alert, marker, ok := calloutMarker(para, source)
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
		bq.SetAttributeString(calloutAttr, alert)
		return ast.WalkSkipChildren, nil
	})
}

// calloutMarker inspects a blockquote's leading paragraph. If its first line is a
// callout marker, it returns the lowercase alert type and the inline node that
// ends the marker line (the node bearing the line break, or the last node when
// the marker is the paragraph's only line).
func calloutMarker(para *ast.Paragraph, source []byte) (alert string, marker ast.Node, ok bool) {
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
	return strings.ToLower(m[1]), end, true
}
