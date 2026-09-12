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
	"github.com/mozilla/markfluence/internal/frontmatter"
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
	got := plannedChangesFM(fm, page, "max")
	if len(got) != 0 {
		t.Errorf("changes = %+v, want none", got)
	}
}

func TestPlannedChangesFillsMissingFields(t *testing.T) {
	page := &client.Page{ID: "123", Title: "Runbook", Links: client.Links{WebUI: "/spaces/ENG/pages/123/Runbook"}}
	got := plannedChangesFM(map[string]string{}, page, "")
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
	got := plannedChangesFM(fm, page, "")
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
	// A top-level live page (no ParentID) already recorded as null must not be
	// treated as a diff. The frontmatter side is "", not "null": every null
	// spelling parses to "" now, so feeding "null" here would test a map the
	// parser can no longer produce and would pass while fix looped forever.
	fm := map[string]string{"parent": ""}
	page := &client.Page{ID: "1", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}
	got := plannedChangesFM(fm, page, "")
	for _, ch := range got {
		if ch.field == "parent" {
			t.Errorf("parent change = %+v, want none (both sides are null)", ch)
		}
	}
}

func TestPlannedChangesTitlePresentIsUntouched(t *testing.T) {
	fm := map[string]string{"page_id": "1", "space": "ENG", "parent": "null", "title": "Kept"}
	page := &client.Page{ID: "1", Title: "Live Title", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}
	got := plannedChangesFM(fm, page, "")
	for _, ch := range got {
		if ch.field == "title" {
			t.Errorf("title change = %+v, want none: an existing title is never overwritten", ch)
		}
	}
}

