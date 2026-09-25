package update

import (
	"errors"
	"fmt"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/parentref"
	"github.com/mozilla/markfluence/internal/project"
)

// checkSpace refuses a page that is not in the space the file declares (#10,
// _plans/053 D1). The declaration is the file's own space field or, failing
// that, the project's space: default -- a project that says ENG and a page
// somewhere else is a mistake whose intent update cannot know.
//
// A refusal, never a move: moving between spaces needs write access to both,
// takes the whole subtree along, and can change who may see it, and nobody has
// needed it.
func checkSpace(meta pagemeta.Resolved, root *project.Root, liveSpace string) error {
	declared, fromProject := meta.Space(root)
	if declared == "" || declared == liveSpace {
		return nil
	}
	where := "this file declares"
	if fromProject {
		where = project.Filename + "'s space: default is"
	}
	return fmt.Errorf("the page is in space %s, but %s %s; update does not move a page between "+
		"spaces -- move it in Confluence, or correct the space", liveSpace, where, declared)
}

// move is a move update is about to make, nil when the page is already where
// the file says.
type move struct {
	// from and to are the page's parent before and after, "" for the top of
	// the space.
	from, to string
	// toTitle names the new parent in the human line, "" for the top.
	toTitle string
	// position and target are MovePage's arguments.
	position, target string
}

// planMove decides whether the file's parent calls for a move, and checks
// everything about the move that can be checked without making it: the parent
// resolves, it is a page or folder in the page's space, and it is not the page
// itself or below it. Everything here is a read, so --dry-run runs it too and
// reports a move that would fail.
//
// An absent parent leaves the page where it is (L9). A blank one -- null, ~,
// or nothing -- is the top of the space, create's reading of the same line.
func planMove(
	c *client.ConfluenceClient, meta pagemeta.Resolved, root *project.Root, fileDir string,
	page *client.Page, spaceKey string,
) (*move, error) {
	ref, declared := meta.Parent()
	if !declared {
		return nil, nil
	}
	if ref == "" {
		if page.ParentID == "" {
			return nil, nil
		}
		last, err := lastRootPage(c, spaceKey, page.ID)
		if err != nil {
			return nil, err
		}
		return &move{from: page.ParentID, position: client.MoveAfter, target: last}, nil
	}

	id, err := parentref.Resolve(root, fileDir, ref)
	if err != nil {
		return nil, err
	}
	if id == page.ParentID {
		return nil, nil
	}
	t, err := parentref.Lookup(c, id, page.SpaceID)
	if err != nil {
		return nil, err
	}
	within, err := parentref.Within(c, t, page.ID)
	if err != nil {
		return nil, err
	}
	if within {
		return nil, fmt.Errorf("parent %s is this page or one of the pages under it, "+
			"so moving the page there would make a loop", ref)
	}
	return &move{from: page.ParentID, to: id, toTitle: t.Title, position: client.MoveAppend, target: id}, nil
}

// lastRootPage is the page a move to the top of the space goes after, which
// places it last there, the way a move under a parent appends it. There is no
// "append to the space": a v1 move needs a page to be placed relative to.
//
// A space always has at least one page at the top, its homepage, so an empty
// answer is not a state to recover from.
func lastRootPage(c *client.ConfluenceClient, spaceKey, pageID string) (string, error) {
	roots, err := c.ListSpaceRootPages(spaceKey)
	if err != nil {
		return "", err
	}
	var last *client.ChildNode
	for i := range roots {
		r := &roots[i]
		if r.ID == pageID || (r.Status != "" && r.Status != "current") {
			continue
		}
		if last == nil || r.Extensions.Position > last.Extensions.Position {
			last = r
		}
	}
	if last == nil {
		return "", errors.New("the space has no page at the top to place this one after")
	}
	return last.ID, nil
}

// describe is the human line for a move, and the note on it.
func (m *move) describe(dryRun bool) string {
	verb := "moved"
	if dryRun {
		verb = "would move"
	}
	where := "to the top of the space"
	if m.to != "" {
		where = fmt.Sprintf("under %q", m.toTitle)
	}
	return verb + " " + where + " (placed last; reorder in Confluence if needed)"
}
