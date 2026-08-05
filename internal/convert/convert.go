// Package convert turns a markdown body into Confluence storage-format HTML.
//
// It parses with goldmark (GFM) and renders through a custom node renderer that
// emits storage format, overriding the default HTML renderer for the nodes whose
// storage form differs from plain HTML.
package convert

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
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
// and resolves image paths. version is the build stamp substituted for the
// <!-- markfluence-version --> token.
func MdToConfluence(md *frontmatter.MarkdownFile, baseURL, spaceKey, version string) (*ConfluencePage, error) {
	// Shield raw ac:/ri: storage tags so goldmark passes them through instead of
	// escaping them; restore them after rendering.
	shielded, unshield := shieldStorage(md.Body)
	dir := filepath.Dir(md.Filename)
	// The documentation root is the working directory: markfluence is run from
	// the root of a documentation tree. An unresolvable cwd disables the check
	// rather than failing the conversion.
	root, err := os.Getwd()
	if err != nil {
		root = ""
	}
	r := &storageRenderer{
		baseDir:         dir,
		root:            root,
		currentBasename: filepath.Base(md.Filename),
		baseURL:         baseURL,
		spaceKey:        spaceKey,
		anchorMap:       buildAnchorMap(dir),
		pageMap:         buildPageMap(dir),
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
