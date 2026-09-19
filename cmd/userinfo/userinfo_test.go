package userinfo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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

func spaceRow(key string, ops ...string) string {
	items := make([]string, 0, len(ops))
	for _, o := range ops {
		parts := strings.SplitN(o, ":", 2)
		items = append(items, fmt.Sprintf(`{"operation":%q,"targetType":%q}`, parts[0], parts[1]))
	}
	return fmt.Sprintf(`{"id":1,"key":%q,"name":"N","operations":[%s]}`, key, strings.Join(items, ","))
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
		"personalSpace":{"id":766,"key":"~a@example.com","name":"A Person"}}`}
	res := s.build(t, "60c36d07", false).jsonResult()
	if res.PersonalSpace == nil || res.PersonalSpace.Key != "~a@example.com" {
		t.Fatalf("personal_space = %+v, want the key as the API gave it", res.PersonalSpace)
	}
	if res.PersonalSpace.Key == "~"+res.AccountID {
		t.Error("the key looks derived from the account id, which is not a valid construction")
	}
}

// --- the space survey ----------------------------------------------------------

// Without --spaces nothing is surveyed: the identity half is one request and
// the survey is a walk, which is why it is opt-in.
func TestWithoutSpacesNothingIsWalked(t *testing.T) {
	s := stub{spacePages: map[int]string{0: "[" + spaceRow("ENG", "read:space") + "]"}}
	c, paths := s.client(t)
	user, err := lookup(c, "")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	rep := report{user: user, self: true}
	for _, p := range *paths {
		if p == "/wiki/rest/api/space" {
			t.Errorf("walked the space directory without --spaces: %v", *paths)
		}
	}
	if res := rep.jsonResult(); res.Spaces != nil {
		t.Errorf("spaces = %+v, want null when not asked for", res.Spaces)
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
	if !strings.Contains(r.human(), "none of 1 spaces") {
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
	if !strings.Contains(out, "20 of 20 spaces") || !strings.Contains(out, "--json") {
		t.Errorf("output = %q, want a count and a pointer to --json", out)
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
				"personalSpace":{"id":766,"key":"~a@example.com","name":"A Person"}}`,
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
