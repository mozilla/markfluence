package convert

import (
	"net/url"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// mentionPathRE would be the obvious tool here and is deliberately not used:
// an account id has no fixed shape. Two are live on one instance --
// 60c36d0718e9f60071326951 (24 hex characters, no prefix) and
// 712020:0e5f8a21-3c4d-4e5f-a6b7-c8d9e0f1a2b3 (prefix, colon, UUID) -- so a
// pattern tight enough to describe one rejects the other. The id is whatever
// the last path segment is, and the only real validation is asking the server,
// which is what the caller's warning does.

// mentionAccountID reports the account id a markdown destination mentions, or
// "" when the destination is not a profile URL.
//
// Matching is on the **path**, ignoring host and query, because several
// spellings of the same target are in circulation and every one of them will be
// pasted into a file by somebody:
//
//	home.atlassian.com/people/{id}                     what markfluence emits
//	home.atlassian.com/people/{id}?cloudId=…           Confluence's person modal
//	home.atlassian.com/o/{orgId}/people/{id}?cloudId=… the redirect target
//	{site}/wiki/people/{id}                            Confluence's own renderer
//	{site}/wiki/display/~{id}                          legacy; 302s to the above
//	/people/{id}, /wiki/people/{id}                    root-relative
//
// Ignoring the query follows the same logic as ignoring the host: neither
// cloudId nor ref nor the /o/{orgId} segment identifies the person. Only the id
// does, and <ri:user ri:account-id="…"/> stores nothing else -- so requiring
// any of them to match would invent a constraint the stored data does not have.
//
// The host being irrelevant is also what lets `check` recognise a mention with
// no client and no site: see mentionHost.
func mentionAccountID(dest string) string {
	// Absolute or root-relative only. A *relative* destination is a local file
	// reference, and treating one as a profile URL destroyed real doc links:
	// "[@ada](../people/ada.md)" in a tree with a people/ directory published
	// as a mention of account "ada.md", with the link gone and rewriteDocLink's
	// LINK BROKEN and not-yet-published checks skipped entirely, because this
	// runs first. Found in review.
	path := dest
	if u, err := url.Parse(dest); err == nil {
		if u.Scheme == "" && !strings.HasPrefix(dest, "/") {
			return ""
		}
		if u.Path != "" {
			path = u.Path
		}
	} else {
		if !strings.HasPrefix(dest, "/") {
			return ""
		}
		if i := strings.IndexAny(dest, "?#"); i >= 0 {
			path = dest[:i]
		}
	}
	path = strings.TrimSuffix(path, "/")
	// Confluence's own forms live under /wiki; Atlassian Home's do not.
	path = strings.TrimPrefix(path, "/wiki")

	// Anchored at the start of the path, not searched for anywhere in it. A
	// LastIndex of "/people/" matched any prefix at all, so
	// "https://github.com/orgs/mozilla/people/willkg" became a mention of
	// account "willkg".
	switch {
	case strings.HasPrefix(path, "/people/"):
		return trimProfileID(strings.TrimPrefix(path, "/people/"))
	case strings.HasPrefix(path, "/display/~"):
		// The legacy Confluence form spells the id as a personal space key.
		return trimProfileID(strings.TrimPrefix(path, "/display/~"))
	case strings.HasPrefix(path, "/o/"):
		// The org-scoped redirect target: /o/{orgId}/people/{id}.
		rest := strings.TrimPrefix(path, "/o/")
		i := strings.Index(rest, "/people/")
		if i < 0 || strings.Contains(rest[:i], "/") {
			return ""
		}
		return trimProfileID(rest[i+len("/people/"):])
	}
	return ""
}

// trimProfileID accepts a last path segment as an account id, rejecting
// anything with a further slash in it -- that would mean /people/ named a
// collection rather than a person.
func trimProfileID(seg string) string {
	if seg == "" || strings.Contains(seg, "/") {
		return ""
	}
	// A destination is a URL, so the id may be percent-encoded -- a colon
	// often is. Decoding failure leaves it as written, which then simply will
	// not resolve, and the caller's warning says so.
	if decoded, err := url.PathUnescape(seg); err == nil {
		return decoded
	}
	return seg
}

// mentionMarker is what a link's text must start with for the link to publish
// as a mention rather than as an ordinary link to somebody's profile page.
//
// Load-bearing, not decoration. The URL cannot tell the two intents apart --
// "mention this person" and "link to this person's profile" point at the same
// place -- so without a marker, anyone who deliberately wrote the second would
// silently get the first. The cost is that link *text* now carries meaning, so
// stripping the "@" changes what publishes; accepted, because the alternative
// is a silent reinterpretation and this one is visible in a diff.
const mentionMarker = "@"

// mentionFor reports the account id a link should publish as a mention, or ""
// when it should stay an ordinary link.
func mentionFor(n *ast.Link, source []byte) string {
	if !strings.HasPrefix(linkTextOf(n, source), mentionMarker) {
		return ""
	}
	return mentionAccountID(string(n.Destination))
}

// linkTextOf is a link's visible text, concatenated from its text descendants.
//
// Only the leading character is ever examined, but the whole string is built
// rather than just the first text node: "[**@Ada**](…)" and "[`@Ada`](…)" both
// nest the "@" a level down, and reading one node deep would miss it and
// publish a plain link instead of a mention.
func linkTextOf(n *ast.Link, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := c.(*ast.Text); ok {
				b.Write(t.Segment.Value(source))
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// mentionElement is the storage a mention publishes as.
//
// No ri:local-id: it is a per-instance server-generated id, and a mention
// published with only the account id resolves to the same person -- verified
// against the live API, read back as an ADF mention node
// (docs/confluence/links-and-anchors.md). So it belongs in droppedAttrs'
// category, and omitting it loses nothing.
func mentionElement(accountID string) string {
	return `<ac:link><ri:user ri:account-id="` + xmlAttrEscape(accountID) + `" /></ac:link>`
}
