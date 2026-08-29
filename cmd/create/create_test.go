package create

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/project"
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
		if pf.pageID != "123" || pf.url != wantURL {
			t.Errorf("fields = %+v, want id 123, url %s", pf, wantURL)
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
		if pf.pageID != "999" || pf.url != "" {
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

// parentServer stands in for Confluence for the parent lookups, routing by path
// so a folder id can 404 as a page and resolve as a folder -- which is the whole
// shape of the bug in #68.
func parentServer(t *testing.T, pages, folders map[string]string) *client.ConfluenceClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body string
		var ok bool
		switch {
		case strings.HasPrefix(r.URL.Path, "/wiki/api/v2/pages/"):
			body, ok = pages[strings.TrimPrefix(r.URL.Path, "/wiki/api/v2/pages/")]
		case strings.HasPrefix(r.URL.Path, "/wiki/api/v2/folders/"):
			body, ok = folders[strings.TrimPrefix(r.URL.Path, "/wiki/api/v2/folders/")]
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"status":404,"code":"NOT_FOUND"}]}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL})
}

// TestCheckParentInSpaceAcceptsFolder is #68: a folder id is a legitimate parent,
// but every v2 page route answers one with 404, so looking it up as a page and
// stopping there rejected a parent Confluence would have accepted.
func TestCheckParentInSpaceAcceptsFolder(t *testing.T) {
	const space = "77"
	for _, tc := range []struct {
		name     string
		pages    map[string]string
		folders  map[string]string
		wantKind string
		wantErr  string
	}{
		{
			name:     "a page parent",
			pages:    map[string]string{"100": `{"id":"100","spaceId":"77"}`},
			wantKind: "page",
		},
		{
			name:     "a folder parent",
			folders:  map[string]string{"200": `{"id":"200","type":"folder","spaceId":"77"}`},
			wantKind: "folder",
		},
		{
			name:    "neither kind exists",
			wantErr: "parent 300 not found: no page or folder has that id",
		},
		{
			name:    "a folder in the wrong space",
			folders: map[string]string{"200": `{"id":"200","type":"folder","spaceId":"99"}`},
			wantErr: "parent folder 200 is not in the target space",
		},
		{
			name:    "a page in the wrong space",
			pages:   map[string]string{"100": `{"id":"100","spaceId":"99"}`},
			wantErr: "parent page 100 is not in the target space",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := parentServer(t, tc.pages, tc.folders)
			id := "300"
			if len(tc.pages) > 0 {
				id = "100"
			} else if len(tc.folders) > 0 {
				id = "200"
			}

			kind, err := checkParentInSpace(c, id, space)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error %q, got kind %q", tc.wantErr, kind)
				}
				if err.Error() != tc.wantErr {
					t.Errorf("error = %q, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", kind, tc.wantKind)
			}
		})
	}
}

// searchServer stands in for the duplicate-title lookup, recording the request
// URL so a test can assert which statuses were actually asked for.
func searchServer(t *testing.T, body string) (*client.ConfluenceClient, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.String()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL}), &got
}

// TestCheckTitleFreeSeesArchivedPages: an archived page is invisible in the page
// tree but still reserves its title, so a check that asked only for current
// pages passed validation and then failed the POST -- in a batch, after earlier
// pages had already been created.
func TestCheckTitleFreeSeesArchivedPages(t *testing.T) {
	t.Run("the request asks for archived too", func(t *testing.T) {
		c, url := searchServer(t, `{"results":[],"_links":{}}`)
		if err := checkTitleFree(c, "Runbook", "ENG", "77"); err != nil {
			t.Fatalf("checkTitleFree: %v", err)
		}
		for _, want := range []string{"status=current", "status=archived", "space-id=77"} {
			if !strings.Contains(*url, want) {
				t.Errorf("request %q does not contain %q", *url, want)
			}
		}
	})

	t.Run("an archived clash says so", func(t *testing.T) {
		c, _ := searchServer(t, `{"results":[{"id":"400","title":"Runbook","status":"archived",`+
			`"_links":{"webui":"/spaces/ENG/pages/400/Runbook"}}],"_links":{}}`)
		err := checkTitleFree(c, "Runbook", "ENG", "77")
		if err == nil {
			t.Fatal("want an error for a title held by an archived page")
		}
		// Naming the archive is the point: otherwise the author searches the page
		// tree, finds nothing, and concludes the check is broken.
		for _, want := range []string{"archived", `"Runbook"`, "ENG", "/spaces/ENG/pages/400/Runbook"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("message %q does not mention %q", err.Error(), want)
			}
		}
	})

	t.Run("a current clash reads as before", func(t *testing.T) {
		c, _ := searchServer(t, `{"results":[{"id":"500","title":"Runbook","status":"current",`+
			`"_links":{"webui":"/spaces/ENG/pages/500/Runbook"}}],"_links":{}}`)
		err := checkTitleFree(c, "Runbook", "ENG", "77")
		if err == nil {
			t.Fatal("want an error for a title held by a current page")
		}
		if strings.Contains(err.Error(), "archived") {
			t.Errorf("message %q should not mention archiving for a current page", err.Error())
		}
	})

	t.Run("a free title is no error", func(t *testing.T) {
		c, _ := searchServer(t, `{"results":[],"_links":{}}`)
		if err := checkTitleFree(c, "Brand New", "ENG", "77"); err != nil {
			t.Errorf("checkTitleFree = %v, want nil", err)
		}
	})
}

