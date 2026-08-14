package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.urls = append(s.urls, r.URL.String())
		if s.idx >= len(s.bodies) {
			t.Errorf("unexpected extra request: %s", r.URL)
			w.WriteHeader(500)
			return
		}
		body := s.bodies[s.idx]
		s.idx++
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(Config{SiteURL: srv.URL, Username: "u", Token: "t"}), s
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(page("/rest/api/search?next=true&cursor=loop", row("1"))))
	}))
	t.Cleanup(srv.Close)
	c := New(Config{SiteURL: srv.URL, Username: "u", Token: "t"})

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
