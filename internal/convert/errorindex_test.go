package convert_test

// The invariant cmd/create's preflight conversion rests on: MdToConfluence's
// *error* does not depend on the link index, even though its rendered page
// emphatically does.
//
// create is three-phase (_plans/034). Preflight validates every file, reserve
// creates a content-less page for each and seeds its id into the shared index,
// publish converts and fills each page in. Preflight converts too, purely to
// learn whether the file can convert at all. That is sound for a narrower
// reason than "the converter ignores the index": whether renderImage runs at
// all does depend on it, since renderLink skips a broken link's children and
// Broken is decided by FileExists. What holds is that reserve only calls
// SetPage, which writes idx.pages alone -- nothing there can raise an error or
// change one's text, and FileExists/Anchor read idx.anchors, fixed at Build
// time and the same in both phases. It is also why the preflight result is
// *discarded* rather than reused by publish: the HTML does differ, since an
// in-set link resolves only once the id is there.
//
// Both halves are asserted together on purpose. Either alone can pass
// vacuously -- "the errors match" proves nothing if the seeding never mattered.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/project"
)

// convertSeeded renders body as root/main.md against the link index for root
// with seed applied to it, mirroring what create's reserve phase does to the
// index it shares with publish. A nil seed is the unseeded (preflight) case.
func convertSeeded(
	t *testing.T, root, body string, seed map[string]linkindex.PageEntry, images ...string,
) (*convert.ConfluencePage, error) {
	t.Helper()
	writeImages(t, root, images...)
	md, err := frontmatter.Parse(filepath.Join(root, "main.md"), body)
	if err != nil {
		t.Fatal(err)
	}
	r, err := project.FromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.FS.Close() }()
	idx := testIndex(t, r)
	for key, entry := range seed {
		idx.SetPage(key, entry)
	}
	return convert.MdToConfluence(md, r, idx, "https://wiki.example.net", "ENG", "vtest")
}

// siblingEntry is what create's reserve phase seeds for an in-set file: the id
// of the stub it just created, under the file's root-relative key.
var siblingEntry = map[string]linkindex.PageEntry{
	"sibling.md": {PageID: "4242", Title: "Sibling"},
}

// writeSibling creates an unpublished sibling.md -- a title and no page_id,
// which is what makes a link to it unresolvable until an id is seeded.
func writeSibling(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "sibling.md")
	if err := os.WriteFile(path, []byte("---\ntitle: Sibling\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestErrorDoesNotDependOnTheIndex is the invariant itself: a file that cannot
// convert returns the identical error whether or not the index knows the
// batch's own page ids. The link comes before the images because a collision
// stops the walk -- put it after and the case would never resolve a link at
// all, and would pass for the wrong reason.
func TestErrorDoesNotDependOnTheIndex(t *testing.T) {
	root := t.TempDir()
	writeSibling(t, root)
	body := "see [sibling](sibling.md)\n\n![a](a/diagram.png)\n\n![b](b/diagram.png)\n"
	images := []string{"a/diagram.png", "b/diagram.png"}

	_, unseeded := convertSeeded(t, root, body, nil, images...)
	_, seeded := convertSeeded(t, root, body, siblingEntry, images...)

	if unseeded == nil || seeded == nil {
		t.Fatalf("want a refusal from both: unseeded=%v seeded=%v", unseeded, seeded)
	}
	if unseeded.Error() != seeded.Error() {
		t.Errorf("the error depends on the index:\n unseeded: %s\n   seeded: %s", unseeded, seeded)
	}
}

// TestPageDoesDependOnTheIndex is the other half, and the reason preflight's
// page is thrown away instead of reused: seeding an id turns "link not
// resolved" into a real URL, so a page converted before the reserve phase is
// not the page that should be published.
func TestPageDoesDependOnTheIndex(t *testing.T) {
	root := t.TempDir()
	writeSibling(t, root)
	body := "see [sibling](sibling.md)\n"

	before, err := convertSeeded(t, root, body, nil)
	if err != nil {
		t.Fatalf("unseeded: %v", err)
	}
	after, err := convertSeeded(t, root, body, siblingEntry)
	if err != nil {
		t.Fatalf("seeded: %v", err)
	}

	if before.HTML == after.HTML {
		t.Fatalf("seeding an id changed nothing; this test proves nothing:\n%s", before.HTML)
	}
	if len(before.Warnings) != 1 {
		t.Errorf("unseeded warnings = %v, want one \"link not resolved\"", before.Warnings)
	}
	if len(after.Warnings) != 0 {
		t.Errorf("seeded warnings = %v, want none", after.Warnings)
	}
}
