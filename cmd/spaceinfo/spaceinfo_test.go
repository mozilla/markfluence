package spaceinfo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
)

// stub is the server a test serves: one space, its pages, and whichever of the
// optional reads it wants to succeed.
type stub struct {
	// pages is served as one response page per element, so a test can make the
	// walk span several requests. A row is "id,status,parentId,createdAt,versionAt".
	pages [][]string
	// operations is the ?expand=operations array; nil omits the key entirely,
	// which is the "could not be determined" case.
	operations []string
	omitOps    bool
	// spaceStates is state/settings' answer; nil makes it 403 (not an admin).
	spaceStates []string
	// homepageStates is the homepage's state/available answer; nil makes it 403.
	homepageStates []string
	homepageID     string
	omitHomepage   bool
	spaceStatus    string
	// walkFails makes the page walk error partway, after one good response.
	walkFails bool
}

// iso renders a timestamp the way Confluence does: **milliseconds**, as in
// 2026-09-15T10:04:00.000Z. That precision is not decoration -- a stub serving
// second-precision stamps hid a real bug, because comparing them against a
// formatted cutoff lexicographically puts a row inside the cutoff's own second
// below it ('.' sorts under 'Z'). The suite serves what the API serves.
func iso(daysAgo int) string {
	return time.Now().UTC().AddDate(0, 0, -daysAgo).Format("2006-01-02T15:04:05.000Z07:00")
}

// row builds a page row for the stub: created and lastEdited are days ago.
func row(id, status, parentID string, created, lastEdited int) string {
	return strings.Join([]string{id, status, parentID, iso(created), iso(lastEdited)}, ",")
}

// client starts the stub, returning the client and a recorder of every path it
// saw -- the recorder is returned rather than stored on stub, which is a value
// receiver, so an assignment to a field would not reach the caller.
func (s stub) client(t *testing.T) (*client.ConfluenceClient, *[]string) {
	t.Helper()
	var seen []string
	served := 0
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/state/settings"):
			if s.spaceStates == nil {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"statusCode":403,"message":"PermissionException"}`))
				return
			}
			_, _ = w.Write([]byte(statesJSON("spaceContentStates", s.spaceStates)))
		case strings.HasSuffix(r.URL.Path, "/state/available"):
			if s.homepageStates == nil {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"statusCode":403,"message":"PermissionException"}`))
				return
			}
			_, _ = w.Write([]byte(statesJSON("spaceContentStates", s.homepageStates)))
		case strings.Contains(r.URL.Path, "/api/v2/spaces/") && strings.HasSuffix(r.URL.Path, "/pages"):
			if s.walkFails && served > 0 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if served >= len(s.pages) {
				_, _ = w.Write([]byte(`{"results":[],"_links":{}}`))
				return
			}
			page := s.pages[served]
			served++
			more := served < len(s.pages) || s.walkFails
			_, _ = w.Write([]byte(pagesJSON(page, more)))
		case strings.Contains(r.URL.Path, "/rest/api/space/"):
			_, _ = w.Write([]byte(s.spaceJSON()))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return c, &seen
}

func statesJSON(key string, names []string) string {
	items := make([]string, 0, len(names))
	for i, n := range names {
		items = append(items, fmt.Sprintf(`{"id":%d,"name":%q,"color":"#000000"}`, i+10, n))
	}
	return fmt.Sprintf(`{%q:[%s],"customContentStates":[]}`, key, strings.Join(items, ","))
}

// pagesJSON renders one response page. more adds a cursor, so the walk
// continues -- which is also how a *short* page mid-collection is served:
// termination is the missing next link, never the row count.
func pagesJSON(rows []string, more bool) string {
	items := make([]string, 0, len(rows))
	for _, r := range rows {
		f := strings.Split(r, ",")
		parent := ""
		if f[2] != "" {
			parent = fmt.Sprintf(`"parentId":%q,`, f[2])
		}
		items = append(items, fmt.Sprintf(
			`{"id":%q,"status":%q,%s"createdAt":%q,"version":{"number":1,"createdAt":%q},`+
				`"title":"T","totalSize":99999}`, f[0], f[1], parent, f[3], f[4]))
	}
	next := ""
	if more {
		next = `,"next":"/wiki/api/v2/spaces/77/pages?cursor=next"`
	}
	return fmt.Sprintf(`{"results":[%s],"totalSize":99999,"_links":{"base":"/wiki"%s}}`,
		strings.Join(items, ","), next)
}

