package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/project"
)

func TestResolveTitlePageID(t *testing.T) {
	// The flags are gone (#139): update consumes metadata and has no way to
	// invent any, so this reads whatever pagemeta resolved -- from the file's
	// frontmatter or from its pages: entry, indistinguishably by design.
	tests := map[string]struct {
		fields                map[string]string
		wantTitle, wantPageID string
		wantPresent           bool
	}{
		"both present": {
			map[string]string{"title": "T", "page_id": "111"}, "T", "111", true},
		"page id only": {
			map[string]string{"page_id": "111"}, "", "111", false},
		"title present but empty": {
			map[string]string{"title": "", "page_id": "111"}, "", "111", true},
		"whitespace title is empty but present": {
			map[string]string{"title": "   ", "page_id": "111"}, "", "111", true},
		"nothing": {map[string]string{}, "", "", false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			title, present, pageID := resolveTitlePageID(tc.fields)
			if title != tc.wantTitle || pageID != tc.wantPageID || present != tc.wantPresent {
				t.Errorf("= %q/%v/%q, want %q/%v/%q",
					title, present, pageID, tc.wantTitle, tc.wantPresent, tc.wantPageID)
			}
		})
	}
}

// The three page-metadata flags are gone, and their absence is pinned rather
// than incidental: re-adding one would put back the single-FILE-only shape that
// made docs/**/*.md inexpressible from CI, which is the whole reason #139
// exists.
func TestPageMetadataFlagsAreGone(t *testing.T) {
	for _, name := range []string{"title", "page-id", "page-width"} {
		if f := Cmd.Flags().Lookup(name); f != nil {
			t.Errorf("--%s exists; page metadata belongs in the file or its entry, not in a flag", name)
		}
	}
	// The invocation flags stay: they describe the run, not the page.
	for _, name := range []string{"message", "force", "dry-run"} {
		if f := Cmd.Flags().Lookup(name); f == nil {
			t.Errorf("--%s is missing; it describes the run and should have stayed", name)
		}
	}
}

func TestResolveWidth(t *testing.T) {
	withWidth := map[string]string{"title": "T", "page_width": "wide"}
	noWidth := map[string]string{"title": "T"}

	// A project file declaring a width, and one declaring nothing.
	declared := &project.Root{File: "/repo/markfluence.yaml", Config: project.Config{PageWidth: "narrow"}}
	bare := &project.Root{File: "/repo/markfluence.yaml"}

	t.Run("the file's own width", func(t *testing.T) {
		w, apply, err := resolveWidth(withWidth, bare)
		if err != nil || !apply || w != pagewidth.Wide {
			t.Fatalf("= %q/%v/%v, want wide/true/nil", w, apply, err)
		}
	})
	t.Run("no width anywhere means no width request", func(t *testing.T) {
		if _, apply, err := resolveWidth(noWidth, bare); err != nil || apply {
			t.Fatalf("= apply %v err %v, want false/nil", apply, err)
		}
		if _, apply, err := resolveWidth(map[string]string{}, bare); err != nil || apply {
			t.Fatalf("(no fields) = apply %v err %v, want false/nil", apply, err)
		}
	})
	t.Run("an invalid width errors", func(t *testing.T) {
		bad := map[string]string{"page_width": "huge"}
		if _, apply, err := resolveWidth(bad, bare); err == nil || apply {
			t.Fatalf("= apply %v err %v, want false/error", apply, err)
		}
	})

	// The behavior change #100 made: a project-wide page_width makes update
	// assert a width on a file that declares none, where before that file's
	// live width was left alone. It is what "declared means asserted" (L9)
	// means one level up.
	t.Run("project file makes update assert a width", func(t *testing.T) {
		w, apply, err := resolveWidth(noWidth, declared)
		if err != nil || !apply || w != pagewidth.Narrow {
			t.Fatalf("= %q/%v/%v, want narrow/true/nil", w, apply, err)
		}
	})
	t.Run("the file beats the project file", func(t *testing.T) {
		w, apply, err := resolveWidth(withWidth, declared)
		if err != nil || !apply || w != pagewidth.Wide {
			t.Fatalf("= %q/%v/%v, want wide/true/nil", w, apply, err)
		}
	})
	// The escape hatch has to keep working: a project that omits the key gets
	// no width request at all.
	t.Run("no project width means no width request", func(t *testing.T) {
		if _, apply, err := resolveWidth(noWidth, declaredNothing()); err != nil || apply {
			t.Fatalf("= apply %v err %v, want false/nil", apply, err)
		}
	})
	t.Run("invalid project width names the project file", func(t *testing.T) {
		bad := &project.Root{File: "/repo/markfluence.yaml", Config: project.Config{PageWidth: "huge"}}
		_, apply, err := resolveWidth(noWidth, bad)
		if err == nil || apply {
			t.Fatalf("= apply %v err %v, want false/error", apply, err)
		}
		if !strings.Contains(err.Error(), "/repo/markfluence.yaml") {
			t.Errorf("error = %q, want it to name the project file", err)
		}
	})
	t.Run("nil root is not a panic", func(t *testing.T) {
		if _, apply, err := resolveWidth(noWidth, nil); err != nil || apply {
			t.Fatalf("= apply %v err %v, want false/nil", apply, err)
		}
	})
}

