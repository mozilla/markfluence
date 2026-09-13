package frontmatter

// Writing into a nested mapping -- markfluence.yaml's pages: block (#139),
// where internal/project keeps a file's page metadata.
//
// It lives here rather than in internal/project so that goccy stays confined to
// this package: the dialect, its refusals, and every rule about emitting a
// value that reads back as itself are one package's business, and a second
// package building nodes is how two spellings of "write a YAML scalar" come to
// disagree.

import (
	"fmt"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// indentColumn is the column a key at depth d (1-based) must be emitted at.
//
// Measured, not guessed, and it is the sharpest trap in this file: a node's
// *column* is what goccy indents by, so a mapping value built at the default
// position (column 1) is emitted at the left margin however deep it was
// appended -- which makes `space: ENG` appended inside a pages: entry come out
// as a top-level setting instead. That is valid YAML saying something entirely
// different, so nothing downstream would refuse it: the loader would read a
// project-wide space nobody wrote.
//
// Two spaces per level, so depth 1 is column 1, depth 2 column 3, depth 3
// column 5 -- matching seqIndentColumn's reasoning, which fixes the same
// problem for a block sequence's items.
func indentColumn(depth int) int { return 2*depth - 1 }

// childColumn returns the column a new key inside m must be emitted at: the
// column an existing child already uses, or the two-space default when m is
// empty.
//
// Taking it from a sibling rather than computing it is what lets a project file
// indented some other way be written to at all. A constant refused any
// `pages:` block not indented by exactly two spaces -- a four-space file, which
// the loader accepts happily, could never be recorded into, so create made the
// page and reported a parse failure. And one-space indentation was worse: the
// output nested the new entry *inside* its predecessor, which verifyNested
// caught only because it checks depth rather than parseability.
func childColumn(m *ast.MappingNode, depth int) int {
	for _, v := range m.Values {
		if c := v.Key.GetToken().Position.Column; c > 0 {
			return c
		}
	}
	// Empty: use the mapping's *own* column, which is where whoever built it
	// meant its children to sit. Falling back to the constant here put a
	// just-created entry's first field at the same column as the entry key, so
	// it came out as the entry's *sibling* rather than its child -- a field
	// escaping one level, which is the same class of silent wrongness
	// indentColumn exists to prevent.
	if c := m.GetToken().Position.Column; c > 0 {
		return c
	}
	return indentColumn(depth)
}

func posAt(column int) *token.Position { return &token.Position{Line: 1, Column: column} }

// SetNested sets fields inside the mapping reached by path, creating any level
// of the path that does not exist yet, and returns the rewritten document.
//
// path is a sequence of keys from the document root: {"pages", "docs/a.md"}
// addresses that file's entry. An empty path sets fields at the top level.
//
// Like UpdateField, an existing key keeps its own key node so a blank line or
// comment living in that node's origin survives, and only its value is
// replaced. New keys are inserted in canonical order (keyLess), so an entry's
// fields read in the same order frontmatter itself writes them.
//
// The result is re-read and verified before it is returned (see verifyNested),
// because the failure mode here is a document that parses perfectly and says
// the wrong thing.
func SetNested(content string, path []string, fields []Field) (string, error) {
	b, err := docReader.parse(content)
	if err != nil {
		return "", err
	}
	target := b.mapping
	for i, key := range path {
		next, err := childMapping(target, key, i+1)
		if err != nil {
			return "", err
		}
		target = next
	}
	depth := len(path) + 1
	for _, f := range fields {
		setNestedField(target, f, depth)
	}
	// A document that held only comments has an empty mapping, and its orphan
	// comment rides along with the first key written -- the same courtesy
	// setField extends a frontmatter block, and it matters more here: the
	// marker file ships as a comment and nothing else.
	if b.orphan != nil && len(b.mapping.Values) > 0 {
		_ = b.mapping.Values[0].SetComment(b.orphan)
		b.orphan = nil
	}

	out := b.mapping.String()
	if out != "" {
		out += "\n"
	}
	if err := verifyNested(out, path, fields); err != nil {
		return "", err
	}
	return out, nil
}

// childMapping returns the mapping under key, creating it when absent. depth is
// key's own depth, 1-based.
func childMapping(parent *ast.MappingNode, key string, depth int) (*ast.MappingNode, error) {
	for _, v := range parent.Values {
		if v.Key.GetToken().Value != key {
			continue
		}
		// The child column comes from *this* key's real position, never from
		// an absolute depth: a document whose root mapping is itself indented
		// ("  pages: {}") loads fine, and computing from depth then emitted the
		// entry at pages:' own column.
		childCol := v.Key.GetToken().Position.Column + 2
		switch m := v.Value.(type) {
		case *ast.MappingNode:
			toBlock(m, childCol)
			return m, nil
		case *ast.MappingValueNode:
			// goccy renders a one-pair mapping as a MappingValueNode, so an
			// entry with a single field arrives in the other shape. Promoting it
			// keeps the rest of this function working on one type; the promoted
			// node carries the original pair, so nothing is lost.
			promoted := ast.Mapping(token.New("", "", posAt(childCol)), false)
			promoted.Values = append(promoted.Values, m)
			v.Value = promoted
			return promoted, nil
		case *ast.NullNode:
			// `pages:` with nothing after it. Writing into it is what the
			// caller asked for, and an empty mapping is what it meant.
			created := ast.Mapping(token.New("", "", posAt(childCol)), false)
			v.Value = created
			return created, nil
		default:
			return nil, fmt.Errorf("%q already holds a %s, not a mapping", key, v.Value.Type())
		}
	}
	col := childColumn(parent, depth)
	created := ast.Mapping(token.New("", "", posAt(col+2)), false)
	insertSorted(parent, ast.MappingValue(token.New("", "", posAt(col)),
		keyNode(key, col), created), key)
	return created, nil
}

// toBlock converts a flow mapping to block style, re-positioning it and
// everything under it for the depth it now sits at.
//
// `pages: {}` is a flow mapping, and it is exactly the shape a project that has
// chosen the manifest but registered nothing has. Appending a block entry to a
// flow mapping emits "pages: {\n  a.md:" and does not parse -- measured, and
// caught by verifyNested rather than reasoned about. A *populated* flow mapping
// needs its existing children moved too, since they carry the columns flow gave
// them; leaving them behind produced a different unparseable file.
//
// Style is deliberately not preserved, unlike a frontmatter sequence rewrite:
// a nested entry cannot be written in flow style without re-emitting the whole
// line, and block is the only readable style for a mapping of mappings. Valid
// YAML in a readable style is the contract; matching an author's flow braces is
// not.
func toBlock(m *ast.MappingNode, col int) {
	if !m.IsFlowStyle {
		return
	}
	m.IsFlowStyle = false
	// The mapping's own column too, not just its children's: a flow mapping's
	// token sits at the brace ("pages: {}" puts it at column 8), and
	// childColumn reads that column when the mapping is empty -- so leaving it
	// would put the first entry written into `pages: {}` at column 8.
	m.GetToken().Position.Column = col
	for _, v := range m.Values {
		v.Key.GetToken().Position.Column = col
		if inner, ok := v.Value.(*ast.MappingNode); ok {
			toBlock(inner, col+2)
		}
	}
}

// keyNode builds a mapping key, quoting it when a plain one would not read back
// as itself.
//
// Values have had this since #130 (valueNodeFor's readsBackAs fallback); keys
// had nothing, so a legitimate filename was refused rather than quoted. A page
// key is a path, and a path may hold any of YAML's indicators: `#x.md`,
// `- a.md`, `a: b.md`, `!a.md`, `*a.md`, `|a.md`, a tab, a leading space. Each
// of those made the rewritten file unparseable -- safely, since verifyNested
// refuses it, but with the page already created and nothing recording it.
//
// A key that merely *looks* like another type (`true.md` is not one, but a bare
// `true` would be) is quoted for the same reason a value is: markfluence reads
// the token text either way, and every other tool does not.
func keyNode(key string, column int) ast.MapKeyNode {
	plain := ast.String(token.New(key, key, posAt(column)))
	if keyReadsBackAs(plain, key) {
		return plain
	}
	// doubleQuoted leaves the origin empty so goccy escapes from the value.
	q := ast.String(token.New(key, `"`+key+`"`, posAt(column)))
	q.Token.Type = token.DoubleQuoteType
	q.Value = key
	return q
}

// keyReadsBackAs emits n as the key of a one-pair mapping, re-parses it, and
// reports whether it survived as a string holding want -- readsBackAs's rule,
// applied to the other side of the colon.
func keyReadsBackAs(n ast.MapKeyNode, want string) bool {
	m := emptyMapping()
	m.Values = append(m.Values, ast.MappingValue(token.New("", "", pos()),
		n, ast.String(token.New("v", "v", pos()))))
	f, err := parser.ParseBytes([]byte(m.String()+"\n"), 0)
	if err != nil || len(f.Docs) == 0 {
		return false
	}
	pair := soleMappingPair(f.Docs[0].Body)
	if pair == nil {
		return false
	}
	if _, ok := pair.Key.(*ast.StringNode); !ok {
		return false
	}
	return pair.Key.GetToken().Value == want
}

// setNestedField replaces or inserts f's key in m, emitting it at depth.
func setNestedField(m *ast.MappingNode, f Field, depth int) {
	for _, v := range m.Values {
		if v.Key.GetToken().Value == f.Key {
			v.Value = nestedValue(f)
			return
		}
	}
	col := childColumn(m, depth)
	insertSorted(m, ast.MappingValue(token.New("", "", posAt(col)),
		keyNode(f.Key, col), nestedValue(f)), f.Key)
}

// nestedValue builds f's value node.
//
// A sequence is always emitted **flow** here -- `labels: [a, b]` -- unlike a
// frontmatter rewrite, which preserves the style it found. A block sequence's
// items need an indent column of their own (seqIndentColumn), and inside a
// nested mapping that column depends on the mapping's depth, so preserving
// block style would mean a second place where a miscomputed column silently
// changes what the file says. Flow needs no indent at all, and an entry's only
// list is labels, which is short.
func nestedValue(f Field) ast.Node {
	if f.List != nil {
		return sequenceNodeFor(f.List, true)
	}
	// Through valueWithComment, so a Field.Comment is honoured here exactly as
	// Render and setField honour it. Dropping it was silent, and SetNested is
	// exported and takes []Field -- the next caller to set one (recording the
	// original parent: path the way parentField does for frontmatter) would
	// have lost it with nothing said.
	return valueWithComment(f, true)
}

// insertSorted puts mv into m at the position canonical order wants for key.
func insertSorted(m *ast.MappingNode, mv *ast.MappingValueNode, key string) {
	at := len(m.Values)
	for i, v := range m.Values {
		if keyLess(key, v.Key.GetToken().Value) {
			at = i
			break
		}
	}
	m.Values = append(m.Values, nil)
	copy(m.Values[at+1:], m.Values[at:])
	m.Values[at] = mv
}

// verifyNested re-reads a written document and confirms every field landed at
// path holding what was asked for.
//
// This is the write-then-re-read discipline valueNodeFor already applies to a
// single scalar, raised a level -- and it is load-bearing rather than
// belt-and-braces. The failure this file's indentColumn comment describes
// produces a document that parses cleanly and reads back with the field at the
// *wrong depth*, so "did it parse" proves nothing at all. Only asking "is the
// value where I put it" catches that.
func verifyNested(content string, path []string, fields []Field) error {
	b, err := docReader.parse(content)
	if err != nil {
		return fmt.Errorf("the rewritten file does not parse: %w", err)
	}
	m := b.mapping
	for _, key := range path {
		found := false
		for _, v := range m.Values {
			if v.Key.GetToken().Value != key {
				continue
			}
			switch inner := v.Value.(type) {
			case *ast.MappingNode:
				m, found = inner, true
			case *ast.MappingValueNode:
				promoted := emptyMapping()
				promoted.Values = append(promoted.Values, inner)
				m, found = promoted, true
			}
			break
		}
		if !found {
			return fmt.Errorf("the rewritten file lost %q", key)
		}
	}
	for _, f := range fields {
		if err := verifyField(m, f); err != nil {
			return err
		}
	}
	return nil
}

// verifyField confirms one field reads back as written.
func verifyField(m *ast.MappingNode, f Field) error {
	for _, v := range m.Values {
		if v.Key.GetToken().Value != f.Key {
			continue
		}
		if f.List != nil {
			seq, ok := v.Value.(*ast.SequenceNode)
			if !ok {
				return fmt.Errorf("%q was written as a list and read back as %s",
					f.Key, v.Value.Type())
			}
			got, err := docReader.sequence(f.Key, seq)
			if err != nil {
				return fmt.Errorf("%q did not read back: %w", f.Key, err)
			}
			if len(got) != len(f.List) {
				return fmt.Errorf("%q read back as %d items, wrote %d", f.Key, len(got), len(f.List))
			}
			for i := range got {
				if got[i] != f.List[i] {
					return fmt.Errorf("%q[%d] read back as %q, wrote %q", f.Key, i, got[i], f.List[i])
				}
			}
			return nil
		}
		got, err := docReader.scalar(f.Key, v.Value)
		if err != nil {
			return fmt.Errorf("%q did not read back: %w", f.Key, err)
		}
		if got == f.Value {
			return nil
		}
		// page_id and parent are written as YAML integers and nulls, so an
		// intended "null" (or "") reads back as "" -- every null spelling does.
		// That is agreement, not drift.
		if typedFields[f.Key] && nullish(f.Value) && got == "" {
			return nil
		}
		return fmt.Errorf("%q read back as %q, wrote %q", f.Key, got, f.Value)
	}
	return fmt.Errorf("%q is missing from the rewritten file", f.Key)
}

// nullish reports whether a written value meant "unset".
func nullish(v string) bool { return v == "" || v == "null" }