func (s stub) spaceJSON() string {
	ops := `"operations":[{"operation":"read","targetType":"space"}]`
	if s.operations != nil {
		items := make([]string, 0, len(s.operations))
		for _, o := range s.operations {
			parts := strings.SplitN(o, ":", 2)
			items = append(items, fmt.Sprintf(`{"operation":%q,"targetType":%q}`, parts[0], parts[1]))
		}
		ops = `"operations":[` + strings.Join(items, ",") + `]`
	}
	if s.omitOps {
		ops = `"_expandable":{"operations":""}`
	}
	home := `"homepage":{"id":"500","title":"Space Home"}`
	if s.homepageID != "" {
		home = fmt.Sprintf(`"homepage":{"id":%q,"title":"Space Home"}`, s.homepageID)
	}
	if s.omitHomepage {
		home = `"_x":0`
	}
	status := s.spaceStatus
	if status == "" {
		status = "current"
	}
	// id is a bare number and homepage.id is a string, which is what the real
	// v1 route does -- a stub serving both as strings hid exactly that bug.
	return fmt.Sprintf(`{"id":77,"key":"ENG","name":"Engineering","type":"global",`+
		`"status":%q,%s,%s,"description":{"plain":{"value":"The eng space"}},`+
		`"metadata":{"labels":{"results":[{"name":"docs","prefix":"team"}]}}}`,
		status, home, ops)
}

// build runs the command's own gathering against the stub, which is what both
// renderers consume.
func (s stub) build(t *testing.T, days int) report {
	t.Helper()
	c, _ := s.client(t)
	space, err := c.GetSpace("ENG")
	if err != nil {
		t.Fatalf("GetSpace: %v", err)
	}
	if space == nil {
		t.Fatal("GetSpace returned no space")
	}
	return buildReport(c, space, days)
}

// mustBuild is build for a table entry, where a t.Fatalf inside a map literal
// would be awkward.
func (s stub) mustBuild(t *testing.T) report {
	t.Helper()
	return s.build(t, 7)
}

// --- counts --------------------------------------------------------------------

// The counts come from each row, never from totalSize -- which the stub serves
// as 99999 on every response precisely so a reader of this test sees that
// nothing consults it. docs/confluence/search.md measures totalSize drifting
// and forbids reporting it as a count.
func TestCountsIgnoreTotalSize(t *testing.T) {
	s := stub{pages: [][]string{{
		row("1", "current", "", 30, 30),
		row("2", "current", "1", 30, 30),
	}}}
	r := s.build(t, 7)
	if r.counts == nil {
		t.Fatal("no counts")
	}
	if r.counts.Current != 2 {
		t.Errorf("current = %d, want 2 (not totalSize's 99999)", r.counts.Current)
	}
}

// The walk spans response pages, and every one of them is short -- two rows
// against a limit of 250 -- with a cursor saying more remains. Termination is
// the missing next link; a row count is never the signal.
func TestWalkFollowsTheCursorAcrossShortPages(t *testing.T) {
	s := stub{pages: [][]string{
		{row("1", "current", "", 30, 30), row("2", "current", "1", 30, 30)},
		{row("3", "current", "1", 30, 30)},
		{row("4", "archived", "1", 30, 30)},
	}}
	r := s.build(t, 7)
	if r.counts.Current != 3 || r.counts.Archived != 1 {
		t.Errorf("counts = %+v, want 3 current / 1 archived", r.counts)
	}
}

// The route's default includes archived pages, so the split comes from each
// row's status. An archived row must not land in current.
func TestArchivedPagesAreSplitOut(t *testing.T) {
	s := stub{pages: [][]string{{
		row("1", "current", "", 30, 30),
		row("2", "archived", "", 30, 30),
		row("3", "archived", "1", 30, 30),
	}}}
	r := s.build(t, 7)
	if r.counts.Current != 1 || r.counts.Archived != 2 {
		t.Errorf("counts = %+v, want 1 current / 2 archived", r.counts)
	}
	// And an archived root is not a root page: roots describes the live tree.
	if r.counts.Roots != 1 {
		t.Errorf("roots = %d, want 1", r.counts.Roots)
	}
}

// A walk that fails partway reports nothing rather than a partial count: this
// command's output is mostly counts, and a wrong one is worse than none.
func TestAFailedWalkReportsNoCounts(t *testing.T) {
	s := stub{
		pages:     [][]string{{row("1", "current", "", 30, 30)}, {row("2", "current", "", 30, 30)}},
		walkFails: true,
	}
	r := s.build(t, 7)
	if r.counts != nil {
		t.Errorf("counts = %+v, want nil after a failed walk", r.counts)
	}
	// The rest of the report still stands.
	if r.space.Key != "ENG" {
		t.Errorf("space = %+v, want the identity intact", r.space)
	}
	res := r.jsonResult()
	if res.Pages != nil || res.Recent != nil {
		t.Errorf("pages/recent = %+v/%+v, want both null", res.Pages, res.Recent)
	}
}

