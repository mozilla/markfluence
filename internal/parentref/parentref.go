// Package parentref resolves a file's parent: reference, the part create,
// update and diff share: a ".md" path to the page id its file declares, and an
// id to what it names in a space.
//
// A package because each command had grown its own copy of ".md parent -> page
// id", and the copies had already drifted: one read through the documentation
// root's os.Root and refused a symlink, another only read. update moving pages
// (#10, _plans/053) would have made a third. The parts that differ stay with
// the caller -- create's in-set parents and --parent flag, and diff reporting a
// failure as a note rather than failing.
package parentref

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/project"
)

// IsFile reports whether ref names a parent by its file rather than by id.
func IsFile(ref string) bool { return strings.HasSuffix(ref, ".md") }

// File is a ".md" parent reference located inside a documentation root.
type File struct {
	// Abs is the parent file's absolute path.
	Abs string
	// Rel is its path relative to the root, slash-separated, which is how
	// root.FS names it.
	Rel string
}

// Locate finds the file a ".md" parent reference names, relative to fileDir,
// the directory of the file declaring it. It reads nothing but the file's
// metadata.
//
// A parent outside root is an error rather than an unresolved reference the
// way a link is (S2): a parent is load-bearing, and publishing under the wrong
// parent, or silently under none, is worse than not publishing. The check goes
// through root.FS, so a symbolic link partway down the path that leads out of
// the root is refused too, and a parent file that is itself a link is refused
// the way a linked image is.
func Locate(root *project.Root, fileDir, ref string) (File, error) {
	if root == nil || root.FS == nil {
		return File{}, fmt.Errorf("parent %s: no documentation root to resolve it in", ref)
	}
	abs, err := filepath.Abs(filepath.Join(fileDir, filepath.FromSlash(ref)))
	if err != nil {
		return File{}, err
	}
	outside := fmt.Errorf("parent %s resolves outside the documentation root (%s); a parent must be within it",
		ref, root.Dir)
	rel, err := filepath.Rel(root.Dir, abs)
	if err != nil {
		return File{}, err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return File{}, outside
	}
	info, err := root.FS.Lstat(rel)
	if err != nil && strings.Contains(err.Error(), "escapes from parent") {
		// An escape only os.Root can see -- a symlinked intermediate directory
		// -- reads as "not found" otherwise, the same trap
		// internal/convert/images.go names; say so rather than sending the
		// author looking for a typo.
		return File{}, outside
	}
	if err != nil || info.IsDir() {
		return File{}, fmt.Errorf("parent file not found: %s", ref)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return File{}, fmt.Errorf("parent file is a symlink, not a regular file: %s", ref)
	}
	return File{Abs: abs, Rel: rel}, nil
}

// PageID reads the page id f's file declares, through root.FS. It returns ""
// with no error for a parent that declares none yet, which each caller words
// differently.
//
// Through pagemeta rather than the file's own frontmatter: the parent's
// coordinates may live in its pages: entry, and reading only the file reports
// a published parent as unpublished (#139).
func PageID(root *project.Root, f File, ref string) (string, error) {
	data, err := root.FS.ReadFile(f.Rel)
	if err != nil {
		return "", fmt.Errorf("parent %s: %w", ref, err)
	}
	pmf, err := frontmatter.Parse(f.Abs, string(data))
	if err != nil {
		return "", fmt.Errorf("parent %s: %w", ref, err)
	}
	key, _ := pagemeta.KeyFor(root, f.Abs)
	meta, err := pagemeta.Resolve(key, pmf, root)
	if err != nil {
		return "", fmt.Errorf("parent %s: %w", ref, err)
	}
	return strings.TrimSpace(meta.Fields["page_id"]), nil
}

