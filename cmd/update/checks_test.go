package update

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/project"
)

// livePage is a fake Confluence page that remembers its version, so a test can
// run update twice and have the second run see what the first one did -- which
// is the only honest way to exercise the idempotence check, since the sha it
// compares is one markfluence computed itself.
type livePage struct {
	mu      sync.Mutex
	version int
	puts    int
	labels  int
}

func (p *livePage) client(t *testing.T) *client.ConfluenceClient {
	t.Helper()
	return clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "label"):
			p.labels++
			_, _ = w.Write([]byte(`{"results":[]}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/pages/"):
			p.puts++
			p.version++
			_, _ = w.Write([]byte(pageWithVersion("1", p.version, "2026-01-01T00:00:00Z")))
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/attachment"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(pageWithVersion("1", p.version, "2026-01-01T00:00:00Z")))
		}
	})
}

// publish runs one update against the page, recording its line as the batch
// loop does.
func publish(t *testing.T, c *client.ConfluenceClient, path string) *updateResult {
	t.Helper()
	logs := actionlog.NewCache()
	r := processFile(path, c, project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache(), logs)
	recordAction(logs, r)
	return r
}

// seedBase writes a base for a file directly, for the cases a real publish
// cannot produce.
func seedBase(t *testing.T, dir string, e actionlog.Entry) {
	t.Helper()
	root, err := project.FromPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.FS.Close() }()
	e.Status = actionlog.StatusOK
	if err := actionlog.For(root).Append(e); err != nil {
		t.Fatal(err)
	}
}

// The point of the whole change: the page moved past this copy, so publishing
// would destroy somebody's work. Refused rather than warned -- a warning that
// publishes anyway is the same data loss with extra text.
func TestADivergedPageIsRefused(t *testing.T) {
	p := &livePage{version: 9}
	dir, path := inProject(t, "---\npage_id: 1\n---\nMy edits.\n")
	seedBase(t, dir, actionlog.Entry{File: "f.md", PageID: "1", PageVersion: 7, PublishSHA256: "whatever"})

	r := publish(t, p.client(t), path)
	if r.ok {
		t.Fatalf("result = %+v, want a refusal", r)
	}
	if r.code != jsonout.CodeConflict {
		t.Errorf("code = %q, want CONFLICT: nothing about the file is defective", r.code)
	}
	if p.puts != 0 {
		t.Error("wrote to a page that had moved")
	}
	for _, want := range []string{"v7", "v9", "--force"} {
		if !strings.Contains(r.errMsg, want) {
			t.Errorf("message %q does not mention %q", r.errMsg, want)
		}
	}
}

// Refused even with no local edits, which is the worse case rather than the
// milder one: publishing would replace their work with the very bytes they
// started from.
func TestADivergedPageIsRefusedEvenWithNoLocalEdits(t *testing.T) {
	p := &livePage{version: 3}
	_, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	first := publish(t, p.client(t), path)
	if !first.ok {
		t.Fatalf("setup publish failed: %s", first.errMsg)
	}
	// Somebody edits the page in the UI: the version moves, the file does not.
	p.version++

	r := publish(t, p.client(t), path)
	if r.ok || r.code != jsonout.CodeConflict {
		t.Fatalf("result = %+v, want a CONFLICT refusal", r)
	}
}

func TestForcePublishesOverADivergedPage(t *testing.T) {
	force = true
	t.Cleanup(func() { force = false })

	p := &livePage{version: 9}
	dir, path := inProject(t, "---\npage_id: 1\n---\nMy edits.\n")
	seedBase(t, dir, actionlog.Entry{File: "f.md", PageID: "1", PageVersion: 7})

	r := publish(t, p.client(t), path)
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want ok/published", r)
	}
	if p.puts != 1 {
		t.Errorf("PUTs = %d, want 1: --force always PUTs", p.puts)
	}
}

// The replacement for the mtime skip: publish, then publish again unchanged.
// The second run must send no body PUT and must say why.
func TestAnUnchangedFileSkipsTheBodyPut(t *testing.T) {
	p := &livePage{version: 3}
	_, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	c := p.client(t)

	if r := publish(t, c, path); !r.ok || r.status != statusPublished {
		t.Fatalf("first run = %+v, want ok/published", r)
	}
	if p.puts != 1 {
		t.Fatalf("first run made %d PUTs, want 1", p.puts)
	}

	r := publish(t, c, path)
	if !r.ok || r.status != statusSkipped {
		t.Fatalf("second run = %+v, want ok/skipped", r)
	}
	if p.puts != 1 {
		t.Errorf("second run republished: PUTs = %d, want still 1", p.puts)
	}
	if r.bodyChanged == nil || *r.bodyChanged {
		t.Errorf("body_changed = %v, want false", r.bodyChanged)
	}
	if r.versionNew != r.versionPrev {
		t.Errorf("version %d -> %d, want them equal when the body did not move", r.versionPrev, r.versionNew)
	}
	if r.base == nil {
		t.Error("base = nil, want the base the first run recorded")
	}
}

// The bug the ordering fix exists for. The old skip returned before the
// attachment, width and label passes, so redrawing an image or adding a label
// without touching the prose did nothing at all.
func TestABodyUnchangedSkipStillAppliesLabels(t *testing.T) {
	p := &livePage{version: 3}
	_, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	c := p.client(t)

	if r := publish(t, c, path); !r.ok {
		t.Fatalf("first run: %s", r.errMsg)
	}
	before := p.labels

	// Add a labels: line. The rendered body is unchanged, so the body PUT is
	// skipped -- but the labels must still be asserted.
	if err := os.WriteFile(path, []byte("---\npage_id: 1\nlabels: [runbook]\n---\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := publish(t, c, path)
	if !r.ok {
		t.Fatalf("second run: %s", r.errMsg)
	}
	if p.puts != 1 {
		t.Errorf("republished the body for a labels-only change: PUTs = %d", p.puts)
	}
	if p.labels == before {
		t.Error("no label request: a body-unchanged skip must still assert labels")
	}
	// Something was written, so this is a publish rather than a skip.
	if r.status != statusPublished {
		t.Errorf("status = %q, want published: a label was applied", r.status)
	}
}

// Accrual: once the sha does the skipping, most runs skip, so a publish-only
// log would never keep a base current in a tree that is already published.
func TestABodyUnchangedSkipRecordsItsBase(t *testing.T) {
	p := &livePage{version: 3}
	dir, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	c := p.client(t)
	if r := publish(t, c, path); !r.ok {
		t.Fatalf("first run: %s", r.errMsg)
	}
	if r := publish(t, c, path); r.status != statusSkipped {
		t.Fatalf("second run status = %q, want skipped", r.status)
	}

	got, ok := baseIn(t, dir, "f.md")
	if !ok {
		t.Fatal("no base after a skip")
	}
	if got.PageVersion != 4 {
		t.Errorf("PageVersion = %d, want 4 (where the first publish left it)", got.PageVersion)
	}
}

// Scenario 9: the entry describes a different page, so comparing its version
// would invent "the page moved" out of a retarget. Discarded, which leaves the
// file unknown -- and unknown means publish.
func TestABaseNamingAnotherPageIsDiscarded(t *testing.T) {
	p := &livePage{version: 3}
	dir, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	seedBase(t, dir, actionlog.Entry{File: "f.md", PageID: "999", PageVersion: 40, PublishSHA256: "x"})

	r := publish(t, p.client(t), path)
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want ok/published", r)
	}
	if r.base != nil {
		t.Errorf("base = %+v, want nil: it describes another page", r.base)
	}
	if r.bodyChanged != nil {
		t.Errorf("body_changed = %v, want nil: no usable base, so no check ran", *r.bodyChanged)
	}
}

// Degrade per field, not per line: an export writes a version-only line during
// its walk, and that half still refuses a moved page.
func TestAVersionOnlyBaseStillRefusesAMovedPage(t *testing.T) {
	p := &livePage{version: 9}
	dir, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	seedBase(t, dir, actionlog.Entry{File: "f.md", PageID: "1", PageVersion: 7})

	r := publish(t, p.client(t), path)
	if r.ok || r.code != jsonout.CodeConflict {
		t.Fatalf("result = %+v, want a CONFLICT refusal from the version alone", r)
	}
}

// And the other half on its own: a sha with no version cannot speak to
// divergence, but still answers "would publishing change anything".
func TestAShaOnlyBaseStillSkipsAnUnchangedBody(t *testing.T) {
	p := &livePage{version: 3}
	dir, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	c := p.client(t)
	first := publish(t, c, path)
	if !first.ok {
		t.Fatalf("first run: %s", first.errMsg)
	}
	// A line carrying the sha and no version at all.
	seedBase(t, dir, actionlog.Entry{File: "f.md", PageID: "1", PublishSHA256: first.publishSHA})

	r := publish(t, c, path)
	if !r.ok || r.status != statusSkipped {
		t.Fatalf("result = %+v, want ok/skipped", r)
	}
	if p.puts != 1 {
		t.Errorf("PUTs = %d, want still 1", p.puts)
	}
}

func TestNoBaseMeansPublishSilently(t *testing.T) {
	p := &livePage{version: 3}
	_, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")

	r := publish(t, p.client(t), path)
	if !r.ok || r.status != statusPublished {
		t.Fatalf("result = %+v, want ok/published", r)
	}
	if r.base != nil || r.bodyChanged != nil {
		t.Errorf("base/body_changed = %+v/%v, want both nil", r.base, r.bodyChanged)
	}
	if len(r.warnings) != 0 {
		t.Errorf("warnings = %q, want none: an unknown base is reported once per run, not per file", r.warnings)
	}
}
