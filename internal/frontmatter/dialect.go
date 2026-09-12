package frontmatter

// markfluence's YAML dialect: how a mapping of scalars and sequences is read,
// and what is refused.
//
// The fenced frontmatter block is one use of this, not the only one --
// markfluence.yaml is read by internal/project through ReadMapping (#100).
// Every rule here was found by probing the pinned goccy rather than reasoned
// about, which is why a second copy of them for a second file would be a
// second set of the same bugs: the scalar node-kind whitelist (plainScalar),
// the single-line rule (spansLines), every null spelling reading as "", and
// the refusal of anything that is not a flat mapping -- or, where a dialect
// asks for depth (Dialect.MaxDepth, #139), of anything past it.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// Dialect names the document a diagnostic is about, so one copy of the reader
// can report for two different files without either borrowing the other's
// wording.
//
// Doc names the document as a whole ("frontmatter", "a project file") and Item
// names one of its keys ("frontmatter", "setting"). They are separate because
// the two kinds of message read differently: `frontmatter must be a flat
// mapping` and `setting "space" must be a single scalar value` are both right,
// and neither noun works in the other's sentence.
//
// Item goes only where a *named* key is the subject. A sentence about keys in
// general says "key" literally, which is why the flat-mapping refusal takes
// neither field for that word.
type Dialect struct {
	Doc  string
	Item string
	// MaxDepth is how many levels of nested mapping a value may hold. Zero --
	// the default, and what the fenced block keeps -- means flat: a mapping
	// value is refused exactly as any other non-scalar is.
	//
	// A depth rather than a list of keys allowed to nest, so the reader still
	// learns a kind and not a name: markfluence.yaml's `pages:` needs two
	// levels (pages -> path -> fields) and says so by asking for two, and a
	// mapping under a key whose own table says scalar is refused by that table
	// with its own message rather than by the reader (#139).
	MaxDepth int
}

// Item is one key/value pair of a mapping, in source order.
//
// Exactly one of Value, List and Map describes the value, and the nil-ness of
// the latter two is what says which: List is nil for a scalar and non-nil
// (possibly empty) for a sequence, which is what distinguishes `labels: []`
// from an absent key, and Map is non-nil (possibly empty) for a nested
// mapping, which only arrives within Dialect.MaxDepth. A caller reading
// len(List) or len(Map) instead of testing for nil cannot tell an empty
// collection from a scalar.
//
// Line is 1-based within the text passed to ReadMapping, and is carried
// because a caller rejecting a key -- an unknown setting in a project file --
// has to be able to say where it is.
type Item struct {
	Key   string
	Line  int
	Value string
	List  []string
	Map   []Item
}

// ReadMapping reads text as a single flat YAML mapping, applying every rule
// this file describes. An empty document and a comment-only one are both empty
// mappings rather than errors, since neither is invalid YAML -- and for
// markfluence.yaml that is the shape that ships today.
//
// It reports items in source order rather than as a map, because a caller
// validating keys needs their positions and a map would lose them.
func (d Dialect) ReadMapping(text string) ([]Item, error) {
	r := reader{Dialect: d}
	b, err := r.parse(text)
	if err != nil {
		return nil, err
	}
	return r.items(b.mapping, 0)
}

// reader is the dialect plus the two things that differ per document and are
// nobody else's business.
//
// scalarOnly names keys that must not hold a sequence, which is frontmatter's
// own concern (a `parent: [x]` read as absent published at the space root and
// wrote `parent: null` over the author's intent). fixup adjusts a parse
// error's text; frontmatter's block excludes the "---" opener, so its
// positions are one line short, and a whole file needs no correction at all.
type reader struct {
	Dialect
	scalarOnly map[string]bool
	fixup      func(string) string
}

// blockReader reads a fenced frontmatter block. Package-level because the
// write-side verification helpers (readsBackAs, readsBackInSeqAs) re-read a
// node they are about to emit and discard the message, so threading a reader
// to them would carry only the parts they ignore.
var blockReader = reader{
	Dialect:    Dialect{Doc: "frontmatter", Item: "frontmatter"},
	scalarOnly: scalarFields,
	fixup:      shiftLeadingPosition,
}

