package create

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/project"
)

// fakeConfluence is a minimal in-memory double covering exactly what create's
// full flow touches: space resolution, title-freeness search, page
// create/update, and page-width content properties. It has no attachment
// support, so every fixture in this file must reference no local images
// (planAttachments skips ListAttachments entirely when there are none to
// sync, so nothing here needs to fake it).
type fakeConfluence struct {
	t      *testing.T
	mu     sync.Mutex
	nextID int
	pages  map[string]*fakePage // id -> page

	// failCreateForTitle, when set, makes CreatePage fail for that one title --
	// used to test a reserve failure cascading to a child.
	failCreateForTitle string
	// failUpdateForTitle, when set, makes UpdatePage fail for the page that was
	// created under that title -- used to test a publish-phase failure after a
	// successful reserve.
	failUpdateForTitle string
}

type fakePage struct {
	id, title, parentID, spaceID, body string
	version                            int
}

func newFakeConfluence(t *testing.T) (*client.ConfluenceClient, *fakeConfluence) {
	t.Helper()
	f := &fakeConfluence{t: t, nextID: 100, pages: map[string]*fakePage{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL, Username: "u", Token: "t"}), f
}

func (f *fakeConfluence) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/wiki/api/v2/spaces":
		_, _ = fmt.Fprint(w, `{"results":[{"id":"space1"}]}`)

	case r.Method == http.MethodGet && r.URL.Path == "/wiki/api/v2/pages":
		// The title-freeness search (checkTitleFree). Every title is free.
		_, _ = fmt.Fprint(w, `{"results":[]}`)

	case r.Method == http.MethodPost && r.URL.Path == "/wiki/api/v2/pages":
		f.createPage(w, r)

	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/wiki/api/v2/pages/"):
		f.updatePage(w, r)

	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/properties"):
		_, _ = fmt.Fprint(w, `{"results":[]}`)

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/wiki/api/v2/pages/"):
		// UpdatePage's updateLanded re-read after a failed PUT (client.go). Always
		// answer not-found, so a forced failure in these tests is never mistaken
		// for a write that actually landed.
		w.WriteHeader(http.StatusNotFound)

	case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/properties"):
		w.WriteHeader(http.StatusOK)

	default:
		f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (f *fakeConfluence) createPage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SpaceID  string `json:"spaceId"`
		Title    string `json:"title"`
		ParentID string `json:"parentId"`
		Body     struct {
			Value string `json:"value"`
		} `json:"body"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if f.failCreateForTitle != "" && body.Title == f.failCreateForTitle {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `boom`)
		return
	}

	f.nextID++
	id := strconv.Itoa(f.nextID)
	f.pages[id] = &fakePage{
		id: id, title: body.Title, parentID: body.ParentID, spaceID: body.SpaceID,
		body: body.Body.Value, version: 1,
	}
	_, _ = fmt.Fprintf(w, `{"id":%q,"title":%q,"spaceId":%q,"version":{"number":1},`+
		`"_links":{"webui":"/spaces/ENG/pages/%s"}}`, id, body.Title, body.SpaceID, id)
}

func (f *fakeConfluence) updatePage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/wiki/api/v2/pages/"), "/properties")
	var body struct {
		Title string `json:"title"`
		Body  struct {
			Value string `json:"value"`
		} `json:"body"`
		Version struct {
			Number int `json:"number"`
		} `json:"version"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	p, ok := f.pages[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if f.failUpdateForTitle != "" && p.title == f.failUpdateForTitle {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `boom`)
		return
	}
	p.title, p.body, p.version = body.Title, body.Body.Value, body.Version.Number
	_, _ = fmt.Fprintf(w, `{"id":%q,"title":%q,"version":{"number":%d},`+
		`"_links":{"webui":"/spaces/ENG/pages/%s"}}`, id, p.title, p.version, id)
}

