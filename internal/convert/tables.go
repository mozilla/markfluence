package convert

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// tableLayout is the Confluence table layout every table is published with.
// Without a layout attribute Confluence auto-sizes the table but leaves it
// unanchored; "align-start" auto-sizes it to its content and left-aligns it on the
// page, which is what a markdown table should look like. The other values
// Confluence accepts are "align-end", "center", "wide", and "full-width".
//
// Keep this attribute if column widths are ever emitted: a <colgroup> on a table
// with no layout makes Confluence pick one from the total column width, so the
// table silently acquires a layout nobody asked for. Which one it picks, and the
// rest of the table vocabulary, is in docs/confluence/storage-format.md.
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

// tableKind and tableCellKind are the GFM table node kinds, aliased so
// renderer.go's registration list does not need the extension AST import.
var (
	tableKind     = east.KindTable
	tableCellKind = east.KindTableCell
)

// cellBGAttr is the node attribute set on a table cell carrying a background
// color; its value is the resolved hex for data-highlight-colour.
const cellBGAttr = "mfCellBG"

// cellBGSwatches maps a color name to its hex. These are the 21 swatches the
// Confluence editor's cell background picker offers, so a color set from markdown
// is indistinguishable from one set by hand in the editor (and shows up as the
// selected swatch there). Read off an editor-authored page on 2026-08-04; the
// picker is a grid of seven hue columns by three shades, where the grey column
// runs white / light grey / grey.
//
// Confluence accepts any hex in data-highlight-colour, so this map is markfluence's
// vocabulary rather than a limit imposed by the server; #rrggbb also works
// directly for a color outside the palette.
var cellBGSwatches = map[string]string{
	"white":        "#ffffff",
	"light-grey":   "#f4f5f7",
	"light-gray":   "#f4f5f7",
	"grey":         "#b3bac5",
	"gray":         "#b3bac5",
	"light-blue":   "#deebff",
	"blue":         "#b3d4ff",
	"bold-blue":    "#4c9aff",
	"light-teal":   "#e6fcff",
	"teal":         "#b3f5ff",
	"bold-teal":    "#79e2f2",
	"light-green":  "#e3fcef",
	"green":        "#abf5d1",
	"bold-green":   "#57d9a3",
	"light-yellow": "#fffae6",
	"yellow":       "#fff0b3",
	"bold-yellow":  "#ffc400",
	"light-red":    "#ffebe6",
	"red":          "#ffbdad",
	"bold-red":     "#ff8f73",
	"light-purple": "#eae6ff",
	"purple":       "#c0b6f2",
	"bold-purple":  "#998dd9",
}

var (
	// cellBGMarkerRE matches a cell background marker comment, "<!-- bg:green -->".
	cellBGMarkerRE = regexp.MustCompile(`(?i)^<!--\s*bg:\s*(\S+)\s*-->$`)
	// cellBGHexRE matches a literal color, "#ffebe6".
	cellBGHexRE = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

// tableCellBGTransformer implements the cell background color marker: an HTML
// comment at the start of a table cell,
//
//	| auth | <!-- bg:light-red --> down |
//
// which is invisible in a plain markdown preview. The marker is stripped from the
// cell and the resolved color is stashed on the cell node for renderTableCell to
// emit as data-highlight-colour. An unresolvable color is dropped with a warning.
type tableCellBGTransformer struct{ r *storageRenderer }

func (t tableCellBGTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != tableCellKind {
			return ast.WalkContinue, nil
		}
		t.applyCellBG(n, source)
		return ast.WalkSkipChildren, nil
	})
}

// applyCellBG consumes a leading marker on one cell. A marker anywhere else in
// the cell is left alone and warned about: it would otherwise be a silent no-op,
// since a comment is invisible in both markdown and Confluence.
func (t tableCellBGTransformer) applyCellBG(cell ast.Node, source []byte) {
	if first := cell.FirstChild(); first != nil {
		if value, ok := cellBGMarker(first, source); ok {
			cell.RemoveChild(cell, first)
			trimLeadingSpaces(cell.FirstChild(), source)
			if hex, ok := resolveCellBG(value); ok {
				cell.SetAttributeString(cellBGAttr, hex)
			} else {
				t.warn(cell, source, value, "unknown color: use a swatch name or a #rrggbb hex")
			}
		}
	}
	for c := cell.FirstChild(); c != nil; c = c.NextSibling() {
		if value, ok := cellBGMarker(c, source); ok {
			t.warn(cell, source, value, "a bg marker must come first in the cell")
		}
	}
}

// warn records an ignored marker, quoting the cell's text so the author can find
// it in a page full of tables.
func (t tableCellBGTransformer) warn(cell ast.Node, source []byte, value, problem string) {
	label := strings.TrimSpace(nodeText(cell, source))
	if runes := []rune(label); len(runes) > 40 {
		label = string(runes[:40]) + "..."
	}
	t.r.warnings = append(t.r.warnings,
		fmt.Sprintf("table cell %q: ignoring bg:%s (%s)", label, value, problem))
}

// cellBGMarker reports whether an inline node is a background marker comment,
// returning the color it names.
func cellBGMarker(n ast.Node, source []byte) (string, bool) {
	raw, ok := n.(*ast.RawHTML)
	if !ok {
		return "", false
	}
	var b strings.Builder
	for i := 0; i < raw.Segments.Len(); i++ {
		seg := raw.Segments.At(i)
		b.Write(seg.Value(source))
	}
	m := cellBGMarkerRE.FindStringSubmatch(strings.TrimSpace(b.String()))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// resolveCellBG turns a marker's color into a hex, accepting a swatch name or a
// literal #rrggbb.
func resolveCellBG(value string) (string, bool) {
	v := strings.ToLower(value)
	if hex, ok := cellBGSwatches[v]; ok {
		return hex, true
	}
	if cellBGHexRE.MatchString(v) {
		return v, true
	}
	return "", false
}

// trimLeadingSpaces drops the whitespace a consumed marker leaves behind at the
// start of a cell ("<!-- bg:red --> down" renders as "down", not " down").
func trimLeadingSpaces(n ast.Node, source []byte) {
	txt, ok := n.(*ast.Text)
	if !ok {
		return
	}
	seg := txt.Segment
	for seg.Start < seg.Stop && (source[seg.Start] == ' ' || source[seg.Start] == '\t') {
		seg.Start++
	}
	txt.Segment = seg
}

// renderTableCell emits a <th>/<td>, adding data-highlight-colour for a cell with
// a background color marker. It otherwise reproduces what the GFM renderer emits,
// including the align attribute Confluence discards (see issue #48).
func (r *storageRenderer) renderTableCell(
	w util.BufWriter, _ []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	n := node.(*east.TableCell)
	tag := "td"
	if n.Parent().Kind() == east.KindTableHeader {
		tag = "th"
	}
	if !entering {
		_, _ = w.WriteString("</" + tag + ">\n")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<" + tag)
	if n.Alignment != east.AlignNone {
		_, _ = w.WriteString(` align="` + n.Alignment.String() + `"`)
	}
	if v, ok := n.AttributeString(cellBGAttr); ok {
		if hex, ok := v.(string); ok {
			_, _ = w.WriteString(` data-highlight-colour="` + hex + `"`)
		}
	}
	_ = w.WriteByte('>')
	return ast.WalkContinue, nil
}
