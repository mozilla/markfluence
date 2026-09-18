package pagestatus

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

// vocabulary is the space's four defaults, in the space's own order.
const vocabulary = `{"spaceContentStates":[
	{"id":10,"color":"#ffc400","name":"Rough draft"},
	{"id":11,"color":"#2684ff","name":"In progress"},
	{"id":12,"color":"#57d9a3","name":"Ready for review"},
	{"id":13,"color":"#1d7afc","name":"Verified"}],
	"customContentStates":[]}`

// server serves the two state routes, recording each request as
// "METHOD path[?query]". current is the body for a GET of /state; available is
// the body for /state/available.
func server(t *testing.T, current, available string) (*client.ConfluenceClient, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		line := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			line += "?" + r.URL.RawQuery
		}
		seen = append(seen, line)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/state/available"):
			_, _ = w.Write([]byte(available))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(current))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL, Username: "u", Token: "t"}), &seen
}

func TestDeclared(t *testing.T) {
	for name, tc := range map[string]struct {
		fields    map[string]string
		want      string
		declared  bool
		wantError bool
	}{
		"absent":         {fields: map[string]string{}, declared: false},
		"a name":         {fields: map[string]string{Field: "Ready for review"}, want: "Ready for review", declared: true},
		"trimmed":        {fields: map[string]string{Field: "  Verified  "}, want: "Verified", declared: true},
		"present, empty": {fields: map[string]string{Field: ""}, wantError: true},
		"whitespace":     {fields: map[string]string{Field: "   "}, wantError: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, declared, err := Declared(tc.fields)
			if tc.wantError {
				if err == nil {
					t.Fatalf("Declared = (%q, %v, nil), want an error", got, declared)
				}
				return
			}
			if err != nil {
				t.Fatalf("Declared: %v", err)
			}
			if got != tc.want || declared != tc.declared {
				t.Errorf("Declared = (%q, %v), want (%q, %v)", got, declared, tc.want, tc.declared)
			}
		})
	}
}

// An absent key must reach no request at all -- not a read, not a write. The
// property matters more than "no write": the read alone would cost one request
// per page on every tree that does not use the field.
func TestAnAbsentFieldAsksNothing(t *testing.T) {
	c, seen := server(t, `{}`, vocabulary)
	name, declared, err := Declared(map[string]string{"title": "T"})
	if err != nil || declared || name != "" {
		t.Fatalf("Declared = (%q, %v, %v)", name, declared, err)
	}
	if len(*seen) != 0 {
		t.Errorf("requests = %v, want none", *seen)
	}
	_ = c
}

func TestResolveIsCaseInsensitive(t *testing.T) {
	c, _ := server(t, `{}`, vocabulary)
	for _, spelling := range []string{"Ready for review", "ready for review", "READY FOR REVIEW"} {
		got, err := Resolve(c, "123", spelling)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", spelling, err)
		}
		if got.ID != 12 {
			t.Errorf("Resolve(%q).ID = %d, want 12", spelling, got.ID)
		}
		// The canonical spelling comes back, which is what read/export emit.
		if got.Name != "Ready for review" {
			t.Errorf("Resolve(%q).Name = %q, want the space's spelling", spelling, got.Name)
		}
	}
}

// The error is where most authors first learn what a space allows, since the
// vocabulary is per-space server state: nothing in --help or completion can
// carry it.
func TestResolveUnknownNamesTheSpacesOwn(t *testing.T) {
	c, _ := server(t, `{}`, vocabulary)
	_, err := Resolve(c, "123", "Reviewed")
	if err == nil {
		t.Fatal("Resolve succeeded, want an error")
	}
	for _, want := range []string{"Reviewed", "Rough draft", "In progress", "Ready for review", "Verified"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to name %q", err, want)
		}
	}
}

