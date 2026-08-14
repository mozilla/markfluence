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
	// the id the walk started from.
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
	// Confluence trees should not contain cycles, but an unbounded walk has no
	// other backstop if one ever appears, and the set costs nothing.
	visited := map[string]bool{rootID: true}
	var out []Node

	var walk func(parentID string, depth int) error
	walk = func(parentID string, depth int) error {
		if maxDepth != AllDepths && depth > maxDepth {
			return nil
		}
		children, err := siblings(c, parentID)
		if err != nil {
			return err
		}
		for _, ch := range children {
			if visited[ch.ID] {
				continue
			}
			visited[ch.ID] = true
			out = append(out, Node{
				ID:       ch.ID,
				Type:     ch.Type,
				Title:    ch.Title,
				Status:   ch.Status,
				ParentID: parentID,
				Depth:    depth,
				Space:    client.SpaceKeyFromWebUI(ch.Links.WebUI),
				URL:      nodeURL(c, ch.Links.WebUI),
			})
			// Depth-first, so a node's subtree is printed under it rather than
			// after all of its siblings.
			if err := walk(ch.ID, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(rootID, 1); err != nil {
		return nil, err
	}
	return out, nil
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
	// Stable, so two rows sharing a position keep pages-before-folders rather
	// than reordering between runs.
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Extensions.Position < all[j].Extensions.Position
	})
	return all, nil
}
