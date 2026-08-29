package fix

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
)

// --- locatePage --------------------------------------------------------------

func pageJSON(id, title, parentID, webui string) string {
	return fmt.Sprintf(`{"id":%q,"title":%q,"parentId":%q,"_links":{"webui":%q}}`,
		id, title, parentID, webui)
}

func TestLocatePageByIDFound(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wiki/api/v2/pages/123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(pageJSON("123", "Runbook", "", "/spaces/ENG/pages/123/Runbook")))
	})
	page, err := locatePage(map[string]string{"page_id": "123"}, c)
	if err != nil {
		t.Fatalf("locatePage: %v", err)
	}
	if page.ID != "123" || page.Title != "Runbook" {
		t.Errorf("page = %+v, want id=123 title=Runbook", page)
	}
}

func TestLocatePageByIDNotFound(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":404,"title":"Cannot find a page with id 999"}]}`))
	})
	_, err := locatePage(map[string]string{"page_id": "999"}, c)
	if err == nil {
		t.Fatal("want an error for a page_id that resolves to nothing")
	}
	for _, want := range []string{"999", "not found", "remove it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestLocatePageNoIDOrTitle(t *testing.T) {
	// example.net never resolves to anything reachable, so a stray request would
	// fail loudly rather than pass; this path must return before any request.
	c := client.New(client.Config{SiteURL: "https://wiki.example.net"})
	_, err := locatePage(map[string]string{}, c)
	if err == nil || !strings.Contains(err.Error(), "no page_id or title") {
		t.Errorf("err = %v, want a no-page_id-or-title error", err)
	}
}

func TestLocatePageByTitleNoMatch(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	_, err := locatePage(map[string]string{"title": "Ghost"}, c)
	if err == nil || !strings.Contains(err.Error(), `no Confluence page found with title "Ghost"`) {
		t.Errorf("err = %v, want a no-match error naming the title", err)
	}
}

func TestLocatePageByTitleOneMatch(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/wiki/api/v2/pages/"):
			_, _ = w.Write([]byte(pageJSON("55", "Runbook", "", "/spaces/ENG/pages/55/Runbook")))
		default:
			_, _ = w.Write([]byte(`{"results":[` + pageJSON("55", "Runbook", "", "/spaces/ENG/pages/55/Runbook") + `]}`))
		}
	})
	page, err := locatePage(map[string]string{"title": "Runbook"}, c)
	if err != nil {
		t.Fatalf("locatePage: %v", err)
	}
	if page.ID != "55" {
		t.Errorf("page.ID = %q, want 55 (the single match, re-fetched)", page.ID)
	}
}

