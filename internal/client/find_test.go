package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// findServer answers the three requests FindByTitle can make -- the space
// lookup, the v2 pages query, and the CQL search -- by path, and records the
// URLs so a test can assert what was actually asked for.
type findServer struct {
	spaces, pages, search string
	spacesStatus          int
	pagesStatus           int
	searchStatus          int
	urls                  []string
}

func newFindServer(t *testing.T, f *findServer) *ConfluenceClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.urls = append(f.urls, r.URL.String())
		body, status := "", 200
		switch {
		case strings.HasPrefix(r.URL.Path, "/wiki/api/v2/spaces"):
			body, status = f.spaces, f.spacesStatus
		case strings.HasPrefix(r.URL.Path, "/wiki/api/v2/pages"):
			body, status = f.pages, f.pagesStatus
		case strings.HasPrefix(r.URL.Path, "/wiki/rest/api/search"):
			body, status = f.search, f.searchStatus
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			status = 500
		}
		if status == 0 {
			status = 200
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(Config{SiteURL: srv.URL, Username: "u", Token: "t"})
}

func v2Page(id, title, status, space string) string {
	webui := "/spaces/" + space + "/pages/" + id + "/" + strings.ReplaceAll(title, " ", "+")
	return `{"id":"` + id + `","title":"` + title + `","status":"` + status +
		`","_links":{"webui":"` + webui + `"}}`
}

func cqlFolder(id, title, space string) string {
	return `{"entityType":"content","url":"/spaces/` + space + `/folder/` + id + `",` +
		`"content":{"id":"` + id + `","type":"folder","status":"current","title":"` + title + `"}}`
}

// TestFindByTitleMergesBothBackends is the point of the whole two-request
// design: the v2 half cannot see the folder and the CQL half cannot see the
// archived page, so either alone returns an incomplete answer.
func TestFindByTitleMergesBothBackends(t *testing.T) {
	c := newFindServer(t, &findServer{
		pages: `{"results":[` +
			v2Page("500", "Runbook", "current", "ENG") + `,` +
			v2Page("400", "Runbook", "archived", "ENG") + `],"_links":{}}`,
		search: `{"totalSize":1,"results":[` + cqlFolder("300", "Runbook", "OPS") + `],"_links":{}}`,
	})

	got, err := c.FindByTitle("Runbook", "")
	if err != nil {
		t.Fatalf("FindByTitle: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d matches, want 3: %+v", len(got), got)
	}

	// Sorted by space, then type, then numeric id: ENG's archived 400 before
	// ENG's current 500 (400 < 500 numerically), then OPS's folder.
	want := []struct{ id, typ, status, space string }{
		{"400", "page", "archived", "ENG"},
		{"500", "page", "current", "ENG"},
		{"300", "folder", "current", "OPS"},
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.id || g.Type != w.typ || g.Status != w.status || g.Space != w.space {
			t.Errorf("match %d = %+v, want id=%s type=%s status=%s space=%s",
				i, g, w.id, w.typ, w.status, w.space)
		}
	}
	if want := "/wiki/spaces/OPS/folder/300"; !strings.HasSuffix(got[2].URL, want) {
		t.Errorf("folder URL = %q, want it to end with %q", got[2].URL, want)
	}
}

// TestFindByTitleAsksForArchivedPages: the archived half is the reason create's
// duplicate check was wrong, so the request has to actually ask for it.
func TestFindByTitleAsksForArchivedPages(t *testing.T) {
	f := &findServer{pages: `{"results":[],"_links":{}}`, search: `{"results":[],"_links":{}}`}
	c := newFindServer(t, f)
	if _, err := c.FindByTitle("X", ""); err != nil {
		t.Fatalf("FindByTitle: %v", err)
	}
	var pagesURL string
	for _, u := range f.urls {
		if strings.HasPrefix(u, "/wiki/api/v2/pages") {
			pagesURL = u
		}
	}
	if !strings.Contains(pagesURL, "status=current") || !strings.Contains(pagesURL, "status=archived") {
		t.Errorf("pages request = %q, want both statuses", pagesURL)
	}
}

