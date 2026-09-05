package attachfile

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
)

func managed(title, source string) client.Attachment {
	a := client.Attachment{ID: "att1", Title: title}
	a.Metadata.Comment = "markfluence: sha256=abc path=" + source
	return a
}

func TestResolveUsesRecordedSource(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := Resolve(root, managed("x.png", "assets/x.png"), false)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "assets", "x.png")
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

// TestResolveIgnoresNameWhenUnmanaged is why restoration reads the comment and
// never interprets the stored name: a file literally named "a%2Fb.png" must not
// be scattered into a/b.png.
//
// This used to be a judgement call -- a name that looked encoded might have been
// one markfluence wrote, and there was no telling. It is not one any more.
// markfluence names an attachment by its base name, so a name containing "%2F"
// is a filename, and convert.sourceFor answers the same way on the markdown
// side. The two agreeing is what keeps a downloaded file where the markdown
// says it is.
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
	got, err := Resolve(root, managed("x.png", "assets/x.png"), true)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "x.png")
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

// TestResolveAllowsLegitimateParent covers the supported shared-asset layout:
// an image above its page encodes with "..", and as long as it still lands
// inside --dest it is fine.
func TestResolveAllowsLegitimateParent(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	got, err := Resolve(root, managed("logo.png", "../assets/logo.png"), false)
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
	return clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
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
	got := Write(c, withDownload(managed("x.png", "assets/x.png")),
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

// TestWriteRemovesPartialFileOnDownloadFailure: Create makes (or truncates) the
// destination before the download runs, so a failure partway through -- or
// before a single byte is written, as here -- must not leave that file behind.
// Left in place, the next run's own-existence check would see it and report a
// skip, silently masking a download that never actually completed.
func TestWriteRemovesPartialFileOnDownloadFailure(t *testing.T) {
	root := t.TempDir()
	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	att := withDownload(client.Attachment{Title: "x.png"})

	got := Write(c, att, Options{Root: root})
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "x.png")); !os.IsNotExist(err) {
		t.Errorf("a failed download left a file behind (err=%v); a retry would see it and skip", err)
	}

	// The retry itself: without the fix, this would report skipped instead of
	// trying again.
	c2 := testClient(t, "BYTES")
	if got := Write(c2, att, Options{Root: root}); got.Status != StatusDownloaded {
		t.Errorf("retry status = %q, want downloaded (not skipped over the earlier failure)", got.Status)
	}
}

func TestWriteDryRunCreatesNothing(t *testing.T) {
	root := t.TempDir()
	c := testClient(t, "BYTES")
	got := Write(c, withDownload(managed("x.png", "assets/x.png")),
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
	got := Write(c, withDownload(managed("x.png", "assets/x.png")),
		Options{Root: root, Flat: true})
	if want := filepath.Join(root, "x.png"); got.DestPath != want {
		t.Errorf("dest = %q, want %q", got.DestPath, want)
	}
}

// --- containment against symlinks --------------------------------------------
//
// Resolve's check is lexical and cannot see symlinks: a destination containing
// a symlinked directory would pass the string comparison while the write landed
// elsewhere on disk. Confinement is enforced by os.Root in Write, so these
// exercise Write rather than Resolve, and each asserts that nothing was created
// outside the destination -- not merely that an error came back.

// symlinkFixture builds a destination containing links that point out of it,
// and returns the destination and the outside directory to check for leaks.
func symlinkFixture(t *testing.T) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "dest")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A symlinked directory, as in a repo where docs/assets -> ../shared/assets.
	if err := os.Symlink(outside, filepath.Join(root, "assets")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// A symlinked file, targeting a path outside the destination.
	if err := os.Symlink(filepath.Join(outside, "leak.png"), filepath.Join(root, "link.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return root, outside
}

func assertNothingOutside(t *testing.T, outside string) {
	t.Helper()
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("wrote outside the destination: %v", names)
	}
}

// TestWriteRefusesSymlinkedDirectory is the case a lexical clamp misses: every
// path component looks contained, but "assets" leads out of the destination.
func TestWriteRefusesSymlinkedDirectory(t *testing.T) {
	root, outside := symlinkFixture(t)
	c := testClient(t, "PWNED")

	got := Write(c, withDownload(managed("x.png", "assets/x.png")), Options{Root: root})
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	assertNothingOutside(t, outside)
}

// TestWriteRefusesSymlinkedFile covers the final component being a link out,
// which would otherwise be followed and truncated by a create.
func TestWriteRefusesSymlinkedFile(t *testing.T) {
	root, outside := symlinkFixture(t)
	c := testClient(t, "PWNED")

	got := Write(c, withDownload(client.Attachment{Title: "link.png"}), Options{Root: root})
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.Err == nil || !strings.Contains(got.Err.Error(), "outside the destination") {
		t.Errorf("error = %v, want it to explain the refusal", got.Err)
	}
	assertNothingOutside(t, outside)
}

// TestWriteRefusesSymlinkedFileUnderForce pins that --force does not weaken
// containment: it governs overwriting, not where a file may be written.
func TestWriteRefusesSymlinkedFileUnderForce(t *testing.T) {
	root, outside := symlinkFixture(t)
	c := testClient(t, "PWNED")

	got := Write(c, withDownload(client.Attachment{Title: "link.png"}),
		Options{Root: root, Force: true})
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want failed even with --force", got.Status)
	}
	assertNothingOutside(t, outside)
}

