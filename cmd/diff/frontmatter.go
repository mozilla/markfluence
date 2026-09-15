package diff

// The frontmatter half: a per-field comparison of values, reported separately
// from the body diff because a value difference is a one-line fact rather than
// a hunk, and because only a report can carry provenance.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/labels"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/project"
)

// difference is one field the file and the page disagree about.
type difference struct {
	// Field is the frontmatter key.
	Field string
	// Confluence is the page's value, meaningless when !Comparable.
	Confluence string
	// Local is the file's effective value, as written.
	Local string
	// Source names the location that supplied Local, so a reader knows which
	// file to edit.
	Source string
	// Comparable is false when the page's side could not be fetched. Such a
	// field is reported but does not count as a difference: claiming the file
	// and the page disagree when nobody could ask the page is worse than
	// saying nothing.
	Comparable bool
	// Note explains a comparison that is not a plain string match -- a parent
	// resolved from a path to an id, or why a field is uncomparable.
	Note string
}

// fieldOrder is the order differences are reported in, which is frontmatter's
// own canonical order rather than the order they happen to be computed in: two
// runs against the same file must produce the same report.
//
// page_id is absent deliberately. It is how the page was found, so it cannot
// differ; it names the page in the report's header instead.
var fieldOrder = []string{"title", "space", "parent", "page_width", "labels"}

// compareMetadata compares every field the local side declares against the
// page, and returns the differences in fieldOrder plus any warnings raised on
// the way.
//
// Only declared fields are compared, which is the rule that makes this report
// mean something: `update` does not touch a field the file does not declare, so
// a field the file is silent about has no difference to have. Absent means
// untouched for page_width and labels (L9), absent means "keep the live title"
// for title, and absent means "do not move the page" for space and parent.
func compareMetadata(
	c *client.ConfluenceClient, page *client.Page,
	meta pagemeta.Resolved, root *project.Root, fileDir string,
) ([]difference, []string) {
	var out []difference
	var warnings []string

	add := func(d difference) {
		if d.Comparable && d.Confluence == d.Local {
			return
		}
		out = append(out, d)
	}

	if local, ok := declared(meta, "title"); ok {
		add(difference{
			Field: "title", Confluence: page.Title, Local: local,
			Source: sourceLabel(meta.Origin["title"]), Comparable: true,
		})
	}

	if local, ok := declared(meta, "space"); ok {
		add(difference{
			Field: "space", Confluence: client.SpaceKeyFromWebUI(page.Links.WebUI), Local: local,
			Source: sourceLabel(meta.Origin["space"]), Comparable: true,
			// Named here rather than left implicit: `update` sends no spaceId,
			// so unlike a title this is a disagreement it will not reconcile.
			Note: "update does not move a page between spaces",
		})
	}

	if local, ok := declared(meta, "parent"); ok {
		add(parentDifference(page, meta, root, fileDir, local))
	}

	// The warnings are collected whether or not there is a row, and that is
	// load-bearing: a labels: value markfluence refuses yields *no* row, and
	// gating the warning on the row made `diff` report "in sync" and exit 0
	// about a file that cannot be published at all.
	d, w, ok := widthDifference(c, page, meta, root)
	warnings = append(warnings, w...)
	if ok {
		add(d)
	}
	d, w, ok = labelDifference(c, page, meta)
	warnings = append(warnings, w...)
	if ok {
		add(d)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return indexOf(out[i].Field) < indexOf(out[j].Field)
	})
	return out, warnings
}

// declared reads a field's effective local value, reporting whether it says
// anything at all. Every null spelling already reads as "" in the map the
// frontmatter reader produces, so a blank value is silence.
func declared(meta pagemeta.Resolved, field string) (string, bool) {
	v := strings.TrimSpace(meta.Fields[field])
	return v, v != ""
}

