// Package pageref resolves the way a user names a Confluence page on the command
// line into a page id.
//
// Three spellings are accepted, because all three are things a user naturally
// has to hand: a bare numeric id, a Confluence page URL (pasted from a browser),
// and a markdown file whose frontmatter carries a page_id. Every command that
// takes a page argument accepts all three, so the meaning of that argument does
// not depend on which command it was given to.
package pageref

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
)

// pagePathRE matches the numeric id in a modern Confluence page URL path,
// e.g. /wiki/spaces/ENG/pages/123456/Some+Title (the trailing slug is optional).
var pagePathRE = regexp.MustCompile(`/pages/(\d+)(?:/|$)`)

// Resolve turns a command-line page argument into a page id.
//
// An existing file is tried first, so a numerically-named markdown file is read
// as a file rather than mistaken for an id.
func Resolve(arg string) (string, error) {
	if arg == "" {
		return "", fmt.Errorf("no page given")
	}
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		mf, err := frontmatter.ParseFile(arg)
		if err != nil {
			return "", err
		}
		if mf.PageID() == "" {
			return "", fmt.Errorf("no page_id in frontmatter of %s", arg)
		}
		return mf.PageID(), nil
	}
	if IsDigits(arg) {
		return arg, nil
	}
	if id, ok := fromURL(arg); ok {
		return id, nil
	}
	return "", fmt.Errorf(
		"%q is not a numeric page id, a Confluence page URL, or a markdown file with a page_id", arg)
}

// fromURL pulls a page id out of a Confluence URL: the modern
// /wiki/.../pages/<id>/... path form, or a legacy ?pageId=<id> query parameter.
func fromURL(arg string) (string, bool) {
	u, err := url.Parse(arg)
	if err != nil || u.Host == "" {
		return "", false
	}
	if id := u.Query().Get("pageId"); IsDigits(id) {
		return id, true
	}
	if m := pagePathRE.FindStringSubmatch(u.Path); m != nil {
		return m[1], true
	}
	return "", false
}

// IsDigits reports whether s is a non-empty run of ASCII digits.
func IsDigits(s string) bool {
	if s == "" {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) == -1
}
