package convert

import (
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

// tableLayout is the Confluence table layout every table is published with.
// Without a layout attribute Confluence auto-sizes the table but leaves it
// unanchored; "align-start" auto-sizes it to its content and left-aligns it on the
// page, which is what a markdown table should look like. The other values Confluence
// accepts are "center", "wide", and "full-width".
//
// Note that a colwidth <colgroup> on a table with no layout attribute makes
// Confluence default the layout to "full-width", so this attribute must stay if
// column widths are ever emitted.
const tableLayout = "align-start"

// renderTable emits the <table> tag with the Confluence layout attribute. Only the
// table element itself is overridden; the GFM renderer still emits the thead/tbody,
// rows, and cells.
func (r *storageRenderer) renderTable(
	w util.BufWriter, _ []byte, _ ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString(`<table data-layout="` + tableLayout + "\">\n")
	} else {
		_, _ = w.WriteString("</table>\n")
	}
	return ast.WalkContinue, nil
}

// tableKind is the GFM table node kind, aliased so renderer.go's registration list
// does not need the extension AST import.
var tableKind = east.KindTable
