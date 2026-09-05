package export

// The invariant that ties the two halves of an export together: an attachment
// is written where the exported markdown says it is. They are computed in
// different places -- the destination by the converter through pagedoc.Options,
// the path by attachfile -- so nothing but a test that reads both catches them
// drifting apart.

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/attachfile"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pageslug"
	"github.com/mozilla/markfluence/internal/pagetree"
)

// nativePageServer serves a page whose only image is an attachment with no
// markfluence comment -- one uploaded through the browser, which is what every
// page that did not originate here looks like.
func nativePageServer(t *testing.T, comment string, extra ...string) *client.ConfluenceClient {
	t.Helper()
	meta := "{}"
	if comment != "" {
		meta = `{"comment":"` + comment + `"}`
	}
	// extra names further attachments by their recorded path, for tests about
	// where an attachment is allowed to land.
	more := ""
	for i, path := range extra {
		more += `,{"id":"x` + string(rune('0'+i)) + `","title":"` + filepath.Base(path) +
			`","metadata":{"comment":"markfluence: sha256=zzz path=` + path + `"},` +
			`"_links":{"download":"/download/x.png"}}`
	}
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/attachment"):
			_, _ = w.Write([]byte(`{"results":[{"id":"a1","title":"diagram.png","metadata":` +
				meta + `,"_links":{"download":"/download/diagram.png"}}` + more + `]}`))
		case strings.Contains(r.URL.Path, "/download/"):
			_, _ = w.Write([]byte("PNG"))
		default:
			_, _ = w.Write([]byte(`{"id":"1","title":"Runbook","spaceId":"77","body":{"storage":` +
				`{"value":"<p><ac:image><ri:attachment ri:filename=\"diagram.png\" /></ac:image></p>",` +
				`"representation":"storage"}},"_links":{"webui":"/spaces/ENG/pages/1/Runbook"}}`))
		}
	})
}

// TestExportWritesAttachmentsWhereTheMarkdownPointsThem is the whole property.
// It failed when export rendered with an AttachmentDir and wrote without one:
// the file landed at dest/diagram.png while the markdown said
// runbook/diagram.png, so the export previewed broken and republishing it wrote
// IMAGE BROKEN over the live image.
func TestExportWritesAttachmentsWhereTheMarkdownPointsThem(t *testing.T) {
	for _, c := range []struct{ name, comment, wantDest string }{
		{"no recorded path", "", "runbook/diagram.png"},
		{"recorded path", "markfluence: sha256=abc path=assets/diagram.png", "assets/diagram.png"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			cl := nativePageServer(t, c.comment)
			p := page(t, cl)
			res := exportOne(cl, p, dir, pagedoc.Placement{},
				placement{file: pageslug.Filename(p.Title, p.ID)}, newClaims())
			if res.err != nil {
				t.Fatalf("export: %v", res.err)
			}

			md, err := os.ReadFile(filepath.Join(dir, "runbook.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(md), "]("+c.wantDest+")") {
				t.Errorf("markdown does not point at %s:\n%s", c.wantDest, md)
			}
			// The destination is relative to the markdown file, which sits at
			// the top of dest -- so it is also the path under dest.
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(c.wantDest))); err != nil {
				t.Errorf("no file where the markdown points (%s): %v", c.wantDest, err)
			}
		})
	}
}

