package client

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	sleep = func(time.Duration) {} // don't actually sleep between retries in tests
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

// --- retry / backoff ---------------------------------------------------------

// countingServer returns the responses from handler in order (by request count,
// 1-based) and records how many requests it received.
func countingServer(t *testing.T, handler func(w http.ResponseWriter, n int32)) (*ConfluenceClient, *int32) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handler(w, atomic.AddInt32(&n, 1))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "u", "t"), &n
}

func TestSendRetriesOn429ThenSucceeds(t *testing.T) {
	c, n := countingServer(t, func(w http.ResponseWriter, req int32) {
		if req == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1"}`))
	})
	if _, err := c.GetPage("1"); err != nil {
		t.Fatalf("GetPage after 429 retry: %v", err)
	}
	if got := atomic.LoadInt32(n); got != 2 {
		t.Errorf("requests = %d, want 2 (one retry)", got)
	}
}

func TestSendRetries503ForGet(t *testing.T) {
	c, n := countingServer(t, func(w http.ResponseWriter, req int32) {
		if req <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1"}`))
	})
	if _, err := c.GetPage("1"); err != nil {
		t.Fatalf("GetPage after 503 retries: %v", err)
	}
	if got := atomic.LoadInt32(n); got != 3 {
		t.Errorf("requests = %d, want 3", got)
	}
}

