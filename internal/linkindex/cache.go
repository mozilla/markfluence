package linkindex

import "github.com/mozilla/markfluence/internal/project"

// Cache builds an Index per distinct root, reusing it for every later call
// with the same root. Most batch commands hit this constantly: many files in
// one invocation typically share one project, and should share one index --
// built once, not once per file or per directory, which is the cost Build's
// doc comment measures.
//
// Not safe for concurrent use.
type Cache struct {
	byRoot map[string]*Index
}

// NewCache builds an empty Cache.
func NewCache() *Cache {
	return &Cache{byRoot: map[string]*Index{}}
}

// Get returns the Index for root, building it (via Build) only the first time
// a given root.Dir is seen.
func (c *Cache) Get(root *project.Root) (*Index, error) {
	if idx, ok := c.byRoot[root.Dir]; ok {
		return idx, nil
	}
	idx, err := Build(root)
	if err != nil {
		return nil, err
	}
	c.byRoot[root.Dir] = idx
	return idx, nil
}
