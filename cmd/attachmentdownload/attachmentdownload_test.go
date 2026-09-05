package attachmentdownload

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
)

func TestSelectAttachmentsAll(t *testing.T) {
	all := []client.Attachment{{Title: "a.png"}, {Title: "b.png"}}
	got, missing := selectAttachments(all, nil)
	if len(got) != 2 || len(missing) != 0 {
		t.Fatalf("got %d wanted / %d missing, want 2/0", len(got), len(missing))
	}
}

func TestSelectAttachmentsByNamePreservesRequestOrder(t *testing.T) {
	all := []client.Attachment{{Title: "a.png"}, {Title: "b.png"}, {Title: "c.png"}}
	got, missing := selectAttachments(all, []string{"c.png", "a.png"})
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	if got[0].Title != "c.png" || got[1].Title != "a.png" {
		t.Errorf("order = %q/%q, want c.png/a.png", got[0].Title, got[1].Title)
	}
}

func TestSelectAttachmentsReportsMissing(t *testing.T) {
	all := []client.Attachment{{Title: "a.png"}}
	got, missing := selectAttachments(all, []string{"a.png", "nope.png"})
	if len(got) != 1 {
		t.Errorf("wanted = %d, want 1", len(got))
	}
	if len(missing) != 1 || missing[0] != "nope.png" {
		t.Errorf("missing = %v, want [nope.png]", missing)
	}
}

// TestPageDirForUsesTheTitle covers the lookup this command gained for the sake
// of placement: an attachment with no recorded path goes under a directory named
// after its page, so the page's title is needed even though nothing else here
// reads it.
func TestPageDirForUsesTheTitle(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","title":"Deploy: Prod","spaceId":"77"}`))
	})
	got, err := pageDirFor(c, "1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "deploy-prod" {
		t.Errorf("pageDirFor = %q, want deploy-prod", got)
	}
}

// TestPageDirForFallsBackToAFolder: pageref.Resolve accepts a folder URL, and
// every v2 page route answers a folder id with 404, so the page lookup missing
// is not the same as the id being wrong.
func TestPageDirForFallsBackToAFolder(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/folders/") {
			_, _ = w.Write([]byte(`{"id":"9","title":"Runbooks"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"title":"Cannot find a page with id 9"}]}`))
	})
	got, err := pageDirFor(c, "9")
	if err != nil {
		t.Fatal(err)
	}
	if got != "runbooks" {
		t.Errorf("pageDirFor = %q, want runbooks", got)
	}
}

// TestPageDirForFailsWhenNeitherExists: the run must stop before writing
// anything, since scoping half the attachments and not the other half is worse
// than scoping none.
func TestPageDirForFailsWhenNeitherExists(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"title":"Cannot find a page with id 404"}]}`))
	})
	if _, err := pageDirFor(c, "404"); err == nil {
		t.Error("want an error when the id is neither a page nor a folder")
	}
}
