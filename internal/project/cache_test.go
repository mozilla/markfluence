package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheResolveReusesRootForSameDirectory(t *testing.T) {
	dir := t.TempDir()
	c := NewCache("")
	defer c.Close()

	first, err := c.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("Resolve for the same directory should return the cached Root, not discover again")
	}
}

func TestCacheRootsReturnsDistinctSortedValues(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a")
	b := filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// No project file anywhere, so each falls back to its own directory --
	// two distinct roots.
	c := NewCache("")
	defer c.Close()
	if _, err := c.Resolve(b); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resolve(a); err != nil {
		t.Fatal(err)
	}

	got := c.Roots()
	want := []string{a, b}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Roots() = %v, want %v (sorted)", got, want)
	}
}

// TestCacheResolveBackfillsSharedRoot is the point of walkAndCache: two
// directories under the same project root must share one *Root -- opened
// once -- rather than each independently discovering (and os.OpenRoot-ing)
// the identical root, which is the double-cost a per-startDir-only cache
// still pays across a batch spanning many subdirectories of one project.
func TestCacheResolveBackfillsSharedRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, Filename), []byte("# marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	x := filepath.Join(root, "docs", "a")
	y := filepath.Join(root, "docs", "b")
	for _, d := range []string{x, y} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	c := NewCache("")
	defer c.Close()
	rx, err := c.Resolve(x)
	if err != nil {
		t.Fatal(err)
	}
	ry, err := c.Resolve(y)
	if err != nil {
		t.Fatal(err)
	}
	if rx != ry {
		t.Error("Resolve for two directories under the same project root returned distinct *Root values, want one shared")
	}
	if _, ok := c.byDir[filepath.Join(root, "docs")]; !ok {
		t.Error("the shared ancestor 'docs', visited resolving x, should be backfilled into the cache")
	}
}

// TestCacheResolveFallbackDoesNotContaminateAncestors guards the hazard
// walkAndCache's backfill has to avoid: the no-project-file fallback binds a
// Root to the directory Resolve was actually called with, not to any
// ancestor visited along the way, so a second, unrelated directory sharing
// that ancestor must fall back to *itself*, not inherit the first
// directory's fallback root.
func TestCacheResolveFallbackDoesNotContaminateAncestors(t *testing.T) {
	base := t.TempDir()
	x := filepath.Join(base, "a", "x")
	y := filepath.Join(base, "a", "y")
	for _, d := range []string{x, y} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	c := NewCache("")
	defer c.Close()
	rx, err := c.Resolve(x)
	if err != nil {
		t.Fatal(err)
	}
	ry, err := c.Resolve(y)
	if err != nil {
		t.Fatal(err)
	}
	if rx.Dir != x {
		t.Errorf("Resolve(x).Dir = %q, want %q (no project file, falls back to itself)", rx.Dir, x)
	}
	if ry.Dir != y {
		t.Errorf("Resolve(y).Dir = %q, want %q -- got %q instead, contaminated by x's fallback",
			ry.Dir, y, ry.Dir)
	}
}

func TestCacheOverrideAppliesToEveryResolve(t *testing.T) {
	override := t.TempDir()
	base := t.TempDir()
	a := filepath.Join(base, "a")
	b := filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	c := NewCache(override)
	defer c.Close()
	for _, dir := range []string{a, b} {
		got, err := c.Resolve(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got.Dir != override {
			t.Errorf("Resolve(%q).Dir = %q, want the override %q", dir, got.Dir, override)
		}
	}
	if roots := c.Roots(); len(roots) != 1 || roots[0] != override {
		t.Errorf("Roots() = %v, want exactly [%q]", roots, override)
	}
}

func TestCacheCloseClearsEntries(t *testing.T) {
	c := NewCache("")
	if _, err := c.Resolve(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if len(c.byDir) != 0 {
		t.Errorf("byDir has %d entries after Close, want 0", len(c.byDir))
	}
}