// declaredNothing is a project file with no settings -- the marker that ships.
func declaredNothing() *project.Root {
	return &project.Root{File: "/repo/markfluence.yaml"}
}

func TestRootErrorCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, project.Filename)
	if err := os.WriteFile(path, []byte("spce: ENG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := project.Discover(dir)
	if err == nil {
		t.Fatal("want an error")
	}
	if got := rootErrorCode(err); got != jsonout.CodeValidation {
		t.Errorf("code = %q, want VALIDATION: a malformed project file is a local defect, not I/O", got)
	}
	// Anything else really is a failure to resolve the root.
	if got := rootErrorCode(os.ErrPermission); got != jsonout.CodeIO {
		t.Errorf("code = %q, want IO", got)
	}
	// RootError leaves a ConfigError as itself -- the root was found, and it is
	// the file in it that is wrong.
	if msg := project.RootError(err).Error(); strings.Contains(msg, "resolving the documentation root") {
		t.Errorf("error = %q, want no root-resolution heading", msg)
	}
}

// TestProcessFileRejectsNonNumericPageID covers the local half of the fix: a
// page_id that is not an id never reaches the API, so the reader gets a sentence
// instead of a 400 body. The client points at a host that does not resolve, so a
// request would fail loudly rather than pass.
func TestProcessFileRejectsNonNumericPageID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte("---\ntitle: T\npage_id: TODO\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	c := client.New(client.Config{SiteURL: "https://wiki.example.net"})
	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if r.ok {
		t.Fatal("a non-numeric page_id must fail the file")
	}
	if !strings.Contains(r.errMsg, `"TODO"`) || !strings.Contains(r.errMsg, "not a numeric page id") {
		t.Errorf("errMsg = %q, want the not-numeric sentence", r.errMsg)
	}
	if r.code != jsonout.CodeValidation {
		t.Errorf("code = %q, want %q", r.code, jsonout.CodeValidation)
	}
}

// TestProcessFileReportsMissingPage is the issue itself: a page_id the server
// answers 404 for used to surface as "GET https://...: HTTP 404: {...}".
func TestProcessFileReportsMissingPage(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wiki/api/v2/pages/999" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":404,"code":"NOT_FOUND"}]}`))
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte("---\ntitle: T\npage_id: 999\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if r.ok {
		t.Fatal("a page_id that resolves to nothing must fail the file")
	}
	want := "page_id 999 not found (deleted or wrong); " +
		"correct it, or remove it and use create instead"
	if r.errMsg != want {
		t.Errorf("errMsg =\n %q\nwant\n %q", r.errMsg, want)
	}
	// The raw transport error must not leak: no method, URL, or response body.
	for _, unwanted := range []string{"HTTP 404", "GET ", c.SiteURL(), "errors"} {
		if strings.Contains(r.errMsg, unwanted) {
			t.Errorf("errMsg = %q, should not contain %q", r.errMsg, unwanted)
		}
	}
	if r.code != jsonout.CodeNotFound {
		t.Errorf("code = %q, want %q", r.code, jsonout.CodeNotFound)
	}
}

// pageWithVersion builds a minimal page fixture with a given version number and
// createdAt, which is what processFile's mtime-skip check compares the file
// against.
func pageWithVersion(id string, versionNumber int, createdAt string) string {
	return fmt.Sprintf(
		`{"id":%q,"title":"Old Title","version":{"number":%d,"createdAt":%q},`+
			`"_links":{"webui":"/spaces/ENG/pages/%s/Old+Title"}}`,
		id, versionNumber, createdAt, id)
}

func writeUpdateFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestProcessFilePublishesSuccessfully is the full happy path: the file's mtime
// is "now" (just written), which is after the page's 2020 version, so this also
// covers the file-newer-than-page half of the mtime check.
func TestProcessFilePublishesSuccessfully(t *testing.T) {
	var sawPut bool
	var putVersion int
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		case http.MethodPut:
			sawPut = true
			var body struct {
				Version struct {
					Number int `json:"number"`
				} `json:"version"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			putVersion = body.Version.Number
			_, _ = w.Write([]byte(pageWithVersion("1", body.Version.Number, "2026-01-01T00:00:00Z")))
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	})

	path := writeUpdateFixture(t, "---\npage_id: 1\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want ok/published", r)
	}
	if !sawPut {
		t.Fatal("want UpdatePage to have been called")
	}
	if putVersion != 4 {
		t.Errorf("PUT carried version = %d, want 4 (previous 3 + 1)", putVersion)
	}
	if r.versionNew != 4 {
		t.Errorf("r.versionNew = %d, want 4", r.versionNew)
	}
}

