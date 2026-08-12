package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
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
	bodies    []string
	ctypes    []string
	idx       int
}

// lastBody returns the body of the most recent request, for assertions about
// what was actually sent.
func (s *scripted) lastBody() string {
	if len(s.bodies) == 0 {
		return ""
	}
	return s.bodies[len(s.bodies)-1]
}

// lastContentType returns the Content-Type of the most recent request. An upload
// needs it to reparse the body: the multipart boundary lives there.
func (s *scripted) lastContentType() string {
	if len(s.ctypes) == 0 {
		return ""
	}
	return s.ctypes[len(s.ctypes)-1]
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
		body, _ := io.ReadAll(r.Body)
		s.bodies = append(s.bodies, string(body))
		s.ctypes = append(s.ctypes, r.Header.Get("Content-Type"))
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
	return New(Config{SiteURL: srv.URL, Username: "u", Token: "t"}), s
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
	return New(Config{SiteURL: srv.URL, Username: "u", Token: "t"}), &n
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

// A legacy comment still identifies an unchanged file, so a format change does
// not force a re-upload of every attachment.
func TestSyncAttachmentsSkipsWhenLegacyChecksumMatches(t *testing.T) {
	path, sum := writeTempImage(t)
	list := `{"results":[{"id":"a1","title":"x.png","metadata":{"comment":"` +
		legacyChecksumPrefix + sum + `"}}]}`
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
		legacyChecksumPrefix + `stale"}}]}`
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
		`{"id":"a1","title":"same.png","metadata":{"comment":"` + legacyChecksumPrefix + sum + `"}},` +
		`{"id":"a2","title":"stale.png","metadata":{"comment":"` + legacyChecksumPrefix + `stale"}}` +
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

// TestListAttachmentsDecodesMetadata pins the fields against a payload shaped
// like a real one (verified against Cloud): fileSize and mediaType live under
// extensions, and the download link is an API path, not /download/attachments.
func TestListAttachmentsDecodesMetadata(t *testing.T) {
	list := `{"results":[{` +
		`"id":"att99","title":"assets%2Fx.png",` +
		`"metadata":{"comment":"` + attachmentCommentPrefix + `sha256=abc path=assets/x.png"},` +
		`"version":{"number":3,"when":"2026-08-05T22:17:28.040Z"},` +
		`"extensions":{"mediaType":"image/png","fileSize":171},` +
		`"_links":{"download":"/rest/api/content/1/child/attachment/att99/download"}` +
		`}]}`
	c, _ := newServer(t, resp{200, list})
	got, err := c.ListAttachments("1")
	if err != nil || len(got) != 1 {
		t.Fatalf("ListAttachments = %v, %v", got, err)
	}
	a := got[0]
	if a.Extensions.FileSize != 171 || a.Extensions.MediaType != "image/png" {
		t.Errorf("extensions = %+v, want 171/image/png", a.Extensions)
	}
	if a.Version.Number != 3 {
		t.Errorf("version.number = %d, want 3", a.Version.Number)
	}
	if a.Links.Download != "/rest/api/content/1/child/attachment/att99/download" {
		t.Errorf("download = %q", a.Links.Download)
	}
	if m := a.Meta(); !m.Managed || m.Source != "assets/x.png" || m.SHA256 != "abc" {
		t.Errorf("meta = %+v", m)
	}
}

// TestListAttachmentsPaginates covers the offset loop: a full first page means
// there may be more, and a short page ends it. Without this, a page with more
// than attachmentPageSize attachments is silently truncated.
func TestListAttachmentsPaginates(t *testing.T) {
	var full strings.Builder
	full.WriteString(`{"results":[`)
	for i := range attachmentPageSize {
		if i > 0 {
			full.WriteByte(',')
		}
		fmt.Fprintf(&full, `{"id":"a%d","title":"f%d.png"}`, i, i)
	}
	full.WriteString(`]}`)

	c, s := newServer(t,
		resp{200, full.String()},
		resp{200, `{"results":[{"id":"last","title":"last.png"}]}`},
	)
	got, err := c.ListAttachments("1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != attachmentPageSize+1 {
		t.Fatalf("len = %d, want %d", len(got), attachmentPageSize+1)
	}
	if got[len(got)-1].Title != "last.png" {
		t.Errorf("last title = %q, want last.png", got[len(got)-1].Title)
	}
	if len(s.calls) != 2 {
		t.Errorf("calls = %v, want 2 requests", s.calls)
	}
}

// TestListAttachmentsStopsOnShortFirstPage guards the common case: one request,
// not a speculative second.
func TestListAttachmentsStopsOnShortFirstPage(t *testing.T) {
	c, s := newServer(t, resp{200, `{"results":[{"id":"a1","title":"x.png"}]}`})
	if _, err := c.ListAttachments("1"); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 1 {
		t.Errorf("calls = %v, want a single request", s.calls)
	}
}

func TestDownloadAttachmentWritesBytes(t *testing.T) {
	c, _ := newServer(t, resp{200, "PNGBYTES"})
	var att Attachment
	att.Links.Download = "/rest/api/content/1/child/attachment/att1/download"
	var buf bytes.Buffer
	if err := c.DownloadAttachment(att, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "PNGBYTES" {
		t.Errorf("body = %q, want PNGBYTES", buf.String())
	}
}

func TestDownloadAttachmentRequiresLink(t *testing.T) {
	c, _ := newServer(t)
	if err := c.DownloadAttachment(Attachment{Title: "x.png"}, io.Discard); err == nil {
		t.Fatal("want an error when the attachment has no download link")
	}
}

func TestDownloadAttachmentPropagatesHTTPError(t *testing.T) {
	c, _ := newServer(t, resp{404, "gone"})
	var att Attachment
	att.Links.Download = "/rest/api/content/1/child/attachment/att1/download"
	err := c.DownloadAttachment(att, io.Discard)
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 404 {
		t.Fatalf("err = %v, want *HTTPError 404", err)
	}
}

// TestDownloadAttachmentDoesNotLeakCredentialsOnRedirect is the security
// assertion behind reusing send: the real endpoint 302s to Atlassian's media
// host, which carries its own token in the query string and must never receive
// the site credentials. Go's default policy drops Authorization on a cross-host
// hop -- this fails if anyone adds a CheckRedirect that forwards headers.
//
// The media server is addressed as "localhost" while the origin is
// "127.0.0.1". Both resolve to the same listener, but Go compares hostnames
// with the port stripped, so two httptest servers would otherwise look like the
// same host and the header would be forwarded -- making the test pass
// vacuously.
func TestDownloadAttachmentDoesNotLeakCredentialsOnRedirect(t *testing.T) {
	var mediaAuth string
	var reached bool
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		mediaAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("MEDIABYTES"))
	}))
	t.Cleanup(media.Close)
	mediaURL := strings.Replace(media.URL, "127.0.0.1", "localhost", 1)
	if mediaURL == media.URL {
		t.Fatalf("expected a 127.0.0.1 test URL to rewrite, got %q", media.URL)
	}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("origin request lost its Authorization header")
		}
		http.Redirect(w, r, mediaURL+"/file/abc/binary?token=xyz", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	c := New(Config{SiteURL: origin.URL, Username: "u", Token: "secret"})
	var att Attachment
	att.Links.Download = "/rest/api/content/1/child/attachment/att1/download"
	var buf bytes.Buffer
	if err := c.DownloadAttachment(att, &buf); err != nil {
		t.Fatal(err)
	}
	if !reached {
		t.Fatal("redirect was not followed")
	}
	if buf.String() != "MEDIABYTES" {
		t.Errorf("body = %q, want the redirected bytes", buf.String())
	}
	if mediaAuth != "" {
		t.Errorf("Authorization leaked to the media host: %q", mediaAuth)
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
	// Clear any inherited env for a deterministic baseline. CONFLUENCE_CLOUD_ID
	// matters here too: a stray one would reroute BaseURL to the gateway.
	t.Setenv("CONFLUENCE_URL", "")
	t.Setenv("CONFLUENCE_USERNAME", "")
	t.Setenv("CONFLUENCE_TOKEN", "")
	t.Setenv("CONFLUENCE_CLOUD_ID", "")

	// All from .env.
	c, err := Resolve(Options{})
	if err != nil || c.BaseURL() != "https://file.example.net" {
		t.Fatalf("Resolve(.env) = %v, %v", c, err)
	}

	// Flag beats env beats .env for the URL.
	t.Setenv("CONFLUENCE_URL", "https://env.example.net")
	if c, _ := Resolve(Options{URL: "https://flag.example.net"}); c.BaseURL() != "https://flag.example.net" {
		t.Errorf("flag should win, got %q", c.BaseURL())
	}
	if c, _ := Resolve(Options{}); c.BaseURL() != "https://env.example.net" {
		t.Errorf("env should beat .env, got %q", c.BaseURL())
	}

	// Missing token (no flag for it) is an error.
	t.Setenv("CONFLUENCE_TOKEN", "")
	if err := os.WriteFile(".env", []byte("CONFLUENCE_URL=u\nCONFLUENCE_USERNAME=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(Options{URL: "u", Username: "x"}); err == nil {
		t.Error("Resolve with no token: want error")
	}
}

func TestNewGatewayBase(t *testing.T) {
	tests := []struct {
		name               string
		site, cloudID      string
		wantBase, wantSite string
	}{
		{
			"no cloud ID keeps the site as the request base",
			"https://wiki.example.net", "",
			"https://wiki.example.net", "https://wiki.example.net",
		},
		{
			"cloud ID routes requests to the gateway, site unchanged",
			"https://wiki.example.net", "abc-123",
			gatewayPrefix + "abc-123", "https://wiki.example.net",
		},
		{
			"trailing slash trimmed from both",
			"https://wiki.example.net/", "abc-123",
			gatewayPrefix + "abc-123", "https://wiki.example.net",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := New(Config{SiteURL: tc.site, CloudID: tc.cloudID})
			if c.BaseURL() != tc.wantBase {
				t.Errorf("BaseURL = %q, want %q", c.BaseURL(), tc.wantBase)
			}
			if c.SiteURL() != tc.wantSite {
				t.Errorf("SiteURL = %q, want %q", c.SiteURL(), tc.wantSite)
			}
		})
	}
}

func TestResolveNext(t *testing.T) {
	const gw = gatewayPrefix + "abc-123"
	tests := []struct {
		name, base, next, want string
	}{
		{"no next page", gw, "", ""},
		{
			"site-relative path appends, preserving the gateway prefix",
			gw, "/wiki/api/v2/pages/1/properties?cursor=X",
			gw + "/wiki/api/v2/pages/1/properties?cursor=X",
		},
		{
			"prefix already applied is not doubled",
			gw, "/ex/confluence/abc-123/wiki/api/v2/pages/1/properties?cursor=X",
			gw + "/wiki/api/v2/pages/1/properties?cursor=X",
		},
		{
			"absolute next is used as-is",
			gw, "https://elsewhere.example.net/wiki/api/v2/pages?cursor=X",
			"https://elsewhere.example.net/wiki/api/v2/pages?cursor=X",
		},
		{
			"site base (no cloud ID) appends as before",
			"https://wiki.example.net", "/wiki/api/v2/pages/1/properties?cursor=X",
			"https://wiki.example.net/wiki/api/v2/pages/1/properties?cursor=X",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveNext(tc.base, tc.next); got != tc.want {
				t.Errorf("resolveNext(%q, %q) = %q, want %q", tc.base, tc.next, got, tc.want)
			}
		})
	}
}

