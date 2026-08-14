package client

import (
	"fmt"
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
	var all []SearchResult
	rawURL := c.baseURL + searchPath
	params := url.Values{"cql": {cql}, "limit": {strconv.Itoa(searchPageSize)}}

	for page := 0; rawURL != ""; page++ {
		if page >= maxSearchPages {
			return nil, fmt.Errorf("CQL search did not terminate after %d pages (query: %s)",
				maxSearchPages, cql)
		}
		var out struct {
			Results []SearchResult `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if err := c.doJSON(http.MethodGet, rawURL, params, nil, &out, timeoutRead); err != nil {
			return nil, err
		}
		all = append(all, out.Results...)
		rawURL = searchNextURL(c.baseURL, out.Links.Next)
		// The next link carries the cursor, limit and cql already.
		params = nil
	}
	return all, nil
}

// searchNextURL turns /search's _links.next into an absolute URL.
//
// Unlike a v2 next link, this one is relative to the /wiki context --
// "/rest/api/search?next=true&cursor=..." -- so the prefix has to be added
// before resolveNext, which would otherwise build BASE/rest/api/search and 404.
// Going through resolveNext keeps the gateway handling in one place.
func searchNextURL(base, next string) string {
	if next == "" {
		return ""
	}
	if strings.HasPrefix(next, "http://") || strings.HasPrefix(next, "https://") {
		return next
	}
	return resolveNext(base, "/wiki"+next)
}