// TestWriteErrorNamesTheAttachment keeps a refusal actionable: os.Root's bare
// message says neither which attachment nor where it was going.
func TestWriteErrorNamesTheAttachment(t *testing.T) {
	root, _ := symlinkFixture(t)
	c := testClient(t, "PWNED")

	got := Write(c, withDownload(client.Attachment{Title: "link.png"}), Options{Root: root})
	if got.Err == nil || !strings.Contains(got.Err.Error(), "link.png") {
		t.Errorf("error %v does not name the attachment", got.Err)
	}
}

// TestWriteCreatesRootWhenMissing covers the destination not existing yet, which
// is the ordinary first-export case.
func TestWriteCreatesRootWhenMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new", "dest")
	c := testClient(t, "BYTES")

	got := Write(c, withDownload(managed("x.png", "assets/x.png")), Options{Root: root})
	if got.Status != StatusDownloaded {
		t.Fatalf("status = %q (%v), want downloaded", got.Status, got.Err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "assets", "x.png")); err != nil || string(b) != "BYTES" {
		t.Errorf("contents = %q, %v", b, err)
	}
}

// TestResolveDoesNotExpandTildeOrVars pins that a home-directory or variable
// reference in a recorded path is an ordinary filename to Go, not an expansion.
// Tilde expansion is a shell feature; if that ever stopped being true, a hostile
// comment could aim a write at the user's home directory.
func TestResolveDoesNotExpandTildeOrVars(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	cases := map[string]string{
		"~/.ssh/authorized_keys": filepath.Join(root, "~", ".ssh", "authorized_keys"),
		"$HOME/x.png":            filepath.Join(root, "$HOME", "x.png"),
		// "~" alone is a legal filename, so it is a file inside root -- not the
		// home directory, and not an error.
		"~": filepath.Join(root, "~"),
	}
	for src, want := range cases {
		got, err := Resolve(root, managed("n.png", src), false)
		if err != nil || got != want {
			t.Errorf("Resolve(path=%q) = %q, %v; want %q", src, got, err, want)
		}
		if strings.Contains(got, os.Getenv("HOME")) && os.Getenv("HOME") != "" {
			t.Errorf("Resolve(path=%q) = %q, which reached the home directory", src, got)
		}
	}
}

// TestResolveCleansInteriorTraversal covers "." and ".." inside a path being
// folded away before the containment comparison, so an interior ".." cannot be
// used to smuggle a path past a naive prefix check.
func TestResolveCleansInteriorTraversal(t *testing.T) {
	root := filepath.Clean("/tmp/dest")
	cases := map[string]string{
		"a/./b.png":       filepath.Join(root, "a", "b.png"),
		"a/../b.png":      filepath.Join(root, "b.png"),
		"a/b/../../c.png": filepath.Join(root, "c.png"),
		"a//b.png":        filepath.Join(root, "a", "b.png"),
		"./x.png":         filepath.Join(root, "x.png"),
	}
	for src, want := range cases {
		got, err := Resolve(root, managed("n.png", src), false)
		if err != nil || got != want {
			t.Errorf("Resolve(path=%q) = %q, %v; want %q", src, got, err, want)
		}
	}
}

// TestResolveNormalizesRoot covers roots that are not already clean. Join cleans
// its result, so comparing against an uncleaned root mixes forms and rejects
// everything -- fail-closed, but baffling. A trailing separator is the natural
// way to type a directory, so this is a real input, not a contrived one.
func TestResolveNormalizesRoot(t *testing.T) {
	want := filepath.Join(filepath.Clean("/tmp/dest"), "assets", "x.png")
	roots := []string{
		"/tmp/dest",
		"/tmp/dest/",
		"/tmp/./dest",
		"/tmp//dest",
		"/tmp/other/../dest",
	}
	for _, root := range roots {
		got, err := Resolve(root, managed("x.png", "assets/x.png"), false)
		if err != nil {
			t.Errorf("Resolve(root=%q) errored: %v", root, err)
			continue
		}
		if got != want {
			t.Errorf("Resolve(root=%q) = %q, want %q", root, got, want)
		}
	}
}

// TestResolveNormalizedRootStillRefusesEscapes guards the obvious way to get
// normalization wrong: cleaning the root must not soften containment.
func TestResolveNormalizedRootStillRefusesEscapes(t *testing.T) {
	for _, root := range []string{"/tmp/dest/", "/tmp/./dest", "/tmp//dest"} {
		if got, err := Resolve(root, managed("n.png", "../escape.png"), false); err == nil {
			t.Errorf("Resolve(root=%q, path=../escape.png) = %q, nil; want a refusal", root, got)
		}
	}
}

// TestWriteNormalizesRoot is the end-to-end form: a trailing separator on the
// destination must still write the file, in the right place.
func TestWriteNormalizesRoot(t *testing.T) {
	dir := t.TempDir()
	c := testClient(t, "BYTES")
	got := Write(c, withDownload(managed("x.png", "assets/x.png")),
		Options{Root: dir + string(os.PathSeparator)})
	if got.Status != StatusDownloaded {
		t.Fatalf("status = %q (%v), want downloaded", got.Status, got.Err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "assets", "x.png")); err != nil || string(b) != "BYTES" {
		t.Errorf("contents = %q, %v", b, err)
	}
}
