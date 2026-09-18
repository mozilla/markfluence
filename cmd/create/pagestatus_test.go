package create

import (
	"fmt"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagestatus"
	"github.com/mozilla/markfluence/internal/project"
)

// preflightServer answers everything resolveFile asks: the space lookup (with a
// homepage id), the title search, and the state vocabulary. It records every
// path so a test can assert nothing was written.
func preflightServer(t *testing.T, vocabulary string) (*client.ConfluenceClient, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/state/available"):
			_, _ = w.Write([]byte(vocabulary))
		case strings.HasSuffix(r.URL.Path, "/spaces"):
			// homepageId is in the same response ResolveSpaceID already reads,
			// which is what makes the create-side probe free.
			_, _ = w.Write([]byte(`{"results":[{"id":"77","key":"ENG","homepageId":"5000"}]}`))
		case strings.Contains(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(`{"results":[],"_links":{}}`))
		default:
			_, _ = w.Write([]byte(`{"results":[],"_links":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL}), &paths
}

const createVocabulary = `{"spaceContentStates":[
	{"id":10,"color":"#ffc400","name":"Rough draft"},
	{"id":12,"color":"#57d9a3","name":"Ready for review"}],"customContentStates":[]}`

func resolveWithStatus(t *testing.T, c *client.ConfluenceClient, body string) (record, error) {
	t.Helper()
	dir := t.TempDir()
	path := write(t, dir, "a.md", body)
	roots := project.NewCache("")
	t.Cleanup(roots.Close)
	return resolveFile(path, c, map[string]bool{}, map[string]string{}, roots, linkindex.NewCache())
}

// preflight carries the name and resolves nothing, and asks for no vocabulary
// while doing it. The statuses a page may be given depend on the (caller,
// page) pair rather than on the space, so the only authoritative page is the
// one being created -- and it does not exist yet. Probing the parent or the
// homepage instead was measured refusing a name the new page went on to
// accept, and needing edit permission the account creating pages need not
// have.
func TestPreflightCarriesTheNameAndAsksNothing(t *testing.T) {
	c, paths := preflightServer(t, createVocabulary)
	r, err := resolveWithStatus(t, c,
		"---\ntitle: X\nspace: ENG\npage_status: Ready for review\n---\nbody\n")
	if err != nil {
		t.Fatalf("resolveFile: %v", err)
	}
	if !r.statusDeclared || r.statusName != "Ready for review" {
		t.Fatalf("record = %q declared=%v", r.statusName, r.statusDeclared)
	}
	for _, p := range *paths {
		if strings.Contains(p, "/state") {
			t.Errorf("paths = %v, want no vocabulary request in preflight", *paths)
		}
	}
}

// A name the page will refuse is therefore *not* a preflight failure, and that
// is the knowing cost: it is a warning on a created page instead. The offline
// half is still caught (see TestPreflightRefusesAnEmptyStatus).
func TestPreflightAcceptsAnyNonEmptyName(t *testing.T) {
	c, _ := preflightServer(t, createVocabulary)
	r, err := resolveWithStatus(t, c,
		"---\ntitle: X\nspace: ENG\npage_status: Probably Not Real\n---\nbody\n")
	if err != nil {
		t.Fatalf("resolveFile: %v, want the name carried for the publish phase", err)
	}
	if r.statusName != "Probably Not Real" {
		t.Errorf("statusName = %q", r.statusName)
	}
}

// A file declaring no status asks for no vocabulary: the same
// gated-on-declaration property update has.
func TestPreflightAsksNothingWithoutTheField(t *testing.T) {
	c, paths := preflightServer(t, createVocabulary)
	r, err := resolveWithStatus(t, c, "---\ntitle: X\nspace: ENG\n---\nbody\n")
	if err != nil {
		t.Fatalf("resolveFile: %v", err)
	}
	if r.statusDeclared {
		t.Error("statusDeclared is true for a file with no page_status")
	}
	for _, p := range *paths {
		if strings.Contains(p, "/state") {
			t.Errorf("paths = %v, want no state request", *paths)
		}
	}
}

// An empty value is a defect in the file, caught before any request.
func TestPreflightRefusesAnEmptyStatus(t *testing.T) {
	c, paths := preflightServer(t, createVocabulary)
	_, err := resolveWithStatus(t, c, "---\ntitle: X\nspace: ENG\npage_status:\n---\nbody\n")
	if err == nil {
		t.Fatal("resolveFile succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "has no value") {
		t.Errorf("err = %v", err)
	}
	for _, p := range *paths {
		if strings.Contains(p, "/state") {
			t.Errorf("paths = %v, want no state request", *paths)
		}
	}
}

// page_status is deliberately not persisted, for labels' reasons: persist
// records what create resolved, and a status is something the author declared
// -- writing it back would also rewrite their spelling to the space's.
func TestPersistDoesNotRecordTheStatus(t *testing.T) {
	r := record{
		title: "X", spaceKey: "ENG", width: "max",
		statusName: "Ready for review", statusDeclared: true,
	}
	entry := persistEntry(r, "456", "123")
	if _, ok := entry.Fields[pagestatus.Field]; ok {
		t.Errorf("entry carries %s: persist records what create resolved, not what the file declared",
			pagestatus.Field)
	}
}

// --- the publish phase ----------------------------------------------------------

// publishServer answers publishOne's requests. liveState is what the new page
// reports before the status write; vocabulary is what it may be given.
func publishServer(t *testing.T, vocabulary string, versionAfter int) (*client.ConfluenceClient, *[]string) {
	t.Helper()
	var paths []string
	var wroteStatus bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/state/available"):
			_, _ = w.Write([]byte(vocabulary))
		case strings.HasSuffix(r.URL.Path, "/state"):
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{}`)) // a fresh page carries no status
				return
			}
			wroteStatus = true
			_, _ = w.Write([]byte(`{"contentState":{"id":12,"name":"Ready for review","color":"#57d9a3"}}`))
		case strings.Contains(r.URL.Path, "/child/attachment"):
			_, _ = w.Write([]byte(`{"results":[],"_links":{}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v2/pages/"):
			// The post-status version read: the status write moved the page.
			v := 2
			if wroteStatus {
				v = versionAfter
			}
			_, _ = fmt.Fprintf(w,
				`{"id":"900","title":"X","status":"current","version":{"number":%d},`+
					`"_links":{"webui":"/spaces/ENG/pages/900/X"}}`, v)
		default: // the body PUT and the property writes
			_, _ = w.Write([]byte(
				`{"id":"900","title":"X","version":{"number":2},` +
					`"_links":{"webui":"/spaces/ENG/pages/900/X"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL}), &paths
}

// publishRecord is a record as preflight leaves one: the status carried by
// name. Built directly rather than through resolveFile, which would make the
// server calls this test is trying to observe in isolation.
func publishRecord(t *testing.T, name string) record {
	t.Helper()
	dir := t.TempDir()
	path := write(t, dir, "a.md", "---\ntitle: X\nspace: ENG\n---\nbody\n")
	mf, err := frontmatter.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	root, err := project.FromPath(dir)
	if err != nil {
		t.Fatalf("FromPath: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	index, err := linkindex.Build(root)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return record{
		filename: path, absPath: path, mdfile: mf, title: "X", spaceKey: "ENG",
		width: pagewidth.Max, root: root, index: index,
		statusName: name, statusDeclared: name != "",
	}
}

// The name is resolved against the page that was just created -- the only page
// whose answer is authoritative -- and applied.
func TestPublishResolvesTheStatusAgainstTheCreatedPage(t *testing.T) {
	c, paths := publishServer(t, createVocabulary, 3)
	res := publishOne(publishRecord(t, "Ready for review"), &createResult{}, "900", 1, c,
		pagedoc.NewUserCache())

	if !res.ok {
		t.Fatalf("result = %+v, want ok", res)
	}
	if res.pageStatus == nil || res.pageStatus.Name != "Ready for review" {
		t.Fatalf("pageStatus = %+v", res.pageStatus)
	}
	var asked bool
	for _, p := range *paths {
		if p == "GET /wiki/rest/api/content/900/state/available" {
			asked = true
		}
	}
	if !asked {
		t.Errorf("paths = %v, want the created page asked for its own vocabulary", *paths)
	}
}

// The live bug that motivated moving the resolution here, in create's half: a
// status write bumps the page version, so the base recorded for the new page
// must name where the page ended up. Recording the body PUT's version left
// every created page with a status one version behind itself, and the first
// update of it reported a conflict.
func TestPublishRecordsTheVersionTheStatusWriteLeft(t *testing.T) {
	c, _ := publishServer(t, createVocabulary, 9)
	res := publishOne(publishRecord(t, "Ready for review"), &createResult{}, "900", 1, c,
		pagedoc.NewUserCache())

	if !res.ok {
		t.Fatalf("result = %+v, want ok", res)
	}
	if res.pageVersion != 9 {
		t.Errorf("pageVersion = %d, want 9: the base must name where the page ended up", res.pageVersion)
	}
}

// A name the created page will not take is a warning on a page that exists,
// not a failure -- the trade record.statusName describes. The page keeps its
// body, and the result stays ok so the id is reported rather than orphaned.
func TestPublishWarnsOnAStatusTheNewPageRefuses(t *testing.T) {
	c, paths := publishServer(t, createVocabulary, 3)
	res := publishOne(publishRecord(t, "Reviewed"), &createResult{}, "900", 1, c,
		pagedoc.NewUserCache())

	if !res.ok || res.status != statusCreated {
		t.Fatalf("result = %+v, want an ok/created result", res)
	}
	if res.pageStatus != nil {
		t.Errorf("pageStatus = %+v, want nil for a status that was not set", res.pageStatus)
	}
	var warned bool
	for _, w := range res.warnings {
		if strings.Contains(w, "page status") && strings.Contains(w, "Reviewed") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("warnings = %v, want one naming the refused status", res.warnings)
	}
	for _, p := range *paths {
		if p == "PUT /wiki/rest/api/content/900/state" {
			t.Errorf("paths = %v, want no status write for an unresolvable name", *paths)
		}
	}
}

// An undeclared status reaches no state route at all in the publish phase
// either, not just in preflight.
func TestPublishAsksNothingWithoutTheField(t *testing.T) {
	c, paths := publishServer(t, createVocabulary, 3)
	res := publishOne(publishRecord(t, ""), &createResult{}, "900", 1, c, pagedoc.NewUserCache())
	if !res.ok {
		t.Fatalf("result = %+v, want ok", res)
	}
	for _, p := range *paths {
		if strings.Contains(p, "/state") {
			t.Errorf("paths = %v, want no state request", *paths)
		}
	}
}
