package userinfo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
	"github.com/mozilla/markfluence/internal/ui"
)

// stub serves the two identity routes and the space directory.
type stub struct {
	// user is the JSON body both identity routes return.
	user string
	// spacePages maps an offset to that page's rows, so a test can make the
	// directory answer short pages that are not the end.
	spacePages map[int]string
	notFound   bool
}

func (s stub) client(t *testing.T) (*client.ConfluenceClient, *[]string) {
	t.Helper()
	var paths []string
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/wiki/rest/api/space":
			start, _ := strconv.Atoi(r.URL.Query().Get("start"))
			rows, ok := s.spacePages[start]
			if !ok {
				rows = "[]"
			}
			_, _ = fmt.Fprintf(w, `{"results":%s,"start":%d}`, rows, start)
		default:
			if s.notFound {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"statusCode":404,"message":"No user found"}`))
				return
			}
			body := s.user
			if body == "" {
				body = `{"accountId":"acc-1","displayName":"A Person","email":"a@example.com",
					"accountType":"atlassian","accountStatus":"active"}`
			}
			_, _ = w.Write([]byte(body))
		}
	})
	return c, &paths
}

// spaceRow builds one row of the space collection. The id is derived from the
// key rather than fixed, because the survey deduplicates by id: a shared id
// would make every row in a fixture collapse into one.
func spaceRow(key string, ops ...string) string {
	items := make([]string, 0, len(ops))
	for _, o := range ops {
		parts := strings.SplitN(o, ":", 2)
		items = append(items, fmt.Sprintf(`{"operation":%q,"targetType":%q}`, parts[0], parts[1]))
	}
	id := 0
	for _, r := range key {
		id = id*31 + int(r)
	}
	return fmt.Sprintf(`{"id":%d,"key":%q,"name":"N","operations":[%s]}`,
		id, key, strings.Join(items, ","))
}

func (s stub) build(t *testing.T, accountID string, spaces bool) report {
	t.Helper()
	c, _ := s.client(t)
	user, err := lookup(c, accountID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if user == nil {
		t.Fatal("lookup returned no user")
	}
	rep := report{user: user, self: accountID == ""}
	if spaces {
		survey, err := surveySpaces(c)
		if err != nil {
			t.Fatalf("surveySpaces: %v", err)
		}
		rep.spaces = survey
	}
	return rep
}

// --- identity ------------------------------------------------------------------

// No argument asks the current-user route; an id asks the by-id one. The two
// see different things, which is why both forms exist.
func TestTheFormsAskDifferentRoutes(t *testing.T) {
	s := stub{}
	c, paths := s.client(t)
	if _, err := lookup(c, ""); err != nil {
		t.Fatalf("lookup(self): %v", err)
	}
	if _, err := lookup(c, "acc-9"); err != nil {
		t.Fatalf("lookup(id): %v", err)
	}
	if (*paths)[0] != "/wiki/rest/api/user/current" || (*paths)[1] != "/wiki/rest/api/user" {
		t.Errorf("paths = %v", *paths)
	}
	// Neither form touches the user *directory*, which cannot see a
	// deactivated account and needs a scope these routes do not.
	for _, p := range *paths {
		if strings.Contains(p, "/search/user") {
			t.Errorf("asked the user directory: %v", *paths)
		}
	}
}

// self records which question was asked, so a consumer is not left guessing
// whether the result describes the caller.
func TestSelfDistinguishesTheTwoForms(t *testing.T) {
	if self := (stub{}).build(t, "", false).jsonResult(); !self.Self {
		t.Error("self is false for the no-argument form")
	}
	if other := (stub{}).build(t, "acc-1", false).jsonResult(); other.Self {
		t.Error("self is true for an explicit account id")
	}
}

// An app account is a service-account token, which is most of why permissions
// surprise people -- so the human output says so rather than printing "app".
func TestAppAccountIsSpelledOut(t *testing.T) {
	s := stub{user: `{"accountId":"712020:abc","displayName":"SRE API Token","accountType":"app",
		"accountStatus":"active"}`}
	out := s.build(t, "", false).human()
	if !strings.Contains(out, "app (a service account)") {
		t.Errorf("output = %q, want the account type explained", out)
	}
}

// external and guest print even when false: for a restricted-access question,
// a negative is information, so the row must not be omitted as empty.
func TestExternalAndGuestPrintWhenFalse(t *testing.T) {
	out := (stub{}).build(t, "", false).human()
	for _, want := range []string{"external:", "guest:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want a %q row even when false", out, want)
		}
	}
}

// A deactivated account resolves here and nowhere else -- half the reason the
// one-argument form exists.
func TestADeactivatedAccountResolves(t *testing.T) {
	s := stub{user: `{"accountId":"old-1","displayName":"Mark Reid (Deactivated)",
		"accountType":"atlassian","accountStatus":"inactive"}`}
	res := s.build(t, "old-1", false).jsonResult()
	if res.DisplayName != "Mark Reid (Deactivated)" {
		t.Errorf("display_name = %q", res.DisplayName)
	}
	if res.AccountStatus != "inactive" {
		t.Errorf("account_status = %q", res.AccountStatus)
	}
}

// No personal space is a real answer, printed as such -- not an omitted row
// that reads like a failed lookup.
func TestNoPersonalSpaceSaysNone(t *testing.T) {
	s := stub{user: `{"accountId":"712020:abc","displayName":"SRE API Token","accountType":"app"}`}
	r := s.build(t, "", false)
	if !strings.Contains(r.human(), "personal space: (none)") {
		t.Errorf("output = %q, want an explicit (none)", r.human())
	}
	if res := r.jsonResult(); res.PersonalSpace != nil {
		t.Errorf("personal_space = %+v, want null", res.PersonalSpace)
	}
}

// The key comes from the expansion, never from the account id: email-form and
// id-form personal space keys are both live on one instance.
func TestPersonalSpaceKeyIsNotDerived(t *testing.T) {
	s := stub{user: `{"accountId":"60c36d07","displayName":"A Person",
		"personalSpace":{"id":766,"key":"~a@example.com","name":"A Person",
		"_links":{"webui":"/spaces/~a@example.com"}}}`}
	r := s.build(t, "60c36d07", false)
	res := r.jsonResult()
	// The nil check first: if user() ever regresses to dropping a space the
	// response carried -- the regression this test exists for -- reading a
	// field off it panics instead of reporting the failure.
	if res.PersonalSpace == nil {
		t.Fatal("personal_space is null though the response carried one")
	}
	if res.PersonalSpace.Key != "~a@example.com" {
		t.Fatalf("personal_space = %+v, want the key as the API gave it", res.PersonalSpace)
	}
	if res.PersonalSpace.URL == "" {
		t.Error("personal_space.url is empty though the response carried a link")
	}
	// The URL is what a reader wants to click, and the human row shows it
	// rather than the name, which is the display name already a line above.
	if !strings.Contains(r.human(), "/wiki/spaces/~a@example.com") {
		t.Errorf("output = %q, want the personal space URL", r.human())
	}
	if res.PersonalSpace.Key == "~"+res.AccountID {
		t.Error("the key looks derived from the account id, which is not a valid construction")
	}
}

// --- the space survey ----------------------------------------------------------

// An account id is never surveyed, and the point is attribution rather than
// cost: the route reports what the *authenticated* account may do, so printing
// it under a named account's name would be a wrong answer.
func TestAnAccountIDIsNotSurveyed(t *testing.T) {
	s := stub{spacePages: map[int]string{0: "[" + spaceRow("ENG", "read:space") + "]"}}
	c, paths := s.client(t)
	user, err := lookup(c, "acc-1")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	rep := report{user: user, self: false}
	for _, p := range *paths {
		if p == "/wiki/rest/api/space" {
			t.Errorf("surveyed spaces for an account the caller named: %v", *paths)
		}
	}
	if res := rep.jsonResult(); res.Spaces != nil {
		t.Errorf("spaces = %+v, want null for a named account", res.Spaces)
	}
}

// create:page is the write predicate, administer:space the admin one, and
// read-only or comment-only spaces are neither.
func TestSurveyMapsOperations(t *testing.T) {
	s := stub{spacePages: map[int]string{0: "[" + strings.Join([]string{
		spaceRow("READONLY", "read:space"),
		spaceRow("COMMENTS", "read:space", "create:comment"),
		spaceRow("WRITEABLE", "read:space", "create:page"),
		spaceRow("OWNED", "read:space", "create:page", "administer:space"),
		// export:space is not admin: an admin's row carries it, but it is not
		// the permission, so the predicate stays administer:space alone.
		spaceRow("EXPORTABLE", "read:space", "export:space"),
	}, ",") + "]"}}
	res := s.build(t, "", true).jsonResult()
	if res.Spaces.Visible != 5 {
		t.Errorf("visible = %d, want 5", res.Spaces.Visible)
	}
	if strings.Join(res.Spaces.Write, ",") != "OWNED,WRITEABLE" {
		t.Errorf("write = %v, want the two create:page spaces, sorted", res.Spaces.Write)
	}
	if strings.Join(res.Spaces.Admin, ",") != "OWNED" {
		t.Errorf("admin = %v, want only the administer:space one", res.Spaces.Admin)
	}
}

// The survey walks past a short page, because this directory returns them
// mid-collection -- the trap internal/client's TestAShortSpacePageIsNotTheEnd
// pins at the route level, checked here through the command's own path.
func TestSurveyWalksPastAShortPage(t *testing.T) {
	s := stub{spacePages: map[int]string{
		0: "[" + spaceRow("A", "read:space", "create:page") + "]",
		1: "[" + spaceRow("B", "read:space", "create:page") + "]",
	}}
	res := s.build(t, "", true).jsonResult()
	if res.Spaces.Visible != 2 {
		t.Errorf("visible = %d, want 2: a short page is not the end here", res.Spaces.Visible)
	}
}

// An account that may create pages nowhere gets [] -- a real answer, distinct
// from the null that means "not surveyed".
func TestNoWriteAccessIsAnEmptyListNotNull(t *testing.T) {
	s := stub{spacePages: map[int]string{0: "[" + spaceRow("READONLY", "read:space") + "]"}}
	r := s.build(t, "", true)
	res := r.jsonResult()
	if res.Spaces == nil || res.Spaces.Write == nil || len(res.Spaces.Write) != 0 {
		t.Errorf("write = %+v, want an empty list", res.Spaces)
	}
	if !strings.Contains(r.human(), "write access:   none") {
		t.Errorf("output = %q, want it to say none explicitly", r.human())
	}
}

// The output says "create pages", never "write access to pages": editing an
// existing page is not a space grant (#168/#170).
func TestTheSurveyDoesNotPromisePageEditing(t *testing.T) {
	s := stub{spacePages: map[int]string{0: "[" + strings.Join([]string{
		spaceRow("A", "read:space", "create:page"),
	}, ",") + "]"}}
	out := s.build(t, "", true).human()
	if !strings.Contains(out, "write access:") {
		t.Errorf("output = %q, want a write access row", out)
	}
	if strings.Contains(out, "edit") {
		t.Errorf("output = %q, must not promise editing", out)
	}
}

// A long list is summarised rather than printed: 97 keys on one line is not
// output anybody reads, and --json has them.
func TestALongListIsSummarised(t *testing.T) {
	rows := make([]string, 0, 20)
	for i := range 20 {
		rows = append(rows, spaceRow(fmt.Sprintf("S%02d", i), "read:space", "create:page"))
	}
	s := stub{spacePages: map[int]string{0: "[" + strings.Join(rows, ",") + "]"}}
	r := s.build(t, "", true)
	out := r.human()
	if !strings.Contains(out, "write access:   20 (too many to list; see --json output)") {
		t.Errorf("output = %q, want a count and a pointer to --json", out)
	}
	// The total belongs to the "visible spaces" row alone: repeating it on
	// this line and the admin one put the same number on screen three times.
	if strings.Count(out, "20 of 20") > 0 {
		t.Errorf("output = %q, must not repeat the visible total", out)
	}
	// The keys are still all there in --json.
	if res := r.jsonResult(); len(res.Spaces.Write) != 20 {
		t.Errorf("write = %d keys, want all 20 in --json", len(res.Spaces.Write))
	}
}

// --- failure and contract ------------------------------------------------------

func TestUnknownAccountIsNil(t *testing.T) {
	c, _ := stub{notFound: true}.client(t)
	user, err := lookup(c, "nobody")
	if err != nil {
		t.Fatalf("lookup = %v, want nil so the caller words it", err)
	}
	if user != nil {
		t.Errorf("user = %+v, want nil", user)
	}
}

// Exit codes follow the contract page-info and space-info draw: 2 for a bad
// invocation, 1 for a request that was fine and answered no.
func TestExitCodes(t *testing.T) {
	if got := ui.ExitCode(operationalFail("no such account", jsonout.CodeNotFound)); got != 1 {
		t.Errorf("operational exit = %d, want 1", got)
	}
	if got := ui.ExitCode(fatalFail("no account id given", jsonout.CodeValidation)); got != 2 {
		t.Errorf("usage exit = %d, want 2", got)
	}
}

// A blank argument is refused before credentials resolve, so a local defect
// does not report itself as a missing token.
func TestBlankArgumentIsRefusedBeforeCredentials(t *testing.T) {
	t.Setenv("CONFLUENCE_URL", "")
	t.Setenv("CONFLUENCE_USERNAME", "")
	t.Setenv("CONFLUENCE_TOKEN", "")
	err := run(Cmd, []string{"  "})
	if !ui.IsSilent(err) || ui.ExitCode(err) != 2 {
		t.Fatalf("run = %v, want a silent exit-2 error", err)
	}
}

// Built with the command's own builder, so a renamed key has to reach the
// schema through this test rather than validating a copy.
func TestSchemaConformance(t *testing.T) {
	cases := map[string]report{
		"self, no survey": (stub{}).build(t, "", false),
		"an app account with no personal space": stub{
			user: `{"accountId":"712020:abc","displayName":"SRE API Token","accountType":"app"}`,
		}.build(t, "", false),
		"another account, surveyed": stub{
			user: `{"accountId":"60c36d07","displayName":"A Person",
				"personalSpace":{"id":766,"key":"~a@example.com","name":"A Person",
				"_links":{"webui":"/spaces/~a@example.com"}}}`,
			spacePages: map[int]string{0: "[" + spaceRow("A", "read:space", "create:page") + "]"},
		}.build(t, "60c36d07", true),
	}
	for name, rep := range cases {
		t.Run(name, func(t *testing.T) {
			env := jsonout.NewEnvelope("user-info", []any{rep.jsonResult()},
				map[string]int{"total": 1, "succeeded": 1, "failed": 0})
			var buf bytes.Buffer
			if err := jsonout.Emit(&buf, env); err != nil {
				t.Fatalf("Emit: %v", err)
			}
			schematest.ValidateEnvelope(t, buf.Bytes())
		})
	}
}

func TestFatalFailValidates(t *testing.T) {
	var buf bytes.Buffer
	if err := jsonout.EmitError(&buf, "user-info", "no such account", jsonout.CodeNotFound); err != nil {
		t.Fatalf("EmitError: %v", err)
	}
	schematest.ValidateError(t, buf.Bytes())
}

// Every field always marshals, per internal/schematest's rule.
func TestEveryFieldMarshals(t *testing.T) {
	blob, err := json.Marshal((stub{}).build(t, "", false).jsonResult())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{
		"ok", "self", "account_id", "display_name", "public_name", "email",
		"account_type", "account_status", "external_collaborator", "guest",
		"personal_space", "spaces",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("result is missing %q", key)
		}
	}
}

func TestCommandIsInTheSchemaEnum(t *testing.T) {
	if !strings.Contains(strings.Join(schematest.Commands(t), " "), "user-info") {
		t.Error("user-info is not in the schema's command enum")
	}
}

// The attribution rule, through run() rather than around it.
//
// Deleting the `if rep.self` guard leaves every other test in this file
// passing while the command starts printing the caller's writable spaces
// under whatever account id was named -- which is the wrong answer the guard
// exists to prevent. So this drives the real entry point and asserts on the
// requests the server saw.
func TestRunNeverSurveysForANamedAccount(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/wiki/rest/api/space" {
			_, _ = w.Write([]byte(`{"results":[` + spaceRow("ENG", "read:space", "create:page") + `]}`))
			return
		}
		_, _ = w.Write([]byte(`{"accountId":"someone-else","displayName":"Someone Else"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CONFLUENCE_URL", srv.URL)
	t.Setenv("CONFLUENCE_USERNAME", "u")
	t.Setenv("CONFLUENCE_TOKEN", "t")

	out := captureStdout(t, func() {
		if err := run(Cmd, []string{"someone-else"}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	for _, p := range paths {
		if p == "/wiki/rest/api/space" {
			t.Errorf("surveyed spaces for a named account: %v", paths)
		}
	}
	for _, leaked := range []string{"visible spaces", "write access", "admin access", "ENG"} {
		if strings.Contains(out, leaked) {
			t.Errorf("output attributes the caller's access to a named account:\n%s", out)
		}
	}
}

// And the no-argument form does survey, so the guard is not simply off.
func TestRunSurveysForTheSelfForm(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/wiki/rest/api/space" {
			start := r.URL.Query().Get("start")
			if start != "0" {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[` + spaceRow("ENG", "read:space", "create:page") + `]}`))
			return
		}
		_, _ = w.Write([]byte(`{"accountId":"me","displayName":"Me"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CONFLUENCE_URL", srv.URL)
	t.Setenv("CONFLUENCE_USERNAME", "u")
	t.Setenv("CONFLUENCE_TOKEN", "t")

	out := captureStdout(t, func() {
		if err := run(Cmd, nil); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "write access:   1 -- ENG") {
		t.Errorf("the no-argument form did not survey:\n%s", out)
	}
}

// captureStdout runs fn with stdout redirected, returning what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = saved
	return <-done
}

// The flag is gone: the survey is part of the no-argument form and is absent
// from the other, because the route answers for the authenticated account.
func TestThereIsNoSpacesFlag(t *testing.T) {
	if f := Cmd.Flags().Lookup("spaces"); f != nil {
		t.Error("--spaces exists; the survey belongs to the no-argument form, " +
			"and cannot describe an account the caller named")
	}
}

// The no-argument form is marked, so the two forms do not produce
// structurally identical blocks with no sign of which account is described.
func TestTheSelfFormIsMarked(t *testing.T) {
	if out := (stub{}).build(t, "", false).human(); !strings.Contains(out, "(these credentials)") {
		t.Errorf("output = %q, want the self form marked", out)
	}
	if out := (stub{}).build(t, "acc-1", false).human(); strings.Contains(out, "these credentials") {
		t.Errorf("output = %q, must not claim somebody else is you", out)
	}
}

// The survey deduplicates by space id. Measured, this route does not repeat a
// row -- 533 rows, 533 distinct keys -- so this guards against a paging
// semantic that was not observed rather than one that was. It is worth having
// because a duplicate would inflate "visible spaces" and list a key twice,
// neither of which a reader can check.
func TestTheSurveyDeduplicates(t *testing.T) {
	row := spaceRow("ENG", "read:space", "create:page")
	s := stub{spacePages: map[int]string{
		0: "[" + row + "]",
		1: "[" + row + "]", // the same space again, as an overlapping window would
	}}
	res := s.build(t, "", true).jsonResult()
	if res.Spaces.Visible != 1 {
		t.Errorf("visible = %d, want 1: a repeated row must not inflate the count",
			res.Spaces.Visible)
	}
	if len(res.Spaces.Write) != 1 {
		t.Errorf("write = %v, want the key once", res.Spaces.Write)
	}
}