// write writes a markdown fixture and returns its path.
func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// resetOpts zeroes every package-level flag var this file's tests touch, and
// restores the previous values on cleanup -- these are cobra flag globals, so
// a test setting one must not leak it into the next.
func resetOpts(t *testing.T) {
	t.Helper()
	space, parent, title, width := spaceOpt, parentOpt, titleOpt, pageWidthOpt
	persist, noPersist, dryRun := persistOpt, noPersistOpt, dryRunOpt
	spaceOpt, parentOpt, titleOpt, pageWidthOpt = "", "", "", ""
	persistOpt, noPersistOpt, dryRunOpt = true, false, false
	t.Cleanup(func() {
		spaceOpt, parentOpt, titleOpt, pageWidthOpt = space, parent, title, width
		persistOpt, noPersistOpt, dryRunOpt = persist, noPersist, dryRun
	})
}

// buildRecords resolves every file into a record via the same resolveFile the
// real command uses, then topologically sorts them -- the exact plumbing
// run() drives, minus the cobra/flag-parsing layer.
func buildRecords(t *testing.T, c *client.ConfluenceClient, files []string) []record {
	t.Helper()
	inSetAbs := map[string]bool{}
	for _, f := range files {
		if abs, err := filepath.Abs(f); err == nil {
			inSetAbs[abs] = true
		}
	}
	spaceCache := map[string]string{}
	roots := project.NewCache("")
	t.Cleanup(roots.Close)
	indexes := linkindex.NewCache()

	var records []record
	for _, f := range files {
		r, err := resolveFile(f, c, inSetAbs, spaceCache, roots, indexes)
		if err != nil {
			t.Fatalf("resolveFile(%s): %v", f, err)
		}
		records = append(records, r)
	}
	byAbs := map[string]record{}
	for _, r := range records {
		byAbs[r.absPath] = r
	}
	ordered, err := topoSort(records, byAbs)
	if err != nil {
		t.Fatalf("topoSort: %v", err)
	}
	return ordered
}

// TestCreateAllResolvesLinksRegardlessOfDirection is commit 8's whole point:
// two sibling files link to each other, with no parent relationship deciding
// which is created first. Before the reserve/publish split, the link index
// was a snapshot taken before either file existed, so *neither* direction
// resolved (a regression from the old per-directory-rebuild code, which at
// least resolved the backward case). After the split, both resolve, because
// every id is reserved before either file is converted.
func TestCreateAllResolvesLinksRegardlessOfDirection(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"
	aPath := write(t, dir, "a.md", "---\ntitle: A\n---\n[to b](b.md)\n")
	bPath := write(t, dir, "b.md", "---\ntitle: B\n---\n[to a](a.md)\n")

	c, fake := newFakeConfluence(t)
	ordered := buildRecords(t, c, []string{aPath, bPath})
	results := createAll(ordered, c, true)

	for _, res := range results {
		if !res.ok {
			t.Fatalf("file %s failed: %s", res.file, res.errMsg)
		}
	}

	aBody := fake.pages[results[0].pageID].body
	bBody := fake.pages[results[1].pageID].body
	if !strings.Contains(aBody, "/pages/") || strings.Contains(aBody, `href="b.md"`) {
		t.Errorf("a.md's link to b.md did not resolve:\n%s", aBody)
	}
	if !strings.Contains(bBody, "/pages/") || strings.Contains(bBody, `href="a.md"`) {
		t.Errorf("b.md's link to a.md did not resolve:\n%s", bBody)
	}
}

// TestCreateAllNoPersistStillResolvesLinks is the other half of the point:
// --no-persist means the reserved id is never written to frontmatter, but the
// link index is seeded in memory regardless, so publish still resolves a link
// to it within the same run.
func TestCreateAllNoPersistStillResolvesLinks(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"
	aPath := write(t, dir, "a.md", "---\ntitle: A\n---\n[to b](b.md)\n")
	bPath := write(t, dir, "b.md", "---\ntitle: B\n---\nno links here\n")

	c, fake := newFakeConfluence(t)
	ordered := buildRecords(t, c, []string{aPath, bPath})
	results := createAll(ordered, c, false) // doPersist=false

	for _, res := range results {
		if !res.ok {
			t.Fatalf("file %s failed: %s", res.file, res.errMsg)
		}
		if res.persisted {
			t.Errorf("file %s reports persisted under --no-persist", res.file)
		}
	}
	aBody := fake.pages[results[0].pageID].body
	if !strings.Contains(aBody, "/pages/") || strings.Contains(aBody, `href="b.md"`) {
		t.Errorf("a.md's link to b.md did not resolve under --no-persist:\n%s", aBody)
	}

	// Frontmatter on disk must be untouched.
	raw, err := os.ReadFile(aPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "page_id") {
		t.Errorf("--no-persist wrote page_id back to %s", aPath)
	}
}

