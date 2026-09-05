package export

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/pagetree"
)

func node(id, typ, title, parent string) pagetree.Node {
	return pagetree.Node{ID: id, Type: typ, Title: title, ParentID: parent}
}

// TestLayoutMirrorsTheHierarchy is the shape of an exported tree: a page is a
// file with a directory beside it, a folder is only a directory, and a page
// under a folder sits inside it.
func TestLayoutMirrorsTheHierarchy(t *testing.T) {
	got, warnings := layout("Home", "1", []pagetree.Node{
		node("2", pagetree.TypePage, "Onboarding", "1"),
		node("3", pagetree.TypePage, "Escalation", "2"),
		node("4", pagetree.TypeFolder, "Runbooks", "1"),
		node("5", pagetree.TypePage, "Deploy", "4"),
	})
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	for _, c := range []struct{ id, file, childDir string }{
		{"1", "home.md", "home"},
		{"2", "home/onboarding.md", "home/onboarding"},
		{"3", "home/onboarding/escalation.md", "home/onboarding/escalation"},
		{"4", "", "home/runbooks"},
		{"5", "home/runbooks/deploy.md", "home/runbooks/deploy"},
	} {
		p := got[c.id]
		if p.file != c.file {
			t.Errorf("%s file = %q, want %q", c.id, p.file, c.file)
		}
		if p.childDir != c.childDir {
			t.Errorf("%s childDir = %q, want %q", c.id, p.childDir, c.childDir)
		}
	}
	if !got["4"].folder {
		t.Error("a folder must be marked as one: it gets a directory and no file")
	}
}

// TestLayoutParentPathsPointAtTheParentFile is what makes an exported tree
// publishable into fresh pages: create resolves parent: against the referring
// file's own directory.
func TestLayoutParentPathsPointAtTheParentFile(t *testing.T) {
	got, _ := layout("Home", "1", []pagetree.Node{
		node("2", pagetree.TypePage, "Onboarding", "1"),
		node("3", pagetree.TypePage, "Escalation", "2"),
		node("4", pagetree.TypeFolder, "Runbooks", "1"),
		node("5", pagetree.TypePage, "Deploy", "4"),
	})
	if want := "../home.md"; got["2"].parentFile != want {
		t.Errorf("2 parentFile = %q, want %q", got["2"].parentFile, want)
	}
	if want := "../onboarding.md"; got["3"].parentFile != want {
		t.Errorf("3 parentFile = %q, want %q", got["3"].parentFile, want)
	}
	// The root's parent is outside the export, and a folder has no file to
	// point at: both keep an id, which the caller supplies.
	if got["1"].parentFile != "" {
		t.Errorf("root parentFile = %q, want empty", got["1"].parentFile)
	}
	if got["5"].parentFile != "" {
		t.Errorf("a page under a folder has no parent file, got %q", got["5"].parentFile)
	}
}

// TestLayoutDisambiguatesCollidingSiblings covers the lossy slug: two titles,
// one name. Every member of the group takes the suffix, so the result does not
// depend on which was walked first.
func TestLayoutDisambiguatesCollidingSiblings(t *testing.T) {
	got, warnings := layout("Home", "1", []pagetree.Node{
		node("2", pagetree.TypePage, "Deploy: Prod", "1"),
		node("3", pagetree.TypePage, "Deploy Prod", "1"),
	})
	if got["2"].file != "home/deploy-prod-2.md" || got["3"].file != "home/deploy-prod-3.md" {
		t.Errorf("files = %q and %q, want both suffixed", got["2"].file, got["3"].file)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Deploy: Prod") {
		t.Fatalf("warnings = %v, want one naming both titles", warnings)
	}
}

// TestLayoutCollisionNamespaceCoversFolders: a page and a folder both want the
// same directory, so they are one group even though only one of them has a file.
func TestLayoutCollisionNamespaceCoversFolders(t *testing.T) {
	got, warnings := layout("Home", "1", []pagetree.Node{
		node("2", pagetree.TypePage, "Team", "1"),
		node("3", pagetree.TypeFolder, "Team", "1"),
	})
	if got["2"].childDir == got["3"].childDir {
		t.Errorf("page and folder share the directory %q", got["2"].childDir)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want one", warnings)
	}
}

// TestLayoutWithoutARootNodeStartsAtTheTop is the --space and folder-root case:
// there is no file for the thing named, so its children are the top level.
func TestLayoutWithoutARootNodeStartsAtTheTop(t *testing.T) {
	got, _ := layout("", "", []pagetree.Node{
		node("2", pagetree.TypePage, "Handbook", ""),
		node("3", pagetree.TypePage, "Onboarding", "2"),
	})
	if want := "handbook.md"; got["2"].file != want {
		t.Errorf("file = %q, want %q at the top level", got["2"].file, want)
	}
	if want := "handbook/onboarding.md"; got["3"].file != want {
		t.Errorf("file = %q, want %q", got["3"].file, want)
	}
	if want := "../handbook.md"; got["3"].parentFile != want {
		t.Errorf("parentFile = %q, want %q", got["3"].parentFile, want)
	}
}