// A name matching only a *custom* status gets its own message. Custom statuses
// follow the account, so this is the file that works for its author and fails
// for everyone else -- and a message that listed only the space's statuses,
// while the author can see theirs in Confluence's own picker, is how that
// becomes an unresolvable bug report.
func TestResolveRefusesACustomStatusByName(t *testing.T) {
	available := `{"spaceContentStates":[{"id":10,"name":"Rough draft","color":"#ffc400"}],
		"customContentStates":[{"id":99,"name":"Mine alone","color":"#ff0000"}]}`
	c, _ := server(t, `{}`, available)
	_, err := Resolve(c, "123", "Mine alone")
	if err == nil {
		t.Fatal("Resolve succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "your own custom statuses") {
		t.Errorf("err = %v, want it to explain that a custom status is account-scoped", err)
	}
	if !strings.Contains(err.Error(), "Rough draft") {
		t.Errorf("err = %v, want it to name what the page can be given", err)
	}
}

// Two of the space's statuses differing only in case is ambiguous, and guessing
// between them would publish one of two different lozenges at random.
func TestResolveRefusesAnAmbiguousMatch(t *testing.T) {
	available := `{"spaceContentStates":[
		{"id":1,"name":"Draft","color":"#ffc400"},
		{"id":2,"name":"draft","color":"#2684ff"}],"customContentStates":[]}`
	c, _ := server(t, `{}`, available)
	_, err := Resolve(c, "123", "DRAFT")
	if err == nil {
		t.Fatal("Resolve succeeded, want an ambiguity error")
	}
	if !strings.Contains(err.Error(), "more than one") {
		t.Errorf("err = %v, want an ambiguity error", err)
	}
}

// The read in Apply is not an optimisation: a state write bumps the page
// version, so an already-matching status must produce no PUT at all or every
// run adds a version to every page.
func TestApplyDoesNotWriteAnUnchangedStatus(t *testing.T) {
	current := `{"contentState":{"id":12,"name":"Ready for review","color":"#57d9a3"}}`
	c, seen := server(t, current, vocabulary)
	action, err := Apply(c, "123", client.ContentState{ID: 12, Name: "Ready for review"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if action.Action != "unchanged" {
		t.Errorf("Action = %q, want unchanged", action.Action)
	}
	for _, req := range *seen {
		if strings.HasPrefix(req, "PUT") {
			t.Errorf("requests = %v, want no PUT", *seen)
		}
	}
}

func TestApplyWritesAChangedStatus(t *testing.T) {
	current := `{"contentState":{"id":10,"name":"Rough draft","color":"#ffc400"}}`
	c, seen := server(t, current, vocabulary)
	action, err := Apply(c, "123", client.ContentState{ID: 12, Name: "Ready for review"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if action.Action != "set" || action.Name != "Ready for review" {
		t.Errorf("Action = %+v, want set/Ready for review", action)
	}
	var puts int
	for _, req := range *seen {
		if strings.HasPrefix(req, "PUT") {
			puts++
		}
	}
	if puts != 1 {
		t.Errorf("requests = %v, want exactly one PUT", *seen)
	}
}

// A page with no status takes the write path: nothing to find unchanged.
func TestApplySetsAStatusOnAPageWithNone(t *testing.T) {
	c, seen := server(t, `{"lastUpdated":"2026-09-18T11:01:06Z"}`, vocabulary)
	action, err := Apply(c, "123", client.ContentState{ID: 11, Name: "In progress"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if action.Action != "set" {
		t.Errorf("Action = %q, want set", action.Action)
	}
	if len(*seen) != 2 {
		t.Errorf("requests = %v, want a GET then a PUT", *seen)
	}
}

// Available reports the space's own statuses and not the caller's custom ones:
// info's row describes the page for whoever reads it, not for whoever ran it.
//
// The per-page rule Resolve rests on is measured in
// docs/confluence/page-status.md and cannot be stubbed usefully here; what is
// pinned is that nothing caches one page's answer for another, which is now
// true by construction -- Resolve takes the page it is asking about and holds
// no state.
func TestAvailableDropsCustomStatuses(t *testing.T) {
	available := `{"spaceContentStates":[{"id":10,"name":"Rough draft","color":"#ffc400"}],
		"customContentStates":[{"id":99,"name":"Mine alone","color":"#ff0000"}]}`
	c, _ := server(t, `{}`, available)
	got, err := Available(c, "123")
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Rough draft" {
		t.Errorf("Available = %+v, want the space's own only", got)
	}
}

// A page that can be given nothing, by a caller holding a same-named custom
// status, must still get a sentence that finishes: the custom branch used to
// run first and end "This page can be given " with an empty list.
func TestUnknownStatusWithNoSpaceStatusesReadsWell(t *testing.T) {
	available := `{"spaceContentStates":[],
		"customContentStates":[{"id":99,"name":"Mine alone","color":"#ff0000"}]}`
	c, _ := server(t, `{}`, available)
	_, err := Resolve(c, "123", "Mine alone")
	if err == nil {
		t.Fatal("Resolve succeeded, want a refusal")
	}
	if strings.HasSuffix(err.Error(), "given ") || strings.HasSuffix(err.Error(), ": ") {
		t.Errorf("err = %q, want a sentence that finishes", err)
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Errorf("err = %q, want the custom-status reason kept", err)
	}
}