// TestCreateAllReserveFailureSkipsChildPublish covers cascading failure: a
// parent whose reservation fails must never reach CreatePage for its child,
// and the child's result must say why.
func TestCreateAllReserveFailureSkipsChildPublish(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"
	parentPath := write(t, dir, "parent.md", "---\ntitle: Parent\n---\nbody\n")
	childPath := write(t, dir, "child.md", "---\ntitle: Child\nparent: parent.md\n---\nbody\n")

	c, fake := newFakeConfluence(t)
	fake.failCreateForTitle = "Parent"
	ordered := buildRecords(t, c, []string{parentPath, childPath})
	results := createAll(ordered, c, true)

	if results[0].ok {
		t.Fatal("parent reservation should have failed")
	}
	if results[1].ok {
		t.Fatal("child must not be created when its parent failed")
	}
	if !strings.Contains(results[1].errMsg, "parent page was not created") {
		t.Errorf("child errMsg = %q, want it to name the reason", results[1].errMsg)
	}
	if len(fake.pages) != 0 {
		t.Errorf("no page should have been created, got %d", len(fake.pages))
	}
}

// TestCreateAllDryRunCreatesNothing asserts the reserve/publish split honors
// --dry-run at both phases: no CreatePage, no UpdatePage, nothing written to
// disk, but the preview still resolves links (against whatever the index
// already has) and reports as if it had run.
func TestCreateAllDryRunCreatesNothing(t *testing.T) {
	resetOpts(t)
	dryRunOpt = true
	dir := t.TempDir()
	spaceOpt = "ENG"
	aPath := write(t, dir, "a.md", "---\ntitle: A\n---\nbody\n")

	c, fake := newFakeConfluence(t)
	ordered := buildRecords(t, c, []string{aPath})
	results := createAll(ordered, c, true)

	if !results[0].ok {
		t.Fatalf("dry-run result failed: %s", results[0].errMsg)
	}
	if results[0].pageID != "" {
		t.Errorf("pageID = %q, want empty under --dry-run", results[0].pageID)
	}
	if !results[0].persisted {
		t.Error("persisted should reflect intent (true) even though nothing was written")
	}
	if len(fake.pages) != 0 {
		t.Errorf("dry-run must create nothing, got %d pages", len(fake.pages))
	}
	raw, err := os.ReadFile(aPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "page_id") {
		t.Error("dry-run must not write to the file")
	}
}

// TestCreateAllPublishFailureNullsPageIDAndURL covers a publish-phase failure
// after a successful reserve: the stub really was created on the server, but
// the result must still report page_id/url as null on failure -- the same
// contract abortedResult holds for every failure except a blocked page_id --
// or a --json consumer sees a "failed" result that also names a page.
func TestCreateAllPublishFailureNullsPageIDAndURL(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"
	aPath := write(t, dir, "a.md", "---\ntitle: A\n---\nbody\n")

	c, fake := newFakeConfluence(t)
	fake.failUpdateForTitle = "A"
	ordered := buildRecords(t, c, []string{aPath})
	results := createAll(ordered, c, true)

	if results[0].ok {
		t.Fatal("publish should have failed")
	}
	if results[0].pageID != "" {
		t.Errorf("pageID = %q, want empty on a publish-phase failure", results[0].pageID)
	}
	if results[0].url != "" {
		t.Errorf("url = %q, want empty on a publish-phase failure", results[0].url)
	}
	j := results[0].jsonResult()
	if j.PageID != nil || j.URL != nil {
		t.Errorf("json page_id=%v url=%v, want both null", j.PageID, j.URL)
	}
}