// TestProcessFileSkipsWhenFileOlderThanPage is the mtime-skip guarantee itself:
// a file not modified since the page's last version must not republish.
func TestProcessFileSkipsWhenFileOlderThanPage(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s request: a skip must not touch the page further", r.Method)
		}
		_, _ = w.Write([]byte(pageWithVersion("1", 3, "2099-01-01T00:00:00Z")))
	})

	path := writeUpdateFixture(t, "---\npage_id: 1\n---\nHello.\n")
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusSkipped {
		t.Fatalf("result = %+v, want ok/skipped", r)
	}
	if r.versionNew != 3 {
		t.Errorf("r.versionNew = %d, want 3 (unchanged by a skip)", r.versionNew)
	}
}

// TestProcessFileForceBypassesMtimeSkip: --force publishes even when the file
// looks unchanged since the page's last version.
func TestProcessFileForceBypassesMtimeSkip(t *testing.T) {
	var sawPut bool
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2099-01-01T00:00:00Z")))
		case http.MethodPut:
			sawPut = true
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2099-01-01T00:00:00Z")))
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	})

	path := writeUpdateFixture(t, "---\npage_id: 1\n---\nHello.\n")
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	force = true
	t.Cleanup(func() { force = false })

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want ok/published: --force bypasses the mtime skip", r)
	}
	if !sawPut {
		t.Fatal("want UpdatePage to have been called despite the old mtime")
	}
}

// TestResolveTitlePageIDSeparatesAbsentFromEmpty pins the distinction the
// empty-title check rests on. An absent title means the file does not manage
// its page's title, which update honours by keeping the live one; a present but
// empty title is a half-finished edit.
func TestResolveTitlePageIDSeparatesAbsentFromEmpty(t *testing.T) {
	tests := []struct {
		name, content, cliTitle string
		wantTitle               string
		wantPresent             bool
	}{
		{"absent", "---\npage_id: 1\n---\nb\n", "", "", false},
		{"present and empty", "---\ntitle:\npage_id: 1\n---\nb\n", "", "", true},
		{"present and null", "---\ntitle: null\npage_id: 1\n---\nb\n", "", "", true},
		{"present with value", "---\ntitle: T\npage_id: 1\n---\nb\n", "", "T", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mf, err := frontmatter.Parse("f.md", tc.content)
			if err != nil {
				t.Fatal(err)
			}
			title, present, _ := resolveTitlePageID(mf.Frontmatter)
			if title != tc.wantTitle || present != tc.wantPresent {
				t.Errorf("resolveTitlePageID = %q/%v, want %q/%v",
					title, present, tc.wantTitle, tc.wantPresent)
			}
		})
	}
}

