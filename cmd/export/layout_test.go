package export

import (
	"os"
	"path/filepath"
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

// TestCheckTarget covers the three usage errors, all recognizable without a
// server: no target, two targets, and a space walk whose depth was never asked
// for.
func TestCheckTarget(t *testing.T) {
	for _, c := range []struct {
		name       string
		args       []string
		space      string
		depthGiven bool
		wantErr    string
	}{
		{"page alone", []string{"123"}, "", false, ""},
		{"space with depth", nil, "ENG", true, ""},
		{"neither", nil, "", false, "no page given"},
		{"both", []string{"123"}, "ENG", false, "cannot be combined"},
		{"space without depth", nil, "ENG", false, "needs an explicit --depth"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := checkTarget(c.args, c.space, c.depthGiven)
			switch {
			case c.wantErr == "" && err != nil:
				t.Errorf("checkTarget = %v, want nil", err)
			case c.wantErr != "" && err == nil:
				t.Errorf("checkTarget = nil, want an error mentioning %q", c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("checkTarget = %v, want it to mention %q", err, c.wantErr)
			}
		})
	}
}

// TestParseDepth pins the vocabulary, including the difference from children's:
// 0 is a request for the named page alone rather than for nothing.
func TestParseDepth(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"0", 0, false},
		{"1", 1, false},
		{"all", -1, false},
		{"-1", 0, true},
		{"", 0, true},
		{"deep", 0, true},
	} {
		got, err := parseDepth(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseDepth(%q) = %d, nil; want an error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseDepth(%q) = %d, %v; want %d, nil", c.in, got, err, c.want)
		}
	}
}

// TestWriteProjectFile covers the marker an exported tree needs to be
// republishable: written for a multi-page export, never for a single page (its
// file is already at dest, so the fallback root is dest), and never over an
// existing one.
func TestWriteProjectFile(t *testing.T) {
	t.Run("multi-page writes it", func(t *testing.T) {
		dir := t.TempDir()
		got, err := writeProjectFile(dir, true)
		if err != nil || got != markerWrote {
			t.Fatalf("writeProjectFile = %q, %v; want %q", got, err, markerWrote)
		}
		body, err := os.ReadFile(filepath.Join(dir, "markfluence.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "Marks the root") {
			t.Errorf("body = %q, want the explanatory comment", body)
		}
	})

	t.Run("single page writes none", func(t *testing.T) {
		dir := t.TempDir()
		got, err := writeProjectFile(dir, false)
		if err != nil || got != markerSkipped {
			t.Fatalf("writeProjectFile = %q, %v; want no marker", got, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "markfluence.yaml")); err == nil {
			t.Error("a single-page export must not plant a project file")
		}
	})

	t.Run("existing is left alone", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "markfluence.yaml")
		if err := os.WriteFile(path, []byte("mine: keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := writeProjectFile(dir, true)
		if err != nil || got != markerExists {
			t.Fatalf("writeProjectFile = %q, %v; want %q", got, err, markerExists)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "mine: keep\n" {
			t.Errorf("body = %q, want the existing file untouched", body)
		}
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		dryRun = true
		t.Cleanup(func() { dryRun = false })
		got, err := writeProjectFile(dir, true)
		if err != nil || got != markerWrote {
			t.Fatalf("writeProjectFile = %q, %v; want it previewed as written", got, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "markfluence.yaml")); err == nil {
			t.Error("a dry run must not create the file")
		}
	})
}
