// Package pageslug turns a Confluence page or folder title into a filename-safe
// slug, and names the file and directory a page occupies on disk.
//
// It is a package rather than a helper inside one command because three
// commands need the same answer: `export` writes a tree of them, and `read` and
// `attachment-download` position an attachment under the page's own directory
// (_plans/029). Two copies would mean an attachment landing somewhere the
// markdown does not point.
//
// The slug is filename-specific rather than either of the converter's
// heading-anchor sluggers. It has to drop path separators, cap its length, and
// produce something usable when a title slugs to nothing; and reusing an anchor
// slugger would mean a change to anchor generation silently renaming exported
// files.
package pageslug

import (
	"regexp"
	"strings"
)

// unsafeRE matches everything a slug drops: anything that is not a letter,
// digit, underscore, hyphen, or whitespace. Unicode letters are kept, so a
// non-Latin title still yields a usable name.
var unsafeRE = regexp.MustCompile(`[^\p{L}\p{N}_\s-]+`)

// whitespaceRE collapses each whitespace run into a single hyphen.
var whitespaceRE = regexp.MustCompile(`\s+`)

// Max caps the slug so a long title can't produce a filename the filesystem
// rejects; 80 leaves room for an extension and a disambiguating suffix well
// inside every limit.
const Max = 80

// Slug is the filename-safe form of a title, or "" when nothing survives.
//
// Punctuation is dropped rather than replaced, so equivalence runs through
// whitespace alone: "Title:1" slugs to "title1" while "Title 1" and "Title: 1"
// both slug to "title-1". Two properties callers depend on: it lowercases, so
// titles differing only in case collide and can be caught; and it drops "/", so
// no title can inject a path separator into a tree.
//
// It is lossy, and no readable slug can avoid being lossy -- the caller decides
// what to do when two titles collide.
func Slug(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = unsafeRE.ReplaceAllString(s, "")
	s = whitespaceRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len([]rune(s)) > Max {
		s = strings.Trim(string([]rune(s)[:Max]), "-")
	}
	return s
}

// For is the slug to use for a page or folder, falling back to its id when the
// title slugs to nothing at all -- a title of "?!" or "..." leaves no
// characters, and an id is always usable.
func For(title, id string) string {
	if s := Slug(title); s != "" {
		return s
	}
	return id
}

// Filename is the markdown file a page is written as.
func Filename(title, id string) string { return For(title, id) + ".md" }
