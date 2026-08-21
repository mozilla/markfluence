// Package pagedoc renders a fetched Confluence page as a markdown document:
// frontmatter plus the converted body.
//
// It exists so `read` and `export` emit byte-identical markdown -- that they
// agree is the property export rests on, since an exported tree is meant to be
// the same thing `read` prints. It needs a client (page width, attachment list)
// and so cannot live in internal/convert, which is deliberately client-free;
// that is why StorageToMarkdown takes a sources map rather than fetching one.
package pagedoc

import (
	"fmt"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
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
// The page must have been fetched with its body (GetPageBodyOrNil).
func Render(c *client.ConfluenceClient, page *client.Page) (Doc, error) {
	body, err := convert.StorageToMarkdown(page.Body.Storage.Value, Options(c, page))
	if err != nil {
		return Doc{}, err
	}
	return Doc{Frontmatter: Frontmatter(c, page), Body: body}, nil
}

// Options assembles what the converter cannot fetch for itself: the attachment
// source paths, the resolved <ac:link> page URLs, and the site base.
//
// Both read and export go through it rather than assembling their own, which is
// the same reason Render exists -- the two must emit byte-identical markdown,
// and options built in two places are options that can disagree.
func Options(c *client.ConfluenceClient, page *client.Page) convert.StorageOptions {
	return convert.StorageOptions{
		Sources:   Sources(c, page),
		PageLinks: PageLinks(c, page),
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