// parentDifference compares a parent, resolving a ".md" reference to the page
// id it names before comparing.
//
// A file that came out of an export tree spells its parent as a relative path
// to the parent's own .md while the live page's parent is an id, so comparing
// the spellings would report a difference on every such file. This is the
// resolution `create` already does for a .md parent, minus the refusals that
// exist to protect a publish: nothing is written here, so an unreadable parent
// is reported rather than fatal.
func parentDifference(
	page *client.Page, meta pagemeta.Resolved, root *project.Root, fileDir, local string,
) difference {
	d := difference{
		Field: "parent", Confluence: page.ParentID, Local: local,
		Source: sourceLabel(meta.Origin["parent"]), Comparable: true,
		Note: "update does not move a page to a new parent",
	}
	if !strings.HasSuffix(local, ".md") {
		return d
	}

	id, err := parentPageID(root, fileDir, local)
	switch {
	case err != nil:
		d.Note = err.Error()
	case id == "":
		d.Note = "that file declares no page_id, so it names no page yet"
	default:
		// Compare the ids; keep showing the path, which is what the file says
		// and what a reader would go and edit.
		d.Note = "resolves to page " + id
		if id == page.ParentID {
			// Agreement. Reported by returning a difference whose sides match,
			// which add() drops -- so the caller stays the only place that
			// decides what counts.
			d.Confluence, d.Local = id, id
		} else {
			d.Local = fmt.Sprintf("%s (page %s)", local, id)
		}
	}
	return d
}

// parentPageID reads the page id a parent .md reference names, through the
// root's os.Root so the read stays inside the documentation root.
func parentPageID(root *project.Root, fileDir, ref string) (string, error) {
	if root == nil || root.FS == nil {
		return "", fmt.Errorf("no documentation root, so %s cannot be resolved", ref)
	}
	abs := filepath.Join(fileDir, filepath.FromSlash(ref))
	rel, err := filepath.Rel(root.Dir, abs)
	if err != nil {
		return "", fmt.Errorf("%s cannot be resolved", ref)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside the documentation root", ref)
	}
	data, err := root.FS.ReadFile(rel)
	if err != nil {
		return "", fmt.Errorf("%s cannot be read", ref)
	}
	pmf, err := frontmatter.Parse(abs, string(data))
	if err != nil {
		return "", fmt.Errorf("%s has unreadable frontmatter", ref)
	}
	// Through pagemeta, not pmf.PageID(): the parent's coordinates may live in
	// its own pages: entry, and reading only the file would report a published
	// parent as unpublished.
	key, _ := pagemeta.KeyFor(root, abs)
	pMeta, err := pagemeta.Resolve(key, pmf, root)
	if err != nil {
		return "", fmt.Errorf("%s: %v", ref, err)
	}
	return strings.TrimSpace(pMeta.Fields["page_id"]), nil
}

// widthDifference compares the page width, reporting ok=false when nothing
// declares one.
//
// The local side is the same two-step `update` applies: the file's own
// page_width -- from its frontmatter or its pages: entry -- then the project
// file's project-wide default, which is a real declaration (#100) and makes
// `update` assert a width on a file that declares none.
func widthDifference(
	c *client.ConfluenceClient, page *client.Page, meta pagemeta.Resolved, root *project.Root,
) (difference, []string, bool) {
	local, source, ok := declaredWidth(meta, root)
	if !ok {
		return difference{}, nil, false
	}
	d := difference{Field: "page_width", Local: local, Source: source, Comparable: true}

	live, explicit, err := pagewidth.Read(c, page.ID)
	if err != nil {
		// Uncomparable, not different. read/export omit the field on a failed
		// fetch, which is right for them and actively misleading here: the
		// declared width may well be the live one.
		d.Comparable = false
		d.Note = "the page's width could not be read"
		return d, []string{"page_width could not be compared: " + err.Error()}, true
	}
	d.Confluence = string(live)
	if !explicit {
		// The page carries no width property at all, so this is what it renders
		// as rather than something it says. info draws the same distinction
		// (page_width.default), and without it a file declaring narrow reads as
		// agreeing with a page that has never been given a width.
		d.Note = "the page states no width; narrow is the site default"
	}
	return d, nil, true
}

