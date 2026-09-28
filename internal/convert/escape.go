package convert

import (
	"regexp"
	"strings"
	"unicode"
)

// Text read from storage is written into Markdown, and wherever its characters
// spell Markdown syntax the next publish turns them into that syntax: literal
// "*not emphasis*" publishes as <em>, "1. not a list" as a list (#203). A
// backslash before any ASCII punctuation character makes it literal in
// CommonMark, so the fix is the one every Markdown writer makes: escape text
// that would otherwise read as syntax. What goldmark treats as syntax, and that
// each escape here publishes back as the literal character, was measured
// (_plans/056).
//
// The escaping is minimal rather than total. Escaping every punctuation
// character would be correct and unreadable -- snake\_case, a \* b -- so each
// rule escapes only where the character could take effect. A text node's
// first and last characters sit beside whatever the neighbouring node renders,
// which the node cannot see, so at an edge every rule assumes the worst: an
// extra escape round-trips as the literal character, and a missing one is the
// bug.

// escapeText escapes the inline syntax in one text node's (collapsed) text.
// inLink is set inside a link's text, where a "]" would end the text early and
// must be escaped too; elsewhere a lone "]" is inert and common in prose.
func escapeText(s string, inLink bool) string {
	return escapeFor(s, inLink, false)
}

// escapeRawText is escapeText for a text node inside raw storage written
// inline -- an inline macro, a list in a pipe-table cell, an <ac:link> passed
// through. The text between inline HTML tags is still Markdown, so "_x_" in a
// status macro's title published as <em> (_plans/056, amended). "<" and "&"
// are left to xmlTextEscape, which writes both as entities.
func escapeRawText(s string, inLink bool) string {
	return escapeFor(s, inLink, true)
}

// escapeFor is escapeText, and escapeRawText when inRaw is set.
func escapeFor(s string, inLink, inRaw bool) string {
	if !mayNeedEscape(s) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i, c := range rs {
		if inRaw && (c == '<' || c == '&') {
			b.WriteRune(c)
			continue
		}
		if !needsEscape(rs, i, inLink) {
			b.WriteRune(c)
			continue
		}
		// A tilde at its node's end -- last, or followed only by whitespace
		// that may move to the next node -- may meet the "~~" of a del mark
		// rendered next, and goldmark reads "\~~~b~~" as a run of three that
		// closes nothing, so the strikethrough is lost. A character reference
		// is not part of any run. Raw storage is followed by a tag, never a
		// mark, and xmlTextEscape would mangle the reference, so it keeps the
		// backslash.
		if c == '~' && edgeSpace(rs[i+1:]) && !inRaw {
			b.WriteString("&#126;")
			continue
		}
		b.WriteByte('\\')
		b.WriteRune(c)
	}
	return b.String()
}

// mayNeedEscape is a quick test that s holds nothing escapeText could escape,
// so that ordinary prose skips the per-character pass. "." and ":" matter only
// in "www." and "://", so they are looked for in that form.
func mayNeedEscape(s string) bool {
	return strings.ContainsAny(s, "\\`*_~[]<&@") || strings.Contains(s, "://") ||
		(strings.Contains(s, ".") && strings.Contains(strings.ToLower(s), "www."))
}

// needsEscape reports whether rs[i] must be escaped.
//
// A neighbour is unknown (-1) past the node's edge, and also when it is
// whitespace at the node's edge: that whitespace is not reliably this node's.
// renderMark moves a mark's edge whitespace outside its delimiters and
// publishing then stores it in the neighbouring node, so a decision that
// trusted it would differ between a read and the read after it -- "\" before
// a space the mark gives away would go unescaped and escape the delimiter.
func needsEscape(rs []rune, i int, inLink bool) bool {
	prev := func() rune {
		if i > 0 && !edgeSpace(rs[:i]) {
			return rs[i-1]
		}
		return -1
	}
	next := func() rune {
		if i < len(rs)-1 && !edgeSpace(rs[i+1:]) {
			return rs[i+1]
		}
		return -1
	}
	switch rs[i] {
	case '\\':
		// Only a backslash before punctuation is an escape; before anything
		// else it is literal (C:\path). At the edge the next character is
		// whatever the next node renders, which is usually punctuation.
		n := next()
		return n == -1 || isASCIIPunct(n)
	case '`':
		// Any backtick can open a code span.
		return true
	case '*':
		// A "*" with whitespace on both sides is neither left- nor
		// right-flanking, so it can neither open nor close emphasis.
		return !isSpace(prev()) || !isSpace(next())
	case '_':
		// An intraword "_" cannot open or close emphasis (snake_case), and one
		// with whitespace on both sides is not flanking at all.
		p, n := prev(), next()
		intraword := isAlnum(p) && isAlnum(n)
		spaced := isSpace(p) && isSpace(n)
		return !intraword && !spaced
	case '~':
		// goldmark takes a single tilde as strikethrough, so every one could
		// matter. But a strikethrough needs a closer, and a closer is preceded
		// by something other than whitespace: escaping every possible closer
		// leaves an opener with nothing to pair with. A run of two could pair
		// with a "~~" markfluence emits, so a tilde beside another is escaped,
		// and so is one at the node's end, where the next node may render
		// exactly that "~~". This keeps "about ~5 min" readable.
		p, n := prev(), next()
		return !isSpace(p) || p == '~' || n == '~' || n == -1
	case '[':
		// "[x]: y" at a line start is a link reference definition and
		// vanishes, "[ ] x" in a list item is a checkbox, and "[x]" is a link
		// wherever a definition exists.
		return true
	case ']':
		return inLink
	case '<':
		// The starts of a tag, a comment, a declaration and an autolink.
		// "a < b" and "<3" are text.
		n := next()
		return n == -1 || unicode.IsLetter(n) || n == '/' || n == '!' || n == '?'
	case '&':
		// An entity reference publishes as its character; "AT&T" is text.
		return next() == -1 || entityRE.MatchString(string(rs[i:min(len(rs), i+maxEntity)]))
	case ':', '.', '@':
		return autolinks(rs, i)
	}
	return false
}