func TestLocatePageByTitleMultipleMatchesDisambiguates(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[` +
			pageJSON("1", "Runbook", "", "/spaces/ENG/pages/1/Runbook") + `,` +
			pageJSON("2", "Runbook", "", "/spaces/OPS/pages/2/Runbook") + `]}`))
	})
	_, err := locatePage(map[string]string{"title": "Runbook"}, c)
	if err == nil {
		t.Fatal("want an error when multiple pages share the title")
	}
	for _, want := range []string{"found 2 pages", "Runbook", "1", "2", "add a page_id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// --- plannedChanges ------------------------------------------------------------

func TestPlannedChangesNoneWhenConsistent(t *testing.T) {
	fm := map[string]string{
		"page_id": "123", "space": "ENG", "parent": "null", "title": "Runbook", "page_width": "max",
	}
	page := &client.Page{ID: "123", Title: "Runbook", Links: client.Links{WebUI: "/spaces/ENG/pages/123/Runbook"}}
	got := plannedChanges(fm, page, "max")
	if len(got) != 0 {
		t.Errorf("changes = %+v, want none", got)
	}
}

func TestPlannedChangesFillsMissingFields(t *testing.T) {
	page := &client.Page{ID: "123", Title: "Runbook", Links: client.Links{WebUI: "/spaces/ENG/pages/123/Runbook"}}
	got := plannedChanges(map[string]string{}, page, "")
	want := map[string]string{"page_id": "123", "space": "ENG", "parent": "null", "title": "Runbook"}
	if len(got) != len(want) {
		t.Fatalf("changes = %+v, want %d entries", got, len(want))
	}
	for _, ch := range got {
		if ch.oldDisplay != noneDisplay {
			t.Errorf("field %s: old = %q, want %q", ch.field, ch.oldDisplay, noneDisplay)
		}
		if want[ch.field] != ch.newValue {
			t.Errorf("field %s: new = %q, want %q", ch.field, ch.newValue, want[ch.field])
		}
	}
}

func TestPlannedChangesUpdatesFieldsThatDiffer(t *testing.T) {
	fm := map[string]string{"page_id": "999", "space": "OLD", "parent": "1"}
	page := &client.Page{ID: "123", ParentID: "2", Links: client.Links{WebUI: "/spaces/ENG/pages/123/Runbook"}}
	got := plannedChanges(fm, page, "")
	byField := map[string]change{}
	for _, ch := range got {
		byField[ch.field] = ch
	}
	if ch := byField["page_id"]; ch.oldDisplay != "999" || ch.newValue != "123" {
		t.Errorf("page_id change = %+v, want 999 -> 123", ch)
	}
	if ch := byField["space"]; ch.oldDisplay != "OLD" || ch.newValue != "ENG" {
		t.Errorf("space change = %+v, want OLD -> ENG", ch)
	}
	if ch := byField["parent"]; ch.oldDisplay != "1" || ch.newValue != "2" {
		t.Errorf("parent change = %+v, want 1 -> 2", ch)
	}
}

func TestPlannedChangesParentNullNormalizes(t *testing.T) {
	// A top-level live page (no ParentID) already recorded as "null" must not be
	// treated as a diff -- orNull("") and the frontmatter's "null" must compare equal.
	fm := map[string]string{"parent": "null"}
	page := &client.Page{ID: "1", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}
	got := plannedChanges(fm, page, "")
	for _, ch := range got {
		if ch.field == "parent" {
			t.Errorf("parent change = %+v, want none (both sides are null)", ch)
		}
	}
}

func TestPlannedChangesTitlePresentIsUntouched(t *testing.T) {
	fm := map[string]string{"page_id": "1", "space": "ENG", "parent": "null", "title": "Kept"}
	page := &client.Page{ID: "1", Title: "Live Title", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}
	got := plannedChanges(fm, page, "")
	for _, ch := range got {
		if ch.field == "title" {
			t.Errorf("title change = %+v, want none: an existing title is never overwritten", ch)
		}
	}
}

func TestPlannedChangesSkipsWidthWhenLiveWidthUnknown(t *testing.T) {
	fm := map[string]string{"page_id": "1", "space": "ENG", "parent": "null", "title": "X"}
	page := &client.Page{ID: "1", Title: "X", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}
	got := plannedChanges(fm, page, "")
	for _, ch := range got {
		if ch.field == "page_width" {
			t.Errorf("page_width change = %+v, want none when liveWidth is unknown", ch)
		}
	}
}

func TestPlannedChangesWidthDefaultsToMaxWhenUnset(t *testing.T) {
	fm := map[string]string{"page_id": "1", "space": "ENG", "parent": "null", "title": "X"}
	page := &client.Page{ID: "1", Title: "X", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}

	t.Run("live width already max: no change", func(t *testing.T) {
		got := plannedChanges(fm, page, "max")
		for _, ch := range got {
			if ch.field == "page_width" {
				t.Errorf("page_width change = %+v, want none: unset frontmatter defaults to max", ch)
			}
		}
	})
	t.Run("live width differs: filled from (none)", func(t *testing.T) {
		got := plannedChanges(fm, page, "narrow")
		var found *change
		for i, ch := range got {
			if ch.field == "page_width" {
				found = &got[i]
			}
		}
		if found == nil || found.oldDisplay != noneDisplay || found.newValue != "narrow" {
			t.Errorf("page_width change = %+v, want (none) -> narrow", found)
		}
	})
}

func TestPlannedChangesWidthCaseInsensitive(t *testing.T) {
	fm := map[string]string{
		"page_id": "1", "space": "ENG", "parent": "null", "title": "X", "page_width": " Wide ",
	}
	page := &client.Page{ID: "1", Title: "X", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}
	got := plannedChanges(fm, page, "wide")
	for _, ch := range got {
		if ch.field == "page_width" {
			t.Errorf("page_width change = %+v, want none: %q normalizes to wide", ch, fm["page_width"])
		}
	}
}

func TestPlannedChangesWidthDiffers(t *testing.T) {
	fm := map[string]string{
		"page_id": "1", "space": "ENG", "parent": "null", "title": "X", "page_width": "narrow",
	}
	page := &client.Page{ID: "1", Title: "X", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}
	got := plannedChanges(fm, page, "max")
	var found *change
	for i, ch := range got {
		if ch.field == "page_width" {
			found = &got[i]
		}
	}
	if found == nil || found.oldDisplay != "narrow" || found.newValue != "max" {
		t.Errorf("page_width change = %+v, want narrow -> max", found)
	}
}

// --- processFile: the write-only-on-a-real-change guarantee -------------------

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixServer answers locatePage's page_id lookup and pagewidth.Read's
// content-property lookup for one page.
func fixServer(t *testing.T, page string, widthProperty string) *client.ConfluenceClient {
	t.Helper()
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			if widthProperty == "" {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"value":` + widthProperty + `}]}`))
		default:
			_, _ = w.Write([]byte(page))
		}
	})
}

