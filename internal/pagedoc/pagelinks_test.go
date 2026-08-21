package pagedoc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/convert"
)

// linkServer answers the two requests resolving an <ac:link> target takes -- the
// space-key lookup and the title search -- from a fixture of space key -> titles
// that exist there, and records every path it was asked for so the request count
// can be asserted as well as the result.
func linkServer(t *testing.T, spaces map[string][]string) (*client.ConfluenceClient, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		q := r.URL.Query()

		if strings.HasSuffix(r.URL.Path, "/spaces") {
			key := q.Get("keys")
			rows := []map[string]any{}
			if _, ok := spaces[key]; ok {
				rows = append(rows, map[string]any{"id": "space-" + key})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": rows})
			return
		}

		// /wiki/api/v2/pages?title=...&space-id=...
		spaceKey := strings.TrimPrefix(q.Get("space-id"), "space-")
		rows := []map[string]any{}
		for i, title := range spaces[spaceKey] {
			if title != q.Get("title") {
				continue
			}
			rows = append(rows, map[string]any{
				"id": fmt.Sprintf("%d", 100+i), "title": title, "status": "current",
				"_links": map[string]any{"webui": fmt.Sprintf("/spaces/%s/pages/%d/x", spaceKey, 100+i)},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": rows})
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL, Username: "u", Token: "t"}), &paths
}

// pageWithBody is a fetched page carrying a storage body and a webui link, which
// is where the page's own space key comes from.
func pageWithBody(spaceKey, body string) *client.Page {
	p := &client.Page{ID: "1", Title: "Host"}
	p.Body.Storage.Value = body
	p.Links.WebUI = "/spaces/" + spaceKey + "/pages/1/Host"
	return p
}

// TestPageLinksResolvesTitles covers the whole point of the lookup: a link
// naming no space is resolved in the page's own space, and one naming a space is
// resolved there instead.
func TestPageLinksResolvesTitles(t *testing.T) {
	c, _ := linkServer(t, map[string][]string{
		"SRE":     {"Runbook"},
		"FIREFOX": {"Felt Privacy Workstream"},
	})
	page := pageWithBody("SRE",
		`<p><ac:link><ri:page ri:content-title="Runbook" /><ac:link-body>a</ac:link-body></ac:link>`+
			`<ac:link><ri:page ri:space-key="FIREFOX" ri:content-title="Felt Privacy Workstream" />`+
			`<ac:link-body>b</ac:link-body></ac:link></p>`)

	got := PageLinks(c, page)
	want := map[convert.PageLinkTarget]string{
		{Title: "Runbook"}:                                      c.SiteURL() + "/wiki/spaces/SRE/pages/100/x",
		{SpaceKey: "FIREFOX", Title: "Felt Privacy Workstream"}: c.SiteURL() + "/wiki/spaces/FIREFOX/pages/100/x",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%v: got %q, want %q", k, got[k], v)
		}
	}
}

// TestPageLinksResolvesEachSpaceOnce is why the space id is cached: without it a
// page with several links into one space pays a space lookup per link.
func TestPageLinksResolvesEachSpaceOnce(t *testing.T) {
	c, paths := linkServer(t, map[string][]string{"SRE": {"A", "B", "C"}})
	page := pageWithBody("SRE",
		`<p><ac:link><ri:page ri:content-title="A" /></ac:link>`+
			`<ac:link><ri:page ri:content-title="B" /></ac:link>`+
			`<ac:link><ri:page ri:content-title="C" /></ac:link></p>`)

	if got := PageLinks(c, page); len(got) != 3 {
		t.Fatalf("got %d links, want 3: %v", len(got), got)
	}
	spaceLookups := 0
	for _, p := range *paths {
		if strings.Contains(p, "/spaces?") {
			spaceLookups++
		}
	}
	if spaceLookups != 1 {
		t.Errorf("resolved the space %d times, want 1:\n%s", spaceLookups, strings.Join(*paths, "\n"))
	}
}

// TestPageLinksDeduplicatesTargets: the same page linked twice is one lookup.
func TestPageLinksDeduplicatesTargets(t *testing.T) {
	c, paths := linkServer(t, map[string][]string{"SRE": {"A"}})
	page := pageWithBody("SRE",
		`<p><ac:link><ri:page ri:content-title="A" /></ac:link>`+
			`<ac:link><ri:page ri:content-title="A" /></ac:link></p>`)

	PageLinks(c, page)
	titleLookups := 0
	for _, p := range *paths {
		if strings.Contains(p, "title=A") {
			titleLookups++
		}
	}
	if titleLookups != 1 {
		t.Errorf("looked the title up %d times, want 1:\n%s", titleLookups, strings.Join(*paths, "\n"))
	}
}

// TestPageLinksOmitsWhatItCannotResolve is the contract with the converter: a
// missing entry means passthrough, so a title that resolves to nothing must be
// absent rather than mapped to an empty URL, which would render as a link with
// no destination.
func TestPageLinksOmitsWhatItCannotResolve(t *testing.T) {
	c, _ := linkServer(t, map[string][]string{"SRE": {"A"}})
	page := pageWithBody("SRE",
		`<p><ac:link><ri:page ri:content-title="Gone" /></ac:link>`+
			`<ac:link><ri:page ri:space-key="NOSUCH" ri:content-title="A" /></ac:link></p>`)

	if got := PageLinks(c, page); len(got) != 0 {
		t.Errorf("got %v, want nothing resolved", got)
	}
}

// TestPageLinksSkipsLookupWithoutLinks pins the cheap path, the same way
// TestSourcesSkipsLookupWithoutReferences does: the nil client would panic if a
// request were attempted.
func TestPageLinksSkipsLookupWithoutLinks(t *testing.T) {
	page := pageWithBody("SRE", `<p>a <ac:link><ri:user ri:account-id="x" /></ac:link> mention</p>`)
	if got := PageLinks(nil, page); got != nil {
		t.Errorf("PageLinks = %v, want nil with no page link", got)
	}
}
