package convert

import (
	"regexp"
	"strings"
)

// A table reads back as a GFM pipe table only when Markdown expresses all of
// it, and as raw storage otherwise (#55, _plans/055). The pipe table used to be
// unconditional, so a page's layout, column widths, merged cells or a
// headerless shape were dropped on read, and the next publish stripped them
// from the page. A table that is nearly expressible is raw too: degrading one
// cell is still loss, and the raw form is exact.
//
// What counts is an allowlist, so an attribute nobody has seen yet keeps the
// table raw rather than being dropped. A few attributes are ignored because the
// editor writes them on tables nobody configured (page 2913502220, measured
// 2026-09-25): honouring them would send every table the editor has saved back
// as HTML. docs/confluence/storage-format.md has what each attribute does.

// renderTable renders a table as a GFM pipe table when tableShape says Markdown
// can hold it, and as raw storage with Markdown cell bodies otherwise.
func (r *mdRenderer) renderTable(n *snode) string {
	shape, ok := tableShape(n)
	if !ok {
		return r.renderRawBlock(n)
	}
	if shape.header == nil {
		return ""
	}
	head := r.cellTexts(shape.header)
	seps := make([]string, len(shape.aligns))
	for i, a := range shape.aligns {
		seps[i] = alignSeparators[a]
	}
	var b strings.Builder
	b.WriteString("| " + strings.Join(head, " | ") + " |\n")
	b.WriteString("| " + strings.Join(seps, " | ") + " |")
	for _, row := range shape.rows {
		b.WriteString("\n| " + strings.Join(r.cellTexts(row), " | ") + " |")
	}
	return b.String()
}

// alignSeparators are the GFM delimiter cells, keyed by the alignment recovered
// from storage. There is no left: Confluence cannot state one, so a left column
// and an unaligned one are the same column (storage-format.md).
var alignSeparators = map[string]string{
	"":       "---",
	"center": ":---:",
	"right":  "---:",
}

// pipeTable is a table Markdown can hold: one header row of <th>, body rows of
// <td>, every row as wide as the header, and one alignment per column.
type pipeTable struct {
	header *snode
	rows   []*snode
	aligns []string
}

// tableShape reports whether a pipe table expresses n exactly, and its parts
// when it does. A table with no rows at all is expressible as nothing, which
// is what the pipe renderer has always written for one.
func tableShape(n *snode) (pipeTable, bool) {
	if !allowedAttrs(n, tableAttrOK) {
		return pipeTable{}, false
	}
	var trs []*snode
	for _, k := range n.kids {
		switch k.name {
		case "":
			if strings.TrimSpace(k.text) != "" {
				return pipeTable{}, false
			}
		case "thead", "tbody":
			if !allowedAttrs(k, idOnly) {
				return pipeTable{}, false
			}
			for _, tr := range k.kids {
				switch {
				case tr.name == "tr":
					trs = append(trs, tr)
				case tr.name != "" || strings.TrimSpace(tr.text) != "":
					return pipeTable{}, false
				}
			}
		case "tr":
			trs = append(trs, k)
		case "colgroup":
			if n.attrs["data-layout"] != "align-start" || !pixelColgroup(k) {
				return pipeTable{}, false
			}
		default:
			// A <tfoot> is a row Confluence renders as an ordinary one but
			// that a pipe table would republish without the tag; anything
			// else is unknown.
			return pipeTable{}, false
		}
	}
	if len(trs) == 0 {
		return pipeTable{}, true
	}

	var t pipeTable
	var voted []bool
	for i, tr := range trs {
		cells, ok := rowCells(tr)
		if !ok || len(cells) == 0 {
			return pipeTable{}, false
		}
		// GFM has no table without a header row, and no header anywhere but
		// the top: the first row must be all <th> and no other row may hold
		// one, or the next publish turns <td>s into <th>s (or back).
		want := "td"
		if i == 0 {
			want = "th"
			t.header = tr
			t.aligns = make([]string, len(cells))
			voted = make([]bool, len(cells))
		} else {
			t.rows = append(t.rows, tr)
		}
		if len(cells) != len(t.aligns) {
			return pipeTable{}, false
		}
		for col, c := range cells {
			if c.name != want || !cellExpressible(c) {
				return pipeTable{}, false
			}
			a, ok := cellAlign(c)
			if !ok {
				return pipeTable{}, false
			}
			// GFM aligns a column, Confluence a paragraph, so a column whose
			// cells disagree cannot be written without republishing some of
			// them with an alignment they did not have. An empty cell has
			// nothing to align, so it does not vote: republished, it gains an
			// empty aligned paragraph, which reads back the same.
			switch {
			case emptyCell(c):
			case !voted[col]:
				t.aligns[col], voted[col] = a, true
			case a != t.aligns[col]:
				return pipeTable{}, false
			}
		}
	}
	return t, true
}

