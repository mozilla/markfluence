package convert

import (
	"fmt"
	"html"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark/ast"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// renderLink renders a link, rewriting same-page and cross-file anchors to
// Confluence ids and sibling .md links to their published Confluence URLs.
// Links it does not rewrite fall back to the default goldmark rendering. A
// link whose target is Broken (not merely unresolved) replaces the whole
// element -- tags and visible text alike -- with literal "LINK BROKEN: ..."
// text, matching images.go's precedent for a missing image: this is a defect
// shipped to readers, not just a diagnostic.
func (r *storageRenderer) renderLink(
	w util.BufWriter, _ []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if !entering {
		if r.linkBrokenText != "" {
			// entering wrote the replacement text and skipped children; no
			// <a> was opened, so there is nothing to close.
			return ast.WalkContinue, nil
		}
		_, _ = w.WriteString("</a>")
		return ast.WalkContinue, nil
	}
	href, rewritten, brokenText := r.rewriteHref(string(n.Destination))
	r.linkBrokenText = brokenText
	if brokenText != "" {
		_, _ = w.WriteString(html.EscapeString(brokenText))
		return ast.WalkSkipChildren, nil
	}
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
// verbatim rather than re-escaped) -- or, when the link resolves to a Broken
// target, the literal replacement text to render instead of any <a> element,
// in which case href/rewritten are meaningless and must not be used.
func (r *storageRenderer) rewriteHref(href string) (newHref string, rewritten bool, brokenText string) {
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
		key, _ := r.resolveDocKey(path)
		if nf, ok := r.index.Anchor(key, decodeDestination(frag)); ok {
			// path keeps the spelling it was written with: it is a destination,
			// and the doc-link step decodes it again for its own lookup.
			href = path + "#" + escapeFragment(nf)
			rewritten = true
		}
	}

	newHref, ok, brokenText := r.rewriteDocLink(href)
	if brokenText != "" {
		return "", false, brokenText
	}
	if ok {
		return newHref, true, ""
	}
	return href, rewritten, ""
}

// rewriteDocLink rewrites a sibling .md href (with optional fragment) to its
// Confluence URL. ok=false with brokenText=="" means an absolute URL, a
// non-.md href, or a target that exists but has no page_id yet -- none of
// these are errors; the last one warns (minimal R1: reported, not silently
// dead) via the same r.warnings list images.go already populates on a broken
// reference. A non-empty brokenText means the target is Broken -- missing
// entirely, or resolving outside the documentation root -- and the caller
// must render that text in place of any link element, the way images.go
// already does for a missing image. FileExists is what tells "missing
// entirely" apart from "not published yet": both look identical to
// index.Page (a miss), but only the first is a defect.
func (r *storageRenderer) rewriteDocLink(href string) (newHref string, ok bool, brokenText string) {
	path, fragment := href, ""
	if i := strings.Index(href, "#"); i >= 0 {
		path, fragment = href[:i], href[i:]
	}
	if !strings.HasSuffix(path, ".md") {
		return "", false, ""
	}
	if strings.Contains(path, "://") || strings.HasPrefix(path, "//") {
		return "", false, ""
	}
	key, escapes := r.resolveDocKey(path)
	entry, found := r.index.Page(key)
	if !found {
		switch {
		case escapes:
			msg := fmt.Sprintf("LINK BROKEN: %s (outside the documentation root)", href)
			r.broken = append(r.broken, msg)
			return "", false, msg
		case !r.index.FileExists(key):
			msg := fmt.Sprintf("LINK BROKEN: %s (not found)", href)
			r.broken = append(r.broken, msg)
			return "", false, msg
		default:
			// The file exists but has no page_id yet -- the normal state of
			// every page in a tree that hasn't been published, not an error.
			r.warnings = append(r.warnings, fmt.Sprintf("link not resolved: %s", href))
			return "", false, ""
		}
	}

	var built string
	if r.spaceKey != "" {
		slug := ""
		if entry.Title != "" {
			slug = url.QueryEscape(entry.Title)
		}
		built = fmt.Sprintf("%s/wiki/spaces/%s/pages/%s/%s",
			r.baseURL, r.spaceKey, entry.PageID, slug)
	} else {
		built = fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%s", r.baseURL, entry.PageID)
	}
	return built + fragment, true, ""
}

// resolveDocKey resolves a link/anchor destination -- relative to r.baseDir,
// the referencing file's own directory, same as an image src -- to the
// root-relative, slash-separated path the link index is keyed by, and
// whether that path escapes the documentation root.
//
// The index itself needs no clamp: it is built by walking downward from
// root, so it can never contain an entry for a path outside it, and an
// escaping key is therefore already guaranteed to miss (025's S2
// discussion). escapes exists only so a caller can tell that guaranteed miss
// apart from a genuine "not found" for Broken-severity reporting -- a purely
// lexical check, since nothing here reads the filesystem the way images.go's
// os.Root-backed check does.
func (r *storageRenderer) resolveDocKey(dest string) (key string, escapes bool) {
	abs, err := filepath.Abs(filepath.Join(r.baseDir, decodeDestination(dest)))
	if err != nil {
		return "", false
	}
	key, _ = rootRelativeKey(r.root, abs)
	return key, key == ".." || strings.HasPrefix(key, "../")
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
