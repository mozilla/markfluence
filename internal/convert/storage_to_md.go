package convert

// storage_to_md.go is the best-effort inverse of MdToConfluence: it turns a
// Confluence storage-format body (XHTML) back into GitHub-Flavored Markdown.
//
// Constructs MdToConfluence emits round-trip faithfully; editor-authored content
// markfluence never emits degrades gracefully -- macro bodies are rendered, and
// unknown leaf macros pass through as raw storage (which MdToConfluence's ac:/ri:
// shield re-publishes verbatim). Parsing uses encoding/xml with the built-in HTML
// entity table. Storage format is well-formed XHTML, so the input is never the
// reason a parse fails -- but the decoder's configuration can be, which is what
// autoCloseElems exists to say.
//
// <ac:link> is the one storage element with enough shape to need its own file:
// aclink.go.

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// calloutMacroInverse maps a Confluence callout macro back to a GitHub alert type.
// The forward map (calloutMacro) is many-to-one, so this is the canonical inverse:
// CAUTION is unrecoverable (it folded into "warning"), and "note" came from
// IMPORTANT while "info" came from NOTE.
var calloutMacroInverse = map[string]string{
	"info":    "NOTE",
	"tip":     "TIP",
	"note":    "IMPORTANT",
	"warning": "WARNING",
}

// StorageToMarkdown converts a Confluence storage-format body to Markdown.
//
// opts carries the things this package cannot fetch for itself -- attachment
// source paths and resolved <ac:link> page URLs -- because internal/convert
// holds no client. All of it is optional; see StorageOptions.
func StorageToMarkdown(storage string, opts StorageOptions) (string, error) {
	root, err := parseStorage(storage)
	if err != nil {
		return "", err
	}
	r := &mdRenderer{
		sources:      opts.Sources,
		pageLinks:    opts.PageLinks,
		siteURL:      strings.TrimSuffix(opts.SiteURL, "/"),
		headingSlugs: headingSlugs(root),
	}
	blocks := r.blockStrings(root.kids, "")
	out := strings.Join(blocks, "\n\n")
	out = strings.Trim(out, "\n")
	if out == "" {
		return "", nil
	}
	return out + "\n", nil
}

// mdRenderer carries the per-conversion context the storage->markdown walk needs.
// A fresh one is used per conversion, so nothing leaks between documents.
type mdRenderer struct {
	// sources maps attachment name -> the image path it was published from. May
	// be nil, in which case paths are recovered by decoding attachment names.
	sources map[string]string

	// pageLinks maps an <ac:link> page target -> its absolute URL, resolved by
	// the caller. A target missing from it passes through as raw storage.
	pageLinks map[PageLinkTarget]string

	// siteURL is the Confluence site base, for a space link. Never the gateway.
	siteURL string

	// headingSlugs maps this document's own Confluence heading anchors to their
	// GitHub equivalents, which is how a same-page anchor link is recovered.
	headingSlugs map[string]string
}

// sourceFor resolves an attachment name back to the markdown image path to write.
// The path recorded on the attachment wins because it is exact; otherwise the
// name is decoded. An absolute path is never something markfluence published, so
// it is refused in both cases and the raw attachment name is used instead.
func (r *mdRenderer) sourceFor(filename string) string {
	if src, ok := r.sources[filename]; ok && src != "" && !path.IsAbs(src) {
		return src
	}
	if src, ok := AttachmentSource(filename); ok {
		return src
	}
	return filename
}

// snode is a minimal parsed storage node: an element (name + attrs + children) or
// a text node (name == "").
type snode struct {
	name  string
	attrs map[string]string
	text  string
	kids  []*snode
}

// autoCloseElems is xml.HTMLAutoClose without "link", which cannot be left in.
//
// The decoder matches an auto-close name against Name.Local alone and ignores
// the namespace prefix, so the HTML void element "link" also matches Confluence's
// <ac:link>. The element is then closed the instant it opens, its children become
// siblings, and the real </ac:link> arrives against an empty stack -- which is a
// hard error even with Strict off, since non-strict mode invents *missing* end
// tags but cannot absorb a surplus one. Dropping the entry costs nothing: <link>
// is a <head> element and never appears in a storage body.
var autoCloseElems = withoutElem(xml.HTMLAutoClose, "link")

