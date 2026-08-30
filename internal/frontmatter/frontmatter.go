// Package frontmatter parses and rewrites the flat YAML frontmatter block that
// markfluence markdown files carry, and models a parsed file as a MarkdownFile.
//
// It handles flat key: value pairs only -- no nested structures, lists, or
// multiline values -- with single/double-quote support and inline-`#`-comment
// stripping, plus surgical single-line write-back.
package frontmatter

import (
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// frontmatterRE matches a leading `---\n...\n---\n` block (DOTALL, non-greedy),
// anchored at the start of the document.
var frontmatterRE = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n`)

// inlineCommentRE finds the first whitespace-then-`#` (a YAML inline comment).
var inlineCommentRE = regexp.MustCompile(`\s#`)

// Extract pulls the YAML frontmatter from content, returning the flat
// key->value map and the body (content with the frontmatter block removed). With
// no frontmatter it returns an empty map and content unchanged.
//
// Full-line `#` comments are skipped; a trailing inline `#` comment is stripped
// from each unquoted value; quoted values are read via ParseValue.
func Extract(content string) (map[string]string, string) {
	loc := frontmatterRE.FindStringSubmatchIndex(content)
	if loc == nil {
		return map[string]string{}, content
	}
	fmText := content[loc[2]:loc[3]]
	body := content[loc[1]:]

	fm := map[string]string{}
	for _, line := range strings.Split(fmText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, ":"); i >= 0 {
			key := strings.TrimSpace(line[:i])
			fm[key] = ParseValue(line[i+1:])
		}
	}
	return fm, body
}

// ParseValue parses a frontmatter value (the text after the first `:`). A value
// whose first non-space rune is `'` or `"` is read as a quoted string (inline
// `#` comments inside it are preserved); otherwise a trailing inline comment is
// stripped. An unterminated quote falls back to unquoted handling.
func ParseValue(raw string) string {
	stripped := strings.TrimLeftFunc(raw, unicode.IsSpace)
	if len(stripped) > 0 && (stripped[0] == '\'' || stripped[0] == '"') {
		if v, ok := scanQuoted(stripped); ok {
			return v
		}
	}
	return stripInlineComment(raw)
}

// scanQuoted parses a leading quoted token from s (which starts with `'` or `"`).
// It returns the unquoted value, or ok=false if the quote is unterminated. Single
// quotes are literal with `”` -> `'`; double quotes honor `\"` and `\\` escapes.
// Anything after the closing quote is ignored.
func scanQuoted(s string) (string, bool) {
	r := []rune(s)
	quote := r[0]
	var out []rune
	for i := 1; i < len(r); {
		c := r[i]
		if quote == '\'' {
			if c == '\'' {
				if i+1 < len(r) && r[i+1] == '\'' { // doubled '' -> literal '
					out = append(out, '\'')
					i += 2
					continue
				}
				return string(out), true // closing quote
			}
			out = append(out, c)
			i++
		} else { // double quote
			if c == '\\' && i+1 < len(r) && (r[i+1] == '"' || r[i+1] == '\\') {
				out = append(out, r[i+1])
				i += 2
				continue
			}
			if c == '"' {
				return string(out), true // closing quote
			}
			out = append(out, c)
			i++
		}
	}
	return "", false // unterminated
}

// stripInlineComment removes a trailing whitespace-then-`#` comment and trims.
func stripInlineComment(value string) string {
	if loc := inlineCommentRE.FindStringIndex(value); loc != nil {
		value = value[:loc[0]]
	}
	return strings.TrimSpace(value)
}

