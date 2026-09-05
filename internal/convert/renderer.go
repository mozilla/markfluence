package convert

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/project"
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

	// root bounds which images and parent references may be read (S1/S2): the
	// documentation root, discovered per file rather than assumed to be the
	// working directory. Every attachment's Source is recorded relative to it.
	root *project.Root

	// Link/anchor rewriting context, populated per conversion.
	//
	// currentBasename is the bare filename, used to build a same-page anchor's
	// fake self-link; currentDocKey is the same file's root-relative path, used
	// to look up its own anchors in index -- the two differ by more than a
	// leading directory whenever the file isn't at the index's root.
	currentBasename string
	currentDocKey   string
	baseURL         string
	spaceKey        string
	// index is the tree-wide link/anchor index, built once per root and shared
	// across every file converted under it (internal/linkindex).
	index *linkindex.Index

	// Image side effects.
	attachments []Attachment
	broken      []string
	warnings    []string
	// seen maps an attachment name to the image that claimed it. It records the
	// source path, not just the fact of a claim, because the name is now the
	// base name: a second image reaching the same name is either the same asset
	// again (deduped) or a different asset that cannot be published alongside it
	// (refused), and only the path tells them apart.
	seen map[string]claimedName

	// pastedNames are the attachment names referenced by raw ri:filename in the
	// body -- storage the shield passes through untouched, which renderImage
	// therefore never sees. A converted image landing on one of these rebinds a
	// reference the author did not touch, which is worth a warning.
	pastedNames map[string]bool

	// linkBrokenText is the literal replacement text for the *ast.Link
	// currently being rendered, set on entering when its target is Broken and
	// cleared (empty) otherwise; renderLink's matching leaving call reads it
	// once to decide whether a closing "</a>" is due. Per-node transient
	// state, the same shape as seen above -- safe because goldmark never
	// renders two Link nodes concurrently (markdown has no nested links).
	linkBrokenText string

	// lineOffset is the number of lines the frontmatter block consumed in the
	// original file, added to every line nodeLine reports: goldmark parses
	// md.Body, which Extract already stripped of that block, so every
	// position it sees is relative to the body, not the file a reader would
	// open and count lines in.
	lineOffset int
}

// nodeLine returns the 1-indexed source line n starts on -- in the original
// file, frontmatter included via lineOffset -- and whether one could be
// found. Neither *ast.Link nor *ast.Image carries its own position --
// goldmark's parser never calls SetLines on either -- so this walks to the
// first descendant *ast.Text, which does, via the same Segment nodeText
// already reads. ok is false for a node with no text descendant at all (e.g.
// an empty link), in which case a caller must not report a line at all
// rather than a wrong one.
func (r *storageRenderer) nodeLine(n ast.Node, source []byte) (line int, ok bool) {
	offset := -1
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || offset >= 0 {
			return ast.WalkContinue, nil
		}
		if t, isText := c.(*ast.Text); isText {
			offset = t.Segment.Start
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	if offset < 0 {
		return 0, false
	}
	return r.lineOffset + 1 + bytes.Count(source[:offset], []byte("\n")), true
}

// linePrefix returns "line %d: " for a node whose source line nodeLine can
// find, or "" otherwise -- callers prepend the result to a diagnostic
// message unconditionally, so a message that can't be located reads exactly
// as it did before this existed.
func (r *storageRenderer) linePrefix(n ast.Node, source []byte) string {
	if line, ok := r.nodeLine(n, source); ok {
		return fmt.Sprintf("line %d: ", line)
	}
	return ""
}

// claimedName is an image that has taken an attachment name, kept so a later
// image reaching the same name can be reported against it by path and line.
type claimedName struct {
	source string
	line   string // a "line N: " prefix, or "" when the position is unknown
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
// it with an alert type, it becomes that alert's calloutTargets construct -- a
// callout macro, or an ADF extension for the purple panel that has no macro.
// Otherwise a plain <blockquote>.
func (r *storageRenderer) renderBlockquote(
	w util.BufWriter, _ []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if attr, ok := node.AttributeString(calloutAttr); ok {
		alert, _ := attr.(string) // set by calloutTransformer, always a string
		target := calloutTargets[alert]
		switch {
		case target.panelType != "":
			// A purple panel has no macro, so it is written the way Confluence
			// itself writes one: an ADF extension. No ac:adf-fallback -- it is a
			// cache Confluence regenerates on demand, and one written here would
			// go stale the moment the body changed.
			if entering {
				_, _ = fmt.Fprintf(w,
					`<ac:adf-extension><ac:adf-node type="panel">`+
						`<ac:adf-attribute key="panel-type">%s</ac:adf-attribute><ac:adf-content>`,
					target.panelType)
			} else {
				_, _ = w.WriteString(`</ac:adf-content></ac:adf-node></ac:adf-extension>`)
			}
		default:
			if entering {
				_, _ = fmt.Fprintf(w,
					`<ac:structured-macro ac:name="%s" ac:schema-version="1"><ac:rich-text-body>`, target.macro)
			} else {
				_, _ = w.WriteString(`</ac:rich-text-body></ac:structured-macro>`)
			}
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
