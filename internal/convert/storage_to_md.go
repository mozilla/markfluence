package convert

// storage_to_md.go is the best-effort inverse of MdToConfluence: it turns a
// Confluence storage-format body (XHTML) back into GitHub-Flavored Markdown.
//
// Constructs MdToConfluence emits round-trip faithfully; editor-authored content
// markfluence never emits degrades gracefully -- macro bodies are rendered, and
// unknown leaf macros pass through as raw storage (which MdToConfluence's ac:/ri:
// shield re-publishes verbatim). Parsing uses encoding/xml with the built-in HTML
// entity table; storage format is well-formed XHTML, so this never errors on it.

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
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
// sources maps an attachment name to the markdown image path it was published
// from, as recorded on the attachment when markfluence uploaded it. It is
// optional: a nil map (or a name missing from it) falls back to decoding the
// attachment name, which is exact for names markfluence created and a
// best-effort guess for hand-uploaded ones.
func StorageToMarkdown(storage string, sources map[string]string) (string, error) {
	root, err := parseStorage(storage)
	if err != nil {
		return "", err
	}
	r := &mdRenderer{sources: sources}
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
}

// sourceFor resolves an attachment name back to the markdown image path to write.
// The path recorded on the attachment wins because it is exact; otherwise the
// name is decoded. An absolute path is never something markfluence published, so
// it is refused in both cases and the raw attachment name is used instead.
func (r *mdRenderer) sourceFor(filename string) string {
	if src, ok := r.sources[filename]; ok && src != "" && !path.IsAbs(src) {
		return src
	}
	if src, ok := attachmentSource(filename); ok {
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

// parseStorage parses a storage fragment into a tree rooted at a nameless node.
func parseStorage(storage string) (*snode, error) {
	dec := xml.NewDecoder(strings.NewReader(storage))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity       // resolve &nbsp; and friends
	dec.AutoClose = xml.HTMLAutoClose // treat <br>, <hr>, <img>, ... as void

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
	case "ac:image", "a", "strong", "b", "em", "i", "code", "del", "s", "strike", "br":
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
	seps := make([]string, len(head))
	for i := range seps {
		seps[i] = "---"
	}
	b.WriteString("| " + strings.Join(seps, " | ") + " |")
	for _, row := range rows {
		b.WriteString("\n| " + strings.Join(r.cellTexts(row), " | ") + " |")
	}
	return b.String()
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

// cellTexts renders a row's cells to inline strings with pipes escaped.
func (r *mdRenderer) cellTexts(tr *snode) []string {
	var cells []string
	for _, c := range tr.kids {
		if c.name == "th" || c.name == "td" {
			text := strings.ReplaceAll(r.renderInlineChildren(c), "|", `\|`)
			cells = append(cells, text)
		}
	}
	return cells
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
	for _, k := range n.kids {
		b.WriteString(r.renderInline(k))
	}
	return strings.TrimSpace(b.String())
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
			src = r.sourceFor(k.attrs["ri:filename"])
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
