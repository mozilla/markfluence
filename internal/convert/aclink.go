package convert

// aclink.go converts <ac:link> -- the Confluence editor's internal link -- back
// to markdown.
//
// One rule decides every form: convert when the markdown republishes to a link
// resolving to the same target, pass the storage through when it would not.
// Passthrough is not a failure mode here. MdToConfluence's ac:/ri: shield
// republishes raw storage byte-identical, so a mention or an attachment link
// survives a round trip intact where a markdown link would quietly break. What
// each form looks like in the wild, with counts, is in
// docs/confluence/links-and-anchors.md.

import (
	"fmt"
	"strings"
)

// PageLinkTarget identifies the page an <ac:link> points at. Confluence names it
// by title and never by id, so turning one into a URL takes a lookup -- which is
// the caller's job, since internal/convert holds no client.
//
// An empty SpaceKey means the link named no space, which Confluence reads as the
// space of the page the link is on. It is left empty rather than filled in here
// for the same reason: only the caller knows what page this body came from.
type PageLinkTarget struct {
	SpaceKey string
	Title    string
}

// StorageOptions carries what StorageToMarkdown cannot work out for itself.
// Every field is optional, and each one absent degrades to a worse rendering
// rather than an error -- a read is worth completing without any of them.
type StorageOptions struct {
	// Sources maps an attachment name to the markdown image path it was
	// published from, as recorded on the attachment when markfluence uploaded
	// it. A nil map, or a name missing from it, falls back to decoding the
	// attachment name: exact for names markfluence created, a best-effort guess
	// for hand-uploaded ones.
	Sources map[string]string

	// PageLinks maps each page an <ac:link> points at to its absolute URL, as
	// gathered by PageLinkTargets and resolved by the caller. A target missing
	// from it passes through as raw storage rather than becoming a link with
	// nowhere to go.
	PageLinks map[PageLinkTarget]string

	// SiteURL is the Confluence site base, used to build a space link. It must
	// be the site and never the gateway, since these URLs are published back
	// into a page. Empty passes space links through.
	SiteURL string
}

// PageLinkTargets returns every page an <ac:link> in this storage points at, so
// a caller holding a client can resolve them and hand the URLs back through
// StorageOptions.PageLinks. Targets are deduplicated, in document order.
//
// It reports targets inside raw-serialized macros too, which the renderer will
// never convert. Matching the renderer's macro rules here would mean keeping two
// copies of them in step, and the cost of being wrong is one wasted lookup
// rather than a wrong answer.
//
// A parse failure yields no targets rather than an error: the caller is about to
// call StorageToMarkdown on the same storage, which reports it once.
func PageLinkTargets(storage string) []PageLinkTarget {
	root, err := parseStorage(storage)
	if err != nil {
		return nil
	}
	var out []PageLinkTarget
	seen := map[PageLinkTarget]bool{}
	walkNodes(root, func(n *snode) {
		if n.name != "ac:link" {
			return
		}
		p := findChild(n, "ri:page")
		if p == nil {
			return
		}
		if t := pageTarget(p); t.Title != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	})
	return out
}

// pageTarget reads an <ri:page> element as a link target.
func pageTarget(n *snode) PageLinkTarget {
	return PageLinkTarget{
		SpaceKey: n.attrs["ri:space-key"],
		Title:    n.attrs["ri:content-title"],
	}
}

// walkNodes calls fn on n and every descendant, in document order.
func walkNodes(n *snode, fn func(*snode)) {
	fn(n)
	for _, k := range n.kids {
		walkNodes(k, fn)
	}
}

// headingSlugs maps each heading's Confluence anchor to its GitHub one, which is
// how a same-page <ac:link ac:anchor="..."> recovers a markdown fragment.
//
// The slug cannot be inverted -- confluenceSlug turns both a space and a hyphen
// into "-", so "DOM-Security-Team" could have come from either -- but the
// heading that produced it is in the document being converted, so the mapping is
// exact rather than guessed.
func headingSlugs(root *snode) map[string]string {
	out := map[string]string{}
	walkNodes(root, func(n *snode) {
		if len(n.name) != 2 || n.name[0] != 'h' || n.name[1] < '1' || n.name[1] > '6' {
			return
		}
		if text := strings.TrimSpace(collapse(textContent(n))); text != "" {
			out[confluenceSlug(text)] = githubSlug(text)
		}
	})
	return out
}