// --- resolveFile's own validation branches ------------------------------------

func TestResolveFileNoTitle(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "a.md", "no frontmatter here\n")
	roots := project.NewCache("")
	t.Cleanup(roots.Close)

	_, err := resolveFile(path, testClient(), map[string]bool{}, map[string]string{}, roots, linkindex.NewCache())
	if err == nil || !strings.Contains(err.Error(), "no title given") {
		t.Errorf("err = %v, want a no-title error", err)
	}
}

func TestResolveFileNoSpace(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "a.md", "---\ntitle: X\n---\nbody\n")
	roots := project.NewCache("")
	t.Cleanup(roots.Close)

	_, err := resolveFile(path, testClient(), map[string]bool{}, map[string]string{}, roots, linkindex.NewCache())
	if err == nil || !strings.Contains(err.Error(), "no space given") {
		t.Errorf("err = %v, want a no-space error", err)
	}
}

func TestResolveFileSpaceConflict(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "a.md", "---\ntitle: X\nspace: OPS\n---\nbody\n")
	roots := project.NewCache("")
	t.Cleanup(roots.Close)

	spaceOpt = "ENG"
	t.Cleanup(func() { spaceOpt = "" })

	_, err := resolveFile(path, testClient(), map[string]bool{}, map[string]string{}, roots, linkindex.NewCache())
	want := `--space "ENG" conflicts with frontmatter space "OPS"`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

// --- topoSort ------------------------------------------------------------------

// TestTopoSortDetectsCycle: two in-set files naming each other as parent form a
// graph with no valid order at all, which must be rejected outright rather than
// left to loop or silently drop one side.
func TestTopoSortDetectsCycle(t *testing.T) {
	a := record{filename: "a.md", absPath: "/docs/a.md", parent: parentInfo{kind: parentInSet, abs: "/docs/b.md"}}
	b := record{filename: "b.md", absPath: "/docs/b.md", parent: parentInfo{kind: parentInSet, abs: "/docs/a.md"}}
	byAbs := map[string]record{a.absPath: a, b.absPath: b}

	_, err := topoSort([]record{a, b}, byAbs)
	if err == nil || !strings.Contains(err.Error(), "parent cycle detected") {
		t.Errorf("err = %v, want a parent-cycle error", err)
	}
}

// TestTopoSortOrdersParentsBeforeChildren: fed in child-first input order, the
// output must still place each in-set parent before the child that names it.
func TestTopoSortOrdersParentsBeforeChildren(t *testing.T) {
	grandchild := record{filename: "c.md", absPath: "/docs/c.md", parent: parentInfo{kind: parentInSet, abs: "/docs/b.md"}}
	child := record{filename: "b.md", absPath: "/docs/b.md", parent: parentInfo{kind: parentInSet, abs: "/docs/a.md"}}
	parent := record{filename: "a.md", absPath: "/docs/a.md"}
	byAbs := map[string]record{
		grandchild.absPath: grandchild, child.absPath: child, parent.absPath: parent,
	}

	// Deliberately out of order: grandchild, child, parent.
	got, err := topoSort([]record{grandchild, child, parent}, byAbs)
	if err != nil {
		t.Fatalf("topoSort: %v", err)
	}
	pos := map[string]int{}
	for i, r := range got {
		pos[r.absPath] = i
	}
	if pos[parent.absPath] > pos[child.absPath] || pos[child.absPath] > pos[grandchild.absPath] {
		t.Errorf("order = %v, want parent before child before grandchild", got)
	}
}