// autolinks reports whether rs[i] is the character that makes GFM autolink a
// bare URL or email address: the ":" of "https://", the "." of a "www." that
// starts a word, or the "@" of an address. Escaping it keeps plain text plain;
// a URL stored as text rather than as a link is someone's choice, and the
// editor turns a typed one into a link (_plans/056, D7).
//
// An "@" at the node's start is the one character left alone at an edge: a
// link whose text starts with "@" is how a mention is written (#91), and
// escaping it would turn every such link into a plain profile link.
func autolinks(rs []rune, i int) bool {
	before := strings.ToLower(string(rs[max(0, i-5):i]))
	switch rs[i] {
	case ':':
		after := string(rs[i+1 : min(len(rs), i+3)])
		return after == "//" && (strings.HasSuffix(before, "http") ||
			strings.HasSuffix(before, "https") || strings.HasSuffix(before, "ftp"))
	case '.':
		if !strings.HasSuffix(before, "www") {
			return false
		}
		return i == 3 || !isAlnum(rs[i-4])
	case '@':
		return i > 0 && i < len(rs)-1 && isEmailLocal(rs[i-1]) && isAlnum(rs[i+1])
	}
	return false
}

// isEmailLocal reports whether r may end the local part of an email address.
func isEmailLocal(r rune) bool {
	return isAlnum(r) || strings.ContainsRune(".!#$%&'*+/=?^_`{|}~-", r)
}

// entityRE matches a character reference at the start of a string: named,
// decimal or hexadecimal, as CommonMark recognises them.
var entityRE = regexp.MustCompile(`^&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

// edgeSpace reports whether rs is nothing but whitespace, so that whitespace
// beside a character reaches all the way to the node's edge.
func edgeSpace(rs []rune) bool {
	for _, r := range rs {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// maxEntity bounds how far entityRE can match: "&", a 32-character name, ";".
const maxEntity = 34

// isSpace reports whether r is known whitespace; an unknown neighbour is not.
func isSpace(r rune) bool { return r != -1 && unicode.IsSpace(r) }

// isAlnum reports whether r is a known letter or digit.
func isAlnum(r rune) bool { return r != -1 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

// isASCIIPunct reports whether r is one of the characters CommonMark lets a
// backslash escape.
func isASCIIPunct(r rune) bool {
	return r < 128 && strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", r)
}

// escapeLineStarts escapes a block marker at the start of each line of a
// paragraph's rendered text, where a line starts at the paragraph's start and
// after every hard break. A marker there would open a block on publish: "1. x"
// a list, "# x" a heading, "---" under a line a setext heading (_plans/056,
// D3). It runs over rendered Markdown rather than text nodes, because only the
// assembled paragraph knows where its lines start; that is safe because
// nothing markfluence emits inline begins with a block marker (D4). It splits
// only at hard breaks, so raw storage holding a newline is never touched.
func escapeLineStarts(s string) string {
	lines := strings.Split(s, hardBreak)
	for i, line := range lines {
		lines[i] = escapeLineStart(line)
	}
	return strings.Join(lines, hardBreak)
}

// escapeLineStart escapes the block marker, if any, that line starts with.
func escapeLineStart(line string) string {
	body := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(body)]
	if m := orderedMarkerRE.FindStringSubmatchIndex(body); m != nil {
		// Escape the delimiter: "1\. x".
		return indent + body[:m[2]] + `\` + body[m[2]:]
	}
	for _, re := range blockStartREs {
		if re.MatchString(body) {
			return indent + `\` + body
		}
	}
	return line
}

// orderedMarkerRE matches an ordered list marker; its group is the delimiter.
var orderedMarkerRE = regexp.MustCompile(`^[0-9]{1,9}([.)])(?: |$)`)

// blockStartREs are the other lines that open a block, each escaped by a
// backslash before its first character.
var blockStartREs = []*regexp.Regexp{
	regexp.MustCompile(`^#{1,6}(?: |$)`),                             // an ATX heading
	regexp.MustCompile(`^>`),                                         // a blockquote, space or not
	regexp.MustCompile(`^[-+*](?: |$)`),                              // a bulleted list item
	regexp.MustCompile(`^(?:-+|=+) *$`),                              // a setext underline or thematic break
	regexp.MustCompile(`^(?:(?:\* *){3,}|(?:- *){3,}|(?:_ *){3,})$`), // a thematic break: one character, repeated
	regexp.MustCompile(`^(?:~~~|` + "```" + `)`),                     // a code fence
	// A table delimiter row, under a line that then becomes the header.
	regexp.MustCompile(`^[|:-][|: -]*$`),
}

// escapeHeadingClose escapes a trailing run of "#" after whitespace in a
// heading's text, which ATX headings read as a closing sequence and drop:
// "## Item #" publishes as "Item" (_plans/056, D6), and "### #" as an empty
// heading. "C#" has no whitespace before its "#" and is not a closing
// sequence.
func escapeHeadingClose(s string) string {
	if m := headingCloseRE.FindStringIndex(s); m != nil {
		i := m[0] + len(strings.TrimRight(s[m[0]:], "#"))
		return s[:i] + `\` + s[i:]
	}
	return s
}

// headingCloseRE matches a run of "#" ending the text after whitespace, or
// making up the whole text: "### #" is an empty heading with a closing
// sequence.
var headingCloseRE = regexp.MustCompile(`(?:^|\s)#+$`)
