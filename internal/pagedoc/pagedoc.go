// Package pagedoc renders a fetched Confluence page as a markdown document:
// frontmatter plus the converted body.
//
// One conversion, parameterized. Every command that renders a page goes through
// Options, so `read` and `export` cannot drift apart by accident -- only by
// argument, and the arguments are the page's position in whatever is being
// written and the form its parent takes. For a page at the top level of that
// tree, which is what `read` prints and what a single-page export writes, the
// two are byte-identical.
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
// pageDir is where the page's file sits relative to the root of what is being
// written, in slash form -- "" for a file at that root, which is what `read`
// passes and what a single-page export produces. See convert.StorageOptions.
//
// The page must have been fetched with its body (GetPageBodyOrNil).
func Render(c *client.ConfluenceClient, page *client.Page, pageDir string) (Doc, error) {
	body, err := convert.StorageToMarkdown(page.Body.Storage.Value, Options(c, page, pageDir))
	if err != nil {
		return Doc{}, err
	}
	return Doc{Frontmatter: Frontmatter(c, page), Body: body}, nil
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
// One conversion, parameterized. read and export produce identical markdown for
// identical arguments; what differs between them is the arguments -- read has
// no tree, so it passes the empty position and the page's parent id, while a
// tree export passes the page's directory and a parent path.
func Options(c *client.ConfluenceClient, page *client.Page, pageDir string) convert.StorageOptions {
	return convert.StorageOptions{
		Sources:   Sources(c, page),
		PageLinks: PageLinks(c, page),
		PageDir:   pageDir,
		// Where an attachment with no recorded path is placed: the directory
		// named after the page, beside the page's own file. Computed here rather
		// than in the converter, which has no business knowing how a title
		// becomes a directory name, and computed once so that every command
		// placing such an attachment agrees with the markdown that points at it.
		AttachmentDir: path.Join(pageDir, pageslug.For(page.Title, page.ID)),
		// The site, never the gateway: these URLs are published into a page.
		SiteURL: c.SiteURL(),
	}
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
// references skips the lookup entirely, and a failed lookup returns nil so the
// converter falls back to decoding names -- a read is worth completing without
// it, the same way a failed page-width read is tolerated in Frontmatter.
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
// page_id, and (best-effort) page_width. parent is "null" for a top-level page,
// else the parent's page id (both free from the fetched page). A failed
// page_width read is tolerated -- the field is simply omitted rather than
// failing the render.
func Frontmatter(c *client.ConfluenceClient, page *client.Page) string {
	parent := page.ParentID
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