// quoteValue quotes value for frontmatter, preferring single quotes.
func quoteValue(value string) string {
	if !strings.Contains(value, "'") {
		return "'" + value + "'"
	}
	if !strings.Contains(value, `"`) {
		return `"` + value + `"`
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// renderValue renders a value for a frontmatter line, quoting it only when a bare
// round-trip through ParseValue wouldn't reproduce it.
func renderValue(value string) string {
	if ParseValue(" "+value) != value {
		return quoteValue(value)
	}
	return value
}

// fieldOrder is the canonical leading order of frontmatter keys; any key not
// listed here is emitted after these, in alphabetical order. Every write goes
// through UpdateField, so this is the single source of frontmatter field order
// across all commands.
var fieldOrder = []string{"title", "space", "parent", "page_id"}

// UpdateField adds or updates key in content's frontmatter, returning the new
// content. An existing key's value is replaced; a missing key is added; with no
// frontmatter block one is created at the top. The value is auto-quoted when
// needed to round-trip. A non-empty comment is written as a trailing `  # ...`
// annotation, kept distinct from the value so the value round-trips cleanly.
//
// The whole block is rewritten in the canonical field order (fieldOrder, then
// the rest alphabetically). Full-line `#` comments are preserved at the top of
// the block; blank lines are dropped.
func UpdateField(content, key, value, comment string) string {
	rendered := renderValue(value)
	if comment != "" {
		rendered += "  # " + comment
	}
	newLine := key + ": " + rendered

	loc := frontmatterRE.FindStringSubmatchIndex(content)
	if loc == nil {
		return "---\n" + newLine + "\n---\n" + content
	}
	body := content[loc[1]:]

	comments, fields := splitFrontmatter(content[loc[2]:loc[3]])
	fields[key] = newLine

	lines := comments
	for _, k := range orderedKeys(fields) {
		lines = append(lines, fields[k])
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body
}

// splitFrontmatter parses a frontmatter block's inner text into its full-line
// comments (in order) and a key->line map (each value the raw `key: ...` line,
// so quoting and inline comments are preserved). Blank lines are dropped.
func splitFrontmatter(fmText string) (comments []string, fields map[string]string) {
	fields = map[string]string{}
	for _, line := range strings.Split(fmText, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "#"):
			comments = append(comments, trimmed)
		default:
			if i := strings.Index(trimmed, ":"); i >= 0 {
				fields[strings.TrimSpace(trimmed[:i])] = trimmed
			}
		}
	}
	return comments, fields
}

// orderedKeys returns the keys of fields in canonical order: those in fieldOrder
// first (in that order), then any remaining keys alphabetically.
func orderedKeys(fields map[string]string) []string {
	rank := map[string]int{}
	for i, k := range fieldOrder {
		rank[k] = i
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, iok := rank[keys[i]]
		rj, jok := rank[keys[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok:
			return true
		case jok:
			return false
		default:
			return keys[i] < keys[j]
		}
	})
	return keys
}

// MarkdownFile is a markdown source file parsed once: its path, raw text,
// frontmatter map, and body (content with the frontmatter block stripped).
//
// Frontmatter is exported so callers that must distinguish absent from
// present-but-blank (e.g. the fix command) can read it directly. The accessor
// methods provide normalized reads: PageID/Space/Parent treat missing, blank, or
// literal "null" as unset (returning ""), while Title only collapses missing or
// blank -- a title is free text, so a literal "null" is kept.
type MarkdownFile struct {
	Filename    string
	Content     string
	Frontmatter map[string]string
	Body        string
}

// ErrUnterminatedFrontmatter is returned by Parse/ParseFile when content opens
// with a "---\n" delimiter that never closes. Extract is deliberately lenient
// about this shape -- a regex miss just falls back to "no frontmatter, whole
// file is body" -- which would otherwise hide a common paste mistake
// completely, including from every command that calls Parse.
var ErrUnterminatedFrontmatter = errors.New(
	`unterminated frontmatter block: starts with "---" but has no closing "---" line`)

// Parse builds a MarkdownFile from an in-memory content string tagged with
// filename, or reports ErrUnterminatedFrontmatter.
func Parse(filename, content string) (*MarkdownFile, error) {
	if strings.HasPrefix(content, "---\n") && !frontmatterRE.MatchString(content) {
		return nil, ErrUnterminatedFrontmatter
	}
	fm, body := Extract(content)
	return &MarkdownFile{Filename: filename, Content: content, Frontmatter: fm, Body: body}, nil
}

// ParseFile reads filename from disk and parses it.
func ParseFile(filename string) (*MarkdownFile, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return Parse(filename, string(data))
}

// coordinate reads a page-coordinate field, mapping the no-value sentinels to "".
func (m *MarkdownFile) coordinate(key string) string {
	v, ok := m.Frontmatter[key]
	if !ok {
		return ""
	}
	v = strings.TrimSpace(v)
	if v == "" || v == "null" {
		return ""
	}
	return v
}

// Title returns the title, "" if missing or blank ("null" is a legal title).
func (m *MarkdownFile) Title() string {
	return strings.TrimSpace(m.Frontmatter["title"])
}

// PageID returns the page id, "" if missing, blank, or "null".
func (m *MarkdownFile) PageID() string { return m.coordinate("page_id") }

// Space returns the space key, "" if missing, blank, or "null".
func (m *MarkdownFile) Space() string { return m.coordinate("space") }

// Parent returns the parent, "" if missing, blank, or "null".
func (m *MarkdownFile) Parent() string { return m.coordinate("parent") }
