package convert

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/yuin/goldmark/ast"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// pageEntry is a sibling document's Confluence coordinates for link rewriting.
type pageEntry struct {
	pageID string
	title  string
}

var (
	// nonSlugRE strips everything except letters, digits, underscore, whitespace,
	// and hyphens (Unicode-aware).
	nonSlugRE       = regexp.MustCompile(`[^\p{L}\p{N}_\s-]`)
	whitespaceRE    = regexp.MustCompile(`\s`)
	whitespaceRunRE = regexp.MustCompile(`\s+`)
)

// githubSlug replicates GitHub's heading-anchor slugger: lowercase; strip all but
// letters/digits/underscore/whitespace/hyphen; each whitespace char becomes one
// hyphen; trim leading/trailing hyphens.
func githubSlug(heading string) string {
	s := strings.ToLower(heading)
	s = nonSlugRE.ReplaceAllString(s, "")
	s = whitespaceRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// confluenceSlug replicates Confluence's scheme: preserve case and punctuation,
// collapsing runs of whitespace to single hyphens.
func confluenceSlug(heading string) string {
	return whitespaceRunRE.ReplaceAllString(strings.TrimSpace(heading), "-")
}

// extractHeadings returns the text of each ATX heading in a frontmatter-stripped
// body, skipping fenced code blocks so "#" lines inside samples aren't headings.
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

// buildAnchorMap maps each *.md file in dir to its {githubSlug: confluenceSlug}.
func buildAnchorMap(dir string) map[string]map[string]string {
	out := map[string]map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		_, body := frontmatter.Extract(string(data))
		anchors := map[string]string{}
		for _, h := range extractHeadings(body) {
			if gh := githubSlug(h); gh != "" {
				anchors[gh] = confluenceSlug(h)
			}
		}
		out[e.Name()] = anchors
	}
	return out
}

// buildPageMap maps each *.md file in dir with a usable page_id to its
// Confluence coordinates.
func buildPageMap(dir string) map[string]pageEntry {
	out := map[string]pageEntry{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		mf := frontmatter.Parse(e.Name(), string(data))
		if id := mf.PageID(); id != "" {
			out[e.Name()] = pageEntry{pageID: id, title: mf.Title()}
		}
	}
	return out
}

// renderLink renders a link, rewriting same-page and cross-file anchors to
// Confluence ids and sibling .md links to their published Confluence URLs. Links
// it does not rewrite fall back to the default goldmark rendering.
func (r *storageRenderer) renderLink(
	w util.BufWriter, _ []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if !entering {
		_, _ = w.WriteString("</a>")
		return ast.WalkContinue, nil
	}
	href, rewritten := r.rewriteHref(string(n.Destination))
	_, _ = w.WriteString(`<a href="`)
	if rewritten {
		_, _ = w.WriteString(href) // already composed and escaped
	} else {
		_, _ = w.Write(util.EscapeHTML(util.URLEscape([]byte(href), true)))
	}
	_ = w.WriteByte('"')
	if n.Title != nil {
		_, _ = w.WriteString(` title="`)
		gmhtml.DefaultWriter.Write(w, n.Title)
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
	return ast.WalkContinue, nil
}

// rewriteHref applies anchor rewriting then internal-doc-link rewriting. It
// returns the possibly-rewritten href and whether any rewrite occurred (a
// rewritten href is already fully composed and escaped, so it must be written
// verbatim rather than re-escaped).
func (r *storageRenderer) rewriteHref(href string) (string, bool) {
	rewritten := false

	if strings.HasPrefix(href, "#") {
		if nf := r.anchorMap[r.currentBasename][decodeDestination(href[1:])]; nf != "" {
			// Same-page anchors become fake cross-file links to the current
			// file so the doc-link step can fully qualify them. The filename is
			// encoded going in because what is being built here is a
			// destination, and this one may still be emitted as an href if the
			// doc-link step cannot resolve it (a file with no page_id yet).
			href = encodeDestination(r.currentBasename) + "#" + escapeFragment(nf)
			rewritten = true
		}
	} else if path, frag, ok := splitMarkdownAnchor(href); ok {
		if nf := r.anchorMap[docKey(path)][decodeDestination(frag)]; nf != "" {
			// path keeps the spelling it was written with: it is a destination,
			// and the doc-link step decodes it again for its own lookup.
			href = path + "#" + escapeFragment(nf)
			rewritten = true
		}
	}

	if newHref, ok := r.rewriteDocLink(href); ok {
		return newHref, true
	}
	return href, rewritten
}

// rewriteDocLink rewrites a sibling .md href (with optional fragment) to its
// Confluence URL. It returns ok=false for absolute URLs, non-.md hrefs, or files
// not in the page map.
func (r *storageRenderer) rewriteDocLink(href string) (string, bool) {
	path, fragment := href, ""
	if i := strings.Index(href, "#"); i >= 0 {
		path, fragment = href[:i], href[i:]
	}
	if !strings.HasSuffix(path, ".md") {
		return "", false
	}
	if strings.Contains(path, "://") || strings.HasPrefix(path, "//") {
		return "", false
	}
	entry, ok := r.pageMap[docKey(path)]
	if !ok {
		return "", false
	}

	var newHref string
	if r.spaceKey != "" {
		slug := ""
		if entry.title != "" {
			slug = url.QueryEscape(entry.title)
		}
		newHref = fmt.Sprintf("%s/wiki/spaces/%s/pages/%s/%s",
			r.baseURL, r.spaceKey, entry.pageID, slug)
	} else {
		newHref = fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%s", r.baseURL, entry.pageID)
	}
	return newHref + fragment, true
}

// docKey turns a link destination into the key the page and anchor maps are
// built with: the bare filename as it appears on disk. Both maps are keyed by
// os.ReadDir's e.Name(), so the destination has to be decoded first -- a link
// to "my doc.md" is spelled "my%20doc.md", and comparing that to the filename
// silently misses, publishing a relative href that is a dead link on Confluence.
func docKey(dest string) string {
	return filepath.Base(decodeDestination(dest))
}

// splitMarkdownAnchor splits "path.md#fragment" into its path and fragment,
// requiring a non-empty fragment and at least one character before ".md".
func splitMarkdownAnchor(href string) (path, fragment string, ok bool) {
	i := strings.Index(href, "#")
	if i < 0 {
		return "", "", false
	}
	path, fragment = href[:i], href[i+1:]
	if fragment == "" || !strings.HasSuffix(path, ".md") || len(path) <= len(".md") {
		return "", "", false
	}
	return path, fragment, true
}

// escapeFragment escapes &, <, and > in an anchor fragment (attribute quotes are
// left alone, matching the reference behavior).
func escapeFragment(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