func TestPlannedChangesSkipsWidthWhenLiveWidthUnknown(t *testing.T) {
	fm := map[string]string{"page_id": "1", "space": "ENG", "parent": "null", "title": "X"}
	page := &client.Page{ID: "1", Title: "X", Links: client.Links{WebUI: "/spaces/ENG/pages/1/X"}}
	got := plannedChangesFM(fm, page, "")
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
		got := plannedChangesFM(fm, page, "max")
		for _, ch := range got {
			if ch.field == "page_width" {
				t.Errorf("page_width change = %+v, want none: unset frontmatter defaults to max", ch)
			}
		}
	})
	t.Run("live width differs: filled from (none)", func(t *testing.T) {
		got := plannedChangesFM(fm, page, "narrow")
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
	got := plannedChangesFM(fm, page, "wide")
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
	got := plannedChangesFM(fm, page, "max")
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
	content := "---\ntitle: X\nspace: ENG\nparent: null\npage_id: 1\npage_width: max\n---\nbody\n"
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

// TestProcessFileNormalizesFieldOrder pins that a file whose values all match
// its live page is still rewritten when its fields are out of canonical order,
// and reports that separately from any value change.
func TestProcessFileNormalizesFieldOrder(t *testing.T) {
	content := "---\npage_id: 1\nspace: ENG\nparent: null\ntitle: X\npage_width: max\n---\nbody\n"
	path := writeFixture(t, content)
	c := fixServer(t, pageJSON("1", "X", "", "/spaces/ENG/pages/1/X"), `"max"`)

	r := processFile(path, c)
	if !r.ok || r.status != statusChanged {
		t.Fatalf("result = %+v, want ok/changed", r)
	}
	if !r.reordered {
		t.Error("reordered = false, want true")
	}
	if len(r.changes) != 0 {
		t.Errorf("changes = %+v, want none: only the order differs", r.changes)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntitle: X\nspace: ENG\nparent: null\npage_id: 1\npage_width: max\n---\nbody\n"
	if string(got) != want {
		t.Errorf("file =\n%q\nwant\n%q", got, want)
	}
}

// TestProcessFileTopLevelPageConverges is the regression for a fix that planned
// `parent: (none) -> null` forever: a null parent parses to "", which the old
// present-but-blank branch read as "no value" and re-wrote on every run.
func TestProcessFileTopLevelPageConverges(t *testing.T) {
	content := "---\ntitle: X\nspace: ENG\nparent: null\npage_id: 1\npage_width: max\n---\nbody\n"
	path := writeFixture(t, content)
	c := fixServer(t, pageJSON("1", "X", "", "/spaces/ENG/pages/1/X"), `"max"`)

	r := processFile(path, c)
	if r.status != statusConsistent {
		t.Fatalf("status = %q with changes %+v, want consistent", r.status, r.changes)
	}
}

// plannedChangesFM adapts plannedChanges for the tests that predate labels:
// no list fields, and a nil liveLabels meaning the label read failed, which
// plans no label change at all. That is exactly what a width or title test
// wants -- one field under test and nothing else moving.
func plannedChangesFM(fm map[string]string, page *client.Page, liveWidth string) []change {
	mf := &frontmatter.MarkdownFile{Frontmatter: fm, Lists: map[string][]string{}}
	return plannedChanges(mf, page, liveWidth, nil)
}

// --- labels -------------------------------------------------------------------

// mdFile parses a frontmatter block into the MarkdownFile plannedChanges takes,
// so a label test exercises the real reader rather than a hand-built Lists map.
func mdFile(t *testing.T, block string) *frontmatter.MarkdownFile {
	t.Helper()
	mf, err := frontmatter.Parse("doc.md", "---\n"+block+"\n---\nbody\n")
	if err != nil {
		t.Fatalf("Parse(%q) = %v", block, err)
	}
	return mf
}

func labelChangeIn(changes []change) (change, bool) {
	for _, ch := range changes {
		if ch.field == "labels" {
			return ch, true
		}
	}
	return change{}, false
}

// TestFixAdoptsHandLabels is the reason fix reconciles labels at all: it is the
// only way to take over a page somebody labeled in the UI. Note the direction
// -- update leaves an absent key alone, fix fills it in, because fix reconciles
// the file to the page and update the page to the file.
func TestFixAdoptsHandLabels(t *testing.T) {
	page := &client.Page{ID: "1", Title: "T"}
	got := plannedChanges(mdFile(t, "page_id: 1"), page, "", []string{"ci/cd", "runbook"})

	ch, ok := labelChangeIn(got)
	if !ok {
		t.Fatalf("changes = %+v, want a labels change", got)
	}
	if ch.oldDisplay != noneDisplay {
		t.Errorf("old = %q, want %q", ch.oldDisplay, noneDisplay)
	}
	if ch.newValue != "[ci/cd, runbook]" {
		t.Errorf("new = %q, want [ci/cd, runbook]", ch.newValue)
	}
	if len(ch.newList) != 2 {
		t.Errorf("newList = %v, want the two names to write", ch.newList)
	}
}

// TestFixLeavesAMatchingSetAlone: compared as sets, so a file that merely
// orders its labels differently or repeats one is not rewritten, and the
// author's own ordering survives.
func TestFixLeavesAMatchingSetAlone(t *testing.T) {
	page := &client.Page{ID: "1", Title: "T"}
	for _, block := range []string{
		"page_id: 1\nlabels: [ci/cd, runbook]",
		"page_id: 1\nlabels: [runbook, ci/cd]",
		"page_id: 1\nlabels: [runbook, ci/cd, runbook]",
	} {
		got := plannedChanges(mdFile(t, block), page, "", []string{"ci/cd", "runbook"})
		if ch, ok := labelChangeIn(got); ok {
			t.Errorf("%q planned %+v, want no labels change", block, ch)
		}
	}
}

// TestFixPlansNothingWhenNeitherHasLabels: a file with no key and a page with
// no labels must not gain a "labels: []" that says nothing.
func TestFixPlansNothingWhenNeitherHasLabels(t *testing.T) {
	page := &client.Page{ID: "1", Title: "T"}
	got := plannedChanges(mdFile(t, "page_id: 1"), page, "", []string{})
	if ch, ok := labelChangeIn(got); ok {
		t.Errorf("planned %+v, want no labels change", ch)
	}
}

// TestFixPlansNothingWhenTheReadFailed is the distinction nil carries. A failed
// read must not look like "the page has no labels", or a transient failure
// would propose stripping every label from the file.
func TestFixPlansNothingWhenTheReadFailed(t *testing.T) {
	page := &client.Page{ID: "1", Title: "T"}
	got := plannedChanges(mdFile(t, "page_id: 1\nlabels: [runbook]"), page, "", nil)
	if ch, ok := labelChangeIn(got); ok {
		t.Errorf("planned %+v, want no labels change when the read failed", ch)
	}
}

// TestFixRemovesLabelsThePageNoLongerHas: reconciling downward too, including
// to the empty set, which is a real state a page can be in.
func TestFixRemovesLabelsThePageNoLongerHas(t *testing.T) {
	page := &client.Page{ID: "1", Title: "T"}
	got := plannedChanges(mdFile(t, "page_id: 1\nlabels: [runbook, gone]"), page, "", []string{})

	ch, ok := labelChangeIn(got)
	if !ok {
		t.Fatalf("changes = %+v, want a labels change", got)
	}
	if ch.newValue != "[]" {
		t.Errorf("new = %q, want []", ch.newValue)
	}
	if ch.newList == nil || len(ch.newList) != 0 {
		t.Errorf("newList = %v, want a non-nil empty list", ch.newList)
	}
}

// TestFixReconcilesAnInvalidLabel: the live set is what gets written and it
// came from the server, so it is valid by construction. This is the one place
// fix repairs a file check would have refused.
func TestFixReconcilesAnInvalidLabel(t *testing.T) {
	page := &client.Page{ID: "1", Title: "T"}
	got := plannedChanges(mdFile(t, `page_id: 1
labels: ["Runbook Two"]`), page, "", []string{"runbook", "two"})

	ch, ok := labelChangeIn(got)
	if !ok {
		t.Fatalf("changes = %+v, want the invalid label reconciled", got)
	}
	if ch.newValue != "[runbook, two]" {
		t.Errorf("new = %q, want [runbook, two]", ch.newValue)
	}
}

// labelFixServer answers the page, the width property, and a label list.
func labelFixServer(t *testing.T, page string, liveLabels ...string) *client.ConfluenceClient {
	t.Helper()
	rows := make([]string, 0, len(liveLabels))
	for i, n := range liveLabels {
		rows = append(rows, fmt.Sprintf(`{"id":"%d","name":%q,"prefix":"global"}`, i+1, n))
	}
	body := `{"results":[` + strings.Join(rows, ",") + `]}`
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "label"):
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[{"value":"max"}]}`))
		default:
			_, _ = w.Write([]byte(page))
		}
	})
}

// TestProcessFileKeepsBlockLabelStyle is the end-to-end form-preservation
// contract. A set large enough to be written as a block list is exactly the set
// whose flow spelling is an unreadable single line, so converting it on the
// first fix that changes one label would defeat the reason block form is
// accepted at all.
func TestProcessFileKeepsBlockLabelStyle(t *testing.T) {
	content := "---\ntitle: X\nspace: ENG\nparent: null\npage_id: 1\n" +
		"labels:\n  - runbook\n  - stale\npage_width: max\n---\nbody\n"
	path := writeFixture(t, content)
	c := labelFixServer(t, pageJSON("1", "X", "", "/spaces/ENG/pages/1/X"), "runbook", "howto")

	r := processFile(path, c)
	if !r.ok || r.status != statusChanged {
		t.Fatalf("result = %+v, want ok/changed", r)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "labels: [") {
		t.Errorf("a block list was converted to flow:\n%s", got)
	}
	for _, want := range []string{"- howto", "- runbook"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("file = %q, want a %q item", got, want)
		}
	}
	if strings.Contains(string(got), "stale") {
		t.Errorf("file = %q, want the dropped label gone", got)
	}
}

// TestProcessFileLabelFixConverges is the property the "continuous"/"delivery"
// pair in the SRE space is the absence of: reconcile once, and the second run
// has nothing to do. A file that never converges means running fix, being told
// it changed something, and getting the same change forever.
func TestProcessFileLabelFixConverges(t *testing.T) {
	content := "---\ntitle: X\nspace: ENG\nparent: null\npage_id: 1\npage_width: max\n---\nbody\n"
	path := writeFixture(t, content)
	c := labelFixServer(t, pageJSON("1", "X", "", "/spaces/ENG/pages/1/X"), "ci/cd", "runbook")

	first := processFile(path, c)
	if !first.ok || first.status != statusChanged {
		t.Fatalf("first run = %+v, want ok/changed", first)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "labels: [ci/cd, runbook]") {
		t.Errorf("file = %q, want a sorted flow list for a newly added key", got)
	}

	second := processFile(path, c)
	if !second.ok || second.status != statusConsistent {
		t.Fatalf("second run = %+v (changes %+v), want ok/consistent", second, second.changes)
	}
}

// TestFixCorrectsALabelCaseMismatch. update and check warn "Update the file to
// match" for `labels: [Runbook]` against a page carrying `runbook`, since
// Confluence lowercases server-side. fix is the command that is supposed to do
// that updating, and it used to normalize case before comparing -- so it saw no
// difference, reported "already consistent", and left the author with a warning
// on every run and no command that would silence it.
func TestFixCorrectsALabelCaseMismatch(t *testing.T) {
	page := &client.Page{ID: "1", Title: "T"}
	got := plannedChanges(mdFile(t, "page_id: 1\nlabels: [Runbook]"), page, "", []string{"runbook"})

	ch, ok := labelChangeIn(got)
	if !ok {
		t.Fatalf("changes = %+v, want the case mismatch reconciled", got)
	}
	if ch.newValue != "[runbook]" {
		t.Errorf("new = %q, want [runbook]", ch.newValue)
	}
	if ch.oldDisplay != "[Runbook]" {
		t.Errorf("old = %q, want the spelling the file had", ch.oldDisplay)
	}
}

// TestFixRepairsAScalarLabelsValue: a scalar labels: is refused by check,
// update and create, so fix has to offer a way out of it whatever the page
// carries. It used to read the key as absent, which meant "already consistent"
// for a file nothing else would accept -- and it repaired the same file when
// the page happened to have labels, so the behaviour was inconsistent as well
// as incomplete.
func TestFixRepairsAScalarLabelsValue(t *testing.T) {
	page := &client.Page{ID: "1", Title: "T"}
	tests := []struct {
		name, block string
		live        []string
		wantNew     string
	}{
		{"page has none", "page_id: 1\nlabels: runbook", []string{}, "[]"},
		{"page has some", "page_id: 1\nlabels: runbook", []string{"howto"}, "[howto]"},
		{"null value", "page_id: 1\nlabels:", []string{"howto"}, "[howto]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plannedChanges(mdFile(t, tt.block), page, "", tt.live)
			ch, ok := labelChangeIn(got)
			if !ok {
				t.Fatalf("changes = %+v, want the scalar value repaired", got)
			}
			if ch.newValue != tt.wantNew {
				t.Errorf("new = %q, want %q", ch.newValue, tt.wantNew)
			}
			if ch.newList == nil {
				t.Error("newList = nil, want a list to write")
			}
		})
	}
}
