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
	// A list cannot sit in an aligned column: the pipe cell publishes it inside
	// the column's aligned <p>, where a list is invalid, and the next read loses
	// it.
	for _, tr := range append([]*snode{t.header}, t.rows...) {
		cells, _ := rowCells(tr)
		for col, c := range cells {
			if t.aligns[col] != "" && (findChild(c, "ul") != nil || findChild(c, "ol") != nil) {
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
	// An empty paragraph between two lines is a <br><br> a pipe cell keeps;
	// one at either end of a cell with content in it becomes a <br> at the end
	// of the row, which the next read cannot tell from nothing.
	if !emptyCell(c) {
		var content []*snode
		for _, k := range c.kids {
			if k.name != "" || strings.TrimSpace(k.text) != "" {
				content = append(content, k)
			}
		}
		for _, k := range []*snode{content[0], content[len(content)-1]} {
			if k.name == "p" && emptyCell(k) {
				return false
			}
		}
	}
	for _, k := range c.kids {
		switch {
		case k.name == "":
		case k.name == "p":
			if !allowedAttrs(k, paragraphAttrOK) || onlyBreaks(k) {
				return false
			}
		case k.name == "ul", k.name == "ol":
			// A list in a pipe cell is its raw tags on the row's one line, and
			// a "|" anywhere in them -- text or an href -- ends the cell:
			// unescaped it splits the row and the table stops parsing, and
			// "\|" survives into an attribute as a literal backslash.
			if strings.Contains(serialize(k), "|") {
				return false
			}
		case cellInline[k.name]:
		default:
			return false
		}
	}
	return true
}

// onlyBreaks reports whether a paragraph holds line breaks and nothing else,
// which a pipe cell renders as nothing at all; the raw form keeps it.
func onlyBreaks(p *snode) bool {
	seen := false
	for _, k := range p.kids {
		switch {
		case k.name == "br":
			seen = true
		case k.name != "" || strings.TrimSpace(k.text) != "":
			return false
		}
	}
	return seen
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
// style unmatched, and so the table raw. It is the one definition of a
// declaration read understands: onlyTextAlign validates with it and styleAlign
// reads with it.
var textAlignDeclRE = regexp.MustCompile(`(?i)text-align\s*:\s*(left|start|center|right|end|justify)\s*(;|$)`)

// onlyTextAlign reports whether a style attribute holds no declaration but a
// single text-align, the one a pipe table carries (as its delimiter row). Two
// are refused rather than resolved: CSS takes the last, and a style that says
// it twice was not written by an editor.
func onlyTextAlign(style string) bool {
	return len(textAlignDeclRE.FindAllString(style, -1)) <= 1 &&
		strings.Trim(textAlignDeclRE.ReplaceAllString(style, ""), " \t\n;") == ""
}

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
		var a string
		switch {
		case k.name == "p" && emptyCell(k):
			continue // nothing to align, as an empty cell does not vote in its column
		case k.name == "p":
			// A paragraph's own declaration wins over the cell's, as in CSS,
			// so a left paragraph in a centred cell is left.
			var declared bool
			if a, declared = styleAlign(k.attrs["style"]); !declared {
				a = cell
			}
		case k.name == "" && strings.TrimSpace(k.text) == "":
			continue
		case k.name == "" || cellInline[k.name]:
			// Loose inline content is a line with no paragraph to align it,
			// so it has the cell's alignment.
			a = cell
		default:
			continue
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
	m := textAlignDeclRE.FindStringSubmatch(style)
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

// hoistCellAlign returns a raw cell with its paragraphs' shared alignment moved
// onto the cell, so the paragraphs can stay Markdown: a Markdown paragraph has
// no alignment, and one written as storage keeps its images and links as
// storage too -- an image that is never uploaded when the file is published to
// a new page. A text-align on the cell is a form Confluence honours
// (storage-format.md, verified 2026-08-07), and the next read keeps it, since a
// raw cell's attributes are written as they are.
//
// Only when every piece of content is a paragraph declaring the same alignment
// and nothing else in its style, and the cell's own style is at most a
// text-align: loose text or a list would otherwise gain an alignment it did not
// have. Otherwise the cell is returned unchanged and rawCellBlocks writes an
// aligned paragraph as storage.
func hoistCellAlign(c *snode) *snode {
	if !onlyTextAlign(c.attrs["style"]) {
		return c
	}
	var decl string
	for _, k := range c.kids {
		switch {
		case k.name == "" && strings.TrimSpace(k.text) == "":
		case k.name == "p" && emptyCell(k):
		case k.name == "p" && allowedAttrs(k, paragraphAttrOK):
			m := textAlignDeclRE.FindString(k.attrs["style"])
			if m == "" || (decl != "" && !strings.EqualFold(normalizeDecl(m), decl)) {
				return c
			}
			decl = normalizeDecl(m)
		default:
			return c
		}
	}
	if decl == "" {
		return c
	}
	out := &snode{name: c.name, attrs: map[string]string{}, kids: make([]*snode, len(c.kids))}
	for k, v := range c.attrs {
		out.attrs[k] = v
	}
	out.attrs["style"] = decl
	for i, k := range c.kids {
		out.kids[i] = k
		if k.name == "p" && k.attrs["style"] != "" {
			p := &snode{name: "p", attrs: map[string]string{}, kids: k.kids}
			for a, v := range k.attrs {
				if a != "style" {
					p.attrs[a] = v
				}
			}
			out.kids[i] = p
		}
	}
	return out
}

// normalizeDecl spells a matched text-align declaration the way markfluence
// writes one.
func normalizeDecl(m string) string {
	return "text-align: " + strings.ToLower(textAlignDeclRE.FindStringSubmatch(m)[1]) + ";"
}

// rawCellBlocks renders a raw table cell's body as Markdown blocks. What
// differs from blockStrings is each because the raw form must lose nothing:
//
//   - text and inline elements sitting directly in the cell, outside any
//     paragraph, are one paragraph, not a block apiece;
//   - an empty paragraph -- a deliberate blank line, Enter twice in the editor
//     -- stays as <p />, unless the cell holds nothing else;
//   - a paragraph that renders to nothing although it holds something (a
//     <br />, a date read has no Markdown for) stays storage;
//   - a paragraph carrying an attribute Markdown cannot hold, in practice an
//     alignment hoistCellAlign could not move to the cell, stays storage on
//     its own line, which costs that paragraph's editability rather than its
//     alignment;
//   - a table stays raw. A nested table that fits GFM would otherwise be
//     republished with markfluence's layout and a <thead> it did not have.
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
			blocks = append(blocks, serialize(k))
		case k.name == "p":
			flush()
			if s := r.blockStrings([]*snode{k}, ""); len(s) > 0 && strings.TrimSpace(strings.Join(s, "")) != "" {
				blocks = append(blocks, s...)
			} else {
				blocks = append(blocks, serialize(k))
			}
		case k.name == "table":
			flush()
			blocks = append(blocks, r.renderRawBlock(k))
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
