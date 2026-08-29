package project

import (
	"path/filepath"
	"sort"
)

// Cache resolves a root per starting directory, reusing the result -- and its
// open os.Root handle -- for every later call with the same directory. Most
// batch commands hit this constantly: many files in one invocation typically
// share a directory or a project, and re-walking for each one would be the
// same per-conversion cost 025 measured as quadratic at scale.
//
// Not safe for concurrent use.
type Cache struct {
	override string
	byDir    map[string]*Root
}

// NewCache builds a Cache that applies override -- --root's value, or "" when
// the flag wasn't set -- to every Resolve call.
func NewCache(override string) *Cache {
	return &Cache{override: override, byDir: map[string]*Root{}}
}

// Resolve returns the root for startDir, discovering (or applying the
// override) only the first time a given directory is seen. With an override,
// every startDir maps to the same *Root, opened once. With no override, the
// walk up from startDir consults the cache at every level (walkAndCache) so a
// batch spanning many subdirectories of one project pays for Discover's walk
// -- and os.OpenRoot -- once for the whole subtree, not once per distinct
// starting directory (the quadratic cost 025 measured, reintroduced at the
// per-directory granularity a naive byDir[startDir] cache still leaves).
func (c *Cache) Resolve(startDir string) (*Root, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}
	if root, ok := c.byDir[abs]; ok {
		return root, nil
	}
	if c.override != "" {
		root, err := FromPath(c.override)
		if err != nil {
			return nil, err
		}
		c.byDir[abs] = root
		return root, nil
	}
	return c.walkAndCache(abs)
}

// walkAndCache walks upward from abs exactly like Discover, but checks the
// cache at every level first: once the walk reaches a directory this Cache
// has already resolved a root for, every directory visited since abs is
// backfilled to that same *Root instead of opening a second os.Root for a
// root a sibling subtree already found. The no-project-file fallback is
// backfilled only to abs itself, never to an ancestor: that Root is bound to
// abs specifically (Discover's own contract -- see TestDiscoverFallsBackToStartDir
// and TestCacheRootsReturnsDistinctSortedValues), so caching it at an
// ancestor would wrongly hand every unrelated directory above it the same
// fallback root.
func (c *Cache) walkAndCache(abs string) (*Root, error) {
	var visited []string
	dir := abs
	for {
		if root, ok := c.byDir[dir]; ok {
			for _, d := range visited {
				c.byDir[d] = root
			}
			return root, nil
		}
		visited = append(visited, dir)

		hit, file, err := probeMarker(dir)
		if err != nil {
			return nil, err
		}
		if hit {
			root, err := open(dir, file)
			if err != nil {
				return nil, err
			}
			for _, d := range visited {
				c.byDir[d] = root
			}
			return root, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			root, err := open(abs, "")
			if err != nil {
				return nil, err
			}
			c.byDir[abs] = root
			return root, nil
		}
		dir = parent
	}
}

// Roots returns every distinct root Dir this cache has resolved, sorted, for
// reporting -- once per distinct value, not once per file, since a batch
// commonly resolves the same root for every file in it.
func (c *Cache) Roots() []string {
	seen := map[string]bool{}
	var out []string
	for _, root := range c.byDir {
		if !seen[root.Dir] {
			seen[root.Dir] = true
			out = append(out, root.Dir)
		}
	}
	sort.Strings(out)
	return out
}

// Close closes every root handle this cache opened. Call it once, after every
// Resolve call is done -- a root can be reused across many files, so nothing
// closes it until the whole cache does.
func (c *Cache) Close() {
	for _, root := range c.byDir {
		_ = root.FS.Close()
	}
	clear(c.byDir)
}
