package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// urlLog is a test server that returns canned responses in order and records
// the full request URL of each call. The shared scripted server records only
// methods, and every trap this file covers is about the URL that gets requested
// next.
type urlLog struct {
	bodies []string
	urls   []string
	idx    int
}

func newSearchServer(t *testing.T, bodies ...string) (*ConfluenceClient, *urlLog) {
	t.Helper()
	s := &urlLog{bodies: bodies}
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		s.urls = append(s.urls, r.URL.String())
		if s.idx >= len(s.bodies) {
			t.Errorf("unexpected extra request: %s", r.URL)
			w.WriteHeader(500)
			return
		}
		body := s.bodies[s.idx]
		s.idx++
		_, _ = w.Write([]byte(body))
	})
	return c, s
}

// row builds a minimal folder search hit.
func row(id string) string {
	return fmt.Sprintf(
		`{"entityType":"content","title":"T","url":"/spaces/ENG/folder/%s",`+
			`"content":{"id":%q,"type":"folder","status":"current","title":"T"}}`, id, id)
}

// page wraps rows in a search response, with next when nextURL is non-empty.
func page(nextURL string, rows ...string) string {
	links := `"_links":{"base":"https://wiki.example.net/wiki","context":"/wiki"}`
	if nextURL != "" {
		links = fmt.Sprintf(`"_links":{"base":"https://wiki.example.net/wiki","context":"/wiki","next":%q}`, nextURL)
	}
	return fmt.Sprintf(`{"totalSize":99,"start":0,"limit":250,"results":[%s],%s}`,
		strings.Join(rows, ","), links)
}

// TestSearchCQLKeepsGoingAfterAShortPage is the trap that matters most: unlike
// every other v1 collection, /search returns pages shorter than the limit in the
// middle of a walk. Terminating on a short page (listV1's rule) silently drops
// everything after the first one.
func TestSearchCQLKeepsGoingAfterAShortPage(t *testing.T) {
	c, s := newSearchServer(t,
		page("/rest/api/search?next=true&cursor=a&limit=250", row("1"), row("2")),
		page("/rest/api/search?next=true&cursor=b&limit=250", row("3")), // short, but not the end
		page("", row("4"), row("5")),
	)
	got, err := c.SearchCQL(`type = folder and title = "T"`)
	if err != nil {
		t.Fatalf("SearchCQL: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d results, want 5 (a short mid-walk page must not end the walk)", len(got))
	}
	if len(s.urls) != 3 {
		t.Fatalf("made %d requests, want 3", len(s.urls))
	}
}

// TestSearchCQLNextGetsTheWikiPrefix pins the other half of the paging rule: the
// next link is relative to the /wiki context, not an absolute path like a v2
// one, so resolveNext alone would request /rest/api/search and 404.
func TestSearchCQLNextGetsTheWikiPrefix(t *testing.T) {
	c, s := newSearchServer(t,
		page("/rest/api/search?next=true&cursor=abc&limit=250", row("1")),
		page("", row("2")),
	)
	if _, err := c.SearchCQL("type = folder"); err != nil {
		t.Fatalf("SearchCQL: %v", err)
	}
	if want := "/wiki/rest/api/search"; !strings.HasPrefix(s.urls[1], want+"?") {
		t.Errorf("second request = %q, want it to start with %q", s.urls[1], want)
	}
	if !strings.Contains(s.urls[1], "cursor=abc") {
		t.Errorf("second request = %q, want it to carry the cursor", s.urls[1])
	}
}

// TestSearchCQLFirstRequestCarriesCQLAndNoStart guards against reintroducing
// offset paging: the endpoint accepts start and ignores it, so a start
// parameter is at best noise and at worst a sign someone has rebuilt the
// truncating loop.
func TestSearchCQLFirstRequestCarriesCQLAndNoStart(t *testing.T) {
	c, s := newSearchServer(t, page(""))
	if _, err := c.SearchCQL(`title = "x"`); err != nil {
		t.Fatalf("SearchCQL: %v", err)
	}
	u := s.urls[0]
	if !strings.Contains(u, "cql=") || !strings.Contains(u, "limit=250") {
		t.Errorf("first request = %q, want cql and limit", u)
	}
	if strings.Contains(u, "start=") {
		t.Errorf("first request = %q, want no start parameter", u)
	}
}

