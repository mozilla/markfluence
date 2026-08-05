package convert

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// storageRenderer overrides the default goldmark HTML renderer for the nodes
// whose Confluence storage form differs from plain HTML. Node kinds it does not
// register fall through to the default HTML renderer.
//
// It also accumulates the side effects of image rendering: local attachments to
// upload, broken-image messages, and image-property warnings. A fresh renderer is
// used per conversion, so this state does not leak between documents.
type storageRenderer struct {
	baseDir string

	// root bounds which images may be published: the documentation root, which
	// markfluence is expected to be run from. Empty disables the check.
	root string

	// Link/anchor rewriting context, populated per conversion.
	currentBasename string
	baseURL         string
	spaceKey        string
	anchorMap       map[string]map[string]string // filename -> github slug -> confluence slug
	pageMap         map[string]pageEntry         // filename -> page id + title

	// Image side effects.
	attachments []Attachment
	broken      []string
	warnings    []string
	seen        map[string]bool
}

// RegisterFuncs registers the node handlers this renderer overrides.
func (r *storageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindText, r.renderText)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindBlockquote, r.renderBlockquote)
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(tableKind, r.renderTable)
	reg.Register(tableCellKind, r.renderTableCell)
}

// renderText renders inline text, collapsing soft line breaks to a single space
// so soft-wrapped source lines don't become hard line breaks in Confluence. Hard
// line breaks (a trailing backslash or two spaces) become <br />.
func (r *storageRenderer) renderText(
	w util.BufWriter, source []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	value := n.Segment.Value(source)
	if n.IsRaw() {
		gmhtml.DefaultWriter.RawWrite(w, value)
		return ast.WalkContinue, nil
	}
	gmhtml.DefaultWriter.Write(w, value)
	switch {
	case n.HardLineBreak():
		_, _ = w.WriteString("<br />\n")
	case n.SoftLineBreak():
		_ = w.WriteByte(' ')
	}
	return ast.WalkContinue, nil
}

// renderFencedCodeBlock renders a fenced code block as a Confluence code macro,
// using the fence info string as the language.
func (r *storageRenderer) renderFencedCodeBlock(
	w util.BufWriter, source []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	language := ""
	if lang := n.Language(source); lang != nil {
		language = string(lang)
	}
	writeCodeMacro(w, language, codeBlockText(node, source))
	return ast.WalkSkipChildren, nil
}

// renderCodeBlock renders an indented code block as a code macro (no language).
func (r *storageRenderer) renderCodeBlock(
	w util.BufWriter, source []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	writeCodeMacro(w, "", codeBlockText(node, source))
	return ast.WalkSkipChildren, nil
}

// renderBlockquote renders a blockquote. When the callout transformer has tagged
// it, it becomes an info/tip/note/warning macro; otherwise a plain <blockquote>.
func (r *storageRenderer) renderBlockquote(
	w util.BufWriter, _ []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if macro, ok := node.AttributeString(calloutAttr); ok {
		if entering {
			_, _ = fmt.Fprintf(w, `<ac:structured-macro ac:name="%s" ac:schema-version="1"><ac:rich-text-body>`, macro)
		} else {
			_, _ = w.WriteString(`</ac:rich-text-body></ac:structured-macro>`)
		}
		return ast.WalkContinue, nil
	}
	if entering {
		_, _ = w.WriteString("<blockquote>\n")
	} else {
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}

// codeBlockText concatenates a code block's raw source lines.
func codeBlockText(node ast.Node, source []byte) string {
	var b bytes.Buffer
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(source))
	}
	return b.String()
}

// writeCodeMacro writes a Confluence code macro for content in the given
// language (omit the language parameter when empty). The single trailing newline
// is dropped and any "]]>" is escaped so it can't close the CDATA section early.
func writeCodeMacro(w util.BufWriter, language, content string) {
	content = strings.TrimSuffix(content, "\n")
	content = strings.ReplaceAll(content, "]]>", "]]]]><![CDATA[>")
	_, _ = w.WriteString(`<ac:structured-macro ac:name="code" ac:schema-version="1">`)
	if language != "" {
		_, _ = fmt.Fprintf(w, `<ac:parameter ac:name="language">%s</ac:parameter>`, language)
	}
	_, _ = w.WriteString(`<ac:plain-text-body><![CDATA[`)
	_, _ = w.WriteString(content)
	_, _ = w.WriteString(`]]></ac:plain-text-body></ac:structured-macro>`)
}
