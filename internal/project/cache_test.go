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