// TestAttachmentCommentRecordsSource checks the comment an upload stamps: the
// checksum used to skip unchanged files, plus the path the image was published
// from so a later read recovers it exactly.
func TestAttachmentCommentRecordsSource(t *testing.T) {
	got := attachmentComment("abc123", "../assets/logo.png")
	if want := "markfluence: sha256=abc123 path=../assets/logo.png"; got != want {
		t.Errorf("attachmentComment = %q, want %q", got, want)
	}
	if got := attachmentComment("abc123", ""); got != "markfluence: sha256=abc123" {
		t.Errorf("attachmentComment without a source = %q", got)
	}
}

func TestParseAttachmentComment(t *testing.T) {
	cases := []struct {
		name    string
		comment string
		want    AttachmentMeta
	}{
		{"current form", "markfluence: sha256=abc123 path=assets/x.png",
			AttachmentMeta{SHA256: "abc123", Source: "assets/x.png", Managed: true}},
		{"no source recorded", "markfluence: sha256=abc123",
			AttachmentMeta{SHA256: "abc123", Managed: true}},
		// The path is written last and unquoted, so it may contain spaces.
		{"source with spaces", "markfluence: sha256=abc123 path=my docs/a b.png",
			AttachmentMeta{SHA256: "abc123", Source: "my docs/a b.png", Managed: true}},
		{"legacy form", legacyChecksumPrefix + "abc123",
			AttachmentMeta{SHA256: "abc123", Managed: true}},
		{"hand-uploaded", "a note from a human", AttachmentMeta{}},
		{"empty", "", AttachmentMeta{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseAttachmentComment(c.comment); got != c.want {
				t.Errorf("parseAttachmentComment(%q) = %+v, want %+v", c.comment, got, c.want)
			}
		})
	}
}

