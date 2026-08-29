package convert

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark/ast"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

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
		if nf, ok := r.index.Anchor(r.currentDocKey, decodeDestination(href[1:])); ok {
			// Same-page anchors become fake cross-file links to the current
			// file so the doc-link step can fully qualify them. The filename is
			// encoded going in because what is being built here is a
			// destination, and this one may still be emitted as an href if the
			// doc-link step cannot resolve it (a file with no page_id yet).
			href = encodeDestination(r.currentBasename) + "#" + escapeFragment(nf)
			rewritten = true
		}
	} else if path, frag, ok := splitMarkdownAnchor(href); ok {
		if nf, ok := r.index.Anchor(r.resolveDocKey(path), decodeDestination(frag)); ok {
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
// not in the link index -- the last one warns (minimal R1: every reference that
// looks like it should resolve and doesn't is said out loud, via the same
// r.warnings list images.go already populates on a broken reference). The first
// two don't warn, because they were never meant to resolve here in the first
// place -- a mention, an attachment link, or an external URL.
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
	entry, ok := r.index.Page(r.resolveDocKey(path))
	if !ok {
		r.warnings = append(r.warnings, fmt.Sprintf("link not resolved: %s", href))
		return "", false
	}

	var newHref string
	if r.spaceKey != "" {
		slug := ""
		if entry.Title != "" {
			slug = url.QueryEscape(entry.Title)
		}
		newHref = fmt.Sprintf("%s/wiki/spaces/%s/pages/%s/%s",
			r.baseURL, r.spaceKey, entry.PageID, slug)
	} else {
		newHref = fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%s", r.baseURL, entry.PageID)
	}
	return newHref + fragment, true
}

// resolveDocKey resolves a link/anchor destination -- relative to r.baseDir,
// the referencing file's own directory, same as an image src -- to the
// root-relative, slash-separated path the link index is keyed by.
//
// An escaping destination (one that climbs above root) still returns its
// computed string rather than refusing it: the index is built by walking
// downward from root, so it can never contain an entry for a path outside it,
// and an escaping key is therefore already guaranteed to miss. That is what
// lets link resolution need no clamp at all (025's S2 discussion) where an
// image leaf and a parent: read still need one -- both of those are reads,
// and this is a lookup against data already collected inside root.
func (r *storageRenderer) resolveDocKey(dest string) string {
	abs, err := filepath.Abs(filepath.Join(r.baseDir, decodeDestination(dest)))
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(r.root.Dir, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
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
