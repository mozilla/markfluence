package export

// Where every page and folder in a walked subtree goes on disk.
//
// The Confluence hierarchy is mirrored: a page becomes <slug>.md and gains a
// <slug>/ beside it holding its children and its own unrecorded attachments; a
// folder becomes a directory with no file of its own. Every path an export
// writes comes from here, so the rules exist in one place -- _plans/029
// §Layout.

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mozilla/markfluence/internal/pageslug"
	"github.com/mozilla/markfluence/internal/pagetree"
)

// placement is where one node's file and children live, relative to dest.
type placement struct {
	// dir holds this node's own file; "" is dest itself.
	dir string
	// childDir holds this node's children and its unrecorded attachments: dir
	// plus its slug. A folder has one of these and nothing else.
	childDir string
	// file is the markdown file, relative to dest. Empty for a folder.
	file string
	// parentFile is what this node's parent: should say: a path to the parent's
	// own .md, relative to this node's dir. Empty when the parent is not an
	// exported page -- the root's own parent, or a folder -- and the id stands.
	parentFile string
	// folder marks a directory-only node.
	folder bool
}

// layout assigns a placement to the export root and to everything under it,
// and reports the slug collisions it disambiguated.
//
// The root is described separately because pagetree walks what is *under* a
// node, so the named page is not among nodes. An empty rootID means there is no
// root file at all -- a folder or a whole space -- and the walk's top level
// starts at dest.
func layout(rootTitle, rootID string, nodes []pagetree.Node) (map[string]placement, []string) {
	out := make(map[string]placement, len(nodes)+1)
	var warnings []string

	// Grouped by parent, because siblings are the only scope in which two names
	// can collide once the hierarchy is mirrored.
	byParent := map[string][]pagetree.Node{}
	for _, n := range nodes {
		byParent[n.ParentID] = append(byParent[n.ParentID], n)
	}

	top := ""
	if rootID != "" {
		out[rootID] = placement{
			childDir: pageslug.For(rootTitle, rootID),
			file:     pageslug.Filename(rootTitle, rootID),
		}
		top = out[rootID].childDir
	}

	// Depth-first, so a parent is placed before the children that need its
	// directory. The walk's own visited guard means this cannot cycle.
	var assign func(parentID, dir string)
	assign = func(parentID, dir string) {
		children := byParent[parentID]
		slugs, w := siblingSlugs(children)
		warnings = append(warnings, w...)
		for _, n := range children {
			p := placement{
				dir:      dir,
				childDir: path.Join(dir, slugs[n.ID]),
				folder:   n.Type == pagetree.TypeFolder,
			}
			if !p.folder {
				p.file = path.Join(dir, slugs[n.ID]+".md")
				if parent, ok := out[parentID]; ok && parent.file != "" {
					p.parentFile = relativeTo(dir, parent.file)
				}
			}
			out[n.ID] = p
			assign(n.ID, p.childDir)
		}
	}
	assign(rootID, top)

	sort.Strings(warnings) // map iteration order must not reach the output
	return out, warnings
}

// siblingSlugs names each node in one directory, appending the page id to every
// member of a group that slugs the same.
//
// Every member, not just the ones seen later: suffixing the second arrival
// would make a filename depend on walk order, and this way no member holds a
// privileged unsuffixed name. Refusing outright was the earlier design and is
// worse -- a space nobody can retitle would be unexportable over a punctuation
// variant, and an exported filename is ergonomic where identity lives in
// page_id (L8). See _plans/029 §Collisions.
//
// The namespace covers page files, page directories and folder directories
// together, since a page "Team" and a folder "Team" both want team/.
func siblingSlugs(nodes []pagetree.Node) (map[string]string, []string) {
	groups := map[string][]pagetree.Node{}
	var order []string
	for _, n := range nodes {
		s := pageslug.For(n.Title, n.ID)
		if _, seen := groups[s]; !seen {
			order = append(order, s)
		}
		groups[s] = append(groups[s], n)
	}

	out := make(map[string]string, len(nodes))
	var warnings []string
	for _, s := range order {
		group := groups[s]
		if len(group) == 1 {
			out[group[0].ID] = s
			continue
		}
		names := make([]string, 0, len(group))
		for _, n := range group {
			out[n.ID] = s + "-" + n.ID
			names = append(names, fmt.Sprintf("%q", n.Title))
		}
		warnings = append(warnings, fmt.Sprintf(
			"%s all slug to %q; writing them with their page ids appended",
			strings.Join(names, ", "), s))
	}
	return out, warnings
}

// relativeTo expresses target -- a path relative to dest -- as a path from dir.
// Both are slash form; package path has no Rel, so this is filepath.Rel
// bracketed by the conversions, as internal/convert does for the same reason on
// an attachment destination.
func relativeTo(dir, target string) string {
	if dir == "" {
		return target
	}
	rel, err := filepath.Rel(filepath.FromSlash(dir), filepath.FromSlash(target))
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
}
