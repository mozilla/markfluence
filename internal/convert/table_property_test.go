package convert

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/project"
)

// The table property test (#55, _plans/055): generate tables from a seed, read
// each one, publish the Markdown, and check three things -- nothing errors, the
// Markdown is a fixed point, and nothing that takes effect in Confluence is
// lost. Two code reviews each found edge cases in the rules that decide between
// a pipe table and a raw one; this tries the shapes nobody thought of.
//
// The third check compares a model of each table (tableModel) rather than its
// storage, because storage legitimately changes: ids are regenerated, a pipe
// table gains markfluence's layout and a <thead>, a raw cell's shared alignment
// moves onto the cell. What the model ignores is the list of differences read
// is allowed to make, written down in one place.

// propertySeeds is how many tables the ordinary test run tries. The fuzz
// target below runs as many more as it is given time for.
const propertySeeds = 3000

func TestTablePropertyRoundTrip(t *testing.T) {
	env := newPropertyEnv(t)
	for seed := range uint64(propertySeeds) {
		if msg := env.check(seed); msg != "" {
			t.Fatal(msg)
		}
	}
}

// FuzzTableProperty runs the same check over seeds the fuzzer chooses:
// go test ./internal/convert -run '^$' -fuzz FuzzTableProperty -fuzztime 60s
func FuzzTableProperty(f *testing.F) {
	f.Add(uint64(0))
	f.Fuzz(func(t *testing.T, seed uint64) {
		if msg := newPropertyEnv(t).check(seed); msg != "" {
			t.Fatal(msg)
		}
	})
}

