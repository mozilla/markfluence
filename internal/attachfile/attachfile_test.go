package attachfile

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

func managed(title, source string) client.Attachment {
	a := client.Attachment{ID: "att1", Title: title}
	a.Metadata.Comment = "markfluence: sha256=abc path=" + source
	return a
}

func TestResolveUsesRecordedSource(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := Resolve(root, managed("assets%2Fx.png", "assets/x.png"), false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "assets", "x.png")
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

// TestResolveIgnoresNameWhenUnmanaged is why restoration reads the comment and
// never decodes the stored name: a hand-uploaded file literally named
// "a%2Fb.png" must not be scattered into a/b.png.
func TestResolveIgnoresNameWhenUnmanaged(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := Resolve(root, client.Attachment{Title: "a%2Fb.png"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "a%2Fb.png")
	if got != want {
		t.Errorf("Resolve = %q, want the literal stored name %q", got, want)
	}
}

func TestResolveFlatIgnoresSource(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := Resolve(root, managed("assets%2Fx.png", "assets/x.png"), true)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "assets%2Fx.png")
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

// TestResolveAllowsLegitimateParent covers the supported shared-asset layout:
// an image above its page encodes with "..", and as long as it still lands
// inside --dest it is fine.
func TestResolveAllowsLegitimateParent(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := Resolve(root, managed("..%2Fassets%2Flogo.png", "../assets/logo.png"), false)
	if err == nil {
		t.Fatalf("Resolve = %q; a source escaping the root must be refused", got)
	}

	// The same path is fine when the page sits a directory deeper.
	got, err = Resolve(root, managed("x", "docs/../assets/logo.png"), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "assets", "logo.png"); got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

// TestResolveRefusesEscapes is the clamp. A source path comes from an
// attachment comment, which anyone able to edit the page controls.
func TestResolveRefusesEscapes(t *testing.T) {
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
		if got, err := Resolve(root, managed("n.png", src), false); err == nil {
			t.Errorf("Resolve(path=%q) = %q, nil; want a refusal", src, got)
		}
	}
}

// TestResolveRefusesRootPrefixSibling guards a string-prefix mistake:
// "/tmp/destmore" starts with "/tmp/dest" but is not inside it.
func TestResolveRefusesRootPrefixSibling(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	if got, err := Resolve(root, managed("n.png", "../destmore/x.png"), false); err == nil {
		t.Errorf("Resolve = %q, nil; want a refusal for a sibling sharing the root's prefix", got)
	}
}

func TestResolveEscapeMessageNamesTheAttachment(t *testing.T) {
	_, err := Resolve(filepath.Clean("/tmp/dest"), managed("evil.png", "../../x"), false)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "evil.png") {
		t.Errorf("error %q does not name the attachment", err)
	}
}

// --- Write -------------------------------------------------------------------

// testClient serves body for any request, standing in for Confluence.
func testClient(t *testing.T, body string) *client.ConfluenceClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL, Username: "u", Token: "t"})
}

// withDownload returns an attachment carrying a download link, without which
// Write has nothing to fetch.
func withDownload(a client.Attachment) client.Attachment {
	a.Links.Download = "/rest/api/content/1/child/attachment/att1/download"
	return a
}

func TestWriteCreatesNestedPath(t *testing.T) {
	root := t.TempDir()
	c := testClient(t, "BYTES")
	got := Write(c, withDownload(managed("assets%2Fx.png", "assets/x.png")),
		Options{Root: root})
	if got.Status != StatusDownloaded {
		t.Fatalf("status = %q (%v), want downloaded", got.Status, got.Err)
	}
	b, err := os.ReadFile(filepath.Join(root, "assets", "x.png"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "BYTES" {
		t.Errorf("contents = %q, want BYTES", b)
	}
}

func TestWriteSkipsExistingUnlessForce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.png")
	if err := os.WriteFile(path, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	att := withDownload(client.Attachment{Title: "x.png"})
	c := testClient(t, "NEW")

	if got := Write(c, att, Options{Root: root}); got.Status != StatusSkipped {
		t.Errorf("status = %q, want skipped", got.Status)
	}
	if b, _ := os.ReadFile(path); string(b) != "ORIGINAL" {
		t.Errorf("contents = %q; a skip must not write", b)
	}

	if got := Write(c, att, Options{Root: root, Force: true}); got.Status != StatusDownloaded {
		t.Errorf("status = %q, want downloaded under --force", got.Status)
	}
	if b, _ := os.ReadFile(path); string(b) != "NEW" {
		t.Errorf("contents = %q, want NEW after --force", b)
	}
}

func TestWriteDryRunCreatesNothing(t *testing.T) {
	root := t.TempDir()
	c := testClient(t, "BYTES")
	got := Write(c, withDownload(managed("assets%2Fx.png", "assets/x.png")),
		Options{Root: root, DryRun: true})
	if got.Status != StatusDownloaded {
		t.Errorf("status = %q, want downloaded (the forecast)", got.Status)
	}
	if got.DestPath == "" {
		t.Error("a dry run should still report where the file would go")
	}
	if _, err := os.Stat(filepath.Join(root, "assets")); !os.IsNotExist(err) {
		t.Error("dry run created a directory")
	}
}

// TestWriteRefusesEscapeWithoutWriting is the clamp at the Write layer: a
// refused path must not leave a file anywhere.
func TestWriteRefusesEscapeWithoutWriting(t *testing.T) {
	root := t.TempDir()
	c := testClient(t, "BYTES")
	got := Write(c, withDownload(managed("evil.png", "../escape.png")), Options{Root: root})
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.png")); !os.IsNotExist(err) {
		t.Error("a refused attachment was written outside the root")
	}
}

func TestWriteFlatUsesStoredName(t *testing.T) {
	root := t.TempDir()
	c := testClient(t, "BYTES")
	got := Write(c, withDownload(managed("assets%2Fx.png", "assets/x.png")),
		Options{Root: root, Flat: true})
	if want := filepath.Join(root, "assets%2Fx.png"); got.DestPath != want {
		t.Errorf("dest = %q, want %q", got.DestPath, want)
	}
}
