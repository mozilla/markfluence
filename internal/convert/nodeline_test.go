package convert

import (
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// firstNode returns the first descendant of kind in source's parsed AST, or
// nil if there isn't one.
func firstNode(source []byte, kind ast.NodeKind) ast.Node {
	doc := goldmark.DefaultParser().Parse(text.NewReader(source))
	var found ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && found == nil && n.Kind() == kind {
			found = n
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return found
}

func TestNodeLineOnFirstLine(t *testing.T) {
	source := []byte("[text](target.md)\n")
	link := firstNode(source, ast.KindLink)
	if link == nil {
		t.Fatal("no link found")
	}
	r := &storageRenderer{}
	if line, ok := r.nodeLine(link, source); !ok || line != 1 {
		t.Errorf("nodeLine = %d, %v, want 1, true", line, ok)
	}
}

func TestNodeLineSeveralLinesDown(t *testing.T) {
	source := []byte("para one\n\npara two\n\n[text](target.md)\n")
	link := firstNode(source, ast.KindLink)
	if link == nil {
		t.Fatal("no link found")
	}
	r := &storageRenderer{}
	if line, ok := r.nodeLine(link, source); !ok || line != 5 {
		t.Errorf("nodeLine = %d, %v, want 5, true", line, ok)
	}
}

// TestNodeLineInsideNestedConstruct covers a link nested three levels deep
// (blockquote > list item > paragraph) -- nodeLine walks to the first
// descendant *ast.Text regardless of how many container levels sit between
// it and the node passed in.
func TestNodeLineInsideNestedConstruct(t *testing.T) {
	source := []byte("> - [text](target.md)\n")
	link := firstNode(source, ast.KindLink)
	if link == nil {
		t.Fatal("no link found")
	}
	r := &storageRenderer{}
	if line, ok := r.nodeLine(link, source); !ok || line != 1 {
		t.Errorf("nodeLine = %d, %v, want 1, true", line, ok)
	}
}

// TestNodeLineNoTextDescendantIsNotOK is the fallback path: a node with no
// *ast.Text descendant at all (constructed directly rather than parsed --
// whether "[]( target.md )" itself ever produces a childless Link node is a
// goldmark implementation detail this test shouldn't depend on) must report
// ok=false rather than a wrong line.
func TestNodeLineNoTextDescendantIsNotOK(t *testing.T) {
	link := ast.NewLink()
	r := &storageRenderer{}
	if line, ok := r.nodeLine(link, []byte("irrelevant")); ok {
		t.Errorf("nodeLine = %d, true, want ok=false for a childless node", line)
	}
}

// TestNodeLineAppliesLineOffset is what makes a reported line match the file
// a reader opens, not the frontmatter-stripped body goldmark actually parses.
func TestNodeLineAppliesLineOffset(t *testing.T) {
	source := []byte("[text](target.md)\n")
	link := firstNode(source, ast.KindLink)
	if link == nil {
		t.Fatal("no link found")
	}
	r := &storageRenderer{lineOffset: 4}
	if line, ok := r.nodeLine(link, source); !ok || line != 5 {
		t.Errorf("nodeLine = %d, %v, want 5, true", line, ok)
	}
}

func TestLinePrefixEmptyWhenLineNotFound(t *testing.T) {
	r := &storageRenderer{}
	if got := r.linePrefix(ast.NewLink(), []byte("irrelevant")); got != "" {
		t.Errorf("linePrefix = %q, want empty", got)
	}
}

func TestLinePrefixFormat(t *testing.T) {
	source := []byte("[text](target.md)\n")
	link := firstNode(source, ast.KindLink)
	if link == nil {
		t.Fatal("no link found")
	}
	r := &storageRenderer{}
	if got, want := r.linePrefix(link, source), "line 1: "; got != want {
		t.Errorf("linePrefix = %q, want %q", got, want)
	}
}
