package update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
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

// In gateway mode the request base is api.atlassian.com, which must never reach a
// reader. Every pageURL branch has to resolve against the site instead.
func TestPageURLUsesSiteNotGateway(t *testing.T) {
	c := client.New(client.Config{SiteURL: "https://wiki.example.net", CloudID: "abc-123"})

	tests := []struct {
		name       string
		base, webu string
		want       string
	}{
		{
			"no webui link falls back to the site",
			"", "",
			"https://wiki.example.net/wiki/pages/viewpage.action?pageId=42",
		},
		{
			"webui with no base joins onto the site",
			"", "/spaces/ENG/pages/42/Title",
			"https://wiki.example.net/wiki/spaces/ENG/pages/42/Title",
		},
		{
			"the API's own base is preferred when present",
			"https://wiki.example.net/wiki", "/spaces/ENG/pages/42/Title",
			"https://wiki.example.net/wiki/spaces/ENG/pages/42/Title",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := &client.Page{ID: "42", Links: client.Links{Base: tc.base, WebUI: tc.webu}}
			if got := pageURL(c, page, "42"); got != tc.want {
				t.Errorf("pageURL = %q, want %q", got, tc.want)
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

	r := processFile(path, client.New(client.Config{SiteURL: "https://wiki.example.net"}), project.NewCache(""))
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wiki/api/v2/pages/999" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":404,"code":"NOT_FOUND"}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte("---\ntitle: T\npage_id: 999\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	r := processFile(path, client.New(client.Config{SiteURL: srv.URL}), project.NewCache(""))
	if r.ok {
		t.Fatal("a page_id that resolves to nothing must fail the file")
	}
	want := "page_id 999 not found (deleted or wrong); " +
		"correct it, or remove it and use create instead"
	if r.errMsg != want {
		t.Errorf("errMsg =\n %q\nwant\n %q", r.errMsg, want)
	}
	// The raw transport error must not leak: no method, URL, or response body.
	for _, unwanted := range []string{"HTTP 404", "GET ", srv.URL, "errors"} {
		if strings.Contains(r.errMsg, unwanted) {
			t.Errorf("errMsg = %q, should not contain %q", r.errMsg, unwanted)
		}
	}
	if r.code != jsonout.CodeNotFound {
		t.Errorf("code = %q, want %q", r.code, jsonout.CodeNotFound)
	}
}
