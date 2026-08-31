// Package pagetree walks the tree of pages and folders under a Confluence node.
//
// It lives apart from the commands because the walk is not specific to one:
// listing a subtree and exporting a subtree need the same traversal, and the
// rules it encodes are the kind that must not exist in two copies — see
// docs/confluence/folders.md for the evidence behind each one.
package pagetree

import (
	"sort"

	"github.com/mozilla/markfluence/internal/client"
)

// AllDepths walks the whole subtree, however deep it goes.
const AllDepths = -1

// Node is one page or folder in a walked subtree.
type Node struct {
	ID     string
	Type   string // "page" or "folder"
	Title  string
	Status string
	// ParentID is the node this one hangs off, which for a top-level result is
	// the id the walk started from — or "" for a page at the root of a space,
	// which hangs off no node at all.
	ParentID string
	// Depth is 1 for a direct child, 2 for its child, and so on.
	Depth int
	Space string
	URL   string
}

// Walk returns every page and folder under rootID, depth-first, siblings in the
// order Confluence displays them. Pass AllDepths for no limit; depth 1 is direct
// children only.
//
// A folder counts as a level, exactly like a page: at depth 1 a child folder
// appears as a row but its contents do not. That is only reasonable because
// folders are reported rather than silently traversed — the caller can see there
// is more below and ask for it.
func Walk(c *client.ConfluenceClient, rootID string, maxDepth int) ([]Node, error) {
	w := &walker{c: c, maxDepth: maxDepth, visited: map[string]bool{rootID: true}}
	if err := w.descend(rootID, 1); err != nil {
		return nil, err
	}
	return w.out, nil
}

// WalkSpace returns every page and folder in a space, named by key, in the same
// order and shape Walk returns them.
//
// Depth 1 is the space's *root pages* — normally just the homepage, sometimes
// more (docs/confluence/spaces.md) — so their children are depth 2. A root page
// has no parent node, and reports ParentID "" to say so.
//
// The space is the level above them rather than a node of its own: a space is not
// a page, so it cannot be a row, and every root page really does sit at the top
// of the tree a reader sees in Confluence.
func WalkSpace(c *client.ConfluenceClient, spaceKey string, maxDepth int) ([]Node, error) {
	roots, err := c.ListSpaceRootPages(spaceKey)
	if err != nil {
		return nil, err
	}
	// Sorted for the same reason siblings are: two root pages come back in
	// whatever order the collection route chose, and position is the order
	// Confluence shows them in.
	byPosition(roots)

	w := &walker{c: c, maxDepth: maxDepth, visited: map[string]bool{}}
	if err := w.emit(roots, "", 1); err != nil {
		return nil, err
	}
	return w.out, nil
}

// walker carries the state one traversal accumulates, so Walk and WalkSpace can
// differ only in what they seed it with. Both go through emit, which is what
// keeps "a folder counts as a level" and the visited guard in one copy.
type walker struct {
	c        *client.ConfluenceClient
	maxDepth int
	visited  map[string]bool
	out      []Node
}

// descend lists what is directly under parentID and emits it at depth.
//
// The bound is checked here as well as in emit, and not redundantly: this one
// saves the request pair a level nobody asked for would have cost, where emit's
// decides what is reported.
func (w *walker) descend(parentID string, depth int) error {
	if w.maxDepth != AllDepths && depth > w.maxDepth {
		return nil
	}
	children, err := siblings(w.c, parentID)
	if err != nil {
		return err
	}
	return w.emit(children, parentID, depth)
}

// emit records each node at depth and recurses into it, depth-first, so a node's
// subtree is printed under it rather than after all of its siblings.
func (w *walker) emit(nodes []client.ChildNode, parentID string, depth int) error {
	if w.maxDepth != AllDepths && depth > w.maxDepth {
		return nil
	}
	for _, ch := range nodes {
		if w.visited[ch.ID] {
			continue
		}
		w.visited[ch.ID] = true
		w.out = append(w.out, Node{
			ID:       ch.ID,
			Type:     ch.Type,
			Title:    ch.Title,
			Status:   ch.Status,
			ParentID: parentID,
			Depth:    depth,
			Space:    client.SpaceKeyFromWebUI(ch.Links.WebUI),
			URL:      nodeURL(w.c, ch.Links.WebUI),
		})
		if err := w.descend(ch.ID, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// nodeURL builds the link a reader follows. A v1 child row carries webui but no
// base (base is on the collection, not the row), so the site is prepended here —
// SiteURL, never BaseURL, or a gateway user gets URLs they cannot open.
func nodeURL(c *client.ConfluenceClient, webui string) string {
	if webui == "" {
		return ""
	}
	return c.SiteURL() + "/wiki" + webui
}

// siblings lists the pages and folders directly under id as one ordered slice.
//
// They arrive from two separate requests, so concatenating them would group all
// folders before all pages and lose the order a reader sees in Confluence.
// Sorting the combined slice by position restores it.
func siblings(c *client.ConfluenceClient, id string) ([]client.ChildNode, error) {
	pages, err := c.ListChildPages(id)
	if err != nil {
		return nil, err
	}
	folders, err := c.ListChildFolders(id)
	if err != nil {
		return nil, err
	}
	all := make([]client.ChildNode, 0, len(pages)+len(folders))
	all = append(all, pages...)
	all = append(all, folders...)
	byPosition(all)
	return all, nil
}

// byPosition orders nodes the way Confluence displays them. Stable, so two rows
// sharing a position keep the order they arrived in — pages before folders for a
// merged sibling listing — rather than reordering between runs.
func byPosition(nodes []client.ChildNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Extensions.Position < nodes[j].Extensions.Position
	})
}