// TestSyncAttachmentsStampsSource checks that an upload records the source path,
// so reading the page back can recover the image's original location.
func TestSyncAttachmentsStampsSource(t *testing.T) {
	path, _ := writeTempImage(t)
	c, s := newServer(t, resp{200, `{"results":[]}`}, resp{200, `{}`})
	_, err := c.SyncAttachments("1", []LocalAttachment{
		{Path: path, Filename: "assets%2Fx.png", Source: "assets/x.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.lastBody(), "path=assets/x.png") {
		t.Errorf("upload body did not record the source path:\n%s", s.lastBody())
	}
}

// uploadParts reparses the multipart body of the last request into one entry per
// form field: the part's headers alongside its raw bytes.
func uploadParts(t *testing.T, s *scripted) map[string]struct {
	header textproto.MIMEHeader
	value  string
} {
	t.Helper()
	_, params, err := mime.ParseMediaType(s.lastContentType())
	if err != nil {
		t.Fatalf("upload Content-Type %q: %v", s.lastContentType(), err)
	}
	out := map[string]struct {
		header textproto.MIMEHeader
		value  string
	}{}
	mr := multipart.NewReader(strings.NewReader(s.lastBody()), params["boundary"])
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("reading upload parts: %v", err)
		}
		b, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("reading part %q: %v", p.FormName(), err)
		}
		out[p.FormName()] = struct {
			header textproto.MIMEHeader
			value  string
		}{p.Header, string(b)}
	}
	return out
}

