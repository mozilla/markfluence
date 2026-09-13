package update

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/project"
)

// inProject writes a markdown file under a directory that is a real project
// root, which is what makes a log exist at all.
func inProject(t *testing.T, body string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename), []byte("# marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

// baseIn reads the recorded base for key straight off disk.
func baseIn(t *testing.T, dir, key string) (actionlog.Entry, bool) {
	t.Helper()
	root, err := project.FromPath(dir)
	if err != nil {
		t.Fatalf("reopening root: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return actionlog.For(root).Base(key)
}

// publishingServer answers a GET with a page at version 3 and a PUT with the
// version it was sent.
func publishingServer(t *testing.T) *client.ConfluenceClient {
	t.Helper()
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		case http.MethodPut:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
}

// run is what the batch loop does for one file: publish, then record.
func runOne(t *testing.T, path string) *updateResult {
	t.Helper()
	r := processFile(path, publishingServer(t), project.NewCache(""),
		linkindex.NewCache(), pagedoc.NewUserCache())
	recordAction(actionlog.NewCache(), r)
	return r
}

func TestAPublishRecordsItsBase(t *testing.T) {
	dir, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	r := runOne(t, path)
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want ok/published", r)
	}

	got, ok := baseIn(t, dir, "f.md")
	if !ok {
		t.Fatal("a successful publish recorded no base")
	}
	if got.Action != actionlog.ActionUpdate || got.Status != actionlog.StatusOK {
		t.Errorf("action/status = %q/%q", got.Action, got.Status)
	}
	if got.PageID != "1" {
		t.Errorf("PageID = %q, want 1", got.PageID)
	}
	// The version the publish produced, not the one it started from: the base
	// has to name what the page is now, or the next run reads a divergence.
	if got.PageVersion != 4 {
		t.Errorf("PageVersion = %d, want 4", got.PageVersion)
	}
	if got.PublishSHA256 == "" {
		t.Error("no publish sha recorded")
	}
}

// The sha must be of what the PUT sent, which includes the title: a file whose
// only change is its title would otherwise hash the same and never publish.
func TestTheRecordedShaCoversTheTitle(t *testing.T) {
	_, a := inProject(t, "---\npage_id: 1\ntitle: One\n---\nSame body.\n")
	_, b := inProject(t, "---\npage_id: 1\ntitle: Two\n---\nSame body.\n")
	if runOne(t, a).publishSHA == runOne(t, b).publishSHA {
		t.Error("two files differing only in title hashed the same")
	}

	_, c := inProject(t, "---\npage_id: 1\ntitle: One\n---\nDifferent body.\n")
	if runOne(t, a).publishSHA == runOne(t, c).publishSHA {
		t.Error("two files differing only in body hashed the same")
	}
}

// A skip must not record. In this change that is the mtime skip, which
// establishes only that two timestamps are ordered a certain way -- not that
// this copy matches the page -- so a line would claim a base nothing verified,
// and would silence divergence detection for a page edited in the UI.
func TestTheMtimeSkipRecordsNothing(t *testing.T) {
	dir, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pageWithVersion("1", 3, "2099-01-01T00:00:00Z")))
	})

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	recordAction(actionlog.NewCache(), r)
	if r.status != statusSkipped {
		t.Fatalf("status = %q, want skipped", r.status)
	}
	if _, ok := baseIn(t, dir, "f.md"); ok {
		t.Error("a skip recorded a base it never verified")
	}
}

// Nothing claims the file, so there is no page for a base to be against.
func TestAnUnmanagedFileRecordsNothing(t *testing.T) {
	dir, path := inProject(t, "Just markdown, no frontmatter.\n")
	c := clienttest.New(t, func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected %s: an unmanaged file makes no request", r.Method)
	})

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	recordAction(actionlog.NewCache(), r)
	if !r.unmanaged {
		t.Fatalf("result = %+v, want unmanaged", r)
	}
	if _, ok := baseIn(t, dir, "f.md"); ok {
		t.Error("an unmanaged file recorded a base")
	}
}

// A preview that logged would claim a publish happened.
func TestADryRunRecordsNothing(t *testing.T) {
	dryRun = true
	t.Cleanup(func() { dryRun = false })

	dir, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	runOne(t, path)
	if _, ok := baseIn(t, dir, "f.md"); ok {
		t.Error("a dry run recorded a base")
	}
	if _, err := os.Stat(filepath.Join(dir, actionlog.Dirname)); err == nil {
		t.Error("a dry run created the state directory")
	}
}

// A failure is history and never a base: Base skips it, but it is on disk.
func TestAFailedPublishIsRecordedAsAFailure(t *testing.T) {
	dir, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"status":500,"title":"boom"}]}`))
	})

	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache())
	recordAction(actionlog.NewCache(), r)
	if r.ok {
		t.Fatal("want a failed result")
	}
	if _, ok := baseIn(t, dir, "f.md"); ok {
		t.Error("a failed publish became a base")
	}
	data, err := os.ReadFile(filepath.Join(dir, actionlog.Dirname, actionlog.Filename))
	if err != nil {
		t.Fatalf("no log written for a failure: %v", err)
	}
	if !strings.Contains(string(data), `"status":"failed"`) {
		t.Errorf("the failure was not recorded: %s", data)
	}
}

// #139's rule, applied here: a root with no project file refuses rather than
// creating one, so a lone file carrying a page_id publishes and records
// nothing at all.
func TestNoProjectFileMeansNoLogIsCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	if err := os.WriteFile(path, []byte("---\npage_id: 1\n---\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := runOne(t, path)
	if !r.ok {
		t.Fatalf("result = %+v, want a successful publish", r)
	}
	if _, err := os.Stat(filepath.Join(dir, actionlog.Dirname)); err == nil {
		t.Error("created a state directory under a root with no project file")
	}
}
