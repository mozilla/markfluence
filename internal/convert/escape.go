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
	if !strings.ContainsAny(s, "\\`*_~[]<&") {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i, c := range rs {
		if needsEscape(rs, i, inLink) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// needsEscape reports whether rs[i] must be escaped.
func needsEscape(rs []rune, i int, inLink bool) bool {
	// prev and next are -1 at the node's edge: unknown.
	prev, next := rune(-1), rune(-1)
	if i > 0 {
		prev = rs[i-1]
	}
	if i < len(rs)-1 {
		next = rs[i+1]
	}
	switch rs[i] {
	case '\\':
		// Only a backslash before punctuation is an escape; before anything
		// else it is literal (C:\path). At the edge the next character is
		// whatever the next node renders, which is usually punctuation.
		return next == -1 || isASCIIPunct(next)
	case '`':
		// Any backtick can open a code span.
		return true
	case '*':
		// A "*" with whitespace on both sides is neither left- nor
		// right-flanking, so it can neither open nor close emphasis.
		return !isSpace(prev) || !isSpace(next)
	case '_':
		// An intraword "_" cannot open or close emphasis (snake_case), and one
		// with whitespace on both sides is not flanking at all.
		intraword := isAlnum(prev) && isAlnum(next)
		spaced := isSpace(prev) && isSpace(next)
		return !intraword && !spaced
	case '~':
		// goldmark takes a single tilde as strikethrough, so every one could
		// matter. But a strikethrough needs a closer, and a closer is preceded
		// by something other than whitespace: escaping every possible closer
		// leaves an opener with nothing to pair with. A run of two could pair
		// with a "~~" markfluence emits, so a tilde beside another is escaped
		// too. This keeps "about ~5 min" readable.
		return !isSpace(prev) || prev == '~' || next == '~'
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
		return next == -1 || unicode.IsLetter(next) || next == '/' || next == '!' || next == '?'
	case '&':
		// An entity reference publishes as its character; "AT&T" is text.
		return next == -1 || entityRE.MatchString(string(rs[i:min(len(rs), i+maxEntity)]))
	}
	return false
}

// entityRE matches a character reference at the start of a string: named,
// decimal or hexadecimal, as CommonMark recognises them.
var entityRE = regexp.MustCompile(`^&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

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