func TestRootsCountsPagesWithNoParent(t *testing.T) {
	s := stub{pages: [][]string{{
		row("1", "current", "", 30, 30),
		row("2", "current", "", 30, 30),
		row("3", "current", "1", 30, 30),
	}}}
	r := s.build(t, 7)
	if r.counts.Roots != 2 {
		t.Errorf("roots = %d, want 2", r.counts.Roots)
	}
}

// --- the window ----------------------------------------------------------------

// pages_touched counts PAGES, not edits. This page's history holds nine
// versions inside the window; only the latest timestamp is available, so it
// contributes 1 -- and that is the number, not 9.
func TestTouchedCountsPagesNotEdits(t *testing.T) {
	s := stub{pages: [][]string{{row("1", "current", "", 400, 1)}}}
	r := s.build(t, 7)
	if r.counts.Touched != 1 {
		t.Errorf("touched = %d, want 1: this counts pages, not edits", r.counts.Touched)
	}
	if r.counts.Created != 0 {
		t.Errorf("created = %d, want 0: the page predates the window", r.counts.Created)
	}
}

// A page created inside the window has its first version inside it too, so it
// is counted in both -- and the overlap is reported, so the two are never
// added together.
func TestCreatedAndTouchedOverlapIsReported(t *testing.T) {
	s := stub{pages: [][]string{{
		row("1", "current", "", 2, 2),   // created and touched in the window
		row("2", "current", "", 400, 1), // only touched
		row("3", "current", "", 400, 400),
	}}}
	r := s.build(t, 7)
	if r.counts.Created != 1 || r.counts.Touched != 2 || r.counts.CreatedTouched != 1 {
		t.Errorf("counts = %+v, want created 1 / touched 2 / overlap 1", r.counts)
	}
	res := r.jsonResult()
	if res.Recent.PagesCreatedAndTouched != 1 {
		t.Errorf("pages_created_and_touched = %d, want 1", res.Recent.PagesCreatedAndTouched)
	}
	// The human line names it inline, for the same reason.
	if !strings.Contains(r.human(), "also created then") {
		t.Errorf("human output does not name the overlap:\n%s", r.human())
	}
}

// --since 0 is today only, and it has to actually count today.
//
// The regression for a real bug: the cutoff was "now minus N days", so at N=0
// it was *this instant* and nothing could be after it -- against a live
// instance the counts were always zero while the help promised "today only".
// The window now opens at midnight UTC, so a page touched earlier today counts
// and one from three days ago does not.
func TestSinceZeroCountsToday(t *testing.T) {
	s := stub{pages: [][]string{{
		row("1", "current", "", 0, 0),
		row("2", "current", "", 3, 3),
	}}}
	r := s.build(t, 0)
	if r.counts.Touched != 1 || r.counts.Created != 1 {
		t.Errorf("counts = %+v, want 1 created / 1 touched for a zero-day window", r.counts)
	}
	// And the wording must not read like a 24-hour window, or a zero for today
	// is indistinguishable from a zero for yesterday-and-today.
	out := r.human()
	if !strings.Contains(out, "1 today") {
		t.Errorf("human output = %q, want it to say today", out)
	}
	if strings.Contains(out, "in the last day") {
		t.Errorf("human output = %q, must not read as a 24-hour window", out)
	}
}

// The window opens at midnight, so it is a calendar boundary rather than a
// rolling one: a page touched late yesterday is outside --since 0 and inside
// --since 1, whatever time of day the command runs.
func TestWindowOpensAtMidnight(t *testing.T) {
	now := time.Date(2026, 9, 18, 14, 30, 0, 0, time.UTC)
	if got := windowStart(now, 0); !got.Equal(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("windowStart(0) = %v, want midnight today", got)
	}
	if got := windowStart(now, 7); !got.Equal(time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("windowStart(7) = %v, want midnight seven days back", got)
	}
}