// declaredWidth resolves the width the file asserts, and names where it came
// from.
//
// The two-step is a third copy of a rule that also lives in update's
// resolveWidth and check's project-default lint. It cannot be shared from
// internal/pagewidth, which cannot import internal/project (pagewidth ->
// client -> project), and a new package for six lines would be worse. An
// invalid value is left to `check` and `update` to report: this command's job
// is the comparison, and refusing to diff a file over a width typo would hide
// every other difference in it.
func declaredWidth(meta pagemeta.Resolved, root *project.Root) (value, source string, ok bool) {
	if _, declaredHere := declared(meta, "page_width"); declaredHere {
		w, err := pagewidth.Declared(meta.Fields)
		if err != nil {
			return "", "", false
		}
		return string(w), sourceLabel(meta.Origin["page_width"]), true
	}
	if root != nil && root.Config.PageWidth != "" {
		w, err := pagewidth.Declared(map[string]string{"page_width": root.Config.PageWidth})
		if err != nil {
			return "", "", false
		}
		// A distinct label from a pages: entry's: both live in
		// markfluence.yaml, but one is a top-level setting and the other is a
		// field inside this file's entry, and a reader is being told where to
		// go and edit.
		return string(w), project.Filename + " (project default)", true
	}
	return "", "", false
}

// labelDifference compares the managed labels, reporting ok=false when the file
// declares none.
//
// Only global: labels are managed, so only those are compared -- a my: or
// team: label has no frontmatter spelling and is nobody's difference.
func labelDifference(
	c *client.ConfluenceClient, page *client.Page, meta pagemeta.Resolved,
) (difference, []string, bool) {
	set, err := labels.Declared(meta.Lists, meta.Fields)
	if err != nil {
		// A labels: value markfluence refuses -- a scalar, a name Confluence
		// would split on. `check` and `update` report it properly; here there
		// is nothing to compare, so the warning is the whole report and must
		// not be dropped.
		return difference{}, []string{"labels could not be compared: " + err.Error()}, false
	}
	if !set.Declared {
		return difference{}, nil, false
	}
	// Declared's own warnings travel too: a label it case-repaired is compared
	// as the repaired name, which is what would publish, and saying so is how
	// a reader understands a row that looks like it agrees with their file.
	warnings := append([]string(nil), set.Warnings...)

	d := difference{
		Field: "labels", Local: renderList(set.Names),
		Source: sourceLabel(meta.Origin["labels"]), Comparable: true,
	}
	live, err := labels.Read(c, page.ID)
	if err != nil {
		d.Comparable = false
		d.Note = "the page's labels could not be read"
		return d, append(warnings, "labels could not be compared: "+err.Error()), true
	}
	// Compared as a set: Confluence has no label order, so a reordering is not
	// a difference and cannot reach the page.
	d.Confluence = renderList(labels.Global(live))
	return d, warnings, true
}

// renderList spells a declared list the way frontmatter's flow style does, and
// sorts it, so a set comparison is a string comparison and the report is
// stable.
func renderList(names []string) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return "[" + strings.Join(sorted, ", ") + "]"
}

// sourceLabel spells a provenance for a reader: the file to go and edit.
//
// FromBoth is not collapsed the way pagemeta.MetadataSource collapses it. Here
// it is the most useful answer of the three: correcting a field both locations
// supply means editing both files, and being told only one of them is how a
// value comes back on the next run.
func sourceLabel(s pagemeta.Source) string {
	switch s {
	case pagemeta.FromFrontmatter:
		return "frontmatter"
	case pagemeta.FromManifest:
		return project.Filename
	case pagemeta.FromBoth:
		return "frontmatter and " + project.Filename
	default:
		return ""
	}
}

func indexOf(field string) int {
	for i, f := range fieldOrder {
		if f == field {
			return i
		}
	}
	return len(fieldOrder)
}
