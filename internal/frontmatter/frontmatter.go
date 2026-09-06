// Package frontmatter parses and rewrites the YAML frontmatter block that
// markfluence markdown files carry, and models a parsed file as a MarkdownFile.
//
// The block is real YAML, parsed and emitted by goccy/go-yaml. It is still
// restricted to flat key: value pairs -- no nesting, lists, or multiline
// values -- but that restriction is now enforced by scalarValue rather than
// assumed by a line-splitting parser that could not see a violation.
//
// Writes go through valueNodeFor, which verifies its own output: it emits with
// goccy's chosen style, re-reads the result, and falls back to a double-quoted
// scalar when the two disagree. goccy's default is wrong for a handful of
// shapes -- a tab is dropped, a value starting "? " produces a document goccy
// itself refuses to parse -- and a hand-written predicate listing them would be
// incomplete, since those cases turned up only by probing. Checking beats
// predicting.
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

// scalarValue reads a mapping value as a string. It is a whitelist: every other
// node kind, including an anchor, an alias, a tag, and a "|" literal block,
// reports GetToken().Value as the indicator character rather than the content,
// so a blacklist of sequences and mappings would silently read "&" or "|".
//
// Every spelling of null -- an absent value, "null", "~", "Null" -- reads as
// "", so a null is unset whatever the author wrote. The old parser mapped only
// the literal "null", which meant "parent: ~" read as though it were a page id.
func scalarValue(key string, n ast.Node) (string, error) {
	if spansLines(n.GetToken().Origin) {
		return "", fmt.Errorf("frontmatter %q must be a single-line scalar; "+
			"a value split over several lines is not supported", key)
	}
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

// spansLines reports whether a token's source text runs past its own line.
//
// This is what enforces the "no multiline values" half of the flat contract,
// and it has to be enforced at read time rather than trusted: an untouched key
// is re-emitted from the node the parser produced, and goccy's re-emission of a
// parsed node is not identity. A plain scalar continued on the next line comes
// back as a "|-" block, which Parse then refuses -- so UpdateField would write a
// file it cannot read, after create had already made the page. A multi-line
// single-quoted scalar is worse: it re-emits on one line, silently turning
// "sq\nline" into "sq line".
//
// Trailing newlines and spaces are stripped first because a token's origin runs
// up to the next one, so even `title: T` carries the line break that follows it.
// A value markfluence wrote is never affected: a newline inside one is emitted
// as a two-character \n escape inside a double-quoted scalar, which occupies a
// single physical line.
func spansLines(origin string) bool {
	return strings.Contains(strings.TrimRight(origin, "\n\t "), "\n")
}

// toMap reads a mapping into the flat key->value map every caller uses.
func toMap(m *ast.MappingNode) (map[string]string, error) {
	fm := make(map[string]string, len(m.Values))
	for _, v := range m.Values {
		key := v.Key.GetToken().Value
		s, err := scalarValue(key, v.Value)
		if err != nil {
			return nil, err
		}
		fm[key] = s
	}
	return fm, nil
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

// commentGroup builds a trailing "# text" comment.
func commentGroup(text string) *ast.CommentGroupNode {
	return ast.CommentGroup([]*token.Token{token.New(" "+text, "# "+text, pos())})
}

// valueWithComment builds a value node carrying an optional trailing comment.
// The comment goes on the value node: set on the enclosing pair it renders as a
// full-line comment above the key instead.
func valueWithComment(key, value, comment string) ast.Node {
	v := valueNodeFor(key, value)
	if comment != "" {
		_ = v.SetComment(commentGroup(comment))
	}
	return v
}

// mappingValue builds one `key: value` pair.
func mappingValue(key, value, comment string) *ast.MappingValueNode {
	return ast.MappingValue(token.New("", "", pos()),
		ast.String(token.New(key, key, pos())), valueWithComment(key, value, comment))
}

// Field is one frontmatter entry for Render.
type Field struct {
	Key     string
	Value   string
	Comment string
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
		m.Values = append(m.Values, mappingValue(f.Key, f.Value, f.Comment))
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
	loc := frontmatterRE.FindStringSubmatchIndex(content)
	if loc == nil {
		return Render([]Field{{Key: key, Value: value, Comment: comment}}) + content, nil
	}
	b, err := parseBlock(content[loc[2]:loc[3]])
	if err != nil {
		return "", err
	}
	setField(b, key, value, comment)
	return "---\n" + b.mapping.String() + "\n---\n" + content[loc[1]:], nil
}

// setField replaces or inserts key in b's mapping.
func setField(b *block, key, value, comment string) {
	// An existing key keeps its own key node, not just its position: a blank
	// line before it lives in that node's token origin, so swapping the whole
	// pair would silently delete it. Only the value is replaced -- and replaced,
	// never mutated, since mutating a plain token re-emits it unquoted whatever
	// it now holds.
	for _, v := range b.mapping.Values {
		if v.Key.GetToken().Value == key {
			v.Value = valueWithComment(key, value, comment)
			return
		}
	}
	mv := mappingValue(key, value, comment)
	// A comment that had no key to attach to rides along with the first key
	// added, rather than being dropped on the first write.
	if b.orphan != nil && len(b.mapping.Values) == 0 {
		_ = mv.SetComment(b.orphan)
		b.orphan = nil
	}
	at := len(b.mapping.Values)
	for i, v := range b.mapping.Values {
		if keyLess(key, v.Key.GetToken().Value) {
			at = i
			break
		}
	}
	b.mapping.Values = append(b.mapping.Values, nil)
	copy(b.mapping.Values[at+1:], b.mapping.Values[at:])
	b.mapping.Values[at] = mv
}

// --- MarkdownFile ---------------------------------------------------------------

// MarkdownFile is a markdown source file parsed once: its path, raw text,
// frontmatter map, and body (content with the frontmatter block stripped).
//
// Frontmatter is exported so callers that must distinguish absent from
// present-but-blank (e.g. the fix command) can read it directly. The accessor
// methods provide normalized reads: every null spelling and a blank value alike
// read as "".
type MarkdownFile struct {
	Filename    string
	Content     string
	Frontmatter map[string]string
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
			Frontmatter: map[string]string{}, Body: content,
		}, nil
	}
	b, err := parseBlock(content[loc[2]:loc[3]])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	fm, err := toMap(b.mapping)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	return &MarkdownFile{
		Filename: filename, Content: content, Frontmatter: fm, Body: content[loc[1]:],
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
