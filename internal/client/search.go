package client

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// searchPath is the v1 CQL search endpoint. There is no v2 equivalent, and no
// non-CQL route reaches a folder by title -- /rest/api/content?type=folder
// answers 501 (docs/confluence/search.md).
const searchPath = "/wiki/rest/api/search"

// searchPageSize is the per-request page size for a CQL search.
const searchPageSize = 250

// maxSearchPages bounds the cursor walk. Nothing observed suggests the server
// loops, but this endpoint ignores start and reports a totalSize that disagrees
// with what it returns, so a next link that never clears is a cheap way to hang
// forever. Exceeding the bound is an error rather than a truncated list: a
// short answer that looks complete is the failure this whole file is written to
// avoid.
const maxSearchPages = 200

// excerptMode is the excerpt= value asked for on every search request.
//
// It is passed explicitly even though the server's default is identical, so what
// ships is what was tested. The tradeoff is recorded rather than avoided: an
// unrecognized excerpt= value returns 200 with an *empty* excerpt rather than an
// error, so if Atlassian ever renames this value, excerpts go silently missing
// (docs/confluence/search.md). "indexed" is not an alternative -- it truncates to
// 50 characters.
const excerptMode = "highlight"

// SearchResult is one row of a CQL search.
//
// The addressable fields live under content; the row's own url is
// context-relative ("/spaces/{key}/pages/{id}/{slug}" or
// "/spaces/{key}/folder/{id}"), so it yields a space key through
// SpaceKeyFromWebUI and an absolute URL by joining the site's /wiki context.
//
// EntityType is "content" for a page or folder. A query that does not constrain
// type can return rows whose Content is zero: `type = space` answers with
// entityType "space" and no content object at all, the addressable data sitting
// in a sibling space object instead. (`type = user` does *not* behave that way --
// it returns ordinary content rows, effectively unfiltered.)
type SearchResult struct {
	Content struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Status string `json:"status"`
		Title  string `json:"title"`
		Links  Links  `json:"_links"`
	} `json:"content"`
	EntityType string `json:"entityType"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Excerpt    string `json:"excerpt"`
}

// The markers Confluence wraps matched terms in. siteSearch ~ decorates both the
// row's title and its excerpt with them; 40 of 50 rows sampled carried them
// (docs/confluence/search.md).
const (
	highlightOpen  = "@@@hl@@@"
	highlightClose = "@@@endhl@@@"
)

// cleanExcerpt turns a raw search excerpt into a single line of plain text.
//
// Three passes, in this order:
//
//   - strip the highlight markers siteSearch ~ puts around every matched term;
//   - unescape HTML entities exactly once -- the field arrives escaped, as in
//     "Base Load Engineer&#39;s Hand-off Log";
//   - collapse every run of whitespace, including the newlines 40 of 50 rows
//     carry, to a single space, and trim.
//
// Stripping precedes unescaping because an entity-encoded marker has never been
// observed, while an entity that decodes to whitespace is handled by collapsing
// last. A page whose body literally contains "@@@hl@@@" loses it; that is
// accepted, and it is not a thing anyone writes.
//
// Collapsing happens here rather than at the point of display so there is one
// canonical excerpt: the human output and --json cannot disagree about it, and a
// --json consumer does not inherit the index's arbitrary line breaks.
func cleanExcerpt(s string) string {
	s = strings.ReplaceAll(s, highlightOpen, "")
	s = strings.ReplaceAll(s, highlightClose, "")
	// strings.Fields splits on any whitespace run and drops the empties, so
	// rejoining is the collapse and the trim at once.
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// escapeCQL escapes a value for use inside a double-quoted CQL string literal.
//
// This is not cosmetic. An unescaped quote ends the literal and the rest of the
// value becomes query syntax: title="a" or type=page matched 20508 rows on a
// site where the honest query matched one.
//
// Backslash is replaced first. Doing the quote first would leave the backslash
// pass re-escaping the escapes it had just introduced.
func escapeCQL(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// SearchCQL runs a CQL query and returns every row, following the cursor.
//
// The caller owns the query string and is responsible for escaping anything
// interpolated into it -- see escapeCQL, and prefer FindByTitle, which builds
// its own query, over assembling one here.
//
// Three things about this endpoint's paging, each of which fails silently if
// ignored (docs/confluence/search.md):
//
//   - start is accepted and ignored, so offset paging returns page one forever;
//   - a short page does not mean the end, so listV1's termination rule would
//     stop early -- only a missing next link ends the walk;
//   - totalSize is an estimate that has been observed both to drift between
//     pages and to be nonzero against an empty results array, so nothing here
//     may branch on it.
func (c *ConfluenceClient) SearchCQL(cql string) ([]SearchResult, error) {
	rows, _, err := c.searchCQLBounded(cql, 0)
	return rows, err
}

// searchCQLBounded is SearchCQL with a row bound. It returns at most max rows
// plus whether more were available; max <= 0 means every row.
//
// The bound exists because a full-text query is not a title lookup: one word can
// match thousands of pages, and walking all of them is thousands of rows over
// dozens of sequential requests for a caller who wanted to know whether a page
// existed.
//
// It asks the server for max+1 rows and reports the surplus as "more" rather than
// counting anything. totalSize cannot supply that count -- it drifted 294 -> 292
// -> 291 against 289 rows actually collected, and read 1 against an empty results
// array (docs/confluence/search.md).
func (c *ConfluenceClient) searchCQLBounded(cql string, max int) ([]SearchResult, bool, error) {
	var all []SearchResult
	rawURL := c.baseURL + searchPath
	params := url.Values{
		"cql":     {cql},
		"limit":   {strconv.Itoa(searchRequestSize(max))},
		"excerpt": {excerptMode},
	}

	for page := 0; rawURL != ""; page++ {
		if page >= maxSearchPages {
			return nil, false, fmt.Errorf("CQL search did not terminate after %d pages (query: %s)",
				maxSearchPages, cql)
		}
		var out struct {
			Results []SearchResult `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if err := c.doJSON(http.MethodGet, rawURL, params, nil, &out, timeoutRead); err != nil {
			return nil, false, err
		}
		all = append(all, out.Results...)
		if max > 0 && len(all) > max {
			return all[:max], true, nil
		}
		rawURL = searchNextURL(c.baseURL, out.Links.Next)
		// The next link carries the cursor, limit and cql already.
		params = nil
	}
	return all, false, nil
}