func TestSendDoesNotRetryPostOn503(t *testing.T) {
	c, n := countingServer(t, func(w http.ResponseWriter, _ int32) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if _, err := c.CreatePage("S", "T", "<p/>", ""); err == nil {
		t.Fatal("CreatePage: want error on 503")
	}
	if got := atomic.LoadInt32(n); got != 1 {
		t.Errorf("requests = %d, want 1 (POST not retried on 503)", got)
	}
}

func TestSendRetriesPostOn429(t *testing.T) {
	c, n := countingServer(t, func(w http.ResponseWriter, req int32) {
		if req == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"id":"9"}`))
	})
	if _, err := c.CreatePage("S", "T", "<p/>", ""); err != nil {
		t.Fatalf("CreatePage after 429 retry: %v", err)
	}
	if got := atomic.LoadInt32(n); got != 2 {
		t.Errorf("requests = %d, want 2 (POST retried on 429)", got)
	}
}

func TestSendExhaustsRetries(t *testing.T) {
	c, n := countingServer(t, func(w http.ResponseWriter, _ int32) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := c.GetPage("1"); err == nil {
		t.Fatal("GetPage: want error after exhausting retries")
	}
	if got := atomic.LoadInt32(n); got != maxRetries+1 {
		t.Errorf("requests = %d, want %d (initial + %d retries)", got, maxRetries+1, maxRetries)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("5"); d != 5*time.Second {
		t.Errorf("parseRetryAfter(5) = %v, want 5s", d)
	}
	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("parseRetryAfter(empty) = %v, want 0", d)
	}
	if d := parseRetryAfter("garbage"); d != 0 {
		t.Errorf("parseRetryAfter(garbage) = %v, want 0", d)
	}
}

func TestBackoffCapsAndHonorsRetryAfter(t *testing.T) {
	if d := backoff(0, 0); d != baseBackoff {
		t.Errorf("backoff(0,0) = %v, want %v", d, baseBackoff)
	}
	if d := backoff(1, 0); d != 2*baseBackoff {
		t.Errorf("backoff(1,0) = %v, want %v", d, 2*baseBackoff)
	}
	if d := backoff(100, 0); d != maxBackoff {
		t.Errorf("backoff(100,0) = %v, want cap %v", d, maxBackoff)
	}
	if d := backoff(0, 3*time.Second); d != 3*time.Second {
		t.Errorf("backoff with Retry-After = %v, want 3s", d)
	}
	if d := backoff(0, time.Hour); d != maxBackoff {
		t.Errorf("backoff with huge Retry-After = %v, want cap %v", d, maxBackoff)
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

func TestGetPageBodyOrNilReturnsStorageBody(t *testing.T) {
	c, s := newServer(t, resp{200,
		`{"id":"1","title":"T","body":{"storage":{"value":"<p>hi</p>","representation":"storage"}}}`})
	p, err := c.GetPageBodyOrNil("1")
	if err != nil || p == nil {
		t.Fatalf("GetPageBodyOrNil = %v, %v; want a page", p, err)
	}
	if p.Body.Storage.Value != "<p>hi</p>" {
		t.Errorf("body = %q, want <p>hi</p>", p.Body.Storage.Value)
	}
	if !eqStrings(s.calls, []string{"GET"}) {
		t.Errorf("calls = %v, want [GET]", s.calls)
	}
}

func TestGetPageBodyOrNilEmptyBody(t *testing.T) {
	c, _ := newServer(t, resp{200, `{"id":"1","title":"Folder"}`})
	p, err := c.GetPageBodyOrNil("1")
	if err != nil || p == nil {
		t.Fatalf("GetPageBodyOrNil = %v, %v; want a page", p, err)
	}
	if p.Body.Storage.Value != "" {
		t.Errorf("body = %q, want empty", p.Body.Storage.Value)
	}
}

func TestGetPageBodyOrNilReturnsNilOn404(t *testing.T) {
	c, _ := newServer(t, resp{404, `{"errors":[]}`})
	p, err := c.GetPageBodyOrNil("nope")
	if err != nil || p != nil {
		t.Fatalf("GetPageBodyOrNil = %v, %v; want nil, nil", p, err)
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

func TestPlanAttachmentsClassifiesWithoutUploading(t *testing.T) {
	path, sum := writeTempImage(t)
	// same.png matches (skip), stale.png differs (update), new.png is absent (create).
	list := `{"results":[` +
		`{"id":"a1","title":"same.png","metadata":{"comment":"` + attachmentChecksumPrefix + sum + `"}},` +
		`{"id":"a2","title":"stale.png","metadata":{"comment":"` + attachmentChecksumPrefix + `stale"}}` +
		`]}`
	c, s := newServer(t, resp{200, list})
	actions, err := c.PlanAttachments("1", []LocalAttachment{
		{Path: path, Filename: "same.png"},
		{Path: path, Filename: "stale.png"},
		{Path: path, Filename: "new.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []SyncAction{{"same.png", "skipped"}, {"stale.png", "updated"}, {"new.png", "created"}}
	if len(actions) != len(want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Errorf("actions[%d] = %v, want %v", i, actions[i], want[i])
		}
	}
	// A plan reads only; it must never upload.
	if !eqStrings(s.calls, []string{"GET"}) {
		t.Errorf("calls = %v, want [GET] (no uploads)", s.calls)
	}
}

// --- misc --------------------------------------------------------------------

func TestLoadDotenv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# a comment\n" +
		"CONFLUENCE_URL=https://file.example.net\n" +
		"export CONFLUENCE_USERNAME=\"me@example.net\"\n" +
		"CONFLUENCE_TOKEN='tok#en'\n" +
		"\n" +
		"NOEQUALS\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := loadDotenv(path)
	if err != nil {
		t.Fatalf("loadDotenv: %v", err)
	}
	if env["CONFLUENCE_URL"] != "https://file.example.net" {
		t.Errorf("URL = %q", env["CONFLUENCE_URL"])
	}
	if env["CONFLUENCE_USERNAME"] != "me@example.net" { // export prefix + quotes stripped
		t.Errorf("USERNAME = %q", env["CONFLUENCE_USERNAME"])
	}
	if env["CONFLUENCE_TOKEN"] != "tok#en" { // single quotes stripped, # kept
		t.Errorf("TOKEN = %q", env["CONFLUENCE_TOKEN"])
	}
	if _, ok := env["NOEQUALS"]; ok {
		t.Error("line without '=' should be skipped")
	}

	if got, err := loadDotenv(filepath.Join(dir, "absent")); err == nil || len(got) != 0 {
		t.Errorf("missing file = %v, %v; want empty + error", got, err)
	}
}

func TestResolveValuePrecedence(t *testing.T) {
	dotenv := map[string]string{"K": "from-dotenv"}
	t.Setenv("K", "from-env")
	if got := resolveValue("from-flag", "K", dotenv); got != "from-flag" {
		t.Errorf("flag should win, got %q", got)
	}
	if got := resolveValue("", "K", dotenv); got != "from-env" {
		t.Errorf("env should beat .env, got %q", got)
	}
	t.Setenv("K", "")
	if got := resolveValue("", "K", dotenv); got != "from-dotenv" {
		t.Errorf(".env should be the fallback, got %q", got)
	}
}

func TestResolve(t *testing.T) {
	// Work in a temp dir so Resolve reads our .env, not the repo's.
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(".env", []byte(
		"CONFLUENCE_URL=https://file.example.net\n"+
			"CONFLUENCE_USERNAME=file-user\n"+
			"CONFLUENCE_TOKEN=file-pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Clear any inherited env for a deterministic baseline.
	t.Setenv("CONFLUENCE_URL", "")
	t.Setenv("CONFLUENCE_USERNAME", "")
	t.Setenv("CONFLUENCE_TOKEN", "")

	// All from .env.
	c, err := Resolve("", "", "")
	if err != nil || c.BaseURL() != "https://file.example.net" {
		t.Fatalf("Resolve(.env) = %v, %v", c, err)
	}

	// Flag beats env beats .env for the URL.
	t.Setenv("CONFLUENCE_URL", "https://env.example.net")
	if c, _ := Resolve("https://flag.example.net", "", ""); c.BaseURL() != "https://flag.example.net" {
		t.Errorf("flag should win, got %q", c.BaseURL())
	}
	if c, _ := Resolve("", "", ""); c.BaseURL() != "https://env.example.net" {
		t.Errorf("env should beat .env, got %q", c.BaseURL())
	}

	// Missing token (no flag for it) is an error.
	t.Setenv("CONFLUENCE_TOKEN", "")
	if err := os.WriteFile(".env", []byte("CONFLUENCE_URL=u\nCONFLUENCE_USERNAME=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("u", "x", ""); err == nil {
		t.Error("Resolve with no token: want error")
	}
}
