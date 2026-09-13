package actionlog

import "github.com/mozilla/markfluence/internal/project"

// Cache hands out one Log per distinct root, so a batch reads each log once
// rather than once per file.
//
// It exists for the same reason linkindex.Cache does, and it matters more here
// than the file size suggests: without it, `update docs/**/*.md` would re-read
// and re-parse the whole log for every file in the batch, which is quadratic in
// a tree's own publishing history.
//
// A batch may legitimately span roots (docs/root-model.md), so this is keyed by
// root rather than assuming one log per run. Each file's line goes to its own
// root's log, which falls out of the key being root-relative: a key means
// nothing without the root it is relative to.
//
// Not safe for concurrent use.
type Cache struct {
	byRoot map[string]*Log
}

// NewCache builds an empty Cache.
func NewCache() *Cache {
	return &Cache{byRoot: map[string]*Log{}}
}

// Get returns the Log for root, or nil when root declares no project file.
func (c *Cache) Get(root *project.Root) *Log {
	if root == nil || root.File == "" {
		return nil
	}
	if l, ok := c.byRoot[root.Dir]; ok {
		return l
	}
	l := For(root)
	c.byRoot[root.Dir] = l
	return l
}

// Logs returns every log handed out, for a caller reporting once per run.
func (c *Cache) Logs() []*Log {
	out := make([]*Log, 0, len(c.byRoot))
	for _, l := range c.byRoot {
		out = append(out, l)
	}
	return out
}