// TestProcessFileRejectsEmptyTitle exercises the error path itself, not just
// resolveTitlePageID. The client points at a URL nothing serves: reaching it
// would be the failure, since the check has to fire before any request the way
// the non-numeric page_id check does.
func TestProcessFileRejectsEmptyTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte("---\ntitle:\npage_id: 123\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	c := client.New(client.Config{SiteURL: "https://wiki.invalid"})
	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if r.ok {
		t.Fatal("a present-but-empty title must fail the file")
	}
	if !strings.Contains(r.errMsg, "present but empty") {
		t.Errorf("errMsg = %q, want the empty-title sentence", r.errMsg)
	}
	if r.code != jsonout.CodeValidation {
		t.Errorf("code = %q, want %q", r.code, jsonout.CodeValidation)
	}
}

// TestProcessFileKeepsLiveTitleWhenAbsent is the other half: no title key is a
// legitimate shape, and update takes the page's own title.
func TestProcessFileKeepsLiveTitleWhenAbsent(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/pages/123") && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"id":"123","title":"Live Title","version":{"number":3},` +
				`"_links":{"webui":"/spaces/ENG/pages/123/Live"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte("---\npage_id: 123\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if r.title != "Live Title" {
		t.Errorf("title = %q, want the live page's title", r.title)
	}
}

// --- labels -------------------------------------------------------------------

// labelServer answers a publish plus whatever label traffic the run makes,
// recording every label path it sees so a test can assert on requests that
// were *not* made.
func labelServer(t *testing.T, live string, paths *[]string) *client.ConfluenceClient {
	t.Helper()
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "label") {
			*paths = append(*paths, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(live))
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		case http.MethodPut:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		default:
			t.Errorf("unexpected method: %s %s", r.Method, r.URL.Path)
		}
	})
}

// TestProcessFileAbsentLabelsMakesNoRequest is the test that makes "absent
// means untouched" a property rather than an implementation detail. Not merely
// "no write": no *request*, so there is no path by which a run that never
// mentioned labels can decide to change them.
func TestProcessFileAbsentLabelsMakesNoRequest(t *testing.T) {
	var paths []string
	c := labelServer(t, `{"results":[]}`, &paths)
	path := writeUpdateFixture(t, "---\npage_id: 1\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result = %+v, want ok", r)
	}
	if len(paths) != 0 {
		t.Errorf("label requests = %v, want none for a file with no labels key", paths)
	}
	if r.labels != nil {
		t.Errorf("r.labels = %v, want nil so --json reports null", r.labels)
	}
}

// TestProcessFileAssertsTheDeclaredSet: declared means exact, so a label on the
// page that the file does not list is removed.
func TestProcessFileAssertsTheDeclaredSet(t *testing.T) {
	var paths []string
	live := `{"results":[
		{"id":"1","name":"runbook","prefix":"global"},
		{"id":"2","name":"stale","prefix":"global"}
	]}`
	c := labelServer(t, live, &paths)
	path := writeUpdateFixture(t, "---\npage_id: 1\nlabels: [runbook, howto]\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want ok/published", r)
	}
	got := map[string]string{}
	for _, l := range r.labels {
		got[l.Name] = l.Action
	}
	want := map[string]string{"howto": "added", "stale": "removed", "runbook": "unchanged"}
	for name, action := range want {
		if got[name] != action {
			t.Errorf("labels[%q] = %q, want %q (all: %v)", name, got[name], action, r.labels)
		}
	}
	// The removal must go out as ?name=, never a path segment.
	var sawRemoval bool
	for _, p := range paths {
		if strings.HasPrefix(p, http.MethodDelete) {
			sawRemoval = true
			if !strings.Contains(p, "?name=stale") {
				t.Errorf("removal request = %q, want the ?name= form", p)
			}
		}
	}
	if !sawRemoval {
		t.Error("want the surplus label removed")
	}
}

// TestProcessFileUnmanagedLabelsSurvive: a my: or team: label has no
// frontmatter spelling, so asserting a set that cannot mention it must not
// remove it.
func TestProcessFileUnmanagedLabelsSurvive(t *testing.T) {
	var paths []string
	live := `{"results":[
		{"id":"1","name":"mine","prefix":"my"},
		{"id":"2","name":"eng","prefix":"team"}
	]}`
	c := labelServer(t, live, &paths)
	path := writeUpdateFixture(t, "---\npage_id: 1\nlabels: [runbook]\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result = %+v, want ok", r)
	}
	for _, p := range paths {
		if strings.HasPrefix(p, http.MethodDelete) {
			t.Errorf("removal request %q, want no unmanaged label removed", p)
		}
	}
}

// TestProcessFileEmptyLabelsRemovesThemAll pins the other half of the
// absent/empty distinction: "labels: []" is a declaration, not a no-op.
func TestProcessFileEmptyLabelsRemovesThemAll(t *testing.T) {
	var paths []string
	c := labelServer(t, `{"results":[{"id":"1","name":"stale","prefix":"global"}]}`, &paths)
	path := writeUpdateFixture(t, "---\npage_id: 1\nlabels: []\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result = %+v, want ok", r)
	}
	if r.labels == nil {
		t.Fatal("r.labels = nil, want [] so --json distinguishes it from an absent key")
	}
	if len(r.labels) != 1 || r.labels[0].Action != "removed" {
		t.Errorf("labels = %v, want stale removed", r.labels)
	}
}

// TestProcessFileInvalidLabelFailsBeforeAnyWrite is the ordering that matters:
// a name Confluence would split publishes successfully and cannot then be
// cleaned up, so the run must stop before the body goes out.
func TestProcessFileInvalidLabelFailsBeforeAnyWrite(t *testing.T) {
	var sawWrite bool
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			sawWrite = true
		}
		_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
	})
	path := writeUpdateFixture(t, "---\npage_id: 1\nlabels: [Runbook Two]\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if r.ok {
		t.Fatal("result ok, want a validation failure")
	}
	if r.code != jsonout.CodeValidation {
		t.Errorf("code = %q, want VALIDATION", r.code)
	}
	if sawWrite {
		t.Error("a write went out for a file with an invalid label")
	}
}

// TestProcessFileLabelFailureIsAWarning: the body is published by the time
// labels are applied, so a label failure must not report the publish as failed.
func TestProcessFileLabelFailureIsAWarning(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "label") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		default:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
	path := writeUpdateFixture(t, "---\npage_id: 1\nlabels: [runbook]\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want the publish still reported as ok", r)
	}
	if r.labels != nil {
		t.Errorf("r.labels = %v, want nil rather than a set that was not asserted", r.labels)
	}
	if !strings.Contains(strings.Join(r.warnings, " "), "could not set labels") {
		t.Errorf("warnings = %q, want the label failure reported", r.warnings)
	}
}

// TestProcessFileLabelCaseWarns: lowercasing is a repair, so the run succeeds,
// but silently rewriting an author's label is how a file stays out of step with
// its page forever.
func TestProcessFileLabelCaseWarns(t *testing.T) {
	var paths []string
	c := labelServer(t, `{"results":[]}`, &paths)
	path := writeUpdateFixture(t, "---\npage_id: 1\nlabels: [Runbook]\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result = %+v, want ok", r)
	}
	if !strings.Contains(strings.Join(r.warnings, " "), "not lowercase") {
		t.Errorf("warnings = %q, want the case warning", r.warnings)
	}
	if len(r.labels) != 1 || r.labels[0].Name != "runbook" {
		t.Errorf("labels = %v, want the lowercased name", r.labels)
	}
}

// TestProcessFileSkippedFileSkipsLabels: a file the mtime check skipped is
// skipped entirely, labels included.
func TestProcessFileSkippedFileSkipsLabels(t *testing.T) {
	var paths []string
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "label") {
			paths = append(paths, r.URL.Path)
		}
		_, _ = w.Write([]byte(pageWithVersion("1", 3, "2999-01-01T00:00:00Z")))
	})
	path := writeUpdateFixture(t, "---\npage_id: 1\nlabels: [runbook]\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if r.status != statusSkipped {
		t.Fatalf("status = %q, want skipped", r.status)
	}
	if len(paths) != 0 {
		t.Errorf("label requests = %v, want none for a skipped file", paths)
	}
}

// TestProcessFileWarnsAboutAMentionThatNamesNobody is the end-to-end shape of
// the only signal an author gets. Confluence publishes a bad account id happily
// as "@Unlicensed user", so a typo in a hand-edited profile URL would otherwise
// reach nobody, silently, on every run.
func TestProcessFileWarnsAboutAMentionThatNamesNobody(t *testing.T) {
	const bogus = "712020:00000000-0000-0000-0000-000000000000"
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/user") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No user found with key : null"}`))
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		default:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
	path := writeUpdateFixture(t,
		"---\npage_id: 1\n---\nPing [@Nobody](https://home.atlassian.com/people/"+bogus+") now.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result = %+v, want the page still published", r)
	}
	joined := strings.Join(r.warnings, " ")
	if !strings.Contains(joined, bogus) || !strings.Contains(joined, "Unlicensed user") {
		t.Errorf("warnings = %q, want one naming the id and what a reader will see", r.warnings)
	}
}

