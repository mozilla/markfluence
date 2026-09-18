package create

import (
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
	return resolveFile(path, c, map[string]bool{}, map[string]string{}, roots,
		linkindex.NewCache(), pagestatus.NewCache(), map[string]string{})
}

// The name is resolved in preflight, against the space's homepage, because the
// page being created does not exist yet and the vocabulary route is per-page.
func TestPreflightResolvesTheStatusAgainstTheSpaceHomepage(t *testing.T) {
	c, paths := preflightServer(t, createVocabulary)
	r, err := resolveWithStatus(t, c,
		"---\ntitle: X\nspace: ENG\npage_status: Ready for review\n---\nbody\n")
	if err != nil {
		t.Fatalf("resolveFile: %v", err)
	}
	if !r.statusDeclared || r.status.ID != 12 {
		t.Fatalf("record status = %+v declared=%v", r.status, r.statusDeclared)
	}
	var askedHomepage bool
	for _, p := range *paths {
		if p == "GET /wiki/rest/api/content/5000/state/available" {
			askedHomepage = true
		}
	}
	if !askedHomepage {
		t.Errorf("paths = %v, want the space homepage asked for the vocabulary", *paths)
	}
}

// A bad name fails preflight, which is the whole reason the lookup is there: the
// alternative is a created page with no status and a warning the author has to
// undo by hand (#127's reasoning).
func TestPreflightRefusesAnUnknownStatusWithNothingWritten(t *testing.T) {
	c, paths := preflightServer(t, createVocabulary)
	_, err := resolveWithStatus(t, c,
		"---\ntitle: X\nspace: ENG\npage_status: Reviewed\n---\nbody\n")
	if err == nil {
		t.Fatal("resolveFile succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "Ready for review") {
		t.Errorf("err = %v, want the space's statuses named", err)
	}
	for _, p := range *paths {
		if strings.HasPrefix(p, "POST") || strings.HasPrefix(p, "PUT") {
			t.Errorf("paths = %v, want nothing written", *paths)
		}
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
		status: client.ContentState{ID: 12, Name: "Ready for review"}, statusDeclared: true,
	}
	entry := persistEntry(r, "456", "123")
	if _, ok := entry.Fields[pagestatus.Field]; ok {
		t.Errorf("entry carries %s: persist records what create resolved, not what the file declared",
			pagestatus.Field)
	}
}