func TestProcessFileConsistentDoesNotWrite(t *testing.T) {
	content := "---\npage_id: 1\nspace: ENG\nparent: null\ntitle: X\npage_width: max\n---\nbody\n"
	path := writeFixture(t, content)
	c := fixServer(t, pageJSON("1", "X", "", "/spaces/ENG/pages/1/X"), `"max"`)

	r := processFile(path, c)
	if !r.ok || r.status != statusConsistent {
		t.Fatalf("result = %+v, want ok/consistent", r)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("file was modified, want it untouched:\n%s", got)
	}
}

func TestProcessFileDryRunDoesNotWrite(t *testing.T) {
	content := "---\npage_id: 1\ntitle: X\n---\nbody\n"
	path := writeFixture(t, content)
	c := fixServer(t, pageJSON("1", "X", "", "/spaces/ENG/pages/1/X"), `"max"`)

	dryRun = true
	t.Cleanup(func() { dryRun = false })

	r := processFile(path, c)
	if !r.ok || r.status != statusChanged || len(r.changes) == 0 {
		t.Fatalf("result = %+v, want ok/changed with a nonempty diff", r)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("dry-run modified the file, want it untouched:\n%s", got)
	}
}

func TestProcessFileWritesOnRealChange(t *testing.T) {
	content := "---\npage_id: 123\ntitle: X\n---\nbody\n"
	path := writeFixture(t, content)
	c := fixServer(t, pageJSON("123", "X", "", "/spaces/ENG/pages/123/X"), `"max"`)

	r := processFile(path, c)
	if !r.ok || r.status != statusChanged {
		t.Fatalf("result = %+v, want ok/changed", r)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "page_id: 123") {
		t.Errorf("file = %q, want it to record the resolved page_id", got)
	}
	if !strings.Contains(string(got), "space: ENG") {
		t.Errorf("file = %q, want it to record the resolved space", got)
	}
}

func TestProcessFileFailsWhenPageNotFound(t *testing.T) {
	content := "---\npage_id: 999\ntitle: X\n---\nbody\n"
	path := writeFixture(t, content)
	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":404,"title":"Cannot find a page with id 999"}]}`))
	})

	r := processFile(path, c)
	if r.ok {
		t.Fatal("want a failure when the page_id resolves to nothing")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("file was modified on failure, want it untouched:\n%s", got)
	}
}

// --- norm / orNull -------------------------------------------------------------

func TestNorm(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"  ":     "",
		"null":   "",
		" null ": "",
		"ENG":    "ENG",
		" ENG  ": "ENG",
		"Null":   "Null", // only the literal lowercase "null" is a sentinel
	}
	for in, want := range cases {
		if got := norm(in); got != want {
			t.Errorf("norm(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOrNull(t *testing.T) {
	if got := orNull(""); got != "null" {
		t.Errorf(`orNull("") = %q, want "null"`, got)
	}
	if got := orNull("123"); got != "123" {
		t.Errorf(`orNull("123") = %q, want "123"`, got)
	}
}
