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

// Content types a full-text search may be restricted to.
//
// This is markfluence's vocabulary, not Confluence's. The index also holds
// attachments, comments, databases and whiteboards, all of which match a
// full-text query and none of which is an id any markfluence command accepts.
// SearchTypeFolder is absent on purpose: full text cannot see a folder at all
// (docs/confluence/search.md), and the command that finds one is `find`.
const (
	SearchTypePage     = "page"
	SearchTypeBlogpost = "blogpost"
	// SearchTypeAll drops the type clause, returning whatever the index holds.
	SearchTypeAll = "all"
)

// SearchMatch is one full-text hit: addressable, and cleaned.
//
// Every field comes from the row's content object rather than from the row
// itself. The row-level title is HTML-escaped *and* carries highlight markers
// where content.title is neither, and that is not a corner case -- see
// docs/confluence/search.md. Nothing here may be switched back to the row.
//
// Type is a plain string rather than a closed set. A SearchTypeAll or raw-CQL
// search can return whiteboard, database, or whatever content type Atlassian adds
// next, and a closed set would turn a new one into a validation failure.
//
// There is deliberately no Status. CQL cannot see archived content, so every hit
// is current, and carrying the field would imply a distinction a full-text search
// cannot make.
type SearchMatch struct {
	ID      string
	Type    string
	Title   string
	Space   string
	URL     string
	Excerpt string
}

// SearchResults is what a full-text search returns.
//
// A struct rather than extra return values, so a field can be added without
// breaking every caller -- Skipped is the second field this shape grew.
type SearchResults struct {
	// Matches is in the server's relevance order, which is the only order there
	// is: score is 0.0 on every row of every query sampled, so nothing may
	// re-sort these.
	Matches []SearchMatch
	// More reports that the row bound was reached with rows still available. A
	// flag rather than a count, because totalSize cannot supply one.
	More bool
	// Skipped counts index rows with no addressable content object. `type =
	// space` answers with hundreds of them, carrying a space object instead.
	// They are counted rather than dropped quietly: a query matching only those
	// would otherwise look like a successful empty result, and an empty result is
	// what a caller acts on.
	Skipped int
}

// SearchText runs a full-text search, optionally restricted to a space key and to
// one content type, returning at most limit matches (limit <= 0 for all of them).
//
// The query is ranked by `siteSearch ~`, not the `text ~` that Atlassian
// documents. That is not a stylistic choice: for "deploy runbook", `text ~`
// ranked six unrelated pages above every page whose title contains both words,
// while `siteSearch ~` returned them in order. With score at 0.0 on every row
// there is no client-side re-ranking to fall back on, so the field choice is the
// whole of the ranking (docs/confluence/search.md).
//
// See buildTextCQL for why the clause order matters, and why `text ~` is in the
// query anyway. Both guard a failure mode in which a scoped search silently
// returns every page in the space.
//
// Multi-word queries are ANDed across terms and are not phrase matches -- word
// order does not matter, and one non-matching term empties the result.
//
// contentType is one of the SearchType constants; SearchTypeAll leaves the query
// untyped, which is the only way this returns rows a markfluence command cannot
// use. The caller is expected to have validated it.
func (c *ConfluenceClient) SearchText(query, spaceKey, contentType string, limit int) (SearchResults, error) {
	if spaceKey != "" {
		// The id is discarded: CQL matches on the key. This call exists only to
		// refuse an unknown key, which CQL would answer with zero rows -- reading
		// exactly like "no matches", on which a caller's next move is to create
		// the page it thinks is missing. Same guard, same reason, as FindByTitle.
		spaceID, err := c.ResolveSpaceID(spaceKey)
		if err != nil {
			return SearchResults{}, err
		}
		if spaceID == "" {
			return SearchResults{}, fmt.Errorf("%w: %q", ErrSpaceNotFound, spaceKey)
		}
	}
	return c.searchMatches(buildTextCQL(query, spaceKey, contentType), limit)
}

// SearchRawCQL runs a caller-supplied CQL query.
//
// Nothing is escaped, prepended or appended: the caller owns the query, which is
// the entire point of the escape hatch. A space or type clause added here would
// silently regroup a query containing `or` and answer a different question, which
// is why the command refuses those flags alongside raw CQL rather than merging
// them.
func (c *ConfluenceClient) SearchRawCQL(cql string, limit int) (SearchResults, error) {
	return c.searchMatches(cql, limit)
}

// buildTextCQL assembles the full-text query.
//
// Two things here are load-bearing and neither is cosmetic. Both exist because
// Confluence will silently drop the siteSearch clause and answer a completely
// different question (docs/confluence/search.md).
//
// **The siteSearch clause must come first.** Confluence drops it when it is the
// *middle* clause of three: `type = page and siteSearch ~ "netlify" and space =
// "SRE"` returned 1122 rows -- byte-identical to `type = page and space = "SRE"`,
// every page in the space -- while the same three clauses with siteSearch leading
// returned the honest 15. First and last are both safe; middle is not. This is a
// server-side parser bug, so the ordering below is the fix, and TestBuildTextCQL
// pins it.
//
// **The redundant text clause is a safety net.** siteSearch supplies the ranking
// and text supplies nothing but a floor: if the clause is ever dropped again --
// by a reordering here, or by a change on Atlassian's side -- text still
// constrains the results to content that actually contains the query. That turns
// the failure mode from "every page in the space, looking like a working search"
// into "the right pages, in a worse order". It costs the handful of hits siteSearch
// matches and text does not (15 -> 14 on the query above); ranking is unchanged.
//
// Both the query and the space key go through escapeCQL: a bare quote in either
// ends the string literal and turns the remainder into query syntax.
func buildTextCQL(query, spaceKey, contentType string) string {
	escaped := escapeCQL(query)
	cql := fmt.Sprintf(`siteSearch ~ "%s" and text ~ "%s"`, escaped, escaped)
	if contentType != "" && contentType != SearchTypeAll {
		cql += " and type = " + contentType
	}
	if spaceKey != "" {
		cql += fmt.Sprintf(` and space = "%s"`, escapeCQL(spaceKey))
	}
	return cql
}

// searchMatches runs a bounded query and converts its rows into matches.
//
// Note that a skipped row is not replaced: a bound of 25 over rows including
// three content-less ones yields 22 matches. That is honest -- 25 rows were
// examined -- and it is unreachable unless the caller asked for an untyped or raw
// query.
func (c *ConfluenceClient) searchMatches(cql string, limit int) (SearchResults, error) {
	rows, more, err := c.searchCQLBounded(cql, limit)
	if err != nil {
		return SearchResults{}, err
	}
	out := SearchResults{Matches: make([]SearchMatch, 0, len(rows)), More: more}
	for _, r := range rows {
		if r.Content.ID == "" {
			out.Skipped++
			continue
		}
		// webui and the row's url were byte-identical on all 50 rows sampled, so
		// the fallback is belt-and-braces rather than a known case -- but a row
		// with neither would otherwise lose both its space key and its URL.
		link := r.Content.Links.WebUI
		if link == "" {
			link = r.URL
		}
		out.Matches = append(out.Matches, SearchMatch{
			ID:      r.Content.ID,
			Type:    r.Content.Type,
			Title:   r.Content.Title,
			Space:   SpaceKeyFromWebUI(link),
			URL:     c.contextURL(link),
			Excerpt: cleanExcerpt(r.Excerpt),
		})
	}
	return out, nil
}
