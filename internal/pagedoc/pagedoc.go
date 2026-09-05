// Package pagedoc renders a fetched Confluence page as a markdown document:
// frontmatter plus the converted body.
//
// One conversion, parameterized. Every command that renders a page goes through
// Options, so `read` and `export` cannot drift apart by accident -- only by
// argument, and the argument is where the page sits in whatever is being
// written. For a page at the top level of that tree, which is what `read`
// prints and what a single-page export writes, the two are byte-identical.
//
// It needs a client (page width, attachment list) and so cannot live in
// internal/convert, which is deliberately client-free; that is why
// StorageToMarkdown takes a sources map rather than fetching one.
package pagedoc

import (
	"fmt"
	"path"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pageslug"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/ui"
)

// Placement is where a page is being written and what that implies for its
// frontmatter. The zero value is "on its own": no tree, no directory, parent
// taken from the page itself -- which is what `read` prints and what a
// single-page export writes.
type Placement struct {
	// Dir is where the page's file sits relative to the root of what is being
	// written, in slash form. "" is that root.
	Dir string

	// AttachmentDir overrides where an attachment with no recorded path is
	// placed. Empty derives it from the page's own title, which is right for a
	// page standing alone.
	//
	// A tree sets it, because a title is not enough there: two siblings whose
	// titles slug the same are written to directories disambiguated by page id,
	// and deriving the directory here would put both pages' attachments back in
	// one place -- the very collision page-scoping exists to prevent. The caller
	// that named the directories is the only one that knows.
	AttachmentDir string

	// Attachments is the page's attachment list, when the caller has already
	// fetched it. Options otherwise fetches its own, which for a tree export is
	// a second listing of every page on top of the walk's own requests.
	Attachments []client.Attachment

	// Parent overrides the parent: frontmatter field. Empty derives it from the
	// page, which yields its parent id or "null".
	//
	// A tree export sets it to a relative path to the parent's own .md file, so
	// the tree can be published into fresh pages; create resolves such a path
	// against the referring file's directory. It stays an id for the export
	// root, whose parent is outside the tree, and for a page whose parent is a
	// folder, which has no file to point at.
	Parent string
}

// Doc is a page rendered as markdown. Frontmatter and Body are separate because
// read prints them together while export writes them to a file, and because a
// caller may want the body alone.
type Doc struct {
	Frontmatter string
	Body        string
}

// String is the full document: frontmatter, a blank line, then the body.
func (d Doc) String() string { return d.Frontmatter + "\n" + d.Body }

// Render converts a page's storage body to markdown and builds its frontmatter.
//
// See Placement for what pl carries.
//
// The page must have been fetched with its body (GetPageBodyOrNil).
func Render(c *client.ConfluenceClient, page *client.Page, pl Placement) (Doc, error) {
	body, err := convert.StorageToMarkdown(page.Body.Storage.Value, Options(c, page, pl))
	if err != nil {
		return Doc{}, err
	}
	return Doc{Frontmatter: Frontmatter(c, page, pl.Parent), Body: body}, nil
}

// Options assembles what the converter cannot fetch for itself: the attachment
// source paths, the resolved <ac:link> page URLs, the page's position, and the
// site base.
//
// Every command that renders a page goes through it rather than assembling its
// own, which is the same reason Render exists: options built in two places are
// options that can disagree, and here a disagreement means an attachment
// written to a path the markdown does not point at.
//
// One conversion, parameterized by where the page sits. read and export produce
// identical markdown for the same position; read has no tree, so it passes the
// empty one.
func Options(c *client.ConfluenceClient, page *client.Page, pl Placement) convert.StorageOptions {
	return convert.StorageOptions{
		Sources:   pl.sources(c, page),
		PageLinks: PageLinks(c, page),
		PageDir:   pl.Dir,
		// Where an attachment with no recorded path is placed: the directory
		// named after the page, beside the page's own file. Computed here rather
		// than in the converter, which has no business knowing how a title
		// becomes a directory name, and computed once so that every command
		// placing such an attachment agrees with the markdown that points at it.
		AttachmentDir: AttachmentDirFor(page, pl),
		// The site, never the gateway: these URLs are published into a page.
		SiteURL: c.SiteURL(),
	}
}

// sources is the attachment name-to-path map, from the caller's own listing
// when it has one.
func (pl Placement) sources(c *client.ConfluenceClient, page *client.Page) map[string]string {
	if pl.Attachments != nil {
		return SourcesFrom(pl.Attachments)
	}
	return Sources(c, page)
}

// AttachmentDirFor is where this placement puts an attachment with no recorded
// path: the directory the placement names, or one derived from the page when it
// names none.
//
// Exported because the write side needs the identical answer -- the markdown
// destination and attachfile.Options.Dir are the same decision, and computing
// it twice is how they drift.
func AttachmentDirFor(page *client.Page, pl Placement) string {
	return pl.attachmentDir(page)
}

func (pl Placement) attachmentDir(page *client.Page) string {
	if pl.AttachmentDir != "" {
		return pl.AttachmentDir
	}
	return AttachmentDir(page, pl.Dir)
}

// AttachmentDir is where an attachment with no recorded path belongs when
// nothing else has decided: the directory named after the page, beside the
// page's own file at pageDir.
//
// Exported because both sides of the same decision need it and must not compute
// it twice: the markdown that points at the attachment (through Options) and
// the write that puts it there (attachfile.Options.Dir). A caller that renders
// a page and writes its attachments passes this to both.
func AttachmentDir(page *client.Page, pageDir string) string {
	return path.Join(pageDir, pageslug.For(page.Title, page.ID))
}

