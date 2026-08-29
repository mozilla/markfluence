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
// override, via project.Resolve) only the first time a given directory is
// seen.
func (c *Cache) Resolve(startDir string) (*Root, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}
	if root, ok := c.byDir[abs]; ok {
		return root, nil
	}
	root, err := Resolve(c.override, abs)
	if err != nil {
		return nil, err
	}
	c.byDir[abs] = root
	return root, nil
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