// renderACLink renders an <ac:link> as a markdown link where the link would
// still resolve after being republished, and as raw storage where it would not.
func (r *mdRenderer) renderACLink(n *snode) string {
	anchor := n.attrs["ac:anchor"]
	target := acLinkTarget(n)
	switch {
	case target == nil:
		if anchor != "" {
			return r.renderAnchorLink(n, anchor)
		}
		// No target and no anchor: there is nothing to point at.
		return serialize(n)
	case target.name == "ri:page":
		return r.renderPageLink(n, target, anchor)
	case target.name == "ri:space":
		return r.renderSpaceLink(n, target)
	default:
		// ri:user has no markdown equivalent at all; ri:attachment would
		// republish as a dead relative href, since only images are uploaded
		// (images.go); ri:blog-post cannot be resolved to an id, because
		// SearchPagesByTitle does not see blog posts. All three round-trip
		// exactly as storage.
		return serialize(n)
	}
}

// acLinkTarget returns the ri:* element naming what the link points at, or nil
// for a link that names nothing (an anchor on the current page, or a broken one).
func acLinkTarget(n *snode) *snode {
	for _, k := range n.kids {
		if strings.HasPrefix(k.name, "ri:") {
			return k
		}
	}
	return nil
}

// renderPageLink renders a link to another page, whose URL the caller resolved.
func (r *mdRenderer) renderPageLink(n, target *snode, anchor string) string {
	href, ok := r.pageLinks[pageTarget(target)]
	if !ok {
		return serialize(n)
	}
	if anchor != "" {
		// Appended verbatim, still percent-encoded: it is a URL fragment, and
		// this is the spelling Confluence itself writes.
		href += "#" + anchor
	}
	return mdLink(r.acLinkText(n, target.attrs["ri:content-title"]), href)
}

// renderSpaceLink renders a link to a space, which needs no lookup -- a space
// key is its own URL segment.
func (r *mdRenderer) renderSpaceLink(n, target *snode) string {
	key := target.attrs["ri:space-key"]
	if key == "" || r.siteURL == "" {
		return serialize(n)
	}
	return mdLink(r.acLinkText(n, key), r.siteURL+"/wiki/spaces/"+key)
}

// renderAnchorLink renders a link to a heading on this same page.
func (r *mdRenderer) renderAnchorLink(n *snode, anchor string) string {
	slug, ok := r.headingSlugs[decodeDestination(anchor)]
	if !ok {
		// A "#slug" matching no heading publishes as a dead relative href, and
		// the forward path says nothing about it. Keep the storage, which works.
		return serialize(n)
	}
	return mdLink(r.acLinkText(n, anchor), "#"+slug)
}

// acLinkText is the link's visible text: its body if it has one, else fallback.
// Confluence displays the target's own name for a bodyless link, so the fallback
// is that name.
//
// A body comes in two spellings -- ac:link-body holds rich text, and
// ac:plain-text-link-body holds CDATA -- and both occur on real pages.
func (r *mdRenderer) acLinkText(n *snode, fallback string) string {
	if b := findChild(n, "ac:link-body"); b != nil {
		if s := r.renderInlineChildren(b); s != "" {
			return s
		}
	}
	if b := findChild(n, "ac:plain-text-link-body"); b != nil {
		if s := strings.TrimSpace(collapse(textContent(b))); s != "" {
			return s
		}
	}
	return fallback
}

// mdLink renders an inline markdown link, falling back to showing the
// destination when there is no text for it.
func mdLink(text, dest string) string {
	if text == "" {
		text = dest
	}
	return fmt.Sprintf("[%s](%s)", text, dest)
}
