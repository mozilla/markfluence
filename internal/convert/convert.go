// Package convert turns a markdown body into Confluence storage-format HTML.
//
// It parses with goldmark (GFM) and renders through a custom node renderer that
// emits storage format, overriding the default HTML renderer for the nodes whose
// storage form differs from plain HTML.
package convert

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

const (
	// tocToken is replaced by the Confluence table-of-contents macro.
	tocToken = "<!-- confluence-toc -->"
	tocMacro = `<ac:structured-macro ac:name="toc" ac:schema-version="1" />`
	// versionToken is replaced by the build version stamp passed to MdToConfluence.
	versionToken = "<!-- markfluence-version -->"
)

// newMarkdown builds the goldmark instance: GFM for tables/strikethrough/
// task-lists/autolinks, the callout and table-cell-background AST transformers,
// XHTML self-closing tags and raw-HTML passthrough (storage format is XHTML), and
// our storageRenderer registered at a priority below the default HTML (1000) and
// GFM table (500) renderers so its node handlers win.
func newMarkdown(r *storageRenderer) goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(
				util.Prioritized(calloutTransformer{}, 100),
				util.Prioritized(tableCellBGTransformer{r: r}, 101),
			),
		),
		goldmark.WithRendererOptions(
			html.WithXHTML(),
			html.WithUnsafe(),
			renderer.WithNodeRenderers(util.Prioritized(r, 100)),
		),
	)
}

// MdToConfluence converts a markdown file's body to Confluence storage-format
// HTML. baseURL and spaceKey build the Confluence URLs that internal document
// links point at; md.Filename locates sibling files for link/anchor rewriting
// and resolves image paths. root bounds which images and parent references may
// be read (S1/S2) and is what an image's recorded Source is relative to; the
// caller discovers it (per-file, via internal/project) rather than
// MdToConfluence assuming the working directory. index is the tree-wide
// link/anchor index for root -- built once and shared across every file
// converted under it (internal/linkindex.Build), not rebuilt here per
// conversion. version is the build stamp substituted for the
// <!-- markfluence-version --> token.
func MdToConfluence(
	md *frontmatter.MarkdownFile, root *project.Root, index *linkindex.Index, baseURL, spaceKey, version string,
) (*ConfluencePage, error) {
	// Shield raw ac:/ri: storage tags so goldmark passes them through instead of
	// escaping them; restore them after rendering.
	shielded, unshield := shieldStorage(md.Body)
	dir := filepath.Dir(md.Filename)
	r := &storageRenderer{
		baseDir:         dir,
		root:            root,
		currentBasename: filepath.Base(md.Filename),
		currentDocKey:   DocKeyFor(root, md.Filename),
		baseURL:         baseURL,
		spaceKey:        spaceKey,
		index:           index,
		// goldmark parses md.Body, not md.Content -- every position it
		// reports is relative to the file with its frontmatter block already
		// stripped. lineOffset is that block's own line count, added back so
		// a reported line matches what a reader sees opening the file, not
		// what the parser sees after Extract already removed the header.
		lineOffset: strings.Count(md.Content[:len(md.Content)-len(md.Body)], "\n"),
		// Scanned from the unshielded body: after shielding, the tag names are
		// sentinels and the attribute would no longer match.
		pastedNames: pastedAttachmentNames(md.Body),
	}
	var buf bytes.Buffer
	if err := newMarkdown(r).Convert([]byte(shielded), &buf); err != nil {
		return nil, err
	}
	out := unshield(buf.String())
	out = strings.ReplaceAll(out, tocToken, tocMacro)
	out = strings.ReplaceAll(out, versionToken, version)

	page := &ConfluencePage{
		HTML:        out,
		Attachments: r.attachments,
		Broken:      r.broken,
		Warnings:    r.warnings,
	}
	// Emit empty JSON arrays (not null) for the absent cases.
	if page.Attachments == nil {
		page.Attachments = []Attachment{}
	}
	if page.Broken == nil {
		page.Broken = []string{}
	}
	if page.Warnings == nil {
		page.Warnings = []string{}
	}
	return page, nil
}

// DocKeyFor resolves filename's own path to the key the link index would use
// for it: relative to root, slash-separated. filename is always at or under
// its own root by construction (root was discovered from this same file's
// directory), so this cannot escape the way an arbitrary link destination
// could; the fallback (filename's bare basename) only matters if filepath.Abs
// or filepath.Rel itself fails, which needs an unreadable working directory.
//
// Exported so a caller seeding the link index directly -- create's reserve
// phase, injecting an id that exists only in memory before publish -- computes
// the identical key MdToConfluence would, rather than a second copy that could
// silently drift from it.
func DocKeyFor(root *project.Root, filename string) string {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return filepath.Base(filename)
	}
	key, ok := rootRelativeKey(root, abs)
	if !ok {
		return filepath.Base(filename)
	}
	return key
}

// rootRelativeKey converts an already-absolute path into the root-relative,
// slash-separated form the link index is keyed by -- the one step DocKeyFor
// and (*storageRenderer).resolveDocKey (links.go) share; each computes abs
// its own way and picks its own fallback on failure, but not this one.
func rootRelativeKey(root *project.Root, abs string) (key string, ok bool) {
	rel, err := filepath.Rel(root.Dir, abs)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