// TestSearchCQLIgnoresTotalSize: totalSize has been observed nonzero against an
// empty results array, so an empty page with no next is simply the end.
func TestSearchCQLIgnoresTotalSize(t *testing.T) {
	c, s := newSearchServer(t, `{"totalSize":7,"results":[],"_links":{"base":"b"}}`)
	got, err := c.SearchCQL(`title = "gone"`)
	if err != nil {
		t.Fatalf("SearchCQL: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d results, want 0", len(got))
	}
	if len(s.urls) != 1 {
		t.Errorf("made %d requests, want 1 -- totalSize must not drive the loop", len(s.urls))
	}
}

// TestSearchCQLBoundsARunawayCursor: a next link that never clears must fail
// loudly rather than hang or return a plausible-looking prefix of the results.
func TestSearchCQLBoundsARunawayCursor(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(page("/rest/api/search?next=true&cursor=loop", row("1"))))
	})

	got, err := c.SearchCQL("type = folder")
	if err == nil {
		t.Fatalf("got %d results and no error, want an error", len(got))
	}
	if got != nil {
		t.Errorf("got %d results alongside the error, want none", len(got))
	}
	if !strings.Contains(err.Error(), "did not terminate") {
		t.Errorf("error = %v, want it to name the runaway walk", err)
	}
}

