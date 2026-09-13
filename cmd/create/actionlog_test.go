package create

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/project"
)

func baseIn(t *testing.T, dir, key string) (actionlog.Entry, bool) {
	t.Helper()
	root, err := project.FromPath(dir)
	if err != nil {
		t.Fatalf("reopening root: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return actionlog.For(root).Base(key)
}

// A created page is as much a base as an updated one: the file and the page
// are in step the moment create finishes, which is exactly what a merge base
// records.
func TestACreateRecordsItsBase(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"
	writeManifest(t, dir, "")
	titleOpt = "A"
	path := write(t, dir, "a.md", "# A\n")

	c, _ := newFakeConfluence(t)
	results := createAll(buildRecords(t, c, []string{path}), c, true)
	if !results[0].ok {
		t.Fatalf("create failed: %s", results[0].errMsg)
	}

	got, ok := baseIn(t, dir, "a.md")
	if !ok {
		t.Fatal("a successful create recorded no base")
	}
	if got.Action != actionlog.ActionCreate || got.Status != actionlog.StatusOK {
		t.Errorf("action/status = %q/%q", got.Action, got.Status)
	}
	if got.PageID == "" {
		t.Error("no page id recorded")
	}
	// create reserves a stub and then publishes into it, so the page is at
	// version 2 by the end -- the base must name where the page landed, not
	// where the stub started.
	if got.PageVersion != 2 {
		t.Errorf("PageVersion = %d, want 2 (the stub is v1, the publish makes v2)", got.PageVersion)
	}
	if got.PublishSHA256 == "" {
		t.Error("no publish sha recorded")
	}
}

func TestADryRunCreateRecordsNothing(t *testing.T) {
	resetOpts(t)
	dir := t.TempDir()
	spaceOpt = "ENG"
	writeManifest(t, dir, "")
	titleOpt = "A"
	dryRunOpt = true
	path := write(t, dir, "a.md", "# A\n")

	c, _ := newFakeConfluence(t)
	createAll(buildRecords(t, c, []string{path}), c, true)

	if _, err := os.Stat(filepath.Join(dir, actionlog.Dirname)); err == nil {
		t.Error("a dry run created the state directory")
	}
}