// searchRequestSize is the limit to ask for, given a row bound.
//
// One more than the caller needs, so the surplus answers "are there more?"
// without touching totalSize -- and never more than the page size, since a bound
// of 5 should be one small request rather than a 250-row fetch that discards 245.
func searchRequestSize(max int) int {
	if max <= 0 || max >= searchPageSize {
		return searchPageSize
	}
	return max + 1
}

// searchNextURL turns /search's _links.next into an absolute URL, re-attaching
// the excerpt mode.
//
// Unlike a v2 next link, this one is relative to the /wiki context --
// "/rest/api/search?next=true&cursor=..." -- so the prefix has to be added
// before resolveNext, which would otherwise build BASE/rest/api/search and 404.
// Going through resolveNext keeps the gateway handling in one place.
func searchNextURL(base, next string) string {
	if next == "" {
		return ""
	}
	abs := next
	if !strings.HasPrefix(next, "http://") && !strings.HasPrefix(next, "https://") {
		abs = resolveNext(base, "/wiki"+next)
	}
	return withExcerptMode(abs)
}

// withExcerptMode sets excerpt= on an already-built URL.
//
// The next link carries cql, limit and the cursor but *not* excerpt, and doJSON
// appends its params with a bare "?" -- so passing excerpt alongside a next link
// would produce a second "?" and a malformed URL, while passing nothing would
// request page two differently from page one. Setting it on the parsed URL is
// where both are fixable at once.
//
// A URL that will not parse is returned unchanged: it came from resolveNext or
// from the server, so failing the walk over it would trade a working request for
// an error.
func withExcerptMode(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set("excerpt", excerptMode)
	u.RawQuery = q.Encode()
	return u.String()
}