// TestProcessFileDoesNotWarnAboutAResolvableMention, and the mention still
// publishes as a mention rather than a link.
func TestProcessFileDoesNotWarnAboutAResolvableMention(t *testing.T) {
	const good = "60c36d0718e9f60071326951"
	var published string
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/user") {
			_, _ = fmt.Fprintf(w, `{"accountId":%q,"displayName":"Ada Lovelace"}`, good)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		default:
			// Decoded, not the raw request bytes: the storage travels as a
			// JSON string, so "<" arrives as \u003c and a raw substring match
			// would fail against a body that is perfectly correct.
			var req struct {
				Body struct {
					Value string `json:"value"`
				} `json:"body"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &req)
			published = req.Body.Value
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
	path := writeUpdateFixture(t,
		"---\npage_id: 1\n---\nPing [@Ada](https://home.atlassian.com/people/"+good+") now.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result = %+v, want ok", r)
	}
	if len(r.warnings) != 0 {
		t.Errorf("warnings = %q, want none", r.warnings)
	}
	if !strings.Contains(published, `<ri:user ri:account-id="`+good+`"`) {
		t.Errorf("published body = %q, want a mention", published)
	}
}

// --- project-wide settings ----------------------------------------------------

// propertyServer answers a publish plus any content-property traffic, recording
// every property path so a test can assert on a request that was *not* made.
// Width lives in two content properties (docs/confluence/page-width.md).
func propertyServer(t *testing.T, paths *[]string) *client.ConfluenceClient {
	t.Helper()
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "properties") {
			*paths = append(*paths, r.Method+" "+r.URL.Path)
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"p1","version":{"number":1}}`))
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		case http.MethodPut:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		default:
			t.Errorf("unexpected method: %s %s", r.Method, r.URL.Path)
		}
	})
}

