package convert

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// storageRenderer overrides the default goldmark HTML renderer for the nodes
// whose Confluence storage form differs from plain HTML. Node kinds it does not
// register fall through to the default HTML renderer.
type storageRenderer struct{}

// RegisterFuncs registers the node handlers this renderer overrides.
func (r *storageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindText, r.renderText)
}

// renderText renders inline text, collapsing soft line breaks to a single space
// so soft-wrapped source lines don't become hard line breaks in Confluence. Hard
// line breaks (a trailing backslash or two spaces) become <br />.
func (r *storageRenderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	value := n.Segment.Value(source)
	if n.IsRaw() {
		gmhtml.DefaultWriter.RawWrite(w, value)
		return ast.WalkContinue, nil
	}
	gmhtml.DefaultWriter.Write(w, value)
	switch {
	case n.HardLineBreak():
		_, _ = w.WriteString("<br />\n")
	case n.SoftLineBreak():
		_ = w.WriteByte(' ')
	}
	return ast.WalkContinue, nil
}