// TestTablePropertyCorpus runs the check over real tables: a file of storage
// fragments, one table per record separated by a line holding only "\x00",
// named by MF_TABLE_CORPUS. Skipped without it; the corpus is page content and
// is never committed.
func TestTablePropertyCorpus(t *testing.T) {
	path := os.Getenv("MF_TABLE_CORPUS")
	if path == "" {
		t.Skip("MF_TABLE_CORPUS not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	env := newPropertyEnv(t)
	failed := 0
	tables := strings.Split(string(data), "\n\x00\n")
	for i, s := range tables {
		if strings.TrimSpace(s) == "" {
			continue
		}
		if msg := env.checkStorage(fmt.Sprintf("corpus table %d", i), s); msg != "" {
			failed++
			t.Error(msg)
		}
	}
	t.Logf("%d tables, %d failed", len(tables), failed)
}

// propertyEnv is a documentation root holding the image the generator refers
// to, so a published image is not IMAGE BROKEN.
type propertyEnv struct {
	t     testing.TB
	dir   string
	root  *project.Root
	index *linkindex.Index
}

func newPropertyEnv(t testing.TB) *propertyEnv {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "d.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := project.FromPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	idx, err := linkindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	return &propertyEnv{t: t, dir: dir, root: root, index: idx}
}

func (e *propertyEnv) check(seed uint64) string {
	g := &tableGen{r: rand.New(rand.NewPCG(seed, 0x55))}
	return e.checkStorage(fmt.Sprintf("seed %d", seed), g.table(0))
}

// checkStorage returns "" when storage passes all three checks, and a report
// naming what failed otherwise.
func (e *propertyEnv) checkStorage(name, storage string) string {
	fail := func(what string, parts ...string) string {
		return fmt.Sprintf("%s: %s\n--- storage ---\n%s\n%s", name, what, storage, strings.Join(parts, "\n"))
	}
	md, err := StorageToMarkdown(storage, StorageOptions{})
	if err != nil {
		return fail("read: " + err.Error())
	}
	published, err := e.publish(md)
	if err != nil {
		return fail("publish: "+err.Error(), "--- markdown ---", md)
	}
	again, err := StorageToMarkdown(published, StorageOptions{})
	if err != nil {
		return fail("second read: "+err.Error(), "--- markdown ---", md, "--- published ---", published)
	}
	if again != md {
		return fail("the Markdown is not a fixed point",
			"--- markdown ---", md, "--- published ---", published, "--- read again ---", again)
	}
	before, err := modelOf(storage)
	if err != nil {
		return fail("model: " + err.Error())
	}
	after, err := modelOf(published)
	if err != nil {
		return fail("model of published: " + err.Error())
	}
	if before != after {
		return fail("publishing the Markdown changed the table",
			"--- markdown ---", md, "--- published ---", published,
			"--- model before ---", before, "--- model after ---", after)
	}
	return ""
}

func (e *propertyEnv) publish(md string) (string, error) {
	f, err := frontmatter.Parse(filepath.Join(e.dir, "main.md"), md)
	if err != nil {
		return "", err
	}
	page, err := MdToConfluence(f, e.root, e.index, "https://wiki.example.net", "ENG")
	if err != nil {
		return "", err
	}
	if len(page.Broken) > 0 {
		return "", fmt.Errorf("broken: %v", page.Broken)
	}
	return page.HTML, nil
}

// --- the model -----------------------------------------------------------------

// modelOf renders every table in storage as a canonical string of what takes
// effect in Confluence (docs/confluence/storage-format.md), ignoring what read
// may legitimately change:
//
//   - server-generated ids;
//   - data-table-width, and data-table-display-mode="default";
//   - no data-layout, which reads as align-start (D2);
//   - a <colgroup> of pixel widths on an align-start table (the editor's
//     measurement, which read ignores);
//   - thead/tbody/tfoot: Confluence renders every row the same way;
//   - a span of 1;
//   - where an alignment is written: each paragraph's effective alignment is
//     modelled, so a cell style and a paragraph style that say the same are
//     the same, and left, start and justify are no alignment;
//   - loose inline content in a cell, which is a paragraph;
//   - a cell holding nothing but empty paragraphs, which is an empty cell;
//   - b/i/s/strike, which are strong/em/del.
func modelOf(storage string) (string, error) {
	root, err := parseStorage(storage)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	var walk func(n *snode)
	walk = func(n *snode) {
		for _, k := range n.kids {
			if k.name == "table" {
				modelTable(&b, k, "")
				continue
			}
			walk(k)
		}
	}
	walk(root)
	return b.String(), nil
}

func modelTable(b *strings.Builder, t *snode, indent string) {
	attrs := map[string]string{}
	for k, v := range t.attrs {
		attrs[k] = v
	}
	layout := attrs["data-layout"]
	delete(attrs, "data-table-width")
	if attrs["data-table-display-mode"] == "default" {
		delete(attrs, "data-table-display-mode")
	}
	if layout == "" {
		attrs["data-layout"] = "align-start"
	}
	fmt.Fprintf(b, "%stable%s\n", indent, modelAttrs(attrs))
	for _, sec := range t.kids {
		switch sec.name {
		case "colgroup":
			if layout == "align-start" && pixelColgroup(sec) {
				continue
			}
			var cols []string
			for _, c := range sec.kids {
				if c.name == "col" {
					cols = append(cols, "col"+modelAttrs(c.attrs))
				}
			}
			fmt.Fprintf(b, "%s  colgroup %s\n", indent, strings.Join(cols, " "))
		case "thead", "tbody", "tfoot":
			for _, tr := range sec.kids {
				if tr.name == "tr" {
					modelRow(b, tr, indent+"  ")
				}
			}
		case "tr":
			modelRow(b, sec, indent+"  ")
		}
	}
}

func modelRow(b *strings.Builder, tr *snode, indent string) {
	fmt.Fprintf(b, "%srow%s\n", indent, modelAttrs(tr.attrs))
	for _, c := range tr.kids {
		if c.name != "th" && c.name != "td" {
			continue
		}
		attrs := map[string]string{}
		for k, v := range c.attrs {
			attrs[k] = v
		}
		for _, k := range []string{"rowspan", "colspan"} {
			if strings.TrimSpace(attrs[k]) == "1" {
				delete(attrs, k)
			}
		}
		if v, ok := attrs["data-highlight-colour"]; ok {
			attrs["data-highlight-colour"] = strings.ToLower(v)
		}
		cellAlign, _ := styleAlign(attrs["style"])
		if onlyTextAlign(attrs["style"]) {
			delete(attrs, "style")
		}
		fmt.Fprintf(b, "%s  %s%s\n", indent, c.name, modelAttrs(attrs))
		for _, blk := range modelBlocks(c, cellAlign, indent+"    ") {
			b.WriteString(blk)
		}
	}
}

// modelBlocks models a cell's content as blocks. An empty cell -- nothing but
// empty paragraphs -- has none.
func modelBlocks(c *snode, cellAlign, indent string) []string {
	if emptyCell(c) {
		return nil
	}
	var out []string
	var run []*snode
	flush := func() {
		if s := modelInline(run); strings.TrimSpace(s) != "" {
			out = append(out, fmt.Sprintf("%sp[%s] %s\n", indent, cellAlign, strings.TrimSpace(s)))
		}
		run = nil
	}
	for _, k := range c.kids {
		switch k.name {
		case "", "strong", "b", "em", "i", "code", "del", "s", "strike", "a", "ac:image", "br", "ac:link":
			run = append(run, k)
		case "p":
			flush()
			a, declared := styleAlign(k.attrs["style"])
			if !declared {
				a = cellAlign
			}
			text := strings.TrimSpace(modelInline(k.kids))
			if strings.Trim(text, "⏎") == "" {
				a = "" // an empty paragraph's alignment shows nothing
			}
			out = append(out, fmt.Sprintf("%sp[%s] %s\n", indent, a, text))
		case "ul", "ol":
			flush()
			out = append(out, indent+modelList(k)+"\n")
		case "table":
			flush()
			var b strings.Builder
			modelTable(&b, k, indent)
			out = append(out, b.String())
		case "ac:structured-macro":
			flush()
			out = append(out, fmt.Sprintf("%smacro %s %s %q\n", indent, k.attrs["ac:name"],
				macroParam(k, "language"), textContent(findChild(k, "ac:plain-text-body"))))
		default:
			flush()
			out = append(out, fmt.Sprintf("%s%s %s\n", indent, k.name, strings.TrimSpace(modelInline(k.kids))))
		}
	}
	flush()
	return joinParagraphs(out, indent)
}

// joinParagraphs joins adjacent paragraphs of one alignment with a line break.
// A pipe cell holds its lines on one row joined by <br>, which publishes as
// line breaks in one paragraph where the page had a paragraph per line: the
// two look the same in a cell, and markfluence has always mapped them so.
func joinParagraphs(blocks []string, indent string) []string {
	var out []string
	for _, blk := range blocks {
		if n := len(out); n > 0 {
			prev, cur := out[n-1], blk
			if pa, pt, ok := paraParts(prev, indent); ok {
				if ca, ct, ok := paraParts(cur, indent); ok {
					// An empty line has no alignment of its own to disagree with.
					switch {
					case strings.Trim(pt, "⏎") == "":
						pa = ca
					case strings.Trim(ct, "⏎") == "":
						ca = pa
					}
					if pa == ca {
						out[n-1] = fmt.Sprintf("%sp[%s] %s\n", indent, pa, pt+"⏎"+ct)
						continue
					}
				}
			}
		}
		out = append(out, blk)
	}
	return out
}

// paraParts splits a modelled paragraph into its alignment and text.
func paraParts(blk, indent string) (string, string, bool) {
	rest, ok := strings.CutPrefix(blk, indent+"p[")
	if !ok {
		return "", "", false
	}
	align, text, ok := strings.Cut(strings.TrimSuffix(rest, "\n"), "] ")
	return align, text, ok
}

func macroParam(m *snode, name string) string {
	for _, k := range m.kids {
		if k.name == "ac:parameter" && k.attrs["ac:name"] == name {
			return textContent(k)
		}
	}
	return ""
}

func modelList(l *snode) string {
	var items []string
	for _, li := range l.kids {
		if li.name == "li" {
			var parts []string
			for _, k := range li.kids {
				if k.name == "p" {
					parts = append(parts, strings.TrimSpace(modelInline(k.kids)))
				} else {
					parts = append(parts, strings.TrimSpace(modelInline([]*snode{k})))
				}
			}
			items = append(items, strings.Join(parts, " "))
		}
	}
	return l.name + "[" + strings.Join(items, "; ") + "]"
}

var inlineAlias = map[string]string{"b": "strong", "i": "em", "s": "del", "strike": "del"}

// modelInline renders inline content with its marks, collapsing whitespace.
func modelInline(kids []*snode) string {
	var b strings.Builder
	for _, k := range kids {
		switch k.name {
		case "":
			b.WriteString(k.text)
		case "br":
			b.WriteString("⏎")
		case "a":
			fmt.Fprintf(&b, "<a %s>%s</a>", k.attrs["href"], modelInline(k.kids))
		case "ac:image":
			f := ""
			if att := findChild(k, "ri:attachment"); att != nil {
				f = att.attrs["ri:filename"]
			}
			fmt.Fprintf(&b, "<img %s>", f)
		default:
			name := k.name
			if a, ok := inlineAlias[name]; ok {
				name = a
			}
			fmt.Fprintf(&b, "<%s>%s</%s>", name, modelInline(k.kids), name)
		}
	}
	s := whitespaceRunRE.ReplaceAllString(b.String(), " ")
	// Whitespace beside a line break shows nothing, and publishing writes a
	// newline after every <br />.
	return strings.ReplaceAll(strings.ReplaceAll(s, " ⏎", "⏎"), "⏎ ", "⏎")
}

func modelAttrs(attrs map[string]string) string {
	var keys []string
	for k := range attrs {
		if !isID(k) && !droppedAttrs[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%q", k, normalizeWidths(attrs[k]))
	}
	return b.String()
}

var pointZeroRE = regexp.MustCompile(`(\d)\.0px`)

// normalizeWidths reads 300px and 300.0px as one width: Confluence rewrites
// the first as the second on write.
func normalizeWidths(v string) string { return pointZeroRE.ReplaceAllString(v, "${1}px") }

// --- the generator ---------------------------------------------------------------

// tableGen generates table storage from a seed. The attribute vocabulary is
// what the live survey found (layouts, colgroups, colours, alignments, ids,
// valign, class), weighted towards tables a pipe table can hold, so both paths
// get exercised. Text avoids a leading Markdown block marker ("1. ", "# "),
// which #203 covers, and inline tags read has no Markdown for (<time>, <u>),
// which read drops in every paragraph, and two lists side by side, which
// Markdown reads back as one (#205) -- all gaps older than #55.
type tableGen struct{ r *rand.Rand }

func (g *tableGen) chance(p float64) bool { return g.r.Float64() < p }

func (g *tableGen) pick(xs ...string) string { return xs[g.r.IntN(len(xs))] }

func (g *tableGen) table(depth int) string {
	var attrs []string
	layout := ""
	if g.chance(0.6) {
		layout = g.pick("align-start", "align-start", "align-start", "default", "center", "wide", "full-width")
		attrs = append(attrs, fmt.Sprintf(`data-layout="%s"`, layout))
	}
	if g.chance(0.4) {
		attrs = append(attrs, fmt.Sprintf(`data-table-width="%d"`, 200+g.r.IntN(900)))
	}
	if g.chance(0.15) {
		attrs = append(attrs, `data-table-display-mode="`+g.pick("default", "default", "fixed")+`"`)
	}
	if g.chance(0.3) {
		attrs = append(attrs, `ac:local-id="t1"`)
	}
	if g.chance(0.05) {
		attrs = append(attrs, `class="wrapped"`)
	}
	cols := 1 + g.r.IntN(4)
	rows := 1 + g.r.IntN(4)
	var b strings.Builder
	b.WriteString("<table")
	for _, a := range attrs {
		b.WriteString(" " + a)
	}
	b.WriteString(">")
	if g.chance(0.3) {
		b.WriteString("<colgroup>")
		for range cols {
			if g.chance(0.9) {
				fmt.Fprintf(&b, `<col style="width: %d.0px;" />`, 40+g.r.IntN(300))
			} else {
				b.WriteString("<col />")
			}
		}
		b.WriteString("</colgroup>")
	}
	headerRow := g.chance(0.85)
	headerCol := g.chance(0.08)
	// One alignment per column, usually honoured by every cell in it.
	aligns := make([]string, cols)
	for i := range aligns {
		if g.chance(0.35) {
			aligns[i] = g.pick("center", "right", "end", "left", "start", "justify")
		}
	}
	thead := headerRow && g.chance(0.4)
	if thead {
		b.WriteString("<thead>")
	} else if g.chance(0.8) {
		b.WriteString("<tbody>")
		thead = false
	}
	sectionOpen := strings.HasSuffix(b.String(), "<tbody>") || thead
	for row := range rows {
		n := cols
		if g.chance(0.05) && n > 1 {
			n-- // a short row
		}
		b.WriteString("<tr")
		if g.chance(0.3) {
			b.WriteString(` ac:local-id="r"`)
		}
		b.WriteString(">")
		for col := 0; col < n; col++ {
			name := "td"
			if (row == 0 && headerRow) || (col == 0 && headerCol) {
				name = "th"
			}
			var cattrs []string
			if g.chance(0.05) && col < n-1 {
				cattrs = append(cattrs, `colspan="2"`)
				col++
			} else if g.chance(0.1) {
				cattrs = append(cattrs, `colspan="1"`)
			}
			if g.chance(0.04) {
				cattrs = append(cattrs, `rowspan="2"`)
			}
			if g.chance(0.2) {
				cattrs = append(cattrs, `data-highlight-colour="`+g.pick("#e3fcef", "#FFEBE6", "#deebff", "#123456")+`"`)
			}
			if g.chance(0.03) {
				cattrs = append(cattrs, `valign="bottom"`)
			}
			if g.chance(0.03) {
				cattrs = append(cattrs, `class="numberingColumn"`)
			}
			if g.chance(0.3) {
				cattrs = append(cattrs, `ac:local-id="c"`)
			}
			align := aligns[min(col, cols-1)]
			if align != "" && g.chance(0.1) {
				align = "" // a cell that disagrees with its column
			}
			cellStyle := align != "" && g.chance(0.2)
			if cellStyle {
				cattrs = append(cattrs, fmt.Sprintf(`style="text-align: %s;"`, align))
			}
			fmt.Fprintf(&b, "<%s", name)
			for _, a := range cattrs {
				b.WriteString(" " + a)
			}
			b.WriteString(">")
			b.WriteString(g.cell(align, cellStyle, depth))
			fmt.Fprintf(&b, "</%s>", name)
		}
		b.WriteString("</tr>")
		if thead && row == 0 {
			b.WriteString("</thead><tbody>")
		}
	}
	if sectionOpen {
		b.WriteString("</tbody>")
	}
	b.WriteString("</table>")
	return b.String()
}

var words = []string{"auth", "billing", "search", "up", "down", "3", "12", "a|b", "ok", "since noon", "x"}

func (g *tableGen) text() string {
	n := 1 + g.r.IntN(3)
	var ws []string
	for range n {
		ws = append(ws, words[g.r.IntN(len(words))])
	}
	return strings.Join(ws, " ")
}

func (g *tableGen) inline() string {
	switch g.r.IntN(8) {
	case 0:
		return "<strong>" + g.text() + "</strong>"
	case 1:
		return "<em>" + g.text() + "</em>"
	case 2:
		return "<code>" + g.text() + "</code>"
	case 3:
		return `<a href="https://example.com/p">` + g.text() + "</a>"
	case 4:
		return `<ac:image><ri:attachment ri:filename="d.png" /></ac:image>`
	default:
		return g.text()
	}
}

func (g *tableGen) paragraph(align string, cellStyle bool) string {
	var attrs string
	if g.chance(0.4) {
		attrs += ` local-id="p"`
	}
	if align != "" && !cellStyle {
		attrs += fmt.Sprintf(` style="text-align: %s;"`, align)
	}
	body := g.inline()
	if g.chance(0.3) {
		body += " " + g.inline()
	}
	if g.chance(0.1) {
		body += "<br />" + g.text()
	}
	return "<p" + attrs + ">" + body + "</p>"
}

func (g *tableGen) cell(align string, cellStyle bool, depth int) string {
	if g.chance(0.08) {
		return "" // empty
	}
	if g.chance(0.04) {
		return "<p />"
	}
	var b strings.Builder
	blocks := 1
	if g.chance(0.25) {
		blocks += 1 + g.r.IntN(2)
	}
	lastList := false
	for range blocks {
		x := g.r.Float64()
		if lastList && x >= 0.77 && x < 0.84 {
			x = 0 // two lists side by side merge into one in any Markdown (#205)
		}
		lastList = x >= 0.77 && x < 0.84
		switch {
		case x < 0.6:
			b.WriteString(g.paragraph(align, cellStyle))
		case x < 0.68:
			b.WriteString(g.text()) // loose text
			if g.chance(0.5) {
				b.WriteString(" " + g.inline())
			}
		case x < 0.74:
			b.WriteString("<p />")
		case x < 0.77:
			b.WriteString("<p><br /></p>")
		case x < 0.84:
			tag := g.pick("ul", "ol")
			b.WriteString("<" + tag + ">")
			for range 1 + g.r.IntN(3) {
				if g.chance(0.5) {
					b.WriteString("<li><p>" + g.text() + "</p></li>")
				} else {
					b.WriteString("<li>" + g.text() + "</li>")
				}
			}
			b.WriteString("</" + tag + ">")
		case x < 0.88:
			b.WriteString(`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">bash</ac:parameter>` +
				`<ac:plain-text-body><![CDATA[echo ` + g.text() + `]]></ac:plain-text-body></ac:structured-macro>`)
		case x < 0.92:
			b.WriteString("<h3>" + g.text() + "</h3>")
		case x < 0.95 && depth == 0:
			b.WriteString(g.table(depth + 1))
		default:
			b.WriteString(g.paragraph(align, cellStyle))
		}
	}
	return b.String()
}