// writeProject writes a markfluence.yaml and a markdown file under one root,
// returning the file's path.
func writeProject(t *testing.T, projectFile, md string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename), []byte(projectFile), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The behavior change #100 makes to update, asserted on the wire rather than
// through resolveWidth's bool: a project-wide page_width makes update write the
// width for a file that declares none, where before it made no width request.
func TestProcessFileAppliesProjectWidth(t *testing.T) {
	var paths []string
	c := propertyServer(t, &paths)
	path := writeProject(t, "page_width: narrow\n", "---\npage_id: 1\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result not ok: %+v", r)
	}
	if len(paths) == 0 {
		t.Fatal("no content-property requests: the project width was not applied")
	}
	writes := 0
	for _, p := range paths {
		if strings.HasPrefix(p, http.MethodPost) || strings.HasPrefix(p, http.MethodPut) {
			writes++
		}
	}
	// Both the published and draft appearance properties, or the reader and the
	// editor disagree about the width.
	if writes != 2 {
		t.Errorf("property writes = %d (%v), want 2", writes, paths)
	}
}

// The escape hatch, asserted the same way: a project that declares no width
// makes no width *request* at all, which is what keeps "absent means untouched"
// a property rather than an implementation detail.
func TestProcessFileNoProjectWidthMakesNoWidthRequest(t *testing.T) {
	var paths []string
	c := propertyServer(t, &paths)
	path := writeProject(t, "space: ENG\n", "---\npage_id: 1\n---\nHello.\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result not ok: %+v", r)
	}
	if len(paths) != 0 {
		t.Errorf("content-property requests = %v, want none", paths)
	}
}