// PageLinks maps each page an <ac:link> in this body points at to its absolute
// URL. Confluence names a link target by title and never by id, so every one of
// them costs a lookup -- which is why the converter asks for the list rather
// than the whole body being scanned speculatively.
//
// Best-effort in the same way Sources is: a body with no page link makes no
// request at all, and a target that fails to resolve is simply left out, which
// the converter renders as raw storage rather than a link with no destination.
// A read is worth completing without it.
func PageLinks(c *client.ConfluenceClient, page *client.Page) map[convert.PageLinkTarget]string {
	targets := convert.PageLinkTargets(page.Body.Storage.Value)
	if len(targets) == 0 {
		return nil
	}
	// A link naming no space means the space of the page it sits on.
	pageSpace := client.SpaceKeyFromWebUI(page.Links.WebUI)

	out := map[convert.PageLinkTarget]string{}
	spaceIDs := map[string]string{} // space key -> id, resolved once each
	for _, t := range targets {
		key := t.SpaceKey
		if key == "" {
			key = pageSpace
		}
		if key == "" {
			// Nothing to scope the title to. A site-wide search would answer
			// with whichever same-titled page sorted first, and a wrong target
			// is worse than none.
			continue
		}
		spaceID, ok := spaceIDs[key]
		if !ok {
			var err error
			if spaceID, err = c.ResolveSpaceID(key); err != nil {
				ui.Debug(fmt.Sprintf("resolving space %s for a page link: %v", key, err))
			}
			spaceIDs[key] = spaceID
		}
		if spaceID == "" {
			continue
		}
		if url := pageURLByTitle(c, t.Title, spaceID); url != "" {
			out[t] = url
		}
	}
	return out
}

// pageURLByTitle resolves one title to a page URL, preferring a current page
// over an archived one of the same title. A title is unique among current pages
// in a space, so there is at most one of those.
func pageURLByTitle(c *client.ConfluenceClient, title, spaceID string) string {
	matches, err := c.PagesByTitle(title, spaceID)
	if err != nil {
		ui.Debug(fmt.Sprintf("resolving page link %q: %v", title, err))
		return ""
	}
	url := ""
	for _, m := range matches {
		if m.Status == client.StatusCurrent {
			return m.URL
		}
		if url == "" {
			url = m.URL
		}
	}
	return url
}

// Sources maps each attachment name on the page to the markdown image path it
// was published from, letting the converter restore an image's original
// location exactly rather than inferring it from the attachment name.
//
// It is an optimization, not a requirement: a page with no attachment
// references skips the lookup entirely, and a failed lookup returns nil, which
// leaves every attachment looking unrecorded -- so the markdown points into the
// page's own directory, which is where an unrecorded one is placed anyway. A
// read is worth completing without it, the same way a failed page-width read is
// tolerated in Frontmatter.
func Sources(c *client.ConfluenceClient, page *client.Page) map[string]string {
	if !strings.Contains(page.Body.Storage.Value, "<ri:attachment") {
		return nil
	}
	atts, err := c.ListAttachments(page.ID)
	if err != nil {
		ui.Debug(fmt.Sprintf("listing attachments for %s: %v", page.ID, err))
		return nil
	}
	return SourcesFrom(atts)
}

// SourcesFrom builds the name-to-source map from an already-fetched attachment
// list, for a caller that needs the attachments for its own reasons and should
// not fetch them twice.
func SourcesFrom(atts []client.Attachment) map[string]string {
	sources := map[string]string{}
	for _, a := range atts {
		if src := a.Meta().Source; src != "" {
			sources[a.Title] = src
		}
	}
	return sources
}

// Frontmatter builds the YAML frontmatter prefix: title, space, parent,
// page_id, and (best-effort) page_width. A failed page_width read is tolerated
// -- the field is simply omitted rather than failing the render.
//
// parentOverride replaces the parent field when set; empty derives it from the
// page, which is "null" for a top-level page and the parent's id otherwise
// (both free from the fetched page). See Placement.Parent.
func Frontmatter(c *client.ConfluenceClient, page *client.Page, parentOverride string) string {
	parent := parentOverride
	if parent == "" {
		parent = page.ParentID
	}
	if parent == "" {
		parent = "null"
	}
	width := ""
	if w, _, err := pagewidth.Read(c, page.ID); err == nil {
		width = string(w)
	}
	return RenderFrontmatter(page.Title, client.SpaceKeyFromWebUI(page.Links.WebUI), parent, page.ID, width)
}

// RenderFrontmatter assembles the frontmatter block from resolved field values,
// omitting space/parent/page_width when empty. UpdateField emits them in the
// canonical order and auto-quotes values as needed.
func RenderFrontmatter(title, space, parent, pageID, width string) string {
	fm := ""
	fm = frontmatter.UpdateField(fm, "title", title, "")
	if space != "" {
		fm = frontmatter.UpdateField(fm, "space", space, "")
	}
	if parent != "" {
		fm = frontmatter.UpdateField(fm, "parent", parent, "")
	}
	fm = frontmatter.UpdateField(fm, "page_id", pageID, "")
	if width != "" {
		fm = frontmatter.UpdateField(fm, "page_width", width, "")
	}
	return fm
}
