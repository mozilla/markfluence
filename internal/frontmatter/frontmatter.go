// Package frontmatter parses and rewrites the YAML frontmatter block that
// markfluence markdown files carry, and models a parsed file as a MarkdownFile.
//
// The block is real YAML, parsed and emitted by goccy/go-yaml. It is still
// flat -- no nesting -- but a value may be a scalar or a sequence of scalars,
// in either YAML spelling: the flow form "[a, b]" or the block form of "- a"
// lines. Every scalar, including a sequence's elements, must occupy a single
// line; that is enforced by scalarValue and elementValue rather than assumed by
// a line-splitting parser that could not see a violation. Scalars land in
// MarkdownFile.Frontmatter and sequences in MarkdownFile.Lists, so a key
// appears in exactly one map and nothing here knows which keys are lists.
//
// Writes go through valueNodeFor and elementNodeFor, which verify their own
// output: they emit with goccy's chosen style, re-read the result, and fall
// back to a double-quoted scalar when the two disagree. goccy's default is
// wrong for a handful of shapes -- a tab is dropped, a value starting "? "
// produces a document goccy itself refuses to parse, and a bare comma or "]"
// inside a flow sequence silently changes the list -- and a hand-written
// predicate listing them would be incomplete, since those cases turned up only
// by probing. Checking beats predicting.
package frontmatter

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// frontmatterRE matches a leading `---\n...\n---\n` block (DOTALL, non-greedy),
// anchored at the start of the document.
var frontmatterRE = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n`)

// ErrUnterminatedFrontmatter is returned by Parse/ParseFile when content opens
// with a "---\n" delimiter that never closes. This is a lexical check on the
// delimiters, made before any YAML parsing, so it cannot be confused with a
// goccy error about the block's contents.
//
// It is a lexical check, not a parse: a document whose very first line is a
// bare thematic break (a markdown horizontal rule) is indistinguishable from
// unterminated frontmatter and is flagged the same way. Accepted deliberately.
var ErrUnterminatedFrontmatter = errors.New(
	`unterminated frontmatter block: starts with "---" but has no closing "---" line`)

// fieldOrder is the canonical leading order of frontmatter keys; any key not
// listed here sorts after these, alphabetically. Every write that orders fields
// goes through keyLess, so this is the single source of frontmatter field order
// across all commands.
var fieldOrder = []string{"title", "space", "parent", "page_id"}

// typedFields are the keys whose value domain is not free text: a numeric id or
// the null that means "unset". They are written as YAML integers and nulls
// rather than strings, because `page_id: "123"` and `parent: "null"` are valid
// YAML that says the wrong thing.
//
// Quoting is goccy's job; this typing is ours, and it is deliberately confined
// to two keys whose domains are closed. A title of "123" is still a string.
var typedFields = map[string]bool{"page_id": true, "parent": true}

// scalarFields are the keys markfluence reads as single values, and which are
// therefore an error when written as a sequence.
//
// Without this a sequence-valued key simply lands in Lists, and a key no
// command looks for there reads as *absent* -- which for `parent` meant
// `parent:` written as a list published the page at the space root and then
// had `parent: null` written over the author's intent, with no error anywhere.
// Refusing it restores what the reader did before sequences existed.
//
// A whitelist of names, in a package that otherwise learns kinds rather than
// names, and deliberately so: the alternative -- allowing only `labels` to be a
// sequence -- would refuse an unknown list key like `reviewers`, which is the
// generality #21 and #100 need. Unknown keys stay permissive in both
// directions; these five do not, because their readers return a plain string
// with nowhere to put an error.
var scalarFields = map[string]bool{
	"title": true, "space": true, "parent": true, "page_id": true, "page_width": true,
}

// keyLess orders two frontmatter keys: fieldOrder first, in that order, then
// everything else alphabetically.
func keyLess(a, b string) bool {
	ra, aok := fieldRank(a)
	rb, bok := fieldRank(b)
	switch {
	case aok && bok:
		return ra < rb
	case aok != bok:
		return aok
	default:
		return a < b
	}
}

func fieldRank(key string) (int, bool) {
	for i, k := range fieldOrder {
		if k == key {
			return i, true
		}
	}
	return 0, false
}

// pos is the position every hand-built token carries. Column must be at least
// 1: MappingValueNode.String() indents by column-1 and panics on 0.
func pos() *token.Position { return &token.Position{Line: 1, Column: 1} }

// block is a parsed frontmatter block: its mapping, plus any comment that had
// no key to attach to (a block holding nothing but a comment). The orphan is
// carried onto the first key later inserted rather than dropped.
type block struct {
	mapping *ast.MappingNode
	orphan  *ast.CommentGroupNode
}

func emptyMapping() *ast.MappingNode {
	return ast.Mapping(token.New("", "", pos()), false)
}

// parseBlock parses a frontmatter block's inner text. An empty block, and one
// holding only comments, are empty mappings rather than errors -- neither is
// invalid YAML. Any other shape (a bare scalar, a top-level list) is an error.
func parseBlock(fmText string) (*block, error) {
	f, err := parser.ParseBytes([]byte(fmText), parser.ParseComments)
	if err != nil {
		return nil, formatParseError(err)
	}
	// A "..." line inside the block starts a second document, and reading only
	// the first would drop every key after it without a word: `update` would
	// then report "no page id" about a file that visibly has one.
	if len(f.Docs) > 1 {
		return nil, errors.New(`frontmatter must be a single document: remove the "..." line`)
	}
	if len(f.Docs) == 0 || f.Docs[0].Body == nil {
		return &block{mapping: emptyMapping()}, nil
	}
	switch b := f.Docs[0].Body.(type) {
	case *ast.MappingNode:
		return &block{mapping: b}, nil
	case *ast.MappingValueNode:
		m := emptyMapping()
		m.Values = append(m.Values, b)
		return &block{mapping: m}, nil
	case *ast.CommentGroupNode:
		return &block{mapping: emptyMapping(), orphan: b}, nil
	default:
		return nil, fmt.Errorf("frontmatter must be a flat mapping of key: value pairs, found %s",
			b.Type())
	}
}

// formatParseError reduces a goccy error to a single line and corrects its
// position for the "---" opener, which the block text does not include.
//
// goccy's default Error() renders a multi-line source excerpt with ASCII
// pointer art, which would land verbatim in check --json's error string.
// Known limit: a duplicate-key message embeds a second position ("already
// defined at [1:1]") that stays block-relative.
func formatParseError(err error) error {
	msg := yaml.FormatError(err, false, false)
	return errors.New(shiftLeadingPosition(msg))
}

// positionRE matches a leading "[line:col] " position stamp.
var positionRE = regexp.MustCompile(`^\[(\d+):(\d+)\] `)

// shiftLeadingPosition rewrites a leading [line:col] to account for the "---"
// line that opens the block.
func shiftLeadingPosition(msg string) string {
	m := positionRE.FindStringSubmatch(msg)
	if m == nil {
		return msg
	}
	var line, col int
	if _, err := fmt.Sscanf(m[1]+" "+m[2], "%d %d", &line, &col); err != nil {
		return msg
	}
	return fmt.Sprintf("[%d:%d] %s", line+1, col, msg[len(m[0]):])
}

// scalarValue reads a mapping value as a string, rejecting anything that spans
// more than one line.
//
// Every spelling of null -- an absent value, "null", "~", "Null" -- reads as
// "", so a null is unset whatever the author wrote. The old parser mapped only
// the literal "null", which meant "parent: ~" read as though it were a page id.
func scalarValue(key string, n ast.Node) (string, error) {
	if spansLines(n.GetToken().Origin) {
		return "", fmt.Errorf("frontmatter %q must be a single-line scalar; "+
			"a value split over several lines is not supported", key)
	}
	return plainScalar(key, n)
}

// plainScalar is the node-kind whitelist shared by scalarValue and
// elementValue. It is a whitelist because every other node kind -- an anchor,
// an alias, a tag, a "|" literal block -- reports GetToken().Value as its
// indicator character rather than its content, so a blacklist of sequences and
// mappings would silently read "&" or "|". A "- |-" sequence element is the
// case that makes this load-bearing twice over: its token does not span lines,
// so the whitelist is the only thing that catches it.
func plainScalar(key string, n ast.Node) (string, error) {
	switch v := n.(type) {
	case *ast.NullNode:
		return "", nil
	case *ast.StringNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode,
		*ast.InfinityNode, *ast.NanNode:
		return v.GetToken().Value, nil
	default:
		return "", fmt.Errorf("frontmatter %q must be a single scalar value, found %s",
			key, n.Type())
	}
}

// elementValue reads one sequence element. It shares scalarValue's whitelist
// but applies the line rule to the origin trimmed at *both* ends, because a
// leading newline in an element's origin is structure rather than content: it
// means the element began on a new line, which is true of every block item and
// of a flow sequence wrapped across lines. Trimming only the right, as
// scalarValue does, would refuse "[a,\n  b]" for no reason.
//
// What it still refuses is an element whose own value runs past its line -- a
// plain scalar continued on the next line, or a multi-line quoted one -- for
// the same reason scalarValue does.
func elementValue(key string, n ast.Node) (string, error) {
	if strings.Contains(strings.TrimSpace(n.GetToken().Origin), "\n") {
		return "", fmt.Errorf("frontmatter %q must be a single-line scalar; "+
			"a list element split over several lines is not supported", key)
	}
	return plainScalar(key, n)
}

// sequenceValue reads a mapping value as a list of strings. Both YAML spellings
// are accepted -- the flow form "[a, b]" and the block form of "- a" lines --
// since goccy parses both correctly and a block list survives Normalize intact,
// so refusing one would mean rejecting a file that was understood perfectly.
// What the style does decide is how a rewrite is emitted; see setField.
//
// The element index is carried into the key so a message points at the item
// that is wrong rather than at the field.
func sequenceValue(key string, n *ast.SequenceNode) ([]string, error) {
	out := make([]string, 0, len(n.Values))
	for i, e := range n.Values {
		s, err := elementValue(fmt.Sprintf("%s[%d]", key, i), e)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// spansLines reports whether a token's source text runs past its own line.
//
// This is what enforces the single-line half of the contract, and it has to be
// enforced at read time rather than trusted: an untouched key is re-emitted
// from the node the parser produced, and goccy's re-emission of a parsed node
// is not identity.
//
// What that costs differs by shape, measured against the pinned goccy rather
// than assumed. A "|" or ">" block is the one that breaks outright -- it
// re-emits as a block, which plainScalar's whitelist then refuses, so a write
// would produce a file markfluence cannot read, in create only after the page
// had been made. A plain scalar continued on the next line and a multi-line
// quoted one both re-emit folded onto one line, which parses but silently
// rewrites the author's file. Neither is something to do on the way past while
// setting some unrelated field, so both are refused up front.
//
// Trailing newlines and spaces are stripped first because a token's origin runs
// up to the next one, so even `title: T` carries the line break that follows it.
// A value markfluence wrote is never affected: a newline inside one is emitted
// as a two-character \n escape inside a double-quoted scalar, which occupies a
// single physical line.
func spansLines(origin string) bool {
	return strings.Contains(strings.TrimRight(origin, "\n\t "), "\n")
}

// toMaps reads a mapping into the two maps every caller uses: scalars by key,
// and sequences by key.
//
// A sequence-valued key is absent from the scalar map rather than present in
// some flattened spelling. Leaving it there would be worse than absent --
// MarkdownFile.field would hand update a title of "[a, b]" -- and re-typing the
// scalar map to hold both would touch every caller for no gain. The parser
// learns a kind, not a name: nothing here knows which keys are lists.
func toMaps(m *ast.MappingNode) (map[string]string, map[string][]string, error) {
	fm := make(map[string]string, len(m.Values))
	lists := map[string][]string{}
	for _, v := range m.Values {
		key := v.Key.GetToken().Value
		if seq, ok := v.Value.(*ast.SequenceNode); ok {
			if scalarFields[key] {
				return nil, nil, fmt.Errorf(
					"frontmatter %q must be a single value, not a list", key)
			}
			l, err := sequenceValue(key, seq)
			if err != nil {
				return nil, nil, err
			}
			lists[key] = l
			continue
		}
		s, err := scalarValue(key, v.Value)
		if err != nil {
			return nil, nil, err
		}
		fm[key] = s
	}
	return fm, lists, nil
}

// --- writing ------------------------------------------------------------------

// isDigits reports whether s is one or more ASCII digits. Local rather than
// internal/pageref's copy, which cannot be imported: pageref reads frontmatter.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// doubleQuoted builds a double-quoted scalar. The origin is left empty so goccy
// renders and escapes it from the value; hand-escaping would be one more thing
// to get wrong.
func doubleQuoted(value string) ast.Node {
	t := token.New(value, "", pos())
	t.Type = token.DoubleQuoteType
	return ast.String(t)
}

// valueNodeFor builds the node to write for key = value.
//
// page_id and parent are typed: digits become an integer and the unset
// spellings become a null, so the emitted YAML says what it means rather than
// `page_id: "123"`. Everything else is a string in whatever style goccy picks,
// verified by round-tripping it and falling back to a double-quoted scalar when
// goccy's choice does not read back.
//
// Double quotes are the last resort, so there is nothing to fall back to if
// they fail too and no error is returned; the fuzz test is what guards that,
// and no probed input has ever reached it.
func valueNodeFor(key, value string) ast.Node {
	if typedFields[key] {
		switch {
		case value == "" || value == "null":
			return ast.Null(token.New("null", "null", pos()))
		case isDigits(value):
			return ast.Integer(token.New(value, value, pos()))
		}
	}
	n, err := yaml.ValueToNode(value)
	if err != nil || !readsBackAs(n, value) {
		return doubleQuoted(value)
	}
	return n
}

// readsBackAs emits n as the only value of a one-key mapping, re-parses it, and
// reports whether it survived as a string holding want.
//
// Requiring a *string* is the point, not a detail. scalarValue flattens every
// scalar kind to its token text, so comparing text alone says ".inf" round-trips
// -- markfluence reads it back as ".inf" either way. But `title: .inf` is a
// float to any conforming reader, which is the #130 class all over again. The
// node kind is the only thing that distinguishes "we wrote a string" from "we
// wrote something that happens to spell the same".
func readsBackAs(n ast.Node, want string) bool {
	m := emptyMapping()
	m.Values = append(m.Values, ast.MappingValue(token.New("", "", pos()),
		ast.String(token.New("v", "v", pos())), n))
	f, err := parser.ParseBytes([]byte(m.String()+"\n"), 0)
	if err != nil || len(f.Docs) == 0 {
		return false
	}
	parsed, ok := f.Docs[0].Body.(*ast.MappingNode)
	if !ok || len(parsed.Values) != 1 {
		return false
	}
	if _, ok := parsed.Values[0].Value.(*ast.StringNode); !ok {
		return false
	}
	got, err := scalarValue("v", parsed.Values[0].Value)
	return err == nil && got == want
}

// seqIndentColumn is the column block sequence items are emitted at, which
// renders them as "  - item". A parsed node's own indentation is not
// reproduced: valid YAML in the right style is the contract, matching an
// author's byte-for-byte indent is not.
const seqIndentColumn = 3

// sequenceNodeFor builds the node to write for a list-valued key, in the given
// style. Block items are emitted at seqIndentColumn; with a default position
// they would render flush against the margin, which is valid YAML but not the
// convention anyone writes.
func sequenceNodeFor(values []string, flow bool) ast.Node {
	at := pos()
	if !flow {
		at = &token.Position{Line: 1, Column: seqIndentColumn}
	}
	seq := ast.Sequence(token.New("", "", at), flow)
	for _, v := range values {
		seq.Values = append(seq.Values, elementNodeFor(v, flow))
	}
	return seq
}

// elementNodeFor builds one sequence element: goccy's chosen style when it
// reads back, else a double-quoted scalar. valueNodeFor's sibling, minus the
// typed-field rule, which is confined to page_id and parent and neither is a
// list.
func elementNodeFor(value string, flow bool) ast.Node {
	n, err := yaml.ValueToNode(value)
	if err != nil || !readsBackInSeqAs(n, value, flow) {
		return doubleQuoted(value)
	}
	return n
}

// readsBackInSeqAs emits n as the only element of a sequence in the style about
// to be written, re-parses it, and reports whether it survived as a string
// holding want.
//
// A sequence needs its own check rather than readsBackAs': a value can read
// back perfectly as a *mapping* value and still be wrong in a list. Measured
// against the pinned goccy, "x,y" and "has]bracket" both pass readsBackAs, but
// emitted bare into a flow sequence the first becomes two elements and the
// second ends the sequence outright. For labels that means publishing two
// labels where the author wrote one -- the exact defect #138's validation
// exists to prevent, arriving from the writer instead of the server.
//
// The style parameter is about minimal quoting, not correctness. Flow is the
// stricter of the two contexts -- a comma and a bracket are significant there
// and inert in block form -- so verifying in flow would be safe for both and
// merely over-quote a block list. Verifying in *block* while writing flow is
// the direction that corrupts, and is what TestSequenceWriteThenReadRoundTrips
// pins by running the hazard corpus through both styles.
//
// Requiring a *string* back is the same point readsBackAs makes: comparing text
// alone says ".inf" round-trips, and block style hands it back as an Infinity
// node that every conforming reader sees as a float.
func readsBackInSeqAs(n ast.Node, want string, flow bool) bool {
	m := emptyMapping()
	seq := ast.Sequence(token.New("", "", pos()), flow)
	seq.Values = append(seq.Values, n)
	m.Values = append(m.Values, ast.MappingValue(token.New("", "", pos()),
		ast.String(token.New("v", "v", pos())), seq))
	f, err := parser.ParseBytes([]byte(m.String()+"\n"), 0)
	if err != nil || len(f.Docs) == 0 {
		return false
	}
	pair := soleMappingPair(f.Docs[0].Body)
	if pair == nil {
		return false
	}
	parsed, ok := pair.Value.(*ast.SequenceNode)
	if !ok || len(parsed.Values) != 1 {
		return false
	}
	if _, ok := parsed.Values[0].(*ast.StringNode); !ok {
		return false
	}
	got, err := elementValue("v[0]", parsed.Values[0])
	return err == nil && got == want
}

// soleMappingPair returns the one key/value pair of a single-pair document, or
// nil. goccy renders a one-key mapping as a MappingValueNode in some shapes and
// a MappingNode in others, so a type assertion on either alone silently reports
// "did not read back" and demotes every value to double quotes.
func soleMappingPair(n ast.Node) *ast.MappingValueNode {
	switch b := n.(type) {
	case *ast.MappingValueNode:
		return b
	case *ast.MappingNode:
		if len(b.Values) == 1 {
			return b.Values[0]
		}
	}
	return nil
}

// commentGroup builds a trailing "# text" comment.
func commentGroup(text string) *ast.CommentGroupNode {
	return ast.CommentGroup([]*token.Token{token.New(" "+text, "# "+text, pos())})
}

// nodeFor builds f's value node: a sequence when f is a list, else a scalar.
func nodeFor(f Field, flow bool) ast.Node {
	if f.List != nil {
		return sequenceNodeFor(f.List, flow)
	}
	return valueNodeFor(f.Key, f.Value)
}

// valueWithComment builds a value node carrying an optional trailing comment.
// The comment goes on the value node: set on the enclosing pair it renders as a
// full-line comment above the key instead.
func valueWithComment(f Field, flow bool) ast.Node {
	v := nodeFor(f, flow)
	if f.Comment != "" {
		_ = v.SetComment(commentGroup(f.Comment))
	}
	return v
}

// mappingValue builds one `key: value` pair.
func mappingValue(f Field, flow bool) *ast.MappingValueNode {
	return ast.MappingValue(token.New("", "", pos()),
		ast.String(token.New(f.Key, f.Key, pos())), valueWithComment(f, flow))
}

// Field is one frontmatter entry for Render.
//
// List, when non-nil, makes the field a YAML sequence and Value is ignored.
// Non-nil rather than non-empty, so an empty list renders "key: []" -- which is
// a meaningful declaration for a field like labels, where it means "remove
// them all" -- while a nil list stays a scalar.
type Field struct {
	Key     string
	Value   string
	Comment string
	List    []string
}

// Render builds a frontmatter block from scratch, in canonical order,
// delimiters included. It cannot fail: it constructs nodes and never parses a
// caller's text.
//
// Separate from UpdateField because building and editing are different jobs and
// only editing can fail. Chaining UpdateField to build a five-field block would
// mean parsing and re-emitting it five times, and would push a parse error into
// pagedoc, read, and export, none of which have anything to do with it. A test
// pins that the two agree on the same fields.
func Render(fields []Field) string {
	// Last write wins, so a repeated key cannot emit a duplicate the parser
	// would then reject. No caller repeats one; this is what makes "cannot fail"
	// true rather than true-for-well-behaved-input.
	seen := make(map[string]int, len(fields))
	ordered := make([]Field, 0, len(fields))
	for _, f := range fields {
		if i, dup := seen[f.Key]; dup {
			ordered[i] = f
			continue
		}
		seen[f.Key] = len(ordered)
		ordered = append(ordered, f)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return keyLess(ordered[i].Key, ordered[j].Key) })

	m := emptyMapping()
	for _, f := range ordered {
		// Flow style for a block built from scratch: there is no author choice
		// to honour here, and a generated list is short.
		m.Values = append(m.Values, mappingValue(f, true))
	}
	if len(m.Values) == 0 {
		return "---\n---\n"
	}
	return "---\n" + m.String() + "\n---\n"
}

// UpdateField adds or updates key in content's frontmatter, returning the new
// content. With no frontmatter block one is created at the top.
//
// The edit is surgical: an existing key keeps its position and only its value
// node is replaced, and a new key is inserted before the first key that sorts
// after it. Nothing else moves, so a file in git gets the smallest diff the
// emitter can produce -- though not a literally minimal one, since re-emitting
// normalizes intra-line whitespace and can move a blank line, which lives in the
// preceding value's token rather than in a node of its own.
//
// The value node is replaced rather than mutated in place. Mutating a plain
// token's value re-emits it unquoted whatever it now contains, which is exactly
// the bug this package was rewritten to fix.
func UpdateField(content, key, value, comment string) (string, error) {
	return updateField(content, Field{Key: key, Value: value, Comment: comment})
}

// UpdateListField adds or updates key in content's frontmatter as a YAML
// sequence, returning the new content.
//
// A key that already holds a sequence keeps its style: rewriting a block list
// emits a block list. That is a deliberate contract -- a set large enough to be
// written as a block list is exactly the set whose flow spelling is an
// unreadable single line -- but it is style only, not formatting: the items are
// re-emitted at the package's own indent rather than the author's.
func UpdateListField(content, key string, values []string) (string, error) {
	return updateField(content, Field{Key: key, List: values})
}

func updateField(content string, f Field) (string, error) {
	loc := frontmatterRE.FindStringSubmatchIndex(content)
	if loc == nil {
		return Render([]Field{f}) + content, nil
	}
	b, err := parseBlock(content[loc[2]:loc[3]])
	if err != nil {
		return "", err
	}
	setField(b, f)
	return "---\n" + b.mapping.String() + "\n---\n" + content[loc[1]:], nil
}

// setField replaces or inserts f's key in b's mapping.
func setField(b *block, f Field) {
	// An existing key keeps its own key node, not just its position: a blank
	// line before it lives in that node's token origin, so swapping the whole
	// pair would silently delete it. Only the value is replaced -- and replaced,
	// never mutated, since mutating a plain token re-emits it unquoted whatever
	// it now holds.
	for _, v := range b.mapping.Values {
		if v.Key.GetToken().Value == f.Key {
			v.Value = valueWithComment(f, existingSeqIsFlow(v.Value))
			return
		}
	}
	mv := mappingValue(f, true)
	// A comment that had no key to attach to rides along with the first key
	// added, rather than being dropped on the first write.
	if b.orphan != nil && len(b.mapping.Values) == 0 {
		_ = mv.SetComment(b.orphan)
		b.orphan = nil
	}
	at := len(b.mapping.Values)
	for i, v := range b.mapping.Values {
		if keyLess(f.Key, v.Key.GetToken().Value) {
			at = i
			break
		}
	}
	b.mapping.Values = append(b.mapping.Values, nil)
	copy(b.mapping.Values[at+1:], b.mapping.Values[at:])
	b.mapping.Values[at] = mv
}

// existingSeqIsFlow reports the style to write a replacement sequence in: the
// style the value being replaced already had, defaulting to flow for anything
// that was not a sequence (a new list, or one replacing a scalar).
func existingSeqIsFlow(current ast.Node) bool {
	if seq, ok := current.(*ast.SequenceNode); ok {
		return seq.IsFlowStyle
	}
	return true
}

// Normalize rewrites content's frontmatter in canonical field order, reporting
// whether anything moved.
//
// It is a no-op when the order already holds, which is what makes the reported
// boolean mean exactly "keys moved" and keeps the blank-line handling
// predictable: blank lines are dropped only as a consequence of a real reorder,
// never as a side effect of some unrelated field being written. A canonical
// file keeps its blank lines -- this normalizes ordering, it is not a formatter.
func Normalize(content string) (string, bool, error) {
	loc := frontmatterRE.FindStringSubmatchIndex(content)
	if loc == nil {
		return content, false, nil
	}
	b, err := parseBlock(content[loc[2]:loc[3]])
	if err != nil {
		return "", false, err
	}
	if isCanonical(b.mapping) {
		return content, false, nil
	}
	sort.SliceStable(b.mapping.Values, func(i, j int) bool {
		return keyLess(b.mapping.Values[i].Key.GetToken().Value,
			b.mapping.Values[j].Key.GetToken().Value)
	})
	return "---\n" + dropBlankLines(b.mapping.String()) + "\n---\n" + content[loc[1]:], true, nil
}

// isCanonical reports whether a mapping's keys are already in keyLess order.
func isCanonical(m *ast.MappingNode) bool {
	for i := 1; i < len(m.Values); i++ {
		if keyLess(m.Values[i].Key.GetToken().Value, m.Values[i-1].Key.GetToken().Value) {
			return false
		}
	}
	return true
}

// dropBlankLines removes blank lines from an emitted block.
//
// Textual rather than structural because a blank line is not a node: it lives in
// the preceding value's token origin, so reordering carries it to a position
// that means nothing.
//
// Safe as a text filter because no value this package can emit carries a
// *meaningful* blank line. A scalar holding a newline is written as a
// double-quoted scalar with an escape, on one physical line. A block sequence
// does span lines, and a blank line between two of its items is inert in YAML,
// so dropping it changes nothing but the diff. The shape this would corrupt is
// a "|" block, where a blank line is content -- and that is refused on read,
// which is what keeps this filter honest.
func dropBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// --- MarkdownFile ---------------------------------------------------------------

// MarkdownFile is a markdown source file parsed once: its path, raw text,
// frontmatter maps, and body (content with the frontmatter block stripped).
//
// Frontmatter holds the scalar fields and Lists the sequence-valued ones; a key
// appears in exactly one of them. Both are exported, and both are non-nil even
// for a file with no frontmatter at all, so callers that must distinguish
// absent from present-but-blank (e.g. the fix command) can read them directly.
// The accessor methods provide normalized reads: every null spelling and a
// blank value alike read as "".
type MarkdownFile struct {
	Filename    string
	Content     string
	Frontmatter map[string]string
	Lists       map[string][]string
	Body        string
}

// Parse builds a MarkdownFile from an in-memory content string tagged with
// filename, or reports why the frontmatter could not be read.
func Parse(filename, content string) (*MarkdownFile, error) {
	loc := frontmatterRE.FindStringSubmatchIndex(content)
	if loc == nil {
		if strings.HasPrefix(content, "---\n") {
			return nil, ErrUnterminatedFrontmatter
		}
		return &MarkdownFile{
			Filename: filename, Content: content,
			Frontmatter: map[string]string{}, Lists: map[string][]string{}, Body: content,
		}, nil
	}
	b, err := parseBlock(content[loc[2]:loc[3]])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	fm, lists, err := toMaps(b.mapping)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	return &MarkdownFile{
		Filename: filename, Content: content, Frontmatter: fm, Lists: lists,
		Body: content[loc[1]:],
	}, nil
}

// ParseFile reads filename from disk and parses it.
func ParseFile(filename string) (*MarkdownFile, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return Parse(filename, string(data))
}

// field reads a frontmatter field, treating whitespace-only as unset. Every
// null spelling already read as "" at parse time.
func (m *MarkdownFile) field(key string) string {
	return strings.TrimSpace(m.Frontmatter[key])
}

// Title returns the title, "" if missing, blank, or null.
func (m *MarkdownFile) Title() string { return m.field("title") }

// TitleField reports the title and whether a title key was present at all.
//
// The two differ where it matters: an absent title is a positive statement that
// a file does not manage its page's title, which update honours by keeping the
// live one, while a present-but-empty title is a half-finished edit that should
// not silently publish under whatever the page is called now.
func (m *MarkdownFile) TitleField() (title string, present bool) {
	raw, present := m.Frontmatter["title"]
	return strings.TrimSpace(raw), present
}

// PageID returns the page id, "" if missing, blank, or null.
func (m *MarkdownFile) PageID() string { return m.field("page_id") }

// Space returns the space key, "" if missing, blank, or null.
func (m *MarkdownFile) Space() string { return m.field("space") }

// Parent returns the parent, "" if missing, blank, or null.
func (m *MarkdownFile) Parent() string { return m.field("parent") }
