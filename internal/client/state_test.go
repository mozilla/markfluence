package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stateRequest is one request the stub saw.
type stateRequest struct {
	method string
	path   string
	query  string
	body   string
}

// stateServer serves the v1 content-state routes, recording every request.
// respond writes the body for a GET of /state; the available route and the PUT
// get fixed answers unless respond handles them.
func stateServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (
	*ConfluenceClient, *[]stateRequest,
) {
	t.Helper()
	var seen []stateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, stateRequest{
			method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body),
		})
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(Config{SiteURL: srv.URL, Username: "u", Token: "t"}), &seen
}

// The safety property this whole feature rests on: a PUT carrying a name the
// server does not recognise *creates* a status that no API route can delete, so
// SetPageState must send the id and nothing else. See state.go and
// docs/confluence/page-status.md.
func TestSetPageStateSendsOnlyTheID(t *testing.T) {
	c, seen := stateServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"contentState":{"id":7,"name":"Verified","color":"#1d7afc"}}`))
	})
	if err := c.SetPageState("123", 7); err != nil {
		t.Fatalf("SetPageState: %v", err)
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %d, want 1", len(*seen))
	}
	got := (*seen)[0]
	if got.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", got.method)
	}
	if got.path != "/wiki/rest/api/content/123/state" {
		t.Errorf("path = %q", got.path)
	}
	// ?status=current names the *content status* to act on, not the state.
	if got.query != "status=current" {
		t.Errorf("query = %q, want status=current", got.query)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(got.body), &payload); err != nil {
		t.Fatalf("body %q: %v", got.body, err)
	}
	if len(payload) != 1 {
		t.Fatalf("body = %q, want exactly one key", got.body)
	}
	if _, ok := payload["id"]; !ok {
		t.Errorf("body = %q, want an id key", got.body)
	}
	for _, forbidden := range []string{"name", "color"} {
		if _, ok := payload[forbidden]; ok {
			t.Errorf("body carries %q: a named PUT creates a status no route can delete", forbidden)
		}
	}
}

// "No status" arrives as a missing contentState object, in two shapes: {} for a
// page that never had one, and a lone lastUpdated for one whose status was
// removed. Both must read as nil rather than as a status with a zero id.
func TestPageStateAbsenceIsNotAZeroValue(t *testing.T) {
	for name, body := range map[string]string{
		"never had one": `{}`,
		"was removed":   `{"lastUpdated":"2026-09-18T11:01:06.372Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := stateServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			})
			state, err := c.PageState("123")
			if err != nil {
				t.Fatalf("PageState: %v", err)
			}
			if state != nil {
				t.Errorf("PageState = %+v, want nil", state)
			}
		})
	}
}

func TestPageStateReportsTheStatus(t *testing.T) {
	c, seen := stateServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			`{"contentState":{"id":3086974986,"color":"#57d9a3","name":"Ready for review"},` +
				`"lastUpdated":"2026-09-18T10:51:49.179Z"}`))
	})
	state, err := c.PageState("123")
	if err != nil {
		t.Fatalf("PageState: %v", err)
	}
	if state == nil || state.ID != 3086974986 || state.Name != "Ready for review" {
		t.Fatalf("PageState = %+v", state)
	}
	if (*seen)[0].method != http.MethodGet || (*seen)[0].query != "status=current" {
		t.Errorf("request = %+v", (*seen)[0])
	}
}

// The two halves of the vocabulary are kept apart, not merged: custom statuses
// follow the account rather than the space, so only the space's own are valid
// for a file. Merging them is what would make a committed page_status: publish
// for its author and fail for a colleague.
func TestAvailableStatesKeepsTheTwoHalvesApart(t *testing.T) {
	c, seen := stateServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"spaceContentStates":[
			{"id":0,"color":"#ffc400","name":"Rough draft"},
			{"id":1,"color":"#2684ff","name":"In progress"}],
			"customContentStates":[{"id":3087237122,"color":"#ff0000","name":"Totally made up"}]}`))
	})
	got, err := c.AvailableStates("123")
	if err != nil {
		t.Fatalf("AvailableStates: %v", err)
	}
	if len(got.Space) != 2 || got.Space[0].Name != "Rough draft" {
		t.Errorf("Space = %+v", got.Space)
	}
	if len(got.Custom) != 1 || got.Custom[0].Name != "Totally made up" {
		t.Errorf("Custom = %+v", got.Custom)
	}
	if (*seen)[0].path != "/wiki/rest/api/content/123/state/available" {
		t.Errorf("path = %q", (*seen)[0].path)
	}
	// The available route takes no status parameter; sending one would be
	// harmless but is not what was measured.
	if (*seen)[0].query != "" {
		t.Errorf("query = %q, want none", (*seen)[0].query)
	}
}

// An id that does not exist is a clean 404 naming it, and the message reaches
// the caller intact -- the one signal that tells a disabled status apart from a
// request that lost its id (which 400s about a colour instead).
func TestSetPageStateUnknownIDReportsTheServerMessage(t *testing.T) {
	c, _ := stateServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(
			`{"statusCode":404,"message":"Content state with 999999999 id does not exist, or is disabled"}`))
	})
	err := c.SetPageState("123", 999999999)
	if err == nil {
		t.Fatal("SetPageState succeeded, want a 404")
	}
	if !strings.Contains(err.Error(), "does not exist, or is disabled") {
		t.Errorf("err = %v, want the server's own wording", err)
	}
}
