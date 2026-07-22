package client

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	retrySleep = 0 // don't actually sleep between retries in tests
	os.Exit(m.Run())
}

// scripted is a test server that returns canned responses in order and records
// the method of each request it receives.
type scripted struct {
	responses []resp
	calls     []string
	idx       int
}

type resp struct {
	status int
	body   string
}

func newServer(t *testing.T, responses ...resp) (*ConfluenceClient, *scripted) {
	t.Helper()
	s := &scripted{responses: responses}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls = append(s.calls, r.Method)
		if s.idx >= len(s.responses) {
			t.Errorf("unexpected extra request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		out := s.responses[s.idx]
		s.idx++
		w.WriteHeader(out.status)
		_, _ = w.Write([]byte(out.body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "u", "t"), s
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- content properties ------------------------------------------------------

func TestSetContentPropertyCreatesWhenAbsent(t *testing.T) {
	c, s := newServer(t, resp{200, `{"results":[]}`}, resp{200, `{}`})
	action, err := c.SetContentProperty("1", "k", "max")
	if err != nil || action != "set" {
		t.Fatalf("action=%q err=%v, want set/nil", action, err)
	}
	if !eqStrings(s.calls, []string{"GET", "POST"}) {
		t.Errorf("calls = %v, want [GET POST]", s.calls)
	}
}

func TestSetContentPropertySkipsWhenEqual(t *testing.T) {
	c, s := newServer(t, resp{200, `{"results":[{"id":"p","value":"max","version":{"number":2}}]}`})
	action, err := c.SetContentProperty("1", "k", "max")
	if err != nil || action != "unchanged" {
		t.Fatalf("action=%q err=%v, want unchanged/nil", action, err)
	}
	if !eqStrings(s.calls, []string{"GET"}) {
		t.Errorf("calls = %v, want [GET] (no write)", s.calls)
	}
}

func TestSetContentPropertyUpdatesWhenDifferent(t *testing.T) {
	c, s := newServer(t,
		resp{200, `{"results":[{"id":"p","value":"default","version":{"number":2}}]}`},
		resp{200, `{}`})
	action, err := c.SetContentProperty("1", "k", "max")
	if err != nil || action != "set" {
		t.Fatalf("action=%q err=%v, want set/nil", action, err)
	}
	if !eqStrings(s.calls, []string{"GET", "PUT"}) {
		t.Errorf("calls = %v, want [GET PUT]", s.calls)
	}
}

func TestSetContentPropertyRetriesOnceAndDetectsApplied(t *testing.T) {
	// First GET fails; the retry re-reads and finds the value already applied.
	c, s := newServer(t,
		resp{500, `boom`},
		resp{200, `{"results":[{"id":"p","value":"max","version":{"number":2}}]}`})
	action, err := c.SetContentProperty("1", "k", "max")
	if err != nil || action != "unchanged" {
		t.Fatalf("action=%q err=%v, want unchanged/nil", action, err)
	}
	if !eqStrings(s.calls, []string{"GET", "GET"}) {
		t.Errorf("calls = %v, want [GET GET]", s.calls)
	}
}

func TestListContentPropertiesFollowsPagination(t *testing.T) {
	c, s := newServer(t,
		resp{200, `{"results":[{"key":"a"}],"_links":{"next":"/wiki/api/v2/pages/1/properties?cursor=X"}}`},
		resp{200, `{"results":[{"key":"b"}],"_links":{}}`})
	props, err := c.ListContentProperties("1")
	if err != nil {
		t.Fatal(err)
	}
	if len(props) != 2 || props[0].Key != "a" || props[1].Key != "b" {
		t.Errorf("props = %v, want keys [a b]", props)
	}
	if !eqStrings(s.calls, []string{"GET", "GET"}) {
		t.Errorf("calls = %v, want [GET GET]", s.calls)
	}
}

// --- pages -------------------------------------------------------------------

func TestGetPageOrNilReturnsNilOn404(t *testing.T) {
	c, _ := newServer(t, resp{404, `{"errors":[]}`})
	p, err := c.GetPageOrNil("nope")
	if err != nil || p != nil {
		t.Fatalf("GetPageOrNil = %v, %v; want nil, nil", p, err)
	}
}

func TestGetPagePropagatesError(t *testing.T) {
	c, _ := newServer(t, resp{500, `boom`})
	if _, err := c.GetPage("1"); err == nil {
		t.Fatal("GetPage: want error on 500")
	}
}

func TestResolveSpaceID(t *testing.T) {
	c, _ := newServer(t, resp{200, `{"results":[{"id":"123"}]}`})
	if id, err := c.ResolveSpaceID("ENG"); err != nil || id != "123" {
		t.Fatalf("ResolveSpaceID = %q, %v; want 123", id, err)
	}
	c2, _ := newServer(t, resp{200, `{"results":[]}`})
	if id, err := c2.ResolveSpaceID("NOPE"); err != nil || id != "" {
		t.Fatalf("ResolveSpaceID(missing) = %q, %v; want empty", id, err)
	}
}

func TestSearchPagesByTitle(t *testing.T) {
	c, _ := newServer(t, resp{200, `{"results":[{"id":"1","title":"T"}]}`})
	pages, err := c.SearchPagesByTitle("T", "")
	if err != nil || len(pages) != 1 || pages[0].ID != "1" {
		t.Fatalf("SearchPagesByTitle = %v, %v", pages, err)
	}
}

// --- attachments -------------------------------------------------------------

func writeTempImage(t *testing.T) (path, sum string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "x.png")
	if err := os.WriteFile(path, []byte("image-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := fileChecksum(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, sum
}

func TestSyncAttachmentsCreatesWhenAbsent(t *testing.T) {
	path, _ := writeTempImage(t)
	c, s := newServer(t, resp{200, `{"results":[]}`}, resp{200, `{}`})
	actions, err := c.SyncAttachments("1", []LocalAttachment{{Path: path, Filename: "x.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Action != "created" {
		t.Fatalf("actions = %v, want [created]", actions)
	}
	if !eqStrings(s.calls, []string{"GET", "POST"}) {
		t.Errorf("calls = %v, want [GET POST]", s.calls)
	}
}

func TestSyncAttachmentsSkipsWhenChecksumMatches(t *testing.T) {
	path, sum := writeTempImage(t)
	list := `{"results":[{"id":"a1","title":"x.png","metadata":{"comment":"` +
		attachmentChecksumPrefix + sum + `"}}]}`
	c, s := newServer(t, resp{200, list})
	actions, err := c.SyncAttachments("1", []LocalAttachment{{Path: path, Filename: "x.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Action != "skipped" {
		t.Fatalf("actions = %v, want [skipped]", actions)
	}
	if !eqStrings(s.calls, []string{"GET"}) {
		t.Errorf("calls = %v, want [GET] (no upload)", s.calls)
	}
}

func TestSyncAttachmentsUpdatesWhenChecksumDiffers(t *testing.T) {
	path, _ := writeTempImage(t)
	list := `{"results":[{"id":"a1","title":"x.png","metadata":{"comment":"` +
		attachmentChecksumPrefix + `stale"}}]}`
	c, s := newServer(t, resp{200, list}, resp{200, `{}`})
	actions, err := c.SyncAttachments("1", []LocalAttachment{{Path: path, Filename: "x.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Action != "updated" {
		t.Fatalf("actions = %v, want [updated]", actions)
	}
	if !eqStrings(s.calls, []string{"GET", "POST"}) {
		t.Errorf("calls = %v, want [GET POST]", s.calls)
	}
}

// --- misc --------------------------------------------------------------------

func TestSplitAuth(t *testing.T) {
	u, tok, ok := SplitAuth("alice@example.net:secret:with:colons")
	if !ok || u != "alice@example.net" || tok != "secret:with:colons" {
		t.Errorf("SplitAuth = %q, %q, %v", u, tok, ok)
	}
	if _, _, ok := SplitAuth("no-colon"); ok {
		t.Error("SplitAuth(no-colon) ok = true, want false")
	}
}
