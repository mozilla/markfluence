package update

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pagestatus"
	"github.com/mozilla/markfluence/internal/project"
)

// statusVocabulary is the space's own statuses; no custom ones.
const statusVocabulary = `{"spaceContentStates":[
	{"id":10,"color":"#ffc400","name":"Rough draft"},
	{"id":12,"color":"#57d9a3","name":"Ready for review"}],"customContentStates":[]}`

// statusServer publishes normally and serves the state routes, recording every
// request path so a test can assert on what was *not* asked.
//
// liveState is the body for a GET of /state: `{}` is a page with no status.
func statusServer(t *testing.T, liveState string) (*client.ConfluenceClient, *[]string) {
	t.Helper()
	var paths []string
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/state/available"):
			_, _ = w.Write([]byte(statusVocabulary))
		case strings.HasSuffix(r.URL.Path, "/state"):
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(liveState))
				return
			}
			_, _ = w.Write([]byte(`{"contentState":{"id":12,"name":"Ready for review","color":"#57d9a3"}}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		case r.Method == http.MethodPut:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})
	return c, &paths
}

func publishWith(t *testing.T, c *client.ConfluenceClient, path string) *updateResult {
	t.Helper()
	return processFile(path, c, project.NewCache(""), linkindex.NewCache(),
		pagedoc.NewUserCache(), actionlog.NewCache(), pagestatus.NewCache())
}

// stateRequests filters the recorded paths down to the state routes.
func stateRequests(paths []string) []string {
	var out []string
	for _, p := range paths {
		if strings.Contains(p, "/state") {
			out = append(out, p)
		}
	}
	return out
}

// The property that keeps this free for every tree not using the field: a file
// with no page_status makes no state request at all -- no write, and no read
// either. Asserted on the requests the stub saw rather than on the absence of a
// write, which is what "not even read" means.
func TestNoStatusDeclaredAsksNothing(t *testing.T) {
	c, paths := statusServer(t, `{}`)
	_, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	r := publishWith(t, c, path)
	if !r.ok {
		t.Fatalf("result = %+v, want ok", r)
	}
	if got := stateRequests(*paths); len(got) != 0 {
		t.Errorf("state requests = %v, want none", got)
	}
	if r.pageStatus != nil {
		t.Errorf("pageStatus = %+v, want nil for an undeclared field", r.pageStatus)
	}
}

func TestADeclaredStatusIsAsserted(t *testing.T) {
	c, paths := statusServer(t, `{}`)
	_, path := inProject(t, "---\npage_id: 1\npage_status: Ready for review\n---\nHello.\n")
	r := publishWith(t, c, path)
	if !r.ok {
		t.Fatalf("result = %+v, want ok: %s", r, r.errMsg)
	}
	if r.pageStatus == nil || r.pageStatus.Name != "Ready for review" || r.pageStatus.Action != "set" {
		t.Fatalf("pageStatus = %+v, want Ready for review/set", r.pageStatus)
	}
	var puts int
	for _, p := range stateRequests(*paths) {
		if strings.HasPrefix(p, "PUT") {
			puts++
		}
	}
	if puts != 1 {
		t.Errorf("state requests = %v, want one PUT", stateRequests(*paths))
	}
}

// A case variant publishes, and publishes the space's own spelling: only the id
// travels, so the file's spelling cannot reach the page.
func TestADeclaredStatusMatchesCaseInsensitively(t *testing.T) {
	c, _ := statusServer(t, `{}`)
	_, path := inProject(t, "---\npage_id: 1\npage_status: ready for review\n---\nHello.\n")
	r := publishWith(t, c, path)
	if !r.ok {
		t.Fatalf("result = %+v, want ok: %s", r, r.errMsg)
	}
	if r.pageStatus == nil || r.pageStatus.Name != "Ready for review" {
		t.Errorf("pageStatus = %+v, want the space's spelling", r.pageStatus)
	}
}

// An already-matching status writes nothing, because a state write bumps the
// page version: a run that changed nothing must not add one.
func TestAnUnchangedStatusIsNotRewritten(t *testing.T) {
	live := `{"contentState":{"id":12,"name":"Ready for review","color":"#57d9a3"}}`
	c, paths := statusServer(t, live)
	_, path := inProject(t, "---\npage_id: 1\npage_status: Ready for review\n---\nHello.\n")
	r := publishWith(t, c, path)
	if !r.ok {
		t.Fatalf("result = %+v, want ok: %s", r, r.errMsg)
	}
	if r.pageStatus == nil || r.pageStatus.Action != "unchanged" {
		t.Fatalf("pageStatus = %+v, want unchanged", r.pageStatus)
	}
	for _, p := range stateRequests(*paths) {
		if strings.HasPrefix(p, "PUT") {
			t.Errorf("state requests = %v, want no PUT", stateRequests(*paths))
		}
	}
}

// A name the space does not offer fails the file, and fails it before the body
// PUT: the report is about the file, and half-publishing it would be worse than
// not publishing it.
func TestAnUnknownStatusFailsTheFileBeforeAnyWrite(t *testing.T) {
	c, paths := statusServer(t, `{}`)
	_, path := inProject(t, "---\npage_id: 1\npage_status: Reviewed\n---\nHello.\n")
	r := publishWith(t, c, path)
	if r.ok {
		t.Fatal("result is ok, want a validation failure")
	}
	if r.code != "VALIDATION" {
		t.Errorf("code = %q, want VALIDATION", r.code)
	}
	if !strings.Contains(r.errMsg, "Ready for review") {
		t.Errorf("errMsg = %q, want the space's own statuses named", r.errMsg)
	}
	for _, p := range *paths {
		if strings.HasPrefix(p, "PUT") {
			t.Errorf("requests = %v, want nothing written", *paths)
		}
	}
}

// A present-but-empty value is a defect in the file, reported without asking
// Confluence anything -- there is no spelling of page_status that clears one,
// so an empty value cannot be an instruction.
func TestAnEmptyStatusFailsOffline(t *testing.T) {
	c, paths := statusServer(t, `{}`)
	_, path := inProject(t, "---\npage_id: 1\npage_status:\n---\nHello.\n")
	r := publishWith(t, c, path)
	if r.ok {
		t.Fatal("result is ok, want a validation failure")
	}
	if !strings.Contains(r.errMsg, "has no value") {
		t.Errorf("errMsg = %q", r.errMsg)
	}
	if got := stateRequests(*paths); len(got) != 0 {
		t.Errorf("state requests = %v, want none", got)
	}
}

// Setting a status is a change, so a file whose body is already published still
// reports "published" rather than "skipped" -- it really did move the page.
func TestSettingAStatusCountsAsAChange(t *testing.T) {
	r := &updateResult{pageStatus: nil}
	if setAStatus(r) {
		t.Error("an undeclared status counts as a change")
	}
	r.pageStatus = &jsonout.PageStatus{Name: "Ready for review", Action: "unchanged"}
	if setAStatus(r) {
		t.Error("an unchanged status counts as a change; it wrote nothing")
	}
	r.pageStatus = &jsonout.PageStatus{Name: "Ready for review", Action: "set"}
	if !setAStatus(r) {
		t.Error("a set status does not count as a change")
	}
}

// The regression for the bug the first live create found: a status write bumps
// the page version, so a base recorded from the body PUT's version leaves the
// page one ahead of its own base and the next run refuses the file as diverged.
//
// The stub answers v9 for the post-write read, which is what must be recorded --
// not the body PUT's v4.
func TestAStatusWriteIsRecordedAtThePagesFinalVersion(t *testing.T) {
	var wroteStatus bool
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/state/available"):
			_, _ = w.Write([]byte(statusVocabulary))
		case strings.HasSuffix(r.URL.Path, "/state"):
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			wroteStatus = true
			_, _ = w.Write([]byte(`{"contentState":{"id":12,"name":"Ready for review","color":"#57d9a3"}}`))
		case r.Method == http.MethodGet:
			// Before the status write the page is at v3; afterwards the status
			// write has moved it, and the stub reports where it landed.
			if wroteStatus {
				_, _ = w.Write([]byte(pageWithVersion("1", 9, "2026-01-01T00:00:00Z")))
				return
			}
			_, _ = w.Write([]byte(pageWithVersion("1", 3, "2020-01-01T00:00:00Z")))
		case r.Method == http.MethodPut:
			_, _ = w.Write([]byte(pageWithVersion("1", 4, "2026-01-01T00:00:00Z")))
		}
	})

	_, path := inProject(t, "---\npage_id: 1\npage_status: Ready for review\n---\nHello.\n")
	r := publishWith(t, c, path)
	if !r.ok {
		t.Fatalf("result = %+v, want ok: %s", r, r.errMsg)
	}
	if got := r.loggedVersion(); got != 9 {
		t.Errorf("loggedVersion = %d, want 9: the base must name where the page ended up", got)
	}
	// The human output still reports the body publish it actually made.
	if r.versionNew != 4 {
		t.Errorf("versionNew = %d, want 4: that is the version the body PUT produced", r.versionNew)
	}
}

// Without a status write there is no extra read and nothing to correct.
func TestNoStatusWriteLeavesTheLoggedVersionAlone(t *testing.T) {
	c, _ := statusServer(t, `{}`)
	_, path := inProject(t, "---\npage_id: 1\n---\nHello.\n")
	r := publishWith(t, c, path)
	if r.logVersion != 0 {
		t.Errorf("logVersion = %d, want 0 (unset)", r.logVersion)
	}
	if r.loggedVersion() != r.versionNew {
		t.Errorf("loggedVersion = %d, want versionNew %d", r.loggedVersion(), r.versionNew)
	}
}
