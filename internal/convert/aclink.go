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

	"github.com/mozilla/markfluence/internal/linkindex"
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

	// UserNames carries what the caller learned about each mentioned account
	// id, in three states:
	//
	//   - id -> a name: render the mention as a link to that person
	//   - id -> "": the account genuinely does not resolve, so render a
	//     placeholder (see unknownUserName) -- still a link, because the id is
	//     the useful part and a reader should not have to read XML to find it
	//   - id absent: nothing is known, because the lookup could not be made, so
	//     pass the storage through untouched
	//
	// The last two have to stay distinct. Rendering a placeholder for a lookup
	// that merely failed would write a fabricated name over a real one across
	// every page of an export, in a file that then looks authoritative.
	//
	// Note what is *not* here: a site. The profile URL a mention renders to
	// lives on Atlassian Home rather than on the Confluence site, so it needs
	// nothing from the caller's configuration -- see renderUserMention.
	UserNames map[string]string

	// PageDir is where the page's markdown file sits, relative to the root of
	// whatever is being written -- "" for a file at that root, "home" for
	// dest/home/child.md's parent, and so on, in slash form.
	//
	// It exists because a markdown destination is resolved relative to the file
	// that carries it, while an attachment's recorded path is relative to the
	// root. Those coincide only for a file at the root, which is the only case
	// single-page export ever produced. Writing a tree ends the coincidence: a
	// page at dest/home/child.md referencing a recorded assets/brand.png has to
	// say ../assets/brand.png, or publishing the export back reports
	// IMAGE BROKEN for every shared asset below the root.
	//
	PageDir string

	// AttachmentDir is where an attachment with *no* recorded path is placed,
	// relative to that same root: the directory named after the page, beside
	// the page's own file, so that two pages' same-named native attachments
	// cannot collide (internal/attachfile).
	//
	// It is a second field rather than PageDir plus a slug because deriving it
	// would mean this package knowing how a title becomes a directory name,
	// which is internal/pageslug's business and is applied by the caller that
	// also decides where the file goes.
	//
	// Empty means the root, not "beside the page": with PageDir "home" it
	// yields ../diagram.png. Every caller sets both together through
	// pagedoc.Options, and one that sets only PageDir would point every native
	// attachment a directory too high.
	AttachmentDir string
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