// A file declaring its own width wins over the project file, and the value on
// the wire is the file's.
func TestProcessFileFrontmatterWidthBeatsProjectWidth(t *testing.T) {
	var bodies []string
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "properties") {
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			buf := make([]byte, 512)
			n, _ := r.Body.Read(buf)
			bodies = append(bodies, string(buf[:n]))
			_, _ = w.Write([]byte(`{"id":"p1","version":{"number":1}}`))
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		default:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
	path := writeProject(t, "page_width: narrow\n",
		"---\npage_id: 1\npage_width: wide\n---\nHello.\n")

	if r := processFile(path, c, project.NewCache(""), linkindex.NewCache(),
		pagedoc.NewUserCache()); !r.ok {
		t.Fatalf("result not ok: %+v", r)
	}
	if len(bodies) == 0 {
		t.Fatal("no width written")
	}
	for _, b := range bodies {
		// wide -> "full-width"; narrow -> "default".
		if !strings.Contains(b, "full-width") {
			t.Errorf("property body = %q, want the file's own width (full-width)", b)
		}
	}
}

// --- manifest metadata --------------------------------------------------------

// writeManifestProject writes a markfluence.yaml and a markdown file under one
// root, returning the file's path.
func writeManifestProject(t *testing.T, projectFile, md string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename), []byte(projectFile), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The point of #139: a pristine file, published from its manifest entry.
func TestProcessFilePublishesFromAManifestEntry(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		default:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
	// No frontmatter at all.
	path := writeManifestProject(t,
		"pages:\n  f.md:\n    title: From The Manifest\n    page_id: 1\n", "# Hello\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want published", r)
	}
	if r.title != "From The Manifest" {
		t.Errorf("title = %q, want the manifest's", r.title)
	}
	if r.metadataSource != "manifest" {
		t.Errorf("metadata_source = %q, want manifest", r.metadataSource)
	}
}

// A file nothing claims is skipped, ok, with no request made -- which is what
// keeps a glob over a docs tree from going red when somebody adds a draft. The
// server fails any request so the skip has to be local.
func TestProcessFileSkipsAnUnmanagedFile(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	path := writeManifestProject(t, "pages:\n  other.md:\n    page_id: 9\n", "# Draft\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusSkipped {
		t.Fatalf("result = %+v, want a successful skip", r)
	}
	if !r.unmanaged {
		t.Error("unmanaged = false; the skip reason must be distinguishable from the mtime skip")
	}
	if r.metadataSource != "" {
		t.Errorf("metadata_source = %q, want empty for an unclaimed file", r.metadataSource)
	}
}

// Frontmatter markfluence knows nothing about -- a shape it explicitly
// preserves -- says nothing about Confluence.
func TestProcessFileSkipsForeignFrontmatter(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	path := writeManifestProject(t, "space: ENG\n",
		"---\nreviewers: [ana, bo]\nowner: sre\n---\n# Post\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusSkipped || !r.unmanaged {
		t.Fatalf("result = %+v, want a successful unmanaged skip", r)
	}
}

// A file that IS registered but has no page id fails: something claimed it and
// create has not run. This is the case that must not be swept into the skip.
func TestProcessFileRegisteredWithNoPageIDFails(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	path := writeManifestProject(t, "pages:\n  f.md:\n    title: Claimed\n", "# Hello\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if r.ok {
		t.Fatalf("result = %+v, want a failure", r)
	}
	if !strings.Contains(r.errMsg, "no page id") {
		t.Errorf("errMsg = %q, want the no-page-id message", r.errMsg)
	}
	if !strings.Contains(r.errMsg, project.Filename) {
		t.Errorf("errMsg = %q, want it to mention the project file as a place to set one", r.errMsg)
	}
}

// The clobber case: a page_id in two places naming two pages. Publishing to
// either would be a guess, so the file fails and nothing is written.
func TestProcessFileCoordinateDisagreementFails(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	path := writeManifestProject(t, "pages:\n  f.md:\n    page_id: 1\n",
		"---\npage_id: 999\n---\n# Hello\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if r.ok {
		t.Fatalf("result = %+v, want a failure", r)
	}
	if !strings.Contains(r.errMsg, "disagree about where this page is") {
		t.Errorf("errMsg = %q, want the disagreement message", r.errMsg)
	}
	if r.code != jsonout.CodeValidation {
		t.Errorf("code = %q, want VALIDATION", r.code)
	}
}

