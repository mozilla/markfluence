package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// spaceRows builds n synthetic space rows from base, every third one granting
// create:page and the first granting administer:space.
func spaceRows(base, n int) string {
	rows := make([]string, 0, n)
	for i := base; i < base+n; i++ {
		ops := `{"operation":"read","targetType":"space"}`
		if i%3 == 0 {
			ops += `,{"operation":"create","targetType":"page"}`
		}
		if i == 0 {
			ops += `,{"operation":"administer","targetType":"space"}`
		}
		rows = append(rows, fmt.Sprintf(
			`{"id":%d,"key":"S%d","name":"Space %d","operations":[%s]}`, 1000+i, i, i, ops))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

// spaceServer serves /rest/api/space, handing each offset whatever page(start)
// returns, and records the offsets asked for.
func spaceServer(t *testing.T, page func(start int) string) (*ConfluenceClient, *[]int) {
	t.Helper()
	var starts []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wiki/rest/api/space" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		starts = append(starts, start)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"results":%s,"start":%d,"limit":250}`, page(start), start)
	}))
	t.Cleanup(srv.Close)
	return New(Config{SiteURL: srv.URL, Username: "u", Token: "t"}), &starts
}

// The regression for the trap that shaped this whole file, measured against
// the live instance: asked for 250 from start=0 the route answered **200**,
// and start=200 then answered 250 more, against 525 spaces in total. A short
// page is not the end here.
//
// listV1 stops on exactly that short page, so pointing this route at it
// reports 200 spaces and truncates the survey with no error -- which the first
// version of this probe did, producing a confident wrong answer. Termination
// must be an empty page and nothing else.
func TestAShortSpacePageIsNotTheEnd(t *testing.T) {
	c, starts := spaceServer(t, func(start int) string {
		switch start {
		case 0:
			return spaceRows(0, 200) // short, and not the end
		case 200:
			return spaceRows(200, 250)
		case 450:
			return spaceRows(450, 75)
		default:
			return "[]"
		}
	})

	var seen int
	err := c.WalkSpaceOperations(func(SpaceRef, []SpaceOperation) error {
		seen++
		return nil
	})
	if err != nil {
		t.Fatalf("WalkSpaceOperations: %v", err)
	}
	if seen != 525 {
		t.Errorf("walked %d spaces, want 525: a short page is not the end of this collection", seen)
	}
	// Offsets advance by rows returned, not by the limit asked for -- the two
	// differ here, which is the trap.
	want := []int{0, 200, 450, 525}
	if len(*starts) != len(want) {
		t.Fatalf("offsets = %v, want %v", *starts, want)
	}
	for i, w := range want {
		if (*starts)[i] != w {
			t.Errorf("offsets = %v, want %v", *starts, want)
			break
		}
	}
}

// An empty first page is a legitimate answer, not an error.
func TestNoVisibleSpacesWalksNothing(t *testing.T) {
	c, _ := spaceServer(t, func(int) string { return "[]" })
	var seen int
	err := c.WalkSpaceOperations(func(SpaceRef, []SpaceOperation) error {
		seen++
		return nil
	})
	if err != nil || seen != 0 {
		t.Errorf("= %d spaces, %v; want 0, nil", seen, err)
	}
}

// The walk is bounded, because termination is an empty page: a server that
// clamped start would otherwise hand back rows forever. searchCQLBounded's
// guard, for the same hazard.
func TestTheSpaceWalkIsBounded(t *testing.T) {
	c, starts := spaceServer(t, func(int) string { return spaceRows(0, 250) })
	var seen int
	err := c.WalkSpaceOperations(func(SpaceRef, []SpaceOperation) error {
		seen++
		return nil
	})
	// An **error**, not a truncated list: returning the rows collected so far
	// would report "visible spaces: 50000" over the same page two hundred
	// times, exit 0 -- the confident wrong answer this pager exists to stop.
	if err == nil {
		t.Fatal("hitting the page guard returned no error; a short answer that looks complete " +
			"is the failure this route is written to avoid")
	}
	if !strings.Contains(err.Error(), "start offset") {
		t.Errorf("err = %v, want it to name the likely cause", err)
	}
	if len(*starts) != maxSpacePages {
		t.Errorf("made %d requests against a server that never ends, want the %d-page guard",
			len(*starts), maxSpacePages)
	}
}