// TestCreateAllDryRunCrossLinkResolves covers a batch of two new files linking
// to each other under --dry-run: since neither has a real id yet, the shared
// link index must still be seeded (with an empty id) so the preview resolves
// the link exactly like a real run would, rather than warning it can't be
// resolved.
func TestCreateAllDryRunCrossLinkResolves(t *testing.T) {
	resetOpts(t)
	dryRunOpt = true
	dir := t.TempDir()
	spaceOpt = "ENG"
	aPath := write(t, dir, "a.md", "---\ntitle: A\n---\n[to b](b.md)\n")
	bPath := write(t, dir, "b.md", "---\ntitle: B\n---\n[to a](a.md)\n")

	c, _ := newFakeConfluence(t)
	ordered := buildRecords(t, c, []string{aPath, bPath})
	results := createAll(ordered, c, true)

	for _, res := range results {
		if !res.ok {
			t.Fatalf("file %s failed: %s", res.file, res.errMsg)
		}
		for _, w := range res.warnings {
			t.Errorf("file %s: unexpected warning in dry-run preview: %s", res.file, w)
		}
	}
}

// TestReserveOneWriteFailureKeepsPageIDVisible covers the orphan case: the
// stub is really created on the server, and only the frontmatter write-back
// afterward fails. res must still carry the id/url -- via failKeepingPage --
// or the page becomes untraceable, with nothing local pointing at it.
func TestReserveOneWriteFailureKeepsPageIDVisible(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"
	aPath := write(t, dir, "a.md", "---\ntitle: A\n---\nbody\n")

	c, fake := newFakeConfluence(t)
	ordered := buildRecords(t, c, []string{aPath})
	r := ordered[0]
	// os.WriteFile refuses a directory, simulating the write-back failing
	// after CreatePage has already succeeded against the fake server.
	r.filename = dir

	res, pageID, _, ok := reserveOne(r, "", c, true)
	if ok {
		t.Fatal("reserveOne should report failure when the write-back fails")
	}
	if pageID != "" {
		t.Errorf("returned pageID = %q, want empty (nothing for phase 3 to publish)", pageID)
	}
	if res.ok {
		t.Error("result should be failed")
	}
	if res.pageID == "" || res.url == "" {
		t.Errorf("result pageID=%q url=%q, want both kept so the orphaned page stays traceable",
			res.pageID, res.url)
	}
	if len(fake.pages) != 1 {
		t.Fatalf("fake pages = %d, want exactly the one CreatePage created before the write failed", len(fake.pages))
	}
}

// TestCreateAllStubIsEmptyThenPublished checks the actual sequencing: the
// stub created in phase 2 has no body, and phase 3's UpdatePage is what gives
// it real content -- the "every page's v1 is a stub" cost 025 accepts.
func TestCreateAllStubIsEmptyThenPublished(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"
	aPath := write(t, dir, "a.md", "---\ntitle: A\n---\n# Hello\n")

	c, fake := newFakeConfluence(t)
	ordered := buildRecords(t, c, []string{aPath})
	results := createAll(ordered, c, true)

	if !results[0].ok {
		t.Fatalf("failed: %s", results[0].errMsg)
	}
	p := fake.pages[results[0].pageID]
	if p.version != 2 {
		t.Errorf("final version = %d, want 2 (stub at 1, published at 2)", p.version)
	}
	if !strings.Contains(p.body, "Hello") {
		t.Errorf("published body missing content: %q", p.body)
	}
}

// TestCreateBatchIgnoresDirectoryNesting is guarantee L8 (no-layout-inference,
// docs/guarantees.md) at the batch level: a file nested several directories
// deep, alongside files at shallower levels with names that could plausibly
// read as its ancestors, must still be created as a top-level page unless a
// parent: field or --parent said otherwise. Nothing about the tree shape may
// contribute to the decision.
func TestCreateBatchIgnoresDirectoryNesting(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"

	shallow := write(t, dir, "docs.md", "---\ntitle: Docs\n---\nbody\n")
	nested := write(t, dir, "docs/guide.md", "---\ntitle: Guide\n---\nbody\n")
	deep := write(t, dir, "docs/guide/page.md", "---\ntitle: Page\n---\nbody\n")

	c, _ := newFakeConfluence(t)
	ordered := buildRecords(t, c, []string{shallow, nested, deep})
	for _, r := range ordered {
		if r.parent.kind != parentTop {
			t.Errorf("file %s: parent kind = %q, want top -- its directory depth must not "+
				"imply a parent relationship to the other files in this batch", r.filename, r.parent.kind)
		}
	}
}
