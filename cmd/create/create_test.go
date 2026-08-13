package create

import (
	"errors"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pagewidth"
)

// testClient is a client that talks to nothing: it exists so URL-building and the
// page_id checks can be exercised without a server. A page_id check that reaches
// the network would fail loudly rather than pass quietly.
func testClient() *client.ConfluenceClient {
	return client.New(client.Config{SiteURL: "https://wiki.example.net"})
}

// TestPageIDFailureFor covers the two server-answer cases for a frontmatter
// page_id, which used to be one bare message ("a page already exists at this
// page_id") and one silent success that created a second page.
func TestPageIDFailureFor(t *testing.T) {
	c := testClient()

	t.Run("a page is already there", func(t *testing.T) {
		page := &client.Page{
			ID: "123", Title: "Deploy Runbook",
			Links: client.Links{WebUI: "/spaces/ENG/pages/123/Deploy+Runbook"},
		}
		err := pageIDFailureFor(c, "123", page)
		var pf *pageIDFailure
		if !errors.As(err, &pf) {
			t.Fatalf("error is %T, want *pageIDFailure", err)
		}
		wantURL := "https://wiki.example.net/wiki/spaces/ENG/pages/123/Deploy+Runbook"
		if pf.pageID != "123" || pf.url != wantURL || pf.title != "Deploy Runbook" {
			t.Errorf("fields = %+v, want id 123, title Deploy Runbook, url %s", pf, wantURL)
		}
		// The URL is the point of the issue: it must be in the message a human
		// reads, not only in the JSON fields.
		for _, want := range []string{"123", `"Deploy Runbook"`, wantURL} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("message %q does not mention %q", err.Error(), want)
			}
		}
	})

	t.Run("the id resolves to nothing", func(t *testing.T) {
		err := pageIDFailureFor(c, "999", nil)
		var pf *pageIDFailure
		if !errors.As(err, &pf) {
			t.Fatalf("error is %T, want *pageIDFailure", err)
		}
		if pf.pageID != "999" || pf.url != "" || pf.title != "" {
			t.Errorf("fields = %+v, want id 999 and no page details", pf)
		}
		// Must say the id is bad and what to do -- not create a page silently.
		for _, want := range []string{"999", "not found", "remove it"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("message %q does not mention %q", err.Error(), want)
			}
		}
	})
}

// TestCheckPageIDLocalCases covers what checkPageID decides without asking the
// server. Both paths must return before any request: testClient points at a host
// that does not exist, so a stray fetch surfaces as a transport error instead.
func TestCheckPageIDLocalCases(t *testing.T) {
	c := testClient()

	if err := checkPageID(c, ""); err != nil {
		t.Errorf("checkPageID with no page_id = %v, want nil", err)
	}

	err := checkPageID(c, "TODO")
	var pf *pageIDFailure
	if !errors.As(err, &pf) {
		t.Fatalf("error is %T (%v), want *pageIDFailure", err, err)
	}
	if pf.pageID != "TODO" || pf.url != "" {
		t.Errorf("fields = %+v, want id TODO and no url", pf)
	}
	for _, want := range []string{`"TODO"`, "not a numeric page id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
}

func TestResolveTitle(t *testing.T) {
	mf := frontmatter.Parse("f.md", "---\ntitle: FM Title\n---\nb\n")
	if got := resolveTitle("CLI Title", mf); got != "CLI Title" {
		t.Errorf("flag override = %q, want CLI Title", got)
	}
	if got := resolveTitle("", mf); got != "FM Title" {
		t.Errorf("frontmatter = %q, want FM Title", got)
	}
	empty := frontmatter.Parse("f.md", "body, no frontmatter\n")
	if got := resolveTitle("", empty); got != "" {
		t.Errorf("absent = %q, want empty", got)
	}
}

func TestResolveWidth(t *testing.T) {
	withFM := map[string]string{"page_width": "wide"}
	t.Run("flag overrides frontmatter", func(t *testing.T) {
		if w, err := resolveWidth("narrow", withFM); err != nil || w != pagewidth.Narrow {
			t.Fatalf("= %q/%v, want narrow/nil", w, err)
		}
	})
	t.Run("frontmatter when no flag", func(t *testing.T) {
		if w, err := resolveWidth("", withFM); err != nil || w != pagewidth.Wide {
			t.Fatalf("= %q/%v, want wide/nil", w, err)
		}
	})
	t.Run("defaults to max when unset", func(t *testing.T) {
		if w, err := resolveWidth("", map[string]string{}); err != nil || w != pagewidth.Max {
			t.Fatalf("= %q/%v, want max/nil", w, err)
		}
	})
	t.Run("invalid flag errors", func(t *testing.T) {
		if _, err := resolveWidth("huge", map[string]string{}); err == nil {
			t.Fatal("want error for invalid --page-width")
		}
	})
}

func TestWantPersist(t *testing.T) {
	tests := []struct {
		name               string
		persist, noPersist bool
		want               bool
	}{
		{"default", true, false, true},
		{"no-persist", true, true, false},
		{"persist=false", false, false, false},
		{"both off", false, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wantPersist(tc.persist, tc.noPersist); got != tc.want {
				t.Errorf("wantPersist(%v,%v) = %v, want %v", tc.persist, tc.noPersist, got, tc.want)
			}
		})
	}
}

func TestOverrideNeedsSingleFile(t *testing.T) {
	if !overrideNeedsSingleFile("T", 2) {
		t.Error("--title with 2 files should require single FILE")
	}
	if overrideNeedsSingleFile("T", 1) {
		t.Error("--title with 1 file is fine")
	}
	if overrideNeedsSingleFile("", 3) {
		t.Error("no --title should not trigger the guard")
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