// An account with no personal space -- every service-account token -- is a
// real answer, not a failure, and must not be confused with a lookup that
// could not run.
func TestCurrentUserWithoutAPersonalSpace(t *testing.T) {
	c := userRouteServer(t, `{"accountId":"712020:abc","displayName":"SRE API Token",
		"accountType":"app","accountStatus":"active","isExternalCollaborator":false,"isGuest":false}`)
	u, err := c.CurrentUser()
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if u.PersonalSpace != nil {
		t.Errorf("PersonalSpace = %+v, want nil", u.PersonalSpace)
	}
	if u.AccountType != "app" {
		t.Errorf("AccountType = %q, want app", u.AccountType)
	}
}

// The personal space comes from the expansion, never from the account id: this
// instance carries keys in both an email form and an id form, so there is no
// format to build. The id inside it is a **number**, as the space routes are.
func TestUserInfoReadsThePersonalSpaceExpansion(t *testing.T) {
	c := userRouteServer(t, `{"accountId":"60c36d07","displayName":"William Kahn-Greene",
		"accountType":"atlassian","accountStatus":"active",
		"personalSpace":{"id":76646426,"key":"~awu@mozilla.com","name":"A Person"}}`)
	u, err := c.UserInfo("60c36d07")
	if err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	if u.PersonalSpace == nil {
		t.Fatal("PersonalSpace is nil")
	}
	if u.PersonalSpace.Key != "~awu@mozilla.com" {
		t.Errorf("key = %q: an email-form key must survive, since it cannot be derived",
			u.PersonalSpace.Key)
	}
	if u.PersonalSpace.ID != "76646426" {
		t.Errorf("id = %q, want the number decoded as a string", u.PersonalSpace.ID)
	}
}

// An unknown account id is nil rather than an error, so the caller words it.
func TestUserInfoUnknownAccountIsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"statusCode":404,"message":"No user found with account id"}`))
	}))
	t.Cleanup(srv.Close)
	c := New(Config{SiteURL: srv.URL, Username: "u", Token: "t"})
	u, err := c.UserInfo("nobody")
	if err != nil {
		t.Fatalf("UserInfo = %v, want nil error", err)
	}
	if u != nil {
		t.Errorf("user = %+v, want nil", u)
	}
}

// The two identity routes are distinct, and each form must ask its own.
func TestTheTwoIdentityRoutes(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accountId":"a","displayName":"A"}`))
	}))
	t.Cleanup(srv.Close)
	c := New(Config{SiteURL: srv.URL, Username: "u", Token: "t"})
	if _, err := c.CurrentUser(); err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if _, err := c.UserInfo("a"); err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	want := []string{"/wiki/rest/api/user/current", "/wiki/rest/api/user"}
	for i, w := range want {
		if paths[i] != w {
			t.Errorf("paths = %v, want %v", paths, want)
			break
		}
	}
}

func userRouteServer(t *testing.T, body string) *ConfluenceClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(Config{SiteURL: srv.URL, Username: "u", Token: "t"})
}

// The personal space URL comes from the response's own link, made absolute
// against SiteURL. Building it from the key instead would need an escaping
// rule -- an email-keyed personal space links as /spaces/~a@example.com, with
// the @ unescaped -- and inventing one here is a second place to get it wrong.
func TestPersonalSpaceURLComesFromTheLink(t *testing.T) {
	c := userRouteServer(t, `{"accountId":"a","displayName":"A Person",
		"personalSpace":{"id":766,"key":"~a@example.com","name":"A Person",
		"_links":{"webui":"/spaces/~a@example.com"}}}`)
	u, err := c.CurrentUser()
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if want := c.SiteURL() + "/wiki/spaces/~a@example.com"; u.PersonalSpace.URL != want {
		t.Errorf("URL = %q, want %q", u.PersonalSpace.URL, want)
	}
}

// No link means no URL, not a half-built one.
func TestPersonalSpaceWithoutALinkHasNoURL(t *testing.T) {
	c := userRouteServer(t, `{"accountId":"a","personalSpace":{"id":1,"key":"~a","name":"A"}}`)
	u, err := c.CurrentUser()
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if u.PersonalSpace.URL != "" {
		t.Errorf("URL = %q, want empty", u.PersonalSpace.URL)
	}
}