// Resolve turns a parent reference into the id it names: an id is returned as
// given, and a ".md" path is located and read. A ".md" parent that declares no
// page id is an error naming both places one could be, and anything else that
// is not a number is refused before it reaches a request, where the API would
// answer with a 400 that says nothing useful (and a raw value would be pasted
// into a URL path).
func Resolve(root *project.Root, fileDir, ref string) (string, error) {
	if !IsFile(ref) {
		if !pageref.IsDigits(ref) {
			return "", fmt.Errorf("parent %q is not a page or folder id or a path to a .md file", ref)
		}
		return ref, nil
	}
	f, err := Locate(root, fileDir, ref)
	if err != nil {
		return "", err
	}
	id, err := PageID(root, f, ref)
	if err != nil {
		return "", err
	}
	if id != "" && !pageref.IsDigits(id) {
		return "", fmt.Errorf("parent %s: its page_id %q is not a number", ref, id)
	}
	if id == "" {
		return "", fmt.Errorf("parent not yet published (no page_id in the file or in %s): %s",
			project.Filename, ref)
	}
	return id, nil
}

// Target is what a parent id names: a page or a folder.
type Target struct {
	ID string
	// Kind is "page" or "folder".
	Kind string
	// Title is the page's or folder's title, for a message naming it.
	Title string
	// ParentID and ParentType are the target's own parent, "" at the top of a
	// space.
	ParentID, ParentType string
}

// Lookup reports what parentID names in spaceID, refusing a page or folder in
// another space and an id naming neither.
//
// A parent may be either kind, and the two live in separate v2 route families:
// a folder id answers every page route with 404, so finding nothing as a page
// proves nothing until the folder route has also been asked
// (docs/confluence/folders.md).
func Lookup(c *client.ConfluenceClient, parentID, spaceID string) (Target, error) {
	p, err := c.GetPageOrNil(parentID)
	if err != nil {
		return Target{}, err
	}
	if p != nil {
		if p.SpaceID != spaceID {
			return Target{}, fmt.Errorf("parent page %s is not in the target space", parentID)
		}
		return Target{ID: p.ID, Kind: "page", Title: p.Title, ParentID: p.ParentID, ParentType: p.ParentType}, nil
	}
	f, err := c.GetFolderOrNil(parentID)
	if err != nil {
		return Target{}, err
	}
	if f != nil {
		if f.SpaceID != spaceID {
			return Target{}, fmt.Errorf("parent folder %s is not in the target space", parentID)
		}
		return Target{ID: f.ID, Kind: "folder", Title: f.Title, ParentID: f.ParentID, ParentType: f.ParentType}, nil
	}
	// Neither kind, so "page" would be the wrong noun in the error.
	return Target{}, fmt.Errorf("parent %s not found: no page or folder has that id", parentID)
}

// maxDepth bounds the walk in Within. Confluence has no documented depth
// limit, and a cycle in parent links should be impossible, but a server that
// answered with one would otherwise loop forever.
const maxDepth = 100

// Within reports whether t is pageID itself or somewhere below it, by walking
// t's parents to the top of the space. Moving a page under such a target would
// make a loop, which Confluence refuses with a 400; asking first lets a dry run
// say so too.
//
// A walk of parent links rather than the v2 ancestors route, which answers in
// one request but needs read:content.metadata:confluence, a scope nothing else
// markfluence does needs (docs/confluence/api.md). This costs a request per
// level, only when a move is about to happen.
func Within(c *client.ConfluenceClient, t Target, pageID string) (bool, error) {
	id := t.ID
	parentID, parentType := t.ParentID, t.ParentType
	for range maxDepth {
		if id == pageID {
			return true, nil
		}
		if parentID == "" {
			return false, nil
		}
		// An ancestor that cannot be read -- a whiteboard or database, which
		// neither route answers for, or a page the caller cannot see -- ends
		// the walk with "no loop". That only weakens the preview: Confluence
		// still refuses a real loop with a 400 when the move is made.
		id = parentID
		switch parentType {
		case "folder":
			f, err := c.GetFolderOrNil(id)
			if err != nil {
				return false, err
			}
			if f == nil {
				return false, nil
			}
			parentID, parentType = f.ParentID, f.ParentType
		default:
			p, err := c.GetPageOrNil(id)
			if err != nil {
				return false, err
			}
			if p == nil {
				return false, nil
			}
			parentID, parentType = p.ParentID, p.ParentType
		}
	}
	return false, fmt.Errorf("parent %s: more than %d levels above it; giving up", t.ID, maxDepth)
}