// parse parses a document's text into a flat mapping. Any other shape (a bare
// scalar, a top-level list) is an error.
func (r reader) parse(text string) (*block, error) {
	f, err := parser.ParseBytes([]byte(text), parser.ParseComments)
	if err != nil {
		return nil, r.parseError(err)
	}
	// A "..." line starts a second document, and reading only the first would
	// drop every key after it without a word: `update` would then report "no
	// page id" about a file that visibly has one.
	if len(f.Docs) > 1 {
		return nil, fmt.Errorf(`%s must be a single document: remove the "..." line`, r.Doc)
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
		// "key: value pairs" is literal, not r.Item: Item names one key in a
		// sentence about that key ("setting \"space\" must be ..."), and reads
		// as nonsense here -- this said "a flat mapping of frontmatter: value
		// pairs" before a test pinned the whole sentence.
		return nil, fmt.Errorf("%s must be a flat mapping of key: value pairs, found %s",
			r.Doc, b.Type())
	}
}

// parseError reduces a goccy error to a single line and applies the reader's
// position correction.
//
// goccy's default Error() renders a multi-line source excerpt with ASCII
// pointer art, which would land verbatim in check --json's error string.
// Known limit: a duplicate-key message embeds a second position ("already
// defined at [1:1]") that no fixup touches.
func (r reader) parseError(err error) error {
	msg := yaml.FormatError(err, false, false)
	if r.fixup != nil {
		msg = r.fixup(msg)
	}
	return errors.New(msg)
}

// positionRE matches a leading "[line:col] " position stamp.
var positionRE = regexp.MustCompile(`^\[(\d+):(\d+)\] `)

// shiftLeadingPosition rewrites a leading [line:col] to account for the "---"
// line that opens a frontmatter block.
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

// scalar reads a mapping value as a string, rejecting anything that spans more
// than one line.
//
// Every spelling of null -- an absent value, "null", "~", "Null" -- reads as
// "", so a null is unset whatever the author wrote. The old parser mapped only
// the literal "null", which meant "parent: ~" read as though it were a page id.
func (r reader) scalar(key string, n ast.Node) (string, error) {
	if spansLines(n.GetToken().Origin) {
		return "", fmt.Errorf("%s %q must be a single-line scalar; "+
			"a value split over several lines is not supported", r.Item, key)
	}
	return r.plainScalar(key, n)
}

// plainScalar is the node-kind whitelist shared by scalar and element. It is a
// whitelist because every other node kind -- an anchor, an alias, a tag, a "|"
// literal block -- reports GetToken().Value as its indicator character rather
// than its content, so a blacklist of sequences and mappings would silently
// read "&" or "|". A "- |-" sequence element is the case that makes this
// load-bearing twice over: its token does not span lines, so the whitelist is
// the only thing that catches it.
func (r reader) plainScalar(key string, n ast.Node) (string, error) {
	switch v := n.(type) {
	case *ast.NullNode:
		return "", nil
	case *ast.StringNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode,
		*ast.InfinityNode, *ast.NanNode:
		return v.GetToken().Value, nil
	default:
		return "", fmt.Errorf("%s %q must be a single scalar value, found %s",
			r.Item, key, n.Type())
	}
}

// element reads one sequence element. It shares scalar's whitelist but applies
// the line rule to the origin trimmed at *both* ends, because a leading
// newline in an element's origin is structure rather than content: it means the
// element began on a new line, which is true of every block item and of a flow
// sequence wrapped across lines. Trimming only the right, as scalar does, would
// refuse "[a,\n  b]" for no reason.
//
// What it still refuses is an element whose own value runs past its line -- a
// plain scalar continued on the next line, or a multi-line quoted one -- for
// the same reason scalar does.
func (r reader) element(key string, n ast.Node) (string, error) {
	if strings.Contains(strings.TrimSpace(n.GetToken().Origin), "\n") {
		return "", fmt.Errorf("%s %q must be a single-line scalar; "+
			"a list element split over several lines is not supported", r.Item, key)
	}
	return r.plainScalar(key, n)
}