// TestSyncAttachmentsLabelsTextPartsUTF8 covers the charset label on the upload
// form. A multipart text part with no Content-Type is decoded as ISO-8859-1 by
// Confluence's servlet stack, which double-encodes every non-ASCII byte of the
// recorded path -- so `read` recovers a filename that does not exist. The bytes
// are UTF-8 either way; what has to survive is the label saying so.
func TestSyncAttachmentsLabelsTextPartsUTF8(t *testing.T) {
	path, sum := writeTempImage(t)
	source := "assets/probe-café.png"
	c, s := newServer(t, resp{200, `{"results":[]}`}, resp{200, `{}`})
	_, err := c.SyncAttachments("1", []LocalAttachment{
		{Path: path, Filename: "assets%2Fprobe-café.png", Source: source},
	})
	if err != nil {
		t.Fatal(err)
	}

	parts := uploadParts(t, s)
	if got := parts["_charset_"].value; got != "UTF-8" {
		t.Errorf("_charset_ field = %q, want UTF-8", got)
	}
	for _, name := range []string{"_charset_", "comment", "minorEdit"} {
		got := parts[name].header.Get("Content-Type")
		if _, params, err := mime.ParseMediaType(got); err != nil || params["charset"] != "UTF-8" {
			t.Errorf("%s part Content-Type = %q, want a UTF-8 charset", name, got)
		}
	}
	if want := attachmentComment(sum, source); parts["comment"].value != want {
		t.Errorf("comment part = %q, want %q", parts["comment"].value, want)
	}
	// The name rides in Content-Disposition, which was never affected; check it
	// stays literal UTF-8 rather than being escaped into \u form.
	if got := parts["file"].header.Get("Content-Disposition"); !strings.Contains(got, "probe-café.png") {
		t.Errorf("file part Content-Disposition = %q, want a literal UTF-8 filename", got)
	}
}