// withoutElem returns names with one entry removed.
func withoutElem(names []string, drop string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != drop {
			out = append(out, n)
		}
	}
	return out
}

// parseStorage parses a storage fragment into a tree rooted at a nameless node.
func parseStorage(storage string) (*snode, error) {
	dec := xml.NewDecoder(strings.NewReader(storage))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity    // resolve &nbsp; and friends
	dec.AutoClose = autoCloseElems // treat <br>, <hr>, <img>, ... as void

	root := &snode{}
	stack := []*snode{root}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			n := &snode{name: qname(t.Name), attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[qname(a.Name)] = a.Value
			}
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			top.kids = append(top.kids, &snode{text: string(t)})
		}
	}
	return root, nil
}

// qname renders an xml.Name as "prefix:local" (storage never binds the ac:/ri:
// prefixes, so the decoder leaves the prefix in Space).
func qname(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

// --- block rendering ---------------------------------------------------------

// blockStrings renders block-level children to a slice of block strings (joined
// by callers with blank lines). listIndent is the continuation indent applied to
// nested lists.
func (r *mdRenderer) blockStrings(kids []*snode, listIndent string) []string {
	var out []string
	for _, k := range kids {
		if k.name == "" {
			if s := strings.TrimSpace(collapse(k.text)); s != "" {
				out = append(out, s)
			}
			continue
		}
		if s := r.renderBlock(k, listIndent); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// renderBlock renders a single block-level element.
func (r *mdRenderer) renderBlock(n *snode, listIndent string) string {
	switch n.name {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(n.name[1] - '0')
		return strings.Repeat("#", level) + " " + r.renderInlineChildren(n)
	case "p":
		return r.renderInlineChildren(n)
	case "ul":
		return r.renderList(n, false, listIndent)
	case "ol":
		return r.renderList(n, true, listIndent)
	case "blockquote":
		return prefixLines(strings.Join(r.blockStrings(n.kids, ""), "\n\n"), "> ")
	case "hr":
		return "---"
	case "pre":
		return "```\n" + textContent(n) + "\n```"
	case "table":
		return r.renderTable(n)
	case "ac:structured-macro":
		return r.renderMacro(n, true)
	case "ac:image", "ac:link", "a", "strong", "b", "em", "i", "code", "del", "s", "strike", "br":
		// An inline element sitting at block level (Confluence often emits a bare
		// <ac:image> not wrapped in <p>) is rendered as its own paragraph.
		return r.renderInline(n)
	case "ac:layout", "ac:layout-section", "ac:layout-cell":
		return r.renderRawBlock(n)
	case "div":
		return strings.Join(r.blockStrings(n.kids, listIndent), "\n\n")
	default:
		// Unknown element: render its children as blocks (transparent wrapper).
		return strings.Join(r.blockStrings(n.kids, listIndent), "\n\n")
	}
}

// renderList renders a ul/ol, incrementing ordered markers and indenting nested
// lists to align under their item text.
func (r *mdRenderer) renderList(n *snode, ordered bool, indent string) string {
	var lines []string
	i := 0
	for _, k := range n.kids {
		if k.name != "li" {
			continue
		}
		i++
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", i)
		}
		cont := indent + strings.Repeat(" ", len(marker))
		lines = append(lines, indent+marker+r.renderListItem(k, cont))
	}
	return strings.Join(lines, "\n")
}

// renderListItem renders an <li>: its inline/paragraph content on the first line,
// with any nested lists indented beneath it.
func (r *mdRenderer) renderListItem(li *snode, cont string) string {
	var head strings.Builder
	var tail []string
	for _, k := range li.kids {
		switch k.name {
		case "ul":
			tail = append(tail, r.renderList(k, false, cont))
		case "ol":
			tail = append(tail, r.renderList(k, true, cont))
		case "p":
			if s := r.renderInlineChildren(k); s != "" {
				if head.Len() > 0 {
					head.WriteString(" ")
				}
				head.WriteString(s)
			}
		default:
			head.WriteString(r.renderInline(k))
		}
	}
	item := strings.TrimSpace(head.String())
	if len(tail) > 0 {
		item += "\n" + strings.Join(tail, "\n")
	}
	return item
}

// renderTable renders a table as a GFM pipe table. Alignment is not preserved.
func (r *mdRenderer) renderTable(n *snode) string {
	var rows []*snode
	var header *snode
	var walk func(*snode)
	walk = func(x *snode) {
		for _, k := range x.kids {
			switch k.name {
			case "thead", "tbody":
				walk(k)
			case "tr":
				if header == nil && len(rows) == 0 && rowHasHeaderCell(k) {
					header = k
				} else {
					rows = append(rows, k)
				}
			}
		}
	}
	walk(n)
	if header == nil {
		if len(rows) == 0 {
			return ""
		}
		header, rows = rows[0], rows[1:]
	}

	head := r.cellTexts(header)
	var b strings.Builder
	b.WriteString("| " + strings.Join(head, " | ") + " |\n")
	b.WriteString("| " + strings.Join(columnSeparators(header, rows, len(head)), " | ") + " |")
	for _, row := range rows {
		b.WriteString("\n| " + strings.Join(r.cellTexts(row), " | ") + " |")
	}
	return b.String()
}

// alignSeparators are the GFM delimiter cells, keyed by the alignment recovered
// from storage.
var alignSeparators = map[string]string{
	"left":   ":---",
	"center": ":---:",
	"right":  "---:",
}

// textAlignRE pulls the value out of a text-align declaration anywhere in a style
// attribute.
var textAlignRE = regexp.MustCompile(`(?i)text-align\s*:\s*([a-z]+)`)

// whitespaceRunRE collapses a run of whitespace to a single space in collapse
// below. Not the same concern as linkindex's identically-shaped regexp (that
// one collapses a run to a hyphen, for a Confluence anchor slug); duplicated
// rather than imported, since sharing it would couple this file's general
// text-collapsing to an unrelated package over one regexp literal.
var whitespaceRunRE = regexp.MustCompile(`\s+`)

// columnSeparators builds the delimiter row, recovering each column's alignment
// from its cells.
//
// Confluence aligns a paragraph while GFM aligns a column, so a column whose
// cells disagree cannot be represented: the most common alignment wins and the
// rest are dropped. Cells that declare nothing do not vote -- a single centered
// cell in a column of plain ones still centers the column, which is the only
// reading that survives the round trip at all.
func columnSeparators(header *snode, rows []*snode, cols int) []string {
	counts := make([]map[string]int, cols)
	order := make([]map[string]int, cols)
	seen := 0
	for _, tr := range append([]*snode{header}, rows...) {
		if tr == nil {
			continue
		}
		i := 0
		for _, c := range tr.kids {
			if c.name != "th" && c.name != "td" {
				continue
			}
			if i >= cols {
				break
			}
			if a := cellAlignment(c); a != "" {
				if counts[i] == nil {
					counts[i], order[i] = map[string]int{}, map[string]int{}
				}
				counts[i][a]++
				if _, ok := order[i][a]; !ok {
					seen++
					order[i][a] = seen
				}
			}
			i++
		}
	}

	seps := make([]string, cols)
	for i := range seps {
		seps[i] = "---"
		best := ""
		for a, n := range counts[i] {
			// Ties go to whichever alignment appeared first, so the delimiter row
			// does not depend on map iteration order.
			if best == "" || n > counts[i][best] || (n == counts[i][best] && order[i][a] < order[i][best]) {
				best = a
			}
		}
		if sep, ok := alignSeparators[best]; ok {
			seps[i] = sep
		}
	}
	return seps
}

// cellAlignment reports the alignment stored on one cell, normalized to the GFM
// vocabulary. Confluence's own form is a text-align on a paragraph inside the
// cell, which is what markfluence writes; the cell-level form works too and is
// read for the same reason. "start"/"end" are ADF's names for left/right and turn
// up in hand-edited storage.
func cellAlignment(c *snode) string {
	styles := []string{c.attrs["style"]}
	for _, k := range c.kids {
		if k.name == "p" {
			styles = append(styles, k.attrs["style"])
		}
	}
	for _, s := range styles {
		m := textAlignRE.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		switch strings.ToLower(m[1]) {
		case "left", "start":
			return "left"
		case "center":
			return "center"
		case "right", "end":
			return "right"
		}
	}
	return ""
}

// rowHasHeaderCell reports whether a <tr> contains a <th>.
func rowHasHeaderCell(tr *snode) bool {
	for _, c := range tr.kids {
		if c.name == "th" {
			return true
		}
	}
	return false
}

// cellTexts renders a row's cells to inline strings with pipes escaped,
// prefixed with a bg: marker for a cell carrying a background color.
func (r *mdRenderer) cellTexts(tr *snode) []string {
	var cells []string
	for _, c := range tr.kids {
		if c.name == "th" || c.name == "td" {
			text := strings.ReplaceAll(r.renderInlineChildren(c), "|", `\|`)
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

// cellBGMarkerComment recovers a "<!-- bg:NAME -->" marker from a cell's
// data-highlight-colour, the inverse of resolveCellBG. A hex outside the 21
// swatches -- set directly in hand-edited storage, never by markfluence --
// round-trips as the literal hex rather than a name.
func cellBGMarkerComment(c *snode) string {
	hex, ok := c.attrs["data-highlight-colour"]
	if !ok || hex == "" {
		return ""
	}
	hex = strings.ToLower(hex)
	name, ok := cellBGNames[hex]
	if !ok {
		name = hex
	}
	return "<!-- bg:" + name + " -->"
}

// renderMacro renders an <ac:structured-macro>: the code/toc/callout macros
// MdToConfluence emits become their markdown equivalents; any other macro passes
// through as raw storage. A block-context unknown macro uses the round-trip-safe
// multi-line form (renderRawBlock, markdown body); an inline one stays raw on a
// single line so it does not break out of its paragraph.
func (r *mdRenderer) renderMacro(n *snode, block bool) string {
	switch name := n.attrs["ac:name"]; {
	case name == "code":
		return renderCodeMacro(n)
	case name == "toc":
		return tocToken
	case calloutMacroInverse[name] != "":
		return r.renderCallout(n, name)
	case block:
		return r.renderRawBlock(n)
	default:
		return serialize(n)
	}
}

// renderCodeMacro renders a code macro to a fenced block with its language.
func renderCodeMacro(n *snode) string {
	lang, code := "", ""
	for _, k := range n.kids {
		switch {
		case k.name == "ac:parameter" && k.attrs["ac:name"] == "language":
			lang = textContent(k)
		case k.name == "ac:plain-text-body":
			code = textContent(k)
		}
	}
	return "```" + lang + "\n" + code + "\n```"
}

// renderCallout renders a callout macro as a GitHub alert blockquote.
func (r *mdRenderer) renderCallout(n *snode, macro string) string {
	content := "[!" + calloutMacroInverse[macro] + "]"
	if body := findChild(n, "ac:rich-text-body"); body != nil {
		if inner := strings.Join(r.blockStrings(body.kids, ""), "\n\n"); inner != "" {
			content += "\n" + inner
		}
	}
	return prefixLines(content, "> ")
}

// --- inline rendering --------------------------------------------------------

// renderInlineChildren renders a node's children as a single inline string.
func (r *mdRenderer) renderInlineChildren(n *snode) string {
	var b strings.Builder
	for _, k := range coalesceSplitMarks(n.kids) {
		b.WriteString(r.renderInline(k))
	}
	return strings.TrimSpace(b.String())
}

// formatMarks are the inline formatting tags coalesceSplitMarks may hoist
// across a link boundary.
var formatMarks = map[string]bool{
	"strong": true, "b": true,
	"em": true, "i": true,
	"del": true, "s": true, "strike": true,
}

// coalesceSplitMarks merges a formatting run that Confluence's own editor can
// split around a link. ADF (Confluence's native document model) carries marks
// per text run rather than as nested elements, so "**text [link](url)**" --
// which markfluence always writes nested, as one <strong> wrapping both the
// text and the link -- comes back from a page that has since been edited and
// saved in Confluence's editor as two adjacent runs sharing the mark instead:
// <strong>text </strong><a href="url"><strong>link</strong></a>. Verified
// 2026-08-30 via a direct atlas_doc_format PUT of the unmodified ADF markfluence
// itself had published, which is what the editor does on any save; the same PUT
// with the link's href pointing at another Confluence page produces the
// identical split with <ac:link> in place of <a> (see isLinkNode). Rendered as
// two independent nodes that becomes "**text **[**link**](url)": the closing
// ** is preceded by a space, so CommonMark's flanking rule refuses to treat it
// as emphasis at all -- the markdown comes back not merely unstyled but
// literally reading "**text **". This restores the nested form before
// rendering, the only shape markdown can actually express, by hoisting the
// mark to wrap the whole run including the link and dropping the now-redundant
// inner one.
//
// Merging two adjacent same-tag mark elements outright (mergeMarkRun's first
// case, needed nowhere else) is what lets a third run on either side of the
// link fold into an already-repaired node; it is not itself a repair, since
// "<strong>a</strong><strong>b</strong>" is valid nested markdown either way.
func coalesceSplitMarks(kids []*snode) []*snode {
	out := make([]*snode, 0, len(kids))
	for _, k := range kids {
		if len(out) > 0 {
			if merged := mergeMarkRun(out[len(out)-1], k); merged != nil {
				out[len(out)-1] = merged
				continue
			}
		}
		out = append(out, k)
	}
	return out
}

// isLinkNode reports whether n is a link element coalesceSplitMarks may hoist
// a mark across: a markdown link, or the editor's own internal <ac:link> (used
// for a page, space, or user link -- see aclink.go).
func isLinkNode(n *snode) bool {
	return n.name == "a" || n.name == "ac:link"
}

// linkTextBody returns the node whose children hold a link's visible text --
// the <a> itself, or an <ac:link>'s <ac:link-body> -- or nil if it has neither.
// An <ac:link>'s other body spelling, ac:plain-text-link-body, holds CDATA and
// so can never carry a mark element to unwrap.
func linkTextBody(n *snode) *snode {
	if n.name == "a" {
		return n
	}
	return findChild(n, "ac:link-body")
}

// withLinkTextBody returns a copy of link node n with body's children replaced
// by kids -- unwrapping a mark mergeMarkRun is hoisting out of it. body is
// n itself for an <a>, or its <ac:link-body> child for an <ac:link>, whose
// other children (ri:page, ac:anchor, ...) must survive untouched.
func withLinkTextBody(n, body *snode, kids []*snode) *snode {
	if n.name == "a" {
		return &snode{name: "a", attrs: n.attrs, kids: kids}
	}
	newKids := make([]*snode, len(n.kids))
	for i, k := range n.kids {
		if k == body {
			k = &snode{name: k.name, attrs: k.attrs, kids: kids}
		}
		newKids[i] = k
	}
	return &snode{name: n.name, attrs: n.attrs, kids: newKids}
}

// mergeMarkRun merges two adjacent inline nodes when they carry the same
// formatting mark: either both are the same mark element, or one is a mark and
// the other is a link whose entire visible text is that same mark (the split
// coalesceSplitMarks exists to repair). Returns nil when they don't combine.
func mergeMarkRun(prev, cur *snode) *snode {
	switch {
	case prev.name == cur.name && formatMarks[prev.name]:
		return &snode{name: prev.name, attrs: mergeAttrs(prev.attrs, cur.attrs), kids: concatKids(prev.kids, cur.kids)}
	case formatMarks[prev.name] && isLinkNode(cur):
		if link := hoistMarkIntoLink(cur, prev.name); link != nil {
			return &snode{name: prev.name, attrs: prev.attrs, kids: concatKids(prev.kids, []*snode{link})}
		}
	case isLinkNode(prev) && formatMarks[cur.name]:
		if link := hoistMarkIntoLink(prev, cur.name); link != nil {
			return &snode{name: cur.name, attrs: cur.attrs, kids: concatKids([]*snode{link}, cur.kids)}
		}
	}
	return nil
}

// mergeAttrs unions two attribute maps; a key present in both keeps a's value,
// so merging n adjacent same-tag runs left to right is order-independent.
// nil-safe in both directions, since most snodes carry no attrs at all.
func mergeAttrs(a, b map[string]string) map[string]string {
	if len(b) == 0 {
		return a
	}
	out := make(map[string]string, len(a)+len(b))
	for k, v := range b {
		out[k] = v
	}
	for k, v := range a {
		out[k] = v
	}
	return out
}

// hoistMarkIntoLink strips a redundant mark wrapping the entirety of link's
// visible text, returning the link with that text unwrapped, or nil if the
// link has no text body or is not entirely marked (a link only partly marked
// is left alone: there's nothing correct to hoist).
func hoistMarkIntoLink(link *snode, mark string) *snode {
	body := linkTextBody(link)
	if body == nil {
		return nil
	}
	inner, ok := unwrapSoleMark(body, mark)
	if !ok {
		return nil
	}
	return withLinkTextBody(link, body, inner)
}

// unwrapSoleMark reports whether n's entire content is a single child element
// carrying the given mark, returning that child's own children -- the link's
// content with the redundant inner mark stripped.
func unwrapSoleMark(n *snode, mark string) ([]*snode, bool) {
	if len(n.kids) != 1 || n.kids[0].name != mark {
		return nil, false
	}
	return n.kids[0].kids, true
}

// concatKids returns a with b appended, without aliasing either's backing array.
func concatKids(a, b []*snode) []*snode {
	out := make([]*snode, 0, len(a)+len(b))
	out = append(out, a...)
	return append(out, b...)
}

// renderInline renders one inline node.
func (r *mdRenderer) renderInline(n *snode) string {
	if n.name == "" {
		return collapse(n.text)
	}
	switch n.name {
	case "strong", "b":
		return "**" + r.renderInlineChildren(n) + "**"
	case "em", "i":
		return "*" + r.renderInlineChildren(n) + "*"
	case "code":
		return "`" + textContent(n) + "`"
	case "del", "s", "strike":
		return "~~" + r.renderInlineChildren(n) + "~~"
	case "br":
		return "  \n"
	case "a":
		return r.renderLink(n)
	case "ac:image":
		return r.renderImage(n)
	case "ac:link":
		return r.renderACLink(n)
	case "ac:structured-macro":
		// An inline macro (e.g. status/emoticon) stays raw on one line so it does
		// not break out of its paragraph.
		return r.renderMacro(n, false)
	default:
		return r.renderInlineChildren(n)
	}
}

// renderLink renders an <a> as a markdown link, falling back to the href as text.
func (r *mdRenderer) renderLink(n *snode) string {
	href := n.attrs["href"]
	text := r.renderInlineChildren(n)
	if text == "" {
		text = href
	}
	if title := n.attrs["title"]; title != "" {
		return fmt.Sprintf("[%s](%s %s)", text, href, jstr(title))
	}
	return fmt.Sprintf("[%s](%s)", text, href)
}

// renderImage renders an <ac:image> as a markdown image, reconstructing the
// title/width/height/align attributes into a plain title or a JSON title.
func (r *mdRenderer) renderImage(n *snode) string {
	alt := n.attrs["ac:alt"]
	src := ""
	for _, k := range n.kids {
		switch k.name {
		case "ri:attachment":
			// sourceFor yields a filesystem path; a destination is a URL, so a
			// space or a "%" in the path has to be encoded or the markdown does
			// not parse as an image at all.
			src = encodeDestination(r.sourceFor(k.attrs["ri:filename"]))
		case "ri:url":
			src = k.attrs["ri:value"]
		}
	}
	if title := imageTitle(n.attrs); title != "" {
		return fmt.Sprintf("![%s](%s %s)", alt, src, title)
	}
	return fmt.Sprintf("![%s](%s)", alt, src)
}

// imageTitle inverts parseImageTitle: a lone tooltip becomes a quoted string; any
// width/height/align becomes the JSON-title object form.
func imageTitle(attrs map[string]string) string {
	title, width, height, align := attrs["ac:title"], attrs["ac:width"], attrs["ac:height"], attrs["ac:align"]
	if width == "" && height == "" && align == "" {
		if title == "" {
			return ""
		}
		return jstr(title)
	}
	var parts []string
	if title != "" {
		parts = append(parts, `"title":`+jstr(title))
	}
	if width != "" {
		parts = append(parts, `"width":`+width)
	}
	if height != "" {
		parts = append(parts, `"height":`+height)
	}
	if align != "" {
		parts = append(parts, `"align":`+jstr(align))
	}
	return "'{" + strings.Join(parts, ",") + "}'"
}

// --- helpers -----------------------------------------------------------------

// collapse replaces every run of whitespace with a single space (reusing the
// package's whitespace regexp).
func collapse(s string) string {
	return whitespaceRunRE.ReplaceAllString(s, " ")
}

// textContent concatenates all descendant text of a node (used for code, where
// inline formatting must not be interpreted).
func textContent(n *snode) string {
	var b strings.Builder
	var walk func(*snode)
	walk = func(x *snode) {
		for _, k := range x.kids {
			if k.name == "" {
				b.WriteString(k.text)
			} else {
				walk(k)
			}
		}
	}
	walk(n)
	return b.String()
}

// findChild returns n's first direct child with the given name, or nil.
func findChild(n *snode, name string) *snode {
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

// prefixLines prefixes every line of s with prefix. Empty lines get the prefix
// with its trailing whitespace trimmed, so a blockquote's blank lines are ">"
// rather than "> " (no trailing whitespace).
func prefixLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	empty := strings.TrimRight(prefix, " ")
	for i, line := range lines {
		if line == "" {
			lines[i] = empty
		} else {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// jstr renders a string as a JSON string literal.
func jstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// serialize re-emits a node as storage XML, for passing an unknown macro through
// unchanged (MdToConfluence's shield re-publishes it verbatim).
func serialize(n *snode) string {
	if n.name == "" {
		return xmlTextEscape(n.text)
	}
	var b strings.Builder
	b.WriteString("<" + n.name + attrString(n.attrs))
	if len(n.kids) == 0 {
		b.WriteString(" />")
		return b.String()
	}
	b.WriteString(">")
	for _, k := range n.kids {
		b.WriteString(serialize(k))
	}
	b.WriteString("</" + n.name + ">")
	return b.String()
}

// droppedAttrs are server-generated per-instance ids that are noise in the output
// and that Confluence regenerates on publish, so passthrough serialization omits
// them.
var droppedAttrs = map[string]bool{"ac:macro-id": true, "ac:local-id": true}

// attrString renders an element's attributes (sorted, XML-escaped) as a leading-
// space attribute list, dropping the server-generated ids in droppedAttrs.
func attrString(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		if droppedAttrs[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, ` %s="%s"`, k, xmlAttrEscape(attrs[k]))
	}
	return b.String()
}

// renderRawBlock emits a block-level storage element for passthrough in a form
// that round-trips through MdToConfluence: each wrapper tag on its own line (a
// CommonMark type-7 HTML block), a content container's body converted to markdown
// and set off by blank lines, and leaf elements (e.g. ac:parameter) serialized
// raw on a single line. This covers both column layouts and bodied macros
// (expand, panel, …), keeping their bodies readable while the structure and
// parameters survive verbatim.
func (r *mdRenderer) renderRawBlock(n *snode) string {
	open := "<" + n.name + attrString(n.attrs) + ">"
	closeTag := "</" + n.name + ">"

	// Content container: raw tags around a markdown body.
	if n.name == "ac:rich-text-body" || n.name == "ac:layout-cell" {
		if md := strings.Join(r.blockStrings(n.kids, ""), "\n\n"); md != "" {
			return open + "\n\n" + md + "\n\n" + closeTag
		}
		return open + closeTag
	}
	if len(n.kids) == 0 {
		return "<" + n.name + attrString(n.attrs) + " />"
	}

	// Wrapper: one line per element child; a child with element children (or a
	// content container) recurses, a leaf child is serialized raw inline.
	var parts []string
	for _, k := range n.kids {
		if k.name == "" {
			continue // drop inter-tag whitespace
		}
		if k.name == "ac:rich-text-body" || k.name == "ac:layout-cell" || hasElementChild(k) {
			parts = append(parts, r.renderRawBlock(k))
		} else {
			parts = append(parts, serialize(k))
		}
	}
	return open + "\n" + strings.Join(parts, "\n") + "\n" + closeTag
}

// hasElementChild reports whether n has any element (non-text) child.
func hasElementChild(n *snode) bool {
	for _, k := range n.kids {
		if k.name != "" {
			return true
		}
	}
	return false
}

func xmlTextEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func xmlAttrEscape(s string) string {
	return strings.ReplaceAll(xmlTextEscape(s), `"`, "&quot;")
}