func TestSearchCQLParsesAHit(t *testing.T) {
	c, _ := newSearchServer(t, page("", row("2971140132")))
	got, err := c.SearchCQL("type = folder")
	if err != nil {
		t.Fatalf("SearchCQL: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	h := got[0]
	if h.Content.ID != "2971140132" || h.Content.Type != "folder" || h.Content.Status != "current" {
		t.Errorf("content = %+v, want the folder's id/type/status", h.Content)
	}
	// The row's url is context-relative, and is what a space key comes from.
	if want := "/spaces/ENG/folder/2971140132"; h.URL != want {
		t.Errorf("url = %q, want %q", h.URL, want)
	}
	if got := SpaceKeyFromWebUI(h.URL); got != "ENG" {
		t.Errorf("space from url = %q, want ENG", got)
	}
}

// TestSearchCQLRequestsTheExcerptMode: excerpt= is passed explicitly so what
// ships is what was tested, and an unrecognized value returns an empty excerpt
// rather than an error -- which is exactly why it must not be left to a default
// that could change underneath us.
func TestSearchCQLRequestsTheExcerptMode(t *testing.T) {
	c, s := newSearchServer(t, page(""))
	if _, err := c.SearchCQL(`title = "x"`); err != nil {
		t.Fatalf("SearchCQL: %v", err)
	}
	if want := "excerpt=" + excerptMode; !strings.Contains(s.urls[0], want) {
		t.Errorf("first request = %q, want it to carry %q", s.urls[0], want)
	}
}

// TestSearchCQLCarriesTheExcerptModeOntoLaterPages: the server's next link
// carries cql, limit and the cursor but not excerpt, so without re-attaching it
// page two would be requested differently from page one.
func TestSearchCQLCarriesTheExcerptModeOntoLaterPages(t *testing.T) {
	c, s := newSearchServer(t,
		page("/rest/api/search?next=true&cursor=abc&limit=250", row("1")),
		page("", row("2")),
	)
	if _, err := c.SearchCQL("type = folder"); err != nil {
		t.Fatalf("SearchCQL: %v", err)
	}
	if want := "excerpt=" + excerptMode; !strings.Contains(s.urls[1], want) {
		t.Errorf("second request = %q, want it to carry %q", s.urls[1], want)
	}
	// The cursor must survive having excerpt merged in, and there must still be
	// exactly one query string.
	if !strings.Contains(s.urls[1], "cursor=abc") {
		t.Errorf("second request = %q, want it to keep the cursor", s.urls[1])
	}
	if n := strings.Count(s.urls[1], "?"); n != 1 {
		t.Errorf("second request = %q, want exactly one %q", s.urls[1], "?")
	}
}

// TestSearchCQLBoundedStopsAtTheBound checks the truncation contract: at most max
// rows, and "more" reported from the surplus row rather than from totalSize.
func TestSearchCQLBoundedStopsAtTheBound(t *testing.T) {
	c, s := newSearchServer(t,
		page("/rest/api/search?next=true&cursor=a&limit=3", row("1"), row("2")),
		page("/rest/api/search?next=true&cursor=b&limit=3", row("3"), row("4")),
	)
	got, more, err := c.searchCQLBounded("type = page", 3)
	if err != nil {
		t.Fatalf("searchCQLBounded: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}
	if !more {
		t.Error("more = false, want true -- a surplus row was returned")
	}
	// It must stop as soon as the bound is exceeded rather than walking to the
	// end of the cursor.
	if len(s.urls) != 2 {
		t.Errorf("made %d requests, want 2", len(s.urls))
	}
}

// TestSearchCQLBoundedExactlyAtTheBound: max rows with the cursor exhausted is
// not truncation. Reporting "more exist" here would send a caller off to re-run
// with --limit all for nothing.
func TestSearchCQLBoundedExactlyAtTheBound(t *testing.T) {
	c, _ := newSearchServer(t, page("", row("1"), row("2"), row("3")))
	got, more, err := c.searchCQLBounded("type = page", 3)
	if err != nil {
		t.Fatalf("searchCQLBounded: %v", err)
	}
	if len(got) != 3 || more {
		t.Errorf("got %d rows, more = %v; want 3 and false", len(got), more)
	}
}

// TestSearchCQLBoundedIgnoresTotalSize: the canned pages report totalSize 99, so
// a bound-checking loop that consulted it would claim more rows exist after the
// cursor has run out.
func TestSearchCQLBoundedIgnoresTotalSize(t *testing.T) {
	c, _ := newSearchServer(t, page("", row("1")))
	got, more, err := c.searchCQLBounded("type = page", 10)
	if err != nil {
		t.Fatalf("searchCQLBounded: %v", err)
	}
	if len(got) != 1 || more {
		t.Errorf("got %d rows, more = %v; want 1 and false (totalSize is 99 and must not be read)",
			len(got), more)
	}
}

// TestSearchCQLBoundedShortPageStillWalks: the short-page trap again, but under a
// bound, where the temptation to treat a short page as the end is stronger.
func TestSearchCQLBoundedShortPageStillWalks(t *testing.T) {
	c, _ := newSearchServer(t,
		page("/rest/api/search?next=true&cursor=a&limit=101", row("1")),
		page("", row("2"), row("3")),
	)
	got, more, err := c.searchCQLBounded("type = page", 100)
	if err != nil {
		t.Fatalf("searchCQLBounded: %v", err)
	}
	if len(got) != 3 || more {
		t.Errorf("got %d rows, more = %v; want 3 and false", len(got), more)
	}
}

func TestSearchRequestSize(t *testing.T) {
	tests := []struct {
		name string
		max  int
		want int
	}{
		// One more than asked for, so the surplus answers "are there more?".
		{"small bound", 5, 6},
		{"one below the page size", searchPageSize - 1, searchPageSize},
		// Never more than a page: the bound cannot enlarge a request.
		{"at the page size", searchPageSize, searchPageSize},
		{"above the page size", searchPageSize + 100, searchPageSize},
		// Unbounded, and defensively the same for a negative.
		{"unbounded", 0, searchPageSize},
		{"negative", -1, searchPageSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := searchRequestSize(tt.max); got != tt.want {
				t.Errorf("searchRequestSize(%d) = %d, want %d", tt.max, got, tt.want)
			}
		})
	}
}

// TestSearchCQLBoundedAsksForOneExtraRow pins the request size end-to-end, since
// asking for exactly max would make "more exist" unanswerable without a second
// request.
func TestSearchCQLBoundedAsksForOneExtraRow(t *testing.T) {
	c, s := newSearchServer(t, page("", row("1")))
	if _, _, err := c.searchCQLBounded("type = page", 25); err != nil {
		t.Fatalf("searchCQLBounded: %v", err)
	}
	if !strings.Contains(s.urls[0], "limit=26") {
		t.Errorf("first request = %q, want limit=26", s.urls[0])
	}
}

func TestEscapeCQL(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", `Deploy runbook`, `Deploy runbook`},
		{"quote", `a"b`, `a\"b`},
		{"backslash", `a\b`, `a\\b`},
		// Backslash must be escaped first, or this collapses back to a bare
		// quote and closes the literal.
		{"escaped quote", `a\"b`, `a\\\"b`},
		{"injection", `a" or type=page`, `a\" or type=page`},
		{"unicode", `Zoë Café — 50% (draft)`, `Zoë Café — 50% (draft)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeCQL(tt.in); got != tt.want {
				t.Errorf("escapeCQL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// pageRow builds a page search hit, with the row-level title and excerpt in the
// decorated, entity-escaped form the server really sends.
func pageRow(id, contentTitle, rowTitle, excerpt string) string {
	return fmt.Sprintf(
		`{"entityType":"content","title":%q,"url":"/spaces/ENG/pages/%s/slug","excerpt":%q,`+
			`"content":{"id":%q,"type":"page","status":"current","title":%q,`+
			`"_links":{"webui":"/spaces/ENG/pages/%s/slug"}}}`,
		rowTitle, id, excerpt, id, contentTitle, id)
}

// spaceRow builds the content-less row a `type = space` query returns: no content
// object at all, the addressable data in a sibling space object.
func spaceRow(key string) string {
	return fmt.Sprintf(
		`{"entityType":"space","title":%q,"url":"/spaces/%s",`+
			`"space":{"key":%q,"name":%q,"status":"current"}}`, key, key, key, key)
}

// cqlOf pulls the cql parameter back out of a recorded request URL.
func cqlOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parsing %q: %v", rawURL, err)
	}
	return u.Query().Get("cql")
}

func TestCleanExcerpt(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "Deploying to prod", "Deploying to prod"},
		// siteSearch ~ wraps every matched term; 40 of 50 sampled rows had these.
		{"markers", "a @@@hl@@@deploy@@@endhl@@@ runbook", "a deploy runbook"},
		{"entity", "Base Load Engineer&#39;s Log", "Base Load Engineer's Log"},
		{"ampersand", "Apps &amp; Actions", "Apps & Actions"},
		// The index breaks excerpts at the source's own line boundaries.
		{"newlines", "one\ntwo\nthree", "one two three"},
		{"whitespace runs", "one  \t\n  two", "one two"},
		{"trims", "  padded  ", "padded"},
		{"all at once", "@@@hl@@@Deploy@@@endhl@@@&#39;s\n  guide", "Deploy's guide"},
		{"empty", "", ""},
		{"whitespace only", " \n\t ", ""},
		// A single unescape pass, so text that was literally "&#39;" in the page
		// survives as itself rather than decoding twice.
		{"double escaped", "a &amp;#39; b", "a &#39; b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanExcerpt(tt.in); got != tt.want {
				t.Errorf("cleanExcerpt(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCleanExcerptSpans covers the marked runs alongside the text. The cases are
// the shapes actually observed in docs/confluence/search.md -- markers, entities,
// embedded newlines -- rather than invented ones.
func TestCleanExcerptSpans(t *testing.T) {
	tests := []struct {
		name, in, wantText string
		wantSpans          []ExcerptSpan
	}{
		{
			"no markers is one unmatched span", "Deploying to prod", "Deploying to prod",
			[]ExcerptSpan{{Text: "Deploying to prod"}},
		},
		{
			"a marked term", "a @@@hl@@@deploy@@@endhl@@@ runbook", "a deploy runbook",
			[]ExcerptSpan{{Text: "a "}, {Text: "deploy", Match: true}, {Text: " runbook"}},
		},
		{
			"marked at the start", "@@@hl@@@Deploy@@@endhl@@@ now", "Deploy now",
			[]ExcerptSpan{{Text: "Deploy", Match: true}, {Text: " now"}},
		},
		{
			"marked at the end", "now @@@hl@@@deploy@@@endhl@@@", "now deploy",
			[]ExcerptSpan{{Text: "now "}, {Text: "deploy", Match: true}},
		},
		{
			"two marked terms", "@@@hl@@@Runbook@@@endhl@@@: Grafana @@@hl@@@deploys@@@endhl@@@",
			"Runbook: Grafana deploys",
			[]ExcerptSpan{
				{Text: "Runbook", Match: true}, {Text: ": Grafana "}, {Text: "deploys", Match: true},
			},
		},
		{
			// Adjacent runs coalesce by flag, so this is one span, not two.
			"adjacent marked terms", "@@@hl@@@de@@@endhl@@@@@@hl@@@ploy@@@endhl@@@", "deploy",
			[]ExcerptSpan{{Text: "deploy", Match: true}},
		},
		{
			// The space is inside the marked run, so the highlight stays one block.
			"a space inside a marked run", "@@@hl@@@foo bar@@@endhl@@@ baz", "foo bar baz",
			[]ExcerptSpan{{Text: "foo bar", Match: true}, {Text: " baz"}},
		},
		{
			"entity inside a marked run", "@@@hl@@@Engineer&#39;s@@@endhl@@@ log", "Engineer's log",
			[]ExcerptSpan{{Text: "Engineer's", Match: true}, {Text: " log"}},
		},
		{
			"newline collapses between a marked and an unmatched word",
			"@@@hl@@@deploys@@@endhl@@@\nStage", "deploys Stage",
			[]ExcerptSpan{{Text: "deploys", Match: true}, {Text: " Stage"}},
		},
		{
			"markers, entity and newline at once", "@@@hl@@@Deploy@@@endhl@@@&#39;s\n  guide",
			"Deploy's guide",
			[]ExcerptSpan{{Text: "Deploy", Match: true}, {Text: "'s guide"}},
		},
		{"empty", "", "", nil},
		{"whitespace only", " \n\t ", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, spans := cleanExcerptSpans(tt.in)
			if text != tt.wantText {
				t.Errorf("text = %q, want %q", text, tt.wantText)
			}
			if !reflect.DeepEqual(spans, tt.wantSpans) {
				t.Errorf("spans = %#v, want %#v", spans, tt.wantSpans)
			}
		})
	}
}

// TestCleanExcerptSpansReassemble is the invariant that lets Excerpt and Spans
// coexist: concatenating the spans must reproduce the excerpt exactly. If this
// ever fails, the human output and --json are describing different text.
func TestCleanExcerptSpansReassemble(t *testing.T) {
	inputs := []string{
		"Deploying to prod",
		"a @@@hl@@@deploy@@@endhl@@@ runbook",
		"@@@hl@@@Runbook@@@endhl@@@: Grafana @@@hl@@@deploys@@@endhl@@@",
		"@@@hl@@@Deploy@@@endhl@@@&#39;s\n  guide",
		"Base Load Engineer&#39;s Hand-off Log",
		"one\ntwo\nthree",
		"  padded  ",
		"",
		" \n\t ",
		"@@@hl@@@foo bar@@@endhl@@@ baz",
		"a &amp;#39; b",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			text, spans := cleanExcerptSpans(in)
			var b strings.Builder
			for _, sp := range spans {
				b.WriteString(sp.Text)
			}
			if b.String() != text {
				t.Errorf("spans reassemble to %q, want %q", b.String(), text)
			}
		})
	}
}

func TestBuildTextCQL(t *testing.T) {
	tests := []struct {
		name, query, space, ctype, want string
	}{
		{
			"typed", "deploy runbook", "", SearchTypePage,
			`siteSearch ~ "deploy runbook" and text ~ "deploy runbook" and type = page`,
		},
		{
			"typed and scoped", "deploy", "ENG", SearchTypePage,
			`siteSearch ~ "deploy" and text ~ "deploy" and type = page and space = "ENG"`,
		},
		{
			"blogpost", "deploy", "", SearchTypeBlogpost,
			`siteSearch ~ "deploy" and text ~ "deploy" and type = blogpost`,
		},
		// SearchTypeAll drops the clause rather than emitting `type = all`.
		{"all", "deploy", "", SearchTypeAll, `siteSearch ~ "deploy" and text ~ "deploy"`},
		{
			"all and scoped", "deploy", "ENG", SearchTypeAll,
			`siteSearch ~ "deploy" and text ~ "deploy" and space = "ENG"`,
		},
		{"empty type", "deploy", "", "", `siteSearch ~ "deploy" and text ~ "deploy"`},
		// Every interpolated value is escaped: a bare quote in any of them would
		// end the literal and turn the rest into query syntax.
		{
			"escapes the query", `a" or type=page`, "", SearchTypePage,
			`siteSearch ~ "a\" or type=page" and text ~ "a\" or type=page" and type = page`,
		},
		{
			"escapes the space key", "deploy", `a"b`, SearchTypePage,
			`siteSearch ~ "deploy" and text ~ "deploy" and type = page and space = "a\"b"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTextCQL(tt.query, tt.space, tt.ctype)
			if got != tt.want {
				t.Errorf("buildTextCQL(%q, %q, %q) =\n  %q\nwant\n  %q",
					tt.query, tt.space, tt.ctype, got, tt.want)
			}
		})
	}
}

// TestBuildTextCQLLeadsWithSiteSearch is a regression guard, and the thing to
// read before reordering anything in buildTextCQL.
//
// Confluence silently drops a siteSearch clause that sits in the *middle* of
// three: `type = page and siteSearch ~ "netlify" and space = "SRE"` returned 1122
// rows, byte-identical to `type = page and space = "SRE"` -- every page in the
// space, presented as a search result. The same clauses with siteSearch leading
// returned the honest 15.
//
// So the clause must never be preceded by another. This asserts the property
// rather than a literal string, so it keeps holding as clauses are added.
func TestBuildTextCQLLeadsWithSiteSearch(t *testing.T) {
	for _, tt := range []struct {
		name, query, space, ctype string
	}{
		{"query only", "netlify", "", SearchTypeAll},
		{"with type", "netlify", "", SearchTypePage},
		{"with space", "netlify", "SRE", SearchTypeAll},
		// The combination that was broken: three clauses.
		{"with type and space", "netlify", "SRE", SearchTypePage},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTextCQL(tt.query, tt.space, tt.ctype)
			if !strings.HasPrefix(got, "siteSearch ~ ") {
				t.Errorf("cql = %q\nwant it to *begin* with the siteSearch clause: "+
					"Confluence drops that clause when another precedes it and a third follows, "+
					"turning a scoped search into a listing of the whole space", got)
			}
		})
	}
}

// TestBuildTextCQLKeepsTheTextFallback: siteSearch supplies the ranking and text
// supplies a floor. If the siteSearch clause is ever dropped -- by a reordering
// here or a change on Atlassian's side -- text keeps the results constrained to
// content that contains the query, so the failure degrades to a worse order
// rather than to every page in the space.
func TestBuildTextCQLKeepsTheTextFallback(t *testing.T) {
	got := buildTextCQL("netlify", "SRE", SearchTypePage)
	if !strings.Contains(got, `text ~ "netlify"`) {
		t.Errorf("cql = %q, want a redundant text clause as the safety net", got)
	}
}

// TestSearchTextUsesSiteSearchNotText is the ranking decision, pinned. text ~
// ranked six unrelated pages above every obviously-relevant one, and with score
// at 0.0 there is no client-side re-ranking to recover from that.
func TestSearchTextUsesSiteSearchNotText(t *testing.T) {
	c, s := newSearchServer(t, page("", pageRow("1", "T", "T", "")))
	if _, err := c.SearchText("deploy", "", SearchTypePage, 25); err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	cql := cqlOf(t, s.urls[0])
	if !strings.HasPrefix(cql, "siteSearch ~") {
		t.Errorf("cql = %q, want siteSearch ~ leading -- it is what ranks, and Confluence "+
			"drops the clause if anything precedes it with a third clause following", cql)
	}
	// text ~ is present deliberately, as the safety net described in
	// buildTextCQL -- but it must never be what leads, since it ranks poorly.
	if strings.HasPrefix(cql, "text ~") {
		t.Errorf("cql = %q, want text ~ as the fallback rather than the lead", cql)
	}
}

// TestSearchTextReadsContentTitleNotTheRowTitle: the row's title is
// entity-escaped and marker-decorated, so reading it would surface
// "Deployment @@@hl@@@runbook@@@endhl@@@" as a page title.
func TestSearchTextReadsContentTitleNotTheRowTitle(t *testing.T) {
	c, _ := newSearchServer(t, page("", pageRow(
		"123", "Deployment runbook", "Deployment @@@hl@@@runbook@@@endhl@@@", "an excerpt")))
	got, err := c.SearchText("runbook", "", SearchTypePage, 25)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(got.Matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(got.Matches))
	}
	m := got.Matches[0]
	if m.Title != "Deployment runbook" {
		t.Errorf("title = %q, want the clean content.title", m.Title)
	}
	if m.ID != "123" || m.Type != "page" || m.Space != "ENG" {
		t.Errorf("match = %+v, want id/type/space from the content object", m)
	}
	if want := "/wiki/spaces/ENG/pages/123/slug"; !strings.HasSuffix(m.URL, want) {
		t.Errorf("url = %q, want it to end with %q", m.URL, want)
	}
}

// TestSearchTextCountsContentLessRows: a `type = space` query returns hundreds of
// rows with no content object. Dropping them quietly would report a successful
// empty result for a query that matched plenty.
func TestSearchTextCountsContentLessRows(t *testing.T) {
	c, _ := newSearchServer(t, page("", spaceRow("EW"), spaceRow("ENG")))
	got, err := c.SearchText("deploy", "", SearchTypeAll, 25)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(got.Matches) != 0 {
		t.Errorf("got %d matches, want 0", len(got.Matches))
	}
	if got.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", got.Skipped)
	}
}

// TestSearchTextMixesContentAndContentLessRows: the content rows still come back,
// and the skipped count is not silently folded into them.
func TestSearchTextMixesContentAndContentLessRows(t *testing.T) {
	c, _ := newSearchServer(t, page("",
		pageRow("1", "One", "One", ""), spaceRow("EW"), pageRow("2", "Two", "Two", "")))
	got, err := c.SearchText("deploy", "", SearchTypeAll, 25)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(got.Matches) != 2 || got.Skipped != 1 {
		t.Errorf("got %d matches and %d skipped, want 2 and 1", len(got.Matches), got.Skipped)
	}
}

// TestSearchTextPreservesServerOrder: the server's relevance order is the only
// order there is, so nothing may sort these -- not even into a stable one.
func TestSearchTextPreservesServerOrder(t *testing.T) {
	c, _ := newSearchServer(t, page("",
		pageRow("300", "Zebra", "Zebra", ""),
		pageRow("100", "Apple", "Apple", ""),
		pageRow("200", "Mango", "Mango", "")))
	got, err := c.SearchText("x", "", SearchTypePage, 25)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	var ids []string
	for _, m := range got.Matches {
		ids = append(ids, m.ID)
	}
	want := []string{"300", "100", "200"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (the server's order, neither id- nor title-sorted)", ids, want)
	}
}

// TestSearchTextRefusesAnUnknownSpace: CQL answers an unknown space key with zero
// rows, which reads exactly like "no matches" -- and the next move on that is to
// create a page that already exists.
func TestSearchTextRefusesAnUnknownSpace(t *testing.T) {
	c, s := newSearchServer(t, `{"results":[]}`)
	_, err := c.SearchText("deploy", "NOPE", SearchTypePage, 25)
	if !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("err = %v, want ErrSpaceNotFound", err)
	}
	// It must refuse before searching, not after.
	if len(s.urls) != 1 {
		t.Errorf("made %d requests, want 1 (the space lookup only)", len(s.urls))
	}
}

// TestSearchRawCQLPassesTheQueryThrough: no escaping, no type clause, no space
// clause. Adding any of them would regroup a query containing `or` and silently
// answer a different question.
func TestSearchRawCQLPassesTheQueryThrough(t *testing.T) {
	c, s := newSearchServer(t, page("", pageRow("1", "T", "T", "")))
	raw := `type = page and (title ~ "a" or title ~ "b")`
	if _, err := c.SearchRawCQL(raw, 25); err != nil {
		t.Fatalf("SearchRawCQL: %v", err)
	}
	if got := cqlOf(t, s.urls[0]); got != raw {
		t.Errorf("cql = %q, want it verbatim: %q", got, raw)
	}
}

// TestSearchTextReportsMore ties the bound to what a caller reports: the surplus
// row is dropped and More says so, without any count being claimed.
func TestSearchTextReportsMore(t *testing.T) {
	c, _ := newSearchServer(t, page("",
		pageRow("1", "A", "A", ""), pageRow("2", "B", "B", ""), pageRow("3", "C", "C", "")))
	got, err := c.SearchText("x", "", SearchTypePage, 2)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(got.Matches) != 2 || !got.More {
		t.Errorf("got %d matches, more = %v; want 2 and true", len(got.Matches), got.More)
	}
}

// TestSearchTextCleansTheExcerpt: the cleaning has to be on the path a command
// actually uses, not only in the helper's own unit test.
func TestSearchTextCleansTheExcerpt(t *testing.T) {
	// A real newline, since the index breaks excerpts at the source's own lines.
	c, _ := newSearchServer(t, page("", pageRow("1", "T", "T",
		"@@@hl@@@Deploy@@@endhl@@@ing to prod.\nSee Engineer&#39;s guide.")))
	got, err := c.SearchText("deploy", "", SearchTypePage, 25)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	want := "Deploying to prod. See Engineer's guide."
	if got.Matches[0].Excerpt != want {
		t.Errorf("excerpt = %q, want %q", got.Matches[0].Excerpt, want)
	}
}

// TestSearchResultsCarriesTheQuery: the command logs this under --debug, and the
// one bug this file produced in anger was a clause silently dropped from an
// assembled query -- which is invisible unless the query itself is reportable.
func TestSearchResultsCarriesTheQuery(t *testing.T) {
	c, _ := newSearchServer(t, page("", pageRow("1", "T", "T", "")))
	got, err := c.SearchText("netlify", "", SearchTypePage, 10)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if want := buildTextCQL("netlify", "", SearchTypePage); got.CQL != want {
		t.Errorf("CQL = %q, want %q", got.CQL, want)
	}
}

// TestSearchResultsCarriesTheQueryOnFailure: it is wanted most when the search
// did not work.
func TestSearchResultsCarriesTheQueryOnFailure(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	got, err := c.SearchRawCQL(`type = space`, 10)
	if err == nil {
		t.Fatal("got no error, want one")
	}
	if got.CQL != `type = space` {
		t.Errorf("CQL = %q, want it carried on the failure path", got.CQL)
	}
}