// TestFindByTitleEscapesTheCQLQuery: an unescaped quote would end the string
// literal and hand the rest of the title to the query parser.
func TestFindByTitleEscapesTheCQLQuery(t *testing.T) {
	f := &findServer{pages: `{"results":[],"_links":{}}`, search: `{"results":[],"_links":{}}`}
	c := newFindServer(t, f)
	if _, err := c.FindByTitle(`a" or type=page`, ""); err != nil {
		t.Fatalf("FindByTitle: %v", err)
	}
	var raw string
	for _, u := range f.urls {
		if strings.HasPrefix(u, "/wiki/rest/api/search") {
			raw = u
		}
	}
	// The query is URL-encoded on the wire; %5C is the backslash that keeps the
	// quote inside the literal.
	if !strings.Contains(raw, "%5C%22") {
		t.Errorf("search request = %q, want the quote backslash-escaped", raw)
	}
	if !strings.Contains(raw, "type+%3D+folder") && !strings.Contains(raw, "type%20%3D%20folder") {
		t.Errorf("search request = %q, want the folder type pinned", raw)
	}
}

func TestFindByTitleScopesBothHalvesToTheSpace(t *testing.T) {
	f := &findServer{
		spaces: `{"results":[{"id":"9001"}]}`,
		pages:  `{"results":[],"_links":{}}`,
		search: `{"results":[],"_links":{}}`,
	}
	c := newFindServer(t, f)
	if _, err := c.FindByTitle("X", "ENG"); err != nil {
		t.Fatalf("FindByTitle: %v", err)
	}
	joined := strings.Join(f.urls, "\n")
	if !strings.Contains(joined, "space-id=9001") {
		t.Errorf("no space-id on the pages request:\n%s", joined)
	}
	if !strings.Contains(joined, "space+%3D+%22ENG%22") && !strings.Contains(joined, "space%20%3D%20%22ENG%22") {
		t.Errorf("no space clause on the CQL request:\n%s", joined)
	}
}

// TestFindByTitleRefusesAnUnknownSpace: CQL answers an unknown key with zero
// results, which is indistinguishable from "no such page", so the key is
// checked up front.
func TestFindByTitleRefusesAnUnknownSpace(t *testing.T) {
	f := &findServer{spaces: `{"results":[]}`}
	c := newFindServer(t, f)
	_, err := c.FindByTitle("X", "NOPE")
	if !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("err = %v, want ErrSpaceNotFound", err)
	}
	if !strings.Contains(err.Error(), `"NOPE"`) {
		t.Errorf("err = %v, want it to name the key", err)
	}
	for _, u := range f.urls {
		if strings.HasPrefix(u, "/wiki/rest/api/search") || strings.HasPrefix(u, "/wiki/api/v2/pages") {
			t.Errorf("searched anyway with an unknown space: %s", u)
		}
	}
}

// TestFindByTitleFailsWhenEitherHalfFails: a partial answer here reads as
// "nothing found", and the caller acts on that by creating a duplicate.
func TestFindByTitleFailsWhenEitherHalfFails(t *testing.T) {
	t.Run("folder half fails", func(t *testing.T) {
		c := newFindServer(t, &findServer{
			pages:        `{"results":[` + v2Page("1", "X", "current", "ENG") + `],"_links":{}}`,
			search:       `{"message":"boom"}`,
			searchStatus: 500,
		})
		got, err := c.FindByTitle("X", "")
		if err == nil {
			t.Fatalf("got %d matches and no error, want an error", len(got))
		}
		if got != nil {
			t.Errorf("got %d matches alongside the error, want none", len(got))
		}
	})
	t.Run("page half fails", func(t *testing.T) {
		c := newFindServer(t, &findServer{pages: `{"message":"boom"}`, pagesStatus: 500})
		if _, err := c.FindByTitle("X", ""); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestFindByTitleEmptyIsNotAnError(t *testing.T) {
	c := newFindServer(t, &findServer{
		pages:  `{"results":[],"_links":{}}`,
		search: `{"totalSize":3,"results":[],"_links":{}}`,
	})
	got, err := c.FindByTitle("nothing", "")
	if err != nil {
		t.Fatalf("FindByTitle: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d matches, want 0 (totalSize must not invent results)", len(got))
	}
}

func TestSortMatchesPutsSpacelessRowsLast(t *testing.T) {
	m := []TitleMatch{
		{ID: "3", Type: "page", Space: ""},
		{ID: "2", Type: "page", Space: "ENG"},
		{ID: "10", Type: "page", Space: "ENG"},
	}
	sortMatches(m)
	got := []string{m[0].ID, m[1].ID, m[2].ID}
	want := []string{"2", "10", "3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (numeric ids, spaceless last)", got, want)
		}
	}
}
