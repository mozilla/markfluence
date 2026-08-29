package linkindex

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mozilla/markfluence/internal/project"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rootAt(t *testing.T, dir string) *project.Root {
	t.Helper()
	r, err := project.FromPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.FS.Close() })
	return r
}

// TestBuildKeysByRootRelativePathNotBasename is Scenario A's fix: two files
// sharing a basename in different directories must not collide -- each gets
// its own entry, keyed by its full path from root.
func TestBuildKeysByRootRelativePathNotBasename(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "overview.md"), "---\npage_id: 999\ntitle: Product Overview\n---\nbody\n")
	write(t, filepath.Join(root, "setup", "overview.md"), "---\npage_id: 777\ntitle: Setup Overview\n---\nbody\n")

	idx, err := Build(rootAt(t, root))
	if err != nil {
		t.Fatal(err)
	}

	top, ok := idx.Page("overview.md")
	if !ok || top.PageID != "999" {
		t.Errorf("Page(%q) = %+v, %v; want page_id 999", "overview.md", top, ok)
	}
	nested, ok := idx.Page("setup/overview.md")
	if !ok || nested.PageID != "777" {
		t.Errorf("Page(%q) = %+v, %v; want page_id 777", "setup/overview.md", nested, ok)
	}
}

// TestBuildCollectsAnchorsPerPage covers a heading on one page not leaking
// into another page's anchor set.
func TestBuildCollectsAnchorsPerPage(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "# Section One\n")
	write(t, filepath.Join(root, "b.md"), "# Section Two\n")

	idx, err := Build(rootAt(t, root))
	if err != nil {
		t.Fatal(err)
	}

	if slug, ok := idx.Anchor("a.md", "section-one"); !ok || slug != "Section-One" {
		t.Errorf("Anchor(a.md, section-one) = %q, %v", slug, ok)
	}
	if _, ok := idx.Anchor("a.md", "section-two"); ok {
		t.Error("a.md must not see b.md's heading")
	}
}

// TestBuildSkipsPageWithNoPageID covers a draft (no page_id yet, the ordinary
// state of an unpublished file): it must not appear in Page, since 025 treats
// "not in the index" as the unresolved case a link falls through on.
func TestBuildSkipsPageWithNoPageID(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "draft.md"), "---\ntitle: Draft\n---\nbody\n")

	idx, err := Build(rootAt(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx.Page("draft.md"); ok {
		t.Error("a page_id-less file must not be in the index")
	}
}

// TestBuildDoesNotDescendSymlinkedDirectory is the non-goal: a symlinked
// directory inside root must not be walked into, even though its target
// (here, outside root) holds a page_id-bearing file that would otherwise be
// found.
func TestBuildDoesNotDescendSymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.md"), "---\npage_id: 555\ntitle: Secret\n---\nbody\n")
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	idx, err := Build(rootAt(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx.Page("linked/secret.md"); ok {
		t.Error("the walk must not descend a symlinked directory")
	}
}

func TestSetPageOverridesAndInjects(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.md"), "---\npage_id: 1\ntitle: A\n---\nbody\n")

	idx, err := Build(rootAt(t, root))
	if err != nil {
		t.Fatal(err)
	}
	idx.SetPage("a.md", PageEntry{PageID: "2", Title: "A renamed"})
	if got, ok := idx.Page("a.md"); !ok || got.PageID != "2" {
		t.Errorf("Page(a.md) = %+v, %v; want the override", got, ok)
	}

	// b.md was never on disk with a page_id -- this is create's reserve
	// phase injecting an id that exists only in memory.
	idx.SetPage("b.md", PageEntry{PageID: "3", Title: "B"})
	if got, ok := idx.Page("b.md"); !ok || got.PageID != "3" {
		t.Errorf("Page(b.md) = %+v, %v; want the injected entry", got, ok)
	}
}