// A soft disagreement is visible and recoverable, so it warns and the file wins.
func TestProcessFileSoftDisagreementWarns(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		default:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
	path := writeManifestProject(t,
		"pages:\n  f.md:\n    title: Manifest Title\n    page_id: 1\n",
		"---\ntitle: File Title\n---\n# Hello\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok {
		t.Fatalf("result = %+v, want success", r)
	}
	if r.title != "File Title" {
		t.Errorf("title = %q, want the file's", r.title)
	}
	if len(r.warnings) == 0 {
		t.Fatal("no warnings; a soft disagreement must be reported")
	}
	if !strings.Contains(r.warnings[0], "title") {
		t.Errorf("warnings = %#v, want one about title", r.warnings)
	}
	// Both locations spoke, and frontmatter is the one that won.
	if r.metadataSource != "frontmatter" {
		t.Errorf("metadata_source = %q, want frontmatter", r.metadataSource)
	}
}

// A batch mixes the two: one file unmanaged, one publishing. The unmanaged one
// must not fail the batch.
func TestRunBatchSkipsUnmanagedAndPublishesTheRest(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		default:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename),
		[]byte("pages:\n  published.md:\n    title: P\n    page_id: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"published.md", "draft.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	roots := project.NewCache("")
	defer roots.Close()
	indexes := linkindex.NewCache()
	users := pagedoc.NewUserCache()

	got := map[string]string{}
	for _, name := range []string{"published.md", "draft.md"} {
		r := processFile(filepath.Join(dir, name), c, roots, indexes, users)
		if !r.ok {
			t.Fatalf("%s failed: %s", name, r.errMsg)
		}
		got[name] = r.status
	}
	if got["published.md"] != statusPublished {
		t.Errorf("published.md status = %q, want published", got["published.md"])
	}
	if got["draft.md"] != statusSkipped {
		t.Errorf("draft.md status = %q, want skipped", got["draft.md"])
	}
}

// metadata_source is null on an unmanaged skip even for a file whose
// frontmatter holds a known field: the question it answers is which location
// supplied the metadata a page was published from, and nothing was published.
// Setting it before the Managed() check reported "frontmatter" here, which
// contradicted the schema and left --json unable to tell the two skips apart.
func TestProcessFileUnmanagedSkipReportsNoSource(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	// title and space are fields markfluence knows, so this file *contributes*
	// metadata -- but names no page, so nothing claims it.
	path := writeManifestProject(t, "space: ENG\n", "---\ntitle: Draft\nspace: ENG\n---\n# Draft\n")

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	if !r.ok || r.status != statusSkipped || !r.unmanaged {
		t.Fatalf("result = %+v, want an unmanaged skip", r)
	}
	if r.metadataSource != "" {
		t.Errorf("metadata_source = %q, want empty", r.metadataSource)
	}
	if got := r.jsonResult().MetadataSource; got != nil {
		t.Errorf("--json metadata_source = %q, want null", *got)
	}
}

// The two skips have to be distinguishable, which is the whole reason the
// unmanaged flag exists.
func TestRenderHumanDistinguishesTheTwoSkips(t *testing.T) {
	unmanaged := &updateResult{file: "a.md", ok: true, status: statusSkipped, unmanaged: true}
	unchanged := &updateResult{file: "a.md", ok: true, status: statusSkipped}

	got := captureStdout(t, unmanaged.renderHuman)
	if !strings.Contains(got, "not published by markfluence") {
		t.Errorf("unmanaged skip rendered %q", got)
	}
	got = captureStdout(t, unchanged.renderHuman)
	if !strings.Contains(got, "no changes") {
		t.Errorf("unchanged skip rendered %q", got)
	}
}

// captureStdout runs fn with os.Stdout redirected, returning what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	rd, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, rd)
		done <- b.String()
	}()
	fn()
	os.Stdout = saved
	_ = w.Close()
	out := <-done
	_ = rd.Close()
	return out
}