// A timestamp at the cutoff's own second is inside the window. Compared as
// strings it was not: Confluence sends milliseconds and a formatted cutoff has
// none, so "...T00:00:00.000Z" sorts below "...T00:00:00Z".
func TestACutoffSecondStampIsInsideTheWindow(t *testing.T) {
	cutoff := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	if !inWindow("2026-09-11T00:00:00.000Z", cutoff) {
		t.Error("a stamp at the cutoff instant read as outside the window")
	}
	if inWindow("2026-09-10T23:59:59.999Z", cutoff) {
		t.Error("a stamp a millisecond before the cutoff read as inside")
	}
	// A stamp that will not parse cannot be placed, so it is not counted
	// rather than counted wrongly.
	if inWindow("not a timestamp", cutoff) {
		t.Error("an unparseable stamp was counted")
	}
}

// last_activity is the newest version timestamp across current pages -- not
// the last row walked, and not createdAt.
func TestLastActivityIsTheNewestVersionTimestamp(t *testing.T) {
	s := stub{pages: [][]string{{
		row("1", "current", "", 400, 2),
		row("2", "current", "", 400, 40),
	}}}
	r := s.build(t, 7)
	want := iso(2)[:10]
	if !strings.HasPrefix(r.counts.LastActivity, want) {
		t.Errorf("last activity = %q, want it to start %q", r.counts.LastActivity, want)
	}
}

// --- access --------------------------------------------------------------------

func TestAccessMapsOperations(t *testing.T) {
	for name, tc := range map[string]struct {
		ops                             []string
		wantRead, wantCreate, wantAdmin bool
	}{
		"read only":     {[]string{"read:space"}, true, false, false},
		"can create":    {[]string{"read:space", "create:page"}, true, true, false},
		"comments only": {[]string{"read:space", "create:comment"}, true, false, false},
		"admin":         {[]string{"read:space", "create:page", "administer:space"}, true, true, true},
		// export:space is not admin: an admin's row carries it, but so can a
		// non-admin's, so the predicate is administer:space alone.
		"export is not admin": {[]string{"read:space", "export:space"}, true, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			r := stub{operations: tc.ops, pages: [][]string{{row("1", "current", "", 30, 30)}}}.build(t, 7)
			res := r.jsonResult()
			if res.Access == nil {
				t.Fatal("access is null though operations came back")
			}
			if res.Access.Read != tc.wantRead || res.Access.CreatePages != tc.wantCreate ||
				res.Access.Admin != tc.wantAdmin {
				t.Errorf("access = %+v, want read=%v create=%v admin=%v",
					res.Access, tc.wantRead, tc.wantCreate, tc.wantAdmin)
			}
		})
	}
}

// A missing operations expansion is "could not be determined", not "may do
// nothing" -- so null, and the human row says so rather than printing "none".
func TestMissingOperationsIsNullNotNoAccess(t *testing.T) {
	r := stub{omitOps: true, pages: [][]string{{row("1", "current", "", 30, 30)}}}.build(t, 7)
	if res := r.jsonResult(); res.Access != nil {
		t.Errorf("access = %+v, want null when the expansion did not come back", res.Access)
	}
	if !strings.Contains(r.human(), "could not be read") {
		t.Errorf("human output does not distinguish unknown access:\n%s", r.human())
	}
}

// The word "write" must not appear: create:page is a space grant and page edit
// is not a space property at all (#168), so promising write would be wrong.
func TestAccessNeverSaysWrite(t *testing.T) {
	r := stub{operations: []string{"read:space", "create:page"},
		pages: [][]string{{row("1", "current", "", 30, 30)}}}.build(t, 7)
	out := r.human()
	if strings.Contains(strings.ToLower(out), "write") {
		t.Errorf("output says \"write\" about a space grant:\n%s", out)
	}
	if !strings.Contains(out, "create pages") {
		t.Errorf("output does not say \"create pages\":\n%s", out)
	}
}

// --- page statuses -------------------------------------------------------------

// A space admin gets the space's own configured list, and it is labelled as
// the space's.
func TestStatusesPreferTheSpaceConfiguration(t *testing.T) {
	r := stub{
		spaceStates:    []string{"Rough draft", "Verified"},
		homepageStates: []string{"Only", "The", "Homepage"},
		pages:          [][]string{{row("1", "current", "", 30, 30)}},
	}.build(t, 7)

	res := r.jsonResult()
	if res.PageStatuses.Source == nil || *res.PageStatuses.Source != "space" {
		t.Fatalf("source = %v, want \"space\"", res.PageStatuses.Source)
	}
	if res.PageStatuses.ProbePageID != nil {
		t.Errorf("probe_page_id = %v, want null for the space source", res.PageStatuses.ProbePageID)
	}
	if got := strings.Join(*res.PageStatuses.Names, ","); got != "Rough draft,Verified" {
		t.Errorf("names = %q", got)
	}
	if !strings.Contains(r.human(), "page statuses:") {
		t.Errorf("human label is not the plain one:\n%s", r.human())
	}
}

