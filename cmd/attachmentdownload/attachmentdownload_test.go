package attachmentdownload

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

// managed builds an attachment carrying a recorded source path.
func managed(title, source string) client.Attachment {
	a := client.Attachment{ID: "att1", Title: title}
	a.Metadata.Comment = "markfluence: sha256=abc path=" + source
	return a
}

func TestDestPathUsesRecordedSource(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := destPath(root, managed("assets%2Fx.png", "assets/x.png"), false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "assets", "x.png")
	if got != want {
		t.Errorf("destPath = %q, want %q", got, want)
	}
}

// TestDestPathIgnoresNameWhenUnmanaged is why restoration reads the comment and
// never decodes the stored name: a hand-uploaded file literally named
// "a%2Fb.png" must not be scattered into a/b.png.
func TestDestPathIgnoresNameWhenUnmanaged(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := destPath(root, client.Attachment{Title: "a%2Fb.png"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "a%2Fb.png")
	if got != want {
		t.Errorf("destPath = %q, want the literal stored name %q", got, want)
	}
}

func TestDestPathFlatIgnoresSource(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := destPath(root, managed("assets%2Fx.png", "assets/x.png"), true)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "assets%2Fx.png")
	if got != want {
		t.Errorf("destPath = %q, want %q", got, want)
	}
}

// TestDestPathAllowsLegitimateParent covers the supported shared-asset layout:
// an image above its page encodes with "..", and as long as it still lands
// inside --dest it is fine.
func TestDestPathAllowsLegitimateParent(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := destPath(root, managed("..%2Fassets%2Flogo.png", "../assets/logo.png"), false)
	if err == nil {
		t.Fatalf("destPath = %q; a source escaping the root must be refused", got)
	}

	// The same path is fine when the page sits a directory deeper.
	got, err = destPath(root, managed("x", "docs/../assets/logo.png"), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "assets", "logo.png"); got != want {
		t.Errorf("destPath = %q, want %q", got, want)
	}
}

// TestDestPathRefusesEscapes is the clamp. A source path comes from an
// attachment comment, which anyone able to edit the page controls.
func TestDestPathRefusesEscapes(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	cases := []string{
		"../../escape.png",
		"../../../.ssh/authorized_keys",
		"/etc/passwd",
		"/tmp/dest-sibling/x.png",
		"..",
		".",
	}
	for _, src := range cases {
		if got, err := destPath(root, managed("n.png", src), false); err == nil {
			t.Errorf("destPath(path=%q) = %q, nil; want a refusal", src, got)
		}
	}
}

// TestDestPathRefusesRootPrefixSibling guards a string-prefix mistake:
// "/tmp/destmore" starts with "/tmp/dest" but is not inside it.
func TestDestPathRefusesRootPrefixSibling(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	if got, err := destPath(root, managed("n.png", "../destmore/x.png"), false); err == nil {
		t.Errorf("destPath = %q, nil; want a refusal for a sibling sharing the root's prefix", got)
	}
}

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

// TestDestPathEscapeMessageNamesTheAttachment keeps the failure actionable: the
// user needs to know which attachment was refused.
func TestDestPathEscapeMessageNamesTheAttachment(t *testing.T) {
	_, err := destPath(filepath.Clean("/tmp/dest"), managed("evil.png", "../../x"), false)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "evil.png") {
		t.Errorf("error %q does not name the attachment", err)
	}
}