// page fetches the body the way run() does, so the test exercises export() with
// what it really receives.
func page(t *testing.T, c *client.ConfluenceClient) *client.Page {
	t.Helper()
	p, err := c.GetPageBodyOrNil("1")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// treeServer serves a two-level tree: Home (1) with a child Onboarding (2),
// each carrying one Confluence-native attachment of the same name -- the case
// page-scoping exists for.
func treeServer(t *testing.T) *client.ConfluenceClient {
	t.Helper()
	body := func(id, title string) string {
		return `{"id":"` + id + `","title":"` + title + `","spaceId":"77","body":{"storage":` +
			`{"value":"<p><ac:image><ri:attachment ri:filename=\"diagram.png\" /></ac:image></p>",` +
			`"representation":"storage"}},"_links":{"webui":"/spaces/ENG/pages/` + id + `/x"}}`
	}
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/page"):
			if strings.Contains(r.URL.Path, "/1/") {
				_, _ = w.Write([]byte(`{"results":[{"id":"2","type":"page","title":"Onboarding",` +
					`"status":"current","extensions":{"position":1},` +
					`"_links":{"webui":"/spaces/ENG/pages/2/Onboarding"}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/folder"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/attachment"):
			_, _ = w.Write([]byte(`{"results":[{"id":"a1","title":"diagram.png","metadata":{},` +
				`"_links":{"download":"/download/diagram.png"}}]}`))
		case strings.Contains(r.URL.Path, "/download/"):
			_, _ = w.Write([]byte("PNG"))
		case strings.Contains(r.URL.Path, "/pages/2"):
			_, _ = w.Write([]byte(body("2", "Onboarding")))
		default:
			_, _ = w.Write([]byte(body("1", "Home")))
		}
	})
}

// TestExportTreeMirrorsTheHierarchy is the feature end to end: the exact set of
// files written, the parent: path that makes the tree republishable, and two
// same-named native attachments landing in different directories rather than on
// top of each other.
func TestExportTreeMirrorsTheHierarchy(t *testing.T) {
	dir := t.TempDir()
	c := treeServer(t)
	root := page(t, c)

	nodes, err := walkUnder(c, root.ID, pagetree.AllDepths)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	results := exportNodes(c, root,
		rootRef{ID: root.ID, Title: root.Title, File: true}, dir, nodes)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("%s: %v", r.title(), r.err)
		}
	}

	for _, want := range []string{
		"home.md",
		"home/onboarding.md",
		"home/diagram.png",
		"home/onboarding/diagram.png",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	child, err := os.ReadFile(filepath.Join(dir, "home", "onboarding.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(child), "parent: ../home.md") {
		t.Errorf("child frontmatter does not point at its parent file:\n%s", child)
	}
	if !strings.Contains(string(child), "](onboarding/diagram.png)") {
		t.Errorf("child image is not page-scoped:\n%s", child)
	}
	parent, err := os.ReadFile(filepath.Join(dir, "home.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(parent), "](home/diagram.png)") {
		t.Errorf("root image is not page-scoped:\n%s", parent)
	}
}

// TestExportSkipsTheRenderForAnExistingFile is the retry story: a page already
// on disk is not re-rendered, so the run does not pay for the page-width read
// and the link lookups a render costs -- while its attachments are still
// checked, which is what lets a retry finish a run that died partway through
// downloading them.
func TestExportSkipsTheRenderForAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	var propertyReads, downloads int
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			propertyReads++
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/attachment"):
			_, _ = w.Write([]byte(`{"results":[{"id":"a1","title":"diagram.png","metadata":{},` +
				`"_links":{"download":"/download/diagram.png"}}]}`))
		case strings.Contains(r.URL.Path, "/download/"):
			downloads++
			_, _ = w.Write([]byte("PNG"))
		default:
			_, _ = w.Write([]byte(`{"id":"1","title":"Runbook","spaceId":"77","body":{"storage":` +
				`{"value":"<p><ac:image><ri:attachment ri:filename=\"diagram.png\" /></ac:image></p>",` +
				`"representation":"storage"}},"_links":{"webui":"/spaces/ENG/pages/1/Runbook"}}`))
		}
	})
	p := page(t, c)
	place := placement{file: "runbook.md"}

	first := exportOne(c, p, dir, pagedoc.Placement{}, place, newClaims())
	if first.pageStatus != statusWrote {
		t.Fatalf("first run status = %q, want %q", first.pageStatus, statusWrote)
	}
	readsAfterFirst, downloadsAfterFirst := propertyReads, downloads

	// Delete the attachment but keep the page file: the shape a run that died
	// partway through its attachments leaves behind.
	if err := os.Remove(filepath.Join(dir, "runbook", "diagram.png")); err != nil {
		t.Fatal(err)
	}

	second := exportOne(c, p, dir, pagedoc.Placement{}, place, newClaims())
	if second.pageStatus != attachfile.StatusSkipped {
		t.Errorf("second run status = %q, want %q", second.pageStatus, attachfile.StatusSkipped)
	}
	if propertyReads != readsAfterFirst {
		t.Errorf("page width was read again on a skipped page (%d then %d)",
			readsAfterFirst, propertyReads)
	}
	if downloads != downloadsAfterFirst+1 {
		t.Errorf("downloads = %d, want the missing attachment fetched again", downloads)
	}
	if _, err := os.Stat(filepath.Join(dir, "runbook", "diagram.png")); err != nil {
		t.Errorf("the attachment was not restored: %v", err)
	}
}

// captureBoth runs fn with stdout and stderr redirected into one buffer.
// Human output splits Success/Info (stdout) from Warn/Error (stderr), so a test
// about what a reader sees needs both.
func captureBoth(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outOld, errOld := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	fn()
	os.Stdout, os.Stderr = outOld, errOld
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
