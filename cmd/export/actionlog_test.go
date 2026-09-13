package export

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagetree"
	"github.com/mozilla/markfluence/internal/project"
)

// versionedTreeServer serves Home (1, v7) with one child Onboarding (2, v11),
// both carrying a version so the recorded base has one to be about.
func versionedTreeServer(t *testing.T) *client.ConfluenceClient {
	t.Helper()
	body := func(id, title string, version int) string {
		return `{"id":"` + id + `","title":"` + title + `","spaceId":"77",` +
			`"version":{"number":` + strconv.Itoa(version) + `,"createdAt":"2026-01-01T00:00:00Z"},` +
			`"body":{"storage":{"value":"<p>Body of ` + title + `.</p>","representation":"storage"}},` +
			`"_links":{"webui":"/spaces/ENG/pages/` + id + `/x"}}`
	}
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/page"):
			if strings.Contains(r.URL.Path, "/1/") {
				_, _ = w.Write([]byte(`{"results":[{"id":"2","type":"page","title":"Onboarding",` +
					`"status":"current","extensions":{"position":1},` +
					`"_links":{"webui":"/spaces/ENG/pages/2/Onboarding"}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/folder"),
			strings.Contains(r.URL.Path, "/child/attachment"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/pages/2"):
			_, _ = w.Write([]byte(body("2", "Onboarding", 11)))
		default:
			_, _ = w.Write([]byte(body("1", "Home", 7)))
		}
	})
}

// exportTree runs a real two-page export into dir and returns its results.
func exportTree(t *testing.T, dir string) *client.ConfluenceClient {
	t.Helper()
	c := versionedTreeServer(t)
	root, err := c.GetPageBodyOrNil("1")
	if err != nil {
		t.Fatalf("fetching root: %v", err)
	}
	err = runExport(c, dir, target{
		page:      root,
		ref:       rootRef{ID: root.ID, Title: root.Title, File: true},
		multiPage: true,
		walk:      func() ([]pagetree.Node, error) { return walkUnder(c, root.ID, pagetree.AllDepths) },
		fail:      func(err error, _ jsonout.Code) error { t.Fatalf("export failed: %v", err); return nil },
	})
	if err != nil {
		t.Fatalf("runExport: %v", err)
	}
	return c
}

func baseIn(t *testing.T, dir, key string) (actionlog.Entry, bool) {
	t.Helper()
	root, err := project.Discover(dir)
	if err != nil {
		t.Fatalf("discovering root: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return actionlog.For(root).Base(key)
}

// The base an export establishes is what makes arrangement 2 work at all:
// export, edit, publish back. Without it the first update has nothing to
// compare against.
func TestAnExportRecordsABasePerWrittenPage(t *testing.T) {
	dir := t.TempDir()
	exportTree(t, dir)

	for _, tc := range []struct {
		key     string
		pageID  string
		version int
	}{
		{"home.md", "1", 7},
		{"home/onboarding.md", "2", 11},
	} {
		got, ok := baseIn(t, dir, tc.key)
		if !ok {
			t.Errorf("%s: no base recorded", tc.key)
			continue
		}
		if got.Action != actionlog.ActionExport {
			t.Errorf("%s: action = %q", tc.key, got.Action)
		}
		if got.PageID != tc.pageID || got.PageVersion != tc.version {
			t.Errorf("%s: page %s v%d, want %s v%d", tc.key, got.PageID, got.PageVersion, tc.pageID, tc.version)
		}
		// The second pass is what supplies this. Without it the first update
		// after an export republishes identical content.
		if got.PublishSHA256 == "" {
			t.Errorf("%s: no publish sha -- the post-walk pass did not run for it", tc.key)
		}
	}
}

// The version line is written during the walk so a run that dies partway
// leaves every already-written page recorded (#139 D10); the sha line comes
// from the pass afterwards. Both are on disk, and the later one wins.
func TestTheWalkWritesBeforeTheShaPass(t *testing.T) {
	dir := t.TempDir()
	exportTree(t, dir)

	data, err := os.ReadFile(filepath.Join(dir, actionlog.Dirname, actionlog.Filename))
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want two pages x (walk line + sha line):\n%s", len(lines), data)
	}
	// The first two carry no sha: during the walk a file's siblings are not on
	// disk yet, so its render is not the one update will recompute.
	for i, line := range lines[:2] {
		if !strings.Contains(line, `"publish_sha256":""`) {
			t.Errorf("walk line %d carries a sha: %s", i, line)
		}
	}
}

// export skips a page whose file already exists (S3), and that file is
// somebody's -- possibly edited. Claiming it was derived from this version
// would silence divergence detection for exactly the file most likely to need
// it.
func TestASkippedPageRecordsNoBase(t *testing.T) {
	dir := t.TempDir()
	// Plant home.md so the root page is skipped; the child still exports.
	if err := os.WriteFile(filepath.Join(dir, "home.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exportTree(t, dir)

	if _, ok := baseIn(t, dir, "home.md"); ok {
		t.Error("a skipped page recorded a base it has no grounds for")
	}
	if _, ok := baseIn(t, dir, "home/onboarding.md"); !ok {
		t.Error("the page that was written recorded nothing")
	}
}

func TestADryRunExportRecordsNothing(t *testing.T) {
	dryRun = true
	t.Cleanup(func() { dryRun = false })

	dir := t.TempDir()
	exportTree(t, dir)
	if _, err := os.Stat(filepath.Join(dir, actionlog.Dirname)); err == nil {
		t.Error("a dry run created the state directory")
	}
}