// MentionTargets returns every account id mentioned in this storage, so a
// caller holding a client can resolve them to display names and hand the names
// back through StorageOptions.UserNames. Ids are deduplicated, in document
// order.
//
// PageLinkTargets' sibling, and it reports ids inside raw-serialized macros for
// the same reason: matching the renderer's macro rules here would mean keeping
// two copies of them in step, and the cost of being wrong is one wasted lookup
// rather than a wrong answer.
//
// A parse failure yields no targets rather than an error, since the caller is
// about to call StorageToMarkdown on the same storage, which reports it once.
func MentionTargets(storage string) []string {
	root, err := parseStorage(storage)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	walkNodes(root, func(n *snode) {
		if n.name != "ac:link" {
			return
		}
		u := findChild(n, "ri:user")
		if u == nil {
			return
		}
		if id := u.attrs["ri:account-id"]; id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
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
// The slug cannot be inverted -- linkindex.ConfluenceSlug turns both a space and
// a hyphen into "-", so "DOM-Security-Team" could have come from either -- but
// the heading that produced it is in the document being converted, so the
// mapping is exact rather than guessed.
func headingSlugs(root *snode) map[string]string {
	out := map[string]string{}
	walkNodes(root, func(n *snode) {
		if len(n.name) != 2 || n.name[0] != 'h' || n.name[1] < '1' || n.name[1] > '6' {
			return
		}
		if text := strings.TrimSpace(collapse(textContent(n))); text != "" {
			out[linkindex.ConfluenceSlug(text)] = linkindex.GithubSlug(text)
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
	case target.name == "ri:user":
		return r.renderUserMention(n, target)
	default:
		// ri:attachment would republish as a dead relative href, since only
		// images are uploaded (images.go); ri:blog-post cannot be resolved to
		// an id, because SearchPagesByTitle does not see blog posts. Both
		// round-trip exactly as storage.
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

// mentionHost is where a user profile lives. Atlassian Home, not the Confluence
// site -- and that is the finding the markdown spelling rests on, not a detail.
// Confluence's own renderer still emits {site}/wiki/people/{accountId}, and
// that URL no longer resolves usefully in a browser: it bounces through a
// separate login and lands on a blank page. This one works, with no cloudId
// parameter needed (docs/confluence/links-and-anchors.md).
//
// A consequence worth knowing before changing it: because the URL names no
// site, a mention in markdown carries no site either. Nothing here depends on
// SiteURL, on CONFLUENCE_CLOUD_ID, or on which instance the page came from, so
// the same mention converts identically everywhere -- which is also what lets
// `check` recognise one with no client at all.
const mentionHost = "https://home.atlassian.com"

// MentionURL is the profile URL a mention renders to. Exported because the
// forward direction has to recognise what this direction emits, and a second
// copy of the path would be a second thing to keep in step.
func MentionURL(accountID string) string {
	return mentionHost + "/people/" + accountID
}

// unknownUserName is the display name rendered for a mention whose account
// genuinely does not resolve.
//
// Rendered as a link rather than passed through as storage, because the
// alternative is worse for a reader: passthrough puts a wall of XML in one
// table cell and a name in every other, and the unresolvable id -- the one
// useful thing left -- becomes the hardest part to find.
//
// It mirrors what Confluence itself renders, and the reason is a measurement
// that overturned the first choice here. A *deactivated* account resolves
// perfectly well: surveying every mention on a real page (18 of them, six
// departed) the user lookup answered 200 for all 18, returning names like
// "Mark Reid (Deactivated)" -- Confluence appends the suffix itself. So a
// departed colleague keeps their name and this branch is never reached for
// them.
//
// What does reach it is an id that genuinely does not exist: a typo, a
// hand-edited URL. That is exactly the case Confluence renders as
// "@Unlicensed user", verified via ADF. Since the only ids that land here are
// the ones the page will label that way, matching the wording means the
// markdown and the rendered page agree instead of offering a reader two
// different words for one thing.
//
// The earlier value was "Unknown user", argued on the premise that a
// deactivated account would take this path and "unlicensed" would assert a
// reason nobody checked. The premise was false.
const unknownUserName = "Unlicensed user"

// renderUserMention renders a mention as a link to the person's profile, with
// their display name as the text and an "@" marking it as a mention.
//
// The "@" is what the forward direction reads to tell a mention from a
// deliberate link to somebody's profile page: the URL alone cannot, since both
// spellings point at the same place. So it is load-bearing rather than
// decoration, even though recognition is by URL.
//
// The three UserNames states map to three outcomes -- a name, the placeholder,
// or untouched storage. See StorageOptions.UserNames for why the last two are
// not the same thing.
func (r *mdRenderer) renderUserMention(n, target *snode) string {
	id := target.attrs["ri:account-id"]
	if id == "" {
		return serialize(n)
	}
	name, known := r.userNames[id]
	if !known {
		return serialize(n)
	}
	if name == "" {
		name = unknownUserName
	}
	return mdLink("@"+escapeLinkText(name), MentionURL(id))
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
//
// Only the *raw* sources are escaped, and which is which is the whole point of
// the split below. An ac:link-body has already been rendered to markdown by
// renderInlineChildren, so escaping it would turn a bold link body into a
// literal "\*\*bold\*\*". The CDATA body and the fallback are plain text
// straight off the server -- a page title, a space key, an anchor -- and a "]"
// in any of them ends the link early.
func (r *mdRenderer) acLinkText(n *snode, fallback string) string {
	if b := findChild(n, "ac:link-body"); b != nil {
		if s := r.inlineTextForLink(b); s != "" {
			return s
		}
	}
	if b := findChild(n, "ac:plain-text-link-body"); b != nil {
		if s := strings.TrimSpace(collapse(textContent(b))); s != "" {
			return escapeLinkText(s)
		}
	}
	return escapeLinkText(fallback)
}

// inlineTextForLink renders a node's children as a markdown link's text,
// escaping the result when it is nothing but plain text.
//
// The distinction matters both ways, and an earlier version got it wrong in one
// direction. Escaping a *rendered* body turns "<strong>bold</strong>" into a
// literal "\*\*bold\*\*", which is why the escaping was first applied only to
// the raw sources. But the common case for a link body is plain text, and
// leaving it unescaped loses the link outright: a page titled "Q1 Draft]"
// rendered as "[Q1 Draft] notes](url)", which CommonMark reads as literal text,
// so the next update publishes no link at all. Found in review.
//
// "Nothing but plain text" is checkable rather than guessable: a text node is
// an snode with an empty name, so a body whose every descendant is one carries
// no markup for escaping to damage.
func (r *mdRenderer) inlineTextForLink(n *snode) string {
	rendered := r.renderInlineChildren(n)
	if !onlyText(n) {
		return rendered
	}
	return escapeLinkText(rendered)
}

// onlyText reports whether every descendant of n is a text node, so rendering
// it produced no markdown syntax of its own.
func onlyText(n *snode) bool {
	for _, k := range n.kids {
		if k.name != "" || !onlyText(k) {
			return false
		}
	}
	return true
}

// escapeLinkText makes plain text safe to use as a markdown link's text.
//
// The set is deliberately the one that *breaks* a link rather than everything
// markdown reads specially: an unescaped "]" ends the text early and leaves the
// rest of the line as literal junk, and a backslash has to go first or it would
// escape the escapes. A title like "*Foo*" is a different problem -- it renders
// as emphasis instead of as asterisks, losing fidelity without breaking the
// link -- and is knowingly not handled here, since escaping every markdown
// indicator in every recovered title is a larger change with its own round-trip
// consequences.
//
// Before this, mdLink was a bare Sprintf: any page title holding a bracket
// exported as a broken link, which mentions turned from theoretical into likely
// because display names carry them.
func escapeLinkText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "[", `\[`)
	return strings.ReplaceAll(s, "]", `\]`)
}

// mdLink renders an inline markdown link, falling back to showing the
// destination when there is no text for it.
func mdLink(text, dest string) string {
	if text == "" {
		text = dest
	}
	return fmt.Sprintf("[%s](%s)", text, dest)
}
