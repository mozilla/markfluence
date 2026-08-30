// Package linkindex builds the tree-wide index internal/convert resolves
// document links and same-page/cross-file anchors against.
//
// It replaces a per-directory, basename-keyed lookup with a path-keyed one
// covering the whole tree below a project.Root: a link destination resolves
// by where it actually points, not by matching a bare filename against
// whatever happens to share the linking file's own directory. That is what
// fixes a same-basename file in a different directory resolving to the wrong
// page (025 Scenario A) and what makes a link that could traverse above root
// simply not found rather than accidentally safe by basename flattening
// (Scenario F) -- no clamp is needed here, since an escaping path is never in
// an index built by walking downward from root in the first place.
//
// Built once per root and shared across every file converted under it.
// Rebuilding per file, per directory, was the pre-025 code's accidental
// O(n^2): each conversion re-read its own directory, so a directory of 40
// files cost 40 reads for each of 400 conversions. See _plans/025's
// measurement.
package linkindex

import (
	"io/fs"
	"regexp"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/project"
)

// PageEntry is a markdown file's Confluence coordinates, keyed in an Index by
// its path relative to the root.
type PageEntry struct {
	PageID string
	Title  string
}

// Index is the tree-wide page and anchor map. Keys are always root-relative,
// slash-separated paths -- the same form internal/convert resolves a link
// destination to before looking it up.
type Index struct {
	pages   map[string]PageEntry
	anchors map[string]map[string]string
}

// Build walks root's tree once, via root.FS, collecting every *.md file's
// page_id/title (when it has one) and heading anchors. Walking through
// root.FS is what keeps the walk from ever descending a symlinked directory:
// a symlink's directory entry reports its own type (a link, not a
// directory), so fs.WalkDir calls the visit function for it once and does
// not recurse -- matching the non-goal in docs/guarantees.md#symlinks. An
// unreadable file is skipped rather than failing the whole build; a page with
// no page_id yet is walked (its anchors still work) but has no PageEntry, the
// same as a draft with no page_id has always had no place in this lookup.
func Build(root *project.Root) (*Index, error) {
	idx := &Index{pages: map[string]PageEntry{}, anchors: map[string]map[string]string{}}
	fsys := root.FS.FS()
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil
		}
		mf, err := frontmatter.Parse(path, string(data))
		if err != nil {
			// A malformed sibling is skipped exactly like an unreadable one --
			// one broken file elsewhere in the tree must not block checking or
			// converting an unrelated one.
			return nil
		}
		if id := mf.PageID(); id != "" {
			idx.pages[path] = PageEntry{PageID: id, Title: mf.Title()}
		}
		anchors := map[string]string{}
		for _, h := range extractHeadings(mf.Body) {
			if gh := GithubSlug(h); gh != "" {
				anchors[gh] = ConfluenceSlug(h)
			}
		}
		idx.anchors[path] = anchors
		return nil
	})
	if err != nil {
		return nil, err
	}
	return idx, nil
}

// Page returns the coordinates recorded for path (root-relative,
// slash-separated), and whether an entry exists there.
func (idx *Index) Page(path string) (PageEntry, bool) {
	e, ok := idx.pages[path]
	return e, ok
}

// FileExists reports whether path (root-relative, slash-separated) is a
// walked `.md` file under the index's root, regardless of whether it has a
// page_id yet. It answers "does this file exist at all" independent of
// Page's "is this file published" -- Build records an anchors entry (even an
// empty one) for every walked file, unlike pages, which only gets one when
// the file has a page_id, so this reuses that map rather than adding new
// bookkeeping.
func (idx *Index) FileExists(path string) bool {
	_, ok := idx.anchors[path]
	return ok
}

// Anchor returns the Confluence-side slug matching a GitHub-style slug on the
// page at path, and whether it exists.
func (idx *Index) Anchor(path, githubSlug string) (string, bool) {
	m, ok := idx.anchors[path]
	if !ok {
		return "", false
	}
	s, ok := m[githubSlug]
	return s, ok
}

// SetPage overrides -- or injects -- the entry for path. create's reserve
// phase uses this to seed ids that exist only in memory (not yet, or never,
// under --no-persist, written back to frontmatter) before its publish phase
// converts anything, so every link resolves regardless of creation order.
func (idx *Index) SetPage(path string, entry PageEntry) {
	idx.pages[path] = entry
}

var (
	// nonSlugRE strips everything except letters, digits, underscore,
	// whitespace, and hyphens (Unicode-aware).
	nonSlugRE       = regexp.MustCompile(`[^\p{L}\p{N}_\s-]`)
	whitespaceRE    = regexp.MustCompile(`\s`)
	whitespaceRunRE = regexp.MustCompile(`\s+`)
)

// GithubSlug replicates GitHub's heading-anchor slugger: lowercase; strip all
// but letters/digits/underscore/whitespace/hyphen; each whitespace char
// becomes one hyphen; trim leading/trailing hyphens.
func GithubSlug(heading string) string {
	s := strings.ToLower(heading)
	s = nonSlugRE.ReplaceAllString(s, "")
	s = whitespaceRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// ConfluenceSlug replicates Confluence's scheme: preserve case and
// punctuation, collapsing runs of whitespace to single hyphens. Confluence
// assigns this anchor to a heading itself -- there is no id attribute for
// internal/convert to emit -- so this exists purely to compute what
// Confluence's own anchor will be.
func ConfluenceSlug(heading string) string {
	return whitespaceRunRE.ReplaceAllString(strings.TrimSpace(heading), "-")
}

// extractHeadings returns the text of each ATX heading in a
// frontmatter-stripped body, skipping fenced code blocks so "#" lines inside
// samples aren't headings.
func extractHeadings(body string) []string {
	var headings []string
	inCode := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		hashes := 0
		for hashes < len(line) && line[hashes] == '#' {
			hashes++
		}
		if hashes == 0 || hashes >= len(line) {
			continue
		}
		rest := line[hashes:]
		if strings.TrimLeft(rest, " \t") == rest { // no whitespace after the #s
			continue
		}
		if text := strings.TrimSpace(rest); text != "" {
			headings = append(headings, text)
		}
	}
	return headings
}
