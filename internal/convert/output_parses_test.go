package convert_test

// This file tests guarantee L7 (output-is-valid-markdown, docs/guarantees.md):
// anything markfluence writes to disk is markdown that renders. Every other
// test in this package checks that StorageToMarkdown produces a specific
// *string*; this one checks that the string it produces is actually
// recognized as the markdown it looks like, by feeding it back through a real
// parser rather than just eyeballing the golden.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// gfmForL7 is a plain GFM parser -- deliberately not the storageRenderer
// instance internal/convert uses to go the other way, since L7 is about
// whether a generic markdown consumer (a preview pane, GitHub, another tool)
// recognizes the output, not whether markfluence's own machinery can read it
// back.
var gfmForL7 = goldmark.New(goldmark.WithExtensions(extension.GFM))

// imageOrLinkRE approximates GFM's own bracket syntax well enough to count how
// many "![...](...)" / "[...](...)" occurrences the source *looks like it
// contains, as a floor: a real parser is allowed to recognize more (a
// reference-style link this regex doesn't match), never fewer. The
// destination alternatives mirror the one thing that turns real bracket
// syntax into inert literal text: a bare (non-angle-bracketed) destination
// containing a raw space is not a valid link/image at all, by GFM's own rule
// (see the "bare space" cases in doc-links-encoded and images-encoded-src) --
// counting it here would be this test's own false positive, not a bug.
var imageOrLinkRE = regexp.MustCompile(`!?\[[^\]]*\]\((<[^>]*>|[^()\s]*)\)`)

// tableSeparatorRE matches a GFM table header-separator row, e.g. "|---|---|"
// or "--- | :---:". Its presence is what commits the source to being parsed
// as a table rather than a paragraph of pipe characters.
var tableSeparatorRE = regexp.MustCompile(`(?m)^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`)

// fencedCodeRE matches a whole ``` ... ``` fenced block, so its contents can
// be excluded from checks that only make sense for prose.
var fencedCodeRE = regexp.MustCompile("(?s)```.*?```")

// TestStorageToMarkdownOutputParsesAsMarkdown converts every storage2md case's
// input fresh -- the exact body read/export would write to disk -- through a
// real GFM parser, and confirms the bracket/table syntax markfluence emitted
// was actually recognized as such, not left as literal text a broken
// construct degrades to. Deliberately reconverts input.storage rather than
// reading the output.md golden: a golden is static, so reading it directly
// would check that one committed snapshot parses forever, regardless of
// whether the converter that produced it still does the same thing today.
func TestStorageToMarkdownOutputParsesAsMarkdown(t *testing.T) {
	entries, err := os.ReadDir(storage2mdDir)
	if err != nil {
		t.Fatalf("reading %s: %v", storage2mdDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			in, err := os.ReadFile(filepath.Join(storage2mdDir, name, "input.storage"))
			if err != nil {
				t.Fatalf("reading input: %v", err)
			}
			md, err := convert.StorageToMarkdown(string(in), convert.StorageOptions{})
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			assertParsesCleanly(t, []byte(md))
		})
	}
}

// TestForwardCorpusOutputParsesAsMarkdown does the same check against the
// larger regression corpus: every forward golden's real, markfluence-emitted
// storage HTML, converted back to markdown fresh and parsed.
func TestForwardCorpusOutputParsesAsMarkdown(t *testing.T) {
	entries, err := os.ReadDir(regressionDir)
	if err != nil {
		t.Fatalf("reading %s: %v", regressionDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(regressionDir, name, "test.output"))
			if err != nil {
				t.Fatalf("reading golden: %v", err)
			}
			var page struct {
				HTML string `json:"html"`
			}
			if err := json.Unmarshal(data, &page); err != nil {
				t.Fatalf("parsing golden: %v", err)
			}
			md, err := convert.StorageToMarkdown(page.HTML, convert.StorageOptions{})
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			assertParsesCleanly(t, []byte(md))
		})
	}
}

// assertParsesCleanly is the shared check TestStorageToMarkdownOutputParsesAsMarkdown
// and cmd/read's/cmd/export's own tests can reuse: parse source and confirm the
// image/link/table syntax it appears to contain was actually recognized.
func assertParsesCleanly(t *testing.T, source []byte) {
	t.Helper()

	doc := gfmForL7.Parser().Parse(text.NewReader(source))

	var images, links, tables int
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindImage:
			images++
		case ast.KindLink:
			links++
		case extast.KindTable:
			tables++
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		t.Fatalf("walking parsed AST: %v", err)
	}

	// prose excludes fenced code for every check below: example code inside
	// one is free to contain bracket or table-separator-looking text (e.g. a
	// snippet documenting markdown syntax) with no bearing on this guarantee,
	// and counting it would be this test's own false positive, not a bug.
	prose := fencedCodeRE.ReplaceAll(source, nil)

	wantAtLeast := len(imageOrLinkRE.FindAllIndex(prose, -1))
	if got := images + links; got < wantAtLeast {
		t.Errorf("parsed %d image/link node(s), want at least %d matching the source's bracket syntax:\n%s",
			got, wantAtLeast, source)
	}

	if tableSeparatorRE.Match(prose) && tables == 0 {
		t.Errorf("source has a table separator row but no Table node was parsed -- "+
			"it degraded to plain text:\n%s", source)
	}

	// A structural sanity check independent of the floor comparison above,
	// which can't see a rendering bug that mangles bracket syntax badly enough
	// that the output no longer looks like link/image syntax at all -- nothing
	// would be left for imageOrLinkRE to flag as missing. Every "[" markfluence
	// emits outside a code fence closes, so an unequal count means something (a
	// link, an alt-text bracket) was truncated or malformed outright.
	if opens, closes := bytes.Count(prose, []byte("[")), bytes.Count(prose, []byte("]")); opens != closes {
		t.Errorf("unbalanced brackets outside fenced code: %d '[' vs %d ']':\n%s", opens, closes, source)
	}
}