// sequence reads a mapping value as a list of strings. Both YAML spellings are
// accepted -- the flow form "[a, b]" and the block form of "- a" lines -- since
// goccy parses both correctly and a block list survives Normalize intact, so
// refusing one would mean rejecting a file that was understood perfectly. What
// the style does decide is how a rewrite is emitted; see setField.
//
// The element index is carried into the key so a message points at the item
// that is wrong rather than at the field.
func (r reader) sequence(key string, n *ast.SequenceNode) ([]string, error) {
	out := make([]string, 0, len(n.Values))
	for i, e := range n.Values {
		s, err := r.element(fmt.Sprintf("%s[%d]", key, i), e)
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

// items reads a mapping into its key/value pairs, in source order.
//
// depth is how far down this mapping already is, so a nested mapping is read
// only while there is allowance left. Past the allowance a mapping value falls
// through to r.scalar and is refused there, which is what keeps a flat dialect
// (MaxDepth 0) behaving exactly as it did before nesting existed -- including
// the message.
func (r reader) items(m *ast.MappingNode, depth int) ([]Item, error) {
	out := make([]Item, 0, len(m.Values))
	for _, v := range m.Values {
		key := v.Key.GetToken().Value
		it := Item{Key: key, Line: v.Key.GetToken().Position.Line}
		if seq, ok := v.Value.(*ast.SequenceNode); ok {
			if r.scalarOnly[key] {
				return nil, fmt.Errorf(
					"%s %q must be a single value, not a list", r.Item, key)
			}
			l, err := r.sequence(key, seq)
			if err != nil {
				return nil, err
			}
			it.List = l
			out = append(out, it)
			continue
		}
		if depth < r.MaxDepth {
			nested, ok, err := r.nested(v.Value, depth)
			if err != nil {
				return nil, err
			}
			if ok {
				it.Map = nested
				out = append(out, it)
				continue
			}
		}
		s, err := r.scalar(key, v.Value)
		if err != nil {
			return nil, err
		}
		it.Value = s
		out = append(out, it)
	}
	return out, nil
}

// nested reads a mapping value one level down, reporting whether the value was
// a mapping at all. It is not an error for it not to be: a nesting-capable
// dialect still holds plain scalars at every level, so a non-mapping falls back
// to the scalar path rather than being refused for the shape it does have.
//
// goccy renders a one-key mapping as a MappingValueNode and a multi-key one as
// a MappingNode, the same split soleMappingPair exists for on the write side,
// so both have to be accepted -- treating only the plural form as nesting would
// read a single-field entry as a broken scalar.
func (r reader) nested(n ast.Node, depth int) ([]Item, bool, error) {
	var m *ast.MappingNode
	switch v := n.(type) {
	case *ast.MappingNode:
		m = v
	case *ast.MappingValueNode:
		m = emptyMapping()
		m.Values = append(m.Values, v)
	default:
		return nil, false, nil
	}
	items, err := r.items(m, depth+1)
	if err != nil {
		return nil, false, err
	}
	return items, true, nil
}

// maps reads a mapping into the two maps frontmatter's own callers use: scalars
// by key, and sequences by key.
//
// A sequence-valued key is absent from the scalar map rather than present in
// some flattened spelling. Leaving it there would be worse than absent --
// MarkdownFile.field would hand update a title of "[a, b]" -- and re-typing the
// scalar map to hold both would touch every caller for no gain. The parser
// learns a kind, not a name: nothing here knows which keys are lists.
func (r reader) maps(m *ast.MappingNode) (map[string]string, map[string][]string, error) {
	items, err := r.items(m, 0)
	if err != nil {
		return nil, nil, err
	}
	fm := make(map[string]string, len(items))
	lists := map[string][]string{}
	for _, it := range items {
		if it.List != nil {
			lists[it.Key] = it.List
			continue
		}
		fm[it.Key] = it.Value
	}
	return fm, lists, nil
}