// rowCells returns a row's cells, or false when the row carries an attribute
// or a child a pipe table cannot hold.
func rowCells(tr *snode) ([]*snode, bool) {
	if !allowedAttrs(tr, idOnly) {
		return nil, false
	}
	var cells []*snode
	for _, c := range tr.kids {
		switch c.name {
		case "th", "td":
			cells = append(cells, c)
		case "":
			if strings.TrimSpace(c.text) != "" {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return cells, true
}

// pixelColgroup reports whether a <colgroup> holds nothing but pixel column
// widths, which is what the browser editor writes on an align-start table
// whenever it saves one -- the widths it measured, summing to the
// data-table-width it writes beside them (page 3109814418, 2026-09-25, a
// markfluence table saved without touching the table). A resize writes the
// same shape, so ignoring it loses a resized column's width on the next
// publish; nothing tells the two apart but a sum Atlassian does not document.
// On any other layout a <colgroup> keeps the table raw: a table in the
// default or full-width layout is not one markfluence wrote.
func pixelColgroup(g *snode) bool {
	if !allowedAttrs(g, idOnly) {
		return false
	}
	for _, c := range g.kids {
		switch {
		case c.name == "col":
			if len(c.kids) > 0 || !allowedAttrs(c, pixelWidth) {
				return false
			}
		case c.name != "" || strings.TrimSpace(c.text) != "":
			return false
		}
	}
	return true
}

// pixelWidthRE matches the style the editor writes on a <col>.
var pixelWidthRE = regexp.MustCompile(`^\s*width:\s*[0-9]+(\.[0-9]+)?px;?\s*$`)

// pixelWidth accepts a <col>'s id and a pixel width.
func pixelWidth(name, value string) bool {
	return isID(name) || (name == "style" && pixelWidthRE.MatchString(value))
}

// emptyCell reports whether a cell holds no content: no text, and no element
// but empty paragraphs.
func emptyCell(c *snode) bool {
	for _, k := range c.kids {
		switch k.name {
		case "":
			if strings.TrimSpace(k.text) != "" {
				return false
			}
		case "p":
			if !emptyCell(k) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// cellExpressible reports whether a cell's attributes and children fit in a
// pipe table cell: a colour, an alignment, and content that is paragraphs,
// lists or inline markup. A heading, a code block or any other block macro, a
// nested table and the rest need more than one physical line.
func cellExpressible(c *snode) bool {
	if !allowedAttrs(c, cellAttrOK) {
		return false
	}
	for _, k := range c.kids {
		switch {
		case k.name == "":
		case k.name == "p":
			if !allowedAttrs(k, paragraphAttrOK) {
				return false
			}
		case k.name == "ul", k.name == "ol", cellInline[k.name]:
		default:
			return false
		}
	}
	return true
}

// cellInline are the elements that may sit directly in a cell, outside any
// paragraph, and still render as the cell's inline text: the ones renderInline
// has a Markdown form for. A macro is not among them: directly in a cell it is
// usually a block one.
var cellInline = map[string]bool{
	"ac:image": true, "ac:link": true, "a": true, "strong": true, "b": true, "em": true,
	"i": true, "code": true, "del": true, "s": true, "strike": true, "br": true,
}

// allowedAttrs reports whether ok accepts every attribute of n.
func allowedAttrs(n *snode, ok func(name, value string) bool) bool {
	for k, v := range n.attrs {
		if !ok(k, v) {
			return false
		}
	}
	return true
}

// isID reports whether an attribute is a server-generated id. The editor writes
// ac:local-id on tables, rows and cells, and a bare local-id on every
// paragraph (page 3109814418); Confluence regenerates both on publish.
func isID(name string) bool { return name == "ac:local-id" || name == "local-id" }

// idOnly accepts only a server-generated id.
func idOnly(name, _ string) bool { return isID(name) }

// tableAttrOK accepts what the editor writes on a table nobody configured.
// data-table-width is ignored whatever its value: the editor writes one on
// every table it saves, and a hand resize is lost with it on the next publish.
// Any other layout or display mode is a choice somebody made.
func tableAttrOK(name, value string) bool {
	switch {
	case isID(name), name == "data-table-width":
		return true
	case name == "data-layout":
		return value == "align-start"
	case name == "data-table-display-mode":
		return value == "default"
	}
	return false
}

// cellAttrOK accepts a cell's id, its colour (a bg: marker), a span of one,
// and a style that says nothing but its alignment.
func cellAttrOK(name, value string) bool {
	switch {
	case isID(name), name == "data-highlight-colour":
		return true
	case name == "rowspan", name == "colspan":
		return strings.TrimSpace(value) == "1"
	case name == "style":
		return onlyTextAlign(value)
	}
	return false
}

// paragraphAttrOK accepts a cell paragraph's id and a style that says nothing
// but its alignment.
func paragraphAttrOK(name, value string) bool {
	switch {
	case isID(name):
		return true
	case name == "style":
		return onlyTextAlign(value)
	}
	return false
}

// textAlignDeclRE matches one text-align declaration in a style attribute,
// with a value the delimiter row can carry or that has no effect in Confluence
// (justify is stored and ignored, storage-format.md). Any other value keeps the
// style unmatched, and so the table raw.
var textAlignDeclRE = regexp.MustCompile(`(?i)text-align\s*:\s*(left|start|center|right|end|justify)\s*(;|$)`)

// onlyTextAlign reports whether a style attribute holds no declaration but
// text-align, the one a pipe table carries (as its delimiter row).
func onlyTextAlign(style string) bool {
	return strings.Trim(textAlignDeclRE.ReplaceAllString(style, ""), " \t\n;") == ""
}

// textAlignRE pulls the value out of a text-align declaration anywhere in a style
// attribute.
var textAlignRE = regexp.MustCompile(`(?i)text-align\s*:\s*([a-z]+)`)

// cellAlign reports the one alignment a cell's content carries, normalized to
// the delimiter row's vocabulary, or false when its paragraphs disagree -- a
// cell with one centred line and one plain one has no column alignment that
// republishes it unchanged.
//
// Confluence's own form is a text-align on each paragraph, which is what
// markfluence writes; a text-align on the cell works too and covers every
// paragraph in it. "start"/"end" are ADF's names for left/right and turn up in
// hand-edited storage, and left is no alignment at all, since Confluence has
// no explicit left (storage-format.md).
func cellAlign(c *snode) (string, bool) {
	cell, _ := styleAlign(c.attrs["style"])
	found, seen := cell, false
	for _, k := range c.kids {
		if k.name != "p" {
			continue
		}
		// A paragraph's own declaration wins over the cell's, as in CSS, so a
		// left paragraph in a centred cell is left.
		a, declared := styleAlign(k.attrs["style"])
		if !declared {
			a = cell
		}
		if seen && a != found {
			return "", false
		}
		found, seen = a, true
	}
	return found, true
}

// styleAlign reads a text-align declaration from a style attribute, and
// whether there was one: left, start and justify are declarations of no
// alignment.
func styleAlign(style string) (string, bool) {
	m := textAlignRE.FindStringSubmatch(style)
	if m == nil {
		return "", false
	}
	switch strings.ToLower(m[1]) {
	case "center":
		return "center", true
	case "right", "end":
		return "right", true
	}
	return "", true
}

// rawCellBlocks renders a raw table cell's body as Markdown blocks. Three
// things differ from blockStrings, each because the raw form must lose
// nothing:
//
//   - text and inline elements sitting directly in the cell, outside any
//     paragraph, are one paragraph, not a block apiece;
//   - an empty paragraph -- a deliberate blank line, Enter twice in the editor
//     -- stays as <p />, unless the cell holds nothing else;
//   - a paragraph carrying an attribute Markdown cannot hold, in practice an
//     alignment, stays storage on its own line, which costs that paragraph's
//     editability rather than its alignment.
func (r *mdRenderer) rawCellBlocks(c *snode) []string {
	if emptyCell(c) {
		return nil
	}
	var run []*snode
	var blocks []string
	flush := func() {
		if s := strings.TrimSpace(r.renderInlineChildren(&snode{kids: run})); s != "" {
			blocks = append(blocks, s)
		}
		run = nil
	}
	for _, k := range c.kids {
		switch {
		case k.name == "" || cellInline[k.name] || rawCellInline[k.name]:
			run = append(run, k)
			continue
		case k.name == "p" && emptyCell(k):
			flush()
			blocks = append(blocks, "<p />")
		case k.name == "p" && !allowedAttrs(k, idOnly):
			flush()
			blocks = append(blocks, serialize(withoutIDs(k)))
		default:
			flush()
			blocks = append(blocks, r.blockStrings([]*snode{k}, "")...)
		}
	}
	flush()
	return blocks
}

// rawCellInline are inline elements beyond cellInline that group into a raw
// cell's loose paragraph rather than becoming a block of their own. read has no
// Markdown for them and renders their text (or nothing), the same as in any
// paragraph; that is an older gap than this one.
var rawCellInline = map[string]bool{
	"span": true, "u": true, "sub": true, "sup": true, "time": true, "ac:emoticon": true,
}

// withoutIDs is n with the server-generated ids removed from it and everything
// under it. attrString drops ac:local-id already; the bare local-id the editor
// writes on a paragraph would otherwise be copied into the Markdown.
func withoutIDs(n *snode) *snode {
	c := &snode{name: n.name, text: n.text}
	if n.attrs != nil {
		c.attrs = map[string]string{}
		for k, v := range n.attrs {
			if !isID(k) {
				c.attrs[k] = v
			}
		}
	}
	for _, k := range n.kids {
		c.kids = append(c.kids, withoutIDs(k))
	}
	return c
}

// cellTexts renders a row's cells to inline strings with pipes escaped,
// prefixed with a bg: marker for a cell carrying a background color.
func (r *mdRenderer) cellTexts(tr *snode) []string {
	var cells []string
	for _, c := range tr.kids {
		if c.name == "th" || c.name == "td" {
			text := r.renderCellLines(c)
			if marker := cellBGMarkerComment(c); marker != "" {
				if text == "" {
					text = marker
				} else {
					text = marker + " " + text
				}
			}
			cells = append(cells, text)
		}
	}
	return cells
}
