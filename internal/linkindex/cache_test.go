package linkindex

import (
	"path/filepath"
	"testing"
)

// TestCacheBuildsOncePerRoot is the entire point of Cache: a second Get for the
// same root.Dir must return the index already built, not rebuild it -- proven
// by changing the tree on disk between the two calls and confirming the second
// call doesn't see the change.
func TestCacheBuildsOncePerRoot(t *testing.T) {
	root := rootAt(t, t.TempDir())
	write(t, filepath.Join(root.Dir, "a.md"), "---\npage_id: 1\ntitle: A\n---\nbody\n")

	c := NewCache()
	idx1, err := c.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx1.Page("a.md"); !ok {
		t.Fatal("want a.md indexed on the first build")
	}

	write(t, filepath.Join(root.Dir, "b.md"), "---\npage_id: 2\ntitle: B\n---\nbody\n")

	idx2, err := c.Get(root)
	if err != nil {
		t.Fatal(err)
	}
	if idx2 != idx1 {
		t.Error("Get returned a different *Index for the same root, want the cached one")
	}
	if _, ok := idx2.Page("b.md"); ok {
		t.Error("second Get saw a file added after the first build, want the cached (stale) index")
	}
}

// TestCacheBuildsSeparatelyPerRoot: two distinct roots must not share an index
// -- the memoization key is root.Dir, not "the first root ever seen."
func TestCacheBuildsSeparatelyPerRoot(t *testing.T) {
	rootA := rootAt(t, t.TempDir())
	write(t, filepath.Join(rootA.Dir, "a.md"), "---\npage_id: 1\ntitle: A\n---\nbody\n")
	rootB := rootAt(t, t.TempDir())
	write(t, filepath.Join(rootB.Dir, "b.md"), "---\npage_id: 2\ntitle: B\n---\nbody\n")

	c := NewCache()
	idxA, err := c.Get(rootA)
	if err != nil {
		t.Fatal(err)
	}
	idxB, err := c.Get(rootB)
	if err != nil {
		t.Fatal(err)
	}
	if idxA == idxB {
		t.Fatal("two different roots got the same *Index")
	}
	if _, ok := idxA.Page("b.md"); ok {
		t.Error("rootA's index contains rootB's file")
	}
	if _, ok := idxB.Page("a.md"); ok {
		t.Error("rootB's index contains rootA's file")
	}
}

func TestCacheGetPropagatesBuildError(t *testing.T) {
	root := rootAt(t, t.TempDir())
	if err := root.FS.Close(); err != nil {
		t.Fatal(err)
	}
	c := NewCache()
	if _, err := c.Get(root); err == nil {
		t.Fatal("want an error when the root's filesystem is already closed")
	}
}
