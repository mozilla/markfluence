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

// TestCacheCloseIsSafeWithABackfilledSharedRoot: walkAndCache's backfill can
// point several byDir keys at the same *Root, so Close -- which iterates every
// key and closes its Root unconditionally -- calls FS.Close() on that same
// underlying handle more than once. os.Root.Close is safe to call repeatedly
// (confirmed: it returns nil every time, not an error on the second call), so
// this must not panic or fail.
func TestCacheCloseIsSafeWithABackfilledSharedRoot(t *testing.T) {
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
	if _, err := c.Resolve(x); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resolve(y); err != nil {
		t.Fatal(err)
	}
	// x, y, and the backfilled "docs" ancestor all share one *Root.
	if len(c.byDir) < 3 {
		t.Fatalf("byDir has %d entries, want at least 3 (x, y, and the backfilled ancestor)", len(c.byDir))
	}
	c.Close() // must not panic despite closing the same *os.Root more than once
}

// TestCacheResolveIndependentOfBatchComposition is the other half of
// guarantee L2 (invocation-independent, docs/guarantees.md): resolving one
// file's root must not depend on which other files happen to be in the same
// batch. A single Cache is shared across every file in a create/update
// invocation, so its memoization must be purely an optimization -- it must
// never change what a given directory resolves to depending on what else was
// resolved through the same Cache, in either order.
//
// target and sibling deliberately belong to two different, unrelated
// projects (each with its own marker file): a contamination bug that just
// returns whatever the cache last resolved -- rather than what was actually
// asked for -- would otherwise happen to produce the right answer whenever a
// test's fixtures all shared one root, and pass by accident.
func TestCacheResolveIndependentOfBatchComposition(t *testing.T) {
	projectA := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectA, Filename), []byte("# marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(projectA, "docs")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	projectB := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectB, Filename), []byte("# marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(projectB, "docs")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}

	alone := NewCache("")
	defer alone.Close()
	wantRoot, err := alone.Resolve(target)
	if err != nil {
		t.Fatal(err)
	}
	if wantRoot.Dir != projectA {
		t.Fatalf("test fixture is wrong: target resolved to %q, want %q", wantRoot.Dir, projectA)
	}

	// A second, independent Cache resolves an unrelated sibling from a
	// different project first, then the same target -- simulating a batch
	// that also happened to include a file from "sibling". The order and the
	// extra file must not change target's result.
	withSibling := NewCache("")
	defer withSibling.Close()
	if _, err := withSibling.Resolve(sibling); err != nil {
		t.Fatal(err)
	}
	got, err := withSibling.Resolve(target)
	if err != nil {
		t.Fatal(err)
	}

	if got.Dir != wantRoot.Dir || got.File != wantRoot.File {
		t.Errorf("target resolved to Dir=%q File=%q alongside a sibling from a different project, "+
			"want Dir=%q File=%q (its result when resolved alone) -- batch composition must not matter",
			got.Dir, got.File, wantRoot.Dir, wantRoot.File)
	}
}
