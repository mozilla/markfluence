// Package pageref resolves the way a user names a Confluence page on the command
// line into a page id.
//
// Three spellings are accepted, because all three are things a user naturally
// has to hand: a bare numeric id, a Confluence page or folder URL (pasted from a
// browser), and a markdown file that carries a page_id. Every command that
// takes a page argument accepts all three, so the meaning of that argument does
// not depend on which command it was given to.
//
// "Carries a page_id" means either location it may live in: the file's own
// frontmatter, or a pages: entry for it in the project's markfluence.yaml
// (#139). Reading only the frontmatter left seven commands -- info, read,
// children, export and the three attachment-* verbs -- unable to name a file
// they could publish, which is a worse inconsistency than the extra lookup
// costs: the argument would have meant different things to update and to info.
package pageref

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/project"
)

// declaredPageID reads a file's page_id from wherever it lives.
//
// The root is discovered from the file's own directory, which is the same
// per-file discovery every other read does (docs/root-model.md) -- and it has to
// be discovered here rather than passed in: these seven commands take a single
// page and build no project.Cache, so threading a root through would mean
// seven signature changes to reach one lookup. A file with no project file above
// it, or one whose project declares no pages:, resolves exactly as it did
// before.
//
// A malformed markfluence.yaml is *not* fatal here, deliberately. This function
// answers "which page does this argument name", and a project file that cannot
// be understood does not stop the frontmatter from answering it; the commands
// that bound reads by the root resolve it themselves and report the problem
// there. Refusing here would make `info 123` fail for a broken file it never
// consults.
func declaredPageID(arg string, mf *frontmatter.MarkdownFile) (string, error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return strings.TrimSpace(mf.PageID()), nil
	}
	root, err := project.Discover(filepath.Dir(abs))
	if err != nil {
		return strings.TrimSpace(mf.PageID()), nil
	}
	defer func() { _ = root.FS.Close() }()

	key, _ := pagemeta.KeyFor(root, abs)
	meta, err := pagemeta.Resolve(key, mf, root)
	if err != nil {
		// The two locations name different pages. Unlike the malformed-file
		// case above, this is exactly the question being asked, and answering
		// with either id would be a guess.
		return "", err
	}
	return strings.TrimSpace(meta.Fields["page_id"]), nil
}

// pagePathRE matches the numeric id in a modern Confluence content URL path,
// e.g. /wiki/spaces/ENG/pages/123456/Some+Title (the trailing slug is optional).
//
// /folder/ is accepted alongside /pages/ because a folder is a legitimate
// argument to a command that walks a tree, and a folder URL is what a browser
// hands you. The id is all this returns, so a command that can only use a page
// reports its own not-found rather than a parse error -- the tradeoff for the
// argument meaning the same thing everywhere.
var pagePathRE = regexp.MustCompile(`/(?:pages|folder)/(\d+)(?:/|$)`)

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
		id, err := declaredPageID(arg, mf)
		if err != nil {
			return "", err
		}
		if id == "" {
			return "", fmt.Errorf(
				"no page_id for %s: set one in its frontmatter or in its %s entry",
				arg, project.Filename)
		}
		return id, nil
	}
	if IsDigits(arg) {
		return arg, nil
	}
	if id, ok := fromURL(arg); ok {
		return id, nil
	}
	return "", fmt.Errorf(
		"%q is not a numeric id, a Confluence page or folder URL, or a markdown file with a page_id", arg)
}

// fromURL pulls a content id out of a Confluence URL: the modern
// /wiki/.../pages/<id>/... or /wiki/.../folder/<id> path form, or a legacy
// ?pageId=<id> query parameter.
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
