// Package convert turns a markdown body into Confluence storage-format HTML.
//
// It parses with goldmark (GFM) and renders through a custom node renderer that
// emits storage format, overriding the default HTML renderer for the nodes whose
// storage form differs from plain HTML.
package convert

import (
	"bytes"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// tocMacro replaces the <!-- confluence-toc --> placeholder.
const tocMacro = `<ac:structured-macro ac:name="toc" ac:schema-version="1" />`

// newMarkdown builds the goldmark instance: GFM for tables/strikethrough/
// task-lists/autolinks, the callout AST transformer, XHTML self-closing tags and
// raw-HTML passthrough (storage format is XHTML), and our storageRenderer
// registered at a priority below the default HTML (1000) and GFM table (500)
// renderers so its node handlers win.
func newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized(calloutTransformer{}, 100)),
		),
		goldmark.WithRendererOptions(
			html.WithXHTML(),
			html.WithUnsafe(),
			renderer.WithNodeRenderers(util.Prioritized(&storageRenderer{}, 100)),
		),
	)
}

// MdToConfluence converts a markdown file's body to Confluence storage-format
// HTML. baseURL and spaceKey build the Confluence URLs that internal document
// links point at; md.Filename locates sibling files for link/anchor rewriting
// and resolves image paths.
func MdToConfluence(md *frontmatter.MarkdownFile, baseURL, spaceKey string) (*ConfluencePage, error) {
	// Shield raw ac:/ri: storage tags so goldmark passes them through instead of
	// escaping them; restore them after rendering.
	shielded, unshield := shieldStorage(md.Body)
	var buf bytes.Buffer
	if err := newMarkdown().Convert([]byte(shielded), &buf); err != nil {
		return nil, err
	}
	out := unshield(buf.String())
	out = strings.ReplaceAll(out, "<!-- confluence-toc -->", tocMacro)
	return &ConfluencePage{
		HTML:        out,
		Attachments: []Attachment{},
		Broken:      []string{},
		Warnings:    []string{},
	}, nil
}