// TestSyncAttachmentsRestampsMangledSource covers the repair path: a recorded
// path that disagrees with the local source is an update even when the checksum
// says the bytes are unchanged, since a skip would leave the wrong path stored
// until the file happened to change.
func TestSyncAttachmentsRestampsMangledSource(t *testing.T) {
	path, sum := writeTempImage(t)
	// A comment stored double-encoded: "é" recorded as "Ã©".
	list := `{"results":[{"id":"a1","title":"assets%2Fprobe-café.png","metadata":{"comment":"` +
		attachmentComment(sum, "assets/probe-cafÃ©.png") + `"}}]}`
	c, s := newServer(t, resp{200, list}, resp{200, `{}`})
	actions, err := c.SyncAttachments("1", []LocalAttachment{
		{Path: path, Filename: "assets%2Fprobe-café.png", Source: "assets/probe-café.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Action != "updated" {
		t.Fatalf("actions = %v, want [updated]", actions)
	}
	if got := uploadParts(t, s)["comment"].value; got != attachmentComment(sum, "assets/probe-café.png") {
		t.Errorf("restamped comment = %q", got)
	}
}

// TestSyncAttachmentsSkipsLegacyCommentWithNoSource pins the limit of that
// restamping: a legacy comment records no path at all, which is not a
// disagreement. Treating it as one would re-upload every attachment stamped by
// an older markfluence -- the churn the checksum comparison exists to avoid.
func TestSyncAttachmentsSkipsLegacyCommentWithNoSource(t *testing.T) {
	path, sum := writeTempImage(t)
	list := `{"results":[{"id":"a1","title":"x.png","metadata":{"comment":"` +
		legacyChecksumPrefix + sum + `"}}]}`
	c, s := newServer(t, resp{200, list})
	actions, err := c.SyncAttachments("1", []LocalAttachment{
		{Path: path, Filename: "x.png", Source: "assets/x.png"},
	})
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

// TestSyncAttachmentsSkipsWhenCurrentChecksumMatches is the new-format twin of the
// legacy skip test.
func TestSyncAttachmentsSkipsWhenCurrentChecksumMatches(t *testing.T) {
	path, sum := writeTempImage(t)
	list := `{"results":[{"id":"a1","title":"x.png","metadata":{"comment":"` +
		attachmentComment(sum, "x.png") + `"}}]}`
	c, s := newServer(t, resp{200, list})
	actions, err := c.SyncAttachments("1", []LocalAttachment{
		{Path: path, Filename: "x.png", Source: "x.png"},
	})
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
