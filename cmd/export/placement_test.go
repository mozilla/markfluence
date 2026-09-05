package export

// The invariant that ties the two halves of an export together: an attachment
// is written where the exported markdown says it is. They are computed in
// different places -- the destination by the converter through pagedoc.Options,
// the path by attachfile -- so nothing but a test that reads both catches them
// drifting apart.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
)

// nativePageServer serves a page whose only image is an attachment with no
// markfluence comment -- one uploaded through the browser, which is what every
// page that did not originate here looks like.
func nativePageServer(t *testing.T, comment string) *client.ConfluenceClient {
	t.Helper()
	meta := "{}"
	if comment != "" {
		meta = `{"comment":"` + comment + `"}`
	}
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/attachment"):
			_, _ = w.Write([]byte(`{"results":[{"id":"a1","title":"diagram.png","metadata":` +
				meta + `,"_links":{"download":"/download/diagram.png"}}]}`))
		case strings.HasPrefix(r.URL.Path, "/download/"):
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
			res := export(nativePageServer(t, c.comment), page(t, nativePageServer(t, c.comment)), dir)
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