// A non-admin falls back to the homepage's own list -- and both the label and
// the JSON say that is what it is, because it is not the space's list:
// Confluence decides the list per (caller, page).
func TestStatusesFallBackToTheHomepageAndSaySo(t *testing.T) {
	r := stub{
		homepageStates: []string{"Rough draft", "In progress"},
		pages:          [][]string{{row("1", "current", "", 30, 30)}},
	}.build(t, 7)

	res := r.jsonResult()
	if res.PageStatuses.Source == nil || *res.PageStatuses.Source != "page" {
		t.Fatalf("source = %v, want \"page\"", res.PageStatuses.Source)
	}
	if res.PageStatuses.ProbePageID == nil || *res.PageStatuses.ProbePageID != "500" {
		t.Errorf("probe_page_id = %v, want the homepage id", res.PageStatuses.ProbePageID)
	}
	if !strings.Contains(r.human(), "page statuses (yours, on the homepage):") {
		t.Errorf("human label does not name the source:\n%s", r.human())
	}
}

// Neither source readable is a normal outcome for a collaborator who can create
// pages but cannot edit the homepage (#168), so it reports rather than fails --
// and points at the command that can answer.
func TestStatusesUnavailableIsReportedNotFailed(t *testing.T) {
	r := stub{pages: [][]string{{row("1", "current", "", 30, 30)}}}.build(t, 7)

	res := r.jsonResult()
	if res.PageStatuses.Source != nil || res.PageStatuses.Names != nil {
		t.Errorf("page_statuses = %+v, want source and names null", res.PageStatuses)
	}
	out := r.human()
	if !strings.Contains(out, "not available") || !strings.Contains(out, "page-info PAGE") {
		t.Errorf("human output does not explain or redirect:\n%s", out)
	}
}

// --- the rest ------------------------------------------------------------------

// An archived space still reports its counts; status is how a reader knows how
// to read them, which is why it sits beside the type rather than below.
func TestAnArchivedSpaceStillReportsCounts(t *testing.T) {
	r := stub{spaceStatus: "archived", pages: [][]string{{row("1", "current", "", 30, 30)}}}.build(t, 7)
	if r.counts == nil || r.counts.Current != 1 {
		t.Fatalf("counts = %+v, want them reported", r.counts)
	}
	if !strings.Contains(r.human(), "global (archived)") {
		t.Errorf("human output does not surface the archived status:\n%s", r.human())
	}
	if res := r.jsonResult(); res.Status != "archived" {
		t.Errorf("status = %q, want archived", res.Status)
	}
}

// A space with no homepage skips both the homepage row and the status probe,
// rather than asking about an empty id.
func TestNoHomepageSkipsTheProbe(t *testing.T) {
	s := stub{omitHomepage: true, pages: [][]string{{row("1", "current", "", 30, 30)}}}
	c, requests := s.client(t)
	space, err := c.GetSpace("ENG")
	if err != nil || space == nil {
		t.Fatalf("GetSpace: %v", err)
	}
	r := buildReport(c, space, 7)
	for _, p := range *requests {
		if strings.Contains(p, "/state/available") {
			t.Errorf("probed state/available with no homepage: %v", *requests)
		}
	}
	if strings.Contains(r.human(), "homepage:") {
		t.Errorf("human output has a homepage row for a space with none:\n%s", r.human())
	}
}

func TestUnknownSpaceIsNil(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"statusCode":404,"message":"No space found with key 'NOPE'"}`))
	})
	space, err := c.GetSpace("NOPE")
	if err != nil {
		t.Fatalf("GetSpace = %v, want nil error so the caller can word the typo", err)
	}
	if space != nil {
		t.Errorf("space = %+v, want nil", space)
	}
}

// Every field always marshals, per internal/schematest's rule: no omitempty,
// so the tests' closed schema and required catch an added or renamed one.
func TestJSONResultAlwaysCarriesEveryField(t *testing.T) {
	r := stub{pages: [][]string{{row("1", "current", "", 30, 30)}}}.build(t, 7)
	blob, err := json.Marshal(r.jsonResult())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{
		"ok", "key", "name", "id", "type", "status", "description", "labels",
		"homepage_id", "homepage_title", "access", "page_statuses", "pages", "recent",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("result is missing %q", key)
		}
	}
}
