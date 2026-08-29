package update

import (
	"encoding/json"
	"fmt"
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
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/project"
)

func TestResolveTitlePageID(t *testing.T) {
	mf := frontmatter.Parse("f.md", "---\ntitle: FM Title\npage_id: 111\n---\nbody\n")

	tests := []struct {
		name                  string
		cliTitle, cliPageID   string
		wantTitle, wantPageID string
	}{
		{"flags override frontmatter", "CLI Title", "222", "CLI Title", "222"},
		{"frontmatter when no flags", "", "", "FM Title", "111"},
		{"only page-id overridden", "", "222", "FM Title", "222"},
		{"only title overridden", "CLI Title", "", "CLI Title", "111"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			title, pageID := resolveTitlePageID(tc.cliTitle, tc.cliPageID, mf)
			if title != tc.wantTitle || pageID != tc.wantPageID {
				t.Errorf("resolveTitlePageID = %q/%q, want %q/%q",
					title, pageID, tc.wantTitle, tc.wantPageID)
			}
		})
	}
}

func TestResolveTitlePageIDEmptyWhenAbsent(t *testing.T) {
	mf := frontmatter.Parse("f.md", "body only, no frontmatter\n")
	title, pageID := resolveTitlePageID("", "", mf)
	if title != "" || pageID != "" {
		t.Errorf("resolveTitlePageID = %q/%q, want empty/empty", title, pageID)
	}
}

func TestResolveWidth(t *testing.T) {
	withFM := frontmatter.Parse("f.md", "---\ntitle: T\npage_width: wide\n---\nb\n")
	noWidth := frontmatter.Parse("f.md", "---\ntitle: T\n---\nb\n")
	noFM := frontmatter.Parse("f.md", "b\n")

	t.Run("flag overrides frontmatter", func(t *testing.T) {
		w, apply, err := resolveWidth("narrow", withFM)
		if err != nil || !apply || w != pagewidth.Narrow {
			t.Fatalf("= %q/%v/%v, want narrow/true/nil", w, apply, err)
		}
	})
	t.Run("frontmatter when no flag", func(t *testing.T) {
		w, apply, err := resolveWidth("", withFM)
		if err != nil || !apply || w != pagewidth.Wide {
			t.Fatalf("= %q/%v/%v, want wide/true/nil", w, apply, err)
		}
	})
	t.Run("no flag and no frontmatter width -> skip", func(t *testing.T) {
		if _, apply, err := resolveWidth("", noWidth); err != nil || apply {
			t.Fatalf("= apply %v err %v, want false/nil", apply, err)
		}
		if _, apply, err := resolveWidth("", noFM); err != nil || apply {
			t.Fatalf("(no frontmatter) = apply %v err %v, want false/nil", apply, err)
		}
	})
	t.Run("invalid flag errors", func(t *testing.T) {
		if _, apply, err := resolveWidth("huge", noFM); err == nil || apply {
			t.Fatalf("= apply %v err %v, want false/error", apply, err)
		}
	})
}

func TestOverrideNeedsSingleFile(t *testing.T) {
	tests := []struct {
		name                string
		cliTitle, cliPageID string
		nFiles              int
		want                bool
	}{
		{"title with two files", "T", "", 2, true},
		{"page-id with two files", "", "9", 2, true},
		{"page-id with one file", "", "9", 1, false},
		{"no overrides, many files", "", "", 3, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := overrideNeedsSingleFile(tc.cliTitle, tc.cliPageID, tc.nFiles); got != tc.want {
				t.Errorf("overrideNeedsSingleFile = %v, want %v", got, tc.want)
			}
		})
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
	r := processFile(path, c, project.NewCache(""), linkindex.NewCache())
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

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache())
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

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache())
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

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache())
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

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache())
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want ok/published: --force bypasses the mtime skip", r)
	}
	if !sawPut {
		t.Fatal("want UpdatePage to have been called despite the old mtime")
	}
}
