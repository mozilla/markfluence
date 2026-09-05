package export

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/attachfile"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pagetree"
)

func node(id, typ, title, parent string) pagetree.Node {
	return pagetree.Node{ID: id, Type: typ, Title: title, ParentID: parent}
}

// TestLayoutMirrorsTheHierarchy is the shape of an exported tree: a page is a
// file with a directory beside it, a folder is only a directory, and a page
// under a folder sits inside it.
func TestLayoutMirrorsTheHierarchy(t *testing.T) {
	got := layout(rootRef{ID: "1", Title: "Home", File: true}, []pagetree.Node{
		node("2", pagetree.TypePage, "Onboarding", "1"),
		node("3", pagetree.TypePage, "Escalation", "2"),
		node("4", pagetree.TypeFolder, "Runbooks", "1"),
		node("5", pagetree.TypePage, "Deploy", "4"),
	})
	for id, p := range got {
		if p.warning != "" {
			t.Errorf("%s carries a warning it should not: %q", id, p.warning)
		}
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
	got := layout(rootRef{ID: "1", Title: "Home", File: true}, []pagetree.Node{
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
	got := layout(rootRef{ID: "1", Title: "Home", File: true}, []pagetree.Node{
		node("2", pagetree.TypePage, "Deploy: Prod", "1"),
		node("3", pagetree.TypePage, "Deploy Prod", "1"),
	})
	if got["2"].file != "home/deploy-prod-2.md" || got["3"].file != "home/deploy-prod-3.md" {
		t.Errorf("files = %q and %q, want both suffixed", got["2"].file, got["3"].file)
	}
	// Both members carry it, so --json reports it against each affected page
	// rather than the run having nowhere to put it.
	for _, id := range []string{"2", "3"} {
		if !strings.Contains(got[id].warning, "Deploy: Prod") {
			t.Errorf("%s warning = %q, want it to name both titles", id, got[id].warning)
		}
	}
}

// TestLayoutCollisionNamespaceCoversFolders: a page and a folder both want the
// same directory, so they are one group even though only one of them has a file.
func TestLayoutCollisionNamespaceCoversFolders(t *testing.T) {
	got := layout(rootRef{ID: "1", Title: "Home", File: true}, []pagetree.Node{
		node("2", pagetree.TypePage, "Team", "1"),
		node("3", pagetree.TypeFolder, "Team", "1"),
	})
	if got["2"].childDir == got["3"].childDir {
		t.Errorf("page and folder share the directory %q", got["2"].childDir)
	}
	if got["2"].warning == "" || got["3"].warning == "" {
		t.Errorf("both the page and the folder should be warned about: %q / %q",
			got["2"].warning, got["3"].warning)
	}
}

// TestLayoutUnderAFolderRoot is the folder-as-target case, and the reason
// rootRef carries an id separately from whether there is a file: the walk's
// top-level nodes report the folder as their parent, so grouping by parent
// needs that id even though nothing is written for it.
func TestLayoutUnderAFolderRoot(t *testing.T) {
	got := layout(rootRef{ID: "9"}, []pagetree.Node{
		node("2", pagetree.TypePage, "Runbook", "9"),
		node("3", pagetree.TypePage, "Escalation", "2"),
	})
	if want := "runbook.md"; got["2"].file != want {
		t.Errorf("file = %q, want %q at the top level", got["2"].file, want)
	}
	if want := "runbook/escalation.md"; got["3"].file != want {
		t.Errorf("file = %q, want %q", got["3"].file, want)
	}
	if want := "../runbook.md"; got["3"].parentFile != want {
		t.Errorf("parentFile = %q, want %q", got["3"].parentFile, want)
	}
	if got["2"].parentFile != "" {
		t.Errorf("a page whose parent is the folder keeps an id, got %q", got["2"].parentFile)
	}
}

// TestLayoutWithoutARootNodeStartsAtTheTop is the --space and folder-root case:
// there is no file for the thing named, so its children are the top level.
func TestLayoutWithoutARootNodeStartsAtTheTop(t *testing.T) {
	got := layout(rootRef{}, []pagetree.Node{
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

// TestCheckSpaceDepth: --space --depth 0 passes checkTarget, which only asks
// whether --depth was given, and would then walk nothing at all -- writing a
// stray project-marker file and reporting success. The value has to be checked
// too.
func TestCheckSpaceDepth(t *testing.T) {
	if err := checkSpaceDepth("ENG", depthNone); err == nil {
		t.Error("want a refusal for --space --depth 0")
	}
	if err := checkSpaceDepth("ENG", 1); err != nil {
		t.Errorf("--space --depth 1 must be allowed: %v", err)
	}
	if err := checkSpaceDepth("", depthNone); err != nil {
		t.Errorf("a page export at depth 0 is the default: %v", err)
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

// TestAttachmentDirFollowsTheDisambiguatedSlug is the invariant the conflict
// rule leans on: page directories are unique, so two pages' same-named native
// attachments cannot meet. It failed when the attachment directory was
// recomputed from the title instead of taken from the layout -- two siblings
// whose titles slug the same then shared one directory, and the second page's
// image was skipped as "already there" while its markdown pointed at the first
// page's bytes. A native attachment carries no checksum, so nothing else would
// have caught it.
func TestAttachmentDirFollowsTheDisambiguatedSlug(t *testing.T) {
	places := layout(rootRef{ID: "1", Title: "Home", File: true}, []pagetree.Node{
		node("2", pagetree.TypePage, "Deploy: Prod", "1"),
		node("3", pagetree.TypePage, "Deploy Prod", "1"),
	})

	two := pagedoc.AttachmentDirFor(&client.Page{ID: "2", Title: "Deploy: Prod"},
		pagedoc.Placement{Dir: places["2"].dir, AttachmentDir: places["2"].childDir})
	three := pagedoc.AttachmentDirFor(&client.Page{ID: "3", Title: "Deploy Prod"},
		pagedoc.Placement{Dir: places["3"].dir, AttachmentDir: places["3"].childDir})

	if two == three {
		t.Fatalf("both pages place attachments in %q", two)
	}
	if two != places["2"].childDir || three != places["3"].childDir {
		t.Errorf("attachment dirs %q and %q do not match the layout's %q and %q",
			two, three, places["2"].childDir, places["3"].childDir)
	}
}

// TestFileFlagIsReservedNotTheSlug goes through exportNodes rather than
// re-implementing the override-then-reserve sequence, because the sequence is
// the thing under test: reserving before applying --file leaves the file this
// run actually writes unprotected, and a hand-rolled body would pass either way.
func TestFileFlagIsReservedNotTheSlug(t *testing.T) {
	fileFlag = "custom.md"
	// The two extra attachments are not referenced by the body; this is about
	// where an attachment may land, not about which are exported.
	allAttachments = true
	t.Cleanup(func() { fileFlag, allAttachments = "", false })

	dir := t.TempDir()
	c := nativePageServer(t, "", "custom.md", "home.md")
	p, err := c.GetPageBodyOrNil("1")
	if err != nil {
		t.Fatal(err)
	}

	results := exportNodes(c, p, rootRef{ID: p.ID, Title: p.Title, File: true}, dir, nil)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if want := filepath.Join(dir, "custom.md"); results[0].destPath != want {
		t.Errorf("destPath = %q, want %q", results[0].destPath, want)
	}
	// Both directions. The attachment recording the path --file named must be
	// refused; the one recording the slug this run does not use must not be,
	// since nothing is written there.
	byName := map[string]attachment{}
	for _, a := range results[0].attachments {
		byName[a.name] = a
	}
	if a := byName["custom.md"]; a.status != attachfile.StatusFailed ||
		!strings.Contains(a.err.Error(), "own file") {
		t.Errorf("custom.md = %+v, want a refusal naming the page's own file", a)
	}
	if a := byName["home.md"]; a.status == attachfile.StatusFailed {
		t.Errorf("home.md = %+v, want it written: --file means nothing is at the slug", a)
	}
}

// TestCollisionWarningSurvivesASuccessfulExport is the bug a hand-built result
// hid: the placement's warning is appended early in exportOne and the
// referenced-attachment warnings were *assigned* later, so a page that exported
// cleanly -- the whole motivating case -- lost it in both human output and
// --json, while a page that failed kept it.
func TestCollisionWarningSurvivesASuccessfulExport(t *testing.T) {
	dir := t.TempDir()
	c := nativePageServer(t, "")
	p, err := c.GetPageBodyOrNil("1")
	if err != nil {
		t.Fatal(err)
	}

	res := exportOne(c, p, dir, pagedoc.Placement{AttachmentDir: "runbook"},
		placement{file: "runbook.md", childDir: "runbook", warning: "COLLISION"}, newClaims())
	if res.err != nil {
		t.Fatalf("export: %v", res.err)
	}
	if len(res.warnings) == 0 || res.warnings[0] != "COLLISION" {
		t.Errorf("warnings = %v, want the placement's warning kept", res.warnings)
	}
}

// TestFolderCollisionIsReported closes the half of the warning regression that
// survived: siblingSlugs covers folder directories in the same namespace as
// page files, so two sibling folders that slug the same are renamed -- but a
// folder produces no result, so its warning had nowhere to land and the user
// got id-suffixed directories with no explanation anywhere.
func TestFolderCollisionIsReported(t *testing.T) {
	places := layout(rootRef{ID: "1", Title: "Home", File: true}, []pagetree.Node{
		node("2", pagetree.TypeFolder, "Run Books", "1"),
		node("3", pagetree.TypeFolder, "Run: Books", "1"),
		node("4", pagetree.TypePage, "Deploy", "2"),
	})
	if places["2"].childDir == places["3"].childDir {
		t.Fatal("the two folders share a directory")
	}
	if places["2"].warning == "" {
		t.Fatal("the layout does not warn about the folder collision")
	}

	dir := t.TempDir()
	c := nativePageServer(t, "")
	p, err := c.GetPageBodyOrNil("1")
	if err != nil {
		t.Fatal(err)
	}
	results := exportNodes(c, p, rootRef{ID: "1", Title: "Home", File: true}, dir, []pagetree.Node{
		node("2", pagetree.TypeFolder, "Run Books", "1"),
		node("3", pagetree.TypeFolder, "Run: Books", "1"),
	})
	if len(results) != 1 {
		t.Fatalf("got %d results, want just the root page", len(results))
	}
	found := false
	for _, w := range results[0].warnings {
		if strings.Contains(w, "Run: Books") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want the folder collision reported somewhere", results[0].warnings)
	}
}

// TestReportOneWarnsInHumanOutput covers the two loops in reportOne: a page
// that exported and a page that failed both have to say what they were warned
// about, and only --json was asserted before.
func TestReportOneWarnsInHumanOutput(t *testing.T) {
	for _, c := range []struct {
		name string
		r    result
	}{
		{"exported", result{
			page: &client.Page{ID: "1", Title: "Home"}, pageStatus: statusWrote,
			destPath: "/out/home.md", warnings: []string{"RENAMED"},
		}},
		{"failed", result{
			node: &pagetree.Node{ID: "2", Title: "Child"}, place: placement{file: "child.md"},
			err: errors.New("boom"), warnings: []string{"RENAMED"},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := captureBoth(t, func() { reportOne(c.r) })
			if !strings.Contains(out, "RENAMED") {
				t.Errorf("output = %q, want the warning", out)
			}
		})
	}
}
